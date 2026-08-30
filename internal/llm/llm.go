// Package llm turns human input into todo lists using the Claude API.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/arvlas/todo-bot/internal/domain"
)

// maxTokens caps one reply. A list long enough to exceed this is longer than
// anyone will act on, and the cap keeps a runaway response from costing real
// money.
const maxTokens = 4000

// Client builds and edits lists. It holds no conversation state: an edit is one
// call carrying the current list, which keeps every request the same size no
// matter how many times a list has been revised.
type Client struct {
	api   anthropic.Client
	model string
}

// New builds a client against the given model. A non-empty baseURL points at a
// different API host, which is what lets the offline suite run the bot without
// a network or a budget.
func New(apiKey, model, baseURL string) *Client {
	options := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		options = append(options, option.WithBaseURL(baseURL))
	}

	return &Client{
		api:   anthropic.NewClient(options...),
		model: model,
	}
}

// Create turns a message into a new list.
func (c *Client) Create(ctx context.Context, message string) (domain.List, error) {
	return c.complete(ctx, fmt.Sprintf(createPrompt, message))
}

// Update applies an instruction to an existing list and returns the result.
func (c *Client) Update(ctx context.Context, list domain.List, message string) (domain.List, error) {
	current, err := json.Marshal(list)
	if err != nil {
		return domain.List{}, fmt.Errorf("encode current list: %w", err)
	}

	return c.complete(ctx, fmt.Sprintf(updatePrompt, current, message))
}

// complete runs one request and parses the list out of it.
func (c *Client) complete(ctx context.Context, prompt string) (domain.List, error) {
	resp, err := c.api.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: maxTokens,
		System: []anthropic.TextBlockParam{{
			Text: systemPrompt,
			// The system prompt is byte-identical on every request, so it is
			// worth caching even at Haiku prices.
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return domain.List{}, fmt.Errorf("claude request: %w", err)
	}

	var reply strings.Builder
	for _, block := range resp.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			reply.WriteString(text.Text)
		}
	}

	list, err := parseList(reply.String())
	if err != nil {
		return domain.List{}, err
	}

	if len(list.Items) == 0 {
		return domain.List{}, domain.ErrInvalidInput
	}

	return list, nil
}

// parseList reads the list out of a reply. The prompt asks for bare JSON, but a
// stray sentence or a code fence around it is the cheapest failure mode there
// is to tolerate, so the object is located rather than assumed.
func parseList(reply string) (domain.List, error) {
	start := strings.Index(reply, "{")
	end := strings.LastIndex(reply, "}")
	if start < 0 || end < start {
		return domain.List{}, fmt.Errorf("%w: no JSON object in reply", domain.ErrInvalidInput)
	}

	var list domain.List
	err := json.Unmarshal([]byte(reply[start:end+1]), &list)
	if err != nil {
		return domain.List{}, fmt.Errorf("decode reply: %w", err)
	}

	// Empty items are the one thing a model reliably produces when it has
	// nothing to say, and they render as blank lines.
	kept := list.Items[:0]
	for _, item := range list.Items {
		item.Text = strings.TrimSpace(item.Text)
		if item.Text != "" {
			kept = append(kept, item)
		}
	}
	list.Items = kept
	list.Title = strings.TrimSpace(list.Title)

	return list, nil
}

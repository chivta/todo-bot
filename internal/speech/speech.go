// Package speech transcribes voice messages. Claude models take no audio, so
// this is the one place the bot talks to a second provider.
package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/arvlas/todo-bot/internal/domain"
)

const (
	defaultBaseURL     = "https://api.openai.com"
	transcriptionsPath = "/v1/audio/transcriptions"
	// requestTimeout bounds one transcription. Telegram voice messages are
	// short; anything slower than this is a stuck request, not a long one.
	requestTimeout = 60 * time.Second
	// audioFileName is what the upload is called. The extension is not
	// cosmetic: the API picks its decoder from it, and Telegram voice messages
	// are OGG/Opus.
	audioFileName = "voice.ogg"
	// maxErrorBody caps how much of a failed response is read into an error.
	maxErrorBody = 2000
)

// Client transcribes audio.
type Client struct {
	http    *http.Client
	apiKey  string
	model   string
	baseURL string
}

// New builds a transcription client. A non-empty baseURL points at a different
// host, which is what lets the offline suite run the bot without a network.
func New(apiKey, model, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return &Client{
		http:    &http.Client{Timeout: requestTimeout},
		apiKey:  apiKey,
		model:   model,
		baseURL: strings.TrimSuffix(baseURL, "/"),
	}
}

// Transcribe returns the words in an OGG/Opus voice message, or
// domain.ErrNoSpeech when there are none.
func (c *Client) Transcribe(ctx context.Context, audio []byte) (string, error) {
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)

	file, err := form.CreateFormFile("file", audioFileName)
	if err != nil {
		return "", fmt.Errorf("build upload: %w", err)
	}
	_, err = file.Write(audio)
	if err != nil {
		return "", fmt.Errorf("write audio: %w", err)
	}

	err = form.WriteField("model", c.model)
	if err != nil {
		return "", fmt.Errorf("write model field: %w", err)
	}

	err = form.Close()
	if err != nil {
		return "", fmt.Errorf("close upload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+transcriptionsPath, body)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("authorization", "Bearer "+c.apiKey)
	req.Header.Set("content-type", form.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("transcription request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return "", fmt.Errorf("transcription failed with %d: %s", resp.StatusCode, detail)
	}

	var decoded struct {
		Text string `json:"text"`
	}
	err = json.NewDecoder(resp.Body).Decode(&decoded)
	if err != nil {
		return "", fmt.Errorf("decode transcription: %w", err)
	}

	text := strings.TrimSpace(decoded.Text)
	if text == "" {
		return "", domain.ErrNoSpeech
	}

	return text, nil
}

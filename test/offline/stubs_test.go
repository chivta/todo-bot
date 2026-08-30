package offline

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// The two providers the bot depends on, stubbed. They are deliberately dumb:
// what is being tested is the bot's wiring around them, not the model. Each
// records what it was asked, which is how a test proves the bot sent the list
// the user replied to rather than starting a new one.

// claudeStub is a Messages API endpoint returning a canned list.
type claudeStub struct {
	http *httptest.Server

	mu      sync.Mutex
	prompts []string
	reply   func(prompt string) string
}

func newClaudeStub() *claudeStub {
	s := &claudeStub{reply: defaultReply}
	s.http = httptest.NewServer(http.HandlerFunc(s.handle))

	return s
}

func (s *claudeStub) URL() string { return s.http.URL }
func (s *claudeStub) Close()      { s.http.Close() }

// SetReply replaces what the model says, as raw response text.
func (s *claudeStub) SetReply(reply func(prompt string) string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reply = reply
}

// Prompts returns every user prompt the bot has sent, in order.
func (s *claudeStub) Prompts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.prompts...)
}

func (s *claudeStub) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	var request struct {
		Messages []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &request)

	prompt := ""
	if len(request.Messages) > 0 && len(request.Messages[0].Content) > 0 {
		prompt = request.Messages[0].Content[0].Text
	}

	s.mu.Lock()
	s.prompts = append(s.prompts, prompt)
	reply := s.reply
	s.mu.Unlock()

	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":          "msg_stub",
		"type":        "message",
		"role":        "assistant",
		"model":       "claude-haiku-4-5",
		"content":     []any{map[string]any{"type": "text", "text": reply(prompt)}},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
}

// defaultReply answers a create prompt with one list and an update prompt with
// another, so a test can tell which path the bot took from the chat alone.
func defaultReply(prompt string) string {
	if strings.Contains(prompt, "<list>") {
		return `{"title":"Errands","items":[{"text":"Buy milk","done":false},{"text":"Pick up parcel","done":false}]}`
	}

	return `{"title":"Errands","items":[{"text":"Buy milk","done":false},{"text":"Call the dentist","done":false}]}`
}

// speechStub is a transcription endpoint.
type speechStub struct {
	http *httptest.Server

	mu       sync.Mutex
	received [][]byte
	text     string
}

func newSpeechStub() *speechStub {
	s := &speechStub{text: "buy milk and call the dentist"}
	s.http = httptest.NewServer(http.HandlerFunc(s.handle))

	return s
}

func (s *speechStub) URL() string { return s.http.URL }
func (s *speechStub) Close()      { s.http.Close() }

// SetText replaces what the next voice message transcribes to.
func (s *speechStub) SetText(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.text = text
}

// Received returns the audio the bot uploaded, which is how a test proves it
// downloaded the right file rather than sending an empty body.
func (s *speechStub) Received() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([][]byte(nil), s.received...)
}

func (s *speechStub) handle(w http.ResponseWriter, r *http.Request) {
	audio := []byte(nil)
	file, _, err := r.FormFile("file")
	if err == nil {
		audio, _ = io.ReadAll(file)
		file.Close()
	}

	s.mu.Lock()
	s.received = append(s.received, audio)
	text := s.text
	s.mu.Unlock()

	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"text": text})
}

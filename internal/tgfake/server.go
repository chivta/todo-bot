// Package tgfake is a fake Telegram Bot API server for tests.
//
// The bot under test points its API base URL here and is otherwise untouched:
// a real process, a real HTTP client, a real polling loop, real routing. Only
// Telegram is replaced. That covers the wiring where bots break, whether
// commands are reachable, routed, authorised and answered, with no network, no
// credentials and no rate limits, so it belongs in CI.
//
// It models the API, not Telegram. Whether a delete is permitted, whether your
// HTML parses, how flood control behaves: those need a real bot. See
// reference/manual-checklist.md.
package tgfake

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// pollTick is how often a waiting getUpdates re-checks the queue, and how often
// the assertion helpers re-check recorded calls.
const pollTick = 10 * time.Millisecond

// Call is one Bot API request the bot made.
type Call struct {
	Method string
	Params map[string]any
}

// Text returns a string parameter, empty when absent.
func (c Call) Text(key string) string {
	value, _ := c.Params[key].(string)
	return value
}

// Int returns an integer parameter, zero when absent or unparseable. Bot
// frameworks send numbers as form strings or as JSON numbers depending on the
// encoding they chose, so both are accepted.
func (c Call) Int(key string) int64 {
	switch value := c.Params[key].(type) {
	case string:
		n, _ := strconv.ParseInt(value, 10, 64)
		return n
	case float64:
		return int64(value)
	case int64:
		return value
	}

	return 0
}

// update is one queued update plus the kind Telegram would file it under, which
// is what allowed_updates filters on.
type update struct {
	id      int64
	kind    string
	payload map[string]any
}

// Server is a fake Bot API endpoint.
type Server struct {
	http *httptest.Server

	mu         sync.Mutex
	updates    []update
	calls      []Call
	failures   map[string]failure
	nextID     int64
	webhookURL string
	// files is Telegram's file storage: what getFile resolves and the
	// /file/bot<token>/ endpoint serves. See media.go.
	files map[string][]byte
}

type failure struct {
	status      int
	description string
}

// New starts a fake server. Close it when the test finishes.
func New() *Server {
	s := &Server{failures: map[string]failure{}, files: map[string][]byte{}, nextID: 1000}
	s.http = httptest.NewServer(http.HandlerFunc(s.handle))

	return s
}

// URL is what the bot under test should use as its Bot API base URL.
func (s *Server) URL() string {
	return s.http.URL
}

// Close shuts the server down and stops webhook delivery.
func (s *Server) Close() {
	s.stopWebhook()
	s.http.Close()
}

// FailNext makes the next call to method fail, for exercising the bot's error
// handling. Provoking a failure on demand is the main thing a fake can do that
// the real API cannot.
func (s *Server) FailNext(method string, status int, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failures[method] = failure{status: status, description: description}
}

// Calls returns every request the bot has made, in order.
//
// getUpdates is not recorded, so polling cadence itself is not assertable here.
// Everything else is, including methods this server does not model.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]Call(nil), s.calls...)
}

// WaitForCall blocks until the bot calls method with a parameter containing
// want ("" matches any), and returns it.
func (s *Server) WaitForCall(method, param, want string, timeout time.Duration) (Call, error) {
	deadline := time.Now().Add(timeout)

	for {
		for _, call := range s.Calls() {
			if call.Method != method {
				continue
			}
			if want == "" || strings.Contains(call.Text(param), want) {
				return call, nil
			}
		}

		if time.Now().After(deadline) {
			return Call{}, fmt.Errorf("no %s call with %s containing %q within %s; calls so far: %s",
				method, param, want, timeout, s.summary())
		}
		time.Sleep(pollTick)
	}
}

// WaitForMessage is the common case: a message sent to the chat.
func (s *Server) WaitForMessage(want string, timeout time.Duration) (string, error) {
	call, err := s.WaitForCall("sendMessage", "text", want, timeout)
	if err != nil {
		return "", err
	}

	return call.Text("text"), nil
}

// WaitForDelete blocks until the bot deletes messageID in chatID.
//
// Asserting on the specific message id matters: a bot that deletes its own
// prompt instead of the credential the user sent passes a "was anything
// deleted?" check while leaving the secret in the chat.
func (s *Server) WaitForDelete(chatID, messageID int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		for _, call := range s.Calls() {
			if call.Method != "deleteMessage" {
				continue
			}
			if call.Int("chat_id") == chatID && call.Int("message_id") == messageID {
				return nil
			}
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("message %d in chat %d was not deleted within %s; calls so far: %s",
				messageID, chatID, timeout, s.summary())
		}
		time.Sleep(pollTick)
	}
}

// IndexOf returns the position of the first call to method whose param contains
// want ("" matches any), or -1.
//
// Ordering is a real property and a real bug class: a bot that says "send me
// the token" and then exits, or deletes a message before it has read it, has
// correct pieces in the wrong sequence. Compare two indexes to assert on it.
func (s *Server) IndexOf(method, param, want string) int {
	for i, call := range s.Calls() {
		if call.Method != method {
			continue
		}
		if want == "" || strings.Contains(call.Text(param), want) {
			return i
		}
	}

	return -1
}

// ExpectNoMessageTo reports the offending text if the bot messages chatID
// within the window, and an empty string when it stays silent.
//
// Scoping by chat is essential, not a refinement. A bot that is busy doing
// something else, reporting a startup failure to its own chat say, produces
// sendMessage calls that have nothing to do with what is being tested. An
// unscoped check reads those as "the bot answered a chat it should not have",
// which is a false report of a security hole.
//
// The text is returned rather than an error so a test can tell "the bot did
// something it should not have" apart from a harness problem.
func (s *Server) ExpectNoMessageTo(chatID int64, window time.Duration) string {
	deadline := time.Now().Add(window)

	for time.Now().Before(deadline) {
		if texts := s.MessagesTo(chatID); len(texts) > 0 {
			return texts[0]
		}
		time.Sleep(pollTick)
	}

	return ""
}

// MessagesTo returns the text of every message the bot sent to chatID.
func (s *Server) MessagesTo(chatID int64) []string {
	var texts []string
	for _, call := range s.Calls() {
		if call.Method != "sendMessage" {
			continue
		}
		if call.Int("chat_id") == chatID {
			texts = append(texts, call.Text("text"))
		}
	}

	return texts
}

func (s *Server) summary() string {
	var methods []string
	for _, call := range s.Calls() {
		methods = append(methods, call.Method)
	}
	if len(methods) == 0 {
		return "(none)"
	}

	return strings.Join(methods, ", ")
}

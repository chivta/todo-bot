package tgfake

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// longPollWait bounds how long getUpdates holds a request open. Real Telegram
// blocks for the caller's timeout; blocking here too keeps the bot's polling
// loop from spinning, and returning well before the caller's own deadline keeps
// shutdown responsive.
const longPollWait = 2 * time.Second

// botID is the id getMe reports. Tests rarely care, but a framework will reject
// an empty or inconsistent identity at startup.
const botID = 424242

// defaultExcluded are the update kinds Telegram withholds unless the bot names
// them in allowed_updates. Subscribing to chat_member is a step bots forget,
// and the symptom is silence rather than an error.
var defaultExcluded = map[string]bool{
	"chat_member":            true,
	"message_reaction":       true,
	"message_reaction_count": true,
}

// handle routes /bot<token>/<method>. The token is never checked, so any string
// works as BOT_TOKEN in the test environment.
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	// File downloads live on a different path shape: /file/bot<token>/<path>.
	if strings.HasPrefix(r.URL.Path, "/file/") {
		s.serveFile(w, r)
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "bot") {
		http.NotFound(w, r)
		return
	}
	method := parts[1]

	params := readParams(r)
	if method != "getUpdates" {
		s.record(Call{Method: method, Params: params})
	}

	fail, failing := s.takeFailure(method)
	if failing {
		writeJSON(w, fail.status, map[string]any{
			"ok": false, "error_code": fail.status, "description": fail.description,
		})
		return
	}

	switch method {
	case "getMe":
		writeJSON(w, http.StatusOK, ok(botUser()))
	case "getUpdates":
		writeJSON(w, http.StatusOK, ok(s.takeUpdates(params)))
	case "setWebhook":
		s.startWebhook(params)
		writeJSON(w, http.StatusOK, ok(true))
	case "deleteWebhook":
		s.stopWebhook()
		writeJSON(w, http.StatusOK, ok(true))
	case "sendMessage":
		writeJSON(w, http.StatusOK, ok(s.newMessage(params)))
	case "editMessageText":
		// telebot decodes the result into a Message and rejects a bare "true",
		// so an edit has to come back as the message it produced.
		writeJSON(w, http.StatusOK, ok(editedMessage(params)))
	case "getFile":
		writeJSON(w, http.StatusOK, ok(fileInfo(params)))
	case "sendMediaGroup":
		writeJSON(w, http.StatusOK, ok([]any{s.newMessage(params)}))
	default:
		// Unknown methods succeed rather than error: the point is the bot's
		// behaviour, not completeness of the API. Every call is still recorded,
		// so a test can assert on one this server does not model. Add a case
		// only when a framework rejects the bare "true" result.
		writeJSON(w, http.StatusOK, ok(true))
	}
}

// takeUpdates returns queued updates, honouring offset, limit and
// allowed_updates, and waits a little when there is nothing rather than
// busy-looping.
//
// Honouring offset rather than draining the queue is what makes a redelivery
// bug visible: a bot that crashes mid-handler and restarts should see the
// update again, because it never confirmed it.
func (s *Server) takeUpdates(params map[string]any) []map[string]any {
	deadline := time.Now().Add(longPollWait)
	offset := Call{Params: params}.Int("offset")
	allowed := allowedKinds(params)

	limit := Call{Params: params}.Int("limit")
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	for {
		s.mu.Lock()
		// An update is confirmed once getUpdates asks for a higher offset, so
		// anything below it can be dropped for good.
		if offset > 0 {
			kept := s.updates[:0]
			for _, u := range s.updates {
				if u.id >= offset {
					kept = append(kept, u)
				}
			}
			s.updates = kept
		}

		var batch []map[string]any
		for _, u := range s.updates {
			if allowed != nil && !allowed[u.kind] {
				continue
			}
			if allowed == nil && defaultExcluded[u.kind] {
				continue
			}

			batch = append(batch, map[string]any{"update_id": u.id, u.kind: u.payload})
			if int64(len(batch)) == limit {
				break
			}
		}
		s.mu.Unlock()

		if len(batch) > 0 {
			return batch
		}
		if time.Now().After(deadline) {
			return []map[string]any{}
		}
		time.Sleep(pollTick)
	}
}

// allowedKinds returns the subscribed update kinds, or nil for the default set.
func allowedKinds(params map[string]any) map[string]bool {
	raw, present := params["allowed_updates"]
	if !present {
		return nil
	}

	var kinds []string
	switch value := raw.(type) {
	case string:
		_ = json.Unmarshal([]byte(value), &kinds)
	case []any:
		for _, item := range value {
			if kind, isString := item.(string); isString {
				kinds = append(kinds, kind)
			}
		}
	}

	if len(kinds) == 0 {
		return nil
	}

	allowed := map[string]bool{}
	for _, kind := range kinds {
		allowed[kind] = true
	}

	return allowed
}

// startWebhook switches delivery from long polling to POSTing updates at the
// registered URL, so the same suite covers a webhook bot. The bot must be told
// to register a URL a test can reach, usually its own local listener.
func (s *Server) startWebhook(params map[string]any) {
	target := Call{Params: params}.Text("url")
	if target == "" {
		return
	}

	s.mu.Lock()
	running := s.webhookURL != ""
	s.webhookURL = target
	s.mu.Unlock()

	if running {
		return
	}

	go s.deliverWebhook()
}

func (s *Server) stopWebhook() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.webhookURL = ""
}

// deliverWebhook drains queued updates to the registered URL. Delivery is
// sequential and ignores the response body, which is all Telegram promises.
func (s *Server) deliverWebhook() {
	client := &http.Client{Timeout: 10 * time.Second}

	for {
		s.mu.Lock()
		target := s.webhookURL
		var next *update
		if target != "" && len(s.updates) > 0 {
			u := s.updates[0]
			s.updates = s.updates[1:]
			next = &u
		}
		s.mu.Unlock()

		if target == "" {
			return
		}
		if next == nil {
			time.Sleep(pollTick)
			continue
		}

		body, _ := json.Marshal(map[string]any{"update_id": next.id, next.kind: next.payload})
		request, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			return
		}
		request.Header.Set("content-type", "application/json")

		response, err := client.Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
	}
}

// newMessage is a plausible sent-message result, which the bot's framework
// decodes and would reject if it were empty.
func (s *Server) newMessage(params map[string]any) map[string]any {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.mu.Unlock()

	call := Call{Params: params}

	return map[string]any{
		"message_id": id,
		"from":       botUser(),
		"chat":       map[string]any{"id": call.Int("chat_id"), "type": "private"},
		"date":       time.Now().Unix(),
		"text":       call.Text("text"),
	}
}

func (s *Server) record(call Call) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, call)
}

func (s *Server) takeFailure(method string) (failure, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fail, present := s.failures[method]
	if present {
		delete(s.failures, method)
	}

	return fail, present
}

// readParams accepts both encodings a bot framework may use: form values and a
// JSON body.
func readParams(r *http.Request) map[string]any {
	params := map[string]any{}

	body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	defer r.Body.Close()

	contentType := r.Header.Get("content-type")
	switch {
	case strings.Contains(contentType, "json"):
		_ = json.Unmarshal(body, &params)
	case strings.Contains(contentType, "multipart/form-data"):
		// Used by frameworks that upload files, and by some for every call.
		// File parts are skipped; only the text fields are assertable.
		if _, directives, err := mime.ParseMediaType(contentType); err == nil {
			reader := multipart.NewReader(bytes.NewReader(body), directives["boundary"])
			form, err := reader.ReadForm(8 << 20)
			if err == nil {
				for key, list := range form.Value {
					if len(list) > 0 {
						params[key] = list[0]
					}
				}
			}
		}
	default:
		if values, err := url.ParseQuery(string(body)); err == nil {
			for key, list := range values {
				if len(list) > 0 {
					params[key] = list[0]
				}
			}
		}
	}

	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			params[key] = values[0]
		}
	}

	return params
}

func ok(result any) map[string]any {
	return map[string]any{"ok": true, "result": result}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

package tgfake

import (
	"net/http"
	"strings"
	"time"
)

// Voice messages and edits, which the reference harness does not model.
//
// A bot that accepts audio does two things a text-only bot never does: it
// resolves a file id through getFile and then downloads from a different path
// shape entirely. Both are wiring, both are invisible to a handler test, and a
// wrong base URL in either one is a bot that silently answers nothing.

// filePathPrefix is where a stored file appears to live on Telegram's servers.
const filePathPrefix = "voice/"

// PutVoice stores audio under a file id, as if a user had recorded it. The bot
// reaches it through getFile followed by the file download endpoint, exactly as
// it would in production.
func (s *Server) PutVoice(fileID string, audio []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.files[fileID] = audio
}

// SendVoice queues a voice message from chatID and returns its message id.
func (s *Server) SendVoice(chatID int64, fileID string) int64 {
	return s.sendVoice(chatID, fileID, 0)
}

// SendVoiceReply queues a voice message answering replyTo.
func (s *Server) SendVoiceReply(chatID int64, replyTo int64, fileID string) int64 {
	return s.sendVoice(chatID, fileID, replyTo)
}

// SendReply queues a text message answering replyTo, which is the whole
// mechanic of a bot that edits what it already sent.
func (s *Server) SendReply(chatID int64, replyTo int64, text string) int64 {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.mu.Unlock()

	message := s.message(chatID, id, text, nil)
	message["reply_to_message"] = s.message(chatID, replyTo, "", nil)
	s.queue("message", message)

	return id
}

func (s *Server) sendVoice(chatID int64, fileID string, replyTo int64) int64 {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.mu.Unlock()

	message := s.message(chatID, id, "", nil)
	delete(message, "text")
	message["voice"] = map[string]any{
		"file_id":        fileID,
		"file_unique_id": fileID,
		"duration":       3,
		"mime_type":      "audio/ogg",
	}
	if replyTo != 0 {
		message["reply_to_message"] = s.message(chatID, replyTo, "", nil)
	}

	s.queue("message", message)

	return id
}

// serveFile answers /file/bot<token>/<file_path> with the stored bytes.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	index := strings.Index(path, filePathPrefix)
	if index < 0 {
		http.NotFound(w, r)
		return
	}

	s.mu.Lock()
	audio, present := s.files[path[index+len(filePathPrefix):]]
	s.mu.Unlock()

	if !present {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("content-type", "audio/ogg")
	_, _ = w.Write(audio)
}

// fileInfo is what getFile returns: where the file can be downloaded from.
func fileInfo(params map[string]any) map[string]any {
	fileID := Call{Params: params}.Text("file_id")

	return map[string]any{
		"file_id":        fileID,
		"file_unique_id": fileID,
		"file_size":      1024,
		"file_path":      filePathPrefix + fileID,
	}
}

// editedMessage is the message an editMessageText call produces. The id is the
// one that was edited, which is what makes an edit distinguishable from a new
// message in the recorded calls.
func editedMessage(params map[string]any) map[string]any {
	call := Call{Params: params}

	return map[string]any{
		"message_id": call.Int("message_id"),
		"from":       botUser(),
		"chat":       map[string]any{"id": call.Int("chat_id"), "type": "private"},
		"date":       time.Now().Unix(),
		"text":       call.Text("text"),
	}
}

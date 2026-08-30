package tgfake

import (
	"strings"
	"time"
)

// The update constructors below are the test's side of the conversation: each
// queues one update as if a user had done something, and returns the id of the
// message it created so a test can assert on that exact message.
//
// The kind passed to queue is the Update field name Telegram would use. It is
// what allowed_updates filters on, so a bot that never subscribed to a kind
// will not receive it here either. That mirrors a real and quiet bug: the bot
// handles callback queries perfectly and never sees one.

// SendText queues a plain message from chatID.
func (s *Server) SendText(chatID int64, text string) int64 {
	return s.queueMessage("message", chatID, text, nil)
}

// SendCommand queues a command from chatID, carrying the entity Telegram
// attaches. A message whose text starts with "/" is not a command to a bot
// framework without it: the handler never fires and the test times out with no
// clue why.
func (s *Server) SendCommand(chatID int64, text string) int64 {
	command := text
	if space := strings.IndexByte(text, ' '); space > 0 {
		command = text[:space]
	}

	var entities []map[string]any
	if strings.HasPrefix(text, "/") {
		entities = []map[string]any{{
			"type":   "bot_command",
			"offset": 0,
			"length": len(command),
		}}
	}

	return s.queueMessage("message", chatID, text, entities)
}

// EditText queues an edit of messageID, which arrives as edited_message rather
// than message. A bot that routes commands only on message silently ignores a
// user fixing a typo in one.
func (s *Server) EditText(chatID, messageID int64, text string) {
	message := s.message(chatID, messageID, text, nil)
	message["edit_date"] = time.Now().Unix()

	s.queue("edited_message", message)
}

// ClickButton queues a callback query, as if the user tapped an inline keyboard
// button carrying data on messageID.
//
// Two things are worth asserting after one: that the bot calls
// answerCallbackQuery, without which the client shows a spinner until it times
// out, and that it checks who tapped. Button payloads are guessable and arrive
// from anyone who can see the message.
func (s *Server) ClickButton(chatID, messageID int64, data string) {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.mu.Unlock()

	s.queue("callback_query", map[string]any{
		"id":            itoa(id),
		"from":          user(chatID),
		"chat_instance": itoa(chatID),
		"data":          data,
		"message":       s.message(chatID, messageID, "", nil),
	})
}

// ChatMemberChange queues a my_chat_member update, the bot's own status in a
// chat changing: "member" when it is added to a group, "kicked" when a user
// blocks it, "administrator" when it is promoted.
//
// Blocking is the one to test. A bot that treats the resulting 403 on its next
// send as fatal takes itself down because one user tapped Block.
func (s *Server) ChatMemberChange(chatID int64, oldStatus, newStatus string) {
	s.queue("my_chat_member", map[string]any{
		"chat":            map[string]any{"id": chatID, "type": "private"},
		"from":            user(chatID),
		"date":            time.Now().Unix(),
		"old_chat_member": map[string]any{"status": oldStatus, "user": botUser()},
		"new_chat_member": map[string]any{"status": newStatus, "user": botUser()},
	})
}

// InlineQuery queues an inline query, the "@yourbot something" flow. It arrives
// from any user in any chat, so the bot's chat gating does not apply to it.
func (s *Server) InlineQuery(userID int64, query string) {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.mu.Unlock()

	s.queue("inline_query", map[string]any{
		"id":     itoa(id),
		"from":   user(userID),
		"query":  query,
		"offset": "",
	})
}

// queueMessage queues a message-shaped update and returns its message id.
func (s *Server) queueMessage(kind string, chatID int64, text string, entities []map[string]any) int64 {
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.mu.Unlock()

	s.queue(kind, s.message(chatID, id, text, entities))

	return id
}

// queue appends an update under the given kind, which allowed_updates filters
// on.
func (s *Server) queue(kind string, payload map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	s.updates = append(s.updates, update{id: s.nextID, kind: kind, payload: payload})
}

func (s *Server) message(chatID, messageID int64, text string, entities []map[string]any) map[string]any {
	message := map[string]any{
		"message_id": messageID,
		"from":       user(chatID),
		"chat":       map[string]any{"id": chatID, "type": "private"},
		"date":       time.Now().Unix(),
		"text":       text,
	}
	if entities != nil {
		message["entities"] = entities
	}

	return message
}

func user(id int64) map[string]any {
	return map[string]any{
		"id": id, "is_bot": false, "first_name": "Tester", "username": "tester",
	}
}

func botUser() map[string]any {
	return map[string]any{
		"id": botID, "is_bot": true, "first_name": "Fake", "username": "fake_test_bot",
	}
}

package offline

import (
	"strings"
	"testing"
	"time"
)

const chatID = 555

// A command is only a command with its bot_command entity attached, which is
// what SendCommand adds and SendText does not.
func TestStartIsRoutedAndAnswered(t *testing.T) {
	h := start(t, nil)

	h.telegram.SendCommand(chatID, "/start")

	text, err := h.telegram.WaitForMessage("voice message", wait)
	if err != nil {
		t.Fatalf("%v\n--- bot log ---\n%s", err, h.bot.Logs())
	}
	if !strings.Contains(text, "reply") {
		t.Errorf("/start never mentions replying, which is the whole mechanic: %q", text)
	}
}

// The placeholder has to come back before the list, or the handler was blocking
// on the model while every other user's updates waited behind it.
func TestTextMessageBecomesAFormattedList(t *testing.T) {
	h := start(t, nil)

	h.telegram.SendText(chatID, "tomorrow buy milk and call the dentist")

	list := h.waitForEdit("Buy milk")
	if !strings.Contains(list, "<b>Errands</b>") {
		t.Errorf("list has no title: %q", list)
	}
	if !strings.Contains(list, "1. Buy milk") || !strings.Contains(list, "2. Call the dentist") {
		t.Errorf("list is not numbered: %q", list)
	}

	placeholder := h.telegram.IndexOf("sendMessage", "text", "Queued")
	if placeholder < 0 {
		t.Fatalf("no placeholder was sent; calls: %v", h.telegram.Calls())
	}
	if edit := h.telegram.IndexOf("editMessageText", "text", "Buy milk"); edit < placeholder {
		t.Errorf("the list arrived before the placeholder, so the handler blocked on the model")
	}
}

// The mechanic the whole bot rests on: a reply carries the list it answers into
// the model, and the answer replaces that list.
func TestReplyToAListEditsThatList(t *testing.T) {
	h := start(t, nil)

	h.telegram.SendText(chatID, "buy milk and call the dentist")
	h.waitForEdit("Call the dentist")

	call, err := h.telegram.WaitForCall("editMessageText", "text", "Call the dentist", wait)
	if err != nil {
		t.Fatalf("%v", err)
	}
	listMessageID := call.Int("message_id")

	h.telegram.SendReply(chatID, listMessageID, "drop the dentist, add pick up parcel")

	updated := h.waitForEdit("Pick up parcel")
	if strings.Contains(updated, "Call the dentist") {
		t.Errorf("the reply did not replace the list: %q", updated)
	}

	prompts := h.claude.Prompts()
	if len(prompts) < 2 {
		t.Fatalf("the model was asked %d times, want 2\n--- bot log ---\n%s", len(prompts), h.bot.Logs())
	}
	if !strings.Contains(prompts[1], "Call the dentist") {
		t.Errorf("the reply prompt did not carry the current list: %q", prompts[1])
	}
	if !strings.Contains(prompts[1], "drop the dentist") {
		t.Errorf("the reply prompt did not carry the instruction: %q", prompts[1])
	}
}

// A message that is not a reply starts fresh, no matter what came before it.
func TestMessageWithoutAReplyStartsANewList(t *testing.T) {
	h := start(t, nil)

	h.telegram.SendText(chatID, "buy milk and call the dentist")
	h.waitForEdit("Call the dentist")

	h.telegram.SendText(chatID, "water the plants")
	h.waitForEdit("Buy milk")

	for _, prompt := range h.claude.Prompts() {
		if strings.Contains(prompt, "<list>") {
			t.Errorf("a plain message was treated as an edit: %q", prompt)
		}
	}
}

// A reply to something the bot has no list for — an old message, its own error
// notice — behaves like a plain message rather than failing.
func TestReplyToAnUnknownMessageStartsANewList(t *testing.T) {
	h := start(t, nil)

	h.telegram.SendReply(chatID, 9999, "buy milk")

	h.waitForEdit("Buy milk")
	for _, prompt := range h.claude.Prompts() {
		if strings.Contains(prompt, "<list>") {
			t.Errorf("the bot tried to edit a list it does not have: %q", prompt)
		}
	}
}

// Voice is two pieces of wiring a handler test cannot see: resolving a file id
// and downloading from a different path shape entirely.
func TestVoiceMessageIsDownloadedTranscribedAndListed(t *testing.T) {
	h := start(t, nil)

	audio := []byte("OggS-fake-opus-payload")
	h.telegram.PutVoice("voice-1", audio)
	h.telegram.SendVoice(chatID, "voice-1")

	list := h.waitForEdit("Buy milk")
	if !strings.Contains(list, "<b>Errands</b>") {
		t.Errorf("voice message did not produce a list: %q", list)
	}

	received := h.speech.Received()
	if len(received) != 1 || string(received[0]) != string(audio) {
		t.Fatalf("transcription received %q, want the downloaded audio", received)
	}
	if prompts := h.claude.Prompts(); len(prompts) == 0 || !strings.Contains(prompts[0], "buy milk and call the dentist") {
		t.Errorf("the transcript never reached the model: %v", prompts)
	}
}

// A voice reply is the same edit path, spoken.
func TestVoiceReplyEditsTheList(t *testing.T) {
	h := start(t, nil)

	h.telegram.SendText(chatID, "buy milk and call the dentist")
	call, err := h.telegram.WaitForCall("editMessageText", "text", "Call the dentist", wait)
	if err != nil {
		t.Fatalf("%v", err)
	}

	h.speech.SetText("drop the dentist and add pick up parcel")
	h.telegram.PutVoice("voice-2", []byte("OggS-fake-opus-payload"))
	h.telegram.SendVoiceReply(chatID, call.Int("message_id"), "voice-2")

	h.waitForEdit("Pick up parcel")
	if prompts := h.claude.Prompts(); !strings.Contains(prompts[len(prompts)-1], "<list>") {
		t.Errorf("a voice reply did not take the edit path: %q", prompts[len(prompts)-1])
	}
}

// The bot's username is public, so an allow list is the only thing between a
// stranger and the API budget. The assertion is scoped to the stranger's chat:
// an unscoped one reads the owner's own traffic as a leak.
func TestUnauthorizedUserIsTurnedAway(t *testing.T) {
	const owner = 111
	const stranger = 222

	h := start(t, map[string]string{"ALLOWED_USERS": "111"})

	h.telegram.SendText(stranger, "buy milk")
	h.telegram.SendText(owner, "buy milk and call the dentist")

	h.waitForEdit("Buy milk")

	for _, text := range h.telegram.MessagesTo(stranger) {
		if !strings.Contains(text, "private") {
			t.Errorf("a stranger got %q, want only the refusal", text)
		}
	}
	if len(h.claude.Prompts()) != 1 {
		t.Errorf("the model was asked %d times, want 1: a stranger spent the budget", len(h.claude.Prompts()))
	}
}

// An empty message is answered rather than queued: no placeholder, no model
// call, no cost.
func TestEmptyMessageIsRejectedWithoutCallingTheModel(t *testing.T) {
	h := start(t, nil)

	h.telegram.SendText(chatID, "   ")

	_, err := h.telegram.WaitForMessage("Send me something", wait)
	if err != nil {
		t.Fatalf("%v\n--- bot log ---\n%s", err, h.bot.Logs())
	}
	if n := len(h.claude.Prompts()); n != 0 {
		t.Errorf("the model was asked %d times for an empty message", n)
	}
}

// Provoking a failure on demand is the one thing a fake can do that the real
// API cannot. A bot that dies on its first failed send is a real and common bug.
func TestBotSurvivesAFailedSend(t *testing.T) {
	h := start(t, nil)

	h.telegram.FailNext("sendMessage", 400, "Bad Request: message is too long")
	h.telegram.SendText(chatID, "buy milk")

	// Give the failed request time to land before the one that must succeed.
	time.Sleep(500 * time.Millisecond)
	h.telegram.SendText(chatID, "buy milk and call the dentist")

	h.waitForEdit("Buy milk")
}

// The model is a network call away and answers with free text; neither is
// trustworthy. Nonsense back has to reach the user as a sentence, not silence.
func TestUnparseableModelReplyIsReportedToTheUser(t *testing.T) {
	h := start(t, nil)
	h.claude.SetReply(func(string) string { return "I'm afraid I can't do that" })

	h.telegram.SendText(chatID, "buy milk")

	h.waitForEdit("couldn't find any tasks")
}

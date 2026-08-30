package bot

import (
	"errors"
	"strings"

	"github.com/arvlas/todo-bot/internal/domain"
)

// Every user-facing string lives here. Nothing in the other layers formats text
// for a human: they pass states and domain errors, and this file names them.
// Keeping it in one file is what makes the bot translatable later, and what
// stops HTML-escaping bugs from spreading through business logic.
const (
	textStart = "👋 Tell me what you need to do — type it or send a voice message — " +
		"and I'll turn it into a list.\n\n" +
		"<b>To change a list</b>, reply to it:\n" +
		"<code>add buy milk</code>\n" +
		"<code>drop the second one</code>\n" +
		"<code>mark the dentist done</code>\n\n" +
		"A message that isn't a reply always starts a fresh list."
	textEmptyQuery   = "Send me something to put on a list."
	textQueued       = "⏳ Queued…"
	textTranscribing = "🎧 Listening…"
	textThinking     = "📝 Writing your list…"
)

// errorText maps every domain error onto the one sentence the user gets. An
// error missing from this map is a bug on our side, not theirs — which is why
// the fallback apologises rather than leaking the error.
var errorText = map[error]string{
	domain.ErrInvalidInput: "🤔 I couldn't find any tasks in that. Try naming them one by one.",
	domain.ErrNoSpeech:     "🔇 I couldn't hear anything in that voice message.",
	domain.ErrRateLimited:  "🐢 You've hit your limit. Try again in a little while.",
	domain.ErrQueueFull:    "🚦 I'm at capacity right now. Try again in a minute.",
	domain.ErrUnauthorized: "🔒 This bot is private.",
	domain.ErrTimeout:      "⏱ That took too long and I gave up. Try again.",
	domain.ErrNotFound:     "🤷 I don't have that list any more. Send your tasks again.",
}

const textUnexpected = "💥 Something went wrong on my side. Try again."

// describe names an error for the user, falling back to the generic apology.
func describe(err error) string {
	for sentinel, text := range errorText {
		if errors.Is(err, sentinel) {
			return text
		}
	}

	return textUnexpected
}

// stateText names a job state for the user.
func stateText(state domain.State) string {
	switch state {
	case domain.StateTranscribing:
		return textTranscribing
	case domain.StateThinking:
		return textThinking
	default:
		return textQueued
	}
}

// escape makes a value safe for tele.ModeHTML. Every interpolated value goes
// through it: one raw "<" in a task makes Telegram reject the whole message.
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

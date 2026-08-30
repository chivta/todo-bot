package domain

import "time"

// Job is one request accepted from a user: either a fresh dump of tasks or a
// reply asking for changes to a list the bot already produced.
//
// StatusMessageID is the placeholder the handler already sent. The worker edits
// it into the finished list, which is what lets the handler return immediately
// instead of blocking telebot's update loop.
type Job struct {
	UserID          int64
	ChatID          int64
	StatusMessageID int
	// Text is the message body. Empty when the request arrived as a voice
	// message, in which case VoiceFileID is set and has to be transcribed first.
	Text        string
	VoiceFileID string
	// ReplyToMessageID is the message the user replied to, or 0. A reply to a
	// message the bot still has a list for means "change that list"; anything
	// else starts a new one.
	ReplyToMessageID int
	RequestedAt      time.Time
}

// State is how far along a job is. The delivery layer maps these onto text; the
// worker never formats a sentence itself.
type State int

const (
	StateQueued State = iota
	StateTranscribing
	StateThinking
)

// Item is one task. Done items stay in the list struck through, because a user
// who says "mark the second one done" wants to see it, not lose it.
type Item struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// List is a rendered todo list. It is stored per bot message, so a reply to
// that message can be resolved back to the list it shows.
type List struct {
	Title string `json:"title"`
	Items []Item `json:"items"`
}

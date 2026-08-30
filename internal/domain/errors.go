package domain

import "errors"

// Sentinel errors describing every condition the bot knows how to explain to a
// user. Anything else surfaces as a generic failure, which is deliberate: an
// unmapped error is a bug on our side, not something to leak verbatim into a
// chat. Keep the values machine-readable — the human sentence lives in
// internal/bot/text.go.
var (
	// ErrNotFound is returned when a replied-to message has no stored list.
	ErrNotFound = errors.New("not_found")
	// ErrInvalidInput is returned when a message yields no tasks at all.
	ErrInvalidInput = errors.New("invalid_input")
	// ErrNoSpeech is returned when a voice message transcribes to nothing.
	ErrNoSpeech = errors.New("no_speech")
	// ErrRateLimited is returned when a user has spent their request budget.
	ErrRateLimited = errors.New("rate_limited")
	// ErrQueueFull is returned when the queue cannot accept more work.
	ErrQueueFull = errors.New("queue_full")
	// ErrUnauthorized is returned when a user is not on the allow list.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrTimeout is returned when a job outran its budget.
	ErrTimeout = errors.New("timeout")
)

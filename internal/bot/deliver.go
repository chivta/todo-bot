package bot

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/rs/zerolog/log"
	tele "gopkg.in/telebot.v4"

	"github.com/arvlas/todo-bot/internal/domain"
)

// maxVoiceBytes caps a download. Telegram's own getFile ceiling is 20 MB, and a
// voice message anywhere near it is not something worth transcribing.
const maxVoiceBytes = 20 << 20

// Progress reports how far along a job is by rewriting its status message.
func (b *Bot) Progress(ctx context.Context, job domain.Job, state domain.State) error {
	return b.editStatus(ctx, job, stateText(state))
}

// Deliver shows the finished list and returns the ID of the message showing it.
// That ID is what a user replies to, so the caller stores the list under it.
//
// The placeholder is edited in place rather than replaced: the list appears
// where the user is already looking, and nothing extra is left in the chat.
func (b *Bot) Deliver(ctx context.Context, job domain.Job, list domain.List) (int, error) {
	text := render(list)

	err := b.sends.wait(ctx)
	if err != nil {
		return 0, err
	}

	_, err = b.bot.Edit(statusMessage(job), text, tele.ModeHTML, tele.NoPreview)
	if err == nil {
		return job.StatusMessageID, nil
	}

	// The placeholder is gone or uneditable — deleted by the user, too old,
	// whatever. The list still has to arrive, so send it as a new message.
	log.Debug().Err(err).Int64("chat_id", job.ChatID).Msg("failed to edit list into placeholder")

	err = b.sends.wait(ctx)
	if err != nil {
		return 0, err
	}

	msg, err := b.bot.Send(&tele.Chat{ID: job.ChatID}, text, tele.ModeHTML, tele.NoPreview)
	if err != nil {
		return 0, fmt.Errorf("send list: %w", err)
	}

	return msg.ID, nil
}

// Download fetches a Telegram file by ID.
func (b *Bot) Download(ctx context.Context, fileID string) ([]byte, error) {
	err := b.sends.wait(ctx)
	if err != nil {
		return nil, err
	}

	reader, err := b.bot.File(&tele.File{FileID: fileID})
	if err != nil {
		return nil, fmt.Errorf("open telegram file: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(io.LimitReader(reader, maxVoiceBytes))
	if err != nil {
		return nil, fmt.Errorf("read telegram file: %w", err)
	}

	return data, nil
}

// Fail replaces the status message with the reason the request went nowhere.
func (b *Bot) Fail(ctx context.Context, job domain.Job, cause error) {
	err := b.editStatus(ctx, job, describe(cause))
	if err != nil {
		log.Error().Err(err).Int64("chat_id", job.ChatID).Msg("failed to report job failure")
	}
}

// editStatus rewrites the placeholder message the handler left behind. A failed
// edit is not worth failing a job over — the list still arrives, as its own
// message.
func (b *Bot) editStatus(ctx context.Context, job domain.Job, text string) error {
	err := b.sends.wait(ctx)
	if err != nil {
		return err
	}

	_, err = b.bot.Edit(statusMessage(job), text, tele.ModeHTML, tele.NoPreview)
	if err != nil {
		log.Debug().Err(err).Int64("chat_id", job.ChatID).Msg("failed to edit status message")
	}

	return nil
}

// statusMessage addresses the placeholder without having to hold the original
// *tele.Message, which the worker never sees.
func statusMessage(job domain.Job) tele.StoredMessage {
	return tele.StoredMessage{
		MessageID: strconv.Itoa(job.StatusMessageID),
		ChatID:    job.ChatID,
	}
}

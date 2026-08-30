// Package bot is the Telegram delivery layer: it accepts messages, hands them
// to the pipeline and renders whatever comes back. It holds no business logic.
package bot

import (
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	tele "gopkg.in/telebot.v4"

	"github.com/arvlas/todo-bot/internal/domain"
	"github.com/arvlas/todo-bot/internal/metrics"
)

const (
	// pollerTimeout is how long a long-poll request waits for an update.
	pollerTimeout = 10 * time.Second
	// sendRate and sendBurst keep every outbound call, across all chats and
	// workers, inside Telegram's global 30-per-second ceiling.
	sendRate  = 25
	sendBurst = 5
)

// submitter is the work pipeline seen from the delivery layer. Declaring it
// here, in the consumer, is what keeps the dependency pointing one way.
type submitter interface {
	Submit(job domain.Job) error
}

// Bot is the Telegram delivery layer.
type Bot struct {
	bot     *tele.Bot
	jobs    submitter
	allowed map[int64]bool
	sends   *sendLimiter
}

// New builds the bot. An empty allowedUsers means anyone may use it. A non-empty
// apiURL points at a different Bot API server, which is what lets the whole bot
// run against a fake one in tests.
func New(token, apiURL string, allowedUsers []int64) (*Bot, error) {
	settings := tele.Settings{
		Token:  token,
		Poller: &tele.LongPoller{Timeout: pollerTimeout},
		OnError: func(err error, c tele.Context) {
			log.Error().Err(err).Msg("telegram handler failed")
		},
	}
	if apiURL != "" {
		settings.URL = apiURL
	}

	inner, err := tele.NewBot(settings)
	if err != nil {
		return nil, err
	}

	return &Bot{
		bot:     inner,
		allowed: allowList(allowedUsers),
		sends:   newSendLimiter(sendRate, sendBurst),
	}, nil
}

// Run registers the handlers and serves updates until ctx is cancelled.
//
// The submitter arrives here rather than in New because the bot and the
// pipeline reference each other; handing it over at Run means neither has to be
// constructed half-finished.
func (b *Bot) Run(ctx context.Context, jobs submitter) error {
	b.jobs = jobs

	b.bot.Handle("/start", b.handleStart)
	b.bot.Handle("/help", b.handleStart)
	b.bot.Handle(tele.OnText, b.handleText)
	b.bot.Handle(tele.OnVoice, b.handleVoice)

	err := b.bot.SetCommands([]tele.Command{
		{Text: "start", Description: "how this bot works"},
		{Text: "help", Description: "how this bot works"},
	})
	if err != nil {
		// Not fatal: the bot works fine, the command menu is just not published.
		log.Warn().Err(err).Msg("failed to publish bot commands")
	}

	// telebot's Start blocks and has no context-aware variant, so cancellation
	// is bridged onto Stop.
	go func() {
		<-ctx.Done()
		b.bot.Stop()
	}()

	log.Info().Str("username", b.bot.Me.Username).Msg("telegram bot listening")
	b.bot.Start()
	log.Info().Msg("telegram bot stopped")

	return nil
}

func (b *Bot) handleStart(c tele.Context) error {
	if !b.authorized(c) {
		return c.Send(describe(domain.ErrUnauthorized))
	}

	return c.Send(textStart, tele.ModeHTML, tele.NoPreview)
}

// handleText queues a typed message.
func (b *Bot) handleText(c tele.Context) error {
	text := strings.TrimSpace(c.Text())
	if text == "" {
		return c.Send(textEmptyQuery)
	}

	return b.submit(c, domain.Job{Text: text})
}

// handleVoice queues a voice message. The audio is not downloaded here: fetching
// and transcribing it is exactly the kind of slow work a handler must not do.
func (b *Bot) handleVoice(c tele.Context) error {
	voice := c.Message().Voice
	if voice == nil {
		return c.Send(textEmptyQuery)
	}

	return b.submit(c, domain.Job{VoiceFileID: voice.FileID})
}

// submit answers immediately with a placeholder the workers then edit, so the
// handler never waits on slow work — a blocked handler stalls the update loop
// for every other user too.
func (b *Bot) submit(c tele.Context, job domain.Job) error {
	if !b.authorized(c) {
		return c.Send(describe(domain.ErrUnauthorized))
	}

	status, err := c.Bot().Send(c.Chat(), textQueued)
	if err != nil {
		return err
	}

	job.UserID = c.Sender().ID
	job.ChatID = c.Chat().ID
	job.StatusMessageID = status.ID
	job.ReplyToMessageID = repliedTo(c.Message())
	job.RequestedAt = time.Now()

	err = b.jobs.Submit(job)
	if err != nil {
		metrics.IncRejected()
		log.Info().Err(err).Int64("user_id", job.UserID).Msg("request rejected")
		_, editErr := c.Bot().Edit(status, describe(err))

		return editErr
	}

	metrics.IncRequest()
	log.Info().
		Int64("user_id", job.UserID).
		Int("reply_to", job.ReplyToMessageID).
		Bool("voice", job.VoiceFileID != "").
		Msg("request queued")

	return nil
}

// repliedTo returns the message this one answers, or 0. Whether that message is
// a list the bot still knows about is the pipeline's problem, not ours.
func repliedTo(msg *tele.Message) int {
	if msg == nil || msg.ReplyTo == nil {
		return 0
	}

	return msg.ReplyTo.ID
}

// authorized reports whether the sender of an update may use the bot.
func (b *Bot) authorized(c tele.Context) bool {
	sender := c.Sender()
	if sender == nil {
		// An update with no identifiable sender is only let through when the
		// bot is public anyway.
		return len(b.allowed) == 0
	}

	return b.allows(sender.ID)
}

// allows reports whether a user ID is on the allow list. An empty list means no
// list was configured, which makes the bot public — stated explicitly here
// because "no users configured" silently meaning "everyone" is otherwise an
// unpleasant surprise.
func (b *Bot) allows(id int64) bool {
	if len(b.allowed) == 0 {
		return true
	}

	return b.allowed[id]
}

// allowList indexes the configured user IDs for lookup.
func allowList(ids []int64) map[int64]bool {
	allowed := make(map[int64]bool, len(ids))
	for _, id := range ids {
		allowed[id] = true
	}

	return allowed
}

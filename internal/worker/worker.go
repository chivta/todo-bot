package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/arvlas/todo-bot/internal/domain"
	"github.com/arvlas/todo-bot/internal/metrics"
)

// telegram is the delivery layer as the pipeline sees it. The pipeline never
// formats a sentence and never imports telebot: it reports outcomes as domain
// values and sentinel errors, and the delivery layer decides what a human reads.
type telegram interface {
	Progress(ctx context.Context, job domain.Job, state domain.State) error
	Deliver(ctx context.Context, job domain.Job, list domain.List) (int, error)
	Fail(ctx context.Context, job domain.Job, cause error)
	Download(ctx context.Context, fileID string) ([]byte, error)
}

// repository is the list store as the pipeline sees it.
type repository interface {
	Get(ctx context.Context, chatID int64, messageID int) (domain.List, error)
	Save(ctx context.Context, chatID int64, messageID int, list domain.List) error
}

// lister is the model that turns words into lists.
type lister interface {
	Create(ctx context.Context, message string) (domain.List, error)
	Update(ctx context.Context, list domain.List, message string) (domain.List, error)
}

// transcriber turns a voice message into words.
type transcriber interface {
	Transcribe(ctx context.Context, audio []byte) (string, error)
}

// Limits are the knobs that keep one busy user from starving everyone else.
type Limits struct {
	Workers         int
	QueueSize       int
	UserRatePerHour int
	UserBurst       int
	JobTimeout      time.Duration
}

// Pipeline accepts jobs and runs them on a bounded worker pool.
type Pipeline struct {
	repo    repository
	lists   lister
	speech  transcriber
	out     telegram
	limits  Limits
	jobs    chan domain.Job
	buckets *userBuckets
}

// New builds the pipeline. Nothing starts until Run is called.
func New(repo repository, lists lister, speech transcriber, out telegram, limits Limits) *Pipeline {
	return &Pipeline{
		repo:    repo,
		lists:   lists,
		speech:  speech,
		out:     out,
		limits:  limits,
		jobs:    make(chan domain.Job, limits.QueueSize),
		buckets: newUserBuckets(limits.UserRatePerHour, limits.UserBurst),
	}
}

// Submit queues a job, or reports why it cannot be. It never blocks: the caller
// is a Telegram handler, and a blocked handler stalls the whole update loop.
func (p *Pipeline) Submit(job domain.Job) error {
	if !p.buckets.allow(job.UserID) {
		return domain.ErrRateLimited
	}

	select {
	case p.jobs <- job:
		metrics.SetQueueDepth(len(p.jobs))
		return nil
	default:
		return domain.ErrQueueFull
	}
}

// Run starts the worker pool and serves jobs until ctx is cancelled.
func (p *Pipeline) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	for range p.limits.Workers {
		wg.Go(func() { p.worker(ctx) })
	}

	log.Info().Int("workers", p.limits.Workers).Msg("pipeline started")
	wg.Wait()
	log.Info().Msg("pipeline stopped")

	return nil
}

func (p *Pipeline) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-p.jobs:
			metrics.SetQueueDepth(len(p.jobs))
			p.handle(ctx, job)
		}
	}
}

// handle runs one job to completion. Errors are reported to the user here and
// logged once — this is the layer that understands them, so nothing above or
// below logs them again.
func (p *Pipeline) handle(ctx context.Context, job domain.Job) {
	jobCtx, cancel := context.WithTimeout(ctx, p.limits.JobTimeout)
	defer cancel()

	err := p.run(jobCtx, job)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			err = domain.ErrTimeout
		}

		metrics.IncJobFailed()
		log.Error().Err(err).Int64("user_id", job.UserID).Msg("job failed")
		p.out.Fail(ctx, job, err)

		return
	}

	metrics.IncJob()
}

func (p *Pipeline) run(ctx context.Context, job domain.Job) error {
	message, err := p.words(ctx, job)
	if err != nil {
		return err
	}

	err = p.out.Progress(ctx, job, domain.StateThinking)
	if err != nil {
		return err
	}

	list, err := p.compose(ctx, job, message)
	if err != nil {
		return err
	}

	// The message the list lands on is the one users reply to, so it is the key
	// the list is stored under. Saving before the send would key it to the
	// placeholder, which the delivery layer is free to replace.
	messageID, err := p.out.Deliver(ctx, job, list)
	if err != nil {
		return err
	}

	err = p.repo.Save(ctx, job.ChatID, messageID, list)
	if err != nil {
		// The user already has their list; failing the job now would be a lie.
		// Losing the row only means a reply to it starts a fresh list.
		log.Warn().Err(err).Int64("chat_id", job.ChatID).Msg("failed to store list")
	}

	return nil
}

// words returns what the user actually said, transcribing first when the
// request arrived as a voice message.
func (p *Pipeline) words(ctx context.Context, job domain.Job) (string, error) {
	if job.VoiceFileID == "" {
		return job.Text, nil
	}

	err := p.out.Progress(ctx, job, domain.StateTranscribing)
	if err != nil {
		return "", err
	}

	audio, err := p.out.Download(ctx, job.VoiceFileID)
	if err != nil {
		return "", fmt.Errorf("download voice message: %w", err)
	}

	metrics.IncTranscription()

	text, err := p.speech.Transcribe(ctx, audio)
	if err != nil {
		return "", err
	}

	log.Debug().Int64("user_id", job.UserID).Str("transcript", text).Msg("voice message transcribed")

	return text, nil
}

// compose builds the list the job asks for: an edit of the list the user
// replied to, or a brand new one.
//
// A reply to anything the bot has no list for — an old message pruned from the
// store, someone else's message, the bot's own error notice — starts a new
// list rather than failing. That is the same thing the user would get by not
// replying at all, which is a far better answer than an error.
func (p *Pipeline) compose(ctx context.Context, job domain.Job, message string) (domain.List, error) {
	if strings.TrimSpace(message) == "" {
		return domain.List{}, domain.ErrInvalidInput
	}

	if job.ReplyToMessageID != 0 {
		current, err := p.repo.Get(ctx, job.ChatID, job.ReplyToMessageID)
		switch {
		case err == nil:
			metrics.IncListUpdated()
			return p.lists.Update(ctx, current, message)
		case !errors.Is(err, domain.ErrNotFound):
			return domain.List{}, fmt.Errorf("look up replied-to list: %w", err)
		}
	}

	metrics.IncListCreated()

	return p.lists.Create(ctx, message)
}

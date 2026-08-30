package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/arvlas/todo-bot/internal/bot"
	"github.com/arvlas/todo-bot/internal/config"
	"github.com/arvlas/todo-bot/internal/health"
	"github.com/arvlas/todo-bot/internal/llm"
	"github.com/arvlas/todo-bot/internal/logging"
	"github.com/arvlas/todo-bot/internal/metrics"
	"github.com/arvlas/todo-bot/internal/speech"
	"github.com/arvlas/todo-bot/internal/storage"
	"github.com/arvlas/todo-bot/internal/worker"
)

// components is how many long-running goroutines run collects errors from.
const components = 3

func main() {
	cfg, err := config.Load()
	if err != nil {
		// The logger is not configured yet, and a config failure is exactly the
		// kind of thing that must be visible even when logging is broken.
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logging.Init(cfg.LogLevel)

	err = run(cfg)
	if err != nil {
		log.Error().Err(err).Msg("todobot exited with an error")
		os.Exit(1)
	}
}

func run(cfg config.Config) error {
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// A cancellable child of the signal context, so a failure during startup can
	// bring down whatever is already running.
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, components)

	// The probe server goes up first and stays up for the whole run. Anything
	// slow during startup — migrations, authenticating — can outlast the
	// liveness probe's budget, and a container with nothing listening is killed
	// mid-startup and restarted into the same wait.
	probes := health.New(cfg.HTTPAddr, metrics.Handler())
	wg.Go(func() { errs <- probes.Run(ctx) })

	db, err := storage.Open(ctx, cfg.DBPath)
	if err != nil {
		return shutdown(cancel, &wg, errs, err)
	}
	defer db.Close()

	telegram, err := bot.New(cfg.BotToken, cfg.TelegramAPIURL, cfg.AllowedUsers)
	if err != nil {
		return shutdown(cancel, &wg, errs, fmt.Errorf("create telegram bot: %w", err))
	}

	if len(cfg.AllowedUsers) == 0 {
		log.Warn().Msg("ALLOWED_USERS is empty: anyone who finds this bot can spend its API budget")
	}

	// The bot and the pipeline are two halves of one loop: the bot submits jobs
	// and the pipeline delivers through the bot. The submitter is handed over at
	// Run rather than at construction so neither has to be built half-finished.
	pipeline := worker.New(
		storage.NewListRepo(db),
		llm.New(cfg.ClaudeAPIKey, cfg.ClaudeModel, cfg.ClaudeAPIURL),
		speech.New(cfg.OpenAIAPIKey, cfg.TranscribeModel, cfg.OpenAIAPIURL),
		telegram,
		worker.Limits{
			Workers:         cfg.Workers,
			QueueSize:       cfg.QueueSize,
			UserRatePerHour: cfg.UserRatePerHour,
			UserBurst:       cfg.UserBurst,
			JobTimeout:      cfg.JobTimeout,
		},
	)

	wg.Go(func() { errs <- pipeline.Run(ctx) })
	wg.Go(func() { errs <- telegram.Run(ctx, pipeline) })

	return shutdown(nil, &wg, errs, nil)
}

// shutdown waits for every started component to return and folds their errors
// together with cause. Passing a non-nil cancel stops them first, which is what
// a startup failure needs; a nil cancel means the components are already winding
// down on their own.
func shutdown(cancel context.CancelFunc, wg *sync.WaitGroup, errs chan error, cause error) error {
	if cancel != nil {
		cancel()
	}

	wg.Wait()
	close(errs)

	joined := cause
	for err := range errs {
		joined = errors.Join(joined, err)
	}

	log.Info().Msg("shutdown complete")

	return joined
}

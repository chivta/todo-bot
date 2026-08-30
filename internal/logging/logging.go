package logging

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/arvlas/todo-bot/internal/metrics"
)

// errorCounterHook ties the logger to the metrics registry: every error-level
// event bumps the error counter, so metrics cannot drift from the logs. Doing
// it here rather than at each error site means there is no way to log an error
// and forget to count it.
type errorCounterHook struct{}

func (errorCounterHook) Run(_ *zerolog.Event, level zerolog.Level, _ string) {
	if level >= zerolog.ErrorLevel {
		metrics.IncErrors()
	}
}

// Init configures the global logger. It is called once, from main. Logging is a
// cross-cutting concern; threading a logger through every constructor is noise
// that hides the dependencies that actually matter.
func Init(level string) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	parsed, err := zerolog.ParseLevel(level)
	if err != nil {
		parsed = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(parsed)

	log.Logger = zerolog.New(os.Stdout).
		With().Timestamp().Caller().Logger().
		Hook(errorCounterHook{})
}

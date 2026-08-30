package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
)

// Config is the fully validated runtime configuration of the bot. Every field
// is documented here rather than only in .example.env, because this is the
// struct people read when they wonder what a setting does.
type Config struct {
	// BotToken is the Telegram bot token from @BotFather.
	BotToken string `env:"BOT_TOKEN" validate:"required"`
	// TelegramAPIURL points the bot at a different Bot API server. Empty in
	// production; the offline test suite sets it to a fake server.
	TelegramAPIURL string `env:"TELEGRAM_API_URL"`
	// AllowedUsers pins who may use the bot. Empty means anyone may.
	AllowedUsers []int64 `env:"ALLOWED_USERS" envSeparator:","`

	// ClaudeAPIKey and ClaudeModel drive list building. The default model is
	// the cheapest one the Claude API offers.
	ClaudeAPIKey string `env:"CLAUDE_API_KEY" validate:"required"`
	ClaudeModel  string `env:"CLAUDE_MODEL"   validate:"required"`
	// ClaudeAPIURL overrides the API host. Empty in production; the offline
	// suite points it at a stub so the tests need no network and no budget.
	ClaudeAPIURL string `env:"CLAUDE_API_URL"`

	// OpenAIAPIKey and TranscribeModel handle voice messages. Anthropic models
	// take no audio, so transcription is a separate provider.
	OpenAIAPIKey    string `env:"OPENAI_API_KEY"   validate:"required"`
	TranscribeModel string `env:"TRANSCRIBE_MODEL" validate:"required"`
	// OpenAIAPIURL overrides the transcription host, for the same reason.
	OpenAIAPIURL string `env:"OPENAI_API_URL"`

	// DBPath is the SQLite file. It belongs on persistent storage.
	DBPath string `env:"DB_PATH" validate:"required"`

	// Workers bounds how many jobs run at once across all users, and QueueSize
	// how many may wait. Together they keep one busy user from stalling
	// everyone else.
	Workers   int `env:"WORKERS"    validate:"required,min=1,max=32"`
	QueueSize int `env:"QUEUE_SIZE" validate:"required,min=1"`

	// UserRatePerHour and UserBurst are the per-user token bucket. UserBurst
	// doubles as the cap on how many jobs one user may have in flight.
	UserRatePerHour int `env:"USER_RATE_PER_HOUR" validate:"required,min=1"`
	UserBurst       int `env:"USER_BURST"         validate:"required,min=1"`

	// JobTimeout bounds a single request end to end: transcription plus the
	// model call plus delivery.
	JobTimeout time.Duration `env:"JOB_TIMEOUT" validate:"required,min=1s"`

	HTTPAddr string `env:"HTTP_ADDR" validate:"required"`
	LogLevel string `env:"LOG_LEVEL" validate:"required,oneof=debug info warn error"`
}

// Load reads .env when present, overlays the process environment and validates
// the result. Any problem is fatal for the caller: a bot that starts with a
// half-valid config fails later and more confusingly, in front of users.
func Load() (Config, error) {
	_ = godotenv.Load()

	// Defaults are set before parsing, so the environment only has to carry
	// what actually differs from them.
	cfg := Config{
		ClaudeModel:     "claude-haiku-4-5",
		TranscribeModel: "whisper-1",
		DBPath:          "data/todobot.db",
		Workers:         3,
		QueueSize:       64,
		UserRatePerHour: 120,
		UserBurst:       5,
		JobTimeout:      2 * time.Minute,
		HTTPAddr:        ":8080",
		LogLevel:        "info",
	}

	err := env.Parse(&cfg)
	if err != nil {
		return Config{}, fmt.Errorf("parse env: %w", err)
	}

	err = validator.New().Struct(cfg)
	if err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

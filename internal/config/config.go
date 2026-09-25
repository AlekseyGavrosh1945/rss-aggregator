// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the runtime parameters of the application.
type Config struct {
	// BotToken is the Telegram bot token; an empty value disables the bot.
	BotToken string
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// FetchInterval is how often the feeds are polled.
	FetchInterval time.Duration
	// FetchTimeout is the per-feed download timeout.
	FetchTimeout time.Duration
	// Workers is the number of goroutines fetching feeds in parallel.
	Workers int
	// HTTPAddr is the address of the HTTP server serving /healthz.
	HTTPAddr string
}

// Load reads the configuration from the environment, applying defaults
// for unset variables and returning an error for malformed values.
func Load() (Config, error) {
	cfg := Config{
		BotToken:    os.Getenv("BOT_TOKEN"),
		DatabaseURL: getenv("DATABASE_URL", "postgres://rss:rss@localhost:5432/rss?sslmode=disable"),
		HTTPAddr:    getenv("HTTP_ADDR", ":8080"),
	}

	var err error
	if cfg.FetchInterval, err = getDuration("FETCH_INTERVAL", time.Minute); err != nil {
		return cfg, err
	}
	if cfg.FetchTimeout, err = getDuration("FETCH_TIMEOUT", 15*time.Second); err != nil {
		return cfg, err
	}
	if cfg.Workers, err = getPositiveInt("WORKERS", 8); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getDuration(key string, def time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	return v, nil
}

func getPositiveInt(key string, def int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", key, err)
	}
	if v < 1 {
		return 0, fmt.Errorf("config: %s: must be >= 1, got %d", key, v)
	}
	return v, nil
}

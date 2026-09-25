package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{"BOT_TOKEN", "DATABASE_URL", "FETCH_INTERVAL", "FETCH_TIMEOUT", "WORKERS", "HTTP_ADDR"} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BotToken != "" {
		t.Errorf("BotToken = %q, want empty", cfg.BotToken)
	}
	if cfg.DatabaseURL != "postgres://rss:rss@localhost:5432/rss?sslmode=disable" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.FetchInterval != time.Minute {
		t.Errorf("FetchInterval = %v, want 1m", cfg.FetchInterval)
	}
	if cfg.FetchTimeout != 15*time.Second {
		t.Errorf("FetchTimeout = %v, want 15s", cfg.FetchTimeout)
	}
	if cfg.Workers != 8 {
		t.Errorf("Workers = %d, want 8", cfg.Workers)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("BOT_TOKEN", "secret")
	t.Setenv("DATABASE_URL", "postgres://other")
	t.Setenv("FETCH_INTERVAL", "30s")
	t.Setenv("FETCH_TIMEOUT", "5s")
	t.Setenv("WORKERS", "3")
	t.Setenv("HTTP_ADDR", ":9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BotToken != "secret" || cfg.DatabaseURL != "postgres://other" ||
		cfg.FetchInterval != 30*time.Second || cfg.FetchTimeout != 5*time.Second ||
		cfg.Workers != 3 || cfg.HTTPAddr != ":9090" {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	t.Setenv("FETCH_INTERVAL", "not-a-duration")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error for bad FETCH_INTERVAL")
	}

	t.Setenv("FETCH_INTERVAL", "")
	t.Setenv("WORKERS", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error for WORKERS=0")
	}
}

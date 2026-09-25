// Command aggregator runs the RSS feed aggregator with an optional
// Telegram bot, a polling scheduler and a health-check HTTP server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AlekseyGavrosh1945/rss-aggregator/internal/config"
	"github.com/AlekseyGavrosh1945/rss-aggregator/internal/feed"
	"github.com/AlekseyGavrosh1945/rss-aggregator/internal/scheduler"
	"github.com/AlekseyGavrosh1945/rss-aggregator/internal/server"
	"github.com/AlekseyGavrosh1945/rss-aggregator/internal/storage"
	"github.com/AlekseyGavrosh1945/rss-aggregator/internal/telegram"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := storage.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer store.Close()

	if err := storage.Migrate(ctx, store.Pool()); err != nil {
		return fmt.Errorf("migrate db: %w", err)
	}
	log.Info("database ready")

	fetcher := feed.NewFetcher(cfg.FetchTimeout)

	bot, err := telegram.New(cfg.BotToken, store, fetcher, log)
	if err != nil {
		return fmt.Errorf("init telegram bot: %w", err)
	}

	var notifier scheduler.Notifier = scheduler.NoopNotifier{}
	if bot != nil {
		notifier = bot
	}

	sched := scheduler.New(store, fetcher, notifier,
		cfg.FetchInterval, cfg.Workers, log)
	go sched.Run(ctx)

	api := server.New(cfg.HTTPAddr, store, log)
	go func() {
		if err := api.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server stopped", "err", err)
			stop()
		}
	}()
	log.Info("http server listening", "addr", cfg.HTTPAddr)

	if bot != nil {
		go bot.Start()
		log.Info("telegram bot started")
	} else {
		log.Warn("BOT_TOKEN is empty: bot disabled, feed fetching only")
	}

	<-ctx.Done()
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := api.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown", "err", err)
	}
	if bot != nil {
		bot.Stop()
	}
	return nil
}

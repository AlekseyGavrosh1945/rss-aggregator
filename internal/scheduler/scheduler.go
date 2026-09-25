// Package scheduler periodically fetches all feeds with a worker pool
// and delivers new posts to subscribers.
package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/AlekseyGavrosh1945/rss-aggregator/internal/feed"
	"github.com/AlekseyGavrosh1945/rss-aggregator/internal/storage"
)

// Store is the persistence surface the scheduler needs.
type Store interface {
	ListFeeds(ctx context.Context) ([]storage.Feed, error)
	CreatePosts(ctx context.Context, feedID int64, posts []storage.Post) ([]storage.Post, error)
	Subscribers(ctx context.Context, feedID int64) ([]int64, error)
	UpdateFeedMeta(ctx context.Context, feedID int64, title, fetchErr string) error
}

// Notifier delivers new posts to a user (implemented by the Telegram bot).
type Notifier interface {
	NotifyNewPosts(ctx context.Context, chatID int64, feedTitle string, posts []storage.Post) error
}

// NoopNotifier drops notifications; used when the bot is disabled.
type NoopNotifier struct{}

// NotifyNewPosts implements Notifier by doing nothing.
func (NoopNotifier) NotifyNewPosts(context.Context, int64, string, []storage.Post) error {
	return nil
}

// FeedFetcher retrieves a single feed.
type FeedFetcher interface {
	Fetch(ctx context.Context, url string) (*feed.FeedData, error)
}

// Scheduler polls feeds on a fixed interval using a bounded worker pool.
type Scheduler struct {
	store    Store
	fetcher  FeedFetcher
	notifier Notifier
	interval time.Duration
	workers  int
	log      *slog.Logger
}

// New creates a Scheduler; workers below 1 are clamped to 1.
func New(store Store, fetcher FeedFetcher, notifier Notifier,
	interval time.Duration, workers int, log *slog.Logger) *Scheduler {
	if workers < 1 {
		workers = 1
	}
	return &Scheduler{
		store:    store,
		fetcher:  fetcher,
		notifier: notifier,
		interval: interval,
		workers:  workers,
		log:      log,
	}
}

// Run starts the polling loop and blocks until ctx is canceled.
func (s *Scheduler) Run(ctx context.Context) {
	s.refresh(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refresh(ctx)
		}
	}
}

// refresh dispatches one fetch of every feed across the worker pool.
func (s *Scheduler) refresh(ctx context.Context) {
	feeds, err := s.store.ListFeeds(ctx)
	if err != nil {
		s.log.Error("list feeds", "err", err)
		return
	}
	if len(feeds) == 0 {
		return
	}

	jobs := make(chan storage.Feed)
	var wg sync.WaitGroup
	for range s.workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				s.processFeed(ctx, f)
			}
		}()
	}
	for _, f := range feeds {
		jobs <- f
	}
	close(jobs)
	wg.Wait()
}

// processFeed fetches one feed, stores new posts and notifies subscribers.
func (s *Scheduler) processFeed(ctx context.Context, f storage.Feed) {
	data, err := s.fetcher.Fetch(ctx, f.URL)
	if err != nil {
		s.log.Warn("fetch failed", "url", f.URL, "err", err)
		if uerr := s.store.UpdateFeedMeta(ctx, f.ID, "", err.Error()); uerr != nil {
			s.log.Error("update feed meta", "feed_id", f.ID, "err", uerr)
		}
		return
	}

	posts := make([]storage.Post, 0, len(data.Items))
	for _, it := range data.Items {
		posts = append(posts, storage.Post{
			GUID:        it.GUID,
			Title:       it.Title,
			Link:        it.Link,
			PublishedAt: it.PublishedAt,
		})
	}

	inserted, err := s.store.CreatePosts(ctx, f.ID, posts)
	if err != nil {
		s.log.Error("save posts", "url", f.URL, "err", err)
		return
	}
	if err := s.store.UpdateFeedMeta(ctx, f.ID, data.Title, ""); err != nil {
		s.log.Error("update feed meta", "feed_id", f.ID, "err", err)
	}
	if len(inserted) == 0 {
		return
	}
	s.log.Info("new posts", "url", f.URL, "count", len(inserted))

	chatIDs, err := s.store.Subscribers(ctx, f.ID)
	if err != nil {
		s.log.Error("list subscribers", "feed_id", f.ID, "err", err)
		return
	}
	for _, chatID := range chatIDs {
		if err := s.notifier.NotifyNewPosts(ctx, chatID, data.Title, inserted); err != nil {
			s.log.Warn("notify failed", "chat_id", chatID, "err", err)
		}
	}
}

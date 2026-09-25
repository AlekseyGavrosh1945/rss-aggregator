package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/AlekseyGavrosh1945/rss-aggregator/internal/feed"
	"github.com/AlekseyGavrosh1945/rss-aggregator/internal/storage"
)

type fakeStore struct {
	mu          sync.Mutex
	feeds       []storage.Feed
	saved       map[int64][]storage.Post
	subscribers map[int64][]int64
	meta        []metaCall
}

type metaCall struct {
	feedID   int64
	title    string
	fetchErr string
}

func (s *fakeStore) ListFeeds(context.Context) ([]storage.Feed, error) { return s.feeds, nil }

func (s *fakeStore) CreatePosts(_ context.Context, feedID int64, posts []storage.Post) ([]storage.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	have := make(map[string]bool, len(s.saved[feedID]))
	for _, p := range s.saved[feedID] {
		have[p.GUID] = true
	}
	inserted := []storage.Post{}
	for _, p := range posts {
		if have[p.GUID] {
			continue
		}
		have[p.GUID] = true
		s.saved[feedID] = append(s.saved[feedID], p)
		inserted = append(inserted, p)
	}
	return inserted, nil
}

func (s *fakeStore) Subscribers(_ context.Context, feedID int64) ([]int64, error) {
	return s.subscribers[feedID], nil
}

func (s *fakeStore) UpdateFeedMeta(_ context.Context, feedID int64, title, fetchErr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta = append(s.meta, metaCall{feedID: feedID, title: title, fetchErr: fetchErr})
	return nil
}

type fakeFetcher struct {
	mu      sync.Mutex
	results map[string]*feed.FeedData
	errs    map[string]error
}

func (f *fakeFetcher) Fetch(_ context.Context, url string) (*feed.FeedData, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs[url]; err != nil {
		return nil, err
	}
	return f.results[url], nil
}

type notifyCall struct {
	chatID    int64
	feedTitle string
	posts     []storage.Post
}

type fakeNotifier struct {
	mu    sync.Mutex
	calls []notifyCall
}

func (n *fakeNotifier) NotifyNewPosts(_ context.Context, chatID int64, feedTitle string, posts []storage.Post) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, notifyCall{chatID: chatID, feedTitle: feedTitle, posts: posts})
	return nil
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var testFeedData = &feed.FeedData{
	Title: "Example",
	Items: []feed.Item{
		{GUID: "a", Title: "A", Link: "https://example.com/a"},
		{GUID: "b", Title: "B", Link: "https://example.com/b"},
	},
}

func TestRefreshDeliversNewPostsToSubscribers(t *testing.T) {
	const feedURL = "https://example.com/rss"
	store := &fakeStore{
		feeds:       []storage.Feed{{ID: 1, URL: feedURL}},
		saved:       map[int64][]storage.Post{},
		subscribers: map[int64][]int64{1: {101, 202}},
	}
	fetcher := &fakeFetcher{results: map[string]*feed.FeedData{feedURL: testFeedData}}
	notifier := &fakeNotifier{}

	s := New(store, fetcher, notifier, time.Hour, 4, testLogger())
	s.refresh(context.Background())

	if len(notifier.calls) != 2 {
		t.Fatalf("got %d notify calls, want 2 (one per subscriber)", len(notifier.calls))
	}
	for _, c := range notifier.calls {
		if len(c.posts) != 2 {
			t.Errorf("chat %d: got %d posts, want 2", c.chatID, len(c.posts))
		}
		if c.feedTitle != "Example" {
			t.Errorf("chat %d: feedTitle = %q, want %q", c.chatID, c.feedTitle, "Example")
		}
	}
	store.mu.Lock()
	lastMeta := store.meta[len(store.meta)-1]
	store.mu.Unlock()
	if lastMeta.title != "Example" || lastMeta.fetchErr != "" {
		t.Errorf("meta = %+v, want clean fetch with title", lastMeta)
	}

	// A second refresh must deliver only the genuinely new post.
	updated := &feed.FeedData{
		Title: "Example",
		Items: append(append([]feed.Item{}, testFeedData.Items...),
			feed.Item{GUID: "c", Title: "C", Link: "https://example.com/c"}),
	}
	fetcher.mu.Lock()
	fetcher.results[feedURL] = updated
	fetcher.mu.Unlock()

	before := len(notifier.calls)
	s.refresh(context.Background())
	if len(notifier.calls) != before+2 {
		t.Fatalf("got %d new notify calls, want 2", len(notifier.calls)-before)
	}
	for _, c := range notifier.calls[before:] {
		if len(c.posts) != 1 || c.posts[0].GUID != "c" {
			t.Errorf("chat %d: got %+v, want only post %q", c.chatID, c.posts, "c")
		}
	}
}

func TestRefreshRecordsFetchErrors(t *testing.T) {
	const feedURL = "https://broken.example/rss"
	store := &fakeStore{
		feeds: []storage.Feed{{ID: 7, URL: feedURL}},
		saved: map[int64][]storage.Post{},
	}
	fetcher := &fakeFetcher{errs: map[string]error{feedURL: errors.New("boom")}}
	notifier := &fakeNotifier{}

	s := New(store, fetcher, notifier, time.Hour, 2, testLogger())
	s.refresh(context.Background())

	if len(notifier.calls) != 0 {
		t.Errorf("got %d notify calls, want 0", len(notifier.calls))
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.meta) != 1 {
		t.Fatalf("got %d meta calls, want 1", len(store.meta))
	}
	if store.meta[0].feedID != 7 || store.meta[0].fetchErr == "" {
		t.Errorf("meta = %+v, want recorded error for feed 7", store.meta[0])
	}
}

func TestRefreshWithoutFeedsIsNoop(t *testing.T) {
	store := &fakeStore{saved: map[int64][]storage.Post{}}
	notifier := &fakeNotifier{}
	s := New(store, &fakeFetcher{}, notifier, time.Hour, 4, testLogger())
	s.refresh(context.Background())
	if len(notifier.calls) != 0 {
		t.Errorf("got %d notify calls, want 0", len(notifier.calls))
	}
}

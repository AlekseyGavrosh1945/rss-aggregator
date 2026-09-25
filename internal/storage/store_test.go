package storage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// newTestStore connects to the database from TEST_DATABASE_URL and applies
// migrations. Integration tests are skipped when the variable is not set.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping integration test")
	}
	ctx := context.Background()
	st, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(st.Close)
	if err := Migrate(ctx, st.Pool()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func uniqueID() int64 { return time.Now().UnixNano() }

func TestMigrateIsIdempotent(t *testing.T) {
	st := newTestStore(t)
	if err := Migrate(context.Background(), st.Pool()); err != nil {
		t.Fatalf("second migrate run: %v", err)
	}
}

func TestSubscribeIsIdempotent(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	userID, err := st.EnsureUser(ctx, uniqueID())
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	feedURL := fmt.Sprintf("https://example.com/%d.xml", uniqueID())

	f, isNew, err := st.Subscribe(ctx, userID, feedURL)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if !isNew {
		t.Error("first Subscribe: isNew = false, want true")
	}
	if _, isNew, err := st.Subscribe(ctx, userID, feedURL); err != nil || isNew {
		t.Errorf("second Subscribe: isNew = %v, err = %v; want false, nil", isNew, err)
	}

	feeds, err := st.UserFeeds(ctx, userID)
	if err != nil {
		t.Fatalf("UserFeeds: %v", err)
	}
	if len(feeds) != 1 || feeds[0].URL != feedURL || feeds[0].ID != f.ID {
		t.Errorf("UserFeeds = %+v, want exactly feed %d", feeds, f.ID)
	}
}

func TestUnsubscribe(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	userID, err := st.EnsureUser(ctx, uniqueID())
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	feedURL := fmt.Sprintf("https://example.com/%d.xml", uniqueID())
	if _, _, err := st.Subscribe(ctx, userID, feedURL); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	removed, err := st.Unsubscribe(ctx, userID, feedURL)
	if err != nil || !removed {
		t.Errorf("Unsubscribe = %v, %v; want true, nil", removed, err)
	}
	if removed, _ := st.Unsubscribe(ctx, userID, feedURL); removed {
		t.Error("second Unsubscribe removed a row, want none")
	}
}

func TestCreatePostsDeduplicates(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	userID, err := st.EnsureUser(ctx, uniqueID())
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	f, _, err := st.Subscribe(ctx, userID, fmt.Sprintf("https://example.com/%d.xml", uniqueID()))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	pub := time.Date(2025, 9, 1, 12, 0, 0, 0, time.UTC)
	posts := []Post{
		{GUID: "g1", Title: "one", Link: "https://example.com/1", PublishedAt: &pub},
		{GUID: "g2", Title: "two", Link: "https://example.com/2"},
	}
	inserted, err := st.CreatePosts(ctx, f.ID, posts)
	if err != nil {
		t.Fatalf("CreatePosts: %v", err)
	}
	if len(inserted) != 2 {
		t.Fatalf("first insert: got %d new posts, want 2", len(inserted))
	}
	if inserted[0].GUID != "g1" || inserted[0].PublishedAt == nil {
		t.Errorf("returned post = %+v, want guid g1 with timestamp", inserted[0])
	}

	// Re-inserting the same entries plus one new one must yield only the new one.
	inserted, err = st.CreatePosts(ctx, f.ID, append(posts,
		Post{GUID: "g3", Title: "three", Link: "https://example.com/3"}))
	if err != nil {
		t.Fatalf("second CreatePosts: %v", err)
	}
	if len(inserted) != 1 || inserted[0].GUID != "g3" {
		t.Errorf("second insert: got %+v, want only guid g3", inserted)
	}
}

func TestSubscribers(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	feedURL := fmt.Sprintf("https://example.com/%d.xml", uniqueID())
	chatA, chatB := uniqueID(), uniqueID()

	userA, _ := st.EnsureUser(ctx, chatA)
	userB, _ := st.EnsureUser(ctx, chatB)
	f, _, err := st.Subscribe(ctx, userA, feedURL)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if _, _, err := st.Subscribe(ctx, userB, feedURL); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	got, err := st.Subscribers(ctx, f.ID)
	if err != nil {
		t.Fatalf("Subscribers: %v", err)
	}
	if len(got) != 2 || !containsChat(got, chatA) || !containsChat(got, chatB) {
		t.Errorf("Subscribers = %v, want chat ids %d and %d", got, chatA, chatB)
	}
}

func TestUpdateFeedMeta(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	userID, _ := st.EnsureUser(ctx, uniqueID())
	feedURL := fmt.Sprintf("https://example.com/%d.xml", uniqueID())
	f, _, err := st.Subscribe(ctx, userID, feedURL)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := st.UpdateFeedMeta(ctx, f.ID, "Real Title", ""); err != nil {
		t.Fatalf("UpdateFeedMeta: %v", err)
	}
	if err := st.UpdateFeedMeta(ctx, f.ID, "", "connection reset"); err != nil {
		t.Fatalf("UpdateFeedMeta: %v", err)
	}

	feeds, err := st.UserFeeds(ctx, userID)
	if err != nil {
		t.Fatalf("UserFeeds: %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("got %d feeds, want 1", len(feeds))
	}
	if feeds[0].Title != "Real Title" {
		t.Errorf("Title = %q, want %q (must survive error updates)", feeds[0].Title, "Real Title")
	}
	if feeds[0].LastError != "connection reset" {
		t.Errorf("LastError = %q, want %q", feeds[0].LastError, "connection reset")
	}
}

func containsChat(ids []int64, chat int64) bool {
	for _, id := range ids {
		if id == chat {
			return true
		}
	}
	return false
}

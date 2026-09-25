// Package storage implements the PostgreSQL persistence layer.
package storage

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Feed is a tracked RSS feed.
type Feed struct {
	ID        int64
	URL       string
	Title     string
	LastError string
}

// Post is a single entry of a feed.
type Post struct {
	GUID        string
	Title       string
	Link        string
	PublishedAt *time.Time
}

// Store wraps a pgx connection pool and provides typed queries.
type Store struct {
	pool *pgxpool.Pool
}

// Connect opens a connection pool and verifies it with a ping.
func Connect(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Pool exposes the underlying pool (used for migrations).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Close releases the pool connections.
func (s *Store) Close() { s.pool.Close() }

// Ping checks database availability.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// EnsureUser returns the id of the Telegram user, creating it if needed.
func (s *Store) EnsureUser(ctx context.Context, chatID int64) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (chat_id) VALUES ($1)
		ON CONFLICT (chat_id) DO UPDATE SET chat_id = EXCLUDED.chat_id
		RETURNING id
	`, chatID).Scan(&id)
	return id, err
}

// Subscribe subscribes the user to the feed, creating the feed if needed.
// It reports whether the subscription itself is new.
func (s *Store) Subscribe(ctx context.Context, userID int64, feedURL string) (Feed, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Feed{}, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	if _, err := tx.Exec(ctx, `
		INSERT INTO feeds (url, title) VALUES ($1, $1)
		ON CONFLICT (url) DO NOTHING
	`, feedURL); err != nil {
		return Feed{}, false, err
	}

	var f Feed
	if err := tx.QueryRow(ctx, `
		SELECT id, url, title FROM feeds WHERE url = $1
	`, feedURL).Scan(&f.ID, &f.URL, &f.Title); err != nil {
		return Feed{}, false, err
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO subscriptions (user_id, feed_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, userID, f.ID)
	if err != nil {
		return Feed{}, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Feed{}, false, err
	}
	return f, tag.RowsAffected() == 1, nil
}

// Unsubscribe removes the user's subscription to the feed identified by url.
// It reports whether a subscription was actually removed.
func (s *Store) Unsubscribe(ctx context.Context, userID int64, feedURL string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM subscriptions s
		USING feeds f
		WHERE s.feed_id = f.id AND s.user_id = $1 AND f.url = $2
	`, userID, feedURL)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// UnsubscribeByID removes the user's subscription to the feed by id.
// It reports whether a subscription was actually removed.
func (s *Store) UnsubscribeByID(ctx context.Context, userID, feedID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM subscriptions WHERE user_id = $1 AND feed_id = $2`, userID, feedID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// UserFeeds lists the feeds the user is subscribed to.
func (s *Store) UserFeeds(ctx context.Context, userID int64) ([]Feed, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.url, f.title, f.last_error
		FROM feeds f
		JOIN subscriptions s ON s.feed_id = f.id
		WHERE s.user_id = $1
		ORDER BY s.created_at, f.id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectFeeds(rows)
}

// ListFeeds returns all tracked feeds.
func (s *Store) ListFeeds(ctx context.Context) ([]Feed, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, url, title, last_error FROM feeds ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectFeeds(rows)
}

// Subscribers returns the chat ids of all users subscribed to the feed.
func (s *Store) Subscribers(ctx context.Context, feedID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.chat_id
		FROM users u
		JOIN subscriptions s ON s.user_id = u.id
		WHERE s.feed_id = $1
	`, feedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chatIDs := []int64{}
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			return nil, err
		}
		chatIDs = append(chatIDs, chatID)
	}
	return chatIDs, rows.Err()
}

// CreatePosts inserts feed entries, skipping the ones already stored.
// It returns only the posts that were actually inserted (i.e. new ones).
func (s *Store) CreatePosts(ctx context.Context, feedID int64, posts []Post) ([]Post, error) {
	if len(posts) == 0 {
		return nil, nil
	}

	guids := make([]string, len(posts))
	titles := make([]string, len(posts))
	links := make([]string, len(posts))
	published := make([]*time.Time, len(posts))
	for i, p := range posts {
		guids[i], titles[i], links[i], published[i] = p.GUID, p.Title, p.Link, p.PublishedAt
	}

	// A single round trip: unnest the arrays, let the unique index on
	// (feed_id, guid) silently drop duplicates, and return what was inserted.
	rows, err := s.pool.Query(ctx, `
		INSERT INTO posts (feed_id, guid, title, link, published_at)
		SELECT $1, g, t, l, p
		FROM unnest($2::text[], $3::text[], $4::text[], $5::timestamptz[]) AS x(g, t, l, p)
		ON CONFLICT (feed_id, guid) DO NOTHING
		RETURNING guid, title, link, published_at
	`, feedID, guids, titles, links, published)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	inserted := []Post{}
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.GUID, &p.Title, &p.Link, &p.PublishedAt); err != nil {
			return nil, err
		}
		inserted = append(inserted, p)
	}
	return inserted, rows.Err()
}

// UpdateFeedMeta records the result of the last fetch attempt for the feed.
// An empty title leaves the stored title unchanged.
func (s *Store) UpdateFeedMeta(ctx context.Context, feedID int64, title, fetchErr string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE feeds
		SET last_fetched_at = now(),
		    last_error = $2,
		    title = CASE WHEN $3 = '' THEN title ELSE $3 END
		WHERE id = $1
	`, feedID, fetchErr, title)
	return err
}

func collectFeeds(rows pgx.Rows) ([]Feed, error) {
	feeds := []Feed{}
	for rows.Next() {
		var f Feed
		if err := rows.Scan(&f.ID, &f.URL, &f.Title, &f.LastError); err != nil {
			return nil, err
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

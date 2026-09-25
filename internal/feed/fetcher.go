// Package feed downloads and parses RSS/Atom feeds.
package feed

import (
	"context"
	"fmt"
	"time"

	"github.com/mmcdole/gofeed"
)

// Item is a single entry of a feed, normalized across RSS and Atom.
type Item struct {
	GUID        string
	Title       string
	Link        string
	PublishedAt *time.Time
}

// FeedData is the parsed result of one feed fetch.
type FeedData struct {
	Title string
	Items []Item
}

// Fetcher downloads feeds and converts them to FeedData.
type Fetcher struct {
	parser  *gofeed.Parser
	timeout time.Duration
}

// NewFetcher creates a Fetcher with the given per-request timeout.
func NewFetcher(timeout time.Duration) *Fetcher {
	return &Fetcher{parser: gofeed.NewParser(), timeout: timeout}
}

// Fetch downloads and parses the feed at url.
func (f *Fetcher) Fetch(ctx context.Context, url string) (*FeedData, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	parsed, err := f.parser.ParseURLWithContext(url, ctx)
	if err != nil {
		return nil, fmt.Errorf("parse feed %s: %w", url, err)
	}

	items := make([]Item, 0, len(parsed.Items))
	for _, it := range parsed.Items {
		guid := it.GUID
		if guid == "" {
			// Some feeds have no GUID; the link is a stable enough fallback.
			guid = it.Link
		}
		items = append(items, Item{
			GUID:        guid,
			Title:       it.Title,
			Link:        it.Link,
			PublishedAt: it.PublishedParsed,
		})
	}
	return &FeedData{Title: parsed.Title, Items: items}, nil
}

package feed

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// feedLinkTypes are the <link type="..."> values that point to a feed.
var feedLinkTypes = map[string]bool{
	"application/rss+xml":   true,
	"application/atom+xml":  true,
	"application/feed+json": true,
}

// ResolveFeedURL maps rawURL to an actual feed URL. If rawURL itself is a
// valid feed, it is returned unchanged. Otherwise the page HTML is searched
// for feed autodiscovery tags: <link rel="alternate" type="application/rss+xml">
// (or atom/feed+json), and the first match is resolved against the page URL.
func (f *Fetcher) ResolveFeedURL(ctx context.Context, rawURL string) (string, error) {
	if _, err := f.Fetch(ctx, rawURL); err == nil {
		return rawURL, nil
	}
	return f.discoverInPage(ctx, rawURL)
}

func (f *Fetcher) discoverInPage(ctx context.Context, pageURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("resolve feed %s: %w", pageURL, err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch page %s: %w", pageURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch page %s: HTTP %d", pageURL, resp.StatusCode)
	}

	return findFeedLink(resp.Body, pageURL)
}

// findFeedLink scans HTML for the first feed autodiscovery <link> tag and
// resolves its (possibly relative) href against the page URL.
func findFeedLink(r io.Reader, pageURL string) (string, error) {
	tokenizer := html.NewTokenizer(r)
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return "", fmt.Errorf("feed link not found in page %s", pageURL)
		case html.StartTagToken, html.SelfClosingTagToken:
		default:
			continue
		}

		name, more := tokenizer.TagName()
		if string(name) != "link" {
			continue
		}

		var rel, typ, href string
		for more {
			var key, val []byte
			key, val, more = tokenizer.TagAttr()
			switch strings.ToLower(string(key)) {
			case "rel":
				rel = strings.ToLower(string(val))
			case "type":
				typ = strings.ToLower(string(val))
			case "href":
				href = strings.TrimSpace(string(val))
			}
		}

		if rel != "alternate" || !feedLinkTypes[typ] || href == "" {
			continue
		}

		base, err := url.Parse(pageURL)
		if err != nil {
			return "", fmt.Errorf("resolve feed %s: %w", pageURL, err)
		}
		ref, err := url.Parse(href)
		if err != nil {
			continue // broken href, keep looking for other link tags
		}
		return base.ResolveReference(ref).String(), nil
	}
}

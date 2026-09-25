package feed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func rssBody() string {
	body, err := os.ReadFile("testdata/sample.rss")
	if err != nil {
		panic(err) // the fixture ships with the repo
	}
	return string(body)
}

// muxWithFeed serves a page and a feed; pageBody may be empty to serve only
// the feed under /rss.
func muxWithFeed(pageHTML string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/rss", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rssBody()))
	})
	if pageHTML != "" {
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(pageHTML))
		})
	}
	return mux
}

func TestResolveFeedURLAlreadyFeed(t *testing.T) {
	srv := httptest.NewServer(muxWithFeed(""))
	defer srv.Close()

	got, err := NewFetcher(time.Second).ResolveFeedURL(context.Background(), srv.URL+"/rss")
	if err != nil {
		t.Fatalf("ResolveFeedURL: %v", err)
	}
	if got != srv.URL+"/rss" {
		t.Errorf("got %q, want %q unchanged", got, srv.URL+"/rss")
	}
}

func TestResolveFeedURLFromHTMLLink(t *testing.T) {
	page := `<html><head>
		<title>Site</title>
		<link rel="stylesheet" href="/style.css">
		<link rel="alternate" type="application/rss+xml" title="RSS" href="/rss">
	</head><body>hello</body></html>`
	srv := httptest.NewServer(muxWithFeed(page))
	defer srv.Close()

	got, err := NewFetcher(time.Second).ResolveFeedURL(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatalf("ResolveFeedURL: %v", err)
	}
	if got != srv.URL+"/rss" {
		t.Errorf("got %q, want %q", got, srv.URL+"/rss")
	}
}

func TestResolveFeedURLAbsoluteHref(t *testing.T) {
	page := `<html><head><link rel="alternate" type="application/atom+xml" href="https://example.com/atom.xml"></head></html>`
	srv := httptest.NewServer(muxWithFeed(page))
	defer srv.Close()

	got, err := NewFetcher(time.Second).ResolveFeedURL(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatalf("ResolveFeedURL: %v", err)
	}
	if got != "https://example.com/atom.xml" {
		t.Errorf("got %q, want absolute href unchanged", got)
	}
}

func TestResolveFeedURLNotFound(t *testing.T) {
	srv := httptest.NewServer(muxWithFeed("<html><head><title>no feed here</title></head></html>"))
	defer srv.Close()

	if _, err := NewFetcher(time.Second).ResolveFeedURL(context.Background(), srv.URL+"/"); err == nil {
		t.Fatal("expected an error for a page without feed links, got nil")
	}
}

func TestFindFeedLinkIgnoresBrokenHref(t *testing.T) {
	page := `<html><head>
		<link rel="alternate" type="application/rss+xml" href="ht tp://bad url">
		<link rel="alternate" type="application/rss+xml" href="/rss">
	</head></html>`
	got, err := findFeedLink(strings.NewReader(page), "https://example.com/")
	if err != nil {
		t.Fatalf("findFeedLink: %v", err)
	}
	if got != "https://example.com/rss" {
		t.Errorf("got %q, want %q (first working link wins)", got, "https://example.com/rss")
	}
}

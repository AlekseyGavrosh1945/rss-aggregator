package feed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func serve(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetcherParsesRSS(t *testing.T) {
	body, err := os.ReadFile("testdata/sample.rss")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := serve(t, http.StatusOK, string(body))

	f := NewFetcher(5 * time.Second)
	data, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if data.Title != "Habr / Go" {
		t.Errorf("Title = %q, want %q", data.Title, "Habr / Go")
	}
	if len(data.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(data.Items))
	}

	first := data.Items[0]
	want := time.Date(2025, 9, 1, 10, 0, 0, 0, time.UTC)
	if first.GUID != "habr-1" {
		t.Errorf("GUID = %q, want %q", first.GUID, "habr-1")
	}
	if first.Title != "Первая новость" {
		t.Errorf("Title = %q, want %q", first.Title, "Первая новость")
	}
	if first.Link != "https://habr.com/ru/articles/1/" {
		t.Errorf("Link = %q", first.Link)
	}
	if first.PublishedAt == nil || !first.PublishedAt.Equal(want) {
		t.Errorf("PublishedAt = %v, want %v", first.PublishedAt, want)
	}

	// An item without GUID must fall back to its link.
	second := data.Items[1]
	if second.GUID != "https://habr.com/ru/articles/2/" {
		t.Errorf("GUID fallback = %q, want the item link", second.GUID)
	}
}

func TestFetcherReportsHTTPError(t *testing.T) {
	srv := serve(t, http.StatusNotFound, "not found")
	if _, err := NewFetcher(time.Second).Fetch(context.Background(), srv.URL); err == nil {
		t.Fatal("expected an error for HTTP 404, got nil")
	}
}

func TestFetcherReportsInvalidXML(t *testing.T) {
	srv := serve(t, http.StatusOK, "this is definitely not xml")
	if _, err := NewFetcher(time.Second).Fetch(context.Background(), srv.URL); err == nil {
		t.Fatal("expected a parse error, got nil")
	}
}

func TestFetcherRespectsTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()

	if _, err := NewFetcher(50*time.Millisecond).Fetch(context.Background(), srv.URL); err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
}

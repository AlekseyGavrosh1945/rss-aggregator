// Package server exposes a minimal HTTP API for health checks.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Pinger is the dependency the health endpoint checks.
type Pinger interface {
	Ping(ctx context.Context) error
}

// New creates the HTTP server with a /healthz endpoint that reports
// 200 when the database is reachable and 503 otherwise.
func New(addr string, db Pinger, log *slog.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")
		if err := db.Ping(ctx); err != nil {
			log.Warn("health check failed", "err", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "db unavailable"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

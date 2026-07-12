// Package handlers holds the HTTP handlers. In Phase 1 there is only the home
// page; later phases add auth, documents, and signing.
package handlers

import (
	"log/slog"
	"net/http"

	"github.com/programuoki/signflow/internal/config"
	"github.com/programuoki/signflow/internal/db"
	"github.com/programuoki/signflow/internal/web"
)

// Handlers bundles the dependencies every handler needs. It is constructed once
// in main and its methods are registered on the router.
type Handlers struct {
	Cfg     config.Config
	Queries *db.Queries
	Log     *slog.Logger
}

func New(cfg config.Config, q *db.Queries, log *slog.Logger) *Handlers {
	return &Handlers{Cfg: cfg, Queries: q, Log: log}
}

// Home renders the landing page, including a live user count read from Postgres.
func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	count, err := h.Queries.CountUsers(r.Context())
	if err != nil {
		h.Log.Error("count users", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, web.Home(count))
}

// Health is a plain-text liveness probe for Railway / uptime checks.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	if _, err := h.Queries.CountUsers(r.Context()); err != nil {
		http.Error(w, "db unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

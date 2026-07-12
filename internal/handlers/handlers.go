// Package handlers holds the HTTP handlers, router, and middleware.
package handlers

import (
	"log/slog"
	"net/http"

	"github.com/programuoki/signflow/internal/config"
	"github.com/programuoki/signflow/internal/db"
	"github.com/programuoki/signflow/internal/email"
	"github.com/programuoki/signflow/internal/session"
	"github.com/programuoki/signflow/internal/storage"
	"github.com/programuoki/signflow/internal/web"
)

// Handlers bundles the dependencies every handler needs. It is constructed once
// in main and its methods are registered on the router.
type Handlers struct {
	Cfg      config.Config
	Queries  *db.Queries
	Sessions *session.Manager
	Mailer   email.Sender
	Store    storage.Store
	Log      *slog.Logger
}

func New(cfg config.Config, q *db.Queries, sessions *session.Manager, mailer email.Sender, store storage.Store, log *slog.Logger) *Handlers {
	return &Handlers{Cfg: cfg, Queries: q, Sessions: sessions, Mailer: mailer, Store: store, Log: log}
}

// Home renders the landing page. If the visitor is signed in we send them to
// their dashboard instead.
func (h *Handlers) Home(w http.ResponseWriter, r *http.Request) {
	if _, ok := UserFrom(r.Context()); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	count, err := h.Queries.CountUsers(r.Context())
	if err != nil {
		h.serverError(w, r, "count users", err)
		return
	}
	render(w, r, http.StatusOK, web.Home(h.nav(r), count))
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

// serverError logs and returns a 500.
func (h *Handlers) serverError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	h.Log.Error(msg, "err", err, "path", r.URL.Path)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

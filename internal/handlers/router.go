package handlers

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Router builds the Chi router with the base middleware stack. Route groups for
// auth-protected pages are added in later phases.
func (h *Handlers) Router(staticFS fs.FS) http.Handler {
	r := chi.NewRouter()

	// Base middleware, outermost first.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)    // request logging
	r.Use(middleware.Recoverer) // turn panics into 500s instead of crashing

	// Static assets (CSS, vendored htmx).
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	r.Get("/", h.Home)
	r.Get("/healthz", h.Health)

	return r
}

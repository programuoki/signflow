package handlers

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/csrf"
)

// Router builds the Chi router with the middleware stack and all routes.
//
// csrfKey must be 32 bytes; it authenticates the CSRF token cookie.
func (h *Handlers) Router(staticFS fs.FS, csrfKey []byte) http.Handler {
	r := chi.NewRouter()

	// Base middleware, outermost first.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// CSRF: enforced on all unsafe methods (POST/PUT/PATCH/DELETE). Safe methods
	// pass through but get the token cookie set so forms can embed it. The field
	// name matches the hidden input our templates render.
	// In dev we serve plaintext HTTP on localhost. gorilla/csrf otherwise assumes
	// TLS and enforces a strict Origin/Referer check that rejects http://
	// origins. Marking dev requests plaintext relaxes that one check while
	// keeping the token check. Production (HTTPS at Railway's edge) is NOT marked,
	// so it retains the full strict-origin protection.
	if !h.Cfg.IsProd() {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, csrf.PlaintextHTTPRequest(req))
			})
		})
	}

	csrfMW := csrf.Protect(
		csrfKey,
		csrf.Secure(h.Cfg.IsProd()),
		csrf.Path("/"),
		csrf.SameSite(csrf.SameSiteLaxMode),
		csrf.FieldName("gorilla.csrf.Token"),
	)
	r.Use(csrfMW)

	// Resolve the session cookie into the request context for every route.
	r.Use(h.LoadUser)

	// Static assets (CSS, vendored htmx).
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Public routes.
	r.Get("/", h.Home)
	r.Get("/healthz", h.Health)

	// Auth routes.
	r.Get("/register", h.RegisterForm)
	r.Post("/register", h.Register)
	r.Get("/login", h.LoginForm)
	r.Post("/login", h.Login)
	r.Post("/logout", h.Logout)
	r.Get("/forgot", h.ForgotForm)
	r.Post("/forgot", h.Forgot)
	r.Get("/reset/{token}", h.ResetForm)
	r.Post("/reset/{token}", h.Reset)

	// Authenticated routes.
	r.Group(func(pr chi.Router) {
		pr.Use(h.RequireAuth)
		pr.Get("/dashboard", h.Dashboard)
	})

	return r
}

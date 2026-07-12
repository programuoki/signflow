package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/programuoki/signflow/internal/db"
	"github.com/programuoki/signflow/internal/session"
)

type ctxKey int

const userCtxKey ctxKey = iota

// LoadUser resolves the session cookie into the current user and stashes it in
// the request context. It never blocks anonymous requests — it just annotates
// them — so it can wrap every route.
func (h *Handlers) LoadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := h.Sessions.User(r.Context(), r)
		if err == nil {
			r = r.WithContext(context.WithValue(r.Context(), userCtxKey, user))
		} else if !errors.Is(err, session.ErrNoSession) {
			// A real DB error is worth logging, but we still serve the request
			// as anonymous rather than 500 the whole site.
			h.Log.Error("load user from session", "err", err)
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth blocks routes that need a signed-in user, redirecting to login.
func (h *Handlers) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFrom(r.Context()); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// UserFrom returns the current user from the context, if any.
func UserFrom(ctx context.Context) (db.User, bool) {
	u, ok := ctx.Value(userCtxKey).(db.User)
	return u, ok
}

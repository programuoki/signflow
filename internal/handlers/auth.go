package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/gorilla/csrf"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/programuoki/signflow/internal/auth"
	"github.com/programuoki/signflow/internal/db"
	"github.com/programuoki/signflow/internal/email"
	"github.com/programuoki/signflow/internal/web"
)

// passwordResetTTL is how long a reset link stays valid.
const passwordResetTTL = time.Hour

// nav builds the per-request nav model (current user + CSRF token) for templates.
func (h *Handlers) nav(r *http.Request) web.Nav {
	n := web.Nav{CSRFToken: csrf.Token(r)}
	if u, ok := UserFrom(r.Context()); ok {
		n.LoggedIn = true
		n.Email = u.Email
	}
	return n
}

// --- Register ---

func (h *Handlers) RegisterForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, web.Register(h.nav(r), "", ""))
}

func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	emailAddr := normalizeEmail(r.FormValue("email"))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	fail := func(msg string) {
		render(w, r, http.StatusUnprocessableEntity, web.Register(h.nav(r), emailAddr, msg))
	}

	if !validEmail(emailAddr) {
		fail("Please enter a valid email address.")
		return
	}
	if password != confirm {
		fail("Passwords do not match.")
		return
	}
	hash, err := auth.HashPassword(password)
	if errors.Is(err, auth.ErrPasswordLength) {
		fail("Password must be between 8 and 64 characters.")
		return
	}
	if err != nil {
		h.serverError(w, r, "hash password", err)
		return
	}

	// The UNIQUE constraint on email is the real guard; check first for a nice
	// message, and still handle the race by inspecting the insert error.
	if _, err := h.Queries.GetUserByEmail(r.Context(), emailAddr); err == nil {
		fail("That email is already registered. Try logging in.")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		h.serverError(w, r, "lookup user", err)
		return
	}

	user, err := h.Queries.CreateUser(r.Context(), db.CreateUserParams{
		Email:        emailAddr,
		PasswordHash: hash,
	})
	if err != nil {
		if isUniqueViolation(err) {
			fail("That email is already registered. Try logging in.")
			return
		}
		h.serverError(w, r, "create user", err)
		return
	}

	if err := h.Sessions.Create(r.Context(), w, user.ID); err != nil {
		h.serverError(w, r, "create session", err)
		return
	}
	h.Log.Info("user registered", "email", user.Email)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// --- Login / Logout ---

func (h *Handlers) LoginForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, web.Login(h.nav(r), "", ""))
}

func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	emailAddr := normalizeEmail(r.FormValue("email"))
	password := r.FormValue("password")

	// One generic message for every failure mode avoids leaking which emails
	// are registered.
	invalid := func() {
		render(w, r, http.StatusUnprocessableEntity, web.Login(h.nav(r), emailAddr, "Invalid email or password."))
	}

	user, err := h.Queries.GetUserByEmail(r.Context(), emailAddr)
	if errors.Is(err, pgx.ErrNoRows) {
		invalid()
		return
	}
	if err != nil {
		h.serverError(w, r, "lookup user", err)
		return
	}
	if !auth.CheckPassword(user.PasswordHash, password) {
		invalid()
		return
	}

	if err := h.Sessions.Create(r.Context(), w, user.ID); err != nil {
		h.serverError(w, r, "create session", err)
		return
	}
	h.Log.Info("user logged in", "email", user.Email)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Sessions.Destroy(r.Context(), w, r); err != nil {
		h.serverError(w, r, "destroy session", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// --- Password reset ---

func (h *Handlers) ForgotForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, web.Forgot(h.nav(r), false, ""))
}

func (h *Handlers) Forgot(w http.ResponseWriter, r *http.Request) {
	emailAddr := normalizeEmail(r.FormValue("email"))

	// Always show the same confirmation regardless of whether the account
	// exists — no account enumeration. Do the real work only if it does.
	if validEmail(emailAddr) {
		if err := h.sendResetLink(r, emailAddr); err != nil {
			h.Log.Error("send reset link", "err", err, "email", emailAddr)
			// Still show the generic confirmation; don't reveal internal errors.
		}
	}
	render(w, r, http.StatusOK, web.Forgot(h.nav(r), true, emailAddr))
}

// sendResetLink creates a reset token for an existing user and emails the link.
// A no-op (nil) if the email isn't registered.
func (h *Handlers) sendResetLink(r *http.Request, emailAddr string) error {
	user, err := h.Queries.GetUserByEmail(r.Context(), emailAddr)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	raw, hash, err := auth.GenerateToken()
	if err != nil {
		return err
	}
	if _, err := h.Queries.CreatePasswordReset(r.Context(), db.CreatePasswordResetParams{
		TokenHash: hash,
		UserID:    user.ID,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(passwordResetTTL), Valid: true},
	}); err != nil {
		return err
	}

	link := h.Cfg.BaseURL + "/reset/" + raw
	text := fmt.Sprintf(
		"Someone requested a password reset for your SignFlow account.\n\n"+
			"Reset your password (link valid for 1 hour):\n%s\n\n"+
			"If this wasn't you, you can safely ignore this email.",
		link,
	)
	return h.Mailer.Send(r.Context(), email.Message{
		To:      user.Email,
		Subject: "Reset your SignFlow password",
		Text:    text,
		HTML:    fmt.Sprintf(`<p>Reset your SignFlow password (valid 1 hour):</p><p><a href="%s">%s</a></p>`, link, link),
	})
}

func (h *Handlers) ResetForm(w http.ResponseWriter, r *http.Request) {
	token := chiURLParam(r, "token")
	if _, err := h.Queries.GetValidPasswordReset(r.Context(), auth.HashToken(token)); err != nil {
		render(w, r, http.StatusOK, web.ResetInvalid(h.nav(r)))
		return
	}
	render(w, r, http.StatusOK, web.ResetPassword(h.nav(r), token, ""))
}

func (h *Handlers) Reset(w http.ResponseWriter, r *http.Request) {
	token := chiURLParam(r, "token")
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	prt, err := h.Queries.GetValidPasswordReset(r.Context(), auth.HashToken(token))
	if err != nil {
		render(w, r, http.StatusOK, web.ResetInvalid(h.nav(r)))
		return
	}

	fail := func(msg string) {
		render(w, r, http.StatusUnprocessableEntity, web.ResetPassword(h.nav(r), token, msg))
	}
	if password != confirm {
		fail("Passwords do not match.")
		return
	}
	hash, err := auth.HashPassword(password)
	if errors.Is(err, auth.ErrPasswordLength) {
		fail("Password must be between 8 and 64 characters.")
		return
	}
	if err != nil {
		h.serverError(w, r, "hash password", err)
		return
	}

	if err := h.Queries.UpdateUserPassword(r.Context(), db.UpdateUserPasswordParams{
		ID:           prt.UserID,
		PasswordHash: hash,
	}); err != nil {
		h.serverError(w, r, "update password", err)
		return
	}
	// Burn this token and any other outstanding ones, and log the user out of
	// all existing sessions — a password change should invalidate them.
	_ = h.Queries.MarkPasswordResetUsed(r.Context(), prt.ID)
	_ = h.Queries.InvalidateUserPasswordResets(r.Context(), prt.UserID)
	_ = h.Queries.DeleteSessionsForUser(r.Context(), prt.UserID)

	h.Log.Info("password reset completed", "user_id", prt.UserID.String())
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- Dashboard ---

func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, web.Dashboard(h.nav(r)))
}

// --- helpers ---

func normalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func validEmail(s string) bool {
	if s == "" || len(s) > 254 {
		return false
	}
	_, err := mail.ParseAddress(s)
	return err == nil
}

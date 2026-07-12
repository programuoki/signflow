// Package session implements server-side sessions backed by Postgres.
//
// The browser only ever holds an opaque random token in an HttpOnly cookie. The
// authoritative session lives in the `sessions` table, keyed by the SHA-256 hash
// of that token. This is the deliberate counterpoint to the Pica app's JWT: no
// claims in the token, state on the server, and revocation is a single DELETE.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/programuoki/signflow/internal/db"
)

// CookieName is the session cookie. The __Host- prefix would be even stricter
// but requires HTTPS always; we keep a plain name so dev over http works.
const CookieName = "signflow_session"

// Lifetime is how long a session (and its cookie) stays valid.
const Lifetime = 30 * 24 * time.Hour

// ErrNoSession means the request carried no valid session.
var ErrNoSession = errors.New("no session")

// Manager creates, resolves, and destroys sessions.
type Manager struct {
	q      *db.Queries
	secure bool // set Secure flag on cookies (true in production)
}

func NewManager(q *db.Queries, secure bool) *Manager {
	return &Manager{q: q, secure: secure}
}

// Create issues a new session for userID and writes the cookie.
func (m *Manager) Create(ctx context.Context, w http.ResponseWriter, userID pgtype.UUID) error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(Lifetime)
	if _, err := m.q.CreateSession(ctx, db.CreateSessionParams{
		TokenHash: hashToken(token),
		UserID:    userID,
		ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	}); err != nil {
		return err
	}
	http.SetCookie(w, m.cookie(token, expires))
	return nil
}

// User resolves the current user from the request's session cookie. It returns
// ErrNoSession when there is no cookie or the session is expired/unknown.
func (m *Manager) User(ctx context.Context, r *http.Request) (db.User, error) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return db.User{}, ErrNoSession
	}
	row, err := m.q.GetUserBySessionToken(ctx, hashToken(c.Value))
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, ErrNoSession
	}
	if err != nil {
		return db.User{}, err
	}
	return row.User, nil
}

// Destroy deletes the current session server-side and clears the cookie.
func (m *Manager) Destroy(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		if err := m.q.DeleteSession(ctx, hashToken(c.Value)); err != nil {
			return err
		}
	}
	// Expire the cookie in the browser.
	http.SetCookie(w, m.cookie("", time.Unix(0, 0)))
	return nil
}

// cookie builds the session cookie with security flags applied.
func (m *Manager) cookie(value string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,                 // JS can't read it — mitigates XSS token theft
		Secure:   m.secure,             // HTTPS-only in production
		SameSite: http.SameSiteLaxMode, // sent on top-level navigations, not cross-site POSTs
	}
}

// randomToken returns 32 bytes of CSPRNG entropy, URL-safe base64 encoded.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken is what we actually store/look up, so a DB dump reveals no usable
// session tokens.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

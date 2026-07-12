package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/programuoki/signflow/internal/auth"
	"github.com/programuoki/signflow/internal/db"
	"github.com/programuoki/signflow/internal/email"
	"github.com/programuoki/signflow/internal/web"
)

// signerLinkTTL bounds how long a signing link is valid. Signers take days, not
// minutes, so this is generous — but it's still bounded, so a leaked link isn't
// useful forever.
const signerLinkTTL = 30 * 24 * time.Hour

const maxSignedNameLen = 200

// --- Owner: invite signers ---

// InviteSigners creates signer rows for a draft, flips it draft -> sent, and
// emails each signer their tokened link. All DB writes happen in one transaction.
func (h *Handlers) InviteSigners(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)
	id, ok := parseUUID(chiURLParam(r, "id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	doc, err := h.Queries.GetDocumentForOwner(r.Context(), db.GetDocumentForOwnerParams{ID: id, OwnerID: user.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.serverError(w, r, "get document", err)
		return
	}
	if doc.Status != "draft" {
		http.Error(w, "signers can only be invited while the document is a draft", http.StatusUnprocessableEntity)
		return
	}

	emails := parseEmails(r.FormValue("emails"))
	if len(emails) == 0 {
		h.renderDocumentDetail(w, r, doc, "Enter at least one valid email address.")
		return
	}

	// One transaction: create every signer + flip the document to sent. If any
	// insert fails, nothing is committed and the document stays a draft.
	type invite struct{ email, link string }
	var invites []invite

	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		h.serverError(w, r, "begin tx", err)
		return
	}
	defer tx.Rollback(context.Background())
	q := h.Queries.WithTx(tx)

	for _, addr := range emails {
		raw, hash, err := auth.GenerateToken()
		if err != nil {
			h.serverError(w, r, "generate token", err)
			return
		}
		if _, err := q.CreateSigner(r.Context(), db.CreateSignerParams{
			DocumentID: doc.ID,
			Email:      addr,
			TokenHash:  hash,
			ExpiresAt:  pgtype.Timestamptz{Time: time.Now().Add(signerLinkTTL), Valid: true},
		}); err != nil {
			h.serverError(w, r, "create signer", err)
			return
		}
		invites = append(invites, invite{email: addr, link: h.Cfg.BaseURL + "/sign/" + raw})
	}

	if _, err := q.MarkDocumentSent(r.Context(), db.MarkDocumentSentParams{ID: doc.ID, OwnerID: user.ID}); err != nil {
		h.serverError(w, r, "mark sent", err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.serverError(w, r, "commit", err)
		return
	}

	// Send the links after the DB is committed (console sender prints them).
	for _, inv := range invites {
		text := fmt.Sprintf(
			"You've been invited to sign \"%s\" on SignFlow.\n\n"+
				"Open your signing link (valid 30 days):\n%s\n",
			doc.Filename, inv.link)
		if err := h.Mailer.Send(r.Context(), email.Message{
			To:      inv.email,
			Subject: fmt.Sprintf("Please sign \"%s\"", doc.Filename),
			Text:    text,
			HTML:    fmt.Sprintf(`<p>You've been invited to sign <strong>%s</strong>.</p><p><a href="%s">Open your signing link</a> (valid 30 days).</p>`, doc.Filename, inv.link),
		}); err != nil {
			h.Log.Error("send signer invite", "err", err, "to", inv.email)
		}
	}
	h.Log.Info("document sent for signature", "id", doc.ID.String(), "signers", len(invites))

	http.Redirect(w, r, "/documents/"+doc.ID.String(), http.StatusSeeOther)
}

// --- Signer: the tokened, account-less signing flow ---

// SignPage renders the signing page for a valid token.
func (h *Handlers) SignPage(w http.ResponseWriter, r *http.Request) {
	signer, doc, ok := h.resolveSigner(w, r)
	if !ok {
		return
	}
	h.renderSignState(w, r, signer, doc, "")
}

// SignDownload streams the document to a signer via their token (no account).
func (h *Handlers) SignDownload(w http.ResponseWriter, r *http.Request) {
	_, doc, ok := h.resolveSigner(w, r)
	if !ok {
		return
	}
	h.streamDocument(w, r, doc)
}

// Sign records the signature: typed name + now + SHA-256 of the document at this
// moment. If this is the last outstanding signer, the document flips to completed.
func (h *Handlers) Sign(w http.ResponseWriter, r *http.Request) {
	signer, doc, ok := h.resolveSigner(w, r)
	if !ok {
		return
	}
	// Idempotent: re-posting a signed token just shows the signed state.
	if signer.Status == "signed" {
		h.renderSignState(w, r, signer, doc, "")
		return
	}
	if signerExpired(signer) {
		render(w, r, http.StatusOK, web.SignExpired(h.nav(r)))
		return
	}

	name := strings.TrimSpace(r.FormValue("signed_name"))
	if name == "" || len(name) > maxSignedNameLen {
		h.renderSignState(w, r, signer, doc, "Please type your full name (up to 200 characters).")
		return
	}

	// The document hash AT SIGNING TIME — recomputed from the stored bytes right
	// now, not trusted from the record. This is what the signature attests to.
	integ, err := h.checkIntegrity(r.Context(), doc)
	if err != nil {
		h.serverError(w, r, "hash document", err)
		return
	}

	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		h.serverError(w, r, "begin tx", err)
		return
	}
	defer tx.Rollback(context.Background())
	q := h.Queries.WithTx(tx)

	n, err := q.SignSigner(r.Context(), db.SignSignerParams{
		ID:            signer.ID,
		SignedName:    &name,
		SignedDocHash: &integ.CurrentHash,
	})
	if err != nil {
		h.serverError(w, r, "sign", err)
		return
	}
	if n == 0 {
		// Lost a race — already signed between load and now. Show signed state.
		_ = tx.Rollback(r.Context())
		h.SignPage(w, r)
		return
	}

	pending, err := q.CountPendingSigners(r.Context(), doc.ID)
	if err != nil {
		h.serverError(w, r, "count pending", err)
		return
	}
	if pending == 0 {
		if err := q.MarkDocumentCompleted(r.Context(), doc.ID); err != nil {
			h.serverError(w, r, "mark completed", err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.serverError(w, r, "commit", err)
		return
	}
	h.Log.Info("document signed", "doc", doc.ID.String(), "signer", signer.Email, "hash", integ.CurrentHash, "remaining", pending)

	// PRG: redirect to GET so a refresh doesn't re-post.
	http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
}

// --- helpers ---

// resolveSigner loads the signer for the {token} param and its document. Writes
// the appropriate error page and returns ok=false when the token is unknown.
func (h *Handlers) resolveSigner(w http.ResponseWriter, r *http.Request) (db.Signer, db.Document, bool) {
	token := chiURLParam(r, "token")
	signer, err := h.Queries.GetSignerByToken(r.Context(), auth.HashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		render(w, r, http.StatusNotFound, web.SignInvalid(h.nav(r)))
		return db.Signer{}, db.Document{}, false
	}
	if err != nil {
		h.serverError(w, r, "get signer", err)
		return db.Signer{}, db.Document{}, false
	}
	doc, err := h.Queries.GetDocument(r.Context(), signer.DocumentID)
	if err != nil {
		h.serverError(w, r, "get document", err)
		return db.Signer{}, db.Document{}, false
	}
	return signer, doc, true
}

// renderSignState shows the right signing page for the signer's state: expired,
// already-signed, or the sign form (with an optional error message).
func (h *Handlers) renderSignState(w http.ResponseWriter, r *http.Request, signer db.Signer, doc db.Document, errMsg string) {
	integ, err := h.checkIntegrity(r.Context(), doc)
	if err != nil {
		h.serverError(w, r, "hash document", err)
		return
	}
	nav := h.nav(r)
	docView := viewDocument(doc)
	integView := viewIntegrity(integ, doc)

	if signer.Status == "signed" {
		render(w, r, http.StatusOK, web.SignDone(nav, docView, integView, viewSigner(signer)))
		return
	}
	if signerExpired(signer) {
		render(w, r, http.StatusOK, web.SignExpired(nav))
		return
	}
	token := chiURLParam(r, "token")
	render(w, r, http.StatusOK, web.SignForm(nav, docView, integView, signer.Email, token, errMsg))
}

// integrity is the result of recomputing the stored file's hash and comparing it
// to the hash recorded at upload.
type integrity struct {
	Matches     bool
	CurrentHash string
}

// checkIntegrity re-hashes the stored bytes and compares to doc.FileHash. A
// mismatch means the file changed after upload — the tamper-evidence property.
func (h *Handlers) checkIntegrity(ctx context.Context, doc db.Document) (integrity, error) {
	rc, err := h.Store.Open(ctx, doc.StorageKey)
	if err != nil {
		return integrity{}, err
	}
	defer rc.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, rc); err != nil {
		return integrity{}, err
	}
	cur := hex.EncodeToString(sum.Sum(nil))
	return integrity{Matches: cur == doc.FileHash, CurrentHash: cur}, nil
}

func signerExpired(s db.Signer) bool {
	return s.ExpiresAt.Valid && s.ExpiresAt.Time.Before(time.Now())
}

// parseEmails splits a textarea of emails (newline/comma separated), normalizes,
// validates, and de-duplicates while preserving order.
func parseEmails(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';' || r == ' ' || r == '\t'
	})
	seen := map[string]bool{}
	var out []string
	for _, f := range fields {
		addr := normalizeEmail(f)
		if addr == "" || seen[addr] || !validEmail(addr) {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	return out
}

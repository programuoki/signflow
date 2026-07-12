package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/programuoki/signflow/internal/db"
	"github.com/programuoki/signflow/internal/web"
)

// Dashboard lists the signed-in user's documents.
func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)
	docs, err := h.Queries.ListDocumentsByOwner(r.Context(), user.ID)
	if err != nil {
		h.serverError(w, r, "list documents", err)
		return
	}
	render(w, r, http.StatusOK, web.Dashboard(h.nav(r), viewDocuments(docs)))
}

// UploadDocument accepts a multipart file, hashes and stores it, and records it
// as a draft owned by the current user.
func (h *Handlers) UploadDocument(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	// Cap the request body. +1KiB slack covers multipart field overhead so a
	// file exactly at the limit still succeeds.
	r.Body = http.MaxBytesReader(w, r.Body, h.Cfg.MaxUploadBytes+1024)

	file, header, err := r.FormFile("file")
	if err != nil {
		// Covers both "no file" and "too large" (MaxBytesReader error).
		h.Log.Warn("upload rejected", "err", err, "user", user.Email)
		http.Error(w, "upload failed: file missing or too large", http.StatusRequestEntityTooLarge)
		return
	}
	defer file.Close()

	if header.Size == 0 {
		http.Error(w, "upload failed: empty file", http.StatusBadRequest)
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	key, size, hash, err := h.Store.Save(r.Context(), file)
	if err != nil {
		h.serverError(w, r, "store file", err)
		return
	}

	doc, err := h.Queries.CreateDocument(r.Context(), db.CreateDocumentParams{
		OwnerID:     user.ID,
		Filename:    sanitizeFilename(header.Filename),
		ContentType: contentType,
		Size:        size,
		FileHash:    hash,
		StorageKey:  key,
	})
	if err != nil {
		_ = h.Store.Delete(r.Context(), key) // don't orphan the blob
		h.serverError(w, r, "create document", err)
		return
	}
	h.Log.Info("document uploaded", "id", doc.ID.String(), "owner", user.Email, "size", size, "sha256", hash)

	h.respondWithDocList(w, r, user)
}

// ViewDocument renders a single document's detail page (owner only): metadata,
// an integrity check, and the signer roster.
func (h *Handlers) ViewDocument(w http.ResponseWriter, r *http.Request) {
	doc, ok := h.ownedDocument(w, r)
	if !ok {
		return
	}
	h.renderDocumentDetail(w, r, doc, "")
}

// renderDocumentDetail loads the integrity status and signer roster and renders
// the owner detail page, optionally with an inline error (e.g. bad invite input).
func (h *Handlers) renderDocumentDetail(w http.ResponseWriter, r *http.Request, doc db.Document, errMsg string) {
	integ, err := h.checkIntegrity(r.Context(), doc)
	if err != nil {
		h.serverError(w, r, "hash document", err)
		return
	}
	signers, err := h.Queries.ListSignersByDocument(r.Context(), doc.ID)
	if err != nil {
		h.serverError(w, r, "list signers", err)
		return
	}
	render(w, r, http.StatusOK, web.DocumentDetail(
		h.nav(r), viewDocument(doc), viewIntegrity(integ, doc), viewSigners(signers, doc), errMsg,
	))
}

// DownloadDocument streams the stored file back with its original name (owner only).
func (h *Handlers) DownloadDocument(w http.ResponseWriter, r *http.Request) {
	doc, ok := h.ownedDocument(w, r)
	if !ok {
		return
	}
	h.streamDocument(w, r, doc)
}

// streamDocument writes the stored file as an attachment. Shared by the owner
// download route and the tokened signer download route.
func (h *Handlers) streamDocument(w http.ResponseWriter, r *http.Request, doc db.Document) {
	rc, err := h.Store.Open(r.Context(), doc.StorageKey)
	if err != nil {
		h.serverError(w, r, "open file", err)
		return
	}
	defer rc.Close()

	safe := sanitizeFilename(doc.Filename)
	w.Header().Set("Content-Type", doc.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", doc.Size))
	// Provide both a plain ASCII filename and a UTF-8 one per RFC 6266.
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", safe, url.PathEscape(doc.Filename)))
	if _, err := io.Copy(w, rc); err != nil {
		h.Log.Warn("stream download", "err", err, "id", doc.ID.String())
	}
}

// DeleteDocument deletes a draft the user owns. Non-drafts are refused — the
// first real business rule: once a document is sent, its record is immutable.
func (h *Handlers) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)
	id, ok := parseUUID(chiURLParam(r, "id"))
	if !ok {
		http.NotFound(w, r)
		return
	}

	doc, err := h.Queries.GetDocumentForOwner(r.Context(), db.GetDocumentForOwnerParams{ID: id, OwnerID: user.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r) // not owned / not found — don't leak which
		return
	}
	if err != nil {
		h.serverError(w, r, "get document", err)
		return
	}
	if doc.Status != "draft" {
		http.Error(w, "only draft documents can be deleted", http.StatusUnprocessableEntity)
		return
	}

	n, err := h.Queries.DeleteDraftDocument(r.Context(), db.DeleteDraftDocumentParams{ID: id, OwnerID: user.ID})
	if err != nil {
		h.serverError(w, r, "delete document", err)
		return
	}
	if n == 1 {
		// Best-effort blob cleanup; a leftover file is harmless but we try.
		_ = h.Store.Delete(r.Context(), doc.StorageKey)
		h.Log.Info("document deleted", "id", doc.ID.String(), "owner", user.Email)
	}

	h.respondWithDocList(w, r, user)
}

// --- helpers ---

// ownedDocument loads the {id} document if the current user owns it, else writes
// a 404 and returns ok=false.
func (h *Handlers) ownedDocument(w http.ResponseWriter, r *http.Request) (db.Document, bool) {
	user := mustUser(r)
	id, ok := parseUUID(chiURLParam(r, "id"))
	if !ok {
		http.NotFound(w, r)
		return db.Document{}, false
	}
	doc, err := h.Queries.GetDocumentForOwner(r.Context(), db.GetDocumentForOwnerParams{ID: id, OwnerID: user.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return db.Document{}, false
	}
	if err != nil {
		h.serverError(w, r, "get document", err)
		return db.Document{}, false
	}
	return doc, true
}

// respondWithDocList returns the refreshed list partial to HTMX callers, or
// redirects to the dashboard for a plain form post.
func (h *Handlers) respondWithDocList(w http.ResponseWriter, r *http.Request, user db.User) {
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	docs, err := h.Queries.ListDocumentsByOwner(r.Context(), user.ID)
	if err != nil {
		h.serverError(w, r, "list documents", err)
		return
	}
	render(w, r, http.StatusOK, web.DocumentList(viewDocuments(docs)))
}

func mustUser(r *http.Request) db.User {
	u, _ := UserFrom(r.Context()) // routes are behind RequireAuth
	return u
}

func parseUUID(s string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return u, false
	}
	return u, u.Valid
}

// sanitizeFilename strips path separators and control characters so a filename
// can't traverse directories or inject headers.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(rn rune) rune {
		if rn < 0x20 || rn == 0x7f || rn == '"' {
			return -1
		}
		return rn
	}, name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "document"
	}
	return name
}

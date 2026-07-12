-- name: CreateDocument :one
INSERT INTO documents (owner_id, filename, content_type, size, file_hash, storage_key)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListDocumentsByOwner :many
SELECT * FROM documents
WHERE owner_id = $1
ORDER BY created_at DESC;

-- name: GetDocumentForOwner :one
-- Owner-scoped read: returns nothing if the document belongs to someone else,
-- so authorization is enforced in the query, not just the handler.
SELECT * FROM documents
WHERE id = $1 AND owner_id = $2;

-- name: GetDocument :one
-- Unscoped read by id. ONLY for flows already authorized by a signer token —
-- never call this directly from a user-facing route (use GetDocumentForOwner).
SELECT * FROM documents WHERE id = $1;

-- name: MarkDocumentSent :execrows
-- draft -> sent. Owner-scoped and draft-guarded, so it is idempotent-safe and
-- can't resurrect a completed document.
UPDATE documents SET status = 'sent'
WHERE id = $1 AND owner_id = $2 AND status = 'draft';

-- name: MarkDocumentCompleted :exec
UPDATE documents SET status = 'completed' WHERE id = $1;

-- name: DeleteDraftDocument :execrows
-- Deletes only if the caller owns it AND it is still a draft. Returns the number
-- of rows affected so the handler can tell "deleted" from "not allowed".
DELETE FROM documents
WHERE id = $1 AND owner_id = $2 AND status = 'draft';

-- name: CreateSigner :one
INSERT INTO signers (document_id, email, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetSignerByToken :one
-- The token hash is the only key a signer presents. Unique, so it maps to
-- exactly one signer row — no signer can reach another's row or document.
SELECT * FROM signers WHERE token_hash = $1;

-- name: ListSignersByDocument :many
SELECT * FROM signers WHERE document_id = $1 ORDER BY created_at;

-- name: SignSigner :execrows
-- Records the signature, but only if still pending. The status='pending' guard
-- makes signing single-use: a spent token updates zero rows.
UPDATE signers
SET status = 'signed', signed_name = $2, signed_at = now(), signed_doc_hash = $3
WHERE id = $1 AND status = 'pending';

-- name: CountPendingSigners :one
SELECT count(*) FROM signers WHERE document_id = $1 AND status = 'pending';

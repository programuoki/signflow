-- name: CreateAuditEvent :exec
INSERT INTO audit_events (
    document_id, event_type, message, actor_type, actor_user_id, actor_signer_id, actor_label
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListAuditEventsByDocument :many
SELECT * FROM audit_events WHERE document_id = $1 ORDER BY seq;

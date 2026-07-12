package handlers

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/programuoki/signflow/internal/db"
)

// Event type constants — one per state change we record.
const (
	evtUploaded  = "document.uploaded"
	evtSent      = "document.sent"
	evtViewed    = "signer.viewed"
	evtSigned    = "document.signed"
	evtCompleted = "document.completed"
	evtDeleted   = "document.deleted"
	evtBadToken  = "signer.bad_token"
	evtTamper    = "document.tamper_detected"
)

// auditActor is the polymorphic actor for an audit event: exactly one of user /
// signer, or neither for system/anonymous. Label is the human identity frozen
// into the row.
type auditActor struct {
	Type     string
	UserID   pgtype.UUID
	SignerID pgtype.UUID
	Label    string
}

func actorUser(u db.User) auditActor {
	return auditActor{Type: "user", UserID: u.ID, Label: u.Email}
}
func actorSigner(s db.Signer) auditActor {
	return auditActor{Type: "signer", SignerID: s.ID, Label: s.Email}
}
func actorSystem() auditActor {
	return auditActor{Type: "system", Label: "system"}
}
func actorAnonymous() auditActor {
	return auditActor{Type: "anonymous", Label: "anonymous"}
}

// audit writes one event. Pass a transaction-bound *db.Queries (via WithTx) to
// record the event atomically with the state change it describes; pass h.Queries
// for best-effort read-path events (viewed, tamper). Returns the DB error so tx
// callers can roll back on failure.
func (h *Handlers) audit(ctx context.Context, q *db.Queries, docID pgtype.UUID, eventType, message string, a auditActor) error {
	return q.CreateAuditEvent(ctx, db.CreateAuditEventParams{
		DocumentID:    docID,
		EventType:     eventType,
		Message:       message,
		ActorType:     a.Type,
		ActorUserID:   a.UserID,
		ActorSignerID: a.SignerID,
		ActorLabel:    a.Label,
	})
}

// auditBestEffort records a read-path event and only logs on failure — used where
// the event must not fail the user's request (page views).
func (h *Handlers) auditBestEffort(ctx context.Context, docID pgtype.UUID, eventType, message string, a auditActor) {
	if err := h.audit(ctx, h.Queries, docID, eventType, message, a); err != nil {
		h.Log.Error("audit write", "err", err, "event", eventType)
	}
}

// shortHash abbreviates a hex hash for audit messages.
func shortHash(hash string) string {
	if len(hash) <= 16 {
		return hash
	}
	return hash[:8] + "…" + hash[len(hash)-8:]
}

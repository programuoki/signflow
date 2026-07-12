package handlers

import (
	"github.com/programuoki/signflow/internal/db"
	"github.com/programuoki/signflow/internal/web"
)

func viewIntegrity(integ integrity, doc db.Document) web.IntegrityView {
	return web.IntegrityView{
		Matches:     integ.Matches,
		StoredHash:  doc.FileHash,
		CurrentHash: integ.CurrentHash,
	}
}

func viewSigner(s db.Signer) web.SignerView {
	v := web.SignerView{
		Email:    s.Email,
		Status:   s.Status,
		IsSigned: s.Status == "signed",
	}
	if s.SignedName != nil {
		v.SignedName = *s.SignedName
	}
	if s.SignedAt.Valid {
		v.SignedAt = s.SignedAt.Time.Format("2006-01-02 15:04 MST")
	}
	if s.SignedDocHash != nil {
		v.SignedDocHash = *s.SignedDocHash
	}
	return v
}

func viewAuditEvents(events []db.AuditEvent) []web.AuditView {
	out := make([]web.AuditView, 0, len(events))
	for _, e := range events {
		out = append(out, web.AuditView{
			Time:       e.CreatedAt.Time.Format("2006-01-02 15:04:05 MST"),
			ActorLabel: e.ActorLabel,
			ActorType:  e.ActorType,
			Message:    e.Message,
			EventType:  e.EventType,
			Security:   e.EventType == "signer.bad_token" || e.EventType == "document.tamper_detected",
		})
	}
	return out
}

func viewSigners(signers []db.Signer, doc db.Document) web.SignerRoster {
	roster := web.SignerRoster{Total: len(signers)}
	for _, s := range signers {
		sv := viewSigner(s)
		// Flag any signature made against a different hash than the file now
		// holds — extra tamper visibility on the roster.
		if sv.IsSigned {
			roster.Signed++
			sv.HashMatchesNow = sv.SignedDocHash == doc.FileHash
		}
		roster.Signers = append(roster.Signers, sv)
	}
	return roster
}

package handlers

import (
	"fmt"

	"github.com/programuoki/signflow/internal/db"
	"github.com/programuoki/signflow/internal/web"
)

func viewDocument(d db.Document) web.DocumentView {
	return web.DocumentView{
		ID:          d.ID.String(),
		Filename:    d.Filename,
		ContentType: d.ContentType,
		SizeHuman:   humanizeBytes(d.Size),
		FileHash:    d.FileHash,
		Status:      d.Status,
		CreatedAt:   d.CreatedAt.Time.Format("2006-01-02 15:04"),
		IsDraft:     d.Status == "draft",
	}
}

func viewDocuments(docs []db.Document) []web.DocumentView {
	out := make([]web.DocumentView, 0, len(docs))
	for _, d := range docs {
		out = append(out, viewDocument(d))
	}
	return out
}

// humanizeBytes renders a byte count as e.g. "1.4 MB".
func humanizeBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

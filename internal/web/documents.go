package web

// DocumentView is the template-facing shape of a document: plain strings, no
// pgtype. Handlers map db.Document into this.
type DocumentView struct {
	ID          string
	Filename    string
	ContentType string
	SizeHuman   string
	FileHash    string
	Status      string
	CreatedAt   string
	IsDraft     bool
}

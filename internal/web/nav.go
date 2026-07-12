package web

// Nav carries the per-request bits the layout needs: who is signed in (for the
// header) and a CSRF token (for the logout form, which is a POST).
type Nav struct {
	LoggedIn  bool
	Email     string
	CSRFToken string
}

// Package static embeds the CSS and vendored JS so the binary is fully
// self-contained — no separate asset directory to deploy.
package static

import "embed"

//go:embed assets
var FS embed.FS

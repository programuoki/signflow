// Package migrations embeds the goose SQL migration files so they can be run
// at application startup without shipping the .sql files alongside the binary.
// This is what makes `railway up` self-migrating.
package migrations

import "embed"

//go:embed migrations/*.sql
var FS embed.FS

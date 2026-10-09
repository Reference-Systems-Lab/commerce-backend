// Package migrations embeds the database migrations in the binary, so `backend migrate` needs no files.
package migrations

import "embed"

// FS holds the goose migrations, applied in file-name order.
//
//go:embed *.sql
var FS embed.FS

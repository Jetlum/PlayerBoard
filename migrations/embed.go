// Package migrations embeds the SQL migration files so they ship inside the binary
// and can be applied on boot via golang-migrate's iofs source.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

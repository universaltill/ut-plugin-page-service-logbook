// Package db embeds the plugin's SQLite migrations (one statement per
// `db_exec` call, ADR-0149 §2; no PRAGMA / BEGIN, #3157 slice A rules).
package db

import "embed"

// Migrations holds migrations/*.sql, applied in file-name order.
//
//go:embed migrations/*.sql
var Migrations embed.FS

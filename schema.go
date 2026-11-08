// Package aminte holds the one file that the Go packages cannot reach from their own folders.
package aminte

import _ "embed"

// Schema is the one canonical DDL of life.db: docs/schema/schema.sql, embedded so that the binary carries it.
// internal/db reads it. The file in docs/ is edited in place (AGENTS.md, "The one hard rule"). go:embed may reach
// a subfolder of the package's folder, which is why this file is at the root.
//
//go:embed docs/schema/schema.sql
var Schema string

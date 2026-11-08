package migrations

import "embed"

// Files contains reviewed, explicit MySQL migrations. Nothing runs on API startup.
//
//go:embed *.up.sql
var Files embed.FS

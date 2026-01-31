package lemuria

import "embed"

// StaticFS contains the embedded static files for the web UI.
//
//go:embed all:static
var StaticFS embed.FS

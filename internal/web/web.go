package web

import "embed"

// Dist holds the built SPA. The repository ships a placeholder index.html
// so the Go build always succeeds; the Vite build overwrites it.
//
//go:embed all:dist
var Dist embed.FS

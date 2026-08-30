package web

import (
	"embed"
)

// DistFS contains the pre-compiled Vite production frontend assets.
//
//go:embed all:dist
var DistFS embed.FS

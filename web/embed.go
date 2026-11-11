// Package web embeds the built frontend assets.
package web

import "embed"

// Files contains the production frontend build. The dist directory is kept in
// the repository with a .gitkeep placeholder so this package always compiles.
//
//go:embed all:dist
var Files embed.FS

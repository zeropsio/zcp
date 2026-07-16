// Package webui embeds the built Data Console SPA. The SPA is framework-free
// (no build step) so the binary is fully self-contained and cross-compiles to
// the container without a Node toolchain. The React/Vite swap, if wanted later,
// is local to this package (the server only knows the fs.FS).
package webui

import (
	"embed"
	"io/fs"
)

//go:embed dist
var distFS embed.FS

// dist is the SPA rooted at dist/. fs.Sub on a compile-time embed with a known
// directory cannot fail; the explicit discard documents that.
var dist, _ = fs.Sub(distFS, "dist")

// FS returns the embedded SPA filesystem.
func FS() fs.FS { return dist }

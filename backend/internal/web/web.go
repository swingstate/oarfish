// Package web embeds the production frontend assets.
package web

import (
	"embed"
	"io/fs"
)

// The all: prefix keeps dist/.gitkeep embeddable, so the module compiles
// before the frontend has been built. Build the frontend first for a usable UI.
//
//go:embed all:dist
var dist embed.FS

// Assets returns the embedded frontend rooted at the Vite output directory.
func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// Unreachable: "dist" is a compile-time embedded directory.
		panic(err)
	}
	return sub
}

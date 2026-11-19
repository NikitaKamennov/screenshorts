// Package frontend embeds the desktop interface without a separate JavaScript build.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed web
var assets embed.FS

// Files returns the root served by the desktop shell.
func Files() (fs.FS, error) {
	return fs.Sub(assets, "web")
}

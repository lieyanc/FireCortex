// Package webui embeds the exported frontend so the server ships as a single binary.
//
// dist/ is populated by scripts/build-binary.sh (a copy of frontend/out) right
// before go build; the repository only tracks dist/.gitkeep, so plain backend
// builds compile without a frontend and serve the API only.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the embedded frontend export, or nil when the binary was built without one.
func FS() fs.FS {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil
	}
	if info, err := fs.Stat(dist, "index.html"); err != nil || info.IsDir() {
		return nil
	}
	return dist
}

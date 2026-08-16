package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var content embed.FS

func Dist() fs.FS {
	dist, err := fs.Sub(content, "dist")
	if err != nil {
		return content
	}
	return dist
}

func Index() ([]byte, bool) {
	index, err := content.ReadFile("dist/index.html")
	if err != nil {
		return nil, false
	}
	return index, true
}

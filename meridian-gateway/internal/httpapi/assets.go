package httpapi

import (
	"embed"
	"io/fs"
)

//go:embed web/*
var webFiles embed.FS

func WebAssets() fs.FS {
	assets, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	return assets
}

//go:embed openapi.json
var OpenAPISpec []byte

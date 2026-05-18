// Package static embeds the on-disk static assets into the binary at build time.
//
// We expose the bundled FS through Files so handlers can simply do
//
//	http.FileServer(http.FS(static.Files))
//
// at routing time.
package static

import "embed"

//go:embed *.css *.js
var Files embed.FS

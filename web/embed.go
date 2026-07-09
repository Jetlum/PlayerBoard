// Package web embeds the dev dashboard so the api can serve it in DevMode with no
// separate frontend build or CORS setup (same origin as the API).
package web

import "embed"

//go:embed *.html
var FS embed.FS

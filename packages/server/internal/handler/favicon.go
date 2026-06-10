package handler

import (
	_ "embed"
	"net/http"
	"strconv"
	"time"
)

// Favicon served at /favicon.ico and /favicon.svg. The asset is the
// same SVG the web app ships; the API subdomain serves it so browser
// auto-requests to api.memax.app/favicon.ico don't flood logs with
// 404s AND so the admin / developer hub tabs get a proper icon when
// someone has the API open in a tab (e.g. while manually hitting the
// OAuth metadata endpoints from a browser).
//
// Served as image/svg+xml for BOTH paths — all modern browsers
// accept SVG as a favicon. Legacy clients that strictly require a
// multi-resolution .ico container would see a broken icon, which is
// fine for an API surface.

//go:embed assets/favicon.svg
var faviconSVG []byte

// FaviconHandler writes the embedded SVG with long-lived cache
// headers. The asset is baked into the binary, so a cache bust
// happens naturally on every server deploy.
func FaviconHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Content-Length", strconv.Itoa(len(faviconSVG)))
	// 1-year immutable cache — the content hashes through the
	// binary version (a deploy invalidates), so browsers and CDNs
	// can cache aggressively.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Last-Modified", time.Unix(0, 0).UTC().Format(http.TimeFormat))
	_, _ = w.Write(faviconSVG)
}

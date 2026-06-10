package attachments

import (
	"bytes"
	"context"
	"image"
	// Register stdlib raster decoders. WebP is deferred to phase 2
	// (requires golang.org/x/image/webp). If the whitelist grows to
	// include webp, also import _ "golang.org/x/image/webp" here.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
)

// probeReadCap bounds how many bytes we read to decode image headers.
// image.DecodeConfig only needs the header, so 2MB is conservative —
// even multi-megapixel JPEGs fit their headers in the first few KB,
// but a cap is set so a single malformed upload can't hold the
// decoder open on a huge buffered read.
const probeReadCap = 2 * 1024 * 1024

// inlineWhitelist is the set of content-types that the view endpoint
// is permitted to serve with Content-Disposition: inline. Every other
// MIME (including image/svg+xml and image/heic) is forced to
// Content-Disposition: attachment, even if the DB row's
// content_type matches and inline_eligible is true.
//
// Defense in depth: the backend also writes inline_eligible=false for
// any upload that fails image.DecodeConfig, so the check is effective
// at two layers — whitelist here and DB flag in the handler.
var inlineWhitelist = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/gif":  {},
	// "image/webp" — phase 2 with golang.org/x/image/webp
	// "image/heic" — deferred; browser support is uneven
	// "image/svg+xml" — never; SVG is scriptable
}

// IsInlineWhitelisted reports whether the content-type is allowed to
// be served with Content-Disposition: inline. Match is
// case-insensitive on the media-type portion; parameters like
// "; charset=..." are stripped before comparison.
func IsInlineWhitelisted(contentType string) bool {
	media := strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.Index(media, ";"); idx >= 0 {
		media = strings.TrimSpace(media[:idx])
	}
	_, ok := inlineWhitelist[media]
	return ok
}

// ProbeImageBytes runs image.DecodeConfig on an in-memory buffer.
// Returns (width, height, true) on success; (0, 0, false) on any
// decode error including unrecognized format. Never returns an
// error — callers use the ok flag to branch into downgrade.
func ProbeImageBytes(data []byte) (width int, height int, ok bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// ProbeImageObject fetches up to probeReadCap bytes from the object
// store and runs image.DecodeConfig on them. Used for FileRef
// uploads where the client pushed bytes directly to the bucket via
// a presigned URL and the server never sees the in-memory payload.
//
// Adds one bucket GET per upload for declared image/* FileRef cases.
// Accepted cost to avoid the larger security regression of trusting
// client-declared MIME at the view endpoint.
func ProbeImageObject(ctx context.Context, store objectstore.Store, key string) (width int, height int, ok bool) {
	if store == nil || strings.TrimSpace(key) == "" {
		return 0, 0, false
	}
	res, err := store.Get(ctx, key)
	if err != nil {
		return 0, 0, false
	}
	defer res.Body.Close()
	cfg, _, err := image.DecodeConfig(io.LimitReader(res.Body, probeReadCap))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

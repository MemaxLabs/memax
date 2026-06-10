package handler

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
)

// AdminSeedImagesHandler backs `POST /v1/admin/seed-images/upload` —
// admin-only inline-image upload for onboarding seed templates.
//
// Flow (client uploads via three-phase, like /v1/uploads):
//  1. Admin POSTs filename + content-type + size_bytes
//  2. Server validates (admin auth via AdminMiddleware), generates a
//     uuid-keyed object_key under `public/seed-images/<uuid>.<ext>`,
//     and presigns a PUT URL bound to the declared size
//  3. Client PUTs the file directly to the presigned URL
//  4. Stable public URL is returned at step 2 alongside the upload URL,
//     so the client can insert `![alt](public_url)` into the seed's
//     markdown content as soon as the PUT finishes
//
// Public-read by design: seed images ship to every signup, so they're
// public content. The bucket prefix `public/*` is anonymous-readable
// (Cloudflare R2 public.r2.dev scoped to that prefix; local MinIO
// `mc anonymous set download local/memax-storage/public`). The
// resulting URL goes into seed markdown and resolves for any user
// receiving a copy.
//
// Constraints (codex review `bpvr1s3oq`):
//   - Whitelist content types: image/png, image/jpeg, image/webp,
//     image/gif. NO image/svg+xml — public SVG has XSS footgun.
//   - Cap size at 5MB (seed images are screenshots / logos, not
//     hi-res photo assets).
//   - Object key has no original filename (privacy) — uuid + ext only.
//   - 503 if R2_PUBLIC_BASE_URL env unset (deploy-time misconfig
//     surfaces to admin clearly rather than silently uploading to
//     unreachable URLs).
//
// Not exposed via memax-sdk (admin endpoints stay under
// packages/web/src/lib/admin-client/, never in memax-sdk — see
// AGENTS.md "Admin Surface Boundary").
type AdminSeedImagesHandler struct {
	objectStore objectstore.Store
	// publicBase is the bucket-relative public-read base URL captured
	// at startup. Empty → uploads are unavailable (handler returns
	// 503). Cached at construction rather than read per request so the
	// configured value matches what server logs report at boot;
	// matches the rest of the codebase's "config at startup" pattern
	// (codex post-exec review `bdqqn8y1t` Low #1).
	publicBase string
}

func NewAdminSeedImagesHandler(s objectstore.Store, publicBase string) *AdminSeedImagesHandler {
	return &AdminSeedImagesHandler{
		objectStore: s,
		publicBase:  strings.TrimRight(strings.TrimSpace(publicBase), "/"),
	}
}

const (
	adminSeedImageMaxBytes        int64 = 5 * 1024 * 1024
	adminSeedImageKeyPrefix             = "public/seed-images"
	adminSeedImagePresignDuration       = 15 * time.Minute
)

// adminSeedImageAllowedContentTypes is the whitelist of accepted
// inline-image MIME types. SVG is intentionally omitted — public-
// read SVG can carry script payloads (XSS) without server-side
// sanitization.
var adminSeedImageAllowedContentTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

type adminSeedImageUploadRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

// Upload handles `POST /v1/admin/seed-images/upload`. Auth is enforced
// by the admin middleware wrapping registerAdminRoutes; no per-handler
// gate needed here.
func (h *AdminSeedImagesHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if h.publicBase == "" {
		writeError(w, http.StatusServiceUnavailable, "uploads_unavailable",
			"Seed image uploads require R2_PUBLIC_BASE_URL to be configured.")
		return
	}
	if h.objectStore == nil {
		writeError(w, http.StatusServiceUnavailable, "uploads_unavailable",
			"Object store is not configured.")
		return
	}

	var req adminSeedImageUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}

	contentType := normalizeContentType(req.ContentType)
	ext, ok := adminSeedImageAllowedContentTypes[contentType]
	if !ok {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_content_type",
			fmt.Sprintf("Content type %q is not allowed for seed images.", contentType))
		return
	}

	if req.SizeBytes <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_size", "size_bytes must be a positive integer.")
		return
	}
	if req.SizeBytes > adminSeedImageMaxBytes {
		WriteErrorWithDetails(w, http.StatusRequestEntityTooLarge, "file_too_large",
			fmt.Sprintf("File size %d exceeds the seed-image limit of %d bytes.",
				req.SizeBytes, adminSeedImageMaxBytes),
			map[string]any{
				"size_bytes": req.SizeBytes,
				"max_bytes":  adminSeedImageMaxBytes,
			})
		return
	}

	id, err := newSeedImageID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "id_generation_failed", err.Error())
		return
	}
	objectKey := fmt.Sprintf("%s/%s%s", adminSeedImageKeyPrefix, id, ext)

	uploadURL, headers, err := h.objectStore.PresignPutWithSize(
		r.Context(),
		objectKey,
		contentType,
		req.SizeBytes,
		adminSeedImagePresignDuration,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "presign_failed", err.Error())
		return
	}

	publicURL := fmt.Sprintf("%s/%s", h.publicBase, objectKey)

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"object_key":   objectKey,
		"public_url":   publicURL,
		"upload_url":   uploadURL,
		"headers":      headers,
		"content_type": contentType,
	}})
}

// newSeedImageID returns a 16-byte hex id (32 chars) — enough entropy
// for a single bucket prefix without uuid lib dependency.
func newSeedImageID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

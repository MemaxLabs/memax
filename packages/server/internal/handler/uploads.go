package handler

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
)

// UploadPurpose names the single legitimate upload flow. Memory attachments
// are user-picked files attached to memories; agent session sync was the
// other path historically but has been removed. Clients MUST supply this
// value in the presign request — there is no default.
const (
	UploadPurposeMemoryAttachment = "memory_attachment"
)

// MemoryAttachmentContentTypes is the allowlist for user-picked attachment
// uploads. The guiding rule: only accept formats we can actually ingest
// (extract text from, or describe via vision). Video/audio/archives are
// rejected because we have no pipeline to turn them into memory content.
var MemoryAttachmentContentTypes = map[string]struct{}{
	// Documents
	"application/pdf": {},
	// Text
	"text/plain":       {},
	"text/markdown":    {},
	"text/html":        {},
	"text/csv":         {},
	"application/json": {},
	// Images (vision)
	"image/png":  {},
	"image/jpeg": {},
	"image/webp": {},
	"image/gif":  {},
}

// UploadLimitsResolver looks up the effective per-user limits so the uploads
// handler can enforce plan-based caps on memory attachments. The interface
// is narrow on purpose — tests pass a hand-rolled stub, production wires
// planresolver.Resolver.
type UploadLimitsResolver interface {
	ResolveForRequest(ctx context.Context, userID, hubID string) model.UserLimits
}

type UploadsHandler struct {
	store    objectstore.Store
	resolver UploadLimitsResolver
}

func NewUploadsHandler(store objectstore.Store) *UploadsHandler {
	if store == nil {
		return &UploadsHandler{}
	}
	return &UploadsHandler{store: store}
}

// SetLimitsResolver wires in the plan-limits resolver used to enforce
// per-plan caps on memory attachments. If unset, the handler falls back to
// a conservative 5 MiB limit so no plan state in the DB never means
// "unlimited uploads."
func (h *UploadsHandler) SetLimitsResolver(r UploadLimitsResolver) {
	h.resolver = r
}

func (h *UploadsHandler) Create(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "uploads_unavailable", "Object storage is not configured")
		return
	}

	var req struct {
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
		SizeBytes   int64  `json:"size_bytes"`
		Purpose     string `json:"purpose"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}
	if strings.TrimSpace(req.Filename) == "" {
		writeError(w, http.StatusBadRequest, "missing_filename", "Filename is required")
		return
	}
	if strings.TrimSpace(req.ContentType) == "" {
		writeError(w, http.StatusBadRequest, "missing_content_type", "Content type is required")
		return
	}
	if req.SizeBytes <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_size", "size_bytes must be a positive integer")
		return
	}

	// Normalize content type: strip params like "; charset=utf-8" and lowercase.
	contentType := normalizeContentType(req.ContentType)

	// Validate purpose + per-purpose allowlist + per-purpose size cap.
	ownerID := GetUserID(r)
	maxBytes, ok := h.resolveMaxBytes(r.Context(), ownerID, req.Purpose, contentType, w)
	if !ok {
		return
	}
	if req.SizeBytes > maxBytes {
		WriteErrorWithDetails(w, http.StatusRequestEntityTooLarge, "file_too_large",
			fmt.Sprintf("File size %d exceeds the %s limit of %d bytes.", req.SizeBytes, req.Purpose, maxBytes),
			map[string]any{
				"size_bytes": req.SizeBytes,
				"max_bytes":  maxBytes,
				"purpose":    req.Purpose,
			})
		return
	}

	// Build purpose-scoped object key so sessions and attachments live under
	// distinct prefixes. This makes per-purpose retention policies and
	// bucket-level inspection trivial later.
	filename := sanitizeUploadFilename(req.Filename)
	prefix := objectKeyPrefix(req.Purpose)
	objectKey := fmt.Sprintf("owners/%s/%s/%s/%s",
		ownerID, prefix, time.Now().UTC().Format("20060102"), buildUploadName(filename))

	// Bake the client's declared size into the presigned URL so the storage
	// layer rejects any PUT with a mismatched body length. Combined with the
	// application-layer check above (req.SizeBytes <= maxBytes), this means
	// the client can't bypass the plan cap by lying on either side — the
	// claim is validated, and the claim is what the signature enforces.
	uploadURL, headers, err := h.store.PresignPutWithSize(r.Context(), objectKey, contentType, req.SizeBytes, 15*time.Minute)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "presign_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"object_key":   objectKey,
		"upload_url":   uploadURL,
		"headers":      headers,
		"filename":     filename,
		"content_type": contentType,
		"size_bytes":   req.SizeBytes,
		"purpose":      req.Purpose,
		"max_bytes":    maxBytes,
		"expires_in":   900,
	}})
}

// resolveMaxBytes validates the purpose + content type, and returns the
// effective byte cap for this upload. On validation failure it writes the
// HTTP response and returns ok=false.
func (h *UploadsHandler) resolveMaxBytes(ctx context.Context, ownerID, purpose, contentType string, w http.ResponseWriter) (int64, bool) {
	switch purpose {
	case UploadPurposeMemoryAttachment:
		if _, allowed := MemoryAttachmentContentTypes[contentType]; !allowed {
			WriteErrorWithDetails(w, http.StatusUnsupportedMediaType, "unsupported_content_type",
				fmt.Sprintf("Content type %q is not allowed for memory_attachment uploads. Supported: PDF, plain/markdown/html/csv text, JSON, PNG/JPEG/WEBP/GIF images.", contentType),
				map[string]any{"purpose": purpose, "content_type": contentType})
			return 0, false
		}
		return h.memoryAttachmentCap(ctx, ownerID, w), true

	case "":
		writeError(w, http.StatusBadRequest, "missing_purpose",
			"purpose is required: pass \"memory_attachment\" for user-picked files.")
		return 0, false

	default:
		WriteErrorWithDetails(w, http.StatusBadRequest, "invalid_purpose",
			fmt.Sprintf("Unknown purpose %q. Expected \"memory_attachment\".", purpose),
			map[string]any{"purpose": purpose})
		return 0, false
	}
}

// memoryAttachmentCap returns the per-plan byte cap for the caller. Falls
// back to the conservative free-tier cap if the resolver isn't wired or
// returns a zero/negative limit. -1 in the plan row means unlimited, but
// we still apply a hard server-side ceiling (2 GiB) so a misconfigured
// plan row can't let a client upload a 100 GB file.
const memoryAttachmentFallbackBytes int64 = 5 * 1024 * 1024           // 5 MiB
const memoryAttachmentHardCeilingBytes int64 = 2 * 1024 * 1024 * 1024 // 2 GiB

func (h *UploadsHandler) memoryAttachmentCap(ctx context.Context, ownerID string, _ http.ResponseWriter) int64 {
	if h.resolver == nil {
		return memoryAttachmentFallbackBytes
	}
	limits := h.resolver.ResolveForRequest(ctx, ownerID, "")
	return clampMemoryAttachmentCap(limits.MaxAttachmentBytes)
}

// clampMemoryAttachmentCap enforces the server-side bounds on whatever the
// plan row says. Negative means "unlimited up to the server ceiling";
// zero means "row hasn't been populated, use conservative default";
// positive values are honored up to the hard ceiling so a misconfigured
// plan row (e.g. 100 GiB) can't let a client upload beyond what the
// storage layer and our processing pipelines are sized for.
func clampMemoryAttachmentCap(raw int64) int64 {
	switch {
	case raw < 0:
		return memoryAttachmentHardCeilingBytes
	case raw == 0:
		return memoryAttachmentFallbackBytes
	case raw > memoryAttachmentHardCeilingBytes:
		return memoryAttachmentHardCeilingBytes
	default:
		return raw
	}
}

// VerifyUploadKey returns nil if objectKey has the prefix that the given
// owner's upload with the given purpose would have produced. This is the
// server-side defense for the cross-purpose bypass: without it, a caller
// could presign under one purpose (cheap cap) and then submit the key via
// a different endpoint that enforces a different cap.
//
// Accepts any key that begins with `owners/{ownerID}/memory/` — the date
// segment and random suffix are produced by the presign handler and don't
// need re-validation.
func VerifyUploadKey(objectKey, ownerID, purpose string) error {
	if strings.TrimSpace(objectKey) == "" {
		return fmt.Errorf("object_key is empty")
	}
	if strings.TrimSpace(ownerID) == "" {
		return fmt.Errorf("owner_id is empty")
	}
	expected := fmt.Sprintf("owners/%s/%s/", ownerID, objectKeyPrefix(purpose))
	if !strings.HasPrefix(objectKey, expected) {
		return fmt.Errorf("object_key does not match the %s purpose or owner; got %q, expected prefix %q", purpose, objectKey, expected)
	}
	return nil
}

// normalizeContentType drops params ("; charset=utf-8") and lowercases so
// the allowlist comparison is stable. Content-Type headers are case-
// insensitive per RFC 7231.
func normalizeContentType(ct string) string {
	ct = strings.TrimSpace(ct)
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		ct = ct[:idx]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}

// objectKeyPrefix returns the per-purpose segment used in the S3 key so
// memory attachments live under a distinct prefix. Safe for unknown purposes
// (returns "uploads") because validation runs before this ever gets called,
// but the fallback keeps us from panicking.
func objectKeyPrefix(purpose string) string {
	switch purpose {
	case UploadPurposeMemoryAttachment:
		return "memory"
	default:
		return "uploads"
	}
}

func sanitizeUploadFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	if name == "" || name == "." {
		return "upload"
	}
	return name
}

func buildUploadName(filename string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d-%s", time.Now().UnixNano(), filename)
	}
	return fmt.Sprintf("%x-%s", b, filename)
}

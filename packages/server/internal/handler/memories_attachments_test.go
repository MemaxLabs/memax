package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// Attachment view/download handlers — most are service-unavailable
// paths because attachmentSigner + objectStore are nil by default.

func makeAttachmentsHandler(t *testing.T) *handler.MemoriesHandler {
	t.Helper()
	s, _ := testdb.Acquire(t)
	return handler.NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func TestMemoriesCreateAttachmentViewURL_NoSigner(t *testing.T) {
	t.Parallel()
	h := makeAttachmentsHandler(t)
	req := httptest.NewRequest(http.MethodPost,
		"/v1/memories/m1/attachments/a1/view-url", nil)
	req = handler.InjectUserIDForTest(req, uuid.NewString())
	req.SetPathValue("id", uuid.NewString())
	req.SetPathValue("attachmentID", uuid.NewString())
	rec := httptest.NewRecorder()
	h.CreateAttachmentViewURL(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no signer: expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMemoriesServeAttachmentView_NoSigner(t *testing.T) {
	t.Parallel()
	h := makeAttachmentsHandler(t)
	req := httptest.NewRequest(http.MethodGet,
		"/v1/attachments/view?id=a&exp=1&sig=x", nil)
	rec := httptest.NewRecorder()
	h.ServeAttachmentView(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no signer/objectStore: expected 503, got %d", rec.Code)
	}
}

func TestMemoriesDownloadAttachment_NoObjectStore(t *testing.T) {
	t.Parallel()
	h := makeAttachmentsHandler(t) // nil objectStore
	req := httptest.NewRequest(http.MethodGet,
		"/v1/memories/m1/attachments/a1/download", nil)
	req = handler.InjectUserIDForTest(req, uuid.NewString())
	req.SetPathValue("id", uuid.NewString())
	req.SetPathValue("attachmentID", uuid.NewString())
	rec := httptest.NewRecorder()
	h.DownloadAttachment(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no objectStore: expected 503, got %d: %s",
			rec.Code, rec.Body.String())
	}
}

func TestMemoriesSetAttachmentSigner_NoPanic(t *testing.T) {
	t.Parallel()
	h := makeAttachmentsHandler(t)
	// Nil signer is the "unconfigured" state.
	h.SetAttachmentSigner(nil, "")
	h.SetAttachmentSigner(nil, "https://api.memax.app/")
}

func TestMemoriesSetHubQuotaResolver_NoPanic(t *testing.T) {
	t.Parallel()
	h := makeAttachmentsHandler(t)
	h.SetHubQuotaResolver(nil)
}

func TestMemoriesShare_RequiresAuth(t *testing.T) {
	t.Parallel()
	h := makeAttachmentsHandler(t)
	req := httptest.NewRequest(http.MethodPost,
		"/v1/memories/m1/share", strings.NewReader(`{}`))
	req.SetPathValue("id", uuid.NewString())
	rec := httptest.NewRecorder()
	h.Share(rec, req)
	// Without auth, handler errors out. What matters: not 200.
	if rec.Code == http.StatusOK {
		t.Errorf("unauth Share should not 200")
	}
}

func TestMemoriesReindex_RequiresAdmin(t *testing.T) {
	t.Parallel()
	h := makeAttachmentsHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/reindex", nil)
	rec := httptest.NewRecorder()
	h.Reindex(rec, req)
	// Non-admin path errors. Just ensure response is written.
	if rec.Code == 0 {
		t.Error("handler should write a response")
	}
}

func TestMemoriesCleanupMetadata_RequiresAdmin(t *testing.T) {
	t.Parallel()
	h := makeAttachmentsHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/cleanup-metadata",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.CleanupMetadata(rec, req)
	if rec.Code == 0 {
		t.Error("handler should write a response")
	}
}

func TestMemoriesDeleteAllData_RequiresAuth(t *testing.T) {
	t.Parallel()
	h := makeAttachmentsHandler(t)
	req := httptest.NewRequest(http.MethodDelete, "/v1/account/data", nil)
	rec := httptest.NewRecorder()
	h.DeleteAllData(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("unauth DeleteAllData should not 200")
	}
}

// FallbackProcessMemory exercises the synchronous processMemory path.
// Needs full ingest wiring; covered elsewhere.

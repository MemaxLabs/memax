package fileproc_test

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic/mockllm"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/fileproc"
)

// fakePDFBase64 is a valid base64 payload that is NOT actually a real
// PDF — gopdf will fail to parse, forcing the LLM-fallback branch.
// That's exactly the branch we want to test: native extraction failed,
// so we hand the bytes to Claude.
func fakePDFBase64(t *testing.T, body string) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString([]byte(body))
}

func TestExtractTextPDFFallsBackToLLMOnNativeFailure(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("# Extracted Title\n\nThis is the PDF contents.")
	p := fileproc.New(srv.Client())
	if p == nil {
		t.Fatal("expected non-nil processor")
	}

	// "not a PDF" content — native extraction errors → LLM fallback.
	content := fakePDFBase64(t, "not actually a pdf but long enough to pass base64 decode")
	text, err := p.ExtractText(content, "pdf")
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "Extracted Title") {
		t.Errorf("LLM fallback text not returned: %q", text)
	}

	// Verify the Anthropic call used CompleteMessages (multimodal) and
	// carried a document block — inspect the captured body.
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(reqs))
	}
	bodyStr := string(reqs[0].Body)
	if !strings.Contains(bodyStr, `"type":"document"`) {
		t.Errorf("PDF fallback should send a document block: %s", bodyStr[:min(400, len(bodyStr))])
	}
	if !strings.Contains(bodyStr, `"media_type":"application/pdf"`) {
		t.Error("PDF fallback should set media_type=application/pdf")
	}
}

func TestExtractTextImageCallsLLMWithImageBlock(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText("Text extracted from the image.")
	p := fileproc.New(srv.Client())

	// PNG-like base64 (starts with iVBOR) so detectImageMediaType
	// chooses image/png.
	content := "iVBORw0KGgo" + strings.Repeat("A", 100)
	text, err := p.ExtractText(content, "image")
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "Text extracted from the image.") {
		t.Errorf("LLM response not returned: %q", text)
	}
	reqs := srv.Requests()
	bodyStr := string(reqs[0].Body)
	if !strings.Contains(bodyStr, `"type":"image"`) {
		t.Error("image path should send an image block")
	}
	if !strings.Contains(bodyStr, `"media_type":"image/png"`) {
		t.Errorf("PNG signature should yield image/png, got: %s", bodyStr[:min(400, len(bodyStr))])
	}
}

func TestExtractTextImageDetectsMediaTypeFromBase64Prefix(t *testing.T) {
	t.Parallel()
	// Pin detectImageMediaType's branches end-to-end. Each prefix
	// selects a different media_type in the Anthropic payload.
	cases := map[string]string{
		"/9j/":   "image/jpeg",
		"iVBOR":  "image/png",
		"R0lGOD": "image/gif",
		"UklGR":  "image/webp",
	}
	for prefix, wantMediaType := range cases {
		t.Run(wantMediaType, func(t *testing.T) {
			t.Parallel()
			srv := mockllm.New(t)
			srv.EnqueueText("ok")
			p := fileproc.New(srv.Client())
			content := prefix + strings.Repeat("x", 50)
			if _, err := p.ExtractText(content, "image"); err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			body := string(srv.Requests()[0].Body)
			if !strings.Contains(body, `"media_type":"`+wantMediaType+`"`) {
				t.Errorf("prefix %q should yield %q, body=%s",
					prefix, wantMediaType, body[:min(400, len(body))])
			}
		})
	}
}

func TestExtractTextPDFPropagatesLLMError(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueStatus(http.StatusServiceUnavailable, "upstream down")
	p := fileproc.New(srv.Client())

	content := fakePDFBase64(t, "not a pdf")
	_, err := p.ExtractText(content, "pdf")
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error should mention upstream status, got %v", err)
	}
}

func TestExtractTextImagePropagatesLLMError(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueStatus(http.StatusTooManyRequests, "slow down")
	p := fileproc.New(srv.Client())

	if _, err := p.ExtractText("iVBOR"+strings.Repeat("A", 50), "image"); err == nil {
		t.Fatal("expected error")
	}
}

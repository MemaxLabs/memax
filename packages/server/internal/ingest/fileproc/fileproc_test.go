package fileproc

import (
	"context"
	"strings"
	"testing"
)

func TestNewRejectsNilClient(t *testing.T) {
	t.Parallel()
	if p := New(nil); p != nil {
		t.Errorf("nil client should produce nil processor, got %+v", p)
	}
}

func TestExtractTextRejectsOversize(t *testing.T) {
	t.Parallel()
	// maxBase64Size = 10MB. Anything above that short-circuits
	// before touching the LLM client. Use a large string padded
	// with 'x'.
	big := strings.Repeat("x", maxBase64Size+1)
	p := &Processor{} // no client — oversize path doesn't call it
	_, err := p.ExtractText(big, "pdf")
	if err == nil {
		t.Error("oversize content should error")
	}
	if !strings.Contains(err.Error(), "file too large") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtractTextRejectsUnsupportedContentType(t *testing.T) {
	t.Parallel()
	p := &Processor{}
	_, err := p.ExtractText("c29tZS1kYXRh", "video") // arbitrary base64, unsupported type
	if err == nil {
		t.Error("unsupported content type should error")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtractTextContextAcceptsContext(t *testing.T) {
	t.Parallel()
	// Signature guard — makes sure the context-flavored entrypoint
	// still exists and surfaces the same "unsupported type" error
	// as the background-ctx version.
	p := &Processor{}
	_, err := p.ExtractTextContext(context.Background(), "data", "application/zip")
	if err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestExtractPDFRejectsInvalidBase64(t *testing.T) {
	t.Parallel()
	// Non-base64 input → base64.DecodeString errors before we
	// attempt native extraction or the LLM fallback.
	p := &Processor{}
	_, err := p.ExtractText("not valid base64!!!", "pdf")
	if err == nil {
		t.Error("invalid base64 should error")
	}
}

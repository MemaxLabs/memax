package extract

import (
	"context"
	"testing"
)

// Pure tests: the Extractor has a nil-client early-return path
// (New returns nil) and a short-content early-return path (len <
// 100). These are deterministic and don't need to mock Anthropic.

func TestNewRejectsNilClient(t *testing.T) {
	t.Parallel()
	if e := New(nil); e != nil {
		t.Errorf("nil anthropic client should produce nil extractor, got %+v", e)
	}
}

func TestExtractShortContentReturnsNil(t *testing.T) {
	t.Parallel()
	// Short content bypasses the LLM — a bare extractor (no
	// client) should still return nil for short inputs rather
	// than panicking. This is the cheap-path guard the ingest
	// pipeline relies on.
	e := &Extractor{} // no client
	out := e.Extract("short", "hint")
	if out != nil {
		t.Errorf("short content should return nil, got %+v", out)
	}
}

func TestExtractShortBorderlineContent(t *testing.T) {
	t.Parallel()
	// Exactly 99 chars → below threshold → nil.
	short := ""
	for i := 0; i < 99; i++ {
		short += "x"
	}
	e := &Extractor{}
	if got := e.Extract(short, ""); got != nil {
		t.Errorf("99-char content should short-circuit, got %+v", got)
	}
}

func TestExtractContextRespectsContext(t *testing.T) {
	t.Parallel()
	// Short content path doesn't actually use the context, but
	// the signature change guard against a future refactor that
	// removes the ctx parameter: confirm the context-flavored
	// entrypoint compiles and returns nil for short input.
	e := &Extractor{}
	out := e.ExtractContext(context.Background(), "short", "hint")
	if out != nil {
		t.Errorf("short content via context path should return nil, got %+v", out)
	}
}

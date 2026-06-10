package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// captureHandler parses a slog JSON record to a map for assertions.
func captureHandler(t *testing.T) (slog.Handler, func() map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, nil)
	decode := func() map[string]any {
		var m map[string]any
		if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
			t.Fatalf("decode: %v (raw=%q)", err, buf.String())
		}
		buf.Reset()
		return m
	}
	return h, decode
}

func TestCtxAttrsHandler_InjectsStashedAttrs(t *testing.T) {
	inner, decode := captureHandler(t)
	logger := slog.New(NewCtxAttrsHandler(inner))

	ctx := WithAttrs(context.Background(),
		slog.Int64("job_id", 42),
		slog.String("job_kind", "dream_cycle"),
	)

	logger.InfoContext(ctx, "working")

	got := decode()
	if got["job_id"].(float64) != 42 {
		t.Errorf("job_id missing/wrong: %v", got)
	}
	if got["job_kind"] != "dream_cycle" {
		t.Errorf("job_kind missing/wrong: %v", got)
	}
	if got["msg"] != "working" {
		t.Errorf("msg missing/wrong: %v", got)
	}
}

func TestCtxAttrsHandler_NoAttrsWhenCtxEmpty(t *testing.T) {
	inner, decode := captureHandler(t)
	logger := slog.New(NewCtxAttrsHandler(inner))

	logger.InfoContext(context.Background(), "no enrichment")

	got := decode()
	if _, present := got["job_id"]; present {
		t.Errorf("expected no job_id: %v", got)
	}
	if got["msg"] != "no enrichment" {
		t.Errorf("msg wrong: %v", got)
	}
}

func TestWithAttrs_Accumulates(t *testing.T) {
	inner, decode := captureHandler(t)
	logger := slog.New(NewCtxAttrsHandler(inner))

	ctx := WithAttrs(context.Background(), slog.String("a", "1"))
	ctx = WithAttrs(ctx, slog.String("b", "2"))

	logger.InfoContext(ctx, "merged")

	got := decode()
	if got["a"] != "1" || got["b"] != "2" {
		t.Errorf("expected both a+b, got %v", got)
	}
}

func TestWithAttrs_PreservesUnderlyingCtx(t *testing.T) {
	// Context cancellation/values should pass through unchanged —
	// WithAttrs must not break ctx chains.
	type k struct{}
	parent := context.WithValue(context.Background(), k{}, "hello")
	enriched := WithAttrs(parent, slog.Int("x", 1))

	if got := enriched.Value(k{}); got != "hello" {
		t.Errorf("parent value lost: %v", got)
	}
}

package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── TeeHandler ──────────────────────────────────────────────────

func TestNewTeeHandlerFiltersNil(t *testing.T) {
	t.Parallel()

	// Nil entries in the handler list are stripped at construction
	// — callers pass `nil` when a handler can't be built (missing
	// config) and NewTeeHandler should tolerate that without a
	// nil-pointer panic during Enabled/Handle.
	tee := NewTeeHandler(nil, nil, nil)
	if got := tee.Enabled(context.Background(), slog.LevelInfo); got {
		t.Error("all-nil tee should report Enabled=false")
	}
}

func TestTeeHandlerEnabledShortCircuits(t *testing.T) {
	t.Parallel()

	// Enabled is an OR across the handler set. One-yes is enough.
	yes := &fakeHandler{enabledLevel: slog.LevelDebug}
	no := &fakeHandler{enabledLevel: slog.LevelError}
	tee := NewTeeHandler(yes, no)
	if !tee.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("tee should be enabled when any child is enabled")
	}
	tee = NewTeeHandler(no, no)
	if tee.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("tee should be disabled when no child is enabled")
	}
}

func TestTeeHandlerHandleFansOut(t *testing.T) {
	t.Parallel()

	// Handle emits the same record to every child handler whose
	// Enabled() allows the level. Children that reject the level
	// via Enabled() are skipped — emitted records MUST NOT fire
	// their Handle().
	accepting := &fakeHandler{enabledLevel: slog.LevelDebug}
	rejecting := &fakeHandler{enabledLevel: slog.LevelError}
	tee := NewTeeHandler(accepting, rejecting)
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	if err := tee.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if accepting.handleCalls != 1 {
		t.Errorf("accepting handler: got %d calls, want 1", accepting.handleCalls)
	}
	if rejecting.handleCalls != 0 {
		t.Errorf("rejecting handler: got %d calls, want 0 (level below threshold)", rejecting.handleCalls)
	}
}

func TestTeeHandlerHandleJoinsErrors(t *testing.T) {
	t.Parallel()

	// When multiple children return errors, Handle joins them so
	// callers see every failure — losing one would silence real
	// outage signals in pipelines that tee stdout + remote sinks.
	e1 := errors.New("sink a failed")
	e2 := errors.New("sink b failed")
	h1 := &fakeHandler{enabledLevel: slog.LevelDebug, handleErr: e1}
	h2 := &fakeHandler{enabledLevel: slog.LevelDebug, handleErr: e2}
	tee := NewTeeHandler(h1, h2)

	err := tee.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "x", 0))
	if err == nil {
		t.Fatal("expected joined error, got nil")
	}
	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Errorf("joined error missing a child: %v", err)
	}
}

func TestTeeHandlerWithAttrsPreservesFanOut(t *testing.T) {
	t.Parallel()

	// WithAttrs must return a TeeHandler whose children are all
	// the originals' WithAttrs views. Otherwise attrs get dropped
	// on one sink but not another — a classic tracing-breakage
	// pattern.
	a := &fakeHandler{enabledLevel: slog.LevelDebug}
	b := &fakeHandler{enabledLevel: slog.LevelDebug}
	tee := NewTeeHandler(a, b)
	decorated, ok := tee.WithAttrs([]slog.Attr{slog.String("req_id", "abc")}).(*teeHandler)
	if !ok {
		t.Fatal("WithAttrs should return *teeHandler")
	}
	if len(decorated.handlers) != 2 {
		t.Errorf("decorated tee lost children: got %d, want 2", len(decorated.handlers))
	}
	if a.withAttrsCalls != 1 || b.withAttrsCalls != 1 {
		t.Errorf("withAttrs fanout mismatch: a=%d b=%d", a.withAttrsCalls, b.withAttrsCalls)
	}
}

func TestTeeHandlerWithGroupPreservesFanOut(t *testing.T) {
	t.Parallel()

	a := &fakeHandler{enabledLevel: slog.LevelDebug}
	b := &fakeHandler{enabledLevel: slog.LevelDebug}
	tee := NewTeeHandler(a, b)
	decorated, ok := tee.WithGroup("request").(*teeHandler)
	if !ok {
		t.Fatal("WithGroup should return *teeHandler")
	}
	if len(decorated.handlers) != 2 {
		t.Errorf("got %d children, want 2", len(decorated.handlers))
	}
	if a.withGroupCalls != 1 || b.withGroupCalls != 1 {
		t.Errorf("withGroup fanout mismatch: a=%d b=%d", a.withGroupCalls, b.withGroupCalls)
	}
}

// ─── NewLogger / SetupLogger ────────────────────────────────────

func TestNewLoggerWithoutLokiReturnsStdoutOnly(t *testing.T) {
	// No t.Parallel() — t.Setenv is incompatible with it, and
	// the env state matters for SetupLoki's env-driven config
	// detection.

	// With no Loki env vars set, SetupLogger should return a
	// stdout-only logger with Enabled=false and a no-op Shutdown.
	// Any other behavior risks leaking a background Loki shipper
	// in tests that expect zero side-effects.
	t.Setenv("LOKI_ENDPOINT", "")

	setup, err := SetupLogger("memax-test")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if setup.Enabled {
		t.Error("Enabled should be false when no Loki endpoint configured")
	}
	if setup.Logger == nil {
		t.Fatal("Logger should be non-nil even without Loki")
	}
	if err := setup.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown should be a no-op when no Loki: got %v", err)
	}

	// Smoke: logger accepts a call without panicking.
	setup.Logger.Info("smoke test", "k", "v")

	// NewLogger wraps SetupLogger; confirm same shape.
	logger, shutdown, err := NewLogger("memax-test")
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	if logger == nil {
		t.Fatal("NewLogger returned nil logger")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("NewLogger shutdown should no-op without Loki: %v", err)
	}
}

// ─── Fake handler for tracking calls ────────────────────────────

type fakeHandler struct {
	mu             sync.Mutex
	enabledLevel   slog.Level
	handleCalls    int
	handleErr      error
	withAttrsCalls int
	withGroupCalls int
	records        []slog.Record
}

func (f *fakeHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= f.enabledLevel
}
func (f *fakeHandler) Handle(_ context.Context, r slog.Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handleCalls++
	f.records = append(f.records, r)
	return f.handleErr
}
func (f *fakeHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.withAttrsCalls++
	return f
}
func (f *fakeHandler) WithGroup(_ string) slog.Handler {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.withGroupCalls++
	return f
}

// Smoke the actual slog + TeeHandler integration: write through
// the tee and verify the stdout child produces JSON.
func TestTeeHandlerProducesJSONViaRealStdoutChild(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	stdoutLike := slog.NewJSONHandler(&buf, nil)
	tee := NewTeeHandler(stdoutLike)
	logger := slog.New(tee)
	logger.Info("hello", "user_id", "u1")

	line := buf.String()
	if !strings.Contains(line, `"msg":"hello"`) || !strings.Contains(line, `"user_id":"u1"`) {
		t.Errorf("tee-wrapped JSON handler produced unexpected output: %q", line)
	}
}

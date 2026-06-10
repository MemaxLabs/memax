package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/observability"
)

// captureLogger returns (logger, readLines) where readLines returns
// each JSON record as a map, most-recent-first-in-order.
func captureLogger(t *testing.T) (*slog.Logger, func() []map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	h := observability.NewCtxAttrsHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(h)
	read := func() []map[string]any {
		var out []map[string]any
		for line := range bytes.SplitSeq(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n")) {
			if len(line) == 0 {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal(line, &m); err != nil {
				t.Fatalf("decode: %v (raw=%q)", err, line)
			}
			out = append(out, m)
		}
		return out
	}
	return logger, read
}

func TestLoggerMiddleware_WrapsJobContextAndLogsLifecycle(t *testing.T) {
	logger, read := captureLogger(t)
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	mw := NewLoggerMiddleware()
	job := &rivertype.JobRow{
		ID:      12345,
		Kind:    "memory_process",
		Queue:   "default",
		Attempt: 2,
	}

	// Inner function simulates a worker emitting a mid-job log.
	inner := func(ctx context.Context) error {
		slog.InfoContext(ctx, "halfway there", "memory_id", "abc-123")
		return nil
	}

	if err := mw.Work(context.Background(), job, inner); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	lines := read()
	if len(lines) != 3 {
		t.Fatalf("expected 3 log lines (started/mid/completed), got %d: %v", len(lines), lines)
	}

	for i, line := range lines {
		if line["job_id"].(float64) != 12345 {
			t.Errorf("line %d missing job_id=12345: %v", i, line)
		}
		if line["job_kind"] != "memory_process" {
			t.Errorf("line %d missing job_kind: %v", i, line)
		}
		if line["job_queue"] != "default" {
			t.Errorf("line %d missing job_queue: %v", i, line)
		}
		if line["job_attempt"].(float64) != 2 {
			t.Errorf("line %d missing job_attempt: %v", i, line)
		}
	}

	if lines[0]["msg"] != "job started" {
		t.Errorf("expected 'job started', got %v", lines[0]["msg"])
	}
	if lines[1]["msg"] != "halfway there" || lines[1]["memory_id"] != "abc-123" {
		t.Errorf("mid-job line wrong: %v", lines[1])
	}
	if lines[2]["msg"] != "job completed" {
		t.Errorf("expected 'job completed', got %v", lines[2]["msg"])
	}
}

func TestLoggerMiddleware_ReportsError(t *testing.T) {
	logger, read := captureLogger(t)
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	mw := NewLoggerMiddleware()
	job := &rivertype.JobRow{ID: 7, Kind: "send_email", Queue: "default", Attempt: 1}

	wantErr := errors.New("smtp blew up")
	err := mw.Work(context.Background(), job, func(context.Context) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("middleware must propagate inner error, got %v", err)
	}

	lines := read()
	if len(lines) != 2 {
		t.Fatalf("expected started + failed (2), got %d", len(lines))
	}
	if lines[1]["msg"] != "job failed" {
		t.Errorf("expected 'job failed', got %v", lines[1]["msg"])
	}
	if !strings.Contains(lines[1]["error"].(string), "smtp blew up") {
		t.Errorf("error missing from failure log: %v", lines[1])
	}
}

func TestLoggerMiddleware_ElapsedIsPositive(t *testing.T) {
	logger, read := captureLogger(t)
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	mw := NewLoggerMiddleware()
	job := &rivertype.JobRow{ID: 1, Kind: "x", Queue: "default", Attempt: 1}
	_ = mw.Work(context.Background(), job, func(context.Context) error {
		time.Sleep(2 * time.Millisecond)
		return nil
	})

	lines := read()
	completed := lines[len(lines)-1]
	// slog encodes Duration as nanoseconds number.
	elapsedNS, ok := completed["elapsed"].(float64)
	if !ok || elapsedNS <= 0 {
		t.Errorf("elapsed missing/zero: %v", completed)
	}
}

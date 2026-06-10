package observability

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

func TestSetupLokiDisabledWithoutEnv(t *testing.T) {
	t.Setenv("LOKI_URL", "")
	t.Setenv("LOKI_USERNAME", "")
	t.Setenv("LOKI_PASSWORD", "")

	setup, err := SetupLoki("memax-api")
	if err != nil {
		t.Fatalf("SetupLoki() error = %v", err)
	}
	if setup.Enabled {
		t.Fatalf("expected disabled setup")
	}
}

func TestSetupLokiRejectsPartialConfig(t *testing.T) {
	t.Setenv("LOKI_URL", "https://logs-prod-021.grafana.net")
	t.Setenv("LOKI_USERNAME", "")
	t.Setenv("LOKI_PASSWORD", "")

	if _, err := SetupLoki("memax-api"); err == nil {
		t.Fatalf("expected error for partial config")
	}
}

func TestLokiHandlerPushesStructuredLogs(t *testing.T) {
	var payload []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		payload, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("LOKI_URL", server.URL)
	t.Setenv("LOKI_USERNAME", "user")
	t.Setenv("LOKI_PASSWORD", "pass")
	t.Setenv("LOKI_QUEUE_SIZE", "10")
	t.Setenv("LOKI_BATCH_MAX_RECORDS", "1")
	t.Setenv("LOKI_BATCH_INTERVAL_MS", "10")
	t.Setenv("LOKI_REQUEST_TIMEOUT_MS", "500")
	t.Setenv("MEMAX_ENV", "test")
	t.Setenv("FLY_REGION", "iad")
	t.Setenv("FLY_MACHINE_ID", "machine-123")
	t.Setenv("FLY_APP_NAME", "memax-api-staging")

	setup, err := SetupLoki("memax-api")
	if err != nil {
		t.Fatalf("SetupLoki() error = %v", err)
	}

	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    [16]byte{1, 2, 3},
		SpanID:     [8]byte{4, 5, 6},
		TraceFlags: trace.FlagsSampled,
	}))
	logger := slog.New(setup.Handler)
	logger.InfoContext(ctx, "hello", "memory_id", "abc123")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := setup.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	var req lokiPushRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(req.Streams) != 1 {
		t.Fatalf("expected one stream, got %d", len(req.Streams))
	}
	if got := req.Streams[0].Stream["service"]; got != "memax-api" {
		t.Fatalf("service label = %q", got)
	}
	if got := req.Streams[0].Stream["environment"]; got != "test" {
		t.Fatalf("environment label = %q", got)
	}
	if got := req.Streams[0].Stream["fly_region"]; got != "iad" {
		t.Fatalf("fly_region label = %q", got)
	}
	if len(req.Streams[0].Values) != 1 {
		t.Fatalf("expected one value, got %d", len(req.Streams[0].Values))
	}

	var line map[string]any
	if err := json.Unmarshal([]byte(req.Streams[0].Values[0][1]), &line); err != nil {
		t.Fatalf("unmarshal line: %v", err)
	}
	if got := line["message"]; got != "hello" {
		t.Fatalf("message = %#v", got)
	}
	if got := line["memory_id"]; got != "abc123" {
		t.Fatalf("memory_id = %#v", got)
	}
	if got := line["fly.region"]; got != "iad" {
		t.Fatalf("fly.region = %#v", got)
	}
	if got := line["fly.machine.id"]; got != "machine-123" {
		t.Fatalf("fly.machine.id = %#v", got)
	}
	if got := line["fly.app.name"]; got != "memax-api-staging" {
		t.Fatalf("fly.app.name = %#v", got)
	}
	if _, ok := line["host.name"]; !ok {
		t.Fatalf("expected host.name in line")
	}
	if _, ok := line["process.pid"]; !ok {
		t.Fatalf("expected process.pid in line")
	}
	if _, ok := line["trace_id"]; !ok {
		t.Fatalf("expected trace_id in line")
	}
}

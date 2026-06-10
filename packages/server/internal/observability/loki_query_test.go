package observability

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, server *httptest.Server) *QueryClient {
	t.Helper()
	return &QueryClient{
		baseURL:    server.URL,
		username:   "user",
		password:   "pass",
		env:        "staging",
		httpClient: server.Client(),
	}
}

func TestQueryJobLogs_Success(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture and assert the LogQL query is server-pinned.
		capturedQuery = r.URL.Query().Get("query")
		if !strings.Contains(capturedQuery, `environment="staging"`) {
			t.Errorf("env filter missing/wrong: %s", capturedQuery)
		}
		if !strings.Contains(capturedQuery, `job_id="42"`) {
			t.Errorf("job_id filter missing/wrong: %s", capturedQuery)
		}
		if !strings.Contains(capturedQuery, `service=~"memax-.*"`) {
			t.Errorf("service filter missing/wrong: %s", capturedQuery)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("missing basic auth")
		}
		fmt.Fprintln(w, `{
			"status":"success",
			"data":{
				"resultType":"streams",
				"result":[
					{
						"stream":{"service":"memax-worker"},
						"values":[
							["1713800000000000000","{\"time\":\"2026-04-22T00:00:00Z\",\"level\":\"INFO\",\"msg\":\"job started\",\"job_id\":42}"],
							["1713800000500000000","{\"level\":\"INFO\",\"msg\":\"halfway\",\"job_id\":42,\"memory_id\":\"abc\"}"]
						]
					}
				]
			}
		}`)
	}))
	defer server.Close()

	c := newTestClient(t, server)
	lines, err := c.QueryJobLogs(context.Background(), 42, QueryJobLogsOptions{
		Since: time.Unix(1713799000, 0),
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %+v", len(lines), lines)
	}
	if lines[0].Message != "job started" || lines[0].Level != "INFO" {
		t.Errorf("first line wrong: %+v", lines[0])
	}
	if lines[1].Fields["memory_id"] != "abc" {
		t.Errorf("memory_id missing from fields: %+v", lines[1])
	}
	if _, present := lines[0].Fields["time"]; present {
		t.Error("slog time should be stripped; Loki ingest ts is authoritative")
	}
}

// Real production log bodies come from the Loki handler in loki.go,
// which writes `message`/`level`/`time` (not slog-default `msg`).
// This test locks in parsing of exporter-shaped JSON so a regression
// on decodeLogLine can't silently blank every production panel row.
func TestQueryJobLogs_ExporterFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{
			"status":"success",
			"data":{
				"resultType":"streams",
				"result":[
					{
						"stream":{"service":"memax-worker"},
						"values":[
							["1713800000000000000","{\"time\":\"2026-04-22T00:00:00Z\",\"level\":\"INFO\",\"message\":\"job started\",\"service\":\"memax-worker\",\"environment\":\"production\",\"job_id\":42,\"job_kind\":\"memory_process\"}"]
						]
					}
				]
			}
		}`)
	}))
	defer server.Close()

	c := newTestClient(t, server)
	lines, err := c.QueryJobLogs(context.Background(), 42, QueryJobLogsOptions{
		Since: time.Unix(1713799000, 0),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0].Message != "job started" {
		t.Errorf("message should be parsed from `message` key: %+v", lines[0])
	}
	if lines[0].Level != "INFO" {
		t.Errorf("level missing: %+v", lines[0])
	}
	// Reserved keys should be stripped; domain attrs should survive.
	for _, reserved := range []string{"message", "msg", "level", "time"} {
		if _, present := lines[0].Fields[reserved]; present {
			t.Errorf("reserved key %q leaked into fields: %+v", reserved, lines[0].Fields)
		}
	}
	if lines[0].Fields["job_id"] == nil {
		t.Errorf("job_id should appear in fields: %+v", lines[0].Fields)
	}
}

func TestQueryJobLogs_NonJSONFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{
			"status":"success",
			"data":{
				"resultType":"streams",
				"result":[
					{
						"stream":{"service":"memax-worker"},
						"values":[["1713800000000000000","plain-text log from some subprocess"]]
					}
				]
			}
		}`)
	}))
	defer server.Close()

	c := newTestClient(t, server)
	lines, err := c.QueryJobLogs(context.Background(), 1, QueryJobLogsOptions{
		Since: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("want 1, got %d", len(lines))
	}
	if lines[0].Message != "plain-text log from some subprocess" {
		t.Errorf("fallback didn't preserve body: %+v", lines[0])
	}
}

func TestQueryJobLogs_EnvIsolation(t *testing.T) {
	// The key safety invariant: caller CANNOT influence env.
	// QueryJobLogs takes no env parameter. We verify by inspecting
	// what goes out the wire from two separate clients, one per env.
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Query().Get("query"))
		fmt.Fprintln(w, `{"status":"success","data":{"resultType":"streams","result":[]}}`)
	}))
	defer server.Close()

	stagingClient := &QueryClient{baseURL: server.URL, username: "u", password: "p", env: "staging", httpClient: server.Client()}
	prodClient := &QueryClient{baseURL: server.URL, username: "u", password: "p", env: "production", httpClient: server.Client()}

	_, _ = stagingClient.QueryJobLogs(context.Background(), 1, QueryJobLogsOptions{Since: time.Now().Add(-time.Hour)})
	_, _ = prodClient.QueryJobLogs(context.Background(), 1, QueryJobLogsOptions{Since: time.Now().Add(-time.Hour)})

	if !strings.Contains(got[0], `environment="staging"`) {
		t.Errorf("staging client leaked non-staging env: %s", got[0])
	}
	if !strings.Contains(got[1], `environment="production"`) {
		t.Errorf("prod client leaked non-prod env: %s", got[1])
	}
	if strings.Contains(got[0], "production") {
		t.Errorf("staging query must NOT mention production: %s", got[0])
	}
	if strings.Contains(got[1], "staging") {
		t.Errorf("prod query must NOT mention staging: %s", got[1])
	}
}

func TestQueryJobLogs_PropagatesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "upstream error")
	}))
	defer server.Close()

	c := newTestClient(t, server)
	_, err := c.QueryJobLogs(context.Background(), 1, QueryJobLogsOptions{Since: time.Now().Add(-time.Hour)})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("expected 502 error, got %v", err)
	}
}

func TestQueryJobLogs_StripsPushSuffixFromBaseURL(t *testing.T) {
	t.Setenv("LOKI_URL", "https://logs-prod-021.grafana.net/loki/api/v1/push")
	t.Setenv("LOKI_USERNAME", "u")
	t.Setenv("LOKI_PASSWORD", "p")
	t.Setenv("MEMAX_ENV", "staging")

	c := newQueryClientFromEnv(defaultQueryHTTPClient())
	if c == nil {
		t.Fatal("expected client, got nil")
	}
	if c.baseURL != "https://logs-prod-021.grafana.net" {
		t.Errorf("baseURL should strip /push suffix, got %q", c.baseURL)
	}
	if c.env != "staging" {
		t.Errorf("env should come from MEMAX_ENV, got %q", c.env)
	}
}

func TestQueryJobLogs_NilWhenUnconfigured(t *testing.T) {
	t.Setenv("LOKI_URL", "")
	t.Setenv("LOKI_USERNAME", "")
	t.Setenv("LOKI_PASSWORD", "")

	if c := newQueryClientFromEnv(defaultQueryHTTPClient()); c != nil {
		t.Errorf("expected nil when LOKI_* unset, got %+v", c)
	}
}

func TestQueryJobLogs_RequiresSince(t *testing.T) {
	c := &QueryClient{baseURL: "https://x", env: "dev"}
	_, err := c.QueryJobLogs(context.Background(), 1, QueryJobLogsOptions{})
	if err == nil || !strings.Contains(err.Error(), "Since is required") {
		t.Errorf("expected Since-required error, got %v", err)
	}
}

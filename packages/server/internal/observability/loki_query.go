package observability

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// QueryClient reads logs from Grafana Cloud Loki. Separate from the
// lokiExporter in loki.go which only pushes — query and push share
// the same host + credentials but hit different paths.
//
// Env isolation: at construction time we capture MEMAX_ENV and bake
// it into every query as a label filter. The caller cannot override
// this. Staging admin dashboard → staging Loki logs only. Prod →
// prod only. This is a structural guarantee, not a convention.
//
// Cardinality: we intentionally filter the job_id as a *parsed field*
// (`| json | job_id=...`), not a label. Loki's pricing is label-
// cardinality-sensitive, and job IDs are unbounded — making job_id
// a label would be a pricing disaster at scale. Parsed fields cost
// query compute only, and only for the filtered time range.
type QueryClient struct {
	baseURL    string
	username   string
	password   string
	env        string // frozen at construction — never sourced from request input
	httpClient *http.Client
	logger     *slog.Logger
}

// LogLine is one structured log record returned from Loki. Timestamp
// is Loki's ingest timestamp; Level and Message come from slog's
// JSON body. Fields is everything else (job_id, memory_id, duration,
// caller-supplied attrs).
type LogLine struct {
	Timestamp time.Time      `json:"timestamp"`
	Level     string         `json:"level"` // "INFO" / "WARN" / "ERROR" / "DEBUG"
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// NewQueryClient builds a client from LOKI_URL, LOKI_USERNAME, and
// LOKI_PASSWORD. Returns nil when any required var is missing — the
// admin logs endpoint treats nil as "feature disabled" and returns
// a friendly 503 / clear empty state, rather than crashing.
func NewQueryClient() *QueryClient {
	return newQueryClientFromEnv(defaultQueryHTTPClient())
}

func newQueryClientFromEnv(httpClient *http.Client) *QueryClient {
	endpoint := strings.TrimSpace(os.Getenv("LOKI_URL"))
	username := strings.TrimSpace(os.Getenv("LOKI_USERNAME"))
	password := strings.TrimSpace(os.Getenv("LOKI_PASSWORD"))
	if endpoint == "" || username == "" || password == "" {
		return nil
	}
	// LOKI_URL is a base. Pusher appends `/loki/api/v1/push`; we
	// append `/loki/api/v1/query_range`. Defensively strip the push
	// suffix if someone set LOKI_URL to the push URL directly.
	endpoint = strings.TrimRight(endpoint, "/")
	endpoint = strings.TrimSuffix(endpoint, "/loki/api/v1/push")

	return &QueryClient{
		baseURL:    endpoint,
		username:   username,
		password:   password,
		env:        Env(),
		httpClient: httpClient,
		logger:     slog.Default(),
	}
}

func defaultQueryHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// Env returns the deployment environment string this client is
// pinned to. Handlers can surface this in responses so the UI can
// display "Showing production logs" without trusting client-side
// state.
func (c *QueryClient) QueryEnv() string { return c.env }

// QueryJobLogsOptions tunes the LogQL query. Caller never gets to
// influence the service label or the env label — both are fixed.
type QueryJobLogsOptions struct {
	// Since is the oldest log timestamp to return. Must be non-zero.
	Since time.Time
	// Until is the newest log timestamp to return. Zero means "now".
	Until time.Time
	// Limit is the max number of lines returned. Loki caps at 5000.
	// Zero means "use default" (500).
	Limit int
	// Direction: "forward" (oldest first) or "backward" (newest
	// first). Zero defaults to "forward", suitable for chronological
	// display and polling-based tail.
	Direction string
}

// QueryJobLogs returns logs emitted by any memax service for the
// given River job id within the requested time range.
func (c *QueryClient) QueryJobLogs(ctx context.Context, jobID int64, opts QueryJobLogsOptions) ([]LogLine, error) {
	if c == nil {
		return nil, fmt.Errorf("loki query client not configured")
	}
	if opts.Since.IsZero() {
		return nil, fmt.Errorf("QueryJobLogsOptions.Since is required")
	}
	until := opts.Until
	if until.IsZero() {
		until = time.Now()
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}
	direction := opts.Direction
	if direction == "" {
		direction = "forward"
	}

	// Both label selectors AND the field filter are server-pinned.
	// jobID comes through strconv, not fmt.Sprintf, so there's no
	// path for injection even if a hypothetical caller passed a
	// crafted string — the int64 type enforces it.
	query := fmt.Sprintf(
		`{service=~"memax-.*",environment=%q} | json | job_id=%q`,
		c.env,
		strconv.FormatInt(jobID, 10),
	)

	return c.run(ctx, query, opts.Since, until, limit, direction)
}

// lokiQueryResponse mirrors Loki's `/query_range` response shape.
// `resultType` is always "streams" for a log query.
type lokiQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string `json:"stream"`
			// Values are [timestamp_ns_string, log_body_string]
			// tuples. Using [][]string keeps decoding cheap — we
			// parse nanoseconds and body separately.
			Values [][]string `json:"values"`
		} `json:"result"`
	} `json:"data"`
	// Present on error responses.
	ErrorType string `json:"errorType,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (c *QueryClient) run(ctx context.Context, logql string, since, until time.Time, limit int, direction string) ([]LogLine, error) {
	u, err := url.Parse(c.baseURL + "/loki/api/v1/query_range")
	if err != nil {
		return nil, fmt.Errorf("parse loki url: %w", err)
	}
	q := u.Query()
	q.Set("query", logql)
	q.Set("start", strconv.FormatInt(since.UnixNano(), 10))
	q.Set("end", strconv.FormatInt(until.UnixNano(), 10))
	q.Set("limit", strconv.Itoa(limit))
	q.Set("direction", direction)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	auth := base64.StdEncoding.EncodeToString([]byte(c.username + ":" + c.password))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("loki request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024)) // 16 MiB safety cap
	if err != nil {
		return nil, fmt.Errorf("read loki response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Truncate the upstream body so we don't flood our own logs
		// when Loki returns an HTML challenge page or a huge stack.
		snippet := body
		if len(snippet) > 512 {
			snippet = snippet[:512]
		}
		return nil, fmt.Errorf("loki status %d: %s", resp.StatusCode, snippet)
	}

	var parsed lokiQueryResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode loki response: %w", err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("loki %s: %s", parsed.ErrorType, parsed.Error)
	}

	lines := make([]LogLine, 0, limit)
	for _, stream := range parsed.Data.Result {
		for _, entry := range stream.Values {
			if len(entry) != 2 {
				continue
			}
			ts, err := parseLokiTimestamp(entry[0])
			if err != nil {
				// Keep going — one malformed line shouldn't eat
				// the rest of a legitimate page.
				continue
			}
			lines = append(lines, decodeLogLine(ts, entry[1]))
		}
	}

	// Loki returns entries already sorted by stream+ts but streams
	// may interleave. Sort globally to guarantee chronological order
	// regardless of direction.
	sort.Slice(lines, func(i, j int) bool {
		return lines[i].Timestamp.Before(lines[j].Timestamp)
	})
	if direction == "backward" {
		// Caller asked newest-first. Reverse after the unified sort.
		for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
	}

	return lines, nil
}

func parseLokiTimestamp(s string) (time.Time, error) {
	ns, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, ns), nil
}

// decodeLogLine parses a slog-produced JSON body into a LogLine. If
// the body isn't JSON (e.g. third-party process logs), we fall back
// to a message-only record so the line still renders.
//
// We accept both `message` and `msg` as the message key: the Loki
// handler in loki.go writes `message`, the default slog JSONHandler
// on stdout writes `msg`. Real production traffic comes from the
// Loki handler so `message` is the common path, but tests and dev
// stdout tailing can still produce `msg`-keyed bodies.
func decodeLogLine(ts time.Time, body string) LogLine {
	line := LogLine{Timestamp: ts}
	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		line.Message = body
		return line
	}

	if v, ok := raw["message"].(string); ok {
		line.Message = v
	} else if v, ok := raw["msg"].(string); ok {
		line.Message = v
	}
	if v, ok := raw["level"].(string); ok {
		line.Level = v
	}

	// slog-owned fields go into the top-level struct; everything
	// else becomes Fields. Drop both message keys so the field map
	// doesn't carry a duplicate of whatever the top-level Message
	// already holds; drop `time` because the Loki ingest timestamp
	// is authoritative (the slog time can drift when the push
	// batches).
	for _, reserved := range []string{"message", "msg", "level", "time"} {
		delete(raw, reserved)
	}
	if len(raw) > 0 {
		line.Fields = raw
	}
	return line
}

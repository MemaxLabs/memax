package compile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Compiler compiles and parses back: the compile service over HTTP
// (Client), or an in-process fake in tests (compiletest.Fake).
type Compiler interface {
	Compile(ctx context.Context, in *Input) (*Result, error)
	ParseBack(ctx context.Context, req *ParseBackRequest) (*ParseBackResult, error)
}

// ErrUnavailable is returned when the compile service didn't answer
// after every attempt (it is down, restarting or unreachable). It is
// transient: a compile job retries, and an API call can be retried.
var ErrUnavailable = errors.New("compile: the compile service is unavailable")

// InputError is a compile input the compiler refused (HTTP 422). It is
// permanent: compiling the same input again fails the same way.
type InputError struct {
	Message string
	Issues  []Issue
}

func (e *InputError) Error() string {
	if len(e.Issues) == 0 {
		return "compile: input refused: " + e.Message
	}
	more := ""
	if len(e.Issues) > 1 {
		more = fmt.Sprintf(" (and %d more)", len(e.Issues)-1)
	}
	return fmt.Sprintf("compile: input refused: %s: %s%s", e.Issues[0].InstancePath, e.Issues[0].Message, more)
}

// Client defaults.
const (
	DefaultTimeout  = 5 * time.Second
	DefaultAttempts = 3
	DefaultBackoff  = 100 * time.Millisecond
	// maxResponse bounds what the client reads: every adapter's output
	// for a large Brief stays well under it.
	maxResponse = 16 << 20
)

// Client calls the compile service (packages/compile-service) on the
// private network.
type Client struct {
	base     string
	http     *http.Client
	timeout  time.Duration
	attempts int
	backoff  time.Duration
	log      *slog.Logger
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithTimeout bounds each attempt (DefaultTimeout).
func WithTimeout(d time.Duration) ClientOption { return func(c *Client) { c.timeout = d } }

// WithAttempts sets how many times a transient failure is tried
// (DefaultAttempts), with WithBackoff doubling between tries.
func WithAttempts(n int) ClientOption { return func(c *Client) { c.attempts = max(n, 1) } }

// WithBackoff sets the first wait between attempts (DefaultBackoff).
func WithBackoff(d time.Duration) ClientOption { return func(c *Client) { c.backoff = d } }

// WithHTTPClient replaces the HTTP client.
func WithHTTPClient(hc *http.Client) ClientOption { return func(c *Client) { c.http = hc } }

// WithClientLogger replaces slog.Default().
func WithClientLogger(l *slog.Logger) ClientOption { return func(c *Client) { c.log = l } }

// NewClient returns a client for the compile service at baseURL (for
// example http://memax-compile.internal:8080), or nil when baseURL is
// empty: nil means compiling is disabled.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	c := &Client{
		base: baseURL, timeout: DefaultTimeout, attempts: DefaultAttempts, backoff: DefaultBackoff, log: slog.Default(),
		// Idle connections close before the service's 65 s keep-alive
		// does, so the client never reuses a socket the server is closing.
		http: &http.Client{Transport: &http.Transport{
			MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: DefaultTimeout,
		}},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Compile renders an input.
func (c *Client) Compile(ctx context.Context, in *Input) (*Result, error) {
	var out Result
	if err := c.do(ctx, http.MethodPost, "/compile", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ParseBack reads a hand edit back as changes.
func (c *Client) ParseBack(ctx context.Context, req *ParseBackRequest) (*ParseBackResult, error) {
	var out ParseBackResult
	if err := c.do(ctx, http.MethodPost, "/parse-back", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Health asks the service whether it is up, and which adapters it has.
func (c *Client) Health(ctx context.Context) (*Health, error) {
	var out Health
	if err := c.do(ctx, http.MethodGet, "/health", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// errorBody is the service's error envelope.
type errorBody struct {
	Error struct {
		Code    string  `json:"code"`
		Message string  `json:"message"`
		Issues  []Issue `json:"issues"`
	} `json:"error"`
}

// permanentError is a failure retrying won't fix.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("compile: encode request: %w", err)
		}
	}
	var last error
	wait := c.backoff
	for attempt := 1; attempt <= c.attempts; attempt++ {
		err := c.once(ctx, method, path, payload, out)
		if err == nil {
			return nil
		}
		var perm *permanentError
		var input *InputError
		if errors.As(err, &perm) || errors.As(err, &input) {
			return err
		}
		last = err
		if attempt == c.attempts || ctx.Err() != nil {
			break
		}
		c.log.WarnContext(ctx, "compile service: retrying", "path", path, "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("compile: %s: %w (after %v)", path, ctx.Err(), last)
		case <-time.After(wait):
		}
		wait *= 2
	}
	return fmt.Errorf("%w: %s failed after %d attempts: %w", ErrUnavailable, path, c.attempts, last)
}

func (c *Client) once(ctx context.Context, method, path string, payload []byte, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return &permanentError{fmt.Errorf("compile: %w", err)}
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("compile: %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return fmt.Errorf("compile: %s: read response: %w", path, err)
	}
	if len(raw) > maxResponse {
		return &permanentError{fmt.Errorf("compile: %s: response over %d bytes", path, maxResponse)}
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, out); err != nil {
			return &permanentError{fmt.Errorf("compile: %s: decode response: %w", path, err)}
		}
		return nil
	}
	var eb errorBody
	_ = json.Unmarshal(raw, &eb)
	msg := eb.Error.Message
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	switch {
	case resp.StatusCode == http.StatusUnprocessableEntity:
		return &InputError{Message: msg, Issues: eb.Error.Issues}
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return fmt.Errorf("compile: %s: %d %s", path, resp.StatusCode, msg)
	}
	return &permanentError{fmt.Errorf("compile: %s: %d %s: %s", path, resp.StatusCode, eb.Error.Code, msg)}
}

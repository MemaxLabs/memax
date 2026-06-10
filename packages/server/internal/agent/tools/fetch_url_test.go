package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
)

// fakeFetchURLService records each call so tests can assert
// the tool wrapper passes through the right (OwnerID, URL)
// pair and surfaces sentinels correctly.
type fakeFetchURLService struct {
	mu       sync.Mutex
	requests []tools.FetchURLRequest
	err      error
	outcome  tools.FetchURLOutcome
}

func (f *fakeFetchURLService) FetchURL(_ context.Context, in tools.FetchURLRequest) (tools.FetchURLOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, in)
	if f.err != nil {
		return tools.FetchURLOutcome{}, f.err
	}
	return f.outcome, nil
}

func makeFetchCall(t *testing.T, args tools.FetchURLArgs) sdktool.Call {
	t.Helper()
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return sdktool.Call{
		Use: sdkmodel.ToolUse{
			ID:    "call-fetch-1",
			Name:  "fetch_url",
			Input: body,
		},
	}
}

func invokeFetch(t *testing.T, tool sdktool.Tool, args tools.FetchURLArgs) sdkmodel.ToolResult {
	t.Helper()
	def, ok := tool.(sdktool.Definition)
	if !ok {
		t.Fatalf("not a Definition: %T", tool)
	}
	res, err := def.Handler(context.Background(), makeFetchCall(t, args))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	return res
}

// Happy path: service returns a body + metadata, wrapper
// passes through. OwnerID flows from scope to service.
func TestFetchURLTool_HappyPath(t *testing.T) {
	t.Parallel()
	svc := &fakeFetchURLService{
		outcome: tools.FetchURLOutcome{
			FinalURL:    "https://example.com/page",
			StatusCode:  200,
			ContentType: "text/html",
			Title:       "Example",
			Body:        "<html>...</html>",
			Truncated:   false,
		},
	}
	tool, err := tools.NewFetchURLTool(tools.FetchURLToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	if err != nil {
		t.Fatalf("NewFetchURLTool: %v", err)
	}

	res := invokeFetch(t, tool, tools.FetchURLArgs{URL: "  https://example.com/page  "})
	if res.IsError {
		t.Fatalf("IsError = true: %s", res.Content)
	}
	var got tools.FetchURLResult
	if err := json.Unmarshal([]byte(res.Content), &got); err != nil {
		t.Fatalf("decode: %v (raw=%s)", err, res.Content)
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
	if got.Title != "Example" {
		t.Errorf("Title = %q, want %q", got.Title, "Example")
	}
	if got.FinalURL != "https://example.com/page" {
		t.Errorf("FinalURL = %q", got.FinalURL)
	}

	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(svc.requests))
	}
	req := svc.requests[0]
	if req.OwnerID != testOwner {
		t.Errorf("OwnerID = %q, want %q", req.OwnerID, testOwner)
	}
	// URL is trimmed before dispatch.
	if req.URL != "https://example.com/page" {
		t.Errorf("URL = %q, want trimmed", req.URL)
	}
}

// Empty URL → wrapper rejects with IsError without calling the service.
func TestFetchURLTool_EmptyURLRejects(t *testing.T) {
	t.Parallel()
	svc := &fakeFetchURLService{}
	tool, _ := tools.NewFetchURLTool(tools.FetchURLToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	for _, u := range []string{"", "  ", "\t\n"} {
		res := invokeFetch(t, tool, tools.FetchURLArgs{URL: u})
		if !res.IsError {
			t.Errorf("url=%q IsError=false, want true", u)
		}
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.requests) != 0 {
		t.Errorf("service called %d times, want 0", len(svc.requests))
	}
}

// Service returns ErrFetchURLBlocked → wrapper surfaces a
// generic rejection message (doesn't leak the specific
// blocklist reason). Protects against prompt-driven
// enumeration of internal hosts.
func TestFetchURLTool_BlockedURLGenericMessage(t *testing.T) {
	t.Parallel()
	svc := &fakeFetchURLService{err: tools.ErrFetchURLBlocked}
	tool, _ := tools.NewFetchURLTool(tools.FetchURLToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeFetch(t, tool, tools.FetchURLArgs{URL: "http://10.0.0.5/"})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
	// Error message must not contain the specific IP / host.
	if errBody := res.Content; errBody == "" || string(errBody) == "10.0.0.5" {
		t.Errorf("error body should be generic, got %q", errBody)
	}
}

// Service returns ErrFetchURLNetworkFailed → wrapper surfaces
// a generic network error. Distinguishable from blocked-URL
// (Blocked is terminal; Network is retryable).
func TestFetchURLTool_NetworkFailedMaps(t *testing.T) {
	t.Parallel()
	svc := &fakeFetchURLService{err: tools.ErrFetchURLNetworkFailed}
	tool, _ := tools.NewFetchURLTool(tools.FetchURLToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeFetch(t, tool, tools.FetchURLArgs{URL: "https://example.com/"})
	if !res.IsError {
		t.Errorf("IsError = false, want true")
	}
}

// Generic service error → wrapper surfaces as IsError without
// claiming a typed failure.
func TestFetchURLTool_GenericServiceErrorPropagates(t *testing.T) {
	t.Parallel()
	svc := &fakeFetchURLService{err: errors.New("upstream blew up")}
	tool, _ := tools.NewFetchURLTool(tools.FetchURLToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeFetch(t, tool, tools.FetchURLArgs{URL: "https://example.com/"})
	if !res.IsError {
		t.Errorf("IsError = false, want true")
	}
}

// Wire result: the service's outcome fields are echoed
// faithfully — Truncated, ContentType, etc. all flow through
// untouched.
func TestFetchURLTool_PassesThroughOutcomeFields(t *testing.T) {
	t.Parallel()
	svc := &fakeFetchURLService{
		outcome: tools.FetchURLOutcome{
			FinalURL:    "https://example.com/big",
			StatusCode:  206,
			ContentType: "application/json",
			Body:        `{"key":"value"}`,
			Truncated:   true,
		},
	}
	tool, _ := tools.NewFetchURLTool(tools.FetchURLToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeFetch(t, tool, tools.FetchURLArgs{URL: "https://example.com/big"})
	if res.IsError {
		t.Fatalf("IsError = true: %s", res.Content)
	}
	var got tools.FetchURLResult
	_ = json.Unmarshal([]byte(res.Content), &got)
	if got.StatusCode != 206 {
		t.Errorf("StatusCode = %d, want 206", got.StatusCode)
	}
	if got.ContentType != "application/json" {
		t.Errorf("ContentType = %q", got.ContentType)
	}
	if !got.Truncated {
		t.Errorf("Truncated = false, want true")
	}
}

// JSON-escape regression. Codex Phase 3.6b3g v2 [High]
// caught that a fixed RAW-byte cap doesn't account for
// encoding/json's escape expansion of `<`, `>`, `&`, and
// control characters. This test feeds an outcome with a
// body of pure `<` characters at the adapter cap, and
// asserts the marshaled tool result is (a) under
// fetchURLMaxResultBytes and (b) valid JSON.
func TestFetchURLTool_HTMLBodyDoesNotBlowJSONCap(t *testing.T) {
	t.Parallel()
	// 48KB of `<` — each escapes to "\u003c" (6 bytes) so
	// the un-shrunk JSON would be ~288KB.
	bigHTML := strings.Repeat("<", 48*1024)
	svc := &fakeFetchURLService{
		outcome: tools.FetchURLOutcome{
			FinalURL:    "https://example.com/big",
			StatusCode:  200,
			ContentType: "text/html",
			Body:        bigHTML,
			Truncated:   false,
		},
	}
	tool, _ := tools.NewFetchURLTool(tools.FetchURLToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeFetch(t, tool, tools.FetchURLArgs{URL: "https://example.com/big"})
	if res.IsError {
		t.Fatalf("IsError = true: %s", res.Content)
	}
	// (a) marshaled size fits inside the SDK cap.
	if len(res.Content) > 64*1024 {
		t.Errorf("len(Content) = %d, want <= 65536 (SDK MaxResultBytes)", len(res.Content))
	}
	// (b) valid JSON.
	var got tools.FetchURLResult
	if err := json.Unmarshal([]byte(res.Content), &got); err != nil {
		t.Fatalf("decode: %v (raw len=%d)", err, len(res.Content))
	}
	// (c) Truncated must be true — agent shouldn't be told
	// it received the full body.
	if !got.Truncated {
		t.Errorf("Truncated = false, want true (body was shrunk to fit JSON cap)")
	}
	// (d) The body field is non-empty (we kept SOMETHING)
	// and shorter than the original.
	if len(got.Body) == 0 {
		t.Errorf("Body empty after shrink — should keep at least minBodyKeep bytes")
	}
	if len(got.Body) >= len(bigHTML) {
		t.Errorf("Body not shrunk: len=%d (original=%d)", len(got.Body), len(bigHTML))
	}
}

// Same regression with control chars (\x00) — JSON escapes
// control bytes to "\u0000" (6 bytes). 48KB of NUL → 288KB
// JSON; the marshal-and-shrink loop must converge.
func TestFetchURLTool_ControlCharBodyDoesNotBlowJSONCap(t *testing.T) {
	t.Parallel()
	bigCtl := strings.Repeat("\x00", 48*1024)
	svc := &fakeFetchURLService{
		outcome: tools.FetchURLOutcome{
			FinalURL:    "https://example.com/binary",
			StatusCode:  200,
			ContentType: "application/octet-stream",
			Body:        bigCtl,
			Truncated:   false,
		},
	}
	tool, _ := tools.NewFetchURLTool(tools.FetchURLToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeFetch(t, tool, tools.FetchURLArgs{URL: "https://example.com/binary"})
	if res.IsError {
		t.Fatalf("IsError = true: %s", res.Content)
	}
	if len(res.Content) > 64*1024 {
		t.Errorf("len(Content) = %d, want <= 65536", len(res.Content))
	}
	var got tools.FetchURLResult
	if err := json.Unmarshal([]byte(res.Content), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Truncated {
		t.Errorf("Truncated = false, want true")
	}
}

// Oversized FinalURL with empty body: even with no body
// content, an adversarially-large FinalURL could bust the
// JSON cap. Codex Phase 3.6b3g v3 [Medium] flagged that the
// fallback path didn't recheck size after dropping body.
// safefetch caps URLs at 8KB upstream (MaxURLLength), but
// the wrapper is the last line of defense — this test pins
// that even an artificial oversized FinalURL doesn't
// produce JSON > MaxResultBytes.
func TestFetchURLTool_OversizedFinalURLTruncatedToFit(t *testing.T) {
	t.Parallel()
	// Bigger than fetchURLMaxResultBytes (64KB) on its own.
	bigURL := "https://example.com/" + strings.Repeat("a", 80*1024)
	svc := &fakeFetchURLService{
		outcome: tools.FetchURLOutcome{
			FinalURL:    bigURL,
			StatusCode:  200,
			ContentType: "text/plain",
			Body:        "", // body is already empty
			Truncated:   false,
		},
	}
	tool, _ := tools.NewFetchURLTool(tools.FetchURLToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeFetch(t, tool, tools.FetchURLArgs{URL: "https://example.com/x"})
	if res.IsError {
		t.Fatalf("IsError = true: %s", res.Content)
	}
	if len(res.Content) > 64*1024 {
		t.Errorf("len(Content) = %d, want <= 65536", len(res.Content))
	}
	var got tools.FetchURLResult
	if err := json.Unmarshal([]byte(res.Content), &got); err != nil {
		t.Fatalf("decode: %v (raw len=%d)", err, len(res.Content))
	}
	if !got.Truncated {
		t.Errorf("Truncated = false, want true (FinalURL was forcibly shrunk to fit)")
	}
	// FinalURL must have been truncated — sanity check it's
	// shorter than the input.
	if len(got.FinalURL) >= len(bigURL) {
		t.Errorf("FinalURL not truncated: len=%d (input=%d)", len(got.FinalURL), len(bigURL))
	}
}

// Constructor validates required deps.
func TestNewFetchURLTool_Validation(t *testing.T) {
	t.Parallel()
	cases := map[string]tools.FetchURLToolConfig{
		"nil service": {
			Service:  nil,
			Resolver: newResolver(hubA),
			Session:  agent.SessionDescriptor{OwnerID: testOwner, Type: agent.ScopeTypeSingleHub, HubIDs: []string{hubA}},
		},
		"nil resolver": {
			Service: &fakeFetchURLService{},
			Session: agent.SessionDescriptor{OwnerID: testOwner, Type: agent.ScopeTypeSingleHub, HubIDs: []string{hubA}},
		},
		"invalid session": {
			Service:  &fakeFetchURLService{},
			Resolver: newResolver(hubA),
			Session:  agent.SessionDescriptor{},
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tools.NewFetchURLTool(cfg); err == nil {
				t.Errorf("err = nil, want validation failure for %q", name)
			}
		})
	}
}

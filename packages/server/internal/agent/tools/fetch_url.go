//memax:lint-skip:tool-store
//
// Rationale for the skip directive: fetch_url is a READ tool
// that doesn't touch the store at all — it goes through
// internal/safefetch over the public HTTP/HTTPS network. The
// scripts/check-tool-store-calls lint enforces patterns
// (Resolve before service, HubIDs slice on the request) that
// only apply to store-backed tools. The security checks the
// lint cares about don't apply here:
//
//   - There is no hub-scoped data being read or mutated; the
//     fetch is an outbound network request, not a memax
//     resource lookup.
//   - The session scope is still resolved (we use
//     scope.OwnerID for audit logging) but no store filter is
//     constructed.
//   - The SSRF protection is the actual security boundary,
//     and that's enforced inside internal/safefetch via the
//     dial-time IP block + URL validation. Codex Phase 3.6b3f
//     v3 specifically pinned the dial-time check (not just
//     hostname) as the requirement.

// Phase 3.6b3g — fetch_url tool wrapper.
//
// The chat agent's only read-side OUTBOUND tool: takes a URL
// the user (or agent, with the user's prior consent at
// session create) wants to fetch, and returns the response
// metadata + a capped slice of the response body. Plan 24
// §"Tool table" pins this as opt-in (off by default), no
// approval prompt, SSRF-safe.
//
// **Why no approval prompt.** Approvals are for MUTATIONS the
// user is delegating to the agent (push/forget/propose). A
// read-only outbound fetch has no inbox-row, no graph
// mutation, no resource consumption beyond the user's quota
// (rate-limited at the session layer). The user opts in at
// session-create and the agent fetches at will within that
// session.
//
// **Why scope-resolve at all if no store call?** Two
// reasons: (1) the audit log carries scope.OwnerID so a fetch
// can be attributed to a user; (2) future per-plan rate
// limits (e.g. Free users get 10 fetches/turn, Pro 50) need
// the scope to look up the right limit. Today both are
// not-yet-wired, but the resolve-up-front pattern keeps the
// shape stable.

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
)

// FetchURLService is the production-side dependency the tool
// calls into. Implementations live in workerapp and wrap an
// internal/safefetch.Client.
//
// Returns ErrFetchURLBlocked when the URL fails URL-level
// validation (bad scheme, userinfo, blocked host, blocked
// literal IP) — the tool wrapper translates this into a
// generic "URL not allowed" error for the agent. We don't
// surface the specific reason because a hostile prompt
// shouldn't be able to enumerate the blocklist via error
// messages. ErrFetchURLNetworkFailed covers transport-level
// failures (DNS, dial, timeout, mid-stream disconnect); the
// distinction lets the agent retry on transient failures
// without retrying URLs the user can't reach by design.
type FetchURLService interface {
	FetchURL(ctx context.Context, in FetchURLRequest) (FetchURLOutcome, error)
}

// FetchURLRequest carries the per-call inputs the service
// needs. OwnerID is captured for audit; URL is the
// agent-supplied target; the wrapper has no caller-controlled
// extras for v1.
type FetchURLRequest struct {
	OwnerID string
	URL     string
}

// FetchURLOutcome is the post-fetch metadata + body. The
// service decides what subset of the body to surface (full
// raw, plain-text extract, etc.); this struct is the wire-
// shape the agent ultimately sees.
type FetchURLOutcome struct {
	FinalURL    string
	StatusCode  int
	ContentType string
	Title       string // optional — extracted from <title> if HTML
	Body        string // text-form of body, capped + utf-8-best-effort
	Truncated   bool   // true if the upstream body exceeded the body cap
}

// ErrFetchURLBlocked — URL failed URL-level / IP-level
// validation. The agent should treat this as terminal for
// this URL; retrying with the same URL won't change the
// outcome.
var ErrFetchURLBlocked = errors.New("url is not allowed (blocked host, scheme, or IP range)")

// ErrFetchURLNetworkFailed — fetch failed at the network
// layer (DNS, dial, timeout, server disconnect). The agent
// MAY retry; the underlying error string is logged but not
// surfaced to the user verbatim (could leak internal
// hostnames or path).
var ErrFetchURLNetworkFailed = errors.New("network fetch failed")

// FetchURLToolConfig wires the per-turn tool to the runtime's
// service + resolver + session descriptor.
type FetchURLToolConfig struct {
	Service  FetchURLService
	Resolver *agent.ScopeResolver
	Session  agent.SessionDescriptor
}

// FetchURLArgs is the wire shape the agent sends.
type FetchURLArgs struct {
	URL string `json:"url"`
}

// FetchURLResult is the wire shape returned to the agent on
// success.
type FetchURLResult struct {
	FinalURL    string `json:"final_url"`
	StatusCode  int    `json:"status_code"`
	ContentType string `json:"content_type,omitempty"`
	Title       string `json:"title,omitempty"`
	Body        string `json:"body,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
}

var errFetchURLEmptyURL = errors.New("url is required")

// NewFetchURLTool returns the SDK-side fetch_url tool.
func NewFetchURLTool(cfg FetchURLToolConfig) (sdktool.Tool, error) {
	if cfg.Service == nil {
		return nil, errors.New("tools: FetchURLToolConfig.Service is required")
	}
	if cfg.Resolver == nil {
		return nil, errors.New("tools: FetchURLToolConfig.Resolver is required")
	}
	if err := cfg.Session.Validate(); err != nil {
		return nil, fmt.Errorf("tools: FetchURLToolConfig.Session: %w", err)
	}
	return sdktool.Definition{
		ToolSpec: sdkmodel.ToolSpec{
			Name:        "fetch_url",
			Description: "Fetch a public URL over HTTP/HTTPS and return its content (capped at 5 MB). The fetch is read-only — the URL is GET'd once with no auth, and private/internal IPs (loopback, link-local, RFC1918, cloud metadata, etc.) are blocked by the SSRF-safe transport. Use for reading public web pages, public API responses, or RSS feeds the user wants the agent to read on their behalf.",
			SearchHint:  "fetch http https url web page download",
			InputSchema: map[string]any{
				"type":                 "object",
				"required":             []any{"url"},
				"additionalProperties": false,
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "Public HTTP or HTTPS URL to fetch. Must not target private/internal IPs or memax-owned hosts. Must not include user:password@ credentials in the URL.",
						"minLength":   1,
					},
				},
			},
			// 64KB — the wrapper itself enforces this same
			// number (fetchURLMaxResultBytes) by iteratively
			// shrinking the body field if JSON-marshaling
			// the wire result would otherwise overflow. The
			// SDK's MaxResultBytes is a backstop. Sized to
			// match.
			MaxResultBytes: fetchURLMaxResultBytes,
		},
		Handler: (&fetchURLHandler{
			service:  cfg.Service,
			resolver: cfg.Resolver,
			session:  cfg.Session,
		}).execute,
	}, nil
}

type fetchURLHandler struct {
	service  FetchURLService
	resolver *agent.ScopeResolver
	session  agent.SessionDescriptor
}

func (h *fetchURLHandler) execute(ctx context.Context, call sdktool.Call) (sdkmodel.ToolResult, error) {
	args, err := sdktool.DecodeInput[FetchURLArgs](call.Use)
	if err != nil {
		return errFetchURLResult(call, fmt.Sprintf("decode args: %v", err))
	}
	rawURL := strings.TrimSpace(args.URL)
	if rawURL == "" {
		return errFetchURLResult(call, errFetchURLEmptyURL.Error())
	}

	scope, err := h.resolver.Resolve(ctx, h.session)
	if err != nil {
		return errFetchURLResult(call, fmt.Sprintf("scope resolve: %v", err))
	}
	if scope.OwnerID == "" {
		// A scope without an owner shouldn't happen in
		// practice — the validator runs at session-create —
		// but fail closed defensively. Without an owner the
		// audit log can't attribute the fetch.
		return errFetchURLResult(call, "session has no resolved owner; cannot fetch")
	}

	outcome, err := h.service.FetchURL(ctx, FetchURLRequest{
		OwnerID: scope.OwnerID,
		URL:     rawURL,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrFetchURLBlocked):
			// Don't leak the specific reason. Generic
			// rejection helps the agent give the user a
			// useful response without enumerating our
			// blocklist.
			return errFetchURLResult(call, "url is not allowed by the safe-fetch policy (private IP, blocked host, unsupported scheme, or contains credentials)")
		case errors.Is(err, ErrFetchURLNetworkFailed):
			return errFetchURLResult(call, "fetch failed (network error or upstream unavailable)")
		default:
			return errFetchURLResult(call, fmt.Sprintf("fetch_url: %v", err))
		}
	}

	wire := FetchURLResult{
		FinalURL:    outcome.FinalURL,
		StatusCode:  outcome.StatusCode,
		ContentType: outcome.ContentType,
		Title:       outcome.Title,
		Body:        outcome.Body,
		Truncated:   outcome.Truncated,
	}
	body := marshalFetchURLResult(&wire)
	return sdkmodel.ToolResult{Content: string(body)}, nil
}

// fetchURLMaxResultBytes is the JSON-encoded result cap. The
// SDK enforces it via the tool's MaxResultBytes (set above);
// this helper enforces the same number BEFORE the SDK has a
// chance to truncate the marshaled JSON string (which would
// produce invalid JSON). Codex Phase 3.6b3g v2 [High] flagged
// that a fixed RAW-byte cap doesn't suffice because
// encoding/json escapes `<`, `>`, `&` (HTMLEscape default)
// plus control characters — a 48KB body of `<` chars expands
// to ~288KB JSON.
const fetchURLMaxResultBytes = 64 * 1024

// marshalFetchURLResult marshals the wire result to JSON,
// iteratively shrinking the Body field until the encoded
// size fits inside fetchURLMaxResultBytes. Each shrink
// halves the body; this terminates in O(log n) steps even
// for adversarial content (a 48KB all-`<` body fits inside
// ~16KB JSON after 4 halvings; pathological all-`\x00`
// bodies converge similarly).
//
// On any shrink, Truncated is set to true so the wire
// result's flag accurately reflects what the agent received.
// Title is never trimmed (it's small — 256-char cap at the
// adapter); FinalURL is bounded at 8KB by safefetch's
// MaxURLLength check, but we have a final-fallback truncation
// here for defense-in-depth — codex Phase 3.6b3g v3 [Medium]
// flagged that even with the body dropped, an oversized
// FinalURL could bust the cap. After body-drop, if STILL
// over the limit, we truncate FinalURL with a "[truncated]"
// suffix so the agent knows.
func marshalFetchURLResult(wire *FetchURLResult) []byte {
	const minBodyKeep = 256 // floor for body halving
	for {
		out, err := json.Marshal(wire)
		if err != nil {
			// Marshal can't realistically fail on this
			// struct shape, but if the body is somehow not
			// valid UTF-8 the encoder might error. Drop the
			// body as a last-resort fallback and try again.
			wire.Body = ""
			wire.Truncated = true
			out, _ = json.Marshal(wire)
			// Even with body dropped a marshal error means
			// some other field has a UTF-8 issue; still
			// fall through to the size-check below.
			if len(out) <= fetchURLMaxResultBytes {
				return out
			}
		}
		if len(out) <= fetchURLMaxResultBytes {
			return out
		}
		// Result too big. First, halve the body until either
		// it fits or hits the floor.
		bodyLen := len(wire.Body)
		if bodyLen > minBodyKeep {
			newLen := bodyLen / 2
			if newLen < minBodyKeep {
				newLen = minBodyKeep
			}
			wire.Body = wire.Body[:newLen]
			wire.Truncated = true
			continue
		}
		// Body is already at the floor. Drop it entirely.
		if wire.Body != "" {
			wire.Body = ""
			wire.Truncated = true
			continue
		}
		// Body is already empty and we're STILL over cap —
		// the metadata is the problem. The most likely
		// culprit is an oversized FinalURL (safefetch caps
		// it at MaxURLLength=8KB upstream, but we don't
		// trust the bound for fail-safety). Truncate the
		// FinalURL hard with a sentinel suffix so the
		// agent can tell.
		const urlTruncSuffix = "...[truncated]"
		if len(wire.FinalURL) > len(urlTruncSuffix) {
			// Halve FinalURL until result fits OR we hit the
			// suffix-only floor.
			newLen := len(wire.FinalURL) / 2
			if newLen < len(urlTruncSuffix) {
				newLen = 0
			}
			wire.FinalURL = wire.FinalURL[:newLen] + urlTruncSuffix
			wire.Truncated = true
			continue
		}
		// Last resort: drop FinalURL entirely. Title is
		// already capped at 256 chars at the adapter, so
		// metadata is now a few hundred bytes — guaranteed
		// to fit.
		wire.FinalURL = ""
		wire.Title = ""
		wire.Truncated = true
		out, _ = json.Marshal(wire)
		return out
	}
}

// errFetchURLResult shapes an SDK ToolResult marked as an error.
func errFetchURLResult(call sdktool.Call, msg string) (sdkmodel.ToolResult, error) {
	body, _ := json.Marshal(map[string]any{
		"error":   msg,
		"tool":    call.Use.Name,
		"call_id": call.Use.ID,
	})
	return sdkmodel.ToolResult{
		Content: string(body),
		IsError: true,
	}, nil
}

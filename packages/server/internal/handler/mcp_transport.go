package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The transport in front of the go-sdk handlers. One endpoint serves both
// protocol eras, and the go-sdk handles each era in a different mode:
//
//   - The 2026-07-28 protocol only runs stateless (StreamableHTTPOptions
//     .Stateless). There a request carries its version and capabilities in
//     _meta, and a tool asks the person through multi round-trip requests
//     (resultType "input_required", then a retry with the answer), so no
//     request has to come back to the machine that asked.
//   - A legacy (2025-era) request served statelessly gets a throwaway
//     session with no capabilities, and the go-sdk refuses any request
//     from the server to the client in that mode. So a legacy client that
//     can answer elicitation/create (it says so at initialize) gets a real
//     session from the stateful handler instead, with server-sent-event
//     responses so the elicitation travels on the tools/call's response.
//     Every other legacy request is served statelessly with JSON
//     responses, which is what V1 did for all of them.
//
// A session lives in one process. With several machines (production runs
// two), a request carrying a session another machine minted is replayed
// there (Fly's fly-replay with prefer_instance); if that machine is gone,
// or the session expired, the request is served statelessly: elicitation
// is then unavailable for it, and writes fall back to proposals in Review,
// but the request never fails because of where it landed.

// mcpHTTPKey carries the response writer into the request handed to the
// go-sdk, so a tool handler can reach both (mcpHTTPFrom).
type mcpHTTPKey struct{}

const mcpTokenExtra = "memax.request"

// ServeHTTP routes one MCP request.
func (h *MCPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r = normalizeMCPHeaders(r)
	var body []byte
	if r.Method == http.MethodPost {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, mcpMaxBody+1))
		if err != nil {
			writeRPCError(w, nil, -32700, "Could not read request body")
			return
		}
		if len(body) > mcpMaxBody {
			http.Error(w, fmt.Sprintf("request body exceeds %d bytes", mcpMaxBody), http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	peek := peekMCP(r, body)

	// A write the credential's scope doesn't allow, in a space on the V2
	// record, is a step-up challenge (RFC 6750 insufficient_scope), so the
	// client can ask the person for more.
	if peek.tool != "" && h.v2 != nil {
		canonical := mcpCanonicalName(string(h.mode), peek.tool)
		if mcpWriteTools[canonical] {
			if scope, needed := h.v2.StepUp(r, canonical, peek.args); needed {
				writeInsufficientScope(w, r, peek.id, scope)
				return
			}
		}
	}

	if sid := strings.TrimSpace(r.Header.Get("Mcp-Session-Id")); sid != "" && !peek.modern {
		if h.sessionKnown(sid) {
			h.serveGoSDK(h.stateful, w, r)
			return
		}
		if owner := sessionOwner(sid); owner != "" && h.instance != "" && owner != h.instance &&
			r.Header.Get("Fly-Replay-Src") == "" && r.Header.Get("Fly-Preferred-Instance-Unavailable") == "" &&
			len(body) < 1<<20 {
			// Fly's proxy replays the request on the machine holding the
			// session, or on any machine if that one is gone.
			w.Header().Set("Fly-Replay", "prefer_instance="+owner)
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		// An expired session, or one from a machine that's gone: serve it
		// statelessly rather than send the client back to initialize.
		r = r.Clone(r.Context())
		r.Header.Del("Mcp-Session-Id")
	}

	switch r.Method {
	case http.MethodPost:
		if peek.method == "initialize" && !peek.modern && peek.elicitation {
			h.serveGoSDK(h.stateful, w, r)
			return
		}
		h.serveGoSDK(h.stateless, w, r)
	case http.MethodGet:
		// No session to stream: the stateless handler answers 405.
		h.serveGoSDK(h.stateless, w, r)
	case http.MethodDelete:
		// Ending a session this server doesn't hold is a no-op, as in V1.
		w.WriteHeader(http.StatusOK)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// serveGoSDK hands the request to a go-sdk handler with the request (and
// its auth context) attached to the token info the go-sdk passes to every
// tool call. Our auth middleware already verified the credential; the
// go-sdk's bearer middleware is only the carrier, and it binds a legacy
// session to the user who opened it.
func (h *MCPHandler) serveGoSDK(next http.Handler, w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(context.WithValue(r.Context(), mcpHTTPKey{}, w))
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Authorization")), "bearer ") {
		// Memory mode and handler tests authenticate without a header.
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer memax-authenticated")
	}
	carrier := auth.RequireBearerToken(func(_ context.Context, _ string, req *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{
			UserID:     GetUserID(req),
			Expiration: time.Now().Add(24 * time.Hour),
			Extra:      map[string]any{mcpTokenExtra: req},
		}, nil
	}, nil)
	carrier(next).ServeHTTP(w, r)
}

// mcpHTTPFrom returns the HTTP request and response writer of a tool call.
func mcpHTTPFrom(req *mcp.CallToolRequest) (*http.Request, http.ResponseWriter) {
	if req == nil || req.Extra == nil || req.Extra.TokenInfo == nil {
		return nil, nil
	}
	r, _ := req.Extra.TokenInfo.Extra[mcpTokenExtra].(*http.Request)
	if r == nil {
		return nil, nil
	}
	w, _ := r.Context().Value(mcpHTTPKey{}).(http.ResponseWriter)
	if w == nil {
		w = discardHeaders{}
	}
	return r, w
}

// discardHeaders is a ResponseWriter whose headers go nowhere.
type discardHeaders struct{}

func (discardHeaders) Header() http.Header         { return http.Header{} }
func (discardHeaders) Write(b []byte) (int, error) { return len(b), nil }
func (discardHeaders) WriteHeader(int)             {}

// sessionKnown reports whether this process holds the legacy session.
func (h *MCPHandler) sessionKnown(id string) bool {
	for ss := range h.server.Sessions() {
		if ss.ID() == id {
			return true
		}
	}
	return false
}

// sessionOwner is the machine that minted a session ID ("" for IDs from
// before sessions carried one, or from a single-machine setup).
func sessionOwner(id string) string {
	owner, _, ok := strings.Cut(id, ".")
	if !ok {
		return ""
	}
	return owner
}

// normalizeMCPHeaders keeps V1's leniency: V1 answered any POST whatever
// its Accept and Content-Type, and the go-sdk requires both. A client that
// sends neither still gets a JSON response, as before.
func normalizeMCPHeaders(r *http.Request) *http.Request {
	accept := r.Header.Values("Accept")
	jsonOK, streamOK := false, false
	for _, v := range accept {
		for _, part := range strings.Split(v, ",") {
			base, _, _ := strings.Cut(strings.TrimSpace(part), ";")
			switch strings.ToLower(strings.TrimSpace(base)) {
			case "application/json", "application/*":
				jsonOK = true
			case "text/event-stream", "text/*":
				streamOK = true
			case "*/*":
				jsonOK, streamOK = true, true
			}
		}
	}
	needsCT := r.Method == http.MethodPost && strings.TrimSpace(r.Header.Get("Content-Type")) == ""
	needsAccept := r.Method != http.MethodDelete && (!jsonOK || !streamOK)
	if !needsCT && !needsAccept {
		return r
	}
	r = r.Clone(r.Context())
	if needsCT {
		r.Header.Set("Content-Type", "application/json")
	}
	if needsAccept {
		if r.Method == http.MethodGet {
			r.Header.Set("Accept", "text/event-stream")
		} else {
			r.Header.Set("Accept", "application/json, text/event-stream")
		}
	}
	return r
}

// mcpPeek is what routing needs from a request body.
type mcpPeek struct {
	id     any
	method string
	// modern: the request follows the 2026-07-28 protocol.
	modern bool
	// elicitation: an initialize that advertises elicitation.
	elicitation bool
	// tool and args: a tools/call.
	tool string
	args json.RawMessage
}

func peekMCP(r *http.Request, body []byte) mcpPeek {
	var p mcpPeek
	if v := strings.TrimSpace(r.Header.Get("Mcp-Protocol-Version")); v >= "2026-07-28" {
		p.modern = true
	}
	if len(body) == 0 {
		return p
	}
	var msg struct {
		ID     any    `json:"id"`
		Method string `json:"method"`
		Params struct {
			Meta            map[string]any  `json:"_meta"`
			Name            string          `json:"name"`
			Arguments       json.RawMessage `json:"arguments"`
			ProtocolVersion string          `json:"protocolVersion"`
			Capabilities    struct {
				Elicitation json.RawMessage `json:"elicitation"`
			} `json:"capabilities"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &msg) != nil {
		return p // a batch or garbage: the go-sdk answers it
	}
	p.id, p.method = msg.ID, msg.Method
	if v, _ := msg.Params.Meta["io.modelcontextprotocol/protocolVersion"].(string); v >= "2026-07-28" {
		p.modern = true
	}
	if msg.Method == "initialize" {
		e := bytes.TrimSpace(msg.Params.Capabilities.Elicitation)
		p.elicitation = len(e) > 0 && !bytes.Equal(e, []byte("null"))
	}
	if msg.Method == "tools/call" {
		p.tool, p.args = msg.Params.Name, msg.Params.Arguments
		if len(p.args) == 0 {
			p.args = json.RawMessage(`{}`)
		}
	}
	return p
}

// writeInsufficientScope answers a write the token's scope doesn't cover
// with 403 and a challenge naming the scope to ask for (MCP authorization,
// scope challenge handling; RFC 6750 §3.1).
func writeInsufficientScope(w http.ResponseWriter, r *http.Request, id any, scope string) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(
		`Bearer error="insufficient_scope", scope=%q, resource_metadata=%q, error_description=%q`,
		scope, MCPResourceMetadataURL(r), "This connection can only read here. Ask the person to grant "+scope+"."))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id,omitempty"`
		Error   any    `json:"error"`
	}{"2.0", id, struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{-32001, "This connection's scope can only read in this space. Reconnect it with " + scope + " to propose memories."}})
}

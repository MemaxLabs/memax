// Package mcpv2 serves the MCP tools in spaces that switched to the V2
// record (plan 25 §5.12, epic 1.7). It implements handler.MCPV2: the
// handler's go-sdk server calls it for every tool call, and it answers the
// calls that touch a space on V2, leaving the rest to the V1 tools exactly
// as they were.
//
// Every write goes through internal/ledger (Ledger.Apply), and every read
// through ledger.Read, so policy, receipts and row-level security hold.
// Credentials map onto actors in one place, v2api's principalFor
// (Handler.Principal).
//
// What the tools do on V2:
//
//   - memax_push proposes. An agent at Write keeps its own first-party
//     statements. An agent at Propose whose client can elicit asks the
//     person in the agent (Keep / Edit / Leave as proposal): over multi
//     round-trip requests on 2026-07-28 clients, over elicitation/create
//     on a legacy session. Only accept-and-keep is a Keep, recorded as
//     kept by the person via the agent, assurance client_attested. A
//     client that can't elicit gets the proposal's ID and the Review link.
//   - memax_recall reads kept memories (hybrid when V2 embeddings are on,
//     else lexical; plan §5.11, and _meta says which),
//     plus this session's own pending proposals, how the
//     connection's decision gates ended, and without a query a digest of
//     each space (with the gates still waiting). A decision a newer one
//     superseded stays kept but is left out, as in the compiled files.
//   - memax_search searches kept memories and decisions (the same).
//   - memax_push says so when a write touches a decision in force and so
//     waits for the judge in Review (policy touches_decision).
//   - memax_get reads one memory with its receipts and sources.
//   - memax_forget records a forget request: a person forgets the memory
//     (or keeps it) on the web; an agent never forgets.
//   - every response carries the connection's waiting Forget notices
//     (notices.go): each memory forgotten since it read it, once.
//   - memax_capture writes notes (V1's note path), never proposals.
//   - memax_request_decision asks a decision gate (G-) through the ledger
//     and returns its ID at once; on 2026-07-28 clients that can elicit it
//     also asks the person in the agent (gates.go). The answer, kept as the
//     person's decision, reaches the asking connection in its next recall,
//     once.
//   - memax_list, memax_hubs, memax_hub_members and memax_topics are
//     compatibility aliases (topics are the Brief's sections).
//
// Agents read only the spaces they're connected to: an agent's read scope
// on V2 is its connected spaces, intersected with its credential's scope.
package mcpv2

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/spacemode"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

// Server is the V2 tool surface.
type Server struct {
	v2      *v2api.Handler
	ledger  *ledger.Ledger
	spaces  *spacemode.Resolver
	search  *v2recall.Searcher
	digest  Digester
	reads   ReadRecorder
	state   *stateSigner
	appBase string
	log     *slog.Logger
	now     func() time.Time

	// recallBudget is the internal deadline of a recall's V2 part (plan
	// §5.11: 250 ms, inside the 300 ms p95 of N2).
	recallBudget time.Duration
	// elicitTimeout bounds how long a legacy session's elicitation/create
	// waits for the person.
	elicitTimeout time.Duration
	// stateTTL is how long a multi round-trip confirmation stays valid.
	stateTTL time.Duration
}

// Options configures a Server.
type Options struct {
	// V2 maps credentials onto ledger actors (and carries the ledger).
	V2 *v2api.Handler
	// Spaces says which spaces are on the V2 record.
	Spaces *spacemode.Resolver
	// StateSecret signs multi round-trip request state. Empty means a
	// random per-process key, so a confirmation must come back to the
	// same process.
	StateSecret []byte
	// AppBaseURL is the web app (https://memax.app), for Review links.
	AppBaseURL string
	// Digest builds recall's digest. Nil uses the compiled digest when
	// Compile is set (each space's latest compiled file, with the lexical
	// digest for spaces not compiled yet), and the lexical one otherwise.
	Digest Digester
	// Compile reads compiled artifacts (compile.Service); nil means none.
	Compile Previewer
	// Reads records agent reads (R-); nil uses the no-op hook.
	Reads ReadRecorder
	// Search answers recall and search queries. Nil is lexical only
	// (v2recall.New); the API server passes the hybrid searcher (vectors
	// and the reranker, when configured).
	Search *v2recall.Searcher
	Logger *slog.Logger
	Now    func() time.Time
}

// New returns the V2 tool surface, or nil when there is no ledger or no
// space switch (memory mode): then every space behaves as V1.
func New(o Options) *Server {
	if o.V2 == nil || o.V2.Ledger() == nil || o.Spaces == nil {
		return nil
	}
	s := &Server{
		v2: o.V2, ledger: o.V2.Ledger(), spaces: o.Spaces, search: v2recall.New(o.V2.Ledger()),
		digest: o.Digest, reads: o.Reads, appBase: strings.TrimRight(o.AppBaseURL, "/"),
		log: o.Logger, now: o.Now,
		recallBudget: 250 * time.Millisecond, elicitTimeout: 3 * time.Minute, stateTTL: 10 * time.Minute,
	}
	if o.Search != nil {
		s.search = o.Search
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	s.log = s.log.With("component", "mcpv2")
	if s.now == nil {
		s.now = time.Now
	}
	if s.digest == nil {
		s.digest = lexicalDigest{search: s.search}
		if o.Compile != nil && !isNilPreviewer(o.Compile) {
			s.digest = newCompiledDigest(s.ledger, o.Compile, s.digest, s.log)
		}
	}
	if s.reads == nil {
		s.reads = noReads{}
	}
	secret := o.StateSecret
	if len(secret) == 0 {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
	}
	s.state = &stateSigner{key: deriveKey(secret)}
	return s
}

// deriveKey separates the request-state key from whatever secret it came
// from (it may be the JWT secret).
func deriveKey(secret []byte) []byte {
	h := sha256.New()
	h.Write([]byte("memax mcp request state v1\x00"))
	h.Write(secret)
	return h.Sum(nil)
}

var _ handler.MCPV2 = (*Server)(nil)

// CallTool implements handler.MCPV2.
func (s *Server) CallTool(ctx context.Context, c *handler.MCPToolCall) (*mcp.CallToolResult, bool) {
	if s == nil {
		return nil, false
	}
	v, ok := s.resolve(ctx, c)
	if !ok {
		return nil, false
	}
	var res *mcp.CallToolResult
	switch c.Tool {
	case "memax_push":
		res, ok = s.push(ctx, c, v)
	case "memax_recall":
		res, ok = s.recall(ctx, c, v)
	case "memax_search":
		res, ok = s.searchTool(ctx, c, v)
	case "memax_get":
		res, ok = s.get(ctx, c, v)
	case "memax_list":
		res, ok = s.list(ctx, c, v)
	case "memax_hubs":
		res, ok = s.hubs(ctx, c, v)
	case "memax_hub_members":
		res, ok = s.members(ctx, c, v)
	case "memax_topics":
		res, ok = s.topics(ctx, c, v)
	case "memax_forget":
		res, ok = s.forget(ctx, c, v)
	case "memax_capture":
		res, ok = s.noteWrite(ctx, c, v)
	case "memax_request_decision":
		res, ok = s.requestDecision(ctx, c, v)
	default:
		return nil, false
	}
	// Forget's notices ride on the next response, whichever tool it is,
	// except a question put to the person (input_required).
	if ok && res != nil && len(res.InputRequests) == 0 {
		res = s.deliverNotices(ctx, v, c.Tool, res)
	}
	return res, ok
}

// StepUp implements handler.MCPV2: an OAuth token whose scope only reads,
// writing to a space on the V2 record, is asked to step up to
// memax:propose (403 insufficient_scope). On V1 the V1 tools answer it as
// before.
func (s *Server) StepUp(ctx context.Context, c *handler.MCPToolCall) (string, bool) {
	if s == nil {
		return "", false
	}
	// A grant whose scope only reads, or an older grant (no recorded
	// scope) without write access: re-authorizing can fix either.
	g := handler.GetGrant(c.HTTP)
	readOnly := g.AutonomyCeiling() == "read" ||
		(g.PrincipalType == "oauth_grant" && g.AutonomyCeiling() == "" && !g.DefaultPermissions.Has(handler.PermMemoryWrite))
	if !readOnly {
		return "", false
	}
	if c.Tool != "memax_push" && c.Tool != "memax_capture" && c.Tool != "memax_request_decision" {
		return "", false
	}
	hubID := handler.GetWriteHubID(c.HTTP)
	switch ref := argHub(c.Args); {
	case c.Tool == "memax_push":
		hub, err := c.ResolveHub(ref)
		if err != nil {
			return "", false
		}
		hubID = hub.Hub.ID
	case c.Tool == "memax_request_decision" && ref != "":
		// A space on V2 named here is where the gate goes (requestDecision).
		if hub, err := c.ResolveHub(ref); err == nil {
			if on, err := s.spaces.IsV2(ctx, hub.Hub.ID); err == nil && on {
				hubID = hub.Hub.ID
			}
		}
	}
	on, err := s.spaces.IsV2(ctx, hubID)
	if err != nil || !on {
		return "", false
	}
	return handler.ScopeRead + " " + handler.ScopePropose, true
}

// argHub is a call's hub_id, or its space_id alias.
func argHub(args json.RawMessage) string {
	var a struct {
		HubID   string `json:"hub_id"`
		SpaceID string `json:"space_id"`
	}
	_ = json.Unmarshal(args, &a)
	if strings.TrimSpace(a.HubID) != "" {
		return strings.TrimSpace(a.HubID)
	}
	return strings.TrimSpace(a.SpaceID)
}

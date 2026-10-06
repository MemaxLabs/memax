package mcpv2

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// view is one call's picture of the spaces: which hubs it reaches, which
// of them are on V2, and (loaded when first needed) the caller as the
// ledger sees them.
type view struct {
	s *Server
	c *handler.MCPToolCall
	// v2 holds the reachable hubs that are on the V2 record.
	v2 map[string]bool

	hubs     []model.HubWithRole
	hubsErr  error
	hubsDone bool

	p      *v2api.Principal
	pErr   *v2api.PrincipalError
	pDone  bool
	spaces []space
}

// space is a space on V2 the caller reaches, with what it may do there.
type space struct {
	ID   uuid.UUID
	Hub  model.Hub
	Role string
	// Readable: a person, or an agent connected to the space (paused
	// agents still read).
	Readable bool
	Grant    ledger.SpaceGrant
}

// resolve builds the view and reports whether any reachable hub is on V2;
// when none is, the call is V1's.
func (s *Server) resolve(ctx context.Context, c *handler.MCPToolCall) (*view, bool) {
	v := &view{s: s, c: c}
	ids := handler.GetAccessibleHubIDs(c.HTTP)
	if len(ids) == 0 {
		hubs, err := v.allHubs()
		if err != nil {
			return nil, false
		}
		for _, h := range hubs {
			ids = append(ids, h.Hub.ID)
		}
	}
	on, err := s.spaces.V2Spaces(ctx, ids)
	if err != nil {
		// The switch can't be read: keep V1's behaviour, which is today's.
		s.log.WarnContext(ctx, "mcp: can't read which spaces are on V2; serving V1", "error", err)
		return nil, false
	}
	if len(on) == 0 {
		return nil, false
	}
	v.v2 = on
	return v, true
}

func (v *view) allHubs() ([]model.HubWithRole, error) {
	if !v.hubsDone {
		v.hubs, v.hubsErr = v.c.AccessibleHubs()
		v.hubsDone = true
	}
	return v.hubs, v.hubsErr
}

// hub resolves a hub reference the way the V1 tools do.
func (v *view) hub(ref string) (model.HubWithRole, error) {
	return v.c.ResolveHub(ref)
}

// onV2 reports whether a hub is on the V2 record.
func (v *view) onV2(hubID string) bool { return v.v2[hubID] }

// exclude is the set V1 reads leave out: every hub on V2.
func (v *view) exclude() map[string]bool { return v.v2 }

// principal is the caller as the ledger sees them.
func (v *view) principal(ctx context.Context) (*v2api.Principal, *mcp.CallToolResult) {
	if !v.pDone {
		v.p, v.pErr = v.s.v2.Principal(v.c.HTTP, policy.ViaMCP)
		v.pDone = true
		if v.pErr == nil {
			v.spaces = v.buildSpaces()
		}
	}
	if v.pErr != nil {
		return nil, errorResult(v.pErr.Message)
	}
	return v.p, nil
}

func (v *view) buildSpaces() []space {
	hubs, _ := v.allHubs()
	byID := map[string]model.HubWithRole{}
	for _, h := range hubs {
		byID[h.Hub.ID] = h
	}
	var out []space
	for _, g := range v.p.Scope.Spaces {
		id := g.SpaceID.String()
		h, ok := byID[id]
		if !v.v2[id] || !ok {
			continue
		}
		sp := space{ID: g.SpaceID, Hub: h.Hub, Role: h.Role, Grant: g, Readable: true}
		if v.p.Actor.Kind == policy.ActorAgent {
			sp.Readable = false
			if c := v.p.Connection; c != nil && c.State != ledger.ConnectionDisconnected {
				_, sp.Readable = c.AutonomyIn(g.SpaceID)
			}
		}
		out = append(out, sp)
	}
	return out
}

// readable are the spaces on V2 the caller may read.
func (v *view) readable(ctx context.Context) ([]space, *mcp.CallToolResult) {
	if _, res := v.principal(ctx); res != nil {
		return nil, res
	}
	var out []space
	for _, sp := range v.spaces {
		if sp.Readable {
			out = append(out, sp)
		}
	}
	return out, nil
}

// spaceFor returns the caller's V2 space for a hub on V2. reading asks for
// read access: an agent must be connected to the space.
func (v *view) spaceFor(ctx context.Context, hub model.Hub, reading bool) (space, *mcp.CallToolResult) {
	p, res := v.principal(ctx)
	if res != nil {
		return space{}, res
	}
	for _, sp := range v.spaces {
		if sp.Hub.ID != hub.ID {
			continue
		}
		if reading && !sp.Readable {
			return space{}, errorResult(fmt.Sprintf("%s isn't connected to %s, so it can't read it. Connect it in Agents%s.",
				actorName(p), hub.Name, v.s.linkSuffix(hub, "agents")))
		}
		return sp, nil
	}
	return space{}, errorResult("Hub not found or not accessible.")
}

// ids lists spaces' IDs.
func ids(spaces []space) []uuid.UUID {
	out := make([]uuid.UUID, len(spaces))
	for i, sp := range spaces {
		out[i] = sp.ID
	}
	return out
}

// narrowSpaces keeps the spaces matching a hub reference ("" keeps all).
func (v *view) narrowSpaces(spaces []space, ref string) ([]space, *mcp.CallToolResult) {
	if ref == "" {
		return spaces, nil
	}
	hub, err := v.hub(ref)
	if err != nil {
		return nil, errorResult("Space not found or not accessible.")
	}
	idx := slices.IndexFunc(spaces, func(sp space) bool { return sp.Hub.ID == hub.Hub.ID })
	if idx < 0 {
		return nil, nil
	}
	return spaces[idx : idx+1], nil
}

func actorName(p *v2api.Principal) string {
	if p != nil && p.Actor.Name != "" {
		return p.Actor.Name
	}
	return "This agent"
}

// --- Links ---

func (s *Server) spaceURL(hub model.Hub, place string) string {
	if s.appBase == "" {
		return ""
	}
	return s.appBase + "/" + url.PathEscape(hub.Slug) + "/" + place
}

// reviewURL deep-links a proposal in the space's Review.
func (s *Server) reviewURL(hub model.Hub, ref string) string {
	u := s.spaceURL(hub, "review")
	if u == "" || ref == "" {
		return u
	}
	return u + "?ref=" + url.QueryEscape(ref)
}

func (s *Server) memoryURL(hub model.Hub, ref string) string {
	u := s.spaceURL(hub, "memories")
	if u == "" {
		return ""
	}
	return u + "/" + url.PathEscape(ref)
}

// linkSuffix is " at <url>" for a place, or nothing without a web app.
func (s *Server) linkSuffix(hub model.Hub, place string) string {
	if u := s.spaceURL(hub, place); u != "" {
		return " at " + u
	}
	return ""
}

// --- Results ---

func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, IsError: true}
}

func textResult(text string, structured any) *mcp.CallToolResult {
	res := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
	if structured != nil {
		res.StructuredContent = structured
	}
	return res
}

// resultText is a result's text content.
func resultText(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var parts []string
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// item is a V2 hit as a result item.
func (s *Server) item(sp space, ref string, id uuid.UUID, statement string, section ledger.Section, kind ledger.Kind, state string, score float64) handler.MCPItem {
	return handler.MCPItem{
		ID: id.String(), Ref: ref, Record: handler.MCPRecordV2, SpaceID: sp.ID.String(), Space: sp.Hub.Name,
		Text: statement, Section: string(section), Kind: string(kind), State: state, Score: score,
		URL: s.memoryURL(sp.Hub, ref),
	}
}

// sectionLabel is how a section reads in text.
func sectionLabel(s ledger.Section) string {
	switch s {
	case ledger.SectionDecisions:
		return "Decisions"
	case ledger.SectionConventions:
		return "Conventions"
	case ledger.SectionPreferences:
		return "Preferences"
	case ledger.SectionOpenQuestion:
		return "Open questions"
	}
	return string(s)
}

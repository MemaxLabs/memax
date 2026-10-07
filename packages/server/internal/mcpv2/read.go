package mcpv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

type readArgs struct {
	Query      string `json:"query"`
	Limit      int    `json:"limit"`
	HubID      string `json:"hub_id"`
	SpaceID    string `json:"space_id"`
	SessionRef string `json:"session_ref"`
	Kind       string `json:"kind"`
	// IncludeNotes (memax_search) also searches the notes the person the
	// connection works for may read.
	IncludeNotes bool `json:"include_notes"`
}

// v2Part is the V2 half of a read that spans both records.
type v2Part struct {
	out  handler.MCPRecallOutput
	text string
	// retrieval is how the query ran (_meta), when there was one.
	retrieval *v2recall.Retrieval
}

// readTarget narrows a read to the space it names, when that space is on
// V2: v1 = false then means the V1 tools have nothing to add.
func (v *view) readTarget(ctx context.Context, ref string) (spaces []space, v1 bool, res *mcp.CallToolResult) {
	// The hub the call names is resolved while the caller's principal is:
	// neither depends on the other, and each round trip to Postgres is
	// about 24 ms in production.
	var hub model.HubWithRole
	var err error
	resolved := make(chan struct{})
	if ref != "" {
		go func() {
			defer close(resolved)
			hub, err = v.hub(ref)
		}()
	} else {
		close(resolved)
	}
	readable, res := v.readable(ctx)
	<-resolved
	if res != nil {
		return nil, false, res
	}
	if ref == "" {
		return readable, true, nil
	}
	if err != nil || !v.onV2(hub.Hub.ID) {
		return nil, true, nil // a V1 hub (or none): V1 answers
	}
	sp, res := v.spaceFor(ctx, hub.Hub, true)
	if res != nil {
		return nil, false, res
	}
	return []space{sp}, false, nil
}

// recall is memax_recall (and search_memories) when any reachable space is
// on V2.
func (s *Server) recall(ctx context.Context, c *handler.MCPToolCall, v *view) (*mcp.CallToolResult, bool) {
	var a readArgs
	_ = json.Unmarshal(c.Args, &a)
	query := strings.TrimSpace(a.Query)
	// The query embedding needs only the text: it runs while the caller's
	// principal and spaces resolve.
	emb := s.search.Embed(ctx, query)
	spaces, withV1, res := v.readTarget(ctx, firstNonEmpty(a.HubID, a.SpaceID))
	if res != nil {
		return res, true
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 5
	}

	var wg sync.WaitGroup
	var v1 *mcp.CallToolResult
	if withV1 && query != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v1 = c.RunV1(nil, v.exclude())
		}()
	}
	p := v.p
	part := s.recallV2(ctx, c, p, spaces, query, emb, limit, sessionRefOf(c, a.SessionRef))
	wg.Wait()
	return s.compose(part, v1, query == ""), true
}

// recallV2 reads the V2 spaces within the recall budget.
func (s *Server) recallV2(ctx context.Context, c *handler.MCPToolCall, p *v2api.Principal, spaces []space, query string, emb *v2recall.Embedding, limit int, sessionRef string) v2Part {
	part := v2Part{out: handler.MCPRecallOutput{Results: []handler.MCPItem{}, LexicalOnly: true}}
	if len(spaces) == 0 {
		return part
	}
	ctx, cancel := context.WithTimeout(ctx, s.recallBudget)
	defer cancel()
	scope := p.Scope.Narrow(ids(spaces)...)
	bySpace := map[uuid.UUID]space{}
	for _, sp := range spaces {
		bySpace[sp.ID] = sp
	}
	since := lastSeen(p)
	var b strings.Builder

	// What the answer carries beside the search (or the digest) doesn't
	// depend on it, so it's read at the same time: the session's own
	// proposals with the return notices in one round trip, and the
	// decision gates' news. Each round trip is about 24 ms in production.
	var side sync.WaitGroup
	extrasQ := v2recall.ExtrasQuery{Spaces: ids(spaces), Since: since}
	if sessionRef != "" && p.Actor.Kind == policy.ActorAgent {
		extrasQ.Proposer, extrasQ.SessionRef, extrasQ.ProposalLimit = p.Actor.ID, sessionRef, 10
	}
	var extras v2recall.Extras
	var extrasErr error
	side.Add(1)
	go func() {
		defer side.Done()
		extras, extrasErr = s.search.Extras(ctx, scope, extrasQ)
	}()
	var news gateNews
	if p.Actor.Kind == policy.ActorAgent && p.Connection != nil {
		side.Add(1)
		go func() {
			defer side.Done()
			news = s.takeGateNews(ctx, p, scope, query == "")
		}()
	}

	if query == "" {
		refs := make([]SpaceRef, len(spaces))
		for i, sp := range spaces {
			refs[i] = SpaceRef{ID: sp.ID, Name: sp.Hub.Name, Slug: sp.Hub.Slug, ReviewURL: s.spaceURL(sp.Hub, "review")}
		}
		d, err := s.digest.Digest(ctx, scope, refs, since)
		if err != nil {
			part.out.Partial = true
			s.logReadError(ctx, "digest", err)
		}
		part.out.Digest = d.Spaces

		for _, sd := range d.Spaces {
			writeDigest(&b, sd)
		}
	} else {
		found, err := s.search.Search(ctx, scope, v2recall.Query{Text: query, Filter: v2recall.Filter{Spaces: ids(spaces)}, Limit: limit,
			Embedding: emb})
		if err != nil {
			part.out.Partial = true
			s.logReadError(ctx, "search", err)
		}
		part.out.LexicalOnly = found.LexicalOnly
		if err == nil {
			part.retrieval = &found.Retrieval
		}
		for _, h := range found.Hits {
			part.out.Results = append(part.out.Results, s.hitItem(bySpace[h.SpaceID], h))
		}
		writeItems(&b, "Kept", part.out.Results)
	}

	side.Wait()
	if extrasErr != nil {
		part.out.Partial = true
		s.logReadError(ctx, "session proposals and notices", extrasErr)
	}
	if extrasQ.Proposer != uuid.Nil {
		for _, h := range extras.Proposals {
			part.out.Proposals = append(part.out.Proposals, s.hitItem(bySpace[h.SpaceID], h))
		}
		if len(part.out.Proposals) > 0 {
			writeItems(&b, "Proposed in this session, waiting in Review", part.out.Proposals)
		}
	}

	if since != nil {
		// Rule 11: a Write-level agent's write the judge found contradicting
		// a decision in force went back to Review. Any agent may have read
		// it while it was kept, so every connection hears it once.
		for _, sp := range spaces {
			rs := extras.Returned[sp.ID]
			if len(rs) == 0 {
				continue
			}
			msg := returnedNotice(sp.Hub.Name, rs)
			part.out.Notices = append(part.out.Notices, handler.MCPNotice{Kind: "returned", Message: msg})
			fmt.Fprintf(&b, "%s\n\n", msg)
		}
	}
	// Forget notices ride on every response (notices.go), once each.
	s.writeGateNews(ctx, news, bySpace, &part, &b)
	if ctx.Err() != nil {
		part.out.Partial = true
		b.WriteString("Some spaces didn't answer in time; results may be incomplete.\n")
	}
	part.text = strings.TrimSpace(b.String())
	kind := ledger.ReadRecall
	if query == "" {
		kind = ledger.ReadDigest
	}
	s.recordRead(p, kind, sessionRef, spaces, part.out)
	return part
}

// compose joins the V2 part and the V1 recall of the other spaces.
func (s *Server) compose(part v2Part, v1 *mcp.CallToolResult, digest bool) *mcp.CallToolResult {
	out := part.out
	text := part.text
	if v1 != nil {
		if v1.IsError && len(out.Results) == 0 && len(out.Proposals) == 0 && len(out.Gates) == 0 {
			return v1
		}
		if v1out, ok := v1.StructuredContent.(handler.MCPRecallOutput); ok {
			out.Results = append(out.Results, v1out.Results...)
		}
		if v1Text := resultText(v1); !v1.IsError && v1Text != "No results found." {
			if text != "" {
				text += "\n\nFrom spaces not on V2 yet:\n"
			}
			text += v1Text
		}
	}
	if text == "" {
		if digest {
			text = "Nothing kept yet in the spaces this connection can read."
		} else {
			text = "No results found."
		}
	}
	return withRetrieval(textResult(text, out), part.retrieval, out.LexicalOnly)
}

// searchTool is memax_search when any reachable space is on V2.
func (s *Server) searchTool(ctx context.Context, c *handler.MCPToolCall, v *view) (*mcp.CallToolResult, bool) {
	var a readArgs
	_ = json.Unmarshal(c.Args, &a)
	query := strings.TrimSpace(a.Query)
	if query == "" {
		return errorResult("Say what to search for in query."), true
	}
	kind := ledger.Kind(strings.TrimSpace(a.Kind))
	if kind != "" && !kind.Valid() {
		return errorResult("kind must be fact or decision."), true
	}
	// The query embedding needs only the text: it runs while the caller's
	// principal and spaces resolve.
	emb := s.search.Embed(ctx, query)
	spaces, withV1, res := v.readTarget(ctx, a.SpaceID)
	if res != nil {
		return res, true
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 10
	}
	var wg sync.WaitGroup
	var v1 *mcp.CallToolResult
	if withV1 && kind == "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v1 = c.RunV1(nil, v.exclude())
		}()
	}
	out := handler.MCPSearchOutput{Results: []handler.MCPItem{}, LexicalOnly: true}
	var b strings.Builder
	var retrieval *v2recall.Retrieval
	if len(spaces) > 0 {
		sctx, cancel := context.WithTimeout(ctx, 2*s.recallBudget)
		found, err := s.search.Search(sctx, v.p.Scope.Narrow(ids(spaces)...), v2recall.Query{
			Text: query, Filter: v2recall.Filter{Spaces: ids(spaces), Kind: kind}, Limit: limit, Embedding: emb})
		cancel()
		if err != nil {
			out.Partial = true
			s.logReadError(ctx, "search", err)
		} else {
			out.LexicalOnly, retrieval = found.LexicalOnly, &found.Retrieval
		}
		bySpace := map[uuid.UUID]space{}
		for _, sp := range spaces {
			bySpace[sp.ID] = sp
		}
		for _, h := range found.Hits {
			out.Results = append(out.Results, s.hitItem(bySpace[h.SpaceID], h))
		}
		writeItems(&b, "Kept", out.Results)
		s.recordRead(v.p, ledger.ReadSearch, sessionRefOf(c, a.SessionRef), spaces, handler.MCPRecallOutput{Results: out.Results})
		if a.IncludeNotes && kind == "" {
			notes, err := s.ledger.SearchNotes(ctx, v.p.Scope.Narrow(ids(spaces)...), ledger.NoteQuery{
				SpaceIDs: ids(spaces), Text: query, Limit: limit})
			if err != nil {
				out.Partial = true
				s.logReadError(ctx, "search notes", err)
			}
			var items []handler.MCPItem
			for _, n := range notes {
				items = append(items, noteItem(bySpace[n.SpaceID], n))
			}
			if len(items) > 0 {
				fmt.Fprintf(&b, "Notes (V1 memories, never kept context):\n")
				for i, it := range items {
					fmt.Fprintf(&b, "[%d] %s (%s) %s\n", i+1, it.Ref, it.Space, it.Text)
				}
				b.WriteString("\n")
				out.Results = append(out.Results, items...)
			}
		}
	}
	wg.Wait()
	text := strings.TrimSpace(b.String())
	if v1 != nil && !v1.IsError {
		if v1out, ok := v1.StructuredContent.(handler.MCPSearchOutput); ok {
			out.Results = append(out.Results, v1out.Results...)
		}
		if t := resultText(v1); t != "No results found." {
			if text != "" {
				text += "\n\nFrom spaces not on V2 yet:\n"
			}
			text += t
		}
	}
	if text == "" {
		text = "No results found."
	}
	return withRetrieval(textResult(text, out), retrieval, out.LexicalOnly), true
}

// get is memax_get (and get_memory) when any reachable space is on V2.
func (s *Server) get(ctx context.Context, c *handler.MCPToolCall, v *view) (*mcp.CallToolResult, bool) {
	var a struct {
		ID      string `json:"id"`
		SpaceID string `json:"space_id"`
	}
	_ = json.Unmarshal(c.Args, &a)
	ref := strings.TrimSpace(a.ID)
	_, _, isRef := ledger.ParseRef(ref)
	_, uuidErr := uuid.Parse(ref)
	if !isRef && uuidErr != nil {
		return nil, false
	}
	spaces, _, res := v.readTarget(ctx, a.SpaceID)
	if res != nil {
		return res, true
	}
	if len(spaces) == 0 {
		if isRef {
			return errorResult(fmt.Sprintf("Memory not found: %s. Display IDs live in spaces on the V2 record this connection can read; pass space_id.", ref)), true
		}
		return c.RunV1(nil, v.exclude()), true
	}
	hist, err := s.ledger.GetMemoryHistory(ctx, v.p.Scope.Narrow(ids(spaces)...), ref)
	if errors.Is(err, ledger.ErrNotFound) && !isRef {
		return c.RunV1(nil, v.exclude()), true // a V1 memory's UUID
	}
	if err != nil {
		return s.ledgerError(ctx, err), true
	}
	m := hist.Memory
	sp := spaceOf(spaces, m.SpaceID)
	switch m.Lifecycle {
	case lifecycle.Rejected:
		return errorResult(fmt.Sprintf("Memory not found: %s", ref)), true
	case lifecycle.Proposed:
		if decision, ok := returnedTo(m, hist.Receipts.Receipts); ok {
			what := "a decision in force"
			if decision != "" {
				what = decision + ", a decision in force"
			}
			return errorResult(fmt.Sprintf("%s is back in Review in %s: it was kept at once, then Memax found it contradicts %s. "+
				"It isn't kept now, so don't act on it; a person keeps it or settles the conflict.", m.Ref, sp.Hub.Name, what)), true
		}
		return errorResult(fmt.Sprintf("%s is a proposal waiting in Review in %s; it can be read once a person keeps it.", m.Ref, sp.Hub.Name)), true
	}
	detail := handler.MCPMemoryDetail{
		ID: m.ID.String(), Ref: m.Ref, Record: handler.MCPRecordV2, SpaceID: sp.ID.String(), Space: sp.Hub.Name,
		Text: m.Statement, Section: string(m.Section), Kind: string(m.Kind), State: string(m.State),
		Trust: string(m.Trust), Version: m.Version, CreatedAt: m.CreatedAt.Format(time.RFC3339),
		URL: s.memoryURL(sp.Hub, m.Ref),
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s · %s\n", m.Ref, sp.Hub.Name)
	fmt.Fprintf(&b, "State: %s | Section: %s | Kind: %s | Trust: %s | Version: %d\n", m.State, m.Section, m.Kind, m.Trust, m.Version)
	if m.Lifecycle == lifecycle.Forgotten {
		b.WriteString("\nThis memory was forgotten: its words are gone, and only its receipts remain.\n")
	} else {
		fmt.Fprintf(&b, "\n%s\n", m.Statement)
	}
	if len(m.Sources) > 0 {
		b.WriteString("\n## Sources\n")
		for _, src := range m.Sources {
			detail.Sources = append(detail.Sources, handler.MCPSource{Kind: string(src.Kind), Ref: src.Ref, URI: src.URI,
				External: src.External, Trust: string(src.Trust)})
			line := fmt.Sprintf("- %s: %s", src.Kind, src.Ref)
			if src.URI != "" {
				line += " (" + src.URI + ")"
			}
			if src.External {
				line += " [external]"
			}
			b.WriteString(line + "\n")
		}
	}
	if n := len(hist.Receipts.Receipts); n > 0 {
		b.WriteString("\n## Receipts\n")
		for i := n - 1; i >= 0; i-- {
			rc := hist.Receipts.Receipts[i]
			detail.Receipts = append(detail.Receipts, handler.MCPReceipt{
				Action: string(rc.Action), ActorKind: string(rc.ActorKind), Agent: rc.Agent, Via: string(rc.Via),
				Assurance: string(rc.Assurance), Reason: rc.Reason, At: rc.OccurredAt.Format(time.RFC3339),
			})
			line := fmt.Sprintf("- %s %s by %s", rc.OccurredAt.Format("2006-01-02 15:04"), rc.Action, rc.ActorKind)
			if rc.Agent != "" {
				line += " via " + rc.Agent
			}
			line += " (" + string(rc.Via)
			if rc.Assurance != "" {
				line += ", " + string(rc.Assurance)
			}
			line += ")"
			if rc.Reason != "" {
				line += ": " + rc.Reason
			}
			b.WriteString(line + "\n")
		}
	}
	s.recordRead(v.p, ledger.ReadGet, sessionRefOf(c, ""), []space{sp}, handler.MCPRecallOutput{Results: []handler.MCPItem{{ID: m.ID.String(), Ref: m.Ref, Record: handler.MCPRecordV2, SpaceID: sp.ID.String()}}})
	return textResult(strings.TrimSpace(b.String()), handler.MCPGetOutput{Memory: detail}), true
}

// list is memax_list when any reachable space is on V2.
func (s *Server) list(ctx context.Context, c *handler.MCPToolCall, v *view) (*mcp.CallToolResult, bool) {
	var a struct {
		Limit   int    `json:"limit"`
		Cursor  string `json:"cursor"`
		HubID   string `json:"hub_id"`
		SpaceID string `json:"space_id"`
		TopicID string `json:"topic_id"`
	}
	_ = json.Unmarshal(c.Args, &a)
	ref := firstNonEmpty(a.HubID, a.SpaceID)
	spaces, withV1, res := v.readTarget(ctx, ref)
	if res != nil {
		return res, true
	}
	if withV1 {
		if ref != "" {
			return nil, false // a V1 hub
		}
		res := c.RunV1(nil, v.exclude())
		if res.IsError || len(spaces) == 0 {
			return res, true
		}
		names := make([]string, len(spaces))
		for i, sp := range spaces {
			names[i] = sp.Hub.Name
		}
		note := fmt.Sprintf("\n\nSpaces on the V2 record (pass hub_id to list their kept memories): %s.", strings.Join(names, ", "))
		return textResult(resultText(res)+note, res.StructuredContent), true
	}
	sp := spaces[0]
	limit := a.Limit
	if limit <= 0 {
		limit = 20
	}
	q := ledger.MemoryQuery{SpaceID: sp.ID, States: []lifecycle.Mark{lifecycle.MarkKept, lifecycle.MarkStale, lifecycle.MarkConflict},
		Cursor: a.Cursor, Limit: min(limit, 50)}
	if section := ledger.Section(a.TopicID); a.TopicID != "" {
		if !section.Valid() {
			return errorResult("In a space on the V2 record, topic_id is a section: decisions, conventions, preferences or open_question."), true
		}
		q.Sections = []ledger.Section{section}
	}
	page, err := s.ledger.ListMemories(ctx, v.p.Scope.Narrow(sp.ID), q)
	if err != nil {
		return s.ledgerError(ctx, err), true
	}
	out := handler.MCPListOutput{Memories: []handler.MCPItem{}, NextCursor: page.NextCursor, HasMore: page.HasMore}
	var b strings.Builder
	for _, m := range page.Memories {
		if m.Lifecycle != lifecycle.Kept {
			continue
		}
		out.Memories = append(out.Memories, s.item(sp, m.Ref, m.ID, m.Statement, m.Section, m.Kind, string(m.State), 0))
		fmt.Fprintf(&b, "- %s [%s/%s] %s\n", m.Ref, m.Section, m.State, m.Statement)
	}
	if len(out.Memories) == 0 {
		return textResult(fmt.Sprintf("No kept memories in %s yet.", sp.Hub.Name), out), true
	}
	fmt.Fprintf(&b, "\nShowing %d from %s.", len(out.Memories), sp.Hub.Name)
	if page.HasMore {
		fmt.Fprintf(&b, " More available — pass cursor: \"%s\" for next page.", page.NextCursor)
	}
	s.recordRead(v.p, ledger.ReadList, sessionRefOf(c, ""), []space{sp}, handler.MCPRecallOutput{Results: out.Memories})
	return textResult(b.String(), out), true
}

// hubs is memax_hubs (and list_hubs) when any reachable space is on V2:
// V1's lines for the V1 hubs, then the V2 spaces this caller can read.
func (s *Server) hubs(ctx context.Context, c *handler.MCPToolCall, v *view) (*mcp.CallToolResult, bool) {
	readable, res := v.readable(ctx)
	if res != nil {
		return res, true
	}
	v1 := c.RunV1(nil, v.exclude())
	if v1.IsError {
		return v1, true
	}
	counts, err := s.search.KeptCounts(ctx, v.p.Scope.Narrow(ids(readable)...), ids(readable))
	if err != nil {
		s.logReadError(ctx, "kept counts", err)
	}
	var lines []string
	if t := resultText(v1); t != "No hubs found." {
		lines = append(lines, t)
	}
	active := handler.GetHubID(c.HTTP)
	for _, sp := range readable {
		ref := sp.Hub.Slug
		if sp.Hub.HubType == "personal" {
			ref = "personal"
		}
		mark := ""
		if sp.Hub.ID == active {
			mark = " active"
		}
		line := fmt.Sprintf("- **%s** (%s, %s%s) ref: %s id: %s memories: %d kept · on V2",
			sp.Hub.Name, sp.Hub.HubType, sp.Role, mark, ref, sp.Hub.ID, counts[sp.ID])
		if v.p.Actor.Kind == policy.ActorAgent {
			level := sp.Grant.Autonomy
			if sp.Grant.AgentStatus == policy.AgentPaused {
				level = "paused"
			}
			line += fmt.Sprintf(" · %s: %s", actorName(v.p), level)
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return textResult("No hubs found.", nil), true
	}
	return textResult(strings.Join(lines, "\n"), nil), true
}

// members is memax_hub_members on a space on V2: V1's members, once the
// agent may read the space.
func (s *Server) members(ctx context.Context, c *handler.MCPToolCall, v *view) (*mcp.CallToolResult, bool) {
	hub, err := v.hub(argHub(c.Args))
	if err != nil || !v.onV2(hub.Hub.ID) {
		return nil, false
	}
	if _, res := v.spaceFor(ctx, hub.Hub, true); res != nil {
		return res, true
	}
	return c.RunV1(nil, nil), true
}

// topics is memax_topics on a space on V2: its topics are the Brief's
// sections.
func (s *Server) topics(ctx context.Context, c *handler.MCPToolCall, v *view) (*mcp.CallToolResult, bool) {
	var a struct {
		TopicID string `json:"topic_id"`
	}
	_ = json.Unmarshal(c.Args, &a)
	hub, err := v.hub(argHub(c.Args))
	if err != nil || !v.onV2(hub.Hub.ID) {
		return nil, false
	}
	sp, res := v.spaceFor(ctx, hub.Hub, true)
	if res != nil {
		return res, true
	}
	if a.TopicID != "" {
		args, _ := json.Marshal(map[string]any{"hub_id": sp.Hub.ID, "topic_id": a.TopicID, "limit": 20})
		c.Args = args
		res, _ := s.list(ctx, c, v)
		return res, true
	}
	digests, err := s.search.Digest(ctx, v.p.Scope.Narrow(sp.ID), []uuid.UUID{sp.ID}, 0, nil)
	if err != nil {
		return s.ledgerError(ctx, err), true
	}
	counts, err := s.sectionCounts(ctx, v.p, sp)
	if err != nil {
		return s.ledgerError(ctx, err), true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Sections of %s\n\n", sp.Hub.Name)
	for _, section := range ledger.Sections {
		fmt.Fprintf(&b, "- **%s** (%d kept) [id: %s]\n", sectionLabel(section), counts[section], section)
	}
	if len(digests) == 1 && digests[0].Waiting > 0 {
		fmt.Fprintf(&b, "\n%d waiting in Review%s.\n", digests[0].Waiting, s.linkSuffix(sp.Hub, "review"))
	}
	return textResult(b.String(), nil), true
}

// sectionCounts counts a space's kept memories by section.
func (s *Server) sectionCounts(ctx context.Context, p *v2api.Principal, sp space) (map[ledger.Section]int, error) {
	out := map[ledger.Section]int{}
	cursor := ""
	for range 20 {
		page, err := s.ledger.ListMemories(ctx, p.Scope.Narrow(sp.ID), ledger.MemoryQuery{SpaceID: sp.ID,
			States: []lifecycle.Mark{lifecycle.MarkKept, lifecycle.MarkStale, lifecycle.MarkConflict}, Cursor: cursor, Limit: ledger.MaxPageSize})
		if err != nil {
			return nil, err
		}
		for _, m := range page.Memories {
			if m.Lifecycle == lifecycle.Kept {
				out[m.Section]++
			}
		}
		if !page.HasMore {
			break
		}
		cursor = page.NextCursor
	}
	return out, nil
}

// --- Formatting ---

// noteItem is a note as memax_search returns it: its ref, title and the
// start of its words.
func noteItem(sp space, n ledger.Note) handler.MCPItem {
	ref := n.Ref
	text := strings.TrimSpace(n.Excerpt)
	if text == "" {
		text = n.Title
	}
	return handler.MCPItem{ID: n.ID.String(), Ref: ref, Record: handler.MCPRecordNote, SpaceID: n.SpaceID.String(),
		Space: sp.Hub.Name, Title: n.Title, Text: text, Source: n.Path, Score: n.Score, State: "note"}
}

func (s *Server) hitItem(sp space, h v2recall.Hit) handler.MCPItem {
	return s.item(sp, h.Ref, h.ID, h.Statement, h.Section, h.Kind, h.State, h.Score)
}

func writeItems(b *strings.Builder, title string, items []handler.MCPItem) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", title)
	for i, it := range items {
		fmt.Fprintf(b, "[%d] %s (%s · %s, %s) %s\n", i+1, it.Ref, it.Space, it.Section, it.State, it.Text)
	}
	b.WriteString("\n")
}

func writeDigest(b *strings.Builder, d handler.MCPSpaceDigest) {
	fmt.Fprintf(b, "## %s\n", d.Space)
	var facts []string
	if d.WaitingInReview > 0 {
		facts = append(facts, fmt.Sprintf("%d waiting in Review", d.WaitingInReview))
	}
	if d.ChangedSince != "" {
		facts = append(facts, fmt.Sprintf("%d changed since %s", d.Changed, d.ChangedSince))
	}
	if len(facts) > 0 {
		fmt.Fprintf(b, "%s\n", strings.Join(facts, " · "))
	}
	if c := d.Compiled; c != nil {
		fmt.Fprintf(b, "Compiled %s · %s · %s\n\n%s\n", c.Ref, c.Target, c.CompiledAt, strings.TrimSpace(c.Content))
		if c.Truncated {
			b.WriteString("(Truncated: the whole file is in the repository or in Memax.)\n")
		}
	}
	for _, sec := range d.Sections {
		if len(sec.Memories) == 0 {
			continue
		}
		fmt.Fprintf(b, "### %s\n", sectionLabel(ledger.Section(sec.Section)))
		for _, m := range sec.Memories {
			fmt.Fprintf(b, "- %s %s\n", m.Ref, m.Text)
		}
	}
	b.WriteString("\n")
}

func spaceOf(spaces []space, id uuid.UUID) space {
	for _, sp := range spaces {
		if sp.ID == id {
			return sp
		}
	}
	return space{ID: id}
}

// returnedNotice says which kept writes went back to Review in a space,
// and what they contradict.
func returnedNotice(space string, rs []v2recall.Returned) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = r.Ref
		if r.Decision != "" {
			parts[i] = fmt.Sprintf("%s (contradicts %s)", r.Ref, r.Decision)
		}
	}
	return fmt.Sprintf("Back in Review in %s since this connection was last seen: %s. Each was kept at once, then found to contradict "+
		"a decision in force. They aren't kept now: don't act on them until a person settles them.", space, strings.Join(parts, ", "))
}

// returnedTo is the decision in force a returned write contradicts, from
// its newest `returned` receipt, and whether it was returned at all.
func returnedTo(m *ledger.Memory, receipts []ledger.Receipt) (string, bool) {
	if m.Lifecycle != lifecycle.Proposed || !m.Flags.Has(lifecycle.Conflict) {
		return "", false
	}
	var newest *ledger.Receipt
	for i := range receipts {
		if rc := &receipts[i]; rc.Action == ledger.ActionReturned && (newest == nil || rc.Seq > newest.Seq) {
			newest = rc
		}
	}
	if newest == nil {
		return "", false
	}
	if newest.Source != nil {
		return newest.Source.Ref, true
	}
	return "", true
}

// lastSeen is when the agent's connection was last seen before this
// request: what "changes since your last read" counts from. (Its last
// recorded read would be the same moment give or take a minute, and
// last_seen_at is already on the connection the request resolved.)
func lastSeen(p *v2api.Principal) *time.Time {
	if p == nil || p.Connection == nil || p.Connection.LastSeenAt == nil {
		return nil
	}
	t := *p.Connection.LastSeenAt
	return &t
}

func (s *Server) logReadError(ctx context.Context, what string, err error) {
	if v2recall.IsTimeout(err) {
		s.log.WarnContext(ctx, "mcp: recall budget ran out", "part", what)
		return
	}
	s.log.ErrorContext(ctx, "mcp: V2 read failed", "part", what, "error", err)
}

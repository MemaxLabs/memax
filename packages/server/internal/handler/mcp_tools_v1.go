package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	ingesttitle "github.com/MemaxLabs/memax/packages/server/internal/ingest/title"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/secrets"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// The V1 tools: how every tool behaves in a space that hasn't switched to
// the V2 record. The text they return is exactly V1's; the read tools add
// structured content (their outputSchema in mcp_tools.json) beside it.
//
// exclude, where a tool takes it, lists hubs on the V2 record that the
// read leaves out (internal/mcpv2 passes them for a call that spans both
// records). A nil exclude is V1 exactly.

func (h *MCPHandler) toolRecall(r *http.Request, args json.RawMessage, ownerID string, exclude map[string]bool) *mcp.CallToolResult {
	var a struct {
		Query          string            `json:"query"`
		Limit          int               `json:"limit"`
		TopicID        string            `json:"topic_id"`
		HubID          string            `json:"hub_id"`
		SpaceID        string            `json:"space_id"`
		ProjectContext map[string]string `json:"project_context"`
	}
	_ = json.Unmarshal(args, &a)
	if a.Limit <= 0 {
		a.Limit = 5
	}
	if a.HubID == "" {
		a.HubID = a.SpaceID
	}
	if denied := mcpRequirePermission(r, PermMemoryRead, ""); denied != nil {
		return denied
	}

	// Same recall quota as REST /v1/recall. Reads bill the active
	// read hub (resolveBillingHub's read rule).
	finishOp, denied := h.guardOp(r, ownerID, "recall", GetHubID(r))
	if denied != nil {
		return denied
	}
	opCommitted := false
	defer func() { finishOp(opCommitted) }()

	scope := requestRecallScope(r, nil)
	// Resolve hub_id (supports UUID, slug, or "personal")
	if a.HubID != "" {
		hub, err := h.resolveMCPHub(r, ownerID, a.HubID)
		if err == nil {
			scope.ActiveBoostHubID = hub.Hub.ID
		}
	}
	var filters *model.SearchFilters
	if a.TopicID != "" {
		filters = &model.SearchFilters{TopicID: a.TopicID, Explicit: true}
	}
	results, _, err := h.recall.RunPipeline(r.Context(), a.Query, "mcp", "", a.ProjectContext, a.Limit, ownerID, filters, scope)
	if err == nil {
		// The pipeline ran — charge the recall (empty results still
		// cost COGS, matching the REST commit semantics).
		opCommitted = true
	}
	if err != nil {
		return mcpError(fmt.Sprintf("Recall failed: %s", err.Error()))
	}
	results = withoutHubs(results, exclude)

	// Log the usage event ONCE, above the success/empty branches —
	// "agent recalled X" reflects the user's intent, not whether
	// results were found. A zero-result recall is still real agent
	// activity for the card.
	//
	// hub_id mirrors the REST meter middleware's resolveBillingHub
	// for recall: GetHubID(r) is the active read hub (falls back to
	// the personal hub via HubContext), so default-personal MCP
	// recalls attribute correctly. Explicit scoping via args.hub_id
	// populates scope.ActiveBoostHubID and wins over the default —
	// matching the REST path where an X-Hub-ID header would.
	attributionHubID := scope.ActiveBoostHubID
	if attributionHubID == "" {
		attributionHubID = GetHubID(r)
	}
	if h.logEvent != nil {
		h.logEvent(
			ownerID, "recall", attributionHubID, "mcp", GetAgentName(r),
			map[string]any{"summary": compactActivitySummary(a.Query, 96)},
		)
	}
	// MCP's recall calls RunPipeline directly, bypassing RecallHandler.
	// Recall's agent.changed emit, so the Settings → Your Agents "Last
	// activity" line ticks live for MCP recalls too.
	events.TryPublishAgentActivity(r.Context(), h.events, ownerID, GetAgentName(r))

	out := MCPRecallOutput{Results: recalledItems(results)}
	if len(results) == 0 {
		return mcpStructured("No results found.", out)
	}
	return mcpStructured(formatRecalled(results), out)
}

// toolSearchMemories is memax_search outside the V2 record: the V1 recall
// pipeline, as a list of matches.
func (h *MCPHandler) toolSearchMemories(r *http.Request, args json.RawMessage, ownerID string, exclude map[string]bool) *mcp.CallToolResult {
	var a struct {
		Query   string `json:"query"`
		Limit   int    `json:"limit"`
		SpaceID string `json:"space_id"`
	}
	_ = json.Unmarshal(args, &a)
	if strings.TrimSpace(a.Query) == "" {
		return mcpError("Say what to search for in query.")
	}
	if a.Limit <= 0 {
		a.Limit = 10
	}
	if denied := mcpRequirePermission(r, PermMemoryRead, ""); denied != nil {
		return denied
	}
	finishOp, denied := h.guardOp(r, ownerID, "recall", GetHubID(r))
	if denied != nil {
		return denied
	}
	opCommitted := false
	defer func() { finishOp(opCommitted) }()

	scope := requestRecallScope(r, nil)
	if a.SpaceID != "" {
		hub, err := h.resolveMCPHub(r, ownerID, a.SpaceID)
		if err != nil {
			return mcpError("Space not found or not accessible.")
		}
		scope = requestRecallScope(r, []string{hub.Hub.ID})
	}
	results, _, err := h.recall.RunPipeline(r.Context(), a.Query, "mcp", "", nil, a.Limit, ownerID, nil, scope)
	if err != nil {
		return mcpError(fmt.Sprintf("Search failed: %s", err.Error()))
	}
	opCommitted = true
	results = withoutHubs(results, exclude)
	if h.logEvent != nil {
		h.logEvent(ownerID, "recall", GetHubID(r), "mcp", GetAgentName(r),
			map[string]any{"summary": compactActivitySummary(a.Query, 96)})
	}
	out := MCPSearchOutput{Results: recalledItems(results)}
	if len(results) == 0 {
		return mcpStructured("No results found.", out)
	}
	return mcpStructured(formatRecalled(results), out)
}

func formatRecalled(results []model.RecalledMemory) string {
	var sb strings.Builder
	for i, n := range results {
		score := int(n.RelevanceScore * 100)
		heading := ""
		if n.HeadingChain != "" {
			heading = fmt.Sprintf(" — %s", n.HeadingChain)
		}
		fmt.Fprintf(&sb, "[%d] %s [%s, %s, %d%%, %s] (id: %s)%s\n",
			i+1, n.Title, n.Kind, n.Stability, score, n.Age, n.ID, heading)
		if n.Summary != "" {
			fmt.Fprintf(&sb, "Summary: %s\n", n.Summary)
		}
		fmt.Fprintf(&sb, "Relevant excerpt:\n%s\n\n", n.ChunkContent)
	}
	return sb.String()
}

func recalledItems(results []model.RecalledMemory) []MCPItem {
	items := make([]MCPItem, 0, len(results))
	for _, n := range results {
		items = append(items, MCPItem{
			ID: n.ID, Record: MCPRecordV1, SpaceID: n.HubID, Space: n.HubName, Title: n.Title,
			Text: n.ChunkContent, Summary: n.Summary, Kind: n.Kind, Stability: n.Stability,
			Score: n.RelevanceScore, Age: n.Age, Source: n.Source,
		})
	}
	return items
}

func withoutHubs(results []model.RecalledMemory, exclude map[string]bool) []model.RecalledMemory {
	if len(exclude) == 0 {
		return results
	}
	kept := results[:0:0]
	for _, n := range results {
		if !exclude[n.HubID] {
			kept = append(kept, n)
		}
	}
	return kept
}

func (h *MCPHandler) toolPush(w http.ResponseWriter, r *http.Request, args json.RawMessage, ownerID string) *mcp.CallToolResult {
	var a struct {
		Content        string            `json:"content"`
		Title          string            `json:"title"`
		Hint           string            `json:"hint"`
		Tags           []string          `json:"tags"`
		SourceAgent    string            `json:"source_agent"`
		InitiationType string            `json:"initiation_type"`
		ProjectContext map[string]string `json:"project_context"`
		HubID          string            `json:"hub_id"`
		SpaceID        string            `json:"space_id"`
		HubReason      string            `json:"hub_reason"`
	}
	_ = json.Unmarshal(args, &a)
	if a.HubID == "" {
		a.HubID = a.SpaceID
	}

	if a.Content == "" {
		return mcpError("Content is required.")
	}
	// Secret gate (E2) — same posture as the REST create path: reject
	// with an actionable message the MODEL can relay and act on.
	if hits := secrets.DetectCredentials(a.Content); len(hits) > 0 {
		return mcpError("Push rejected: content appears to contain a credential (" + strings.Join(hits, ", ") + "). Memories are recalled verbatim across the user's agents. Remove the secret and push again — reference secrets by name, never by value.")
	}

	if a.Title == "" {
		a.Title = ingesttitle.GenerateFromContent(a.Content)
	}
	if a.Tags == nil {
		a.Tags = []string{}
	}
	hubRef := strings.TrimSpace(a.HubID)
	hubWithRole, err := h.resolveMCPHub(r, ownerID, hubRef)
	if err != nil {
		return mcpError("Hub not found or not accessible.")
	}
	hub := &hubWithRole.Hub
	hubID := hub.ID
	if denied := mcpRequirePermission(r, PermMemoryWrite, hubID); denied != nil {
		return denied
	}
	role := hubWithRole.Role
	if !canWriteMemories(role) && hub.OwnerID != ownerID {
		return mcpError("Write access to this hub is required.")
	}
	hubReason := strings.TrimSpace(a.HubReason)
	if hub.HubType == "team" && hubReason == "" {
		return mcpError("hub_reason is required when pushing to a team hub.")
	}

	// Quota gate — the SAME push quota the REST middleware enforces.
	// Billing hub = the resolved TARGET hub, mirroring
	// resolveBillingHub's write rule; the resolver routes personal hubs
	// to the owner's plan.
	finishOp, denied := h.guardOp(r, ownerID, "push", hubID)
	if denied != nil {
		return denied
	}
	opCommitted := false
	defer func() { finishOp(opCommitted) }()

	memoryID := generateMCPID()
	now := time.Now()
	projCtx := a.ProjectContext
	if projCtx == nil {
		projCtx = map[string]string{}
	}
	provenance, sourceAgent, claimRejected, provErr := resolveMemoryProvenance(h.store, ownerID, model.PushRequest{
		Source:         "mcp",
		SourceAgent:    a.SourceAgent,
		InitiationType: mcpInitiationType(a.InitiationType),
	}, r)
	if provErr != nil {
		return mcpError("Push failed: " + attributionErrorText(provErr))
	}
	if claimRejected {
		setMemaxWarningHeader(w, memaxWarningClaimRejected)
	} else if requestNeedsReconnectWarning(r) {
		setMemaxWarningHeader(w, memaxWarningReconnectNeeded)
	}
	if sourceAgent != "" && provenance.AttributionSource == model.MemoryAttributionSourceAuth {
		go EnsureConnectedAgent(h.store, ownerID, sourceAgent)
	}
	// Auto-heal: claim-path pushes from API-key principals pin the
	// key's agent_name so connected_agents + subsequent auth-path
	// attribution stay correct.
	if conflict, _ := tryAutoHealAPIKeyAgent(r.Context(), h.store, r, sourceAgent, provenance.AttributionSource); conflict {
		return mcpError("Push failed: requested agent attribution conflicts with the authenticated agent.")
	}
	userProvidedHint := strings.TrimSpace(a.Hint) != ""
	hint := buildHint(a.Hint, "mcp", "", projCtx)

	// Related-context enrichment: same permission check as REST handler.
	allowRelatedCtx := false
	if !userProvidedHint {
		if authCtx := GetAuthContext(r); authCtx != nil {
			allowRelatedCtx = authCtx.PermissionsByHub[hubID].Has(PermMemoryRead)
		} else {
			allowRelatedCtx = canReadMemories(role)
		}
	}
	memory := &model.Memory{
		ID:                             memoryID,
		HubID:                          hubID,
		OwnerID:                        ownerID,
		Title:                          a.Title,
		Content:                        a.Content,
		ContentType:                    "markdown",
		ContentHash:                    hashContentBytes(a.Content),
		Hint:                           hint,
		Kind:                           model.MemoryKindSemantic,
		Stability:                      model.MemoryStabilityEvolving,
		RetrievalWeight:                1.0,
		Tags:                           a.Tags,
		Boundary:                       "private",
		State:                          "active",
		Source:                         "mcp",
		SourceAgent:                    sourceAgent,
		Provenance:                     provenance,
		ProvenanceCreatedByType:        provenance.CreatedByType,
		ProvenanceCreatedBySlug:        provenance.CreatedBySlug,
		ProvenanceCreatedByDisplayName: provenance.CreatedByDisplayName,
		ProvenanceCreatedVia:           provenance.CreatedVia,
		ProvenanceInitiationType:       provenance.InitiationType,
		ProvenanceAttributionSource:    provenance.AttributionSource,
		ProjectContext:                 projCtx,
		HubReason:                      hubReason,
		Version:                        1,
		CreatedAt:                      now,
		UpdatedAt:                      now,
		AccessedAt:                     now,
	}
	// Same sanitization chokepoint as handler.Create — Title, Hint,
	// and Content funnel through sanitizeMemory regardless of intake
	// route (REST vs MCP).
	sanitizeMemory(memory)

	if err := h.store.CreateMemory(memory); err != nil {
		return mcpError(fmt.Sprintf("Push failed: %s", err.Error()))
	}
	// The memory is durable — charge the push (deferred finishOp).
	opCommitted = true
	h.publishMemoryChanged(r.Context(), memory, ownerID)

	// Trigger the same background processing pipeline as POST /v1/memories
	// (chunking, embedding, categorization, summarization)
	if h.memories != nil {
		h.memories.ProcessMemoryBackground(memoryID, ownerID, model.PushRequest{
			Content:             a.Content,
			Title:               a.Title,
			Hint:                hint,
			Kind:                model.MemoryKindSemantic,
			Stability:           model.MemoryStabilityEvolving,
			Tags:                a.Tags,
			ContentType:         "markdown",
			Source:              "mcp",
			HubReason:           hubReason,
			ProjectContext:      projCtx,
			AllowRelatedContext: allowRelatedCtx,
		})
	}

	slog.Info("mcp push", "id", memoryID, "title", a.Title)
	// Log the usage event directly — the HTTP meter middleware's
	// ClassifyOperation only knows REST paths, so /mcp falls through
	// with no auto-logging. Prefer sourceAgent (effective, post-
	// auto-heal slug from resolveMemoryProvenance) over GetAgentName(r)
	// (pre-heal grant), so the first claim push attributes correctly.
	// Summary reads from memory.Title rather than a.Title so the logged
	// activity matches the sanitized title that actually landed in the
	// DB — mirrors the REST path which meters post-sanitizePushRequest.
	if h.logEvent != nil {
		h.logEvent(
			ownerID, "push", hubID, "mcp", sourceAgent,
			map[string]any{"summary": compactActivitySummary(memory.Title, 96)},
		)
	}
	// MCP pushes don't route through MemoriesHandler's HTTP path, so emit
	// agent activity here too. Prefer sourceAgent over GetAgentName(r)
	// because the provenance resolver may have auto-healed to a
	// different slug (Hatch-style claim).
	events.TryPublishAgentActivity(r.Context(), h.events, ownerID, sourceAgent)
	return mcpStructured(
		fmt.Sprintf("Saved: %s (id: %s)%s", a.Title, memoryID, mcpPushAttributionNote(claimRejected)),
		MCPPushOutput{Status: MCPPushSaved, ID: memoryID, SpaceID: hubID},
	)
}

func (h *MCPHandler) toolGet(r *http.Request, args json.RawMessage, exclude map[string]bool) *mcp.CallToolResult {
	var a struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(args, &a)

	hubIDs := GetAccessibleHubIDs(r)
	// Scope-aware load — scope-bounded principals (OAuth grants, hub-
	// allowlisted API keys) get strict hub-only filtering so they
	// cannot fetch a memory outside their granted hubs by ID via MCP.
	memory, err := loadMemoryRespectingScope(r, h.store, a.ID)
	if err != nil {
		return mcpError(fmt.Sprintf("Memory not found: %s", a.ID))
	}
	if denied := mcpRequirePermission(r, PermMemoryRead, memory.HubID); denied != nil {
		return denied
	}
	if exclude[memory.HubID] {
		return mcpError(fmt.Sprintf("%s is a note in a space on the V2 record. Notes are raw material for Dream, not context; search kept memories with memax_search.", a.ID))
	}

	// memax_get is explicit user intent (an agent asked for the full
	// memory), so it counts as a deliberate view for decay scoring —
	// same contract as the REST POST /v1/memories/{id}/access path.
	// Fire-and-forget; a failed increment must not block the read.
	ownerID := GetUserID(r)
	go h.store.IncrementMemoryAccessed(context.Background(), a.ID, ownerID, hubIDs)

	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n", memory.Title)
	fmt.Fprintf(&sb, "Kind: %s | Stability: %s | Source: %s | Created: %s\n", memory.Kind, memory.Stability, memory.Source, memory.CreatedAt.Format("2006-01-02"))
	if len(memory.Tags) > 0 {
		fmt.Fprintf(&sb, "Tags: %s\n", strings.Join(memory.Tags, ", "))
	}
	if memory.SourcePath != "" {
		fmt.Fprintf(&sb, "Source: %s\n", memory.SourcePath)
	}
	if memory.Summary != "" {
		fmt.Fprintf(&sb, "\n## Summary\n%s\n", memory.Summary)
	}
	fmt.Fprintf(&sb, "\n## Content\n%s", memory.Content)

	return mcpStructured(sb.String(), MCPGetOutput{Memory: MCPMemoryDetail{
		ID: memory.ID, Record: MCPRecordV1, SpaceID: memory.HubID, Title: memory.Title, Text: memory.Content,
		Summary: memory.Summary, Kind: string(memory.Kind), Stability: string(memory.Stability),
		Source: memory.Source, Tags: memory.Tags, CreatedAt: memory.CreatedAt.Format(time.RFC3339),
	}})
}

func (h *MCPHandler) toolList(r *http.Request, args json.RawMessage, ownerID string, exclude map[string]bool) *mcp.CallToolResult {
	var a struct {
		Limit   int    `json:"limit"`
		Cursor  string `json:"cursor"`
		Sort    string `json:"sort"`
		HubID   string `json:"hub_id"`
		SpaceID string `json:"space_id"`
		TopicID string `json:"topic_id"`
	}
	_ = json.Unmarshal(args, &a)
	if a.Limit <= 0 {
		a.Limit = 20
	}
	if a.HubID == "" {
		a.HubID = a.SpaceID
	}
	if denied := mcpRequirePermission(r, PermMemoryRead, ""); denied != nil {
		return denied
	}

	hubID := ""
	if a.HubID != "" {
		hub, err := h.resolveMCPHub(r, ownerID, a.HubID)
		if err != nil {
			return mcpError("Hub not found or not accessible.")
		}
		hubID = hub.Hub.ID
	}
	if a.TopicID != "" && !isValidUUID(a.TopicID) {
		return mcpError("topic_id must be a valid UUID")
	}

	var (
		memories   []model.Memory
		nextCursor string
		totalCount int
		err        error
	)
	if len(exclude) > 0 && hubID == "" {
		// A call that spans both records: V1 memories in the V1 hubs
		// only, strictly by hub (no owner fallback into V2 spaces).
		var v1Hubs []string
		for _, id := range GetAccessibleHubIDs(r) {
			if !exclude[id] {
				v1Hubs = append(v1Hubs, id)
			}
		}
		if len(v1Hubs) > 0 {
			memories, nextCursor, totalCount, err = h.store.ListMemoriesInHubs(r.Context(), store.StrictHubListOptions{
				HubIDs: v1Hubs, TopicID: a.TopicID, Sort: a.Sort, Limit: a.Limit, Cursor: a.Cursor,
			})
		}
	} else {
		memories, nextCursor, totalCount, err = h.store.ListMemoriesPaginated(store.ListOptions{
			Scope:   store.VisibilityScope{OwnerID: ownerID, HubIDs: GetAccessibleHubIDs(r)},
			HubID:   hubID,
			TopicID: a.TopicID,
			Limit:   a.Limit,
			Cursor:  a.Cursor,
			Sort:    a.Sort,
		})
	}
	if err != nil {
		return mcpError(fmt.Sprintf("Search failed: %s", err.Error()))
	}

	out := MCPListOutput{Memories: make([]MCPItem, 0, len(memories)), NextCursor: nextCursor, HasMore: nextCursor != "", Total: totalCount}
	for _, m := range memories {
		out.Memories = append(out.Memories, MCPItem{
			ID: m.ID, Record: MCPRecordV1, SpaceID: m.HubID, Title: m.Title, Text: m.Title,
			Summary: m.Summary, Kind: string(m.Kind), Stability: string(m.Stability), Source: m.Source,
		})
	}
	if len(memories) == 0 {
		return mcpStructured(fmt.Sprintf("No memories found. (%d total in workspace)", totalCount), out)
	}

	var sb strings.Builder
	for _, m := range memories {
		fmt.Fprintf(&sb, "- %s [%s/%s] — %s (id: %s)\n", m.Title, m.Kind, m.Stability, m.Source, m.ID)
	}
	fmt.Fprintf(&sb, "\nShowing %d of %d total.", len(memories), totalCount)
	if nextCursor != "" {
		fmt.Fprintf(&sb, " More available — pass cursor: \"%s\" for next page.", nextCursor)
	}
	return mcpStructured(sb.String(), out)
}

func (h *MCPHandler) toolHubs(r *http.Request, ownerID string, exclude map[string]bool) *mcp.CallToolResult {
	if denied := mcpRequirePermission(r, PermHubRead, ""); denied != nil {
		return denied
	}
	hubs, err := h.accessibleMCPHubs(r, ownerID)
	if err != nil {
		return mcpError(fmt.Sprintf("Hubs failed: %s", err.Error()))
	}
	shown := hubs[:0:0]
	for _, item := range hubs {
		if !exclude[item.Hub.ID] {
			shown = append(shown, item)
		}
	}
	if len(shown) == 0 {
		return mcpText("No hubs found.")
	}

	activeHubID := GetHubID(r)
	var sb strings.Builder
	for _, item := range shown {
		ref := mcpHubReference(item.Hub)
		active := ""
		if item.Hub.ID == activeHubID {
			active = " active"
		}
		fmt.Fprintf(&sb, "- **%s** (%s, %s%s) ref: %s id: %s memories: %d\n",
			item.Hub.Name, item.Hub.HubType, item.Role, active, ref, item.Hub.ID, item.MemoryCount)
	}
	return mcpText(strings.TrimSpace(sb.String()))
}

func (h *MCPHandler) toolHubMembers(r *http.Request, args json.RawMessage, ownerID string) *mcp.CallToolResult {
	var a struct {
		HubID   string `json:"hub_id"`
		SpaceID string `json:"space_id"`
	}
	_ = json.Unmarshal(args, &a)
	if a.HubID == "" {
		a.HubID = a.SpaceID
	}

	hub, err := h.resolveMCPHub(r, ownerID, a.HubID)
	if err != nil {
		return mcpError(fmt.Sprintf("Hub members failed: %s", err.Error()))
	}
	if denied := mcpRequirePermission(r, PermHubMembersRead, hub.Hub.ID); denied != nil {
		return denied
	}

	members, err := h.store.ListHubMembers(hub.Hub.ID)
	if err != nil {
		return mcpError(fmt.Sprintf("Hub members failed: %s", err.Error()))
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s members\n\n", hub.Hub.Name)
	if len(members) == 0 {
		sb.WriteString("No members found.")
	} else {
		for _, member := range members {
			email := ""
			if member.UserEmail != "" {
				email = fmt.Sprintf(" <%s>", member.UserEmail)
			}
			fmt.Fprintf(&sb, "- **%s**%s [%s] joined: %s\n",
				mcpMemberDisplayName(member), email, member.Role, member.JoinedAt.Format(time.RFC3339))
		}
	}
	return mcpText(strings.TrimSpace(sb.String()))
}

func (h *MCPHandler) toolForget(r *http.Request, args json.RawMessage, ownerID string, exclude map[string]bool) *mcp.CallToolResult {
	var a struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(args, &a)

	if a.ID == "" {
		return mcpError("Memory ID is required.")
	}

	// Scope-aware load — same boundary as toolGet. Without this a
	// scope-bounded principal could delete a memory outside their
	// granted hubs by guessing its ID.
	memory, err := loadMemoryRespectingScope(r, h.store, a.ID)
	if err != nil {
		return mcpError(fmt.Sprintf("Memory not found: %s", a.ID))
	}
	if denied := mcpRequirePermission(r, PermMemoryDelete, memory.HubID); denied != nil {
		return denied
	}
	if exclude[memory.HubID] {
		return mcpError(fmt.Sprintf("%s is a note in a space on the V2 record, and an agent can't forget there. Ask the person to forget it on the web.", a.ID))
	}

	isMemoryOwner := memory.OwnerID == ownerID
	if !isMemoryOwner {
		role, _ := h.store.GetHubMemberRole(memory.HubID, ownerID)
		hub, hubErr := h.store.GetHub(memory.HubID)
		if hubErr != nil || !canDeleteMemory(role, hub, false) {
			return mcpError("You do not have permission to delete this memory.")
		}
		if err := h.store.DeleteHubMemory(a.ID, memory.HubID); err != nil {
			return mcpError(fmt.Sprintf("Delete failed: %s", err.Error()))
		}
	} else {
		if err := h.store.DeleteMemory(a.ID, ownerID); err != nil {
			return mcpError(fmt.Sprintf("Delete failed: %s", err.Error()))
		}
	}

	slog.Info("mcp forget", "id", a.ID, "title", memory.Title)
	return mcpText(fmt.Sprintf("Forgotten: %s (id: %s)", memory.Title, a.ID))
}

func (h *MCPHandler) toolCapture(w http.ResponseWriter, r *http.Request, args json.RawMessage, ownerID string) *mcp.CallToolResult {
	var a struct {
		Summary   string   `json:"summary"`
		Decisions []string `json:"decisions"`
		Learnings []string `json:"learnings"`
	}
	_ = json.Unmarshal(args, &a)

	if a.Summary == "" {
		return mcpError("Summary is required.")
	}

	content := captureContent(a.Summary, a.Decisions, a.Learnings)
	memoryID := generateMCPID()
	now := time.Now()
	hubID := resolvedHubID(r)
	if hubID == "" {
		return mcpError("No active hub could be resolved for this request.")
	}
	if denied := mcpRequirePermission(r, PermMemoryWrite, hubID); denied != nil {
		return denied
	}

	// Same push quota as REST + toolPush — capture creates a memory.
	finishOp, denied := h.guardOp(r, ownerID, "push", hubID)
	if denied != nil {
		return denied
	}
	opCommitted := false
	defer func() { finishOp(opCommitted) }()
	authAgent := resolveAuthSourceAgent(r)
	if authAgent == "" || authAgent == "unknown" {
		return mcpError("Session capture failed: this connection carries no agent identity. Reconnect the MCP client, or use an API key created with `memax auth create-key <name> --agent <slug>`.")
	}
	provenance, sourceAgent, claimRejected, provErr := resolveMemoryProvenance(h.store, ownerID, model.PushRequest{
		Source:         "mcp",
		InitiationType: model.MemoryInitiationAgentAutomatic,
	}, r)
	if provErr != nil {
		return mcpError("Session capture failed: " + attributionErrorText(provErr))
	}
	if claimRejected {
		setMemaxWarningHeader(w, memaxWarningClaimRejected)
	} else if requestNeedsReconnectWarning(r) {
		setMemaxWarningHeader(w, memaxWarningReconnectNeeded)
	}
	memory := &model.Memory{
		ID:                             memoryID,
		HubID:                          hubID,
		OwnerID:                        ownerID,
		Title:                          fmt.Sprintf("Session capture — %s", now.Format("2006-01-02")),
		Content:                        content,
		ContentType:                    "transcript",
		ContentHash:                    hashContentBytes(content),
		Kind:                           model.MemoryKindEpisodic,
		Stability:                      model.MemoryStabilityVolatile,
		RetrievalWeight:                1.0,
		Tags:                           []string{},
		Boundary:                       "private",
		State:                          "processing",
		Source:                         "mcp/capture",
		SourceAgent:                    sourceAgent,
		Provenance:                     provenance,
		ProvenanceCreatedByType:        provenance.CreatedByType,
		ProvenanceCreatedBySlug:        provenance.CreatedBySlug,
		ProvenanceCreatedByDisplayName: provenance.CreatedByDisplayName,
		ProvenanceCreatedVia:           provenance.CreatedVia,
		ProvenanceInitiationType:       provenance.InitiationType,
		ProvenanceAttributionSource:    provenance.AttributionSource,
		Version:                        1,
		CreatedAt:                      now,
		UpdatedAt:                      now,
		AccessedAt:                     now,
	}
	// MCP intake skips the REST PushRequest shape, so funnel through
	// the same sanitizer as handler.Create before persist.
	sanitizeMemory(memory)

	if err := h.store.CreateMemory(memory); err != nil {
		return mcpError(fmt.Sprintf("Capture failed: %s", err.Error()))
	}
	opCommitted = true
	h.publishMemoryChanged(r.Context(), memory, ownerID)

	// Trigger extraction pipeline in background
	if h.memories != nil {
		// Capture has no user-provided hint, so enrichment is always
		// allowed if the credential has read access.
		captureAllowCtx := false
		if authCtx := GetAuthContext(r); authCtx != nil {
			captureAllowCtx = authCtx.PermissionsByHub[hubID].Has(PermMemoryRead)
		} else {
			captureAllowCtx = true // web session — personal hub owner
		}
		h.memories.ProcessMemoryBackground(memoryID, ownerID, model.PushRequest{
			Content:             content,
			Title:               memory.Title,
			Kind:                model.MemoryKindEpisodic,
			Stability:           model.MemoryStabilityVolatile,
			ContentType:         "transcript",
			Source:              "mcp/capture",
			AllowRelatedContext: captureAllowCtx,
		})
	}

	slog.Info("mcp capture", "id", memoryID, "decisions", len(a.Decisions), "learnings", len(a.Learnings))
	track(ownerID, "api.mcp.capture", map[string]any{"decisions": len(a.Decisions), "learnings": len(a.Learnings)})
	// Log usage event for the agent-card activity line. Capture maps
	// to the "push" op because the underlying memory type + cost
	// profile match — it's a write, not a read.
	if h.logEvent != nil {
		h.logEvent(
			ownerID, "push", hubID, "mcp/capture", sourceAgent,
			map[string]any{"summary": compactActivitySummary(a.Summary, 96)},
		)
	}
	events.TryPublishAgentActivity(r.Context(), h.events, ownerID, sourceAgent)
	return mcpText(fmt.Sprintf("Session captured (id: %s). Key facts will be extracted and stored as separate memories.", memoryID))
}

// captureContent builds the capture's structured content for the
// extraction pipeline.
func captureContent(summary string, decisions, learnings []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Session Summary\n%s\n", summary)
	if len(decisions) > 0 {
		sb.WriteString("\n## Decisions Made\n")
		for _, d := range decisions {
			fmt.Fprintf(&sb, "- %s\n", d)
		}
	}
	if len(learnings) > 0 {
		sb.WriteString("\n## Learnings\n")
		for _, l := range learnings {
			fmt.Fprintf(&sb, "- %s\n", l)
		}
	}
	return sb.String()
}

// MCPCaptureContent is the text memax_capture stores, for the V2 tools'
// secret scan.
func MCPCaptureContent(args json.RawMessage) string {
	var a struct {
		Summary   string   `json:"summary"`
		Decisions []string `json:"decisions"`
		Learnings []string `json:"learnings"`
	}
	_ = json.Unmarshal(args, &a)
	return captureContent(a.Summary, a.Decisions, a.Learnings)
}

func (h *MCPHandler) toolTopics(r *http.Request, args json.RawMessage, ownerID string) *mcp.CallToolResult {
	var a struct {
		TopicID string `json:"topic_id"`
		HubID   string `json:"hub_id"`
		SpaceID string `json:"space_id"`
	}
	_ = json.Unmarshal(args, &a)
	if a.HubID == "" {
		a.HubID = a.SpaceID
	}

	hub, err := h.resolveMCPHub(r, ownerID, a.HubID)
	if err != nil {
		return mcpError(fmt.Sprintf("Failed to resolve hub: %s", err.Error()))
	}
	hubID := hub.Hub.ID
	if denied := mcpRequirePermission(r, PermTopicRead, hubID); denied != nil {
		return denied
	}
	scope := store.VisibilityScope{OwnerID: ownerID, HubIDs: []string{hubID}}

	if a.TopicID != "" {
		// Browse specific topic's memories
		memories, _, err := h.store.ListMemoriesByTopic(scope, a.TopicID, 20, "")
		if err != nil {
			return mcpError(fmt.Sprintf("Failed to list topic memories: %s", err.Error()))
		}

		var sb strings.Builder
		topic, _ := h.store.GetTopic(a.TopicID, hubID)
		if topic != nil {
			fmt.Fprintf(&sb, "## %s (%d memories)\n\n", topic.Name, len(memories))
		}
		for i, m := range memories {
			fmt.Fprintf(&sb, "%d. **%s** [%s/%s] (id: %s)\n", i+1, m.Title, m.Kind, m.Stability, m.ID)
			if m.Summary != "" {
				fmt.Fprintf(&sb, "   %s\n", m.Summary)
			}
		}
		if len(memories) == 0 {
			sb.WriteString("No memories in this topic yet.\n")
		}
		return mcpText(sb.String())
	}

	topics, err := h.store.ListTopics(hubID)
	if err != nil {
		return mcpError(fmt.Sprintf("Failed to list topics: %s", err.Error()))
	}

	counts, _ := h.store.CountMemoriesByTopic(scope, hubID)
	unassigned, _ := h.store.CountUnassignedMemories(scope, hubID)

	var sb strings.Builder
	sb.WriteString("## Topics\n\n")

	if len(topics) == 0 {
		sb.WriteString("No topics yet. Push memories and run a dream cycle to auto-organize.\n")
	} else {
		for _, t := range topics {
			count := counts[t.ID]
			indent := ""
			if t.ParentID != nil {
				indent = "  "
			}
			fmt.Fprintf(&sb, "%s- **%s** (%d memories) [id: %s]\n", indent, t.Name, count, t.ID)
			if t.Description != "" {
				fmt.Fprintf(&sb, "%s  %s\n", indent, t.Description)
			}
		}
	}

	if unassigned > 0 {
		fmt.Fprintf(&sb, "\n📥 **Inbox**: %d unassigned memories\n", unassigned)
	}
	return mcpText(sb.String())
}

// --- Helpers ---

func (h *MCPHandler) accessibleMCPHubs(r *http.Request, ownerID string) ([]model.HubWithRole, error) {
	hubs, err := h.store.ListUserHubs(ownerID)
	if err != nil {
		return nil, err
	}

	accessibleIDs := GetAccessibleHubIDs(r)
	if len(accessibleIDs) == 0 {
		return hubs, nil
	}
	allowed := make(map[string]bool, len(accessibleIDs))
	for _, hubID := range accessibleIDs {
		allowed[hubID] = true
	}

	filtered := make([]model.HubWithRole, 0, len(hubs))
	for _, hub := range hubs {
		if allowed[hub.Hub.ID] {
			filtered = append(filtered, hub)
		}
	}
	return filtered, nil
}

func (h *MCPHandler) resolveMCPHub(r *http.Request, ownerID string, ref string) (model.HubWithRole, error) {
	hubs, err := h.accessibleMCPHubs(r, ownerID)
	if err != nil {
		return model.HubWithRole{}, err
	}

	normalizedRef := strings.ToLower(strings.TrimSpace(ref))
	if normalizedRef == "" {
		normalizedRef = strings.ToLower(strings.TrimSpace(GetHubID(r)))
	}
	if normalizedRef == "" {
		normalizedRef = "personal"
	}

	for _, hub := range hubs {
		switch {
		case strings.EqualFold(hub.Hub.ID, normalizedRef):
			return hub, nil
		case strings.EqualFold(hub.Hub.Slug, normalizedRef):
			return hub, nil
		case normalizedRef == "personal" && hub.Hub.HubType == "personal":
			return hub, nil
		}
	}
	return model.HubWithRole{}, fmt.Errorf("hub not found or not accessible")
}

func mcpHubReference(hub model.Hub) string {
	if hub.HubType == "personal" {
		return "personal"
	}
	return hub.Slug
}

func mcpMemberDisplayName(member model.HubMember) string {
	if member.UserName != "" {
		return member.UserName
	}
	if member.UserEmail != "" {
		return member.UserEmail
	}
	return member.UserID
}

// mcpRequirePermission returns a tool error when the credential lacks
// perm (in hubID, or in any hub when hubID is empty), and nil when it has
// it.
func mcpRequirePermission(r *http.Request, perm Permission, hubID string) *mcp.CallToolResult {
	if hubID != "" {
		if Can(r, perm, hubID) {
			return nil
		}
	} else if CanAny(r, perm) {
		return nil
	}
	slog.Warn("mcp tool denied by grant permission", "user_id", GetUserID(r), "permission", perm, "hub_id", hubID)
	return mcpError("This credential is not allowed to perform that Memax action.")
}

// mcpPushAttributionNote — MCP clients never see HTTP headers, so the
// claim-rejected warning rides in the tool result text.
func mcpPushAttributionNote(claimRejected bool) string {
	if claimRejected {
		return " Note: the agent identity claim was rejected; the memory is credited to the account owner."
	}
	return ""
}

// attributionErrorText renders a resolver error for a tool result: the
// resolver's own message when it is one of ours (it says how to fix the
// credential), a generic line otherwise.
func attributionErrorText(err error) string {
	if valErr, ok := asAttributionValidationError(err); ok {
		return valErr.message + "."
	}
	return "requested agent attribution conflicts with the authenticated agent."
}

// mcpInitiationType filters the caller-asserted initiation_type for MCP
// pushes. A tool call is by definition not a person at the keyboard, so a
// model asserting human_direct is dropped (the resolver then records the
// bound agent, or an unknown author) instead of being trusted into a
// human label.
func mcpInitiationType(requested string) string {
	if model.NormalizeMemoryInitiationType(strings.TrimSpace(requested)) == model.MemoryInitiationHumanDirect {
		return ""
	}
	return requested
}

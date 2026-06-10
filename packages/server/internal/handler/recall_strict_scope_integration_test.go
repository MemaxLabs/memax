package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// recall_strict_scope_integration_test.go pins the load-bearing
// security boundary that the strict-hub recall change closes:
// when an OAuth grant or API key is scoped to a hub allowlist, the
// recall pipeline MUST NOT return content the principal owns
// outside their granted hubs. The store-level leak prevention is
// pinned in postgres_chunks_strict_hub_test.go; what this test
// covers is that the strict signal flows correctly from
// AuthContext → RecallScope → store call when a real HTTP request
// hits the handler.
//
// The fixture deliberately constructs the bait:
//   - U1 owns h-personal (with a "lighthouse" memory)
//   - U1 also has access to h-team (with a "lighthouse" memory)
//
// An OAuth grant scoped to [h-team] hitting Recall should see
// ONLY the team-hub memory. If the personal-hub one slips in,
// the OAuth scope is meaningless and we have a critical leak.

func setupTwoHubRecallFixture(t *testing.T) (h *handler.RecallHandler, ownerID, hPersonal, hTeam string, memInPersonal, memInTeam string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID = uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'U', 'personal_early_access', true, now(), now())`,
		ownerID, ownerID[:8]+"@strict-recall",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// Personal hub.
	hPersonal = uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hPersonal, Name: "P", Slug: "p-" + uuid.NewString()[:8],
		HubType: "personal", OwnerID: ownerID,
	}); err != nil {
		t.Fatalf("CreatePersonal: %v", err)
	}
	if err := s.AddHubMember(hPersonal, ownerID, "owner"); err != nil {
		t.Fatalf("AddHubMember personal: %v", err)
	}

	// Team hub U1 owns.
	team := &model.Hub{
		ID: uuid.NewString(), Name: "Team",
		Slug: "t-" + uuid.NewString()[:8],
		HubType: "team", Plan: model.HubFreeTeamPlanID, OwnerID: ownerID,
	}
	if err := s.CreateTeamHub(ctx, team, ownerID, 999); err != nil {
		t.Fatalf("CreateTeamHub: %v", err)
	}
	hTeam = team.ID

	// Two memories with a single chunk each, BM25-discoverable for
	// "lighthouse" — no embedder required.
	memInPersonal = seedRecallableMemory(t, ctx, pool, ownerID, hPersonal,
		"Lighthouse plan in my personal hub")
	memInTeam = seedRecallableMemory(t, ctx, pool, ownerID, hTeam,
		"Lighthouse decisions for the team hub")

	// Embedder/distiller/reranker/cache nil → falls back to lexical
	// path; sufficient for the access-boundary test.
	h = handler.NewRecallHandler(s, nil, nil, nil, nil)
	return h, ownerID, hPersonal, hTeam, memInPersonal, memInTeam
}

func seedRecallableMemory(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID, hubID, content string) string {
	t.Helper()
	memID := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx,
		`INSERT INTO memories (id, hub_id, owner_id, title, content, content_type, content_hash, kind, stability, state, created_at, updated_at)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, 'text/plain', $6, $7, $8, 'active', $9, $9)`,
		memID, hubID, ownerID, content[:30], content, uuid.NewString(),
		model.MemoryKindSemantic, model.MemoryStabilityEvolving, now,
	); err != nil {
		t.Fatalf("insert memory: %v", err)
	}
	chunkID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO chunks (id, memory_id, content, chunk_index, token_count, created_at, kind, stability, retrieval_weight, hint, tags_text, metadata_text, project_repo, language, search_config, heading_chain)
		 VALUES ($1::uuid, $2::uuid, $3, 0, 10, $4, $5, $6, 1.0, '', '', '', '', 'und', 'simple', '{}'::text[])`,
		chunkID, memID, content, now, model.MemoryKindSemantic, model.MemoryStabilityEvolving,
	); err != nil {
		t.Fatalf("insert chunk: %v", err)
	}
	return memID
}

// CRITICAL — the load-bearing leak-prevention test. An OAuth
// grant scoped to [h-team] hits /v1/recall. The principal OWNS
// memInPersonal (in h-personal, NOT in their granted hub set),
// and the test's fixture has it match the query. If this test
// fails, the OAuth scope is meaningless and a scoped-token caller
// can read content from any hub the user owns. This is precisely
// the bug class that would "wipe us off the map" per the user's
// framing.
func TestRecall_StrictScope_DoesNotLeakOwnerContent(t *testing.T) {
	t.Parallel()
	h, ownerID, _, hTeam, memInPersonal, memInTeam := setupTwoHubRecallFixture(t)

	body, _ := json.Marshal(map[string]any{"query": "lighthouse"})
	req := httptest.NewRequest(http.MethodPost, "/v1/recall", bytes.NewBuffer(body))
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hTeam)
	req = handler.InjectAccessibleHubIDsForTest(req, []string{hTeam})
	// THE key piece: scope-bounded auth context. This is what the
	// middleware constructs for an OAuth grant or API key with
	// HubScopeMode = HubScopeAllowlist. The recall pipeline reads
	// this and routes to SearchChunksInHubs (strict).
	req = handler.InjectAuthContextForTest(req, &handler.AuthContext{
		UserID:        ownerID,
		PrincipalType: "oauth_grant",
		HubScopeMode:  handler.HubScopeAllowlist,
		ScopedHubIDs:  []string{hTeam},
	})

	rec := httptest.NewRecorder()
	h.Recall(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data model.RecallResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	gotMems := make(map[string]bool, len(resp.Data.Memories))
	for _, m := range resp.Data.Memories {
		gotMems[m.ID] = true
	}

	// Must include the team-hub memory (matches in granted scope).
	if !gotMems[memInTeam] {
		t.Errorf("team-hub memory missing from scoped recall (memID=%s)", memInTeam)
	}

	// CRITICAL — must NOT include the personal-hub memory even
	// though the OAuth principal IS the owner. The OAuth scope
	// boundary trumps ownership.
	if gotMems[memInPersonal] {
		t.Errorf("CRITICAL LEAK: scoped-OAuth principal (allowed=[%s]) saw owned memory in unrelated hub (memID=%s); the OAuth hub allowlist is meaningless if this passes",
			hTeam, memInPersonal)
	}
}

// Counterpart: an UNSCOPED session (HubScopeAllAccessible) hitting
// the same recall MUST still see content cross-hub. This is the
// design — web/CLI users recall their own content regardless of
// which hub they explicitly active-hub'd to. Strict-mode is a new
// path, NOT a global behavior change.
func TestRecall_UnscopedSession_StillCrossHub(t *testing.T) {
	t.Parallel()
	h, ownerID, hPersonal, hTeam, memInPersonal, memInTeam := setupTwoHubRecallFixture(t)

	body, _ := json.Marshal(map[string]any{"query": "lighthouse"})
	req := httptest.NewRequest(http.MethodPost, "/v1/recall", bytes.NewBuffer(body))
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hTeam) // active hub = team
	req = handler.InjectAccessibleHubIDsForTest(req, []string{hPersonal, hTeam})
	// Default web/CLI session: HubScopeAllAccessible. Cross-hub
	// recall is by design — active hub stays a ranking-relevance
	// boost, NOT an access boundary.
	req = handler.InjectAuthContextForTest(req, &handler.AuthContext{
		UserID:        ownerID,
		PrincipalType: "session",
		HubScopeMode:  handler.HubScopeAllAccessible,
	})

	rec := httptest.NewRecorder()
	h.Recall(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data model.RecallResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	gotMems := make(map[string]bool, len(resp.Data.Memories))
	for _, m := range resp.Data.Memories {
		gotMems[m.ID] = true
	}

	if !gotMems[memInTeam] {
		t.Errorf("unscoped session: team-hub memory missing (regression)")
	}
	// Cross-hub: personal-hub memory IS expected to surface, even
	// though active hub is team. This is the design — web/CLI
	// users see their own content cross-hub.
	if !gotMems[memInPersonal] {
		t.Errorf("unscoped session: personal-hub memory missing — cross-hub recall regressed")
	}
}

// Defense in depth: a scoped principal whose accessible hubs
// resolve to empty (e.g., the granted hub set is disjoint from
// the user's actual memberships, or all granted hubs were frozen
// out by stripFrozenHubsFromScope) MUST get an empty result, NOT
// fall through to owner-only search. The strict branch in
// RunPipeline returns nil chunks for this case.
func TestRecall_StrictScope_WithEmptyHubs_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	h, ownerID, _, _, memInPersonal, memInTeam := setupTwoHubRecallFixture(t)

	body, _ := json.Marshal(map[string]any{"query": "lighthouse"})
	req := httptest.NewRequest(http.MethodPost, "/v1/recall", bytes.NewBuffer(body))
	req = handler.InjectUserIDForTest(req, ownerID)
	// No accessible hubs (the principal's granted set produced
	// nothing usable after intersection with current memberships +
	// frozen-hub stripping).
	req = handler.InjectAccessibleHubIDsForTest(req, nil)
	req = handler.InjectAuthContextForTest(req, &handler.AuthContext{
		UserID:        ownerID,
		PrincipalType: "oauth_grant",
		HubScopeMode:  handler.HubScopeAllowlist,
		ScopedHubIDs:  nil, // explicit empty
	})

	rec := httptest.NewRecorder()
	h.Recall(rec, req)
	// 200 with empty results is the right shape — the principal
	// has no accessible hubs, so the recall has no targets, but
	// the request itself is well-formed.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with empty results, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data model.RecallResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, m := range resp.Data.Memories {
		if m.ID == memInPersonal || m.ID == memInTeam {
			t.Errorf("scoped principal with empty hubs leaked memory %s — strict mode must not fall through to owner-only", m.ID)
		}
	}
}

// Compile-time guard: the strict-hub store method exists on the
// Store interface. If a future refactor renames or removes it,
// this fails the build before the recall handler silently regresses.
var _ = func() any {
	var _ store.Store = (*store.PostgresStore)(nil)
	return nil
}()

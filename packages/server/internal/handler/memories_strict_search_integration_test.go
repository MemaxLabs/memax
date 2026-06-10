package handler_test

import (
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

// Sibling test to recall_strict_scope_integration_test.go but for
// the OTHER recall surface codex caught: /v1/memories/search and
// /v1/bar/search both call MemoriesHandler.searchMemoriesCore.
// Before commit closing this gap, that core path used owner-OR-hub
// semantics and leaked the same way recall did. This pins the fix
// end-to-end for /v1/memories/search; bar inherits the fix because
// it shares the same core.

func setupMemoriesSearchTwoHubFixture(t *testing.T) (h *handler.MemoriesHandler, ownerID, hPersonal, hTeam string, memInPersonal, memInTeam string) {
	t.Helper()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	ownerID = uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'U', 'personal_early_access', true, now(), now())`,
		ownerID, ownerID[:8]+"@strict-mem-search",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

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

	team := &model.Hub{
		ID: uuid.NewString(), Name: "Team",
		Slug: "t-" + uuid.NewString()[:8],
		HubType: "team", Plan: model.HubFreeTeamPlanID, OwnerID: ownerID,
	}
	if err := s.CreateTeamHub(ctx, team, ownerID, 999); err != nil {
		t.Fatalf("CreateTeamHub: %v", err)
	}
	hTeam = team.ID

	memInPersonal = seedMemoriesSearchMem(t, ctx, pool, ownerID, hPersonal,
		"Lighthouse plan in my personal hub")
	memInTeam = seedMemoriesSearchMem(t, ctx, pool, ownerID, hTeam,
		"Lighthouse decisions for the team hub")

	// All ingestion-side deps nil — Search only needs the store and
	// the embedder (nil falls through to lexical-only path, which
	// is what we want for the BM25-discoverable fixture).
	h = handler.NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	return h, ownerID, hPersonal, hTeam, memInPersonal, memInTeam
}

func seedMemoriesSearchMem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID, hubID, content string) string {
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

// CRITICAL — pins the leak fix for /v1/memories/search. Same shape
// as the recall test: scoped-OAuth principal allowed [h-team] only;
// owns memInPersonal in h-personal. Before fix → memInPersonal
// leaked. After fix → not in results.
func TestMemoriesSearch_StrictScope_DoesNotLeakOwnerContent(t *testing.T) {
	t.Parallel()
	h, ownerID, _, hTeam, memInPersonal, memInTeam := setupMemoriesSearchTwoHubFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/memories/search?q=lighthouse", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hTeam)
	req = handler.InjectAccessibleHubIDsForTest(req, []string{hTeam})
	req = handler.InjectAuthContextForTest(req, &handler.AuthContext{
		UserID:        ownerID,
		PrincipalType: "oauth_grant",
		HubScopeMode:  handler.HubScopeAllowlist,
		ScopedHubIDs:  []string{hTeam},
	})

	rec := httptest.NewRecorder()
	h.Search(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// memorySearchRow has shape {memory_id, ...}; we just check IDs.
	var resp struct {
		Data []struct {
			MemoryID string `json:"memory_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	gotMems := make(map[string]bool, len(resp.Data))
	for _, m := range resp.Data {
		gotMems[m.MemoryID] = true
	}

	if !gotMems[memInTeam] {
		t.Errorf("team-hub memory missing from scoped search (memID=%s)", memInTeam)
	}
	if gotMems[memInPersonal] {
		t.Errorf("CRITICAL LEAK: scoped principal saw owned content from non-granted hub via /v1/memories/search (memID=%s)", memInPersonal)
	}
}

// Cross-hub regression guard: unscoped session still spans both hubs.
func TestMemoriesSearch_UnscopedSession_StillCrossHub(t *testing.T) {
	t.Parallel()
	h, ownerID, hPersonal, hTeam, memInPersonal, memInTeam := setupMemoriesSearchTwoHubFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/memories/search?q=lighthouse", nil)
	req = handler.InjectUserIDForTest(req, ownerID)
	req = handler.InjectHubIDForTest(req, hTeam)
	req = handler.InjectAccessibleHubIDsForTest(req, []string{hPersonal, hTeam})
	req = handler.InjectAuthContextForTest(req, &handler.AuthContext{
		UserID:        ownerID,
		PrincipalType: "session",
		HubScopeMode:  handler.HubScopeAllAccessible,
	})

	rec := httptest.NewRecorder()
	h.Search(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []struct {
			MemoryID string `json:"memory_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	gotMems := make(map[string]bool, len(resp.Data))
	for _, m := range resp.Data {
		gotMems[m.MemoryID] = true
	}
	if !gotMems[memInTeam] || !gotMems[memInPersonal] {
		t.Errorf("unscoped /v1/memories/search lost cross-hub coverage: got=%v want both", gotMems)
	}
}

// Compile-time guard: the strict-hub store method exists.
var _ = func() any {
	var _ store.Store = (*store.PostgresStore)(nil)
	return nil
}()

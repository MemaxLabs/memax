package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// SearchChunksInHubs is the strict-hub variant the agent runtime
// uses for narrow recall (chat with explicit hub_id, dream agent
// per-hub). The test pins:
//
//   - The leak-prevention contract: a memory the user OWNS but
//     that lives OUTSIDE the target hubs MUST NOT appear in
//     results. This is the load-bearing security check Codex
//     flagged as a blocking finding on commit d6324ab5.
//   - The empty-hubIDs fail-closed contract: ErrEmptyHubIDsForStrictSearch
//     instead of falling through to owner-only.
//   - That a hub-team-mate's memory in a target hub IS returned
//     (the access boundary is hub membership, not ownership).

// strictHubFixture seeds:
//   - U1 with personal hub h-personal-u1
//   - U2 with personal hub h-personal-u2
//   - Team hub h-team owned by U1, with U2 as member
//   - Three memories:
//   - U1 owns one in h-personal-u1 (semantic match for "lighthouse")
//   - U1 owns one in h-team (semantic match for "lighthouse")
//   - U2 owns one in h-team (semantic match for "lighthouse")
//
// Test calls SearchChunksInHubs([h-team]). The leak-prevention rule
// says the U1-owned-personal-hub memory MUST NOT appear in results
// despite U1 being the implicit "user."
func strictHubFixture(t *testing.T, ctx context.Context, s store.Store, pool *pgxpool.Pool) (u1, u2, hPersonalU1, hPersonalU2, hTeam string, memInPersonalU1, memInTeamU1, memInTeamU2 string) {
	t.Helper()

	u1 = seedUser(t, pool, "strict-hub-u1")
	u2 = seedUser(t, pool, "strict-hub-u2")
	if _, err := pool.Exec(ctx,
		`UPDATE users SET can_create_hub = true WHERE id IN ($1::uuid, $2::uuid)`,
		u1, u2,
	); err != nil {
		t.Fatalf("set can_create_hub: %v", err)
	}

	hPersonalU1 = uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hPersonalU1, Name: "P1", Slug: "p1-" + uuid.NewString()[:8],
		HubType: "personal", OwnerID: u1,
	}); err != nil {
		t.Fatalf("create personal hub U1: %v", err)
	}
	hPersonalU2 = uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hPersonalU2, Name: "P2", Slug: "p2-" + uuid.NewString()[:8],
		HubType: "personal", OwnerID: u2,
	}); err != nil {
		t.Fatalf("create personal hub U2: %v", err)
	}

	team := &model.Hub{
		ID: uuid.NewString(), Name: "Team",
		Slug:    "team-" + uuid.NewString()[:8],
		HubType: "team", Plan: model.HubFreeTeamPlanID, OwnerID: u1,
	}
	if err := s.CreateTeamHub(ctx, team, u1, 999); err != nil {
		t.Fatalf("CreateTeamHub: %v", err)
	}
	hTeam = team.ID
	if err := s.AddHubMember(hTeam, u2, "contributor"); err != nil {
		t.Fatalf("AddHubMember U2: %v", err)
	}

	memInPersonalU1 = seedSearchableMemory(t, ctx, pool, u1, hPersonalU1, "lighthouse plan in personal hub")
	memInTeamU1 = seedSearchableMemory(t, ctx, pool, u1, hTeam, "lighthouse decisions team U1")
	memInTeamU2 = seedSearchableMemory(t, ctx, pool, u2, hTeam, "lighthouse decisions team U2")

	return u1, u2, hPersonalU1, hPersonalU2, hTeam, memInPersonalU1, memInTeamU1, memInTeamU2
}

// seedSearchableMemory creates a memory + a single chunk so the
// chunk-search path returns hits. We use lexical search (BM25 lane);
// no embedding required.
func seedSearchableMemory(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID, hubID, content string) string {
	t.Helper()
	memID := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx,
		`INSERT INTO memories (id, hub_id, owner_id, title, content, content_type, content_hash, kind, stability, state, created_at, updated_at)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, 'text/plain', $6, $7, $8, 'active', $9, $9)`,
		memID, hubID, ownerID, content[:min(len(content), 30)], content, uuid.NewString(),
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

// THE LOAD-BEARING TEST. SearchChunksInHubs called with [h-team]
// MUST return only hits from h-team — even though u1 owns memInPersonalU1
// (a non-target hub), it must NOT appear. This is the bug Codex caught
// in the recall_memories tool wrapper that the new strict-hub method
// fixes at the store layer.
func TestPostgresSearchChunksInHubs_DoesNotLeakOwnerContent(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	_, _, _, _, hTeam, memInPersonalU1, memInTeamU1, memInTeamU2 := strictHubFixture(t, ctx, s, pool)

	chunks, err := s.SearchChunksInHubs(ctx, "lighthouse", nil, []string{hTeam}, 10, nil, nil)
	if err != nil {
		t.Fatalf("SearchChunksInHubs: %v", err)
	}

	gotMems := make(map[string]bool, len(chunks))
	for _, c := range chunks {
		gotMems[c.MemoryID] = true
	}

	// Must include both memories that ARE in h-team.
	if !gotMems[memInTeamU1] {
		t.Errorf("missing memInTeamU1 (%s) — expected to match in target hub", memInTeamU1)
	}
	if !gotMems[memInTeamU2] {
		t.Errorf("missing memInTeamU2 (%s) — expected to match in target hub (team-mate's content)", memInTeamU2)
	}

	// CRITICAL — must NOT include U1's personal-hub memory even
	// though U1 owns it. Strict-hub semantics: the hub set is the
	// access boundary, NOT ownership. If this fails, the
	// recall_memories tool's narrow-to-hub contract is broken and
	// users see content from hubs they explicitly didn't ask for.
	if gotMems[memInPersonalU1] {
		t.Errorf("CRITICAL LEAK: memInPersonalU1 (%s) appeared in strict-hub search; user-owned content from non-target hub leaked", memInPersonalU1)
	}
}

func TestPostgresSearchChunksInHubs_RejectsEmptyHubIDs(t *testing.T) {
	t.Parallel()
	s, _ := testdb.Acquire(t)
	ctx := context.Background()

	_, err := s.SearchChunksInHubs(ctx, "x", nil, nil, 10, nil, nil)
	if !errors.Is(err, store.ErrEmptyHubIDsForStrictSearch) {
		t.Errorf("expected ErrEmptyHubIDsForStrictSearch for empty hubIDs, got %v", err)
	}
	// Also test []string{} (different from nil but same intent).
	_, err = s.SearchChunksInHubs(ctx, "x", nil, []string{}, 10, nil, nil)
	if !errors.Is(err, store.ErrEmptyHubIDsForStrictSearch) {
		t.Errorf("expected ErrEmptyHubIDsForStrictSearch for []string{}, got %v", err)
	}
}

// Multi-hub strict search returns hits from ALL target hubs but
// nothing outside them.
func TestPostgresSearchChunksInHubs_MultiHubReturnsUnion(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	_, _, _, _, hTeam, memInPersonalU1, memInTeamU1, _ := strictHubFixture(t, ctx, s, pool)

	// Add another team hub U1 owns so we can search across two
	// target hubs simultaneously.
	hTeamB := &model.Hub{
		ID: uuid.NewString(), Name: "TeamB",
		Slug:    "tb-" + uuid.NewString()[:8],
		HubType: "team", Plan: model.HubFreeTeamPlanID, OwnerID: getOwnerOf(t, ctx, pool, hTeam),
	}
	if err := s.CreateTeamHub(ctx, hTeamB, hTeamB.OwnerID, 999); err != nil {
		t.Fatalf("CreateTeamHub: %v", err)
	}
	memInTeamB := seedSearchableMemory(t, ctx, pool, hTeamB.OwnerID, hTeamB.ID, "lighthouse decisions team B")

	chunks, err := s.SearchChunksInHubs(ctx, "lighthouse", nil, []string{hTeam, hTeamB.ID}, 10, nil, nil)
	if err != nil {
		t.Fatalf("SearchChunksInHubs: %v", err)
	}
	gotMems := make(map[string]bool, len(chunks))
	for _, c := range chunks {
		gotMems[c.MemoryID] = true
	}

	if !gotMems[memInTeamU1] || !gotMems[memInTeamB] {
		t.Errorf("multi-hub search missed expected results. got=%v want both memInTeamU1=%s and memInTeamB=%s",
			gotMems, memInTeamU1, memInTeamB)
	}
	if gotMems[memInPersonalU1] {
		t.Errorf("multi-hub leak: memInPersonalU1 appeared")
	}
}

// getOwnerOf reads hub.owner_id directly. Convenience for the
// multi-hub test which doesn't carry the owner ID through the
// fixture return.
func getOwnerOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, hubID string) string {
	t.Helper()
	var ownerID string
	if err := pool.QueryRow(ctx,
		`SELECT owner_id::text FROM hubs WHERE id = $1::uuid`, hubID,
	).Scan(&ownerID); err != nil {
		t.Fatalf("read owner: %v", err)
	}
	return ownerID
}

// SearchChunksForHubs (the existing OR-style method) and
// SearchChunksInHubs (the new strict method) on the same fixture
// produce different results — the OR variant returns the personal-
// hub memory, the strict variant doesn't. This regression test
// pins the semantic difference so a future refactor can't quietly
// collapse the two methods.
func TestPostgresSearchChunksForHubs_VsInHubs_SemanticDifference(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	u1, _, _, _, hTeam, memInPersonalU1, memInTeamU1, _ := strictHubFixture(t, ctx, s, pool)

	// OR-variant: U1 owns memInPersonalU1, and h-team is in the
	// hubIDs list. The owner-OR-hub predicate matches BOTH
	// memInPersonalU1 (via owner) and memInTeamU1 (via hub).
	or, err := s.SearchChunksForHubs(ctx, "lighthouse", nil, u1, []string{hTeam}, 10, nil, nil)
	if err != nil {
		t.Fatalf("SearchChunksForHubs: %v", err)
	}
	orMems := make(map[string]bool)
	for _, c := range or {
		orMems[c.MemoryID] = true
	}
	if !orMems[memInPersonalU1] {
		t.Errorf("OR-variant should include U1-owned memInPersonalU1 (it's how /v1/recall works today)")
	}
	if !orMems[memInTeamU1] {
		t.Errorf("OR-variant should include hub-matched memInTeamU1")
	}

	// Strict variant: same hubIDs, but the OR is gone. Only the
	// h-team match returns; the personal-hub one is filtered out.
	strict, err := s.SearchChunksInHubs(ctx, "lighthouse", nil, []string{hTeam}, 10, nil, nil)
	if err != nil {
		t.Fatalf("SearchChunksInHubs: %v", err)
	}
	strictMems := make(map[string]bool)
	for _, c := range strict {
		strictMems[c.MemoryID] = true
	}
	if strictMems[memInPersonalU1] {
		t.Errorf("strict variant must NOT include memInPersonalU1; this is the security boundary")
	}
	if !strictMems[memInTeamU1] {
		t.Errorf("strict variant must include memInTeamU1")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

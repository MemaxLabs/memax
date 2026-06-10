// Full five-phase dream cycle integration test.
//
// Formerly build-tagged `integration`; now runs on any `go test`
// invocation that has DATABASE_URL set (so local dev with
// docker-compose and CI both exercise it). The test still
// self-skips when the env var is absent, keeping `go test ./...`
// green on machines without a live Postgres.
//
// This is the end-to-end contract test for the engine: one run,
// every phase, a real Postgres, and the observability hooks
// (usage_events, dream_run_id, heartbeat) verified against the
// actual DB state they produce. Unit tests cover the pure helpers;
// the Tier A/B commits each added narrow integration tests for a
// single invariant. This test ties them together: the whole
// lifecycle works end-to-end without regressions between commits.
//
// Build-tagged so unit runs (`go test ./...`) stay fast; CI and
// operators run with `-tags=integration` to include it.
//
// LLM calls hit a deterministic fake that reads the prompt purpose
// and returns canned responses. The fake embedder returns fixed
// vectors per input so similarity search in FindSimilarMemoriesByHub
// produces the pairs we intended to seed. This keeps the test
// hermetic — no network, no external API keys, no non-determinism.

package dreams

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// fakeLLM returns canned responses keyed by the prompt's Purpose
// field — the tracking string every dream phase sets. Concurrent-
// safe via a mutex so the test's per-phase heartbeat loops don't
// race.
type fakeLLM struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeLLM) Complete(_ context.Context, req anthropic.CompleteRequest) (*anthropic.CompleteResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	switch {
	case strings.Contains(req.Purpose, "merge") && !strings.Contains(req.Purpose, "resynthesize"):
		return &anthropic.CompleteResponse{
			Text:         "Merged content — combines both memories' facts.",
			InputTokens:  120,
			OutputTokens: 30,
		}, nil
	case strings.Contains(req.Purpose, "resynthesize_metadata"):
		return &anthropic.CompleteResponse{
			Text:         `{"title":"Merged fact","summary":"Combined summary.","tags":"merged"}`,
			InputTokens:  80,
			OutputTokens: 25,
		}, nil
	case strings.Contains(req.Purpose, "contradiction_detect"):
		return &anthropic.CompleteResponse{
			Text:         `{"contradiction":true,"reason":"Conflicting values stated."}`,
			InputTokens:  90,
			OutputTokens: 20,
		}, nil
	case strings.Contains(req.Purpose, "organize"):
		// Assign the seeded unassigned memory to the seeded topic —
		// IDs must match the fixture exactly (UUIDs must be real
		// UUIDs; Postgres rejects synthetic strings).
		return &anthropic.CompleteResponse{
			Text: `[{"memory_id":"00000000-0000-0000-0000-dd11dddd0004",` +
				`"topic_id":"00000000-0000-0000-0000-dd11cccc0001",` +
				`"confidence":0.9,"reason":"fits the AI cluster"}]`,
			InputTokens:  200,
			OutputTokens: 50,
		}, nil
	case strings.Contains(req.Purpose, "restructure"):
		// No suggested group — keeps the test focused on phases that
		// actually mutate and avoids depending on topic-restructure
		// heuristics that are a moving target.
		return &anthropic.CompleteResponse{
			Text:         `[]`,
			InputTokens:  60,
			OutputTokens: 10,
		}, nil
	}
	return &anthropic.CompleteResponse{Text: "", InputTokens: 10, OutputTokens: 1}, nil
}

// fakeEmbedder yields a 1024-dim vector derived from a SHA-256 of
// the input. 1024 matches the chunks.embedding column (voyage-code-3
// in production). Identical inputs produce identical vectors, so
// seeded "duplicate" memories land on the similarity threshold.
type fakeEmbedder struct{}

func (fakeEmbedder) Embed(texts []string, inputType string) ([][]float64, error) {
	return fakeEmbedder{}.EmbedContext(context.Background(), texts, inputType)
}

func (fakeEmbedder) EmbedContext(_ context.Context, texts []string, _ string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, t := range texts {
		// Canned vectors for contradiction-band pairs: two unit
		// vectors at roughly 41° apart (cos ≈ 0.755) land in the
		// similarity window [ContradictionThreshold, SimilarityThreshold)
		// that drives the contradiction phase.
		if strings.HasPrefix(t, "contradict-A:") {
			vec := make([]float64, 1024)
			vec[0] = 1.0
			out[i] = vec
			continue
		}
		if strings.HasPrefix(t, "contradict-B:") {
			vec := make([]float64, 1024)
			vec[0] = 0.755
			vec[1] = 0.656
			out[i] = vec
			continue
		}
		h := sha256.Sum256([]byte(t))
		// 1024 dims matches the chunks.embedding column (voyage-code-3).
		vec := make([]float64, 1024)
		for j := range vec {
			vec[j] = float64(h[j%32]) / 255.0
		}
		out[i] = vec
	}
	return out, nil
}

func (fakeEmbedder) Dimensions() int { return 1024 }

// fakeMeterRecorder captures every usage_events emit so the test
// can assert per-phase rows landed with the right op + metadata
// without depending on a real Redis / meter.
type fakeMeterRecorder struct {
	mu     sync.Mutex
	events []fakeMeterEvent
}

type fakeMeterEvent struct {
	UserID, Op, HubID, Source, AgentName string
	Metadata                             map[string]any
}

func (r *fakeMeterRecorder) LogEvent(userID, op, hubID, source, agentName string, metadata map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fakeMeterEvent{userID, op, hubID, source, agentName, metadata})
}

// fakePlanResolver always returns DreamsEnabled=true so the cycle
// is never gated by entitlements. Real plan resolution is covered
// by TestEvaluateDreamEligibility.
type fakePlanResolver struct{}

func (fakePlanResolver) GetUserLimits(_ context.Context, userID, planID string) model.UserLimits {
	return model.UserLimits{
		PlanID:          planID,
		PlanDisplayName: "Test",
		DreamsEnabled:   true,
	}
}

// TestDreamRunForActor_FullCycle is the single end-to-end contract
// test: every phase runs, every observability hook fires, and
// every Tier A/B invariant we've been defending holds in concert.
//
// Seeded fixture:
//   - 1 personal hub with a pre-created topic the organize phase
//     will assign into.
//   - 2 duplicate memories (same content → same embedding →
//     similarity 1.0, well above SimilarityThreshold).
//   - 1 archive candidate (accessed_at > 60 days ago,
//     access_count=0, stability=volatile).
//   - 1 unassigned memory the organize LLM will route into topic-ai.
func TestDreamRunForActor_FullCycle(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := store.NewPostgresStore(pool)

	// Disposable fixture IDs — deterministic so re-runs clean up
	// after themselves via the DELETE in t.Cleanup.
	ownerID := "00000000-0000-0000-0000-dd11aaaa0001"
	hubID := "00000000-0000-0000-0000-dd11bbbb0001"
	topicID := "00000000-0000-0000-0000-dd11cccc0001"
	memDupA := "00000000-0000-0000-0000-dd11dddd0001"
	memDupB := "00000000-0000-0000-0000-dd11dddd0002"
	memArchive := "00000000-0000-0000-0000-dd11dddd0003"
	memUnassigned := "00000000-0000-0000-0000-dd11dddd0004" // matches the fake LLM's organize response below
	now := time.Now().UTC()

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM notifications WHERE hub_id = $1::uuid`, hubID)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM dream_actions WHERE run_id IN (SELECT id FROM dream_runs WHERE hub_id = $1::uuid)`, hubID)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM dream_runs WHERE hub_id = $1::uuid`, hubID)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM usage_events WHERE hub_id = $1::uuid`, hubID)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM memory_topics WHERE memory_id IN (SELECT id FROM memories WHERE hub_id = $1::uuid)`, hubID)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM topics WHERE hub_id = $1::uuid`, hubID)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM memories WHERE hub_id = $1::uuid`, hubID)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM hubs WHERE id = $1::uuid`, hubID)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM users WHERE id = $1::uuid`, ownerID)
	})

	// Cleanup any row left behind by a prior failed run — order
	// matters for FK integrity (notifications → dream_runs →
	// dream_actions → memory_topics → memories → topics → hubs →
	// users).
	for _, stmt := range []string{
		`DELETE FROM notifications WHERE hub_id = $1::uuid`,
		`DELETE FROM dream_actions WHERE run_id IN (SELECT id FROM dream_runs WHERE hub_id = $1::uuid)`,
		`DELETE FROM dream_runs WHERE hub_id = $1::uuid`,
		`DELETE FROM usage_events WHERE hub_id = $1::uuid`,
		`DELETE FROM memory_topics WHERE memory_id IN (SELECT id FROM memories WHERE hub_id = $1::uuid)`,
		`DELETE FROM chunks WHERE memory_id IN (SELECT id FROM memories WHERE hub_id = $1::uuid)`,
		`DELETE FROM topics WHERE hub_id = $1::uuid`,
		`DELETE FROM memories WHERE hub_id = $1::uuid`,
		`DELETE FROM hubs WHERE id = $1::uuid`,
	} {
		if _, err := pool.Exec(ctx, stmt, hubID); err != nil {
			t.Fatalf("pre-cleanup %q: %v", stmt, err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, ownerID); err != nil {
		t.Fatalf("pre-cleanup users: %v", err)
	}

	// Seed user, hub, topic.
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, true, $6, $6)`,
		ownerID, "dream-integration@test.local", "dream-integration",
		"free", "personal_pro", now,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, settings, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, 'personal', $4::uuid, '{}'::jsonb, $5, $5)`,
		hubID, "dream-integration-hub", "dream-integration-hub", ownerID, now,
	); err != nil {
		t.Fatalf("seed hub: %v", err)
	}
	if err := s.CreateTopic(&model.Topic{
		ID: topicID, OwnerID: ownerID, HubID: hubID, Name: "topic-ai",
		Description: "AI-related", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed topic: %v", err)
	}

	// Seed memories.
	insertMemory := func(m *model.Memory) {
		t.Helper()
		if err := s.CreateMemory(m); err != nil {
			t.Fatalf("seed memory %s: %v", m.ID, err)
		}
	}
	// Duplicate pair — same Content/Title → same fake embedding →
	// similarity 1.0.
	insertMemory(&model.Memory{
		ID: memDupA, HubID: hubID, OwnerID: ownerID,
		Title: "Duplicate A", Content: "Go handles errors explicitly via return values.",
		ContentType: "text", ContentHash: "dup-A",
		Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0, Boundary: "private", State: "active",
		Tags:        []string{},
		AccessCount: 3,
		CreatedAt:   now.Add(-24 * time.Hour), UpdatedAt: now, AccessedAt: now,
	})
	insertMemory(&model.Memory{
		ID: memDupB, HubID: hubID, OwnerID: ownerID,
		Title: "Duplicate B", Content: "Go handles errors explicitly via return values.",
		ContentType: "text", ContentHash: "dup-B",
		Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0, Boundary: "private", State: "active",
		Tags:        []string{},
		AccessCount: 1,
		CreatedAt:   now.Add(-48 * time.Hour), UpdatedAt: now, AccessedAt: now,
	})
	// Archive candidate — access_count=0, old, decay at floor.
	insertMemory(&model.Memory{
		ID: memArchive, HubID: hubID, OwnerID: ownerID,
		Title: "Archive me", Content: "Outdated build instructions.",
		ContentType: "text", ContentHash: "archive-1",
		Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityVolatile,
		RetrievalWeight: 1.0, Boundary: "private", State: "active",
		Tags:        []string{},
		AccessCount: 0,
		CreatedAt:   now.Add(-180 * 24 * time.Hour), UpdatedAt: now,
		AccessedAt: now.Add(-100 * 24 * time.Hour),
	})
	// Unassigned memory for organize.
	insertMemory(&model.Memory{
		ID: memUnassigned, HubID: hubID, OwnerID: ownerID,
		Title: "Neural network basics", Content: "Multi-layer perceptrons are the foundation of deep learning.",
		ContentType: "text", ContentHash: "unassigned-1",
		Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0, Boundary: "private", State: "active",
		Tags:        []string{},
		AccessCount: 2,
		CreatedAt:   now, UpdatedAt: now, AccessedAt: now,
	})

	// Seed chunks + embeddings for the duplicate pair so
	// FindSimilarMemoriesByHub returns the pair. fakeEmbedder
	// gives identical vectors for identical content so the cosine
	// similarity is 1.0.
	embedder := fakeEmbedder{}
	seedChunksFor := func(memID, chunkID, content string) {
		t.Helper()
		vecs, err := embedder.EmbedContext(ctx, []string{content}, "document")
		if err != nil {
			t.Fatalf("embed: %v", err)
		}
		chunk := model.Chunk{
			ID: chunkID, MemoryID: memID,
			Content: content, ChunkIndex: 0, TokenCount: 10,
			Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0, Embedding: vecs[0], CreatedAt: now,
		}
		if err := s.CreateChunks([]model.Chunk{chunk}); err != nil {
			t.Fatalf("seed chunks for %s: %v", memID, err)
		}
	}
	seedChunksFor(memDupA, "00000000-0000-0000-0000-dd11eeee0001", "Go handles errors explicitly via return values.")
	seedChunksFor(memDupB, "00000000-0000-0000-0000-dd11eeee0002", "Go handles errors explicitly via return values.")

	// Construct the engine via New so txRunner + embedder + etc.
	// wire the same way production does, then swap the LLM client
	// for the fake. The fake satisfies the llmClient interface
	// introduced in this commit.
	fakeClient := &fakeLLM{}
	// New's signature requires *anthropic.Client, but the struct
	// field is an interface after the 3.1 refactor. Build directly
	// so we can inject the fake without touching the constructor's
	// real-client-required contract.
	// PostgresStore satisfies store.TxRunner by embedding; asserting
	// it here gives a loud failure if that ever drifts rather than
	// a segfault deep in a phase.
	var storeIface store.Store = s
	txRunner, ok := storeIface.(store.TxRunner)
	if !ok {
		t.Fatalf("store does not satisfy TxRunner")
	}
	eventsPublisher := events.NewPublisherFromEnv()
	e := &Engine{
		store:         s,
		txRunner:      txRunner,
		embedder:      embedder,
		client:        fakeClient,
		events:        eventsPublisher,
		model:         "haiku-test",
		organizeModel: "sonnet-test",
	}
	e.SetPlanResolver(fakePlanResolver{})
	recorder := &fakeMeterRecorder{}
	e.SetMeter(recorder)

	// Run the cycle.
	run, err := e.RunForActor(t.Context(), hubID, ownerID)
	if err != nil {
		t.Fatalf("RunForActor: %v", err)
	}

	// --- Status and basic counts ---

	if run.Status != model.DreamRunStatusCompleted && run.Status != model.DreamRunStatusPartialFailed {
		t.Fatalf("expected terminal status, got %q", run.Status)
	}
	if run.MemoriesScanned < 4 {
		t.Fatalf("expected ≥4 scanned, got %d", run.MemoriesScanned)
	}
	if run.DuplicatesMerged != 1 {
		t.Fatalf("expected 1 duplicate merge, got %d", run.DuplicatesMerged)
	}
	if run.MemoriesArchived < 1 {
		t.Fatalf("expected ≥1 archive, got %d", run.MemoriesArchived)
	}

	// --- Persistence: the run was written, not just returned ---

	storedRun, err := s.GetLatestDreamRunByHub(hubID)
	if err != nil {
		t.Fatalf("GetLatestDreamRunByHub: %v", err)
	}
	if storedRun.ID != run.ID {
		t.Fatalf("stored run id mismatch: %q vs %q", storedRun.ID, run.ID)
	}
	if storedRun.Status != run.Status {
		t.Fatalf("stored status mismatch: %q vs %q", storedRun.Status, run.Status)
	}
	if storedRun.LastHeartbeatAt == nil {
		t.Fatalf("expected heartbeat to be persisted (A4 follow-up)")
	}

	// --- Observability (B1): every enabled phase emits a
	// dream.<phase> usage event via the meter. We assert against the
	// fakeMeterRecorder capture — the DB-write side belongs to
	// meter.Meter, which has its own tests. Here we care about the
	// engine's contract with its injected meter.

	phaseOps := map[string]bool{
		"dream.merge":         false,
		"dream.contradiction": false,
		"dream.archive":       false,
		"dream.organize":      false,
		"dream.restructure":   false,
	}
	recorder.mu.Lock()
	events := append([]fakeMeterEvent(nil), recorder.events...)
	recorder.mu.Unlock()
	for _, ev := range events {
		if _, ok := phaseOps[ev.Op]; ok {
			phaseOps[ev.Op] = true
			if ev.Source != "dreams" {
				t.Fatalf("meter ev.source = %q, want dreams", ev.Source)
			}
			if ev.AgentName != "" {
				t.Fatalf("meter ev.agent_name = %q, want empty (system event)", ev.AgentName)
			}
		}
	}
	for op, seen := range phaseOps {
		if !seen {
			t.Fatalf("missing meter LogEvent for %s (B1)", op)
		}
	}

	// --- Traceability: notifications tagged with dream_run_id (B5) ---

	rows, err := pool.Query(ctx,
		`SELECT kind, dream_run_id::text FROM notifications WHERE hub_id = $1::uuid ORDER BY created_at`,
		hubID)
	if err != nil {
		t.Fatalf("query notifications: %v", err)
	}
	var taggedCount, untaggedCount int
	for rows.Next() {
		var kind string
		var drID *string
		if err := rows.Scan(&kind, &drID); err != nil {
			t.Fatalf("scan notification: %v", err)
		}
		if drID != nil && *drID == run.ID {
			taggedCount++
		} else {
			untaggedCount++
		}
	}
	rows.Close()
	if taggedCount == 0 {
		t.Fatalf("expected at least one notification tagged with dream_run_id=%s (B5)", run.ID)
	}

	// --- Phase metrics carry real numbers, not placeholders ---

	mergeMetrics, ok := storedRun.PhaseMetrics["merge"]
	if !ok {
		t.Fatalf("no merge phase metrics in stored run")
	}
	if mergeMetrics.LLMCalls < 2 {
		// merge LLM + resynthesize metadata LLM both must count
		// (B1 follow-up closed the undercount).
		t.Fatalf("merge.llm_calls = %d, expected ≥2 (merge+resynthesize)", mergeMetrics.LLMCalls)
	}
	if mergeMetrics.TokensIn == 0 || mergeMetrics.TokensOut == 0 {
		t.Fatalf("merge token counts should be non-zero: in=%d out=%d", mergeMetrics.TokensIn, mergeMetrics.TokensOut)
	}

	// Spot-check the receipt payload has Status (B5 follow-up).
	var dreamCompletedPayload []byte
	if err := pool.QueryRow(ctx,
		`SELECT payload FROM notifications WHERE hub_id = $1::uuid AND kind = 'dream_run_completed' LIMIT 1`,
		hubID).Scan(&dreamCompletedPayload); err == nil && len(dreamCompletedPayload) > 0 {
		var decoded model.DreamRunCompletedPayload
		if err := json.Unmarshal(dreamCompletedPayload, &decoded); err != nil {
			t.Fatalf("unmarshal receipt payload: %v", err)
		}
		if decoded.Status == "" {
			t.Fatalf("receipt payload.status is empty — should carry run.Status")
		}
	}

	// --- Heartbeat persisted + differs from started_at ---

	if !storedRun.LastHeartbeatAt.After(storedRun.StartedAt) {
		t.Fatalf("heartbeat should have advanced past started_at: started=%v heartbeat=%v",
			storedRun.StartedAt, storedRun.LastHeartbeatAt)
	}

	if fakeClient.calls == 0 {
		t.Fatalf("fake LLM was never called — phases did not exercise the LLM surface")
	}

	// --- Phase 2a soft-mode trigger scan: every active memory in
	// the hub gets a dream_trigger_decisions row, even non-fires.
	// The fixture seeds 4 memories without @TODO/@FIXME markers,
	// no source_fetch_hash, and no contradiction history → all
	// rows MUST be Kind=none Fired=false. We pin:
	//   - exactly one row per active memory in the hub
	//   - all rows have trigger_fired=false
	//   - all rows have trigger_kind='none'
	// so a partial-logging regression (missing rows) or a kind-
	// discriminator regression (firing on a clean fixture) both
	// fail the test loudly.
	var decisionCount, firedCount, nonNoneKinds int
	if err := pool.QueryRow(ctx, `
		SELECT
		    COUNT(*),
		    COUNT(*) FILTER (WHERE trigger_fired = true),
		    COUNT(*) FILTER (WHERE trigger_kind <> 'none')
		FROM dream_trigger_decisions WHERE dream_run_id = $1::uuid`,
		run.ID).Scan(&decisionCount, &firedCount, &nonNoneKinds); err != nil {
		t.Fatalf("count dream_trigger_decisions: %v", err)
	}
	// One row per active memory in the fixture. Fixture seeds 4
	// memories (memDupA, memDupB, memArchive, memUnassigned). The
	// merge phase runs BEFORE the trigger scan via the engine
	// order (similarity-scan → trigger-scan → merge-phase), but
	// the trigger scan reads from active_memories which is
	// observed at scan time, not after merge. So all 4 rows
	// should be present.
	const wantDecisions = 4
	if decisionCount != wantDecisions {
		t.Fatalf("dream_trigger_decisions count = %d, want %d (one per active memory in fixture)",
			decisionCount, wantDecisions)
	}
	if firedCount != 0 {
		t.Fatalf("dream_trigger_decisions: %d rows fired, want 0 (clean fixture has no triggers)", firedCount)
	}
	if nonNoneKinds != 0 {
		t.Fatalf("dream_trigger_decisions: %d rows with non-none kind, want 0 (clean fixture)", nonNoneKinds)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM dream_trigger_decisions WHERE dream_run_id = $1::uuid`, run.ID)
	})

	fmt.Fprintf(os.Stderr, "integration ok: %d LLM calls, %d meter events, tagged=%d untagged=%d, decisions=%d (fired=%d)\n",
		fakeClient.calls, len(events), taggedCount, untaggedCount, decisionCount, firedCount)
}

// TestDreamContradictionPath_ProducesTaggedReviews specifically
// exercises the contradiction-review-notification producer so B5's
// "every dream-produced row carries dream_run_id" contract holds
// end-to-end, not just for the completion receipt covered by the
// smoke test above.
//
// Canned embedder vectors place the seeded pair at cosine ≈ 0.755
// — inside [ContradictionThreshold, SimilarityThreshold), the
// band that drives contradiction detection (merge cut-off is 0.85).
// Fake LLM returns contradiction=true for every detect call so the
// review write path always fires.
func TestDreamContradictionPath_ProducesTaggedReviews(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping contradiction integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := store.NewPostgresStore(pool)

	ownerID := "00000000-0000-0000-0000-dd22aaaa0001"
	hubID := "00000000-0000-0000-0000-dd22bbbb0001"
	memA := "00000000-0000-0000-0000-dd22dddd0001"
	memB := "00000000-0000-0000-0000-dd22dddd0002"
	now := time.Now().UTC()

	cleanupSQL := []string{
		`DELETE FROM notifications WHERE hub_id = $1::uuid`,
		`DELETE FROM dream_actions WHERE run_id IN (SELECT id FROM dream_runs WHERE hub_id = $1::uuid)`,
		`DELETE FROM dream_runs WHERE hub_id = $1::uuid`,
		`DELETE FROM usage_events WHERE hub_id = $1::uuid`,
		`DELETE FROM chunks WHERE memory_id IN (SELECT id FROM memories WHERE hub_id = $1::uuid)`,
		`DELETE FROM memories WHERE hub_id = $1::uuid`,
		`DELETE FROM hubs WHERE id = $1::uuid`,
	}
	cleanup := func() {
		for _, stmt := range cleanupSQL {
			_, _ = pool.Exec(context.Background(), stmt, hubID)
		}
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1::uuid`, ownerID)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, true, $6, $6)`,
		ownerID, "dream-contradiction@test.local", "dream-contradiction",
		"free", "personal_pro", now); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, settings, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, 'personal', $4::uuid, '{}'::jsonb, $5, $5)`,
		hubID, "contradiction-hub", "contradiction-hub", ownerID, now); err != nil {
		t.Fatalf("seed hub: %v", err)
	}
	// Personal-hub creation normally auto-adds the owner as a
	// hub_member with role=owner. The raw SQL seed above bypasses
	// that flow, so do it explicitly — otherwise
	// contradictionDecisionRecipients returns empty and every pair
	// is skipped.
	if _, err := pool.Exec(ctx,
		`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'owner')
		 ON CONFLICT DO NOTHING`,
		hubID, ownerID); err != nil {
		t.Fatalf("seed hub member: %v", err)
	}

	insertMemory := func(m *model.Memory) {
		t.Helper()
		if err := s.CreateMemory(m); err != nil {
			t.Fatalf("seed memory %s: %v", m.ID, err)
		}
	}
	insertMemory(&model.Memory{
		ID: memA, HubID: hubID, OwnerID: ownerID,
		Title: "Claim A", Content: "contradict-A: fact one claims value X.",
		ContentType: "text", ContentHash: "ca",
		Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0, Boundary: "private", State: "active",
		Tags: []string{}, AccessCount: 1,
		CreatedAt: now, UpdatedAt: now, AccessedAt: now,
	})
	insertMemory(&model.Memory{
		ID: memB, HubID: hubID, OwnerID: ownerID,
		Title: "Claim B", Content: "contradict-B: fact one claims value Y instead.",
		ContentType: "text", ContentHash: "cb",
		Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0, Boundary: "private", State: "active",
		Tags: []string{}, AccessCount: 1,
		CreatedAt: now, UpdatedAt: now, AccessedAt: now,
	})

	embedder := fakeEmbedder{}
	seedChunk := func(memID, chunkID, content string) {
		t.Helper()
		vecs, err := embedder.EmbedContext(ctx, []string{content}, "document")
		if err != nil {
			t.Fatalf("embed: %v", err)
		}
		chunk := model.Chunk{
			ID: chunkID, MemoryID: memID, Content: content,
			ChunkIndex: 0, TokenCount: 10,
			Kind: model.MemoryKindSemantic, Stability: model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0, Embedding: vecs[0], CreatedAt: now,
		}
		if err := s.CreateChunks([]model.Chunk{chunk}); err != nil {
			t.Fatalf("seed chunk: %v", err)
		}
	}
	seedChunk(memA, "00000000-0000-0000-0000-dd22eeee0001", "contradict-A: fact one claims value X.")
	seedChunk(memB, "00000000-0000-0000-0000-dd22eeee0002", "contradict-B: fact one claims value Y instead.")

	var storeIface store.Store = s
	txRunner, ok := storeIface.(store.TxRunner)
	if !ok {
		t.Fatalf("TxRunner: not satisfied")
	}
	e := &Engine{
		store:         s,
		txRunner:      txRunner,
		embedder:      embedder,
		client:        &fakeLLM{},
		events:        events.NewPublisherFromEnv(),
		model:         "haiku-test",
		organizeModel: "sonnet-test",
	}
	e.SetPlanResolver(fakePlanResolver{})
	e.SetMeter(&fakeMeterRecorder{})

	run, err := e.RunForActor(t.Context(), hubID, ownerID)
	if err != nil {
		t.Fatalf("RunForActor: %v", err)
	}
	if run.ContradictionsFound == 0 {
		t.Fatalf("expected at least one contradiction detected, got %+v", run.PhaseMetrics["contradiction"])
	}

	// Every review_contradiction notification for this hub must
	// carry dream_run_id matching the run that produced it (B5).
	rows, err := pool.Query(ctx,
		`SELECT dream_run_id::text FROM notifications WHERE hub_id = $1::uuid AND kind = 'review_contradiction'`,
		hubID)
	if err != nil {
		t.Fatalf("query review_contradiction notifications: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var drID *string
		if err := rows.Scan(&drID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if drID == nil || *drID != run.ID {
			t.Fatalf("review_contradiction notification missing dream_run_id: got %v, want %s", drID, run.ID)
		}
		count++
	}
	if count == 0 {
		t.Fatalf("expected at least one review_contradiction notification to be tagged")
	}
}

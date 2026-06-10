package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// TestTopicsSameHubParentTrigger verifies migration 013's trigger
// rejects a cross-hub parent_id. Without the trigger, the existing
// FK only checks the referenced row exists; nothing stops a topic
// in hub A from naming a parent in hub B, which would let downstream
// walkers (GetTopicDepth, GetSubtreeMaxDepth) traverse into the
// wrong tenant's tree.
func TestTopicsSameHubParentTrigger(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping same-hub trigger test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	ownerID := "00000000-0000-0000-0000-ff00aaaa0001"
	hubA := "00000000-0000-0000-0000-ff00bbbb0001"
	hubB := "00000000-0000-0000-0000-ff00bbbb0002"
	parentTopic := "00000000-0000-0000-0000-ff00cccc0001"
	childTopic := "00000000-0000-0000-0000-ff00cccc0002"
	now := time.Now().UTC()

	// Seed fixtures.
	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, true, $6, $6)
		 ON CONFLICT (id) DO NOTHING`,
		ownerID, "topic-scope@test.local", "topic-scope@test.local",
		"free", "personal_free", now,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	for _, hubID := range []string{hubA, hubB} {
		_, err = pool.Exec(ctx,
			`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
			 VALUES ($1::uuid, $2, $2, 'personal', $3::uuid, $4, $4)
			 ON CONFLICT (id) DO NOTHING`,
			hubID, "topic-scope-"+hubID[len(hubID)-4:], ownerID, now,
		)
		if err != nil {
			t.Fatalf("seed hub %s: %v", hubID, err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM topics WHERE hub_id IN ($1::uuid, $2::uuid)`, hubA, hubB); err != nil {
		t.Fatalf("cleanup topics: %v", err)
	}

	// Parent lives in hubA.
	if err := s.CreateTopic(&model.Topic{
		ID:        parentTopic,
		OwnerID:   ownerID,
		HubID:     hubA,
		Name:      "Parent in A",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed parent topic: %v", err)
	}

	// Attempt to create a child in hubB that points to the hubA
	// parent — trigger must reject with SQLSTATE 23514.
	err = s.CreateTopic(&model.Topic{
		ID:        childTopic,
		OwnerID:   ownerID,
		HubID:     hubB,
		ParentID:  &parentTopic,
		Name:      "Cross-hub child",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err == nil {
		t.Fatalf("CreateTopic with cross-hub parent must fail")
	}
	if !strings.Contains(err.Error(), "must be in the same hub") {
		t.Fatalf("unexpected error from trigger: %v", err)
	}

	// Same hubA child must work — ruling out a false positive.
	sameHubChild := "00000000-0000-0000-0000-ff00cccc0003"
	if err := s.CreateTopic(&model.Topic{
		ID:        sameHubChild,
		OwnerID:   ownerID,
		HubID:     hubA,
		ParentID:  &parentTopic,
		Name:      "Same-hub child",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("same-hub child should be accepted: %v", err)
	}

	// GetTopicDepth with hubA returns 1 (one ancestor inside hubA).
	depth, err := s.GetTopicDepth(sameHubChild, hubA)
	if err != nil {
		t.Fatalf("GetTopicDepth in hubA: %v", err)
	}
	if depth != 1 {
		t.Fatalf("expected depth 1 in hubA, got %d", depth)
	}
	// Querying the same topic scoped to hubB returns 0 — the walk
	// never crosses the hub boundary, so nothing is visible.
	depth, err = s.GetTopicDepth(sameHubChild, hubB)
	if err != nil {
		t.Fatalf("GetTopicDepth in hubB: %v", err)
	}
	if depth != 0 {
		t.Fatalf("expected depth 0 when scoped to wrong hub, got %d", depth)
	}

	// Cleanup.
	if _, err := pool.Exec(ctx, `DELETE FROM topics WHERE hub_id IN ($1::uuid, $2::uuid)`, hubA, hubB); err != nil {
		t.Logf("cleanup topics: %v", err)
	}
}

// TestTopicsParentHubIDChangeRejected covers the v2 trigger's other
// half (migration 015): changing a parent topic's hub_id must be
// rejected when children still reference the row from a different
// hub. Without this branch, a bulk admin move of a parent topic
// would leave its children cross-hub-dangling — the invariant the
// whole trigger exists to preserve.
func TestTopicsParentHubIDChangeRejected(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping parent hub_id change test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	ownerID := "00000000-0000-0000-0000-ff00aaaa0002"
	hubA := "00000000-0000-0000-0000-ff00bbbb0003"
	hubB := "00000000-0000-0000-0000-ff00bbbb0004"
	parentID := "00000000-0000-0000-0000-ff00cccc0010"
	childID := "00000000-0000-0000-0000-ff00cccc0011"
	now := time.Now().UTC()

	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, true, $6, $6) ON CONFLICT (id) DO NOTHING`,
		ownerID, "parent-move@test.local", "parent-move@test.local",
		"free", "personal_free", now,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	for _, hubID := range []string{hubA, hubB} {
		_, err = pool.Exec(ctx,
			`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at)
			 VALUES ($1::uuid, $2, $2, 'personal', $3::uuid, $4, $4) ON CONFLICT (id) DO NOTHING`,
			hubID, "parent-move-"+hubID[len(hubID)-4:], ownerID, now,
		)
		if err != nil {
			t.Fatalf("seed hub %s: %v", hubID, err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM topics WHERE hub_id IN ($1::uuid, $2::uuid)`, hubA, hubB); err != nil {
		t.Fatalf("cleanup topics: %v", err)
	}

	// Seed a parent in hubA with a child in hubA.
	if err := s.CreateTopic(&model.Topic{
		ID: parentID, OwnerID: ownerID, HubID: hubA, Name: "Parent", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := s.CreateTopic(&model.Topic{
		ID: childID, OwnerID: ownerID, HubID: hubA, ParentID: &parentID,
		Name: "Child", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed child: %v", err)
	}

	// Try to move the parent's hub_id to hubB. Child still references
	// it from hubA → trigger must reject.
	_, err = pool.Exec(ctx,
		`UPDATE topics SET hub_id = $1::uuid WHERE id = $2::uuid`, hubB, parentID)
	if err == nil {
		t.Fatalf("UPDATE parent.hub_id should be rejected while children exist in old hub")
	}
	if !strings.Contains(err.Error(), "child topic") {
		t.Fatalf("unexpected error from v2 trigger: %v", err)
	}

	// Once the child is removed (or moved along), the parent move
	// should succeed. Lock this down too — the trigger must not
	// over-reject.
	if _, err := pool.Exec(ctx, `DELETE FROM topics WHERE id = $1::uuid`, childID); err != nil {
		t.Fatalf("delete child: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE topics SET hub_id = $1::uuid WHERE id = $2::uuid`, hubB, parentID); err != nil {
		t.Fatalf("parent move after child removed should succeed: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM topics WHERE hub_id IN ($1::uuid, $2::uuid)`, hubA, hubB); err != nil {
		t.Logf("cleanup topics: %v", err)
	}
}

// TestHubSettingsRoundTrip covers migration 016's new column:
// CreateHub persists hub.Settings; GetHub + GetHubBySlug scan it
// back. This is the plumbing that makes per-hub dream toggles
// (Tier B #4) possible — the dream engine reads hub.Settings via
// GetHub in resolveHubContext.
func TestHubSettingsRoundTrip(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping hub settings round-trip test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	ownerID := "00000000-0000-0000-0000-ff00aaaa0003"
	hubID := "00000000-0000-0000-0000-ff00bbbb0010"
	now := time.Now().UTC()

	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, true, $6, $6) ON CONFLICT (id) DO NOTHING`,
		ownerID, "hub-settings@test.local", "hub-settings@test.local",
		"free", "personal_free", now,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1::uuid`, hubID); err != nil {
		t.Fatalf("cleanup hub: %v", err)
	}

	hub := &model.Hub{
		ID:        hubID,
		Name:      "Settings test hub",
		Slug:      "hub-settings-test",
		HubType:   "team",
		OwnerID:   ownerID,
		Settings:  map[string]any{"dreams_merge_enabled": false, "dreams_organize_enabled": true},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatalf("CreateHub with settings: %v", err)
	}

	got, err := s.GetHub(hubID)
	if err != nil {
		t.Fatalf("GetHub: %v", err)
	}
	if got.Settings == nil {
		t.Fatalf("GetHub should populate Settings, got nil")
	}
	if got.Settings["dreams_merge_enabled"] != false {
		t.Fatalf("dreams_merge_enabled = %v, want false", got.Settings["dreams_merge_enabled"])
	}
	if got.Settings["dreams_organize_enabled"] != true {
		t.Fatalf("dreams_organize_enabled = %v, want true", got.Settings["dreams_organize_enabled"])
	}

	// GetHubBySlug must also surface the column.
	gotBySlug, err := s.GetHubBySlug(hub.Slug)
	if err != nil {
		t.Fatalf("GetHubBySlug: %v", err)
	}
	if gotBySlug.Settings["dreams_merge_enabled"] != false {
		t.Fatalf("GetHubBySlug lost settings: %v", gotBySlug.Settings)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1::uuid`, hubID); err != nil {
		t.Logf("cleanup hub: %v", err)
	}
}

// TestPatchHubSettingsConcurrent covers commit 3.6's atomic JSONB
// merge path. Two goroutines race partial patches of different
// keys; a read-merge-write implementation would last-write-wins
// and drop one of the two toggles. The jsonb_strip_nulls / ||
// merge keeps both.
func TestPatchHubSettingsConcurrent(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping concurrent patch test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	ownerID := "00000000-0000-0000-0000-ff00aaaa0005"
	hubID := "00000000-0000-0000-0000-ff00bbbb0012"
	now := time.Now().UTC()

	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, true, $6, $6) ON CONFLICT (id) DO NOTHING`,
		ownerID, "patch-concurrent@test.local", "patch-concurrent@test.local",
		"free", "personal_free", now,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1::uuid`, hubID); err != nil {
		t.Fatalf("cleanup hub: %v", err)
	}

	hub := &model.Hub{
		ID:        hubID,
		Name:      "Concurrent patch hub",
		Slug:      "concurrent-patch-hub",
		HubType:   "team",
		OwnerID:   ownerID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1::uuid`, hubID)
	})

	// Two rival patches, staged, then released simultaneously.
	// The iteration count matters: a single race isn't always
	// enough to expose a read-merge-write bug, but looping
	// converges quickly.
	for i := 0; i < 20; i++ {
		if _, err := pool.Exec(ctx,
			`UPDATE hubs SET settings = '{}'::jsonb WHERE id = $1::uuid`, hubID); err != nil {
			t.Fatalf("reset iteration %d: %v", i, err)
		}
		done := make(chan error, 2)
		start := make(chan struct{})
		go func() {
			<-start
			done <- s.PatchHubSettings(ctx, hubID, map[string]any{"dreams_merge_enabled": false})
		}()
		go func() {
			<-start
			done <- s.PatchHubSettings(ctx, hubID, map[string]any{"dreams_archive_enabled": false})
		}()
		close(start)
		for k := 0; k < 2; k++ {
			if err := <-done; err != nil {
				t.Fatalf("iteration %d patch: %v", i, err)
			}
		}
		got, err := s.GetHub(hubID)
		if err != nil {
			t.Fatalf("iteration %d GetHub: %v", i, err)
		}
		if got.Settings["dreams_merge_enabled"] != false {
			t.Fatalf("iteration %d dropped dreams_merge_enabled: %v", i, got.Settings)
		}
		if got.Settings["dreams_archive_enabled"] != false {
			t.Fatalf("iteration %d dropped dreams_archive_enabled: %v", i, got.Settings)
		}
	}
}

// TestPatchHubSettingsNullDeletesKey covers null-semantics on
// Postgres: a null in the patch must delete the existing key via
// jsonb_strip_nulls. In-process tests cover this with a map
// delete; verifying the SQL matches is the point.
func TestPatchHubSettingsNullDeletesKey(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping patch-null test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	ownerID := "00000000-0000-0000-0000-ff00aaaa0006"
	hubID := "00000000-0000-0000-0000-ff00bbbb0013"
	now := time.Now().UTC()

	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, true, $6, $6) ON CONFLICT (id) DO NOTHING`,
		ownerID, "patch-null@test.local", "patch-null@test.local",
		"free", "personal_free", now,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1::uuid`, hubID); err != nil {
		t.Fatalf("cleanup hub: %v", err)
	}

	hub := &model.Hub{
		ID:      hubID,
		Name:    "Patch null hub",
		Slug:    "patch-null-hub",
		HubType: "team",
		OwnerID: ownerID,
		Settings: map[string]any{
			"dreams_merge_enabled":   false,
			"dreams_archive_enabled": false,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1::uuid`, hubID)
	})

	if err := s.PatchHubSettings(ctx, hubID, map[string]any{"dreams_merge_enabled": nil}); err != nil {
		t.Fatalf("PatchHubSettings null: %v", err)
	}
	got, err := s.GetHub(hubID)
	if err != nil {
		t.Fatalf("GetHub: %v", err)
	}
	if _, present := got.Settings["dreams_merge_enabled"]; present {
		t.Fatalf("null patch should delete key, got %v", got.Settings)
	}
	if got.Settings["dreams_archive_enabled"] != false {
		t.Fatalf("untouched key lost: %v", got.Settings)
	}
}

// TestListUserHubsSurfacesSettings covers commit 3.6's widening of
// ListUserHubs to include hub.Settings. The per-hub overrides UI in
// Settings → Intelligence calls useHubs() (which hits this method)
// and needs hub.Settings populated to render the current per-hub
// override state. Without this wiring the web UI would silently
// think every hub was at defaults.
func TestListUserHubsSurfacesSettings(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping list-user-hubs settings test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)
	ownerID := "00000000-0000-0000-0000-ff00aaaa0004"
	hubID := "00000000-0000-0000-0000-ff00bbbb0011"
	now := time.Now().UTC()

	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, name, plan, personal_plan_id, can_create_hub, created_at, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, true, $6, $6) ON CONFLICT (id) DO NOTHING`,
		ownerID, "list-hubs-settings@test.local", "list-hubs-settings@test.local",
		"free", "personal_free", now,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// Clean any prior run artifacts before we re-seed.
	if _, err := pool.Exec(ctx, `DELETE FROM hub_members WHERE hub_id = $1::uuid`, hubID); err != nil {
		t.Fatalf("cleanup hub_members: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1::uuid`, hubID); err != nil {
		t.Fatalf("cleanup hub: %v", err)
	}

	hub := &model.Hub{
		ID:      hubID,
		Name:    "List settings hub",
		Slug:    "list-settings-hub",
		HubType: "team",
		OwnerID: ownerID,
		Settings: map[string]any{
			"dreams_merge_enabled":       false,
			"dreams_restructure_enabled": true,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	if err := s.AddHubMember(hubID, ownerID, "owner"); err != nil {
		t.Fatalf("AddHubMember: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM hub_members WHERE hub_id = $1::uuid`, hubID)
		_, _ = pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1::uuid`, hubID)
	})

	hubs, err := s.ListUserHubs(ownerID)
	if err != nil {
		t.Fatalf("ListUserHubs: %v", err)
	}
	var found *model.HubWithRole
	for i := range hubs {
		if hubs[i].Hub.ID == hubID {
			found = &hubs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("ListUserHubs did not return the seeded hub")
	}
	if found.Hub.Settings == nil {
		t.Fatalf("ListUserHubs should populate Settings, got nil")
	}
	if got := found.Hub.Settings["dreams_merge_enabled"]; got != false {
		t.Fatalf("dreams_merge_enabled = %v, want false", got)
	}
	if got := found.Hub.Settings["dreams_restructure_enabled"]; got != true {
		t.Fatalf("dreams_restructure_enabled = %v, want true", got)
	}
}

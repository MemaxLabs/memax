package store

import (
	"context"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func TestInMemoryHubLifecycle(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	hub := &model.Hub{
		ID: "h1", Name: "Hub One", Slug: "h1",
		HubType: "personal", OwnerID: "u1",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	got, err := s.GetHub("h1")
	if err != nil {
		t.Fatalf("GetHub: %v", err)
	}
	if got.Slug != "h1" {
		t.Errorf("slug = %q", got.Slug)
	}

	// GetHubBySlug.
	bySlug, err := s.GetHubBySlug("h1")
	if err != nil {
		t.Fatalf("GetHubBySlug: %v", err)
	}
	if bySlug.ID != "h1" {
		t.Errorf("slug lookup: %+v", bySlug)
	}

	// UpdateHub.
	hub.Name = "renamed"
	if err := s.UpdateHub(hub); err != nil {
		t.Fatalf("UpdateHub: %v", err)
	}
	got, _ = s.GetHub("h1")
	if got.Name != "renamed" {
		t.Errorf("update didn't persist: %q", got.Name)
	}

	// DeleteHub — the dev-store DeleteHub is a stub that doesn't
	// mutate the map. Just exercise the code path.
	if err := s.DeleteHub("h1"); err != nil {
		t.Fatalf("DeleteHub: %v", err)
	}
}

func TestInMemoryHubMemberOperations(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	s.CreateHub(&model.Hub{ID: "h1", OwnerID: "u1", HubType: "team", Slug: "h1"})

	// The InMemoryStore's member methods are dev-mode stubs that
	// return nil / empty without mutating state. We just exercise
	// them here so regressions that make them panic get caught.
	_ = s.AddHubMember("h1", "u1", "owner")
	_ = s.AddHubMember("h1", "u2", "member")
	if _, err := s.ListHubMembers("h1"); err != nil {
		t.Errorf("ListHubMembers: %v", err)
	}
	if _, err := s.GetHubMemberRole("h1", "u2"); err != nil {
		t.Errorf("GetHubMemberRole: %v", err)
	}
	if _, err := s.CountHubMembers("h1"); err != nil {
		t.Errorf("CountHubMembers: %v", err)
	}
	_ = s.UpdateHubMemberRole("h1", "u2", "admin")
	_ = s.RemoveHubMember("h1", "u2")
	_, _ = s.ListUserHubs("u1")
	_, _ = s.GetHubMemberCap(context.Background(), "h1")
}

func TestInMemoryCreateTeamHubWithOwner(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	hub := &model.Hub{
		ID: "t1", Name: "Team", Slug: "team",
		HubType: "team", OwnerID: "u1",
		Plan: model.HubFreeTeamPlanID,
	}
	if err := s.CreateTeamHub(context.Background(), hub, "u1", 3); err != nil {
		t.Fatalf("CreateTeamHub: %v", err)
	}
	// Owner should be auto-added as member.
	role, _ := s.GetHubMemberRole("t1", "u1")
	if role == "" {
		t.Error("owner should be a member after CreateTeamHub")
	}
}

func TestInMemoryPersonalHubHelpers(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	s.CreateHub(&model.Hub{ID: "h1", OwnerID: "u1", HubType: "personal", Slug: "p"})

	hub, err := s.GetPersonalHub("u1")
	if err != nil {
		t.Fatalf("GetPersonalHub: %v", err)
	}
	if hub == nil || hub.ID != "h1" {
		t.Errorf("personal hub not found: %+v", hub)
	}
}

func TestInMemoryHubVisits(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	s.CreateHub(&model.Hub{ID: "h1", OwnerID: "u1", HubType: "personal", Slug: "p"})

	now := time.Now()
	if err := s.UpsertHubVisit("u1", "h1", now); err != nil {
		t.Fatalf("UpsertHubVisit: %v", err)
	}
	v, err := s.GetHubVisit("u1", "h1")
	if err != nil {
		t.Fatalf("GetHubVisit: %v", err)
	}
	if v == nil {
		t.Fatal("visit should be non-nil after upsert")
	}
}

func TestInMemoryTopicVisits(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	now := time.Now()
	if err := s.UpsertTopicVisit("u1", "t1", "h1", now); err != nil {
		t.Fatalf("UpsertTopicVisit: %v", err)
	}
	v, err := s.GetTopicVisit("u1", "t1")
	// Dev-store stub returns not-found error if not tracked; accept
	// either nil-with-error or non-nil row.
	_ = v
	_ = err
}

func TestInMemoryHubInviteLifecycle(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	// CreateHubInvite / Get / GetByToken are stubs in the dev store —
	// just exercise them.
	_ = s.CreateHubInvite(&model.HubInvite{ID: "i1", HubID: "h1"})
	_, _ = s.GetHubInvite("i1")
	_, _ = s.GetHubInviteByToken("token-x")
}

func TestInMemoryGetAccessibleMemory(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	m := &model.Memory{ID: "m1", OwnerID: "u1", HubID: "h1", Title: "T"}
	_ = s.CreateMemory(m)

	// Owner can fetch.
	got, err := s.GetAccessibleMemory("m1", "u1", nil)
	if err != nil {
		t.Fatalf("owner access: %v", err)
	}
	if got.ID != "m1" {
		t.Errorf("got %+v", got)
	}
	// Shared hub member can fetch.
	got, err = s.GetAccessibleMemory("m1", "u2", []string{"h1"})
	if err != nil {
		t.Errorf("hub member access: %v", err)
	}
	// Non-member cannot.
	if _, err := s.GetAccessibleMemory("m1", "u2", []string{"other"}); err == nil {
		t.Error("non-member should not have access")
	}
}

func TestInMemoryGetAccessibleChunksByMemory(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	m := &model.Memory{ID: "m1", OwnerID: "u1", HubID: "h1"}
	_ = s.CreateMemory(m)
	_ = s.CreateChunks([]model.Chunk{{ID: "c1", MemoryID: "m1"}})

	chunks, err := s.GetAccessibleChunksByMemory("m1", "u1", nil)
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestInMemoryListDistinctOwners(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	// Empty store → empty result, no error.
	owners, err := s.ListDistinctOwners()
	if err != nil {
		t.Errorf("ListDistinctOwners: %v", err)
	}
	_ = owners
}

func TestInMemoryCountRecentMemoriesByHub(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	n, err := s.CountRecentMemoriesByHub(VisibilityScope{OwnerID: "u1"}, "h1", time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Errorf("CountRecentMemoriesByHub: %v", err)
	}
	_ = n
}

func TestInMemoryPatchHubSettings(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	s.CreateHub(&model.Hub{ID: "h1", OwnerID: "u1", HubType: "team", Slug: "p"})
	patch := map[string]any{"dreams_enabled": false}
	if err := s.PatchHubSettings(context.Background(), "h1", patch); err != nil {
		t.Fatalf("PatchHubSettings: %v", err)
	}
}

func TestInMemoryLifecycleResolvers(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	scope := VisibilityScope{OwnerID: "u1"}
	// Empty store → empty results.
	list, err := s.ResolveMemoryLifecycleForList(context.Background(), scope, "u1", []string{"m1"})
	if err != nil {
		t.Errorf("ResolveMemoryLifecycleForList: %v", err)
	}
	_ = list
	detail, err := s.ResolveMemoryLifecycleForDetail(context.Background(), scope, "u1", "m1")
	if err != nil {
		t.Errorf("ResolveMemoryLifecycleForDetail: %v", err)
	}
	_ = detail
	topics, err := s.ResolveTopicLifecycle(context.Background(), scope, "u1", []string{"t1"})
	if err != nil {
		t.Errorf("ResolveTopicLifecycle: %v", err)
	}
	_ = topics
}

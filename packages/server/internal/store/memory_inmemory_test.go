package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Tests below target the InMemoryStore methods that have real logic
// (not simple stubs). The InMemoryStore isn't used in production but
// it's the default Store implementation for unit tests of handlers and
// worker code — regressions here surface as silent failures in those
// tests, so pin the round-trip semantics.

func TestInMemoryStoreAuthIdentityLifecycle(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	// CreateAuthIdentity inserts a new row for a user.
	if err := s.CreateAuthIdentity("u1", "github", "1001", "a@b", "A", ""); err != nil {
		t.Fatalf("CreateAuthIdentity (insert): %v", err)
	}

	// Idempotent: same user re-claiming the same (provider, providerID)
	// is a no-op, not an error.
	if err := s.CreateAuthIdentity("u1", "github", "1001", "a@b", "A", ""); err != nil {
		t.Errorf("idempotent re-insert: %v", err)
	}

	// Conflict: different user trying to claim the same identity →
	// ErrIdentityConflict so the caller can surface a "this account is
	// linked elsewhere" error.
	if err := s.CreateAuthIdentity("u2", "github", "1001", "a@b", "A", ""); !errors.Is(err, ErrIdentityConflict) {
		t.Errorf("conflict: got %v, want ErrIdentityConflict", err)
	}

	// ListAuthIdentities returns the single row.
	ids, err := s.ListAuthIdentities("u1")
	if err != nil {
		t.Fatalf("ListAuthIdentities: %v", err)
	}
	if len(ids) != 1 {
		t.Errorf("expected 1 identity, got %d", len(ids))
	}

	// Delete by provider — only the github one should go.
	if err := s.CreateAuthIdentity("u1", "google", "2002", "", "", ""); err != nil {
		t.Fatalf("add google: %v", err)
	}
	if err := s.DeleteAuthIdentity("u1", "github"); err != nil {
		t.Fatalf("DeleteAuthIdentity: %v", err)
	}
	ids, _ = s.ListAuthIdentities("u1")
	if len(ids) != 1 || ids[0].Provider != "google" {
		t.Errorf("after delete, expected only google, got %+v", ids)
	}

	// Delete of missing identity is an error.
	if err := s.DeleteAuthIdentity("u1", "nonexistent"); err == nil {
		t.Error("expected error for missing identity delete")
	}
}

func TestInMemoryStoreGetUserByEmailAndIdentity(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	u := &model.User{ID: "u1", Email: "  Alice@X.Test  ", Name: "Alice"}
	s.AddUser(u)

	// GetUserByCanonicalEmail normalizes whitespace + case.
	got, err := s.GetUserByCanonicalEmail("ALICE@X.TEST")
	if err != nil {
		t.Fatalf("GetUserByCanonicalEmail: %v", err)
	}
	if got.ID != "u1" {
		t.Errorf("wrong user: %+v", got)
	}

	// Missing email.
	if _, err := s.GetUserByCanonicalEmail("nobody@x.test"); err == nil {
		t.Error("expected not-found error")
	}

	// Auth-identity lookup requires the identity row.
	if err := s.CreateAuthIdentity("u1", "github", "1001", "", "", ""); err != nil {
		t.Fatalf("CreateAuthIdentity: %v", err)
	}
	got, err = s.GetUserByAuthIdentity("github", "1001")
	if err != nil {
		t.Fatalf("GetUserByAuthIdentity: %v", err)
	}
	if got.ID != "u1" {
		t.Errorf("identity lookup: wrong user: %+v", got)
	}

	// Missing identity.
	if _, err := s.GetUserByAuthIdentity("github", "9999"); err == nil {
		t.Error("expected not-found for missing identity")
	}
}

func TestInMemoryStoreOAuthStateLifecycle(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()

	state := model.OAuthState{
		StateHash: "hash-abc",
		Provider:  "github",
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	if err := s.CreateOAuthState(ctx, state); err != nil {
		t.Fatalf("CreateOAuthState: %v", err)
	}

	// Consume once — returns the state.
	got, err := s.ConsumeOAuthState(ctx, "hash-abc")
	if err != nil {
		t.Fatalf("ConsumeOAuthState: %v", err)
	}
	if got.StateHash != "hash-abc" {
		t.Errorf("wrong state returned: %+v", got)
	}
	if got.ConsumedAt == nil {
		t.Error("ConsumedAt should be set after consume")
	}

	// Re-consume: single-use — errors.
	if _, err := s.ConsumeOAuthState(ctx, "hash-abc"); err == nil {
		t.Error("re-consume should error")
	}

	// Unknown state errors.
	if _, err := s.ConsumeOAuthState(ctx, "not-a-state"); err == nil {
		t.Error("unknown state should error")
	}

	// Expired state — create + advance the wall clock by having expiry in the past.
	expired := model.OAuthState{
		StateHash: "hash-old",
		Provider:  "github",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	_ = s.CreateOAuthState(ctx, expired)
	if _, err := s.ConsumeOAuthState(ctx, "hash-old"); err == nil {
		t.Error("expired state should error")
	}

	// CleanupExpiredOAuthStates sweeps — returns nonzero since we just
	// seeded one expired state (now deleted by the ConsumeOAuthState
	// above, so maybe zero). Either way, no error and no panic.
	if _, err := s.CleanupExpiredOAuthStates(ctx); err != nil {
		t.Errorf("CleanupExpiredOAuthStates: %v", err)
	}
}

func TestInMemoryStoreGetAccessibleMemories(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()

	// Seed memories owned by u1 (in h1) and u2 (in h2).
	m1 := &model.Memory{ID: "m1", OwnerID: "u1", HubID: "h1"}
	m2 := &model.Memory{ID: "m2", OwnerID: "u2", HubID: "h2"}
	m3 := &model.Memory{ID: "m3", OwnerID: "u2", HubID: "h1"} // u2 posted to shared h1
	for _, m := range []*model.Memory{m1, m2, m3} {
		if err := s.CreateMemory(m); err != nil {
			t.Fatalf("CreateMemory %s: %v", m.ID, err)
		}
	}

	// u1 in h1 sees m1 (their own) + m3 (same hub). m2 is in h2 (not
	// accessible).
	got, err := s.GetAccessibleMemories([]string{"m1", "m2", "m3"}, "u1", []string{"h1"})
	if err != nil {
		t.Fatalf("GetAccessibleMemories: %v", err)
	}
	if _, ok := got["m2"]; ok {
		t.Error("m2 should not leak to u1 (different hub)")
	}
	if _, ok := got["m1"]; !ok {
		t.Error("m1 should be visible (own memory)")
	}
	if _, ok := got["m3"]; !ok {
		t.Error("m3 should be visible (shared hub)")
	}

	// GetMemoryForAdmin bypasses ownership checks.
	adm, err := s.GetMemoryForAdmin("m2")
	if err != nil {
		t.Fatalf("GetMemoryForAdmin: %v", err)
	}
	if adm.ID != "m2" {
		t.Errorf("admin lookup: got %+v", adm)
	}
	if _, err := s.GetMemoryForAdmin("nothere"); err == nil {
		t.Error("admin lookup of missing id should error")
	}
}

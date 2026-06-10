package store

import (
	"context"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Smoke tests for the InMemoryStore stubs in memory_waitlist_admin.go.
// All dev-mode stubs — these tests exist to catch regressions that
// would panic handler unit tests relying on the shared Store interface.

func TestInMemoryWaitlistStubs(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()

	// Every method exercised — errors are expected (stubs return
	// "not supported"); we care about panics only.
	_ = s.CreateWaitlistEntry(ctx, &model.WaitlistEntry{})
	_, _ = s.GetWaitlistEntry(ctx, "id")
	_, _ = s.GetWaitlistEntryByEmail(ctx, "x@y")
	_, _, _, _ = s.ListWaitlistEntries(ctx, model.WaitlistListOpts{})
	_, _ = s.UpdateWaitlistEntry(ctx, "id", "approved", "notes", nil, "admin-uuid")
	_, _ = s.BatchApproveWaitlist(ctx, []string{"id"}, 1, "admin", "note")
	_, _ = s.GetWaitlistStats(ctx)
	_ = s.CreateWaitlistInvite(ctx, &model.WaitlistInvite{})
	_, _ = s.GetWaitlistInviteByToken(ctx, "token")
	_ = s.ConsumeWaitlistInvite(ctx, "token", "u1")
	_, _ = s.ExpireWaitlistInvites(ctx)
	_, _ = s.ListWaitlistInvitesByEntry(ctx, "entry")
	_, _ = s.ListOrphanWaitlistCandidates(ctx, model.OrphanWaitlistCandidateOpts{})
	_, _ = s.GetOrphanCandidateForUser(ctx, "u1")
	_ = s.RevokeWaitlistInvite(ctx, "invite")
	_, _ = s.GetActiveInviteForEntry(ctx, "entry")
	_, _ = s.ListExpiringWaitlistInvites(ctx, 24)
}

func TestInMemoryCanCreateHub(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()

	// Dev store: always returns true and never errors on set.
	_ = s.SetCanCreateHub(ctx, "u1", false)
	got, err := s.GetCanCreateHub(ctx, "u1")
	if err != nil {
		t.Errorf("GetCanCreateHub: %v", err)
	}
	if !got {
		t.Error("dev store should always allow hub creation")
	}
}

func TestInMemoryAdminRoleStubs(t *testing.T) {
	t.Parallel()
	s := NewInMemoryStore()
	ctx := context.Background()

	// GetAdminRole returns "" in dev-mode.
	role, err := s.GetAdminRole(ctx, "u1")
	if err != nil {
		t.Errorf("GetAdminRole: %v", err)
	}
	if role != "" {
		t.Errorf("dev-mode admin role = %q, want empty", role)
	}

	roles, err := s.ListAdminRoles(ctx)
	if err != nil {
		t.Errorf("ListAdminRoles: %v", err)
	}
	_ = roles

	n, err := s.EnsureAdminRolesByEmail(ctx, []string{"a@b"}, "super_admin")
	if err != nil {
		t.Errorf("EnsureAdminRolesByEmail: %v", err)
	}
	if n != 0 {
		t.Errorf("dev-store should report 0 grants, got %d", n)
	}
}

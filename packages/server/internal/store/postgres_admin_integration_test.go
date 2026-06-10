package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func TestPostgresAdminRoleLifecycle(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	// Seed three users with two admin-eligible emails and one non-admin.
	var userIDs []string
	emails := []string{
		"ADMIN1@memax.test",    // uppercase + trimmed by EnsureAdminRolesByEmail
		"  admin2@memax.test ", // whitespace
		"user@memax.test",      // not an admin
	}
	for _, em := range emails {
		id := uuid.NewString()
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
			 VALUES ($1::uuid, $2, 'A', 'personal_early_access', now(), now())`,
			id, strings.ToLower(strings.TrimSpace(em)),
		); err != nil {
			t.Fatalf("seed user %s: %v", em, err)
		}
		userIDs = append(userIDs, id)
	}

	// --- Before grant: no admin roles.
	role, err := s.GetAdminRole(ctx, userIDs[0])
	if err != nil {
		t.Fatalf("GetAdminRole (absent): %v", err)
	}
	if role != "" {
		t.Errorf("empty admin role expected before grant, got %q", role)
	}
	list, err := s.ListAdminRoles(ctx)
	if err != nil {
		t.Fatalf("ListAdminRoles: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("pre-grant: %d roles, want 0", len(list))
	}

	// --- EnsureAdminRolesByEmail normalizes and grants only the two
	// admin-eligible addresses. Idempotent via ON CONFLICT DO NOTHING.
	n, err := s.EnsureAdminRolesByEmail(ctx,
		[]string{"admin1@memax.test", "ADMIN2@memax.test", "nonexistent@memax.test"},
		"super_admin")
	if err != nil {
		t.Fatalf("EnsureAdminRolesByEmail: %v", err)
	}
	if n != 2 {
		t.Errorf("first grant: n = %d, want 2", n)
	}

	// Re-run: idempotent, reports 0 inserts.
	n, err = s.EnsureAdminRolesByEmail(ctx,
		[]string{"admin1@memax.test", "admin2@memax.test"},
		"super_admin")
	if err != nil {
		t.Fatalf("EnsureAdminRolesByEmail (idempotent): %v", err)
	}
	if n != 0 {
		t.Errorf("idempotent grant: n = %d, want 0", n)
	}

	// --- GetAdminRole reports super_admin for the granted users.
	role, _ = s.GetAdminRole(ctx, userIDs[0])
	if role != "super_admin" {
		t.Errorf("admin[0] role = %q, want super_admin", role)
	}

	// Non-admin user still empty.
	role, _ = s.GetAdminRole(ctx, userIDs[2])
	if role != "" {
		t.Errorf("non-admin user got role: %q", role)
	}

	// --- ListAdminRoles returns two rows.
	list, _ = s.ListAdminRoles(ctx)
	if len(list) != 2 {
		t.Errorf("ListAdminRoles returned %d, want 2", len(list))
	}

	// --- Empty email slice short-circuits (no SQL executed).
	n, err = s.EnsureAdminRolesByEmail(ctx, nil, "super_admin")
	if err != nil {
		t.Errorf("empty slice should be no-op, got %v", err)
	}
	if n != 0 {
		t.Errorf("empty slice returned %d", n)
	}
}

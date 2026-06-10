package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// Email brand is a singleton row seeded by migration 007. Tests exercise
// the Get / partial-patch Update flow — nil fields must leave values
// untouched, non-nil must overwrite.
func TestPostgresEmailBrandSettingsPartialPatch(t *testing.T) {
	t.Parallel()
	base, pool := testdb.Acquire(t)
	s, ok := base.(*store.PostgresStore)
	if !ok {
		t.Fatalf("not a *PostgresStore: %T", base)
	}
	ctx := context.Background()

	// Seed an admin for updated_by FK.
	adminID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'B', 'personal_early_access', now(), now())`,
		adminID, adminID[:8]+"@brand",
	); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	// --- GetEmailBrandSettings reads the seeded singleton row.
	orig, err := s.GetEmailBrandSettings(ctx)
	if err != nil {
		t.Fatalf("GetEmailBrandSettings: %v", err)
	}
	if orig.ProductName == "" {
		t.Error("seed row should have a product name")
	}

	// --- Partial patch: change only PrimaryColor and SupportEmail.
	// All other fields should retain their seeded values.
	newPrimary := "#ff0000"
	newSupport := "help@memax.app"
	patch := model.EmailBrandSettingsPatch{
		PrimaryColor: &newPrimary,
		SupportEmail: &newSupport,
	}
	updated, err := s.UpdateEmailBrandSettings(ctx, patch, &adminID)
	if err != nil {
		t.Fatalf("UpdateEmailBrandSettings: %v", err)
	}
	if updated.PrimaryColor != newPrimary {
		t.Errorf("primary_color = %q, want %q", updated.PrimaryColor, newPrimary)
	}
	if updated.SupportEmail != newSupport {
		t.Errorf("support_email = %q, want %q", updated.SupportEmail, newSupport)
	}
	// Untouched fields must match the original — COALESCE($X, col)
	// semantics are load-bearing for the partial-update contract.
	if updated.ProductName != orig.ProductName {
		t.Errorf("product_name drift: %q vs %q", updated.ProductName, orig.ProductName)
	}
	if updated.BackgroundColor != orig.BackgroundColor {
		t.Errorf("background_color drift: %q vs %q", updated.BackgroundColor, orig.BackgroundColor)
	}
	if updated.UpdatedBy == nil || *updated.UpdatedBy != adminID {
		t.Errorf("updated_by not set: %v", updated.UpdatedBy)
	}

	// --- Second patch with ALL nils leaves values untouched.
	noop, err := s.UpdateEmailBrandSettings(ctx, model.EmailBrandSettingsPatch{}, &adminID)
	if err != nil {
		t.Fatalf("UpdateEmailBrandSettings (noop): %v", err)
	}
	if noop.PrimaryColor != newPrimary {
		t.Errorf("noop patch regressed primary_color: %q", noop.PrimaryColor)
	}
}

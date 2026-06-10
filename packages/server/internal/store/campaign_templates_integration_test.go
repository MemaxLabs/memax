package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func TestPostgresCampaignTemplateLifecycle(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	// Seed admin for created_by/updated_by FK.
	adminID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'CT', 'personal_early_access', now(), now())`,
		adminID, adminID[:8]+"@ct",
	); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	tmpl := &model.CampaignTemplate{
		Slug:        "welcome-" + uuid.NewString()[:8],
		Name:        "Welcome",
		Description: "New user welcome email",
		Subject:     "Welcome to {{product}}",
		BodyHTML:    "<p>hi</p>",
		BodyText:    "hi",
		CreatedBy:   &adminID,
		UpdatedBy:   &adminID,
	}

	// --- Create ---
	if err := s.CreateCampaignTemplate(ctx, tmpl); err != nil {
		t.Fatalf("CreateCampaignTemplate: %v", err)
	}
	if tmpl.ID == "" {
		t.Fatal("CreateCampaignTemplate didn't set ID")
	}

	// --- GetBySlug finds it ---
	got, err := s.GetCampaignTemplateBySlug(ctx, tmpl.Slug)
	if err != nil {
		t.Fatalf("GetCampaignTemplateBySlug: %v", err)
	}
	if got.ID != tmpl.ID {
		t.Errorf("slug lookup returned wrong row")
	}

	// --- GetByID finds it ---
	got, err = s.GetCampaignTemplate(ctx, tmpl.ID)
	if err != nil {
		t.Fatalf("GetCampaignTemplate: %v", err)
	}
	if got.Name != "Welcome" {
		t.Errorf("round-trip lost name: %q", got.Name)
	}

	// --- Unknown slug / id → ErrCampaignTemplateNotFound ---
	if _, err := s.GetCampaignTemplateBySlug(ctx, "nope"); !errors.Is(err, store.ErrCampaignTemplateNotFound) {
		t.Errorf("missing slug: got %v, want ErrCampaignTemplateNotFound", err)
	}
	if _, err := s.GetCampaignTemplate(ctx, uuid.NewString()); !errors.Is(err, store.ErrCampaignTemplateNotFound) {
		t.Errorf("missing id: got %v, want ErrCampaignTemplateNotFound", err)
	}

	// --- Duplicate slug → ErrCampaignTemplateSlugTaken ---
	dup := &model.CampaignTemplate{
		Slug: tmpl.Slug, Name: "Conflict",
		Subject: "s", BodyHTML: "h", BodyText: "t",
		CreatedBy: &adminID, UpdatedBy: &adminID,
	}
	if err := s.CreateCampaignTemplate(ctx, dup); !errors.Is(err, store.ErrCampaignTemplateSlugTaken) {
		t.Errorf("duplicate slug: got %v, want ErrCampaignTemplateSlugTaken", err)
	}

	// --- List default: is_archived=false filter ---
	list, err := s.ListCampaignTemplates(ctx, model.CampaignTemplateListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListCampaignTemplates: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("list returned %d, want 1", len(list))
	}

	// --- Update applies patch, returns fresh row ---
	patch := model.CampaignTemplateUpsert{
		Slug:        "irrelevant",
		Name:        "Welcome v2",
		Description: "updated",
		Subject:     "Hi!",
		BodyHTML:    "<p>hi v2</p>",
		BodyText:    "hi v2",
	}
	updated, err := s.UpdateCampaignTemplate(ctx, tmpl.ID, patch, &adminID)
	if err != nil {
		t.Fatalf("UpdateCampaignTemplate: %v", err)
	}
	if updated.Name != "Welcome v2" {
		t.Errorf("update didn't persist name: %q", updated.Name)
	}
	// Slug is immutable; patch.Slug is ignored by design.
	if updated.Slug != tmpl.Slug {
		t.Errorf("update mutated slug: %q vs %q", updated.Slug, tmpl.Slug)
	}

	// --- Update missing ID → ErrCampaignTemplateNotFound ---
	if _, err := s.UpdateCampaignTemplate(ctx, uuid.NewString(), patch, &adminID); !errors.Is(err, store.ErrCampaignTemplateNotFound) {
		t.Errorf("update missing: got %v, want ErrCampaignTemplateNotFound", err)
	}

	// --- Archive toggle + filter ---
	archived, err := s.SetCampaignTemplateArchived(ctx, tmpl.ID, true, &adminID)
	if err != nil {
		t.Fatalf("SetCampaignTemplateArchived(true): %v", err)
	}
	if !archived.IsArchived {
		t.Error("archive flag not set")
	}
	// Default list excludes archived.
	list, _ = s.ListCampaignTemplates(ctx, model.CampaignTemplateListOpts{Limit: 10})
	if len(list) != 0 {
		t.Errorf("default list included archived template (%d rows)", len(list))
	}
	// IncludeArchived surfaces it.
	list, _ = s.ListCampaignTemplates(ctx, model.CampaignTemplateListOpts{
		Limit: 10, IncludeArchived: true,
	})
	if len(list) != 1 {
		t.Errorf("include-archived list: %d, want 1", len(list))
	}

	// --- Delete ---
	if err := s.DeleteCampaignTemplate(ctx, tmpl.ID); err != nil {
		t.Fatalf("DeleteCampaignTemplate: %v", err)
	}
	if err := s.DeleteCampaignTemplate(ctx, tmpl.ID); !errors.Is(err, store.ErrCampaignTemplateNotFound) {
		t.Errorf("re-delete: got %v, want ErrCampaignTemplateNotFound", err)
	}
}

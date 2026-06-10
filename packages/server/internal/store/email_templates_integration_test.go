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

func TestPostgresEmailTemplateOverrideLifecycle(t *testing.T) {
	t.Parallel()
	base, pool := testdb.Acquire(t)
	// Email-template methods aren't all on the Store interface; cast
	// to the concrete PostgresStore to hit the direct methods.
	s, ok := base.(*store.PostgresStore)
	if !ok {
		t.Fatalf("acquired store is not *PostgresStore: %T", base)
	}
	ctx := context.Background()

	// Seed an admin user for updated_by FK.
	adminID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'E', 'personal_early_access', now(), now())`,
		adminID, adminID[:8]+"@et",
	); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	adminStr := adminID

	// --- Absent override: Get returns (nil, nil) per contract.
	got, err := s.GetEmailTemplateOverride(ctx, "welcome")
	if err != nil {
		t.Fatalf("GetEmailTemplateOverride (absent): %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for absent override, got %+v", got)
	}

	// --- Publishing an absent override → ErrEmailTemplateNotFound.
	if _, err := s.PublishEmailTemplateOverride(ctx, "welcome", &adminStr); !errors.Is(err, store.ErrEmailTemplateNotFound) {
		t.Errorf("publish absent: got %v, want ErrEmailTemplateNotFound", err)
	}

	// --- Upsert (first save): INSERT path mirrors draft → published
	// so a never-published override still produces legible sends.
	override := &model.EmailTemplateOverride{
		Name:         "welcome",
		DraftSubject: "Hi {{name}}",
		DraftHTML:    "<p>hi</p>",
		DraftText:    "hi",
		Notes:        "first draft",
		EditorKind:   "rich",
		EditorState:  map[string]any{"k": "v"},
		UpdatedBy:    &adminStr,
	}
	if err := s.UpsertEmailTemplateOverride(ctx, override); err != nil {
		t.Fatalf("UpsertEmailTemplateOverride (insert): %v", err)
	}
	got, _ = s.GetEmailTemplateOverride(ctx, "welcome")
	if got == nil {
		t.Fatal("expected override after insert")
	}
	// Insert path mirrors draft → published.
	if got.Subject != "Hi {{name}}" || got.HTML != "<p>hi</p>" {
		t.Errorf("insert didn't mirror draft→published: %+v", got)
	}
	// published_at stays NULL until Publish runs.
	if got.PublishedAt != nil {
		t.Errorf("published_at should be nil on first save, got %v", got.PublishedAt)
	}
	// HasUnpublishedDraft: false on first insert (draft == published).
	if got.HasUnpublishedDraft() {
		t.Error("first insert should leave draft == published")
	}

	// --- Second save (UPDATE path): draft-only, published stays frozen.
	override.DraftSubject = "Hey {{name}}!"
	override.DraftHTML = "<p>hey</p>"
	if err := s.UpsertEmailTemplateOverride(ctx, override); err != nil {
		t.Fatalf("UpsertEmailTemplateOverride (update): %v", err)
	}
	got, _ = s.GetEmailTemplateOverride(ctx, "welcome")
	if got.DraftSubject != "Hey {{name}}!" {
		t.Errorf("update didn't persist draft: %q", got.DraftSubject)
	}
	if got.Subject != "Hi {{name}}" {
		t.Errorf("published Subject must not advance on draft save: %q", got.Subject)
	}
	if !got.HasUnpublishedDraft() {
		t.Error("after draft-only save, HasUnpublishedDraft should be true")
	}

	// --- PublishEmailTemplateOverride promotes draft → published.
	pub, err := s.PublishEmailTemplateOverride(ctx, "welcome", &adminStr)
	if err != nil {
		t.Fatalf("PublishEmailTemplateOverride: %v", err)
	}
	if pub.Subject != "Hey {{name}}!" {
		t.Errorf("publish didn't advance Subject: %q", pub.Subject)
	}
	if pub.PublishedAt == nil {
		t.Error("published_at should be set after publish")
	}

	// --- Idempotent republish: draft already matches published → no-op.
	pub2, err := s.PublishEmailTemplateOverride(ctx, "welcome", &adminStr)
	if err != nil {
		t.Fatalf("idempotent publish: %v", err)
	}
	if !pub2.PublishedAt.Equal(*pub.PublishedAt) {
		t.Error("idempotent publish must not advance published_at")
	}

	// --- ListEmailTemplateOverrides returns the row.
	list, err := s.ListEmailTemplateOverrides(ctx)
	if err != nil {
		t.Fatalf("ListEmailTemplateOverrides: %v", err)
	}
	if len(list) != 1 || list[0].Name != "welcome" {
		t.Errorf("list = %+v", list)
	}

	// --- Revision history captures saved + published actions.
	revs, err := s.ListEmailTemplateRevisions(ctx, "welcome", 10)
	if err != nil {
		t.Fatalf("ListEmailTemplateRevisions: %v", err)
	}
	// Two saves + one publish = 3 revisions.
	if len(revs) != 3 {
		t.Errorf("expected 3 revisions (save, save, publish), got %d", len(revs))
	}

	// --- Delete removes the override and logs a "reset" revision.
	if err := s.DeleteEmailTemplateOverride(ctx, "welcome", &adminStr); err != nil {
		t.Fatalf("DeleteEmailTemplateOverride: %v", err)
	}
	if got, _ := s.GetEmailTemplateOverride(ctx, "welcome"); got != nil {
		t.Error("delete didn't remove override")
	}
	// Revision count bumps by one for the reset action.
	revs, _ = s.ListEmailTemplateRevisions(ctx, "welcome", 10)
	if len(revs) != 4 {
		t.Errorf("after delete, expected 4 revisions, got %d", len(revs))
	}
}

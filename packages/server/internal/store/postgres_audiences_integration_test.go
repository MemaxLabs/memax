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

func TestPostgresAudienceCRUD(t *testing.T) {
	t.Parallel()
	s, pool := testdb.Acquire(t)
	ctx := context.Background()

	// Need a user row for the created_by FK.
	userID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'A', 'personal_early_access', now(), now())`,
		userID, userID[:8]+"@aud",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	aud := &model.Audience{
		ID:          uuid.NewString(),
		Name:        "Early adopters",
		Description: "first 1000",
		RuleType:    model.AudienceRuleAll,
		Rule:        model.AudienceRule{},
		CreatedBy:   userID,
	}

	// --- CreateAudience ---
	if err := s.CreateAudience(ctx, aud); err != nil {
		t.Fatalf("CreateAudience: %v", err)
	}

	// --- GetAudience happy path.
	got, err := s.GetAudience(ctx, aud.ID)
	if err != nil {
		t.Fatalf("GetAudience: %v", err)
	}
	if got.Name != "Early adopters" {
		t.Errorf("round-trip: %+v", got)
	}

	// --- GetAudience missing → ErrAudienceNotFound.
	if _, err := s.GetAudience(ctx, uuid.NewString()); !errors.Is(err, store.ErrAudienceNotFound) {
		t.Errorf("missing: got %v, want ErrAudienceNotFound", err)
	}

	// --- ListAudiences returns the row.
	list, err := s.ListAudiences(ctx)
	if err != nil {
		t.Fatalf("ListAudiences: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListAudiences returned %d, want 1", len(list))
	}

	// --- UpdateAudience happy path: rewrite rule + name.
	aud.Name = "Power users"
	aud.RuleType = model.AudienceRuleUsers
	aud.Rule = model.AudienceRule{Emails: []string{"power@test"}}
	if err := s.UpdateAudience(ctx, aud); err != nil {
		t.Fatalf("UpdateAudience: %v", err)
	}
	got, _ = s.GetAudience(ctx, aud.ID)
	if got.Name != "Power users" || got.RuleType != model.AudienceRuleUsers {
		t.Errorf("update didn't persist: %+v", got)
	}
	if len(got.Rule.Emails) != 1 {
		t.Errorf("rule round-trip lost emails: %+v", got.Rule)
	}

	// --- UpdateAudience on missing ID → ErrAudienceNotFound.
	missing := *aud
	missing.ID = uuid.NewString()
	if err := s.UpdateAudience(ctx, &missing); !errors.Is(err, store.ErrAudienceNotFound) {
		t.Errorf("update missing: got %v, want ErrAudienceNotFound", err)
	}

	// --- DeleteAudience removes the row.
	if err := s.DeleteAudience(ctx, aud.ID); err != nil {
		t.Fatalf("DeleteAudience: %v", err)
	}
	// Re-delete → ErrAudienceNotFound.
	if err := s.DeleteAudience(ctx, aud.ID); !errors.Is(err, store.ErrAudienceNotFound) {
		t.Errorf("re-delete: got %v, want ErrAudienceNotFound", err)
	}
}

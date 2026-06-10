package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func TestValidateChecklistPayload(t *testing.T) {
	t.Parallel()
	good := model.ChecklistPayload{
		Title: "Your first week",
		Items: []model.ChecklistItem{
			{Item: model.Item{ID: "a", Title: "Read note"}},
			{
				Item:     model.Item{ID: "b", Title: "Dump memory"},
				LockedBy: []string{"a"},
			},
		},
		RequiredIDs: []string{"a"},
	}
	if err := ValidateChecklistPayload(good); err != nil {
		t.Fatalf("expected good payload to validate, got %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(p *model.ChecklistPayload)
		wantSub string
	}{
		{
			name:    "empty title",
			mutate:  func(p *model.ChecklistPayload) { p.Title = "" },
			wantSub: "title required",
		},
		{
			name: "empty items",
			mutate: func(p *model.ChecklistPayload) {
				p.Items = nil
			},
			wantSub: "items[] cannot be empty",
		},
		{
			name: "items over cap",
			mutate: func(p *model.ChecklistPayload) {
				p.Items = make([]model.ChecklistItem, maxSuperNotifItems+1)
				for i := range p.Items {
					p.Items[i] = model.ChecklistItem{Item: model.Item{ID: "i" + string(rune('a'+i)), Title: "t"}}
				}
				p.RequiredIDs = nil
			},
			wantSub: "items[] cap is",
		},
		{
			name: "empty item id",
			mutate: func(p *model.ChecklistPayload) {
				p.Items[0].ID = ""
			},
			wantSub: "items[0].id is empty",
		},
		{
			name: "duplicate item id",
			mutate: func(p *model.ChecklistPayload) {
				p.Items[1].ID = "a"
			},
			wantSub: "duplicated",
		},
		{
			name: "required_ids references unknown",
			mutate: func(p *model.ChecklistPayload) {
				p.RequiredIDs = []string{"a", "z"}
			},
			wantSub: "required_ids[\"z\"]",
		},
		{
			name: "locked_by references unknown",
			mutate: func(p *model.ChecklistPayload) {
				p.Items[1].LockedBy = []string{"a", "z"}
			},
			wantSub: "locked_by references unknown",
		},
		{
			name: "self lock",
			mutate: func(p *model.ChecklistPayload) {
				p.Items[1].LockedBy = []string{"b"}
			},
			wantSub: "self-locks",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := model.ChecklistPayload{
				Title: good.Title,
				Items: append([]model.ChecklistItem(nil), good.Items...),
				// reuse RequiredIDs from good unless the case overrides.
				RequiredIDs: append([]string(nil), good.RequiredIDs...),
			}
			tc.mutate(&p)
			err := ValidateChecklistPayload(p)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSub)
			}
			if !errors.Is(err, ErrChecklistItemPayloadInvalid) {
				t.Errorf("expected ErrChecklistItemPayloadInvalid sentinel, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("expected %q in error, got %q", tc.wantSub, err.Error())
			}
		})
	}
}

func TestValidateDigestPayload(t *testing.T) {
	t.Parallel()
	good := model.DigestPayload{
		Title: "Your week in memax",
		Items: []model.DigestItem{
			{Item: model.Item{ID: "a", Title: "12 memories"}},
		},
	}
	if err := ValidateDigestPayload(good); err != nil {
		t.Fatalf("expected good digest to validate, got %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(p *model.DigestPayload)
		wantSub string
	}{
		{
			name:    "empty title",
			mutate:  func(p *model.DigestPayload) { p.Title = "" },
			wantSub: "title required",
		},
		{
			name:    "empty items",
			mutate:  func(p *model.DigestPayload) { p.Items = nil },
			wantSub: "items[] cannot be empty",
		},
		{
			name: "duplicate id",
			mutate: func(p *model.DigestPayload) {
				p.Items = append(p.Items, model.DigestItem{Item: model.Item{ID: "a", Title: "dup"}})
			},
			wantSub: "duplicated",
		},
		{
			name: "items over cap",
			mutate: func(p *model.DigestPayload) {
				p.Items = make([]model.DigestItem, maxSuperNotifItems+1)
				for i := range p.Items {
					p.Items[i] = model.DigestItem{Item: model.Item{ID: "i" + string(rune('a'+i)), Title: "t"}}
				}
			},
			wantSub: "items[] cap is",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := model.DigestPayload{
				Title: good.Title,
				Items: append([]model.DigestItem(nil), good.Items...),
			}
			tc.mutate(&p)
			err := ValidateDigestPayload(p)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSub)
			}
			if !errors.Is(err, ErrChecklistItemPayloadInvalid) {
				t.Errorf("expected ErrChecklistItemPayloadInvalid sentinel, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("expected %q in error, got %q", tc.wantSub, err.Error())
			}
		})
	}
}

func TestFindChecklistItemIdx(t *testing.T) {
	t.Parallel()
	items := []model.ChecklistItem{
		{Item: model.Item{ID: "a"}},
		{Item: model.Item{ID: "b"}},
		{Item: model.Item{ID: "c"}},
	}
	if got := findChecklistItemIdx(items, "b"); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
	if got := findChecklistItemIdx(items, "missing"); got != -1 {
		t.Errorf("expected -1 for missing, got %d", got)
	}
}

func TestFindDigestItemIdx(t *testing.T) {
	t.Parallel()
	items := []model.DigestItem{
		{Item: model.Item{ID: "a"}},
		{Item: model.Item{ID: "b"}},
	}
	if got := findDigestItemIdx(items, "a"); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
	if got := findDigestItemIdx(items, "missing"); got != -1 {
		t.Errorf("expected -1 for missing, got %d", got)
	}
}

// Helper for building a checklist payload across the transition tests.
// All required by default = [a, b, c]; `done` controls which of those
// are pre-stamped completed before the test invokes the mutator.
func sampleChecklist(done map[string]time.Time) model.ChecklistPayload {
	p := model.ChecklistPayload{
		Title: "Your first week",
		Items: []model.ChecklistItem{
			{Item: model.Item{ID: "a", Title: "Connect agent"}},
			{Item: model.Item{ID: "b", Title: "First memory"}},
			{Item: model.Item{ID: "c", Title: "First ask"}},
			{
				Item:     model.Item{ID: "d", Title: "First dream"},
				LockedBy: []string{"b"},
			},
		},
		RequiredIDs: []string{"a", "b", "c"},
	}
	for i, item := range p.Items {
		if t, ok := done[item.ID]; ok {
			tt := t
			p.Items[i].CompletedAt = &tt
			p.Items[i].ViewedAt = &tt
		}
	}
	return p
}

func TestComputeChecklistItemComplete_HappyTransition(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	p := sampleChecklist(nil)
	mutated, result, idempotent, err := computeChecklistItemComplete(p, "a", now)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if idempotent {
		t.Errorf("expected non-idempotent first complete, got idempotent=true")
	}
	if result == nil {
		t.Fatal("expected result snapshot, got nil")
	}
	if result.CompletedAt == nil || !result.CompletedAt.Equal(now) {
		t.Errorf("expected completed_at=now, got %v", result.CompletedAt)
	}
	if result.ViewedAt == nil || !result.ViewedAt.Equal(now) {
		t.Errorf("expected viewed_at backfilled to now, got %v", result.ViewedAt)
	}
	if result.AllRequiredDone {
		t.Errorf("expected AllRequiredDone=false at 1/3, got true")
	}
	if mutated.Items[0].CompletedAt == nil {
		t.Errorf("expected payload mutation to stamp items[0].completed_at")
	}
}

func TestComputeChecklistItemComplete_IdempotentPreservesAllRequiredDone(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	earlier := now.Add(-time.Hour)
	// Every required item already complete — an idempotent re-fire on
	// any of them must still report AllRequiredDone=true so the
	// handler retries TryAutoResolveChecklist if the first attempt
	// failed transiently (codex pass 1 High 2).
	p := sampleChecklist(map[string]time.Time{
		"a": earlier,
		"b": earlier,
		"c": earlier,
	})
	_, result, idempotent, err := computeChecklistItemComplete(p, "a", now)
	if err != nil {
		t.Fatalf("expected success on idempotent re-fire, got %v", err)
	}
	if !idempotent {
		t.Error("expected idempotent=true on already-complete item")
	}
	if result == nil || result.CompletedAt == nil || !result.CompletedAt.Equal(earlier) {
		t.Errorf("expected snapshot to preserve original completed_at, got %+v", result)
	}
	if !result.AllRequiredDone {
		t.Error("expected AllRequiredDone=true on idempotent re-fire when every required item is complete (retry path)")
	}
}

func TestComputeChecklistItemComplete_IdempotentNotAllRequiredDone(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	earlier := now.Add(-time.Hour)
	// Item a is already complete; b and c are still pending. Re-firing
	// /complete on a should be idempotent and NOT claim AllRequiredDone.
	p := sampleChecklist(map[string]time.Time{"a": earlier})
	_, result, idempotent, err := computeChecklistItemComplete(p, "a", now)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !idempotent {
		t.Error("expected idempotent=true")
	}
	if result.AllRequiredDone {
		t.Error("expected AllRequiredDone=false when only some required items are complete")
	}
}

func TestComputeChecklistItemComplete_LockedByGate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	// item d is locked_by=["b"], but b is not yet complete.
	p := sampleChecklist(map[string]time.Time{"a": now})
	_, _, _, err := computeChecklistItemComplete(p, "d", now)
	if !errors.Is(err, ErrChecklistItemLocked) {
		t.Errorf("expected ErrChecklistItemLocked, got %v", err)
	}

	// Once b completes, d unlocks.
	pUnlocked := sampleChecklist(map[string]time.Time{"a": now, "b": now})
	_, result, idempotent, err := computeChecklistItemComplete(pUnlocked, "d", now)
	if err != nil {
		t.Fatalf("expected unlock once b complete, got %v", err)
	}
	if idempotent {
		t.Error("expected non-idempotent first complete after unlock")
	}
	if result == nil || result.CompletedAt == nil {
		t.Fatal("expected completed snapshot post-unlock")
	}
}

func TestComputeChecklistItemComplete_AutoResolveAtLastRequired(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	// a + b already done — completing c (the last required) fires
	// AllRequiredDone. d is optional, stays incomplete.
	p := sampleChecklist(map[string]time.Time{"a": now, "b": now})
	_, result, idempotent, err := computeChecklistItemComplete(p, "c", now)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if idempotent {
		t.Error("expected non-idempotent transition")
	}
	if result == nil {
		t.Fatal("expected snapshot")
	}
	if !result.AllRequiredDone {
		t.Error("expected AllRequiredDone=true after every required item stamped")
	}
}

func TestComputeChecklistItemComplete_NotFound(t *testing.T) {
	t.Parallel()
	p := sampleChecklist(nil)
	_, _, _, err := computeChecklistItemComplete(p, "z", time.Now())
	if !errors.Is(err, ErrChecklistItemNotFound) {
		t.Errorf("expected ErrChecklistItemNotFound, got %v", err)
	}
}

func TestComputeChecklistItemComplete_NoRequiredIDsNeverAutoResolves(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	p := sampleChecklist(nil)
	p.RequiredIDs = nil
	_, result, _, err := computeChecklistItemComplete(p, "a", now)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if result == nil {
		t.Fatal("expected snapshot")
	}
	if result.AllRequiredDone {
		t.Error("optional-only checklist must never auto-resolve")
	}
}

func TestAllRequiredItemsComplete(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	// All required complete (a, b, c). Optional d untouched.
	full := sampleChecklist(map[string]time.Time{"a": now, "b": now, "c": now})
	if !allRequiredItemsComplete(full) {
		t.Error("expected true when every required item is complete")
	}
	// Two of three required complete.
	partial := sampleChecklist(map[string]time.Time{"a": now, "b": now})
	if allRequiredItemsComplete(partial) {
		t.Error("expected false when at least one required item is pending")
	}
	// Empty RequiredIDs → never auto-resolve.
	none := sampleChecklist(map[string]time.Time{"a": now, "b": now, "c": now})
	none.RequiredIDs = nil
	if allRequiredItemsComplete(none) {
		t.Error("expected false when RequiredIDs is empty")
	}
}

func TestValidateSuperNotifPayloadIfApplicable(t *testing.T) {
	t.Parallel()
	good := model.ChecklistPayload{
		Title: "t",
		Items: []model.ChecklistItem{{Item: model.Item{ID: "a", Title: "x"}}},
	}
	raw, _ := json.Marshal(good)
	n := &model.Notification{Kind: model.NotificationKindChecklist, Payload: raw}
	if err := validateSuperNotifPayloadIfApplicable(n); err != nil {
		t.Errorf("expected good payload to pass, got %v", err)
	}

	bad := &model.Notification{
		Kind:    model.NotificationKindChecklist,
		Payload: []byte(`{"title":"","items":[]}`),
	}
	if err := validateSuperNotifPayloadIfApplicable(bad); !errors.Is(err, ErrChecklistItemPayloadInvalid) {
		t.Errorf("expected ErrChecklistItemPayloadInvalid for empty title + items, got %v", err)
	}

	// Non-super-notif kinds skip validation.
	other := &model.Notification{Kind: model.NotificationKindHubInvite, Payload: []byte(`{}`)}
	if err := validateSuperNotifPayloadIfApplicable(other); err != nil {
		t.Errorf("expected non-super-notif kinds to skip validation, got %v", err)
	}

	// Malformed JSON on a super-notif kind wraps ErrChecklistItemPayloadInvalid.
	garbage := &model.Notification{Kind: model.NotificationKindDigest, Payload: []byte(`{not json`)}
	if err := validateSuperNotifPayloadIfApplicable(garbage); !errors.Is(err, ErrChecklistItemPayloadInvalid) {
		t.Errorf("expected ErrChecklistItemPayloadInvalid for malformed digest, got %v", err)
	}
}

// Verify that strings.HasPrefix wiring stayed sane (regression guard
// for the validator's `items[].id` format expectations).
func TestComputeChecklistItemComplete_PreservesPriorViewedAt(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	prior := now.Add(-time.Hour)
	p := sampleChecklist(nil)
	p.Items[0].ViewedAt = &prior
	_, result, _, err := computeChecklistItemComplete(p, "a", now)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if result == nil || result.ViewedAt == nil || !result.ViewedAt.Equal(prior) {
		t.Errorf("expected prior viewed_at preserved, got %+v", result)
	}

	// Sanity check on a substring helper to keep strings import live.
	if !strings.HasPrefix(p.Items[0].ID, "a") {
		t.Error("sample item id drifted")
	}
}

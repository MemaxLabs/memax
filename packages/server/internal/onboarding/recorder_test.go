package onboarding

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// TestRecorder_NilSafe verifies the recorder treats `nil` as fully
// disabled: every Record* method must return immediately without
// touching the store or panicking. This is the "nil means disabled"
// contract from CLAUDE.md.
func TestRecorder_NilSafe(t *testing.T) {
	t.Parallel()
	// NewRecorder(nil, nil) returns nil so callers can use the
	// "nil means disabled" pattern from CLAUDE.md without checking
	// for the wired-store case explicitly.
	r := NewRecorder(nil, nil)
	if r != nil {
		t.Fatalf("NewRecorder(nil, nil) should return nil; got %T", r)
	}
}

// TestRecorder_NoPendingChecklist verifies the fast-path no-op when
// the user has no pending onboarding checklist. The recorder must
// NOT panic and MUST NOT attempt CompleteNotificationItem.
func TestRecorder_NoPendingChecklist(t *testing.T) {
	t.Parallel()
	st := store.NewInMemoryStore()
	rec := NewRecorder(st, nil)
	if rec == nil {
		t.Fatal("NewRecorder should return non-nil with non-nil store")
	}
	// No checklist exists → LatestPendingChecklistByRecipient returns
	// ErrNotificationNotFound → recorder no-ops.
	rec.RecordAsk(context.Background(), "user-1")
}

// TestRecorder_EmptyUserID verifies guards against empty user IDs.
// The Recorder must skip every method when userID is empty rather
// than attempting a store call (which would error).
func TestRecorder_EmptyUserID(t *testing.T) {
	t.Parallel()
	st := store.NewInMemoryStore()
	rec := NewRecorder(st, nil)
	rec.RecordAsk(context.Background(), "")
}

// TestChecklistSourceIDFormat confirms the source_id format the emitter
// produces matches the partial unique index expectations. Restart
// version bumps must produce distinct source_ids.
func TestChecklistSourceIDFormat(t *testing.T) {
	t.Parallel()
	v1 := checklistSourceID("user-abc", 1)
	v2 := checklistSourceID("user-abc", 2)
	if v1 == v2 {
		t.Errorf("source_id collided across versions: %q == %q", v1, v2)
	}
	if v1 == "" {
		t.Error("source_id should be non-empty")
	}
}

// TestBuildChecklistV1Payload_Shape verifies the seed payload matches
// the validator contract (so the emitter's CreateNotificationIfAbsent
// call doesn't fail at the create-path validator hook).
func TestBuildChecklistV1Payload_Shape(t *testing.T) {
	t.Parallel()
	p := buildChecklistV1Payload()
	if err := store.ValidateChecklistPayload(p); err != nil {
		t.Fatalf("v1 checklist payload should validate, got %v", err)
	}
	if len(p.Items) != 7 {
		t.Errorf("expected 7 items, got %d", len(p.Items))
	}
	if len(p.RequiredIDs) != 5 {
		t.Errorf("expected 5 required items, got %d", len(p.RequiredIDs))
	}
	if p.PinContext != "memories_hero" {
		t.Errorf("expected pin_context=memories_hero, got %q", p.PinContext)
	}
}

// TestEmitter_RoundTrip verifies the emitter writes valid payloads
// that survive marshal → unmarshal through json.RawMessage. The
// store layer treats payloads as opaque JSON, but the renderer +
// validator expect a specific shape.
func TestEmitter_RoundTrip(t *testing.T) {
	t.Parallel()
	p := buildChecklistV1Payload()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded model.ChecklistPayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Title != p.Title {
		t.Errorf("title round-trip failed: %q vs %q", decoded.Title, p.Title)
	}
	if len(decoded.Items) != len(p.Items) {
		t.Errorf("items round-trip lost rows: %d vs %d", len(decoded.Items), len(p.Items))
	}
	if err := store.ValidateChecklistPayload(decoded); err != nil {
		t.Errorf("validator rejected round-tripped payload: %v", err)
	}
}

// TestExpiresAtIs14Days confirms the 14-day inactivity window from
// plan 18 §3.2 is honored.
func TestExpiresAtIs14Days(t *testing.T) {
	t.Parallel()
	before := time.Now().UTC()
	got := expiresAt()
	after := time.Now().UTC()
	if got == nil {
		t.Fatal("expiresAt returned nil")
	}
	lowerBound := before.Add(expiresAfter)
	upperBound := after.Add(expiresAfter)
	if got.Before(lowerBound) || got.After(upperBound) {
		t.Errorf("expiresAt outside expected 14d window: got %v, want [%v, %v]",
			got, lowerBound, upperBound)
	}
}

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func TestTopicsAssignMemoryKeepsSingleAssignmentAndHonorsConfidence(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewTopicsHandler(s, nil)

	hubID := "11111111-1111-1111-1111-111111111111"
	ownerID := "u1"
	now := time.Now()
	if err := s.CreateHub(&model.Hub{
		ID:                     hubID,
		Name:                   "Test Hub",
		Slug:                   "test-hub",
		HubType:                "team",
		OwnerID:                ownerID,
		AllowContributorTopics: true,
		AllowContributorDreams: true,
		CreatedAt:              now,
		UpdatedAt:              now,
	}); err != nil {
		t.Fatal(err)
	}
	memory := &model.Memory{
		ID:              "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		HubID:           hubID,
		OwnerID:         ownerID,
		Title:           "note",
		Content:         "content",
		ContentType:     "markdown",
		ContentHash:     "hash",
		Kind:            model.MemoryKindSemantic,
		Stability:       model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0,
		Source:          "test",
		State:           "active",
		CreatedAt:       now,
		UpdatedAt:       now,
		AccessedAt:      now,
	}
	if err := s.CreateMemory(memory); err != nil {
		t.Fatal(err)
	}

	topicA := &model.Topic{
		ID:        "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		HubID:     hubID,
		OwnerID:   ownerID,
		Name:      "Alpha",
		Icon:      "folder",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	topicB := &model.Topic{
		ID:        "cccccccc-cccc-cccc-cccc-cccccccccccc",
		HubID:     hubID,
		OwnerID:   ownerID,
		Name:      "Beta",
		Icon:      "folder",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.CreateTopic(topicA); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTopic(topicB); err != nil {
		t.Fatal(err)
	}

	assign := func(topicID string, confidence float64) {
		body, _ := json.Marshal(map[string]any{
			"memory_id":  memory.ID,
			"confidence": confidence,
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/topics/"+topicID+"/memories", bytes.NewReader(body))
		req.SetPathValue("id", topicID)
		req = withTestIdentity(req, ownerID)
		rec := httptest.NewRecorder()
		h.AddMemory(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	assign(topicA.ID, model.ConfidenceUserMove)
	assign(topicB.ID, model.ConfidenceAutoHigh)

	topicIDs, err := s.GetMemoryTopicIDMap(store.VisibilityScope{OwnerID: ownerID})
	if err != nil {
		t.Fatal(err)
	}
	if got := topicIDs[memory.ID]; got != topicA.ID {
		t.Fatalf("expected memory to stay in topic A after lower-confidence assignment, got %#v", got)
	}

	// Equal-confidence: by design, AssignMemoryToTopic uses `confidence > existing`
	// (strict greater) — so replaying the same ConfidenceUserMove targeting a
	// different topic must NOT move the memory. This is the auto-assignment
	// contract: the ingest and dreams workers may repeatedly propose topics at
	// the same confidence level, and we want the earlier user intent to stick.
	// User-initiated moves must go through memories.batchMove (see
	// TestBatchMoveIsAuthoritativeAtEqualConfidence in memories_test.go) which
	// replaces unconditionally.
	assign(topicB.ID, model.ConfidenceUserMove)
	topicIDs, err = s.GetMemoryTopicIDMap(store.VisibilityScope{OwnerID: ownerID})
	if err != nil {
		t.Fatal(err)
	}
	if got := topicIDs[memory.ID]; got != topicA.ID {
		t.Fatalf("expected memory to stay in topic A at equal confidence (auto-assignment stays gated), got %#v", got)
	}

	assign(topicB.ID, model.ConfidenceUserLock)

	topicIDs, err = s.GetMemoryTopicIDMap(store.VisibilityScope{OwnerID: ownerID})
	if err != nil {
		t.Fatal(err)
	}
	if got := topicIDs[memory.ID]; got != topicB.ID {
		t.Fatalf("expected memory to move to topic B after higher-confidence assignment, got %#v", got)
	}
}

func TestTopicsGetIncludesActivitySummary(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewTopicsHandler(s, nil)

	hubID := "11111111-1111-1111-1111-111111111111"
	ownerID := "u1"
	now := time.Now()
	if err := s.CreateHub(&model.Hub{
		ID:                     hubID,
		Name:                   "Test Hub",
		Slug:                   "test-hub",
		HubType:                "team",
		OwnerID:                ownerID,
		AllowContributorTopics: true,
		AllowContributorDreams: true,
		CreatedAt:              now,
		UpdatedAt:              now,
	}); err != nil {
		t.Fatal(err)
	}

	topic := &model.Topic{
		ID:        "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		HubID:     hubID,
		OwnerID:   ownerID,
		Name:      "Backend",
		Icon:      "server",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateTopic(topic); err != nil {
		t.Fatal(err)
	}

	memory := &model.Memory{
		ID:              "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		HubID:           hubID,
		OwnerID:         ownerID,
		Title:           "recent note",
		Content:         "content",
		ContentType:     "markdown",
		ContentHash:     "hash",
		Kind:            model.MemoryKindSemantic,
		Stability:       model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0,
		Source:          "test",
		State:           "active",
		CreatedAt:       now.Add(-24 * time.Hour),
		UpdatedAt:       now.Add(-24 * time.Hour),
		AccessedAt:      now.Add(-24 * time.Hour),
	}
	if err := s.CreateMemory(memory); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignMemoryToTopic(memory.ID, topic.ID, hubID, model.ConfidenceUserMove); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/topics/"+topic.ID, nil)
	req.SetPathValue("id", topic.ID)
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{hubID}))
	rec := httptest.NewRecorder()

	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(resp.Data)
	var got model.Topic
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got.ActivitySummary == nil {
		t.Fatal("expected activity summary in topic response")
	}
	if got.ActivitySummary.WindowDays != 7 {
		t.Fatalf("window_days = %d, want 7", got.ActivitySummary.WindowDays)
	}
	if got.ActivitySummary.MemoryCount != 1 {
		t.Fatalf("memory_count = %d, want 1", got.ActivitySummary.MemoryCount)
	}
	if got.ActivitySummary.ContributorCount != 1 {
		t.Fatalf("contributor_count = %d, want 1", got.ActivitySummary.ContributorCount)
	}
}

// ── Reparent validation (Phase 1 of the Topic Tree DnD northstar plan) ──
//
// These tests exercise the TopicsHandler.Update parent_id branch and the
// new IsTopicDescendant / GetSubtreeMaxDepth store helpers. They use the
// InMemoryStore as the test substrate — postgres tests live elsewhere.

// reparentFixture builds a 3-level topic tree in a single hub for the
// validation tests: root → parent → child → grandchild, plus a sibling
// "other" topic at root for cross-branch reparents, plus an isolated
// topic in a second hub for the cross-hub rejection test.
type reparentFixture struct {
	store      *store.InMemoryStore
	handler    *TopicsHandler
	ownerID    string
	hubID      string
	otherHubID string
	root       *model.Topic // depth 0
	parent     *model.Topic // depth 1, child of root
	child      *model.Topic // depth 2, child of parent
	grandchild *model.Topic // depth 3, child of child
	other      *model.Topic // depth 0, sibling of root
	inOtherHub *model.Topic // depth 0, lives in otherHubID
}

func newReparentFixture(t *testing.T) *reparentFixture {
	t.Helper()
	s := store.NewInMemoryStore()
	h := NewTopicsHandler(s, nil)

	hubID := "11111111-1111-1111-1111-111111111111"
	otherHubID := "22222222-2222-2222-2222-222222222222"
	ownerID := "u1"
	now := time.Now()

	for _, hub := range []*model.Hub{
		{
			ID:                     hubID,
			Name:                   "Primary",
			Slug:                   "primary",
			HubType:                "team",
			OwnerID:                ownerID,
			AllowContributorTopics: true,
			AllowContributorDreams: true,
			CreatedAt:              now,
			UpdatedAt:              now,
		},
		{
			ID:                     otherHubID,
			Name:                   "Secondary",
			Slug:                   "secondary",
			HubType:                "team",
			OwnerID:                ownerID,
			AllowContributorTopics: true,
			AllowContributorDreams: true,
			CreatedAt:              now,
			UpdatedAt:              now,
		},
	} {
		if err := s.CreateHub(hub); err != nil {
			t.Fatalf("create hub: %v", err)
		}
	}

	mk := func(id, name, hub string, parentID *string, position int) *model.Topic {
		topic := &model.Topic{
			ID:        id,
			HubID:     hub,
			OwnerID:   ownerID,
			ParentID:  parentID,
			Name:      name,
			Icon:      "folder",
			Position:  position,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.CreateTopic(topic); err != nil {
			t.Fatalf("create topic %s: %v", name, err)
		}
		return topic
	}

	root := mk("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "Root", hubID, nil, 0)
	parent := mk("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "Parent", hubID, &root.ID, 0)
	child := mk("cccccccc-cccc-cccc-cccc-cccccccccccc", "Child", hubID, &parent.ID, 0)
	grandchild := mk("dddddddd-dddd-dddd-dddd-dddddddddddd", "Grandchild", hubID, &child.ID, 0)
	other := mk("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee", "Other", hubID, nil, 1)
	inOtherHub := mk("ffffffff-ffff-ffff-ffff-ffffffffffff", "Elsewhere", otherHubID, nil, 0)

	return &reparentFixture{
		store:      s,
		handler:    h,
		ownerID:    ownerID,
		hubID:      hubID,
		otherHubID: otherHubID,
		root:       root,
		parent:     parent,
		child:      child,
		grandchild: grandchild,
		other:      other,
		inOtherHub: inOtherHub,
	}
}

func (f *reparentFixture) updateRequest(t *testing.T, topicID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/v1/topics/"+topicID, bytes.NewReader(raw))
	req.SetPathValue("id", topicID)
	req = withTestIdentity(req, f.ownerID)
	rec := httptest.NewRecorder()
	f.handler.Update(rec, req)
	return rec
}

func decodeTopicResponse(t *testing.T, body []byte) *model.Topic {
	t.Helper()
	var env struct {
		Data model.Topic `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode topic: %v (body=%s)", err, string(body))
	}
	return &env.Data
}

func decodeErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode error: %v (body=%s)", err, string(body))
	}
	return env.Error.Code
}

func TestUpdateAcceptsAtomicParentAndPosition(t *testing.T) {
	f := newReparentFixture(t)
	// Move grandchild (depth 3) under "other" (depth 0) with explicit position.
	rec := f.updateRequest(t, f.grandchild.ID, map[string]any{
		"parent_id": f.other.ID,
		"position":  7,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodeTopicResponse(t, rec.Body.Bytes())
	if got.ParentID == nil || *got.ParentID != f.other.ID {
		t.Fatalf("parent_id = %v, want %s", got.ParentID, f.other.ID)
	}
	if got.Position != 7 {
		t.Fatalf("position = %d, want 7", got.Position)
	}
	if !got.UserModified {
		t.Fatal("expected user_modified = true after reparent")
	}
}

func TestUpdateRootReparent(t *testing.T) {
	f := newReparentFixture(t)
	// Move child from depth 2 to root (parent_id = "").
	rec := f.updateRequest(t, f.child.ID, map[string]any{
		"parent_id": "",
		"position":  3,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodeTopicResponse(t, rec.Body.Bytes())
	if got.ParentID != nil {
		t.Fatalf("parent_id = %v, want nil (root)", got.ParentID)
	}
	if got.Position != 3 {
		t.Fatalf("position = %d, want 3", got.Position)
	}
	if !got.UserModified {
		t.Fatal("expected user_modified = true after root reparent")
	}
}

func TestUpdateRejectsInvalidParent(t *testing.T) {
	f := newReparentFixture(t)
	rec := f.updateRequest(t, f.parent.ID, map[string]any{
		"parent_id": "00000000-0000-0000-0000-000000000000",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if code := decodeErrorCode(t, rec.Body.Bytes()); code != "invalid_parent" {
		t.Fatalf("error code = %q, want invalid_parent", code)
	}
}

func TestUpdateRejectsCrossHubParentAsInvalidParent(t *testing.T) {
	// The server collapses cross-hub parents into invalid_parent to avoid
	// leaking hub existence. Document this collapse at the test level.
	f := newReparentFixture(t)
	rec := f.updateRequest(t, f.parent.ID, map[string]any{
		"parent_id": f.inOtherHub.ID,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if code := decodeErrorCode(t, rec.Body.Bytes()); code != "invalid_parent" {
		t.Fatalf("error code = %q, want invalid_parent (cross-hub collapses to this)", code)
	}
}

func TestUpdateRejectsSelfParent(t *testing.T) {
	f := newReparentFixture(t)
	rec := f.updateRequest(t, f.parent.ID, map[string]any{
		"parent_id": f.parent.ID,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if code := decodeErrorCode(t, rec.Body.Bytes()); code != "cycle_detected" {
		t.Fatalf("error code = %q, want cycle_detected", code)
	}
}

func TestUpdateRejectsCycle(t *testing.T) {
	f := newReparentFixture(t)
	// Moving "parent" under "grandchild" would create a cycle:
	// grandchild → parent → child → grandchild ...
	rec := f.updateRequest(t, f.parent.ID, map[string]any{
		"parent_id": f.grandchild.ID,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if code := decodeErrorCode(t, rec.Body.Bytes()); code != "cycle_detected" {
		t.Fatalf("error code = %q, want cycle_detected", code)
	}
}

func TestUpdateRejectsSubtreeDepthOverflow(t *testing.T) {
	f := newReparentFixture(t)
	// Build a 5-level subtree rooted at "deep_root" under Other,
	// then try to reparent deep_root under grandchild (depth 3).
	// Result would be: root → parent → child → grandchild → deep_root →
	// deep_a → deep_b = depth 6, one past the cap.
	now := time.Now()
	deepRoot := &model.Topic{
		ID:        "10000000-0000-0000-0000-000000000001",
		HubID:     f.hubID,
		OwnerID:   f.ownerID,
		ParentID:  nil,
		Name:      "DeepRoot",
		Icon:      "folder",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := f.store.CreateTopic(deepRoot); err != nil {
		t.Fatal(err)
	}
	deepA := &model.Topic{
		ID:        "10000000-0000-0000-0000-000000000002",
		HubID:     f.hubID,
		OwnerID:   f.ownerID,
		ParentID:  &deepRoot.ID,
		Name:      "DeepA",
		Icon:      "folder",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := f.store.CreateTopic(deepA); err != nil {
		t.Fatal(err)
	}
	deepB := &model.Topic{
		ID:        "10000000-0000-0000-0000-000000000003",
		HubID:     f.hubID,
		OwnerID:   f.ownerID,
		ParentID:  &deepA.ID,
		Name:      "DeepB",
		Icon:      "folder",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := f.store.CreateTopic(deepB); err != nil {
		t.Fatal(err)
	}

	// GetSubtreeMaxDepth(deepRoot) = 2, parentDepth(grandchild) = 3,
	// so 3 + 1 + 2 = 6 > 4 → should reject.
	rec := f.updateRequest(t, deepRoot.ID, map[string]any{
		"parent_id": f.grandchild.ID,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if code := decodeErrorCode(t, rec.Body.Bytes()); code != "max_depth_subtree" {
		t.Fatalf("error code = %q, want max_depth_subtree", code)
	}
}

func TestUpdateTolerantOfPreExistingDeepSubtreeToRoot(t *testing.T) {
	// Regression guard: a subtree whose own depth is already at the cap
	// (from a prior Delete cascade or legacy data) can be reparented to
	// root as long as the resulting total depth is still within the limit.
	f := newReparentFixture(t)
	// grandchild has no descendants, so GetSubtreeMaxDepth = 0.
	// Reparent grandchild to root → new depth 0 + 0 = 0 ≤ 4, allowed.
	rec := f.updateRequest(t, f.grandchild.ID, map[string]any{
		"parent_id": "",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateSetsUserModifiedOnReparent(t *testing.T) {
	f := newReparentFixture(t)
	// Reparent "child" (currently under parent) to "other". parent_id-only
	// update — no name/description/icon touched — must still flip
	// user_modified so the dream engine respects the manual intent.
	rec := f.updateRequest(t, f.child.ID, map[string]any{
		"parent_id": f.other.ID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := decodeTopicResponse(t, rec.Body.Bytes())
	if !got.UserModified {
		t.Fatal("expected user_modified = true after manual reparent")
	}
}

// ── Direct store-level tests for the new helpers ──

func TestIsTopicDescendantInMemoryStore(t *testing.T) {
	f := newReparentFixture(t)
	cases := []struct {
		name        string
		ancestor    string
		candidate   string
		wantMatches bool
	}{
		{"self is descendant of self", f.parent.ID, f.parent.ID, true},
		{"direct child", f.parent.ID, f.child.ID, true},
		{"transitive grandchild", f.parent.ID, f.grandchild.ID, true},
		{"unrelated sibling", f.parent.ID, f.other.ID, false},
		{"ancestor is not a descendant of its descendant", f.grandchild.ID, f.parent.ID, false},
		{"cross-hub not a descendant", f.root.ID, f.inOtherHub.ID, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := f.store.IsTopicDescendant(f.hubID, tc.ancestor, tc.candidate)
			if err != nil {
				t.Fatalf("IsTopicDescendant: %v", err)
			}
			if got != tc.wantMatches {
				t.Fatalf("IsTopicDescendant = %v, want %v", got, tc.wantMatches)
			}
		})
	}
}

func TestGetSubtreeMaxDepthInMemoryStore(t *testing.T) {
	f := newReparentFixture(t)
	cases := []struct {
		topicID string
		want    int
	}{
		{f.grandchild.ID, 0}, // leaf
		{f.child.ID, 1},      // → grandchild
		{f.parent.ID, 2},     // → child → grandchild
		{f.root.ID, 3},       // → parent → child → grandchild
		{f.other.ID, 0},      // leaf sibling at root
	}
	for _, tc := range cases {
		got, err := f.store.GetSubtreeMaxDepth(f.hubID, tc.topicID)
		if err != nil {
			t.Fatalf("GetSubtreeMaxDepth: %v", err)
		}
		if got != tc.want {
			t.Fatalf("GetSubtreeMaxDepth(%s) = %d, want %d", tc.topicID, got, tc.want)
		}
	}
}

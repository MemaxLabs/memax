package workerapp

import (
	"context"
	"errors"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// fakeChatProposeTopicMergeStore is a minimal stub for the
// chatProposeTopicMergeStore interface. Tests assemble the
// (hub, role, topic) tuple they want to drive each branch of
// chatProposeTopicMergeAdapter.ProposeTopicMerge.
type fakeChatProposeTopicMergeStore struct {
	hubByID                map[string]*model.Hub
	roleByHubAndUser       map[[2]string]string
	topicByHubAndID        map[[2]string]*model.Topic
	memoryCountsByHub      map[string]map[string]int
	notificationByID       map[string]*model.Notification
	notificationBySourceID map[string]*model.Notification

	getHubErr        error
	getRoleErr       error
	getTopicErr      error
	countErr         error
	createNotifErr   error
	insertReturn     bool // value returned by CreateNotificationIfAbsent (true=fresh, false=upsert)
	insertCalls      int
	insertCalledWith []*model.Notification
}

func (f *fakeChatProposeTopicMergeStore) GetHub(id string) (*model.Hub, error) {
	if f.getHubErr != nil {
		return nil, f.getHubErr
	}
	return f.hubByID[id], nil
}

func (f *fakeChatProposeTopicMergeStore) GetHubMemberRole(hubID, userID string) (string, error) {
	if f.getRoleErr != nil {
		return "", f.getRoleErr
	}
	return f.roleByHubAndUser[[2]string{hubID, userID}], nil
}

func (f *fakeChatProposeTopicMergeStore) GetTopic(id string, hubID string) (*model.Topic, error) {
	if f.getTopicErr != nil {
		return nil, f.getTopicErr
	}
	t, ok := f.topicByHubAndID[[2]string{hubID, id}]
	if !ok {
		return nil, errors.New("topic not found")
	}
	return t, nil
}

func (f *fakeChatProposeTopicMergeStore) CountMemoriesByTopic(_ store.VisibilityScope, hubID string) (map[string]int, error) {
	if f.countErr != nil {
		return nil, f.countErr
	}
	return f.memoryCountsByHub[hubID], nil
}

func (f *fakeChatProposeTopicMergeStore) CreateNotificationIfAbsent(n *model.Notification) (bool, error) {
	if f.createNotifErr != nil {
		return false, f.createNotifErr
	}
	f.insertCalls++
	f.insertCalledWith = append(f.insertCalledWith, n)
	if f.notificationBySourceID == nil {
		f.notificationBySourceID = map[string]*model.Notification{}
	}
	if f.notificationByID == nil {
		f.notificationByID = map[string]*model.Notification{}
	}
	if existing, ok := f.notificationBySourceID[n.SourceID]; ok && existing != nil {
		// Upsert path — production helper returns false. The
		// caller is expected to follow up with
		// GetNotificationBySourceKey to fetch the existing row's
		// actual id + status (Phase 3.6b3f v2 fix).
		return false, nil
	}
	// Stash a copy so the test can later mutate it (e.g. flip
	// status to resolved) without re-keying via the test's
	// reference.
	stored := *n
	f.notificationBySourceID[n.SourceID] = &stored
	f.notificationByID[n.ID] = &stored
	return f.insertReturn, nil
}

func (f *fakeChatProposeTopicMergeStore) GetNotificationBySourceKey(_ context.Context, sourceKind, sourceID string) (*model.Notification, error) {
	// Source key for this fake is just sourceID (test-only;
	// production uses both fields).
	_ = sourceKind
	if existing, ok := f.notificationBySourceID[sourceID]; ok && existing != nil {
		return existing, nil
	}
	return nil, errors.New("notification not found")
}

const (
	pmOwner    = "u-owner"
	pmAdmin    = "u-admin"
	pmContrib  = "u-contrib"
	pmViewer   = "u-viewer"
	pmNonMem   = "u-nonmember"
	pmHubA     = "h-a"
	pmHubB     = "h-b"
	pmTarget   = "t-target"
	pmSrcA     = "t-src-a"
	pmSrcB     = "t-src-b"
	pmSrcOther = "t-src-other-hub"
)

func newProposeStore() *fakeChatProposeTopicMergeStore {
	return &fakeChatProposeTopicMergeStore{
		hubByID: map[string]*model.Hub{
			pmHubA: {ID: pmHubA, HubType: "team", AllowContributorTopics: true},
			pmHubB: {ID: pmHubB, HubType: "team", AllowContributorTopics: true},
		},
		roleByHubAndUser: map[[2]string]string{
			{pmHubA, pmOwner}:   "owner",
			{pmHubA, pmAdmin}:   "admin",
			{pmHubA, pmContrib}: "contributor",
			{pmHubA, pmViewer}:  "viewer",
		},
		topicByHubAndID: map[[2]string]*model.Topic{
			{pmHubA, pmTarget}:   {ID: pmTarget, HubID: pmHubA, Name: "Auth Migration"},
			{pmHubA, pmSrcA}:     {ID: pmSrcA, HubID: pmHubA, Name: "Auth Tickets"},
			{pmHubA, pmSrcB}:     {ID: pmSrcB, HubID: pmHubA, Name: "OAuth Notes"},
			{pmHubB, pmSrcOther}: {ID: pmSrcOther, HubID: pmHubB, Name: "Another Hub"},
		},
		memoryCountsByHub: map[string]map[string]int{
			pmHubA: {pmTarget: 5, pmSrcA: 3, pmSrcB: 2},
		},
		insertReturn: true,
	}
}

// Owner-case happy path: returns proposed status, fresh
// notification, all topics resolved.
func TestChatProposeTopicMergeAdapter_OwnerCase_HappyPath(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	a := &chatProposeTopicMergeAdapter{store: store}

	out, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA, pmSrcB},
		Reason:         "consolidate",
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if out.Status != tools.ProposeStatusProposed {
		t.Errorf("Status = %q, want %q", out.Status, tools.ProposeStatusProposed)
	}
	if out.NotificationID == "" {
		t.Errorf("NotificationID empty; want UUID")
	}
	if out.TargetTitle != "Auth Migration" {
		t.Errorf("TargetTitle = %q, want %q", out.TargetTitle, "Auth Migration")
	}
	if got, want := len(out.SourceTitles), 2; got != want {
		t.Errorf("SourceTitles len = %d, want %d", got, want)
	}
	// Notification was created with the right shape.
	if store.insertCalls != 1 {
		t.Fatalf("insertCalls = %d, want 1", store.insertCalls)
	}
	notif := store.insertCalledWith[0]
	if notif.Kind != model.NotificationKindReviewTopicMerge {
		t.Errorf("Kind = %q, want %q", notif.Kind, model.NotificationKindReviewTopicMerge)
	}
	if notif.SourceKind != model.NotificationSourceTopicReview {
		t.Errorf("SourceKind = %q, want %q", notif.SourceKind, model.NotificationSourceTopicReview)
	}
	if notif.RecipientUserID != pmOwner {
		t.Errorf("RecipientUserID = %q, want %q", notif.RecipientUserID, pmOwner)
	}
	if notif.HubID != pmHubA {
		t.Errorf("HubID = %q, want %q", notif.HubID, pmHubA)
	}
	if notif.Status != model.NotificationStatusPending {
		t.Errorf("Status = %q, want %q", notif.Status, model.NotificationStatusPending)
	}
	// Source-id encodes target + sorted sources + recipient.
	wantSourceID := model.BuildTopicMergeNotificationSourceID(pmTarget, []string{pmSrcA, pmSrcB}, pmOwner)
	if notif.SourceID != wantSourceID {
		t.Errorf("SourceID = %q, want %q", notif.SourceID, wantSourceID)
	}
}

// Source-key dedup: two proposals of the same merge collapse
// onto one notification row. The second call returns
// "already_pending" status instead of "proposed", AND the
// returned notification_id matches the existing row — NOT the
// freshly-generated UUID the second call locally minted.
// Codex Phase 3.6b3f v1 [High] flagged that v1 returned the
// wrong id; this regression pins the fix.
func TestChatProposeTopicMergeAdapter_DedupesByStableSourceID(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	a := &chatProposeTopicMergeAdapter{store: store}

	// First call inserts.
	out1, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA, pmSrcB},
	})
	if err != nil || out1.Status != tools.ProposeStatusProposed {
		t.Fatalf("first call: err=%v status=%q, want proposed", err, out1.Status)
	}
	if out1.NotificationID == "" {
		t.Fatalf("first call NotificationID empty")
	}

	// Second call with the SAME canonical inputs upserts.
	out2, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA, pmSrcB},
	})
	if err != nil {
		t.Fatalf("second call: err = %v, want nil", err)
	}
	if out2.Status != tools.ProposeStatusAlreadyPending {
		t.Errorf("second call Status = %q, want %q", out2.Status, tools.ProposeStatusAlreadyPending)
	}
	// Critical: returned notification_id must match the EXISTING
	// row's id, not the locally-generated UUID for call #2.
	// Without this fix the agent would surface a UUID that
	// doesn't identify any inbox row, and a click-through 404s.
	if out2.NotificationID != out1.NotificationID {
		t.Errorf("second call NotificationID = %q, want %q (existing row id, not freshly minted)",
			out2.NotificationID, out1.NotificationID)
	}
	// Source-id stability: second call's locally-minted notif
	// must have the same source_id as the first.
	if store.insertCalledWith[0].SourceID != store.insertCalledWith[1].SourceID {
		t.Errorf("source ids diverged: %q vs %q",
			store.insertCalledWith[0].SourceID, store.insertCalledWith[1].SourceID)
	}
}

// Resolution-aware status mapping covers the four meaningful
// (Status × Resolution) combinations a topic-merge review can
// land in. Codex Phase 3.6b3f v2 [Medium] pinned that v1's
// "all resolved → already_resolved" was misleading because
// merged / kept_separate / dismissed all live under
// status=resolved with distinct resolutions, and the agent
// needs to phrase its response differently for each.
func TestChatProposeTopicMergeAdapter_ResolvedRowStatusMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		status     model.NotificationStatus
		resolution model.NotificationResolution
		want       string
	}{
		{"resolved + merged", model.NotificationStatusResolved, model.ResolutionMerged, tools.ProposeStatusAlreadyMerged},
		{"resolved + kept_separate", model.NotificationStatusResolved, model.ResolutionKeptSeparate, tools.ProposeStatusAlreadyKeptSeparate},
		{"resolved + dismissed", model.NotificationStatusResolved, model.ResolutionDismissed, tools.ProposeStatusAlreadyDismissed},
		{"resolved + empty resolution", model.NotificationStatusResolved, "", tools.ProposeStatusAlreadyResolved},
		{"resolved + future resolution", model.NotificationStatusResolved, "kept_a", tools.ProposeStatusAlreadyResolved},
		{"status=dismissed (receipt path)", model.NotificationStatusDismissed, "", tools.ProposeStatusAlreadyDismissed},
		{"expired", model.NotificationStatusExpired, "", tools.ProposeStatusAlreadyExpired},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			store := newProposeStore()
			a := &chatProposeTopicMergeAdapter{store: store}

			// Seed the row, then mutate to the test's terminal state.
			if _, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
				OwnerID:        pmOwner,
				HubID:          pmHubA,
				TargetTopicID:  pmTarget,
				SourceTopicIDs: []string{pmSrcA},
			}); err != nil {
				t.Fatalf("seed call: err = %v", err)
			}
			for _, n := range store.notificationBySourceID {
				n.Status = c.status
				n.Resolution = c.resolution
			}

			out, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
				OwnerID:        pmOwner,
				HubID:          pmHubA,
				TargetTopicID:  pmTarget,
				SourceTopicIDs: []string{pmSrcA},
			})
			if err != nil {
				t.Fatalf("re-propose: err = %v, want nil", err)
			}
			if out.Status != c.want {
				t.Errorf("Status = %q, want %q (status=%q resolution=%q)",
					out.Status, c.want, c.status, c.resolution)
			}
		})
	}
}

// Unknown status — codex v2 said don't fall back silently to
// "already_pending" (would lie). The adapter must surface as
// an error so the agent doesn't claim work that may not be
// pending.
func TestChatProposeTopicMergeAdapter_UnknownStatusErrors(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	a := &chatProposeTopicMergeAdapter{store: store}

	if _, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, n := range store.notificationBySourceID {
		n.Status = "limbo" // unknown enum value
	}

	_, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA},
	})
	if err == nil {
		t.Fatalf("err = nil, want error for unknown status (must not silently map to already_pending)")
	}
}

// Mixed-case UUID input dedupes onto the same source_id as
// the canonical lowercase form, because the adapter builds
// the source_id from the resolved Topic.ID (which the store
// returns in canonical form). Codex Phase 3.6b3f v1 [Medium]
// flagged this — without resolved-id dedup, uppercase
// variants could create parallel notification rows for the
// same merge.
func TestChatProposeTopicMergeAdapter_UUIDCasingDedupes(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	// Patch the store fake so a lookup of an UPPERCASE source
	// id resolves to the canonical (lowercase) topic. Mirrors
	// Postgres's case-insensitive UUID column behavior.
	store.topicByHubAndID[[2]string{pmHubA, "T-SRC-A"}] = store.topicByHubAndID[[2]string{pmHubA, pmSrcA}]
	a := &chatProposeTopicMergeAdapter{store: store}

	// First call uses lowercase canonical IDs.
	out1, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA},
	})
	if err != nil || out1.Status != tools.ProposeStatusProposed {
		t.Fatalf("first call: err=%v status=%q", err, out1.Status)
	}

	// Second call uses uppercase. With resolved-id dedup, both
	// resolve to canonical Topic.ID and produce the same
	// source_id → second call is already_pending, not a fresh
	// proposed.
	out2, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{"T-SRC-A"},
	})
	if err != nil {
		t.Fatalf("second call (uppercase): err = %v, want nil", err)
	}
	if out2.Status != tools.ProposeStatusAlreadyPending {
		t.Errorf("Status = %q, want %q (uppercase variant must dedupe via resolved Topic.ID)",
			out2.Status, tools.ProposeStatusAlreadyPending)
	}
	if out2.NotificationID != out1.NotificationID {
		t.Errorf("NotificationID = %q, want %q (uppercase variant must point at the same row)",
			out2.NotificationID, out1.NotificationID)
	}
}

// Defensive: case-folded target appearing in sources after
// resolution must still reject. The wrapper catches the raw
// equality; the adapter's post-resolution re-check catches the
// case-bypass codex flagged.
func TestChatProposeTopicMergeAdapter_CaseFoldedTargetInSourcesRejected(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	// Same trick: register an uppercase alias that resolves to
	// the target's canonical Topic.
	store.topicByHubAndID[[2]string{pmHubA, "T-TARGET"}] = store.topicByHubAndID[[2]string{pmHubA, pmTarget}]
	a := &chatProposeTopicMergeAdapter{store: store}

	// The wrapper's pre-resolution check compares "T-TARGET"
	// against pmTarget — they differ as strings, so it doesn't
	// fire. The adapter's post-resolution re-check compares
	// against the canonical Topic.ID and rejects.
	_, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{"T-TARGET"},
	})
	if !errors.Is(err, tools.ErrProposeTopicMergeInvalidArgs) {
		t.Errorf("err = %v, want ErrProposeTopicMergeInvalidArgs", err)
	}
}

// Cross-hub topic rejection: target lives in hubA, sources
// reference a topic from hubB → NotFound. Same fail-closed
// surface forget_memory uses.
func TestChatProposeTopicMergeAdapter_CrossHubTopic_NotFound(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	a := &chatProposeTopicMergeAdapter{store: store}

	_, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA, pmSrcOther}, // pmSrcOther belongs to hubB
	})
	if !errors.Is(err, tools.ErrProposeTopicMergeNotFound) {
		t.Errorf("err = %v, want ErrProposeTopicMergeNotFound", err)
	}
	if store.insertCalls != 0 {
		t.Errorf("insertCalls = %d, want 0 (validation must short-circuit)", store.insertCalls)
	}
}

// Target lookup miss in the requested hub → NotFound. (Target
// id doesn't exist anywhere — pre-existing topic deletion or
// agent hallucination.)
func TestChatProposeTopicMergeAdapter_TargetMissing_NotFound(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	a := &chatProposeTopicMergeAdapter{store: store}

	_, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  "t-does-not-exist",
		SourceTopicIDs: []string{pmSrcA},
	})
	if !errors.Is(err, tools.ErrProposeTopicMergeNotFound) {
		t.Errorf("err = %v, want ErrProposeTopicMergeNotFound", err)
	}
}

// Owner role can always propose — even when hub flag is false.
func TestChatProposeTopicMergeAdapter_OwnerCanProposeWithoutFlag(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	store.hubByID[pmHubA].AllowContributorTopics = false
	a := &chatProposeTopicMergeAdapter{store: store}

	_, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA},
	})
	if err != nil {
		t.Errorf("err = %v, want nil (owner ignores AllowContributorTopics)", err)
	}
}

// Admin role can always propose — even when hub flag is false.
func TestChatProposeTopicMergeAdapter_AdminCanProposeWithoutFlag(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	store.hubByID[pmHubA].AllowContributorTopics = false
	a := &chatProposeTopicMergeAdapter{store: store}

	_, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmAdmin,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA},
	})
	if err != nil {
		t.Errorf("err = %v, want nil (admin ignores AllowContributorTopics)", err)
	}
}

// Contributor with AllowContributorTopics=true → permitted.
// Codex Phase 3.6b3e v3 pinned this branch as a required
// test.
func TestChatProposeTopicMergeAdapter_ContributorWithFlagPermitted(t *testing.T) {
	t.Parallel()
	store := newProposeStore() // flag=true by default
	a := &chatProposeTopicMergeAdapter{store: store}

	_, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmContrib,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA},
	})
	if err != nil {
		t.Errorf("err = %v, want nil (contributor with flag=true must be permitted)", err)
	}
}

// Contributor with AllowContributorTopics=false → rejected
// with NoPermission. Codex Phase 3.6b3e v3 pinned this.
func TestChatProposeTopicMergeAdapter_ContributorWithoutFlagRejected(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	store.hubByID[pmHubA].AllowContributorTopics = false
	a := &chatProposeTopicMergeAdapter{store: store}

	_, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmContrib,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA},
	})
	if !errors.Is(err, tools.ErrProposeTopicMergeNoPermission) {
		t.Errorf("err = %v, want ErrProposeTopicMergeNoPermission", err)
	}
}

// Viewer never permitted, regardless of hub flag.
func TestChatProposeTopicMergeAdapter_ViewerRejected(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	a := &chatProposeTopicMergeAdapter{store: store}

	_, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmViewer,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA},
	})
	if !errors.Is(err, tools.ErrProposeTopicMergeNoPermission) {
		t.Errorf("err = %v, want ErrProposeTopicMergeNoPermission", err)
	}
}

// Non-member rejected with NoPermission (the chat session's
// scope said this hub is reachable but membership has been
// revoked between scope-resolve and tool-call).
func TestChatProposeTopicMergeAdapter_NonMemberRejected(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	a := &chatProposeTopicMergeAdapter{store: store}

	_, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmNonMem, // not in roleByHubAndUser
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA},
	})
	if !errors.Is(err, tools.ErrProposeTopicMergeNoPermission) {
		t.Errorf("err = %v, want ErrProposeTopicMergeNoPermission", err)
	}
}

// Hub disappears between scope-resolve and tool-call → NotFound.
// Same fail-closed surface as topic-not-found.
func TestChatProposeTopicMergeAdapter_HubDisappeared_NotFound(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	delete(store.hubByID, pmHubA)
	a := &chatProposeTopicMergeAdapter{store: store}

	_, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA},
	})
	if !errors.Is(err, tools.ErrProposeTopicMergeNotFound) {
		t.Errorf("err = %v, want ErrProposeTopicMergeNotFound", err)
	}
}

// Defensive: if a future caller bypasses the wrapper's
// canonicalization and target appears in sources, the adapter
// rejects with InvalidArgs rather than silently proposing a
// self-merge.
func TestChatProposeTopicMergeAdapter_TargetInSourcesRejected(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	a := &chatProposeTopicMergeAdapter{store: store}

	_, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA, pmTarget},
	})
	if !errors.Is(err, tools.ErrProposeTopicMergeInvalidArgs) {
		t.Errorf("err = %v, want ErrProposeTopicMergeInvalidArgs", err)
	}
	if store.insertCalls != 0 {
		t.Errorf("insertCalls = %d, want 0", store.insertCalls)
	}
}

// Validation: empty sources after the wrapper's filter (a
// future caller bypassing canonicalization).
func TestChatProposeTopicMergeAdapter_EmptySourcesRejected(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	a := &chatProposeTopicMergeAdapter{store: store}

	_, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: nil,
	})
	if !errors.Is(err, tools.ErrProposeTopicMergeInvalidArgs) {
		t.Errorf("err = %v, want ErrProposeTopicMergeInvalidArgs", err)
	}
}

// Memory counts populated into the payload from the
// CountMemoriesByTopic map. A failed count returns an empty
// map (best-effort) and per-topic lookups fall through to
// zero — verifies the graceful-degradation path.
func TestChatProposeTopicMergeAdapter_MemoryCountFailureNonFatal(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	store.countErr = errors.New("transient db blip")
	a := &chatProposeTopicMergeAdapter{store: store}

	out, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil (count failure must not block proposal)", err)
	}
	if out.Status != tools.ProposeStatusProposed {
		t.Errorf("Status = %q, want %q", out.Status, tools.ProposeStatusProposed)
	}
}

// Source_id construction is now centralized in
// model.BuildTopicMergeNotificationSourceID (covered by
// internal/model/topic_review_keys_test.go). This test is
// the chat-side integration check: the adapter actually
// CALLS the shared helper and the resulting key in the
// stored notification matches the helper's output for the
// same canonical inputs.
func TestChatProposeTopicMergeAdapter_UsesSharedSourceIDHelper(t *testing.T) {
	t.Parallel()
	store := newProposeStore()
	a := &chatProposeTopicMergeAdapter{store: store}

	if _, err := a.ProposeTopicMerge(context.Background(), tools.ProposeTopicMergeRequest{
		OwnerID:        pmOwner,
		HubID:          pmHubA,
		TargetTopicID:  pmTarget,
		SourceTopicIDs: []string{pmSrcA, pmSrcB},
	}); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(store.insertCalledWith) != 1 {
		t.Fatalf("inserts = %d, want 1", len(store.insertCalledWith))
	}
	wantSourceID := model.BuildTopicMergeNotificationSourceID(pmTarget, []string{pmSrcA, pmSrcB}, pmOwner)
	if got := store.insertCalledWith[0].SourceID; got != wantSourceID {
		t.Errorf("notification SourceID = %q, want %q (must match shared helper)", got, wantSourceID)
	}
}

// canProposeTopicMergeRole table-driven coverage: pins the
// exact role × flag matrix per codex Phase 3.6b3e v3 review
// request.
func TestCanProposeTopicMergeRole_Matrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		role string
		flag bool
		want bool
	}{
		{"owner / flag=true", "owner", true, true},
		{"owner / flag=false", "owner", false, true},
		{"admin / flag=true", "admin", true, true},
		{"admin / flag=false", "admin", false, true},
		{"contributor / flag=true", "contributor", true, true},
		{"contributor / flag=false", "contributor", false, false},
		{"viewer / flag=true", "viewer", true, false},
		{"viewer / flag=false", "viewer", false, false},
		{"empty (non-member)", "", false, false},
		{"unknown role", "manager", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := canProposeTopicMergeRole(c.role, &model.Hub{AllowContributorTopics: c.flag})
			if got != c.want {
				t.Errorf("canProposeTopicMergeRole(%q, flag=%v) = %v, want %v", c.role, c.flag, got, c.want)
			}
		})
	}
}

package workerapp

import (
	"context"
	"errors"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// fakeChatForgetStore is a minimal chatForgetStore stub. Tests
// assemble the (memory, hub, role, attachments) tuple they
// want to drive each branch of chatForgetAdapter.ForgetMemory.
type fakeChatForgetStore struct {
	memoryByID            map[string]*model.Memory
	hubByID               map[string]*model.Hub
	roleByHubAndUser      map[[2]string]string // ([hubID, userID]) → role
	attachmentsByMemoryID map[string][]model.MemoryAttachment
	hubMemoryCount        map[string]int // for CountMemoriesByHub

	getMemoryErr       error
	getHubErr          error
	getRoleErr         error
	deleteMemoryErr    error
	deleteHubMemoryErr error
	countHubErr        error
	clearOverLimitErr  error

	deletedOwnerCount    int
	deletedHubCount      int
	deleteOwnerSeen      []string
	deleteHubSeen        []string
	clearOverLimitCalled []string
	clearOverLimitReturn bool // value returned by ClearHubOverLimit (transitioned)
}

func (f *fakeChatForgetStore) ListMemoryAttachments(memoryID, _ string) ([]model.MemoryAttachment, error) {
	if f.attachmentsByMemoryID == nil {
		return nil, nil
	}
	return f.attachmentsByMemoryID[memoryID], nil
}

// fakeChatForgetMeter records adjust calls so tests can assert
// the post-delete counter accounting matches the REST/MCP
// delete path.
type fakeChatForgetMeter struct {
	memoryAdjusts  []memoryAdjust
	hubAdjusts     []hubMemoryAdjust
	storageAdjusts []storageAdjust
}

type memoryAdjust struct {
	OwnerID string
	Delta   int
}
type hubMemoryAdjust struct {
	HubID string
	Delta int
}
type storageAdjust struct {
	OwnerID string
	Delta   int64
}

func (m *fakeChatForgetMeter) AdjustMemoryCount(_ context.Context, ownerID string, delta int) {
	m.memoryAdjusts = append(m.memoryAdjusts, memoryAdjust{OwnerID: ownerID, Delta: delta})
}
func (m *fakeChatForgetMeter) AdjustHubMemoryCount(_ context.Context, hubID string, delta int) {
	m.hubAdjusts = append(m.hubAdjusts, hubMemoryAdjust{HubID: hubID, Delta: delta})
}
func (m *fakeChatForgetMeter) AdjustStorageBytes(_ context.Context, ownerID string, delta int64) {
	m.storageAdjusts = append(m.storageAdjusts, storageAdjust{OwnerID: ownerID, Delta: delta})
}

// fakeChatForgetEvents records published events.
type fakeChatForgetEvents struct {
	published []events.Event
	err       error
}

func (e *fakeChatForgetEvents) Publish(_ context.Context, evt events.Event) error {
	if e.err != nil {
		return e.err
	}
	e.published = append(e.published, evt)
	return nil
}

func (f *fakeChatForgetStore) GetMemoryInHubs(_ context.Context, id string, hubIDs []string) (*model.Memory, error) {
	if f.getMemoryErr != nil {
		return nil, f.getMemoryErr
	}
	mem, ok := f.memoryByID[id]
	if !ok {
		// Match the Postgres scanMemoryWithAuthor wrap shape:
		// "memory not found: <pgx err>". Codex's Phase 3.6b3e
		// v1 review caught that v1 returned (nil, nil) here,
		// which neither real implementation does.
		return nil, errors.New("memory not found: pgx: no rows in result set")
	}
	for _, h := range hubIDs {
		if mem.HubID == h {
			return mem, nil
		}
	}
	// Out of scope — also surfaces as not-found from the
	// strict-hub query.
	return nil, errors.New("memory not found: pgx: no rows in result set (out of scope)")
}

func (f *fakeChatForgetStore) GetHub(id string) (*model.Hub, error) {
	if f.getHubErr != nil {
		return nil, f.getHubErr
	}
	hub, ok := f.hubByID[id]
	if !ok {
		return nil, errors.New("hub not found")
	}
	return hub, nil
}

func (f *fakeChatForgetStore) GetHubMemberRole(hubID, userID string) (string, error) {
	if f.getRoleErr != nil {
		return "", f.getRoleErr
	}
	return f.roleByHubAndUser[[2]string{hubID, userID}], nil
}

func (f *fakeChatForgetStore) DeleteMemory(id, _ string) error {
	if f.deleteMemoryErr != nil {
		return f.deleteMemoryErr
	}
	f.deletedOwnerCount++
	f.deleteOwnerSeen = append(f.deleteOwnerSeen, id)
	return nil
}

func (f *fakeChatForgetStore) DeleteHubMemory(id, _ string) error {
	if f.deleteHubMemoryErr != nil {
		return f.deleteHubMemoryErr
	}
	f.deletedHubCount++
	f.deleteHubSeen = append(f.deleteHubSeen, id)
	return nil
}

func (f *fakeChatForgetStore) CountMemoriesByHub(_ context.Context, hubID string) (int, error) {
	if f.countHubErr != nil {
		return 0, f.countHubErr
	}
	if f.hubMemoryCount == nil {
		return 0, nil
	}
	return f.hubMemoryCount[hubID], nil
}

func (f *fakeChatForgetStore) ClearHubOverLimit(_ context.Context, hubID string) (bool, error) {
	if f.clearOverLimitErr != nil {
		return false, f.clearOverLimitErr
	}
	f.clearOverLimitCalled = append(f.clearOverLimitCalled, hubID)
	return f.clearOverLimitReturn, nil
}

// fakeChatForgetObjectStore records Delete calls so tests can
// assert blob purge happens after a chat-forget. Mirrors REST
// handler.deleteAttachmentObjects.
type fakeChatForgetObjectStore struct {
	deletedKeys []string
	err         error
}

func (o *fakeChatForgetObjectStore) Delete(_ context.Context, key string) error {
	if o.err != nil {
		return o.err
	}
	o.deletedKeys = append(o.deletedKeys, key)
	return nil
}

// fakeChatForgetHubQuota stubs GetHubMemoryLimit. The default
// zero-value returns 0 (which means "limit reached" for any
// non-empty hub); tests set per-hub limits explicitly.
type fakeChatForgetHubQuota struct {
	limitByHub map[string]int
}

func (q *fakeChatForgetHubQuota) GetHubMemoryLimit(_ context.Context, hubID string) int {
	if q.limitByHub == nil {
		return -1 // unlimited by default
	}
	if v, ok := q.limitByHub[hubID]; ok {
		return v
	}
	return -1
}

const (
	fOwner    = "owner-x"
	fOther    = "owner-other"
	fHubA     = "hub-a"
	fHubTeam  = "hub-team"
	fMem1     = "mem-1"
	fMemTeam  = "mem-team"
	fMemOther = "mem-other"
)

// Owner-case happy path: caller is the memory owner, hub is
// in scope → DeleteMemory called, status=forgotten.
func TestChatForgetAdapter_OwnerCase_DeletesViaDeleteMemory(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMem1: {ID: fMem1, OwnerID: fOwner, HubID: fHubA, Title: "x"},
		},
	}
	a := &chatForgetAdapter{store: store}
	out, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMem1,
		AllowedHubIDs: []string{fHubA},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if out.Status != tools.ForgetStatusForgotten {
		t.Errorf("Status = %q, want %q", out.Status, tools.ForgetStatusForgotten)
	}
	if store.deletedOwnerCount != 1 {
		t.Errorf("DeleteMemory called %d times, want 1", store.deletedOwnerCount)
	}
	if store.deletedHubCount != 0 {
		t.Errorf("DeleteHubMemory called %d times, want 0 (owner-case uses owner path)", store.deletedHubCount)
	}
}

// Out-of-scope memory → ErrForgetMemoryNotFound, no delete.
func TestChatForgetAdapter_OutOfScope_Rejects(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMem1: {ID: fMem1, OwnerID: fOwner, HubID: "hub-elsewhere", Title: "x"},
		},
	}
	a := &chatForgetAdapter{store: store}
	_, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMem1,
		AllowedHubIDs: []string{fHubA}, // doesn't include hub-elsewhere
	})
	if !errors.Is(err, tools.ErrForgetMemoryNotFound) {
		t.Errorf("err = %v, want ErrForgetMemoryNotFound", err)
	}
	if store.deletedOwnerCount != 0 || store.deletedHubCount != 0 {
		t.Errorf("delete called; want zero (out-of-scope must not delete)")
	}
}

// Missing memory → ErrForgetMemoryNotFound.
func TestChatForgetAdapter_NotFound_Rejects(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{memoryByID: map[string]*model.Memory{}}
	a := &chatForgetAdapter{store: store}
	_, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      "missing",
		AllowedHubIDs: []string{fHubA},
	})
	if !errors.Is(err, tools.ErrForgetMemoryNotFound) {
		t.Errorf("err = %v, want ErrForgetMemoryNotFound", err)
	}
}

// Empty AllowedHubIDs → ErrForgetMemoryNotFound (defensive
// — the tool wrapper rejects empty scopes, but the service
// must not fall back to owner-only reads).
func TestChatForgetAdapter_EmptyAllowedHubIDs_Rejects(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{}
	a := &chatForgetAdapter{store: store}
	_, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMem1,
		AllowedHubIDs: []string{},
	})
	if !errors.Is(err, tools.ErrForgetMemoryNotFound) {
		t.Errorf("err = %v, want ErrForgetMemoryNotFound", err)
	}
}

// Team-hub case: caller is NOT the memory owner. Authorize
// via GetHubMemberRole + canDeleteMemoryRole. Owner role →
// DeleteHubMemory called.
func TestChatForgetAdapter_TeamHubCase_OwnerRole_DeletesViaDeleteHubMemory(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMemTeam: {ID: fMemTeam, OwnerID: fOther, HubID: fHubTeam, Title: "team note"},
		},
		hubByID: map[string]*model.Hub{
			fHubTeam: {ID: fHubTeam, HubType: "team", ContributorDeletePolicy: "any"},
		},
		roleByHubAndUser: map[[2]string]string{
			{fHubTeam, fOwner}: "owner",
		},
	}
	a := &chatForgetAdapter{store: store}
	out, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMemTeam,
		AllowedHubIDs: []string{fHubTeam},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if out.Status != tools.ForgetStatusForgotten {
		t.Errorf("Status = %q, want %q", out.Status, tools.ForgetStatusForgotten)
	}
	if store.deletedHubCount != 1 {
		t.Errorf("DeleteHubMemory called %d times, want 1", store.deletedHubCount)
	}
	if store.deletedOwnerCount != 0 {
		t.Errorf("DeleteMemory called %d times, want 0 (non-owner uses hub path)", store.deletedOwnerCount)
	}
}

// Team-hub viewer role → ErrForgetMemoryNoPermission.
func TestChatForgetAdapter_TeamHubViewer_Rejects(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMemTeam: {ID: fMemTeam, OwnerID: fOther, HubID: fHubTeam},
		},
		hubByID: map[string]*model.Hub{
			fHubTeam: {ID: fHubTeam, HubType: "team", ContributorDeletePolicy: "any"},
		},
		roleByHubAndUser: map[[2]string]string{
			{fHubTeam, fOwner}: "viewer",
		},
	}
	a := &chatForgetAdapter{store: store}
	_, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMemTeam,
		AllowedHubIDs: []string{fHubTeam},
	})
	if !errors.Is(err, tools.ErrForgetMemoryNoPermission) {
		t.Errorf("err = %v, want ErrForgetMemoryNoPermission", err)
	}
}

// Team-hub contributor role with hub policy "none" →
// ErrForgetMemoryNoPermission. Pins canDeleteMemoryRole's
// policy lookup.
func TestChatForgetAdapter_TeamHubContributor_PolicyNone_Rejects(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMemTeam: {ID: fMemTeam, OwnerID: fOther, HubID: fHubTeam},
		},
		hubByID: map[string]*model.Hub{
			fHubTeam: {ID: fHubTeam, HubType: "team", ContributorDeletePolicy: "none"},
		},
		roleByHubAndUser: map[[2]string]string{
			{fHubTeam, fOwner}: "contributor",
		},
	}
	a := &chatForgetAdapter{store: store}
	_, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMemTeam,
		AllowedHubIDs: []string{fHubTeam},
	})
	if !errors.Is(err, tools.ErrForgetMemoryNoPermission) {
		t.Errorf("err = %v, want ErrForgetMemoryNoPermission", err)
	}
}

// Team-hub contributor role with hub policy permitting deletes →
// DeleteHubMemory called.
// Phase 3.6b3e v2 codex follow-up: contributor with hub
// policy "own" must reject in the NON-OWNER branch. v1
// silently widened "own" to "any" by treating every policy
// except "none" as permissive. This test pins the strict
// match against handler/hub_roles.go's canDeleteMemory's
// "own" semantics: contributor can delete only memories
// they OWN (this branch is non-owner, so reject).
func TestChatForgetAdapter_TeamHubContributor_PolicyOwn_NonOwner_Rejects(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMemTeam: {ID: fMemTeam, OwnerID: fOther, HubID: fHubTeam},
		},
		hubByID: map[string]*model.Hub{
			fHubTeam: {ID: fHubTeam, HubType: "team", ContributorDeletePolicy: "own"},
		},
		roleByHubAndUser: map[[2]string]string{
			{fHubTeam, fOwner}: "contributor",
		},
	}
	a := &chatForgetAdapter{store: store}
	_, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMemTeam,
		AllowedHubIDs: []string{fHubTeam},
	})
	if !errors.Is(err, tools.ErrForgetMemoryNoPermission) {
		t.Errorf("err = %v, want ErrForgetMemoryNoPermission (policy 'own' must reject in non-owner branch)", err)
	}
	if store.deletedHubCount != 0 {
		t.Errorf("DeleteHubMemory called %d times, want 0", store.deletedHubCount)
	}
}

func TestChatForgetAdapter_TeamHubContributor_PolicyPermits_Deletes(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMemTeam: {ID: fMemTeam, OwnerID: fOther, HubID: fHubTeam},
		},
		hubByID: map[string]*model.Hub{
			fHubTeam: {ID: fHubTeam, HubType: "team", ContributorDeletePolicy: "any"},
		},
		roleByHubAndUser: map[[2]string]string{
			{fHubTeam, fOwner}: "contributor",
		},
	}
	a := &chatForgetAdapter{store: store}
	if _, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMemTeam,
		AllowedHubIDs: []string{fHubTeam},
	}); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if store.deletedHubCount != 1 {
		t.Errorf("DeleteHubMemory called %d times, want 1", store.deletedHubCount)
	}
}

// No live membership in the memory's hub → ErrForgetMemoryNoPermission.
func TestChatForgetAdapter_NonOwner_NoMembership_Rejects(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMemTeam: {ID: fMemTeam, OwnerID: fOther, HubID: fHubTeam},
		},
		hubByID: map[string]*model.Hub{
			fHubTeam: {ID: fHubTeam, HubType: "team"},
		},
		roleByHubAndUser: map[[2]string]string{}, // no membership
	}
	a := &chatForgetAdapter{store: store}
	_, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMemTeam,
		AllowedHubIDs: []string{fHubTeam},
	})
	if !errors.Is(err, tools.ErrForgetMemoryNoPermission) {
		t.Errorf("err = %v, want ErrForgetMemoryNoPermission", err)
	}
}

// DeleteMemory error propagates.
func TestChatForgetAdapter_DeleteError_Propagates(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("synthetic delete failure")
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMem1: {ID: fMem1, OwnerID: fOwner, HubID: fHubA},
		},
		deleteMemoryErr: sentinel,
	}
	a := &chatForgetAdapter{store: store}
	_, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMem1,
		AllowedHubIDs: []string{fHubA},
	})
	if err == nil || !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want errors.Is sentinel", err)
	}
}

// Phase 3.6b3e v2 codex follow-up: side effects match the
// REST DELETE path. On a successful forget the meter sees
// AdjustMemoryCount(-1, owner) + AdjustHubMemoryCount(-1,
// hub) + AdjustStorageBytes(-N, owner) where N is the sum
// of the memory's attachment sizes. The events publisher
// sees a hub.memories.changed event with state="deleted".
func TestChatForgetAdapter_Owner_RunsMeterAndEvents(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMem1: {ID: fMem1, OwnerID: fOwner, HubID: fHubA, Boundary: "private"},
		},
		attachmentsByMemoryID: map[string][]model.MemoryAttachment{
			fMem1: {
				{ID: "a1", SizeBytes: 1024},
				{ID: "a2", SizeBytes: 2048},
			},
		},
	}
	mtr := &fakeChatForgetMeter{}
	pub := &fakeChatForgetEvents{}
	a := &chatForgetAdapter{store: store, meter: mtr, events: pub}

	if _, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMem1,
		AllowedHubIDs: []string{fHubA},
	}); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}

	// Meter calls.
	if len(mtr.memoryAdjusts) != 1 || mtr.memoryAdjusts[0].Delta != -1 || mtr.memoryAdjusts[0].OwnerID != fOwner {
		t.Errorf("memoryAdjusts = %+v, want one (-1, owner) call", mtr.memoryAdjusts)
	}
	if len(mtr.hubAdjusts) != 1 || mtr.hubAdjusts[0].Delta != -1 || mtr.hubAdjusts[0].HubID != fHubA {
		t.Errorf("hubAdjusts = %+v, want one (-1, hub) call", mtr.hubAdjusts)
	}
	if len(mtr.storageAdjusts) != 1 || mtr.storageAdjusts[0].Delta != -3072 || mtr.storageAdjusts[0].OwnerID != fOwner {
		t.Errorf("storageAdjusts = %+v, want one (-3072, owner) call (1024 + 2048 attachment bytes)", mtr.storageAdjusts)
	}

	// Event published.
	if len(pub.published) != 1 {
		t.Fatalf("published = %d events, want 1", len(pub.published))
	}
	ev := pub.published[0]
	if ev.Type != events.EventTypeHubMemoriesChanged {
		t.Errorf("event Type = %q, want %q", ev.Type, events.EventTypeHubMemoriesChanged)
	}
	if ev.HubID != fHubA {
		t.Errorf("event HubID = %q, want %q", ev.HubID, fHubA)
	}
	if ev.EntityID != fMem1 {
		t.Errorf("event EntityID = %q, want %q", ev.EntityID, fMem1)
	}
	if ev.State != "deleted" {
		t.Errorf("event State = %q, want deleted", ev.State)
	}
	if !ev.PrivateOnly {
		t.Errorf("event PrivateOnly = false, want true (memory.Boundary=private)")
	}
}

// Side effects on team-hub case adjust the MEMORY'S OWNER's
// quota (not the acting user's) — team-hub deletes by a
// non-owner free the original owner's quota slot, mirroring
// the REST path's accounting.
func TestChatForgetAdapter_TeamHub_AdjustsOnMemoryOwner(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMemTeam: {ID: fMemTeam, OwnerID: fOther, HubID: fHubTeam, Boundary: "shared"},
		},
		hubByID: map[string]*model.Hub{
			fHubTeam: {ID: fHubTeam, HubType: "team", ContributorDeletePolicy: "any"},
		},
		roleByHubAndUser: map[[2]string]string{
			{fHubTeam, fOwner}: "owner",
		},
	}
	mtr := &fakeChatForgetMeter{}
	pub := &fakeChatForgetEvents{}
	a := &chatForgetAdapter{store: store, meter: mtr, events: pub}

	if _, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner, // acting user
		MemoryID:      fMemTeam,
		AllowedHubIDs: []string{fHubTeam},
	}); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if mtr.memoryAdjusts[0].OwnerID != fOther {
		t.Errorf("AdjustMemoryCount targeted %q, want %q (memory owner, not acting user)",
			mtr.memoryAdjusts[0].OwnerID, fOther)
	}
}

// Nil meter / nil events → adapter still completes the
// delete (counters drift, but the row is gone). Pins the
// degraded mode for dev/staging without Redis.
func TestChatForgetAdapter_NilMeterNilEvents_StillDeletes(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMem1: {ID: fMem1, OwnerID: fOwner, HubID: fHubA},
		},
	}
	a := &chatForgetAdapter{store: store, meter: nil, events: nil}
	if _, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMem1,
		AllowedHubIDs: []string{fHubA},
	}); err != nil {
		t.Errorf("err = %v, want nil (nil meter/events must not block delete)", err)
	}
	if store.deletedOwnerCount != 1 {
		t.Errorf("DeleteMemory called %d times, want 1", store.deletedOwnerCount)
	}
}

// Object-store blob purge runs after a successful delete so
// R2 doesn't accumulate orphaned attachment blobs (REST handler
// .Delete calls deleteAttachmentObjects on the same snapshot).
// Codex Phase 3.6b3e v2 [High] flagged that v1+v2 of this
// adapter skipped the blob delete.
func TestChatForgetAdapter_DeletesAttachmentBlobs(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMem1: {ID: fMem1, OwnerID: fOwner, HubID: fHubA},
		},
		attachmentsByMemoryID: map[string][]model.MemoryAttachment{
			fMem1: {
				{ID: "a1", MemoryID: fMem1, StorageKey: "uploads/a1.bin", SizeBytes: 100},
				{ID: "a2", MemoryID: fMem1, StorageKey: "uploads/a2.bin", SizeBytes: 200},
				{ID: "a3", MemoryID: fMem1, StorageKey: "", SizeBytes: 0}, // empty key → skipped
			},
		},
	}
	blob := &fakeChatForgetObjectStore{}
	a := &chatForgetAdapter{store: store, objectStore: blob}

	if _, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMem1,
		AllowedHubIDs: []string{fHubA},
	}); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got, want := len(blob.deletedKeys), 2; got != want {
		t.Errorf("deletedKeys = %d, want %d (skip empty StorageKey)", got, want)
	}
	wantKeys := map[string]bool{"uploads/a1.bin": true, "uploads/a2.bin": true}
	for _, k := range blob.deletedKeys {
		if !wantKeys[k] {
			t.Errorf("unexpected key deleted: %q", k)
		}
	}
}

// Object-store delete failure is logged but doesn't fail the
// chat-forget — the memory row is already gone, and a leaked
// blob is recoverable by the periodic GC sweeper.
func TestChatForgetAdapter_BlobDeleteFailureNonFatal(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMem1: {ID: fMem1, OwnerID: fOwner, HubID: fHubA},
		},
		attachmentsByMemoryID: map[string][]model.MemoryAttachment{
			fMem1: {{ID: "a1", MemoryID: fMem1, StorageKey: "uploads/a1.bin", SizeBytes: 100}},
		},
	}
	blob := &fakeChatForgetObjectStore{err: errors.New("r2 unavailable")}
	a := &chatForgetAdapter{store: store, objectStore: blob}

	if _, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMem1,
		AllowedHubIDs: []string{fHubA},
	}); err != nil {
		t.Errorf("err = %v, want nil (blob delete failure must not fail the forget)", err)
	}
}

// maybeClearHubOverLimitInline clears the freeze flag when the
// post-delete count is back at/under the hub's plan limit. The
// REST path does this in maybeClearHubOverLimit; without it,
// the push path's frozen-hub short-circuit (memories.go:632)
// keeps the hub frozen indefinitely after a chat-forget brings
// it back under cap. Codex Phase 3.6b3e v2 [Medium] flagged
// this divergence.
func TestChatForgetAdapter_ClearsHubOverLimitWhenUnderCap(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMem1: {ID: fMem1, OwnerID: fOwner, HubID: fHubA},
		},
		hubMemoryCount: map[string]int{fHubA: 9}, // under the limit of 10
	}
	mtr := &fakeChatForgetMeter{}
	hubQuota := &fakeChatForgetHubQuota{limitByHub: map[string]int{fHubA: 10}}
	a := &chatForgetAdapter{store: store, meter: mtr, hubQuota: hubQuota}

	if _, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMem1,
		AllowedHubIDs: []string{fHubA},
	}); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got, want := len(store.clearOverLimitCalled), 1; got != want {
		t.Fatalf("ClearHubOverLimit called %d times, want %d", got, want)
	}
	if store.clearOverLimitCalled[0] != fHubA {
		t.Errorf("ClearHubOverLimit hubID = %q, want %q", store.clearOverLimitCalled[0], fHubA)
	}
}

// When the hub is still over its limit after the delete, the
// flag stays set — the freeze persists until enough memories
// are removed to bring count <= limit.
func TestChatForgetAdapter_DoesNotClearOverLimitWhenStillOverCap(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMem1: {ID: fMem1, OwnerID: fOwner, HubID: fHubA},
		},
		hubMemoryCount: map[string]int{fHubA: 11}, // STILL over the limit of 10
	}
	hubQuota := &fakeChatForgetHubQuota{limitByHub: map[string]int{fHubA: 10}}
	a := &chatForgetAdapter{store: store, meter: &fakeChatForgetMeter{}, hubQuota: hubQuota}

	if _, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMem1,
		AllowedHubIDs: []string{fHubA},
	}); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(store.clearOverLimitCalled) != 0 {
		t.Errorf("ClearHubOverLimit called %d times, want 0 (hub still over cap)", len(store.clearOverLimitCalled))
	}
}

// Unlimited plan (limit = -1) skips the count check entirely
// and clears the marker. Mirrors handler's IsUnlimited path.
func TestChatForgetAdapter_UnlimitedPlanClearsWithoutCount(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{
		memoryByID: map[string]*model.Memory{
			fMem1: {ID: fMem1, OwnerID: fOwner, HubID: fHubA},
		},
		// hubMemoryCount unset — would panic if called, ensuring we don't.
	}
	hubQuota := &fakeChatForgetHubQuota{limitByHub: map[string]int{fHubA: -1}}
	a := &chatForgetAdapter{store: store, meter: &fakeChatForgetMeter{}, hubQuota: hubQuota}

	if _, err := a.ForgetMemory(context.Background(), tools.ForgetMemoryRequest{
		OwnerID:       fOwner,
		MemoryID:      fMem1,
		AllowedHubIDs: []string{fHubA},
	}); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(store.clearOverLimitCalled) != 1 {
		t.Errorf("ClearHubOverLimit called %d times, want 1 (unlimited plan)", len(store.clearOverLimitCalled))
	}
}

// Validation: empty owner / memory id rejected.
func TestChatForgetAdapter_Validation_RejectsEmptyArgs(t *testing.T) {
	t.Parallel()
	store := &fakeChatForgetStore{}
	a := &chatForgetAdapter{store: store}
	for name, in := range map[string]tools.ForgetMemoryRequest{
		"empty owner":     {OwnerID: "", MemoryID: fMem1, AllowedHubIDs: []string{fHubA}},
		"empty memory id": {OwnerID: fOwner, MemoryID: "", AllowedHubIDs: []string{fHubA}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := a.ForgetMemory(context.Background(), in)
			if err == nil {
				t.Errorf("err = nil; want validation failure for %q", name)
			}
		})
	}
}

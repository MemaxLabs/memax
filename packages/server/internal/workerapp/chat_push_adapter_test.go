package workerapp

import (
	"context"
	"errors"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// fakeChatPushStore is a minimal chatPushStore stub. Each
// method is a function field tests can override. The struct
// fields default to "no-op success" when unset.
type fakeChatPushStore struct {
	roleByHub  map[string]string
	roleErr    error
	createErr  error
	createSeen []*model.Memory
}

func (f *fakeChatPushStore) GetHubMemberRole(hubID, _ string) (string, error) {
	if f.roleErr != nil {
		return "", f.roleErr
	}
	return f.roleByHub[hubID], nil
}

func (f *fakeChatPushStore) CreateMemory(m *model.Memory) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.createSeen = append(f.createSeen, m)
	return nil
}

// Phase 3.6b3d v2 — viewer role rejected. The chat session's
// scope.HubIDs is built from READ-accessible hubs, so a
// viewer-role hub appears in scope. Without this gate, the
// agent could push to that hub after approval. Codex's v1
// review flagged this; the gate is in place but the test
// coverage was missing.
func TestChatPushAdapter_RejectsViewerRole(t *testing.T) {
	t.Parallel()
	fs := &fakeChatPushStore{
		roleByHub: map[string]string{"hub-x": "viewer"},
	}
	a := &chatPushAdapter{store: fs}
	mem := &model.Memory{
		ID:      "mem-1",
		HubID:   "hub-x",
		OwnerID: "owner-x",
		Content: "anything",
	}
	_, err := a.CreateAndProcess(context.Background(), mem, model.PushRequest{})
	if err == nil {
		t.Fatal("err = nil; want viewer-role rejection")
	}
	if len(fs.createSeen) != 0 {
		t.Errorf("CreateMemory called %d times, want 0 (rejected before write)", len(fs.createSeen))
	}
}

// Empty role (caller has no live membership in the hub) →
// reject. Catches the case where membership was revoked
// between session-create and message-send.
func TestChatPushAdapter_RejectsEmptyRole(t *testing.T) {
	t.Parallel()
	fs := &fakeChatPushStore{
		roleByHub: map[string]string{}, // no membership
	}
	a := &chatPushAdapter{store: fs}
	_, err := a.CreateAndProcess(context.Background(), &model.Memory{
		ID: "mem-1", HubID: "hub-x", OwnerID: "owner-x",
	}, model.PushRequest{})
	if err == nil {
		t.Fatal("err = nil; want no-membership rejection")
	}
	if len(fs.createSeen) != 0 {
		t.Errorf("CreateMemory called; want zero writes")
	}
}

// Contributor role → write succeeds (PushOutcome carries the
// status). nil river client → saved_unprocessed.
func TestChatPushAdapter_ContributorRoleSucceeds_NilRiver(t *testing.T) {
	t.Parallel()
	fs := &fakeChatPushStore{
		roleByHub: map[string]string{"hub-x": "contributor"},
	}
	a := &chatPushAdapter{store: fs, river: nil}
	out, err := a.CreateAndProcess(context.Background(), &model.Memory{
		ID: "mem-1", HubID: "hub-x", OwnerID: "owner-x",
	}, model.PushRequest{})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if out.Status != tools.PushStatusSavedUnprocessed {
		t.Errorf("Status = %q, want %q (river client unset)", out.Status, tools.PushStatusSavedUnprocessed)
	}
	if len(fs.createSeen) != 1 {
		t.Errorf("CreateMemory call count = %d, want 1", len(fs.createSeen))
	}
}

// Owner role → write succeeds. Smoke that owner is also in
// canWriteMemoriesRole's allow set.
func TestChatPushAdapter_OwnerRoleSucceeds(t *testing.T) {
	t.Parallel()
	fs := &fakeChatPushStore{
		roleByHub: map[string]string{"hub-x": "owner"},
	}
	a := &chatPushAdapter{store: fs}
	out, err := a.CreateAndProcess(context.Background(), &model.Memory{
		ID: "mem-1", HubID: "hub-x", OwnerID: "owner-x",
	}, model.PushRequest{})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if out.Status == "" {
		t.Errorf("Status empty, want non-empty")
	}
}

// GetHubMemberRole DB error → propagates.
func TestChatPushAdapter_RoleQueryError_Propagates(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("synthetic role-query failure")
	fs := &fakeChatPushStore{roleErr: sentinel}
	a := &chatPushAdapter{store: fs}
	_, err := a.CreateAndProcess(context.Background(), &model.Memory{
		ID: "mem-1", HubID: "hub-x", OwnerID: "owner-x",
	}, model.PushRequest{})
	if err == nil {
		t.Fatal("err = nil; want propagation of role-query failure")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v; want errors.Is sentinel (proves %%w wrap)", err)
	}
}

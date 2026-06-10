package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	memaxmodel "github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type fakeBrowseStore struct {
	listCalls int
	lastList  store.StrictHubListOptions
	memories  []memaxmodel.Memory
	hubs      []memaxmodel.HubWithRole
	topics    []memaxmodel.Topic
}

func (f *fakeBrowseStore) ListMemoriesInHubs(_ context.Context, opts store.StrictHubListOptions) ([]memaxmodel.Memory, string, int, error) {
	f.listCalls++
	f.lastList = opts
	return f.memories, "", len(f.memories), nil
}

func (f *fakeBrowseStore) GetMemoryInHubs(_ context.Context, id string, hubIDs []string) (*memaxmodel.Memory, error) {
	for _, m := range f.memories {
		if m.ID != id {
			continue
		}
		for _, hubID := range hubIDs {
			if m.HubID == hubID {
				cp := m
				return &cp, nil
			}
		}
	}
	return nil, store.ErrMemoryNotFound
}

func (f *fakeBrowseStore) ListUserHubs(_ string) ([]memaxmodel.HubWithRole, error) {
	return f.hubs, nil
}

func (f *fakeBrowseStore) GetHub(id string) (*memaxmodel.Hub, error) {
	for _, h := range f.hubs {
		if h.Hub.ID == id {
			cp := h.Hub
			return &cp, nil
		}
	}
	return nil, store.ErrMemoryNotFound
}

func (f *fakeBrowseStore) ListTopics(_ string) ([]memaxmodel.Topic, error) {
	return f.topics, nil
}

func (f *fakeBrowseStore) CountMemoriesByTopic(store.VisibilityScope, string) (map[string]int, error) {
	return map[string]int{}, nil
}

func (f *fakeBrowseStore) CountUnassignedMemories(store.VisibilityScope, string) (int, error) {
	return 0, nil
}

func newBrowseToolConfig(t *testing.T, ownerID string, liveHubIDs []string, sessionHubIDs []string, s AgentBrowseStore) BrowseToolConfig {
	t.Helper()
	svc, err := NewAgentBrowseService(s)
	if err != nil {
		t.Fatalf("NewAgentBrowseService: %v", err)
	}
	return BrowseToolConfig{
		Service: svc,
		Resolver: agent.NewScopeResolver(&fakeMembership{
			hubs: map[string][]string{ownerID: liveHubIDs},
		}),
		Session: agent.SessionDescriptor{
			OwnerID: ownerID,
			Type:    agent.ScopeTypeHubSubset,
			HubIDs:  sessionHubIDs,
		},
	}
}

func callBrowseTool(t *testing.T, tool sdktool.Tool, name string, args any) sdkmodel.ToolResult {
	t.Helper()
	input, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	result, err := tool.Execute(context.Background(), sdktool.Call{
		Use: sdkmodel.ToolUse{
			ID:    "tool_call_test_id",
			Name:  name,
			Input: input,
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return result
}

func TestListMemoriesToolUsesStrictResolvedHubs(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	store := &fakeBrowseStore{
		memories: []memaxmodel.Memory{
			{ID: "m1", HubID: "h-work", Title: "Roadmap", CreatedAt: now},
		},
	}
	tool, err := NewListMemoriesTool(newBrowseToolConfig(t, "u1", []string{"h-work", "h-personal"}, []string{"h-work", "h-personal"}, store))
	if err != nil {
		t.Fatalf("NewListMemoriesTool: %v", err)
	}

	result := callBrowseTool(t, tool, ListMemoriesName, listMemoriesArgs{})
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if store.listCalls != 1 {
		t.Fatalf("list calls = %d, want 1", store.listCalls)
	}
	got := store.lastList.HubIDs
	want := []string{"h-work", "h-personal"}
	if len(got) != len(want) {
		t.Fatalf("HubIDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("HubIDs = %v, want %v", got, want)
		}
	}
}

func TestListMemoriesToolRejectsHubOutsideScope(t *testing.T) {
	store := &fakeBrowseStore{}
	tool, err := NewListMemoriesTool(newBrowseToolConfig(t, "u1", []string{"h-work"}, []string{"h-work"}, store))
	if err != nil {
		t.Fatalf("NewListMemoriesTool: %v", err)
	}

	result := callBrowseTool(t, tool, ListMemoriesName, listMemoriesArgs{HubID: "h-other"})
	if !result.IsError {
		t.Fatal("expected error for out-of-scope hub_id")
	}
	if !strings.Contains(result.Content, "not in this session's scope") {
		t.Fatalf("Content = %q, want scope error", result.Content)
	}
	if store.listCalls != 0 {
		t.Fatalf("list service called for out-of-scope hub_id")
	}
}

func TestListTopicsToolRequiresHubForMultiHubSession(t *testing.T) {
	store := &fakeBrowseStore{}
	tool, err := NewListTopicsTool(newBrowseToolConfig(t, "u1", []string{"h-work", "h-personal"}, []string{"h-work", "h-personal"}, store))
	if err != nil {
		t.Fatalf("NewListTopicsTool: %v", err)
	}

	result := callBrowseTool(t, tool, ListTopicsName, listTopicsArgs{})
	if !result.IsError {
		t.Fatal("expected error when hub_id omitted in multi-hub session")
	}
	if !strings.Contains(result.Content, "hub_id is required") {
		t.Fatalf("Content = %q, want hub_id required", result.Content)
	}
}

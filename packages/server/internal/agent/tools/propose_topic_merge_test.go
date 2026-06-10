package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"
	sdktool "github.com/MemaxLabs/memax-go-agent-sdk/tool"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
)

// fakeProposeTopicMergeService records each ProposeTopicMerge
// call so tests can assert on the canonical request shape the
// wrapper produces (sorted source ids, scope-snapshot HubID,
// trimmed reason).
type fakeProposeTopicMergeService struct {
	mu       sync.Mutex
	requests []tools.ProposeTopicMergeRequest
	err      error
	outcome  tools.ProposeTopicMergeOutcome
}

func (f *fakeProposeTopicMergeService) ProposeTopicMerge(_ context.Context, in tools.ProposeTopicMergeRequest) (tools.ProposeTopicMergeOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, in)
	if f.err != nil {
		return tools.ProposeTopicMergeOutcome{}, f.err
	}
	return f.outcome, nil
}

const (
	tgtTopic = "11111111-2222-3333-4444-555555555555"
	srcA     = "aaaaaaaa-1111-1111-1111-111111111111"
	srcB     = "bbbbbbbb-2222-2222-2222-222222222222"
	srcC     = "cccccccc-3333-3333-3333-333333333333"
	notifID  = "99999999-9999-9999-9999-999999999999"
)

func makeProposeCall(t *testing.T, args tools.ProposeTopicMergeArgs) sdktool.Call {
	t.Helper()
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return sdktool.Call{
		Use: sdkmodel.ToolUse{
			ID:    "call-propose-1",
			Name:  "propose_topic_merge",
			Input: body,
		},
	}
}

func invokeProposeHandler(t *testing.T, tool sdktool.Tool, args tools.ProposeTopicMergeArgs) sdkmodel.ToolResult {
	t.Helper()
	def, ok := tool.(sdktool.Definition)
	if !ok {
		t.Fatalf("tool is not a Definition: %T", tool)
	}
	res, err := def.Handler(context.Background(), makeProposeCall(t, args))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return res
}

// Happy path: proposed merge → wire result has status=proposed,
// notification_id present, sources echoed back in canonical
// (sorted) order.
func TestProposeTopicMergeTool_HappyPath(t *testing.T) {
	t.Parallel()
	svc := &fakeProposeTopicMergeService{
		outcome: tools.ProposeTopicMergeOutcome{
			Status:         tools.ProposeStatusProposed,
			NotificationID: notifID,
			TargetTitle:    "Auth Migration",
			SourceTitles:   []string{"Auth Tickets", "OAuth Notes"},
		},
	}
	tool, err := tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	if err != nil {
		t.Fatalf("NewProposeTopicMergeTool: %v", err)
	}
	res := invokeProposeHandler(t, tool, tools.ProposeTopicMergeArgs{
		HubID:          hubA,
		TargetTopicID:  tgtTopic,
		SourceTopicIDs: []string{srcB, srcA}, // intentional reverse order
		Reason:         "  consolidate auth-related notes  ",
	})
	if res.IsError {
		t.Fatalf("IsError = true: %s", res.Content)
	}
	var got tools.ProposeTopicMergeResult
	if err := json.Unmarshal([]byte(res.Content), &got); err != nil {
		t.Fatalf("decode: %v (raw=%s)", err, res.Content)
	}
	if got.Status != tools.ProposeStatusProposed {
		t.Errorf("Status = %q, want %q", got.Status, tools.ProposeStatusProposed)
	}
	if got.NotificationID != notifID {
		t.Errorf("NotificationID = %q, want %q", got.NotificationID, notifID)
	}
	// Wire result echoes the canonicalized (sorted) sources.
	if len(got.SourceTopicIDs) != 2 || got.SourceTopicIDs[0] != srcA || got.SourceTopicIDs[1] != srcB {
		t.Errorf("SourceTopicIDs = %v, want sorted [%q, %q]", got.SourceTopicIDs, srcA, srcB)
	}

	// Service saw the canonicalized request.
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(svc.requests))
	}
	req := svc.requests[0]
	if req.OwnerID != testOwner {
		t.Errorf("OwnerID = %q, want %q", req.OwnerID, testOwner)
	}
	if req.HubID != hubA {
		t.Errorf("HubID = %q, want %q", req.HubID, hubA)
	}
	if req.TargetTopicID != tgtTopic {
		t.Errorf("TargetTopicID = %q, want %q", req.TargetTopicID, tgtTopic)
	}
	if len(req.SourceTopicIDs) != 2 || req.SourceTopicIDs[0] != srcA || req.SourceTopicIDs[1] != srcB {
		t.Errorf("SourceTopicIDs = %v, want sorted [%q, %q]", req.SourceTopicIDs, srcA, srcB)
	}
	// Reason is trimmed.
	if req.Reason != "consolidate auth-related notes" {
		t.Errorf("Reason = %q, want trimmed", req.Reason)
	}
}

// Source-key order independence: same set in different orders
// → service sees the SAME canonical SourceTopicIDs sequence.
// Codex Phase 3.6b3e v3 review pinned this as a required test
// surface to prove dedup stability.
func TestProposeTopicMergeTool_OrderIndependent(t *testing.T) {
	t.Parallel()
	svc := &fakeProposeTopicMergeService{
		outcome: tools.ProposeTopicMergeOutcome{
			Status:         tools.ProposeStatusProposed,
			NotificationID: notifID,
		},
	}
	tool, _ := tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	orderings := [][]string{
		{srcA, srcB, srcC},
		{srcC, srcB, srcA},
		{srcB, srcA, srcC},
		// duplicates + whitespace + empty entries collapse to the same canonical set.
		{srcA, "  " + srcB + "  ", srcC, srcA, "", srcB},
	}
	for i, ordering := range orderings {
		res := invokeProposeHandler(t, tool, tools.ProposeTopicMergeArgs{
			HubID:          hubA,
			TargetTopicID:  tgtTopic,
			SourceTopicIDs: ordering,
		})
		if res.IsError {
			t.Fatalf("ordering #%d: IsError = true: %s", i, res.Content)
		}
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.requests) != len(orderings) {
		t.Fatalf("requests = %d, want %d", len(svc.requests), len(orderings))
	}
	// Every request must carry the same sorted source list.
	want := []string{srcA, srcB, srcC}
	for i, req := range svc.requests {
		if len(req.SourceTopicIDs) != len(want) {
			t.Errorf("req[%d].SourceTopicIDs len = %d, want %d", i, len(req.SourceTopicIDs), len(want))
			continue
		}
		for j, s := range req.SourceTopicIDs {
			if s != want[j] {
				t.Errorf("req[%d].SourceTopicIDs[%d] = %q, want %q", i, j, s, want[j])
			}
		}
	}
}

// already_pending: service returns the upsert-no-op shape →
// wire result reflects status=already_pending (so the agent
// can tell the user "this suggestion is already in your inbox"
// instead of falsely claiming a fresh proposal).
func TestProposeTopicMergeTool_AlreadyPending(t *testing.T) {
	t.Parallel()
	svc := &fakeProposeTopicMergeService{
		outcome: tools.ProposeTopicMergeOutcome{
			Status:         tools.ProposeStatusAlreadyPending,
			NotificationID: notifID,
		},
	}
	tool, _ := tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeProposeHandler(t, tool, tools.ProposeTopicMergeArgs{
		HubID:          hubA,
		TargetTopicID:  tgtTopic,
		SourceTopicIDs: []string{srcA},
	})
	if res.IsError {
		t.Fatalf("IsError = true: %s", res.Content)
	}
	var got tools.ProposeTopicMergeResult
	_ = json.Unmarshal([]byte(res.Content), &got)
	if got.Status != tools.ProposeStatusAlreadyPending {
		t.Errorf("Status = %q, want %q", got.Status, tools.ProposeStatusAlreadyPending)
	}
}

// Source-target equality: target_topic_id appears in source
// list → wrapper rejects with IsError. The check happens
// before the service call so the service never sees a
// self-merge request.
func TestProposeTopicMergeTool_TargetInSourcesRejects(t *testing.T) {
	t.Parallel()
	svc := &fakeProposeTopicMergeService{}
	tool, _ := tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeProposeHandler(t, tool, tools.ProposeTopicMergeArgs{
		HubID:          hubA,
		TargetTopicID:  tgtTopic,
		SourceTopicIDs: []string{srcA, tgtTopic, srcB},
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true (target appearing in sources must reject)")
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.requests) != 0 {
		t.Errorf("service was called %d times, want 0 (validation must short-circuit)", len(svc.requests))
	}
}

// Empty-after-dedup source list rejects: only whitespace +
// empty + duplicate target. Wrapper short-circuits before
// hitting service.
func TestProposeTopicMergeTool_EmptySourceListRejects(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"missing":         nil,
		"all empty":       {"", "  ", " "},
		"all duplicates":  {srcA, srcA, srcA, srcA},
		"only whitespace": {"   ", "\t", "\n"},
	}
	for name, sources := range cases {
		t.Run(name, func(t *testing.T) {
			svc := &fakeProposeTopicMergeService{}
			tool, _ := tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
				Service:  svc,
				Resolver: newResolver(hubA),
				Session: agent.SessionDescriptor{
					OwnerID: testOwner,
					Type:    agent.ScopeTypeSingleHub,
					HubIDs:  []string{hubA},
				},
			})
			res := invokeProposeHandler(t, tool, tools.ProposeTopicMergeArgs{
				HubID:          hubA,
				TargetTopicID:  tgtTopic,
				SourceTopicIDs: sources,
			})
			// "all duplicates" with srcA reaches the service with one source — the
			// dedup keeps a single unique entry. So validation only rejects when
			// the canonical set is empty.
			if name == "all duplicates" {
				if res.IsError {
					t.Fatalf("IsError = true: %s", res.Content)
				}
				return
			}
			if !res.IsError {
				t.Fatalf("IsError = false, want true for %q", name)
			}
		})
	}
}

// Hub not in scope: scope.HubIDs has hubA but caller passes
// hubB → wrapper rejects with IsError before service call.
// Mirrors forget_memory's cross-scope fail-closed shape.
func TestProposeTopicMergeTool_HubOutOfScopeRejects(t *testing.T) {
	t.Parallel()
	svc := &fakeProposeTopicMergeService{}
	tool, _ := tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeProposeHandler(t, tool, tools.ProposeTopicMergeArgs{
		HubID:          hubB,
		TargetTopicID:  tgtTopic,
		SourceTopicIDs: []string{srcA},
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true (hub_id outside scope must reject)")
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.requests) != 0 {
		t.Errorf("service was called %d times, want 0", len(svc.requests))
	}
}

// Phase 3.7 step 1: a hub the user is a VIEWER on is in
// scope.HubIDs (read-eligible) but NOT in
// scope.WritableHubIDs. propose_topic_merge is a hub-write
// operation (creates a review notification scoped to the
// hub) — wrapper must fast-fail with a "read-only" message
// before hitting the service.
func TestProposeTopicMergeTool_ReadOnlyHub_Rejects(t *testing.T) {
	t.Parallel()
	svc := &fakeProposeTopicMergeService{}
	tool, _ := tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
		Service:  svc,
		Resolver: newReadOnlyResolver(hubA), // user is viewer on hubA
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeProposeHandler(t, tool, tools.ProposeTopicMergeArgs{
		HubID:          hubA,
		TargetTopicID:  tgtTopic,
		SourceTopicIDs: []string{srcA},
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true (viewer-only hub must reject)")
	}
	if !strings.Contains(res.Content, "read-only") {
		t.Errorf("err content = %q, want read-only message", res.Content)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.requests) != 0 {
		t.Errorf("service was called %d times, want 0 (fast-fail at wrapper)", len(svc.requests))
	}
}

// Empty hub_id rejects.
func TestProposeTopicMergeTool_EmptyHubID_Rejects(t *testing.T) {
	t.Parallel()
	svc := &fakeProposeTopicMergeService{}
	tool, _ := tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeProposeHandler(t, tool, tools.ProposeTopicMergeArgs{
		HubID:          "  ",
		TargetTopicID:  tgtTopic,
		SourceTopicIDs: []string{srcA},
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true (empty hub_id must reject)")
	}
}

// Empty target_topic_id rejects.
func TestProposeTopicMergeTool_EmptyTargetID_Rejects(t *testing.T) {
	t.Parallel()
	svc := &fakeProposeTopicMergeService{}
	tool, _ := tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeProposeHandler(t, tool, tools.ProposeTopicMergeArgs{
		HubID:          hubA,
		TargetTopicID:  " ",
		SourceTopicIDs: []string{srcA},
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true (empty target_topic_id must reject)")
	}
}

// Service returns ErrProposeTopicMergeNotFound → wrapper maps
// to a clean error result the agent can interpret without
// confusing it with a transient failure.
func TestProposeTopicMergeTool_NotFoundMaps(t *testing.T) {
	t.Parallel()
	svc := &fakeProposeTopicMergeService{err: tools.ErrProposeTopicMergeNotFound}
	tool, _ := tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeProposeHandler(t, tool, tools.ProposeTopicMergeArgs{
		HubID:          hubA,
		TargetTopicID:  tgtTopic,
		SourceTopicIDs: []string{srcA},
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
}

// Service returns ErrProposeTopicMergeNoPermission → wrapper
// maps to a permission-error result distinct from not-found.
func TestProposeTopicMergeTool_NoPermissionMaps(t *testing.T) {
	t.Parallel()
	svc := &fakeProposeTopicMergeService{err: tools.ErrProposeTopicMergeNoPermission}
	tool, _ := tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeProposeHandler(t, tool, tools.ProposeTopicMergeArgs{
		HubID:          hubA,
		TargetTopicID:  tgtTopic,
		SourceTopicIDs: []string{srcA},
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
}

// Service returns ErrProposeTopicMergeInvalidArgs → wrapper
// surfaces the invalid-args result. Tests the third sentinel
// path (separate from the wrapper's pre-service validation
// short-circuits, which produce their own errors).
func TestProposeTopicMergeTool_InvalidArgsMaps(t *testing.T) {
	t.Parallel()
	svc := &fakeProposeTopicMergeService{err: tools.ErrProposeTopicMergeInvalidArgs}
	tool, _ := tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeProposeHandler(t, tool, tools.ProposeTopicMergeArgs{
		HubID:          hubA,
		TargetTopicID:  tgtTopic,
		SourceTopicIDs: []string{srcA},
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
}

// Generic service error → wrapper surfaces as IsError without
// claiming a typed failure.
func TestProposeTopicMergeTool_GenericServiceErrorPropagates(t *testing.T) {
	t.Parallel()
	svc := &fakeProposeTopicMergeService{err: errors.New("db connection lost")}
	tool, _ := tools.NewProposeTopicMergeTool(tools.ProposeTopicMergeToolConfig{
		Service:  svc,
		Resolver: newResolver(hubA),
		Session: agent.SessionDescriptor{
			OwnerID: testOwner,
			Type:    agent.ScopeTypeSingleHub,
			HubIDs:  []string{hubA},
		},
	})
	res := invokeProposeHandler(t, tool, tools.ProposeTopicMergeArgs{
		HubID:          hubA,
		TargetTopicID:  tgtTopic,
		SourceTopicIDs: []string{srcA},
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
}

// Constructor validates required deps.
func TestNewProposeTopicMergeTool_Validation(t *testing.T) {
	t.Parallel()
	cases := map[string]tools.ProposeTopicMergeToolConfig{
		"nil service": {
			Service:  nil,
			Resolver: newResolver(hubA),
			Session:  agent.SessionDescriptor{OwnerID: testOwner, Type: agent.ScopeTypeSingleHub, HubIDs: []string{hubA}},
		},
		"nil resolver": {
			Service: &fakeProposeTopicMergeService{},
			Session: agent.SessionDescriptor{OwnerID: testOwner, Type: agent.ScopeTypeSingleHub, HubIDs: []string{hubA}},
		},
		"invalid session": {
			Service:  &fakeProposeTopicMergeService{},
			Resolver: newResolver(hubA),
			Session:  agent.SessionDescriptor{}, // missing OwnerID/Type → fails Validate
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tools.NewProposeTopicMergeTool(cfg); err == nil {
				t.Errorf("err = nil, want validation failure for %q", name)
			}
		})
	}
}

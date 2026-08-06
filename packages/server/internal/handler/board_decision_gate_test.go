package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func gateCreateRequest(hubID, userID, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost,
		"/v1/hubs/"+hubID+"/board/decision-gate", strings.NewReader(body))
	req.SetPathValue("id", hubID)
	return withTestIdentity(req, userID)
}

func seedGateHub(t *testing.T, s *boardsTestStore, hubID string) {
	t.Helper()
	if err := s.CreateHub(&model.Hub{ID: hubID, OwnerID: "u1", Slug: "personal", HubType: "personal"}); err != nil {
		t.Fatal(err)
	}
	s.roles[hubID+":u1"] = "owner"
}

func TestDecisionGateCreateAndLimit(t *testing.T) {
	s := newBoardsTestStore()
	seedGateHub(t, s, boardsTestHubID)
	h := NewBoardsHandler(s)

	// Invalid: one option.
	rec := httptest.NewRecorder()
	h.CreateDecisionGate(rec, gateCreateRequest(boardsTestHubID, "u1",
		`{"question":"Which queue?","options":["River"]}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("single option: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// Three gates fill the live cap.
	for i := 0; i < 3; i++ {
		rec = httptest.NewRecorder()
		h.CreateDecisionGate(rec, gateCreateRequest(boardsTestHubID, "u1",
			`{"question":"Q?","options":["A","B"],"context":"why"}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("gate %d: expected 201, got %d: %s", i, rec.Code, rec.Body.String())
		}
	}
	rec = httptest.NewRecorder()
	h.CreateDecisionGate(rec, gateCreateRequest(boardsTestHubID, "u1",
		`{"question":"One more?","options":["A","B"]}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("fourth gate: expected 409 gate_limit, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDecisionGateChooseWritesDecisionMemory(t *testing.T) {
	s := newBoardsTestStore()
	seedGateHub(t, s, boardsTestHubID)
	h := NewBoardsHandler(s)

	rec := httptest.NewRecorder()
	h.CreateDecisionGate(rec, gateCreateRequest(boardsTestHubID, "u1",
		`{"question":"Ship Lane B behind a flag?","options":["Flag first","Straight to prod"],"source_agent":"claude-code"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			Slot model.BoardSlot `json:"slot"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	slotKey := created.Data.Slot.SlotKey

	// Wrong option id rejected.
	rec = httptest.NewRecorder()
	h.ResolveSlot(rec, boardsResolveRequest(boardsTestHubID, slotKey, "u1",
		`{"action":"choose","choice":"opt-99"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad choice: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// Valid choice resolves and records the option id as verdict.
	rec = httptest.NewRecorder()
	h.ResolveSlot(rec, boardsResolveRequest(boardsTestHubID, slotKey, "u1",
		`{"action":"choose","choice":"opt-1"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("choose: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resolved struct {
		Data struct {
			Slot model.BoardSlot `json:"slot"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.Data.Slot.State != model.BoardSlotStateResolved ||
		resolved.Data.Slot.Resolution == nil || resolved.Data.Slot.Resolution.Verdict != "opt-1" {
		t.Fatalf("resolution mismatch: %#v", resolved.Data.Slot.Resolution)
	}

	// The write-back: a rationale memory containing question + choice.
	mems, err := s.ListMemories("u1", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range mems {
		if strings.Contains(m.Title, "Ship Lane B behind a flag?") &&
			strings.Contains(m.Content, "Flag first") && m.Kind == model.MemoryKindRationale {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected decision memory with question+choice, got %#v", mems)
	}
}

func TestDecisionGateChooseRejectedOnNonGateCard(t *testing.T) {
	s := newBoardsTestStore()
	seedGateHub(t, s, boardsTestHubID)
	h := NewBoardsHandler(s)
	seedBoardSlot(t, s, boardsTestHubID, "a-trace")

	rec := httptest.NewRecorder()
	h.ResolveSlot(rec, boardsResolveRequest(boardsTestHubID, "a-trace", "u1",
		`{"action":"choose","choice":"opt-1"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("choose on trace card: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

package handler

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Pure-function coverage for mcp.go helpers.

func TestMCPHubReference(t *testing.T) {
	t.Parallel()
	cases := []struct {
		hub  model.Hub
		want string
	}{
		{model.Hub{HubType: "personal", Slug: "anything"}, "personal"},
		{model.Hub{HubType: "team", Slug: "eng-42"}, "eng-42"},
		{model.Hub{HubType: "team", Slug: ""}, ""},
	}
	for _, tc := range cases {
		if got := mcpHubReference(tc.hub); got != tc.want {
			t.Errorf("mcpHubReference(%+v) = %q, want %q", tc.hub, got, tc.want)
		}
	}
}

func TestMCPMemberDisplayName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		m    model.HubMember
		want string
	}{
		{model.HubMember{UserName: "Alice", UserEmail: "a@b", UserID: "u1"}, "Alice"},
		{model.HubMember{UserEmail: "a@b", UserID: "u1"}, "a@b"},
		{model.HubMember{UserID: "u1"}, "u1"},
		{model.HubMember{}, ""},
	}
	for _, tc := range cases {
		if got := mcpMemberDisplayName(tc.m); got != tc.want {
			t.Errorf("mcpMemberDisplayName(%+v) = %q, want %q", tc.m, got, tc.want)
		}
	}
}

func TestWriteRPCError(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	writeRPCError(rec, "req-1", -32600, "Invalid request")

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q", resp.JSONRPC)
	}
	if resp.ID != "req-1" {
		t.Errorf("id = %v", resp.ID)
	}
	if resp.Error.Code != -32600 {
		t.Errorf("code = %d", resp.Error.Code)
	}
	if resp.Error.Message != "Invalid request" {
		t.Errorf("message = %q", resp.Error.Message)
	}
}

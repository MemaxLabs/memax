package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/summarize"
	"github.com/MemaxLabs/memax/packages/server/internal/meterctx"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// recordingPublisher is a test double for events.Publisher that captures
// every Publish call so assertions can verify no phantom events are emitted
// for memories the store did not actually mutate.
type recordingPublisher struct {
	events []events.Event
}

func (r *recordingPublisher) Publish(_ context.Context, evt events.Event) error {
	r.events = append(r.events, evt)
	return nil
}

func (r *recordingPublisher) Close() error { return nil }

func (r *recordingPublisher) memoryEventIDs() []string {
	out := make([]string, 0, len(r.events))
	for _, e := range r.events {
		if e.Type == events.EventTypeHubMemoriesChanged && e.EntityID != "" {
			out = append(out, e.EntityID)
		}
	}
	return out
}

type stubObjectStore struct {
	putKeys map[string][]byte
	getKeys map[string][]byte
	deleted []string
}

func (s *stubObjectStore) Put(_ context.Context, input objectstore.PutInput) error {
	if s.putKeys == nil {
		s.putKeys = make(map[string][]byte)
	}
	data, _ := io.ReadAll(input.Body)
	s.putKeys[input.Key] = data
	return nil
}

func (s *stubObjectStore) Get(_ context.Context, key string) (*objectstore.GetResult, error) {
	data := s.getKeys[key]
	return &objectstore.GetResult{
		Body:        io.NopCloser(bytes.NewReader(data)),
		ContentType: "text/plain",
		SizeBytes:   int64(len(data)),
	}, nil
}

func (s *stubObjectStore) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return nil
}

func (s *stubObjectStore) PresignPut(_ context.Context, key string, _ string, _ time.Duration) (string, map[string]string, error) {
	return "https://example.test/upload/" + key, map[string]string{"Content-Type": "text/plain"}, nil
}

func (s *stubObjectStore) PresignPutWithSize(_ context.Context, key string, _ string, expected int64, _ time.Duration) (string, map[string]string, error) {
	headers := map[string]string{"Content-Type": "text/plain"}
	if expected > 0 {
		headers["Content-Length"] = fmt.Sprintf("%d", expected)
	}
	return "https://example.test/upload/" + key, headers, nil
}

func TestRegenerateMetadataEnqueuesExistingMemory(t *testing.T) {
	s := store.NewInMemoryStore()
	memory := &model.Memory{
		ID:          "mem-regen",
		HubID:       "hub-1",
		OwnerID:     "owner-1",
		Title:       "Document",
		Content:     strings.Repeat("metadata validation content ", 12),
		ContentType: "text",
		Summary:     `{"title":"bad","summary":"cut off`,
		Hint:        "project context",
		Kind:        model.MemoryKindSemantic,
		Stability:   model.MemoryStabilityStable,
		Tags:        []string{"metadata"},
		Boundary:    "private",
		State:       "active",
		Source:      "manual",
		SourceAgent: "codex",
		SourcePath:  "notes.md",
		ProjectContext: map[string]string{
			"repo": "github.com/MemaxLabs/memax-internal",
		},
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		AccessedAt: time.Now(),
	}
	if err := s.CreateMemory(memory); err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	h := NewMemoriesHandler(s, nil, nil, summarize.New(anthropic.NewFromEnv()), nil, nil, nil, nil, nil, nil, nil)
	var enqueued struct {
		memoryID string
		ownerID  string
		req      model.PushRequest
	}
	h.SetEnqueue(func(memoryID, ownerID string, req model.PushRequest) {
		enqueued.memoryID = memoryID
		enqueued.ownerID = ownerID
		enqueued.req = req
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/memories/mem-regen/regenerate-metadata", nil)
	req.SetPathValue("id", "mem-regen")
	w := httptest.NewRecorder()
	h.RegenerateMetadata(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	if enqueued.memoryID != memory.ID || enqueued.ownerID != memory.OwnerID {
		t.Fatalf("expected enqueue for %s/%s, got %s/%s", memory.ID, memory.OwnerID, enqueued.memoryID, enqueued.ownerID)
	}
	if enqueued.req.Content != memory.Content || enqueued.req.Title != memory.Title {
		t.Fatalf("expected existing content/title to be enqueued, got %#v", enqueued.req)
	}
	if !enqueued.req.AllowRelatedContext {
		t.Fatal("expected metadata regeneration to allow related context")
	}
	if enqueued.req.ProjectContext["repo"] != "github.com/MemaxLabs/memax-internal" {
		t.Fatalf("expected project context to be preserved, got %#v", enqueued.req.ProjectContext)
	}
}

func withTestIdentity(req *http.Request, userID string) *http.Request {
	ctx := context.WithValue(req.Context(), userIDKey, userID)
	ctx = context.WithValue(ctx, hubIDKey, "11111111-1111-1111-1111-111111111111")
	ctx = context.WithValue(ctx, writeHubIDKey, "11111111-1111-1111-1111-111111111111")
	// Default unscoped session AuthContext — same shape the auth
	// middleware installs in production for cookie/JWT auth. Without
	// this, isHubScopeBounded fails closed (strict) and any handler
	// that loads memories via loadMemoryRespectingScope returns 404.
	ctx = context.WithValue(ctx, authContextKey, &AuthContext{
		UserID:       userID,
		HubScopeMode: HubScopeAllAccessible,
	})
	return req.WithContext(ctx)
}

func TestMemoriesCreatePersistsAttachmentFromFileRef(t *testing.T) {
	s := store.NewInMemoryStore()
	blob := &stubObjectStore{}
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, blob)

	body := `{
		"title":"report",
		"source":"web",
		"source_path":"report.pdf",
		"content_type":"pdf",
		"file_ref":{
			"object_key":"owners/u1/memory/20260418/abc-report.pdf",
			"filename":"report.pdf",
			"content_type":"application/pdf",
			"size_bytes":1234,
			"sha256":"abc123"
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(body))
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(resp.Data)
	var memory model.Memory
	if err := json.Unmarshal(data, &memory); err != nil {
		t.Fatal(err)
	}

	attachments, err := s.ListMemoryAttachments(memory.ID, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0].Filename != "report.pdf" {
		t.Fatalf("unexpected filename %q", attachments[0].Filename)
	}
	if memory.OriginalFileRef == "" {
		t.Fatal("expected original_file_ref to be set")
	}
	if len(memory.Attachments) != 1 {
		t.Fatalf("expected response attachments to include original attachment, got %d", len(memory.Attachments))
	}
}

func TestMemoriesCreateSetsWarningHeaderWhenAgentClaimRejected(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	body := `{
		"title":"agent-ish push",
		"content":"hello",
		"source":"api",
		"source_agent":"codex",
		"initiation_type":"agent_automatic"
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(body))
	req = withTestIdentity(req, "u1")
	req = req.WithContext(context.WithValue(req.Context(), grantContextKey, defaultGrantContext("u1")))
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Memax-Warning"); got != memaxWarningClaimRejected {
		t.Fatalf("warning header = %q, want %q", got, memaxWarningClaimRejected)
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(resp.Data)
	var memory model.Memory
	if err := json.Unmarshal(data, &memory); err != nil {
		t.Fatal(err)
	}
	if memory.Provenance == nil {
		t.Fatal("expected provenance")
	}
	if memory.Provenance.CreatedByType != model.MemoryCreatedByHuman {
		t.Fatalf("created_by_type = %q, want %q", memory.Provenance.CreatedByType, model.MemoryCreatedByHuman)
	}
	if memory.Provenance.InitiationType != model.MemoryInitiationHumanDirect {
		t.Fatalf("initiation_type = %q, want %q", memory.Provenance.InitiationType, model.MemoryInitiationHumanDirect)
	}
}

func TestMemoriesCreateAttributesHookCaptureToAuthenticatedAgent(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	body := `{
		"title":"Session capture (claude-code)",
		"content":"captured transcript",
		"source":"hook",
		"source_agent":"claude-code"
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(body))
	req = withTestIdentity(req, "u1")
	ctx := context.WithValue(req.Context(), grantContextKey, GrantContext{
		UserID:             "u1",
		PrincipalType:      "api_key",
		AgentName:          "claude-code",
		HubScopeMode:       HubScopeAllAccessible,
		DefaultPermissions: DefaultUserPermissions(),
		TrustLevel:         TrustElevated,
	})
	ctx = context.WithValue(ctx, agentNameKey, "claude-code")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(resp.Data)
	var memory model.Memory
	if err := json.Unmarshal(data, &memory); err != nil {
		t.Fatal(err)
	}
	if memory.Provenance == nil {
		t.Fatal("expected provenance")
	}
	if memory.Provenance.CreatedBySlug != "claude-code" {
		t.Fatalf("created_by_slug = %q, want claude-code", memory.Provenance.CreatedBySlug)
	}
	if memory.Provenance.CreatedByType != model.MemoryCreatedByAgent {
		t.Fatalf("created_by_type = %q, want %q", memory.Provenance.CreatedByType, model.MemoryCreatedByAgent)
	}
	if memory.Provenance.AttributionSource != model.MemoryAttributionSourceAuth {
		t.Fatalf("attribution_source = %q, want %q", memory.Provenance.AttributionSource, model.MemoryAttributionSourceAuth)
	}
}

func TestMemoriesCreateSetsReconnectWarningHeaderForUnknownOAuthGrant(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	body := `{
		"title":"unknown grant push",
		"content":"hello",
		"source":"api"
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(body))
	req = withTestIdentity(req, "u1")
	req = req.WithContext(context.WithValue(req.Context(), grantContextKey, GrantContext{
		UserID:             "u1",
		GrantID:            "grant-1",
		PrincipalType:      "oauth_grant",
		AgentName:          "unknown",
		HubScopeMode:       HubScopeAllAccessible,
		DefaultPermissions: DefaultUserPermissions(),
		TrustLevel:         TrustStandard,
	}))
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Memax-Warning"); got != memaxWarningReconnectNeeded {
		t.Fatalf("warning header = %q, want %q", got, memaxWarningReconnectNeeded)
	}
}

func TestMemoriesCreatePersistsAssistedByAgentForHumanRequestedAgent(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	body := `{
		"title":"Saved with Claude",
		"content":"important note",
		"source":"web",
		"assisted_by_agent":"claude-code",
		"initiation_type":"human_requested_agent"
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(body))
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(resp.Data)
	var memory model.Memory
	if err := json.Unmarshal(data, &memory); err != nil {
		t.Fatal(err)
	}
	if memory.Provenance == nil {
		t.Fatal("expected provenance")
	}
	if memory.Provenance.CreatedByType != model.MemoryCreatedByHuman {
		t.Fatalf("created_by_type = %q, want %q", memory.Provenance.CreatedByType, model.MemoryCreatedByHuman)
	}
	if memory.Provenance.AssistedByAgent != "claude-code" {
		t.Fatalf("assisted_by_agent = %q, want %q", memory.Provenance.AssistedByAgent, "claude-code")
	}
	if memory.Provenance.InitiationType != model.MemoryInitiationHumanRequestedAgent {
		t.Fatalf("initiation_type = %q, want %q", memory.Provenance.InitiationType, model.MemoryInitiationHumanRequestedAgent)
	}
}

func TestMemoriesCreateRejectsConflictingAttributionFields(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	body := `{
		"title":"Conflicting attribution",
		"content":"important note",
		"source":"web",
		"source_agent":"claude-code",
		"assisted_by_agent":"codex",
		"initiation_type":"human_requested_agent"
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(body))
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "conflicting_attribution_fields") {
		t.Fatalf("expected conflicting_attribution_fields error, got %s", rec.Body.String())
	}
}

func TestMemoriesCreateRejectsUnknownCollaboratorAgent(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	body := `{
		"title":"Unknown collaborator",
		"content":"important note",
		"source":"web",
		"assisted_by_agent":"gpt-7",
		"initiation_type":"human_requested_agent"
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(body))
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown_collaborator_agent") {
		t.Fatalf("expected unknown_collaborator_agent error, got %s", rec.Body.String())
	}
}

func TestMemoriesCreateRejectsCollaboratorOnAgentActorWrite(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	body := `{
		"title":"Agent actor with collaborator",
		"content":"important note",
		"source":"api",
		"source_agent":"claude-code",
		"assisted_by_agent":"codex",
		"initiation_type":"human_requested_agent"
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(body))
	req = withTestIdentity(req, "u1")
	ctx := context.WithValue(req.Context(), grantContextKey, GrantContext{
		UserID:             "u1",
		PrincipalType:      "api_key",
		AgentName:          "claude-code",
		HubScopeMode:       HubScopeAllAccessible,
		DefaultPermissions: DefaultUserPermissions(),
		TrustLevel:         TrustElevated,
	})
	ctx = context.WithValue(ctx, agentNameKey, "claude-code")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "collaborator_requires_human_actor") {
		t.Fatalf("expected collaborator_requires_human_actor error, got %s", rec.Body.String())
	}
}

func TestResolveMemoryProvenance(t *testing.T) {
	makeRequest := func(principalType string, agentName string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/v1/memories", nil)
		req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
		if principalType != "" {
			req = req.WithContext(context.WithValue(req.Context(), grantContextKey, GrantContext{
				UserID:             "u1",
				PrincipalType:      principalType,
				HubScopeMode:       HubScopeAllAccessible,
				DefaultPermissions: DefaultUserPermissions(),
				TrustLevel:         TrustElevated,
			}))
		}
		if agentName != "" {
			req = req.WithContext(context.WithValue(req.Context(), agentNameKey, agentName))
		}
		return req
	}

	tests := []struct {
		name           string
		principalType  string
		authAgent      string
		req            model.PushRequest
		wantSlug       string
		wantSource     string
		wantInitiation string
		wantRejected   bool
		wantErr        bool
	}{
		{
			name:          "auth agent wins over matching claim",
			principalType: "api_key",
			authAgent:     "codex",
			req: model.PushRequest{
				Source:      "api",
				SourceAgent: "codex",
			},
			wantSlug:       "codex",
			wantSource:     model.MemoryAttributionSourceAuth,
			wantInitiation: model.MemoryInitiationUnknown,
		},
		{
			name:          "conflicting auth and claim is rejected",
			principalType: "api_key",
			authAgent:     "codex",
			req: model.PushRequest{
				Source:      "api",
				SourceAgent: "cursor",
			},
			wantErr: true,
		},
		{
			name:          "api key can claim unknown agent on non-web path",
			principalType: "api_key",
			req: model.PushRequest{
				Source:      "api",
				SourceAgent: "openclaw",
			},
			wantSlug:       "openclaw",
			wantSource:     model.MemoryAttributionSourceClaim,
			wantInitiation: model.MemoryInitiationUnknown,
		},
		{
			name:          "oauth grant can claim on mcp path",
			principalType: "oauth_grant",
			req: model.PushRequest{
				Source:      "mcp",
				SourceAgent: "claude-code",
			},
			wantSlug:       "claude-code",
			wantSource:     model.MemoryAttributionSourceClaim,
			wantInitiation: model.MemoryInitiationUnknown,
		},
		{
			name:          "web user cannot claim by spoofing source api",
			principalType: "user",
			req: model.PushRequest{
				Source:      "api",
				SourceAgent: "codex",
			},
			wantSlug:       "",
			wantSource:     model.MemoryAttributionSourceHuman,
			wantInitiation: model.MemoryInitiationHumanDirect,
			wantRejected:   true,
		},
		{
			name:          "web source cannot claim even from agent-capable principal",
			principalType: "api_key",
			req: model.PushRequest{
				Source:      "web",
				SourceAgent: "codex",
			},
			wantSlug:       "",
			wantSource:     model.MemoryAttributionSourceHuman,
			wantInitiation: model.MemoryInitiationHumanDirect,
			wantRejected:   true,
		},
		{
			name:          "explicit initiation is preserved",
			principalType: "api_key",
			authAgent:     "codex",
			req: model.PushRequest{
				Source:         "api",
				InitiationType: model.MemoryInitiationHumanRequestedAgent,
			},
			wantSlug:       "codex",
			wantSource:     model.MemoryAttributionSourceAuth,
			wantInitiation: model.MemoryInitiationHumanRequestedAgent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := makeRequest(tt.principalType, tt.authAgent)
			provenance, slug, rejected, err := resolveMemoryProvenance(store.NewInMemoryStore(), "u1", tt.req, req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveMemoryProvenance: %v", err)
			}
			if slug != tt.wantSlug {
				t.Fatalf("slug = %q, want %q", slug, tt.wantSlug)
			}
			if provenance.AttributionSource != tt.wantSource {
				t.Fatalf("attribution_source = %q, want %q", provenance.AttributionSource, tt.wantSource)
			}
			if provenance.InitiationType != tt.wantInitiation {
				t.Fatalf("initiation_type = %q, want %q", provenance.InitiationType, tt.wantInitiation)
			}
			if rejected != tt.wantRejected {
				t.Fatalf("rejected = %v, want %v", rejected, tt.wantRejected)
			}
		})
	}
}

func TestMemoriesCreateDerivesHashForFileRefWithoutSHA(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, &stubObjectStore{})

	body := `{
		"title":"report",
		"source":"web",
		"source_path":"report.pdf",
		"content_type":"pdf",
		"file_ref":{
			"object_key":"owners/u1/memory/20260418/abc-report.pdf",
			"filename":"report.pdf",
			"content_type":"application/pdf",
			"size_bytes":1234
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(body))
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(resp.Data)
	var memory model.Memory
	if err := json.Unmarshal(data, &memory); err != nil {
		t.Fatal(err)
	}
	if memory.ContentHash == "" {
		t.Fatal("expected non-empty content hash")
	}
	if memory.ContentHash == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatal("expected file_ref upload hash to not fall back to empty-content hash")
	}
}

func TestMemoriesCreateMarksFileRefWithoutInlineContentAsProcessing(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, &stubObjectStore{})

	body := `{
		"title":"notes",
		"source":"web",
		"source_path":"notes.md",
		"content_type":"markdown",
		"file_ref":{
			"object_key":"owners/u1/memory/20260418/abc-notes.md",
			"filename":"notes.md",
			"content_type":"text/markdown",
			"size_bytes":2048,
			"sha256":"abc123"
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(body))
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(resp.Data)
	var memory model.Memory
	if err := json.Unmarshal(data, &memory); err != nil {
		t.Fatal(err)
	}
	if memory.State != "processing" {
		t.Fatalf("expected processing state for file_ref-backed memory, got %q", memory.State)
	}
}

func TestMemoriesDeleteRemovesAttachmentObject(t *testing.T) {
	s := store.NewInMemoryStore()
	blob := &stubObjectStore{}
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, blob)

	createReq := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(`{
		"title":"report",
		"source":"web",
		"source_path":"report.pdf",
		"content_type":"pdf",
		"file_ref":{
			"object_key":"owners/u1/memory/20260418/abc-report.pdf",
			"filename":"report.pdf",
			"content_type":"application/pdf",
			"size_bytes":1234,
			"sha256":"abc123"
		}
	}`))
	createReq = withTestIdentity(createReq, "u1")
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(resp.Data)
	var memory model.Memory
	if err := json.Unmarshal(data, &memory); err != nil {
		t.Fatal(err)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/v1/memories/"+memory.ID, nil)
	deleteReq.SetPathValue("id", memory.ID)
	deleteReq = withTestIdentity(deleteReq, "u1")
	deleteRec := httptest.NewRecorder()
	h.Delete(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}
	if len(blob.deleted) != 1 || blob.deleted[0] != "owners/u1/memory/20260418/abc-report.pdf" {
		t.Fatalf("expected uploaded object to be deleted, got %#v", blob.deleted)
	}
}

func TestMemoriesListUsesResolvedReadHub(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	personalHubID := "11111111-1111-1111-1111-111111111111"
	teamHubID := "22222222-2222-2222-2222-222222222222"

	personal := &model.Memory{
		ID:              "mem-personal",
		HubID:           personalHubID,
		OwnerID:         "u1",
		Title:           "personal note",
		Content:         "personal content",
		ContentType:     "markdown",
		ContentHash:     "hash-personal",
		Kind:            model.MemoryKindSemantic,
		Stability:       model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0,
		Source:          "test",
		State:           "active",
	}
	if err := s.CreateMemory(personal); err != nil {
		t.Fatal(err)
	}

	team := &model.Memory{
		ID:              "mem-team",
		HubID:           teamHubID,
		OwnerID:         "u1",
		Title:           "team note",
		Content:         "team content",
		ContentType:     "markdown",
		ContentHash:     "hash-team",
		Kind:            model.MemoryKindSemantic,
		Stability:       model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0,
		Source:          "test",
		State:           "active",
	}
	if err := s.CreateMemory(team); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/memories", nil)
	ctx := context.WithValue(req.Context(), userIDKey, "u1")
	ctx = context.WithValue(ctx, hubIDKey, teamHubID)
	ctx = context.WithValue(ctx, hubIDsKey, []string{personalHubID, teamHubID})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	data, _ := json.Marshal(resp.Data)
	var result struct {
		Memories []model.Memory `json:"memories"`
		Total    int            `json:"total"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if result.Total != 1 {
		t.Fatalf("expected total 1 for active hub scope, got %d", result.Total)
	}
	if len(result.Memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(result.Memories))
	}
	if result.Memories[0].ID == personal.ID {
		t.Fatalf("expected team hub memory only, got personal memory %q", personal.ID)
	}
	if result.Memories[0].ID != team.ID {
		t.Fatalf("expected team memory %q, got %q", team.ID, result.Memories[0].ID)
	}
	if !strings.EqualFold(result.Memories[0].Title, "team note") {
		t.Fatalf("expected team note, got %q", result.Memories[0].Title)
	}
}

func TestMemoriesCreateDoesNotDedupAcrossHubs(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	teamA := "11111111-1111-1111-1111-111111111111"
	teamB := "22222222-2222-2222-2222-222222222222"
	ownerID := "u1"
	body := `{"title":"same","content":"identical content","content_type":"markdown","source":"web"}`

	makeReq := func(hubID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(body))
		ctx := context.WithValue(req.Context(), userIDKey, ownerID)
		ctx = context.WithValue(ctx, hubIDKey, hubID)
		ctx = context.WithValue(ctx, writeHubIDKey, hubID)
		ctx = context.WithValue(ctx, hubIDsKey, []string{teamA, teamB})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		return rec
	}

	first := makeReq(teamA)
	if first.Code != http.StatusCreated {
		t.Fatalf("expected first create 201, got %d: %s", first.Code, first.Body.String())
	}

	second := makeReq(teamB)
	if second.Code != http.StatusCreated {
		t.Fatalf("expected second create in different hub to create new memory, got %d: %s", second.Code, second.Body.String())
	}

	var firstResp, secondResp model.ApiResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResp); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondResp); err != nil {
		t.Fatal(err)
	}

	firstJSON, _ := json.Marshal(firstResp.Data)
	secondJSON, _ := json.Marshal(secondResp.Data)
	var firstMem, secondMem model.Memory
	if err := json.Unmarshal(firstJSON, &firstMem); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(secondJSON, &secondMem); err != nil {
		t.Fatal(err)
	}

	if firstMem.ID == secondMem.ID {
		t.Fatalf("expected distinct memories across hubs, got same id %q", firstMem.ID)
	}
	if firstMem.HubID != teamA {
		t.Fatalf("expected first memory in hub %q, got %q", teamA, firstMem.HubID)
	}
	if secondMem.HubID != teamB {
		t.Fatalf("expected second memory in hub %q, got %q", teamB, secondMem.HubID)
	}
}

func TestMemoriesListSupportsRecentActorFilteringAndActorCounts(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	now := time.Now()
	mems := []*model.Memory{
		{
			ID:              "mem-agent",
			HubID:           "11111111-1111-1111-1111-111111111111",
			OwnerID:         "u1",
			Title:           "agent note",
			Content:         "agent",
			ContentType:     "markdown",
			ContentHash:     "hash-agent",
			Kind:            model.MemoryKindSemantic,
			Stability:       model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0,
			Source:          "web",
			SourceAgent:     "claude-code",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		},
		{
			ID:              "mem-self",
			HubID:           "11111111-1111-1111-1111-111111111111",
			OwnerID:         "u1",
			Title:           "self note",
			Content:         "self",
			ContentType:     "markdown",
			ContentHash:     "hash-self",
			Kind:            model.MemoryKindSemantic,
			Stability:       model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0,
			Source:          "web",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		},
	}
	for _, mem := range mems {
		if err := s.CreateMemory(mem); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/memories?created_after=7d&actor=agent:claude-code", nil)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(resp.Data)
	var result struct {
		Memories []model.Memory `json:"memories"`
		Total    int            `json:"total"`
		Actors   map[string]int `json:"actors"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if result.Total != 1 {
		t.Fatalf("expected total 1 after actor filter, got %d", result.Total)
	}
	if len(result.Memories) != 1 || result.Memories[0].ID != "mem-agent" {
		t.Fatalf("expected only agent memory, got %+v", result.Memories)
	}
	if result.Actors["agent:claude-code"] != 1 {
		t.Fatalf("expected actor facet for claude-code, got %#v", result.Actors)
	}
	if result.Actors["self"] != 1 {
		t.Fatalf("expected self actor facet, got %#v", result.Actors)
	}
}

func TestMemoriesListTeamHubActorCountsPreferAuthors(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	teamHubID := "22222222-2222-2222-2222-222222222222"
	if err := s.CreateHub(&model.Hub{
		ID:      teamHubID,
		Name:    "Engineering",
		Slug:    "engineering",
		HubType: "team",
		OwnerID: "u1",
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	mems := []*model.Memory{
		{
			ID:              "mem-alice",
			HubID:           teamHubID,
			OwnerID:         "u1",
			AuthorName:      "Alice",
			Title:           "alice note",
			Content:         "via claude",
			ContentType:     "markdown",
			ContentHash:     "hash-alice",
			Kind:            model.MemoryKindSemantic,
			Stability:       model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0,
			Source:          "web",
			SourceAgent:     "claude-code",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		},
		{
			ID:              "mem-bob",
			HubID:           teamHubID,
			OwnerID:         "u2",
			AuthorName:      "Bob",
			Title:           "bob note",
			Content:         "via cursor",
			ContentType:     "markdown",
			ContentHash:     "hash-bob",
			Kind:            model.MemoryKindSemantic,
			Stability:       model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0,
			Source:          "web",
			SourceAgent:     "cursor",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		},
	}
	for _, mem := range mems {
		if err := s.CreateMemory(mem); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/memories?created_after=7d", nil)
	req = withTestIdentity(req, "u1")
	ctx := context.WithValue(req.Context(), hubIDKey, teamHubID)
	ctx = context.WithValue(ctx, writeHubIDKey, teamHubID)
	ctx = context.WithValue(ctx, hubIDsKey, []string{teamHubID})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(resp.Data)
	var result struct {
		Actors map[string]int `json:"actors"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if result.Actors["author:Alice"] != 1 || result.Actors["author:Bob"] != 1 {
		t.Fatalf("expected author facets for team hub, got %#v", result.Actors)
	}
	if _, ok := result.Actors["agent:claude-code"]; ok {
		t.Fatalf("expected team hub actors to prefer authors over agents, got %#v", result.Actors)
	}
	if _, ok := result.Actors["agent:cursor"]; ok {
		t.Fatalf("expected team hub actors to prefer authors over agents, got %#v", result.Actors)
	}
}

// stubStorageMeter satisfies meterInstance for storage-bytes tests
// without spinning up a real meter + Redis. Tracks reserve/commit/
// rollback/adjust calls so tests can verify the handler's lifecycle.
type stubStorageMeter struct {
	// `allowed` controls what ReserveStorageBytes returns. Tests set
	// this to false to simulate an over-limit reservation.
	allowedBytes bool
	currentBytes int64

	memReserveCalls    int
	memCommitCalls     int
	memRollbackCalls   int
	memAdjustDelta     int
	bytesReserveCalls  int
	bytesCommitBytes   int64
	bytesRollbackBytes int64
	bytesAdjustDelta   int64
}

func newStubStorageMeter() *stubStorageMeter { return &stubStorageMeter{allowedBytes: true} }

func (s *stubStorageMeter) ReserveMemoryCount(_ context.Context, _ string, _ int) (bool, int) {
	s.memReserveCalls++
	return true, 0
}
func (s *stubStorageMeter) CommitMemoryCount(_ context.Context, _ string)   { s.memCommitCalls++ }
func (s *stubStorageMeter) RollbackMemoryCount(_ context.Context, _ string) { s.memRollbackCalls++ }
func (s *stubStorageMeter) AdjustMemoryCount(_ context.Context, _ string, delta int) {
	s.memAdjustDelta += delta
}
func (s *stubStorageMeter) ReserveHubMemoryCount(_ context.Context, _ string, _ int) (bool, int) {
	return true, 0
}
func (s *stubStorageMeter) CommitHubMemoryCount(_ context.Context, _ string)   {}
func (s *stubStorageMeter) RollbackHubMemoryCount(_ context.Context, _ string) {}
func (s *stubStorageMeter) AdjustHubMemoryCount(_ context.Context, _ string, _ int) {
}
func (s *stubStorageMeter) ReserveStorageBytes(_ context.Context, _ string, bytes, _ int64) (bool, int64) {
	s.bytesReserveCalls++
	if !s.allowedBytes {
		return false, s.currentBytes
	}
	s.currentBytes += bytes
	return true, s.currentBytes
}
func (s *stubStorageMeter) CommitStorageBytes(_ context.Context, _ string, bytes int64) {
	s.bytesCommitBytes += bytes
}
func (s *stubStorageMeter) RollbackStorageBytes(_ context.Context, _ string, bytes int64) {
	s.bytesRollbackBytes += bytes
}
func (s *stubStorageMeter) AdjustStorageBytes(_ context.Context, _ string, delta int64) {
	s.bytesAdjustDelta += delta
}

func TestMemoryCreateRejectsOverStorageLimit(t *testing.T) {
	// When the storage-bytes reservation fails, the handler must
	// return 402, roll back the memory-count reservation it just
	// took, and delete the orphaned blob (so a blocked upload doesn't
	// leave a forever-unreachable object in the bucket).
	s := store.NewInMemoryStore()
	blob := &stubObjectStore{}
	meter := newStubStorageMeter()
	meter.allowedBytes = false
	meter.currentBytes = 500 * 1024 * 1024

	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, blob)
	h.SetMeter(meter)

	body := `{
		"title":"big",
		"source":"web",
		"source_path":"big.pdf",
		"content_type":"pdf",
		"file_ref":{
			"object_key":"owners/u1/memory/20260419/abc-big.pdf",
			"filename":"big.pdf",
			"content_type":"application/pdf",
			"size_bytes":104857600,
			"sha256":"deadbeef"
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(body))
	req = withTestIdentity(req, "u1")
	// Inject UserLimits into context so the Reserve path runs.
	ctx := meterctx.WithUserLimits(req.Context(), &model.UserLimits{
		PlanID:             "personal_free",
		MemoryLimit:        300,
		StorageBytesLimit:  500 * 1024 * 1024,
		MaxAttachmentBytes: 200 * 1024 * 1024,
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"storage_bytes_limit_reached"`)) {
		t.Fatalf("expected storage_bytes_limit_reached code: %s", rec.Body.String())
	}
	// Memory-count reservation must have been rolled back, not committed.
	if meter.memRollbackCalls != 1 {
		t.Errorf("expected 1 memory-count rollback (after storage limit rejection), got %d", meter.memRollbackCalls)
	}
	if meter.memCommitCalls != 0 {
		t.Errorf("expected 0 memory-count commits, got %d", meter.memCommitCalls)
	}
	// Orphan blob should be deleted to avoid leaking a bucket object.
	if len(blob.deleted) != 1 || blob.deleted[0] != "owners/u1/memory/20260419/abc-big.pdf" {
		t.Errorf("expected orphan blob to be deleted, got %v", blob.deleted)
	}
}

func TestMemoryCreateCommitsStorageBytesOnSuccess(t *testing.T) {
	s := store.NewInMemoryStore()
	blob := &stubObjectStore{}
	meter := newStubStorageMeter()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, blob)
	h.SetMeter(meter)

	body := `{
		"title":"ok",
		"source":"web",
		"source_path":"ok.pdf",
		"content_type":"pdf",
		"file_ref":{
			"object_key":"owners/u1/memory/20260419/abc-ok.pdf",
			"filename":"ok.pdf",
			"content_type":"application/pdf",
			"size_bytes":2048,
			"sha256":"cafebabe"
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/memories", bytes.NewBufferString(body))
	req = withTestIdentity(req, "u1")
	ctx := meterctx.WithUserLimits(req.Context(), &model.UserLimits{
		PlanID:             "personal_pro",
		MemoryLimit:        5000,
		StorageBytesLimit:  5 * 1024 * 1024 * 1024,
		MaxAttachmentBytes: 200 * 1024 * 1024,
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if meter.bytesCommitBytes != 2048 {
		t.Errorf("expected CommitStorageBytes(2048), got %d", meter.bytesCommitBytes)
	}
	if meter.bytesRollbackBytes != 0 {
		t.Errorf("expected no storage rollback, got %d", meter.bytesRollbackBytes)
	}
}

func TestUploadsCreateReturnsPresignedTarget(t *testing.T) {
	h := NewUploadsHandler(&stubObjectStore{})

	req := httptest.NewRequest(http.MethodPost, "/v1/uploads", bytes.NewBufferString(`{
		"filename":"report.pdf",
		"content_type":"application/pdf",
		"size_bytes":2048,
		"purpose":"memory_attachment"
	}`))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()

	h.Create(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"upload_url"`)) {
		t.Fatalf("expected upload_url in response: %s", rec.Body.String())
	}
	// Object key should carry the memory/ prefix so sessions and attachments
	// live under distinct key spaces in the bucket.
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"object_key":"owners/u1/memory/`)) {
		t.Fatalf("expected object_key under owners/u1/memory/: %s", rec.Body.String())
	}
}

// stubUploadLimitsResolver returns a fixed byte cap for the tests that need
// plan-aware behavior without spinning up a full planresolver.
type stubUploadLimitsResolver struct {
	max int64
}

func (s stubUploadLimitsResolver) ResolveForRequest(_ context.Context, _, _ string) model.UserLimits {
	return model.UserLimits{MaxAttachmentBytes: s.max}
}

func TestUploadsCreateRejectsMissingPurpose(t *testing.T) {
	h := NewUploadsHandler(&stubObjectStore{})
	req := httptest.NewRequest(http.MethodPost, "/v1/uploads", bytes.NewBufferString(`{
		"filename":"f.pdf","content_type":"application/pdf","size_bytes":1
	}`))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing purpose, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"missing_purpose"`)) {
		t.Fatalf("expected missing_purpose error code: %s", rec.Body.String())
	}
}

func TestUploadsCreateRejectsUnknownPurpose(t *testing.T) {
	h := NewUploadsHandler(&stubObjectStore{})
	req := httptest.NewRequest(http.MethodPost, "/v1/uploads", bytes.NewBufferString(`{
		"filename":"f.pdf","content_type":"application/pdf","size_bytes":1,"purpose":"something_else"
	}`))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown purpose, got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"invalid_purpose"`)) {
		t.Fatalf("expected invalid_purpose error code: %s", rec.Body.String())
	}
}

func TestUploadsCreateRejectsDisallowedMemoryAttachmentType(t *testing.T) {
	h := NewUploadsHandler(&stubObjectStore{})
	req := httptest.NewRequest(http.MethodPost, "/v1/uploads", bytes.NewBufferString(`{
		"filename":"clip.mp4","content_type":"video/mp4","size_bytes":1024,"purpose":"memory_attachment"
	}`))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 for video upload to memory_attachment, got %d", rec.Code)
	}
}

func TestUploadsCreateRejectsOversizeMemoryAttachmentPerPlan(t *testing.T) {
	h := NewUploadsHandler(&stubObjectStore{})
	h.SetLimitsResolver(stubUploadLimitsResolver{max: 5 * 1024 * 1024}) // 5 MiB free-tier cap
	body := fmt.Sprintf(`{
		"filename":"big.pdf","content_type":"application/pdf","size_bytes":%d,"purpose":"memory_attachment"
	}`, 6*1024*1024)
	req := httptest.NewRequest(http.MethodPost, "/v1/uploads", bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversize memory_attachment, got %d", rec.Code)
	}
}

func TestUploadsCreateAllowsHigherPlanCap(t *testing.T) {
	h := NewUploadsHandler(&stubObjectStore{})
	h.SetLimitsResolver(stubUploadLimitsResolver{max: 200 * 1024 * 1024})
	body := fmt.Sprintf(`{
		"filename":"big.pdf","content_type":"application/pdf","size_bytes":%d,"purpose":"memory_attachment"
	}`, 100*1024*1024)
	req := httptest.NewRequest(http.MethodPost, "/v1/uploads", bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 under pro-plus cap, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadsCreateNormalizesContentType(t *testing.T) {
	h := NewUploadsHandler(&stubObjectStore{})
	req := httptest.NewRequest(http.MethodPost, "/v1/uploads", bytes.NewBufferString(`{
		"filename":"notes.md","content_type":"text/markdown; charset=utf-8","size_bytes":1024,"purpose":"memory_attachment"
	}`))
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 — content-type params must be stripped, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestVerifyUploadKey(t *testing.T) {
	tests := []struct {
		name      string
		objectKey string
		ownerID   string
		purpose   string
		wantErr   bool
	}{
		{"valid memory", "owners/u1/memory/20260418/abcd-file.pdf", "u1", UploadPurposeMemoryAttachment, false},
		{"session key submitted as memory", "owners/u1/sessions/20260418/abcd-f.jsonl", "u1", UploadPurposeMemoryAttachment, true},
		{"other owner's key", "owners/u2/memory/20260418/abcd-file.pdf", "u1", UploadPurposeMemoryAttachment, true},
		{"empty key", "", "u1", UploadPurposeMemoryAttachment, true},
		{"empty owner", "owners/u1/memory/20260418/f.pdf", "", UploadPurposeMemoryAttachment, true},
		{"legacy uploads prefix rejected", "owners/u1/uploads/20260418/f.pdf", "u1", UploadPurposeMemoryAttachment, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyUploadKey(tt.objectKey, tt.ownerID, tt.purpose)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyUploadKey(%q, %q, %q) err = %v, wantErr %v",
					tt.objectKey, tt.ownerID, tt.purpose, err, tt.wantErr)
			}
		})
	}
}

func TestClampMemoryAttachmentCap(t *testing.T) {
	const fiveMiB = int64(5 * 1024 * 1024)
	const hardCeiling = int64(2 * 1024 * 1024 * 1024)
	tests := []struct {
		name string
		in   int64
		want int64
	}{
		{"negative means unlimited → server ceiling", -1, hardCeiling},
		{"zero → conservative fallback", 0, fiveMiB},
		{"positive under ceiling passes through", 50 * 1024 * 1024, 50 * 1024 * 1024},
		{"positive above ceiling clamped", 100 * 1024 * 1024 * 1024, hardCeiling},
		{"exactly ceiling passes through", hardCeiling, hardCeiling},
		{"one over ceiling clamped", hardCeiling + 1, hardCeiling},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampMemoryAttachmentCap(tt.in); got != tt.want {
				t.Errorf("clampMemoryAttachmentCap(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// TestBatchMoveIsAuthoritativeAtEqualConfidence locks in the north-star
// contract rule: memories.batchMove is the authoritative user-move primitive
// and must unconditionally replace a topic assignment — even when the source
// and destination assignments share the same confidence (e.g. a manual move
// from topic A at ConfidenceUserMove → topic B at ConfidenceUserMove).
//
// This is the regression for the equal-confidence drag/drop no-op bug. The
// complementary behavior — AssignMemoryToTopic stays confidence-gated because
// it's the auto-assignment path used by ingest/dreams workers — is covered
// by TestTopicsAssignMemoryKeepsSingleAssignmentAndHonorsConfidence in
// topics_test.go.
func TestBatchMoveIsAuthoritativeAtEqualConfidence(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	hubID := "11111111-1111-1111-1111-111111111111"
	ownerID := "u1"
	now := time.Now()
	if err := s.CreateHub(&model.Hub{
		ID:        hubID,
		Name:      "Personal",
		Slug:      "personal",
		HubType:   "personal",
		OwnerID:   ownerID,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	topicA := &model.Topic{
		ID:        "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		HubID:     hubID,
		OwnerID:   ownerID,
		Name:      "Alpha",
		Icon:      "folder",
		CreatedAt: now,
		UpdatedAt: now,
	}
	topicB := &model.Topic{
		ID:        "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		HubID:     hubID,
		OwnerID:   ownerID,
		Name:      "Beta",
		Icon:      "folder",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateTopic(topicA); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTopic(topicB); err != nil {
		t.Fatal(err)
	}

	memory := &model.Memory{
		ID:              "cccccccc-cccc-cccc-cccc-cccccccccccc",
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
	// Seed the memory into topic A at ConfidenceUserMove (0.85) — the same
	// confidence the batch-move will replay. A confidence-gated primitive would
	// no-op here; the authoritative batchMove must still replace.
	if err := s.AssignMemoryToTopic(memory.ID, topicA.ID, hubID, model.ConfidenceUserMove); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"ids":      []string{memory.ID},
		"hub_id":   hubID,
		"topic_id": topicB.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-move", bytes.NewReader(body))
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{hubID}))
	rec := httptest.NewRecorder()

	h.BatchMove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	result := decodeBatchMoveResult(t, rec.Body.Bytes())
	if result.Moved != 1 {
		t.Fatalf("expected moved=1, got %d", result.Moved)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected no skipped ids, got %+v", result.Skipped)
	}

	topicIDs, err := s.GetMemoryTopicIDMap(store.VisibilityScope{OwnerID: ownerID})
	if err != nil {
		t.Fatal(err)
	}
	if got := topicIDs[memory.ID]; got != topicB.ID {
		t.Fatalf("expected memory in topic B (%q) after authoritative move, got %q", topicB.ID, got)
	}
}

// decodeBatchMoveResult unmarshals a BatchMove API response into the
// structured model so tests can assert on both moved and skipped.
func decodeBatchMoveResult(t *testing.T, body []byte) *model.BatchMoveResult {
	t.Helper()
	var resp model.ApiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode api response: %v", err)
	}
	data, _ := json.Marshal(resp.Data)
	var result model.BatchMoveResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode batch move result: %v", err)
	}
	return &result
}

// TestBatchMoveSkipsUnownedMemoriesInMixedBatch locks in partial-move
// tolerance: when a batch contains memories the caller doesn't own (e.g. a
// team-hub selection with other users' memories), the handler must move the
// owned subset without failing. The UI relies on this — `moved > 0` is
// treated as success; `moved === 0` with a non-empty batch throws so the
// user sees a permission error.
func TestBatchMoveSkipsUnownedMemoriesInMixedBatch(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	hubID := "11111111-1111-1111-1111-111111111111"
	ownerID := "u1"
	now := time.Now()
	if err := s.CreateHub(&model.Hub{
		ID:                     hubID,
		Name:                   "Team Hub",
		Slug:                   "team",
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
		ID:        "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		HubID:     hubID,
		OwnerID:   ownerID,
		Name:      "Destination",
		Icon:      "folder",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateTopic(topic); err != nil {
		t.Fatal(err)
	}

	makeMemory := func(id, owner string) *model.Memory {
		return &model.Memory{
			ID:              id,
			HubID:           hubID,
			OwnerID:         owner,
			Title:           "note " + id,
			Content:         "content",
			ContentType:     "markdown",
			ContentHash:     "hash-" + id,
			Kind:            model.MemoryKindSemantic,
			Stability:       model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0,
			Source:          "test",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		}
	}

	mine1 := makeMemory("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", ownerID)
	mine2 := makeMemory("cccccccc-cccc-cccc-cccc-cccccccccccc", ownerID)
	theirs := makeMemory("dddddddd-dddd-dddd-dddd-dddddddddddd", "u2")
	for _, m := range []*model.Memory{mine1, mine2, theirs} {
		if err := s.CreateMemory(m); err != nil {
			t.Fatal(err)
		}
	}

	body, _ := json.Marshal(map[string]any{
		"ids":      []string{mine1.ID, mine2.ID, theirs.ID},
		"hub_id":   hubID,
		"topic_id": topic.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-move", bytes.NewReader(body))
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{hubID}))
	rec := httptest.NewRecorder()

	h.BatchMove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on mixed-ownership batch, got %d: %s", rec.Code, rec.Body.String())
	}

	result := decodeBatchMoveResult(t, rec.Body.Bytes())
	if result.Moved != 2 {
		t.Fatalf("expected moved=2 (only owner's memories), got %d", result.Moved)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped id, got %+v", result.Skipped)
	}
	if result.Skipped[0].ID != theirs.ID {
		t.Fatalf("expected skipped id %q, got %q", theirs.ID, result.Skipped[0].ID)
	}
	if result.Skipped[0].Reason != model.BatchMoveSkipNotOwned {
		t.Fatalf("expected reason %q, got %q", model.BatchMoveSkipNotOwned, result.Skipped[0].Reason)
	}

	topicIDs, err := s.GetMemoryTopicIDMap(store.VisibilityScope{OwnerID: ownerID})
	if err != nil {
		t.Fatal(err)
	}
	if topicIDs[mine1.ID] != topic.ID || topicIDs[mine2.ID] != topic.ID {
		t.Fatalf("expected owner's memories in destination topic, got %#v", topicIDs)
	}
	if _, assigned := topicIDs[theirs.ID]; assigned {
		t.Fatalf("expected other user's memory untouched, but it has an assignment")
	}
}

// TestBatchMoveSkipsAlreadyAtTarget verifies that moving a memory to its
// current hub+topic is a no-op at the server level and reports a single
// skipped entry with reason "already_at_target". The UI uses this to avoid
// redundant success confirmations.
func TestBatchMoveSkipsAlreadyAtTarget(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	hubID := "11111111-1111-1111-1111-111111111111"
	ownerID := "u1"
	now := time.Now()
	if err := s.CreateHub(&model.Hub{
		ID:        hubID,
		Name:      "Personal",
		Slug:      "personal",
		HubType:   "personal",
		OwnerID:   ownerID,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	topic := &model.Topic{
		ID:        "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		HubID:     hubID,
		OwnerID:   ownerID,
		Name:      "Alpha",
		Icon:      "folder",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateTopic(topic); err != nil {
		t.Fatal(err)
	}
	memory := &model.Memory{
		ID:              "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
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
	if err := s.AssignMemoryToTopic(memory.ID, topic.ID, hubID, model.ConfidenceUserMove); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"ids":      []string{memory.ID},
		"hub_id":   hubID,
		"topic_id": topic.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-move", bytes.NewReader(body))
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{hubID}))
	rec := httptest.NewRecorder()

	h.BatchMove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	result := decodeBatchMoveResult(t, rec.Body.Bytes())
	if result.Moved != 0 {
		t.Fatalf("expected moved=0, got %d", result.Moved)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped id, got %+v", result.Skipped)
	}
	if result.Skipped[0].ID != memory.ID {
		t.Fatalf("expected skipped id %q, got %q", memory.ID, result.Skipped[0].ID)
	}
	if result.Skipped[0].Reason != model.BatchMoveSkipAlreadyAtTarget {
		t.Fatalf("expected reason %q, got %q", model.BatchMoveSkipAlreadyAtTarget, result.Skipped[0].Reason)
	}
}

// TestBatchMovePublishesEventsOnlyForMovedMemories guards against the
// phantom-event regression: the handler used to publishMemoryChanged for
// every accessible memory in the batch regardless of whether the store
// mutated it. Any skipped id (not_owned, not_found, already_at_target)
// must NOT fire a change event — otherwise realtime clients observe a move
// that never happened, which is especially dangerous for team-hub content
// owned by another user.
func TestBatchMovePublishesEventsOnlyForMovedMemories(t *testing.T) {
	s := store.NewInMemoryStore()
	pub := &recordingPublisher{}
	h := NewMemoriesHandler(s, pub, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	hubID := "11111111-1111-1111-1111-111111111111"
	ownerID := "u1"
	now := time.Now()
	if err := s.CreateHub(&model.Hub{
		ID:                     hubID,
		Name:                   "Team Hub",
		Slug:                   "team",
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
		ID:        "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		HubID:     hubID,
		OwnerID:   ownerID,
		Name:      "Destination",
		Icon:      "folder",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateTopic(topic); err != nil {
		t.Fatal(err)
	}

	makeMemory := func(id, owner string) *model.Memory {
		return &model.Memory{
			ID:              id,
			HubID:           hubID,
			OwnerID:         owner,
			Title:           "note " + id,
			Content:         "content",
			ContentType:     "markdown",
			ContentHash:     "hash-" + id,
			Kind:            model.MemoryKindSemantic,
			Stability:       model.MemoryStabilityEvolving,
			RetrievalWeight: 1.0,
			Source:          "test",
			State:           "active",
			CreatedAt:       now,
			UpdatedAt:       now,
			AccessedAt:      now,
		}
	}

	mine := makeMemory("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", ownerID)
	theirs := makeMemory("cccccccc-cccc-cccc-cccc-cccccccccccc", "u2")
	for _, m := range []*model.Memory{mine, theirs} {
		if err := s.CreateMemory(m); err != nil {
			t.Fatal(err)
		}
	}

	body, _ := json.Marshal(map[string]any{
		"ids":      []string{mine.ID, theirs.ID},
		"hub_id":   hubID,
		"topic_id": topic.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-move", bytes.NewReader(body))
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{hubID}))
	rec := httptest.NewRecorder()

	h.BatchMove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	result := decodeBatchMoveResult(t, rec.Body.Bytes())
	if result.Moved != 1 {
		t.Fatalf("expected moved=1, got %d", result.Moved)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].ID != theirs.ID {
		t.Fatalf("expected theirs skipped, got %+v", result.Skipped)
	}

	// Each moved memory fires two events (old snapshot + new snapshot),
	// so the mover loop emits exactly 2 events for the single moved id —
	// never any event referencing the unowned memory.
	emitted := pub.memoryEventIDs()
	for _, id := range emitted {
		if id == theirs.ID {
			t.Fatalf("phantom event emitted for skipped memory %q", theirs.ID)
		}
	}
	countForMine := 0
	for _, id := range emitted {
		if id == mine.ID {
			countForMine++
		}
	}
	if countForMine != 2 {
		t.Fatalf("expected 2 events (old + new) for moved memory %q, got %d — all events: %+v", mine.ID, countForMine, emitted)
	}
}

// ──────────────────────────────────────────────────────────────────────
// BatchDelete — structured result contract (mirrors BatchMove tests)
// ──────────────────────────────────────────────────────────────────────

// decodeBatchDeleteResult unmarshals a BatchDelete API response into the
// structured model so tests can assert on both deleted count and skipped
// per-id reasons.
func decodeBatchDeleteResult(t *testing.T, body []byte) *model.BatchDeleteResult {
	t.Helper()
	var resp model.ApiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode api response: %v", err)
	}
	data, _ := json.Marshal(resp.Data)
	var result model.BatchDeleteResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode batch delete result: %v", err)
	}
	return &result
}

// makeBatchDeleteMemory builds a minimal-but-valid model.Memory for use
// in BatchDelete tests. Keeps per-test noise down.
func makeBatchDeleteMemory(id, ownerID, hubID string, now time.Time) *model.Memory {
	return &model.Memory{
		ID:              id,
		HubID:           hubID,
		OwnerID:         ownerID,
		Title:           "note " + id,
		Content:         "content " + id,
		ContentType:     "markdown",
		ContentHash:     "hash-" + id,
		Kind:            model.MemoryKindSemantic,
		Stability:       model.MemoryStabilityEvolving,
		RetrievalWeight: 1.0,
		Source:          "test",
		State:           "active",
		CreatedAt:       now,
		UpdatedAt:       now,
		AccessedAt:      now,
	}
}

// setupBatchDeleteRequest frames a POST /v1/memories/batch-delete request
// with the standard identity + accessible-hubs context the handler reads.
func setupBatchDeleteRequest(t *testing.T, ids []string, ownerID, hubID string) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-delete", bytes.NewReader(body))
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{hubID}))
	return req
}

// TestBatchDeleteReturnsStructuredResult is the happy-path contract lock-in:
// all requested ids are owned, the handler returns deleted=N with an empty
// skipped slice, and exactly N change events fire (one per deleted memory,
// no phantom events).
func TestBatchDeleteReturnsStructuredResult(t *testing.T) {
	s := store.NewInMemoryStore()
	pub := &recordingPublisher{}
	h := NewMemoriesHandler(s, pub, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	hubID := "11111111-1111-1111-1111-111111111111"
	ownerID := "u1"
	now := time.Now()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "Personal", Slug: "personal", HubType: "personal",
		OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	ids := []string{
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		"cccccccc-cccc-cccc-cccc-cccccccccccc",
	}
	for _, id := range ids {
		if err := s.CreateMemory(makeBatchDeleteMemory(id, ownerID, hubID, now)); err != nil {
			t.Fatal(err)
		}
	}

	req := setupBatchDeleteRequest(t, ids, ownerID, hubID)
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchDeleteResult(t, rec.Body.Bytes())
	if result.Deleted != 3 {
		t.Fatalf("expected deleted=3, got %d", result.Deleted)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected no skipped entries, got %+v", result.Skipped)
	}

	// Phantom-event guard: exactly 3 events, each for one of our ids.
	emitted := pub.memoryEventIDs()
	if len(emitted) != 3 {
		t.Fatalf("expected 3 memory-change events, got %d: %+v", len(emitted), emitted)
	}
}

// TestBatchDeleteSkipsNotOwnedMemoriesInMixedBatch: a personal-hub batch
// that mixes the caller's memories with another user's. The other user's
// memory cannot be deleted (no hub delete path), so it must surface as
// skipped with reason not_owned, AND must NOT emit a phantom change event.
func TestBatchDeleteSkipsNotOwnedMemoriesInMixedBatch(t *testing.T) {
	s := store.NewInMemoryStore()
	pub := &recordingPublisher{}
	h := NewMemoriesHandler(s, pub, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	hubID := "11111111-1111-1111-1111-111111111111"
	ownerID := "u1"
	now := time.Now()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "Personal", Slug: "personal", HubType: "personal",
		OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	mine1 := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ownerID, hubID, now)
	mine2 := makeBatchDeleteMemory("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", ownerID, hubID, now)
	// A memory owned by u2 that shares no hub with u1 — it's simply not
	// accessible through any delete path for u1.
	theirs := makeBatchDeleteMemory("cccccccc-cccc-cccc-cccc-cccccccccccc", "u2", "", now)
	for _, m := range []*model.Memory{mine1, mine2, theirs} {
		if err := s.CreateMemory(m); err != nil {
			t.Fatal(err)
		}
	}

	// Caller u1 has access only to their personal hub. `theirs` is not
	// in scope → partitioned as not_found by the handler.
	req := setupBatchDeleteRequest(t, []string{mine1.ID, mine2.ID, theirs.ID}, ownerID, hubID)
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchDeleteResult(t, rec.Body.Bytes())
	if result.Deleted != 2 {
		t.Fatalf("expected deleted=2 (owner's memories), got %d", result.Deleted)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped entry, got %+v", result.Skipped)
	}
	if result.Skipped[0].ID != theirs.ID {
		t.Fatalf("expected skipped id %q, got %q", theirs.ID, result.Skipped[0].ID)
	}
	// Handler cannot see the u2 memory at all → it surfaces as not_found.
	if result.Skipped[0].Reason != model.BatchDeleteSkipNotFound {
		t.Fatalf("expected reason %q (not accessible to caller), got %q",
			model.BatchDeleteSkipNotFound, result.Skipped[0].Reason)
	}

	// Phantom-event guard: no event for u2's memory.
	for _, id := range pub.memoryEventIDs() {
		if id == theirs.ID {
			t.Fatalf("phantom event emitted for untouched memory %q", theirs.ID)
		}
	}
}

// TestBatchDeleteSkipsNotFoundIDs: unknown UUIDs surface as not_found.
func TestBatchDeleteSkipsNotFoundIDs(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	hubID := "11111111-1111-1111-1111-111111111111"
	ownerID := "u1"
	now := time.Now()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "Personal", Slug: "personal", HubType: "personal",
		OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	real := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ownerID, hubID, now)
	if err := s.CreateMemory(real); err != nil {
		t.Fatal(err)
	}
	bogus := "deadbeef-dead-beef-dead-beefdeadbeef"

	req := setupBatchDeleteRequest(t, []string{real.ID, bogus}, ownerID, hubID)
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchDeleteResult(t, rec.Body.Bytes())
	if result.Deleted != 1 {
		t.Fatalf("expected deleted=1, got %d", result.Deleted)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].ID != bogus {
		t.Fatalf("expected 1 skipped entry for %q, got %+v", bogus, result.Skipped)
	}
	if result.Skipped[0].Reason != model.BatchDeleteSkipNotFound {
		t.Fatalf("expected reason %q, got %q", model.BatchDeleteSkipNotFound, result.Skipped[0].Reason)
	}
}

// TestBatchDeleteFullSkipReturns200 locks in the behavior change from v1:
// a batch where every id is skipped must still return 200 OK with a
// structured result (not 403). The client decides what to show based on
// per-id skip reasons.
func TestBatchDeleteFullSkipReturns200(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	hubID := "11111111-1111-1111-1111-111111111111"
	ownerID := "u1"
	now := time.Now()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "Personal", Slug: "personal", HubType: "personal",
		OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	// Every requested id is unknown → full-skip not_found.
	ids := []string{
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
	}

	req := setupBatchDeleteRequest(t, ids, ownerID, hubID)
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on full-skip, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchDeleteResult(t, rec.Body.Bytes())
	if result.Deleted != 0 {
		t.Fatalf("expected deleted=0, got %d", result.Deleted)
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("expected 2 skipped entries, got %+v", result.Skipped)
	}
	for _, s := range result.Skipped {
		if s.Reason != model.BatchDeleteSkipNotFound {
			t.Fatalf("expected all not_found, got %+v", result.Skipped)
		}
	}
}

// TestBatchDeleteCleansUpAttachmentsForOwnedDelete verifies that an
// owned-memory batch-delete triggers the object-store Delete call for
// every attachment of every actually-deleted memory. Regression guard
// against attachment orphans on the owned path.
func TestBatchDeleteCleansUpAttachmentsForOwnedDelete(t *testing.T) {
	s := store.NewInMemoryStore()
	blob := &stubObjectStore{}
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, blob)

	hubID := "11111111-1111-1111-1111-111111111111"
	ownerID := "u1"
	now := time.Now()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "Personal", Slug: "personal", HubType: "personal",
		OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	mem := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ownerID, hubID, now)
	if err := s.CreateMemory(mem); err != nil {
		t.Fatal(err)
	}
	objectKey := "owners/u1/uploads/report.pdf"
	if err := s.CreateMemoryAttachment(&model.MemoryAttachment{
		ID:          "att-1",
		MemoryID:    mem.ID,
		OwnerID:     ownerID,
		Kind:        "original",
		Filename:    "report.pdf",
		ContentType: "application/pdf",
		SizeBytes:   1234,
		StorageKey:  objectKey,
		CreatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}

	req := setupBatchDeleteRequest(t, []string{mem.ID}, ownerID, hubID)
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(blob.deleted) != 1 || blob.deleted[0] != objectKey {
		t.Fatalf("expected object store Delete(%q), got %+v", objectKey, blob.deleted)
	}
}

// TestBatchDeleteCleansUpAttachmentsForHubAuthorizedDelete is the direct
// regression guard for the v2 correctness gap: when a moderator/admin
// deletes a contributor's memory via hub policy, the attachment objects
// owned by the contributor must still be cleaned up. Previously only the
// owned path invoked deleteAttachmentObjects, leaving hub-authorized
// deletes leaking object storage.
func TestBatchDeleteCleansUpAttachmentsForHubAuthorizedDelete(t *testing.T) {
	s := store.NewInMemoryStore()
	blob := &stubObjectStore{}
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, blob)

	hubID := "11111111-1111-1111-1111-111111111111"
	moderator := "u1"
	contributor := "u2"
	now := time.Now()
	if err := s.CreateHub(&model.Hub{
		ID:                      hubID,
		Name:                    "Team",
		Slug:                    "team",
		HubType:                 "team",
		OwnerID:                 moderator,
		ContributorDeletePolicy: "any",
		CreatedAt:               now,
		UpdatedAt:               now,
	}); err != nil {
		t.Fatal(err)
	}
	// Memory owned by the contributor, living in the team hub.
	mem := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", contributor, hubID, now)
	if err := s.CreateMemory(mem); err != nil {
		t.Fatal(err)
	}
	// Attachment is owned by the contributor — the moderator never sees
	// these rows through the owner-scoped attachment query. The handler
	// must look them up via the memory's actual owner, not the caller.
	objectKey := "owners/u2/uploads/report.pdf"
	if err := s.CreateMemoryAttachment(&model.MemoryAttachment{
		ID:          "att-hub-1",
		MemoryID:    mem.ID,
		OwnerID:     contributor,
		Kind:        "original",
		Filename:    "report.pdf",
		ContentType: "application/pdf",
		SizeBytes:   1234,
		StorageKey:  objectKey,
		CreatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}

	req := setupBatchDeleteRequest(t, []string{mem.ID}, moderator, hubID)
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchDeleteResult(t, rec.Body.Bytes())
	if result.Deleted != 1 {
		t.Fatalf("expected deleted=1, got %d", result.Deleted)
	}
	if len(blob.deleted) != 1 || blob.deleted[0] != objectKey {
		t.Fatalf("expected object store Delete(%q) for contributor attachment, got %+v",
			objectKey, blob.deleted)
	}
}

// failingDeleteStore wraps InMemoryStore so we can inject a store error
// on BatchDeleteMemories / BatchDeleteHubMemories without rewriting the
// whole store. Used to lock in the delete_failed skip reason path.
type failingDeleteStore struct {
	*store.InMemoryStore
	failOwnedBatch bool
	failHubBatch   bool
}

func (f *failingDeleteStore) BatchDeleteMemories(ids []string, ownerID string) ([]string, error) {
	if f.failOwnedBatch {
		return nil, fmt.Errorf("injected owned-batch failure")
	}
	return f.InMemoryStore.BatchDeleteMemories(ids, ownerID)
}

func (f *failingDeleteStore) BatchDeleteHubMemories(ids []string, hubID string) ([]string, error) {
	if f.failHubBatch {
		return nil, fmt.Errorf("injected hub-batch failure")
	}
	return f.InMemoryStore.BatchDeleteHubMemories(ids, hubID)
}

// TestBatchDeleteReportsDeleteFailedOnOwnedStoreError: when the store
// errors on the owned-delete call, every attempted id must surface as
// delete_failed (NOT not_found — that would misreport infra failure as
// "already gone"). The request stays 200 so partial-commit from other
// phases isn't lost.
func TestBatchDeleteReportsDeleteFailedOnOwnedStoreError(t *testing.T) {
	inner := store.NewInMemoryStore()
	s := &failingDeleteStore{InMemoryStore: inner, failOwnedBatch: true}
	blob := &stubObjectStore{}
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, blob)

	hubID := "11111111-1111-1111-1111-111111111111"
	ownerID := "u1"
	now := time.Now()
	if err := inner.CreateHub(&model.Hub{
		ID: hubID, Name: "Personal", Slug: "personal", HubType: "personal",
		OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	mem1 := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ownerID, hubID, now)
	mem2 := makeBatchDeleteMemory("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", ownerID, hubID, now)
	for _, m := range []*model.Memory{mem1, mem2} {
		if err := inner.CreateMemory(m); err != nil {
			t.Fatal(err)
		}
	}

	req := setupBatchDeleteRequest(t, []string{mem1.ID, mem2.ID}, ownerID, hubID)
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on store error (structured partial result), got %d: %s",
			rec.Code, rec.Body.String())
	}
	result := decodeBatchDeleteResult(t, rec.Body.Bytes())
	if result.Deleted != 0 {
		t.Fatalf("expected deleted=0 (owned batch failed), got %d", result.Deleted)
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("expected 2 skipped entries, got %+v", result.Skipped)
	}
	for _, entry := range result.Skipped {
		if entry.Reason != model.BatchDeleteSkipDeleteFailed {
			t.Fatalf("expected delete_failed for every attempted id, got %+v", result.Skipped)
		}
	}
	// Critical: attachment objects must NOT be deleted when the DB rows
	// still exist. Orphaning objects would be data loss.
	if len(blob.deleted) != 0 {
		t.Fatalf("expected no object-store deletes when store call errored, got %+v", blob.deleted)
	}
}

// TestBatchDeleteReportsDeleteFailedOnHubStoreError: the hub phase
// errors but the owned phase should still commit successfully. This
// exercises the partial-commit path that v1's "return 500" strategy
// would have lost.
func TestBatchDeleteReportsDeleteFailedOnHubStoreError(t *testing.T) {
	inner := store.NewInMemoryStore()
	s := &failingDeleteStore{InMemoryStore: inner, failHubBatch: true}
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	personalHub := "11111111-1111-1111-1111-111111111111"
	teamHub := "22222222-2222-2222-2222-222222222222"
	moderator := "u1"
	contributor := "u2"
	now := time.Now()
	if err := inner.CreateHub(&model.Hub{
		ID: personalHub, Name: "Personal", Slug: "personal", HubType: "personal",
		OwnerID: moderator, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := inner.CreateHub(&model.Hub{
		ID: teamHub, Name: "Team", Slug: "team", HubType: "team",
		OwnerID: moderator, ContributorDeletePolicy: "any",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	mine := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", moderator, personalHub, now)
	theirs := makeBatchDeleteMemory("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", contributor, teamHub, now)
	for _, m := range []*model.Memory{mine, theirs} {
		if err := inner.CreateMemory(m); err != nil {
			t.Fatal(err)
		}
	}

	body, _ := json.Marshal(map[string]any{"ids": []string{mine.ID, theirs.ID}})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-delete", bytes.NewReader(body))
	req = withTestIdentity(req, moderator)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{personalHub, teamHub}))
	rec := httptest.NewRecorder()
	h.BatchDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchDeleteResult(t, rec.Body.Bytes())
	// Owned phase succeeds (mine is gone), hub phase fails (theirs
	// surfaces as delete_failed, NOT not_found).
	if result.Deleted != 1 {
		t.Fatalf("expected deleted=1 (owned phase committed), got %d", result.Deleted)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped entry, got %+v", result.Skipped)
	}
	if result.Skipped[0].ID != theirs.ID || result.Skipped[0].Reason != model.BatchDeleteSkipDeleteFailed {
		t.Fatalf("expected delete_failed for %q, got %+v", theirs.ID, result.Skipped)
	}
}

// ──────────────────────────────────────────────────────────────────────
// BatchMove — source hub delete permission enforcement
// ──────────────────────────────────────────────────────────────────────
//
// These tests cover the source_delete_forbidden skip reason added to
// close the permission gap where a caller could bypass a team hub's
// contributor_delete_policy by moving their own memories out to a
// personal hub and deleting them there. The fix requires the move
// handler to check source-hub delete permission in addition to the
// existing destination-hub write check. Same-hub topic reassignments
// and personal-hub sources are carved out (no authority boundary
// crossed).

// makeTeamHub creates a team hub with an explicit contributor_delete_policy
// and returns it after storing. Keeps per-test setup concise.
func makeTeamHub(
	t *testing.T,
	s *store.InMemoryStore,
	id, name, ownerID, deletePolicy string,
	now time.Time,
) *model.Hub {
	t.Helper()
	hub := &model.Hub{
		ID:                      id,
		Name:                    name,
		Slug:                    name,
		HubType:                 "team",
		OwnerID:                 ownerID,
		ContributorDeletePolicy: deletePolicy,
		AllowContributorTopics:  true,
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	return hub
}

// setHubMemberRole fakes a non-owner role for the in-memory store,
// which otherwise hardcodes GetHubMemberRole to return "owner". Uses a
// thin wrapper store; tests that need mixed roles should define their
// own wrapper similar to failingDeleteStore.
//
// NOTE: InMemoryStore.GetHubMemberRole always returns "owner", so for
// tests where the caller needs to be a contributor (the critical case
// for source_delete_forbidden), we rely on the memory owner NOT being
// the caller and the store's store-level not_owned check firing first.
// Wait — that's not the scenario we need. We need caller == memory
// owner AND caller is not the hub owner. The InMemoryStore's role
// hardcoding means this is not directly testable without a wrapper.
//
// Solution: wrap InMemoryStore with a roleOverrideStore that lets each
// test pin the return value of GetHubMemberRole for the relevant hub.
type roleOverrideStore struct {
	*store.InMemoryStore
	roles map[string]string // hubID -> role for any caller
}

func (r *roleOverrideStore) GetHubMemberRole(hubID, _ string) (string, error) {
	if role, ok := r.roles[hubID]; ok {
		return role, nil
	}
	return r.InMemoryStore.GetHubMemberRole(hubID, "")
}

// moveOnlyStore wraps InMemoryStore and panics if BatchMoveMemories is
// called — used to assert the handler short-circuits when every id is
// filtered out at the handler level before the store call.
type moveOnlyStore struct {
	*store.InMemoryStore
	called bool
}

func (m *moveOnlyStore) BatchMoveMemories(
	ids []string,
	targetHubID, targetTopicID, ownerID string,
) (*model.BatchMoveResult, error) {
	m.called = true
	return m.InMemoryStore.BatchMoveMemories(ids, targetHubID, targetTopicID, ownerID)
}

// TestBatchMoveBlocksBypassOfNonePolicySource: the core regression
// guard for the source-delete-forbidden fix. A contributor in a team
// hub with contributor_delete_policy="none" tries to move their own
// memory out to a personal hub. Without the fix, this succeeds and
// bypasses the hub's retention policy. With the fix, the move is
// skipped with source_delete_forbidden and the memory stays put.
func TestBatchMoveBlocksBypassOfNonePolicySource(t *testing.T) {
	inner := store.NewInMemoryStore()
	// Caller is a contributor in the team hub but owner of their
	// personal hub. roleOverrideStore lets us pin the team-hub role
	// to "contributor" while the in-memory store's default "owner"
	// still applies to the personal hub.
	teamHubID := "11111111-1111-1111-1111-111111111111"
	personalHubID := "22222222-2222-2222-2222-222222222222"
	s := &roleOverrideStore{
		InMemoryStore: inner,
		roles: map[string]string{
			teamHubID:     "contributor",
			personalHubID: "owner",
		},
	}
	pub := &recordingPublisher{}
	h := NewMemoriesHandler(s, pub, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	ownerID := "u1"
	now := time.Now()
	makeTeamHub(t, inner, teamHubID, "engineering", "hub-owner", "none", now)
	inner.CreateHub(&model.Hub{
		ID:        personalHubID,
		Name:      "personal",
		Slug:      "personal",
		HubType:   "personal",
		OwnerID:   ownerID,
		CreatedAt: now,
		UpdatedAt: now,
	})

	mem := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ownerID, teamHubID, now)
	if err := inner.CreateMemory(mem); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"ids":    []string{mem.ID},
		"hub_id": personalHubID,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-move", bytes.NewReader(body))
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{teamHubID, personalHubID}))
	rec := httptest.NewRecorder()

	h.BatchMove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchMoveResult(t, rec.Body.Bytes())
	if result.Moved != 0 {
		t.Fatalf("expected moved=0 (source policy blocks), got %d", result.Moved)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped entry, got %+v", result.Skipped)
	}
	if result.Skipped[0].Reason != model.BatchMoveSkipSourceDeleteForbidden {
		t.Fatalf("expected source_delete_forbidden, got %q", result.Skipped[0].Reason)
	}
	// Memory must still be in the team hub.
	stillThere, err := inner.GetMemory(mem.ID, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if stillThere.HubID != teamHubID {
		t.Fatalf("expected memory to remain in team hub, got hub_id=%q", stillThere.HubID)
	}
	// Phantom-event guard: no change events for the blocked memory.
	for _, id := range pub.memoryEventIDs() {
		if id == mem.ID {
			t.Fatalf("phantom event emitted for blocked memory %q", mem.ID)
		}
	}
}

// TestBatchMoveAllowsContributorWithOwnPolicy: regression guard that
// the fix does not over-block. A contributor in "own" policy CAN
// delete their own memory directly, so moving it out should also work.
func TestBatchMoveAllowsContributorWithOwnPolicy(t *testing.T) {
	inner := store.NewInMemoryStore()
	teamHubID := "11111111-1111-1111-1111-111111111111"
	personalHubID := "22222222-2222-2222-2222-222222222222"
	s := &roleOverrideStore{
		InMemoryStore: inner,
		roles: map[string]string{
			teamHubID:     "contributor",
			personalHubID: "owner",
		},
	}
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	ownerID := "u1"
	now := time.Now()
	makeTeamHub(t, inner, teamHubID, "engineering", "hub-owner", "own", now)
	inner.CreateHub(&model.Hub{
		ID: personalHubID, Name: "personal", Slug: "personal",
		HubType: "personal", OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	})
	mem := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ownerID, teamHubID, now)
	inner.CreateMemory(mem)

	body, _ := json.Marshal(map[string]any{"ids": []string{mem.ID}, "hub_id": personalHubID})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-move", bytes.NewReader(body))
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{teamHubID, personalHubID}))
	rec := httptest.NewRecorder()

	h.BatchMove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchMoveResult(t, rec.Body.Bytes())
	if result.Moved != 1 {
		t.Fatalf("expected moved=1 (own policy permits), got %d — skipped=%+v", result.Moved, result.Skipped)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected no skipped entries, got %+v", result.Skipped)
	}
}

// TestBatchMoveAllowsContributorWithAnyPolicy: same regression guard
// for "any" delete policy. Contributors can delete anyone's memory,
// so they can also move their own out.
func TestBatchMoveAllowsContributorWithAnyPolicy(t *testing.T) {
	inner := store.NewInMemoryStore()
	teamHubID := "11111111-1111-1111-1111-111111111111"
	personalHubID := "22222222-2222-2222-2222-222222222222"
	s := &roleOverrideStore{
		InMemoryStore: inner,
		roles: map[string]string{
			teamHubID:     "contributor",
			personalHubID: "owner",
		},
	}
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	ownerID := "u1"
	now := time.Now()
	makeTeamHub(t, inner, teamHubID, "engineering", "hub-owner", "any", now)
	inner.CreateHub(&model.Hub{
		ID: personalHubID, Name: "personal", Slug: "personal",
		HubType: "personal", OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	})
	mem := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ownerID, teamHubID, now)
	inner.CreateMemory(mem)

	body, _ := json.Marshal(map[string]any{"ids": []string{mem.ID}, "hub_id": personalHubID})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-move", bytes.NewReader(body))
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{teamHubID, personalHubID}))
	rec := httptest.NewRecorder()

	h.BatchMove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchMoveResult(t, rec.Body.Bytes())
	if result.Moved != 1 {
		t.Fatalf("expected moved=1 (any policy permits), got %d — skipped=%+v", result.Moved, result.Skipped)
	}
}

// TestBatchMoveSameHubTopicReassignmentSkipsSourceCheck: critical
// carve-out regression guard. Moving a memory between topics within
// the SAME team hub crosses no authority boundary — the memory stays
// in the hub with the same policy. Even a "none"-policy hub must
// allow contributors to reorganize their own memories via topic
// reassignment.
func TestBatchMoveSameHubTopicReassignmentSkipsSourceCheck(t *testing.T) {
	inner := store.NewInMemoryStore()
	teamHubID := "11111111-1111-1111-1111-111111111111"
	s := &roleOverrideStore{
		InMemoryStore: inner,
		roles:         map[string]string{teamHubID: "contributor"},
	}
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	ownerID := "u1"
	now := time.Now()
	makeTeamHub(t, inner, teamHubID, "engineering", "hub-owner", "none", now)

	topicA := &model.Topic{
		ID:    "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		HubID: teamHubID, OwnerID: ownerID, Name: "Alpha", Icon: "folder",
		CreatedAt: now, UpdatedAt: now,
	}
	topicB := &model.Topic{
		ID:    "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		HubID: teamHubID, OwnerID: ownerID, Name: "Beta", Icon: "folder",
		CreatedAt: now, UpdatedAt: now,
	}
	inner.CreateTopic(topicA)
	inner.CreateTopic(topicB)

	mem := makeBatchDeleteMemory("cccccccc-cccc-cccc-cccc-cccccccccccc", ownerID, teamHubID, now)
	inner.CreateMemory(mem)
	inner.AssignMemoryToTopic(mem.ID, topicA.ID, teamHubID, model.ConfidenceUserMove)

	body, _ := json.Marshal(map[string]any{
		"ids":      []string{mem.ID},
		"hub_id":   teamHubID,
		"topic_id": topicB.ID,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-move", bytes.NewReader(body))
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{teamHubID}))
	rec := httptest.NewRecorder()

	h.BatchMove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchMoveResult(t, rec.Body.Bytes())
	if result.Moved != 1 {
		t.Fatalf("expected moved=1 (same-hub carve-out), got %d — skipped=%+v",
			result.Moved, result.Skipped)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected no skipped entries for same-hub reassignment, got %+v",
			result.Skipped)
	}
}

// TestBatchMovePersonalHubSkipsSourceCheck: second carve-out regression
// guard. Memories in personal hubs (or unassigned, hub_id="") have no
// team policy to enforce — the caller has full authority over their
// own data. Moving FROM a personal hub TO a team hub should only
// require destination write permission.
func TestBatchMovePersonalHubSkipsSourceCheck(t *testing.T) {
	inner := store.NewInMemoryStore()
	personalHubID := "22222222-2222-2222-2222-222222222222"
	teamHubID := "11111111-1111-1111-1111-111111111111"
	s := &roleOverrideStore{
		InMemoryStore: inner,
		roles: map[string]string{
			personalHubID: "owner",
			teamHubID:     "contributor",
		},
	}
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	ownerID := "u1"
	now := time.Now()
	inner.CreateHub(&model.Hub{
		ID: personalHubID, Name: "personal", Slug: "personal",
		HubType: "personal", OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	})
	makeTeamHub(t, inner, teamHubID, "engineering", "hub-owner", "none", now)

	mem := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ownerID, personalHubID, now)
	inner.CreateMemory(mem)

	body, _ := json.Marshal(map[string]any{"ids": []string{mem.ID}, "hub_id": teamHubID})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-move", bytes.NewReader(body))
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{teamHubID, personalHubID}))
	rec := httptest.NewRecorder()

	h.BatchMove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchMoveResult(t, rec.Body.Bytes())
	if result.Moved != 1 {
		t.Fatalf("expected moved=1 (personal-hub carve-out), got %d — skipped=%+v",
			result.Moved, result.Skipped)
	}
}

// TestBatchMoveExMemberCannotMoveOwnedMemoryOut: locks in the STRICT
// ex-member behavior. A user who owns a memory in a team hub but is
// no longer a member (GetHubMemberRole returns "") cannot move their
// own memory out. This is a BEHAVIOR TIGHTENING — current (pre-fix)
// code lets ex-members reclaim their memories via the bypass.
//
// Under strict, the hub's retention policy applies uniformly: if
// contributors can't scrub history while they're members, they can't
// scrub it after leaving either. Releasing a stuck memory requires
// hub admin tooling (not yet built) or re-joining the hub.
func TestBatchMoveExMemberCannotMoveOwnedMemoryOut(t *testing.T) {
	inner := store.NewInMemoryStore()
	teamHubID := "11111111-1111-1111-1111-111111111111"
	personalHubID := "22222222-2222-2222-2222-222222222222"
	// Ex-member has NO role in teamHubID (empty string), but is owner
	// of their personal hub. The in-memory store's default
	// GetHubMemberRole returns "owner", so we must explicitly pin ""
	// for the team hub.
	s := &roleOverrideStore{
		InMemoryStore: inner,
		roles: map[string]string{
			teamHubID:     "",
			personalHubID: "owner",
		},
	}
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	ownerID := "u1"
	now := time.Now()
	makeTeamHub(t, inner, teamHubID, "engineering", "hub-owner", "any", now)
	inner.CreateHub(&model.Hub{
		ID: personalHubID, Name: "personal", Slug: "personal",
		HubType: "personal", OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	})
	mem := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ownerID, teamHubID, now)
	inner.CreateMemory(mem)

	// Caller's accessibleHubIDs does NOT include teamHubID — they're
	// not a member. But GetAccessibleMemories still returns the memory
	// via the ownership carve-out at memory.go:125.
	body, _ := json.Marshal(map[string]any{"ids": []string{mem.ID}, "hub_id": personalHubID})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-move", bytes.NewReader(body))
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{personalHubID}))
	rec := httptest.NewRecorder()

	h.BatchMove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchMoveResult(t, rec.Body.Bytes())
	if result.Moved != 0 {
		t.Fatalf("expected moved=0 (ex-member locked out), got %d", result.Moved)
	}
	if len(result.Skipped) != 1 ||
		result.Skipped[0].Reason != model.BatchMoveSkipSourceDeleteForbidden {
		t.Fatalf("expected source_delete_forbidden for ex-member, got %+v",
			result.Skipped)
	}
}

// TestBatchMoveMixedBatchPartialSuccess: one movable id + one
// handler-blocked id in the same request. Asserts the movable id
// goes through, the blocked id is in result.Skipped with the right
// reason, and the phantom-event filter does not emit an event for
// the blocked id.
func TestBatchMoveMixedBatchPartialSuccess(t *testing.T) {
	inner := store.NewInMemoryStore()
	teamHubID := "11111111-1111-1111-1111-111111111111"
	personalHubID := "22222222-2222-2222-2222-222222222222"
	s := &roleOverrideStore{
		InMemoryStore: inner,
		roles: map[string]string{
			teamHubID:     "contributor",
			personalHubID: "owner",
		},
	}
	pub := &recordingPublisher{}
	h := NewMemoriesHandler(s, pub, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	ownerID := "u1"
	now := time.Now()
	makeTeamHub(t, inner, teamHubID, "engineering", "hub-owner", "none", now)
	inner.CreateHub(&model.Hub{
		ID: personalHubID, Name: "personal", Slug: "personal",
		HubType: "personal", OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	})

	// Memory in the blocked team hub.
	blocked := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ownerID, teamHubID, now)
	inner.CreateMemory(blocked)
	// Memory in the personal hub (carve-out allows the move).
	movable := makeBatchDeleteMemory("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", ownerID, personalHubID, now)
	inner.CreateMemory(movable)

	// Target: a second personal hub (really just the personal hub itself
	// renamed; both are-writable for the caller). The test structure is:
	// movable goes to target, blocked stays in teamHub.
	targetHubID := "33333333-3333-3333-3333-333333333333"
	inner.CreateHub(&model.Hub{
		ID: targetHubID, Name: "target", Slug: "target",
		HubType: "personal", OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	})
	s.roles[targetHubID] = "owner"

	body, _ := json.Marshal(map[string]any{
		"ids":    []string{blocked.ID, movable.ID},
		"hub_id": targetHubID,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-move", bytes.NewReader(body))
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{teamHubID, personalHubID, targetHubID}))
	rec := httptest.NewRecorder()

	h.BatchMove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchMoveResult(t, rec.Body.Bytes())
	if result.Moved != 1 {
		t.Fatalf("expected moved=1 (movable went through), got %d", result.Moved)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped entry, got %+v", result.Skipped)
	}
	if result.Skipped[0].ID != blocked.ID ||
		result.Skipped[0].Reason != model.BatchMoveSkipSourceDeleteForbidden {
		t.Fatalf("expected source_delete_forbidden for %q, got %+v",
			blocked.ID, result.Skipped)
	}

	// Phantom-event guard: the blocked id must NOT appear in any
	// published event. Only the movable id should emit events
	// (old + new snapshot = 2 events).
	for _, id := range pub.memoryEventIDs() {
		if id == blocked.ID {
			t.Fatalf("phantom event emitted for blocked memory %q", blocked.ID)
		}
	}
}

// TestBatchMoveFullHandlerSkipShortCircuitsStore: every id is blocked
// at the handler level. Asserts the store is NOT invoked (via
// moveOnlyStore.called) and the handler returns 200 with the
// handler-only skip list.
func TestBatchMoveFullHandlerSkipShortCircuitsStore(t *testing.T) {
	inner := store.NewInMemoryStore()
	teamHubID := "11111111-1111-1111-1111-111111111111"
	personalHubID := "22222222-2222-2222-2222-222222222222"
	moveSpy := &moveOnlyStore{InMemoryStore: inner}
	s := &roleOverrideMoveStore{
		moveOnlyStore: moveSpy,
		roles: map[string]string{
			teamHubID:     "contributor",
			personalHubID: "owner",
		},
	}
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	ownerID := "u1"
	now := time.Now()
	makeTeamHub(t, inner, teamHubID, "engineering", "hub-owner", "none", now)
	inner.CreateHub(&model.Hub{
		ID: personalHubID, Name: "personal", Slug: "personal",
		HubType: "personal", OwnerID: ownerID, CreatedAt: now, UpdatedAt: now,
	})
	mem1 := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ownerID, teamHubID, now)
	mem2 := makeBatchDeleteMemory("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", ownerID, teamHubID, now)
	inner.CreateMemory(mem1)
	inner.CreateMemory(mem2)

	body, _ := json.Marshal(map[string]any{
		"ids":    []string{mem1.ID, mem2.ID},
		"hub_id": personalHubID,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/memories/batch-move", bytes.NewReader(body))
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, []string{teamHubID, personalHubID}))
	rec := httptest.NewRecorder()

	h.BatchMove(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	result := decodeBatchMoveResult(t, rec.Body.Bytes())
	if result.Moved != 0 {
		t.Fatalf("expected moved=0, got %d", result.Moved)
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("expected 2 skipped entries, got %+v", result.Skipped)
	}
	if moveSpy.called {
		t.Fatalf("expected store.BatchMoveMemories to be short-circuited, but it was called")
	}
}

// roleOverrideMoveStore composes roleOverrideStore + moveOnlyStore to
// give the short-circuit test both the role pinning and the store-call
// spy without duplicating code.
type roleOverrideMoveStore struct {
	*moveOnlyStore
	roles map[string]string
}

func (r *roleOverrideMoveStore) GetHubMemberRole(hubID, _ string) (string, error) {
	if role, ok := r.roles[hubID]; ok {
		return role, nil
	}
	return r.moveOnlyStore.GetHubMemberRole(hubID, "")
}

// ── tryAutoHealAPIKeyAgent ──

// healCallingStore records HealAPIKeyAgent calls and returns the
// configured effective slug. Used by auto-heal unit tests to verify
// when the helper invokes the store vs short-circuits.
type healCallingStore struct {
	*store.InMemoryStore
	calls           int
	effective       string
	returnErr       error
	lastKeyID       string
	lastUserID      string
	lastClaimedSlug string
}

func (s *healCallingStore) HealAPIKeyAgent(_ context.Context, keyID, userID, claimedSlug string) (string, error) {
	s.calls++
	s.lastKeyID = keyID
	s.lastUserID = userID
	s.lastClaimedSlug = claimedSlug
	if s.returnErr != nil {
		return "", s.returnErr
	}
	if s.effective == "" {
		return claimedSlug, nil
	}
	return s.effective, nil
}

func TestTryAutoHealAPIKeyAgent(t *testing.T) {
	newRequest := func(principalType, agentName, grantID string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/v1/memories", nil)
		ctx := context.WithValue(req.Context(), authContextKey, &AuthContext{
			UserID:        "u1",
			PrincipalType: principalType,
			GrantID:       grantID,
			AgentName:     agentName,
		})
		return req.WithContext(ctx)
	}

	t.Run("no-op when attribution is not claim-based", func(t *testing.T) {
		s := &healCallingStore{InMemoryStore: store.NewInMemoryStore()}
		req := newRequest("api_key", "", "key-1")
		conflict, err := tryAutoHealAPIKeyAgent(context.Background(), s, req, "hatch", model.MemoryAttributionSourceAuth)
		if err != nil || conflict {
			t.Fatalf("unexpected: conflict=%v err=%v", conflict, err)
		}
		if s.calls != 0 {
			t.Fatalf("expected no store call, got %d", s.calls)
		}
	})

	t.Run("no-op when claimed slug is empty", func(t *testing.T) {
		s := &healCallingStore{InMemoryStore: store.NewInMemoryStore()}
		req := newRequest("api_key", "", "key-1")
		conflict, err := tryAutoHealAPIKeyAgent(context.Background(), s, req, "", model.MemoryAttributionSourceClaim)
		if err != nil || conflict {
			t.Fatalf("unexpected: conflict=%v err=%v", conflict, err)
		}
		if s.calls != 0 {
			t.Fatalf("expected no store call, got %d", s.calls)
		}
	})

	t.Run("skips non-api_key principals", func(t *testing.T) {
		s := &healCallingStore{InMemoryStore: store.NewInMemoryStore()}
		req := newRequest("oauth_grant", "", "grant-1")
		conflict, err := tryAutoHealAPIKeyAgent(context.Background(), s, req, "hatch", model.MemoryAttributionSourceClaim)
		if err != nil || conflict {
			t.Fatalf("unexpected: conflict=%v err=%v", conflict, err)
		}
		if s.calls != 0 {
			t.Fatalf("expected no store call, got %d", s.calls)
		}
	})

	t.Run("skips when auth already has an agent (defensive)", func(t *testing.T) {
		s := &healCallingStore{InMemoryStore: store.NewInMemoryStore()}
		req := newRequest("api_key", "existing", "key-1")
		conflict, err := tryAutoHealAPIKeyAgent(context.Background(), s, req, "hatch", model.MemoryAttributionSourceClaim)
		if err != nil || conflict {
			t.Fatalf("unexpected: conflict=%v err=%v", conflict, err)
		}
		if s.calls != 0 {
			t.Fatalf("expected no store call, got %d", s.calls)
		}
	})

	t.Run("calls store and accepts when effective slug matches claim", func(t *testing.T) {
		s := &healCallingStore{InMemoryStore: store.NewInMemoryStore(), effective: "hatch"}
		req := newRequest("api_key", "", "key-1")
		conflict, err := tryAutoHealAPIKeyAgent(context.Background(), s, req, "hatch", model.MemoryAttributionSourceClaim)
		if err != nil || conflict {
			t.Fatalf("unexpected: conflict=%v err=%v", conflict, err)
		}
		if s.calls != 1 || s.lastKeyID != "key-1" || s.lastUserID != "u1" || s.lastClaimedSlug != "hatch" {
			t.Fatalf("unexpected store args: %+v", s)
		}
	})

	t.Run("returns conflict when effective slug differs", func(t *testing.T) {
		s := &healCallingStore{InMemoryStore: store.NewInMemoryStore(), effective: "other-agent"}
		req := newRequest("api_key", "", "key-1")
		conflict, err := tryAutoHealAPIKeyAgent(context.Background(), s, req, "hatch", model.MemoryAttributionSourceClaim)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if !conflict {
			t.Fatal("expected conflict when stored slug differs from claim")
		}
	})

	t.Run("lets push proceed when row is standalone or missing", func(t *testing.T) {
		s := &healCallingStore{InMemoryStore: store.NewInMemoryStore(), effective: "__empty__"}
		// Force the effective slug to "" via the returnErr-less "already populated"
		// path: override by setting effective to empty-sentinel and clearing it.
		s.effective = ""
		// make returnErr nil but override effective: since effective == "" the
		// mock returns claimedSlug. To represent "row not found", simulate via
		// returnErr set to a generic error — the helper logs and returns no
		// conflict. That's the same outcome from the caller's perspective.
		s.returnErr = errors.New("not found")
		req := newRequest("api_key", "", "key-1")
		conflict, err := tryAutoHealAPIKeyAgent(context.Background(), s, req, "hatch", model.MemoryAttributionSourceClaim)
		if err != nil || conflict {
			t.Fatalf("expected no conflict on store error; got conflict=%v err=%v", conflict, err)
		}
	})
}

// --- GET /v1/memories/{id}/related ---
//
// Related derives neighbor scope from the source memory's hub, NOT the
// session's active hub. These tests pin the authorization and short-circuit
// behavior described in handler.Related.

func newRelatedRequest(t *testing.T, memoryID, ownerID string, hubIDs []string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/memories/"+memoryID+"/related", nil)
	req.SetPathValue("id", memoryID)
	req = withTestIdentity(req, ownerID)
	req = req.WithContext(context.WithValue(req.Context(), hubIDsKey, hubIDs))
	return req
}

func TestMemoriesRelatedReturns404ForInaccessibleMemory(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	now := time.Now()
	ownerID := "u1"
	otherHubID := "99999999-9999-9999-9999-999999999999"
	foreignMem := makeBatchDeleteMemory("ffffffff-ffff-ffff-ffff-ffffffffffff", "other-user", otherHubID, now)
	s.CreateMemory(foreignMem)

	// Caller has NO access to otherHubID — the oracle-closing path.
	req := newRelatedRequest(t, foreignMem.ID, ownerID, []string{"11111111-1111-1111-1111-111111111111"})
	rec := httptest.NewRecorder()

	h.Related(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for inaccessible memory, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMemoriesRelatedReturnsEmptyForProcessingMemory(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	now := time.Now()
	ownerID := "u1"
	hubID := "11111111-1111-1111-1111-111111111111"
	mem := makeBatchDeleteMemory("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ownerID, hubID, now)
	mem.State = "processing"
	s.CreateMemory(mem)

	req := newRelatedRequest(t, mem.ID, ownerID, []string{hubID})
	rec := httptest.NewRecorder()

	h.Related(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for processing memory, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(resp.Data)
	var related []model.RelatedMemory
	if err := json.Unmarshal(data, &related); err != nil {
		t.Fatal(err)
	}
	if len(related) != 0 {
		t.Fatalf("expected empty result for processing memory, got %d", len(related))
	}
}

func TestMemoriesRelatedReturnsEmptyForActiveMemoryWithoutEmbeddings(t *testing.T) {
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	now := time.Now()
	ownerID := "u1"
	hubID := "11111111-1111-1111-1111-111111111111"
	mem := makeBatchDeleteMemory("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", ownerID, hubID, now)
	s.CreateMemory(mem)

	req := newRelatedRequest(t, mem.ID, ownerID, []string{hubID})
	rec := httptest.NewRecorder()

	// InMemoryStore has no embeddings — FindRelatedMemories returns nil, nil.
	// Handler normalizes that to []. Pins: 200 + data=[] is the contract, not 500.
	h.Related(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"data":[]`) {
		t.Fatalf("expected data=[], got body: %s", rec.Body.String())
	}
}

func TestMemoriesRelatedIgnoresSessionHubMismatch(t *testing.T) {
	// Regression guard for the session-coupled bug: opening a memory in
	// hub A while the session's active hub is hub B. The handler must
	// derive neighbor scope from the memory's own hub, not from any
	// X-Hub-ID header / GetHubID value. Even if session hubID is empty
	// (the original 500 path), the handler must not blow up.
	s := store.NewInMemoryStore()
	h := NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	now := time.Now()
	ownerID := "u1"
	memoryHub := "11111111-1111-1111-1111-111111111111"
	sessionHub := "22222222-2222-2222-2222-222222222222"
	mem := makeBatchDeleteMemory("cccccccc-cccc-cccc-cccc-cccccccccccc", ownerID, memoryHub, now)
	s.CreateMemory(mem)

	req := httptest.NewRequest(http.MethodGet, "/v1/memories/"+mem.ID+"/related", nil)
	req.SetPathValue("id", mem.ID)
	ctx := context.WithValue(req.Context(), userIDKey, ownerID)
	// Session hub deliberately wrong — and we assert the handler doesn't use it.
	ctx = context.WithValue(ctx, hubIDKey, sessionHub)
	ctx = context.WithValue(ctx, hubIDsKey, []string{memoryHub, sessionHub})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.Related(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 regardless of session hub mismatch, got %d: %s", rec.Code, rec.Body.String())
	}
}

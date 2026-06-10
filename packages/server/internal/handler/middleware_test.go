package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwtauth "github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type hubContextTestStore struct {
	*store.InMemoryStore
	personalHub *model.Hub
	hubs        []model.HubWithRole
	roles       map[string]string
}

func (s *hubContextTestStore) GetPersonalHub(_ string) (*model.Hub, error) {
	return s.personalHub, nil
}

func (s *hubContextTestStore) ListUserHubs(_ string) ([]model.HubWithRole, error) {
	return s.hubs, nil
}

func (s *hubContextTestStore) GetHubMemberRole(hubID, _ string) (string, error) {
	return s.roles[hubID], nil
}

func TestHubContextFallsBackToPersonalWithoutExplicitReadHub(t *testing.T) {
	personal := &model.Hub{ID: "11111111-1111-1111-1111-111111111111", Name: "Personal"}
	team := &model.Hub{ID: "22222222-2222-2222-2222-222222222222", Name: "Team"}
	s := &hubContextTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		personalHub:   personal,
		hubs: []model.HubWithRole{
			{Hub: *personal, Role: "owner"},
			{Hub: *team, Role: "contributor"},
		},
		roles: map[string]string{
			personal.ID: "owner",
			team.ID:     "contributor",
		},
	}

	var got struct {
		ReadHub  string   `json:"read_hub"`
		BoostHub string   `json:"boost_hub"`
		WriteHub string   `json:"write_hub"`
		HubIDs   []string `json:"hub_ids"`
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.ReadHub = GetHubID(r)
		got.BoostHub = GetRetrievalBoostHubID(r)
		got.WriteHub = GetWriteHubID(r)
		got.HubIDs = GetAccessibleHubIDs(r)
		_ = json.NewEncoder(w).Encode(got)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/memories", nil)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()

	HubContext(s)(next).ServeHTTP(rec, req)

	if got.ReadHub != personal.ID {
		t.Fatalf("expected read hub %q, got %q", personal.ID, got.ReadHub)
	}
	if got.BoostHub != "" {
		t.Fatalf("expected no implicit retrieval boost hub, got %q", got.BoostHub)
	}
	if got.WriteHub != personal.ID {
		t.Fatalf("expected write hub %q, got %q", personal.ID, got.WriteHub)
	}
	if len(got.HubIDs) != 2 {
		t.Fatalf("expected 2 accessible hubs, got %d", len(got.HubIDs))
	}
}

func TestHubContextUsesExplicitHeaderForWrites(t *testing.T) {
	personal := &model.Hub{ID: "11111111-1111-1111-1111-111111111111", Name: "Personal"}
	team := &model.Hub{ID: "22222222-2222-2222-2222-222222222222", Name: "Team"}
	s := &hubContextTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		personalHub:   personal,
		hubs: []model.HubWithRole{
			{Hub: *personal, Role: "owner"},
			{Hub: *team, Role: "contributor"},
		},
		roles: map[string]string{
			personal.ID: "owner",
			team.ID:     "contributor",
		},
	}

	var gotWriteHub string
	var gotBoostHub string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotWriteHub = GetWriteHubID(r)
		gotBoostHub = GetRetrievalBoostHubID(r)
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/memories", nil)
	req.Header.Set("X-Hub-ID", team.ID)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()

	HubContext(s)(next).ServeHTTP(rec, req)

	if gotWriteHub != team.ID {
		t.Fatalf("expected explicit write hub %q, got %q", team.ID, gotWriteHub)
	}
	if gotBoostHub != team.ID {
		t.Fatalf("expected explicit boost hub %q, got %q", team.ID, gotBoostHub)
	}
}

func TestHubContextAppliesReadOnlyGrantPermissions(t *testing.T) {
	personal := &model.Hub{ID: "11111111-1111-1111-1111-111111111111", Name: "Personal"}
	team := &model.Hub{ID: "22222222-2222-2222-2222-222222222222", Name: "Team"}
	s := &hubContextTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		personalHub:   personal,
		hubs: []model.HubWithRole{
			{Hub: *personal, Role: "owner"},
			{Hub: *team, Role: "contributor"},
		},
		roles: map[string]string{
			personal.ID: "owner",
			team.ID:     "contributor",
		},
	}

	var canReadPersonal, canWritePersonal bool
	var hubIDs []string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		canReadPersonal = Can(r, PermMemoryRead, personal.ID)
		canWritePersonal = Can(r, PermMemoryWrite, personal.ID)
		hubIDs = GetAccessibleHubIDs(r)
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/memories", nil)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	req = req.WithContext(context.WithValue(req.Context(), grantContextKey, GrantContext{
		UserID:             "u1",
		PrincipalType:      "api_key",
		HubScopeMode:       HubScopeAllAccessible,
		DefaultPermissions: NewPermissionSet(PermMemoryRead, PermTopicRead, PermHubRead),
		TrustLevel:         TrustElevated,
	}))
	rec := httptest.NewRecorder()

	HubContext(s)(next).ServeHTTP(rec, req)

	if !canReadPersonal {
		t.Fatalf("expected read permission on personal hub")
	}
	if canWritePersonal {
		t.Fatalf("did not expect write permission from read-only grant")
	}
	if len(hubIDs) != 2 {
		t.Fatalf("expected 2 readable hubs, got %d", len(hubIDs))
	}
}

func TestHubContextRestrictsHubScopedGrant(t *testing.T) {
	personal := &model.Hub{ID: "11111111-1111-1111-1111-111111111111", Name: "Personal"}
	team := &model.Hub{ID: "22222222-2222-2222-2222-222222222222", Name: "Team"}
	s := &hubContextTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		personalHub:   personal,
		hubs: []model.HubWithRole{
			{Hub: *personal, Role: "owner"},
			{Hub: *team, Role: "contributor"},
		},
		roles: map[string]string{
			personal.ID: "owner",
			team.ID:     "contributor",
		},
	}

	var gotReadHub, gotWriteHub string
	var hubIDs []string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReadHub = GetHubID(r)
		gotWriteHub = GetWriteHubID(r)
		hubIDs = GetAccessibleHubIDs(r)
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/memories", nil)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	req = req.WithContext(context.WithValue(req.Context(), grantContextKey, GrantContext{
		UserID:             "u1",
		PrincipalType:      "api_key",
		HubScopeMode:       HubScopeAllowlist,
		ScopedHubIDs:       []string{team.ID},
		DefaultPermissions: NewPermissionSet(PermMemoryRead, PermMemoryWrite, PermTopicRead, PermHubRead),
		TrustLevel:         TrustStandard,
	}))
	rec := httptest.NewRecorder()

	HubContext(s)(next).ServeHTTP(rec, req)

	if gotReadHub != team.ID || gotWriteHub != team.ID {
		t.Fatalf("expected scoped hub %q for read/write, got read=%q write=%q", team.ID, gotReadHub, gotWriteHub)
	}
	if len(hubIDs) != 1 || hubIDs[0] != team.ID {
		t.Fatalf("accessible hubs = %v, want [%s]", hubIDs, team.ID)
	}
}

func TestHubContextSuppressesAccountPermissionsForHubScopedGrant(t *testing.T) {
	team := &model.Hub{ID: "22222222-2222-2222-2222-222222222222", Name: "Team"}
	s := &hubContextTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		personalHub:   &model.Hub{ID: "11111111-1111-1111-1111-111111111111", Name: "Personal"},
		hubs:          []model.HubWithRole{{Hub: *team, Role: "admin"}},
		roles:         map[string]string{team.ID: "admin"},
	}

	var canReadConfigs bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		canReadConfigs = CanAny(r, PermConfigRead)
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/configs", nil)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	req = req.WithContext(context.WithValue(req.Context(), grantContextKey, GrantContext{
		UserID:             "u1",
		PrincipalType:      "api_key",
		HubScopeMode:       HubScopeAllowlist,
		ScopedHubIDs:       []string{team.ID},
		DefaultPermissions: NewPermissionSet(PermMemoryRead, PermConfigRead),
		TrustLevel:         TrustStandard,
	}))
	rec := httptest.NewRecorder()

	HubContext(s)(next).ServeHTTP(rec, req)

	if canReadConfigs {
		t.Fatalf("hub-scoped grants should not carry account-level config permissions")
	}
}

// Hub-scoped credentials must STILL get chat:read/write in the
// account scope — chat session lifecycle is owner-scoped (your
// own sessions) and the per-session scope_hub_ids gate handles
// data containment at message-send time. Without this, a
// hub-scoped API key would 403 on /v1/chat/* even though the
// chat handler is designed to accept it. Codex caught this on
// the Phase 3.2 v2 review.
func TestHubContextGrantsChatPermsToHubScopedGrant(t *testing.T) {
	team := &model.Hub{ID: "22222222-2222-2222-2222-222222222222", Name: "Team"}
	s := &hubContextTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		personalHub:   &model.Hub{ID: "11111111-1111-1111-1111-111111111111", Name: "Personal"},
		hubs:          []model.HubWithRole{{Hub: *team, Role: "admin"}},
		roles:         map[string]string{team.ID: "admin"},
	}

	var canReadChat, canWriteChat, canReadConfigs bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		canReadChat = CanAny(r, PermChatRead)
		canWriteChat = CanAny(r, PermChatWrite)
		canReadConfigs = CanAny(r, PermConfigRead)
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/sessions", nil)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	req = req.WithContext(context.WithValue(req.Context(), grantContextKey, GrantContext{
		UserID:        "u1",
		PrincipalType: "api_key",
		HubScopeMode:  HubScopeAllowlist,
		ScopedHubIDs:  []string{team.ID},
		// Grant carries chat:* AND config:read — only chat:* should
		// survive the hub-scoped intersection (config:read is
		// account-level and a hub-scoped key has no business with it).
		DefaultPermissions: NewPermissionSet(PermChatRead, PermChatWrite, PermConfigRead),
		TrustLevel:         TrustStandard,
	}))
	rec := httptest.NewRecorder()

	HubContext(s)(next).ServeHTTP(rec, req)

	if !canReadChat {
		t.Errorf("hub-scoped grant: PermChatRead missing — /v1/chat GET would 403")
	}
	if !canWriteChat {
		t.Errorf("hub-scoped grant: PermChatWrite missing — /v1/chat POST/PATCH/DELETE would 403")
	}
	if canReadConfigs {
		t.Errorf("hub-scoped grant should NOT have PermConfigRead (account-level perm)")
	}
}

func TestHubContextResolvesAccountPermissionsOutsideHubRoles(t *testing.T) {
	team := &model.Hub{ID: "22222222-2222-2222-2222-222222222222", Name: "Team"}
	s := &hubContextTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		personalHub:   &model.Hub{ID: "11111111-1111-1111-1111-111111111111", Name: "Personal"},
		hubs:          []model.HubWithRole{{Hub: *team, Role: "viewer"}},
		roles:         map[string]string{team.ID: "viewer"},
	}

	var canWriteSettings bool
	var canWriteMemory bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		canWriteSettings = CanAny(r, PermSettingsWrite)
		canWriteMemory = Can(r, PermMemoryWrite, team.ID)
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/settings", nil)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	req = req.WithContext(context.WithValue(req.Context(), grantContextKey, GrantContext{
		UserID:             "u1",
		PrincipalType:      "api_key",
		HubScopeMode:       HubScopeAllAccessible,
		DefaultPermissions: NewPermissionSet(PermSettingsWrite, PermMemoryWrite),
		TrustLevel:         TrustElevated,
	}))
	rec := httptest.NewRecorder()

	HubContext(s)(next).ServeHTTP(rec, req)

	if !canWriteSettings {
		t.Fatalf("expected account-level settings permission for all-accessible grant")
	}
	if canWriteMemory {
		t.Fatalf("viewer hub role should not receive memory write")
	}
}

func TestRequireAuthResolvesJWTGrant(t *testing.T) {
	secret := []byte("test-secret")
	token, err := jwtauth.SignGrantAccessToken("u1", "chatgpt", "11111111-1111-1111-1111-111111111111", secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	var got GrantContext
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = grantFromRequest(r)
		w.WriteHeader(http.StatusNoContent)
	})
	grantResolver := func(userID string, grantID string) APIKeyResult {
		if userID != "u1" || grantID != "11111111-1111-1111-1111-111111111111" {
			return APIKeyResult{}
		}
		return APIKeyResult{
			UserID:             userID,
			GrantID:            grantID,
			HubScopeMode:       HubScopeAllowlist,
			ScopedHubIDs:       []string{"hub1"},
			DefaultPermissions: NewPermissionSet(PermMemoryRead),
			TrustLevel:         TrustStandard,
			AgentName:          "chatgpt",
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/chatgpt", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	RequireAuth(secret, nil, grantResolver)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got.PrincipalType != "oauth_grant" || got.AgentName != "chatgpt" || got.HubScopeMode != HubScopeAllowlist {
		t.Fatalf("unexpected grant context: %#v", got)
	}
}

func TestRequireAuthRejectsRevokedJWTGrant(t *testing.T) {
	secret := []byte("test-secret")
	token, err := jwtauth.SignGrantAccessToken("u1", "chatgpt", "11111111-1111-1111-1111-111111111111", secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/mcp/chatgpt", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	RequireAuth(secret, nil, func(string, string) APIKeyResult { return APIKeyResult{} })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for a revoked grant")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// A start key (compile:read alone) is refused by RequireAuth on every
// request but a cloud session start, before any handler: /v1 (its own
// key list included, where a key could make itself a wider one), /mcp
// and every other /v2 route. On the start's route its key and scope come
// from the one-round-trip resolver, and /v2 finds the scope there.
func TestStartKeysReachOnlyTheCloudStart(t *testing.T) {
	user := uuid.New()
	startKey := func(string) APIKeyResult {
		return APIKeyResult{UserID: user.String(), GrantID: uuid.NewString(), HubScopeMode: HubScopeAllowlist,
			ScopedHubIDs: []string{uuid.NewString()}, DefaultPermissions: NewPermissionSet(PermCompileRead), TrustLevel: TrustStandard}
	}
	resolved := 0
	viaStart := func(_ context.Context, key string) (APIKeyResult, ledger.Scope) {
		resolved++
		return startKey(key), ledger.Scope{PersonID: user}
	}
	var reached *http.Request
	h := RequireAuth([]byte("secret"), startKey, nil, WithStartKeys(viaStart))(
		HubContext(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = r
			w.WriteHeader(http.StatusTeapot)
		})))
	send := func(method, path string) *httptest.ResponseRecorder {
		reached = nil
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Authorization", "Bearer mxk_"+uuid.NewString())
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	for _, c := range []struct{ method, path string }{
		{"POST", "/mcp"},
		{"POST", "/mcp/chatgpt"},
		{"GET", "/v1/auth/api-keys"},
		{"POST", "/v1/auth/api-keys"},
		{"GET", "/v1/memories"},
		{"POST", "/v1/recall"},
		{"GET", "/v2/spaces"},
		{"GET", "/v2/spaces/acme-web/session-starts"},
		{"POST", "/v2/spaces/acme-web/session-starts/x"},
		{"POST", "/v2/spaces/acme-web/compile-loads"},
		{"POST", "/v2/cloud-agents"},
	} {
		if rec := send(c.method, c.path); rec.Code != http.StatusForbidden || reached != nil {
			t.Errorf("%s %s with a start key = %d (handler reached: %v), want 403", c.method, c.path, rec.Code, reached != nil)
		}
	}
	if resolved != 0 {
		t.Errorf("the start's resolver ran %d times off its route", resolved)
	}
	rec := send("POST", "/v2/spaces/acme-web/session-starts")
	if rec.Code != http.StatusTeapot || reached == nil || resolved != 1 {
		t.Fatalf("the start = %d, reached %v, resolved %d", rec.Code, reached != nil, resolved)
	}
	if s, ok := ResolvedScope(reached); !ok || s.PersonID != user {
		t.Errorf("the scope read with the key = %+v %v", s, ok)
	}
	if authz := GetAuthContext(reached); authz == nil || len(authz.PermissionsByHub) != 0 {
		t.Errorf("a start key's hub permissions = %+v, want none (and no hub read)", authz)
	}
}

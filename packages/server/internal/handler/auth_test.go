package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func TestRequiredJWTSecret(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		t.Setenv("JWT_SECRET", "")
		_, err := RequiredJWTSecret()
		if err == nil {
			t.Fatal("expected error when JWT_SECRET is unset")
		}
	})

	t.Run("present", func(t *testing.T) {
		t.Setenv("JWT_SECRET", "test-secret")
		secret, err := RequiredJWTSecret()
		if err != nil {
			t.Fatalf("RequiredJWTSecret returned error: %v", err)
		}
		if string(secret) != "test-secret" {
			t.Fatalf("unexpected secret: %q", string(secret))
		}
	})
}

func TestOAuthCallbackURLResolution(t *testing.T) {
	t.Run("defaults to local API", func(t *testing.T) {
		t.Setenv("API_BASE_URL", "")
		t.Setenv("OAUTH_GITHUB_REDIRECT_URL", "")

		baseURL := resolveAPIBaseURL()

		if baseURL != "http://localhost:8080" {
			t.Fatalf("unexpected default API base URL: %q", baseURL)
		}
		if got := resolveGitHubRedirectURL(baseURL); got != "http://localhost:8080/v1/auth/github/callback" {
			t.Fatalf("unexpected GitHub callback URL: %q", got)
		}
		if got := providerCallbackURL(baseURL, "google"); got != "http://localhost:8080/v1/auth/google/callback" {
			t.Fatalf("unexpected Google callback URL: %q", got)
		}
	})

	t.Run("derives callbacks from API base URL", func(t *testing.T) {
		t.Setenv("API_BASE_URL", "https://staging-api.memaxlabs.com/")
		t.Setenv("OAUTH_GITHUB_REDIRECT_URL", "")

		baseURL := resolveAPIBaseURL()

		if baseURL != "https://staging-api.memaxlabs.com" {
			t.Fatalf("unexpected API base URL: %q", baseURL)
		}
		if got := resolveGitHubRedirectURL(baseURL); got != "https://staging-api.memaxlabs.com/v1/auth/github/callback" {
			t.Fatalf("unexpected GitHub callback URL: %q", got)
		}
		if got := providerCallbackURL(baseURL, "google"); got != "https://staging-api.memaxlabs.com/v1/auth/google/callback" {
			t.Fatalf("unexpected Google callback URL: %q", got)
		}
	})

	t.Run("keeps explicit GitHub redirect override", func(t *testing.T) {
		t.Setenv("API_BASE_URL", "https://api.memax.app")
		t.Setenv("OAUTH_GITHUB_REDIRECT_URL", "https://custom.example.com/github/callback")

		if got := resolveGitHubRedirectURL(resolveAPIBaseURL()); got != "https://custom.example.com/github/callback" {
			t.Fatalf("unexpected GitHub callback override: %q", got)
		}
	})
}

func TestParseOAuthAllowlist(t *testing.T) {
	got := parseOAuthAllowlist(" Ziyang@MemaxLabs.com, , jiahao@memaxlabs.com ")

	if _, ok := got["ziyang@memaxlabs.com"]; !ok {
		t.Fatal("expected lowercase trimmed email entry")
	}
	if _, ok := got["jiahao@memaxlabs.com"]; !ok {
		t.Fatal("expected second email entry")
	}
	if _, ok := got[""]; ok {
		t.Fatal("did not expect empty allowlist entry")
	}
}

func TestRedirectAllowlistFromAppBaseURL(t *testing.T) {
	got := redirectAllowlistFromAppBaseURL("https://staging-app.memaxlabs.com/settings")
	if len(got) != 1 || got[0] != "https://staging-app.memaxlabs.com" {
		t.Fatalf("unexpected redirect allowlist: %#v", got)
	}

	if got := redirectAllowlistFromAppBaseURL(""); len(got) != 0 {
		t.Fatalf("expected empty allowlist for missing APP_BASE_URL, got %#v", got)
	}

	if got := redirectAllowlistFromAppBaseURL("https://user:pass@memax.app"); len(got) != 0 {
		t.Fatalf("expected userinfo URL to be rejected, got %#v", got)
	}
}

func TestIsAllowedRedirectUsesAppBaseURL(t *testing.T) {
	h := &AuthHandler{
		redirectAllowlist: redirectAllowlistFromAppBaseURL("https://staging-app.memaxlabs.com"),
	}

	allowed := []string{
		"",
		"https://staging-app.memaxlabs.com/auth/callback",
		"https://staging-app.memaxlabs.com/settings?account_linked=github",
		"http://localhost:3000/auth/callback",
		"http://127.0.0.1:44123/callback",
	}
	for _, raw := range allowed {
		if !h.isAllowedRedirect(raw) {
			t.Fatalf("expected redirect to be allowed: %s", raw)
		}
	}

	blocked := []string{
		"https://memax.app/auth/callback",
		"https://staging-app.memaxlabs.com.evil.com/auth/callback",
		"https://user:pass@staging-app.memaxlabs.com/auth/callback",
		"http://staging-app.memaxlabs.com/auth/callback",
	}
	for _, raw := range blocked {
		if h.isAllowedRedirect(raw) {
			t.Fatalf("expected redirect to be blocked: %s", raw)
		}
	}
}

func TestGoogleEmailAllowlist(t *testing.T) {
	t.Run("allows all when unset", func(t *testing.T) {
		h := &AuthHandler{}
		if !h.isGoogleEmailAllowed("anyone@example.com") {
			t.Fatal("expected empty allowlist to allow verified Google email")
		}
	})

	t.Run("allows exact email", func(t *testing.T) {
		h := &AuthHandler{
			googleAllowedEmails: parseOAuthAllowlist("ziyang@memaxlabs.com"),
		}
		if !h.isGoogleEmailAllowed("Ziyang@MemaxLabs.com") {
			t.Fatal("expected exact email allowlist match")
		}
		if h.isGoogleEmailAllowed("jiahao@memaxlabs.com") {
			t.Fatal("did not expect non-allowlisted email")
		}
	})

	t.Run("allows domain", func(t *testing.T) {
		h := &AuthHandler{
			googleAllowedDomains: parseOAuthAllowlist("memaxlabs.com"),
		}
		if !h.isGoogleEmailAllowed("jiahao@memaxlabs.com") {
			t.Fatal("expected domain allowlist match")
		}
		if h.isGoogleEmailAllowed("jiahao@example.com") {
			t.Fatal("did not expect non-allowlisted domain")
		}
	})
}

func TestAppendQueryParamPreservesExistingQuery(t *testing.T) {
	got := appendQueryParam("https://memax.app/settings?account_linked=google", "account_link_error", "email_conflict")
	if !strings.Contains(got, "account_linked=google") {
		t.Fatalf("existing query param missing from %q", got)
	}
	if !strings.Contains(got, "account_link_error=email_conflict") {
		t.Fatalf("new query param missing from %q", got)
	}
}

// Regression: the post-login redirect used to concat "?code=..." onto
// a clientRedirect that already carried "?invite=TOKEN", producing
// "...callback?invite=TOKEN?code=CODE" — a malformed URL where the
// frontend's URLSearchParams never saw a `code` param and bailed
// with "login not complete" even though auth had succeeded.
//
// appendQueryParam uses url.Parse + Query().Set, so the resulting
// URL always has a single "?" followed by "&"-separated params. Both
// `invite` and `code` must be readable independently.
func TestAppendQueryParamAddsCodeWithoutMalformingInvite(t *testing.T) {
	raw := "https://staging-app.memaxlabs.com/auth/callback?invite=ifKLjcNJfThw4UOcVIc82jaz8w0WUb2oPJSdLjjWtpk"
	got := appendQueryParam(raw, "code", "892e18eb099d9cb815061e3d417b6bd15c5ae63e31e254a0e355d0657202c04d")

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("url.Parse(%q) failed: %v", got, err)
	}
	q := u.Query()
	if q.Get("invite") != "ifKLjcNJfThw4UOcVIc82jaz8w0WUb2oPJSdLjjWtpk" {
		t.Fatalf("invite param clobbered, got %q from %q", q.Get("invite"), got)
	}
	if q.Get("code") != "892e18eb099d9cb815061e3d417b6bd15c5ae63e31e254a0e355d0657202c04d" {
		t.Fatalf("code param missing or wrong, got %q from %q", q.Get("code"), got)
	}
	// Guard against the fmt.Sprintf("%s?code=%s", ...) regression.
	if strings.Count(got, "?") != 1 {
		t.Fatalf("expected a single `?` separator, got %q", got)
	}
}

func TestRedirectOrHandleLinkErrorRedirectsWhenClientRedirectExists(t *testing.T) {
	h := &AuthHandler{}
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/google/callback", nil)
	rec := httptest.NewRecorder()

	h.redirectOrHandleLinkError(rec, req, "https://memax.app/settings", ErrEmailBelongsToAnotherUser)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected temporary redirect, got %d", rec.Code)
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "account_link_error=email_conflict") {
		t.Fatalf("expected error code in redirect location, got %q", location)
	}
}

// TestRedirectHelpersPreserveExistingQuery parametrizes every OAuth
// redirect helper over a matrix of clientRedirect shapes (no query,
// one existing param, multiple params, encoded values, trailing
// fragment). Each helper must:
//  1. produce a well-formed URL (single `?`, only one `#`)
//  2. preserve every pre-existing query param
//  3. add its own params so they're readable via URLSearchParams
//
// The naive `fmt.Sprintf("%s?key=value", raw)` pattern we burned on
// in completeLogin fails #1 and #2 when `raw` already has a query
// string. This test makes any re-introduction of that pattern
// (in any redirect helper) surface immediately.
func TestRedirectHelpersPreserveExistingQuery(t *testing.T) {
	type check struct {
		mustContain    []string
		mustNotClobber map[string]string // query param → expected value
	}

	// Each case targets one helper; the case asserts which params
	// the helper added AND which pre-existing params survived.
	cases := []struct {
		name      string
		clientURL string
		run       func(rec *httptest.ResponseRecorder, req *http.Request)
		checks    check
	}{
		{
			name:      "linkErrorNoQuery",
			clientURL: "https://memax.app/settings",
			run: func(rec *httptest.ResponseRecorder, req *http.Request) {
				(&AuthHandler{}).redirectOrHandleLinkError(rec, req, "https://memax.app/settings", ErrEmailBelongsToAnotherUser)
			},
			checks: check{
				mustContain: []string{"account_link_error=email_conflict"},
			},
		},
		{
			name:      "linkErrorWithExistingInvite",
			clientURL: "https://memax.app/auth/callback?invite=TOKEN123",
			run: func(rec *httptest.ResponseRecorder, req *http.Request) {
				(&AuthHandler{}).redirectOrHandleLinkError(rec, req, "https://memax.app/auth/callback?invite=TOKEN123", ErrEmailBelongsToAnotherUser)
			},
			checks: check{
				mustContain:    []string{"account_link_error=email_conflict"},
				mustNotClobber: map[string]string{"invite": "TOKEN123"},
			},
		},
		{
			name:      "linkResultPreservesInvite",
			clientURL: "https://memax.app/auth/callback?invite=TOKEN999",
			run: func(rec *httptest.ResponseRecorder, req *http.Request) {
				(&AuthHandler{}).redirectLinkResult(rec, req, "https://memax.app/auth/callback?invite=TOKEN999", "github")
			},
			checks: check{
				mustContain:    []string{"account_linked=github"},
				mustNotClobber: map[string]string{"invite": "TOKEN999"},
			},
		},
		{
			name:      "loginErrorWithExistingInvite",
			clientURL: "https://memax.app/auth/callback?invite=TOKEN777",
			run: func(rec *httptest.ResponseRecorder, req *http.Request) {
				(&AuthHandler{}).redirectOrHandleLoginError(rec, req, "https://memax.app/auth/callback?invite=TOKEN777", "bad_account", "This account is not allowed")
			},
			checks: check{
				mustContain:    []string{"error=bad_account", "error_description=This"},
				mustNotClobber: map[string]string{"invite": "TOKEN777"},
			},
		},
		{
			name:      "loginErrorWithMultipleExistingParams",
			clientURL: "https://memax.app/auth/callback?invite=TOKEN&scope=personal&return_to=%2Fhome",
			run: func(rec *httptest.ResponseRecorder, req *http.Request) {
				(&AuthHandler{}).redirectOrHandleLoginError(rec, req, "https://memax.app/auth/callback?invite=TOKEN&scope=personal&return_to=%2Fhome", "not_allowed", "Denied")
			},
			checks: check{
				mustContain: []string{"error=not_allowed"},
				mustNotClobber: map[string]string{
					"invite":    "TOKEN",
					"scope":     "personal",
					"return_to": "/home",
				},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/v1/auth/provider/callback", nil)
			rec := httptest.NewRecorder()
			tc.run(rec, req)

			if rec.Code != http.StatusTemporaryRedirect {
				t.Fatalf("expected 307 redirect, got %d (body=%q)", rec.Code, rec.Body.String())
			}
			loc := rec.Header().Get("Location")
			if loc == "" {
				t.Fatal("no Location header set")
			}
			// Shape: at most one `?` separator. Two `?` is the
			// specific regression we burned on (completeLogin's
			// fmt.Sprintf bug) — it masquerades as a valid URL
			// to string-matching checks while wrecking
			// URLSearchParams parsing on the client.
			if strings.Count(loc, "?") > 1 {
				t.Fatalf("%s: more than one `?` in Location %q", tc.name, loc)
			}
			u, err := url.Parse(loc)
			if err != nil {
				t.Fatalf("%s: url.Parse failed on redirect %q: %v", tc.name, loc, err)
			}
			q := u.Query()
			for _, frag := range tc.checks.mustContain {
				if !strings.Contains(loc, frag) {
					t.Errorf("%s: missing expected fragment %q in %q", tc.name, frag, loc)
				}
			}
			for key, want := range tc.checks.mustNotClobber {
				if got := q.Get(key); got != want {
					t.Errorf("%s: param %q clobbered; got %q want %q (full URL: %q)",
						tc.name, key, got, want, loc)
				}
			}
		})
	}
}

func TestEnsurePersonalHubUsesDefaultNameAndIsIdempotent(t *testing.T) {
	s := store.NewInMemoryStore()
	h := &AuthHandler{store: s}
	user := &model.User{
		ID:   "user-1",
		Name: "Jiahao Ye",
	}

	h.ensurePersonalHub(user)
	h.ensurePersonalHub(user)

	hub, err := s.GetPersonalHub(user.ID)
	if err != nil {
		t.Fatalf("GetPersonalHub returned error: %v", err)
	}
	if hub.Name != model.DefaultPersonalHubName {
		t.Fatalf("expected personal hub name %q, got %q", model.DefaultPersonalHubName, hub.Name)
	}
	if hub.Accent != model.DefaultHubAccent("personal") {
		t.Fatalf("expected personal hub accent %q, got %q", model.DefaultHubAccent("personal"), hub.Accent)
	}

	hubs, err := s.ListUserHubs(user.ID)
	if err != nil {
		t.Fatalf("ListUserHubs returned error: %v", err)
	}
	if len(hubs) != 1 {
		t.Fatalf("expected exactly one personal hub, got %d", len(hubs))
	}
}

// TestUpdateAPIKeyAuthGate guards the auth/permission preamble of
// UpdateAPIKey without touching Postgres. Prior to this test the
// handler read `GetAuthContext(r)` — which is populated by
// AuthorizeHTTP, a middleware that is NOT in the `/v1/auth/api-keys/*`
// mount chain — so every request hit the nil-AuthContext branch and
// returned 401. A 401 on PATCH cascaded through the web client's
// refresh-and-logout path, which is what a user reported in prod as
// "it shows Assigning then logs me out." The handler now reads
// `GetUserID(r)` + `grantFromRequest(r)` instead; these are populated
// by RequireAuth, which IS in this mount chain.
func TestUpdateAPIKeyAuthGate(t *testing.T) {
	// h.pool stays nil — we only exercise the preamble. Every
	// branch below MUST return before any pool call.
	h := &AuthHandler{}

	newRequest := func(
		grant *GrantContext,
		userID string,
		body string,
	) *http.Request {
		req := httptest.NewRequest(
			http.MethodPatch,
			"/v1/auth/api-keys/00000000-0000-0000-0000-000000000001",
			strings.NewReader(body),
		)
		req.SetPathValue("id", "00000000-0000-0000-0000-000000000001")
		ctx := req.Context()
		if userID != "" {
			ctx = context.WithValue(ctx, userIDKey, userID)
		}
		if grant != nil {
			ctx = context.WithValue(ctx, grantContextKey, *grant)
		}
		return req.WithContext(ctx)
	}

	t.Run("no user context returns 401", func(t *testing.T) {
		req := newRequest(nil, "", `{"standalone":true}`)
		rec := httptest.NewRecorder()
		h.UpdateAPIKey(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("want 401, got %d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("api_key principal returns 403", func(t *testing.T) {
		grant := GrantContext{
			UserID:             "u1",
			PrincipalType:      "api_key",
			DefaultPermissions: NewPermissionSet(PermSettingsWrite),
		}
		req := newRequest(&grant, "u1", `{"standalone":true}`)
		rec := httptest.NewRecorder()
		h.UpdateAPIKey(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("want 403 forbidden for api_key, got %d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("oauth_grant principal returns 403", func(t *testing.T) {
		grant := GrantContext{
			UserID:             "u1",
			PrincipalType:      "oauth_grant",
			DefaultPermissions: NewPermissionSet(PermSettingsWrite),
		}
		req := newRequest(&grant, "u1", `{"standalone":true}`)
		rec := httptest.NewRecorder()
		h.UpdateAPIKey(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("want 403 forbidden for oauth_grant, got %d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("user principal without settings:write returns 403", func(t *testing.T) {
		grant := GrantContext{
			UserID:             "u1",
			PrincipalType:      "user",
			DefaultPermissions: NewPermissionSet(PermMemoryRead), // no settings:write
		}
		req := newRequest(&grant, "u1", `{"standalone":true}`)
		rec := httptest.NewRecorder()
		h.UpdateAPIKey(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("want 403 for missing settings:write, got %d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("user principal with settings:write passes gate", func(t *testing.T) {
		// Valid auth + perms. No fields to update → 400 invariant
		// check fires BEFORE any pool access, which is what we want
		// to assert (the request reached past the auth gate). Any
		// non-401/403 status here proves the gate accepted the
		// request; 400 happens to be what the next check emits.
		grant := GrantContext{
			UserID:             "u1",
			PrincipalType:      "user",
			DefaultPermissions: NewPermissionSet(PermSettingsWrite),
		}
		req := newRequest(&grant, "u1", `{}`)
		rec := httptest.NewRecorder()
		h.UpdateAPIKey(rec, req)
		if rec.Code == http.StatusUnauthorized {
			t.Fatalf(
				"regression: valid user request returned 401 — the "+
					"auth gate should not depend on AuthContext. body=%q",
				rec.Body.String(),
			)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("want 400 (no fields to update), got %d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("local principal (dev memory-mode) passes gate", func(t *testing.T) {
		// Auth-disabled dev mode sets PrincipalType=local. Treating
		// it like "user" for this endpoint keeps local dev working.
		grant := GrantContext{
			UserID:             "local",
			PrincipalType:      "local",
			DefaultPermissions: DefaultUserPermissions(),
		}
		req := newRequest(&grant, "local", `{}`)
		rec := httptest.NewRecorder()
		h.UpdateAPIKey(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("want 400 (no fields), got %d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects contradictory agent_name + standalone=true in same request", func(t *testing.T) {
		grant := GrantContext{
			UserID:             "u1",
			PrincipalType:      "user",
			DefaultPermissions: NewPermissionSet(PermSettingsWrite),
		}
		req := newRequest(
			&grant,
			"u1",
			`{"agent_name":"hatch","standalone":true}`,
		)
		rec := httptest.NewRecorder()
		h.UpdateAPIKey(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf(
				"want 400 for contradictory request, got %d body=%q",
				rec.Code,
				rec.Body.String(),
			)
		}
	})
}

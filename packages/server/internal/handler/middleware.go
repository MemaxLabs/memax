package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type contextKey string

const userIDKey contextKey = "user_id"
const tokenHubIDKey contextKey = "token_hub_id"        // set when API key is hub-scoped
const agentNameKey contextKey = "agent_name"           // set when API key has agent identity
const impersonatorIDKey contextKey = "impersonator_id" // set when using an impersonation token

// APIKeyResult holds the resolved identity from an API key.
type APIKeyResult struct {
	UserID             string
	GrantID            string
	HubID              string // empty = user-level token (all hubs), set = hub-scoped token
	HubScopeMode       string
	ScopedHubIDs       []string
	DefaultPermissions PermissionSet
	TrustLevel         string
	RateLimitTier      string
	AgentName          string // agent identity from key creation (e.g., "claude-code", "cursor")
	// KeyStandalone — the key was marked "personal": writes are the
	// owner's own. A key with neither an agent nor this flag has no
	// author identity and cannot write memories.
	KeyStandalone bool
	// OAuthScope is an OAuth grant's granted scope ("memax:read
	// memax:propose"), empty for API keys and older grants.
	OAuthScope string

	// lastUsed is when the credential was last used, as its lookup read
	// it: the lookup writes it again only once it is lastUsedEvery old.
	lastUsed *time.Time
}

// lastUsedEvery is how fresh a credential's last_used is kept: an agent
// making many calls a minute writes it once a minute, not once a call.
const lastUsedEvery = time.Minute

// usedRecently reports whether last_used is fresh enough to leave alone.
func usedRecently(at *time.Time) bool {
	return at != nil && time.Since(*at) < lastUsedEvery
}

// APIKeyResolver resolves an API key string to a user ID and optional hub scope.
type APIKeyResolver func(key string) APIKeyResult

// GrantResolver resolves a server-side delegated auth grant.
type GrantResolver func(userID string, grantID string) APIKeyResult

// StartKeyResolver resolves an API key on the cloud start's route with its
// person's V2 scope, in one round trip (AuthHandler.ResolveStartKey).
type StartKeyResolver func(ctx context.Context, key string) (APIKeyResult, ledger.Scope)

// AuthOption configures RequireAuth.
type AuthOption func(*authOptions)

type authOptions struct {
	startKeys StartKeyResolver
	prefetch  CredentialPrefetcher
}

// Prefetched is what RequireAuth read with a credential, in the
// credential's own round trip, for HubContext to use instead of reading it
// again: the person's hub list (store.QueueRequestHubs) and, for a request
// that builds a V2 principal, their V2 scope.
type Prefetched struct {
	UserID string
	Hubs   []model.HubWithRole
	// Listed says the hub list was read; HubContext reads it when not.
	Listed bool
	// Scope is the person's V2 scope, when it was asked for and read.
	Scope *ledger.Scope
}

// CredentialPrefetcher resolves an API key or an OAuth grant together with
// what HubContext reads next, in one round trip to Postgres where it would
// be two one after the other (AuthHandler). scope asks for the person's V2
// scope as well (a /v2 or /mcp request).
type CredentialPrefetcher interface {
	PrefetchAPIKey(ctx context.Context, key string, scope bool) (APIKeyResult, *Prefetched)
	PrefetchGrant(ctx context.Context, userID, grantID string, scope bool) (APIKeyResult, *Prefetched)
}

// WithCredentialPrefetch resolves API keys (but start keys on the cloud
// start's route, WithStartKeys) and OAuth grants through p, so a request
// made with one reaches its handler after one round trip, as a person's
// session does.
func WithCredentialPrefetch(p CredentialPrefetcher) AuthOption {
	return func(o *authOptions) { o.prefetch = p }
}

const prefetchedKey contextKey = "prefetched"

// WithStartKeys resolves API keys on the cloud start's route (POST
// /v2/spaces/{space}/session-starts) with r, which reads the key and its
// person's scope together; /v2 then reuses the scope (ResolvedScope).
func WithStartKeys(r StartKeyResolver) AuthOption {
	return func(o *authOptions) { o.startKeys = r }
}

// IsSessionStartRoute reports whether r is a cloud session start, the one
// request a start key may make.
func IsSessionStartRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/v2/spaces/")
	space, tail, _ := strings.Cut(rest, "/")
	return ok && space != "" && tail == "session-starts"
}

const resolvedScopeKey contextKey = "resolved_scope"

// ResolvedScope is the V2 scope RequireAuth read with the credential, when
// it did (a key on the cloud start's route): /v2 uses it instead of
// reading the person's spaces again.
func ResolvedScope(r *http.Request) (ledger.Scope, bool) {
	s, ok := r.Context().Value(resolvedScopeKey).(ledger.Scope)
	return s, ok
}

// RequireAuth returns middleware that rejects requests without valid auth.
// Supports both JWT tokens and API keys (mxk_ prefix).
// Pass jwtSecret=nil to disable auth (local dev / memory mode).
//
// A start key (IsStartKey: compile:read, no memory:read) is refused on
// every request but a cloud session start, here, before any handler: it
// reaches nothing else of /v1, /v2 or /mcp, its own key list included.
func RequireAuth(jwtSecret []byte, keyResolver APIKeyResolver, grantResolver GrantResolver, opts ...AuthOption) func(http.Handler) http.Handler {
	var o authOptions
	for _, opt := range opts {
		opt(&o)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// No secret configured → auth disabled (memory mode)
			if jwtSecret == nil {
				authz := &AuthContext{
					UserID:            "local",
					PrincipalType:     "local",
					TrustLevel:        TrustAdmin,
					HubScopeMode:      HubScopeAllAccessible,
					PermissionsByHub:  map[string]PermissionSet{accountPermissionScope: DefaultUserPermissions()},
					DefaultWriteHubID: "local",
				}
				ctx := context.WithValue(r.Context(), userIDKey, "local")
				ctx = context.WithValue(ctx, authContextKey, authz)
				ctx = context.WithValue(ctx, grantContextKey, defaultGrantContext("local"))
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			header := r.Header.Get("Authorization")
			token := strings.TrimPrefix(header, "Bearer ")

			var userID string
			var tokenHubID string     // non-empty if API key is hub-scoped
			var agentName string      // agent identity from API key (e.g., "claude-code")
			var impersonatorID string // set when JWT carries impersonator_id claim
			var grant GrantContext
			var audienceMismatch bool // an audience-bound token sent to another resource
			var scope *ledger.Scope   // the person's V2 scope, when read with the key
			var pf *Prefetched        // the hub list (and scope) read with the credential
			if strings.HasPrefix(token, "mxk_") && keyResolver != nil {
				// API key auth — resolves user, hub scope, and agent identity
				var result APIKeyResult
				if o.startKeys != nil && IsSessionStartRoute(r) {
					var s ledger.Scope
					result, s = o.startKeys(r.Context(), token)
					if result.UserID != "" {
						scope = &s
					}
				} else if o.prefetch != nil {
					result, pf = o.prefetch.PrefetchAPIKey(r.Context(), token, readsV2Scope(r))
				} else {
					result = keyResolver(token)
				}
				if result.UserID != "" && IsStartKey(result.DefaultPermissions) && !IsSessionStartRoute(r) {
					writeJSON(w, http.StatusForbidden, model.ApiResponse{
						Error: &model.Error{Code: "permission_denied", Message: "This is a start key: it only starts cloud agents' sessions " +
							"(POST /v2/spaces/{space}/session-starts). Use a key with read access, or your own sign-in, for anything else."},
					})
					return
				}
				userID = result.UserID
				tokenHubID = result.HubID
				agentName = result.AgentName
				if userID != "" {
					grant = GrantContext{
						UserID:             userID,
						GrantID:            result.GrantID,
						PrincipalType:      "api_key",
						AgentName:          result.AgentName,
						KeyStandalone:      result.KeyStandalone,
						HubScopeMode:       result.HubScopeMode,
						ScopedHubIDs:       result.ScopedHubIDs,
						DefaultPermissions: result.DefaultPermissions,
						TrustLevel:         result.TrustLevel,
						RateLimitTier:      result.RateLimitTier,
					}
				}
			} else {
				// JWT auth — also extract agent_name if present (MCP OAuth tokens)
				claims := ClaimsFromRequest(r, jwtSecret)
				// RFC 8707: a token bound to resources works only there. An MCP
				// OAuth token names its MCP endpoint; anywhere else (another
				// endpoint, the REST API) it is not a credential.
				if claims != nil && len(claims.Aud) > 0 && !claims.Aud.Contains(mcpPublicBaseURL(r)+r.URL.Path) {
					claims = nil
					audienceMismatch = true
				}
				if claims != nil {
					impersonatorID = claims.ImpersonatorID
					if claims.GrantID != "" && grantResolver != nil {
						var result APIKeyResult
						if o.prefetch != nil {
							result, pf = o.prefetch.PrefetchGrant(r.Context(), claims.Sub, claims.GrantID, readsV2Scope(r))
						} else {
							result = grantResolver(claims.Sub, claims.GrantID)
						}
						userID = result.UserID
						agentName = result.AgentName
						if userID != "" {
							grant = GrantContext{
								UserID:             userID,
								GrantID:            result.GrantID,
								PrincipalType:      "oauth_grant",
								AgentName:          result.AgentName,
								HubScopeMode:       result.HubScopeMode,
								ScopedHubIDs:       result.ScopedHubIDs,
								DefaultPermissions: result.DefaultPermissions,
								TrustLevel:         result.TrustLevel,
								RateLimitTier:      result.RateLimitTier,
								OAuthScope:         result.OAuthScope,
								SessionID:          claims.Sid,
							}
						}
					} else {
						userID = claims.Sub
						agentName = claims.AgentName
						grant = defaultGrantContext(userID)
						if agentName != "" {
							grant.AgentName = agentName
						}
						if agentName == "" && claims.ImpersonatorID == "" {
							grant.Surface = claims.Surface
						}
						if claims.ImpersonatorID == "" {
							grant.SessionID = claims.Sid
						}
					}
				}
			}

			if userID == "" {
				// "Run: memax login" is only correct advice for the CLI —
				// MCP clients authenticate via OAuth or an mxk_ API key,
				// and their users may already be logged in to the CLI.
				message := "Authentication required. Run: memax login"
				// For MCP endpoints, include WWW-Authenticate header for OAuth discovery
				if strings.HasPrefix(r.URL.Path, "/mcp") {
					message = "Authentication required. Authorize this MCP client via OAuth (for Codex: codex mcp login memax), or configure an API key with: memax setup --mcp --api-key"
					challenge := fmt.Sprintf(`Bearer resource_metadata="%s"`, MCPResourceMetadataURL(r))
					if audienceMismatch {
						message = "This token was issued for another Memax endpoint. Authorize this MCP client again for this one."
						challenge += `, error="invalid_token", error_description="the token's audience is another resource"`
					}
					w.Header().Set("WWW-Authenticate", challenge)
				} else if audienceMismatch {
					message = "This token is for a Memax MCP endpoint and can't be used here."
				}
				writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
					Error: &model.Error{Code: "unauthorized", Message: message},
				})
				return
			}

			if grant.UserID == "" {
				grant = defaultGrantContext(userID)
			}
			if grant.HubScopeMode == "" {
				grant.HubScopeMode = HubScopeAllAccessible
			}
			if grant.DefaultPermissions == nil {
				grant.DefaultPermissions = DefaultUserPermissions()
			}
			if grant.TrustLevel == "" {
				grant.TrustLevel = TrustElevated
			}

			ctx := context.WithValue(r.Context(), userIDKey, userID)
			ctx = context.WithValue(ctx, grantContextKey, grant)
			if pf != nil && pf.UserID == userID {
				ctx = context.WithValue(ctx, prefetchedKey, pf)
				if scope == nil && pf.Scope != nil {
					scope = pf.Scope
				}
			}
			if scope != nil {
				ctx = context.WithValue(ctx, resolvedScopeKey, *scope)
			}
			if tokenHubID != "" {
				ctx = context.WithValue(ctx, tokenHubIDKey, tokenHubID)
			}
			if agentName != "" {
				ctx = context.WithValue(ctx, agentNameKey, agentName)
			}
			// Propagate impersonator_id from JWT claims into request context
			// so handlers and analytics can attribute the real actor.
			if impersonatorID != "" {
				ctx = context.WithValue(ctx, impersonatorIDKey, impersonatorID)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// MCPResourceMetadataURL is the protected-resource metadata URL (RFC 9728)
// for the MCP endpoint a request was sent to, as WWW-Authenticate
// challenges name it.
func MCPResourceMetadataURL(r *http.Request) string {
	resourcePath := "/.well-known/oauth-protected-resource"
	if r.URL.Path != "" && r.URL.Path != "/" {
		resourcePath += r.URL.Path
	}
	return mcpPublicBaseURL(r) + resourcePath
}

// mcpPublicBaseURL is the API's public origin: API_BASE_URL, or the
// request's own host. A request to MCP_BASE_URL's host (the mcp.memax.app
// alias of the same app) is answered as that host, so the client sees the
// resource it connected to.
func mcpPublicBaseURL(r *http.Request) string {
	if alias := strings.TrimRight(os.Getenv("MCP_BASE_URL"), "/"); alias != "" {
		if u, err := url.Parse(alias); err == nil && strings.EqualFold(u.Host, r.Host) {
			return alias
		}
	}
	if baseURL := strings.TrimRight(os.Getenv("API_BASE_URL"), "/"); baseURL != "" {
		return baseURL
	}
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

// GetUserID extracts the authenticated user ID from request context.
func GetUserID(r *http.Request) string {
	if v, ok := r.Context().Value(userIDKey).(string); ok {
		return v
	}
	return ""
}

// InjectUserIDForTest returns a copy of r with userID installed on
// its context as if the auth middleware had populated it. Exported
// so sibling packages (ratelimit, meter, etc.) can drive the
// middleware chain in tests without re-running the JWT parse path.
// Not for use in production code — the userIDKey is intentionally
// unexported so the normal write path is the auth middleware.
//
// Also installs a default unscoped AuthContext (HubScopeAllAccessible
// + UserID) so isHubScopeBounded reads "default web/CLI session"
// rather than fail-closed-strict. This mirrors what the real auth
// middleware does in production: every authenticated request that
// reaches a handler has BOTH userID AND AuthContext populated.
// Tests that need a scope-bounded principal must call
// InjectAuthContextForTest with a HubScopeAllowlist context, which
// overwrites the default installed here.
func InjectUserIDForTest(r *http.Request, userID string) *http.Request {
	r = r.WithContext(context.WithValue(r.Context(), userIDKey, userID))
	if GetAuthContext(r) == nil {
		r = InjectAuthContextForTest(r, &AuthContext{
			UserID:       userID,
			HubScopeMode: HubScopeAllAccessible,
		})
	}
	return r
}

// InjectGrantForTest is the test-only sibling of InjectUserIDForTest for
// the credential RequireAuth resolved (GetGrant).
func InjectGrantForTest(r *http.Request, g GrantContext) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), grantContextKey, g))
}

// InjectHubIDForTest is the test-only sibling of InjectUserIDForTest
// for the active hub context slot. Handlers that call GetHubID(r)
// read from this value. Used to drive handler tests without running
// the full HubContext middleware.
func InjectHubIDForTest(r *http.Request, hubID string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), hubIDKey, hubID))
}

// InjectAccessibleHubIDsForTest is the test-only sibling of
// InjectUserIDForTest for the accessible hubs slot. Handlers that
// call GetAccessibleHubIDs(r) read from this value.
func InjectAccessibleHubIDsForTest(r *http.Request, hubIDs []string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), hubIDsKey, hubIDs))
}

// InjectWriteHubIDForTest is the test-only sibling for the write
// hub slot. Create/Update handlers call GetWriteHubID to resolve
// the write target; this helper sets it without the HubContext
// middleware.
func InjectWriteHubIDForTest(r *http.Request, hubID string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), writeHubIDKey, hubID))
}

// InjectAuthContextForTest installs a full AuthContext on r without
// running the auth middleware chain. Used by integration tests that
// need to exercise scope-bounded principals (HubScopeMode =
// HubScopeAllowlist) end-to-end through handlers — the
// strict-vs-cross-hub recall decision reads from AuthContext, so
// tests proving the leak is closed need to install one of these.
func InjectAuthContextForTest(r *http.Request, auth *AuthContext) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authContextKey, auth))
}

// GetAgentName returns the agent identity from the API key (e.g., "claude-code").
// Empty for JWT-authenticated requests (web app, CLI with user token).
func GetAgentName(r *http.Request) string {
	if v, ok := r.Context().Value(agentNameKey).(string); ok {
		return v
	}
	return ""
}

// GetImpersonatorID returns the dev user ID who initiated impersonation.
// Empty for normal (non-impersonated) requests.
func GetImpersonatorID(r *http.Request) string {
	if v, ok := r.Context().Value(impersonatorIDKey).(string); ok {
		return v
	}
	return ""
}

// Hub context keys
const userHubsKey contextKey = "user_hubs"
const hubIDKey contextKey = "hub_id"
const hubIDsKey contextKey = "hub_ids"
const retrievalBoostHubIDKey contextKey = "retrieval_boost_hub_id"
const writeHubIDKey contextKey = "write_hub_id"
const timezoneKey contextKey = "timezone"

// GetHubID returns the active hub ID from the request context.
func GetHubID(r *http.Request) string {
	if v, ok := r.Context().Value(hubIDKey).(string); ok {
		return v
	}
	return ""
}

// GetAccessibleHubIDs returns all hub IDs the user has access to.
// hubsWithBatch is a store that sends the hub list with more statements in
// its round trip (store.PostgresStore.ListUserHubsWith).
type hubsWithBatch interface {
	ListUserHubsWith(ctx context.Context, userID string, extra func(*pgx.Batch)) ([]model.HubWithRole, error)
}

// readsV2Scope reports whether a request builds a V2 principal, which
// needs the person's V2 scope: every /v2 route and the MCP endpoints.
func readsV2Scope(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/v2/") || strings.HasPrefix(r.URL.Path, "/mcp")
}

// userHubs is the person's hub list HubContext read (ListUserHubs).
type userHubs struct {
	userID string
	hubs   []model.HubWithRole
}

// RequestHubs returns every hub the person is a member of, with their
// role and whether it is on the V2 record, as HubContext read them for
// this request; ok is false when it didn't (no HubContext, or the list
// failed). It is the person's list, not the credential's: filter it by
// GetAccessibleHubIDs.
func RequestHubs(r *http.Request, userID string) ([]model.HubWithRole, bool) {
	v, ok := r.Context().Value(userHubsKey).(userHubs)
	if !ok || v.userID != userID {
		return nil, false
	}
	return slices.Clone(v.hubs), true
}

func GetAccessibleHubIDs(r *http.Request) []string {
	if v, ok := r.Context().Value(hubIDsKey).([]string); ok {
		return v
	}
	return nil
}

func GetRetrievalBoostHubID(r *http.Request) string {
	if v, ok := r.Context().Value(retrievalBoostHubIDKey).(string); ok {
		return v
	}
	return ""
}

// GetTimezone returns the client's timezone from the X-Timezone header.
// Falls back to "UTC" if not provided.
func GetTimezone(r *http.Request) string {
	if v, ok := r.Context().Value(timezoneKey).(string); ok && v != "" {
		return v
	}
	return "UTC"
}

// GetWriteHubID returns the hub that write operations should target.
// This only follows explicit hub selection (hub-scoped key or X-Hub-ID).
// Otherwise it falls back to the user's personal hub.
func GetWriteHubID(r *http.Request) string {
	if v, ok := r.Context().Value(writeHubIDKey).(string); ok {
		return v
	}
	return ""
}

// HubContext resolves:
//   - the active read hub (header/query/personal)
//   - all accessible hubs for cross-hub reads
//   - the write hub (explicit header or personal fallback)
//
// Runs after RequireAuth.
func HubContext(s store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := GetUserID(r)
			if userID == "" || userID == "local" {
				next.ServeHTTP(w, r)
				return
			}

			grant := grantFromRequest(r)

			// A start key reaches only the cloud start (RequireAuth), which
			// reads its spaces on /v2's own terms: no hub here grants it
			// anything, so nothing is read for it.
			if grant.PrincipalType == "api_key" && IsStartKey(grant.DefaultPermissions) {
				ctx := context.WithValue(r.Context(), authContextKey, &AuthContext{
					UserID: userID, PrincipalType: grant.PrincipalType, GrantID: grant.GrantID,
					AgentName: grant.AgentName, SourceAgent: grant.AgentName, TrustLevel: grant.TrustLevel,
					HubScopeMode: grant.HubScopeMode, ScopedHubIDs: grant.ScopedHubIDs,
					PermissionsByHub: map[string]PermissionSet{},
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			var hubID string
			var retrievalBoostHubID string
			var writeHubID string
			permsByHub := map[string]PermissionSet{}

			// A request that builds a V2 principal (/v2, /mcp) reads the
			// person's V2 scope in the hub list's round trip, and leaves it on
			// the request for principalFor (ResolvedScope): one round trip
			// to Postgres here, not two one after the other.
			var allHubs []model.HubWithRole
			var listErr error
			var scope ledger.Scope
			var scopeErr error
			person, personErr := uuid.Parse(userID)
			if pf, ok := r.Context().Value(prefetchedKey).(*Prefetched); ok && pf.UserID == userID && pf.Listed {
				// Read with the credential, in its round trip (RequireAuth),
				// and the scope with it when the request needs one.
				allHubs = pf.Hubs
			} else if ls, ok := s.(hubsWithBatch); ok {
				// The request's list has no memory counts (memax_hubs reads
				// its own), and the scope rides with it for /v2 and /mcp.
				var extra func(*pgx.Batch)
				if personErr == nil && readsV2Scope(r) {
					extra = func(b *pgx.Batch) { ledger.QueueUserScope(b, person, &scope, &scopeErr) }
				}
				allHubs, listErr = ls.ListUserHubsWith(r.Context(), userID, extra)
			} else {
				allHubs, listErr = s.ListUserHubs(userID)
			}
			scopeRead := personErr == nil && scopeErr == nil && scope.PersonID == person
			// The personal hub and the write hub's role come from allHubs,
			// which ListUserHubs read from the same hubs and hub_members
			// rows, so a request makes one round trip to Postgres here,
			// not up to four (the API is about 24 ms from the database). A
			// personal hub missing from the list (its owner has no
			// membership row), or a failed list, is looked up as before.
			var personal struct {
				id         string
				ok, looked bool
			}
			personalHub := func() (string, bool) {
				if personal.looked {
					return personal.id, personal.ok
				}
				personal.looked = true
				for _, item := range allHubs {
					if item.Hub.HubType == "personal" && item.Hub.OwnerID == userID {
						personal.id, personal.ok = item.Hub.ID, true
						return personal.id, true
					}
				}
				if hub, err := s.GetPersonalHub(userID); err == nil {
					personal.id, personal.ok = hub.ID, true
				}
				return personal.id, personal.ok
			}
			memberRole := func(hubID string) string {
				if listErr != nil {
					role, _ := s.GetHubMemberRole(hubID, userID)
					return role
				}
				for _, item := range allHubs {
					if item.Hub.ID == hubID {
						return item.Role
					}
				}
				return ""
			}
			allowedByScope := map[string]bool{}
			if grant.HubScopeMode == HubScopeAllowlist {
				for _, scopedHubID := range grant.ScopedHubIDs {
					allowedByScope[scopedHubID] = true
				}
			}
			grantPermissions := grant.DefaultPermissions
			if grant.HubScopeMode == HubScopeAllowlist {
				grantPermissions = grantPermissions.Intersect(HubScopedAllowedPermissions())
			}
			if grant.HubScopeMode == HubScopeAllAccessible {
				accountPerms := grant.DefaultPermissions.Intersect(AccountPermissions())
				if len(accountPerms) > 0 {
					permsByHub[accountPermissionScope] = accountPerms
				}
			} else {
				// HubScopeAllowlist: hub-scoped API keys do NOT
				// get the wider AccountPermissions bundle (no
				// settings, no configs, no account-delete — those
				// are owner-level and a per-hub key has no business
				// touching them). But chat session lifecycle IS
				// owner-scoped — your own sessions, not someone
				// else's — and the per-session scope_hub_ids gate
				// in the chat handler intersects against the key's
				// allowlist at create-time, so data containment is
				// preserved at message-send time. Without this
				// branch a hub-scoped key would 403 on /v1/chat/*
				// despite the chat handler being designed to accept
				// it. Codex caught this gap on Phase 3.2 v2.
				chatPerms := grant.DefaultPermissions.Intersect(NewPermissionSet(PermChatRead, PermChatWrite))
				if len(chatPerms) > 0 {
					permsByHub[accountPermissionScope] = chatPerms
				}
			}
			for _, item := range allHubs {
				if grant.HubScopeMode == HubScopeAllowlist && !allowedByScope[item.Hub.ID] {
					continue
				}
				rolePerms := rolePermissionSet(item.Role, &item.Hub)
				effective := grantPermissions.Intersect(rolePerms)
				if len(effective) > 0 {
					permsByHub[item.Hub.ID] = effective
				}
			}

			readHubIDs := hubIDsWithPermission(permsByHub, PermMemoryRead)

			if grant.HubScopeMode == HubScopeAllowlist && len(grant.ScopedHubIDs) > 0 {
				hubID = grant.ScopedHubIDs[0]
				retrievalBoostHubID = hubID
				writeHubID = hubID
			} else {
				// Read context: explicit header/query, then personal hub fallback.
				hubID = r.Header.Get("X-Hub-ID")
				if hubID == "" {
					hubID = r.URL.Query().Get("hub_id")
				}
				retrievalBoostHubID = hubID
				if hubID == "" {
					if id, ok := personalHub(); ok {
						hubID = id
					}
				}

				// Validate membership and grant read access.
				if hubID != "" {
					if !permsByHub[hubID].Has(PermMemoryRead) && !permsByHub[hubID].Has(PermHubRead) {
						if id, ok := personalHub(); ok {
							hubID = id
						}
						retrievalBoostHubID = ""
					}
				}

				// Write context: explicit X-Hub-ID only. Any switched read hub is client-local state.
				writeHubID = r.Header.Get("X-Hub-ID")
				if writeHubID == "" {
					if id, ok := personalHub(); ok {
						writeHubID = id
					}
				}
				if writeHubID != "" {
					if memberRole(writeHubID) == "" {
						if id, ok := personalHub(); ok {
							writeHubID = id
						}
					}
				}
			}

			authz := &AuthContext{
				UserID:              userID,
				PrincipalType:       grant.PrincipalType,
				GrantID:             grant.GrantID,
				AgentName:           grant.AgentName,
				SourceAgent:         grant.AgentName,
				TrustLevel:          grant.TrustLevel,
				HubScopeMode:        grant.HubScopeMode,
				ScopedHubIDs:        grant.ScopedHubIDs,
				RetrievalBoostHubID: retrievalBoostHubID,
				PermissionsByHub:    permsByHub,
				DefaultWriteHubID:   writeHubID,
			}

			ctx := context.WithValue(r.Context(), hubIDKey, hubID)
			if _, already := ResolvedScope(r); scopeRead && !already {
				ctx = context.WithValue(ctx, resolvedScopeKey, scope)
			}
			if listErr == nil {
				ctx = context.WithValue(ctx, userHubsKey, userHubs{userID: userID, hubs: allHubs})
			}
			ctx = context.WithValue(ctx, hubIDsKey, readHubIDs)
			ctx = context.WithValue(ctx, retrievalBoostHubIDKey, retrievalBoostHubID)
			ctx = context.WithValue(ctx, writeHubIDKey, writeHubID)
			ctx = context.WithValue(ctx, authContextKey, authz)
			// Extract client timezone from header (e.g., "America/Los_Angeles")
			if tz := strings.TrimSpace(r.Header.Get("X-Timezone")); tz != "" {
				ctx = context.WithValue(ctx, timezoneKey, tz)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

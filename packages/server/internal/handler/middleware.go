package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

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
}

// APIKeyResolver resolves an API key string to a user ID and optional hub scope.
type APIKeyResolver func(key string) APIKeyResult

// GrantResolver resolves a server-side delegated auth grant.
type GrantResolver func(userID string, grantID string) APIKeyResult

// RequireAuth returns middleware that rejects requests without valid auth.
// Supports both JWT tokens and API keys (mxk_ prefix).
// Pass jwtSecret=nil to disable auth (local dev / memory mode).
func RequireAuth(jwtSecret []byte, keyResolver APIKeyResolver, grantResolver GrantResolver) func(http.Handler) http.Handler {
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
			if strings.HasPrefix(token, "mxk_") && keyResolver != nil {
				// API key auth — resolves user, hub scope, and agent identity
				result := keyResolver(token)
				userID = result.UserID
				tokenHubID = result.HubID
				agentName = result.AgentName
				if userID != "" {
					grant = GrantContext{
						UserID:             userID,
						GrantID:            result.GrantID,
						PrincipalType:      "api_key",
						AgentName:          result.AgentName,
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
				if claims != nil {
					impersonatorID = claims.ImpersonatorID
					if claims.GrantID != "" && grantResolver != nil {
						result := grantResolver(claims.Sub, claims.GrantID)
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
							}
						}
					} else {
						userID = claims.Sub
						agentName = claims.AgentName
						grant = defaultGrantContext(userID)
						if agentName != "" {
							grant.AgentName = agentName
						}
					}
				}
			}

			if userID == "" {
				// For MCP endpoints, include WWW-Authenticate header for OAuth discovery
				if strings.HasPrefix(r.URL.Path, "/mcp") {
					baseURL := os.Getenv("API_BASE_URL")
					if baseURL == "" {
						scheme := "https"
						if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
							scheme = "http"
						}
						baseURL = fmt.Sprintf("%s://%s", scheme, r.Host)
					}
					resourcePath := "/.well-known/oauth-protected-resource"
					if r.URL.Path != "" && r.URL.Path != "/" {
						resourcePath += r.URL.Path
					}
					w.Header().Set("WWW-Authenticate", fmt.Sprintf(
						`Bearer resource_metadata="%s%s"`,
						baseURL,
						resourcePath,
					))
				}
				writeJSON(w, http.StatusUnauthorized, model.ApiResponse{
					Error: &model.Error{Code: "unauthorized", Message: "Authentication required. Run: memax login"},
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

			var hubID string
			var retrievalBoostHubID string
			var writeHubID string
			permsByHub := map[string]PermissionSet{}

			allHubs, _ := s.ListUserHubs(userID)
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
					if hub, err := s.GetPersonalHub(userID); err == nil {
						hubID = hub.ID
					}
				}

				// Validate membership and grant read access.
				if hubID != "" {
					if !permsByHub[hubID].Has(PermMemoryRead) && !permsByHub[hubID].Has(PermHubRead) {
						if hub, err := s.GetPersonalHub(userID); err == nil {
							hubID = hub.ID
						}
						retrievalBoostHubID = ""
					}
				}

				// Write context: explicit X-Hub-ID only. Any switched read hub is client-local state.
				writeHubID = r.Header.Get("X-Hub-ID")
				if writeHubID == "" {
					if hub, err := s.GetPersonalHub(userID); err == nil {
						writeHubID = hub.ID
					}
				}
				if writeHubID != "" {
					role, _ := s.GetHubMemberRole(writeHubID, userID)
					if role == "" {
						if hub, err := s.GetPersonalHub(userID); err == nil {
							writeHubID = hub.ID
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

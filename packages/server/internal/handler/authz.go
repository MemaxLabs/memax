package handler

import (
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

type Permission string

const (
	PermMemoryRead     Permission = "memory:read"
	PermMemoryWrite    Permission = "memory:write"
	PermMemoryDelete   Permission = "memory:delete"
	PermTopicRead      Permission = "topic:read"
	PermTopicWrite     Permission = "topic:write"
	PermDreamRead      Permission = "dream:read"
	PermDreamRun       Permission = "dream:run"
	PermHubRead        Permission = "hub:read"
	PermHubMembersRead Permission = "hub:members:read"
	PermHubManage      Permission = "hub:manage"
	PermSettingsRead   Permission = "settings:read"
	PermSettingsWrite  Permission = "settings:write"
	PermConfigRead     Permission = "config:read"
	PermConfigWrite    Permission = "config:write"
	PermChatRead       Permission = "chat:read"
	PermChatWrite      Permission = "chat:write"
	PermAccountDelete  Permission = "account:delete"
)

const (
	HubScopeAllAccessible = "all_accessible"
	HubScopeAllowlist     = "hub_allowlist"

	TrustPublic   = "public"
	TrustStandard = "standard"
	TrustElevated = "elevated"
	TrustAdmin    = "admin"
)

type PermissionSet map[Permission]bool

func NewPermissionSet(perms ...Permission) PermissionSet {
	set := PermissionSet{}
	for _, perm := range perms {
		set[perm] = true
	}
	return set
}

func (ps PermissionSet) Has(perm Permission) bool {
	return ps != nil && ps[perm]
}

func (ps PermissionSet) Intersect(other PermissionSet) PermissionSet {
	out := PermissionSet{}
	for perm := range ps {
		if other.Has(perm) {
			out[perm] = true
		}
	}
	return out
}

func (ps PermissionSet) Union(other PermissionSet) PermissionSet {
	out := PermissionSet{}
	for perm := range ps {
		out[perm] = true
	}
	for perm := range other {
		out[perm] = true
	}
	return out
}

func (ps PermissionSet) Strings() []string {
	out := make([]string, 0, len(ps))
	for perm := range ps {
		out = append(out, string(perm))
	}
	sort.Strings(out)
	return out
}

func PermissionSetFromStrings(values []string) (PermissionSet, []string) {
	out := PermissionSet{}
	var invalid []string
	for _, value := range values {
		perm := Permission(strings.TrimSpace(value))
		if perm == "" {
			continue
		}
		if !knownPermission(perm) {
			invalid = append(invalid, string(perm))
			continue
		}
		out[perm] = true
	}
	return out, invalid
}

func ExpandPermissionBundles(scopes []string) (PermissionSet, []string) {
	out := PermissionSet{}
	var invalid []string
	for _, raw := range scopes {
		scope := strings.TrimSpace(raw)
		switch scope {
		case "":
			continue
		case "read":
			out = out.Union(NewPermissionSet(PermMemoryRead, PermTopicRead, PermDreamRead, PermHubRead, PermHubMembersRead))
		case "write":
			out = out.Union(NewPermissionSet(PermMemoryWrite))
		case "organize":
			out = out.Union(NewPermissionSet(PermTopicWrite, PermDreamRun))
		case "delete":
			out = out.Union(NewPermissionSet(PermMemoryDelete))
		case "agent-sync":
			out = out.Union(NewPermissionSet(PermConfigRead, PermConfigWrite))
		default:
			perm := Permission(scope)
			if !knownPermission(perm) {
				invalid = append(invalid, scope)
				continue
			}
			out[perm] = true
		}
	}
	return out, invalid
}

func knownPermission(perm Permission) bool {
	switch perm {
	case PermMemoryRead, PermMemoryWrite, PermMemoryDelete,
		PermTopicRead, PermTopicWrite,
		PermDreamRead, PermDreamRun,
		PermHubRead, PermHubMembersRead, PermHubManage,
		PermSettingsRead, PermSettingsWrite,
		PermConfigRead, PermConfigWrite,
		PermChatRead, PermChatWrite,
		PermAccountDelete:
		return true
	default:
		return false
	}
}

type GrantContext struct {
	UserID             string
	GrantID            string
	PrincipalType      string
	AgentName          string
	HubScopeMode       string
	ScopedHubIDs       []string
	DefaultPermissions PermissionSet
	TrustLevel         string
	RateLimitTier      string
}

type AuthContext struct {
	UserID              string
	PrincipalType       string
	GrantID             string
	AgentName           string
	Source              string
	SourceAgent         string
	TrustLevel          string
	HubScopeMode        string
	ScopedHubIDs        []string
	RetrievalBoostHubID string
	PermissionsByHub    map[string]PermissionSet
	DefaultWriteHubID   string
}

const (
	grantContextKey contextKey = "grant_context"
	authContextKey  contextKey = "auth_context"

	accountPermissionScope = "_account"
)

func defaultGrantContext(userID string) GrantContext {
	return GrantContext{
		UserID:             userID,
		PrincipalType:      "user",
		HubScopeMode:       HubScopeAllAccessible,
		DefaultPermissions: DefaultUserPermissions(),
		TrustLevel:         TrustElevated,
	}
}

func DefaultUserPermissions() PermissionSet {
	return NewPermissionSet(
		PermMemoryRead,
		PermMemoryWrite,
		PermMemoryDelete,
		PermTopicRead,
		PermTopicWrite,
		PermDreamRead,
		PermDreamRun,
		PermHubRead,
		PermHubMembersRead,
		PermHubManage,
		PermSettingsRead,
		PermSettingsWrite,
		PermConfigRead,
		PermConfigWrite,
		PermChatRead,
		PermChatWrite,
		PermAccountDelete,
	)
}

func HubScopedAllowedPermissions() PermissionSet {
	return NewPermissionSet(
		PermMemoryRead,
		PermMemoryWrite,
		PermMemoryDelete,
		PermTopicRead,
		PermTopicWrite,
		PermDreamRead,
		PermDreamRun,
		PermHubRead,
		PermHubMembersRead,
		PermHubManage,
	)
}

func AccountPermissions() PermissionSet {
	return NewPermissionSet(
		PermSettingsRead,
		PermSettingsWrite,
		PermConfigRead,
		PermConfigWrite,
		// Chat sessions are owner-scoped at the lifecycle level
		// (your own sessions, not someone else's). HubContext
		// middleware places this set on the synthetic
		// accountPermissionScope so non-hub-scoped routes
		// (CanAny over permsByHub) can see them — without this,
		// /v1/chat/* would return 403 for normal authenticated
		// users because PermChat* would never appear in any
		// permsByHub entry. Codex caught this gap on Phase 3.2.
		PermChatRead,
		PermChatWrite,
		PermAccountDelete,
	)
}

func rolePermissionSet(role string, hub *model.Hub) PermissionSet {
	base := NewPermissionSet(
		PermMemoryRead,
		PermTopicRead,
		PermDreamRead,
		PermHubRead,
		PermHubMembersRead,
	)
	switch role {
	case "owner", "admin":
		return base.Union(NewPermissionSet(PermMemoryWrite, PermMemoryDelete, PermTopicWrite, PermDreamRun, PermHubManage))
	case "contributor":
		perms := base.Union(NewPermissionSet(PermMemoryWrite))
		if hub != nil {
			if hub.ContributorDeletePolicy != "none" {
				perms[PermMemoryDelete] = true
			}
			if hub.AllowContributorTopics {
				perms[PermTopicWrite] = true
			}
			if hub.AllowContributorDreams {
				perms[PermDreamRun] = true
			}
		}
		return perms
	case "viewer":
		return base
	default:
		return PermissionSet{}
	}
}

func grantFromRequest(r *http.Request) GrantContext {
	if v, ok := r.Context().Value(grantContextKey).(GrantContext); ok {
		return v
	}
	return defaultGrantContext(GetUserID(r))
}

func GetAuthContext(r *http.Request) *AuthContext {
	if v, ok := r.Context().Value(authContextKey).(*AuthContext); ok {
		return v
	}
	return nil
}

func Can(r *http.Request, perm Permission, hubID string) bool {
	authz := GetAuthContext(r)
	if authz == nil {
		return false
	}
	return authz.PermissionsByHub[hubID].Has(perm)
}

func CanAny(r *http.Request, perm Permission) bool {
	authz := GetAuthContext(r)
	if authz == nil {
		return false
	}
	for _, perms := range authz.PermissionsByHub {
		if perms.Has(perm) {
			return true
		}
	}
	return false
}

func hubIDsWithPermission(permsByHub map[string]PermissionSet, perm Permission) []string {
	hubIDs := make([]string, 0, len(permsByHub))
	for hubID, perms := range permsByHub {
		if perms.Has(perm) {
			hubIDs = append(hubIDs, hubID)
		}
	}
	return hubIDs
}

func requireRequestPermission(w http.ResponseWriter, r *http.Request, perm Permission, hubID string) bool {
	if hubID != "" {
		if Can(r, perm, hubID) {
			return true
		}
	} else if CanAny(r, perm) {
		return true
	}
	slog.Warn("request denied by grant permission", "user_id", GetUserID(r), "permission", perm, "hub_id", hubID, "path", r.URL.Path, "method", r.Method)
	writeError(w, http.StatusForbidden, "permission_denied", "This credential is not allowed to perform that action.")
	return false
}

func AuthorizeHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		perm, hubScoped, ok := requiredHTTPPermission(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		hubID := ""
		if hubScoped {
			hubID = GetWriteHubID(r)
		}
		if !requireRequestPermission(w, r, perm, hubID) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requiredHTTPPermission(r *http.Request) (Permission, bool, bool) {
	path := r.URL.Path
	method := r.Method
	switch {
	case strings.HasPrefix(path, "/mcp"):
		return "", false, false
	case strings.HasPrefix(path, "/v1/memories/batch-delete"):
		return PermMemoryDelete, false, true
	case strings.HasPrefix(path, "/v1/memories/batch-move"):
		return PermMemoryWrite, true, true
	// POST /v1/memories/{id}/access is a read-scope signal (recording
	// that a memory was deliberately viewed), not a write. Read-only
	// grants must be able to fire it — otherwise the decay multiplier
	// stops reinforcing for CLI/SDK clients holding read grants, and
	// the two MCP implementations diverge (the Go MCP increments
	// after PermMemoryRead, so the REST/TS-MCP path must match).
	// Declared before the generic /v1/memories matcher so the POST
	// verb doesn't fall through to PermMemoryWrite.
	case strings.HasPrefix(path, "/v1/memories/") && strings.HasSuffix(path, "/access"):
		return PermMemoryRead, false, true
	case strings.HasPrefix(path, "/v1/memories") || strings.HasPrefix(path, "/v1/uploads"):
		switch method {
		case http.MethodGet:
			return PermMemoryRead, false, true
		case http.MethodPost, http.MethodPatch, http.MethodPut:
			return PermMemoryWrite, true, true
		case http.MethodDelete:
			return PermMemoryDelete, false, true
		}
	case strings.HasPrefix(path, "/v1/recall") || strings.HasPrefix(path, "/v1/ask"):
		return PermMemoryRead, false, true
	// /v1/bar/* surfaces the same memory-read content as /v1/recall
	// (it calls searchMemoriesCore under the hood, which the same
	// security boundary applies to). Without this floor, a scoped
	// credential could reach bar search without the PermMemoryRead
	// gate even though the underlying store query is identical.
	// Codex caught this gap in the strict-recall review (commit
	// 5655ac2e).
	case strings.HasPrefix(path, "/v1/bar"):
		return PermMemoryRead, false, true
	case strings.HasPrefix(path, "/v1/topics"):
		switch method {
		case http.MethodGet:
			return PermTopicRead, false, true
		default:
			return PermTopicWrite, false, true
		}
	case strings.HasPrefix(path, "/v1/dreams/trigger"):
		return PermDreamRun, false, true
	case strings.HasPrefix(path, "/v1/dreams") || strings.HasPrefix(path, "/v1/reviews"):
		return PermDreamRead, false, true
	// /v1/usage/dreams reports the caller's dream quota state; a
	// credential with NO PermDreamRead anywhere has no business
	// reading it. Per-hub authorization for ?hub_id queries lives
	// in the handler itself; this check is the floor.
	case path == "/v1/usage/dreams":
		return PermDreamRead, false, true
	// /v1/notifications is the Phase 3b successor to /v1/reviews. It
	// covers the inbox surface (list / summary / seen / dismiss /
	// resolve / bulk) that was previously gated under reviews, plus
	// new receipt kinds (dream_run_completed, hub_invite). Gated
	// under PermDreamRead to match the existing reviews convention —
	// the fine-grained per-kind access rules (hub membership for
	// hub_member-audience rows, invitee identity for hub_invite)
	// live at the store layer via notificationVisibilityClause and
	// AcceptHubInviteByID. The coarse AuthorizeHTTP gate here just
	// ensures the caller has the inbox-read permission bundle. Not
	// hub-scoped: the list endpoint answers "what is visible to me
	// across every hub" by default (union visibility per §4.4), so
	// a write-hub hint would be the wrong scope lens.
	case strings.HasPrefix(path, "/v1/notifications"):
		return PermDreamRead, false, true
	case strings.HasPrefix(path, "/v1/settings"):
		if method == http.MethodGet {
			return PermSettingsRead, false, true
		}
		return PermSettingsWrite, false, true
	case strings.HasPrefix(path, "/v1/configs"):
		if method == http.MethodGet {
			return PermConfigRead, false, true
		}
		return PermConfigWrite, false, true
	// Chat sessions are owner-scoped at the lifecycle level (create/read/
	// list/patch/delete your own sessions). The per-session scope_type /
	// scope_hub_ids constrains what data the session can READ at recall
	// time — that's enforced by the chat handler via accessible-hub
	// intersection, not here. This gate is the floor: a credential
	// without chat:* can't touch the session-management surface at all.
	case strings.HasPrefix(path, "/v1/chat"):
		if method == http.MethodGet {
			return PermChatRead, false, true
		}
		return PermChatWrite, false, true
	case strings.HasPrefix(path, "/v1/hubs"):
		if method == http.MethodGet {
			return PermHubRead, false, true
		}
		return PermHubManage, false, true
	case strings.HasPrefix(path, "/v1/account/data"):
		return PermAccountDelete, false, true
	}
	return "", false, false
}

package v2api

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// principal is the caller as the ledger sees them.
type principal struct {
	actor ledger.Actor
	// scope is every space the caller may touch: the user's memberships
	// (ledger.ResolveUserScope), narrowed to the credential's hubs when the
	// credential is hub-scoped.
	scope ledger.Scope
	via   policy.Via
	// impersonated is set for an operator's impersonation session, which
	// may read but not change the record: a receipt must name who acted.
	impersonated bool
}

// principalFor maps the authenticated request onto a ledger actor and
// scope. It is the one place this mapping lives:
//
//   - A signed-in session is a person, acting on their own authority.
//   - An API key, an OAuth grant, or a legacy agent token is an agent
//     working for that person. Its actor ID is the credential (the
//     agent connection, until agent_connections exist), and its autonomy
//     is Propose when the credential has memory:write, Read otherwise.
//     policy.Decide caps API keys at Propose anyway: a key can never keep,
//     reject or forget (HANDOFF §4). Per-space autonomy arrives with agent
//     connections (epic 1.8) and replaces this rule here, nowhere else.
//   - A credential without memory:read can't use /v2 at all.
//   - The surface (via) is what the client says, from X-Memax-Via, but
//     only among api, cli and mcp. Those are all client-attested; the web
//     and Review, which give a keep human_web assurance, need a session
//     the server can tell apart from the CLI's, which doesn't exist yet
//     (plan 25 §5.12, §5.15). Until then nothing on /v2 is human_web.
func (h *Handler) principalFor(r *http.Request) (*principal, *apiError) {
	userID, err := uuid.Parse(handler.GetUserID(r))
	if err != nil {
		return nil, &apiError{status: http.StatusUnauthorized, code: codeUnauthorized,
			message: "Sign in again: this session has no Memax account."}
	}
	grant := handler.GetGrant(r)
	if !grant.DefaultPermissions.Has(handler.PermMemoryRead) {
		return nil, &apiError{status: http.StatusForbidden, code: codePermissionDenied,
			message: "This credential can't read memories. Create a key with read access in Settings."}
	}
	via, apiErr := viaFrom(r)
	if apiErr != nil {
		return nil, apiErr
	}
	scope, err := h.ledger.UserScope(r.Context(), userID)
	if err != nil {
		return nil, h.fromLedger(r, err)
	}
	if grant.HubScopeMode == handler.HubScopeAllowlist {
		scope = scope.Narrow(parseIDs(grant.ScopedHubIDs)...)
	}

	p := &principal{scope: scope, via: via, impersonated: handler.GetImpersonatorID(r) != ""}
	isAgent := grant.PrincipalType == "api_key" || grant.PrincipalType == "oauth_grant" || grant.AgentName != ""
	if !isAgent {
		p.actor = ledger.Actor{Kind: policy.ActorPerson, ID: userID, Credential: policy.CredentialSession}
		return p, nil
	}
	credential := policy.CredentialOAuth
	if grant.PrincipalType == "api_key" {
		credential = policy.CredentialAPIKey
	}
	id, err := uuid.Parse(grant.GrantID)
	if err != nil {
		// A legacy agent token carries no grant; it acts for the user.
		id = userID
	}
	autonomy := policy.AutonomyRead
	if grant.DefaultPermissions.Has(handler.PermMemoryWrite) {
		autonomy = policy.AutonomyPropose
	}
	p.actor = ledger.Actor{
		Kind: policy.ActorAgent, ID: id, Name: grant.AgentName, Agent: grant.AgentName,
		Autonomy: autonomy, Credential: credential,
	}
	return p, nil
}

// clientVias are the surfaces a client may declare.
var clientVias = map[string]policy.Via{"api": policy.ViaAPI, "cli": policy.ViaCLI, "mcp": policy.ViaMCP}

func viaFrom(r *http.Request) (policy.Via, *apiError) {
	raw := strings.TrimSpace(r.Header.Get("X-Memax-Via"))
	if raw == "" {
		return policy.ViaAPI, nil
	}
	v, ok := clientVias[strings.ToLower(raw)]
	if !ok {
		return "", invalidRequest("X-Memax-Via", "X-Memax-Via must be api, cli or mcp.")
	}
	return v, nil
}

// command checks what every command needs on top of the principal.
func (p *principal) command(r *http.Request) (string, *apiError) {
	if p.impersonated {
		return "", &apiError{status: http.StatusForbidden, code: codeImpersonation,
			message: "Impersonation sessions can read the record but not change it: a receipt must name who acted."}
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		return "", &apiError{status: http.StatusBadRequest, code: codeIdempotencyKeyRequired,
			message: "Send an Idempotency-Key header (a uuid works) with every command, and the same key when you retry it."}
	}
	return key, nil
}

func parseIDs(ss []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ss))
	for _, s := range ss {
		if id, err := uuid.Parse(s); err == nil {
			out = append(out, id)
		}
	}
	return out
}

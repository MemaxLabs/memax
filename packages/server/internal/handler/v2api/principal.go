package v2api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/websurface"
)

// principal is the caller as the ledger sees them.
type principal struct {
	actor ledger.Actor
	// scope is every space the caller may touch: the user's memberships
	// (ledger.ResolveUserScope), narrowed to the credential's hubs when the
	// credential is hub-scoped, and for an agent carrying its connection's
	// autonomy in each space (ledger.Scope.WithConnection).
	scope ledger.Scope
	via   policy.Via
	// connection is the agent's connection, when it has one.
	connection *ledger.Connection
	// impersonated is set for an operator's impersonation session, which
	// may read but not change the record: a receipt must name who acted.
	impersonated bool
}

// principalFor maps the authenticated request onto a ledger actor and
// scope. It is the one place this mapping lives:
//
//   - A signed-in session is a person, acting on their own authority.
//   - An API key or an OAuth grant is an agent working for that person,
//     through the agent connection the credential is bound to (plan 25
//     §5.15). The actor is the connection, so receipts name the agent, and
//     its autonomy in each space is the connection's level there, capped by
//     the credential: Read without memory:write. policy.Decide caps API keys
//     at Propose anyway, so a key can never keep, reject or forget
//     (HANDOFF §4).
//   - A credential with no connection, or a paused or disconnected one, and
//     a legacy agent token (which has no credential to bind), only read:
//     their writes are refused with agent_not_connected or agent_paused,
//     which say where to fix it.
//   - A credential without memory:read can't use /v2 at all.
//   - The surface (via) is what the client says, from X-Memax-Via, but
//     only among api, cli and mcp, all client-attested. It is the web
//     (assurance human_web for a person's keep) only when the server can
//     tell: the session was issued to the web app and the web app's proxy
//     signed the request (webSurface, internal/websurface). A request that
//     claims the web and fails that check is refused.
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
	// A credential that names hubs is limited to them. That includes an
	// old key bound to one hub (api_keys.hub_id) whose scope mode was never
	// set to the allowlist: it fails closed here.
	if grant.HubScopeMode == handler.HubScopeAllowlist || len(grant.ScopedHubIDs) > 0 {
		scope = scope.Narrow(parseIDs(grant.ScopedHubIDs)...)
	}

	p := &principal{scope: scope, via: via, impersonated: handler.GetImpersonatorID(r) != ""}
	isAgent := grant.PrincipalType == "api_key" || grant.PrincipalType == "oauth_grant" || grant.AgentName != ""
	if !isAgent {
		p.actor = ledger.Actor{Kind: policy.ActorPerson, ID: userID, Credential: policy.CredentialSession}
		if e := h.webSurface(r, userID, grant, p); e != nil {
			return nil, e
		}
		return p, nil
	}

	credential, kind := policy.CredentialOAuth, ledger.CredentialOAuthGrant
	if grant.PrincipalType == "api_key" {
		credential, kind = policy.CredentialAPIKey, ledger.CredentialAPIKey
	}
	limit := policy.AutonomyRead
	if grant.DefaultPermissions.Has(handler.PermMemoryWrite) {
		limit = policy.AutonomyWrite
	}
	name := grant.AgentName
	if k := ledger.AgentFromV1(name); k != ledger.AgentOther {
		name = k.Name()
	}
	p.actor = ledger.Actor{
		Kind: policy.ActorAgent, ID: userID, Name: name, Agent: grant.AgentName,
		Autonomy: policy.AutonomyRead, Credential: credential,
	}
	// A legacy agent token carries no grant, so nothing to bind: it reads.
	if credID, err := uuid.Parse(grant.GrantID); err == nil && grant.PrincipalType != "user" {
		p.actor.ID = credID
		conn, err := h.ledger.ConnectionForCredential(r.Context(), scope, kind, credID)
		if err != nil {
			return nil, h.fromLedger(r, err)
		}
		if conn != nil {
			p.connection = conn
			p.actor.ID, p.actor.Name, p.actor.Agent = conn.ID, conn.DisplayName, string(conn.Agent)
			h.seen.touch(h, userID, conn)
		}
	}
	p.scope = scope.WithConnection(p.connection, limit)
	return p, nil
}

// webSurface makes a person's request come through the web (via web, so a
// keep is human_web) when both halves of internal/websurface hold: the
// session token was issued to the web app (its surface claim), and the web
// app's proxy signed this request for this user. A request that claims the
// web but whose signature doesn't verify is refused: nothing legitimate
// sends one. A valid signature on a session the web app didn't get (a CLI
// login sent through the public proxy) stays client-attested, and so does
// everything when the mechanism is disabled (no WEB_SURFACE_SECRET).
func (h *Handler) webSurface(r *http.Request, userID uuid.UUID, grant handler.GrantContext, p *principal) *apiError {
	if !websurface.Claimed(r) {
		return nil
	}
	if h.web == nil {
		h.warnWebDisabled()
		return nil
	}
	err := h.web.Verify(r, userID.String())
	var we *websurface.Error
	if errors.As(err, &we) {
		h.log.WarnContext(r.Context(), "v2: refused a request that claims the web app", "reason", we.Reason,
			"method", r.Method, "path", r.URL.Path, "user_id", userID.String())
		return &apiError{status: http.StatusForbidden, code: codeSurfaceUnverified,
			message: "This request says it comes from the Memax web app, but Memax couldn't verify that. Reload the page and try again; if it keeps happening, contact support@memax.app."}
	}
	if err != nil {
		return nil
	}
	if grant.Surface != auth.SurfaceWeb || p.impersonated {
		h.log.InfoContext(r.Context(), "v2: a signed web request on a session the web app wasn't issued counts as client-attested",
			"user_id", userID.String(), "session_surface", grant.Surface)
		return nil
	}
	p.via = policy.ViaWeb
	return nil
}

// warnWebDisabled logs, at most every few minutes, that the web app's
// requests can't be verified.
func (h *Handler) warnWebDisabled() {
	now := h.now().Unix()
	last := h.webWarned.Load()
	if now-last < 300 || !h.webWarned.CompareAndSwap(last, now) {
		return
	}
	h.log.Warn("v2: a request claims the web app, but WEB_SURFACE_SECRET is not set, so it counts as client-attested. " +
		"Keeps of quarantined proposals and of team-space decisions need a person on the web and will be refused. " +
		"Set the same WEB_SURFACE_SECRET on the API and the web app.")
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

// seenEvery is how often an agent's last_seen_at is written at most.
const seenEvery = time.Minute

// seenTracker writes agents' last_seen_at off the request path: at most
// once a minute per connection per process, in the background, never in
// a command's transaction.
type seenTracker struct {
	mu   sync.Mutex
	last map[uuid.UUID]time.Time
	wg   sync.WaitGroup
}

func (s *seenTracker) touch(h *Handler, person uuid.UUID, c *ledger.Connection) {
	now := h.now()
	if c.State == ledger.ConnectionDisconnected || (c.LastSeenAt != nil && now.Sub(*c.LastSeenAt) < seenEvery) {
		return
	}
	s.mu.Lock()
	if s.last == nil || len(s.last) > 10_000 {
		s.last = map[uuid.UUID]time.Time{}
	}
	if t, ok := s.last[c.ID]; ok && now.Sub(t) < seenEvery {
		s.mu.Unlock()
		return
	}
	s.last[c.ID] = now
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.ledger.TouchConnection(ctx, person, c.ID, now); err != nil {
			h.log.Warn("v2: could not record when an agent was last seen", "connection", c.ID.String(), "error", err)
		}
	}()
}

// Wait blocks until background work (last-seen updates) has finished.
// Call it at shutdown, and in tests before the database goes away.
func (h *Handler) Wait() { h.seen.wg.Wait() }

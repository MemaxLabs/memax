package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// OAuth consent (OAuthConsent, plan 25 §5.15): a person lets an MCP client
// connect to one of their spaces. GET /oauth/authorize stores the client's
// request and sends the browser to the web app's page for it
// (/oauth/authorize?request=<id>). The web session says who the person is,
// whichever way they signed in (GitHub, Google, an email code, a passkey):
// the page reads and answers the request through the web app's proxy,
// which attaches the session's access token and refuses requests other
// sites start (Fetch Metadata, SameSite=Strict cookies).
//
// The first person to open a request is bound to it; it is nobody else's.
// "Not you?" releases it. There is no consent token: the request ID is the
// unguessable handle (256 bits, 10 minutes), the session is the person and
// the proxy is the CSRF defence, so a second secret in the same URL would
// add nothing.
//
// The answer is JSON: the URL to send the browser to, the client's
// registered redirect_uri with a code or access_denied, built here. The
// page only follows it.
//
// What the page says an agent will and won't be able to do in a space is
// decided here by policy, for the level consent connects the agent at, so
// it can't promise more or less than is true.

// consentPath is the web app's page for a request.
const consentPath = "/oauth/authorize"

// Abilities: what an agent connected to a space may (can) or may not
// (cannot) do there, as the consent page lists them.
const (
	abilityReadBrief    = "read_brief"    // V2: the Brief and kept memories
	abilityReadMemories = "read_memories" // V1: the space's memories
	abilityPropose      = "propose"       // V2: proposals that wait for a person
	abilityKeep         = "keep"          // V2: keep without a person (Write)
	abilityAdd          = "add"           // V1: memories, kept as they are written
	abilityGate         = "gate"          // ask a person to decide at a fork
	abilityForget       = "forget"
	abilityOtherSpaces  = "other_spaces" // read or write any other space
)

type oauthConsentPerson struct {
	Name string `json:"name"`
}

type oauthConsentTarget struct {
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
}

// oauthConsentSpace is one of the person's spaces, as the consent page
// shows it.
type oauthConsentSpace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	// Kind is personal, project or team; OnV2 whether it is on the V2
	// record (V1's rules apply to the rest).
	Kind string `json:"kind"`
	OnV2 bool   `json:"on_v2"`
	Role string `json:"role"`
	// Disabled: the person's role can't use what the client asked for.
	Disabled bool `json:"disabled"`
	People   int  `json:"people"`
	// Memories counts the kept memories on V2 (absent when the ledger
	// can't say), V1's memories on V1. Targets are the files it compiles.
	Memories *int                 `json:"memories,omitempty"`
	Targets  []oauthConsentTarget `json:"targets,omitempty"`
	// Autonomy is the level consent connects the agent at here, and
	// Ceiling the most a person may later allow it in Agents: the granted
	// scope's and the person's role's (V2 only).
	Autonomy string `json:"autonomy,omitempty"`
	Ceiling  string `json:"ceiling,omitempty"`
	// Can and Cannot are abilities, decided by policy for that level.
	Can    []string `json:"can"`
	Cannot []string `json:"cannot"`
}

// oauthRequestView is what GET /oauth/authorize/requests/{id} answers.
type oauthRequestView struct {
	RequestID string `json:"request_id"`
	// ClientName is the name the client gives itself; ClientHost, for a
	// metadata-document client, the host Memax fetched it from (verified,
	// unlike the name). AgentName is Memax's slug for it.
	ClientName string `json:"client_name"`
	ClientHost string `json:"client_host,omitempty"`
	AgentName  string `json:"agent_name"`
	Resource   string `json:"resource,omitempty"`
	// Scope is the scope the grant will carry: the client's request.
	Scope     string              `json:"scope"`
	ExpiresAt time.Time           `json:"expires_at"`
	ExpiresIn int                 `json:"expires_in"`
	Person    oauthConsentPerson  `json:"person"`
	Spaces    []oauthConsentSpace `json:"spaces"`
}

// oauthDecision is the person's answer to a request.
type oauthDecision struct {
	// Decision is approve or deny.
	Decision string `json:"decision"`
	// SpaceID is the one space an approval connects the agent to.
	SpaceID string `json:"space_id,omitempty"`
}

// oauthDecided says where the browser goes next: the client's registered
// redirect_uri with a code, or with access_denied.
type oauthDecided struct {
	RedirectTo string `json:"redirect_to"`
}

// webPerson is the person a consent request may be answered by: a session
// the web app was issued (the token's surface), as themselves. Agents'
// credentials, CLI sessions and impersonation get "" and answer nothing.
func webPerson(r *http.Request) string {
	g := GetGrant(r)
	if g.PrincipalType != "user" || g.AgentName != "" || g.Surface != auth.SurfaceWeb || GetImpersonatorID(r) != "" {
		return ""
	}
	return GetUserID(r)
}

func refuseNonWebPerson(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "consent_by_person_on_web",
		"Only a person signed in to the Memax web app answers an agent's request. Open the link again in your browser.")
}

// requestGone answers a request that isn't there for this person: unknown,
// answered already, or someone else's.
func requestGone(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "consent_request_not_found",
		"This request has ended, or it isn't yours. Start connecting again from your agent.")
}

// openRequest binds the request to the person when nobody has it yet (a
// request bound by the old GitHub callback, which still carries a consent
// token, counts as nobody's for one release) and loads it. Errors:
// errRequestGone, errRequestExpired.
//
// oauth_authorization_requests.csrf_token is read only to tell those old
// requests apart: once ResumeOnWeb and LegacyConsent go (one release),
// drop the column in a migration, and these clauses with it.
func (h *MCPOAuthHandler) openRequest(ctx context.Context, id, person string) (oauthPendingSession, error) {
	me, err := uuid.Parse(person)
	if err != nil || id == "" {
		return oauthPendingSession{}, errRequestGone
	}
	if _, err := h.authH.pool.Exec(ctx, `
		UPDATE oauth_authorization_requests SET user_id = $2::uuid, csrf_token = ''
		 WHERE id = $1 AND expires_at > now() AND (user_id IS NULL OR csrf_token <> '')`, id, me); err != nil {
		return oauthPendingSession{}, err
	}
	session, err := h.loadOAuthAuthorizationRequest(ctx, id)
	bound, _ := uuid.Parse(session.userID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oauthPendingSession{}, errRequestGone
	case err != nil:
		return oauthPendingSession{}, err
	case time.Now().After(session.expiresAt):
		h.deleteOAuthAuthorizationRequest(session.id)
		return oauthPendingSession{}, errRequestExpired
	case bound != me:
		return oauthPendingSession{}, errRequestGone
	}
	session.userID = me.String()
	return session, nil
}

var (
	errRequestGone    = errors.New("consent request gone")
	errRequestExpired = errors.New("consent request expired")
)

// answerOpenError writes openRequest's error.
func answerOpenError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errRequestGone):
		requestGone(w)
	case errors.Is(err, errRequestExpired):
		writeError(w, http.StatusGone, "consent_request_expired",
			"This request expired: requests last 10 minutes. Start connecting again from your agent.")
	default:
		slog.Error("MCP OAuth: can't open the consent request", "error", err)
		writeError(w, http.StatusInternalServerError, "consent_request_failed", "The request didn't load. Try again.")
	}
}

// OpenRequest serves GET /oauth/authorize/requests/{id}: the request, for
// the person signed in on the web, with their spaces and what the agent
// will and won't be able to do in each.
func (h *MCPOAuthHandler) OpenRequest(w http.ResponseWriter, r *http.Request) {
	if h.authH == nil || h.authH.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "oauth_unavailable", "OAuth is not available")
		return
	}
	person := webPerson(r)
	if person == "" {
		refuseNonWebPerson(w)
		return
	}
	session, err := h.openRequest(r.Context(), r.PathValue("id"), person)
	if err != nil {
		answerOpenError(w, err)
		return
	}
	view, err := h.requestView(r.Context(), session)
	if err != nil {
		slog.Error("MCP OAuth: can't describe the consent request", "error", err)
		writeError(w, http.StatusInternalServerError, "consent_request_failed", "The request didn't load. Try again.")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: view})
}

// DecideRequest serves POST /oauth/authorize/requests/{id}/decision: the
// bound person's approve (one space) or deny. The answer is where to send
// the browser: the client's registered redirect_uri, which Authorize
// checked, with a code or access_denied (and state and iss).
func (h *MCPOAuthHandler) DecideRequest(w http.ResponseWriter, r *http.Request) {
	if h.authH == nil || h.authH.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "oauth_unavailable", "OAuth is not available")
		return
	}
	person := webPerson(r)
	if person == "" {
		refuseNonWebPerson(w)
		return
	}
	var body oauthDecision
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || (body.Decision != "approve" && body.Decision != "deny") {
		writeError(w, http.StatusBadRequest, "invalid_request", `Send {"decision": "approve", "space_id": "…"} or {"decision": "deny"}.`)
		return
	}
	session, err := h.openRequest(r.Context(), r.PathValue("id"), person)
	if err != nil {
		answerOpenError(w, err)
		return
	}
	iss := h.resolveBaseURL(r)
	if body.Decision == "deny" {
		h.deleteOAuthAuthorizationRequest(session.id)
		to, err := errorResponseURL(session.redirectURI, session.state, iss, "access_denied", "The person declined to connect this agent")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "consent_failed", "The client's redirect URI can't be used.")
			return
		}
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: oauthDecided{RedirectTo: to}})
		return
	}

	space, ok, err := h.consentSpace(r.Context(), person, body.SpaceID, session)
	if err != nil {
		slog.Error("MCP OAuth: can't check the consent space", "error", err)
		writeError(w, http.StatusInternalServerError, "consent_failed", "That didn't go through. Try again.")
		return
	}
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "consent_space",
			"That space can't be connected from this account. Choose one of your spaces.")
		return
	}
	scope := consentScope(session.requestedScope)
	perms, _, _ := oauthPermissionsFromScope(scope)
	perms = perms.Intersect(session.requestedPermissions)
	grantID, err := h.createOAuthGrant(r.Context(), session, []string{space}, perms, scope)
	if err != nil {
		slog.Error("failed to create MCP OAuth grant", "error", err)
		writeError(w, http.StatusInternalServerError, "consent_failed", "That didn't go through. Try again.")
		return
	}
	h.connectAgent(r.Context(), session, grantID, []string{space}, scope)

	code := generateOAuthSession()
	if _, err := h.authH.pool.Exec(r.Context(),
		`INSERT INTO auth_codes (code, user_id, expires_at, code_challenge, grant_id, client_id, redirect_uri)
		VALUES ($1, $2::uuid, $3, $4, $5::uuid, $6, $7)`,
		code, session.userID, time.Now().Add(5*time.Minute), session.codeChallenge, grantID, session.clientID, session.redirectURI); err != nil {
		slog.Error("failed to store MCP auth code", "error", err)
		writeError(w, http.StatusInternalServerError, "consent_failed", "That didn't go through. Try again.")
		return
	}
	h.deleteOAuthAuthorizationRequest(session.id)
	to, err := codeResponseURL(session.redirectURI, session.state, iss, code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "consent_failed", "The client's redirect URI can't be used.")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: oauthDecided{RedirectTo: to}})
}

// ReleaseRequest serves POST /oauth/authorize/requests/{id}/release: "Not
// you?". The person bound to the request lets go of it, so whoever signs
// in next can open it; nobody else can.
func (h *MCPOAuthHandler) ReleaseRequest(w http.ResponseWriter, r *http.Request) {
	if h.authH == nil || h.authH.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "oauth_unavailable", "OAuth is not available")
		return
	}
	person := webPerson(r)
	if person == "" {
		refuseNonWebPerson(w)
		return
	}
	if _, err := uuid.Parse(person); err != nil {
		requestGone(w)
		return
	}
	tag, err := h.authH.pool.Exec(r.Context(), `
		UPDATE oauth_authorization_requests SET user_id = NULL
		 WHERE id = $1 AND user_id = $2::uuid AND csrf_token = '' AND expires_at > now()`, r.PathValue("id"), person)
	if err != nil {
		slog.Error("MCP OAuth: can't release the consent request", "error", err)
		writeError(w, http.StatusInternalServerError, "consent_failed", "That didn't go through. Try again.")
		return
	}
	if tag.RowsAffected() != 1 {
		requestGone(w)
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]bool{"released": true}})
}

// ResumeOnWeb sends a browser GitHub returned with a request from before
// sign-in moved to the web app (state "mcp:<request>") on to the web page
// for it, without signing anyone in: the person signs in there. Remove
// after one release, with LegacyConsent.
func (h *MCPOAuthHandler) ResumeOnWeb(w http.ResponseWriter, r *http.Request, requestID string) {
	if page := h.webRequestURL(r, requestID); page != "" {
		http.Redirect(w, r, page, http.StatusSeeOther)
		return
	}
	http.Error(w, "This authorization request can't go on here. Start connecting again from your agent.", http.StatusBadRequest)
}

// LegacyConsent serves POST /oauth/authorize/consent, where V1's consent
// page posted (a page loaded before the deploy): it changes nothing and
// sends the browser to the web page for the request, where the person
// answers signed in. Remove after one release.
func (h *MCPOAuthHandler) LegacyConsent(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	h.ResumeOnWeb(w, r, r.FormValue("session_id"))
}

// consentSpace is the space an approval connects: one of the person's,
// whose role can use what the client asked for.
func (h *MCPOAuthHandler) consentSpace(ctx context.Context, person, spaceID string, session oauthPendingSession) (string, bool, error) {
	if h.authH.store == nil {
		return "", false, errors.New("store is not configured")
	}
	if _, err := uuid.Parse(spaceID); err != nil {
		return "", false, nil
	}
	hubs, err := h.authH.store.ListUserHubs(person)
	if err != nil {
		return "", false, err
	}
	for _, item := range hubs {
		if item.Hub.ID == spaceID {
			usable := len(rolePermissionSet(item.Role, &item.Hub).Intersect(session.requestedPermissions)) > 0
			return item.Hub.ID, usable, nil
		}
	}
	return "", false, nil
}

// requestView is the page's view of a request bound to its person.
func (h *MCPOAuthHandler) requestView(ctx context.Context, session oauthPendingSession) (oauthRequestView, error) {
	if h.authH.store == nil {
		return oauthRequestView{}, errors.New("store is not configured")
	}
	hubs, err := h.authH.store.ListUserHubs(session.userID)
	if err != nil {
		return oauthRequestView{}, err
	}
	spaces := make([]oauthConsentSpace, len(hubs))
	for i, item := range hubs {
		supported := rolePermissionSet(item.Role, &item.Hub).Intersect(session.requestedPermissions)
		memories := item.MemoryCount
		spaces[i] = oauthConsentSpace{
			ID: item.Hub.ID, Name: item.Hub.Name, Slug: item.Hub.Slug, Role: item.Role,
			Kind: item.Hub.HubType, Disabled: len(supported) == 0, Memories: &memories,
			Can: []string{}, Cannot: []string{},
		}
	}
	h.describeSpaces(ctx, session, hubs, spaces)
	return oauthRequestView{
		RequestID:  session.id,
		ClientName: displayOAuthClientName(session.clientName),
		ClientHost: clientHost(session.clientID),
		AgentName:  agentNameFromClientName(session.clientName),
		Resource:   session.resource,
		Scope:      consentScope(session.requestedScope),
		ExpiresAt:  session.expiresAt,
		ExpiresIn:  consentExpiresIn(session.expiresAt),
		Person:     oauthConsentPerson{Name: h.personName(ctx, session.userID)},
		Spaces:     spaces,
	}, nil
}

// consentScope is the scope a consent grants: what the client asked for,
// up to memax:write. The scope is a ceiling, not the level: the agent is
// connected at consentLevel, and only a person on the web raises it.
func consentScope(requested string) string {
	return intersectScopes(requested, strings.Join([]string{ScopeRead, ScopePropose, ScopeWrite}, " "))
}

// consentLevel is the autonomy consent connects an agent at in a space,
// as connectAgent and the ledger's ConnectAgent decide it: where the agent
// starts (Cursor and Gemini CLI read; the rest take the space's default for
// new agents, within the person's ceiling), capped by the granted scope
// and, with no person on the web to raise it, by Propose.
func consentLevel(agent ledger.AgentKind, role policy.Role, sp policy.Space, scope string) policy.Autonomy {
	level := agent.StartAutonomy()
	if level == "" {
		level = policy.DefaultAutonomy(role, sp)
	}
	return policy.MinAutonomy(level, policy.Autonomy(scopeCeiling(scope)), policy.AutonomyPropose)
}

// consentCeiling is the most a person may later allow the agent in a
// space, in Agents: what the grant's scope allows, and what their role may
// let an agent do (policy.AgentCeiling).
func consentCeiling(role policy.Role, sp policy.Space, scope string) policy.Autonomy {
	return policy.MinAutonomy(policy.Autonomy(scopeCeiling(scope)), policy.AgentCeiling(role, sp.Rules))
}

// v2Abilities asks policy what an agent at `level` may do in a space on the
// V2 record. Only a person keeps or forgets; an agent at Read neither
// proposes nor asks.
func v2Abilities(name string, role policy.Role, sp policy.Space, level policy.Autonomy) (can, cannot []string) {
	a := policy.Actor{Kind: policy.ActorAgent, Name: name, Role: role, Autonomy: level,
		Credential: policy.CredentialOAuth, Via: policy.ViaMCP}
	decide := func(act policy.Action) policy.Effect {
		return policy.Decide(a, act, policy.Object{}, sp).Effect
	}
	add := func(ok bool, ability string) {
		if ok {
			can = append(can, ability)
		} else {
			cannot = append(cannot, ability)
		}
	}
	add(decide(policy.ActionRead) != policy.EffectRefuse, abilityReadBrief)
	switch decide(policy.ActionRemember) {
	case policy.EffectApply:
		can = append(can, abilityKeep)
	case policy.EffectPropose, policy.EffectConfirm:
		can = append(can, abilityPropose)
		cannot = append(cannot, abilityKeep)
	default:
		cannot = append(cannot, abilityPropose)
	}
	add(decide(policy.ActionRequestDecision) == policy.EffectApply, abilityGate)
	add(decide(policy.ActionForget) == policy.EffectApply, abilityForget)
	return can, cannot
}

// v1Abilities is what a grant with `perms` does in a space still on V1:
// what it writes is kept as written, and it asks with V1's decision gates.
func v1Abilities(perms PermissionSet) (can, cannot []string) {
	add := func(ok bool, ability string) {
		if ok {
			can = append(can, ability)
		} else {
			cannot = append(cannot, ability)
		}
	}
	add(perms.Has(PermMemoryRead), abilityReadMemories)
	add(perms.Has(PermMemoryWrite), abilityAdd)
	add(perms.Has(PermMemoryWrite), abilityGate)
	add(perms.Has(PermMemoryDelete), abilityForget)
	return can, cannot
}

// consentSpaceRow is what public.hubs says about a space for consent.
type consentSpaceRow struct {
	kind   policy.SpaceKind
	rules  policy.Rules
	onV2   bool
	people int
}

// describeSpaces adds what the consent page shows to each of the person's
// spaces: its kind, whether it is on the V2 record, its people, kept
// memories and files, the level the agent is connected at and how far a
// person may raise it, and what the agent will and won't be able to do
// there. Best effort: a space it can't read keeps what V1 says of it.
func (h *MCPOAuthHandler) describeSpaces(ctx context.Context, session oauthPendingSession, items []model.HubWithRole, out []oauthConsentSpace) {
	if len(items) == 0 {
		return
	}
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.Hub.ID
	}
	rows := map[string]consentSpaceRow{}
	res, err := h.authH.pool.Query(ctx, `
		SELECT h.id::text, h.space_kind, h.rules, h.v2_enabled_at IS NOT NULL,
		       (SELECT count(*) FROM hub_members m WHERE m.hub_id = h.id)
		  FROM hubs h WHERE h.id = ANY ($1::uuid[])`, ids)
	if err != nil {
		slog.Warn("MCP OAuth: can't describe the person's spaces", "error", err)
		return
	}
	for res.Next() {
		var id string
		var row consentSpaceRow
		var rules []byte
		if err := res.Scan(&id, &row.kind, &rules, &row.onV2, &row.people); err != nil {
			res.Close()
			slog.Warn("MCP OAuth: can't describe the person's spaces", "error", err)
			return
		}
		// Rules that don't parse describe nothing rather than the defaults.
		if json.Unmarshal(rules, &row.rules) == nil {
			rows[id] = row
		}
	}
	res.Close()
	if res.Err() != nil {
		slog.Warn("MCP OAuth: can't describe the person's spaces", "error", res.Err())
		return
	}

	scope := consentScope(session.requestedScope)
	digests := h.spaceDigests(ctx, session.userID, rows)
	grantPerms, _, _ := oauthPermissionsFromScope(scope)
	grantPerms = grantPerms.Intersect(session.requestedPermissions)
	agent := ledger.AgentFromV1(agentNameFromClientName(session.clientName))
	name := displayOAuthClientName(session.clientName)
	for i, item := range items {
		row, ok := rows[item.Hub.ID]
		if !ok {
			continue
		}
		sp := &out[i]
		sp.Kind = string(row.kind)
		sp.OnV2 = row.onV2
		sp.People = row.people
		var can, cannot []string
		if row.onV2 {
			role, _ := policy.RoleFromV1(item.Role)
			if item.Hub.OwnerID == session.userID {
				role = policy.RoleOwner
			}
			space := policy.Space{Name: item.Hub.Name, Kind: row.kind, Rules: row.rules}
			level := consentLevel(agent, role, space, scope)
			sp.Autonomy = string(level)
			sp.Ceiling = string(consentCeiling(role, space, scope))
			can, cannot = v2Abilities(name, role, space, level)
			sp.Memories = nil
			if d, ok := digests[item.Hub.ID]; ok {
				kept := d.Kept
				sp.Memories = &kept
				for _, t := range d.Targets {
					sp.Targets = append(sp.Targets, oauthConsentTarget{Kind: string(t.Kind), Path: t.Path})
				}
			}
		} else {
			can, cannot = v1Abilities(rolePermissionSet(item.Role, &item.Hub).Intersect(grantPerms))
		}
		// The grant is scoped to the one space chosen here.
		if len(items) > 1 {
			cannot = append(cannot, abilityOtherSpaces)
		}
		if can == nil {
			can = []string{}
		}
		if cannot == nil {
			cannot = []string{}
		}
		sp.Can, sp.Cannot = can, cannot
	}
}

// spaceDigests reads kept counts and compile targets for the spaces on the
// V2 record, through the ledger (they are row-level secured). Nil when
// there is no ledger or it can't answer: the page then shows no counts.
func (h *MCPOAuthHandler) spaceDigests(ctx context.Context, userID string, rows map[string]consentSpaceRow) map[string]ledger.SpaceDigest {
	if h.ledger == nil {
		return nil
	}
	var ids []uuid.UUID
	for id, row := range rows {
		if u, err := uuid.Parse(id); err == nil && row.onV2 {
			ids = append(ids, u)
		}
	}
	person, err := uuid.Parse(userID)
	if err != nil || len(ids) == 0 {
		return nil
	}
	scope, err := h.ledger.UserScope(ctx, person)
	if err != nil {
		slog.Warn("MCP OAuth: can't read the person's spaces on V2", "error", err)
		return nil
	}
	digests, err := h.ledger.SpaceDigests(ctx, scope.Narrow(ids...))
	if err != nil {
		slog.Warn("MCP OAuth: can't read the person's spaces on V2", "error", err)
		return nil
	}
	out := make(map[string]ledger.SpaceDigest, len(digests))
	for id, d := range digests {
		out[id.String()] = d
	}
	return out
}

// personName names the person as the web app does: display name, else
// name, else email.
func (h *MCPOAuthHandler) personName(ctx context.Context, userID string) string {
	var name string
	if err := h.authH.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(display_name, ''), NULLIF(name, ''), email)
		  FROM users WHERE id = $1::uuid`, userID).Scan(&name); err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}

// clientHost is the host a Client ID Metadata Document client is served
// from: unlike its name, which it says itself, Memax fetched its metadata
// there. Empty for a registered client.
func clientHost(clientID string) string {
	if !isMetadataClientID(clientID) {
		return ""
	}
	u, err := url.Parse(clientID)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

// consentExpiresIn is how long the request has left, in whole seconds,
// so the page counts down on its own clock.
func consentExpiresIn(at time.Time) int {
	if left := time.Until(at); left > 0 {
		return int(left / time.Second)
	}
	return 0
}

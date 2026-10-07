package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// The V2 consent screen (OAuthConsent, plan 25 §5.15): the web app's
// Ledger page at /oauth/authorize, beside V1's at /oauth/consent. The API
// stays the OAuth authority; the page renders ConsentRequest's answer and
// posts the person's decision back to Consent as a plain form, so the
// browser follows the API's redirect to the client's registered
// redirect_uri and nothing in the page ever builds one.
//
// What the page says an agent will and won't be able to do in a space
// comes from here, decided by policy for the level consent connects the
// agent at (connectAgent), so it can't promise more or less than is true.

// The paths of the two consent pages in the web app.
const (
	consentPathV1 = "/oauth/consent"
	consentPathV2 = "/oauth/authorize"
)

// The consent form's `decision` values besides approve.
const (
	consentDeny   = "deny"
	consentSwitch = "switch" // "Not you?"
)

// Abilities: what an agent connected to a space may (can) or may not
// (cannot) do there, as the consent screen lists them.
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

// consentScope is the scope the V2 consent screen grants: what the client
// asked for, never above memax:propose. Write is a person's to allow later,
// in Agents on the web (policy.DecideConnection).
func consentScope(requested string) string {
	return intersectScopes(requested, ScopeRead+" "+ScopePropose)
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

// describeSpaces adds what the V2 consent screen shows to each of the
// person's spaces: its kind, whether it is on the V2 record, its people,
// kept memories and files, and what the agent will and won't be able to
// do there. Best effort: a space it can't read stays as V1's page has it.
func (h *MCPOAuthHandler) describeSpaces(ctx context.Context, session oauthPendingSession, items []model.HubWithRole, out []oauthConsentHub) {
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
	slug := agentNameFromClientName(session.clientName)
	agent := ledger.AgentFromV1(slug)
	name := displayOAuthClientName(session.clientName)
	for i, item := range items {
		row, ok := rows[item.Hub.ID]
		if !ok {
			continue
		}
		hub := &out[i]
		hub.SpaceKind = string(row.kind)
		hub.OnV2 = row.onV2
		hub.PeopleCount = row.people
		var can, cannot []string
		if row.onV2 {
			role, _ := policy.RoleFromV1(item.Role)
			if item.Hub.OwnerID == session.userID {
				role = policy.RoleOwner
			}
			sp := policy.Space{Name: item.Hub.Name, Kind: row.kind, Rules: row.rules}
			level := consentLevel(agent, role, sp, scope)
			hub.Autonomy = string(level)
			can, cannot = v2Abilities(name, role, sp, level)
			if d, ok := digests[item.Hub.ID]; ok {
				kept := d.Kept
				hub.KeptCount = &kept
				for _, t := range d.Targets {
					hub.Targets = append(hub.Targets, oauthConsentTarget{Kind: string(t.Kind), Path: t.Path})
				}
			}
		} else {
			can, cannot = v1Abilities(rolePermissionSet(item.Role, &item.Hub).Intersect(grantPerms))
		}
		// The grant is scoped to the one space chosen here.
		if len(items) > 1 {
			cannot = append(cannot, abilityOtherSpaces)
		}
		hub.Can, hub.Cannot = can, cannot
	}
}

// consentSpaceRow is what public.hubs says about a space for consent.
type consentSpaceRow struct {
	kind   policy.SpaceKind
	rules  policy.Rules
	onV2   bool
	people int
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

// consentPerson is who the request is signed in as, named as the web app
// names a person: display name, else name, else email.
func (h *MCPOAuthHandler) consentPerson(ctx context.Context, userID string) *oauthConsentPerson {
	var name string
	err := h.authH.pool.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(display_name, ''), NULLIF(name, ''), email)
		  FROM users WHERE id = $1::uuid`, userID).Scan(&name)
	if err != nil || strings.TrimSpace(name) == "" {
		return nil
	}
	return &oauthConsentPerson{Name: strings.TrimSpace(name)}
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

// personOnV2 says whether any of the person's spaces is on the V2 record:
// their consent opens on the Ledger page. An error opens V1's, which
// every person can use.
func (h *MCPOAuthHandler) personOnV2(ctx context.Context, userID string) bool {
	var onV2 bool
	err := h.authH.pool.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM hubs h
		   WHERE h.v2_enabled_at IS NOT NULL
		     AND (h.owner_id = $1::uuid
		          OR EXISTS (SELECT 1 FROM hub_members m WHERE m.hub_id = h.id AND m.user_id = $1::uuid)))`,
		userID).Scan(&onV2)
	if err != nil {
		slog.Warn("MCP OAuth: can't tell whether the person is on V2; opening V1's consent", "error", err)
		return false
	}
	return onV2
}

// consentOrigins are the origins a consent form may be posted from: the
// web app's and the API's own (its HTML consent page), and the MCP alias.
func (h *MCPOAuthHandler) consentOrigins(r *http.Request) []string {
	var out []string
	for _, base := range []string{h.resolveAppBaseURL(r), h.resolveBaseURL(r), h.mcpAlias} {
		if u, err := url.Parse(base); err == nil && u.Scheme != "" && u.Host != "" {
			out = append(out, u.Scheme+"://"+strings.ToLower(u.Host))
		}
	}
	return out
}

// consentFetchProblem says why a consent post can't have come from one of
// Memax's consent pages, or "" when it may have. Browsers say where a
// request came from (Fetch Metadata, and Origin on every cross-origin
// post): the web app's page posts from a sibling origin (memax.app to
// api.memax.app), the API's own page from the same one. A post another
// site started is refused even with a valid consent token, so a page
// elsewhere can't submit someone's consent for them. A request with
// neither header comes from no browser, where cross-site forgery doesn't
// arise; the consent token still guards it.
func consentFetchProblem(r *http.Request, trusted []string) string {
	site := r.Header.Get("Sec-Fetch-Site")
	origin := strings.ToLower(strings.TrimSpace(r.Header.Get("Origin")))
	switch site {
	case "same-origin":
		return ""
	case "same-site", "cross-site", "":
		if origin == "" {
			if site == "" {
				return ""
			}
			return "sec-fetch-site " + site + " without an origin"
		}
		if slices.Contains(trusted, origin) {
			return ""
		}
		return "origin " + origin
	default:
		// "none": a navigation no page started (a bookmark, a typed URL)
		// can't be a form post.
		return "sec-fetch-site " + site
	}
}

// githubAuthorizeURL sends the person to sign in with GitHub for the
// pending request; GitHub's callback brings them back to it (state
// "mcp:<request>"). selectAccount shows GitHub's account picker ("Not
// you?").
func (h *MCPOAuthHandler) githubAuthorizeURL(requestID string, selectAccount bool) string {
	params := url.Values{}
	params.Set("client_id", h.authH.clientID)
	params.Set("redirect_uri", h.authH.redirectURL)
	params.Set("scope", "read:user,user:email,read:org")
	params.Set("state", "mcp:"+requestID)
	if selectAccount {
		params.Set("prompt", "select_account")
	}
	return "https://github.com/login/oauth/authorize?" + params.Encode()
}

// switchAccount is "Not you?": the request forgets who signed in, its
// consent token stops working, and the person signs in again at GitHub's
// account picker, coming back to this same request.
func (h *MCPOAuthHandler) switchAccount(w http.ResponseWriter, r *http.Request, session oauthPendingSession) {
	_, err := h.authH.pool.Exec(r.Context(), `
		UPDATE oauth_authorization_requests SET user_id = NULL, csrf_token = ''
		 WHERE id = $1 AND csrf_token = $2`, session.id, session.csrfToken)
	if err != nil {
		slog.Error("MCP OAuth: can't restart sign-in for the request", "error", err)
		http.Error(w, "Couldn't sign you out of this request. Start again from your agent.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, h.githubAuthorizeURL(session.id, true), http.StatusSeeOther)
}

// consentEnded sends a post from the Ledger page whose request is gone
// (expired, answered, or opened from an old link) back to that page,
// which says so. False when there is no web app to send it to.
func (h *MCPOAuthHandler) consentEnded(w http.ResponseWriter, r *http.Request, expired bool) bool {
	base := h.resolveAppBaseURL(r)
	if base == "" {
		return false
	}
	why := "gone"
	if expired {
		why = "expired"
	}
	http.Redirect(w, r, base+consentPathV2+"?ended="+why, http.StatusSeeOther)
	return true
}

// consentRetry sends a post from the Ledger page that can't be granted as
// sent (no space it may use, a permission it can't grant) back to the page
// with the same request, saying why. False when there is no web app.
func (h *MCPOAuthHandler) consentRetry(w http.ResponseWriter, r *http.Request, session oauthPendingSession, code string) bool {
	u := h.webConsentURL(r, session.id, session.csrfToken, true)
	if u == "" {
		return false
	}
	http.Redirect(w, r, u+"&error="+url.QueryEscape(code), http.StatusSeeOther)
	return true
}

// consentExpiresIn is how long the request has left, in whole seconds,
// so the page counts down on its own clock.
func consentExpiresIn(at time.Time) int {
	if left := time.Until(at); left > 0 {
		return int(left / time.Second)
	}
	return 0
}

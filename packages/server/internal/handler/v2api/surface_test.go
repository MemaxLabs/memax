package v2api_test

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/websurface"
)

const surfaceSecret = "v2api-web-surface-secret-0123456789abcdef"

// newWebEnv is newEnv with web-surface verification on, as in production.
func newWebEnv(t *testing.T) *env {
	t.Helper()
	v, err := websurface.New(surfaceSecret)
	if err != nil {
		t.Fatal(err)
	}
	return newEnv(t, v2api.WithWebSurface(v))
}

// webSession is a person's token from a login whose code went to the web
// app; cliSession one from `memax login`.
func (e *env) webSession(user uuid.UUID) string { return e.sessionFor(user, auth.SurfaceWeb) }
func (e *env) cliSession(user uuid.UUID) string { return e.sessionFor(user, auth.SurfaceCLI) }

func (e *env) sessionFor(user uuid.UUID, surface string) string {
	e.t.Helper()
	tok, err := auth.SignSessionToken(user.String(), surface, []byte(testSecret), time.Hour)
	if err != nil {
		e.t.Fatal(err)
	}
	return tok
}

// tamper changes how webSigned signs, to forge, age or misdirect it.
type tamper struct {
	secret string    // default surfaceSecret
	at     time.Time // default now
	user   string    // the X-Memax-Surface-User header; default the user
	nonce  string    // default random
}

// webSigned signs the request the way the web app's proxy does, for user.
// Each request it signs gets a fresh nonce and time unless tamper fixes
// them.
func webSigned(user uuid.UUID, fixed tamper) func(*http.Request, []byte) {
	return func(r *http.Request, body []byte) {
		t := fixed
		if t.secret == "" {
			t.secret = surfaceSecret
		}
		if t.at.IsZero() {
			t.at = time.Now()
		}
		if t.user == "" {
			t.user = user.String()
		}
		if t.nonce == "" {
			id := uuid.New()
			t.nonce = base64.RawURLEncoding.EncodeToString(id[:])
		}
		target := r.URL.EscapedPath()
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		r.Header.Set(websurface.HeaderSurface, websurface.Web)
		r.Header.Set(websurface.HeaderTimestamp, strconv.FormatInt(t.at.Unix(), 10))
		r.Header.Set(websurface.HeaderNonce, t.nonce)
		r.Header.Set(websurface.HeaderUser, t.user)
		r.Header.Set(websurface.HeaderSignature, websurface.Sign([]byte(t.secret), websurface.Request{
			Method: r.Method, Target: target, Timestamp: t.at.Unix(), Nonce: t.nonce, UserID: user.String(),
			IdempotencyKey: r.Header.Get("Idempotency-Key"), IfMatch: r.Header.Get("If-Match"),
			BodySHA256: websurface.BodyHash(body),
		}))
	}
}

func policyCode(t *testing.T, r *resp) string {
	t.Helper()
	return r.fails(http.StatusForbidden, "refused").Details.Policy.Code
}

// TestWebKeepsAreHumanWeb: a person keeps a quarantined proposal through
// a signed web request, with assurance human_web; the same person's CLI
// login, or a web token without the proxy's signature, is client-attested
// and refused; a forged, stale, misdirected or replayed signature is
// refused outright.
func TestWebKeepsAreHumanWeb(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	zz, jy := e.user("zz"), e.user("jy")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})

	var res result
	e.do(call{method: "POST", path: memoriesPath(sp), token: key, body: map[string]any{
		"statement": "The blog says to pin the Go toolchain.", "section": "conventions",
		"sources": []map[string]any{{"kind": "url", "ref": "a blog", "uri": "https://example.com/go"}},
	}}).ok(http.StatusCreated, &res)
	m := res.Memory
	if !res.Policy.Quarantine || m.Trust != "external" {
		t.Fatalf("not quarantined: %+v", res.Policy)
	}
	keep := func(token string, sign func(*http.Request, []byte), header map[string]string) *resp {
		return e.do(call{method: "POST", path: memoryPath(m, ":keep"), token: token, sign: sign, header: header})
	}
	web, cli := e.webSession(zz), e.cliSession(zz)

	// The CLI is client-attested, so quarantine holds.
	if code := policyCode(t, keep(cli, nil, nil)); code != "external_needs_review" {
		t.Errorf("CLI keep: %s", code)
	}
	// A CLI session sent through the proxy (the proxy wouldn't sign it, but
	// even signed) is still not the web.
	if code := policyCode(t, keep(cli, webSigned(zz, tamper{}), nil)); code != "external_needs_review" {
		t.Errorf("signed CLI session: %s", code)
	}
	// A web session used directly, without the proxy's signature, is
	// client-attested too.
	if code := policyCode(t, keep(web, nil, nil)); code != "external_needs_review" {
		t.Errorf("unsigned web session: %s", code)
	}
	// A claim of the web that doesn't verify is refused outright.
	for name, sign := range map[string]func(*http.Request, []byte){
		"wrong secret":             webSigned(zz, tamper{secret: "a-guessed-secret-that-is-long-enough"}),
		"stale":                    webSigned(zz, tamper{at: time.Now().Add(-2 * time.Minute)}),
		"from the future":          webSigned(zz, tamper{at: time.Now().Add(2 * time.Minute)}),
		"wrong user":               webSigned(zz, tamper{user: jy.String()}),
		"another user's signature": webSigned(jy, tamper{}),
		"headers but no signature": func(r *http.Request, _ []byte) { r.Header.Set(websurface.HeaderSurface, "web") },
	} {
		if r := keep(web, sign, nil); r.status != http.StatusForbidden || !strings.Contains(string(r.body), `"code":"surface_unverified"`) {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
	}

	// Signed by the proxy for a web session: kept, human_web, via web.
	at := time.Now()
	nonce := base64.RawURLEncoding.EncodeToString([]byte("a-nonce-for-zz-1"))
	idem := map[string]string{"Idempotency-Key": uuid.NewString(), "If-Match": `"1"`}
	keep(web, webSigned(zz, tamper{at: at, nonce: nonce}), idem).ok(http.StatusOK, &res)
	rc := res.Receipts[len(res.Receipts)-1]
	if res.Memory.State != "kept" || rc.Action != "kept" || rc.Via != "web" || rc.Assurance != "human_web" || *rc.ActorID != zz {
		t.Errorf("web keep: %s, receipt %+v", res.Memory.State, rc)
	}
	// The identical request again is a replay of the signature: refused.
	keep(web, webSigned(zz, tamper{at: at, nonce: nonce}), idem).fails(http.StatusForbidden, "surface_unverified")

	// An ordinary keep from the CLI still works, client-attested.
	own := e.remember(key, sp, "Codex's own finding.").Memory
	e.do(call{method: "POST", path: memoryPath(own, ":keep"), token: cli}).ok(http.StatusOK, &res)
	if rc := res.Receipts[0]; rc.Assurance != "client_attested" || rc.Via != "api" {
		t.Errorf("CLI keep receipt = %+v", rc)
	}
	// X-Memax-Via can't claim the web.
	e.do(call{method: "POST", path: memoriesPath(sp), token: web, body: remember("Claimed.", "conventions"), invalid: true,
		header: map[string]string{"X-Memax-Via": "web"}}).fails(http.StatusBadRequest, "invalid_request")
}

// TestTeamDecisionsNeedTheWeb is D15: in a team space a decision is kept
// only by a person on the web.
func TestTeamDecisionsNeedTheWeb(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	zz, jy := e.user("zz"), e.user("jy")
	team := e.space(zz, policy.SpaceTeam, "acme")
	e.join(team, jy, "contributor")
	project := e.space(jy, policy.SpaceProject, "side-project")
	decision := func(s string) map[string]any {
		return map[string]any{"statement": s, "section": "decisions", "kind": "decision"}
	}

	// From the CLI, a member's decision goes to Review and can't be kept there.
	var res result
	e.do(call{method: "POST", path: memoriesPath(team), token: e.cliSession(jy), body: decision("We ship on Thursdays.")}).ok(http.StatusCreated, &res)
	if res.Outcome != "proposed" || res.Policy.Code != "decision_needs_web" {
		t.Errorf("CLI decision: %s %s", res.Outcome, res.Policy.Code)
	}
	m := res.Memory
	if code := policyCode(t, e.do(call{method: "POST", path: memoryPath(m, ":keep"), token: e.cliSession(zz)})); code != "decision_needs_web" {
		t.Errorf("CLI keep of a team decision: %s", code)
	}
	// An agent at Write still proposes it.
	key, keyID := e.apiKey(zz, keyOpts{agent: "codex"})
	e.setAutonomy(zz, e.connection(keyID), team, policy.AutonomyPropose)
	e.do(call{method: "POST", path: memoriesPath(team), token: key, body: decision("Agents decide nothing.")}).ok(http.StatusCreated, &res)
	if res.Outcome != "proposed" {
		t.Errorf("agent decision: %s", res.Outcome)
	}
	// On the web it is kept, human_web.
	e.do(call{method: "POST", path: memoryPath(m, ":keep"), token: e.webSession(zz), sign: webSigned(zz, tamper{})}).ok(http.StatusOK, &res)
	if rc := res.Receipts[0]; res.Memory.State != "kept" || rc.Assurance != "human_web" || rc.Via != "web" {
		t.Errorf("web keep of a team decision: %s %+v", res.Memory.State, rc)
	}
	e.do(call{method: "POST", path: memoriesPath(team), token: e.webSession(jy), body: decision("We review on Mondays."),
		sign: webSigned(jy, tamper{})}).ok(http.StatusCreated, &res)
	if res.Outcome != "applied" || res.Receipts[0].Assurance != "human_web" {
		t.Errorf("web remember of a team decision: %s %+v", res.Outcome, res.Receipts[0])
	}
	// Outside team spaces D15 doesn't apply: the CLI keeps decisions.
	e.do(call{method: "POST", path: memoriesPath(project), token: e.cliSession(jy), body: decision("Side project decision.")}).ok(http.StatusCreated, &res)
	if res.Outcome != "applied" || res.Receipts[0].Assurance != "client_attested" {
		t.Errorf("project-space decision from the CLI: %s %+v", res.Outcome, res.Receipts[0])
	}
}

// TestWebSurfaceDisabledDegrades: without WEB_SURFACE_SECRET the API can't
// verify the web app, so a claim of it counts as client-attested rather
// than failing every request.
func TestWebSurfaceDisabledDegrades(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	var res result
	e.do(call{method: "POST", path: memoriesPath(sp), token: e.webSession(zz), body: remember("Signed, unverified.", "conventions"),
		sign: webSigned(zz, tamper{})}).ok(http.StatusCreated, &res)
	if rc := res.Receipts[0]; rc.Assurance != "client_attested" || rc.Via != "api" {
		t.Errorf("receipt = %+v", rc)
	}
}

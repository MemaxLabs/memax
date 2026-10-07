package v2api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/passkeys"
	"github.com/MemaxLabs/memax/packages/server/internal/passkeys/passkeytest"
	"github.com/MemaxLabs/memax/packages/server/internal/sessions"
	"github.com/MemaxLabs/memax/packages/server/internal/websurface"
)

const passkeyOrigin = "https://memax.test"

// pkClock is the passkey service's clock, to age challenges.
type pkClock struct{ off atomic.Int64 }

func (c *pkClock) now() time.Time          { return time.Now().Add(time.Duration(c.off.Load())) }
func (c *pkClock) advance(d time.Duration) { c.off.Add(int64(d)) }

// testAccounts is the real auth handler, except that linking a provider
// answers a stand-in for its sign-in page (no GitHub app in tests).
type testAccounts struct{ *handler.AuthHandler }

func (a testAccounts) AccountLinkURL(_ context.Context, user uuid.UUID, provider, redirect string) (string, error) {
	return "https://" + provider + ".example/authorize?user=" + user.String() + "&redirect=" + redirect, nil
}

// passkeyEnv is the sessions env with passkeys on, as in production.
type passkeyEnv struct {
	*sessionsEnv
	pk    *passkeys.Service
	clock *pkClock
}

func newPasskeyEnv(t *testing.T) *passkeyEnv {
	t.Helper()
	v, err := websurface.New(surfaceSecret)
	if err != nil {
		t.Fatal(err)
	}
	clock := &pkClock{}
	var store *sessions.Store
	var pk *passkeys.Service
	var authH *handler.AuthHandler
	e := newEnvWith(t, func(e *env) []v2api.Option {
		store = sessions.New(e.pool, sessions.WithLogger(quiet))
		if pk, err = passkeys.New(e.pool, passkeys.Config{RPID: "memax.test", RPName: "Memax", Origins: []string{passkeyOrigin}},
			passkeys.WithClock(clock.now), passkeys.WithLogger(quiet)); err != nil {
			t.Fatal(err)
		}
		if authH, err = handler.NewAuthHandler(e.pool); err != nil {
			t.Fatal(err)
		}
		authH.SetStore(e.st)
		authH.SetSessions(store)
		authH.SetPasskeys(pk)
		return []v2api.Option{v2api.WithWebSurface(v), v2api.WithSessions(store), v2api.WithPasskeys(pk), v2api.WithAccounts(testAccounts{authH})}
	})
	t.Cleanup(store.Wait)
	return &passkeyEnv{sessionsEnv: &sessionsEnv{env: e, store: store, authH: authH}, pk: pk, clock: clock}
}

// web signs a person in on the web and returns the token (naming the
// session) and the session.
func (e *passkeyEnv) web(user uuid.UUID) (string, uuid.UUID) {
	tok, is := e.signIn(user, sessions.KindWeb, "Chrome on macOS")
	return tok, is.Session.ID
}

// aged makes a session's sign-in an hour old: no longer fresh.
func (e *passkeyEnv) aged(session uuid.UUID) {
	e.exec(`UPDATE sessions SET created_at = now() - interval '1 hour' WHERE id = $1`, session)
}

func asJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type passkeyWire struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Provider       string     `json:"provider"`
	LastUsedAt     *time.Time `json:"last_used_at"`
	BackupEligible bool       `json:"backup_eligible"`
	Synced         bool       `json:"synced"`
}

// addPasskey registers a passkey made by a, from a fresh web session.
func (e *passkeyEnv) addPasskey(user uuid.UUID, token string, a *passkeytest.Authenticator, name string) passkeyWire {
	e.t.Helper()
	var reg struct {
		Options json.RawMessage `json:"options"`
	}
	e.do(call{method: "POST", path: "/v2/me/passkey-registrations", token: token, sign: webSigned(user, tamper{})}).ok(http.StatusOK, &reg)
	cred, err := a.Create(reg.Options)
	if err != nil {
		e.t.Fatal(err)
	}
	var pk passkeyWire
	e.do(call{method: "POST", path: "/v2/me/passkeys", token: token, sign: webSigned(user, tamper{}),
		body: map[string]any{"credential": json.RawMessage(cred), "name": name}}).ok(http.StatusCreated, &pk)
	return pk
}

// challengeOf reads the challenge a 403 needs_passkey carries.
func challengeOf(t *testing.T, r *resp) json.RawMessage {
	t.Helper()
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Policy struct {
					Code string `json:"code"`
				} `json:"policy"`
				Passkey struct {
					Options   json.RawMessage `json:"options"`
					ExpiresAt time.Time       `json:"expires_at"`
				} `json:"passkey"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil || r.status != http.StatusForbidden || env.Error.Code != "needs_passkey" ||
		env.Error.Details.Policy.Code != "needs_passkey" || len(env.Error.Details.Passkey.Options) == 0 {
		t.Fatalf("want 403 needs_passkey with a challenge, got %d: %s", r.status, r.body)
	}
	return env.Error.Details.Passkey.Options
}

// failure is a 403 passkey_invalid's reason.
func failure(t *testing.T, r *resp, status int) string {
	t.Helper()
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Failure string `json:"passkey_failure"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.body, &env); err != nil || r.status != status || env.Error.Code != "passkey_invalid" {
		t.Fatalf("want %d passkey_invalid, got %d: %s", status, r.status, r.body)
	}
	return env.Error.Details.Failure
}

// recheck sends c from user's web session; it must be refused with a
// challenge, which a answers; the very same request then goes again with
// the answer. It returns that response and the answer.
func (e *passkeyEnv) recheck(c call, user uuid.UUID, a *passkeytest.Authenticator) (*resp, string) {
	e.t.Helper()
	h := map[string]string{"Idempotency-Key": uuid.NewString()}
	for k, v := range c.header {
		h[k] = v
	}
	c.header, c.sign = h, webSigned(user, tamper{})
	options := challengeOf(e.t, e.do(c))
	assertion, err := a.Get(options)
	if err != nil {
		e.t.Fatal(err)
	}
	answer := passkeytest.Encode(assertion)
	h[v2api.HeaderPasskey] = answer
	return e.do(c), answer
}

// TestPasskeyRegistrationAndSignIn: a person adds a passkey from a fresh
// web sign-in, sees it on their account, renames it, and signs in with it
// to a web session; adding one needs the web and a fresh sign-in (or a
// passkey they have), and a passkey Memax doesn't know signs nobody in.
func TestPasskeyRegistrationAndSignIn(t *testing.T) {
	t.Parallel()
	e := newPasskeyEnv(t)
	zz := e.user("zz")
	token, session := e.web(zz)
	laptop := passkeytest.New(passkeyOrigin)

	// Not from the CLI, fresh or not.
	cli, _ := e.signIn(zz, sessions.KindCLI, "memax CLI 0.2.1")
	if code := policyCode(t, e.do(call{method: "POST", path: "/v2/me/passkey-registrations", token: cli})); code != "account_needs_web" {
		t.Errorf("CLI registration: %s", code)
	}

	pk := e.addPasskey(zz, token, laptop, "")
	if pk.Name != "iCloud Keychain" || pk.Provider != "iCloud Keychain" || !pk.BackupEligible || !pk.Synced {
		t.Errorf("passkey = %+v", pk)
	}
	var list struct {
		Items []passkeyWire `json:"items"`
	}
	e.do(call{method: "GET", path: "/v2/me/passkeys", token: token}).ok(http.StatusOK, &list)
	if len(list.Items) != 1 || list.Items[0].ID != pk.ID {
		t.Fatalf("passkeys = %+v", list)
	}
	var renamed passkeyWire
	e.do(call{method: "PATCH", path: "/v2/me/passkeys/" + pk.ID.String(), token: token, sign: webSigned(zz, tamper{}),
		body: map[string]any{"name": "  Work   laptop "}}).ok(http.StatusOK, &renamed)
	if renamed.Name != "Work laptop" {
		t.Errorf("renamed = %+v", renamed)
	}
	e.do(call{method: "PATCH", path: "/v2/me/passkeys/" + uuid.NewString(), token: token, sign: webSigned(zz, tamper{}),
		body: map[string]any{"name": "x"}}).fails(http.StatusNotFound, "not_found")

	var acct struct {
		Email         string `json:"email"`
		Initials      string `json:"initials"`
		PasskeyCheck  bool   `json:"passkey_check"`
		SignInMethods []struct {
			Method    string `json:"method"`
			Connected bool   `json:"connected"`
		} `json:"sign_in_methods"`
		Passkeys []passkeyWire `json:"passkeys"`
		Session  struct {
			ID         uuid.UUID  `json:"id"`
			Surface    string     `json:"surface"`
			FreshUntil *time.Time `json:"fresh_until"`
		} `json:"session"`
	}
	e.do(call{method: "GET", path: "/v2/me/account", token: token}).ok(http.StatusOK, &acct)
	if !acct.PasskeyCheck || len(acct.Passkeys) != 1 || acct.Session.ID != session || acct.Session.Surface != "web" ||
		acct.Session.FreshUntil == nil || acct.Initials != "ZZ" || len(acct.SignInMethods) != 3 {
		t.Errorf("account = %+v", acct)
	}

	// The name receipts show, from anywhere the person is signed in.
	var named struct {
		Name     string `json:"name"`
		Initials string `json:"initials"`
	}
	e.do(call{method: "PATCH", path: "/v2/me/account", token: cli, body: map[string]any{"name": "  Ziyang   Zeng "}}).ok(http.StatusOK, &named)
	if named.Name != "Ziyang Zeng" || named.Initials != "ZZ" {
		t.Errorf("renamed = %+v", named)
	}
	e.do(call{method: "PATCH", path: "/v2/me/account", token: cli, body: map[string]any{"name": " "}}).fails(http.StatusBadRequest, "invalid_request")
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	if code := policyCode(t, e.do(call{method: "GET", path: "/v2/me/account", token: key})); code != "account_by_person" {
		t.Errorf("an API key reading the account: %s", code)
	}

	// A second passkey on an old session asks for the first.
	e.aged(session)
	var aged struct {
		Session struct {
			FreshUntil *time.Time `json:"fresh_until"`
		} `json:"session"`
	}
	e.do(call{method: "GET", path: "/v2/me/account", token: token}).ok(http.StatusOK, &aged)
	if aged.Session.FreshUntil != nil {
		t.Errorf("an hour-old sign-in is still fresh until %v", aged.Session.FreshUntil)
	}
	phone := passkeytest.New(passkeyOrigin)
	phone.AAGUID = uuid.MustParse("ea9b8d66-4d01-1d21-3ce4-b6b48cb575d4")
	r, _ := e.recheck(call{method: "POST", path: "/v2/me/passkey-registrations", token: token}, zz, laptop)
	var reg struct {
		Options json.RawMessage `json:"options"`
	}
	r.ok(http.StatusOK, &reg)
	cred, err := phone.Create(reg.Options)
	if err != nil {
		t.Fatal(err)
	}
	var second passkeyWire
	e.do(call{method: "POST", path: "/v2/me/passkeys", token: token, sign: webSigned(zz, tamper{}),
		body: map[string]any{"credential": json.RawMessage(cred)}}).ok(http.StatusCreated, &second)
	if second.Name != "Google Password Manager" {
		t.Errorf("second = %+v", second)
	}

	// Someone without a passkey, on an old session, signs in again first.
	jy := e.user("jy")
	jyTok, jySession := e.web(jy)
	e.aged(jySession)
	if code := policyCode(t, e.do(call{method: "POST", path: "/v2/me/passkey-registrations", token: jyTok, sign: webSigned(jy, tamper{})})); code != "needs_sign_in" {
		t.Errorf("old session, no passkey: %s", code)
	}

	// Sign in with the passkey: the public begin and finish, then the
	// code is a web session.
	var begin struct {
		Options json.RawMessage `json:"options"`
	}
	e.do(call{method: "POST", path: "/v2/passkey-sign-ins"}).ok(http.StatusOK, &begin)
	assertion, err := laptop.Get(begin.Options)
	if err != nil {
		t.Fatal(err)
	}
	var signed struct {
		Code      string `json:"code"`
		ExpiresIn int    `json:"expires_in"`
	}
	e.do(call{method: "POST", path: "/v2/passkey-sign-ins:finish", body: map[string]any{"credential": json.RawMessage(assertion)}}).
		ok(http.StatusOK, &signed)
	if signed.Code == "" || signed.ExpiresIn != 60 {
		t.Fatalf("signed in = %+v", signed)
	}
	if got := e.count(`SELECT count(*) FROM auth_codes WHERE code = $1 AND user_id = $2 AND surface = 'web'`, signed.Code, zz); got != 1 {
		t.Fatalf("the code isn't a web sign-in for zz")
	}
	ex := httptest.NewRecorder()
	e.authH.ExchangeCode(ex, httptest.NewRequest("POST", "/v1/auth/exchange", strings.NewReader(`{"code":"`+signed.Code+`"}`)))
	if ex.Code != http.StatusOK || e.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND kind = 'web' AND revoked_at IS NULL`, zz) != 2 {
		t.Errorf("exchange: %d %s", ex.Code, ex.Body)
	}
	// The same answer once more is refused: each challenge works once.
	if f := failure(t, e.do(call{method: "POST", path: "/v2/passkey-sign-ins:finish", body: map[string]any{"credential": json.RawMessage(assertion)}}),
		http.StatusUnauthorized); f != "used" {
		t.Errorf("replayed sign-in: %s", f)
	}
	// A passkey Memax doesn't know.
	stranger := passkeytest.New(passkeyOrigin)
	if _, err := stranger.Create(reg.Options); err != nil {
		t.Fatal(err)
	}
	e.do(call{method: "POST", path: "/v2/passkey-sign-ins"}).ok(http.StatusOK, &begin)
	unknown, err := stranger.Get(begin.Options)
	if err != nil {
		t.Fatal(err)
	}
	if f := failure(t, e.do(call{method: "POST", path: "/v2/passkey-sign-ins:finish", body: map[string]any{"credential": json.RawMessage(unknown)}}),
		http.StatusUnauthorized); f != "no_credential" {
		t.Errorf("unknown passkey: %s", f)
	}
}

// TestPasskeyReCheckGatesDecisions: with a passkey, each decision that
// needs a person asks for it on the web, goes through with a fresh answer
// bound to it, and records human_web_verified; an answer can't be used
// again, for another request, from another session or after it expired;
// the CLI is refused and never verified.
func TestPasskeyReCheckGatesDecisions(t *testing.T) {
	t.Parallel()
	e := newPasskeyEnv(t)
	zz := e.user("zz")
	token, _ := e.web(zz)
	laptop := passkeytest.New(passkeyOrigin)
	e.addPasskey(zz, token, laptop, "Laptop")
	project := e.space(zz, policy.SpaceProject, "memax-v2")
	team := e.space(zz, policy.SpaceTeam, "acme")
	side := e.space(zz, policy.SpaceProject, "side")
	keyTok, keyID := e.apiKey(zz, keyOpts{agent: "codex"})
	codex := e.connection(keyID)
	e.setAutonomy(zz, codex, team, policy.AutonomyPropose)
	cli, _ := e.signIn(zz, sessions.KindCLI, "memax CLI 0.2.1")

	propose := func(sp space, body map[string]any) memory {
		var res result
		e.do(call{method: "POST", path: memoriesPath(sp), token: keyTok, body: body}).ok(http.StatusCreated, &res)
		return res.Memory
	}
	quarantined := func() memory {
		return propose(project, map[string]any{"statement": "A blog says to pin the toolchain " + uuid.NewString()[:6] + ".", "section": "conventions",
			"sources": []map[string]any{{"kind": "url", "ref": "a blog", "uri": "https://example.com/go"}}})
	}
	decision := func() memory {
		return propose(team, map[string]any{"statement": "We ship on Thursdays " + uuid.NewString()[:6] + ".", "section": "decisions", "kind": "decision"})
	}
	kept := func() memory { return e.remember(token, project, "Kept to forget "+uuid.NewString()[:6]+".").Memory }
	gateRef := func() gate {
		var asked gateResult
		e.do(call{method: "POST", path: gatesPath(team), token: keyTok, body: deployGate()}).ok(http.StatusCreated, &asked)
		return asked.Gate
	}

	type gated struct {
		name string
		call func() call
		// assurance reads the receipt's assurance from the response.
		assurance func(t *testing.T, r *resp) string
	}
	lastReceipt := func(t *testing.T, r *resp) string {
		var res struct {
			Receipts []receipt `json:"receipts"`
		}
		r.ok(http.StatusOK, &res)
		return res.Receipts[len(res.Receipts)-1].Assurance
	}
	cases := []gated{
		{"keep a quarantined proposal", func() call {
			return call{method: "POST", path: memoryPath(quarantined(), ":keep"), token: token}
		}, lastReceipt},
		{"keep a team decision (D15)", func() call {
			return call{method: "POST", path: memoryPath(decision(), ":keep"), token: token}
		}, lastReceipt},
		{"answer a team gate (D15)", func() call {
			g := gateRef()
			return call{method: "POST", path: gatePath(team, g, ":answer"), token: token, body: map[string]any{"option": 1}}
		}, func(t *testing.T, r *resp) string {
			var res gateResult
			r.ok(http.StatusOK, &res)
			if res.Gate.Answer == nil {
				t.Fatal("no answer")
			}
			return res.Gate.Answer.Assurance
		}},
		{"raise an agent", func() call {
			e.setAutonomy(zz, codex, side, policy.AutonomyRead)
			return call{method: "PATCH", path: agentPath(codex, "/spaces/"+side.slug), token: token,
				body: map[string]any{"autonomy": "propose"}}
		}, lastReceipt},
		{"forget a memory", func() call {
			m := kept()
			return call{method: "POST", path: memoryPath(m, ":forget"), token: token, header: map[string]string{"If-Match": `"1"`}}
		}, func(t *testing.T, r *resp) string {
			var res struct {
				Receipts []receipt `json:"receipts"`
			}
			r.ok(http.StatusOK, &res)
			for _, rc := range res.Receipts {
				if rc.Action == "forgot" {
					return rc.Assurance
				}
			}
			t.Fatalf("no forgot receipt: %+v", res.Receipts)
			return ""
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, answer := e.recheck(c.call(), zz, laptop)
			if got := c.assurance(t, r); got != "human_web_verified" {
				t.Errorf("assurance %q, want human_web_verified", got)
			}
			// The answer is spent, and bound: another request with it fails.
			next := c.call()
			h := map[string]string{v2api.HeaderPasskey: answer}
			for k, v := range next.header {
				h[k] = v
			}
			next.sign, next.header = webSigned(zz, tamper{}), h
			if f := failure(t, e.do(next), http.StatusForbidden); f != "used" {
				t.Errorf("reused answer: %s", f)
			}
		})
	}

	t.Run("another request, session or person can't use an answer", func(t *testing.T) {
		m := quarantined()
		keep := call{method: "POST", path: memoryPath(m, ":keep"), token: token, sign: webSigned(zz, tamper{}),
			header: map[string]string{"Idempotency-Key": uuid.NewString()}}
		options := challengeOf(t, e.do(keep))
		assertion, err := laptop.Get(options)
		if err != nil {
			t.Fatal(err)
		}
		answer := passkeytest.Encode(assertion)
		// A different Idempotency-Key is a different request.
		other := keep
		other.header = map[string]string{"Idempotency-Key": uuid.NewString(), v2api.HeaderPasskey: answer}
		if f := failure(t, e.do(other), http.StatusForbidden); f != "other_request" {
			t.Errorf("another request: %s", f)
		}
		// The same request from another of zz's web sessions.
		token2, _ := e.web(zz)
		fromOther := keep
		fromOther.token = token2
		fromOther.header = map[string]string{"Idempotency-Key": keep.header["Idempotency-Key"], v2api.HeaderPasskey: answer}
		if f := failure(t, e.do(fromOther), http.StatusForbidden); f != "other_session" {
			t.Errorf("another session: %s", f)
		}
		// From the CLI, an answer counts for nothing.
		e.do(call{method: "POST", path: memoryPath(m, ":keep"), token: cli,
			header: map[string]string{v2api.HeaderPasskey: answer}}).fails(http.StatusForbidden, "permission_denied")
		// Too late.
		e.clock.advance(passkeys.ChallengeTTL + time.Second)
		late := keep
		late.header = map[string]string{"Idempotency-Key": keep.header["Idempotency-Key"], v2api.HeaderPasskey: answer}
		failed := failure(t, e.do(late), http.StatusForbidden)
		e.clock.advance(-(passkeys.ChallengeTTL + time.Second))
		if failed != "expired" {
			t.Errorf("expired answer: %s", failed)
		}
		// Unreadable.
		bad := keep
		bad.header = map[string]string{"Idempotency-Key": uuid.NewString(), v2api.HeaderPasskey: "bm90LWEtcGFzc2tleS1hbnN3ZXI"}
		if f := failure(t, e.do(bad), http.StatusForbidden); f != "malformed" {
			t.Errorf("malformed answer: %s", f)
		}
	})

	t.Run("an authenticator that didn't verify the person", func(t *testing.T) {
		m := quarantined()
		keep := call{method: "POST", path: memoryPath(m, ":keep"), token: token, sign: webSigned(zz, tamper{}),
			header: map[string]string{"Idempotency-Key": uuid.NewString()}}
		options := challengeOf(t, e.do(keep))
		presenceOnly := *laptop
		presenceOnly.SkipUV = true
		assertion, err := presenceOnly.Get(options)
		if err != nil {
			t.Fatal(err)
		}
		keep.header[v2api.HeaderPasskey] = passkeytest.Encode(assertion)
		if f := failure(t, e.do(keep), http.StatusForbidden); f != "not_verified" {
			t.Errorf("no UV: %s", f)
		}
	})

	t.Run("the CLI", func(t *testing.T) {
		// Today's rule for what needs the web; and with a passkey, Forget
		// waits for the web too.
		if code := policyCode(t, e.do(call{method: "POST", path: memoryPath(quarantined(), ":keep"), token: cli})); code != "external_needs_review" {
			t.Errorf("CLI quarantined keep: %s", code)
		}
		if code := policyCode(t, e.do(call{method: "POST", path: memoryPath(kept(), ":forget"), token: cli,
			header: map[string]string{"If-Match": `"1"`}})); code != "needs_passkey" {
			t.Errorf("CLI forget: %s", code)
		}
	})

	t.Run("a keep that needs nothing more", func(t *testing.T) {
		var res result
		m := propose(project, remember("Plain proposal "+uuid.NewString()[:6]+".", "conventions"))
		e.do(call{method: "POST", path: memoryPath(m, ":keep"), token: token, sign: webSigned(zz, tamper{})}).ok(http.StatusOK, &res)
		if rc := res.Receipts[len(res.Receipts)-1]; rc.Assurance != "human_web" {
			t.Errorf("plain keep: %+v", rc)
		}
	})
}

// TestWithoutAPasskeyTodaysRuleStands: a person with no passkey keeps a
// quarantined proposal on the web at human_web, told a passkey would make
// it stronger; the CLI is refused as before; Forget from the CLI works.
func TestWithoutAPasskeyTodaysRuleStands(t *testing.T) {
	t.Parallel()
	e := newPasskeyEnv(t)
	zz := e.user("zz")
	token, _ := e.web(zz)
	project := e.space(zz, policy.SpaceProject, "memax-v2")
	keyTok, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	var res struct {
		Policy struct {
			Code    string `json:"code"`
			Suggest string `json:"suggest"`
		} `json:"policy"`
		Memory   memory    `json:"memory"`
		Receipts []receipt `json:"receipts"`
	}
	e.do(call{method: "POST", path: memoriesPath(project), token: keyTok, body: map[string]any{
		"statement": "The blog says to pin the Go toolchain.", "section": "conventions",
		"sources": []map[string]any{{"kind": "url", "ref": "a blog", "uri": "https://example.com/go"}},
	}}).ok(http.StatusCreated, &res)
	m := res.Memory
	cli, _ := e.signIn(zz, sessions.KindCLI, "memax CLI 0.2.1")
	if code := policyCode(t, e.do(call{method: "POST", path: memoryPath(m, ":keep"), token: cli})); code != "external_needs_review" {
		t.Errorf("CLI keep: %s", code)
	}
	e.do(call{method: "POST", path: memoryPath(m, ":keep"), token: token, sign: webSigned(zz, tamper{})}).ok(http.StatusOK, &res)
	if rc := res.Receipts[len(res.Receipts)-1]; rc.Assurance != "human_web" || res.Policy.Suggest != "passkey" {
		t.Errorf("web keep: %+v suggest=%q", rc, res.Policy.Suggest)
	}
	// An answer with no passkey on the account checks nothing.
	e.do(call{method: "POST", path: memoriesPath(project), token: token, sign: webSigned(zz, tamper{}),
		body: remember("Fine without a passkey.", "conventions"),
		header: map[string]string{v2api.HeaderPasskey: "bm90LWEtcGFzc2tleS1hbnN3ZXI"}}).fails(http.StatusForbidden, "passkey_invalid")
	own := e.remember(cli, project, "From the CLI.").Memory
	e.do(call{method: "POST", path: memoryPath(own, ":forget"), token: cli, header: map[string]string{"If-Match": `"1"`}}).ok(http.StatusOK, nil)

	var sec struct {
		Assurance    string `json:"assurance"`
		PasskeyCheck bool   `json:"passkey_check"`
	}
	e.do(call{method: "GET", path: "/v2/security", token: token, sign: webSigned(zz, tamper{})}).ok(http.StatusOK, &sec)
	if sec.Assurance != "human_web" || sec.PasskeyCheck {
		t.Errorf("security = %+v", sec)
	}
}

// TestRemovePasskeyAndSignInMethods: removing a passkey, and unlinking a
// provider, ask for the passkey; connecting one needs a fresh sign-in;
// the last way in can't go; V1's link refuses a passkey holder.
func TestRemovePasskeyAndSignInMethods(t *testing.T) {
	t.Parallel()
	e := newPasskeyEnv(t)
	zz := e.user("zz")
	token, session := e.web(zz)
	e.exec(`INSERT INTO auth_identities (user_id, provider, provider_id, provider_email) VALUES ($1, 'github', '1', 'zz@github.test'), ($1, 'google', '2', 'zz@gmail.test')`, zz)

	var redirect struct {
		URL string `json:"url"`
	}
	connect := call{method: "POST", path: "/v2/me/sign-in-methods/google:connect", token: token, sign: webSigned(zz, tamper{}),
		body: map[string]any{"redirect_uri": "http://localhost:3100/settings/account"}}
	e.do(connect).ok(http.StatusOK, &redirect)
	if !strings.HasPrefix(redirect.URL, "https://google.example/") {
		t.Errorf("redirect = %q", redirect.URL)
	}
	e.do(call{method: "POST", path: "/v2/me/sign-in-methods/github:connect", token: token, sign: webSigned(zz, tamper{}),
		body: map[string]any{"redirect_uri": "https://evil.example/steal"}}).fails(http.StatusBadRequest, "invalid_request")

	laptop := passkeytest.New(passkeyOrigin)
	pk := e.addPasskey(zz, token, laptop, "Laptop")
	e.aged(session)
	// Connecting on an old session now asks for the passkey.
	challengeOf(t, e.do(connect))

	var acct struct {
		SignInMethods []struct {
			Method    string `json:"method"`
			Connected bool   `json:"connected"`
		} `json:"sign_in_methods"`
		PasskeyCheck bool `json:"passkey_check"`
	}
	r, _ := e.recheck(call{method: "POST", path: "/v2/me/sign-in-methods/google:disconnect", token: token}, zz, laptop)
	r.ok(http.StatusOK, &acct)
	if acct.SignInMethods[1].Method != "google" || acct.SignInMethods[1].Connected {
		t.Errorf("google still connected: %+v", acct.SignInMethods)
	}
	r, _ = e.recheck(call{method: "POST", path: "/v2/me/sign-in-methods/github:disconnect", token: token}, zz, laptop)
	r.fails(http.StatusConflict, "invalid_transition")
	r, _ = e.recheck(call{method: "POST", path: "/v2/me/sign-in-methods/google:disconnect", token: token}, zz, laptop)
	r.fails(http.StatusNotFound, "not_found")

	// V1's link can't ask for the passkey, so it refuses.
	v1 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/auth/link/google?redirect_uri=http://localhost:3100/x", nil)
	e.authH.LinkProvider(v1, handler.InjectUserIDForTest(req, zz.String()))
	if v1.Code != http.StatusForbidden || !strings.Contains(v1.Body.String(), "needs_passkey") {
		t.Errorf("V1 link: %d %s", v1.Code, v1.Body)
	}

	// Removing the passkey asks for it; then the check is off.
	e.do(call{method: "POST", path: "/v2/me/passkeys/" + pk.ID.String() + ":remove", token: cli(e, zz)}).fails(http.StatusForbidden, "refused")
	r, _ = e.recheck(call{method: "POST", path: "/v2/me/passkeys/" + pk.ID.String() + ":remove", token: token}, zz, laptop)
	r.ok(http.StatusOK, nil)
	e.do(call{method: "GET", path: "/v2/me/account", token: token}).ok(http.StatusOK, &acct)
	if acct.PasskeyCheck {
		t.Error("the check is still on with no passkey")
	}
	e.do(call{method: "POST", path: "/v2/me/passkeys/" + pk.ID.String() + ":remove", token: token, sign: webSigned(zz, tamper{})}).
		fails(http.StatusNotFound, "not_found")
}

func cli(e *passkeyEnv, user uuid.UUID) string {
	tok, _ := e.signIn(user, sessions.KindCLI, "memax CLI 0.2.1")
	return tok
}

// TestForgetAccount: the person types their email; with a passkey it asks
// for it; their personal and project spaces are forgotten (the forgot
// receipts human_web_verified), their agents disconnected, passkeys
// removed and every session signed out; team spaces stay.
func TestForgetAccount(t *testing.T) {
	t.Parallel()
	e := newPasskeyEnv(t)
	zz, jy := e.user("zz"), e.user("jy")
	token, session := e.web(zz)
	personal := e.space(zz, policy.SpacePersonal, "Personal")
	project := e.space(zz, policy.SpaceProject, "memax-v2")
	team := e.space(zz, policy.SpaceTeam, "acme")
	e.join(team, jy, "contributor")
	e.remember(token, personal, "My own preference.")
	e.remember(token, project, "A project convention.")
	e.remember(token, team, "A team convention.")
	e.apiKey(zz, keyOpts{agent: "codex"})
	cliTok := cli(e, zz)
	laptop := passkeytest.New(passkeyOrigin)
	e.addPasskey(zz, token, laptop, "Laptop")
	var email string
	if err := e.pool.QueryRow(context.Background(), `SELECT email FROM users WHERE id = $1`, zz).Scan(&email); err != nil {
		t.Fatal(err)
	}

	forget := func(confirm string) call {
		return call{method: "POST", path: "/v2/me/account:forget", token: token, body: map[string]any{"confirm": confirm}}
	}
	e.do(call{method: "POST", path: "/v2/me/account:forget", token: token, sign: webSigned(zz, tamper{}),
		body: map[string]any{"confirm": "someone@else.test"}}).fails(http.StatusBadRequest, "invalid_request")
	if code := policyCode(t, e.do(call{method: "POST", path: "/v2/me/account:forget", token: cliTok,
		body: map[string]any{"confirm": email}})); code != "account_needs_web" {
		t.Errorf("CLI forget account: %s", code)
	}
	r, _ := e.recheck(forget(strings.ToUpper(email)), zz, laptop)
	var out struct {
		Spaces, Agents, TeamSpacesKept, Sessions, Passkeys int
	}
	var raw map[string]int
	r.ok(http.StatusOK, &raw)
	out.Spaces, out.Agents, out.TeamSpacesKept, out.Sessions, out.Passkeys = raw["spaces"], raw["agents"], raw["team_spaces_kept"], raw["sessions"], raw["passkeys"]
	if out.Spaces != 2 || out.Agents != 1 || out.TeamSpacesKept != 1 || out.Sessions != 2 || out.Passkeys != 1 {
		t.Errorf("forgot = %+v", out)
	}
	if n := e.count(`SELECT count(*) FROM v2.receipts WHERE action = 'forgot' AND object_kind = 'space' AND assurance = 'human_web_verified' AND actor_id = $1`, zz); n != 2 {
		t.Errorf("%d verified space forgets", n)
	}
	if n := e.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, zz); n != 0 {
		t.Errorf("%d sessions still live", n)
	}
	if n := e.count(`SELECT count(*) FROM v2.memory_versions v JOIN v2.memories m ON m.id = v.memory_id WHERE m.space_id = $1 AND v.statement IS NOT NULL`, team.id); n != 1 {
		t.Errorf("the team space's record went too (%d)", n)
	}
	_ = session
}

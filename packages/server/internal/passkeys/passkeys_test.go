package passkeys_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/passkeys"
	"github.com/MemaxLabs/memax/packages/server/internal/passkeys/passkeytest"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

const origin = "https://memax.test"

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type clock struct{ t atomic.Int64 }

func (c *clock) now() time.Time          { return time.Unix(0, c.t.Load()).UTC() }
func (c *clock) advance(d time.Duration) { c.t.Add(int64(d)) }

type fixture struct {
	t     *testing.T
	pool  *pgxpool.Pool
	clock *clock
	svc   *passkeys.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	_, pool := testdb.Acquire(t)
	f := &fixture{t: t, pool: pool, clock: &clock{}}
	f.clock.t.Store(time.Now().UTC().Truncate(time.Microsecond).UnixNano())
	svc, err := passkeys.New(pool, passkeys.Config{RPID: "memax.test", RPName: "Memax", Origins: []string{origin}},
		passkeys.WithClock(f.clock.now), passkeys.WithLogger(quiet))
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

func (f *fixture) person(name string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		id, name+"-"+id.String()[:8]+"@passkeys.test", name); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func js(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// register adds a passkey made by a for person, from session.
func (f *fixture) register(a *passkeytest.Authenticator, person, session uuid.UUID, name string) *passkeys.Passkey {
	f.t.Helper()
	ctx := context.Background()
	opts, _, err := f.svc.BeginRegistration(ctx, person, session)
	if err != nil {
		f.t.Fatal(err)
	}
	resp, err := a.Create(js(f.t, opts))
	if err != nil {
		f.t.Fatal(err)
	}
	pk, err := f.svc.FinishRegistration(ctx, person, session, resp, name)
	if err != nil {
		f.t.Fatalf("finish registration: %v", err)
	}
	return pk
}

func action(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func wantReason(t *testing.T, err error, want passkeys.Reason) {
	t.Helper()
	if got := passkeys.ReasonOf(err); got != want {
		t.Fatalf("reason %q (%v), want %q", got, err, want)
	}
}

// TestRegisterAndSignIn: a person adds a passkey and signs in with it; the
// options are discoverable, user-verified and attestation-free; a second
// passkey from the same authenticator is excluded; the name defaults to the
// provider's.
func TestRegisterAndSignIn(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, session := f.person("zz"), uuid.New()

	opts, expires, err := f.svc.BeginRegistration(ctx, zz, session)
	if err != nil {
		t.Fatal(err)
	}
	if opts.RP.ID != "memax.test" || opts.AuthenticatorSelection.ResidentKey != "required" || !opts.AuthenticatorSelection.RequireResidentKey ||
		opts.AuthenticatorSelection.UserVerification != "required" || opts.Attestation != "none" || len(opts.ExcludeCredentials) != 0 {
		t.Fatalf("creation options: %+v", opts)
	}
	if !strings.Contains(opts.User.Name, "@passkeys.test") || opts.User.DisplayName != "zz" {
		t.Errorf("the user entity: %+v", opts.User)
	}
	if d := expires.Sub(f.clock.now()); d != passkeys.ChallengeTTL {
		t.Errorf("expires in %s", d)
	}
	a := passkeytest.New(origin)
	resp, err := a.Create(js(t, opts))
	if err != nil {
		t.Fatal(err)
	}
	pk, err := f.svc.FinishRegistration(ctx, zz, session, resp, "")
	if err != nil {
		t.Fatal(err)
	}
	if pk.Name != "iCloud Keychain" || pk.Provider != "iCloud Keychain" || !pk.BackupEligible || !pk.BackedUp || pk.LastUsedAt != nil {
		t.Errorf("the passkey: %+v", pk)
	}
	if has, err := f.svc.Has(ctx, zz); err != nil || !has {
		t.Fatalf("has: %v %v", has, err)
	}

	// The same authenticator can't add it twice: it is excluded.
	again, _, err := f.svc.BeginRegistration(ctx, zz, session)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.ExcludeCredentials) != 1 {
		t.Fatalf("exclude: %+v", again.ExcludeCredentials)
	}
	if _, err := a.Create(js(t, again)); !errors.Is(err, passkeytest.ErrExcluded) {
		t.Fatalf("create again: %v", err)
	}

	// Sign in, naming nobody.
	req, _, err := f.svc.BeginSignIn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if req.UserVerification != "required" || len(req.AllowCredentials) != 0 || req.RPID != "memax.test" {
		t.Fatalf("request options: %+v", req)
	}
	assertion, err := a.Get(js(t, req))
	if err != nil {
		t.Fatal(err)
	}
	who, used, err := f.svc.FinishSignIn(ctx, assertion)
	if err != nil || who != zz || used.ID != pk.ID || used.LastUsedAt == nil {
		t.Fatalf("sign in: %v %v %+v", who, err, used)
	}
	// Once only.
	_, _, err = f.svc.FinishSignIn(ctx, assertion)
	wantReason(t, err, passkeys.ReasonUsed)
}

// TestCeremoniesRefuse: each way an answer can be wrong is refused with
// its reason, and a refused answer doesn't use up its challenge.
func TestCeremoniesRefuse(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy := f.person("zz"), f.person("jy")
	session := uuid.New()
	a := passkeytest.New(origin)
	f.register(a, zz, session, "Laptop")

	t.Run("registration", func(t *testing.T) {
		cases := []struct {
			name    string
			mutate  func(*passkeytest.Authenticator)
			person  uuid.UUID
			session uuid.UUID
			advance time.Duration
			want    passkeys.Reason
		}{
			{"from another session", nil, zz, uuid.New(), 0, passkeys.ReasonOtherSession},
			{"by another person", nil, jy, session, 0, passkeys.ReasonUnknown},
			{"without user verification", func(a *passkeytest.Authenticator) { a.SkipUV = true }, zz, session, 0, passkeys.ReasonNotVerified},
			{"on another origin", func(a *passkeytest.Authenticator) { a.Origin = "https://evil.test" }, zz, session, 0, passkeys.ReasonInvalid},
			{"for another RP ID", func(a *passkeytest.Authenticator) { a.RPID = "evil.test" }, zz, session, 0, passkeys.ReasonInvalid},
			{"after it expired", nil, zz, session, passkeys.ChallengeTTL, passkeys.ReasonExpired},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				opts, _, err := f.svc.BeginRegistration(ctx, zz, session)
				if err != nil {
					t.Fatal(err)
				}
				b := passkeytest.New(origin)
				if c.mutate != nil {
					c.mutate(b)
				}
				resp, err := b.Create(js(t, opts))
				if err != nil {
					t.Fatal(err)
				}
				f.clock.advance(c.advance)
				defer f.clock.advance(-c.advance)
				_, err = f.svc.FinishRegistration(ctx, c.person, c.session, resp, "x")
				wantReason(t, err, c.want)
			})
		}
		if _, err := f.svc.FinishRegistration(ctx, zz, session, []byte(`{"id":"nope"}`), ""); passkeys.ReasonOf(err) != passkeys.ReasonMalformed {
			t.Errorf("malformed: %v", err)
		}
	})

	t.Run("sign-in", func(t *testing.T) {
		cases := []struct {
			name    string
			mutate  func(*passkeytest.Authenticator)
			advance time.Duration
			want    passkeys.Reason
		}{
			{"without user verification", func(a *passkeytest.Authenticator) { a.SkipUV = true }, 0, passkeys.ReasonNotVerified},
			{"on another origin", func(a *passkeytest.Authenticator) { a.Origin = "https://memax.test.evil.test" }, 0, passkeys.ReasonInvalid},
			{"after it expired", nil, passkeys.ChallengeTTL + time.Second, passkeys.ReasonExpired},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				req, _, err := f.svc.BeginSignIn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				b := *a
				if c.mutate != nil {
					c.mutate(&b)
				}
				assertion, err := b.Get(js(t, req))
				if err != nil {
					t.Fatal(err)
				}
				f.clock.advance(c.advance)
				defer f.clock.advance(-c.advance)
				_, _, err = f.svc.FinishSignIn(ctx, assertion)
				wantReason(t, err, c.want)
				if c.advance == 0 {
					// A refused answer leaves the challenge for the real one.
					good, err := a.Get(js(t, req))
					if err != nil {
						t.Fatal(err)
					}
					if who, _, err := f.svc.FinishSignIn(ctx, good); err != nil || who != zz {
						t.Fatalf("the real answer: %v %v", who, err)
					}
				}
			})
		}
		// A passkey Memax doesn't know.
		stranger := passkeytest.New(origin)
		opts, _, err := f.svc.BeginRegistration(ctx, jy, session)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stranger.Create(js(t, opts)); err != nil { // never finished: not registered
			t.Fatal(err)
		}
		req, _, err := f.svc.BeginSignIn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		assertion, err := stranger.Get(js(t, req))
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = f.svc.FinishSignIn(ctx, assertion)
		wantReason(t, err, passkeys.ReasonNoCredential)
	})

	t.Run("a cloned key", func(t *testing.T) {
		hw := passkeytest.New(origin)
		hw.Counting = true
		f.register(hw, jy, session, "Security key")
		sign := func() []byte {
			req, _, err := f.svc.BeginSignIn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			out, err := hw.Get(js(t, req))
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
		if _, _, err := f.svc.FinishSignIn(ctx, sign()); err != nil {
			t.Fatal(err)
		}
		// Its copy signs with a count that went backwards.
		hw.Credentials()[0].Count = 0
		_, _, err := f.svc.FinishSignIn(ctx, sign())
		wantReason(t, err, passkeys.ReasonCloned)
	})
}

// TestCheckIsBoundToPersonSessionAndRequest: a re-check's assertion
// verifies once, for the request, the session and the person it was issued
// to, and for nothing else.
func TestCheckIsBoundToPersonSessionAndRequest(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy := f.person("zz"), f.person("jy")
	session := uuid.New()
	a := passkeytest.New(origin)
	pk := f.register(a, zz, session, "Laptop")
	f.register(passkeytest.New(origin), jy, uuid.New(), "Phone")

	keep := passkeys.Binding{Person: zz, Session: session, Action: action("POST /v2/memories/M-0219:keep")}
	issue := func(b passkeys.Binding) []byte {
		t.Helper()
		opts, _, err := f.svc.BeginCheck(ctx, b)
		if err != nil {
			t.Fatal(err)
		}
		if len(opts.AllowCredentials) != 1 || opts.UserVerification != "required" {
			t.Fatalf("check options: %+v", opts)
		}
		out, err := a.Get(js(t, opts))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	cases := []struct {
		name    string
		present passkeys.Binding
		advance time.Duration
		want    passkeys.Reason
	}{
		{"for another request", passkeys.Binding{Person: zz, Session: session, Action: action("POST /v2/memories/M-0220:forget")}, 0, passkeys.ReasonOtherRequest},
		{"from another session", passkeys.Binding{Person: zz, Session: uuid.New(), Action: keep.Action}, 0, passkeys.ReasonOtherSession},
		{"by another person", passkeys.Binding{Person: jy, Session: session, Action: keep.Action}, 0, passkeys.ReasonUnknown},
		{"without a session", passkeys.Binding{Person: zz, Action: keep.Action}, 0, passkeys.ReasonSessionNeeded},
		{"after it expired", keep, passkeys.ChallengeTTL, passkeys.ReasonExpired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertion := issue(keep)
			f.clock.advance(c.advance)
			defer f.clock.advance(-c.advance)
			_, err := f.svc.VerifyCheck(ctx, c.present, assertion)
			wantReason(t, err, c.want)
		})
	}

	assertion := issue(keep)
	used, err := f.svc.VerifyCheck(ctx, keep, assertion)
	if err != nil || used.ID != pk.ID {
		t.Fatalf("verify: %v %+v", err, used)
	}
	_, err = f.svc.VerifyCheck(ctx, keep, assertion)
	wantReason(t, err, passkeys.ReasonUsed)

	// A sign-in challenge isn't a check, and a check isn't a sign-in.
	req, _, err := f.svc.BeginSignIn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	signIn, err := a.Get(js(t, req))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.VerifyCheck(ctx, keep, signIn)
	wantReason(t, err, passkeys.ReasonUnknown)
	_, _, err = f.svc.FinishSignIn(ctx, issue(keep))
	wantReason(t, err, passkeys.ReasonUnknown)

	// Someone without a passkey has nothing to check with.
	nobody := f.person("nobody")
	_, _, err = f.svc.BeginCheck(ctx, passkeys.Binding{Person: nobody, Session: session, Action: keep.Action})
	wantReason(t, err, passkeys.ReasonNoPasskey)
}

// TestManageAndRLS: a person lists, renames and removes their own
// passkeys only; as memax_v2 nobody reads another person's, sweep or not;
// v2.passkey_owner answers only an owner.
func TestManageAndRLS(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy := f.person("zz"), f.person("jy")
	session := uuid.New()
	pk := f.register(passkeytest.New(origin), zz, session, "Laptop")
	other := f.register(passkeytest.New(origin), jy, session, "Phone")

	if _, err := f.svc.Rename(ctx, jy, pk.ID, "mine now"); !errors.Is(err, passkeys.ErrNotFound) {
		t.Fatalf("rename someone else's: %v", err)
	}
	if _, err := f.svc.Remove(ctx, jy, pk.ID); !errors.Is(err, passkeys.ErrNotFound) {
		t.Fatalf("remove someone else's: %v", err)
	}
	renamed, err := f.svc.Rename(ctx, zz, pk.ID, "Work laptop")
	if err != nil || renamed.Name != "Work laptop" {
		t.Fatalf("rename: %v %+v", err, renamed)
	}
	list, err := f.svc.List(ctx, zz)
	if err != nil || len(list) != 1 || list[0].ID != pk.ID {
		t.Fatalf("list: %v %+v", err, list)
	}

	// As memax_v2: the person's own rows only, whatever the transaction sets.
	asV2 := func(person uuid.UUID, sweep string, q string, args ...any) int {
		t.Helper()
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `SELECT set_config('role', 'memax_v2', true), set_config('app.person_id', $1, true), set_config('app.sweep', $2, true)`,
			person.String(), sweep); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := tx.QueryRow(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := asV2(jy, "", `SELECT count(*) FROM v2.passkeys`); n != 1 {
		t.Errorf("jy sees %d passkeys, want only theirs", n)
	}
	if n := asV2(jy, "passkey_sign_in", `SELECT count(*) FROM v2.passkeys WHERE person_id = $1`, zz); n != 0 {
		t.Errorf("setting app.sweep let memax_v2 read %d of someone else's passkeys", n)
	}
	if n := asV2(jy, "", `SELECT count(*) FROM v2.passkey_challenges WHERE person_id = $1`, zz); n != 0 {
		t.Errorf("jy sees %d of zz's challenges", n)
	}
	// The owner lookup answers who, and nothing else.
	var credID []byte
	if err := f.pool.QueryRow(ctx, `SELECT credential_id FROM v2.passkeys WHERE id = $1`, other.ID).Scan(&credID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('role', 'memax_v2', true)`); err != nil {
		t.Fatal(err)
	}
	var owner *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT v2.passkey_owner($1)`, credID).Scan(&owner); err != nil || owner == nil || *owner != jy {
		t.Fatalf("owner: %v %v", owner, err)
	}
	if err := tx.QueryRow(ctx, `SELECT v2.passkey_owner($1)`, []byte("no such credential id at all")).Scan(&owner); err != nil || owner != nil {
		t.Fatalf("unknown owner: %v %v", owner, err)
	}
	_ = tx.Rollback(ctx)

	removed, err := f.svc.Remove(ctx, zz, pk.ID)
	if err != nil || removed.ID != pk.ID {
		t.Fatalf("remove: %v", err)
	}
	if has, _ := f.svc.Has(ctx, zz); has {
		t.Error("zz still has a passkey")
	}
	if n, err := f.svc.RemoveAll(ctx, jy); err != nil || n != 1 {
		t.Fatalf("remove all: %d %v", n, err)
	}
	var left int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM v2.passkeys`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d passkeys left (%v)", left, err)
	}
	if _, err := f.pool.Exec(ctx, `SELECT 1`); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		env     map[string]string
		ok      bool
		rpID    string
		origins []string
		err     string
	}{
		{"off", map[string]string{}, false, "", nil, ""},
		{"production from APP_BASE_URL", map[string]string{"APP_BASE_URL": "https://memax.app"}, true, "memax.app", []string{"https://memax.app"}, ""},
		{"staging on its own host", map[string]string{"APP_BASE_URL": "https://staging.memax.app"}, true, "staging.memax.app", []string{"https://staging.memax.app"}, ""},
		{"localhost over http", map[string]string{"APP_BASE_URL": "http://localhost:3000"}, true, "localhost", []string{"http://localhost:3000"}, ""},
		{"an RP ID above the app", map[string]string{"APP_BASE_URL": "https://app.memax.app", "WEBAUTHN_RP_ID": "memax.app"}, true, "memax.app", []string{"https://app.memax.app"}, ""},
		{"several origins", map[string]string{"WEBAUTHN_RP_ID": "localhost", "WEBAUTHN_RP_ORIGINS": "http://localhost:3100, http://localhost:8790/"}, true, "localhost", []string{"http://localhost:3100", "http://localhost:8790"}, ""},
		{"an origin off the RP ID", map[string]string{"WEBAUTHN_RP_ID": "memax.app", "WEBAUTHN_RP_ORIGINS": "https://memax.dev"}, true, "memax.app", nil, "not on the relying party"},
		{"http off localhost", map[string]string{"APP_BASE_URL": "http://memax.app"}, true, "memax.app", nil, "https"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, ok, err := passkeys.ConfigFromEnv(func(k string) string { return c.env[k] })
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err %v, want %q", err, c.err)
				}
				return
			}
			if err != nil || ok != c.ok || cfg.RPID != c.rpID || strings.Join(cfg.Origins, ",") != strings.Join(c.origins, ",") {
				t.Fatalf("got %+v ok=%v err=%v", cfg, ok, err)
			}
		})
	}
}

func TestCleanName(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"  Work   laptop ": "Work laptop", "": "", "a\x00b": "", strings.Repeat("x", 65): "", "工作电脑": "工作电脑"} {
		got, ok := passkeys.CleanName(in)
		if got != want || ok != (want != "") {
			t.Errorf("CleanName(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
}

package v2ui_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2ui"
)

var since = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

// The rule, every way it can come out: an operator's Off wins over
// everything, On and a space on V2 turn it on, V2_UI_SINCE turns it on for
// accounts created at or after it, and nothing else does.
func TestDecide(t *testing.T) {
	t.Parallel()
	before, at, after := since.Add(-time.Second), since, since.Add(time.Hour)
	cases := []struct {
		name   string
		facts  v2ui.Facts
		since  time.Time
		ui     v2ui.UI
		reason v2ui.Reason
	}{
		{"no V2 space, nothing else", v2ui.Facts{CreatedAt: before}, since, v2ui.V1, v2ui.ReasonNone},
		{"no V2 space, V2_UI_SINCE unset", v2ui.Facts{CreatedAt: after}, time.Time{}, v2ui.V1, v2ui.ReasonNone},
		{"(a) a member of a space on V2", v2ui.Facts{V2Space: true, CreatedAt: before}, since, v2ui.V2, v2ui.ReasonV2Space},
		{"(b) an operator turned it on", v2ui.Facts{Setting: v2ui.On, CreatedAt: before}, since, v2ui.V2, v2ui.ReasonOperatorOn},
		{"(b) on, with a space too", v2ui.Facts{Setting: v2ui.On, V2Space: true}, since, v2ui.V2, v2ui.ReasonOperatorOn},
		{"(c) signed up at V2_UI_SINCE", v2ui.Facts{CreatedAt: at}, since, v2ui.V2, v2ui.ReasonSignedUp},
		{"(c) signed up after it", v2ui.Facts{CreatedAt: after}, since, v2ui.V2, v2ui.ReasonSignedUp},
		{"(c) signed up a second before it", v2ui.Facts{CreatedAt: before}, since, v2ui.V1, v2ui.ReasonNone},
		{"off wins over (a)", v2ui.Facts{Setting: v2ui.Off, V2Space: true}, since, v2ui.V1, v2ui.ReasonOperatorOff},
		{"off wins over (c)", v2ui.Facts{Setting: v2ui.Off, CreatedAt: after}, since, v2ui.V1, v2ui.ReasonOperatorOff},
		{"off wins over (a) and (c)", v2ui.Facts{Setting: v2ui.Off, V2Space: true, CreatedAt: after}, since, v2ui.V1, v2ui.ReasonOperatorOff},
		{"default is the rules", v2ui.Facts{Setting: v2ui.Default, V2Space: true}, since, v2ui.V2, v2ui.ReasonV2Space},
	}
	for _, c := range cases {
		d := v2ui.Decide(c.facts, c.since)
		if d.UI != c.ui || d.Reason != c.reason {
			t.Errorf("%s: %s (%s), want %s (%s)", c.name, d.UI, d.Reason, c.ui, c.reason)
		}
		want := c.facts.Setting
		if want == "" {
			want = v2ui.Default
		}
		if d.Setting != want {
			t.Errorf("%s: setting %q, want %q", c.name, d.Setting, want)
		}
	}
	// No database: V1 for everyone.
	var none *v2ui.Resolver
	if d := none.Decide(v2ui.Facts{V2Space: true}); d.UI != v2ui.V1 {
		t.Errorf("nil resolver = %s", d.UI)
	}
	if d, err := none.For(context.Background(), uuid.New()); err != nil || d.UI != v2ui.V1 {
		t.Errorf("nil resolver For = %v, %v", d, err)
	}
}

func TestSinceFromEnv(t *testing.T) {
	t.Parallel()
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "V2_UI_SINCE" {
				return v
			}
			return ""
		}
	}
	if got, err := v2ui.SinceFromEnv(env("")); err != nil || !got.IsZero() {
		t.Errorf("unset = %v, %v", got, err)
	}
	if got, err := v2ui.SinceFromEnv(env(" 2026-11-01T00:00:00Z ")); err != nil || !got.Equal(since) {
		t.Errorf("set = %v, %v", got, err)
	}
	if got, err := v2ui.SinceFromEnv(env("2026-11-01T09:00:00+09:00")); err != nil || !got.Equal(since) {
		t.Errorf("with an offset = %v, %v", got, err)
	}
	if _, err := v2ui.SinceFromEnv(env("November")); err == nil {
		t.Error("a bad time parsed")
	}
}

func TestParseSetting(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]v2ui.Setting{"on": v2ui.On, " OFF ": v2ui.Off, "default": v2ui.Default} {
		if got, err := v2ui.ParseSetting(in); err != nil || got != want {
			t.Errorf("%q = %q, %v", in, got, err)
		}
	}
	if _, err := v2ui.ParseSetting("yes"); err == nil {
		t.Error("yes parsed")
	}
}

type world struct {
	t    *testing.T
	pool *pgxpool.Pool
	ui   *v2ui.Resolver
	l    *ledger.Ledger
}

func newWorld(t *testing.T) *world {
	t.Helper()
	_, pool := testdb.Acquire(t)
	return &world{t: t, pool: pool, ui: v2ui.New(pool, since), l: ledger.New(pool)}
}

func (w *world) exec(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.pool.Exec(context.Background(), sql, args...); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
}

func (w *world) person(name string, created time.Time) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	w.exec(`INSERT INTO users (id, email, name, created_at) VALUES ($1, $2, $3, $4)`, id, name+"-"+id.String()[:8]+"@v2ui.test", name, created)
	return id
}

// v1Hub is a V1 hub, as V1 makes them: the owner is a member too.
func (w *world) v1Hub(owner uuid.UUID, kind policy.SpaceKind) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	w.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, $2, $2, 'team', $3, $4)`,
		id, "hub-"+id.String()[:8], owner, string(kind))
	w.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, owner)
	return id
}

func (w *world) want(person uuid.UUID, ui v2ui.UI, reason v2ui.Reason) {
	w.t.Helper()
	d, err := w.ui.For(context.Background(), person)
	if err != nil {
		w.t.Fatalf("For: %v", err)
	}
	if d.UI != ui || d.Reason != reason {
		w.t.Errorf("%s (%s), want %s (%s)", d.UI, d.Reason, ui, reason)
	}
}

func (w *world) set(person uuid.UUID, s v2ui.Setting, actor uuid.UUID) v2ui.Decision {
	w.t.Helper()
	d, err := w.ui.Set(context.Background(), person, s, actor, v2ui.ViaTest)
	if err != nil {
		w.t.Fatalf("Set %s: %v", s, err)
	}
	return d
}

// The facts as the database has them: who is in a space on V2 (owners and
// members, a space POST /v2/spaces made included), the operator's choice
// and its audit rows, V2_UI_SINCE against the account's creation, and
// finding a person by email.
func TestResolverReadsTheRecord(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ctx := context.Background()
	old := since.AddDate(0, -3, 0)

	nobody := w.person("nobody", old)
	w.v1Hub(nobody, policy.SpaceTeam)
	w.want(nobody, v2ui.V1, v2ui.ReasonNone)

	// A space made on V2 (memax init's POST /v2/spaces): its owner is on V2.
	creator := w.person("creator", old)
	sp, _, err := w.l.CreateSpace(ctx, ledger.Actor{Kind: policy.ActorPerson, ID: creator}, ledger.NewSpace{Name: "acme web", Kind: policy.SpaceProject})
	if err != nil {
		t.Fatal(err)
	}
	w.want(creator, v2ui.V2, v2ui.ReasonV2Space)
	// So is a member of it, and an owner without a membership row.
	member := w.person("member", old)
	w.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'contributor')`, sp.ID, member)
	w.want(member, v2ui.V2, v2ui.ReasonV2Space)
	w.exec(`DELETE FROM hub_members WHERE hub_id = $1 AND user_id = $2`, sp.ID, creator)
	w.want(creator, v2ui.V2, v2ui.ReasonV2Space)

	// An operator turns it on for someone with no space, and off for a
	// member of one; default gives the rules back. Each change is audited
	// with who made it and what it was before.
	operator := w.person("operator", old)
	if d := w.set(nobody, v2ui.On, operator); d.UI != v2ui.V2 || d.Reason != v2ui.ReasonOperatorOn || d.Setting != v2ui.On {
		t.Errorf("on = %+v", d)
	}
	if d := w.set(member, v2ui.Off, operator); d.UI != v2ui.V1 || d.Reason != v2ui.ReasonOperatorOff {
		t.Errorf("off = %+v", d)
	}
	if d := w.set(member, v2ui.Default, uuid.Nil); d.UI != v2ui.V2 || d.Reason != v2ui.ReasonV2Space || d.Setting != v2ui.Default {
		t.Errorf("default = %+v", d)
	}
	var audits []string
	rows, err := w.pool.Query(ctx, `
		SELECT resource_id || ' ' || action || ' ' || COALESCE(actor_id::text, '-') || ' ' || (metadata->>'previous') || '>' || (metadata->>'setting') || ' ' || (metadata->>'via')
		  FROM admin_audit WHERE resource_type = 'user' ORDER BY created_at`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		audits = append(audits, s)
	}
	want := []string{
		nobody.String() + " v2_ui " + operator.String() + " default>on testing",
		member.String() + " v2_ui " + operator.String() + " default>off testing",
		member.String() + " v2_ui - off>default testing",
	}
	if len(audits) != len(want) {
		t.Fatalf("audits = %q", audits)
	}
	for i := range want {
		if audits[i] != want[i] {
			t.Errorf("audit %d = %q, want %q", i, audits[i], want[i])
		}
	}

	// V2_UI_SINCE: an account created at or after it, not before.
	w.want(w.person("new", since), v2ui.V2, v2ui.ReasonSignedUp)
	w.want(w.person("newer", since.Add(48*time.Hour)), v2ui.V2, v2ui.ReasonSignedUp)
	w.want(w.person("older", since.Add(-time.Minute)), v2ui.V1, v2ui.ReasonNone)
	unset := v2ui.New(w.pool, time.Time{})
	if d, _ := unset.For(ctx, w.person("unset", since.Add(time.Hour))); d.UI != v2ui.V1 {
		t.Errorf("V2_UI_SINCE unset = %s", d.UI)
	}

	// Nobody there: refused, and nothing audited.
	if _, err := w.ui.For(ctx, uuid.New()); !errors.Is(err, v2ui.ErrNoPerson) {
		t.Errorf("For a stranger = %v", err)
	}
	if _, err := w.ui.Set(ctx, uuid.New(), v2ui.On, operator, v2ui.ViaTest); !errors.Is(err, v2ui.ErrNoPerson) {
		t.Errorf("Set a stranger = %v", err)
	}
	if _, err := w.ui.Set(ctx, nobody, v2ui.Setting("maybe"), operator, v2ui.ViaTest); err == nil {
		t.Error("a setting that isn't one was stored")
	}
	var n int
	if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM admin_audit`).Scan(&n); err != nil || n != 3 {
		t.Errorf("audit rows = %d, %v", n, err)
	}

	// By email, in any case.
	var email string
	if err := w.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, creator).Scan(&email); err != nil {
		t.Fatal(err)
	}
	if id, err := w.ui.PersonByEmail(ctx, "  "+strings.ToUpper(email)+" "); err != nil || id != creator {
		t.Errorf("by email = %s, %v", id, err)
	}
	if _, err := w.ui.PersonByEmail(ctx, "nobody@nowhere.test"); !errors.Is(err, v2ui.ErrNoPerson) {
		t.Errorf("unknown email = %v", err)
	}
}

// Switching a V1 space to V2 turns the flag on for its owner and members
// (rule a); switching back turns it off again, unless another space on V2
// or an operator keeps it on.
func TestSwitchTurnsTheFlagOnAndBackOff(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ctx := context.Background()
	owner := w.person("owner", since.AddDate(0, -3, 0))
	teammate := w.person("teammate", since.AddDate(0, -3, 0))
	hub := w.v1Hub(owner, policy.SpaceTeam)
	w.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'contributor')`, hub, teammate)
	w.want(owner, v2ui.V1, v2ui.ReasonNone)
	w.want(teammate, v2ui.V1, v2ui.ReasonNone)

	scope, err := w.l.UserScope(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	actor := ledger.Actor{Kind: policy.ActorPerson, ID: owner}
	st, err := w.l.StartSwitch(ctx, actor, policy.ViaCLI, scope, hub, ledger.SwitchOptions{Key: "to-v2"})
	if err != nil {
		t.Fatal(err)
	}
	if st.State != ledger.SwitchStateSwitched {
		t.Fatalf("switch = %s", st.State)
	}
	w.want(owner, v2ui.V2, v2ui.ReasonV2Space)
	w.want(teammate, v2ui.V2, v2ui.ReasonV2Space)

	if _, err := w.l.SwitchBack(ctx, actor, policy.ViaCLI, scope, hub, "to-v1"); err != nil {
		t.Fatal(err)
	}
	w.want(owner, v2ui.V1, v2ui.ReasonNone)
	w.want(teammate, v2ui.V1, v2ui.ReasonNone)

	// An operator turned it on for the owner: switching back leaves it on.
	w.set(owner, v2ui.On, uuid.Nil)
	if _, err := w.l.StartSwitch(ctx, actor, policy.ViaCLI, scope, hub, ledger.SwitchOptions{Key: "to-v2-again"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.l.SwitchBack(ctx, actor, policy.ViaCLI, scope, hub, "to-v1-again"); err != nil {
		t.Fatal(err)
	}
	w.want(owner, v2ui.V2, v2ui.ReasonOperatorOn)
	w.want(teammate, v2ui.V1, v2ui.ReasonNone)

	// Another space on V2 keeps the teammate on after this one goes back.
	if _, _, err := w.l.CreateSpace(ctx, ledger.Actor{Kind: policy.ActorPerson, ID: teammate}, ledger.NewSpace{Name: "teammate's", Kind: policy.SpaceProject}); err != nil {
		t.Fatal(err)
	}
	w.want(teammate, v2ui.V2, v2ui.ReasonV2Space)
}

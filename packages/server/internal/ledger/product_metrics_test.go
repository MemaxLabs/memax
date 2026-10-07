package ledger_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/ledgertest"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

func f64(v float64) *float64 { return &v }

func week(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

// The gate fixture's people (ledgertest.SeedGateFixture) give exact
// metrics: who counts as new or from V1, whose first session closed, who
// activated, how fast the first file came, who kept in week 4 and who
// pulled a teammate, each worked out by hand from their timelines.
func TestProductMetrics(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	fx := ledgertest.SeedGateFixture(t, f.pool)
	l := ledger.New(f.pool, ledger.WithClock(func() time.Time { return fx.AsOf }))
	m, err := l.GetProductMetrics(context.Background(), fx.From, fx.To)
	if err != nil {
		t.Fatal(err)
	}
	if !m.AsOf.Equal(fx.AsOf) || !m.From.Equal(fx.From) || !m.To.Equal(fx.To) || m.FirstSessionHours != 24 {
		t.Errorf("range = %s..%s as of %s, %v h", m.From, m.To, m.AsOf, m.FirstSessionHours)
	}

	newWeek := func(w string, c ledger.CohortMetrics) ledger.CohortMetrics {
		c.Week, c.Kind = week(w), ledger.CohortNew
		return c
	}
	eve := ledger.CohortMetrics{Kind: ledger.CohortFromV1, People: 1, SessionsClosed: 1, TwoConnections: 1,
		TwoAgentKinds: 1, Compiled: 1, Activated: 1, SignupToFileP50Seconds: f64(1200), RetentionEligible: 1, Retained: 1}
	eveWeek := eve
	eveWeek.Week = week("2026-08-03")
	wantCohorts := []ledger.CohortMetrics{
		// Ada, Ben, Cy, Dee, Fay.
		newWeek("2026-08-03", ledger.CohortMetrics{People: 5, SessionsClosed: 5, TwoConnections: 3, TwoAgentKinds: 2,
			Compiled: 3, Activated: 2, AgentsRead: 3, FirstFiles: 4, FirstFilesUnder5m: 3, FirstFileP50Seconds: f64(210),
			FirstFileP90Seconds: f64(408), SignupToFileP50Seconds: f64(570), RetentionEligible: 2, Retained: 1,
			TeamEligible: 2, TeamPulled: 1}),
		// Kim, Jo.
		newWeek("2026-08-10", ledger.CohortMetrics{People: 2, SessionsClosed: 2, TwoConnections: 1, TwoAgentKinds: 1,
			Compiled: 2, Activated: 1, FirstFiles: 2, FirstFilesUnder5m: 2, FirstFileP50Seconds: f64(180),
			FirstFileP90Seconds: f64(180), SignupToFileP50Seconds: f64(450), RetentionEligible: 1, Retained: 1}),
		// Grace.
		newWeek("2026-08-31", ledger.CohortMetrics{People: 1, SessionsClosed: 1}),
		// Kai, and Lu whose session is still open.
		newWeek("2026-09-28", ledger.CohortMetrics{People: 2, SessionsClosed: 1, TwoConnections: 1, TwoAgentKinds: 1,
			Compiled: 1, Activated: 1, FirstFiles: 2, FirstFilesUnder5m: 2, FirstFileP50Seconds: f64(210),
			FirstFileP90Seconds: f64(234), SignupToFileP50Seconds: f64(330)}),
		eveWeek,
	}
	wantTotals := []ledger.CohortMetrics{
		{Kind: ledger.CohortNew, People: 10, SessionsClosed: 9, TwoConnections: 5, TwoAgentKinds: 4, Compiled: 6,
			Activated: 4, AgentsRead: 3, FirstFiles: 8, FirstFilesUnder5m: 7, FirstFileP50Seconds: f64(180),
			FirstFileP90Seconds: f64(312), SignupToFileP50Seconds: f64(360), RetentionEligible: 3, Retained: 2,
			TeamEligible: 2, TeamPulled: 1},
		eve,
	}
	same(t, "cohorts", m.Cohorts, wantCohorts)
	same(t, "totals", m.Totals, wantTotals)

	wantReview := []ledger.ReviewHealth{
		// Ada's three (two kept in 2 minutes, one rejected in 6), Ben's
		// folded and open ones, Cy's forgotten one. Ada's agent's keep at
		// once and the people's own Keeps aren't proposals.
		{Week: week("2026-08-03"), Proposals: 6, Kept: 2, Rejected: 1, Folded: 1, Forgotten: 1, Open: 1,
			DecisionP50Seconds: f64(120), DecisionP90Seconds: f64(312)},
		// Kai's: one kept after an hour, one kept after the as-of moment.
		{Week: week("2026-09-28"), Proposals: 2, Kept: 1, Open: 1, DecisionP50Seconds: f64(3600), DecisionP90Seconds: f64(3600)},
	}
	same(t, "review", m.Review, wantReview)
	same(t, "review total", m.ReviewTotal, ledger.ReviewHealth{Proposals: 8, Kept: 3, Rejected: 1, Folded: 1,
		Forgotten: 1, Open: 2, DecisionP50Seconds: f64(240), DecisionP90Seconds: f64(2628)})
	if r, ok := m.Review[0].RejectRate(); !ok || r != 1.0/3 {
		t.Errorf("reject rate = %v %v", r, ok)
	}

	// The gates, judged on new people, with from V1 beside them.
	gates := m.PhaseGates()
	want := []ledger.PhaseGate{
		{Phase: 2, Name: "activation", Bar: 0.6, Value: f64(4.0 / 9), Numerator: 4, Denominator: 9, Status: ledger.PhaseGateFail, FromV1: f64(1)},
		{Phase: 2, Name: "first_file", Bar: 300, Value: f64(180), Numerator: 7, Denominator: 8, Status: ledger.PhaseGatePass},
		{Phase: 3, Name: "week4_keeping", Bar: 0.4, Value: f64(2.0 / 3), Numerator: 2, Denominator: 3, Status: ledger.PhaseGatePass, FromV1: f64(1)},
		{Phase: 4, Name: "team_pull", Bar: 0.25, Value: f64(0.5), Numerator: 1, Denominator: 2, Status: ledger.PhaseGatePass},
	}
	same(t, "gates", gates, want)

	// Before anyone's window closes, every gate but the first file waits.
	early := ledger.New(f.pool, ledger.WithClock(func() time.Time { return week("2026-08-03").Add(10*time.Hour + 30*time.Minute) }))
	m, err = early.GetProductMetrics(context.Background(), fx.From, fx.To)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range m.PhaseGates() {
		if g.Status != ledger.PhaseGatePending && g.Name != "first_file" {
			t.Errorf("at Monday 10:30, %s is %s", g.Name, g.Status)
		}
	}
	if n := m.Total(ledger.CohortNew); n.People != 1 || n.SessionsClosed != 0 || n.FirstFiles != 1 {
		t.Errorf("at Monday 10:30, new = %+v (only Ada has signed up, and her file came at 10:06)", n)
	}
}

// same compares as JSON, which shows the difference readably.
func same[T any](t *testing.T, what string, got, want T) {
	t.Helper()
	g, _ := json.MarshalIndent(got, "", "  ")
	w, _ := json.MarshalIndent(want, "", "  ")
	if string(g) != string(w) {
		t.Errorf("%s:\ngot  %s\nwant %s", what, g, w)
	}
}

// The range is checked before anything is read.
func TestProductMetricsRange(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now()
	for name, r := range map[string][2]time.Time{
		"reversed":  {now, now.Add(-time.Hour)},
		"empty":     {now, now},
		"too long":  {now.AddDate(-2, 0, 0), now},
		"open ends": {{}, now},
	} {
		if _, err := f.l.GetProductMetrics(ctx, r[0], r[1]); err == nil || !strings.Contains(err.Error(), "range") {
			t.Errorf("%s: %v", name, err)
		}
	}
	m, err := f.l.GetProductMetrics(ctx, now.AddDate(0, 0, -7), now)
	if err != nil || len(m.Cohorts) != 0 || len(m.Totals) != 0 || len(m.Review) != 0 || m.ReviewTotal.Proposals != 0 {
		t.Errorf("an empty record: %+v %v", m, err)
	}
	if g := m.PhaseGates(); len(g) != 4 || g[0].Status != ledger.PhaseGatePending || g[1].Status != ledger.PhaseGatePending {
		t.Errorf("gates of an empty record: %+v", g)
	}
}

// A person who goes through the real commands (a space on V2, two agents
// connected over OAuth, a compile delivered by their CLI, an agent's
// proposal they keep) is activated, and their review is counted: the rows
// the commands write are the ones the metrics read.
func TestProductMetricsFromCommands(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	ada := f.user("ada")
	sp, _, err := f.l.CreateSpace(ctx, person(ada), ledger.NewSpace{Name: "ada-web"})
	if err != nil {
		t.Fatal(err)
	}
	space := sp.ID
	for _, a := range []ledger.AgentKind{ledger.AgentClaudeCode, ledger.AgentCodex} {
		f.connect(ada, ledger.CredentialOAuthGrant, f.grant(ada, string(a), space), a, at(space, policy.AutonomyPropose))
	}
	scope := f.scope(ada)
	p := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP),
		NewMemory: fact(space, "Tests run with pnpm test.")}).Memory
	f.apply(&ledger.Keep{Meta: meta(person(ada), scope, policy.ViaReview), Memory: p.Ref})
	f.brief(ada, space, 0, demoSections(p.Ref))
	tg := f.target(ada, space, ledger.TargetAgentsMD)
	run := f.compiled(tg, "compiled", p.Ref)
	f.apply(&ledger.RecordDelivery{Meta: meta(person(ada), f.scope(ada), policy.ViaCLI), Target: tg.ID, Compile: run.Ref,
		SHA256: run.DriftSHA256})

	// A day and an hour later, her first session has closed.
	later := ledger.New(f.pool, ledger.WithClock(func() time.Time { return time.Now().Add(25 * time.Hour) }))
	m, err := later.GetProductMetrics(ctx, time.Now().Add(-24*time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	n := m.Total(ledger.CohortNew)
	if n.People != 1 || n.SessionsClosed != 1 || n.TwoConnections != 1 || n.TwoAgentKinds != 1 || n.Compiled != 1 ||
		n.Activated != 1 || n.SignupToFileP50Seconds == nil || n.FirstFiles != 0 {
		t.Errorf("new = %+v", n)
	}
	if r := m.ReviewTotal; r.Proposals != 1 || r.Kept != 1 || r.Open != 0 || r.DecisionP50Seconds == nil {
		t.Errorf("review = %+v", r)
	}
}

// The metrics read across spaces only inside v2.product_metrics and
// v2.review_health, which only memax_v2_metrics may call and which return
// counts and durations. memax_v2 can't call them, can't borrow their
// policies by setting app.sweep, and the metrics role can read nothing
// but their results.
func TestProductMetricsIsolation(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	fx := ledgertest.SeedGateFixture(t, f.pool)

	// memax_v2 (every request) may not call either function.
	for _, call := range []string{
		`SELECT * FROM v2.product_metrics(now() - interval '90 days', now(), interval '1 day', now())`,
		`SELECT * FROM v2.review_health(now() - interval '90 days', now(), now())`,
	} {
		err := f.asV2(nil, nil, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, call); return err })
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("memax_v2 ran %q: %v", call, err)
		}
	}

	// Setting the functions' sweep value admits nothing beyond the scope:
	// nothing with no scope, and only its own space's rows with one.
	var space, tenant uuid.UUID
	if err := f.pool.QueryRow(ctx, `
		SELECT h.id, h.tenant_id FROM public.hubs h JOIN public.users u ON u.id = h.owner_id
		 WHERE u.name = 'ada' AND h.space_kind = 'project'`).Scan(&space, &tenant); err != nil {
		t.Fatal(err)
	}
	const counts = `SELECT (SELECT count(*) FROM v2.receipts), (SELECT count(*) FROM v2.agent_connections),
	                       (SELECT count(*) FROM v2.imports), (SELECT count(*) FROM v2.reads)`
	var all [4]int
	if err := f.pool.QueryRow(ctx, counts).Scan(&all[0], &all[1], &all[2], &all[3]); err != nil || all[0] < 50 || all[3] < 3 {
		t.Fatalf("the fixture wrote %v (%v)", all, err)
	}
	var mine [4]int
	if err := f.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM v2.receipts WHERE space_id = $1),
		       (SELECT count(*) FROM v2.agent_connections c WHERE EXISTS (
		            SELECT 1 FROM v2.agent_connection_spaces s WHERE s.connection_id = c.id AND s.space_id = $1)),
		       (SELECT count(*) FROM v2.imports WHERE space_id = $1),
		       (SELECT count(*) FROM v2.reads WHERE space_id = $1)`, space).Scan(&mine[0], &mine[1], &mine[2], &mine[3]); err != nil {
		t.Fatal(err)
	}
	for _, scope := range [][]uuid.UUID{nil, {space}} {
		var got [4]int
		var tenants []uuid.UUID
		if scope != nil {
			tenants = []uuid.UUID{tenant}
		}
		err := f.asV2(scope, tenants, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT set_config('app.sweep', 'product_metrics', true)`); err != nil {
				return err
			}
			return tx.QueryRow(ctx, counts).Scan(&got[0], &got[1], &got[2], &got[3])
		})
		want := [4]int{}
		if scope != nil {
			want = mine
		}
		if err != nil || got != want {
			t.Errorf("memax_v2 with app.sweep product_metrics and scope %v sees %v, want %v (%v)", scope, got, want, err)
		}
	}

	// The metrics role reads nothing directly, not even with the sweep
	// value set: it may only execute the two functions.
	for _, table := range []string{"v2.receipts", "v2.agent_connections", "v2.imports", "v2.reads", "public.users",
		"public.hubs", "public.hub_members", "public.memories"} {
		_, err := asRole(t, f, ledger.MetricsRole, `SELECT count(*) FROM `+table)
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s read %s: %v", ledger.MetricsRole, table, err)
		}
	}
	if _, err := asRole(t, f, ledger.MetricsRole, `SELECT count(*) FROM v2.read_metrics(current_date)`); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("%s ran v2.read_metrics", ledger.MetricsRole)
	}
	n, err := asRole(t, f, ledger.MetricsRole, `SELECT count(*) FROM v2.product_metrics($1, $2, interval '1 day', $3)`,
		fx.From, fx.To, fx.AsOf)
	if err != nil || n != 7 {
		t.Errorf("%s computed %d rows (%v), want 5 weeks and 2 totals", ledger.MetricsRole, n, err)
	}

	// What they return is counts, dates, cohort names and durations:
	// never an id.
	rows, err := f.pool.Query(ctx, `
		SELECT p.proname, format_type(t, NULL)
		  FROM pg_proc p, unnest(p.proallargtypes) AS t
		 WHERE p.pronamespace = 'v2'::regnamespace AND p.proname IN ('product_metrics', 'review_health')`)
	if err != nil {
		t.Fatal(err)
	}
	types, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (string, error) {
		var fn, typ string
		err := r.Scan(&fn, &typ)
		return fn + ":" + typ, err
	})
	if err != nil || len(types) < 20 {
		t.Fatalf("argument types: %v %v", types, err)
	}
	allowed := []string{"timestamp with time zone", "interval", "date", "text", "bigint", "double precision"}
	for _, ty := range types {
		if !slices.Contains(allowed, ty[strings.IndexByte(ty, ':')+1:]) {
			t.Errorf("%s: only counts, dates, names and durations may leave the function", ty)
		}
	}
}

// asRole runs one query as role (from the login role) and returns its
// single bigint, or the error.
func asRole(t *testing.T, f *fixture, role, query string, args ...any) (int64, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('role', $1, true), set_config('app.sweep', 'product_metrics', true)`, role); err != nil {
		t.Fatal(err)
	}
	var n int64
	err = tx.QueryRow(ctx, query, args...).Scan(&n)
	return n, err
}

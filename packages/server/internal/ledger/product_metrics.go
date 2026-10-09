package ledger

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// The product metrics that judge plan 25's gates (§5.18, §12):
//
//	Phase 2  60% of new people connect 2+ agents and compile in their
//	         first session; first file in under 5 minutes.
//	Phase 3  40% of activated people still keeping in week 4.
//	Phase 4  25% of Pro people add a teammate within 60 days.
//
// plus review health. They are computed by weekly signup cohort from
// receipts, reads, agent connections and imports, across every space, by
// two SECURITY DEFINER functions only memax_v2_metrics may execute
// (migration 053), and come back as counts and durations: no memory text,
// no ids. The SQL there and the definitions here say the same thing:
//
//   - V2 start: the first time the person was a member of a space on the
//     V2 record (the later of joining it and its move to V2).
//   - Cohort: from_v1 when the person owned a V1 memory (not an onboarding
//     seed) written before their V2 start, else new. The gates are judged
//     on new people: a V1 person's switch connects their V1 keys and
//     grants in one go, so counting them would measure the switch, not
//     onboarding. from_v1 is reported beside new, never inside it. Staff
//     (an admin role) are in neither.
//   - Signup: new, the account's creation; from_v1, their V2 start. A
//     person who never reached a V2 space has no receipts to count; the
//     web's funnel events show where they stopped.
//   - First session: FirstSession after signup. init takes minutes, but an
//     agent's connection appears when the agent first signs in over OAuth,
//     which is when the person next opens it; a day still means the first
//     sitting, not the first week.
//   - Activated: by the end of the first session, 2+ of the person's agent
//     connections connected (their `connected` creation receipts) and not
//     disconnected, and a compile delivered to a file by the person's own
//     CLI or daemon (a `delivered` receipt they, or one of their
//     connections, wrote). MCP and copy-out targets are delivered by Memax
//     as they compile, so they don't count. Only closed sessions count, in
//     the numerator and the denominator.
//   - Time to first file: the first delivered compile minus init's first
//     import (§7.3 promises init's first file in five minutes, and the
//     import is init's first trace on the server; detect, sign-in and
//     connect come before it, about a minute of budget). Signup to first
//     file is beside it.
//   - Week-4 keeping: activated people who kept something themselves in
//     days 21–28 after signup, once day 28 has passed.
//   - Team pull: people for whom another person joined a V2 space they own
//     within 60 days of signup, once day 60 has passed. Plans aren't billed
//     yet, so it is every person, not Pro.
//   - Review health: per week a proposal was made, what decided it first
//     (kept, rejected, folded by the judge, forgotten) or nothing yet, the
//     reject rate (rejected / (kept + rejected)), and the time from
//     proposal to a Keep or Reject.

// FirstSession is how long after signup the first session lasts.
const FirstSession = 24 * time.Hour

// MetricsRole is the role the product metrics run as (migration 053): it
// alone may execute v2.product_metrics and v2.review_health, and it can
// read nothing else.
const MetricsRole = "memax_v2_metrics"

// MaxMetricsRange bounds one GetProductMetrics call.
const MaxMetricsRange = 53 * 7 * 24 * time.Hour

// The bars of the phase gates (plan 25 §12).
const (
	BarActivation   = 0.60
	BarFirstFile    = 5 * time.Minute
	BarWeek4Keeping = 0.40
	BarTeamPull     = 0.25
)

// CohortKind splits people by where they came from.
type CohortKind string

const (
	// CohortNew signed up for V2: the gates' people.
	CohortNew CohortKind = "new"
	// CohortFromV1 wrote memories in V1 before their first V2 space.
	CohortFromV1 CohortKind = "from_v1"
)

// CohortMetrics is one signup cohort: a week's people of one kind, or
// (Week zero) every week of the range together.
type CohortMetrics struct {
	// Week is the Monday (UTC) of the signup week; zero for a total.
	Week time.Time  `json:"week,omitzero"`
	Kind CohortKind `json:"cohort"`
	// People signed up in the week.
	People int64 `json:"people"`
	// SessionsClosed are the people whose first session has ended: the
	// denominator of everything below up to AgentsRead.
	SessionsClosed int64 `json:"sessions_closed"`
	// TwoConnections connected 2+ agent connections in the first session;
	// TwoAgentKinds 2+ different agents (two Claude Codes are one).
	TwoConnections int64 `json:"two_connections"`
	TwoAgentKinds  int64 `json:"two_agent_kinds"`
	// Compiled had a compile delivered to a file in the first session.
	Compiled int64 `json:"compiled"`
	// Activated did both (2+ connections and a delivered compile).
	Activated int64 `json:"activated"`
	// AgentsRead had an agent read from Memax in the first session (MCP,
	// or a session-start hook's compile load).
	AgentsRead int64 `json:"agents_read"`
	// FirstFiles had a first delivered compile after init's first import;
	// FirstFilesUnder5m within five minutes of it.
	FirstFiles        int64 `json:"first_files"`
	FirstFilesUnder5m int64 `json:"first_files_under_5m"`
	// Time to first file over FirstFiles, and signup to first file over
	// everyone with a first file, in seconds; nil when there is none.
	FirstFileP50Seconds    *float64 `json:"first_file_p50_seconds"`
	FirstFileP90Seconds    *float64 `json:"first_file_p90_seconds"`
	SignupToFileP50Seconds *float64 `json:"signup_to_file_p50_seconds"`
	RetentionEligible      int64    `json:"retention_eligible"`
	Retained               int64    `json:"retained"`
	TeamEligible           int64    `json:"team_eligible"`
	TeamPulled             int64    `json:"team_pulled"`
}

// ActivationRate is Activated / SessionsClosed; ok is false with no
// closed session.
func (c CohortMetrics) ActivationRate() (float64, bool) { return rate(c.Activated, c.SessionsClosed) }

// RetentionRate is Retained / RetentionEligible.
func (c CohortMetrics) RetentionRate() (float64, bool) { return rate(c.Retained, c.RetentionEligible) }

// TeamPullRate is TeamPulled / TeamEligible.
func (c CohortMetrics) TeamPullRate() (float64, bool) { return rate(c.TeamPulled, c.TeamEligible) }

// ReviewHealth is a week's proposals (Week zero: the whole range) and
// what became of them.
type ReviewHealth struct {
	Week      time.Time `json:"week,omitzero"`
	Proposals int64     `json:"proposals"`
	Kept      int64     `json:"kept"`
	Rejected  int64     `json:"rejected"`
	// Folded were merged into another memory by the judge (a repeat).
	Folded    int64 `json:"folded"`
	Forgotten int64 `json:"forgotten"`
	// Open have no decision yet.
	Open int64 `json:"open"`
	// The time from proposal to Keep or Reject, in seconds.
	DecisionP50Seconds *float64 `json:"decision_p50_seconds"`
	DecisionP90Seconds *float64 `json:"decision_p90_seconds"`
}

// RejectRate is Rejected / (Kept + Rejected).
func (r ReviewHealth) RejectRate() (float64, bool) { return rate(r.Rejected, r.Kept+r.Rejected) }

// PhaseGateStatus is where a phase gate stands.
type PhaseGateStatus string

const (
	PhaseGatePass PhaseGateStatus = "pass"
	PhaseGateFail PhaseGateStatus = "fail"
	// PhaseGatePending has no one whose window has closed yet.
	PhaseGatePending PhaseGateStatus = "pending"
)

// PhaseGate is one of §12's gates, judged on the new cohort over the range.
type PhaseGate struct {
	Phase int `json:"phase"`
	// Name is activation, first_file, week4_keeping or team_pull.
	Name string `json:"name"`
	// Bar is the rate to reach, or for first_file the seconds to stay
	// under (the median).
	Bar float64 `json:"bar"`
	// Value is the rate, or for first_file the median seconds; nil while
	// pending.
	Value *float64 `json:"value"`
	// Numerator / Denominator are the people behind Value (for
	// first_file: under five minutes / with a first file).
	Numerator   int64           `json:"numerator"`
	Denominator int64           `json:"denominator"`
	Status      PhaseGateStatus `json:"status"`
	// FromV1 is the same measure for the from_v1 cohort, for context; nil
	// when it has no one.
	FromV1 *float64 `json:"from_v1"`
}

// ProductMetrics is the cohorts of people who signed up in [From, To),
// as of AsOf.
type ProductMetrics struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	AsOf time.Time `json:"as_of"`
	// FirstSessionHours is FirstSession.
	FirstSessionHours float64 `json:"first_session_hours"`
	// Cohorts are per signup week and kind, new first, oldest week first.
	Cohorts []CohortMetrics `json:"cohorts"`
	// Totals are per kind over the whole range.
	Totals []CohortMetrics `json:"totals"`
	// Review is per week proposals were made, with the range's total.
	Review      []ReviewHealth `json:"review"`
	ReviewTotal ReviewHealth   `json:"review_total"`
}

// Total is the range's total for a kind (zero People when it has none).
func (m ProductMetrics) Total(kind CohortKind) CohortMetrics {
	for _, t := range m.Totals {
		if t.Kind == kind {
			return t
		}
	}
	return CohortMetrics{Kind: kind}
}

// PhaseGates judges §12's gates on the new cohort's totals, with the from_v1
// cohort's value beside each.
func (m ProductMetrics) PhaseGates() []PhaseGate {
	n, v1 := m.Total(CohortNew), m.Total(CohortFromV1)
	ratio := func(phase int, name string, bar float64, num, den int64, other func(CohortMetrics) (float64, bool)) PhaseGate {
		g := PhaseGate{Phase: phase, Name: name, Bar: bar, Numerator: num, Denominator: den, Status: PhaseGatePending}
		if r, ok := rate(num, den); ok {
			g.Value, g.Status = &r, PhaseGateFail
			if r >= bar {
				g.Status = PhaseGatePass
			}
		}
		if r, ok := other(v1); ok {
			g.FromV1 = &r
		}
		return g
	}
	file := PhaseGate{Phase: 2, Name: "first_file", Bar: BarFirstFile.Seconds(), Numerator: n.FirstFilesUnder5m,
		Denominator: n.FirstFiles, Status: PhaseGatePending, Value: n.FirstFileP50Seconds, FromV1: v1.FirstFileP50Seconds}
	if file.Value != nil {
		file.Status = PhaseGateFail
		if *file.Value < file.Bar {
			file.Status = PhaseGatePass
		}
	}
	return []PhaseGate{
		ratio(2, "activation", BarActivation, n.Activated, n.SessionsClosed, CohortMetrics.ActivationRate),
		file,
		ratio(3, "week4_keeping", BarWeek4Keeping, n.Retained, n.RetentionEligible, CohortMetrics.RetentionRate),
		ratio(4, "team_pull", BarTeamPull, n.TeamPulled, n.TeamEligible, CohortMetrics.TeamPullRate),
	}
}

func rate(num, den int64) (float64, bool) {
	if den == 0 {
		return 0, false
	}
	return float64(num) / float64(den), true
}

// GetProductMetrics computes the cohorts of people who signed up in
// [from, to) and the review health of proposals made in it, as of now,
// as MetricsRole, in one snapshot.
func (l *Ledger) GetProductMetrics(ctx context.Context, from, to time.Time) (ProductMetrics, error) {
	if l == nil {
		return ProductMetrics{}, ErrDisabled
	}
	from, to = from.UTC(), to.UTC()
	switch {
	case from.IsZero() || to.IsZero():
		return ProductMetrics{}, invalid("range", "give both ends")
	case !from.Before(to):
		return ProductMetrics{}, invalid("range", "from (%s) must come before to (%s)", from.Format(time.RFC3339), to.Format(time.RFC3339))
	case to.Sub(from) > MaxMetricsRange:
		return ProductMetrics{}, invalid("range", "ask for at most 53 weeks at a time")
	}
	m := ProductMetrics{From: from, To: to, AsOf: l.now().UTC().Truncate(time.Second), FirstSessionHours: FirstSession.Hours(),
		Cohorts: []CohortMetrics{}, Totals: []CohortMetrics{}, Review: []ReviewHealth{}}
	tx, _, err := l.beginRole(ctx, MetricsRole, Scope{}, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return ProductMetrics{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `SELECT * FROM v2.product_metrics($1, $2, $3::interval, $4)`,
		from, to, fmt.Sprintf("%d seconds", int64(FirstSession.Seconds())), m.AsOf)
	if err != nil {
		return ProductMetrics{}, fmt.Errorf("ledger: product metrics: %w", err)
	}
	cohorts, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (CohortMetrics, error) {
		var c CohortMetrics
		var week *time.Time
		err := r.Scan(&week, &c.Kind, &c.People, &c.SessionsClosed, &c.TwoConnections, &c.TwoAgentKinds, &c.Compiled,
			&c.Activated, &c.AgentsRead, &c.FirstFiles, &c.FirstFilesUnder5m, &c.FirstFileP50Seconds,
			&c.FirstFileP90Seconds, &c.SignupToFileP50Seconds, &c.RetentionEligible, &c.Retained, &c.TeamEligible,
			&c.TeamPulled)
		if week != nil {
			c.Week = week.UTC()
		}
		return c, err
	})
	if err != nil {
		return ProductMetrics{}, fmt.Errorf("ledger: product metrics: %w", err)
	}
	for _, c := range cohorts {
		if c.Week.IsZero() {
			m.Totals = append(m.Totals, c)
		} else {
			m.Cohorts = append(m.Cohorts, c)
		}
	}
	sortCohorts(m.Cohorts)
	sortCohorts(m.Totals)

	rows, err = tx.Query(ctx, `SELECT * FROM v2.review_health($1, $2, $3)`, from, to, m.AsOf)
	if err != nil {
		return ProductMetrics{}, fmt.Errorf("ledger: review health: %w", err)
	}
	review, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ReviewHealth, error) {
		var h ReviewHealth
		var week *time.Time
		err := r.Scan(&week, &h.Proposals, &h.Kept, &h.Rejected, &h.Folded, &h.Forgotten, &h.Open,
			&h.DecisionP50Seconds, &h.DecisionP90Seconds)
		if week != nil {
			h.Week = week.UTC()
		}
		return h, err
	})
	if err != nil {
		return ProductMetrics{}, fmt.Errorf("ledger: review health: %w", err)
	}
	for _, h := range review {
		if h.Week.IsZero() {
			m.ReviewTotal = h
		} else {
			m.Review = append(m.Review, h)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ProductMetrics{}, fmt.Errorf("ledger: product metrics: %w", err)
	}
	return m, nil
}

// WeekStart is the Monday, 00:00 UTC, that begins t's week: cohorts and
// review health are counted in these weeks.
func WeekStart(t time.Time) time.Time {
	t = t.UTC()
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

// sortCohorts puts new before from_v1, then the oldest week first.
func sortCohorts(cs []CohortMetrics) {
	order := map[CohortKind]int{CohortNew: 0, CohortFromV1: 1}
	slices.SortStableFunc(cs, func(a, b CohortMetrics) int {
		if d := cmp.Compare(order[a.Kind], order[b.Kind]); d != 0 {
			return d
		}
		return a.Week.Compare(b.Week)
	})
}

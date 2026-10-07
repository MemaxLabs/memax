package reads

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// reportProductMetrics logs every cohort, the range's totals, review
// health and the phase gates (one line each, `metric` naming which), and
// records the totals as gauges per cohort. Counts and seconds only, as
// GetProductMetrics returns them.
func reportProductMetrics(ctx context.Context, log *slog.Logger, m ledger.ProductMetrics) {
	cohort := func(msg, name string, c ledger.CohortMetrics) {
		attrs := []any{"metric", name, "cohort", string(c.Kind)}
		if !c.Week.IsZero() {
			attrs = append(attrs, "week", c.Week.Format(time.DateOnly))
		} else {
			attrs = append(attrs, "from", m.From.Format(time.DateOnly), "to", m.To.Format(time.DateOnly))
		}
		attrs = append(attrs,
			"people", c.People, "sessions_closed", c.SessionsClosed, "two_connections", c.TwoConnections,
			"two_agent_kinds", c.TwoAgentKinds, "compiled", c.Compiled, "activated", c.Activated,
			"agents_read", c.AgentsRead, "first_files", c.FirstFiles, "first_files_under_5m", c.FirstFilesUnder5m,
			"first_file_p50_s", seconds(c.FirstFileP50Seconds), "first_file_p90_s", seconds(c.FirstFileP90Seconds),
			"signup_to_file_p50_s", seconds(c.SignupToFileP50Seconds),
			"retention_eligible", c.RetentionEligible, "retained", c.Retained,
			"team_eligible", c.TeamEligible, "team_pulled", c.TeamPulled)
		log.InfoContext(ctx, msg, attrs...)
	}
	for _, c := range m.Cohorts {
		cohort("product: cohort", "product_cohort", c)
	}
	for _, c := range m.Totals {
		cohort("product: cohorts in range", "product_cohort_total", c)
	}
	review := func(msg, name string, r ledger.ReviewHealth) {
		attrs := []any{"metric", name}
		if !r.Week.IsZero() {
			attrs = append(attrs, "week", r.Week.Format(time.DateOnly))
		}
		rr, _ := r.RejectRate()
		attrs = append(attrs, "proposals", r.Proposals, "kept", r.Kept, "rejected", r.Rejected, "folded", r.Folded,
			"forgotten", r.Forgotten, "open", r.Open, "reject_rate", rr,
			"decision_p50_s", seconds(r.DecisionP50Seconds), "decision_p90_s", seconds(r.DecisionP90Seconds))
		log.InfoContext(ctx, msg, attrs...)
	}
	for _, r := range m.Review {
		review("product: review health", "review_health", r)
	}
	review("product: review health in range", "review_health_total", m.ReviewTotal)
	for _, g := range m.PhaseGates() {
		log.InfoContext(ctx, "product: phase gate", "metric", "phase_gate", "phase", g.Phase, "gate", g.Name,
			"status", string(g.Status), "value", seconds(g.Value), "bar", g.Bar, "numerator", g.Numerator,
			"denominator", g.Denominator, "from_v1", seconds(g.FromV1))
	}

	meter := otel.Meter("memax.product")
	ints := func(name, desc string, v int64, attrs ...attribute.KeyValue) {
		if g, err := meter.Int64Gauge(name, metric.WithDescription(desc)); err == nil {
			g.Record(ctx, v, metric.WithAttributes(attrs...))
		}
	}
	floats := func(name, desc, unit string, v float64, ok bool, attrs ...attribute.KeyValue) {
		if !ok {
			return
		}
		if g, err := meter.Float64Gauge(name, metric.WithDescription(desc), metric.WithUnit(unit)); err == nil {
			g.Record(ctx, v, metric.WithAttributes(attrs...))
		}
	}
	for _, c := range m.Totals {
		kind := attribute.String("cohort", string(c.Kind))
		ints("memax.product.people", "People who signed up in the last ProductMetricsWeeks weeks", c.People, kind)
		ints("memax.product.activated", "People activated in their first session (2+ agents and a delivered compile)", c.Activated, kind)
		r, ok := c.ActivationRate()
		floats("memax.product.activation_rate", "Activated / first sessions closed (Phase 2 gate: 0.6 of new people)", "1", r, ok, kind)
		p50 := c.FirstFileP50Seconds
		floats("memax.product.first_file_p50", "Median time from init's first import to the first delivered compile (Phase 2 gate: 300 s)", "s",
			deref(p50), p50 != nil, kind)
		r, ok = c.RetentionRate()
		floats("memax.product.week4_keeping_rate", "Activated people keeping in week 4 (Phase 3 gate: 0.4)", "1", r, ok, kind)
		r, ok = c.TeamPullRate()
		floats("memax.product.team_pull_rate", "People adding a teammate within 60 days (Phase 4 gate: 0.25)", "1", r, ok, kind)
	}
	t := m.ReviewTotal
	ints("memax.review.proposals", "Proposals made in the last ProductMetricsWeeks weeks", t.Proposals)
	ints("memax.review.open", "Of those, proposals no one has decided", t.Open)
	r, ok := t.RejectRate()
	floats("memax.review.reject_rate", "Rejected / (kept + rejected) of those proposals", "1", r, ok)
	floats("memax.review.decision_p50", "Median time from proposal to Keep or Reject", "s",
		deref(t.DecisionP50Seconds), t.DecisionP50Seconds != nil)
}

// seconds is a duration (or rate) for a log line: its value, or nil when
// there is none.
func seconds(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

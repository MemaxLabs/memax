// Command v2-gate-metrics prints plan 25's phase gates (§12) for people
// who signed up in a date range, with the cohorts, review health and the
// north star behind them (ledger.GetProductMetrics, migration 051). It
// reads only: counts and durations, as the metrics role.
//
//	DATABASE_URL=… go run ./cmd/v2-gate-metrics                          # the last 8 signup weeks
//	DATABASE_URL=… go run ./cmd/v2-gate-metrics -from 2026-10-12 -to 2026-12-07
//	DATABASE_URL=… go run ./cmd/v2-gate-metrics -from 2026-10-12 -as-of 2026-11-30 -json
//
// -from is the first signup day and -to the day after the last (both
// UTC); -as-of judges the record as it stood then (default now).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("connect", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := run(ctx, pool, os.Args[1:], os.Stdout, time.Now); err != nil {
		var usage usageError
		if errors.As(err, &usage) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		slog.Error("gate metrics", "error", err)
		os.Exit(1)
	}
}

type usageError string

func (e usageError) Error() string { return string(e) }

// run parses args, reads the metrics as of -as-of (or now) and writes
// them to out.
func run(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer, now func() time.Time) error {
	fs := flag.NewFlagSet("v2-gate-metrics", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fromFlag := fs.String("from", "", "first signup day, YYYY-MM-DD (default: the Monday 7 weeks before this week's)")
	toFlag := fs.String("to", "", "the day after the last signup day, YYYY-MM-DD (default: next Monday)")
	asOfFlag := fs.String("as-of", "", "judge the record as it stood then: YYYY-MM-DD or RFC 3339 (default: now)")
	asJSON := fs.Bool("json", false, "print JSON instead of tables")
	if err := fs.Parse(args); err != nil {
		return usageError(fmt.Sprintf("v2-gate-metrics: %v\nusage: v2-gate-metrics [-from YYYY-MM-DD] [-to YYYY-MM-DD] [-as-of YYYY-MM-DD] [-json]", err))
	}
	asOf := now().UTC()
	if *asOfFlag != "" {
		t, err := parseMoment(*asOfFlag)
		if err != nil {
			return usageError("v2-gate-metrics: -as-of: give a date (2026-11-30) or a time (2026-11-30T12:00:00Z)")
		}
		asOf = t
	}
	to := ledger.WeekStart(asOf).AddDate(0, 0, 7)
	from := to.AddDate(0, 0, -7*8)
	for name, p := range map[string]struct {
		v   string
		dst *time.Time
	}{"from": {*fromFlag, &from}, "to": {*toFlag, &to}} {
		if p.v == "" {
			continue
		}
		d, err := time.Parse(time.DateOnly, p.v)
		if err != nil {
			return usageError(fmt.Sprintf("v2-gate-metrics: -%s: give a date like 2026-10-12", name))
		}
		*p.dst = d
	}

	if !from.Before(to) {
		return usageError("v2-gate-metrics: -from must come before -to")
	}

	l := ledger.New(pool, ledger.WithClock(func() time.Time { return asOf }))
	m, err := l.GetProductMetrics(ctx, from, to)
	if err != nil {
		var invalid *ledger.ValidationError
		if errors.As(err, &invalid) {
			return usageError("v2-gate-metrics: " + invalid.Message)
		}
		return err
	}
	ns, err := l.GetReadMetrics(ctx, asOf.AddDate(0, 0, -1))
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			ledger.ProductMetrics
			Gates     []ledger.PhaseGate `json:"gates"`
			NorthStar northStar          `json:"north_star"`
		}{m, m.PhaseGates(), northStar{WeekEnding: ns.Day.Format(time.DateOnly), SpacesRead: ns.SpacesRead,
			SpacesTwoAgents: ns.SpacesTwoAgents, ConnectionsReading: ns.ConnectionsReading,
			ConnectionsSeen: ns.ConnectionsSeen, Coverage: ns.Coverage()}})
	}
	return render(out, m, ns)
}

type northStar struct {
	WeekEnding         string  `json:"week_ending"`
	SpacesRead         int64   `json:"spaces_read"`
	SpacesTwoAgents    int64   `json:"spaces_two_agents"`
	ConnectionsReading int64   `json:"connections_reading"`
	ConnectionsSeen    int64   `json:"connections_seen"`
	Coverage           float64 `json:"coverage"`
}

func parseMoment(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.DateOnly, s)
	return t.UTC(), err
}

// render writes the gate table, the cohorts, review health and the north
// star.
func render(out io.Writer, m ledger.ProductMetrics, ns ledger.ReadMetrics) error {
	p := &printer{w: out}
	p.line("Memax V2 gate metrics")
	p.line("People who signed up from %s up to %s, as of %s.", m.From.Format(time.DateOnly), m.To.Format(time.DateOnly),
		m.AsOf.Format("2006-01-02 15:04 MST"))
	p.line("First session: %g hours after signup. Only people who reached a V2 space count; staff are left out.", m.FirstSessionHours)
	p.line("")

	p.line("Gates, judged on new people (from V1 beside them)")
	t := p.table()
	t.row("PHASE", "GATE", "BAR", "NEW", "STATUS", "FROM V1")
	names := map[string]string{"activation": "2+ agents and a compile in the first session",
		"first_file": "first file, median from init's import", "week4_keeping": "activated people keeping in week 4",
		"team_pull": "a teammate within 60 days"}
	for _, g := range m.PhaseGates() {
		bar, value, v1 := pct(g.Bar), "–", "–"
		if g.Name == "first_file" {
			bar = dur(&g.Bar)
			if g.Value != nil {
				value = fmt.Sprintf("%s (%d/%d under 5m)", dur(g.Value), g.Numerator, g.Denominator)
			}
			if g.FromV1 != nil {
				v1 = dur(g.FromV1)
			}
		} else {
			if g.Value != nil {
				value = fmt.Sprintf("%d/%d (%s)", g.Numerator, g.Denominator, pct(*g.Value))
			}
			if g.FromV1 != nil {
				v1 = pct(*g.FromV1)
			}
		}
		t.row(fmt.Sprint(g.Phase), names[g.Name], bar, value, string(g.Status), v1)
	}
	t.flush()
	p.line("")

	p.line("Cohorts by signup week (first-session counts are of closed sessions)")
	t = p.table()
	t.row("WEEK", "COHORT", "PEOPLE", "CLOSED", "2+ AGENTS", "2+ KINDS", "COMPILED", "ACTIVATED", "READ",
		"FIRST FILE p50", "p90", "UNDER 5m", "SIGNUP TO FILE", "WEEK 4", "TEAMMATE")
	cohort := func(week string, c ledger.CohortMetrics) {
		act := fmt.Sprint(c.Activated)
		if r, ok := c.ActivationRate(); ok {
			act = fmt.Sprintf("%d (%s)", c.Activated, pct(r))
		}
		t.row(week, string(c.Kind), fmt.Sprint(c.People), fmt.Sprint(c.SessionsClosed), fmt.Sprint(c.TwoConnections),
			fmt.Sprint(c.TwoAgentKinds), fmt.Sprint(c.Compiled), act, fmt.Sprint(c.AgentsRead),
			dur(c.FirstFileP50Seconds), dur(c.FirstFileP90Seconds), fmt.Sprintf("%d/%d", c.FirstFilesUnder5m, c.FirstFiles),
			dur(c.SignupToFileP50Seconds), fmt.Sprintf("%d/%d", c.Retained, c.RetentionEligible),
			fmt.Sprintf("%d/%d", c.TeamPulled, c.TeamEligible))
	}
	for _, c := range m.Cohorts {
		cohort(c.Week.Format(time.DateOnly), c)
	}
	for _, c := range m.Totals {
		cohort("all", c)
	}
	t.flush()
	p.line("")

	p.line("Review health, by the week proposals were made")
	t = p.table()
	t.row("WEEK", "PROPOSALS", "KEPT", "REJECTED", "FOLDED", "FORGOTTEN", "OPEN", "REJECT RATE", "TO DECISION p50", "p90")
	review := func(week string, r ledger.ReviewHealth) {
		rate := "–"
		if v, ok := r.RejectRate(); ok {
			rate = pct(v)
		}
		t.row(week, fmt.Sprint(r.Proposals), fmt.Sprint(r.Kept), fmt.Sprint(r.Rejected), fmt.Sprint(r.Folded),
			fmt.Sprint(r.Forgotten), fmt.Sprint(r.Open), rate, dur(r.DecisionP50Seconds), dur(r.DecisionP90Seconds))
	}
	for _, r := range m.Review {
		review(r.Week.Format(time.DateOnly), r)
	}
	review("all", m.ReviewTotal)
	t.flush()
	p.line("")

	p.line("North star, the week ending %s: %d of %d spaces read were read by 2+ agents; coverage %d/%d connections (%s).",
		ns.Day.Format(time.DateOnly), ns.SpacesTwoAgents, ns.SpacesRead, ns.ConnectionsReading, ns.ConnectionsSeen,
		pct(ns.Coverage()))
	return p.err
}

type printer struct {
	w   io.Writer
	err error
}

func (p *printer) line(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format+"\n", args...)
	}
}

type table struct {
	p  *printer
	tw *tabwriter.Writer
}

func (p *printer) table() *table {
	return &table{p: p, tw: tabwriter.NewWriter(p.w, 0, 0, 2, ' ', 0)}
}

func (t *table) row(cells ...string) {
	if t.p.err == nil {
		_, t.p.err = fmt.Fprintln(t.tw, strings.Join(cells, "\t"))
	}
}

func (t *table) flush() {
	if t.p.err == nil {
		t.p.err = t.tw.Flush()
	}
}

func pct(v float64) string { return fmt.Sprintf("%.0f%%", 100*v) }

// dur writes seconds as 3m30s, 1h04m or 3d01h; – when there are none.
func dur(s *float64) string {
	if s == nil {
		return "–"
	}
	d := time.Duration(math.Round(*s)) * time.Second
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%02dh", int(d.Hours())/24, int(d.Hours())%24)
}

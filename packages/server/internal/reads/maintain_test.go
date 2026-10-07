package reads_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/ledgertest"
	"github.com/MemaxLabs/memax/packages/server/internal/reads"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// The daily job logs the product metrics of the last ten signup weeks
// beside the north star: one line per cohort, the totals, review health
// and each phase gate, as counts and seconds.
func TestMaintainReportsProductMetrics(t *testing.T) {
	t.Parallel()
	_, pool := testdb.Acquire(t)
	fx := ledgertest.SeedGateFixture(t, pool)
	var out bytes.Buffer
	w := &reads.MaintainWorker{
		Ledger: ledger.New(pool, ledger.WithClock(func() time.Time { return fx.AsOf })),
		Log:    slog.New(slog.NewJSONHandler(&out, nil)),
		Now:    func() time.Time { return fx.AsOf },
	}
	if err := w.Work(context.Background(), &river.Job[reads.MaintainArgs]{}); err != nil {
		t.Fatal(err)
	}
	lines := map[string][]map[string]any{}
	for _, raw := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var l map[string]any
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if name, ok := l["metric"].(string); ok {
			lines[name] = append(lines[name], l)
		}
	}
	// Ten weeks back from Saturday Oct 3 reach Monday Jul 27, so Old (Jul
	// 28, activated, no teammate, no keep in week 4) is in this time.
	if n := len(lines["north_star"]); n != 1 {
		t.Errorf("north star lines: %d", n)
	}
	if n := len(lines["product_cohort"]); n != 6 {
		t.Errorf("cohort lines: %d, want 5 new weeks and 1 from V1", n)
	}
	if n := len(lines["product_cohort_total"]); n != 2 {
		t.Errorf("total lines: %d", n)
	}
	if n := len(lines["review_health"]); n != 2 || len(lines["review_health_total"]) != 1 {
		t.Errorf("review lines: %d", n)
	}
	gates := map[string]map[string]any{}
	for _, g := range lines["phase_gate"] {
		gates[g["gate"].(string)] = g
	}
	for name, want := range map[string][3]any{
		"activation":    {"fail", 5.0, 10.0},
		"first_file":    {"pass", 7.0, 8.0},
		"week4_keeping": {"pass", 2.0, 4.0},
		"team_pull":     {"pass", 1.0, 3.0},
	} {
		g := gates[name]
		if g == nil || g["status"] != want[0] || g["numerator"] != want[1] || g["denominator"] != want[2] {
			t.Errorf("%s: %v, want %v", name, g, want)
		}
	}
	for _, l := range lines["product_cohort"] {
		for k := range l {
			if strings.Contains(k, "id") {
				t.Errorf("a cohort line names %s", k)
			}
		}
	}
}

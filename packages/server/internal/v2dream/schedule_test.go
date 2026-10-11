package v2dream

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// Dream runs in the owner's local night, in their zone: never "Pacific".
// From Nov 1, 2026 British Columbia stays on UTC−7 (tzdata 2026c) while
// Los Angeles goes back to UTC−8, so their nights differ by an hour.
func TestNextSlotIsTheOwnersLocalNight(t *testing.T) {
	t.Parallel()
	cases := []struct {
		zone, after, want string
	}{
		// Winter 2026: Vancouver UTC−7, Los Angeles UTC−8.
		{"America/Vancouver", "2026-12-15T05:00:00Z", "2026-12-15T10:00:00Z"},
		{"America/Los_Angeles", "2026-12-15T05:00:00Z", "2026-12-15T11:00:00Z"},
		// Summer: both UTC−7.
		{"America/Vancouver", "2026-07-15T05:00:00Z", "2026-07-15T10:00:00Z"},
		{"America/Los_Angeles", "2026-07-15T05:00:00Z", "2026-07-15T10:00:00Z"},
		// After tonight's slot: tomorrow's.
		{"America/Vancouver", "2026-12-15T10:00:00Z", "2026-12-16T10:00:00Z"},
		{"Asia/Shanghai", "2026-10-07T00:00:00Z", "2026-10-07T19:00:00Z"},
		{"Europe/Berlin", "2026-10-24T12:00:00Z", "2026-10-25T02:00:00Z"}, // the night clocks go back: 03:00 CET
		{"UTC", "2026-10-07T03:00:00Z", "2026-10-08T03:00:00Z"},
		{"Not/AZone", "2026-10-07T01:00:00Z", "2026-10-07T03:00:00Z"}, // unknown: UTC until Memax learns theirs
	}
	for _, c := range cases {
		if got := NextSlot(utc(c.after), c.zone, Nightly, 3, time.Monday); !got.Equal(utc(c.want)) {
			t.Errorf("%s after %s: %s, want %s", c.zone, c.after, got.Format(time.RFC3339), c.want)
		}
	}
	// The spring-forward night in Los Angeles (Mar 8, 2026): 03:00 exists, PDT.
	if got := NextSlot(utc("2026-03-08T05:00:00Z"), "America/Los_Angeles", Nightly, 3, time.Monday); !got.Equal(utc("2026-03-08T10:00:00Z")) {
		t.Errorf("spring forward: %s", got)
	}
	// Weekly: the next Monday night. Oct 7, 2026 is a Wednesday.
	if got := NextSlot(utc("2026-10-07T12:00:00Z"), "America/Vancouver", Weekly, 3, time.Monday); !got.Equal(utc("2026-10-12T10:00:00Z")) {
		t.Errorf("weekly: %s", got)
	}
	if got := LatestSlot(utc("2026-10-07T12:00:00Z"), "America/Vancouver", Weekly, 3, time.Monday); !got.Equal(utc("2026-10-05T10:00:00Z")) {
		t.Errorf("latest weekly: %s", got)
	}
	if got := LatestSlot(utc("2026-12-15T09:59:00Z"), "America/Vancouver", Nightly, 3, time.Monday); !got.Equal(utc("2026-12-14T10:00:00Z")) {
		t.Errorf("latest nightly: %s", got)
	}
}

// The sweep's decision: a new space waits for its first night; a due one
// runs once for the latest night it missed, however many; the owner's
// plan and busiest spaces set the cadence.
func TestDecide(t *testing.T) {
	t.Parallel()
	cfg := Config{}.withDefaults()
	now := utc("2026-10-07T12:00:00Z")
	s := ledger.SweepSpace{SpaceID: uuid.New(), TimeZone: "America/Vancouver", BusyRank: 1}

	d := cfg.Decide(now, s)
	if d.Slot != nil || !d.DueAt.Equal(utc("2026-10-08T10:00:00Z")) || d.Cadence != "nightly" {
		t.Fatalf("a new space: %+v", d)
	}
	// Due three nights ago, never run: once, for last night.
	s.HasSchedule, s.Cadence, s.ScheduleTimeZone, s.DueAt = true, "nightly", "America/Vancouver", utc("2026-10-04T10:00:00Z")
	d = cfg.Decide(now, s)
	if d.Slot == nil || !d.Slot.Equal(utc("2026-10-07T10:00:00Z")) || !d.DueAt.Equal(utc("2026-10-08T10:00:00Z")) {
		t.Fatalf("catch-up: %+v", d)
	}
	// That slot already queued: nothing more.
	last := *d.Slot
	s.LastSlot = &last
	if d = cfg.Decide(now, s); d.Slot != nil {
		t.Errorf("queued twice: %+v", d)
	}
	// Pro: nightly on the five busiest, weekly for the rest; Free weekly.
	s.BusyRank = 6
	if d = cfg.Decide(now, s); d.Cadence != "weekly" {
		t.Errorf("the sixth busiest space on Pro: %s", d.Cadence)
	}
	free := Config{Plan: PlanFree}.withDefaults()
	s.BusyRank = 1
	if d = free.Decide(now, s); d.Cadence != "weekly" || !d.DueAt.Equal(utc("2026-10-12T10:00:00Z")) {
		t.Errorf("free: %+v", d)
	}
	// A zone Memax can't load is UTC, said so.
	s.TimeZone = "Mars/Olympus"
	if d = cfg.Decide(now, s); d.TimeZone != "UTC" {
		t.Errorf("unknown zone kept as %q", d.TimeZone)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Parallel()
	env := map[string]string{"DREAM_MODEL": "off", "DREAM_PLAN": "free", "DREAM_LOCAL_HOUR": "4", "DREAM_WEEKLY_DAY": "sun",
		"DREAM_ZDR": "false", "DREAM_DRY_RUN": "1", "DREAM_FADE_AFTER_DAYS": "30", "DREAM_MAX_CALLS": "12"}
	c := ConfigFromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if c.Primary.Enabled() || c.Plan != PlanFree || c.LocalHour != 4 || c.weekday() != time.Sunday || c.ZeroDataRetention ||
		!c.DryRun || c.FadeAfter != 30*24*time.Hour || c.MaxCalls != 12 || c.Fallback.Model != DefaultFallbackModel {
		t.Errorf("%+v", c)
	}
	d := ConfigFromEnv(func(string) (string, bool) { return "", false })
	if d.Primary.Model != DefaultPrimaryModel || d.Plan != PlanPro || d.LocalHour != 3 || !d.ZeroDataRetention || !d.Email ||
		d.ProCadence != Nightly || d.FreeCadence != Weekly {
		t.Errorf("defaults %+v", d)
	}
	// D14: each tier on its pinned zero-retention hosts, at fp8 or wider,
	// and at temperature 0 where the model takes one.
	if !slices.Equal(d.Primary.Routing.Providers, anthropic.DefaultProviders[DefaultPrimaryModel]) ||
		!slices.Equal(d.Strong.Routing.Providers, anthropic.DefaultProviders[DefaultStrongModel]) ||
		slices.Contains(d.Primary.Routing.Quantizations, "fp4") || !slices.Contains(d.Primary.Routing.Quantizations, "fp8") {
		t.Errorf("default routing %+v / %+v", d.Primary.Routing, d.Strong.Routing)
	}
	if d.Primary.Temperature == nil || *d.Primary.Temperature != 0 || d.Fallback.Temperature == nil || d.Strong.Temperature != nil {
		t.Errorf("default temperatures %v %v %v", d.Primary.Temperature, d.Fallback.Temperature, d.Strong.Temperature)
	}
	// Named hosts and a floor win; without zero retention nothing is pinned.
	pinned := ConfigFromEnv(func(k string) (string, bool) {
		v, ok := map[string]string{"DREAM_PROVIDERS": "Baseten, together", "DREAM_MIN_QUANTIZATION": "bf16",
			"DREAM_STRONG_TEMPERATURE": "0.2"}[k]
		return v, ok
	})
	// Sonnet 5.5 takes no temperature but its own, so the named one isn't
	// sent (anthropic.ModelParams).
	if !slices.Equal(pinned.Primary.Routing.Providers, []string{"baseten", "together"}) ||
		slices.Contains(pinned.Primary.Routing.Quantizations, "fp8") || pinned.Strong.Temperature != nil {
		t.Errorf("pinned %+v %v", pinned.Primary.Routing, pinned.Strong.Temperature)
	}
	if len(c.Primary.Routing.Providers)+len(c.Fallback.Routing.Providers)+len(c.Fallback.Routing.Quantizations) != 0 {
		t.Errorf("DREAM_ZDR=false still pinned %+v", c.Fallback.Routing)
	}
	// A tier on Claude Haiku 5.5 follows its rules, as the judge's do: no
	// temperature, Vertex, effort medium unless DREAM_*_THINKING names one.
	haiku := ConfigFromEnv(func(k string) (string, bool) {
		v, ok := map[string]string{"DREAM_FALLBACK_MODEL": "anthropic/claude-haiku-5.5", "DREAM_FALLBACK_TEMPERATURE": "0",
			"DREAM_THINKING": "off"}[k]
		return v, ok
	})
	if fb := haiku.Fallback; fb.Temperature != nil || !slices.Equal(fb.Routing.Providers, []string{"google-vertex"}) ||
		fb.Thinking != (anthropic.Thinking{Mode: anthropic.ThinkingAdaptive, Effort: "medium"}) ||
		haiku.Primary.Thinking != (anthropic.Thinking{Mode: anthropic.ThinkingDisabled}) {
		t.Errorf("Haiku 5.5 fallback %+v, primary thinking %+v", fb, haiku.Primary.Thinking)
	}
}

type modelFunc func(context.Context, judge.Call) (string, error)

func (f modelFunc) Complete(ctx context.Context, c judge.Call) (string, error) { return f(ctx, c) }

// A tier that declines (stop_reason refusal) goes straight to the next:
// Dream's own calls don't ask it again, and the edition's usage counts the
// refusal beside the failure.
func TestStructuredFallsThroughARefusal(t *testing.T) {
	t.Parallel()
	e := &Engine{cfg: Config{Primary: judge.Tier{Model: "p"}, Fallback: judge.Tier{Model: "f", Strict: true}}.withDefaults()}
	var tiers []string
	inner := modelFunc(func(_ context.Context, c judge.Call) (string, error) {
		tiers = append(tiers, c.Tier.Name)
		if c.Tier.Name == "primary" {
			return "", &anthropic.RefusalError{Model: "p", Category: "general_harms"}
		}
		return `{"ok":true}`, nil
	})
	schema := mustSchema("refusal-test.json", `{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"}}}`)
	u := &usage{}
	var out struct {
		OK bool `json:"ok"`
	}
	tier, err := e.structured(withUsage(context.Background(), u), &countingModel{inner: inner, budget: 10}, "fold", "s", "p",
		schema, &out)
	if err != nil || tier != "fallback" || !out.OK {
		t.Fatalf("tier %q, out %+v, err %v", tier, out, err)
	}
	if !slices.Equal(tiers, []string{"primary", "fallback"}) {
		t.Errorf("calls = %v; want the primary once, then the fallback", tiers)
	}
	if got := u.snapshot(); got.Calls != 2 || got.Failures != 1 || got.Refusals != 1 {
		t.Errorf("usage = %+v", got)
	}
}

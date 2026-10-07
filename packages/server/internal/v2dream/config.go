package v2dream

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/judge"
)

// Plan is the plan whose cadence a space dreams on (D9). V2 billing lands
// in Phase 3 (3.4); until then every owner is on DREAM_PLAN, and the alpha
// is free, so the default is Pro's cadence (as Ask has no limit during the
// alpha).
type Plan string

// The plans.
const (
	PlanFree Plan = "free"
	PlanPro  Plan = "pro"
)

// Cadence is how often a space dreams.
type Cadence string

// The cadences.
const (
	Nightly Cadence = "nightly"
	Weekly  Cadence = "weekly"
)

// Config configures Dream. Everything is explicit configuration, read once
// at startup (ConfigFromEnv); model tiers are never inferred from a model's
// name.
type Config struct {
	// Primary, Fallback and Strong are the model tiers, as the judge's are:
	// the primary (DeepSeek V4.1 Flash on zero-retention hosts), a
	// strict-schema fallback, and the strong tier that confirms any verdict
	// touching a decision in force. An empty primary model runs Dream
	// without a model: folds, new facts, model dedupe, conflicts and the
	// Brief wait; exact repeats, stale and fade still run.
	Primary, Fallback, Strong judge.Tier
	// ZeroDataRetention routes every call to zero-retention providers only
	// (plan 25 D14).
	ZeroDataRetention bool
	// CallTimeout bounds one model call.
	CallTimeout time.Duration
	// DryRun plans editions and logs them (counts, never words) without
	// publishing anything.
	DryRun bool

	// Plan is every owner's plan until V2 billing exists.
	Plan Plan
	// FreeCadence and ProCadence are the plans' cadences (D9: weekly on
	// Free, nightly on Pro for its ProSpaces busiest spaces, weekly for the
	// rest).
	FreeCadence, ProCadence Cadence
	ProSpaces               int
	// LocalHour is the hour of the owner's local night Dream runs at, 1 to
	// 23 (0 is the default, 3), and WeeklyDay the day a weekly space runs on
	// (Monday).
	LocalHour int
	WeeklyDay time.Weekday
	// ManualPerDay caps run-now per space per rolling day on Pro, and
	// ManualPerWeek on Free.
	ManualPerDay, ManualPerWeek int

	// SweepInterval is how often the catch-up sweep looks for due spaces.
	SweepInterval time.Duration
	// MaxNotes is how many notes an edition reads (the rest wait), MaxCalls
	// how many model calls a run makes at most, MaxFades how many memories
	// one edition fades, and BriefMaxOps how many small changes it makes to
	// the Brief.
	MaxNotes, MaxCalls, MaxFades, BriefMaxOps int
	// FadeAfter is how long a memory goes unread before it fades (60 days).
	FadeAfter time.Duration
	// UndoWindow is how long an action can be undone (30 days).
	UndoWindow time.Duration

	// The bars an action needs, as the model's confidence.
	FoldBar, FactBar, DuplicateBar, ConflictBar, FactConflictBar float64

	// Email turns the morning email on; From is its sender.
	Email bool
	From  string

	Log *slog.Logger
}

// The defaults.
const (
	DefaultPrimaryModel  = judge.DefaultPrimaryModel
	DefaultFallbackModel = judge.DefaultFallbackModel
	DefaultStrongModel   = judge.DefaultStrongModel
	DefaultCallTimeout   = 30 * time.Second
	DefaultLocalHour     = 3
	DefaultProSpaces     = 5
	DefaultSweepInterval = 5 * time.Minute
	DefaultMaxNotes      = 60
	DefaultMaxCalls      = 40
	DefaultMaxFades      = 50
	DefaultBriefMaxOps   = 5
	DefaultFadeAfter     = 60 * 24 * time.Hour
	DefaultUndoWindow    = 30 * 24 * time.Hour
	DefaultFrom          = "Dream at Memax <noreply@memax.app>"
)

func (c Config) withDefaults() Config {
	if c.CallTimeout <= 0 {
		c.CallTimeout = DefaultCallTimeout
	}
	if c.Plan != PlanFree {
		c.Plan = PlanPro
	}
	if c.FreeCadence != Nightly {
		c.FreeCadence = Weekly
	}
	if c.ProCadence != Weekly {
		c.ProCadence = Nightly
	}
	if c.ProSpaces <= 0 {
		c.ProSpaces = DefaultProSpaces
	}
	if c.LocalHour <= 0 || c.LocalHour > 23 {
		c.LocalHour = DefaultLocalHour
	}
	if c.ManualPerDay <= 0 {
		c.ManualPerDay = 3
	}
	if c.ManualPerWeek <= 0 {
		c.ManualPerWeek = 1
	}
	if c.SweepInterval <= 0 {
		c.SweepInterval = DefaultSweepInterval
	}
	if c.MaxNotes <= 0 {
		c.MaxNotes = DefaultMaxNotes
	}
	if c.MaxCalls <= 0 {
		c.MaxCalls = DefaultMaxCalls
	}
	if c.MaxFades <= 0 {
		c.MaxFades = DefaultMaxFades
	}
	if c.BriefMaxOps <= 0 {
		c.BriefMaxOps = DefaultBriefMaxOps
	}
	if c.FadeAfter <= 0 {
		c.FadeAfter = DefaultFadeAfter
	}
	if c.UndoWindow <= 0 {
		c.UndoWindow = DefaultUndoWindow
	}
	if c.FoldBar <= 0 {
		c.FoldBar = 0.7
	}
	if c.FactBar <= 0 {
		c.FactBar = 0.7
	}
	if c.DuplicateBar <= 0 {
		c.DuplicateBar = judge.DefaultThresholds.Duplicate
	}
	if c.ConflictBar <= 0 {
		c.ConflictBar = judge.DefaultThresholds.Contradicts
	}
	if c.FactConflictBar <= 0 {
		c.FactConflictBar = 0.85
	}
	if c.From == "" {
		c.From = DefaultFrom
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	c.Primary.Name, c.Fallback.Name, c.Strong.Name = "primary", "fallback", "strong"
	if c.Primary.MaxTokens <= 0 {
		c.Primary.MaxTokens = 4000
	}
	if c.Fallback.MaxTokens <= 0 {
		c.Fallback.MaxTokens = 4000
	}
	if c.Strong.MaxTokens <= 0 {
		c.Strong.MaxTokens = 8000
	}
	return c
}

// judgeConfig is the judge's configuration on Dream's tiers, for the
// classifier Dream reuses for duplicates and conflicts.
func (c Config) judgeConfig() judge.Config {
	return judge.Config{Primary: c.Primary, Fallback: c.Fallback, Strong: c.Strong,
		ZeroDataRetention: c.ZeroDataRetention, CallTimeout: c.CallTimeout, Log: c.Log}
}

// ConfigFromEnv reads Dream's configuration through lookup (os.LookupEnv),
// once, at startup.
//
//	DREAM_MODEL            primary tier (default deepseek/deepseek-v4.1-flash; "off" runs Dream without a model)
//	DREAM_FALLBACK_MODEL   strict-schema fallback tier (default anthropic/claude-haiku-4.5; "off")
//	DREAM_STRONG_MODEL     tier for verdicts on decisions in force (default anthropic/claude-sonnet-5.5; "off")
//	DREAM_ZDR              zero-data-retention routing (default true)
//	DREAM_TIMEOUT_MS       one model call (default 30000)
//	DREAM_DRY_RUN          plan and log editions without publishing (default false)
//	DREAM_PLAN             the plan every owner dreams on until V2 billing: pro (default, the alpha) or free
//	DREAM_FREE_CADENCE     weekly (default) or nightly
//	DREAM_PRO_CADENCE      nightly (default) or weekly
//	DREAM_PRO_SPACES       Pro's nightly spaces per owner, the busiest (default 5)
//	DREAM_LOCAL_HOUR       the hour of the owner's local night, 1–23 (default 3)
//	DREAM_WEEKLY_DAY       the day weekly spaces run (default monday)
//	DREAM_MANUAL_PER_DAY   run-now per space per day on Pro (default 3)
//	DREAM_MANUAL_PER_WEEK  run-now per space per week on Free (default 1)
//	DREAM_SWEEP_INTERVAL   how often the sweep runs (default 5m)
//	DREAM_MAX_NOTES        notes one edition reads (default 60)
//	DREAM_MAX_CALLS        model calls one run makes (default 40)
//	DREAM_FADE_AFTER_DAYS  days unread before a memory fades (default 60)
//	DREAM_UNDO_DAYS        days an action can be undone (default 30)
//	DREAM_EMAIL            send the morning email (default true)
//	DREAM_EMAIL_FROM       its sender (default "Dream at Memax <noreply@memax.app>")
func ConfigFromEnv(lookup func(string) (string, bool)) Config {
	str := func(key string) (string, bool) {
		v, ok := lookup(key)
		v = strings.TrimSpace(v)
		return v, ok && v != ""
	}
	model := func(key, def string) string {
		v, ok := str(key)
		if !ok {
			return def
		}
		if strings.EqualFold(v, "off") || strings.EqualFold(v, "none") {
			return ""
		}
		return v
	}
	flag := func(key string, def bool) bool {
		v, ok := str(key)
		if !ok {
			return def
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			return def
		}
		return b
	}
	num := func(key string) int {
		v, ok := str(key)
		if !ok {
			return 0
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	c := Config{
		Primary:           judge.Tier{Model: model("DREAM_MODEL", DefaultPrimaryModel)},
		Fallback:          judge.Tier{Model: model("DREAM_FALLBACK_MODEL", DefaultFallbackModel), Strict: true},
		Strong:            judge.Tier{Model: model("DREAM_STRONG_MODEL", DefaultStrongModel), Strict: true},
		ZeroDataRetention: flag("DREAM_ZDR", true),
		DryRun:            flag("DREAM_DRY_RUN", false),
		Email:             flag("DREAM_EMAIL", true),
		ProSpaces:         num("DREAM_PRO_SPACES"),
		ManualPerDay:      num("DREAM_MANUAL_PER_DAY"),
		ManualPerWeek:     num("DREAM_MANUAL_PER_WEEK"),
		MaxNotes:          num("DREAM_MAX_NOTES"),
		MaxCalls:          num("DREAM_MAX_CALLS"),
		WeeklyDay:         time.Monday,
	}
	if ms := num("DREAM_TIMEOUT_MS"); ms > 0 {
		c.CallTimeout = time.Duration(ms) * time.Millisecond
	}
	if v, ok := str("DREAM_PLAN"); ok {
		c.Plan = Plan(strings.ToLower(v))
	}
	if v, ok := str("DREAM_FREE_CADENCE"); ok {
		c.FreeCadence = Cadence(strings.ToLower(v))
	}
	if v, ok := str("DREAM_PRO_CADENCE"); ok {
		c.ProCadence = Cadence(strings.ToLower(v))
	}
	if v, ok := str("DREAM_LOCAL_HOUR"); ok {
		if h, err := strconv.Atoi(v); err == nil && h >= 1 && h <= 23 {
			c.LocalHour = h
		}
	}
	if v, ok := str("DREAM_WEEKLY_DAY"); ok {
		for d := time.Sunday; d <= time.Saturday; d++ {
			if strings.EqualFold(v, d.String()) || strings.EqualFold(v, d.String()[:3]) {
				c.WeeklyDay = d
			}
		}
	}
	if v, ok := str("DREAM_SWEEP_INTERVAL"); ok {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Minute {
			c.SweepInterval = d
		}
	}
	if days := num("DREAM_FADE_AFTER_DAYS"); days > 0 {
		c.FadeAfter = time.Duration(days) * 24 * time.Hour
	}
	if days := num("DREAM_UNDO_DAYS"); days > 0 {
		c.UndoWindow = time.Duration(days) * 24 * time.Hour
	}
	if v, ok := str("DREAM_EMAIL_FROM"); ok {
		c.From = v
	}
	return c.withDefaults()
}

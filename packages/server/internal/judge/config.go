package judge

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// Tier is one model tier (plan 25 §4.3). Tiers are explicit
// configuration, never inferred from a model's name.
type Tier struct {
	// Name is primary, fallback or strong (ledger.Tier*).
	Name string
	// Model is the model slug the gateway knows ("deepseek/deepseek-v4.1-flash").
	// Empty disables the tier.
	Model string
	// Strict asks the provider to enforce the JSON Schema (structured
	// outputs). Tiers without it are prompted for JSON and validated.
	Strict bool
	// MaxTokens bounds the answer.
	MaxTokens int
}

// Enabled reports whether the tier has a model.
func (t Tier) Enabled() bool { return strings.TrimSpace(t.Model) != "" }

// Thresholds are the confidences an outcome needs. They err high: a
// verdict below them is recorded, and the proposal is reviewed as usual.
type Thresholds struct {
	// Duplicate folds a proposal into the memory it repeats.
	Duplicate float64
	// Contradicts flags a conflict with a decision in force.
	Contradicts float64
	// Supersede links an explicit change of a decision in force.
	Supersede float64
	// Updates links an update of a kept fact ("Updates M-0156").
	Updates float64
}

// DefaultThresholds are the bars the eval set (eval/judge) is scored at.
//
// Contradicts is 0.6, not the first guess of 0.8 (live eval, Oct 6, 2026,
// eval/judge/RESULTS.md): every verdict on a decision in force is
// confirmed by the strong tier, which across 5 pipeline runs (pairs.json
// three times, holdout.json twice) flagged nothing that wasn't a conflict
// at any confidence, but put 3 of the 28 planted conflicts at 0.55–0.72.
// At 0.8 the judge flagged 25 of 28; at 0.6, 27–28. The bar assumes the
// strong tier is on; without it the primary's false flags came at 0.8 or
// more, so the bar doesn't guard them either way.
var DefaultThresholds = Thresholds{Duplicate: 0.9, Contradicts: 0.6, Supersede: 0.8, Updates: 0.7}

// Config configures the judge.
type Config struct {
	// Primary is the first tier (DeepSeek V4.1 Flash on zero-retention
	// hosts). Empty Model disables stage 1: the judge runs stage 0 only.
	Primary Tier
	// Fallback is the strict-schema tier tried when the primary's answer
	// is invalid twice or empty (GPT-6 Luna or Claude Haiku 4.5).
	Fallback Tier
	// Strong confirms every verdict that touches a decision in force
	// (Claude Sonnet 5.5). Without it, the primary's verdict stands.
	Strong Tier
	// ZeroDataRetention routes every call to zero-retention providers only
	// (OpenRouter provider.zdr, plan 25 D14).
	ZeroDataRetention bool
	// CallTimeout bounds one model call.
	CallTimeout time.Duration
	// Conditions makes the judge propose "stays true while" conditions
	// from a proposal's sources. Off by default: stale detection, the only
	// reader, is Phase 3, and a wrong condition would flag a fact stale.
	Conditions bool
	// RejectedWithin is the re-proposal window (90 days).
	RejectedWithin time.Duration
	// Candidates is how many kept memories the hybrid set holds (10).
	Candidates int
	// VectorFloor is the cosine similarity a vector candidate needs (plan
	// §5.8: about 0.65; Graphiti uses a loose 0.6 before the model
	// decides). Calibrate it on Voyage embeddings of eval/judge's pairs
	// (TestVectorFloorCalibration, live). 0 is DefaultVectorFloor.
	VectorFloor float64
	// Thresholds default to DefaultThresholds.
	Thresholds Thresholds
	Log        *slog.Logger
}

// The defaults.
const (
	DefaultPrimaryModel  = "deepseek/deepseek-v4.1-flash"
	DefaultFallbackModel = "anthropic/claude-haiku-4.5"
	DefaultStrongModel   = "anthropic/claude-sonnet-5.5"
	DefaultCallTimeout   = 12 * time.Second
	DefaultCandidates    = 10
	DefaultRejectWindow  = 90 * 24 * time.Hour
	DefaultVectorFloor   = 0.65
)

func (c Config) withDefaults() Config {
	if c.CallTimeout <= 0 {
		c.CallTimeout = DefaultCallTimeout
	}
	if c.RejectedWithin <= 0 {
		c.RejectedWithin = DefaultRejectWindow
	}
	if c.Candidates <= 0 {
		c.Candidates = DefaultCandidates
	}
	if c.VectorFloor <= 0 {
		c.VectorFloor = DefaultVectorFloor
	}
	if c.Thresholds == (Thresholds{}) {
		c.Thresholds = DefaultThresholds
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	c.Primary.Name, c.Fallback.Name, c.Strong.Name = ledger.TierPrimary, ledger.TierFallback, ledger.TierStrong
	if c.Primary.MaxTokens <= 0 {
		c.Primary.MaxTokens = 2500
	}
	if c.Fallback.MaxTokens <= 0 {
		c.Fallback.MaxTokens = 2500
	}
	if c.Strong.MaxTokens <= 0 {
		// The strong tier thinks (adaptive thinking on Claude), and its
		// thinking counts against the budget.
		c.Strong.MaxTokens = 8000
	}
	return c
}

// ConfigFromEnv reads the judge's configuration from environment
// variables through lookup (os.LookupEnv). Call it once, at startup, in
// the worker's composition root.
//
//	JUDGE_MODEL           primary tier (default deepseek/deepseek-v4.1-flash; "off" disables stage 1)
//	JUDGE_FALLBACK_MODEL  strict-schema fallback tier (default anthropic/claude-haiku-4.5; "off" disables)
//	JUDGE_STRONG_MODEL    tier for verdicts on decisions in force (default anthropic/claude-sonnet-5.5; "off")
//	JUDGE_ZDR             zero-data-retention routing (default true)
//	JUDGE_CONDITIONS      propose "stays true while" conditions (default false)
//	JUDGE_TIMEOUT_MS      one model call (default 12000)
//	JUDGE_VECTOR_FLOOR    cosine similarity a vector candidate needs (default 0.65)
func ConfigFromEnv(lookup func(string) (string, bool)) Config {
	model := func(key, def string) string {
		v, ok := lookup(key)
		if !ok {
			return def
		}
		v = strings.TrimSpace(v)
		if strings.EqualFold(v, "off") || strings.EqualFold(v, "none") {
			return ""
		}
		return v
	}
	flag := func(key string, def bool) bool {
		v, ok := lookup(key)
		if !ok || strings.TrimSpace(v) == "" {
			return def
		}
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return def
		}
		return b
	}
	c := Config{
		Primary:           Tier{Model: model("JUDGE_MODEL", DefaultPrimaryModel)},
		Fallback:          Tier{Model: model("JUDGE_FALLBACK_MODEL", DefaultFallbackModel), Strict: true},
		Strong:            Tier{Model: model("JUDGE_STRONG_MODEL", DefaultStrongModel), Strict: true},
		ZeroDataRetention: flag("JUDGE_ZDR", true),
		Conditions:        flag("JUDGE_CONDITIONS", false),
	}
	if v, ok := lookup("JUDGE_TIMEOUT_MS"); ok {
		if ms, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && ms > 0 {
			c.CallTimeout = time.Duration(ms) * time.Millisecond
		}
	}
	if v, ok := lookup("JUDGE_VECTOR_FLOOR"); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && f > 0 && f <= 1 {
			c.VectorFloor = f
		}
	}
	return c
}

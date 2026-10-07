// Package trust is what Settings › Security says about how this Memax
// runs (plan 25 §5.16, D14): where the data lives, which outside
// processors see a memory's words and what they keep, and how long
// backups hold what Forget removed. It is read once, at start, from the
// same configuration the processes run on, so the page can't promise more
// than the deployment does: a tier that's off isn't listed, the hosts are
// the ones each tier is pinned to, and Voyage's retention stays
// "unconfirmed" until VOYAGE_RETENTION says the account's opt-out is on.
//
// It holds no words of anyone's: only names, regions, model slugs and
// hosts.
package trust

import (
	"slices"
	"strconv"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/ask"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/rerank"
	"github.com/MemaxLabs/memax/packages/server/internal/v2dream"
	"github.com/MemaxLabs/memax/packages/server/internal/v2index"
)

// Posture is what the Security page shows about this deployment.
type Posture struct {
	// Residency is where each kind of data lives.
	Residency []Place `json:"residency"`
	// Processors are the outside services that see a memory's words.
	Processors []Processor `json:"processors"`
	// BackupDays is how long database backups keep what Forget removed
	// (V2_BACKUP_RETENTION_DAYS; the forget ledger is re-applied after any
	// restore).
	BackupDays int `json:"backup_days"`
}

// What a place holds.
const (
	// HoldsDatabase: memories, receipts, sources and settings.
	HoldsDatabase = "database"
	// HoldsCompute: the API and the worker, which process it.
	HoldsCompute = "compute"
	// HoldsObjects: receipt checkpoints, compiled files and the forget ledger.
	HoldsObjects = "objects"
	// HoldsEdge: the web app and the compile service, which keep nothing
	// between requests.
	HoldsEdge = "edge"
)

// Holds lists what a place can hold, in the page's order.
var Holds = []string{HoldsDatabase, HoldsCompute, HoldsObjects, HoldsEdge}

// Place is where one kind of data lives.
type Place struct {
	Holds string `json:"holds"`
	// Provider is a short name: neon, fly, r2, cloudflare (or any other a
	// self-hosted server names).
	Provider string `json:"provider"`
	// Region is the provider's own name for it ("us-west-2", "sjc"); empty
	// when the provider places it.
	Region string `json:"region,omitempty"`
}

// DefaultResidency is Memax's cloud (plan 25 §5.17): Neon Postgres in AWS
// us-west-2, the API and worker on Fly in sjc, objects in Cloudflare R2,
// and the web app and compile service on Cloudflare Workers.
const DefaultResidency = "database=neon:us-west-2,compute=fly:sjc,objects=r2,edge=cloudflare"

// The retention a processor keeps a memory's words under.
const (
	// RetentionZero: nothing kept (zero-data-retention endpoints only).
	RetentionZero = "zero"
	// RetentionUnconfirmed: the provider keeps inputs unless the account
	// opts out, and Memax hasn't confirmed its opt-out.
	RetentionUnconfirmed = "unconfirmed"
	// RetentionProvider: kept under the provider's own terms.
	RetentionProvider = "provider_terms"
)

// Retentions lists the retention values.
var Retentions = []string{RetentionZero, RetentionUnconfirmed, RetentionProvider}

// The processors.
const (
	ProcessorOpenRouter = "openrouter"
	ProcessorAnthropic  = "anthropic"
	ProcessorVoyage     = "voyage"
	ProcessorResend     = "resend"
)

// ProcessorNames lists them.
var ProcessorNames = []string{ProcessorOpenRouter, ProcessorAnthropic, ProcessorVoyage, ProcessorResend}

// What a model is used for.
const (
	UseJudge         = "judge"
	UseJudgeFallback = "judge_fallback"
	UseJudgeStrong   = "judge_strong"
	UseAsk           = "ask"
	UseDream         = "dream"
	UseDreamFallback = "dream_fallback"
	UseDreamStrong   = "dream_strong"
	UseEmbeddings    = "embeddings"
	UseQueries       = "queries"
	UseRerank        = "rerank"
	UseEmail         = "email"
)

// Uses lists them.
var Uses = []string{UseJudge, UseJudgeFallback, UseJudgeStrong, UseAsk, UseDream, UseDreamFallback, UseDreamStrong,
	UseEmbeddings, UseQueries, UseRerank, UseEmail}

// Processor is an outside service that sees a memory's words.
type Processor struct {
	Name string `json:"name"`
	// Retention is what it keeps (Retention*).
	Retention string `json:"retention"`
	// Uses are what Memax sends it, each with its model and hosts.
	Uses []Use `json:"uses"`
}

// Use is one thing Memax sends a processor.
type Use struct {
	Use string `json:"use"`
	// Model is the model slug; empty for email.
	Model string `json:"model,omitempty"`
	// Hosts are the zero-retention hosts the call is pinned to, in order
	// (OpenRouter's provider.only); empty when the gateway chooses.
	Hosts []string `json:"hosts,omitempty"`
	// MinPrecision is the lowest precision those hosts may run ("fp8").
	MinPrecision string `json:"min_precision,omitempty"`
	// ZeroRetention: the call is routed to zero-retention endpoints only.
	ZeroRetention bool `json:"zero_retention"`
}

// FromEnv reads the posture through lookup (os.LookupEnv):
//
//	V2_DATA_RESIDENCY         where data lives, "holds=provider[:region],…" (default DefaultResidency)
//	V2_BACKUP_RETENTION_DAYS  the database's restore window (default 7, Neon's)
//	VOYAGE_RETENTION          "zero" once Voyage's zero-retention opt-out is confirmed for the
//	                          account; anything else (the default) says it is unconfirmed
//
// and the model tiers as the processes read them: the judge's, Ask's and
// Dream's (JUDGE_*, ASK_*, DREAM_*, with ANTHROPIC_API_KEY and
// ANTHROPIC_BASE_URL), V2 embeddings and rerank (VOYAGE_API_KEY,
// V2_EMBED_MODEL, V2_EMBED_QUERY_MODEL, V2_RERANK_MODEL), and the morning
// email (DREAM_EMAIL with RESEND_API_KEY).
func FromEnv(lookup func(string) (string, bool)) Posture {
	get := func(key string) string {
		v, _ := lookup(key)
		return strings.TrimSpace(v)
	}
	p := Posture{Residency: ParseResidency(get("V2_DATA_RESIDENCY")), BackupDays: ledger.DefaultBackupDays}
	if n, err := strconv.Atoi(get("V2_BACKUP_RETENTION_DAYS")); err == nil && n > 0 {
		p.BackupDays = n
	}

	// Model calls go through the shared Anthropic-compatible client: to
	// OpenRouter, whose provider.zdr routing is the zero-retention promise,
	// or to Anthropic itself.
	if get("ANTHROPIC_API_KEY") != "" {
		gateway := Processor{Name: ProcessorAnthropic, Retention: RetentionProvider}
		openRouter := strings.Contains(strings.ToLower(get("ANTHROPIC_BASE_URL")), "openrouter.ai")
		if openRouter {
			gateway.Name = ProcessorOpenRouter
		}
		tier := func(use string, t judge.Tier, zdr bool) {
			if t.Model == "" {
				return
			}
			gateway.Uses = append(gateway.Uses, modelUse(use, t.Model, t.Routing, openRouter && zdr))
		}
		// The fallback and strong tiers only follow a primary's call: with
		// the primary off, neither is ever called.
		if jc := judge.ConfigFromEnv(lookup); jc.Primary.Model != "" {
			tier(UseJudge, jc.Primary, jc.ZeroDataRetention)
			tier(UseJudgeFallback, jc.Fallback, jc.ZeroDataRetention)
			tier(UseJudgeStrong, jc.Strong, jc.ZeroDataRetention)
		}
		if ac := ask.ConfigFromEnv(lookup); ac.Model != "" {
			gateway.Uses = append(gateway.Uses, modelUse(UseAsk, ac.Model, ac.Routing, openRouter && ac.ZeroDataRetention))
		}
		if dc := v2dream.ConfigFromEnv(lookup); dc.Primary.Model != "" {
			tier(UseDream, dc.Primary, dc.ZeroDataRetention)
			tier(UseDreamFallback, dc.Fallback, dc.ZeroDataRetention)
			tier(UseDreamStrong, dc.Strong, dc.ZeroDataRetention)
		}
		if len(gateway.Uses) > 0 {
			// Zero retention only when every call is routed so.
			if openRouter && !slices.ContainsFunc(gateway.Uses, func(u Use) bool { return !u.ZeroRetention }) {
				gateway.Retention = RetentionZero
			}
			p.Processors = append(p.Processors, gateway)
		}
	}

	if ec := v2index.ConfigFromEnv(lookup); ec.Enabled() {
		voyage := Processor{Name: ProcessorVoyage, Retention: RetentionUnconfirmed}
		if strings.EqualFold(get("VOYAGE_RETENTION"), RetentionZero) {
			voyage.Retention = RetentionZero
		}
		zero := voyage.Retention == RetentionZero
		voyage.Uses = append(voyage.Uses, Use{Use: UseEmbeddings, Model: ec.IndexModel, ZeroRetention: zero})
		if ec.QueryModel != "" {
			voyage.Uses = append(voyage.Uses, Use{Use: UseQueries, Model: ec.QueryModel, ZeroRetention: zero})
		}
		if rm := get("V2_RERANK_MODEL"); !strings.EqualFold(rm, "off") {
			if rm == "" {
				rm = rerank.DefaultVoyageModel
			}
			voyage.Uses = append(voyage.Uses, Use{Use: UseRerank, Model: rm, ZeroRetention: zero})
		}
		p.Processors = append(p.Processors, voyage)
	}

	// The morning email carries kept memories' words (never an outside
	// source's) to the people who may keep them.
	if dc := v2dream.ConfigFromEnv(lookup); dc.Email && get("RESEND_API_KEY") != "" {
		p.Processors = append(p.Processors, Processor{Name: ProcessorResend, Retention: RetentionProvider,
			Uses: []Use{{Use: UseEmail}}})
	}
	return p
}

// modelUse is one model call's use, from its tier's routing.
func modelUse(use, model string, r anthropic.Routing, zdr bool) Use {
	return Use{Use: use, Model: model, Hosts: slices.Clone(r.Providers), MinPrecision: floorOf(r.Quantizations),
		ZeroRetention: zdr}
}

// floorOf names the narrowest precision a routing admits ("fp8"), the
// floor anthropic.QuantizationsAtLeast expanded; empty when it admits any.
func floorOf(quantizations []string) string {
	order := []string{"int4", "fp4", "mxfp4", "nvfp4", "fp6", "int8", "fp8", "mxfp8", "fp16", "bf16", "fp32"}
	for _, q := range order {
		if slices.Contains(quantizations, q) {
			// int8 and fp8 share a width; the floor is named fp8.
			if q == "int8" || q == "mxfp8" {
				return "fp8"
			}
			if q == "int4" || q == "mxfp4" || q == "nvfp4" {
				return "fp4"
			}
			return q
		}
	}
	return ""
}

// ParseResidency reads "holds=provider[:region],…"; an empty value is
// DefaultResidency. Entries with an unknown kind, or without a provider,
// are left out, and each kind is listed once, in Holds' order.
func ParseResidency(v string) []Place {
	if strings.TrimSpace(v) == "" {
		v = DefaultResidency
	}
	byKind := map[string]Place{}
	for _, entry := range strings.Split(v, ",") {
		kind, rest, ok := strings.Cut(strings.TrimSpace(entry), "=")
		kind = strings.ToLower(strings.TrimSpace(kind))
		if !ok || !slices.Contains(Holds, kind) {
			continue
		}
		provider, region, _ := strings.Cut(rest, ":")
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			continue
		}
		byKind[kind] = Place{Holds: kind, Provider: provider, Region: strings.TrimSpace(region)}
	}
	out := []Place{}
	for _, k := range Holds {
		if p, ok := byKind[k]; ok {
			out = append(out, p)
		}
	}
	return out
}

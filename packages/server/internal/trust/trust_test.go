package trust_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/trust"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func names(p trust.Posture) string {
	var out []string
	for _, pr := range p.Processors {
		out = append(out, pr.Name+":"+pr.Retention)
	}
	return strings.Join(out, ",")
}

func uses(pr trust.Processor) string {
	var out []string
	for _, u := range pr.Uses {
		out = append(out, u.Use)
	}
	return strings.Join(out, ",")
}

// The page says only what the configuration does: nothing sent anywhere
// without keys, every tier that's on with the hosts it is pinned to,
// Voyage's retention unconfirmed until VOYAGE_RETENTION says otherwise.
func TestFromEnv(t *testing.T) {
	t.Parallel()
	bare := trust.FromEnv(env(nil))
	if len(bare.Processors) != 0 || bare.BackupDays != 7 || len(bare.Residency) != 4 {
		t.Fatalf("no keys: %+v", bare)
	}

	cloud := map[string]string{
		"ANTHROPIC_API_KEY":  "sk",
		"ANTHROPIC_BASE_URL": "https://openrouter.ai/api",
		"VOYAGE_API_KEY":     "pa",
		"RESEND_API_KEY":     "re",
	}
	p := trust.FromEnv(env(cloud))
	if got := names(p); got != "openrouter:zero,voyage:unconfirmed,resend:provider_terms" {
		t.Fatalf("processors: %s", got)
	}
	if got := uses(p.Processors[0]); got != "judge,judge_fallback,judge_strong,ask,dream,dream_fallback,dream_strong" {
		t.Errorf("OpenRouter's uses: %s", got)
	}
	for _, u := range p.Processors[0].Uses {
		if !u.ZeroRetention || u.MinPrecision != "fp8" || len(u.Hosts) == 0 {
			t.Errorf("%s: %+v, want zero retention on pinned fp8 hosts", u.Use, u)
		}
	}
	strong := p.Processors[0].Uses[2]
	if strong.Model != "anthropic/claude-sonnet-5.5" || !reflect.DeepEqual(strong.Hosts, []string{"google-vertex"}) {
		t.Errorf("the strong tier: %+v", strong)
	}
	if got := uses(p.Processors[1]); got != "embeddings,queries,rerank" || p.Processors[1].Uses[0].ZeroRetention {
		t.Errorf("Voyage: %s %+v", got, p.Processors[1].Uses)
	}

	// Flipping Voyage's retention is configuration, not code.
	cloud["VOYAGE_RETENTION"] = "zero"
	if v := trust.FromEnv(env(cloud)).Processors[1]; v.Retention != trust.RetentionZero || !v.Uses[0].ZeroRetention {
		t.Errorf("VOYAGE_RETENTION=zero: %+v", v)
	}

	// Tiers that are off aren't listed; with the judge's primary off its
	// other tiers are never called either. A host list set in the
	// environment is what's shown.
	cloud["JUDGE_MODEL"] = "off"
	cloud["DREAM_MODEL"] = "off"
	cloud["ASK_PROVIDERS"] = "deepinfra"
	cloud["ASK_MIN_QUANTIZATION"] = "fp16"
	cloud["V2_RERANK_MODEL"] = "off"
	cloud["DREAM_EMAIL"] = "false"
	p = trust.FromEnv(env(cloud))
	if got := uses(p.Processors[0]); got != "ask" {
		t.Errorf("with the judge and Dream off: %s", got)
	}
	if a := p.Processors[0].Uses[0]; !reflect.DeepEqual(a.Hosts, []string{"deepinfra"}) || a.MinPrecision != "fp16" {
		t.Errorf("Ask's routing: %+v", a)
	}
	if got := names(p); got != "openrouter:zero,voyage:zero" {
		t.Errorf("without the morning email or rerank: %s", got)
	}

	// Calls that don't go through OpenRouter's zero-retention routing are
	// kept under the provider's terms, and say so.
	direct := trust.FromEnv(env(map[string]string{"ANTHROPIC_API_KEY": "sk", "ASK_ZDR": "false"}))
	if got := names(direct); got != "anthropic:provider_terms" {
		t.Errorf("Anthropic directly: %s", got)
	}
	for _, u := range direct.Processors[0].Uses {
		if u.ZeroRetention {
			t.Errorf("%s claims zero retention without OpenRouter's routing", u.Use)
		}
	}
	mixed := trust.FromEnv(env(map[string]string{"ANTHROPIC_API_KEY": "sk", "ANTHROPIC_BASE_URL": "https://openrouter.ai/api",
		"ASK_ZDR": "false", "VOYAGE_API_KEY": ""}))
	if got := names(mixed); got != "openrouter:provider_terms" {
		t.Errorf("one tier without zero retention: %s", got)
	}

	if trust.FromEnv(env(map[string]string{"V2_BACKUP_RETENTION_DAYS": "14"})).BackupDays != 14 {
		t.Error("V2_BACKUP_RETENTION_DAYS is ignored")
	}
}

func TestParseResidency(t *testing.T) {
	t.Parallel()
	got := trust.ParseResidency("")
	want := []trust.Place{
		{Holds: "database", Provider: "neon", Region: "us-west-2"},
		{Holds: "compute", Provider: "fly", Region: "sjc"},
		{Holds: "objects", Provider: "r2"},
		{Holds: "edge", Provider: "cloudflare"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the default: %+v", got)
	}
	got = trust.ParseResidency(" edge = vercel , database=Neon:us-east-1, moon=cheese, compute=, objects=r2:wnam")
	want = []trust.Place{
		{Holds: "database", Provider: "neon", Region: "us-east-1"},
		{Holds: "objects", Provider: "r2", Region: "wnam"},
		{Holds: "edge", Provider: "vercel"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a custom list: %+v", got)
	}
}

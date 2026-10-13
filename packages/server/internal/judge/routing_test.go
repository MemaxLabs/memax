package judge_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/anthropic/mockllm"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
)

// Each tier is pinned to its slug's zero-retention hosts at fp8 or better
// (D14), the primary and fallback answer at temperature 0, and the strong
// tier at its model's default; JUDGE_* overrides each, and JUDGE_ZDR=false
// drops the defaults.
func TestConfigRoutingFromEnv(t *testing.T) {
	t.Parallel()
	env := func(kv map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
	}
	fp8, _ := anthropic.QuantizationsAtLeast("fp8")
	temp := func(tier judge.Tier) any {
		if tier.Temperature == nil {
			return nil
		}
		return *tier.Temperature
	}

	c := judge.ConfigFromEnv(env(nil))
	for _, tier := range []judge.Tier{c.Primary, c.Fallback, c.Strong} {
		want := anthropic.Routing{Providers: anthropic.DefaultProviders[tier.Model], Quantizations: fp8}
		if len(want.Providers) == 0 {
			t.Errorf("%s: no default hosts", tier.Model)
		}
		if !reflect.DeepEqual(tier.Routing, want) {
			t.Errorf("%s: routing %+v, want %+v", tier.Model, tier.Routing, want)
		}
	}
	if temp(c.Primary) != 0.0 || temp(c.Fallback) != 0.0 || temp(c.Strong) != nil {
		t.Errorf("temperatures: primary %v, fallback %v, strong %v; want 0, 0, none", temp(c.Primary), temp(c.Fallback), temp(c.Strong))
	}

	c = judge.ConfigFromEnv(env(map[string]string{
		"JUDGE_PROVIDERS":          "deepinfra,coreweave",
		"JUDGE_STRONG_PROVIDERS":   "any",
		"JUDGE_MIN_QUANTIZATION":   "bf16",
		"JUDGE_TEMPERATURE":        "default",
		"JUDGE_STRONG_TEMPERATURE": "0.2",
	}))
	bf16, _ := anthropic.QuantizationsAtLeast("bf16")
	if want := (anthropic.Routing{Providers: []string{"deepinfra", "coreweave"}, Quantizations: bf16}); !reflect.DeepEqual(c.Primary.Routing, want) {
		t.Errorf("primary routing %+v, want %+v", c.Primary.Routing, want)
	}
	if want := (anthropic.Routing{Quantizations: bf16}); !reflect.DeepEqual(c.Strong.Routing, want) {
		t.Errorf("strong routing %+v, want %+v", c.Strong.Routing, want)
	}
	// Sonnet 5.5 takes no temperature but its own, so the named one isn't
	// sent (anthropic.ModelParams).
	if temp(c.Primary) != nil || temp(c.Fallback) != 0.0 || temp(c.Strong) != nil {
		t.Errorf("temperatures: primary %v, fallback %v, strong %v", temp(c.Primary), temp(c.Fallback), temp(c.Strong))
	}

	c = judge.ConfigFromEnv(env(map[string]string{"JUDGE_ZDR": "false"}))
	for _, tier := range []judge.Tier{c.Primary, c.Fallback, c.Strong} {
		if !reflect.DeepEqual(tier.Routing, anthropic.Routing{}) {
			t.Errorf("%s: routing %+v without zero retention", tier.Model, tier.Routing)
		}
	}
}

// A tier's thinking and temperature follow its model's rules
// (anthropic.ModelParams), whichever tier it is: the fallback on Claude
// Haiku 5.5 is pinned to Vertex, sends no temperature (a named one is
// dropped) and thinks at effort medium unless JUDGE_FALLBACK_THINKING says
// otherwise; the default tiers keep the client's default thinking.
func TestConfigFollowsTheModelsRules(t *testing.T) {
	t.Parallel()
	env := func(kv map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
	}
	c := judge.ConfigFromEnv(env(nil))
	for _, tier := range []judge.Tier{c.Primary, c.Fallback, c.Strong} {
		if tier.Thinking != (anthropic.Thinking{}) {
			t.Errorf("%s: thinking %+v by default", tier.Model, tier.Thinking)
		}
	}
	c = judge.ConfigFromEnv(env(map[string]string{"JUDGE_FALLBACK_MODEL": "anthropic/claude-haiku-5.5",
		"JUDGE_FALLBACK_TEMPERATURE": "0"}))
	fb := c.Fallback
	if fb.Temperature != nil || !reflect.DeepEqual(fb.Routing.Providers, []string{"google-vertex"}) || !fb.Strict ||
		fb.Thinking != (anthropic.Thinking{Mode: anthropic.ThinkingAdaptive, Effort: "medium"}) {
		t.Errorf("Haiku 5.5 fallback: %+v", fb)
	}
	if c.Primary.Temperature == nil || *c.Primary.Temperature != 0 {
		t.Errorf("the primary lost its temperature: %v", c.Primary.Temperature)
	}
	c = judge.ConfigFromEnv(env(map[string]string{"JUDGE_FALLBACK_MODEL": "anthropic/claude-haiku-5.5",
		"JUDGE_FALLBACK_THINKING": "off", "JUDGE_THINKING": "low"}))
	if c.Fallback.Thinking != (anthropic.Thinking{Mode: anthropic.ThinkingDisabled}) ||
		c.Primary.Thinking != (anthropic.Thinking{Mode: anthropic.ThinkingAdaptive, Effort: "low"}) {
		t.Errorf("named thinking: primary %+v, fallback %+v", c.Primary.Thinking, c.Fallback.Thinking)
	}
}

// A tier on Haiku 5.5 reaches the gateway as its rules say: strict, its
// thinking and effort beside the schema, and no temperature or top_p.
func TestAnthropicModelSendsHaiku55sRules(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText(`{"pairs":[]}`)
	c := judge.ConfigFromEnv(func(k string) (string, bool) {
		if k == "JUDGE_FALLBACK_MODEL" {
			return "anthropic/claude-haiku-5.5", true
		}
		return "", false
	})
	m := judge.NewAnthropicModel(srv.Client(), true)
	if _, err := m.Complete(context.Background(), judge.Call{Tier: c.Fallback, System: "s", Prompt: "p", Schema: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(srv.Requests()[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	oc, _ := body["output_config"].(map[string]any)
	th, _ := body["thinking"].(map[string]any)
	if oc["effort"] != "medium" || oc["format"] == nil || th["type"] != "adaptive" {
		t.Errorf("output_config %v, thinking %v", body["output_config"], body["thinking"])
	}
	for _, key := range []string{"temperature", "top_p"} {
		if _, ok := body[key]; ok {
			t.Errorf("sent %s", key)
		}
	}
}

// A tier's routing and temperature reach the gateway with every call.
func TestAnthropicModelSendsTierRouting(t *testing.T) {
	t.Parallel()
	srv := mockllm.New(t)
	srv.EnqueueText(`{"pairs":[]}`)
	srv.EnqueueText(`{"pairs":[]}`)
	m := judge.NewAnthropicModel(srv.Client(), true)
	c := judge.ConfigFromEnv(func(string) (string, bool) { return "", false })
	for _, tier := range []judge.Tier{c.Primary, c.Strong} {
		if _, err := m.Complete(context.Background(), judge.Call{Tier: tier, System: "s", Prompt: "p", Schema: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("%d requests", len(reqs))
	}
	for i, tier := range []judge.Tier{c.Primary, c.Strong} {
		var body map[string]any
		if err := json.Unmarshal(reqs[i].Body, &body); err != nil {
			t.Fatal(err)
		}
		p, _ := body["provider"].(map[string]any)
		var only []string
		for _, h := range p["only"].([]any) {
			only = append(only, h.(string))
		}
		if p["zdr"] != true || !reflect.DeepEqual(only, tier.Routing.Providers) || p["quantizations"] == nil {
			t.Errorf("%s: provider = %v", tier.Model, body["provider"])
		}
		temp, sent := body["temperature"]
		if wantSent := tier.Temperature != nil; sent != wantSent || (sent && temp != 0.0) {
			t.Errorf("%s: temperature %v (sent %v)", tier.Model, temp, sent)
		}
	}
}

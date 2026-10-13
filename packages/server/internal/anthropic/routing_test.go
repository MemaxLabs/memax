package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestQuantizationsAtLeast(t *testing.T) {
	t.Parallel()
	cases := []struct {
		floor string
		want  []string
		err   bool
	}{
		{"fp8", []string{"int8", "fp8", "mxfp8", "fp16", "bf16", "fp32", "unknown"}, false},
		{" FP8 ", []string{"int8", "fp8", "mxfp8", "fp16", "bf16", "fp32", "unknown"}, false},
		{"bf16", []string{"fp16", "bf16", "fp32", "unknown"}, false},
		{"fp4", []string{"int4", "fp4", "mxfp4", "nvfp4", "fp6", "int8", "fp8", "mxfp8", "fp16", "bf16", "fp32", "unknown"}, false},
		{"", nil, false},
		{"off", nil, false},
		{"any", nil, false},
		{"fp7", nil, true},
	}
	for _, tc := range cases {
		got, err := QuantizationsAtLeast(tc.floor)
		if (err != nil) != tc.err || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("QuantizationsAtLeast(%q) = %v, %v; want %v (error %v)", tc.floor, got, err, tc.want, tc.err)
		}
	}
	// D14: no fp4-class host passes the default floor.
	floor, _ := QuantizationsAtLeast(DefaultMinQuantization)
	for _, q := range []string{"int4", "fp4", "mxfp4", "nvfp4", "fp6"} {
		if slices.Contains(floor, q) {
			t.Errorf("the default floor admits %s", q)
		}
	}
}

func TestRoutingFromEnv(t *testing.T) {
	t.Parallel()
	env := func(kv map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
	}
	fp8, _ := QuantizationsAtLeast("fp8")
	const flash = "deepseek/deepseek-v4.1-flash"
	cases := []struct {
		name     string
		env      map[string]string
		model    string
		defaults bool
		want     Routing
		err      bool
	}{
		{"defaults for a known slug", nil, flash, true, Routing{Providers: DefaultProviders[flash], Quantizations: fp8}, false},
		{"an unknown slug keeps the floor", nil, "openai/gpt-6-luna", true, Routing{Quantizations: fp8}, false},
		{"no defaults without zero retention", nil, flash, false, Routing{}, false},
		{"named hosts", map[string]string{"P": "Together, deepinfra ,together"}, flash, true,
			Routing{Providers: []string{"together", "deepinfra"}, Quantizations: fp8}, false},
		{"named hosts without zero retention", map[string]string{"P": "deepinfra", "Q": "fp8"}, flash, false,
			Routing{Providers: []string{"deepinfra"}, Quantizations: fp8}, false},
		{"no pin", map[string]string{"P": "any"}, flash, true, Routing{Quantizations: fp8}, false},
		{"no floor", map[string]string{"Q": "off"}, flash, true, Routing{Providers: DefaultProviders[flash]}, false},
		{"a junk floor keeps the default", map[string]string{"Q": "fp7"}, flash, true,
			Routing{Providers: DefaultProviders[flash], Quantizations: fp8}, true},
	}
	for _, tc := range cases {
		got, err := RoutingFromEnv(env(tc.env), "P", "Q", tc.model, tc.defaults)
		if (err != nil) != tc.err || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %+v, %v; want %+v (error %v)", tc.name, got, err, tc.want, tc.err)
		}
	}
	// The defaults are copied, never shared with the table.
	r, _ := RoutingFromEnv(env(nil), "P", "Q", flash, true)
	r.Providers[0] = "changed"
	if DefaultProviders[flash][0] == "changed" {
		t.Error("RoutingFromEnv handed out the default table's slice")
	}
}

func TestTemperatureFromEnv(t *testing.T) {
	t.Parallel()
	env := func(kv map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
	}
	zero := 0.0
	val := func(p *float64) any {
		if p == nil {
			return nil
		}
		return *p
	}
	cases := []struct {
		name string
		env  map[string]string
		def  *float64
		want any
		err  bool
	}{
		{"unset keeps the default", nil, &zero, 0.0, false},
		{"unset with no default", nil, nil, nil, false},
		{"a number", map[string]string{"T": "0.3"}, &zero, 0.3, false},
		{"default sends none", map[string]string{"T": "default"}, &zero, nil, false},
		{"off sends none", map[string]string{"T": "off"}, &zero, nil, false},
		{"out of range keeps the default", map[string]string{"T": "3"}, &zero, 0.0, true},
		{"junk keeps the default", map[string]string{"T": "cold"}, nil, nil, true},
	}
	for _, tc := range cases {
		got, err := TemperatureFromEnv(env(tc.env), "T", tc.def)
		if (err != nil) != tc.err || val(got) != tc.want {
			t.Errorf("%s: %v, %v; want %v (error %v)", tc.name, val(got), err, tc.want, tc.err)
		}
		if got != nil && got == tc.def {
			t.Errorf("%s: the default pointer was shared", tc.name)
		}
	}
}

// The per-model rules are looked up by the exact slug: Claude Sonnet 5.5
// and Haiku 5.5 take no temperature, Haiku 5.5's tiers think at effort
// medium unless told otherwise, and every other slug keeps the defaults.
func TestModelParams(t *testing.T) {
	t.Parallel()
	haiku := ParamsFor(" anthropic/claude-haiku-5.5 ")
	if !haiku.FixedSampling || haiku.Thinking != (Thinking{Mode: ThinkingAdaptive, Effort: "medium"}) {
		t.Errorf("Haiku 5.5: %+v", haiku)
	}
	if sonnet := ParamsFor("anthropic/claude-sonnet-5.5"); !sonnet.FixedSampling || sonnet.Thinking != (Thinking{}) {
		t.Errorf("Sonnet 5.5: %+v", sonnet)
	}
	// Nothing is read from the name: Haiku 4.5, a Haiku 5.5 on another
	// gateway's naming and DeepSeek have no rules.
	for _, slug := range []string{"anthropic/claude-haiku-4.5", "claude-haiku-5-5", "deepseek/deepseek-v4.1-flash", ""} {
		if p := ParamsFor(slug); p != (ModelParams{}) {
			t.Errorf("%q: %+v, want no rules", slug, p)
		}
	}
	// Every slug with rules is one a tier can be pinned for.
	for slug := range DefaultModelParams {
		if len(DefaultProviders[slug]) == 0 {
			t.Errorf("%s has parameter rules but no default hosts", slug)
		}
	}
}

func TestThinkingFromEnv(t *testing.T) {
	t.Parallel()
	env := func(kv map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
	}
	const haiku, flash = "anthropic/claude-haiku-5.5", "deepseek/deepseek-v4.1-flash"
	medium := Thinking{Mode: ThinkingAdaptive, Effort: "medium"}
	cases := []struct {
		name  string
		env   map[string]string
		model string
		want  Thinking
		err   bool
	}{
		{"unset is the model's rule", nil, haiku, medium, false},
		{"unset without a rule is the client's default", nil, flash, Thinking{}, false},
		{"off", map[string]string{"T": " Off "}, haiku, Thinking{Mode: ThinkingDisabled}, false},
		{"disabled", map[string]string{"T": "disabled"}, flash, Thinking{Mode: ThinkingDisabled}, false},
		{"adaptive", map[string]string{"T": "adaptive"}, haiku, Thinking{Mode: ThinkingAdaptive}, false},
		{"an effort level", map[string]string{"T": "LOW"}, haiku, Thinking{Mode: ThinkingAdaptive, Effort: "low"}, false},
		{"default sends the client's default", map[string]string{"T": "default"}, haiku, Thinking{}, false},
		{"blank is unset", map[string]string{"T": " "}, haiku, medium, false},
		{"junk keeps the rule", map[string]string{"T": "lots"}, haiku, medium, true},
	}
	for _, tc := range cases {
		got, err := ThinkingFromEnv(env(tc.env), "T", tc.model)
		if (err != nil) != tc.err || got != tc.want {
			t.Errorf("%s: %+v, %v; want %+v (error %v)", tc.name, got, err, tc.want, tc.err)
		}
	}
	// String reads back as ThinkingFromEnv takes it.
	for _, th := range []Thinking{{}, {Mode: ThinkingDisabled}, {Mode: ThinkingAdaptive}, medium} {
		if got, _ := ThinkingFromEnv(env(map[string]string{"T": th.String()}), "T", flash); got != th {
			t.Errorf("%+v reads back as %+v", th, got)
		}
	}
}

func TestModelTemperatureFromEnv(t *testing.T) {
	t.Parallel()
	env := func(kv map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
	}
	zero := 0.0
	val := func(p *float64) any {
		if p == nil {
			return nil
		}
		return *p
	}
	cases := []struct {
		name  string
		env   map[string]string
		model string
		want  any
		err   bool
	}{
		{"a model that takes one keeps the default", nil, "anthropic/claude-haiku-4.5", 0.0, false},
		{"and a named one", map[string]string{"T": "0.4"}, "anthropic/claude-haiku-4.5", 0.4, false},
		{"Haiku 5.5 gets none", nil, "anthropic/claude-haiku-5.5", nil, false},
		{"Sonnet 5.5 gets none", nil, "anthropic/claude-sonnet-5.5", nil, false},
		{"a temperature named for Haiku 5.5 isn't sent", map[string]string{"T": "0"}, "anthropic/claude-haiku-5.5", nil, true},
		{"default is fine", map[string]string{"T": "default"}, "anthropic/claude-haiku-5.5", nil, false},
	}
	for _, tc := range cases {
		got, err := ModelTemperatureFromEnv(env(tc.env), "T", tc.model, &zero)
		if (err != nil) != tc.err || val(got) != tc.want {
			t.Errorf("%s: %v, %v; want %v (error %v)", tc.name, val(got), err, tc.want, tc.err)
		}
	}
}

// The allowlist goes out as provider.only and provider.order, the floor as
// provider.quantizations, and a temperature (0 included) as temperature,
// on Complete and on a stream; none of them is sent unless asked for.
func TestRequestsCarryRoutingAndTemperature(t *testing.T) {
	t.Parallel()
	var bodies []map[string]any
	srv := fakeAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, body)
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}` + "\n\n"))
			return
		}
		successResponse(w, `{"ok":true}`)
	})
	c := New("k", srv.URL)
	zero := 0.0
	fp8, _ := QuantizationsAtLeast("fp8")
	pinned := CompleteRequest{
		Model: "deepseek/deepseek-v4.1-flash", MaxTokens: 50, Prompt: "p", ZeroDataRetention: true,
		Providers: []string{"together", "baseten"}, Quantizations: fp8, Temperature: &zero,
	}
	if _, err := c.Complete(context.Background(), pinned); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CompleteStreamUsage(context.Background(), pinned, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Complete(context.Background(), CompleteRequest{Model: "m", MaxTokens: 50, Prompt: "p", Providers: []string{"deepinfra"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Complete(context.Background(), CompleteRequest{Model: "m", MaxTokens: 50, Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 4 {
		t.Fatalf("%d requests, want 4", len(bodies))
	}
	want := map[string]any{
		"zdr":           true,
		"only":          []any{"together", "baseten"},
		"order":         []any{"together", "baseten"},
		"quantizations": []any{"int8", "fp8", "mxfp8", "fp16", "bf16", "fp32", "unknown"},
	}
	for i, name := range []string{"complete", "stream"} {
		if !reflect.DeepEqual(bodies[i]["provider"], want) {
			t.Errorf("%s: provider = %v, want %v", name, bodies[i]["provider"], want)
		}
		if temp, ok := bodies[i]["temperature"]; !ok || temp != 0.0 {
			t.Errorf("%s: temperature = %v (sent %v), want 0", name, temp, ok)
		}
	}
	if p := bodies[2]["provider"]; !reflect.DeepEqual(p, map[string]any{"only": []any{"deepinfra"}, "order": []any{"deepinfra"}}) {
		t.Errorf("pinned without zero retention: provider = %v", p)
	}
	for _, key := range []string{"provider", "temperature"} {
		if _, ok := bodies[3][key]; ok {
			t.Errorf("%s sent unasked: %v", key, bodies[3][key])
		}
	}
	for i, body := range bodies {
		if _, ok := body["top_p"]; ok {
			t.Errorf("request %d sent top_p", i)
		}
	}
}

// A request's Thinking goes out as thinking.type and output_config.effort,
// beside a strict format, on Complete and on a stream; without one the
// client's default stands (off for DeepSeek, omitted for Claude). A tier
// on Haiku 5.5, as its configuration builds it, sends no temperature and
// no top_p.
func TestRequestsCarryThinking(t *testing.T) {
	t.Parallel()
	var bodies []map[string]any
	srv := fakeAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		bodies = append(bodies, body)
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}` + "\n\n"))
			return
		}
		successResponse(w, `{"ok":true}`)
	})
	c := New("k", srv.URL)
	const haiku = "anthropic/claude-haiku-5.5"
	none := func(string) (string, bool) { return "", false }
	zero := 0.0
	temp, _ := ModelTemperatureFromEnv(none, "T", haiku, &zero)
	thinking, _ := ThinkingFromEnv(none, "K", haiku)
	tier := CompleteRequest{Model: haiku, MaxTokens: 50, Prompt: "p", Temperature: temp, Thinking: thinking,
		OutputSchema: json.RawMessage(`{"type":"object"}`)}
	reqs := []CompleteRequest{
		tier,
		{Model: haiku, MaxTokens: 50, Prompt: "p", Thinking: Thinking{Mode: ThinkingDisabled}},
		{Model: "deepseek/deepseek-v4.1-flash", MaxTokens: 50, Prompt: "p"},
		{Model: haiku, MaxTokens: 50, Prompt: "p"},
		{Model: haiku, MaxTokens: 50, Prompt: "p", Thinking: Thinking{Effort: "low"}},
	}
	for _, req := range reqs {
		if _, err := c.Complete(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.CompleteStreamUsage(context.Background(), tier, func(string) {}); err != nil {
		t.Fatal(err)
	}
	thinkingOf := func(body map[string]any) any {
		th, ok := body["thinking"].(map[string]any)
		if !ok {
			return nil
		}
		return th["type"]
	}
	effortOf := func(body map[string]any) any {
		oc, _ := body["output_config"].(map[string]any)
		return oc["effort"]
	}
	want := []struct {
		thinking, effort any
		format           bool
	}{
		{"adaptive", "medium", true},
		{"disabled", nil, false},
		{"disabled", nil, false},
		{nil, nil, false},
		{"adaptive", "low", false},
		{"adaptive", "medium", false}, // the stream: no format
	}
	for i, w := range want {
		body := bodies[i]
		oc, _ := body["output_config"].(map[string]any)
		_, hasFormat := oc["format"]
		if thinkingOf(body) != w.thinking || effortOf(body) != w.effort || hasFormat != w.format {
			t.Errorf("request %d: thinking %v, effort %v, format %v; want %v, %v, %v", i, thinkingOf(body), effortOf(body),
				hasFormat, w.thinking, w.effort, w.format)
		}
		for _, key := range []string{"temperature", "top_p", "top_k"} {
			if _, ok := body[key]; ok {
				t.Errorf("request %d sent %s", i, key)
			}
		}
	}
}

// Every default host list names hosts for a slug some tier uses, and none
// names a host twice.
func TestDefaultProviders(t *testing.T) {
	t.Parallel()
	for slug, hosts := range DefaultProviders {
		if len(hosts) == 0 {
			t.Errorf("%s: no hosts", slug)
		}
		if len(slices.Compact(slices.Sorted(slices.Values(hosts)))) != len(hosts) {
			t.Errorf("%s: a host is listed twice: %v", slug, hosts)
		}
		if !reflect.DeepEqual(ParseProviders(strings.Join(hosts, ",")), hosts) {
			t.Errorf("%s: %v doesn't survive the env round trip", slug, hosts)
		}
	}
}

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

package anthropic

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Provider routing (plan 25 D14). OpenRouter serves one model slug from
// many hosts, and with nothing but provider.zdr it picks among every
// zero-retention host, weighted towards the cheapest, fp4-quantised ones
// included. A tier therefore names the hosts that may serve it, in order
// (provider.only and provider.order), and the precisions they may run
// (provider.quantizations). Both are explicit configuration, read once at
// startup by each tier's ConfigFromEnv; nothing here is inferred from a
// model's name.

// DefaultProviders are the zero-retention hosts each model slug is pinned
// to when its tier names none, in order of preference. They were chosen on
// Oct 7, 2026 from OpenRouter's endpoints API
// (/api/v1/models/<slug>/endpoints) and its zero-retention list
// (/api/v1/endpoints/zdr): every one is zero-retention for its slug, none
// declares fp4, and among those they were the quickest to a first token at
// a fair price. eval/judge/RESULTS.md has the table they were chosen from,
// and the docs site lists them as sub-processors. A slug that isn't here
// is routed by zero retention alone.
//
//   - DeepSeek V4.1 Flash (the judge's primary, Ask): Together (first token
//     p50 0.3 s, p90 0.7 s, the largest volume), Baseten (fp8, p50 0.3–0.5 s),
//     CoreWeave (fp8, p50 1.0 s, cheaper), DeepInfra (fp8, cheapest of the
//     four and the steadiest, but p50 1.4 s), in that order.
//   - Claude Haiku 4.5 (the judge's fallback): Amazon Bedrock, then Google
//     Vertex. Both enforce output_config.format.
//   - Claude Sonnet 5.5 (the judge's strong tier): Google Vertex only.
//     Bedrock is zero-retention for it too, but doesn't list structured
//     outputs, and this tier is strict.
var DefaultProviders = map[string][]string{
	"deepseek/deepseek-v4.1-flash": {"together", "baseten", "coreweave", "deepinfra"},
	"anthropic/claude-haiku-4.5":   {"amazon-bedrock", "google-vertex"},
	"anthropic/claude-sonnet-5.5":  {"google-vertex"},
}

// DefaultMinQuantization is the lowest precision a tier's hosts may run
// when its tier names none: fp8 (D14: no fp4 hosts).
const DefaultMinQuantization = "fp8"

// quantizationBits are OpenRouter's quantization levels by weight width.
// "unknown" (a host that doesn't declare its precision) has no width: it
// is admitted at any floor, because the allowlist, not the floor, is what
// vets such a host. Every Claude endpoint is "unknown".
var quantizationBits = []struct {
	name string
	bits int
}{
	{"int4", 4}, {"fp4", 4}, {"mxfp4", 4}, {"nvfp4", 4},
	{"fp6", 6},
	{"int8", 8}, {"fp8", 8}, {"mxfp8", 8},
	{"fp16", 16}, {"bf16", 16},
	{"fp32", 32},
}

// QuantizationsAtLeast returns OpenRouter's quantization levels at least as
// wide as floor ("fp8" admits int8, fp8, mxfp8, fp16, bf16, fp32 and
// unknown), for provider.quantizations. An empty floor, "off", "none" or
// "any" admits every level (nil: send no filter).
func QuantizationsAtLeast(floor string) ([]string, error) {
	floor = strings.ToLower(strings.TrimSpace(floor))
	switch floor {
	case "", "off", "none", "any":
		return nil, nil
	}
	floorBits := 0
	for _, q := range quantizationBits {
		if q.name == floor {
			floorBits = q.bits
		}
	}
	if floorBits == 0 {
		return nil, fmt.Errorf("unknown quantization %q", floor)
	}
	var out []string
	for _, q := range quantizationBits {
		if q.bits >= floorBits {
			out = append(out, q.name)
		}
	}
	return append(out, "unknown"), nil
}

// Routing is one tier's provider routing beyond zero retention.
type Routing struct {
	// Providers is the ordered allowlist of provider slugs ("together",
	// "google-vertex"); a base slug admits all of its endpoints
	// ("baseten" admits "baseten/fp8"). Empty pins no host.
	Providers []string
	// Quantizations admits only endpoints at these precisions. Empty
	// admits any.
	Quantizations []string
}

// RoutingFromEnv reads one tier's routing through lookup (os.LookupEnv):
// providersKey is its allowlist ("together,baseten"; "off" or "any" pins
// no host) and quantKey its precision floor ("fp8"; "off" admits any).
// Unset, the allowlist is DefaultProviders[model] and the floor
// DefaultMinQuantization, but only when defaults is set: they are D14's
// zero-retention routing, so a tier that doesn't route by zero retention
// (a gateway without OpenRouter's provider routing) gets none unless it
// names them. A floor it can't read keeps the default and is reported.
func RoutingFromEnv(lookup func(string) (string, bool), providersKey, quantKey, model string, defaults bool) (Routing, error) {
	var r Routing
	var err error
	if v, ok := lookup(providersKey); ok && strings.TrimSpace(v) != "" {
		r.Providers = ParseProviders(v)
	} else if defaults {
		r.Providers = slices.Clone(DefaultProviders[strings.TrimSpace(model)])
	}
	floor := ""
	if defaults {
		floor = DefaultMinQuantization
	}
	if v, ok := lookup(quantKey); ok && strings.TrimSpace(v) != "" {
		if _, perr := QuantizationsAtLeast(v); perr != nil {
			err = fmt.Errorf("%s: %w; keeping %q", quantKey, perr, floor)
		} else {
			floor = v
		}
	}
	r.Quantizations, _ = QuantizationsAtLeast(floor)
	return r, err
}

// ParseProviders reads a comma-separated allowlist. "off", "none" and
// "any" pin no host.
func ParseProviders(v string) []string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "off", "none", "any":
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// TemperatureFromEnv reads a temperature through lookup: a number in
// [0, 2], or "off"/"default" for the model's own (nil). Unset, it is def.
func TemperatureFromEnv(lookup func(string) (string, bool), key string, def *float64) (*float64, error) {
	if def != nil {
		d := *def // the caller's default is never shared
		def = &d
	}
	v, ok := lookup(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def, nil
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "none", "default":
		return nil, nil
	}
	t, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || t < 0 || t > 2 {
		return def, fmt.Errorf("%s: %q is not a temperature in [0, 2]", key, v)
	}
	return &t, nil
}

// providerObject is the request's "provider" object, or nil when the
// request asks for no routing.
func providerObject(req CompleteRequest) map[string]any {
	if !req.ZeroDataRetention && len(req.Providers) == 0 && len(req.Quantizations) == 0 {
		return nil
	}
	p := map[string]any{}
	if req.ZeroDataRetention {
		p["zdr"] = true
	}
	if len(req.Providers) > 0 {
		// only is the allowlist (no other host serves, even as a
		// fallback); order is the preference within it.
		p["only"] = req.Providers
		p["order"] = req.Providers
	}
	if len(req.Quantizations) > 0 {
		p["quantizations"] = req.Quantizations
	}
	return p
}

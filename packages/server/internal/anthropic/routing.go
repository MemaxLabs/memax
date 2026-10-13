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
//     four and the steadiest, but p50 1.4 s), in that order. Baseten is
//     pinned to its fp8 endpoint: the base slug also admits its fp32 "fast"
//     endpoint, listed on Oct 11, 2026 at twice the price.
//   - Claude Haiku 4.5 (the judge's fallback): Amazon Bedrock, then Google
//     Vertex. Both enforce output_config.format.
//   - Claude Haiku 5.5: Google Vertex only (chosen Oct 11, 2026). Bedrock
//     is zero-retention for it too, but only Vertex lists structured
//     outputs, and the fallback tier is strict. OpenRouter leaves out the
//     regional endpoints (10% dearer) unless asked, so this is Vertex's
//     global endpoint.
//   - Claude Sonnet 5.5 (the judge's strong tier): Google Vertex only.
//     Bedrock is zero-retention for it too, but doesn't list structured
//     outputs, and this tier is strict.
var DefaultProviders = map[string][]string{
	"deepseek/deepseek-v4.1-flash": {"together", "baseten/fp8", "coreweave", "deepinfra"},
	"anthropic/claude-haiku-4.5":   {"amazon-bedrock", "google-vertex"},
	"anthropic/claude-haiku-5.5":   {"google-vertex"},
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

// ModelParams are how a model slug's requests differ from the Messages
// API's defaults, and what thinking its tiers send by default. Like
// DefaultProviders, they are explicit configuration taken from each
// model's documentation and looked up by the exact slug; nothing is parsed
// from a model's name. A slug that isn't in DefaultModelParams takes a
// temperature and gets the client's default thinking.
type ModelParams struct {
	// FixedSampling: the model takes no temperature or top_p but its own.
	// A tier on it sends neither, whatever its configuration says.
	FixedSampling bool
	// Thinking is what a tier on the model sends when its configuration
	// names none (JUDGE_FALLBACK_THINKING and the like).
	Thinking Thinking
}

// DefaultModelParams are the slugs whose requests differ from the defaults.
//
//   - Claude Sonnet 5.5 (the judge's strong tier): a temperature or top_p
//     other than its own is a 400 on Anthropic's API. Its thinking stays
//     the model's own (adaptive), which it rarely uses on these calls.
//   - Claude Haiku 5.5: temperature must be 1 and top_p 0.99, and a request
//     with both is a 400, so it gets neither. OpenRouter drops a
//     temperature it can't route (Oct 11, 2026: accepted at 0 on Vertex,
//     and refused with provider.require_parameters), but Anthropic's API
//     doesn't. Thinking is adaptive at effort medium, the model's own
//     default, named so that a change of default doesn't move it. Of off,
//     low and medium on the judge's sets, only medium flagged every
//     planted conflict in every run with at most one false flag; off made
//     two false flags a run, and low missed a conflict once and made a
//     false flag on the held-out set. Off and low cost 14–22% less and
//     are quicker (eval/judge/RESULTS.md, Oct 11).
var DefaultModelParams = map[string]ModelParams{
	"anthropic/claude-sonnet-5.5": {FixedSampling: true},
	"anthropic/claude-haiku-5.5":  {FixedSampling: true, Thinking: Thinking{Mode: ThinkingAdaptive, Effort: "medium"}},
}

// ParamsFor returns a slug's entry in DefaultModelParams (the zero value
// when it has none).
func ParamsFor(model string) ModelParams { return DefaultModelParams[strings.TrimSpace(model)] }

// Thinking is a request's thinking control: thinking.type and
// output_config.effort. The zero value is the client's default: off for the
// reasoning models served through OpenRouter (DeepSeek thinks unless told
// not to), omitted for Claude, which then thinks as its model does by
// default (Haiku 5.5: adaptive, at effort medium).
type Thinking struct {
	// Mode is ThinkingDisabled or ThinkingAdaptive; "" is the client's
	// default.
	Mode string `json:"mode,omitempty"`
	// Effort is output_config.effort, how much an adaptive thinker thinks
	// ("low", "medium", "high", "xhigh", "max"); "" is the model's default.
	Effort string `json:"effort,omitempty"`
}

// The thinking modes.
const (
	ThinkingDisabled = "disabled"
	ThinkingAdaptive = "adaptive"
)

// EffortLevels are output_config.effort's values.
var EffortLevels = []string{"low", "medium", "high", "xhigh", "max"}

// String is the setting as ThinkingFromEnv reads it.
func (t Thinking) String() string {
	switch {
	case t.Mode == ThinkingDisabled:
		return "off"
	case t.Effort != "":
		return t.Effort
	case t.Mode == ThinkingAdaptive:
		return "adaptive"
	}
	return "default"
}

// ThinkingFromEnv reads a tier's thinking through lookup: "off" (thinking
// disabled), "adaptive" (the model decides how much, at its default
// effort), an effort level (adaptive at that effort: "low", "medium",
// "high", "xhigh", "max"), or "default" (the client's default). Unset, it
// is ParamsFor(model).Thinking. A value it can't read keeps that and is
// reported.
func ThinkingFromEnv(lookup func(string) (string, bool), key, model string) (Thinking, error) {
	def := ParamsFor(model).Thinking
	v, ok := lookup(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def, nil
	}
	switch v = strings.ToLower(strings.TrimSpace(v)); v {
	case "off", "disabled", "none":
		return Thinking{Mode: ThinkingDisabled}, nil
	case "adaptive", "on":
		return Thinking{Mode: ThinkingAdaptive}, nil
	case "default":
		return Thinking{}, nil
	}
	if slices.Contains(EffortLevels, v) {
		return Thinking{Mode: ThinkingAdaptive, Effort: v}, nil
	}
	return def, fmt.Errorf("%s: %q is not off, adaptive, default or an effort level (%s); keeping %s", key, v,
		strings.Join(EffortLevels, ", "), def)
}

// ModelTemperatureFromEnv is TemperatureFromEnv for a tier on model. A
// model with FixedSampling gets no temperature, and one named for it is
// reported and not sent.
func ModelTemperatureFromEnv(lookup func(string) (string, bool), key, model string, def *float64) (*float64, error) {
	if !ParamsFor(model).FixedSampling {
		return TemperatureFromEnv(lookup, key, def)
	}
	if v, ok := lookup(key); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "", "off", "none", "default":
		default:
			return nil, fmt.Errorf("%s: %s takes no temperature but its own; sending none", key, strings.TrimSpace(model))
		}
	}
	return nil, nil
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

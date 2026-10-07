// Package livemeter records what the live evals send to the model gateway
// (OpenRouter's Anthropic-compatible Messages API) and what comes back. It
// is a local reverse proxy in front of ANTHROPIC_BASE_URL: point the shared
// client at URL() and every call goes through unchanged, while the meter
// notes its model, the routing it asked for (provider.zdr, a strict
// output_config.format, thinking), the provider that served it, tokens,
// cost and timing.
//
// It never records a key, a prompt or an answer: only metadata. The evals
// use it to check that every call was served by a zero-retention endpoint
// (plan 25 D14), by a host its tier pinned (provider.only) and at a
// precision its tier admitted (provider.quantizations), and to report each
// tier's latency and cost.
package livemeter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Call is one request through the meter.
type Call struct {
	Model    string
	Stream   bool
	ZDR      bool   // the request asked for provider.zdr
	Strict   bool   // the request carried output_config.format
	Thinking string // the request's thinking.type ("" when omitted)
	// Only and Quantizations are the request's provider.only (the hosts
	// it pinned) and provider.quantizations.
	Only          []string
	Quantizations []string
	// Temperature is the request's temperature (nil when omitted).
	Temperature *float64
	Status      int
	// Provider served the call (the response's "provider", OpenRouter's
	// provider name).
	Provider       string
	InputTokens    int
	OutputTokens   int
	ThinkingTokens int
	Cost           float64 // US dollars, as the gateway billed it
	// Latency is from the request reaching the meter to the response's
	// last byte; FirstToken, on a stream, to the first text delta.
	Latency    time.Duration
	FirstToken time.Duration
	// Error is the gateway's error message on a non-200 (truncated).
	Error string
}

// Meter is the recording proxy.
type Meter struct {
	srv    *httptest.Server
	target *url.URL
	mu     sync.Mutex
	calls  []Call
}

// Start runs a meter in front of target, the gateway's base URL (for
// OpenRouter, https://openrouter.ai/api).
func Start(target string) (*Meter, error) {
	u, err := url.Parse(strings.TrimRight(target, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("livemeter: target %q is not a base URL", target)
	}
	m := &Meter{target: u}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(u)
			pr.Out.Host = u.Host
			// Let the proxy's transport negotiate (and undo) compression, so
			// the meter reads plain bytes and the client gets plain bytes.
			pr.Out.Header.Del("Accept-Encoding")
		},
		FlushInterval: -1, // pass stream events on at once: first-token timing depends on it
	}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		call := requestCall(body)
		rec := &recorder{ResponseWriter: w, start: start, stream: call.Stream}
		proxy.ServeHTTP(rec, r)
		rec.finish(&call)
		call.Latency = time.Since(start)
		m.mu.Lock()
		m.calls = append(m.calls, call)
		m.mu.Unlock()
	}))
	return m, nil
}

// URL is the base URL to give the client.
func (m *Meter) URL() string { return m.srv.URL }

// Close stops the proxy.
func (m *Meter) Close() { m.srv.Close() }

// Calls returns the calls so far.
func (m *Meter) Calls() []Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
}

// Mark returns how many calls the meter has seen, for Since.
func (m *Meter) Mark() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// Since returns the calls after a Mark.
func (m *Meter) Since(mark int) []Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls[min(mark, len(m.calls)):])
}

func requestCall(body []byte) Call {
	var req struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Provider struct {
			ZDR           bool     `json:"zdr"`
			Only          []string `json:"only"`
			Quantizations []string `json:"quantizations"`
		} `json:"provider"`
		OutputConfig *struct {
			Format json.RawMessage `json:"format"`
		} `json:"output_config"`
		Thinking *struct {
			Type string `json:"type"`
		} `json:"thinking"`
		Temperature *float64 `json:"temperature"`
	}
	_ = json.Unmarshal(body, &req)
	c := Call{Model: req.Model, Stream: req.Stream, ZDR: req.Provider.ZDR,
		Strict: req.OutputConfig != nil && len(req.OutputConfig.Format) > 0,
		Only:   req.Provider.Only, Quantizations: req.Provider.Quantizations, Temperature: req.Temperature}
	if req.Thinking != nil {
		c.Thinking = req.Thinking.Type
	}
	return c
}

// recorder watches the response as it is written to the client.
type recorder struct {
	http.ResponseWriter
	start  time.Time
	stream bool
	status int
	buf    bytes.Buffer // a whole JSON body, or a stream's unfinished line
	ev     streamState
}

type streamState struct {
	provider          string
	in, out, thinking int
	cost              float64
	first             time.Duration
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	r.buf.Write(p)
	if r.stream && r.status == http.StatusOK {
		r.scanLines()
	}
	return r.ResponseWriter.Write(p)
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// usage is the gateway's usage object (Anthropic's, plus OpenRouter's cost).
type usage struct {
	InputTokens         int      `json:"input_tokens"`
	OutputTokens        int      `json:"output_tokens"`
	Cost                *float64 `json:"cost"`
	OutputTokensDetails struct {
		ThinkingTokens int `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

func (s *streamState) add(u usage) {
	if u.InputTokens > 0 {
		s.in = u.InputTokens
	}
	if u.OutputTokens > 0 {
		s.out = u.OutputTokens
	}
	if u.OutputTokensDetails.ThinkingTokens > 0 {
		s.thinking = u.OutputTokensDetails.ThinkingTokens
	}
	if u.Cost != nil {
		s.cost = *u.Cost
	}
}

func (r *recorder) scanLines() {
	for {
		line, err := r.buf.ReadString('\n')
		if err != nil { // no newline yet: keep the partial line
			rest := line
			r.buf.Reset()
			r.buf.WriteString(rest)
			return
		}
		line = strings.TrimSpace(line)
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok || data == "[DONE]" {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Provider string `json:"provider"`
				Usage    usage  `json:"usage"`
			} `json:"message"`
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
			Usage    usage  `json:"usage"`
			Provider string `json:"provider"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			r.ev.provider = ev.Message.Provider
			r.ev.add(ev.Message.Usage)
		case "content_block_delta":
			if ev.Delta.Text != "" && r.ev.first == 0 {
				r.ev.first = time.Since(r.start)
			}
		case "message_delta":
			r.ev.add(ev.Usage)
			if r.ev.provider == "" {
				r.ev.provider = ev.Provider
			}
		}
	}
}

func (r *recorder) finish(c *Call) {
	c.Status = r.status
	if c.Status != http.StatusOK {
		msg := r.buf.String()
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(r.buf.Bytes(), &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		c.Error = truncate(msg, 300)
		return
	}
	if r.stream {
		c.Provider, c.InputTokens, c.OutputTokens, c.ThinkingTokens, c.Cost, c.FirstToken =
			r.ev.provider, r.ev.in, r.ev.out, r.ev.thinking, r.ev.cost, r.ev.first
		return
	}
	var resp struct {
		Provider string `json:"provider"`
		Usage    usage  `json:"usage"`
	}
	if json.Unmarshal(r.buf.Bytes(), &resp) == nil {
		var s streamState
		s.add(resp.Usage)
		c.Provider, c.InputTokens, c.OutputTokens, c.ThinkingTokens, c.Cost = resp.Provider, s.in, s.out, s.thinking, s.cost
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Endpoint is one zero-retention endpoint of a model on OpenRouter.
type Endpoint struct {
	// Provider is the provider's name, as responses give it ("DeepInfra").
	Provider string
	// Tag is the endpoint's routing slug ("deepinfra/fp8"); its base
	// ("deepinfra") is what provider.only names.
	Tag string
	// Quantization is the precision the host declares ("fp8", "unknown").
	Quantization string
}

// ZDRList is OpenRouter's zero-retention endpoints: model slug → endpoints.
type ZDRList map[string][]Endpoint

// providerEndpoints are a model's zero-retention endpoints served under one
// provider name.
func (l ZDRList) providerEndpoints(model, provider string) []Endpoint {
	var out []Endpoint
	for _, e := range l[model] {
		if e.Provider == provider {
			out = append(out, e)
		}
	}
	return out
}

// ZDREndpoints is OpenRouter's list of zero-data-retention endpoints. The
// list is public (no key).
func ZDREndpoints(ctx context.Context) (ZDRList, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openrouter.ai/api/v1/endpoints/zdr", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("livemeter: ZDR endpoints: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("livemeter: ZDR endpoints: HTTP %d", resp.StatusCode)
	}
	var list struct {
		Data []struct {
			ModelID      string `json:"model_id"`
			Provider     string `json:"provider_name"`
			Tag          string `json:"tag"`
			Quantization string `json:"quantization"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("livemeter: ZDR endpoints: %w", err)
	}
	out := ZDRList{}
	for _, e := range list.Data {
		out[e.ModelID] = append(out[e.ModelID], Endpoint{Provider: e.Provider, Tag: e.Tag, Quantization: e.Quantization})
	}
	return out, nil
}

// pinnedBy reports whether one of eps answers to a slug in only: the slug
// is the endpoint's tag or its base ("baseten" pins "baseten/fp8").
func pinnedBy(eps []Endpoint, only []string) bool {
	for _, e := range eps {
		base, _, _ := strings.Cut(e.Tag, "/")
		if slices.Contains(only, e.Tag) || slices.Contains(only, base) {
			return true
		}
	}
	return false
}

// admittedBy reports whether one of eps runs at a precision quants admits.
func admittedBy(eps []Endpoint, quants []string) bool {
	for _, e := range eps {
		if slices.Contains(quants, e.Quantization) {
			return true
		}
	}
	return false
}

// Summary is one model's calls.
type Summary struct {
	Model                  string
	Calls, Errors          int
	ZDRAsked, Strict       int
	Providers              map[string]int
	NotZDR                 []string // providers that served a call but aren't zero-retention for the model
	Pinned                 int      // calls that named their hosts (provider.only)
	Unpinned               []string // providers that served a pinned call without being among its hosts
	BelowFloor             []string // providers whose endpoints all run below the call's quantizations
	Temperatures           map[string]int
	Latency, FirstToken    Quantiles
	In, Out, Thinking      int
	Cost                   float64
	ThinkingTypes          map[string]int
	ErrorSamples           []string
	MaxThinkingTokensACall int
}

// Quantiles are a duration distribution's p50, p95 and max.
type Quantiles struct{ P50, P95, Max time.Duration }

// QuantilesOf returns the p50, p95 and max of ds (nearest rank).
func QuantilesOf(ds []time.Duration) Quantiles {
	if len(ds) == 0 {
		return Quantiles{}
	}
	s := slices.Clone(ds)
	slices.Sort(s)
	at := func(q float64) time.Duration {
		i := int(q*float64(len(s))+0.999999) - 1
		return s[max(0, min(i, len(s)-1))]
	}
	return Quantiles{P50: at(0.50), P95: at(0.95), Max: s[len(s)-1]}
}

func (q Quantiles) String() string {
	r := func(d time.Duration) string { return d.Round(10 * time.Millisecond).String() }
	return fmt.Sprintf("p50 %s, p95 %s, max %s", r(q.P50), r(q.P95), r(q.Max))
}

// Summarize groups calls by model. zdr is ZDREndpoints' list (nil skips
// the routing checks).
func Summarize(calls []Call, zdr ZDRList) []Summary {
	by := map[string]*Summary{}
	lat := map[string][]time.Duration{}
	first := map[string][]time.Duration{}
	for _, c := range calls {
		s := by[c.Model]
		if s == nil {
			s = &Summary{Model: c.Model, Providers: map[string]int{}, ThinkingTypes: map[string]int{}, Temperatures: map[string]int{}}
			by[c.Model] = s
		}
		s.Calls++
		if c.ZDR {
			s.ZDRAsked++
		}
		if len(c.Only) > 0 {
			s.Pinned++
		}
		temp := "default"
		if c.Temperature != nil {
			temp = fmt.Sprint(*c.Temperature)
		}
		s.Temperatures[temp]++
		if c.Strict {
			s.Strict++
		}
		s.ThinkingTypes[c.Thinking]++
		if c.Status != http.StatusOK {
			s.Errors++
			if len(s.ErrorSamples) < 3 {
				s.ErrorSamples = append(s.ErrorSamples, fmt.Sprintf("%d %s", c.Status, c.Error))
			}
			continue
		}
		s.Providers[c.Provider]++
		if zdr != nil {
			eps := zdr.providerEndpoints(c.Model, c.Provider)
			if len(eps) == 0 && !slices.Contains(s.NotZDR, c.Provider) {
				s.NotZDR = append(s.NotZDR, c.Provider)
			}
			if len(eps) > 0 && len(c.Only) > 0 && !pinnedBy(eps, c.Only) && !slices.Contains(s.Unpinned, c.Provider) {
				s.Unpinned = append(s.Unpinned, c.Provider)
			}
			if len(eps) > 0 && len(c.Quantizations) > 0 && !admittedBy(eps, c.Quantizations) && !slices.Contains(s.BelowFloor, c.Provider) {
				s.BelowFloor = append(s.BelowFloor, c.Provider)
			}
		}
		s.In += c.InputTokens
		s.Out += c.OutputTokens
		s.Thinking += c.ThinkingTokens
		s.MaxThinkingTokensACall = max(s.MaxThinkingTokensACall, c.ThinkingTokens)
		s.Cost += c.Cost
		lat[c.Model] = append(lat[c.Model], c.Latency)
		if c.FirstToken > 0 {
			first[c.Model] = append(first[c.Model], c.FirstToken)
		}
	}
	out := make([]Summary, 0, len(by))
	for model, s := range by {
		s.Latency, s.FirstToken = QuantilesOf(lat[model]), QuantilesOf(first[model])
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}

// Format renders summaries as a small table.
func Format(sums []Summary) string {
	var b strings.Builder
	var total float64
	for _, s := range sums {
		total += s.Cost
		provs := make([]string, 0, len(s.Providers))
		for p, n := range s.Providers {
			provs = append(provs, fmt.Sprintf("%s %d", p, n))
		}
		sort.Strings(provs)
		temps := make([]string, 0, len(s.Temperatures))
		for t, n := range s.Temperatures {
			temps = append(temps, fmt.Sprintf("%s %d", t, n))
		}
		sort.Strings(temps)
		fmt.Fprintf(&b, "%s: %d calls (%d errors), zdr asked on %d, hosts pinned on %d, strict on %d; tokens in %d, out %d (thinking %d, max %d a call); $%.4f\n",
			s.Model, s.Calls, s.Errors, s.ZDRAsked, s.Pinned, s.Strict, s.In, s.Out, s.Thinking, s.MaxThinkingTokensACall, s.Cost)
		fmt.Fprintf(&b, "    served by: %s; temperature: %s\n", strings.Join(provs, ", "), strings.Join(temps, ", "))
		if len(s.NotZDR) > 0 {
			fmt.Fprintf(&b, "    NOT zero-retention for this model: %v\n", s.NotZDR)
		}
		if len(s.Unpinned) > 0 {
			fmt.Fprintf(&b, "    NOT among the pinned hosts: %v\n", s.Unpinned)
		}
		if len(s.BelowFloor) > 0 {
			fmt.Fprintf(&b, "    BELOW the precision floor: %v\n", s.BelowFloor)
		}
		fmt.Fprintf(&b, "    gateway latency %s", s.Latency)
		if s.FirstToken.Max > 0 {
			fmt.Fprintf(&b, "; first token %s", s.FirstToken)
		}
		b.WriteString("\n")
		for _, e := range s.ErrorSamples {
			fmt.Fprintf(&b, "    error: %s\n", e)
		}
	}
	fmt.Fprintf(&b, "total $%.4f\n", total)
	return b.String()
}

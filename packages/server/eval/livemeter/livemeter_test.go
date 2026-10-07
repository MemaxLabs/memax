package livemeter

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A fake gateway: JSON answers, a stream, and a refusal.
func fakeGateway(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/messages" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(body), `"refuse"`):
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"not_found_error","message":"No endpoints found matching your data policy"}}`)
		case strings.Contains(string(body), `"stream":true`):
			w.Header().Set("Content-Type", "text/event-stream")
			f := w.(http.Flusher)
			for _, ev := range []string{
				`{"type":"message_start","message":{"id":"gen-1","provider":"Fireworks","usage":{"input_tokens":120}}}`,
				`{"type":"content_block_delta","delta":{"type":"text_delta","text":"Jobs run"}}`,
				`{"type":"content_block_delta","delta":{"type":"text_delta","text":" on River."}}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9,"cost":0.00002}}`,
			} {
				fmt.Fprintf(w, "event: x\ndata: %s\n\n", ev)
				f.Flush()
				time.Sleep(5 * time.Millisecond)
			}
		default:
			_, _ = io.WriteString(w, `{"id":"gen-2","provider":"Amazon Bedrock","content":[{"type":"text","text":"{}"}],`+
				`"usage":{"input_tokens":300,"output_tokens":40,"output_tokens_details":{"thinking_tokens":12},"cost":0.0005}}`)
		}
	}))
}

func TestMeterRecordsMetadata(t *testing.T) {
	t.Parallel()
	gw := fakeGateway(t)
	defer gw.Close()
	m, err := Start(gw.URL + "/api")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	post := func(body string) *http.Response {
		t.Helper()
		resp, err := http.Post(m.URL()+"/v1/messages", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := post(`{"model":"anthropic/claude-haiku-4.5","provider":{"zdr":true},"output_config":{"format":{"type":"json_schema","schema":{}}}}`)
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(got), `"provider":"Amazon Bedrock"`) {
		t.Fatalf("the client got %s", got)
	}

	resp = post(`{"model":"deepseek/deepseek-v4.1-flash","stream":true,"thinking":{"type":"disabled"},"temperature":0,` +
		`"provider":{"zdr":true,"only":["together","deepinfra"],"order":["together","deepinfra"],"quantizations":["fp8","unknown"]}}`)
	var text strings.Builder
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		text.WriteString(sc.Text())
	}
	resp.Body.Close()
	if !strings.Contains(text.String(), "on River.") {
		t.Fatalf("the stream reached the client as %q", text.String())
	}

	resp = post(`{"model":"deepseek/deepseek-v4.1-flash","provider":{"zdr":true,"only":["refuse"]}}`)
	resp.Body.Close()

	calls := m.Calls()
	if len(calls) != 3 {
		t.Fatalf("%d calls", len(calls))
	}
	j, s, e := calls[0], calls[1], calls[2]
	if j.Provider != "Amazon Bedrock" || !j.ZDR || !j.Strict || j.InputTokens != 300 || j.OutputTokens != 40 || j.ThinkingTokens != 12 || j.Cost != 0.0005 {
		t.Errorf("json call = %+v", j)
	}
	if !s.Stream || s.Provider != "Fireworks" || s.Thinking != "disabled" || s.InputTokens != 120 || s.OutputTokens != 9 || s.Cost != 0.00002 ||
		s.FirstToken <= 0 || s.FirstToken >= s.Latency {
		t.Errorf("stream call = %+v", s)
	}
	if strings.Join(s.Only, ",") != "together,deepinfra" || strings.Join(s.Quantizations, ",") != "fp8,unknown" ||
		s.Temperature == nil || *s.Temperature != 0 || j.Temperature != nil || j.Only != nil {
		t.Errorf("routing: stream %+v, json %+v", s, j)
	}
	if e.Status != http.StatusNotFound || !strings.Contains(e.Error, "data policy") {
		t.Errorf("refused call = %+v", e)
	}

	zdr := ZDRList{
		"deepseek/deepseek-v4.1-flash": {{Provider: "Fireworks", Tag: "fireworks", Quantization: "unknown"}},
		"anthropic/claude-haiku-4.5":   {{Provider: "Google", Tag: "google-vertex/global", Quantization: "unknown"}},
	}
	sums := Summarize(calls, zdr)
	if len(sums) != 2 {
		t.Fatalf("%d summaries", len(sums))
	}
	haiku, ds := sums[0], sums[1]
	if len(haiku.NotZDR) != 1 || haiku.NotZDR[0] != "Amazon Bedrock" {
		t.Errorf("Bedrock isn't in the fake ZDR list for Haiku: %+v", haiku)
	}
	if ds.Calls != 2 || ds.Errors != 1 || len(ds.NotZDR) != 0 || ds.FirstToken.Max <= 0 || ds.Pinned != 2 {
		t.Errorf("deepseek summary = %+v", ds)
	}
	// Fireworks served a call that pinned Together and DeepInfra.
	if strings.Join(ds.Unpinned, ",") != "Fireworks" || len(ds.BelowFloor) != 0 {
		t.Errorf("deepseek pinning: unpinned %v, below the floor %v", ds.Unpinned, ds.BelowFloor)
	}
	if f := Format(sums); !strings.Contains(f, "total $0.0005") || !strings.Contains(f, "NOT among the pinned hosts: [Fireworks]") {
		t.Errorf("format:\n%s", f)
	}

	// The same call from a host that is pinned, but only at fp4.
	zdr["deepseek/deepseek-v4.1-flash"] = []Endpoint{{Provider: "Fireworks", Tag: "deepinfra/fp4", Quantization: "fp4"}}
	ds = Summarize(calls, zdr)[1]
	if len(ds.Unpinned) != 0 || strings.Join(ds.BelowFloor, ",") != "Fireworks" {
		t.Errorf("a pinned fp4 host: unpinned %v, below the floor %v", ds.Unpinned, ds.BelowFloor)
	}
}

func TestQuantiles(t *testing.T) {
	t.Parallel()
	var ds []time.Duration
	for i := 1; i <= 20; i++ {
		ds = append(ds, time.Duration(i)*time.Second)
	}
	q := QuantilesOf(ds)
	if q.P50 != 10*time.Second || q.P95 != 19*time.Second || q.Max != 20*time.Second {
		t.Errorf("%+v", q)
	}
	if (QuantilesOf(nil) != Quantiles{}) {
		t.Error("empty")
	}
}

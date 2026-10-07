// Package askeval scores ⌘K Ask on the V2 record (internal/ask, plan 25
// §5.11, §11) over a small corpus: citation validity (an answer never
// cites a memory it wasn't given), not covered (a question the record
// can't answer says so instead of guessing), exclusion (superseded
// decisions, quarantined memories and other spaces never reach the
// model), and whether answers cite the memory that answers them.
//
//	go test ./eval/ask/ -v                                              # a deterministic fake model (CI)
//	ASK_EVAL_LIVE=1 ANTHROPIC_API_KEY=… ANTHROPIC_BASE_URL=… go test ./eval/ask/ -v   # also the real answer tier (ASK_MODEL)
//	ASK_EVAL_LIVE=1 V2_EVAL_LIVE=1 … VOYAGE_API_KEY=… go test ./eval/ask/ -v          # and again on hybrid search
//
// The fake model answers like a careful one: it cites the memories that
// share a content word with the question, and says NOT_COVERED when none
// does, but it also invents two citations every time ([M-9999], [N-0882])
// so the citation filter is exercised. Its numbers check the pipeline,
// not answer quality; the live run is the gate for the answer tier. It
// streams through eval/livemeter, which checks each answer's routing (zero
// retention) and reports cost and the gateway's first token. Results are in
// RESULTS.md.
package askeval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/eval/livemeter"
	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/ask"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2index"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type corpus struct {
	Version     int    `json:"version"`
	Description string `json:"description"`
	Memories    []struct {
		ID        string `json:"id"`
		Space     string `json:"space"`
		Section   string `json:"section"`
		Kind      string `json:"kind"`
		Status    string `json:"status"`
		External  bool   `json:"external"`
		Statement string `json:"statement"`
	} `json:"memories"`
	Questions []struct {
		ID       string   `json:"id"`
		Type     string   `json:"type"`
		Question string   `json:"question"`
		Cite     []string `json:"cite"`
		Never    []string `json:"never"`
	} `json:"questions"`
}

func loadCorpus(t *testing.T) corpus {
	t.Helper()
	raw, err := os.ReadFile("corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var c corpus
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		t.Fatalf("corpus.json: %v", err)
	}
	return c
}

func TestCorpusLoads(t *testing.T) {
	t.Parallel()
	c := loadCorpus(t)
	ids := map[string]bool{}
	for _, m := range c.Memories {
		if ids[m.ID] || (m.Space != "mine" && m.Space != "theirs") || !ledger.Section(m.Section).Valid() {
			t.Errorf("memory %s: duplicate, or bad space or section", m.ID)
		}
		ids[m.ID] = true
	}
	types := map[string]int{}
	for _, q := range c.Questions {
		types[q.Type]++
		for _, id := range append(slices.Clone(q.Cite), q.Never...) {
			if !ids[id] {
				t.Errorf("%s names unknown memory %s", q.ID, id)
			}
		}
		if (q.Type == "answerable") != (len(q.Cite) > 0) {
			t.Errorf("%s: an answerable question names what to cite, and only it", q.ID)
		}
	}
	for _, want := range []string{"answerable", "not_covered", "trap"} {
		if types[want] == 0 {
			t.Errorf("no %s questions", want)
		}
	}
}

// overlapModel is the fake answer tier.
type overlapModel struct{}

var (
	memoryBlock = regexp.MustCompile(`(?s)<memory id="([^"]+)"[^>]*>\n(.*?)\n</memory>`)
	word        = regexp.MustCompile(`[\p{L}\p{N}]+`)
	stop        = map[string]bool{"what": true, "which": true, "when": true, "where": true, "does": true, "this": true,
		"that": true, "with": true, "from": true, "should": true, "have": true, "into": true, "many": true, "days": true,
		"week": true, "there": true, "every": true, "never": true}
)

func stems(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range word.FindAllString(strings.ToLower(s), -1) {
		if len(w) < 4 || stop[w] {
			continue
		}
		out[w[:min(len(w), 5)]] = true
	}
	return out
}

func (overlapModel) Stream(_ context.Context, c ask.Call, onText func(string)) (ask.Usage, error) {
	q := c.Prompt[strings.LastIndex(c.Prompt, "Question: ")+len("Question: "):]
	want := stems(q)
	var cited []string
	for _, m := range memoryBlock.FindAllStringSubmatch(c.Prompt, -1) {
		for s := range stems(m[2]) {
			if want[s] {
				cited = append(cited, m[1])
				break
			}
		}
	}
	if len(cited) == 0 {
		onText("NOT_COVERED")
		return ask.Usage{InputTokens: len(c.Prompt) / 4, OutputTokens: 3}, nil
	}
	for _, ref := range cited {
		onText("The record says so.[" + ref + "] ")
	}
	onText("It was decided long ago.[M-9999][N-0882]")
	return ask.Usage{InputTokens: len(c.Prompt) / 4, OutputTokens: 20 * len(cited)}, nil
}

type outcome struct {
	sources, cited []string // fixture ids
	outcome        ask.Outcome
	dropped        int
	firstToken     time.Duration
	failed         bool
}

type mode struct {
	name   string
	model  ask.Model
	cfg    ask.Config
	search *v2recall.Searcher
}

func TestAskEval(t *testing.T) {
	if testing.Short() {
		t.Skip("ask eval: skipped in -short")
	}
	c := loadCorpus(t)
	_, pool := testdb.Acquire(t)
	ctx := context.Background()
	l := ledger.New(pool, ledger.WithLogger(quiet))
	newUser := func(name string) uuid.UUID {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`, id, id.String()[:8]+"@"+name+".test", name); err != nil {
			t.Fatal(err)
		}
		return id
	}
	newSpace := func(owner uuid.UUID, name string) uuid.UUID {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, $2, $3, 'team', $4, 'project')`,
			id, name, id.String(), owner); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, owner); err != nil {
			t.Fatal(err)
		}
		return id
	}
	me, them := newUser("zz"), newUser("other")
	spaces := map[string]uuid.UUID{"mine": newSpace(me, "memax-v2"), "theirs": newSpace(them, "other-team")}
	owners := map[string]uuid.UUID{"mine": me, "theirs": them}
	fixtureOf := map[string]string{} // memory id → fixture id (display IDs repeat across tenants)
	spaceOf := map[string]string{}   // fixture id → space
	for _, m := range c.Memories {
		scope, err := l.UserScope(ctx, owners[m.Space])
		if err != nil {
			t.Fatal(err)
		}
		nm := ledger.NewMemory{SpaceID: spaces[m.Space], Statement: m.Statement, Section: ledger.Section(m.Section), Kind: ledger.KindFact}
		if m.Kind == "decision" {
			nm.Kind = ledger.KindDecision
			nm.Decision = &ledger.DecisionFields{Status: m.Status}
		}
		if m.External {
			nm.Sources = []ledger.SourceInput{{Kind: ledger.SourceURL, Ref: "a blog post", URI: "https://example.com/deploys"}}
		}
		res, err := l.Apply(ctx, &ledger.Remember{Meta: ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: owners[m.Space], Name: "zz"},
			Scope: scope, Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()}, NewMemory: nm})
		if err != nil || res.Memory == nil {
			t.Fatalf("%s: %v %+v", m.ID, err, res.Policy)
		}
		fixtureOf[res.Memory.ID.String()] = m.ID
		spaceOf[m.ID] = m.Space
	}
	scope, err := l.UserScope(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	grant, _ := scope.Grant(spaces["mine"])

	lexical := v2recall.New(l)
	modes := []mode{{"fake (overlap)", overlapModel{}, ask.Config{Model: "fake/overlap", Log: quiet}, lexical}}
	live := os.Getenv("ASK_EVAL_LIVE") == "1"
	var meter *livemeter.Meter
	if live {
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			t.Fatal("ASK_EVAL_LIVE=1 needs ANTHROPIC_API_KEY (and ANTHROPIC_BASE_URL for OpenRouter)")
		}
		base := os.Getenv("ANTHROPIC_BASE_URL")
		if base == "" {
			base = anthropic.DefaultBaseURL
		}
		// The meter sits between the shared client and the gateway: it sees
		// each stream's routing, provider, cost and first token, and changes
		// nothing.
		if meter, err = livemeter.Start(base); err != nil {
			t.Fatal(err)
		}
		defer meter.Close()
		client := anthropic.New(key, meter.URL())
		cfg := ask.ConfigFromEnv(os.LookupEnv)
		if cfg.Model == "" {
			t.Fatal("ASK_EVAL_LIVE=1 needs an ASK_MODEL other than off")
		}
		cfg.Log = quiet
		model := ask.NewAnthropicModel(client, cfg.ZeroDataRetention)
		modes = append(modes, mode{"live " + cfg.Model, model, cfg, lexical})
		// With Voyage too, the search production Ask runs: hybrid, at the
		// recall floor.
		if vcfg := v2index.ConfigFromEnv(os.LookupEnv); os.Getenv("V2_EVAL_LIVE") == "1" && vcfg.Enabled() {
			ix := v2index.New(l, vcfg.IndexEmbedder(), vcfg.IndexModel, 128, quiet)
			for _, sp := range spaces {
				for {
					n, err := ix.Index(ctx, ledger.IndexArgs{SpaceID: sp})
					if err != nil {
						t.Fatal(err)
					}
					if n == 0 {
						break
					}
				}
			}
			vc := v2recall.VectorConfigFromEnv(os.LookupEnv, vcfg.IndexModel)
			vc.Log = quiet
			hybrid := v2recall.New(l).WithVectors(v2recall.NewVectors(l, vcfg.QueryEmbedder(), vcfg.IndexEmbedder(), vc))
			modes = append(modes, mode{"live " + cfg.Model + ", hybrid search", model, cfg, hybrid})
		}
	}

	type score struct {
		invalid, leaked, dropped, failed int
		answerable, cited                int
		uncovered, saidSo                int
		traps                            int
		firstTokens                      []time.Duration
		missed, guessed, leaks, invalidQ []string
	}
	scores := map[string]*score{}
	for _, m := range modes {
		svc := ask.New(l, m.search, m.model, m.cfg)
		s := &score{}
		scores[m.name] = s
		for _, q := range c.Questions {
			o := run(t, svc, ask.Request{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: me, Name: "zz"}, Via: policy.ViaWeb,
				Scope: scope, Space: grant, SpaceName: "memax-v2", Question: q.Question}, fixtureOf)
			if o.failed {
				s.failed++
				continue
			}
			s.dropped += o.dropped
			if o.firstToken > 0 {
				s.firstTokens = append(s.firstTokens, o.firstToken)
			}
			for _, id := range o.cited {
				if !slices.Contains(o.sources, id) {
					s.invalid++
					s.invalidQ = append(s.invalidQ, q.ID+":"+id)
				}
			}
			for _, id := range q.Never {
				if slices.Contains(o.sources, id) || slices.Contains(o.cited, id) {
					s.leaked++
					s.leaks = append(s.leaks, q.ID+":"+id)
				}
			}
			for _, id := range o.sources {
				if spaceOf[id] != "mine" {
					s.leaked++
					s.leaks = append(s.leaks, q.ID+":"+id)
				}
			}
			switch q.Type {
			case "answerable":
				s.answerable++
				all := o.outcome == ask.OutcomeAnswered
				for _, id := range q.Cite {
					all = all && slices.Contains(o.cited, id)
				}
				if all {
					s.cited++
				} else {
					s.missed = append(s.missed, fmt.Sprintf("%s(%s %v)", q.ID, o.outcome, o.cited))
				}
			case "not_covered":
				s.uncovered++
				if o.outcome == ask.OutcomeNotCovered || o.outcome == ask.OutcomeUnsupported {
					s.saidSo++
				} else {
					s.guessed = append(s.guessed, fmt.Sprintf("%s(%s %v)", q.ID, o.outcome, o.cited))
				}
			case "trap":
				s.traps++
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Ask over %d statements, %d questions\n", len(c.Memories), len(c.Questions))
	fmt.Fprintf(&b, "%-40s %8s %8s %9s %8s %8s %8s %10s %10s\n", "mode", "cited", "said so", "invalid", "leaked", "dropped", "failed",
		"1st tok p50", "p95")
	for _, m := range modes {
		s := scores[m.name]
		p50, p95 := "-", "-"
		if len(s.firstTokens) > 0 {
			q := livemeter.QuantilesOf(s.firstTokens)
			p50, p95 = q.P50.Round(time.Millisecond).String(), q.P95.Round(time.Millisecond).String()
		}
		fmt.Fprintf(&b, "%-40s %4d/%-3d %4d/%-3d %9d %8d %8d %8d %10s %10s\n", m.name, s.cited, s.answerable, s.saidSo, s.uncovered,
			s.invalid, s.leaked, s.dropped, s.failed, p50, p95)
		if len(s.missed)+len(s.guessed)+len(s.leaks)+len(s.invalidQ) > 0 {
			fmt.Fprintf(&b, "    missed %v; guessed %v; leaked %v; invalid %v\n", s.missed, s.guessed, s.leaks, s.invalidQ)
		}
	}
	t.Log(b.String())
	if meter != nil {
		// Every answer streamed through a zero-retention endpoint of its
		// model (plan 25 D14), and asked for one.
		zdr, err := livemeter.ZDREndpoints(ctx)
		if err != nil {
			t.Logf("no ZDR list, so the routing check is skipped: %v", err)
		}
		calls := meter.Calls()
		t.Log("gateway (first token here is the gateway's, before the server's own work):\n" + livemeter.Format(livemeter.Summarize(calls, zdr)))
		for _, s := range livemeter.Summarize(calls, zdr) {
			if s.ZDRAsked != s.Calls {
				t.Errorf("%s: %d of %d streams didn't ask for zero retention", s.Model, s.Calls-s.ZDRAsked, s.Calls)
			}
			if len(s.NotZDR) > 0 {
				t.Errorf("%s was served by providers that aren't zero-retention for it: %v", s.Model, s.NotZDR)
			}
		}
		for _, cl := range calls {
			if !cl.Stream {
				t.Errorf("%s: an answer that didn't stream", cl.Model)
			}
		}
	}

	for _, m := range modes {
		s := scores[m.name]
		// What the server guarantees, whatever the model does.
		if s.invalid != 0 {
			t.Errorf("%s: %d citations of memories the model wasn't given reached the person", m.name, s.invalid)
		}
		if s.leaked != 0 {
			t.Errorf("%s: %d superseded, quarantined or other-space memories reached an answer: %v", m.name, s.leaked, s.leaks)
		}
		if s.failed != 0 {
			t.Errorf("%s: %d answers failed", m.name, s.failed)
		}
		bar := 1.0
		if strings.HasPrefix(m.name, "live") {
			// The answer tier's bars (plan §11: an answer the record can't
			// support says so). Calibrate once the live run has a history.
			bar = 0.75
			if q := livemeter.QuantilesOf(s.firstTokens); q.P50 > 1500*time.Millisecond {
				t.Errorf("%s: first token p50 %v, over the 1.5 s budget", m.name, q.P50)
			}
		} else if s.dropped == 0 {
			t.Errorf("%s: no citation was dropped; the fake invents two per answer", m.name)
		}
		if got := float64(s.cited) / float64(s.answerable); got < bar {
			t.Errorf("%s: cited the answering memory on %.2f of answerable questions, want ≥ %.2f", m.name, got, bar)
		}
		if got := float64(s.saidSo) / float64(s.uncovered); got < bar {
			t.Errorf("%s: said 'not covered' on %.2f of questions the record can't answer, want ≥ %.2f", m.name, got, bar)
		}
	}
}

// run asks one question and reads the stream back.
func run(t *testing.T, svc *ask.Service, r ask.Request, fixtureOf map[string]string) outcome {
	t.Helper()
	ctx := context.Background()
	r.Started = time.Now()
	p, err := svc.Prepare(ctx, r)
	if err != nil {
		t.Fatalf("prepare %q: %v", r.Question, err)
	}
	var o outcome
	idOf := map[string]string{} // ref → memory id, within this answer's sources
	svc.Answer(ctx, p, func(name string, data any) error {
		switch d := data.(type) {
		case ask.SourcesEvent:
			for _, s := range d.Sources {
				o.sources = append(o.sources, fixtureOf[s.ID.String()])
				idOf[s.Ref] = s.ID.String()
			}
		case ask.DeltaEvent:
			if o.firstToken == 0 {
				o.firstToken = time.Since(r.Started)
			}
		case ask.DoneEvent:
			o.outcome, o.dropped = d.Outcome, d.Dropped
			for _, ref := range d.Cited {
				// A ref that isn't among the sources maps to nothing, which
				// the validity check counts as invalid.
				o.cited = append(o.cited, fixtureOf[idOf[ref]])
			}
		case ask.ErrorEvent:
			o.failed = true
		}
		return nil
	})
	return o
}

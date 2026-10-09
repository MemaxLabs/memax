package judge_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed/mockembed"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/v2index"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

// synonyms give the bag-of-words embedder a meaning the lexical lanes
// can't see (mockembed.Words).
var synonyms = [][]string{{"river", "queue"}, {"job", "task"}, {"background", "async"}}

// index embeds every waiting version of the space, as the index jobs do.
func (f *fixture) index(e *mockembed.Words, space uuid.UUID) {
	f.t.Helper()
	ix := v2index.New(f.l, e, "voyage-4", 64, quiet)
	for {
		n, err := ix.Index(f.ctx, ledger.IndexArgs{SpaceID: space})
		if err != nil {
			f.t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

// prompts records what the model was asked, and answers from a table.
type prompts struct {
	mu    sync.Mutex
	asked []string
	table map[string]verdict
}

func (p *prompts) Complete(_ context.Context, c judge.Call) (string, error) {
	p.mu.Lock()
	p.asked = append(p.asked, c.Prompt)
	p.mu.Unlock()
	return answerFor(c.Prompt, p.table, nil), nil
}

// The vector candidates add what the lexical lanes miss: a proposal that
// shares no word with the kept memory it updates. Lexically the judge has
// no candidate, asks no model and leaves it alone; with vectors (from the
// same embeddings recall searches) the kept memory is a candidate, the
// model is asked about it, and the update is linked. A vector neighbour
// below the floor stays out.
func TestVectorCandidatesAddWhatLexicalMissed(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	e := mockembed.NewWords(synonyms...)
	river := f.kept(zz, sp, fact("Background jobs run on River."))
	f.kept(zz, sp, fact("The web app is a Next.js project."))
	weak := f.kept(zz, sp, fact("Background colours follow the Paper theme."))
	f.index(e, sp)
	cfg := fakeTiers()

	lexicalModel := &prompts{table: map[string]verdict{river.Ref: {relation: ledger.RelationUpdates, confidence: 0.9}}}
	lexical := withModel(f, lexicalModel, cfg)
	p1 := f.propose(zz, sp, fact("Async tasks use the queue."))
	if r := f.run(lexical, p1); r.Candidates != 0 || r.Outcome != ledger.OutcomeNone || len(lexicalModel.asked) != 0 {
		t.Fatalf("lexical only: %d candidates, %s, %d calls; the words share nothing, so there should be none",
			r.Candidates, r.Outcome, len(lexicalModel.asked))
	}

	vectorModel := &prompts{table: map[string]verdict{river.Ref: {relation: ledger.RelationUpdates, confidence: 0.9}}}
	cfg.Log = quiet
	withVectors := judge.New(f.l, vectorModel, cfg,
		judge.WithVectors(v2recall.NewVectors(f.l, e, e, v2recall.VectorConfig{Model: "voyage-4", Log: quiet})))
	p2 := f.propose(zz, sp, fact("Async tasks use the queue!"))
	f.index(e, sp)
	r := f.run(withVectors, p2)
	if r.Outcome != ledger.OutcomeLinked || r.Target != river.Ref {
		t.Fatalf("with vectors: %+v", r)
	}
	if len(vectorModel.asked) == 0 || !strings.Contains(vectorModel.asked[0], `<candidate id="`+river.Ref+`"`) {
		t.Fatalf("the model wasn't asked about %s", river.Ref)
	}
	if strings.Contains(vectorModel.asked[0], `<candidate id="`+weak.Ref+`"`) {
		t.Errorf("%s (one shared word, below the 0.65 floor) was a candidate", weak.Ref)
	}
	got := f.get(zz, p2.ID)
	if got.Judge == nil || got.Judge.Related == nil || got.Judge.Related.Ref != river.Ref {
		t.Errorf("judge = %+v", got.Judge)
	}
	var raw []byte
	if err := f.pool.QueryRow(f.ctx, `SELECT candidates FROM v2.judge_verdicts WHERE memory_id = $1`, p2.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var cands []ledger.VerdictCandidate
	if err := json.Unmarshal(raw, &cands); err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].Ref != river.Ref || len(cands[0].Sets) != 1 || cands[0].Sets[0] != "vector" {
		t.Errorf("candidates = %+v, want %s from the vector set alone", cands, river.Ref)
	}
}

func fakeTiers() judge.Config {
	return judge.Config{Primary: judge.Tier{Model: "fake-primary"}, Fallback: judge.Tier{Model: "fake-fallback", Strict: true},
		Strong: judge.Tier{Model: "fake-strong", Strict: true}}
}

// The floor is configuration (JUDGE_VECTOR_FLOOR, default 0.65): a higher
// floor drops the same neighbour.
func TestVectorFloorIsConfig(t *testing.T) {
	t.Parallel()
	if c := judge.ConfigFromEnv(func(string) (string, bool) { return "", false }); c.VectorFloor != 0 {
		t.Errorf("unset floor = %v (withDefaults applies 0.65)", c.VectorFloor)
	}
	if c := judge.ConfigFromEnv(func(k string) (string, bool) {
		if k == "JUDGE_VECTOR_FLOOR" {
			return "0.8", true
		}
		return "", false
	}); c.VectorFloor != 0.8 {
		t.Errorf("JUDGE_VECTOR_FLOOR=0.8: %v", c.VectorFloor)
	}
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, "memax-v2")
	e := mockembed.NewWords(synonyms...)
	river := f.kept(zz, sp, fact("Background jobs run on River."))
	p := f.propose(zz, sp, fact("Async tasks use the queue."))
	f.index(e, sp)
	model := &prompts{table: map[string]verdict{river.Ref: {relation: ledger.RelationUpdates, confidence: 0.9}}}
	cfg := fakeTiers()
	cfg.Log, cfg.VectorFloor = quiet, 0.9 // the pair is about 0.75 similar
	j := judge.New(f.l, model, cfg, judge.WithVectors(v2recall.NewVectors(f.l, e, e, v2recall.VectorConfig{Model: "voyage-4", Log: quiet})))
	if r := f.run(j, p); r.Candidates != 0 || len(model.asked) != 0 {
		t.Errorf("floor 0.9: %d candidates, %d calls", r.Candidates, len(model.asked))
	}
}

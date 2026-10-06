package judgeeval

import (
	"context"
	"fmt"
	"html"
	"io"
	"log/slog"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed/mockembed"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2index"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

// The judge's candidate sets on the labelled pairs (plan 25 §5.8): every
// pair's candidate is kept in one space with all the others (105 distinct
// statements), each proposal is judged there, and we count whether the
// labelled candidate reached the model (or stage 0 folded into it), with
// lexical candidates only and with the vector set added.
//
//	go test ./eval/judge/ -run Candidates -v                            # fake embedder (bag of words): the plumbing
//	V2_EVAL_LIVE=1 VOYAGE_API_KEY=… go test ./eval/judge/ -run Candidates -v   # voyage-4: the real gain, and the floors
//
// The fake embedder shares the lexical lanes' view of words, so its gain
// says only that the vector set flows through; the live run is the
// measurement, and it also prints each class's cosine similarities, to
// calibrate JUDGE_VECTOR_FLOOR (0.65) and V2_NEAR_DUPLICATE_FLOOR (0.90).

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

const evalModel = "voyage-4"

// liveEmbedder is Voyage with the index model, when the live run is asked
// for; else the bag-of-words fake.
func liveEmbedder(t *testing.T) (embed.Embedder, string) {
	t.Helper()
	if os.Getenv("V2_EVAL_LIVE") != "1" {
		return mockembed.NewWords(), "fake (bag of words)"
	}
	cfg := v2index.ConfigFromEnv(os.LookupEnv)
	e := cfg.IndexEmbedder()
	if e == nil {
		t.Fatal("V2_EVAL_LIVE=1 needs VOYAGE_API_KEY")
	}
	return e, "live (" + cfg.IndexModel + ")"
}

// recorder is a stage-1 model that records which candidates it was asked
// about and calls everything unrelated, so no verdict changes the record.
type recorder struct {
	mu    sync.Mutex
	asked map[string][]string // proposal statement → candidate refs
}

var candidateID = regexp.MustCompile(`<candidate id="(M-\d+)"`)
var proposalText = regexp.MustCompile(`(?s)<proposal[^>]*>\n(.*?)\n</proposal>`)

func (r *recorder) Complete(_ context.Context, c judge.Call) (string, error) {
	var refs []string
	for _, m := range candidateID.FindAllStringSubmatch(c.Prompt, -1) {
		refs = append(refs, m[1])
	}
	p := proposalText.FindStringSubmatch(c.Prompt)
	if p == nil {
		return "", fmt.Errorf("no proposal in the prompt")
	}
	r.mu.Lock()
	r.asked[html.UnescapeString(p[1])] = refs
	r.mu.Unlock()
	pairs := make([]string, len(refs))
	for i, ref := range refs {
		pairs[i] = fmt.Sprintf(`{"candidate":%q,"relation":"unrelated","confidence":0.9,"explicit_change":false,"rationale":"x","merged_statement":""}`, ref)
	}
	return `{"pairs":[` + strings.Join(pairs, ",") + `],"conditions":[]}`, nil
}

func memoryFor(s side, space uuid.UUID) ledger.NewMemory {
	nm := ledger.NewMemory{SpaceID: space, Statement: s.Statement, Section: ledger.SectionConventions, Kind: ledger.KindFact}
	if s.Kind == "decision" {
		nm.Section, nm.Kind = ledger.SectionDecisions, ledger.KindDecision
		status := ledger.DecisionOpen
		if s.InForce {
			status = ledger.DecisionInForce
		}
		nm.Decision = &ledger.DecisionFields{Area: s.Area, Status: status}
	}
	return nm
}

func TestCandidates(t *testing.T) {
	if testing.Short() {
		t.Skip("candidates: skipped in -short")
	}
	pairs := load(t)
	emb, mode := liveEmbedder(t)
	_, pool := testdb.Acquire(t)
	ctx := context.Background()
	l := ledger.New(pool, ledger.WithLogger(quiet))
	user := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, 'eval')`, user, user.String()[:8]+"@eval.test"); err != nil {
		t.Fatal(err)
	}
	newSpace := func(name string) uuid.UUID {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, $2, $3, 'team', $4, 'project')`,
			id, name, id.String(), user); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, user); err != nil {
			t.Fatal(err)
		}
		return id
	}
	lexicalSpace, vectorSpace := newSpace("lexical"), newSpace("vector")
	scope, err := l.UserScope(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(cmd ledger.Command) *ledger.Memory {
		t.Helper()
		res, err := l.Apply(ctx, cmd)
		if err != nil || res.Memory == nil {
			t.Fatalf("%s: %v %+v", cmd.Name(), err, res.Policy)
		}
		return res.Memory
	}
	person := ledger.Actor{Kind: policy.ActorPerson, ID: user, Name: "eval"}
	agent := ledger.Actor{Kind: policy.ActorAgent, ID: uuid.New(), Name: "Codex", Agent: "codex", Autonomy: policy.AutonomyPropose}
	meta := func(a ledger.Actor) ledger.Meta {
		return ledger.Meta{Actor: a, Scope: scope, Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()}
	}
	targets := map[uuid.UUID]map[string]string{} // space → candidate statement → ref
	for _, sp := range []uuid.UUID{lexicalSpace, vectorSpace} {
		targets[sp] = map[string]string{}
		for _, p := range pairs {
			if _, ok := targets[sp][p.Candidate.Statement]; ok {
				continue
			}
			m := apply(&ledger.Remember{Meta: meta(person), NewMemory: memoryFor(p.Candidate, sp)})
			targets[sp][p.Candidate.Statement] = m.Ref
		}
	}
	proposals := map[uuid.UUID]map[string]*ledger.Memory{}
	for _, sp := range []uuid.UUID{lexicalSpace, vectorSpace} {
		proposals[sp] = map[string]*ledger.Memory{}
		for _, p := range pairs {
			m := meta(agent)
			m.Via = policy.ViaMCP
			proposals[sp][p.ID] = apply(&ledger.Propose{Meta: m, NewMemory: memoryFor(side{Statement: p.Proposal.Statement,
				Kind: p.Proposal.Kind, Area: p.Proposal.Area}, sp)})
		}
	}
	ix := v2index.New(l, emb, evalModel, 128, quiet)
	for {
		n, err := ix.Index(ctx, ledger.IndexArgs{SpaceID: vectorSpace})
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	cfg := judge.Config{Primary: judge.Tier{Model: "recorder"}, Log: quiet}
	run := func(sp uuid.UUID, opts ...judge.Option) (map[string]bool, map[string]int) {
		rec := &recorder{asked: map[string][]string{}}
		j := judge.New(l, rec, cfg, opts...)
		found, sizes := map[string]bool{}, map[string]int{}
		for _, p := range pairs {
			m := proposals[sp][p.ID]
			r, err := j.Run(ctx, ledger.JudgeArgs{MemoryID: m.ID, SpaceID: sp, Version: 1, Mode: ledger.JudgeProposal}, judge.RunOptions{})
			if err != nil {
				t.Fatalf("%s: %v", p.ID, err)
			}
			want := targets[sp][p.Candidate.Statement]
			if r.Stage == ledger.StageExact || r.Stage == ledger.StageNear {
				found[p.ID] = r.Target == want
				continue
			}
			refs := rec.asked[m.Statement]
			sizes[p.ID] = len(refs)
			for _, ref := range refs {
				found[p.ID] = found[p.ID] || ref == want
			}
		}
		return found, sizes
	}
	lexFound, _ := run(lexicalSpace)
	vecFound, vecSizes := run(vectorSpace, judge.WithVectors(v2recall.NewVectors(l, emb, emb, v2recall.VectorConfig{Model: evalModel, Log: quiet})))

	var b strings.Builder
	fmt.Fprintf(&b, "judge candidates over %d pairs, %d kept candidates per space, embedder %s\n", len(pairs), len(targets[lexicalSpace]), mode)
	fmt.Fprintf(&b, "%-12s %5s %9s %9s %6s\n", "class", "pairs", "lexical", "+vector", "gain")
	var related, lex, vec int
	var gained []string
	for _, c := range classes {
		var n, l1, v1 int
		for _, p := range pairs {
			if p.Class != c {
				continue
			}
			n++
			if lexFound[p.ID] {
				l1++
			}
			if vecFound[p.ID] {
				v1++
			}
			if c != ledger.RelationUnrelated && vecFound[p.ID] && !lexFound[p.ID] {
				gained = append(gained, p.ID)
			}
		}
		fmt.Fprintf(&b, "%-12s %5d %9d %9d %+6d\n", c, n, l1, v1, v1-l1)
		if c != ledger.RelationUnrelated {
			related += n
			lex += l1
			vec += v1
		}
	}
	var missed []string
	for _, p := range pairs {
		if p.Class != ledger.RelationUnrelated && !lexFound[p.ID] {
			missed = append(missed, p.ID)
		}
	}
	fmt.Fprintf(&b, "related pairs (all but unrelated): candidate recall %.2f lexical → %.2f with vectors (%d gained: %v)\n",
		float64(lex)/float64(related), float64(vec)/float64(related), len(gained), gained)
	fmt.Fprintf(&b, "related pairs the lexical lanes miss (what vectors can win): %v\n", missed)
	total := 0
	for _, n := range vecSizes {
		total += n
	}
	fmt.Fprintf(&b, "mean candidates per model call with vectors: %.1f (cap %d + keyed decisions)\n",
		float64(total)/float64(max(len(vecSizes), 1)), judge.DefaultCandidates)
	t.Log(b.String())
	if vec < lex {
		t.Errorf("the vector set lost candidates: %d → %d", lex, vec)
	}

	// The cosine similarity of each pair, by class, for the floors.
	texts := make([]string, 0, 2*len(pairs))
	for _, p := range pairs {
		texts = append(texts, p.Proposal.Statement, p.Candidate.Statement)
	}
	vecs, err := emb.EmbedContext(ctx, texts, "document")
	if err != nil {
		t.Fatal(err)
	}
	byClass := map[ledger.Relation][]float64{}
	for i, p := range pairs {
		byClass[p.Class] = append(byClass[p.Class], cosine(vecs[2*i], vecs[2*i+1]))
	}
	b.Reset()
	fmt.Fprintf(&b, "cosine similarity of the pairs (%s): min / p10 / median / max\n", mode)
	for _, c := range classes {
		s := byClass[c]
		sort.Float64s(s)
		fmt.Fprintf(&b, "%-12s %.2f / %.2f / %.2f / %.2f\n", c, s[0], s[len(s)/10], s[len(s)/2], s[len(s)-1])
	}
	above := func(floor float64, keep func(ledger.Relation) bool) (int, int) {
		var in, of int
		for _, c := range classes {
			if !keep(c) {
				continue
			}
			for _, x := range byClass[c] {
				of++
				if x >= floor {
					in++
				}
			}
		}
		return in, of
	}
	for _, floor := range []float64{0.5, 0.6, 0.65, 0.7, 0.8, 0.9} {
		rel, relN := above(floor, func(c ledger.Relation) bool { return c != ledger.RelationUnrelated })
		unr, unrN := above(floor, func(c ledger.Relation) bool { return c == ledger.RelationUnrelated })
		dup, dupN := above(floor, func(c ledger.Relation) bool { return c == ledger.RelationDuplicate })
		other, otherN := above(floor, func(c ledger.Relation) bool { return c != ledger.RelationDuplicate })
		fmt.Fprintf(&b, "floor %.2f: related kept %d/%d, unrelated let in %d/%d; duplicates %d/%d, non-duplicates %d/%d\n",
			floor, rel, relN, unr, unrN, dup, dupN, other, otherN)
	}
	t.Log(b.String())
}

func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

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
	"slices"
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
// pair's candidate is kept in one space with all the others (99 distinct
// statements), each proposal is judged there, and we count whether the
// labelled candidate reached the model (or stage 0 folded into it), with
// lexical candidates only and with the vector set added.
//
//	go test ./eval/judge/ -run Candidates -v                            # fake embedder (bag of words): the plumbing
//	V2_EVAL_LIVE=1 VOYAGE_API_KEY=… go test ./eval/judge/ -run Candidates -v   # voyage-4: the real gain, and the floors
//
// The fake embedder shares the lexical lanes' view of words, so its gain
// says only that the vector set flows through; the live run is the
// measurement. It also sweeps JUDGE_VECTOR_FLOOR end to end, says why each
// pair the lexical lanes miss is or isn't won by the vectors, and prints
// each class's cosine similarities: the judge's (voyage-4 to voyage-4) and
// Remember's near-duplicate check (a draft on the query model,
// voyage-4-lite, to kept memories on voyage-4), to calibrate
// JUDGE_VECTOR_FLOOR (0.65) and V2_NEAR_DUPLICATE_FLOOR (0.90). Results are
// in RESULTS.md.

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

const evalModel = "voyage-4"

// liveEmbedders are Voyage's index and query models when the live run is
// asked for; else the bag-of-words fake for both.
func liveEmbedders(t *testing.T) (index, query embed.Embedder, mode string) {
	t.Helper()
	if os.Getenv("V2_EVAL_LIVE") != "1" {
		w := mockembed.NewWords()
		return w, w, "fake (bag of words)"
	}
	cfg := v2index.ConfigFromEnv(os.LookupEnv)
	index, query = cfg.IndexEmbedder(), cfg.QueryEmbedder()
	if index == nil {
		t.Fatal("V2_EVAL_LIVE=1 needs VOYAGE_API_KEY")
	}
	return index, query, "live (" + cfg.IndexModel + ", queries and drafts " + cfg.QueryModel + ")"
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
	emb, queryEmb, mode := liveEmbedders(t)
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
	vectors := v2recall.NewVectors(l, emb, emb, v2recall.VectorConfig{Model: evalModel, Log: quiet})
	// run judges every proposal of a space. Round 0 is the first verdict;
	// a later round judges again (Force), except the proposals stage 0
	// folded in round 0, which are no longer proposals: their fold doesn't
	// depend on the vectors, so it carries over.
	folded := map[uuid.UUID]map[string]bool{} // space → pair id → whether its round-0 fold hit the labelled candidate
	run := func(sp uuid.UUID, floor float64, round int, opts ...judge.Option) (map[string]bool, map[string]int) {
		rec := &recorder{asked: map[string][]string{}}
		j := judge.New(l, rec, judge.Config{Primary: judge.Tier{Model: "recorder"}, VectorFloor: floor, Log: quiet}, opts...)
		found, sizes := map[string]bool{}, map[string]int{}
		if folded[sp] == nil {
			folded[sp] = map[string]bool{}
		}
		for _, p := range pairs {
			if hit, ok := folded[sp][p.ID]; ok && round > 0 {
				found[p.ID] = hit
				continue
			}
			m := proposals[sp][p.ID]
			r, err := j.Run(ctx, ledger.JudgeArgs{MemoryID: m.ID, SpaceID: sp, Version: 1, Mode: ledger.JudgeProposal,
				Round: round, Force: round > 0}, judge.RunOptions{})
			if err != nil {
				t.Fatalf("%s: %v", p.ID, err)
			}
			if r.Skipped {
				t.Fatalf("%s: round %d was skipped", p.ID, round)
			}
			want := targets[sp][p.Candidate.Statement]
			if r.Stage == ledger.StageExact || r.Stage == ledger.StageNear {
				found[p.ID] = r.Target == want
				folded[sp][p.ID] = found[p.ID]
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
	lexFound, _ := run(lexicalSpace, judge.DefaultVectorFloor, 0)
	vecFound, vecSizes := run(vectorSpace, judge.DefaultVectorFloor, 0, judge.WithVectors(vectors))

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
	fmt.Fprintf(&b, "related pairs the lexical lanes miss: %v (some fold at stage 0 into another pair's memory with the same words; see below)\n", missed)
	fmt.Fprintf(&b, "mean candidates per model call with vectors: %.1f (cap %d + keyed decisions)\n",
		meanSize(vecSizes), judge.DefaultCandidates)
	t.Log(b.String())
	if vec < lex {
		t.Errorf("the vector set lost candidates: %d → %d", lex, vec)
	}

	// Why each pair the lexical lanes miss is, or isn't, won by the
	// vectors: the target's rank and similarity among the proposal's 10
	// nearest kept memories (what judge.Vectors returns before the floor).
	b.Reset()
	fmt.Fprintf(&b, "lexical misses, at the vector floor %.2f:\n", judge.DefaultVectorFloor)
	var notNear, belowFloor, cutByFusion, won, foldedElsewhere int
	for _, id := range missed {
		p := pairByID(pairs, id)
		m := proposals[vectorSpace][id]
		near, err := vectors.Similar(ctx, scope, vectorSpace, m.ID, m.Statement, judge.DefaultCandidates)
		if err != nil {
			t.Fatal(err)
		}
		want := targets[vectorSpace][p.Candidate.Statement]
		at := slices.IndexFunc(near, func(c ledger.JudgeCandidate) bool { return c.Ref == want })
		var why string
		hit, wasFolded := folded[vectorSpace][id]
		switch {
		case wasFolded && !hit:
			foldedElsewhere++
			why = "stage 0 folded it into another memory with the same words, so no model call"
		case at < 0:
			notNear++
			why = "not among the 10 nearest"
		case near[at].Score < judge.DefaultVectorFloor:
			belowFloor++
			why = fmt.Sprintf("rank %d, similarity %.2f: below the floor", at+1, near[at].Score)
		case !vecFound[id]:
			cutByFusion++
			why = fmt.Sprintf("rank %d, similarity %.2f: above the floor, cut when fused with the lexical lanes to %d", at+1, near[at].Score, judge.DefaultCandidates)
		default:
			won++
			why = fmt.Sprintf("rank %d, similarity %.2f: won", at+1, near[at].Score)
		}
		fmt.Fprintf(&b, "  %s (%s): %s\n", id, p.Class, why)
	}
	fmt.Fprintf(&b, "  won %d, cut by fusion %d, below the floor %d, not near %d, folded into another memory %d\n", won, cutByFusion,
		belowFloor, notNear, foldedElsewhere)
	t.Log(b.String())

	// JUDGE_VECTOR_FLOOR end to end: candidate recall on related pairs, how
	// often an unrelated pair's candidate still reaches the model, and the
	// candidates per call.
	b.Reset()
	b.WriteString("JUDGE_VECTOR_FLOOR sweep (the vector space):\n")
	for i, floor := range []float64{0.45, 0.5, 0.55, 0.6, 0.65, 0.7, 0.75} {
		found, sizes := run(vectorSpace, floor, i+1, judge.WithVectors(vectors))
		var rel, unrel int
		for _, p := range pairs {
			if !found[p.ID] {
				continue
			}
			if p.Class == ledger.RelationUnrelated {
				unrel++
			} else {
				rel++
			}
		}
		fmt.Fprintf(&b, "  floor %.2f: related candidate recall %.2f (%d/%d), unrelated candidates reaching the model %d/%d, mean candidates %.1f\n",
			floor, float64(rel)/float64(related), rel, related, unrel, len(pairs)-related, meanSize(sizes))
	}
	t.Log(b.String())

	// The cosine similarity of each pair, by class, for the floors: the
	// judge compares stored voyage-4 vectors; Remember compares a draft
	// embedded with the query model to stored voyage-4 vectors.
	texts := make([]string, 0, 2*len(pairs))
	drafts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		texts = append(texts, p.Proposal.Statement, p.Candidate.Statement)
		drafts = append(drafts, p.Proposal.Statement)
	}
	vecs, err := emb.EmbedContext(ctx, texts, "document")
	if err != nil {
		t.Fatal(err)
	}
	draftVecs, err := queryEmb.EmbedContext(ctx, drafts, "document")
	if err != nil {
		t.Fatal(err)
	}
	judgeSim, draftSim := map[ledger.Relation][]float64{}, map[ledger.Relation][]float64{}
	for i, p := range pairs {
		judgeSim[p.Class] = append(judgeSim[p.Class], cosine(vecs[2*i], vecs[2*i+1]))
		draftSim[p.Class] = append(draftSim[p.Class], cosine(draftVecs[i], vecs[2*i+1]))
	}
	t.Log(similarityReport("judge, proposal to candidate (index model both sides)", mode, judgeSim,
		[]float64{0.5, 0.55, 0.6, 0.65, 0.7, 0.8, 0.9}))
	t.Log(similarityReport("Remember, draft (query model) to kept memory (index model)", mode, draftSim,
		[]float64{0.8, 0.85, 0.88, 0.9, 0.92, 0.94, 0.96}))
}

func meanSize(sizes map[string]int) float64 {
	total := 0
	for _, n := range sizes {
		total += n
	}
	return float64(total) / float64(max(len(sizes), 1))
}

func pairByID(pairs []pair, id string) pair {
	for _, p := range pairs {
		if p.ID == id {
			return p
		}
	}
	return pair{}
}

func similarityReport(what, mode string, byClass map[ledger.Relation][]float64, floors []float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "cosine similarity, %s (%s): min / p10 / p25 / median / p75 / max\n", what, mode)
	for _, c := range classes {
		s := slices.Clone(byClass[c])
		sort.Float64s(s)
		q := func(f float64) float64 { return s[min(len(s)-1, int(f*float64(len(s))))] }
		fmt.Fprintf(&b, "%-12s %.2f / %.2f / %.2f / %.2f / %.2f / %.2f\n", c, s[0], q(0.1), q(0.25), q(0.5), q(0.75), s[len(s)-1])
	}
	for _, floor := range floors {
		count := func(keep func(ledger.Relation) bool) (int, int) {
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
		rel, relN := count(func(c ledger.Relation) bool { return c != ledger.RelationUnrelated })
		unr, unrN := count(func(c ledger.Relation) bool { return c == ledger.RelationUnrelated })
		dup, dupN := count(func(c ledger.Relation) bool { return c == ledger.RelationDuplicate })
		upd, updN := count(func(c ledger.Relation) bool { return c == ledger.RelationUpdates })
		other, otherN := count(func(c ledger.Relation) bool {
			return c != ledger.RelationDuplicate && c != ledger.RelationUpdates
		})
		fmt.Fprintf(&b, "floor %.2f: related %d/%d, unrelated %d/%d; duplicates %d/%d, updates %d/%d, extends+contradicts+unrelated %d/%d\n",
			floor, rel, relN, unr, unrN, dup, dupN, upd, updN, other, otherN)
	}
	return b.String()
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

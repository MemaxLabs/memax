package judge

import (
	"cmp"
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/textsig"
)

// Match is a stage-0 hit: the proposal repeats a memory.
type Match struct {
	Candidate ledger.JudgeCandidate
	Stage     ledger.JudgeStage
	// Outcome is folded (a kept memory) or suppressed (a rejection).
	Outcome ledger.VerdictOutcome
}

// Stage0 looks for a repeat with no model: the same words as a kept
// memory, a near-verbatim repeat of one, or a repeat of something
// rejected within the window (the snapshot holds only those). Kept wins
// over rejected, and exact over near.
func Stage0(statement string, exact, near []ledger.JudgeCandidate) (Match, bool) {
	for _, want := range []lifecycle.Lifecycle{lifecycle.Kept, lifecycle.Rejected} {
		for _, c := range exact {
			if c.Lifecycle == want && textsig.ContentSHA256(c.Statement) == textsig.ContentSHA256(statement) {
				return match(c, ledger.StageExact), true
			}
		}
		for _, c := range near {
			if c.Lifecycle == want && textsig.NearDuplicate(statement, c.Statement) {
				return match(c, ledger.StageNear), true
			}
		}
	}
	return Match{}, false
}

func match(c ledger.JudgeCandidate, stage ledger.JudgeStage) Match {
	m := Match{Candidate: c, Stage: stage, Outcome: ledger.OutcomeFolded}
	if c.Lifecycle == lifecycle.Rejected {
		m.Stage, m.Outcome = ledger.StageReproposal, ledger.OutcomeSuppressed
	}
	return m
}

// Vectors finds kept memories close to a statement by embedding (plan 25
// §5.11; v2recall.Vectors over the V2 memory embeddings). Implementations
// return the k nearest kept memories of the space other than the one
// judged, best first, with Score set to the cosine similarity; the judge
// keeps those at or above Config.VectorFloor and fuses them with the
// lexical lanes by RRF. Nil means lexical candidates only.
type Vectors interface {
	Similar(ctx context.Context, scope ledger.Scope, spaceID, memoryID uuid.UUID, statement string, k int) ([]ledger.JudgeCandidate, error)
}

// aboveFloor keeps the vector candidates at or above the floor.
func aboveFloor(cands []ledger.JudgeCandidate, floor float64) []ledger.JudgeCandidate {
	out := cands[:0:0]
	for _, c := range cands {
		if c.Score >= floor {
			out = append(out, c)
		}
	}
	return out
}

// rrfK is the reciprocal-rank-fusion constant (as in V1's retrieval).
const rrfK = 60

// candidate is a memory the proposal is compared with, and why.
type candidate struct {
	ledger.JudgeCandidate
	sets  []string
	score float64
	keyed bool
}

// gather picks the candidates: the top n kept memories by fused lexical
// (and vector) rank, plus the keyed decisions in force.
func gather(statement, area string, snap *ledger.JudgeSnapshot, vectors []ledger.JudgeCandidate, n int) []candidate {
	byID := map[uuid.UUID]*candidate{}
	add := func(c ledger.JudgeCandidate, set string, score float64) {
		cd := byID[c.ID]
		if cd == nil {
			cd = &candidate{JudgeCandidate: c}
			byID[c.ID] = cd
		}
		if !slices.Contains(cd.sets, set) {
			cd.sets = append(cd.sets, set)
		}
		cd.score += score
	}
	for _, lane := range []struct {
		set string
		cs  []ledger.JudgeCandidate
	}{{"lexical", snap.FTS}, {"lexical", snap.Trigram}, {"vector", vectors}} {
		for rank, c := range lane.cs {
			add(c, lane.set, 1/float64(rrfK+rank+1))
		}
	}
	ranked := make([]*candidate, 0, len(byID))
	for _, c := range byID {
		ranked = append(ranked, c)
	}
	slices.SortFunc(ranked, func(a, b *candidate) int {
		return cmp.Or(cmp.Compare(b.score, a.score), cmp.Compare(a.Ref, b.Ref))
	})
	if len(ranked) > n {
		ranked = ranked[:n]
	}
	out := make([]candidate, 0, len(ranked)+4)
	for _, c := range ranked {
		out = append(out, *c)
	}
	// The keyed set: every decision in force with the same area, or whose
	// area the proposal names, or that it overlaps enough to touch.
	touches := ledger.Touches(statement, area, snap.Decisions)
	if len(touches) > n {
		touches = touches[:n]
	}
	for _, t := range touches {
		keyed := t.Why == "area" || t.Why == "mentions"
		i := slices.IndexFunc(out, func(c candidate) bool { return c.ID == t.Decision.ID })
		if i < 0 {
			out = append(out, candidate{JudgeCandidate: t.Decision, sets: []string{"keyed"}, keyed: keyed})
			continue
		}
		out[i].sets = append(out[i].sets, "keyed")
		out[i].keyed = out[i].keyed || keyed
	}
	return out
}

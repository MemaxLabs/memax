package judgeeval

import (
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/textsig"
)

// The Write-level pre-check (ledger.Touches) on the pairs whose candidate
// is a decision in force: how many contradictions and implicit updates it
// holds for the judge, and how many unrelated or compatible writes it
// holds needlessly (each costs a Review card, never a false conflict).
func TestWritePrecheck(t *testing.T) {
	t.Parallel()
	var held, missed, needless, passed int
	var misses []string
	for _, p := range load(t) {
		if !p.Candidate.InForce {
			continue
		}
		d := ledger.JudgeCandidate{Ref: "M-0002", Statement: p.Candidate.Statement, Kind: ledger.KindDecision,
			Lifecycle: "kept", Area: textsig.NormalizeKey(p.Candidate.Area), InForce: true}
		touched := len(ledger.Touches(p.Proposal.Statement, p.Proposal.Area, []ledger.JudgeCandidate{d})) > 0
		switch must := p.Class == ledger.RelationContradicts || p.Class == ledger.RelationUpdates; {
		case must && touched:
			held++
		case must:
			missed++
			misses = append(misses, p.ID)
		case touched:
			needless++
		default:
			passed++
		}
	}
	t.Logf("write pre-check on decisions in force: held %d of %d contradictions and updates (recall %.2f); "+
		"held %d of %d others needlessly; missed %v (the judge still flags them after the fact)",
		held, held+missed, float64(held)/float64(held+missed), needless, needless+passed, misses)
	if float64(held)/float64(held+missed) < 0.8 {
		t.Errorf("the pre-check holds too few contradictions: %d of %d", held, held+missed)
	}
}

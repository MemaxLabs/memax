package receiptchain

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Verifier recomputes a space's chain from genesis and checks every
// checkpoint against it: the range, the chain hash before and after, the
// Merkle root and the signature. It also checks each receipt's reason
// against its commitment, and that a reason is missing only where the
// object was forgotten.
//
// It starts from genesis, not from the last verified checkpoint: starting
// later would only prove that new receipts chain onto a signed state, and
// a receipt changed after last night's run would go unnoticed until a
// full run. Reading every receipt is what nightly verification costs.
//
// Feed it with Add, in chain order ((txid, seq), as sealed), then Finish.
type Verifier struct {
	space       uuid.UUID
	keys        Keyring
	checkpoints []Checkpoint
	next        int // the checkpoint being filled
	position    int64
	head        Hash
	leaves      []Hash
	forgotten   map[uuid.UUID]bool
	redacted    map[uuid.UUID][]uuid.UUID // object → receipts whose reason is gone
	report      Report
}

// Report is what a verification found.
type Report struct {
	SpaceID uuid.UUID
	// Receipts walked, and the chain hash after them.
	Receipts int64
	Head     Hash
	// Checkpoints checked; Signed of them verified, Unsigned had none.
	Checkpoints, Signed, Unsigned int
	// Problems is everything that doesn't match. Empty means verified.
	Problems []Problem
}

// OK reports whether nothing was wrong.
func (r Report) OK() bool { return len(r.Problems) == 0 }

// ProblemKind names what didn't match.
type ProblemKind string

// The problem kinds.
const (
	// A checkpoint's range doesn't continue the previous one, or its
	// first or last receipt (or last seq) isn't the one at that position.
	ProblemRange ProblemKind = "range"
	// The chain hash before or after a checkpoint's range differs:
	// receipts were changed, inserted, removed or reordered.
	ProblemChain ProblemKind = "chain"
	// The range's Merkle root differs.
	ProblemMerkle ProblemKind = "merkle"
	// The signature doesn't verify, or names a key the keyring lacks.
	ProblemSignature ProblemKind = "signature"
	// A reason doesn't match its commitment.
	ProblemReason ProblemKind = "reason"
	// A reason is gone, but nothing forgot its object.
	ProblemRedaction ProblemKind = "redaction"
	// A checkpoint covers receipts that aren't there.
	ProblemMissing ProblemKind = "missing"
)

// Problem is one mismatch.
type Problem struct {
	Kind ProblemKind
	// Checkpoint is the checkpoint's number, 0 when the problem is a
	// receipt's own.
	Checkpoint int64
	// Receipt is the receipt involved, if one is.
	Receipt uuid.UUID
	Detail  string
}

func (p Problem) String() string {
	s := string(p.Kind)
	if p.Checkpoint > 0 {
		s += fmt.Sprintf(" at checkpoint %d", p.Checkpoint)
	}
	if p.Receipt != uuid.Nil {
		s += " (receipt " + p.Receipt.String() + ")"
	}
	return s + ": " + p.Detail
}

// NewVerifier starts verifying a space against its checkpoints, in number
// order, with the keys they may be signed with.
func NewVerifier(space uuid.UUID, keys Keyring, checkpoints []Checkpoint) *Verifier {
	v := &Verifier{space: space, keys: keys, checkpoints: checkpoints, head: Genesis(space),
		forgotten: map[uuid.UUID]bool{}, redacted: map[uuid.UUID][]uuid.UUID{}}
	v.report.SpaceID = space
	v.report.Checkpoints = len(checkpoints)
	for i, c := range checkpoints {
		want := int64(1)
		if i > 0 {
			want = checkpoints[i-1].PositionTo + 1
		}
		if c.Number != int64(i+1) || c.PositionFrom != want || c.PositionTo < c.PositionFrom || c.SpaceID != space {
			v.problem(ProblemRange, c.Number, uuid.Nil, fmt.Sprintf(
				"checkpoint %d covers %d–%d; the one before it ends at %d", c.Number, c.PositionFrom, c.PositionTo, want-1))
		}
		switch err := keys.Verify(c); {
		case err == nil:
			v.report.Signed++
		case errors.Is(err, ErrUnsigned):
			v.report.Unsigned++
		default:
			v.problem(ProblemSignature, c.Number, uuid.Nil, err.Error())
		}
	}
	return v
}

// Forgotten tells the verifier an object was forgotten (it has a forgot
// receipt, sealed yet or not), so its receipts' reasons may be gone.
func (v *Verifier) Forgotten(object uuid.UUID) { v.forgotten[object] = true }

// Add walks one receipt, the next in chain order.
func (v *Verifier) Add(r Receipt) {
	v.position++
	leaf := Leaf(r)
	before := v.head
	v.head = Next(v.head, leaf)
	v.report.Receipts = v.position
	if r.Action == "forgot" {
		v.forgotten[r.ObjectID] = true
	}
	switch {
	case r.Reason != nil && (r.ReasonSalt == nil || r.ReasonSHA256 == nil):
		v.problem(ProblemReason, 0, r.ID, "it has a reason but no commitment")
	case r.Reason != nil && !bytes.Equal(r.ReasonSHA256, hashBytes(ReasonCommitment(r.ReasonSalt, *r.Reason))):
		v.problem(ProblemReason, 0, r.ID, "its reason doesn't match the commitment sealed with it")
	case r.Reason == nil && r.ReasonSHA256 != nil:
		v.redacted[r.ObjectID] = append(v.redacted[r.ObjectID], r.ID)
	}

	if v.next >= len(v.checkpoints) {
		return // after the last checkpoint: not sealed yet
	}
	c := v.checkpoints[v.next]
	if v.position < c.PositionFrom {
		return
	}
	if v.position == c.PositionFrom {
		v.leaves = v.leaves[:0]
		if r.ID != c.FirstReceiptID {
			v.problem(ProblemRange, c.Number, r.ID, "the receipt at its first position isn't the one it sealed")
		}
		if before != c.Prev {
			v.problem(ProblemChain, c.Number, uuid.Nil, "the chain hash before its range differs from the signed one")
		}
	}
	v.leaves = append(v.leaves, leaf)
	if v.position == c.PositionTo {
		v.closeCheckpoint(c, r)
		v.next++
	}
}

func (v *Verifier) closeCheckpoint(c Checkpoint, last Receipt) {
	if last.ID != c.LastReceiptID || last.Seq != c.LastSeq {
		v.problem(ProblemRange, c.Number, last.ID, "the receipt at its last position isn't the one it sealed")
	}
	if got := MerkleRoot(v.leaves); got != c.MerkleRoot {
		v.problem(ProblemMerkle, c.Number, uuid.Nil, "the Merkle root of its receipts differs from the signed one")
	}
	if v.head != c.Chain {
		v.problem(ProblemChain, c.Number, uuid.Nil, "the chain hash at its end differs from the signed one")
		// Carry on from the signed hash, so each later checkpoint is judged
		// on its own receipts and the report points at the range that
		// changed rather than at everything after it.
		v.head = c.Chain
	}
}

// Finish ends the walk and returns the report.
func (v *Verifier) Finish() Report {
	for ; v.next < len(v.checkpoints); v.next++ {
		c := v.checkpoints[v.next]
		v.problem(ProblemMissing, c.Number, uuid.Nil, fmt.Sprintf(
			"it covers positions %d–%d, but the chain has %d receipts", c.PositionFrom, c.PositionTo, v.position))
	}
	for object, receipts := range v.redacted {
		if v.forgotten[object] {
			continue
		}
		for _, id := range receipts {
			v.problem(ProblemRedaction, 0, id, "its reason is gone, but nothing forgot "+object.String())
		}
	}
	v.report.Head = v.head
	return v.report
}

func (v *Verifier) problem(kind ProblemKind, checkpoint int64, receipt uuid.UUID, detail string) {
	v.report.Problems = append(v.report.Problems, Problem{Kind: kind, Checkpoint: checkpoint, Receipt: receipt, Detail: detail})
}

func hashBytes(h Hash) []byte { return h[:] }

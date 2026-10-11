// Package stale decides what a check run marks stale (the stale-detection
// design §1.4, §5): a pure function over the run's results, as the ledger
// stored them, and the memories they're about. The ledger reads, locks and
// writes; this says which memories to flag, and why. Nothing here does I/O.
//
// A memory is flagged when all of these hold:
//
//  1. It is kept and not already stale.
//  2. One of its conditions is armed (it held once on the default branch)
//     and fails now, in a failure that began at a commit later than its
//     last hold. Unknown is never a failure.
//  3. Nobody kept, edited or verified it since Memax first saw that
//     failure: whoever did knew the repository as it is now.
//  4. The run doesn't trip the bulk cap: a run that would flag more than
//     CapMemories memories, or more than CapShare of the space's armed
//     ones (and at least CapFloor), flags none of them one by one.
//
// A rename of a file only a source cites isn't flagged: the condition will
// follow the move (the words don't name the path). Only an authoritative
// observer's results flag (§2.4: the App, where it covers the repository;
// a person's machine otherwise).
package stale

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/conditions"
)

// The bulk cap (§5.6). The eval tunes these.
const (
	CapMemories = 10
	CapShare    = 0.2
	// CapFloor keeps the share from capping a small space: a space with
	// three armed memories can still flag one.
	CapFloor = 5
)

// Outcome is a condition's latest result on the default branch.
type Outcome string

// The outcomes.
const (
	Holds   Outcome = "holds"
	Fails   Outcome = "fails"
	Unknown Outcome = "unknown"
)

// Renamed is the change a rename's evidence names.
const Renamed = "renamed"

// Check is one condition's state after the run.
type Check struct {
	Condition string
	Origin    conditions.Origin
	From      conditions.From
	Outcome   Outcome
	// Armed: it held once on the default branch.
	Armed bool
	// HeldCommitAt is the commit time of its latest hold.
	HeldCommitAt *time.Time
	// FailedCommitAt is the commit time the current failure began at, and
	// FailedAt when Memax first saw it.
	FailedCommitAt *time.Time
	FailedAt       *time.Time
	// Change is the failure's evidence ("removed", "renamed", …).
	Change string
}

// Memory is a memory the run reported on, with the checks the run changed
// or confirmed.
type Memory struct {
	ID    uuid.UUID
	Kept  bool
	Stale bool
	// ActedAt is the latest keep, edit or verification by a person.
	ActedAt *time.Time
	Checks  []Check
}

// Input is a run's memories and the space's armed count.
type Input struct {
	Memories []Memory
	// Armed is how many memories in the space have an armed condition.
	Armed int
	// Authoritative: this observer's results may flag.
	Authoritative bool
}

// Flag is a memory to flag, with the conditions that failed.
type Flag struct {
	Memory     uuid.UUID
	Conditions []string
}

// Output is what to flag. Capped: the run tripped the bulk cap, so nothing
// is flagged one by one.
type Output struct {
	Flags  []Flag
	Capped bool
}

// Decide says what the run flags.
func Decide(in Input) Output {
	if !in.Authoritative {
		return Output{}
	}
	var out Output
	for _, m := range in.Memories {
		if !m.Kept || m.Stale {
			continue
		}
		var failed []string
		for _, c := range m.Checks {
			if fails(c, m.ActedAt) {
				failed = append(failed, c.Condition)
			}
		}
		if len(failed) > 0 {
			slices.Sort(failed)
			out.Flags = append(out.Flags, Flag{Memory: m.ID, Conditions: failed})
		}
	}
	if n := len(out.Flags); n > CapMemories || (n >= CapFloor && float64(n) > CapShare*float64(in.Armed)) {
		return Output{Capped: true}
	}
	return out
}

// fails reports whether one check flags its memory.
func fails(c Check, actedAt *time.Time) bool {
	switch {
	case c.Outcome != Fails || !c.Armed || c.FailedCommitAt == nil:
		return false
	case c.HeldCommitAt != nil && !c.FailedCommitAt.After(*c.HeldCommitAt):
		return false
	case actedAt != nil && c.FailedAt != nil && !actedAt.Before(*c.FailedAt):
		return false
	case c.Change == Renamed && c.Origin == conditions.OriginDerived && c.From == conditions.FromSource:
		return false
	}
	return true
}

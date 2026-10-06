// Package lifecycle is the V2 memory state machine (plan 25 §5.5):
// one lifecycle plus two flags, the transitions between them, and the
// single state people see.
//
// A memory's lifecycle is one of proposed, kept, merged, faded,
// forgotten or rejected. Stale and conflict are flags, because they are
// conditions that can hold while a memory is kept or proposed ("as its
// source state, plus a note"). The displayed state is derived from both,
// with the precedence forgotten > conflict > stale > proposed > merged >
// faded > kept. `rejected` is internal: it carries no mark in the UI,
// but it keeps reject-rate metrics and lets the judge suppress
// re-proposals.
//
// The transition table here is mirrored in SQL by
// v2.lifecycle_transition_allowed and v2.display_state (migration 028);
// a test in internal/ledger checks that the two agree.
//
// This package is pure: no I/O, no clock.
package lifecycle

import (
	"fmt"
	"slices"
)

// Lifecycle is where a memory is in its life.
type Lifecycle string

// The lifecycles. None is the state before a memory exists; it is never
// stored.
const (
	None      Lifecycle = ""
	Proposed  Lifecycle = "proposed"
	Kept      Lifecycle = "kept"
	Merged    Lifecycle = "merged"
	Faded     Lifecycle = "faded"
	Forgotten Lifecycle = "forgotten"
	Rejected  Lifecycle = "rejected"
)

// Lifecycles lists every stored lifecycle.
var Lifecycles = []Lifecycle{Proposed, Kept, Merged, Faded, Forgotten, Rejected}

// Valid reports whether l is a stored lifecycle (None is not).
func (l Lifecycle) Valid() bool { return slices.Contains(Lifecycles, l) }

// Flag is a condition on a kept or proposed memory.
type Flag string

// The flags.
const (
	Conflict Flag = "conflict"
	Stale    Flag = "stale"
)

// AllFlags lists every flag, in canonical (sorted) order.
var AllFlags = []Flag{Conflict, Stale}

// Flags is a set of flags. Values built with NewFlags, With and Without
// are sorted and free of duplicates, which is the form the database
// stores.
type Flags []Flag

// NewFlags returns the normalized set of fs.
func NewFlags(fs ...Flag) Flags {
	out := Flags{}
	for _, f := range AllFlags {
		if slices.Contains(fs, f) {
			out = append(out, f)
		}
	}
	return out
}

// ParseFlags converts stored flag strings, rejecting unknown values.
func ParseFlags(ss []string) (Flags, error) {
	fs := make([]Flag, 0, len(ss))
	for _, s := range ss {
		f := Flag(s)
		if !slices.Contains(AllFlags, f) {
			return nil, fmt.Errorf("lifecycle: unknown flag %q", s)
		}
		fs = append(fs, f)
	}
	return NewFlags(fs...), nil
}

// Has reports whether f is set.
func (fs Flags) Has(f Flag) bool { return slices.Contains(fs, f) }

// With returns the set plus f.
func (fs Flags) With(f Flag) Flags { return NewFlags(append(slices.Clone(fs), f)...) }

// Without returns the set minus f.
func (fs Flags) Without(f Flag) Flags {
	return NewFlags(slices.DeleteFunc(slices.Clone(fs), func(x Flag) bool { return x == f })...)
}

// Strings returns the flags as stored (never nil).
func (fs Flags) Strings() []string {
	out := make([]string, 0, len(fs))
	for _, f := range NewFlags(fs...) {
		out = append(out, string(f))
	}
	return out
}

// State is a memory's lifecycle and flags together.
type State struct {
	Lifecycle Lifecycle
	Flags     Flags
}

// Mark is the one state people see for a memory: the StateMark.
type Mark string

// The marks. MarkRejected is returned for completeness; the UI shows no
// mark for it.
const (
	MarkProposed  Mark = "proposed"
	MarkKept      Mark = "kept"
	MarkMerged    Mark = "merged"
	MarkStale     Mark = "stale"
	MarkConflict  Mark = "conflict"
	MarkFaded     Mark = "faded"
	MarkForgotten Mark = "forgotten"
	MarkRejected  Mark = "rejected"
)

// Marks lists every mark.
var Marks = []Mark{MarkProposed, MarkKept, MarkMerged, MarkStale, MarkConflict, MarkFaded, MarkForgotten, MarkRejected}

// Valid reports whether m is a known mark.
func (m Mark) Valid() bool { return slices.Contains(Marks, m) }

// DisplayState derives the displayed state from a lifecycle and its
// flags: forgotten > conflict > stale > proposed > merged > faded > kept.
// A rejected memory shows as rejected whatever its flags.
func DisplayState(l Lifecycle, fs Flags) Mark {
	switch {
	case l == Forgotten:
		return MarkForgotten
	case l == Rejected:
		return MarkRejected
	case fs.Has(Conflict):
		return MarkConflict
	case fs.Has(Stale):
		return MarkStale
	}
	return Mark(l)
}

// Mark returns the displayed state of s.
func (s State) Mark() Mark { return DisplayState(s.Lifecycle, s.Flags) }

// Verb is one change to a memory's state. Ledger commands map onto
// verbs after policy has decided the outcome: Remember becomes keep from
// None, a downgraded Remember becomes propose, and so on.
type Verb string

// The verbs (plan 25 §5.5).
const (
	VerbPropose       Verb = "propose"        // None → proposed
	VerbKeep          Verb = "keep"           // None | proposed → kept; refused while in conflict
	VerbEdit          Verb = "edit"           // proposed → proposed, kept → kept; clears stale
	VerbReject        Verb = "reject"         // proposed → rejected
	VerbMerge         Verb = "merge"          // proposed → merged
	VerbUnmerge       Verb = "unmerge"        // merged → proposed (undo of a Dream merge)
	VerbFade          Verb = "fade"           // kept → faded
	VerbRestore       Verb = "restore"        // faded → kept
	VerbForget        Verb = "forget"         // anything but forgotten → forgotten (terminal)
	VerbFlagConflict  Verb = "flag_conflict"  // proposed | kept: + conflict
	VerbClearConflict Verb = "clear_conflict" // proposed | kept in conflict: − conflict
	VerbFlagStale     Verb = "flag_stale"     // kept: + stale
	VerbClearStale    Verb = "clear_stale"    // kept and stale: − stale (Verify: still true)
)

// Verbs lists every verb.
var Verbs = []Verb{
	VerbPropose, VerbKeep, VerbEdit, VerbReject, VerbMerge, VerbUnmerge, VerbFade,
	VerbRestore, VerbForget, VerbFlagConflict, VerbClearConflict, VerbFlagStale, VerbClearStale,
}

// TransitionError explains why a verb can't apply to a state. Message
// is written for people and says what to do instead.
type TransitionError struct {
	From    State
	Verb    Verb
	Message string
}

func (e *TransitionError) Error() string { return e.Message }

// Transition applies v to from and returns the new state, or a
// *TransitionError when the state machine doesn't allow it.
func Transition(from State, v Verb) (State, error) {
	l, fs := from.Lifecycle, NewFlags(from.Flags...)
	refuse := func(msg string) (State, error) {
		return State{}, &TransitionError{From: State{Lifecycle: l, Flags: fs}, Verb: v, Message: msg}
	}
	if l != None && !l.Valid() {
		return refuse(fmt.Sprintf("unknown lifecycle %q", l))
	}
	if l == Forgotten {
		return refuse("this memory is forgotten; nothing can change it")
	}

	switch v {
	case VerbPropose:
		if l == None {
			return State{Lifecycle: Proposed, Flags: Flags{}}, nil
		}
		return refuse("this memory already exists; propose a new one instead")
	case VerbKeep:
		switch l {
		case None:
			return State{Lifecycle: Kept, Flags: Flags{}}, nil
		case Proposed:
			if fs.Has(Conflict) {
				return refuse("this proposal conflicts with the record; resolve the conflict before keeping it")
			}
			return State{Lifecycle: Kept, Flags: fs}, nil
		}
		return refuse(fmt.Sprintf("only proposals can be kept; this memory is %s", l))
	case VerbEdit:
		switch l {
		case Proposed:
			return State{Lifecycle: Proposed, Flags: fs}, nil
		case Kept:
			return State{Lifecycle: Kept, Flags: fs.Without(Stale)}, nil
		}
		return refuse(fmt.Sprintf("only kept memories and proposals can be edited; this memory is %s", l))
	case VerbReject:
		if l == Proposed {
			return State{Lifecycle: Rejected, Flags: Flags{}}, nil
		}
		return refuse(fmt.Sprintf("only proposals can be rejected; this memory is %s", l))
	case VerbMerge:
		if l == Proposed {
			return State{Lifecycle: Merged, Flags: Flags{}}, nil
		}
		return refuse(fmt.Sprintf("only proposals can be merged; this memory is %s", l))
	case VerbUnmerge:
		if l == Merged {
			return State{Lifecycle: Proposed, Flags: Flags{}}, nil
		}
		return refuse(fmt.Sprintf("only merged memories can be unmerged; this memory is %s", l))
	case VerbFade:
		if l == Kept {
			return State{Lifecycle: Faded, Flags: Flags{}}, nil
		}
		return refuse(fmt.Sprintf("only kept memories can fade; this memory is %s", l))
	case VerbRestore:
		if l == Faded {
			return State{Lifecycle: Kept, Flags: Flags{}}, nil
		}
		return refuse(fmt.Sprintf("only faded memories can be restored; this memory is %s", l))
	case VerbForget:
		if l == None {
			return refuse("there is nothing to forget")
		}
		return State{Lifecycle: Forgotten, Flags: Flags{}}, nil
	case VerbFlagConflict:
		if l != Proposed && l != Kept {
			return refuse(fmt.Sprintf("only kept memories and proposals can be in conflict; this memory is %s", l))
		}
		if fs.Has(Conflict) {
			return refuse("this memory is already flagged as a conflict")
		}
		return State{Lifecycle: l, Flags: fs.With(Conflict)}, nil
	case VerbClearConflict:
		if (l != Proposed && l != Kept) || !fs.Has(Conflict) {
			return refuse("this memory has no conflict to resolve")
		}
		return State{Lifecycle: l, Flags: fs.Without(Conflict)}, nil
	case VerbFlagStale:
		if l != Kept {
			return refuse(fmt.Sprintf("only kept memories can go stale; this memory is %s", l))
		}
		if fs.Has(Stale) {
			return refuse("this memory is already stale")
		}
		return State{Lifecycle: l, Flags: fs.With(Stale)}, nil
	case VerbClearStale:
		if l != Kept || !fs.Has(Stale) {
			return refuse("this memory isn't stale")
		}
		return State{Lifecycle: l, Flags: fs.Without(Stale)}, nil
	}
	return refuse(fmt.Sprintf("unknown change %q", v))
}

// Allowed reports whether any verb moves a memory from lifecycle `from`
// to lifecycle `to` (from None means creating it). It is the Go side of
// v2.lifecycle_transition_allowed, which guards the same table in SQL.
func Allowed(from, to Lifecycle) bool {
	flagSets := []Flags{{}, {Conflict}, {Stale}, {Conflict, Stale}}
	for _, v := range Verbs {
		for _, fs := range flagSets {
			next, err := Transition(State{Lifecycle: from, Flags: fs}, v)
			if err == nil && next.Lifecycle == to {
				return true
			}
		}
	}
	return false
}

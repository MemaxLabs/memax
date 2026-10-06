package lifecycle

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

// states lists every state a stored memory can be in (the SQL CHECKs
// allow flags only on proposed and kept, and stale only on kept), plus
// None for "doesn't exist yet".
var states = []State{
	{None, Flags{}},
	{Proposed, Flags{}},
	{Proposed, Flags{Conflict}},
	{Kept, Flags{}},
	{Kept, Flags{Conflict}},
	{Kept, Flags{Stale}},
	{Kept, Flags{Conflict, Stale}},
	{Merged, Flags{}},
	{Faded, Flags{}},
	{Forgotten, Flags{}},
	{Rejected, Flags{}},
}

func st(l Lifecycle, fs ...Flag) State { return State{Lifecycle: l, Flags: NewFlags(fs...)} }

func key(s State, v Verb) string { return fmt.Sprintf("%s%v/%s", s.Lifecycle, s.Flags.Strings(), v) }

// allowed is the whole transition table of plan 25 §5.5, written out.
// Every (state, verb) pair not listed here must be refused.
var allowed = map[string]State{
	key(st(None), VerbPropose): st(Proposed),
	key(st(None), VerbKeep):    st(Kept),

	key(st(Proposed), VerbKeep):         st(Kept),
	key(st(Proposed), VerbEdit):         st(Proposed),
	key(st(Proposed), VerbReject):       st(Rejected),
	key(st(Proposed), VerbMerge):        st(Merged),
	key(st(Proposed), VerbForget):       st(Forgotten),
	key(st(Proposed), VerbFlagConflict): st(Proposed, Conflict),

	// A proposal in conflict can't be kept until one answer wins.
	key(st(Proposed, Conflict), VerbEdit):          st(Proposed, Conflict),
	key(st(Proposed, Conflict), VerbReject):        st(Rejected),
	key(st(Proposed, Conflict), VerbMerge):         st(Merged),
	key(st(Proposed, Conflict), VerbForget):        st(Forgotten),
	key(st(Proposed, Conflict), VerbClearConflict): st(Proposed),

	key(st(Kept), VerbEdit):         st(Kept),
	key(st(Kept), VerbFade):         st(Faded),
	key(st(Kept), VerbForget):       st(Forgotten),
	key(st(Kept), VerbFlagConflict): st(Kept, Conflict),
	key(st(Kept), VerbFlagStale):    st(Kept, Stale),

	key(st(Kept, Conflict), VerbEdit):          st(Kept, Conflict),
	key(st(Kept, Conflict), VerbFade):          st(Faded),
	key(st(Kept, Conflict), VerbForget):        st(Forgotten),
	key(st(Kept, Conflict), VerbClearConflict): st(Kept),
	key(st(Kept, Conflict), VerbFlagStale):     st(Kept, Conflict, Stale),

	// Editing a stale memory is Verify's "update": it is fresh again.
	key(st(Kept, Stale), VerbEdit):         st(Kept),
	key(st(Kept, Stale), VerbFade):         st(Faded),
	key(st(Kept, Stale), VerbForget):       st(Forgotten),
	key(st(Kept, Stale), VerbFlagConflict): st(Kept, Conflict, Stale),
	key(st(Kept, Stale), VerbClearStale):   st(Kept),

	key(st(Kept, Conflict, Stale), VerbEdit):          st(Kept, Conflict),
	key(st(Kept, Conflict, Stale), VerbFade):          st(Faded),
	key(st(Kept, Conflict, Stale), VerbForget):        st(Forgotten),
	key(st(Kept, Conflict, Stale), VerbClearConflict): st(Kept, Stale),
	key(st(Kept, Conflict, Stale), VerbClearStale):    st(Kept, Conflict),

	key(st(Merged), VerbUnmerge): st(Proposed),
	key(st(Merged), VerbForget):  st(Forgotten),

	key(st(Faded), VerbRestore): st(Kept),
	key(st(Faded), VerbForget):  st(Forgotten),

	key(st(Rejected), VerbForget): st(Forgotten),
	// Forgotten is terminal: nothing listed.
}

func TestTransitionExhaustive(t *testing.T) {
	t.Parallel()
	seen := 0
	for _, from := range states {
		for _, v := range Verbs {
			k := key(from, v)
			want, ok := allowed[k]
			got, err := Transition(from, v)
			if !ok {
				var te *TransitionError
				if err == nil {
					t.Errorf("%s: allowed (→ %s%v), want refused", k, got.Lifecycle, got.Flags.Strings())
				} else if !errors.As(err, &te) || te.Message == "" {
					t.Errorf("%s: error %v is not a *TransitionError with a message", k, err)
				}
				continue
			}
			seen++
			if err != nil {
				t.Errorf("%s: refused (%v), want → %s%v", k, err, want.Lifecycle, want.Flags.Strings())
				continue
			}
			if got.Lifecycle != want.Lifecycle || !slices.Equal(got.Flags.Strings(), want.Flags.Strings()) {
				t.Errorf("%s: → %s%v, want %s%v", k, got.Lifecycle, got.Flags.Strings(), want.Lifecycle, want.Flags.Strings())
			}
		}
	}
	if seen != len(allowed) {
		t.Errorf("table has %d entries but only %d matched a reachable state; fix the table", len(allowed), seen)
	}
}

func TestTransitionResultIsAlwaysStorable(t *testing.T) {
	t.Parallel()
	// Mirrors the SQL CHECKs: flags only on proposed/kept, stale only on kept.
	for _, from := range states {
		for _, v := range Verbs {
			got, err := Transition(from, v)
			if err != nil {
				continue
			}
			if len(got.Flags) > 0 && got.Lifecycle != Proposed && got.Lifecycle != Kept {
				t.Errorf("%s → %s carries flags %v", key(from, v), got.Lifecycle, got.Flags)
			}
			if got.Flags.Has(Stale) && got.Lifecycle != Kept {
				t.Errorf("%s → %s is stale but not kept", key(from, v), got.Lifecycle)
			}
		}
	}
}

func TestTransitionRejectsUnknownInput(t *testing.T) {
	t.Parallel()
	if _, err := Transition(State{Lifecycle: "archived"}, VerbKeep); err == nil {
		t.Error("unknown lifecycle accepted")
	}
	if _, err := Transition(State{Lifecycle: Kept}, Verb("delete")); err == nil {
		t.Error("unknown verb accepted")
	}
}

func TestDisplayStatePrecedence(t *testing.T) {
	t.Parallel()
	flagSets := []Flags{{}, {Conflict}, {Stale}, {Conflict, Stale}}
	// forgotten > conflict > stale > proposed > merged > faded > kept;
	// rejected has no mark and ignores flags.
	want := map[Lifecycle][4]Mark{
		Proposed:  {MarkProposed, MarkConflict, MarkStale, MarkConflict},
		Kept:      {MarkKept, MarkConflict, MarkStale, MarkConflict},
		Merged:    {MarkMerged, MarkConflict, MarkStale, MarkConflict},
		Faded:     {MarkFaded, MarkConflict, MarkStale, MarkConflict},
		Forgotten: {MarkForgotten, MarkForgotten, MarkForgotten, MarkForgotten},
		Rejected:  {MarkRejected, MarkRejected, MarkRejected, MarkRejected},
	}
	for _, l := range Lifecycles {
		for i, fs := range flagSets {
			if got := DisplayState(l, fs); got != want[l][i] {
				t.Errorf("DisplayState(%s, %v) = %s, want %s", l, fs, got, want[l][i])
			}
		}
	}
	if len(want) != len(Lifecycles) {
		t.Fatalf("precedence table covers %d lifecycles, want %d", len(want), len(Lifecycles))
	}
}

func TestAllowedMatchesTransitions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from, to Lifecycle
		want     bool
	}{
		{None, Proposed, true}, {None, Kept, true}, {None, Rejected, false}, {None, Forgotten, false},
		{Proposed, Kept, true}, {Proposed, Proposed, true}, {Proposed, Rejected, true}, {Proposed, Merged, true},
		{Proposed, Faded, false}, {Kept, Proposed, false}, {Kept, Faded, true}, {Kept, Merged, false},
		{Faded, Kept, true}, {Merged, Proposed, true}, {Merged, Merged, false}, {Rejected, Proposed, false},
		{Rejected, Forgotten, true}, {Forgotten, Forgotten, false}, {Forgotten, Kept, false},
	}
	for _, c := range cases {
		if got := Allowed(c.from, c.to); got != c.want {
			t.Errorf("Allowed(%q, %q) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestUndoTransitions(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		from, to Lifecycle
		want     bool
	}{
		{Kept, Proposed, true}, {Rejected, Proposed, true},
		{Merged, Proposed, false}, {Kept, Rejected, false}, {Forgotten, Proposed, false}, {Proposed, Kept, false},
	} {
		if got := UndoAllowed(c.from, c.to); got != c.want {
			t.Errorf("UndoAllowed(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
	ok := []struct{ from, to State }{
		{st(Kept), st(Proposed)},
		{st(Rejected), st(Proposed, Conflict)},
		{st(Merged), st(Proposed)},
		{st(Faded), st(Kept, Conflict)},
		{st(Kept), st(Kept, Stale)},
		{st(Proposed), st(Proposed, Conflict)},
	}
	for _, c := range ok {
		if err := CanRestore(c.from, c.to); err != nil {
			t.Errorf("CanRestore(%v → %v): %v", c.from, c.to, err)
		}
	}
	bad := []struct{ from, to State }{
		{st(Forgotten), st(Kept)},
		{st(Kept), st(Merged)},
		{st(Proposed), st(Proposed, Stale)},
		{st(Faded), st(Faded, Conflict)},
		{st(Kept), State{Lifecycle: "archived"}},
	}
	for _, c := range bad {
		var te *TransitionError
		if err := CanRestore(c.from, c.to); !errors.As(err, &te) {
			t.Errorf("CanRestore(%v → %v) = %v, want a TransitionError", c.from, c.to, err)
		}
	}
}

func TestFlagsNormalize(t *testing.T) {
	t.Parallel()
	got := NewFlags(Stale, Conflict, Stale)
	if !slices.Equal(got.Strings(), []string{"conflict", "stale"}) {
		t.Errorf("NewFlags = %v, want [conflict stale]", got)
	}
	if s := (Flags(nil)).Strings(); s == nil || len(s) != 0 {
		t.Errorf("nil Flags.Strings() = %#v, want empty non-nil", s)
	}
	if got := got.Without(Conflict); !slices.Equal(got.Strings(), []string{"stale"}) {
		t.Errorf("Without = %v", got)
	}
	if _, err := ParseFlags([]string{"stale", "archived"}); err == nil {
		t.Error("ParseFlags accepted an unknown flag")
	}
	parsed, err := ParseFlags([]string{"stale", "conflict"})
	if err != nil || !slices.Equal(parsed.Strings(), []string{"conflict", "stale"}) {
		t.Errorf("ParseFlags = %v, %v", parsed, err)
	}
}

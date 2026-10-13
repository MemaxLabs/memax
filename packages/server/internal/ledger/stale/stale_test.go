package stale

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/conditions"
)

var (
	t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	m1 = uuid.MustParse("00000000-0000-7000-8000-000000000001")
)

func at(h int) *time.Time { t := t0.Add(time.Duration(h) * time.Hour); return &t }

// failing is c1 armed at hour 0, failing since a commit at hour 2, first
// seen at hour 3.
func failing(edit func(*Check)) Check {
	c := Check{Condition: "c1", Origin: conditions.OriginDerived, From: conditions.FromStatement, Outcome: Fails,
		Armed: true, HeldCommitAt: at(0), FailedCommitAt: at(2), FailedAt: at(3), Change: "removed"}
	if edit != nil {
		edit(&c)
	}
	return c
}

func kept(checks ...Check) Memory {
	return Memory{ID: m1, Kept: true, ActedAt: at(-24), Checks: checks}
}

func TestDecide(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   Input
		want []string // the flagged memory's conditions; nil: nothing
	}{
		{"an armed condition that stopped holding", Input{Memories: []Memory{kept(failing(nil))}, Armed: 40}, []string{"c1"}},
		{"unknown is never a failure", Input{Memories: []Memory{kept(failing(func(c *Check) { c.Outcome = Unknown }))}, Armed: 40}, nil},
		{"it holds", Input{Memories: []Memory{kept(failing(func(c *Check) { c.Outcome = Holds }))}, Armed: 40}, nil},
		{"never held, so never armed", Input{Memories: []Memory{kept(failing(func(c *Check) { c.Armed, c.HeldCommitAt = false, nil }))}, Armed: 40}, nil},
		{"the failure is older than its last hold", Input{Memories: []Memory{kept(failing(func(c *Check) { c.HeldCommitAt = at(2) }))}, Armed: 40}, nil},
		{"a person verified it after Memax saw the failure", Input{Memories: []Memory{
			func() Memory { m := kept(failing(nil)); m.ActedAt = at(4); return m }()}, Armed: 40}, nil},
		{"a person kept it before Memax saw the failure, whatever the commit's time", Input{Memories: []Memory{
			func() Memory { m := kept(failing(nil)); m.ActedAt = at(2); return m }()}, Armed: 40}, []string{"c1"}},
		{"a proposal", Input{Memories: []Memory{func() Memory { m := kept(failing(nil)); m.Kept = false; return m }()}, Armed: 40}, nil},
		{"already stale", Input{Memories: []Memory{func() Memory { m := kept(failing(nil)); m.Stale = true; return m }()}, Armed: 40}, nil},
		{"a rename of a file only a source cites follows the move", Input{Memories: []Memory{kept(failing(func(c *Check) {
			c.Change, c.From = Renamed, conditions.FromSource
		}))}, Armed: 40}, nil},
		{"a rename of a path the words name", Input{Memories: []Memory{kept(failing(func(c *Check) { c.Change = Renamed }))}, Armed: 40}, []string{"c1"}},
		{"a rename of a person's condition", Input{Memories: []Memory{kept(failing(func(c *Check) {
			c.Change, c.Origin, c.From = Renamed, conditions.OriginPerson, ""
		}))}, Armed: 40}, []string{"c1"}},
		{"every failing condition, in order", Input{Memories: []Memory{kept(
			failing(func(c *Check) { c.Condition = "c3" }), failing(func(c *Check) { c.Condition, c.Outcome = "c2", Unknown }),
			failing(nil))}, Armed: 40}, []string{"c1", "c3"}},
		{"a small space still flags one", Input{Memories: []Memory{kept(failing(nil))}, Armed: 1}, []string{"c1"}},
	}
	for _, c := range cases {
		c.in.Authoritative = true
		out := Decide(c.in)
		if out.Capped {
			t.Errorf("%s: capped", c.name)
			continue
		}
		switch {
		case c.want == nil && len(out.Flags) != 0:
			t.Errorf("%s: flagged %+v", c.name, out.Flags)
		case c.want != nil && (len(out.Flags) != 1 || fmt.Sprint(out.Flags[0].Conditions) != fmt.Sprint(c.want)):
			t.Errorf("%s: flagged %+v, want %v", c.name, out.Flags, c.want)
		}
	}
	if out := Decide(Input{Memories: []Memory{kept(failing(nil))}, Armed: 40}); len(out.Flags) != 0 {
		t.Errorf("an observer that isn't the repository's authority flagged %+v", out.Flags)
	}
}

func TestBulkCap(t *testing.T) {
	t.Parallel()
	many := func(n int) []Memory {
		ms := make([]Memory, n)
		for i := range ms {
			ms[i] = kept(failing(nil))
			ms[i].ID = uuid.New()
		}
		return ms
	}
	for _, c := range []struct {
		flags, armed int
		capped       bool
	}{
		{10, 1000, false}, // ten is the most one run flags
		{11, 1000, true},
		{4, 10, false}, // 40%, but under the floor
		{5, 25, false}, // 20% exactly
		{5, 24, true},  // over 20%
	} {
		out := Decide(Input{Memories: many(c.flags), Armed: c.armed, Authoritative: true})
		if out.Capped != c.capped || (c.capped && len(out.Flags) != 0) || (!c.capped && len(out.Flags) != c.flags) {
			t.Errorf("%d of %d armed: %+v, want capped %v", c.flags, c.armed, out, c.capped)
		}
	}
}

// Memax clears its own flag when the change is undone (a revert): its flag
// came from a check, a failure recovered in this run, and nothing else of
// it fails. Never a flag Dream or a date set, never while something still
// fails, never for an observer that isn't the repository's authority.
func TestDecideUnflags(t *testing.T) {
	t.Parallel()
	recovered := func(edit func(*Check)) Check {
		c := failing(func(c *Check) { c.Outcome, c.Recovered, c.Change = Holds, true, "" })
		if edit != nil {
			edit(&c)
		}
		return c
	}
	stale := func(edit func(*Memory), checks ...Check) Memory {
		m := kept(checks...)
		m.Stale, m.FlaggedByCheck = true, true
		if edit != nil {
			edit(&m)
		}
		return m
	}
	cases := []struct {
		name string
		m    Memory
		want bool
	}{
		{"its failure undone", stale(nil, recovered(nil)), true},
		{"a flag Dream or a date set", stale(func(m *Memory) { m.FlaggedByCheck = false }, recovered(nil)), false},
		{"another condition still fails", stale(func(m *Memory) { m.StillFailing = 1 }, recovered(nil)), false},
		{"it held all along in this run (nothing recovered)", stale(nil, recovered(func(c *Check) { c.Recovered = false })), false},
		{"unknown now, not holds", stale(nil, recovered(func(c *Check) { c.Outcome = Unknown })), false},
		{"not stale", stale(func(m *Memory) { m.Stale = false }, recovered(nil)), false},
		{"not kept", stale(func(m *Memory) { m.Kept = false }, recovered(nil)), false},
	}
	for _, c := range cases {
		out := Decide(Input{Memories: []Memory{c.m}, Armed: 40, Authoritative: true})
		if got := len(out.Unflags) == 1 && out.Unflags[0] == m1; got != c.want {
			t.Errorf("%s: unflags %v, want %v", c.name, out.Unflags, c.want)
		}
		if len(out.Flags) != 0 {
			t.Errorf("%s: flagged %+v", c.name, out.Flags)
		}
	}
	if out := Decide(Input{Memories: []Memory{stale(nil, recovered(nil))}, Armed: 40}); len(out.Unflags) != 0 {
		t.Errorf("an observer that isn't the repository's authority cleared a flag: %v", out.Unflags)
	}
	// A run that trips the bulk cap still clears what it undid.
	var ms []Memory
	for i := range 12 {
		m := kept(failing(nil))
		m.ID = uuid.New()
		_ = i
		ms = append(ms, m)
	}
	ms = append(ms, stale(nil, recovered(nil)))
	if out := Decide(Input{Memories: ms, Armed: 40, Authoritative: true}); !out.Capped || len(out.Unflags) != 1 {
		t.Errorf("capped run: %+v", out)
	}
}

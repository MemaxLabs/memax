package ledger_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

func TestRestoreBriefSections(t *testing.T) {
	t.Parallel()
	states := map[string]lifecycle.Lifecycle{
		"M-0001": lifecycle.Kept, "M-0002": lifecycle.Kept, "M-0003": lifecycle.Faded, "M-0004": lifecycle.Forgotten,
		"M-0005": lifecycle.Proposed, "M-0006": lifecycle.Rejected, "M-0007": lifecycle.Merged,
	}
	state := func(ref string) (lifecycle.Lifecycle, bool) {
		l, ok := states[ref]
		return l, ok
	}
	base := []ledger.BriefSection{
		{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{{Ref: "M-0001"}, {Ref: "M-0003"}, {Ref: "M-0004"}, {Ref: "M-0009"}}},
		{Key: "open", Heading: "Open", Items: []ledger.BriefItem{
			{Text: "Fly.io or Railway is undecided.", Cites: []string{"M-0005", "M-0006", "M-0001", "M-0009"}},
			{Text: "It rests on what faded.", Cites: []string{"M-0003", "M-0007"}},
			{Text: "It cites a forgotten memory.", Cites: []string{"M-0002", "M-0004"}},
			{Cites: []string{"M-0002"}, Forgotten: true},
			{Text: "Still stands.", Cites: []string{"M-0002"}},
		}},
		{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{}},
	}
	got, drops, err := ledger.RestoreBriefSections(base, state)
	if err != nil {
		t.Fatal(err)
	}
	want := []ledger.BriefSection{
		{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{{Ref: "M-0001"}}},
		{Key: "open", Heading: "Open", Items: []ledger.BriefItem{
			{Text: "Fly.io or Railway is undecided.", Cites: []string{"M-0005", "M-0001"}},
			{Text: "Still stands.", Cites: []string{"M-0002"}},
		}},
		{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sections = %+v\nwant %+v", got, want)
	}
	wantDrops := []ledger.BriefDrop{
		{Section: "decisions", Item: "M-0003", Kind: ledger.DropMemory, Refs: []string{"M-0003"}, Reason: ledger.DropNotKept},
		{Section: "decisions", Item: "M-0004", Kind: ledger.DropMemory, Refs: []string{"M-0004"}, Reason: ledger.DropForgotten},
		{Section: "decisions", Item: "M-0009", Kind: ledger.DropMemory, Refs: []string{"M-0009"}, Reason: ledger.DropNotKept},
		{Section: "open", Item: "P:open:0", Kind: ledger.DropCite, Refs: []string{"M-0006"}, Reason: ledger.DropNotKept},
		{Section: "open", Item: "P:open:0", Kind: ledger.DropCite, Refs: []string{"M-0009"}, Reason: ledger.DropNotKept},
		{Section: "open", Item: "P:open:1", Kind: ledger.DropProse, Refs: []string{"M-0003", "M-0007"}, Reason: ledger.DropNotKept},
		{Section: "open", Item: "P:open:2", Kind: ledger.DropProse, Refs: []string{"M-0004"}, Reason: ledger.DropForgotten},
		{Section: "open", Item: "P:open:3", Kind: ledger.DropProse, Refs: []string{"M-0002"}, Reason: ledger.DropForgotten},
	}
	if !reflect.DeepEqual(drops, wantDrops) {
		t.Errorf("drops = %+v\nwant %+v", drops, wantDrops)
	}

	// An uncited line fails as Dream's operations do.
	_, _, err = ledger.RestoreBriefSections([]ledger.BriefSection{{Key: "open", Heading: "Open",
		Items: []ledger.BriefItem{{Text: "An uncited claim."}}}}, state)
	var ve *ledger.ValidationError
	if !errors.As(err, &ve) || ve.Field != "sections.items.cites" || !strings.Contains(ve.Message, "uncited claim") {
		t.Errorf("an uncited line: %v", err)
	}
	_, err = ledger.ApplyBriefOps(base, []ledger.BriefOp{{Op: ledger.BriefOpAdd, Section: "open", Text: "An uncited claim."}},
		func(string) bool { return true })
	var dream *ledger.ValidationError
	if !errors.As(err, &dream) || dream.Message != ve.Message {
		t.Errorf("Dream refuses an uncited line with %v; a restore with %v", err, ve)
	}
}

func TestRestoreBrief(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz, vi := f.user("zz"), f.user("vi")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(space, vi, "viewer")
	river := f.remember(zz, space, "Background jobs run on River.")
	pnpm := f.remember(zz, space, "pnpm workspaces only.")
	docs := f.remember(zz, space, "The docs site builds with Fumadocs.")
	gone := f.remember(zz, space, "The staging database password rotates on Fridays.")
	fly := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(zz), policy.ViaMCP),
		NewMemory: fact(space, "Deploy to Fly.io.")}).Memory

	v1 := f.brief(zz, space, 0, []ledger.BriefSection{
		{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{{Ref: river.Ref}, {Ref: docs.Ref}}},
		{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{{Ref: pnpm.Ref}, {Ref: gone.Ref}}},
		{Key: "open", Heading: "Open", Items: []ledger.BriefItem{
			{Text: "Fly.io or Railway is undecided.", Cites: []string{fly.Ref, river.Ref}},
			{Text: "The docs follow the code.", Cites: []string{docs.Ref}},
			{Text: "Rotation is weekly.", Cites: []string{gone.Ref, pnpm.Ref}},
		}},
	})
	f.brief(zz, space, 1, demoSections(pnpm.Ref))
	target := f.target(zz, space, ledger.TargetAgentsMD)

	// Since version 1: a fact faded, another was forgotten, and the
	// proposal a line cited was rejected.
	f.fade(docs)
	if res := f.apply(forgetCmd(person(zz), f.scope(zz), policy.ViaWeb, gone.Ref, gone.Version)); res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("forget: %s %s", res.Outcome, res.Policy.Message)
	}
	f.apply(&ledger.Reject{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: fly.Ref})
	cur, err := f.l.GetBrief(ctx, f.scope(zz), space)
	if err != nil || cur.Version != 2 {
		t.Fatalf("current Brief = %+v, %v", cur, err)
	}
	gen := f.get(zz, target.ID).DirtyGen

	m := meta(person(zz), f.scope(zz), policy.ViaWeb)
	cmd := &ledger.RestoreBrief{Meta: m, SpaceID: space, Version: 1, ExpectedVersion: 2}
	res := f.apply(cmd)
	b := res.Brief
	if res.Outcome != ledger.OutcomeApplied || b == nil || b.Version != 3 || b.ParentVersion != 2 || !b.Current ||
		b.Title != v1.Title || b.Summary != v1.Summary {
		t.Fatalf("restore = %+v (brief %+v)", res, b)
	}
	want := []ledger.BriefSection{
		{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{{Ref: river.Ref}}},
		{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{{Ref: pnpm.Ref}}},
		{Key: "open", Heading: "Open", Items: []ledger.BriefItem{{Text: "Fly.io or Railway is undecided.", Cites: []string{river.Ref}}}},
	}
	if !reflect.DeepEqual(b.Sections, want) {
		t.Errorf("restored sections = %+v\nwant %+v", b.Sections, want)
	}
	wantDrops := []ledger.BriefDrop{
		{Section: "decisions", Item: docs.Ref, Kind: ledger.DropMemory, Refs: []string{docs.Ref}, Reason: ledger.DropNotKept},
		{Section: "conventions", Item: gone.Ref, Kind: ledger.DropMemory, Refs: []string{gone.Ref}, Reason: ledger.DropForgotten},
		{Section: "open", Item: "P:open:0", Kind: ledger.DropCite, Refs: []string{fly.Ref}, Reason: ledger.DropNotKept},
		{Section: "open", Item: "P:open:1", Kind: ledger.DropProse, Refs: []string{docs.Ref}, Reason: ledger.DropNotKept},
		{Section: "open", Item: "P:open:2", Kind: ledger.DropProse, Refs: []string{gone.Ref}, Reason: ledger.DropForgotten},
	}
	if !reflect.DeepEqual(res.Dropped, wantDrops) {
		t.Errorf("dropped = %+v\nwant %+v", res.Dropped, wantDrops)
	}
	rc := res.Receipts[0]
	if rc.ObjectKind != ledger.ObjectBrief || rc.Action != ledger.ActionRevised || rc.ObjectRef != b.Ref ||
		rc.Source == nil || *rc.Source != (ledger.ReceiptSource{Kind: ledger.ObjectBrief, Ref: v1.Ref}) ||
		rc.Reason != "Restored B-0001, without 4 lines that rest on memories no longer kept." {
		t.Errorf("receipt = %+v", rc)
	}
	noWordsIn(t, rc, river.Statement, docs.Statement, gone.Statement)
	if g := f.get(zz, target.ID).DirtyGen; g <= gen {
		t.Errorf("the target's dirty_gen = %d after the restore, was %d: it must recompile", g, gen)
	}
	if f.jobs(target.ID) == 0 {
		t.Error("no compile job queued for the restore")
	}

	// A retry with the same key returns the same version and what it left out.
	again := f.apply(&ledger.RestoreBrief{Meta: m, SpaceID: space, Version: 1, ExpectedVersion: 2})
	if !again.Replayed || again.Brief.Ref != b.Ref || again.Receipts[0].ID != rc.ID || !reflect.DeepEqual(again.Dropped, wantDrops) {
		t.Errorf("replay = %+v", again)
	}
	if n := f.count(`SELECT count(*) FROM v2.brief_versions WHERE space_id = $1`, space); n != 3 {
		t.Errorf("versions = %d, want 3", n)
	}

	// The version in force is If-Match; the current version and one that
	// doesn't exist can't be restored.
	if _, err := f.l.Apply(ctx, &ledger.RestoreBrief{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), SpaceID: space,
		Version: 1, ExpectedVersion: 2}); !errors.Is(err, ledger.ErrEditClash) {
		t.Errorf("a stale If-Match: %v, want an edit clash", err)
	}
	for _, c := range []struct {
		name    string
		version int
	}{{"the version in force", 3}, {"a version that doesn't exist", 9}} {
		_, err := f.l.Apply(ctx, &ledger.RestoreBrief{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), SpaceID: space,
			Version: c.version, ExpectedVersion: 3})
		var ve *ledger.ValidationError
		if !errors.As(err, &ve) || ve.Field != "version" {
			t.Errorf("%s: %v, want invalid version", c.name, err)
		}
	}
	// Who may restore: whoever may revise the Brief.
	for _, r := range []struct {
		name  string
		actor ledger.Actor
		scope ledger.Scope
		code  string
	}{
		{"viewer", person(vi), f.scope(vi), policy.CodeViewer},
		{"agent", agentFor(policy.AutonomyWrite), f.scope(zz), policy.CodeBriefByPerson},
	} {
		res := f.apply(&ledger.RestoreBrief{Meta: meta(r.actor, r.scope, policy.ViaWeb), SpaceID: space, Version: 1, ExpectedVersion: 3})
		if res.Outcome != ledger.OutcomeRefused || res.Policy.Code != r.code {
			t.Errorf("%s: %s %s, want refused %s", r.name, res.Outcome, res.Policy.Code, r.code)
		}
	}
	// Restoring version 2 brings back what it held; a reason is the person's.
	m2 := meta(person(zz), f.scope(zz), policy.ViaWeb)
	m2.Reason = "back to the short one"
	back := f.apply(&ledger.RestoreBrief{Meta: m2, SpaceID: space, Version: 2, ExpectedVersion: 3})
	if back.Brief.Version != 4 || !reflect.DeepEqual(back.Brief.Sections, demoSections(pnpm.Ref)) || len(back.Dropped) != 0 ||
		back.Receipts[0].Reason != "back to the short one" {
		t.Errorf("restore version 2 = %+v (dropped %+v)", back.Brief, back.Dropped)
	}
	// Outside the scope, the space has no Brief to restore.
	other := f.user("other")
	if _, err := f.l.Apply(ctx, &ledger.RestoreBrief{Meta: meta(person(other), f.scope(other), policy.ViaWeb), SpaceID: space,
		Version: 1, ExpectedVersion: 4}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("outside the scope: %v", err)
	}
	empty := f.space(zz, policy.SpaceProject, "empty")
	if _, err := f.l.Apply(ctx, &ledger.RestoreBrief{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), SpaceID: empty,
		Version: 1, ExpectedVersion: 1}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("a space with no Brief: %v", err)
	}
}

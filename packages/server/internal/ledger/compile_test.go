package ledger_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func ptr[T any](v T) *T { return &v }

func TestReviseBrief(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz, jy, vi := f.user("zz"), f.user("jy"), f.user("vi")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(space, jy, "contributor")
	f.join(space, vi, "viewer")
	river := f.remember(zz, space, "Background jobs run on River.")
	pnpm := f.remember(jy, space, "pnpm workspaces only.")
	codex := agentFor(policy.AutonomyPropose)
	fly := f.apply(&ledger.Propose{Meta: meta(codex, f.scope(zz), policy.ViaMCP), NewMemory: fact(space, "Deploy to Fly.io.")}).Memory

	sections := []ledger.BriefSection{
		{Key: "decisions", Heading: " Decisions ", Items: []ledger.BriefItem{{Ref: strings.ToLower(river.Ref)}}},
		{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{{Ref: pnpm.Ref}}},
		{Key: "open", Heading: "Open", Items: []ledger.BriefItem{
			{Text: "Fly.io or Railway is undecided.", Cites: []string{fly.Ref, river.Ref, fly.Ref}}}},
	}
	m := meta(person(zz), f.scope(zz), policy.ViaWeb)
	m.Reason = "the first Brief"
	res := f.apply(&ledger.ReviseBrief{Meta: m, SpaceID: space, Title: " Memax V2 engineering brief ", Sections: sections})
	b := res.Brief
	if b.Ref != "B-0001" || b.Version != 1 || b.ParentVersion != 0 || !b.Current || b.Title != "Memax V2 engineering brief" {
		t.Fatalf("brief = %+v", b)
	}
	if b.Sections[0].Items[0].Ref != river.Ref || b.Sections[0].Heading != "Decisions" {
		t.Errorf("refs and headings are normalised: %+v", b.Sections[0])
	}
	if cites := b.Sections[2].Items[0].Cites; !slices.Equal(cites, []string{fly.Ref, river.Ref}) {
		t.Errorf("cites = %v, want deduplicated", cites)
	}
	if b.Facts != 3 {
		t.Errorf("facts = %d, want 3", b.Facts)
	}
	rc := res.Receipts[0]
	if rc.ObjectKind != ledger.ObjectBrief || rc.Action != ledger.ActionRevised || rc.ObjectRef != "B-0001" ||
		rc.ObjectID != b.ID || rc.StreamVersion != 1 || rc.Reason != "the first Brief" {
		t.Errorf("receipt = %+v", rc)
	}
	if b.Receipt == nil || b.Receipt.ID != rc.ID {
		t.Errorf("author receipt = %+v", b.Receipt)
	}

	// A second version needs the version it started from.
	next := demoSections(pnpm.Ref, river.Ref)
	if _, err := f.l.Apply(ctx, &ledger.ReviseBrief{Meta: meta(person(jy), f.scope(jy), policy.ViaWeb), SpaceID: space,
		Title: "Brief", Sections: next}); !errors.Is(err, ledger.ErrEditClash) {
		t.Errorf("revise without If-Match: %v, want edit clash", err)
	}
	b2 := f.apply(&ledger.ReviseBrief{Meta: meta(person(jy), f.scope(jy), policy.ViaWeb), SpaceID: space,
		ExpectedVersion: 1, Title: "Brief", Sections: next}).Brief
	if b2.Ref != "B-0002" || b2.Version != 2 || b2.ParentVersion != 1 || b2.ID != b.ID {
		t.Errorf("second version = %+v", b2)
	}
	cur, err := f.l.GetBrief(ctx, f.scope(vi), space)
	if err != nil || cur.Version != 2 || cur.Receipt == nil || cur.Receipt.ActorID == nil || *cur.Receipt.ActorID != jy {
		t.Errorf("GetBrief = %+v, %v", cur, err)
	}
	page, err := f.l.ListBriefVersions(ctx, f.scope(zz), ledger.BriefVersionQuery{SpaceID: space, Limit: 1})
	if err != nil || len(page.Versions) != 1 || page.Versions[0].Version != 2 || !page.HasMore {
		t.Fatalf("versions page 1 = %+v, %v", page, err)
	}
	page, err = f.l.ListBriefVersions(ctx, f.scope(zz), ledger.BriefVersionQuery{SpaceID: space, Cursor: page.NextCursor})
	if err != nil || len(page.Versions) != 1 || page.Versions[0].Version != 1 || page.Versions[0].Current || page.HasMore {
		t.Fatalf("versions page 2 = %+v, %v", page, err)
	}

	// Validation against the record.
	bad := []struct {
		name     string
		sections []ledger.BriefSection
		field    string
	}{
		{"a proposal as an item", demoSections(fly.Ref), "sections.items.ref"},
		{"a memory of no space", demoSections("M-9999"), "sections.items.ref"},
		{"placed twice", demoSections(river.Ref, river.Ref), "sections.items.ref"},
		{"prose without cites", []ledger.BriefSection{{Key: "open", Heading: "Open", Items: []ledger.BriefItem{{Text: "Undecided."}}}}, "sections.items.cites"},
		{"prose citing only a proposal", []ledger.BriefSection{{Key: "open", Heading: "Open", Items: []ledger.BriefItem{{Text: "Undecided.", Cites: []string{fly.Ref}}}}}, "sections.items.cites"},
		{"a ref and prose at once", []ledger.BriefSection{{Key: "open", Heading: "Open", Items: []ledger.BriefItem{{Ref: river.Ref, Text: "x"}}}}, "sections.items"},
		{"a bad key", []ledger.BriefSection{{Key: "Decisions!", Heading: "D"}}, "sections.key"},
		{"a key twice", []ledger.BriefSection{{Key: "a", Heading: "A"}, {Key: "a", Heading: "B"}}, "sections.key"},
		{"a heading with a line break", []ledger.BriefSection{{Key: "a", Heading: "A\n## Injected"}}, "sections.heading"},
		{"prose with a line break", []ledger.BriefSection{{Key: "a", Heading: "A", Items: []ledger.BriefItem{{Text: "x\n# y", Cites: []string{river.Ref}}}}}, "sections.items.text"},
	}
	for _, c := range bad {
		_, err := f.l.Apply(ctx, &ledger.ReviseBrief{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), SpaceID: space,
			ExpectedVersion: 2, Title: "Brief", Sections: c.sections})
		var ve *ledger.ValidationError
		if !errors.As(err, &ve) || ve.Field != c.field {
			t.Errorf("%s: %v, want invalid %s", c.name, err, c.field)
		}
	}

	// Who may edit the Brief.
	refusals := []struct {
		name  string
		actor ledger.Actor
		scope ledger.Scope
		code  string
	}{
		{"viewer", person(vi), f.scope(vi), policy.CodeViewer},
		{"agent", agentFor(policy.AutonomyWrite), f.scope(zz), policy.CodeBriefByPerson},
	}
	for _, r := range refusals {
		res := f.apply(&ledger.ReviseBrief{Meta: meta(r.actor, r.scope, policy.ViaWeb), SpaceID: space, ExpectedVersion: 2,
			Title: "Brief", Sections: next})
		if res.Outcome != ledger.OutcomeRefused || res.Policy.Code != r.code {
			t.Errorf("%s: %s %s, want refused %s", r.name, res.Outcome, res.Policy.Code, r.code)
		}
	}
	secret := f.apply(&ledger.ReviseBrief{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), SpaceID: space, ExpectedVersion: 2,
		Title: "Brief", Summary: "key AKIAIOSFODNN7EXAMPLE", Sections: next})
	if secret.Policy.Code != policy.CodeSecret {
		t.Errorf("a secret in the summary: %s", secret.Policy.Code)
	}

	// Replays return the original version.
	m2 := meta(person(zz), f.scope(zz), policy.ViaWeb)
	cmd := &ledger.ReviseBrief{Meta: m2, SpaceID: space, ExpectedVersion: 2, Title: "Brief 3", Sections: next}
	first := f.apply(cmd)
	again := f.apply(&ledger.ReviseBrief{Meta: m2, SpaceID: space, ExpectedVersion: 2, Title: "Brief 3", Sections: next})
	if !again.Replayed || again.Brief.Ref != first.Brief.Ref || again.Receipts[0].ID != first.Receipts[0].ID {
		t.Errorf("replay = %+v", again)
	}
	if n := f.count(`SELECT count(*) FROM v2.brief_versions WHERE space_id = $1`, space); n != 3 {
		t.Errorf("versions = %d, want 3", n)
	}
	// Outside the scope, the space doesn't exist.
	other := f.user("other")
	if _, err := f.l.GetBrief(ctx, f.scope(other), space); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("GetBrief outside the scope: %v", err)
	}
}

func TestConfigureTarget(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz, vi := f.user("zz"), f.user("vi")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(space, vi, "viewer")

	// A target needs a Brief to compile.
	_, err := f.l.Apply(ctx, &ledger.ConfigureTarget{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), SpaceID: space, Kind: ledger.TargetAgentsMD})
	var ve *ledger.ValidationError
	if !errors.As(err, &ve) || ve.Field != "brief" {
		t.Fatalf("target before a Brief: %v", err)
	}
	f.brief(zz, space, 0, nil)

	defaults := map[ledger.TargetKind]struct {
		path     string
		delivery ledger.Delivery
		scoped   ledger.ScopedMode
	}{
		ledger.TargetAgentsMD:  {"AGENTS.md", ledger.DeliveryLocal, ledger.ScopedInline},
		ledger.TargetClaudeMD:  {"CLAUDE.md", ledger.DeliveryLocal, ""},
		ledger.TargetCursorMDC: {".cursor/rules", ledger.DeliveryLocal, ""},
		ledger.TargetChatGPT:   {"", ledger.DeliveryCopy, ledger.ScopedInline},
	}
	ids := map[ledger.TargetKind]uuid.UUID{}
	for _, k := range ledger.DefaultTargetKinds {
		tg := f.target(zz, space, k)
		want := defaults[k]
		if tg.Path != want.path || tg.Delivery != want.delivery || tg.Settings.Scoped != want.scoped ||
			tg.Settings.Include != ledger.IncludeKeptAndOpen || tg.Settings.Stale != ledger.StaleMark ||
			tg.Settings.SizeBudget != ledger.DefaultSizeBudget {
			t.Errorf("%s defaults = %+v", k, tg)
		}
		if tg.SyncState != ledger.SyncCompiling || tg.DirtyGen != 1 || tg.CompiledGen != 0 || tg.Version != 1 {
			t.Errorf("%s starts %s %d/%d v%d, want compiling 1/0 v1", k, tg.SyncState, tg.DirtyGen, tg.CompiledGen, tg.Version)
		}
		if f.jobs(tg.ID) != 1 {
			t.Errorf("%s: compile jobs = %d, want 1", k, f.jobs(tg.ID))
		}
		ids[k] = tg.ID
	}
	if got := f.get(zz, ids[ledger.TargetChatGPT]).Label; got != "ChatGPT project" {
		t.Errorf("chatgpt label = %q", got)
	}
	list, err := f.l.ListTargets(ctx, f.scope(vi), space)
	if err != nil || len(list) != 4 || list[0].Kind != ledger.TargetAgentsMD || list[3].Kind != ledger.TargetChatGPT {
		t.Fatalf("ListTargets = %+v, %v", list, err)
	}

	bad := []struct {
		name string
		cmd  ledger.ConfigureTarget
		want string
	}{
		{"a second AGENTS.md at the same path", ledger.ConfigureTarget{Kind: ledger.TargetAgentsMD}, "path"},
		{"AGENTS.md under another name", ledger.ConfigureTarget{Kind: ledger.TargetAgentsMD, Path: ptr("docs/README.md")}, "path"},
		{"a path out of the repository", ledger.ConfigureTarget{Kind: ledger.TargetAgentsMD, Path: ptr("../AGENTS.md")}, "path"},
		{"an absolute path", ledger.ConfigureTarget{Kind: ledger.TargetClaudeMD, Path: ptr("/CLAUDE.md")}, "path"},
		{"cursor rules outside .cursor/rules", ledger.ConfigureTarget{Kind: ledger.TargetCursorMDC, Path: ptr("rules")}, "path"},
		{"a path for ChatGPT", ledger.ConfigureTarget{Kind: ledger.TargetChatGPT, Path: ptr("chatgpt.md")}, "path"},
		{"a file copied out", ledger.ConfigureTarget{Kind: ledger.TargetAgentsMD, Path: ptr("pkg/AGENTS.md"), Delivery: ptr(ledger.DeliveryCopy)}, "delivery"},
		{"ChatGPT written locally", ledger.ConfigureTarget{Kind: ledger.TargetGeminiMD, Delivery: ptr(ledger.Delivery("ftp"))}, "delivery"},
		{"a tiny budget", ledger.ConfigureTarget{Kind: ledger.TargetGeminiMD, Settings: &ledger.TargetSettingsInput{SizeBudget: ptr(100)}}, "settings.size_budget"},
		{"user-owned AGENTS.md", ledger.ConfigureTarget{Kind: ledger.TargetAgentsMD, Path: ptr("pkg/AGENTS.md"), Settings: &ledger.TargetSettingsInput{UserOwned: ptr(true)}}, "settings.user_owned"},
		{"scoped on a shim", ledger.ConfigureTarget{Kind: ledger.TargetGeminiMD, Settings: &ledger.TargetSettingsInput{Scoped: ptr(ledger.ScopedOmit)}}, "settings.scoped"},
		{"an unknown kind", ledger.ConfigureTarget{Kind: "notion"}, "kind"},
	}
	for _, c := range bad {
		cmd := c.cmd
		cmd.Meta, cmd.SpaceID = meta(person(zz), f.scope(zz), policy.ViaWeb), space
		_, err := f.l.Apply(ctx, &cmd)
		if !errors.As(err, &ve) || ve.Field != c.want {
			t.Errorf("%s: %v, want invalid %s", c.name, err, c.want)
		}
	}
	if res := f.apply(&ledger.ConfigureTarget{Meta: meta(person(vi), f.scope(vi), policy.ViaWeb), SpaceID: space, Kind: ledger.TargetGeminiMD}); res.Policy.Code != policy.CodeViewer {
		t.Errorf("a viewer adds a target: %s", res.Policy.Code)
	}
	if res := f.apply(&ledger.ConfigureTarget{Meta: meta(agentFor(policy.AutonomyWrite), f.scope(zz), policy.ViaMCP), SpaceID: space, Kind: ledger.TargetGeminiMD}); res.Policy.Code != policy.CodeTargetsByPerson {
		t.Errorf("an agent adds a target: %s", res.Policy.Code)
	}

	// Changing settings: a receipt, a new version, a recompile.
	agents := ids[ledger.TargetAgentsMD]
	f.compiled(f.get(zz, agents), "v1")
	res := f.apply(&ledger.ConfigureTarget{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: agents, ExpectedVersion: 1,
		Settings: &ledger.TargetSettingsInput{Include: ptr(ledger.IncludeKeptOnly), SizeBudget: ptr(16384)}})
	tg := res.Target
	if tg.Settings.Include != ledger.IncludeKeptOnly || tg.Settings.SizeBudget != 16384 || tg.Settings.Scoped != ledger.ScopedInline ||
		tg.Version != 2 || tg.DirtyGen != 2 || tg.SyncState != ledger.SyncCompiling {
		t.Errorf("after configure: %+v", tg)
	}
	if rc := res.Receipts[0]; rc.Action != ledger.ActionConfigured || rc.ObjectKind != ledger.ObjectTarget || rc.ObjectRef != "AGENTS.md" {
		t.Errorf("configure receipt = %+v", rc)
	}
	if _, err := f.l.Apply(ctx, &ledger.ConfigureTarget{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: agents, ExpectedVersion: 1,
		Settings: &ledger.TargetSettingsInput{Stale: ptr(ledger.StaleOmit)}}); !errors.Is(err, ledger.ErrEditClash) {
		t.Errorf("a stale If-Match: %v", err)
	}
	same := f.apply(&ledger.ConfigureTarget{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: agents,
		Settings: &ledger.TargetSettingsInput{Include: ptr(ledger.IncludeKeptOnly)}})
	if !same.Unchanged || len(same.Receipts) != 0 {
		t.Errorf("an unchanged configure wrote: %+v", same)
	}

	// Stop and start again.
	stop := f.apply(&ledger.ConfigureTarget{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: agents, Enabled: ptr(false)})
	if stop.Target.SyncState != ledger.SyncOff || stop.Receipts[0].Action != ledger.ActionStopped {
		t.Errorf("stop = %s %s", stop.Target.SyncState, stop.Receipts[0].Action)
	}
	if _, err := f.l.Apply(ctx, &ledger.RequestCompile{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: agents}); !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("compiling a stopped target: %v", err)
	}
	start := f.apply(&ledger.ConfigureTarget{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: agents, Enabled: ptr(true)})
	if start.Target.SyncState != ledger.SyncCompiling || start.Target.DirtyGen != 3 {
		t.Errorf("start = %s gen %d", start.Target.SyncState, start.Target.DirtyGen)
	}

	// Compile now: any member, a viewer too; not a read-only agent.
	now := f.apply(&ledger.RequestCompile{Meta: meta(person(vi), f.scope(vi), policy.ViaWeb), Target: ids[ledger.TargetClaudeMD]})
	if now.Receipts[0].Action != ledger.ActionRequested || now.Target.DirtyGen != 2 {
		t.Errorf("compile now = %+v", now.Receipts[0])
	}
	if res := f.apply(&ledger.RequestCompile{Meta: meta(agentFor(policy.AutonomyRead), f.scope(zz), policy.ViaMCP), Target: ids[ledger.TargetClaudeMD]}); res.Policy.Code != policy.CodeReadOnly {
		t.Errorf("a read-only agent compiles: %s", res.Policy.Code)
	}
	// One job per target however often it is dirtied.
	if n := f.jobs(ids[ledger.TargetClaudeMD]); n != 1 {
		t.Errorf("claude_md jobs = %d, want 1 (unique by args)", n)
	}
}

// Rule 5's first half: whatever changes the kept set dirties every
// target of the space in the same transaction, and queues its compile.
func TestKeepDirtiesEveryTarget(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	otherSpace := f.space(zz, policy.SpaceProject, "elsewhere")
	f.brief(zz, space, 0, nil)
	f.brief(zz, otherSpace, 0, nil)
	var targets []*ledger.Target
	for _, k := range ledger.DefaultTargetKinds {
		tg := f.target(zz, space, k)
		f.compiled(tg, "v1 "+string(k))
		targets = append(targets, tg)
	}
	elsewhere := f.target(zz, otherSpace, ledger.TargetAgentsMD)
	f.compiled(elsewhere, "elsewhere")
	off := f.apply(&ledger.ConfigureTarget{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: targets[3].ID, Enabled: ptr(false)}).Target
	f.exec(`DELETE FROM river_job`)

	gens := func() []int64 {
		var out []int64
		for _, tg := range targets {
			out = append(out, f.get(zz, tg.ID).DirtyGen)
		}
		return out
	}
	before := gens()
	scope := f.scope(zz)
	step := func(name string, cmd ledger.Command, dirties bool) {
		t.Helper()
		if _, err := f.l.Apply(ctx, cmd); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		after := gens()
		for i, tg := range targets {
			want := before[i]
			if dirties && tg.ID != off.ID {
				want++
			}
			if after[i] != want {
				t.Errorf("%s: %s dirty_gen %d → %d, want %d", name, tg.Kind, before[i], after[i], want)
			}
		}
		if g := f.get(zz, elsewhere.ID).DirtyGen; g != 1 {
			t.Errorf("%s: a target of another space was dirtied (%d)", name, g)
		}
		before = after
	}
	codex := agentFor(policy.AutonomyPropose)
	p := f.apply(&ledger.Propose{Meta: meta(codex, scope, policy.ViaMCP), NewMemory: fact(space, "MCP write tools ask with input_required.")}).Memory
	step("a proposal", &ledger.Propose{Meta: meta(codex, scope, policy.ViaMCP), NewMemory: fact(space, "Another proposal.")}, false)
	step("remember (kept)", &ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: fact(space, "River, not Temporal.")}, true)
	step("keep", &ledger.Keep{Meta: meta(person(zz), scope, policy.ViaReview), Memory: p.Ref}, true)
	r := f.apply(&ledger.Propose{Meta: meta(codex, scope, policy.ViaMCP), NewMemory: fact(space, "Rejected soon.")}).Memory
	step("reject", &ledger.Reject{Meta: meta(person(zz), scope, policy.ViaReview), Memory: r.Ref}, false)
	step("edit a kept memory", &ledger.Edit{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: p.Ref, ExpectedVersion: 1,
		Statement: "MCP write tools must ask with input_required."}, true)
	step("an agent's edit, sent to Review", &ledger.Edit{Meta: meta(agentFor(policy.AutonomyWrite), scope, policy.ViaMCP), Memory: p.Ref,
		ExpectedVersion: 2, Statement: "MCP write tools may skip input_required."}, false)
	q := f.apply(&ledger.Propose{Meta: meta(codex, scope, policy.ViaMCP), NewMemory: fact(space, "Edit me then keep me.")}).Memory
	step("edit and keep a proposal", &ledger.Edit{Meta: meta(person(zz), scope, policy.ViaReview), Memory: q.Ref, ExpectedVersion: 1,
		Statement: "Edited, then kept.", Keep: true}, true)
	step("a new Brief version", &ledger.ReviseBrief{Meta: meta(person(zz), scope, policy.ViaWeb), SpaceID: space, ExpectedVersion: 1,
		Title: "Brief", Sections: demoSections(p.Ref)}, true)

	// One compile_target job per live target, however many changes.
	for _, tg := range targets {
		want := 1
		if tg.ID == off.ID {
			want = 0
		}
		if n := f.jobs(tg.ID); n != want {
			t.Errorf("%s: %d jobs, want %d", tg.Kind, n, want)
		}
	}
	for _, tg := range targets[:3] {
		if st := f.get(zz, tg.ID).SyncState; st != ledger.SyncCompiling {
			t.Errorf("%s is %s after a keep, want compiling", tg.Kind, st)
		}
	}
}

// The River insert runs as the transaction's login role, between two
// role switches, and the rest of the command (down to the deferred
// receipt checks at COMMIT) runs as memax_v2.
func TestEnqueueSwitchesRoleForRiverOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	client, err := river.NewClient(riverpgxv5.New(f.pool), &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	spy := &spyJobs{next: client}
	l := ledger.New(f.pool, ledger.WithJobs(spy))
	// A deferred trigger logs the role COMMIT runs the deferred checks as.
	f.exec(`CREATE TABLE public.commit_roles (role text)`)
	f.exec(`GRANT INSERT ON public.commit_roles TO memax_v2`)
	f.exec(`CREATE FUNCTION public.log_commit_role() RETURNS trigger LANGUAGE plpgsql AS $$
	        BEGIN INSERT INTO public.commit_roles VALUES (current_user); RETURN NULL; END $$`)
	f.exec(`CREATE CONSTRAINT TRIGGER log_commit_role AFTER INSERT ON v2.targets DEFERRABLE INITIALLY DEFERRED
	        FOR EACH ROW EXECUTE FUNCTION public.log_commit_role()`)

	if _, err := l.Apply(ctx, &ledger.ReviseBrief{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), SpaceID: space, Title: "B"}); err != nil {
		t.Fatal(err)
	}
	res, err := l.Apply(ctx, &ledger.ConfigureTarget{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), SpaceID: space, Kind: ledger.TargetAgentsMD})
	if err != nil {
		t.Fatal(err)
	}
	var login string
	if err := f.pool.QueryRow(ctx, `SELECT current_user`).Scan(&login); err != nil {
		t.Fatal(err)
	}
	if len(spy.calls) != 1 || spy.calls[0].user != login || spy.calls[0].jobs != 1 {
		t.Fatalf("River inserts = %+v, want one insert of one job as %s", spy.calls, login)
	}
	if n := f.count(`SELECT count(*) FROM river_job WHERE kind = 'compile_target' AND args->>'target_id' = $1`, res.Target.ID.String()); n != 1 {
		t.Errorf("compile jobs = %d, want 1 committed with the target", n)
	}
	var roles []string
	rows, err := f.pool.Query(ctx, `SELECT role FROM public.commit_roles`)
	if err != nil {
		t.Fatal(err)
	}
	if roles, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || !slices.Equal(roles, []string{ledger.DBRole}) {
		t.Errorf("deferred checks ran as %v (%v), want [%s]: the role must be switched back after the insert", roles, err, ledger.DBRole)
	}
	// memax_v2 itself can't touch River's table.
	var can bool
	if err := f.pool.QueryRow(ctx, `SELECT has_table_privilege('memax_v2', 'public.river_job', 'SELECT')
	                                   OR has_table_privilege('memax_v2', 'public.river_job', 'INSERT')`).Scan(&can); err != nil || can {
		t.Errorf("memax_v2 has privileges on river_job (%v, %v)", can, err)
	}
}

type spyCall struct {
	user string
	jobs int
}

// spyJobs records who inserts, then inserts with River.
type spyJobs struct {
	next  ledger.Jobs
	calls []spyCall
}

func (s *spyJobs) InsertManyTx(ctx context.Context, tx pgx.Tx, params []river.InsertManyParams) ([]*rivertype.JobInsertResult, error) {
	var user string
	if err := tx.QueryRow(ctx, `SELECT current_user`).Scan(&user); err != nil {
		return nil, err
	}
	s.calls = append(s.calls, spyCall{user: user, jobs: len(params)})
	return s.next.InsertManyTx(ctx, tx, params)
}

// TestRiverFailureRollsBackTheCommand: River v0.49 has no savepoints, so
// a failed insert aborts the transaction, and the whole command (receipt,
// projection, idempotency key) rolls back with it.
func TestRiverFailureRollsBackTheCommand(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.brief(zz, space, 0, nil)
	tg := f.target(zz, space, ledger.TargetAgentsMD)
	f.compiled(tg, "v1")
	p := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), f.scope(zz), policy.ViaMCP), NewMemory: fact(space, "Kept soon.")}).Memory
	receipts := f.count(`SELECT count(*) FROM v2.receipts`)

	// Break River's table, the way an outage or a bad migration would.
	f.exec(`ALTER TABLE river_job RENAME TO river_job_gone`)
	m := meta(person(zz), f.scope(zz), policy.ViaReview)
	_, err := f.l.Apply(ctx, &ledger.Keep{Meta: m, Memory: p.Ref})
	if err == nil || !strings.Contains(err.Error(), "enqueue") {
		t.Fatalf("keep with River down: %v, want an enqueue error", err)
	}
	f.exec(`ALTER TABLE river_job_gone RENAME TO river_job`)

	if got, err := f.l.GetMemory(ctx, f.scope(zz), p.ID.String()); err != nil || got.Lifecycle != lifecycle.Proposed {
		t.Errorf("the memory is %v (%v); the keep must have rolled back", got, err)
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts`); n != receipts {
		t.Errorf("receipts %d → %d: the keep's receipt survived", receipts, n)
	}
	if g := f.get(zz, tg.ID); g.DirtyGen != 1 || g.SyncState != ledger.SyncPendingDelivery {
		t.Errorf("target %d %s: the bump survived", g.DirtyGen, g.SyncState)
	}
	if n := f.count(`SELECT count(*) FROM v2.command_keys WHERE idempotency_key = $1`, m.IdempotencyKey); n != 0 {
		t.Errorf("the idempotency key survived")
	}
	// With River back, the same command (same key) applies.
	res := f.apply(&ledger.Keep{Meta: m, Memory: p.Ref})
	if res.Replayed || res.Memory.Lifecycle != lifecycle.Kept || f.jobs(tg.ID) != 1 {
		t.Errorf("retry = replayed %v %s, jobs %d", res.Replayed, res.Memory.Lifecycle, f.jobs(tg.ID))
	}
}

// The generation counter, in the ledger: RecordCompile refuses a run the
// target moved past, records with AllowBehind, and never records twice.
func TestRecordCompileGenerations(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.brief(zz, space, 0, nil)
	tg := f.target(zz, space, ledger.TargetAgentsMD)
	scope, err := f.l.SpaceScope(ctx, space)
	if err != nil {
		t.Fatal(err)
	}
	if len(scope.Spaces) != 1 || scope.Spaces[0].Role != policy.RoleNone {
		t.Fatalf("SpaceScope = %+v", scope)
	}
	brief, _ := f.l.GetBrief(ctx, scope, space)
	record := func(gen int64, allowBehind bool) (ledger.Result, error) {
		ref, err := f.l.ReserveCompileRef(ctx, scope, space)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		return f.l.Apply(ctx, &ledger.RecordCompile{
			Meta:   ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: scope, Via: policy.ViaSystem, IdempotencyKey: uuid.NewString()},
			Target: tg.ID, Ref: ref, Generation: gen, BriefID: brief.ID, BriefVersion: brief.Version,
			InputSHA256: sha("in"), OutputSHA256: sha("out"), DriftSHA256: sha("out"), ArtifactKey: "k/" + ref,
			Files:      []ledger.CompiledOutput{{Path: "AGENTS.md", SHA256: sha("out"), DriftSHA256: sha("out")}},
			EnqueuedAt: now, StartedAt: now, CompiledAt: now, AllowBehind: allowBehind,
		})
	}
	// Dirty it again while "compiling" generation 1.
	f.apply(&ledger.RequestCompile{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: tg.ID})
	if _, err := record(1, false); !errors.Is(err, ledger.ErrBehind) {
		t.Fatalf("recording gen 1 of a gen-2 target: %v, want ErrBehind", err)
	}
	res, err := record(1, true)
	if err != nil || res.Compile == nil || res.Target.CompiledGen != 1 || res.Target.SyncState != ledger.SyncCompiling {
		t.Fatalf("AllowBehind: %+v, %v", res.Target, err)
	}
	res, err = record(2, false)
	if err != nil || res.Target.CompiledGen != 2 || res.Target.SyncState != ledger.SyncPendingDelivery {
		t.Fatalf("gen 2: %+v %v", res.Target, err)
	}
	if res.Compile.Ref != "C-0003" || res.Compile.Status != ledger.CompileCompiled || res.Compile.Brief != brief.Ref {
		t.Errorf("run = %+v", res.Compile)
	}
	rc := res.Receipts[0]
	if rc.ObjectKind != ledger.ObjectCompile || rc.Action != ledger.ActionCompiled || rc.ActorKind != policy.ActorMemax || rc.ObjectRef != res.Compile.Ref {
		t.Errorf("compile receipt = %+v", rc)
	}
	again, err := record(2, false)
	if err != nil || !again.Unchanged {
		t.Errorf("recording gen 2 twice: %+v %v", again, err)
	}
	if n := f.count(`SELECT count(*) FROM v2.compile_runs WHERE target_id = $1`, tg.ID); n != 2 {
		t.Errorf("runs = %d, want 2", n)
	}
	// Only Memax records compiles.
	ref, _ := f.l.ReserveCompileRef(ctx, scope, space)
	now := time.Now()
	refused := f.apply(&ledger.RecordCompile{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: tg.ID, Ref: ref, Generation: 2,
		BriefID: brief.ID, BriefVersion: 1, InputSHA256: sha("x"), Error: "nope", EnqueuedAt: now, StartedAt: now, CompiledAt: now})
	if refused.Policy.Code != policy.CodeCompileByMemax {
		t.Errorf("a person records a compile: %s", refused.Policy.Code)
	}
	// SettleUnchanged: behind, then settles.
	f.apply(&ledger.RequestCompile{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: tg.ID})
	if err := f.l.SettleUnchanged(ctx, scope, tg.ID, 2); !errors.Is(err, ledger.ErrBehind) {
		t.Errorf("settle a behind generation: %v", err)
	}
	if err := f.l.SettleUnchanged(ctx, scope, tg.ID, 3); err != nil {
		t.Fatal(err)
	}
	if g := f.get(zz, tg.ID); g.CompiledGen != 3 || g.SyncState != ledger.SyncPendingDelivery {
		t.Errorf("settled = %d %s", g.CompiledGen, g.SyncState)
	}
	// A failed compile is recorded with its error, and the target settles.
	f.apply(&ledger.RequestCompile{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: tg.ID})
	ref, _ = f.l.ReserveCompileRef(ctx, scope, space)
	failed := f.apply(&ledger.RecordCompile{
		Meta:   ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: scope, Via: policy.ViaSystem, IdempotencyKey: uuid.NewString()},
		Target: tg.ID, Ref: ref, Generation: 4, BriefID: brief.ID, BriefVersion: 1, InputSHA256: sha("x"),
		Error: "compile: input refused: /memories/0/scope/paths/0: bad glob", EnqueuedAt: now, StartedAt: now, CompiledAt: now})
	if failed.Compile.Status != ledger.CompileFailed || failed.Target.CompiledGen != 4 || failed.Target.LastCompile.Ref != ref {
		t.Errorf("failed run = %+v / %+v", failed.Compile, failed.Target)
	}
	runs, err := f.l.ListCompileRuns(ctx, f.scope(zz), ledger.CompileRunQuery{TargetID: tg.ID, Limit: 2})
	if err != nil || len(runs.Runs) != 2 || runs.Runs[0].Status != ledger.CompileFailed || !runs.HasMore {
		t.Errorf("runs = %+v %v", runs, err)
	}
}

func TestDeliveryDriftAndResolution(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(space, jy, "contributor")
	pnpm := f.remember(zz, space, "pnpm workspaces only.")
	receipt := f.remember(zz, space, "Every write tool returns a receipt ID.")
	f.brief(zz, space, 0, demoSections(pnpm.Ref, receipt.Ref))
	tg := f.target(zz, space, ledger.TargetAgentsMD)
	run := f.compiled(tg, "compiled v1")
	if g := f.get(zz, tg.ID); g.SyncState != ledger.SyncPendingDelivery || g.Delivered != nil {
		t.Fatalf("after compile: %s %+v", g.SyncState, g.Delivered)
	}
	dm := func(who uuid.UUID) ledger.Meta { return meta(person(who), f.scope(who), policy.ViaCLI) }

	// Deliveries are acknowledged against the run's drift hash.
	if _, err := f.l.Apply(ctx, &ledger.RecordDelivery{Meta: dm(zz), Target: tg.ID, Compile: run.Ref, SHA256: sha("something else")}); err == nil {
		t.Error("a delivery with the wrong hash was accepted")
	}
	del := f.apply(&ledger.RecordDelivery{Meta: dm(zz), Target: tg.ID, Compile: run.Ref, SHA256: run.DriftSHA256})
	if del.Target.SyncState != ledger.SyncInSync || del.Target.Delivered.Compile != run.Ref || del.Compile.Status != ledger.CompileDelivered {
		t.Fatalf("after delivery: %+v %+v", del.Target, del.Compile)
	}
	if rc := del.Receipts[0]; rc.Action != ledger.ActionDelivered || rc.ObjectKind != ledger.ObjectCompile || rc.StreamVersion != 2 || rc.Via != policy.ViaCLI {
		t.Errorf("delivery receipt = %+v", rc)
	}
	if again := f.apply(&ledger.RecordDelivery{Meta: dm(zz), Target: tg.ID, Compile: run.ID.String(), SHA256: run.DriftSHA256}); !again.Unchanged {
		t.Error("a second acknowledgement of the same run wrote")
	}

	observe := func(content string, changes ...ledger.DriftChange) ledger.Result {
		t.Helper()
		return f.apply(&ledger.RecordObservation{Meta: dm(zz), Target: tg.ID, Path: "AGENTS.md", ObservedSHA256: sha(content),
			ObserverKind: ledger.ObserverDevice, ObserverID: "zz-laptop", ArtifactKey: "obs/" + sha(content), Bytes: len(content),
			BaseCompileID: &run.ID, BaseSHA256: run.DriftSHA256, Changes: ledger.ChangeSet{Changes: changes}})
	}
	// The file as delivered is no drift: nothing is written.
	if same := observe("compiled v1"); !same.Unchanged || len(same.Receipts) != 0 {
		t.Errorf("observing the delivered file: %+v", same)
	}
	decisions := "Decisions"
	first := observe("edited once", ledger.DriftChange{Kind: ledger.ChangeNew, Text: "First draft.", Line: 4, Section: &decisions})
	if first.Target.SyncState != ledger.SyncDrifted || first.Target.OpenDrift != 1 || len(first.Observations) != 1 {
		t.Fatalf("first observation: %+v", first.Target)
	}
	if rc := first.Receipts[0]; rc.Action != ledger.ActionObserved || rc.Source == nil || rc.Source.Ref != "AGENTS.md" {
		t.Errorf("observation receipt = %+v", rc)
	}
	// A newer edit of the same file supersedes the older one.
	second := observe("edited twice",
		ledger.DriftChange{Kind: ledger.ChangeEdit, Ref: pnpm.Ref, Refs: []string{pnpm.Ref}, OldText: pnpm.Statement,
			NewText: "pnpm workspaces only; never npm.", OldLine: 6, NewLine: 6},
		ledger.DriftChange{Kind: ledger.ChangeNew, Text: "Prefer named exports in React components.", Line: 8, Section: &decisions,
			Paths: []string{"packages/web/**"}},
		ledger.DriftChange{Kind: ledger.ChangeNew, Text: "AWS key AKIAIOSFODNN7EXAMPLE goes here.", Line: 9},
		ledger.DriftChange{Kind: ledger.ChangeRemove, Ref: receipt.Ref, Refs: []string{receipt.Ref}, OldText: receipt.Statement, OldLine: 7},
	)
	obs := second.Observations[0]
	if second.Target.OpenDrift != 1 {
		t.Errorf("open drift = %d, want 1", second.Target.OpenDrift)
	}
	var dismissed ledger.ObservationStatus
	if err := f.pool.QueryRow(ctx, `SELECT status FROM v2.target_observations WHERE id = $1`, first.Observations[0].ID).Scan(&dismissed); err != nil || dismissed != ledger.ObservationDismissed {
		t.Errorf("first observation is %s (%v), want dismissed", dismissed, err)
	}

	// Only a person pulls.
	if res := f.apply(&ledger.ResolveDrift{Meta: meta(agentFor(policy.AutonomyWrite), f.scope(zz), policy.ViaMCP), Target: tg.ID, Mode: ledger.DriftPull}); res.Policy.Code != policy.CodeTargetsByPerson {
		t.Errorf("an agent pulls: %s", res.Policy.Code)
	}
	// JY pulls ZZ's edit: the proposals are ZZ's (the person on the device).
	pm := meta(person(jy), f.scope(jy), policy.ViaWeb)
	pm.Reason = "keep the hand edit"
	pull := f.apply(&ledger.ResolveDrift{Meta: pm, Target: tg.ID, Mode: ledger.DriftPull})
	if pull.Receipts[0].Action != ledger.ActionPulled || *pull.Receipts[0].ActorID != jy {
		t.Errorf("pull receipt = %+v", pull.Receipts[0])
	}
	if len(pull.Proposals) != 2 {
		t.Fatalf("proposals = %+v, want 2 (the secret is skipped)", pull.Proposals)
	}
	edit, added := pull.Proposals[0], pull.Proposals[1]
	if edit.Statement != "pnpm workspaces only; never npm." || edit.Lifecycle != lifecycle.Proposed || edit.Section != pnpm.Section {
		t.Errorf("edit proposal = %+v", edit)
	}
	if added.Section != ledger.SectionDecisions || added.Kind != ledger.KindDecision || string(added.Applies) != `{"paths": ["packages/web/**"]}` {
		t.Errorf("new proposal = %+v %s", added, added.Applies)
	}
	for _, p := range pull.Proposals {
		if len(p.Sources) != 1 || p.Sources[0].Kind != ledger.SourceFile || !strings.HasPrefix(p.Sources[0].Ref, "AGENTS.md:") ||
			p.Trust != policy.TrustRepository || p.Sources[0].External {
			t.Errorf("%s source = %+v trust %s", p.Ref, p.Sources, p.Trust)
		}
	}
	if edit.Sources[0].Ref != "AGENTS.md:6" || added.Sources[0].Ref != "AGENTS.md:8" {
		t.Errorf("sources = %s, %s", edit.Sources[0].Ref, added.Sources[0].Ref)
	}
	for _, rc := range pull.Receipts[1:] {
		if rc.Action != ledger.ActionProposed || rc.ActorKind != policy.ActorPerson || *rc.ActorID != zz || rc.Via != policy.ViaCLI {
			t.Errorf("proposal receipt = %+v; the person on the device proposes", rc)
		}
	}
	var supersedes int
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM v2.memory_links WHERE kind = 'supersedes' AND from_memory_id = $1 AND to_memory_id = $2`, edit.ID, pnpm.ID).Scan(&supersedes)
	if supersedes != 1 {
		t.Error("the edit proposal doesn't supersede the memory it edits")
	}
	got := pull.Observations[0]
	if got.Status != ledger.ObservationPulled || got.Resolution == nil || len(got.Resolution.Changes) != 4 {
		t.Fatalf("resolution = %+v", got.Resolution)
	}
	outcomes := []string{}
	for _, c := range got.Resolution.Changes {
		outcomes = append(outcomes, c.Kind+":"+c.Outcome+":"+c.Reason)
	}
	want := []string{"edit:proposed:", "new:proposed:", "new:skipped:secret_detected", "remove:review:"}
	if !slices.Equal(outcomes, want) {
		t.Errorf("outcomes = %v, want %v", outcomes, want)
	}
	if got.Resolution.Changes[3].Ref != receipt.Ref {
		t.Errorf("the removal names %s, want %s", got.Resolution.Changes[3].Ref, receipt.Ref)
	}
	// The removed line is a request, never a forget.
	if m, _ := f.l.GetMemory(ctx, f.scope(zz), receipt.ID.String()); m.Lifecycle != lifecycle.Kept {
		t.Errorf("the removed line's memory is %s", m.Lifecycle)
	}
	// The hand-edited file is the baseline now; the target is in sync with it.
	pt := pull.Target
	if pt.SyncState != ledger.SyncInSync || pt.OpenDrift != 0 || pt.Delivered.SHA256 != obs.ObservedSHA ||
		pt.Delivered.Files[0].Observation == nil || *pt.Delivered.Files[0].Observation != obs.ID || pt.Delivered.Compile != run.Ref {
		t.Errorf("after pull: %+v %+v", pt, pt.Delivered)
	}
	// Replaying the pull writes nothing more.
	replay := f.apply(&ledger.ResolveDrift{Meta: pm, Target: tg.ID, Mode: ledger.DriftPull})
	if !replay.Replayed || len(replay.Proposals) != 2 || len(replay.Observations) != 1 {
		t.Errorf("replay = %+v", replay)
	}
	if _, err := f.l.Apply(ctx, &ledger.ResolveDrift{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: tg.ID, Mode: ledger.DriftPull}); !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("pulling with nothing to pull: %v", err)
	}

	// Overwrite: the edit is accepted as what Memax may write over, and
	// the target recompiles and is delivered again.
	third := observe("edited a third time", ledger.DriftChange{Kind: ledger.ChangeNew, Text: "Nope.", Line: 3})
	if third.Target.SyncState != ledger.SyncDrifted {
		t.Fatalf("third observation: %s", third.Target.SyncState)
	}
	ow := f.apply(&ledger.ResolveDrift{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: tg.ID, Mode: ledger.DriftOverwrite})
	ot := ow.Target
	if ot.SyncState != ledger.SyncCompiling || ot.DirtyGen != third.Target.DirtyGen+1 || ot.Delivered.CompileID != nil ||
		ot.Delivered.SHA256 != sha("edited a third time") || ow.Observations[0].Status != ledger.ObservationOverwritten || len(ow.Proposals) != 0 {
		t.Errorf("after overwrite: %+v %+v", ot, ot.Delivered)
	}
	if f.jobs(tg.ID) != 1 {
		t.Errorf("overwrite queued %d compile jobs, want 1", f.jobs(tg.ID))
	}
	run2 := f.compiled(f.get(zz, tg.ID), "compiled v2")
	f.apply(&ledger.RecordDelivery{Meta: dm(zz), Target: tg.ID, Compile: run2.Ref, SHA256: run2.DriftSHA256})
	if g := f.get(zz, tg.ID); g.SyncState != ledger.SyncInSync || g.Delivered.Compile != run2.Ref {
		t.Errorf("after redelivery: %s %+v", g.SyncState, g.Delivered)
	}

	// Stop: the target is off, and later edits aren't drift.
	observe("edited a fourth time", ledger.DriftChange{Kind: ledger.ChangeNew, Text: "Again.", Line: 3})
	if res := f.apply(&ledger.ResolveDrift{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: tg.ID, Mode: ledger.DriftStop}); res.Target.SyncState != ledger.SyncOff ||
		res.Observations[0].Status != ledger.ObservationStopped || res.Receipts[0].Action != ledger.ActionStopped {
		t.Errorf("after stop: %+v", res.Target)
	}
	if quiet := observe("edited a fifth time"); !quiet.Unchanged {
		t.Error("an observation of a stopped target was recorded")
	}
	if _, err := f.l.Apply(ctx, &ledger.RecordObservation{Meta: dm(zz), Target: tg.ID, Path: "CLAUDE.md", ObservedSHA256: sha("x"),
		ObserverKind: ledger.ObserverDevice, ObserverID: "d", ArtifactKey: "k", Changes: ledger.ChangeSet{}}); err == nil {
		t.Error("an observation of a file the target doesn't write was accepted")
	}
}

// A hand edit seen on GitHub, with nobody known, is proposed by the
// repository and quarantined as external.
func TestPullFromTheRepositoryIsExternal(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.brief(zz, space, 0, nil)
	tg := f.target(zz, space, ledger.TargetAgentsMD)
	f.compiled(tg, "v1")
	scope, _ := f.l.SpaceScope(ctx, space)
	f.apply(&ledger.RecordObservation{
		Meta:   ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorRepository}, Scope: scope, Via: policy.ViaGitHub, IdempotencyKey: uuid.NewString()},
		Target: tg.ID, Path: "AGENTS.md", ObservedSHA256: sha("pushed"), ObserverKind: ledger.ObserverGitHub,
		ObserverID: "MemaxLabs/memax", Commit: "a41e9c2", ArtifactKey: "k", Bytes: 6,
		Changes: ledger.ChangeSet{Changes: []ledger.DriftChange{{Kind: ledger.ChangeNew, Text: "Run tests with pnpm test -- --run.", Line: 5}}},
	})
	pull := f.apply(&ledger.ResolveDrift{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: tg.ID, Mode: ledger.DriftPull})
	if len(pull.Proposals) != 1 {
		t.Fatalf("proposals = %+v", pull.Proposals)
	}
	p := pull.Proposals[0]
	if p.Trust != policy.TrustExternal || !p.Sources[0].External || p.Lifecycle != lifecycle.Proposed {
		t.Errorf("a repository edit = trust %s, external %v", p.Trust, p.Sources[0].External)
	}
	if rc := pull.Receipts[1]; rc.ActorKind != policy.ActorRepository || rc.ActorID != nil || rc.Via != policy.ViaGitHub {
		t.Errorf("proposal receipt = %+v", rc)
	}
	var loc string
	_ = f.pool.QueryRow(ctx, `SELECT locator->>'commit' FROM v2.sources WHERE id = $1`, p.Sources[0].ID).Scan(&loc)
	if loc != "a41e9c2" {
		t.Errorf("source locator commit = %q", loc)
	}
	// A person can still keep it on the web, not from the CLI.
	if res := f.apply(&ledger.Keep{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), Memory: p.Ref}); res.Policy.Code != policy.CodeExternalNeedsReview {
		t.Errorf("keeping a repository edit from the CLI: %s", res.Policy.Code)
	}
}

// Rule 1 on the new tables: no row without a same-transaction receipt,
// except the compile path's bookkeeping on targets.
func TestCompileTablesRequireReceipts(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	b := f.brief(zz, space, 0, nil)
	tg := f.target(zz, space, ledger.TargetAgentsMD)
	run := f.compiled(tg, "v1")
	f.apply(&ledger.RecordObservation{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), Target: tg.ID, Path: "AGENTS.md",
		ObservedSHA256: sha("edit"), ObserverKind: ledger.ObserverDevice, ObserverID: "d", ArtifactKey: "k", Changes: ledger.ChangeSet{}})
	tenant := b.TenantID
	old := b.ReceiptID

	cases := []struct {
		name string
		sql  string
		args []any
		want string
	}{
		{"brief points at another version", `UPDATE v2.briefs SET current_version = 1, stream_version = 9 WHERE space_id = $1`, []any{space}, "MXR01"},
		{"a version with an old receipt", `INSERT INTO v2.brief_versions (id, brief_id, version, tenant_id, space_id, seq, parent_version, title, structure, receipt_id, last_receipt_id)
			VALUES (gen_random_uuid(), $1, 2, $2, $3, 99, 1, 'smuggled', '{"sections": []}', $4, $4)`, []any{b.ID, tenant, space, old}, "MXR01"},
		{"target settings", `UPDATE v2.targets SET settings = '{"include": "kept_only"}' WHERE id = $1`, []any{tg.ID}, "MXR01"},
		{"target leaves drifted", `UPDATE v2.targets SET sync_state = 'in_sync' WHERE id = $1`, []any{tg.ID}, "MXR01"},
		{"pretend delivered", `UPDATE v2.targets SET delivered_compile_id = $2 WHERE id = $1`, []any{tg.ID, run.ID}, "MXR01"},
		{"a run delivered", `UPDATE v2.compile_runs SET status = 'delivered', delivered_at = now() WHERE id = $1`, []any{run.ID}, "MXR01"},
		{"an observation dismissed", `UPDATE v2.target_observations SET status = 'dismissed', resolved_at = now() WHERE target_id = $1`, []any{tg.ID}, "MXR01"},
		{"bookkeeping: a bump", `UPDATE v2.targets SET dirty_gen = dirty_gen + 1, dirty_at = now(), updated_at = now() WHERE id = $1`, []any{tg.ID}, ""},
	}
	for _, c := range cases {
		var stmtErr error
		err := f.rawAs(space, tenant, func(tx pgx.Tx) error {
			_, stmtErr = tx.Exec(ctx, c.sql, c.args...)
			return stmtErr
		})
		if stmtErr != nil {
			t.Errorf("%s: statement failed (%v); the check must be deferred", c.name, stmtErr)
			continue
		}
		if got := sqlstate(err); got != c.want {
			t.Errorf("%s: commit %v (%q), want %q", c.name, err, got, c.want)
		}
	}
	// Bookkeeping on a target that isn't drifted: compiled_gen and the
	// compiling ↔ pending transitions need no receipt.
	f.apply(&ledger.ResolveDrift{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Target: tg.ID, Mode: ledger.DriftOverwrite})
	err := f.rawAs(space, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE v2.targets SET compiled_gen = dirty_gen, sync_state = 'pending_delivery' WHERE id = $1 AND sync_state = 'compiling'`, tg.ID)
		return err
	})
	if err != nil {
		t.Errorf("settling bookkeeping: %v", err)
	}
	// …but pending_delivery → in_sync is a delivery, and needs its receipt.
	err = f.rawAs(space, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE v2.targets SET sync_state = 'in_sync' WHERE id = $1`, tg.ID)
		return err
	})
	if sqlstate(err) != "MXR01" {
		t.Errorf("pending → in sync without a receipt: %v", err)
	}
}

// Rule 13 on the new tables, and the sweeper's one cross-space read.
func TestCompileTablesAreIsolated(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	mine := f.space(zz, policy.SpaceProject, "memax-v2")
	theirs := f.space(jy, policy.SpaceProject, "side")
	for _, s := range []struct{ owner, space uuid.UUID }{{zz, mine}, {jy, theirs}} {
		f.brief(s.owner, s.space, 0, nil)
		tg := f.target(s.owner, s.space, ledger.TargetAgentsMD)
		f.compiled(tg, "v1")
		f.apply(&ledger.RecordObservation{Meta: meta(person(s.owner), f.scope(s.owner), policy.ViaCLI), Target: tg.ID, Path: "AGENTS.md",
			ObservedSHA256: sha("edit"), ObserverKind: ledger.ObserverDevice, ObserverID: "d", ArtifactKey: "k", Changes: ledger.ChangeSet{}})
		f.target(s.owner, s.space, ledger.TargetClaudeMD) // dirty: never compiled
	}
	jyTarget := f.count(`SELECT count(*) FROM v2.targets WHERE space_id = $1`, theirs)
	if jyTarget != 2 {
		t.Fatal("fixture")
	}
	tables := []string{"v2.briefs", "v2.brief_versions", "v2.targets", "v2.compile_runs", "v2.target_observations"}
	scopeA := f.scope(zz)
	for _, tbl := range tables {
		var n, all int
		all = f.count(`SELECT count(*) FROM ` + tbl)
		own := f.count(`SELECT count(*) FROM `+tbl+` WHERE space_id = $1`, mine)
		if err := f.l.Read(ctx, scopeA, func(tx pgx.Tx) error { return tx.QueryRow(ctx, `SELECT count(*) FROM `+tbl).Scan(&n) }); err != nil {
			t.Fatal(err)
		}
		if n != own || own == 0 || own == all {
			t.Errorf("%s: scope A sees %d rows, owns %d of %d", tbl, n, own, all)
		}
		if err := f.l.Read(ctx, ledger.Scope{}, func(tx pgx.Tx) error { return tx.QueryRow(ctx, `SELECT count(*) FROM `+tbl).Scan(&n) }); err != nil || n != 0 {
			t.Errorf("%s: no scope sees %d rows (%v)", tbl, n, err)
		}
	}
	// The ledger API agrees.
	jyTargets, _ := f.l.ListTargets(ctx, f.scope(jy), theirs)
	if _, err := f.l.GetTarget(ctx, scopeA, jyTargets[0].ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("GetTarget across spaces: %v", err)
	}
	if _, err := f.l.ListTargets(ctx, scopeA, theirs); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("ListTargets across spaces: %v", err)
	}
	if _, err := f.l.Apply(ctx, &ledger.RequestCompile{Meta: meta(person(zz), scopeA, policy.ViaWeb), Target: jyTargets[0].ID}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("compiling another space's target: %v", err)
	}
	// The sweeper sees every dirty target's id, in every space; nothing
	// else sees across spaces, and the policy works only inside the
	// function.
	refs, err := f.l.DirtyTargets(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Errorf("dirty targets = %+v, want one per space", refs)
	}
	var leaked int
	err = f.rawAs(mine, uuid.Nil, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM v2.targets WHERE space_id = $1`, theirs).Scan(&leaked)
	})
	if err != nil || leaked != 0 {
		t.Errorf("scope A sees %d of B's targets (%v)", leaked, err)
	}
}

// The new vocabulary is defined in Go and in SQL; they must agree.
func TestCompileVocabularyMatchesSQL(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	check := func(constraint string) []string {
		var def string
		if err := f.pool.QueryRow(context.Background(), `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, constraint).Scan(&def); err != nil {
			t.Fatalf("%s: %v", constraint, err)
		}
		var out []string
		for _, part := range strings.Split(def, "'")[1:] {
			if part != "" && !strings.ContainsAny(part, " (),:") {
				out = append(out, part)
			}
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	same := func(name string, sql []string, code []string) {
		t.Helper()
		c := slices.Clone(code)
		slices.Sort(c)
		if !slices.Equal(sql, c) {
			t.Errorf("%s: SQL %v, Go %v", name, sql, c)
		}
	}
	strs := func(vs ...string) []string { return vs }
	var kinds, deliveries, states, statuses, obs []string
	for _, k := range ledger.TargetKinds {
		kinds = append(kinds, string(k))
	}
	for _, d := range ledger.Deliveries {
		deliveries = append(deliveries, string(d))
	}
	for _, s := range ledger.SyncStates {
		states = append(states, string(s))
	}
	for _, s := range ledger.CompileStatuses {
		statuses = append(statuses, string(s))
	}
	for _, s := range ledger.ObservationStatuses {
		obs = append(obs, string(s))
	}
	same("targets.kind", check("targets_kind_check"), kinds)
	same("targets.delivery", check("targets_delivery_check"), deliveries)
	same("targets.sync_state", check("targets_sync_state_check"), states)
	same("compile_runs.status", check("compile_runs_status_check"), statuses)
	same("target_observations.status", check("target_observations_status_check"), obs)
	same("observer_kind", check("target_observations_observer_check"), strs(ledger.ObserverDevice, ledger.ObserverGitHub))
	actions := []string{}
	for _, a := range []ledger.Action{ledger.ActionRevised, ledger.ActionConfigured, ledger.ActionRequested, ledger.ActionCompiled,
		ledger.ActionDelivered, ledger.ActionObserved, ledger.ActionPulled, ledger.ActionOverwritten, ledger.ActionStopped} {
		actions = append(actions, string(a))
	}
	sqlActions := check("receipts_action_check")
	for _, a := range actions {
		if !slices.Contains(sqlActions, a) {
			t.Errorf("receipts_action_check lacks %q", a)
		}
	}
	// The path rule is the same in both.
	for _, p := range []string{"AGENTS.md", "packages/web/AGENTS.md", ".cursor/rules", "a/../b", "/abs", "a//b", "a/./b", "trailing/", "with space", ""} {
		var sqlOK bool
		if err := f.pool.QueryRow(context.Background(), `SELECT v2.repo_path_valid($1)`, p).Scan(&sqlOK); err != nil {
			t.Fatal(err)
		}
		if sqlOK != ledger.ValidPath(p) {
			t.Errorf("path %q: SQL %v, Go %v", p, sqlOK, ledger.ValidPath(p))
		}
	}
	if !slices.Equal(ledger.CompileUniqueStates, []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStatePending,
		rivertype.JobStateRunning, rivertype.JobStateScheduled, rivertype.JobStateRetryable}) {
		t.Errorf("compile_target is unique over %v; §5.7 says every unfinished state", ledger.CompileUniqueStates)
	}
}

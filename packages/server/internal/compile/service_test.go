package compile_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// A run the ledger doesn't record leaves no artifact: nothing would ever
// reach it, since Forget re-renders only the artifacts a run records. Here
// the target stays locked (as by a Forget purging the space) until the
// recording gives up with ErrBusy.
func TestRunLeavesNoArtifactWhenItsRunIsntRecorded(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{})
	s := f.seed()
	tg := s.targets[ledger.TargetAgentsMD]
	ctx := context.Background()
	var locker pgx.Tx
	f.fake.OnCompile = func(*compile.Input) {
		var err error
		if locker, err = f.pool.Begin(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := locker.Exec(ctx, `SELECT 1 FROM v2.targets WHERE id = $1 FOR UPDATE`, tg.ID); err != nil {
			t.Fatal(err)
		}
	}
	_, err := f.svc.Run(ctx, ledger.CompileTargetArgs{TargetID: tg.ID, SpaceID: s.space}, compile.RunOptions{NoWait: true})
	if locker != nil {
		_ = locker.Rollback(ctx)
	}
	if !errors.Is(err, ledger.ErrBusy) {
		t.Fatalf("Run = %v, want ErrBusy (the target stayed locked)", err)
	}
	if n := len(f.runs(tg)); n != 0 {
		t.Errorf("%d runs recorded", n)
	}
	if keys := f.store.Keys(); len(keys) != 0 {
		t.Errorf("the unrecorded run's artifact is still stored: %v", keys)
	}
}

// seed writes a small space: a decision, a convention, an open question,
// a proposal, a kept external memory, a Brief and the four default
// targets.
type seeded struct {
	owner, space            uuid.UUID
	river, pnpm, open, prop *ledger.Memory
	external                *ledger.Memory
	brief                   *ledger.Brief
	targets                 map[ledger.TargetKind]*ledger.Target
}

func (f *fixture) seed() *seeded {
	f.t.Helper()
	s := &seeded{owner: f.user("zz"), targets: map[ledger.TargetKind]*ledger.Target{}}
	s.space = f.space(s.owner, "memax-v2")
	s.river = f.remember(s.owner, s.space, "Background jobs run on River.", ledger.SectionDecisions)
	s.pnpm = f.remember(s.owner, s.space, "pnpm workspaces only.", ledger.SectionConventions)
	s.open = f.remember(s.owner, s.space, "Fly.io or Railway is undecided.", ledger.SectionOpenQuestion)
	s.prop = f.apply(&ledger.Propose{Meta: ledger.Meta{
		Actor: ledger.Actor{Kind: policy.ActorAgent, ID: uuid.New(), Agent: "codex", Autonomy: policy.AutonomyPropose},
		Scope: f.scope(s.owner), Via: policy.ViaMCP, IdempotencyKey: uuid.NewString()},
		NewMemory: ledger.NewMemory{SpaceID: s.space, Statement: "Proposals never compile.", Section: ledger.SectionConventions}}).Memory
	s.external = f.apply(&ledger.Remember{Meta: f.meta(s.owner, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: s.space, Statement: "A web page said so.", Section: ledger.SectionConventions,
		Sources: []ledger.SourceInput{{Kind: ledger.SourceURL, Ref: "example.com", URI: "https://example.com"}}}}).Memory
	s.brief = f.apply(&ledger.ReviseBrief{Meta: f.meta(s.owner, policy.ViaWeb), SpaceID: s.space, Title: "Memax V2 engineering brief",
		Sections: []ledger.BriefSection{
			{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{{Ref: s.river.Ref}}},
			{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{{Ref: s.pnpm.Ref}}},
			{Key: "open", Heading: "Open", Items: []ledger.BriefItem{{Ref: s.open.Ref}}},
		}}).Brief
	for _, k := range ledger.DefaultTargetKinds {
		s.targets[k] = f.target(s.owner, s.space, k)
	}
	return s
}

func TestRunCompilesRecordsAndStores(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{cfg: compile.Config{AppBaseURL: "https://app.example.com/"}})
	s := f.seed()
	agents := s.targets[ledger.TargetAgentsMD]
	if out := f.run(agents); out != compile.Done {
		t.Fatalf("outcome %v", out)
	}

	in := f.fake.LastInput()
	if in.Version != compile.ContractVersion || in.Brief.ID != s.brief.Ref || in.Space.Kind != "project" ||
		!strings.HasPrefix(in.Space.Slug, "memax-v2-") || in.Space.URL != "https://app.example.com/"+in.Space.Slug+"/brief" {
		t.Errorf("input header = %+v / %+v", in.Brief, in.Space)
	}
	byRef := map[string]compile.InputMemory{}
	for _, m := range in.Memories {
		byRef[m.Ref] = m
	}
	if _, ok := byRef[s.prop.Ref]; ok {
		t.Error("a proposal reached the compiler")
	}
	if _, ok := byRef[s.external.Ref]; ok {
		t.Error("a quarantined (external) memory reached the compiler")
	}
	if m := byRef[s.open.Ref]; m.State != "open" || m.Section != "open" {
		t.Errorf("open question = %+v, want state and section open", m)
	}
	if m := byRef[s.river.Ref]; m.State != "kept" || m.Section != "decisions" || m.ReadScore != 0 || m.Trust != "person" {
		t.Errorf("kept memory = %+v", m)
	}
	if len(in.Targets) != 1 || in.Targets[0].Kind != "agents_md" || in.Targets[0].Path != "AGENTS.md" ||
		in.Targets[0].Scoped != "inline" || in.Targets[0].SizeBudget != ledger.DefaultSizeBudget || in.Targets[0].UserOwned {
		t.Errorf("target settings = %+v", in.Targets)
	}

	tg := f.get(s.owner, agents.ID)
	if tg.SyncState != ledger.SyncPendingDelivery || tg.CompiledGen != tg.DirtyGen || tg.LastCompile == nil {
		t.Fatalf("after compile: %s %d/%d", tg.SyncState, tg.CompiledGen, tg.DirtyGen)
	}
	run := tg.LastCompile
	if run.Status != ledger.CompileCompiled || run.Brief != s.brief.Ref || len(run.Files) != 1 || run.Files[0].Path != "AGENTS.md" ||
		run.DriftSHA256 != run.Files[0].DriftSHA256 || run.Bytes == 0 || run.Generation != tg.DirtyGen {
		t.Errorf("run = %+v", run)
	}
	if !strings.Contains(strings.Join(run.Refs, ","), s.river.Ref) {
		t.Errorf("run refs = %v", run.Refs)
	}
	p, err := f.svc.Preview(context.Background(), f.scope(s.owner), agents.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Compile.Ref != run.Ref || len(p.Files) != 1 || !strings.Contains(p.Files[0].Content, "Background jobs run on River. ["+s.river.Ref+"]") ||
		!strings.Contains(p.Files[0].Content, run.Ref) {
		t.Errorf("preview = %+v", p)
	}
	// The artifact is where the run says.
	key := "v2/spaces/" + s.space.String() + "/targets/" + agents.ID.String() + "/compiles/" + run.Ref + ".json"
	if _, err := f.store.Get(context.Background(), key); err != nil {
		t.Errorf("artifact %s: %v", key, err)
	}

	// The ChatGPT copy-out is in sync once compiled.
	f.run(s.targets[ledger.TargetChatGPT])
	gpt := f.get(s.owner, s.targets[ledger.TargetChatGPT].ID)
	if gpt.SyncState != ledger.SyncInSync || gpt.LastCompile.Status != ledger.CompileDelivered || gpt.Delivered == nil ||
		gpt.Delivered.Compile != gpt.LastCompile.Ref || gpt.LastCompile.Files[0].Label == "" {
		t.Errorf("chatgpt = %s %+v %+v", gpt.SyncState, gpt.LastCompile, gpt.Delivered)
	}
	// Nothing to do for a target that's compiled.
	calls := f.fake.Calls.Load()
	if out := f.run(agents); out != compile.Done || f.fake.Calls.Load() != calls {
		t.Errorf("a compiled target compiled again (%v)", out)
	}
}

// "Skip targets whose output hash is unchanged" (§5.7): no new run when
// the input or the output is the same.
func TestRunSkipsUnchangedOutput(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{})
	s := f.seed()
	tg := s.targets[ledger.TargetAgentsMD]
	f.run(tg)
	before := len(f.runs(tg))

	// Same input: no compiler call, no run.
	f.apply(&ledger.RequestCompile{Meta: f.meta(s.owner, policy.ViaWeb), Target: tg.ID})
	calls := f.fake.Calls.Load()
	f.run(tg)
	if n := len(f.runs(tg)); n != before || f.fake.Calls.Load() != calls {
		t.Errorf("same input: runs %d → %d, calls %d → %d", before, n, calls, f.fake.Calls.Load())
	}
	if g := f.get(s.owner, tg.ID); g.SyncState != ledger.SyncPendingDelivery || g.CompiledGen != g.DirtyGen {
		t.Errorf("after an unchanged compile: %s %d/%d", g.SyncState, g.CompiledGen, g.DirtyGen)
	}
	// A different input with the same bytes (the fake doesn't render
	// stale_after): one identity compile, no run.
	future := time.Now().Add(365 * 24 * time.Hour)
	f.apply(&ledger.Remember{Meta: f.meta(s.owner, policy.ViaWeb), NewMemory: ledger.NewMemory{SpaceID: s.space,
		Statement: "Decided long ago.", Section: ledger.SectionDecisions, StaleAfter: &future}})
	if n := len(f.runs(tg)); n != before {
		t.Fatal("fixture")
	}
	f.run(tg)
	runs := f.runs(tg)
	if len(runs) != before+1 {
		t.Fatalf("a new kept memory should compile: %d runs", len(runs))
	}
	// A real change compiles a new run with a new ID.
	if runs[0].Ref == runs[1].Ref || runs[0].InputSHA256 == runs[1].InputSHA256 {
		t.Errorf("runs = %s %s", runs[0].Ref, runs[1].Ref)
	}
}

// §5.7 step 3: a target dirtied while compiling isn't recorded; the job
// snoozes and compiles the new generation. After the snooze cap, the run
// is recorded behind, and the sweeper comes back.
func TestRunSnoozesWhenTheTargetMovesOn(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{cfg: compile.Config{SnoozeCap: 2}})
	s := f.seed()
	tg := s.targets[ledger.TargetAgentsMD]
	f.fake.OnCompile = func(*compile.Input) {
		f.apply(&ledger.RequestCompile{Meta: f.meta(s.owner, policy.ViaWeb), Target: tg.ID})
	}
	ctx := context.Background()
	args := ledger.CompileTargetArgs{TargetID: tg.ID, SpaceID: s.space}
	out, err := f.svc.Run(ctx, args, compile.RunOptions{NoWait: true})
	if err != nil || out != compile.Snooze {
		t.Fatalf("Run = %v %v, want Snooze", out, err)
	}
	if n := len(f.runs(tg)); n != 0 {
		t.Errorf("a run behind its target was recorded (%d runs)", n)
	}
	if keys := f.store.Keys(); len(keys) != 0 {
		t.Errorf("the superseded artifact is still stored: %v", keys)
	}
	out, err = f.svc.Run(ctx, args, compile.RunOptions{NoWait: true, Snoozes: 2})
	if err != nil || out != compile.Done {
		t.Fatalf("Run at the cap = %v %v, want Done", out, err)
	}
	g := f.get(s.owner, tg.ID)
	if len(f.runs(tg)) != 1 || g.CompiledGen >= g.DirtyGen || g.SyncState != ledger.SyncCompiling {
		t.Errorf("at the cap: runs %d, gen %d/%d %s", len(f.runs(tg)), g.CompiledGen, g.DirtyGen, g.SyncState)
	}
	refs, err := f.l.DirtyTargets(ctx, 10)
	if err != nil || !slices.ContainsFunc(refs, func(r ledger.TargetRef) bool { return r.TargetID == tg.ID }) {
		t.Errorf("the sweeper doesn't see the target behind: %+v %v", refs, err)
	}
	f.fake.OnCompile = nil
	if out := f.run(tg); out != compile.Done {
		t.Fatal(out)
	}
	if g := f.get(s.owner, tg.ID); g.CompiledGen != g.DirtyGen || g.SyncState != ledger.SyncPendingDelivery {
		t.Errorf("after catching up: %d/%d %s", g.CompiledGen, g.DirtyGen, g.SyncState)
	}
}

func TestRunRecordsRefusedInputAndRetriesOutages(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{})
	s := f.seed()
	tg := s.targets[ledger.TargetClaudeMD]
	ctx := context.Background()
	args := ledger.CompileTargetArgs{TargetID: tg.ID, SpaceID: s.space}

	f.fake.Fail(errors.New("connection refused"))
	if _, err := f.svc.Run(ctx, args, compile.RunOptions{NoWait: true}); err == nil {
		t.Fatal("an outage must fail the job so River retries it")
	}
	if n := len(f.runs(tg)); n != 0 {
		t.Errorf("an outage recorded %d runs", n)
	}
	f.fake.Fail(&compile.InputError{Message: "invalid", Issues: []compile.Issue{{InstancePath: "/memories/0/scope/paths/0", Message: "bad glob"}}})
	if out, err := f.svc.Run(ctx, args, compile.RunOptions{NoWait: true}); err != nil || out != compile.Done {
		t.Fatalf("a refused input: %v %v", out, err)
	}
	runs := f.runs(tg)
	if len(runs) != 1 || runs[0].Status != ledger.CompileFailed || !strings.Contains(runs[0].Error, "/memories/0/scope/paths/0") {
		t.Fatalf("runs = %+v", runs)
	}
	if g := f.get(s.owner, tg.ID); g.CompiledGen != g.DirtyGen || g.SyncState == ledger.SyncCompiling {
		t.Errorf("a failed compile leaves %d/%d %s", g.CompiledGen, g.DirtyGen, g.SyncState)
	}
	if len(f.store.Keys()) != 0 {
		t.Error("a failed run stored an artifact")
	}
}

func TestQuietWindow(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{cfg: compile.Config{Quiet: 400 * time.Millisecond, QuietCap: 1500 * time.Millisecond}})
	s := f.seed()
	tg := s.targets[ledger.TargetAgentsMD]
	ctx := context.Background()
	args := ledger.CompileTargetArgs{TargetID: tg.ID, SpaceID: s.space}

	// Waits for the quiet window after the last change. The window is
	// measured from the target's last change (dirty_at, set while
	// seeding), not from when the job starts: under load the seed's tail
	// can eat into the window, so timing from start was flaky.
	lastChange := f.get(s.owner, tg.ID).DirtyAt
	start := time.Now()
	if _, err := f.svc.Run(ctx, args, compile.RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if quiet := time.Since(lastChange); quiet < 400*time.Millisecond-20*time.Millisecond {
		t.Errorf("compiled %v after the last change, want at least the 400ms quiet window", quiet)
	}
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Errorf("quiet run took %v, want no more than the 1.5s cap", d)
	}
	// Under constant change, waits no longer than the cap.
	f.apply(&ledger.RequestCompile{Meta: f.meta(s.owner, policy.ViaWeb), Target: tg.ID})
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Millisecond):
				_, _ = f.l.Apply(ctx, &ledger.RequestCompile{Meta: f.meta(s.owner, policy.ViaWeb), Target: tg.ID})
			}
		}
	}()
	start = time.Now()
	_, err := f.svc.Run(ctx, args, compile.RunOptions{})
	close(stop)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 1400*time.Millisecond || d > 3*time.Second {
		t.Errorf("busy run took %v, want about the 1.5s cap", d)
	}
}

func TestObserveAndDrift(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{})
	s := f.seed()
	tg := s.targets[ledger.TargetAgentsMD]
	f.run(tg)
	ctx := context.Background()
	p, err := f.svc.Preview(ctx, f.scope(s.owner), tg.ID)
	if err != nil {
		t.Fatal(err)
	}
	file := p.Files[0]
	f.apply(&ledger.RecordDelivery{Meta: f.meta(s.owner, policy.ViaCLI), Target: tg.ID, Compile: p.Compile.Ref, SHA256: p.Compile.DriftSHA256})

	observe := func(content string) ledger.Result {
		t.Helper()
		res, err := f.svc.Observe(ctx, f.meta(s.owner, policy.ViaCLI), tg.ID, compile.ObserveInput{
			Path: "AGENTS.md", Content: content, ObserverKind: ledger.ObserverDevice, ObserverID: "zz-laptop"})
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		return res
	}
	// As delivered (CRLF aside): no drift, nothing stored.
	if res := observe(strings.ReplaceAll(file.Content, "\n", "\r\n")); !res.Unchanged || len(f.store.Keys()) != 1 {
		t.Errorf("observing the delivered file: unchanged=%v, %d keys", res.Unchanged, len(f.store.Keys()))
	}
	edited := strings.Replace(file.Content, "pnpm workspaces only.", "pnpm workspaces only, never npm.", 1) + "- Prefer named exports.\n"
	res := observe(edited)
	if res.Unchanged || res.Target.SyncState != ledger.SyncDrifted || len(res.Observations) != 1 {
		t.Fatalf("a hand edit: %+v", res.Target)
	}
	obs := res.Observations[0]
	if obs.BaseCompile != p.Compile.Ref || obs.BaseSHA256 != file.DriftSHA256 || len(obs.Changes.Changes) != 2 || obs.Bytes != len(edited) {
		t.Errorf("observation = %+v", obs)
	}
	raw, err := f.store.Get(ctx, "v2/spaces/"+s.space.String()+"/targets/"+tg.ID.String()+"/observed/"+obs.ObservedSHA)
	if err != nil {
		t.Fatalf("observed content not stored: %v", err)
	}
	stored, _ := io.ReadAll(raw.Body)
	if string(stored) != edited {
		t.Error("the stored observation isn't the observed content")
	}
	d, err := f.svc.Drift(ctx, f.scope(s.owner), tg.ID)
	if err != nil || len(d.Items) != 1 || d.Items[0].Compiled != file.Content || d.Items[0].Observed != edited ||
		d.Items[0].BaseCompile == nil || d.Items[0].BaseCompile.Ref != p.Compile.Ref {
		t.Fatalf("drift = %+v %v", d, err)
	}

	// Pull: the proposals come back; the edited file is the baseline, so
	// observing it again is no drift, and a further edit is compared to it.
	pull := f.apply(&ledger.ResolveDrift{Meta: f.meta(s.owner, policy.ViaWeb), Target: tg.ID, Mode: ledger.DriftPull})
	if len(pull.Proposals) != 2 || pull.Target.SyncState != ledger.SyncInSync {
		t.Fatalf("pull = %d proposals, %s", len(pull.Proposals), pull.Target.SyncState)
	}
	if again := observe(edited); !again.Unchanged {
		t.Error("the pulled file is the baseline now; observing it isn't drift")
	}
	second := observe(edited + "- One more.\n")
	if len(second.Observations[0].Changes.Changes) != 1 || second.Observations[0].BaseCompile != "" ||
		second.Observations[0].BaseSHA256 != obs.ObservedSHA {
		t.Errorf("a second edit is compared to the pulled file: %+v", second.Observations[0])
	}
	// Validation before anything is stored.
	keys := len(f.store.Keys())
	for _, in := range []compile.ObserveInput{
		{Path: "CLAUDE.md", Content: "x", ObserverKind: ledger.ObserverDevice, ObserverID: "d"},
		{Path: "../AGENTS.md", Content: "x", ObserverKind: ledger.ObserverDevice, ObserverID: "d"},
		{Path: "AGENTS.md", Content: "bad\x00", ObserverKind: ledger.ObserverDevice, ObserverID: "d"},
	} {
		if _, err := f.svc.Observe(ctx, f.meta(s.owner, policy.ViaCLI), tg.ID, in); !errors.Is(err, ledger.ErrInvalid) {
			t.Errorf("observe %q: %v, want invalid", in.Path, err)
		}
	}
	if len(f.store.Keys()) != keys {
		t.Error("a refused observation stored content")
	}
}

// A user-owned CLAUDE.md compiles around the file's current content: the
// accepted hand edit.
func TestUserOwnedShimGetsItsCurrentContent(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{})
	s := f.seed()
	tg := s.targets[ledger.TargetClaudeMD]
	f.apply(&ledger.ConfigureTarget{Meta: f.meta(s.owner, policy.ViaWeb), Target: tg.ID,
		Settings: &ledger.TargetSettingsInput{UserOwned: ptr(true)}})
	f.run(tg)
	in := f.fake.LastInput()
	if !in.Targets[0].UserOwned || in.Targets[0].Current == nil || *in.Targets[0].Current != "" {
		t.Fatalf("first compile of a user-owned shim: %+v", in.Targets[0])
	}
	ctx := context.Background()
	mine := "# My notes\n\nKeep this.\n"
	if _, err := f.svc.Observe(ctx, f.meta(s.owner, policy.ViaCLI), tg.ID, compile.ObserveInput{
		Path: "CLAUDE.md", Content: mine, ObserverKind: ledger.ObserverDevice, ObserverID: "d"}); err != nil {
		t.Fatal(err)
	}
	f.apply(&ledger.ResolveDrift{Meta: f.meta(s.owner, policy.ViaWeb), Target: tg.ID, Mode: ledger.DriftOverwrite})
	f.run(tg)
	if cur := f.fake.LastInput().Targets[0].Current; cur == nil || *cur != mine {
		t.Errorf("current = %v, want the person's file", cur)
	}
}

func TestBuildInputDropsWhatTheCompilerRefuses(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{})
	s := f.seed()
	scoped := f.apply(&ledger.Remember{Meta: f.meta(s.owner, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: s.space, Statement: "Web components use the Ledger primitives.", Section: ledger.SectionConventions,
		Applies: json.RawMessage(`{"paths": ["packages/web/**", "../escape/**", "!negated/**", "has space/**"], "agents": ["cursor", "Not An Agent"]}`)}}).Memory
	tg := s.targets[ledger.TargetCursorMDC]
	f.run(tg)
	var m compile.InputMemory
	for _, x := range f.fake.LastInput().Memories {
		if x.Ref == scoped.Ref {
			m = x
		}
	}
	if m.Scope == nil || len(m.Scope.Paths) != 1 || m.Scope.Paths[0] != "packages/web/**" || len(m.Scope.Agents) != 1 {
		t.Errorf("scope = %+v", m.Scope)
	}
	run := f.get(s.owner, tg.ID).LastCompile
	n := 0
	for _, w := range run.Warnings {
		if w.Code == "not_compiled" && w.Ref == scoped.Ref {
			n++
		}
	}
	if n != 3 {
		t.Errorf("warnings = %+v, want one per dropped glob", run.Warnings)
	}
	if len(run.Files) != 1 || !strings.HasPrefix(run.Files[0].Path, ".cursor/rules/") {
		t.Errorf("cursor outputs = %+v", run.Files)
	}
}

// A decision that a newer one superseded stays kept, and stops compiling.
func TestSupersededDecisionsDontCompile(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{})
	s := f.seed()
	old := f.apply(&ledger.Remember{Meta: f.meta(s.owner, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: s.space, Statement: "Deploy the v2 API to Railway.", Section: ledger.SectionDecisions, Kind: ledger.KindDecision,
		Decision: &ledger.DecisionFields{Area: "deploy target"}}}).Memory
	tg := s.targets[ledger.TargetAgentsMD]
	f.run(tg)
	if !slices.ContainsFunc(f.fake.LastInput().Memories, func(m compile.InputMemory) bool { return m.Ref == old.Ref }) {
		t.Fatal("the decision in force didn't compile")
	}
	// An explicit change, linked by the judge (as Memax), then kept.
	p := f.apply(&ledger.Propose{Meta: f.meta(s.owner, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: s.space, Statement: "We moved the v2 API from Railway to Fly.io.", Section: ledger.SectionDecisions,
		Kind: ledger.KindDecision, Decision: &ledger.DecisionFields{Area: "deploy target"}}}).Memory
	scope, err := f.l.SpaceScope(context.Background(), s.space)
	if err != nil {
		t.Fatal(err)
	}
	f.apply(&ledger.RecordVerdict{Meta: ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: scope, Via: policy.ViaSystem,
		IdempotencyKey: "judge-test"}, Memory: p.ID, Version: 1, Mode: ledger.JudgeProposal, Outcome: ledger.OutcomeSuperseding,
		Target: old.ID, Verdict: ledger.Verdict{Stage: ledger.StageLLM, Relation: ledger.RelationUpdates, Related: old.ID}})
	f.apply(&ledger.Keep{Meta: f.meta(s.owner, policy.ViaWeb), Memory: p.Ref})
	f.run(tg)
	refs := []string{}
	for _, m := range f.fake.LastInput().Memories {
		refs = append(refs, m.Ref)
	}
	if slices.Contains(refs, old.Ref) || !slices.Contains(refs, p.Ref) {
		t.Errorf("compiled %v: want %s and not %s", refs, p.Ref, old.Ref)
	}
}

// Rule 11: a Write agent's write kept at once compiles; when the judge
// finds it contradicts a decision in force, it goes back to Review and
// leaves every compiled file. Past the return window it stays, marked in
// conflict.
func TestReturnedWriteLeavesTheCompiledFiles(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOpts{})
	s := f.seed()
	tg := s.targets[ledger.TargetAgentsMD]
	river := f.apply(&ledger.Remember{Meta: f.meta(s.owner, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: s.space, Statement: "Queue background work on River, not Temporal.", Section: ledger.SectionDecisions,
		Kind: ledger.KindDecision, Decision: &ledger.DecisionFields{Area: "job queue"}}}).Memory
	agent := ledger.Actor{Kind: policy.ActorAgent, ID: uuid.New(), Agent: "codex", Autonomy: policy.AutonomyWrite}
	write := func(statement string) *ledger.Memory {
		res := f.apply(&ledger.Propose{Meta: ledger.Meta{Actor: agent, Scope: f.scope(s.owner), Via: policy.ViaMCP, IdempotencyKey: uuid.NewString()},
			NewMemory: ledger.NewMemory{SpaceID: s.space, Statement: statement, Section: ledger.SectionConventions}})
		if res.Memory.Lifecycle != "kept" {
			t.Fatalf("write %q = %s", statement, res.Memory.Lifecycle)
		}
		return res.Memory
	}
	compiled := func() map[string]compile.InputMemory {
		f.run(tg)
		out := map[string]compile.InputMemory{}
		for _, m := range f.fake.LastInput().Memories {
			out[m.Ref] = m
		}
		return out
	}
	flag := func(l *ledger.Ledger, m *ledger.Memory) ledger.Action {
		scope, err := l.SpaceScope(context.Background(), s.space)
		if err != nil {
			t.Fatal(err)
		}
		res, err := l.Apply(context.Background(), &ledger.RecordVerdict{
			Meta:   ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: scope, Via: policy.ViaSystem, IdempotencyKey: uuid.NewString()},
			Memory: m.ID, Version: m.Version, Mode: ledger.JudgeKept, Outcome: ledger.OutcomeFlagged, Target: river.ID,
			Verdict: ledger.Verdict{Stage: ledger.StageLLM, Relation: ledger.RelationContradicts, Related: river.ID}})
		if err != nil {
			t.Fatal(err)
		}
		return res.Receipts[0].Action
	}

	soon := write("Long workflows use Temporal.")
	late := write("Nightly exports run on Temporal.")
	before := compiled()
	if _, ok := before[soon.Ref]; !ok {
		t.Fatalf("the agent's write didn't compile: %v", before)
	}
	if a := flag(f.l, soon); a != ledger.ActionReturned {
		t.Fatalf("verdict = %s, want returned", a)
	}
	pastWindow := ledger.New(f.pool, ledger.WithLogger(quiet), ledger.WithReturnWindow(0))
	if a := flag(pastWindow, late); a != ledger.ActionFlagged {
		t.Fatalf("late verdict = %s, want flagged", a)
	}
	after := compiled()
	if _, ok := after[soon.Ref]; ok {
		t.Errorf("%s is back in Review and still compiles", soon.Ref)
	}
	if m, ok := after[late.Ref]; !ok || !slices.Contains(m.Flags, "conflict") {
		t.Errorf("%s past the window = %+v, want compiled in conflict", late.Ref, m)
	}
	if _, ok := after[river.Ref]; !ok {
		t.Errorf("the decision in force stopped compiling")
	}
}

func ptr[T any](v T) *T { return &v }

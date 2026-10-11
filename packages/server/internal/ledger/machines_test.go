package ledger_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The personal layer's machines (personal-layer design §5): LinkMachine,
// UnlinkMachine, RenameMachine, home targets through ConfigureTarget, who
// may do any of it, and what the database refuses on its own.

// homeFiles are the files a machine with Claude Code and Codex approves.
func homeFiles() []ledger.MachineFile {
	return []ledger.MachineFile{
		{Kind: ledger.TargetClaudeUser},
		{Kind: ledger.TargetCodexUser},
		{Kind: ledger.TargetAgentsMD},
	}
}

func (f *fixture) link(owner, space uuid.UUID, device uuid.UUID, name string, files ...ledger.MachineFile) ledger.Result {
	f.t.Helper()
	if files == nil {
		files = homeFiles()
	}
	return f.apply(&ledger.LinkMachine{Meta: meta(person(owner), f.scope(owner), policy.ViaCLI), SpaceID: space,
		DeviceKey: device, MachineName: name, Platform: "darwin", SessionID: uuid.New(), Files: files})
}

func refusedCode(t *testing.T, res ledger.Result, code string) {
	t.Helper()
	if res.Outcome != ledger.OutcomeRefused || res.Policy.Code != code {
		t.Fatalf("outcome %s (%s), want refused %s", res.Outcome, res.Policy.Code, code)
	}
}

func TestLinkMachineWritesItAllInOneCommand(t *testing.T) {
	f := newCompileFixture(t)
	zz := f.user("zz")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	kept := f.remember(zz, personal, "Answer in British English.")

	res := f.link(zz, personal, uuid.New(), "work-mbp")
	if res.Outcome != ledger.OutcomeApplied || res.Machine == nil {
		t.Fatalf("link: %s %s", res.Outcome, res.Policy.Message)
	}
	m := res.Machine
	if m.Label != "machine-1" || m.Name != "work-mbp" || m.Platform != "darwin" || !m.Linked || m.Version != 1 {
		t.Errorf("machine = %+v", m)
	}
	// The receipts: the machine linked, the first Brief, one per file.
	var actions []string
	for _, rc := range res.Receipts {
		actions = append(actions, rc.ObjectKind+":"+string(rc.Action))
		if rc.ObjectKind == ledger.ObjectMachine && rc.ObjectRef != "machine-1" {
			t.Errorf("a machine receipt names it %q; never its name", rc.ObjectRef)
		}
	}
	if !slices.Equal(actions, []string{"machine:linked", "brief:revised", "target:configured", "target:configured", "target:configured"}) {
		t.Errorf("receipts = %v", actions)
	}
	if res.Receipts[1].Reason != ledger.FirstPersonalBriefReason {
		t.Errorf("Brief reason = %q", res.Receipts[1].Reason)
	}

	// Its targets: one per file, each at home on this machine, compiling.
	byKind := map[ledger.TargetKind]ledger.Target{}
	for _, tg := range res.Targets {
		byKind[tg.Kind] = tg
		if tg.Root != ledger.RootHome || tg.Machine == nil || tg.Machine.ID != m.ID || tg.Machine.Label != "machine-1" ||
			tg.SyncState != ledger.SyncCompiling || tg.Settings.SizeBudget != ledger.DefaultHomeSizeBudget {
			t.Errorf("target %s = %+v", tg.Kind, tg)
		}
		if f.jobs(tg.ID) != 1 {
			t.Errorf("%s has %d compile jobs, want 1", tg.Kind, f.jobs(tg.ID))
		}
	}
	rules, codex, canonical := byKind[ledger.TargetClaudeUser], byKind[ledger.TargetCodexUser], byKind[ledger.TargetAgentsMD]
	if rules.Path != ".claude/rules/memax-personal.md" || rules.Label != "~/.claude/rules/memax-personal.md" ||
		rules.Settings.UserOwned || !slices.Equal(rules.Settings.ReadBy, []string{"claude-code"}) {
		t.Errorf("Claude Code's = %+v", rules)
	}
	if codex.Path != ".codex/AGENTS.md" || !codex.Settings.UserOwned || !slices.Equal(codex.Settings.ReadBy, []string{"codex"}) {
		t.Errorf("Codex's = %+v", codex)
	}
	if canonical.Path != ledger.HomeCanonicalPath || canonical.Settings.UserOwned || canonical.Settings.ReadBy != nil {
		t.Errorf("the canonical = %+v", canonical)
	}
	// What the command answered is what a read finds.
	got, err := f.l.ListMachineTargets(context.Background(), f.scope(zz), personal, m.ID)
	if err != nil || len(got) != 3 {
		t.Fatalf("ListMachineTargets: %d, %v", len(got), err)
	}
	for i := range got {
		a, b := got[i], res.Targets[i]
		if a.ID != b.ID || a.Path != b.Path || a.Label != b.Label || !a.DirtyAt.Equal(b.DirtyAt) ||
			!a.CreatedAt.Equal(b.CreatedAt) || a.Settings.UserOwned != b.Settings.UserOwned || *a.Machine != *b.Machine {
			t.Errorf("read %+v, answered %+v", a, b)
		}
	}

	// The first Brief: four empty sections, so every kept fact compiles at
	// the end of its own, most-read first.
	brief, err := f.l.GetBrief(context.Background(), f.scope(zz), personal)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, s := range brief.Sections {
		keys = append(keys, s.Key)
	}
	if brief.Title != "Personal" || !slices.Equal(keys, []string{"preferences", "conventions", "decisions", "open"}) {
		t.Errorf("first Brief = %q %v", brief.Title, keys)
	}
	_ = kept

	// A second machine gets its own targets and the next label; the Brief
	// stays.
	two := f.link(zz, personal, uuid.New(), "home-linux")
	if two.Machine.Label != "machine-2" || len(two.Targets) != 3 || two.Receipts[1].ObjectKind != ledger.ObjectTarget {
		t.Errorf("second machine = %+v, receipts %d", two.Machine, len(two.Receipts))
	}
	all, err := f.l.ListTargets(context.Background(), f.scope(zz), personal)
	if err != nil || len(all) != 6 {
		t.Fatalf("ListTargets: %d, %v", len(all), err)
	}

	// A Keep in Personal recompiles every home file on every machine.
	before := f.jobs(rules.ID)
	f.remember(zz, personal, "Prefer small commits.")
	for _, tg := range all {
		if cur := f.get(zz, tg.ID); cur.DirtyGen != 2 {
			t.Errorf("%s on %s: dirty_gen %d after a Keep", tg.Kind, tg.Machine.Label, cur.DirtyGen)
		}
	}
	if f.jobs(rules.ID) != before {
		t.Errorf("the job is unique while one waits: %d, want %d", f.jobs(rules.ID), before)
	}

	// Every row the link wrote has a receipt about it.
	if n := f.count(`SELECT count(*) FROM v2.machines m WHERE m.space_id = $1 AND NOT EXISTS (
		SELECT 1 FROM v2.receipts r WHERE r.id = m.created_receipt_id AND r.object_id = m.id)`, personal); n != 0 {
		t.Errorf("%d machines without their receipt", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.targets t WHERE t.space_id = $1 AND NOT EXISTS (
		SELECT 1 FROM v2.receipts r WHERE r.id = t.created_receipt_id AND r.object_id = t.id)`, personal); n != 0 {
		t.Errorf("%d targets without their receipt", n)
	}
}

func TestLinkingAgainConverges(t *testing.T) {
	f := newCompileFixture(t)
	zz := f.user("zz")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	device := uuid.New()
	first := f.link(zz, personal, device, "work-mbp")

	// Again, the same: nothing to write.
	cmd := &ledger.LinkMachine{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), SpaceID: personal, DeviceKey: device,
		MachineName: "work-mbp", Platform: "darwin", SessionID: *first.Machine.SessionID, Files: homeFiles()}
	again := f.apply(cmd)
	if !again.Unchanged || len(again.Receipts) != 0 || again.Machine.ID != first.Machine.ID || len(again.Targets) != 3 {
		t.Errorf("relink = unchanged %v, %d receipts, %+v", again.Unchanged, len(again.Receipts), again.Machine)
	}

	// Codex moved to AGENTS.override.md: the new file compiles, the old one
	// stops, so Codex reads one copy.
	files := homeFiles()
	files[1].Path = ".codex/AGENTS.override.md"
	moved := f.link(zz, personal, device, "work-mbp", files...)
	var states []string
	for _, tg := range moved.Targets {
		if tg.Kind == ledger.TargetCodexUser {
			states = append(states, tg.Path+"="+string(tg.SyncState))
		}
	}
	if !slices.Equal(states, []string{".codex/AGENTS.md=off", ".codex/AGENTS.override.md=compiling"}) {
		t.Errorf("Codex's files = %v", states)
	}

	// Unlinked, then linked again: the same machine, its files back on.
	un := f.apply(&ledger.UnlinkMachine{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Machine: first.Machine.ID})
	if un.Machine.Linked || un.Receipts[0].Action != ledger.ActionUnlinked {
		t.Fatalf("unlink = %+v", un.Machine)
	}
	for _, tg := range un.Targets {
		if tg.SyncState != ledger.SyncOff {
			t.Errorf("%s after unlink: %s", tg.Path, tg.SyncState)
		}
	}
	back := f.link(zz, personal, device, "work-mbp")
	// Linked (1), linked again from a new session with the move (2),
	// unlinked (3), linked again (4).
	if back.Machine.ID != first.Machine.ID || !back.Machine.Linked || back.Machine.Version != 4 {
		t.Errorf("relinked = %+v", back.Machine)
	}
	on := 0
	for _, tg := range back.Targets {
		if tg.SyncState == ledger.SyncCompiling {
			on++
		}
	}
	if on != 3 {
		t.Errorf("%d files back on, want the 3 approved", on)
	}
}

func TestUnlinkStopsEveryFileAndIsIdempotent(t *testing.T) {
	f := newCompileFixture(t)
	zz := f.user("zz")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	res := f.link(zz, personal, uuid.New(), "work-mbp")

	un := f.apply(&ledger.UnlinkMachine{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), Machine: res.Machine.ID})
	var actions []string
	for _, rc := range un.Receipts {
		actions = append(actions, rc.ObjectKind+":"+string(rc.Action))
	}
	if !slices.Equal(actions, []string{"machine:unlinked", "target:stopped", "target:stopped", "target:stopped"}) {
		t.Errorf("receipts = %v", actions)
	}
	if un.Machine.UnlinkedAt == nil {
		t.Error("unlinked_at isn't set")
	}
	again := f.apply(&ledger.UnlinkMachine{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI), Machine: res.Machine.ID})
	if !again.Unchanged || len(again.Receipts) != 0 {
		t.Errorf("a second unlink wrote %d receipts", len(again.Receipts))
	}
	// A file can't come back on, or be added, while its machine is unlinked.
	on := true
	_, err := f.l.Apply(context.Background(), &ledger.ConfigureTarget{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb),
		Target: res.Targets[0].ID, Enabled: &on})
	var se *ledger.TargetStateError
	if !errors.As(err, &se) {
		t.Errorf("turning a file on, on an unlinked machine: %v", err)
	}
	_, err = f.l.Apply(context.Background(), &ledger.ConfigureTarget{Meta: meta(person(zz), f.scope(zz), policy.ViaCLI),
		SpaceID: personal, Kind: ledger.TargetCodexUser, Root: ledger.RootHome, Machine: res.Machine.ID,
		Path: ptr(".codex/AGENTS.override.md")})
	if !errors.As(err, &se) {
		t.Errorf("adding a file to an unlinked machine: %v", err)
	}
}

func TestRenameMachineKeepsTheNameOutOfReceipts(t *testing.T) {
	f := newCompileFixture(t)
	zz := f.user("zz")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	res := f.link(zz, personal, uuid.New(), "work-mbp")
	renamed := f.apply(&ledger.RenameMachine{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Machine: res.Machine.ID,
		ExpectedVersion: 1, NewName: "Ziyang's laptop"})
	if renamed.Machine.Name != "Ziyang's laptop" || renamed.Machine.Version != 2 || renamed.Receipts[0].ObjectRef != "machine-1" {
		t.Errorf("renamed = %+v, receipt %+v", renamed.Machine, renamed.Receipts[0])
	}
	for _, tg := range renamed.Targets {
		if tg.Machine.Name != "Ziyang's laptop" {
			t.Errorf("%s still says %q", tg.Path, tg.Machine.Name)
		}
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND (object_ref LIKE '%laptop%' OR reason LIKE '%laptop%')`, personal); n != 0 {
		t.Errorf("%d receipts name the machine", n)
	}
	_, err := f.l.Apply(context.Background(), &ledger.RenameMachine{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb),
		Machine: res.Machine.ID, ExpectedVersion: 1, NewName: "stale"})
	if !errors.Is(err, ledger.ErrEditClash) {
		t.Errorf("a rename from an old version: %v", err)
	}
}

func TestHomeTargetsThroughConfigureTarget(t *testing.T) {
	f := newCompileFixture(t)
	zz := f.user("zz")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	res := f.link(zz, personal, uuid.New(), "work-mbp", ledger.MachineFile{Kind: ledger.TargetClaudeUser})
	add := func(c *ledger.ConfigureTarget) (ledger.Result, error) {
		c.Meta = meta(person(zz), f.scope(zz), policy.ViaCLI)
		c.SpaceID = personal
		return f.l.Apply(context.Background(), c)
	}
	// memax connect codex on a linked machine adds Codex's file.
	got, err := add(&ledger.ConfigureTarget{Kind: ledger.TargetCodexUser, Root: ledger.RootHome, Machine: res.Machine.ID})
	if err != nil || got.Target == nil || got.Target.Path != ".codex/AGENTS.md" || !got.Target.Settings.UserOwned ||
		got.Target.Settings.SizeBudget != ledger.DefaultHomeSizeBudget || f.jobs(got.Target.ID) != 1 {
		t.Fatalf("add Codex's file: %v %+v", err, got.Target)
	}
	for name, c := range map[string]*ledger.ConfigureTarget{
		"a home kind in a repository":   {Kind: ledger.TargetCodexUser},
		"a repository kind at home":     {Kind: ledger.TargetClaudeMD, Root: ledger.RootHome, Machine: res.Machine.ID},
		"a project folder in ~":         {Kind: ledger.TargetCodexUser, Root: ledger.RootHome, Machine: res.Machine.ID, Path: ptr("code/app/AGENTS.md")},
		"the canonical elsewhere":       {Kind: ledger.TargetAgentsMD, Root: ledger.RootHome, Machine: res.Machine.ID, Path: ptr(".codex/AGENTS.md")},
		"a home file delivered over PR": {Kind: ledger.TargetAgentsMD, Root: ledger.RootHome, Machine: res.Machine.ID, Delivery: ptr(ledger.DeliveryPR)},
		"a budget over 16 KiB":          {Kind: ledger.TargetAgentsMD, Root: ledger.RootHome, Machine: res.Machine.ID, Settings: &ledger.TargetSettingsInput{SizeBudget: ptr(20000)}},
		"user_owned against the path":   {Kind: ledger.TargetAgentsMD, Root: ledger.RootHome, Machine: res.Machine.ID, Settings: &ledger.TargetSettingsInput{UserOwned: ptr(true)}},
		"a machine of no one":           {Kind: ledger.TargetAgentsMD, Root: ledger.RootHome, Machine: uuid.New()},
	} {
		if r, err := add(c); err == nil {
			t.Errorf("%s: applied (%s)", name, r.Outcome)
		}
	}
	// Changing a home file's settings keeps it at home.
	budget := 2048
	changed, err := f.l.Apply(context.Background(), &ledger.ConfigureTarget{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb),
		Target: got.Target.ID, Settings: &ledger.TargetSettingsInput{SizeBudget: &budget}})
	if err != nil || changed.Target.Settings.SizeBudget != 2048 || changed.Target.Root != ledger.RootHome {
		t.Errorf("change a home file's budget: %v %+v", err, changed.Target)
	}
}

func TestMachinesAreTheOwnersAlone(t *testing.T) {
	f := newCompileFixture(t)
	zz, jy := f.user("zz"), f.user("jy")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	project := f.space(zz, policy.SpaceProject, "memax-v2")
	jyPersonal := f.space(jy, policy.SpacePersonal, "JY")
	device := uuid.New()
	res := f.link(zz, personal, device, "work-mbp")

	cmd := func(a ledger.Actor, space uuid.UUID) *ledger.LinkMachine {
		return &ledger.LinkMachine{Meta: meta(a, f.scope(zz), policy.ViaMCP), SpaceID: space, DeviceKey: uuid.New(),
			MachineName: "x", Platform: "linux", Files: homeFiles()}
	}
	// Agents and API keys never link a machine or write a home file.
	agent := agentFor(policy.AutonomyWrite)
	refusedCode(t, f.apply(cmd(agent, personal)), policy.CodeMachinesByPerson)
	key := person(zz)
	key.Credential = policy.CredentialAPIKey
	refusedCode(t, f.apply(cmd(key, personal)), policy.CodeMachinesByPerson)
	// Only a personal space is linked to machines.
	refusedCode(t, f.apply(cmd(person(zz), project)), policy.CodeMachinesPersonal)

	// Another person can't see, link into, unlink or rename my machines,
	// even claiming my device id: their link goes to their own space.
	ctx := context.Background()
	if _, err := f.l.ListMachines(ctx, f.scope(jy), personal); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another person lists my machines: %v", err)
	}
	if _, err := f.l.ListMachineTargets(ctx, f.scope(jy), personal, res.Machine.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another person reads my machine's targets: %v", err)
	}
	if _, err := f.l.Apply(ctx, &ledger.LinkMachine{Meta: meta(person(jy), f.scope(jy), policy.ViaCLI), SpaceID: personal,
		DeviceKey: device, MachineName: "mine now", Platform: "linux", Files: homeFiles()}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another person links into my space: %v", err)
	}
	theirs, err := f.l.Apply(ctx, &ledger.LinkMachine{Meta: meta(person(jy), f.scope(jy), policy.ViaCLI), SpaceID: jyPersonal,
		DeviceKey: device, MachineName: "same device", Platform: "linux", Files: homeFiles()})
	if err != nil || theirs.Machine.ID == res.Machine.ID || theirs.Machine.SpaceID != jyPersonal {
		t.Errorf("my device id in their own space: %v %+v", err, theirs.Machine)
	}
	for _, c := range []ledger.Command{
		&ledger.UnlinkMachine{Meta: meta(person(jy), f.scope(jy), policy.ViaWeb), Machine: res.Machine.ID},
		&ledger.RenameMachine{Meta: meta(person(jy), f.scope(jy), policy.ViaWeb), Machine: res.Machine.ID, NewName: "x"},
	} {
		if _, err := f.l.Apply(ctx, c); !errors.Is(err, ledger.ErrNotFound) {
			t.Errorf("%s of my machine by another person: %v", c.Name(), err)
		}
	}
	// The database agrees: scoped to their space, my machine isn't there,
	// and it can't be written into mine.
	if err := f.rawAs(jyPersonal, jy, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v2.machines WHERE id = $1`, res.Machine.ID).Scan(&n); err != nil || n != 0 {
			t.Errorf("RLS: %d of my machines visible from theirs, %v", n, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A home file's deliveries and hand edits are its person's to report.
	agentMeta := meta(agentFor(policy.AutonomyWrite), f.scope(zz), policy.ViaMCP)
	rep := f.apply(&ledger.RecordDelivery{Meta: agentMeta, Target: res.Targets[0].ID, Compile: "C-0001", SHA256: sha("x")})
	refusedCode(t, rep, policy.CodeMachinesByPerson)
}

func TestTheDatabaseHoldsTheMachineRules(t *testing.T) {
	f := newCompileFixture(t)
	zz := f.user("zz")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	project := f.space(zz, policy.SpaceProject, "memax-v2")
	res := f.link(zz, personal, uuid.New(), "work-mbp")
	ctx := context.Background()
	sqlstate := func(err error) string {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			return pg.Code
		}
		return ""
	}

	// A machine changes only with a receipt about it, except when its
	// daemon was last seen.
	if err := f.rawAs(personal, zz, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE v2.machines SET last_seen_at = now() WHERE id = $1`, res.Machine.ID)
		return err
	}); err != nil {
		t.Errorf("last_seen_at is bookkeeping: %v", err)
	}
	if err := f.rawAs(personal, zz, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE v2.machines SET name = 'renamed' WHERE id = $1`, res.Machine.ID)
		return err
	}); sqlstate(err) != "MXR01" {
		t.Errorf("a rename without a receipt: %v", err)
	}
	// memax_v2 can't delete a machine, nor change its identity.
	if err := f.rawAs(personal, zz, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM v2.machines WHERE id = $1`, res.Machine.ID)
		return err
	}); sqlstate(err) != "42501" {
		t.Errorf("delete a machine: %v", err)
	}
	if err := f.rawAs(personal, zz, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE v2.machines SET device_key = gen_random_uuid() WHERE id = $1`, res.Machine.ID)
		return err
	}); sqlstate(err) != "42501" {
		t.Errorf("change a machine's device: %v", err)
	}

	// A home target lives only in a personal space, on its machine, at a
	// home path.
	insert := func(space uuid.UUID, kind, root, path string, machine *uuid.UUID) error {
		return f.rawAs(space, zz, func(tx pgx.Tx) error {
			id := uuid.Must(uuid.NewV7())
			var rid uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id, via, occurred_at, stream_id, stream_version)
				VALUES (gen_random_uuid(), $1, $2, 'target', $3, 'x', 'configured', 'person', $1, 'cli', now(), $3, 1) RETURNING id`,
				zz, space, id).Scan(&rid); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO v2.targets (id, tenant_id, space_id, kind, root, machine_id, path, delivery, sync_state, created_receipt_id, last_receipt_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7, 'local', 'compiling', $8, $8)`, id, zz, space, kind, root, machine, path, rid)
			return err
		})
	}
	mid := res.Machine.ID
	for name, c := range map[string]struct {
		space         uuid.UUID
		kind, root, p string
		machine       *uuid.UUID
		code          string
	}{
		"in a project space":     {project, "codex_user", "home", ".codex/AGENTS.override.md", &mid, "23503"}, // the machine isn't the project's
		"a home kind at repo":    {personal, "codex_user", "repo", ".codex/AGENTS.override.md", nil, "23514"},
		"home without a machine": {personal, "codex_user", "home", ".codex/AGENTS.override.md", nil, "23514"},
		"a project folder in ~":  {personal, "codex_user", "home", "code/AGENTS.md", &mid, "23514"},
		"a repository kind":      {personal, "claude_md", "home", ".claude/CLAUDE.md", &mid, "23514"},
	} {
		if err := insert(c.space, c.kind, c.root, c.p, c.machine); sqlstate(err) != c.code {
			t.Errorf("%s: %v, want SQLSTATE %s", name, err, c.code)
		}
	}
	// A machine itself only in a personal space.
	if err := f.rawAs(project, zz, func(tx pgx.Tx) error {
		id := uuid.Must(uuid.NewV7())
		var rid uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id, via, occurred_at, stream_id, stream_version)
			VALUES (gen_random_uuid(), $1, $2, 'machine', $3, 'machine-1', 'linked', 'person', $1, 'cli', now(), $3, 1) RETURNING id`,
			zz, project, id).Scan(&rid); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO v2.machines (id, tenant_id, space_id, seq, device_key, name, platform, linked_at, created_receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, 1, gen_random_uuid(), 'x', 'linux', now(), $4, $4)`, id, zz, project, rid)
		return err
	}); sqlstate(err) != "MXH01" {
		t.Errorf("a machine in a project space: %v", err)
	}
}

// The Go rule and the database's agree on every home path.
func TestHomePathRulesMatchTheDatabase(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		kind ledger.TargetKind
		path string
	}{
		{ledger.TargetAgentsMD, ".memax/personal/AGENTS.md"},
		{ledger.TargetAgentsMD, "AGENTS.md"},
		{ledger.TargetAgentsMD, ".memax/AGENTS.md"},
		{ledger.TargetClaudeUser, ".claude/rules/memax-personal.md"},
		{ledger.TargetClaudeUser, ".config/claude/rules/memax-personal.md"},
		{ledger.TargetClaudeUser, ".claude/CLAUDE.md"},
		{ledger.TargetClaudeUser, ".claude/rules/notes.md"},
		{ledger.TargetClaudeUser, "rules/memax-personal.md"},
		{ledger.TargetClaudeUser, ".memax/CLAUDE.md"},
		{ledger.TargetClaudeUser, "CLAUDE.md"},
		{ledger.TargetCodexUser, ".codex/AGENTS.md"},
		{ledger.TargetCodexUser, ".codex/AGENTS.override.md"},
		{ledger.TargetCodexUser, ".codex-work/AGENTS.md"},
		{ledger.TargetCodexUser, "code/app/AGENTS.md"},
		{ledger.TargetCodexUser, ".memax/personal/AGENTS.md"},
		{ledger.TargetCodexUser, ".codex/../AGENTS.md"},
		{ledger.TargetCodexUser, "/home/zz/.codex/AGENTS.md"},
		{ledger.TargetCodexUser, ".codex/AGENTS.local.md"},
		{ledger.TargetClaudeMD, ".claude/CLAUDE.md"},
		{ledger.TargetCursorMDC, ".cursor/rules"},
		{"gemini_user", ".gemini/AGENTS.md"},
	}
	for _, c := range cases {
		var db bool
		if err := f.pool.QueryRow(context.Background(), `SELECT v2.home_path_valid($1, $2)`, string(c.kind), c.path).Scan(&db); err != nil {
			t.Fatal(err)
		}
		if goRule := ledger.ValidHomePath(c.kind, c.path); goRule != db {
			t.Errorf("%s %s: Go says %v, the database %v", c.kind, c.path, goRule, db)
		}
	}
}

func TestForgettingPersonalWholeUnlinksEveryMachine(t *testing.T) {
	f := newCompileFixture(t)
	zz := f.user("zz")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	f.remember(zz, personal, "Answer in British English.")
	one := f.link(zz, personal, uuid.New(), "work-mbp")
	two := f.link(zz, personal, uuid.New(), "home-linux")
	forget := meta(person(zz), f.scope(zz), policy.ViaWeb)
	forget.Reason = "Answer in British English is wrong now."
	f.apply(&ledger.ForgetSpace{Meta: forget, SpaceID: personal})
	// The unlink receipts come after the space's reasons were redacted, so
	// they carry none: a Forget's reason may hold the words it took.
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND reason IS NOT NULL`, personal); n != 0 {
		t.Errorf("%d receipts keep a reason after the space's Forget", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND object_kind = 'machine' AND action = 'unlinked'`, personal); n != 2 {
		t.Errorf("%d machines unlinked, want 2", n)
	}
	for _, m := range []*ledger.Machine{one.Machine, two.Machine} {
		got, err := f.l.GetMachine(context.Background(), f.scope(zz), m.ID)
		if err != nil || got.Linked {
			t.Errorf("%s after the space's Forget: %+v, %v", m.Label, got, err)
		}
	}
	if n := f.count(`SELECT count(*) FROM v2.targets WHERE space_id = $1 AND sync_state <> 'off'`, personal); n != 0 {
		t.Errorf("%d home files still compile", n)
	}
}

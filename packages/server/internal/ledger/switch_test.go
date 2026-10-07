package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb/netsim"
)

// v1World is a realistic V1 team hub and a personal hub, as alpha
// developers have them: members of every role; memories people and agents
// wrote, with chunks, of every shape the switch tells apart; a seed; a
// persona; synced agent files; keys and grants; V1 Dream runs and a
// decision waiting on the board.
type v1World struct {
	owner, admin, contributor, viewer uuid.UUID
	team, personal                    uuid.UUID
	// The team hub's V1 memories by what they are.
	mine, rationale, procedural, teammate      uuid.UUID
	fridays, noFridays                         uuid.UUID
	long, secret, archived, clip               uuid.UUID
	byAgent, byHook, seed, withFile, personal1 uuid.UUID
	ownerKey, contributorKey, grant            uuid.UUID
	persona, globalConfig, projectConfig       uuid.UUID
	cursorConfig                               uuid.UUID
	gate, orphanGate                           uuid.UUID
}

// v1Memory writes a V1 memory and its chunk, as V1's push and ingest do.
func (f *fixture) v1Memory(hub, owner uuid.UUID, content string, set ...string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.exec(`INSERT INTO memories (id, hub_id, owner_id, title, content, content_type, content_hash, state, created_at, updated_at)
	        VALUES ($1, $2, $3, $4, $5, 'text', $6, 'active', $7, $7)`,
		id, hub, owner, truncate(content, 40), content, id.String(), time.Now().Add(-time.Duration(len(content))*time.Minute))
	f.exec(`INSERT INTO chunks (memory_id, content, chunk_index, token_count, search_text, language, search_config)
	        VALUES ($1, $2, 0, 10, $2, 'en', 'simple')`, id, content)
	for _, s := range set {
		f.exec(`UPDATE memories SET `+s+` WHERE id = $1`, id)
	}
	return id
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func newV1World(f *fixture) *v1World {
	f.t.Helper()
	w := &v1World{owner: f.user("zz"), admin: f.user("ad"), contributor: f.user("co"), viewer: f.user("vi")}
	w.team = f.space(w.owner, policy.SpaceTeam, "acme-web")
	w.personal = f.space(w.owner, policy.SpacePersonal, "Personal")
	f.join(w.team, w.admin, "admin")
	f.join(w.team, w.contributor, "contributor")
	f.join(w.team, w.viewer, "viewer")
	f.exec(`UPDATE hubs SET plan = 'pro' WHERE id = $1`, w.team)

	w.mine = f.v1Memory(w.team, w.owner, "We use pnpm workspaces for every package.")
	w.rationale = f.v1Memory(w.team, w.owner, "We chose River over Temporal because it runs in Postgres.", "kind = 'rationale'")
	w.procedural = f.v1Memory(w.team, w.owner, "Run pnpm format before every commit.", "kind = 'procedural'")
	w.teammate = f.v1Memory(w.team, w.contributor, "The staging database lives on Neon.")
	w.fridays = f.v1Memory(w.team, w.owner, "We deploy on Fridays after the standup.")
	w.noFridays = f.v1Memory(w.team, w.admin, "We never deploy on Fridays.")
	w.long = f.v1Memory(w.team, w.owner, strings.Repeat("A design note that runs long. ", 90))
	w.secret = f.v1Memory(w.team, w.owner, "The deploy key is AKIAIOSFODNN7EXAMPLE for the bucket.")
	w.archived = f.v1Memory(w.team, w.owner, "We used to deploy with Heroku.", "state = 'archived'")
	w.clip = f.v1Memory(w.team, w.owner, "A page about connection pooling.", "content_type = 'html'", "source = 'url'",
		"source_path = 'https://example.test/pooling'")
	w.byAgent = f.v1Memory(w.team, w.owner, "Claude noticed the CI cache misses on lockfile changes.",
		"created_by_type = 'agent'", "created_by_slug = 'claude-code'", "source_agent = 'claude-code'")
	w.byHook = f.v1Memory(w.team, w.owner, "Session summary: fixed the flaky upload test.", "created_via = 'hook'")
	w.seed = f.v1Memory(w.team, w.owner, "Welcome to Memax: this is a seed memory.", "source = 'system'",
		"source_kind = 'onboarding-seed'")
	w.withFile = f.v1Memory(w.team, w.owner, "The architecture diagram, as a PDF.", "content_type = 'pdf'")
	f.exec(`INSERT INTO memory_attachments (id, memory_id, owner_id, filename, content_type, storage_key)
	        VALUES ($1, $2, $3, 'arch.pdf', 'application/pdf', $4)`, uuid.New(), w.withFile, w.owner, "attachments/"+w.withFile.String())
	w.personal1 = f.v1Memory(w.personal, w.owner, "I prefer short commit messages.")

	f.exec(`INSERT INTO personas (id, owner_id, source_agent, source_file_path, name, content, content_hash)
	        VALUES ($1, $2, 'openclaw', '~/.openclaw/SOUL.md', 'Ada', 'Ada is calm and precise.', 'h1')`,
		func() uuid.UUID { w.persona = uuid.New(); return w.persona }(), w.owner)
	w.globalConfig, w.projectConfig, w.cursorConfig = uuid.New(), uuid.New(), uuid.New()
	f.exec(`INSERT INTO agent_configs (id, owner_id, agent, file_path, scope, content, content_hash) VALUES
	        ($1, $4, 'claude-code', 'CLAUDE.md', 'global', 'Answer briefly.', 'a'),
	        ($2, $4, 'claude-code', 'CLAUDE.md', 'project:https://github.com/Acme/Web.git', 'Use pnpm.', 'b'),
	        ($3, $4, 'cursor', '.cursor/rules/web.mdc', 'project:git@github.com:acme/web', 'Prefer named exports.', 'c')`,
		w.globalConfig, w.projectConfig, w.cursorConfig, w.owner)
	f.exec(`INSERT INTO connected_agents (owner_id, agent_name, display_name) VALUES ($1, 'claude-code', 'Claude Code')`, w.owner)

	w.ownerKey = f.apiKey(w.owner, "claude-code", nil)
	w.contributorKey = f.apiKey(w.contributor, "codex", []string{"memory:read"}, w.team)
	w.grant = f.grant(w.owner, "cursor", w.personal)

	run := uuid.New()
	f.exec(`INSERT INTO dream_runs (id, owner_id, hub_id, status, memories_scanned, duplicates_merged, report)
	        VALUES ($1, $2, $3, 'completed', 12, 2, 'Merged two duplicates.')`, run, w.owner, w.team)
	f.exec(`INSERT INTO dream_actions (run_id, action_type, source_memory_ids, reason) VALUES ($1, 'merge', $2, 'duplicate')`,
		run, []string{w.mine.String()})
	board := uuid.New()
	f.exec(`INSERT INTO boards (id, hub_id, created_by) VALUES ($1, $2, $3)`, board, w.team, w.owner)
	w.gate, w.orphanGate = uuid.New(), uuid.New()
	f.exec(`INSERT INTO board_slots (id, board_id, slot_key, kind, title, payload) VALUES
	        ($1, $3, 'gate:a', 'decision_gate', 'Which queue?', '{"question":"Which queue?","options":[{"id":"opt-1","label":"River"},{"id":"opt-2","label":"SQS"}],"source_agent":"claude-code"}'),
	        ($2, $3, 'gate:b', 'decision_gate', 'Which font?', '{"question":"Which font?","options":[{"id":"opt-1","label":"Inter"},{"id":"opt-2","label":"Geist"}],"source_agent":"gemini"}')`,
		w.gate, w.orphanGate, board)
	return w
}

// v1Fingerprint hashes every V1 row the switch could touch, per table, so
// a test can prove the switch (and switching back) changed none.
func (f *fixture) v1Fingerprint() map[string]string {
	f.t.Helper()
	out := map[string]string{}
	for _, t := range []string{"memories", "chunks", "memory_attachments", "personas", "agent_configs", "hub_members",
		"api_keys", "oauth_grants", "connected_agents", "dream_runs", "dream_actions", "board_slots", "boards",
		"usage_events", "notifications"} {
		var h string
		if err := f.pool.QueryRow(context.Background(),
			fmt.Sprintf(`SELECT COALESCE(md5(string_agg(t::text, '|' ORDER BY t::text)), '') FROM public.%s t`, t)).Scan(&h); err != nil {
			f.t.Fatalf("fingerprint %s: %v", t, err)
		}
		out[t] = h
	}
	var hubs string
	if err := f.pool.QueryRow(context.Background(), `
		SELECT md5(string_agg((id, name, slug, hub_type, owner_id, plan, settings)::text, '|' ORDER BY id)) FROM public.hubs`).Scan(&hubs); err != nil {
		f.t.Fatal(err)
	}
	out["hubs (V1 columns)"] = hubs
	return out
}

func sameFingerprint(t *testing.T, what string, a, b map[string]string) {
	t.Helper()
	for k, v := range a {
		if b[k] != v {
			t.Errorf("%s: V1 table %s changed", what, k)
		}
	}
}

func TestSwitchPreviewSaysWhatMoves(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	w := newV1World(f)
	ctx := context.Background()
	before := f.v1Fingerprint()
	st, err := f.l.SwitchStatus(ctx, f.scope(w.owner), w.team)
	if err != nil {
		t.Fatal(err)
	}
	sameFingerprint(t, "preview", before, f.v1Fingerprint())
	if st.State != ledger.SwitchStateV1 || st.Space.V2EnabledAt != nil {
		t.Errorf("state = %s", st.State)
	}
	pv := st.Preview
	n := pv.Notes
	// 14 V1 memories in the team hub; the seed is left out.
	if n.Total != 13 || n.Seeds != 1 {
		t.Errorf("notes total %d seeds %d", n.Total, n.Seeds)
	}
	// Candidates: mine, rationale, procedural, teammate, fridays, noFridays.
	if n.Candidates != 6 || n.Agent != 2 || n.Person != 11 {
		t.Errorf("candidates %d agent %d person %d", n.Candidates, n.Agent, n.Person)
	}
	if n.Long != 1 || n.Secret != 1 || n.Archived != 1 || n.External != 1 || n.Format != 1 {
		t.Errorf("holds = %+v", n)
	}
	if n.Fold != 5 || n.Kept != 2 {
		t.Errorf("fold %d kept %d", n.Fold, n.Kept)
	}
	if !slices.Equal(pv.Kinds, []policy.SpaceKind{policy.SpaceTeam, policy.SpaceProject}) {
		t.Errorf("kinds = %v", pv.Kinds)
	}
	if pv.Plan != "pro" || pv.DreamRuns != 1 || pv.Gates != 2 || pv.Empty {
		t.Errorf("plan %q dream runs %d gates %d empty %v", pv.Plan, pv.DreamRuns, pv.Gates, pv.Empty)
	}
	roles := map[string]string{}
	for _, m := range pv.Members {
		roles[m.V1Role] = fmt.Sprintf("%s/%v", m.Role, m.CanForget)
	}
	want := map[string]string{"owner": "owner/true", "admin": "member/true", "contributor": "member/false", "viewer": "viewer/false"}
	for k, v := range want {
		if roles[k] != v {
			t.Errorf("%s maps to %s, want %s", k, roles[k], v)
		}
	}
	// The owner's all-spaces key and the contributor's read-only key reach
	// the team hub; the owner's grant is bound to the personal hub.
	if len(pv.Agents) != 2 {
		t.Fatalf("agents = %+v", pv.Agents)
	}
	for _, a := range pv.Agents {
		want := policy.AutonomyPropose
		if a.PersonID == w.contributor {
			want = policy.AutonomyRead
		}
		if a.Autonomy != want || a.Connected {
			t.Errorf("agent %s: %s connected=%v, want %s", a.Name, a.Autonomy, a.Connected, want)
		}
	}
	// A team hub has no repository; as a project for acme/web, its two
	// project files map to targets.
	if len(pv.Configs) != 0 {
		t.Errorf("a team hub without a repository has configs: %+v", pv.Configs)
	}
	ps, err := f.l.SwitchStatus(ctx, f.scope(w.owner), w.personal)
	if err != nil {
		t.Fatal(err)
	}
	if ps.Preview.Personas != 1 || len(ps.Preview.Configs) != 1 || ps.Preview.Notes.Candidates != 1 {
		t.Errorf("personal preview = %+v", ps.Preview)
	}
}

func TestSwitchMovesEverythingAndLosesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	w := newV1World(f)
	ctx := context.Background()
	before := f.v1Fingerprint()
	repo := "acme/web"
	st, err := f.l.StartSwitch(ctx, person(w.owner), policy.ViaWeb, f.scope(w.owner), w.team,
		ledger.SwitchOptions{Kind: policy.SpaceProject, Repository: &repo, Key: "switch-1"})
	if err != nil {
		t.Fatal(err)
	}
	// No River here: the switch ran within the call.
	if st.State != ledger.SwitchStateSwitched || st.Step != ledger.SwitchStepDone || st.Space.V2EnabledAt == nil {
		t.Fatalf("state %s step %s error %q", st.State, st.Step, st.Error)
	}
	if st.Space.Kind != policy.SpaceProject || st.Space.Repository != repo {
		t.Errorf("space = %+v", st.Space)
	}
	// No V1 row changed (hubs: only the V2 columns).
	sameFingerprint(t, "switch", before, f.v1Fingerprint())

	// Nothing lost: every V1 memory but the seed is a note, by count and
	// by the content hash of its words.
	if got, want := f.count(`SELECT count(*) FROM v2.note_refs WHERE space_id = $1 AND origin = 'memory'`, w.team),
		f.count(`SELECT count(*) FROM memories WHERE hub_id = $1 AND source_kind IS DISTINCT FROM 'onboarding-seed'`, w.team); got != want || got != 13 {
		t.Errorf("notes %d, V1 memories %d", got, want)
	}
	if f.count(`SELECT count(*) FROM v2.note_refs WHERE note_id = $1`, w.seed) != 0 {
		t.Error("the seed became a note")
	}
	var v1Hash, noteHash string
	if err := f.pool.QueryRow(ctx, `SELECT md5(string_agg(content, '|' ORDER BY id)) FROM memories WHERE hub_id = $1 AND source_kind IS DISTINCT FROM 'onboarding-seed'`, w.team).Scan(&v1Hash); err != nil {
		t.Fatal(err)
	}
	scope := f.scope(w.owner)
	if err := f.asV2(scope.SpaceIDs(), scope.TenantIDs(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT md5(string_agg(n.body, '|' ORDER BY n.id)) FROM v2.notes n
		    WHERE n.space_id = $1 AND n.origin = 'memory'`, w.team).Scan(&noteHash)
	}); err != nil {
		t.Fatal(err)
	}
	if v1Hash == "" || v1Hash != noteHash {
		t.Errorf("the notes' words differ from V1's: %s vs %s", noteHash, v1Hash)
	}
	disp := map[string]int{}
	rows, _ := f.pool.Query(ctx, `SELECT disposition, count(*) FROM v2.note_refs WHERE space_id = $1 GROUP BY 1`, w.team)
	for rows.Next() {
		var d string
		var n int
		_ = rows.Scan(&d, &n)
		disp[d] = n
	}
	rows.Close()
	// Fold: the long note, the page, the agent's, the hook's, the PDF, and
	// the two project files.
	if disp["candidate"] != 6 || disp["fold"] != 7 || disp["note"] != 2 {
		t.Errorf("dispositions = %v", disp)
	}

	// The person's own memories went up as one V1 import, as proposals
	// citing their notes: nothing kept.
	p := st.Progress
	if p.Proposed != 6 || len(p.Imports) != 1 || st.ImportID == nil || p.Notes != 13 {
		t.Errorf("progress = %+v", p)
	}
	if f.count(`SELECT count(*) FROM v2.memories WHERE space_id = $1 AND lifecycle = 'kept'`, w.team) != 0 {
		t.Error("the switch kept something")
	}
	if f.count(`SELECT count(*) FROM v2.imports WHERE space_id = $1 AND origin = 'v1'`, w.team) != 1 {
		t.Error("no V1 import")
	}
	if n := f.count(`SELECT count(DISTINCT ms.memory_id) FROM v2.sources s JOIN v2.memory_sources ms ON ms.source_id = s.id
	                  WHERE s.space_id = $1 AND s.kind = 'note' AND s.trust_class = 'person' AND s.locator ? 'note'`, w.team); n != 6 {
		t.Errorf("%d proposals cite a note", n)
	}
	view, err := f.l.GetImport(ctx, f.scope(w.owner), w.team, *st.ImportID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Import.Origin != "v1" || len(view.Memories) != 6 {
		t.Errorf("import = %+v (%d memories)", view.Import, len(view.Memories))
	}
	for _, it := range view.Items {
		if !strings.HasPrefix(it.Ref, "N-") || it.Location != ledger.ImportV1 {
			t.Errorf("item %+v", it)
		}
	}
	// The project files: notes, and the targets they stand for.
	if f.count(`SELECT count(*) FROM v2.note_refs WHERE space_id = $1 AND origin = 'agent_config'`, w.team) != 2 {
		t.Error("the project's agent files aren't notes")
	}
	var kinds []string
	rows, _ = f.pool.Query(ctx, `SELECT kind FROM v2.targets WHERE space_id = $1 ORDER BY kind`, w.team)
	for rows.Next() {
		var k string
		_ = rows.Scan(&k)
		kinds = append(kinds, k)
	}
	rows.Close()
	if !slices.Equal(kinds, []string{"agents_md", "claude_md", "cursor_mdc"}) {
		t.Errorf("targets = %v", kinds)
	}
	// The members' keys are connected at Propose (Read when they can't
	// write), and told.
	if p.Connected != 2 || p.Notified != 2 {
		t.Errorf("connected %d notified %d", p.Connected, p.Notified)
	}
	levels := map[uuid.UUID]string{}
	rows, _ = f.pool.Query(ctx, `SELECT c.credential_id, s.autonomy FROM v2.agent_connection_spaces s
	                               JOIN v2.agent_connections c ON c.id = s.connection_id WHERE s.space_id = $1`, w.team)
	for rows.Next() {
		var id uuid.UUID
		var a string
		_ = rows.Scan(&id, &a)
		levels[id] = a
	}
	rows.Close()
	if levels[w.ownerKey] != "propose" || levels[w.contributorKey] != "read" || len(levels) != 2 {
		t.Errorf("connections = %v", levels)
	}
	if f.count(`SELECT count(*) FROM v2.agent_notices WHERE space_id = $1 AND kind = 'switched' AND op_id IS NULL`, w.team) != 2 {
		t.Error("the agents weren't told")
	}
	// The waiting gate from claude-code moved; gemini's has no agent here.
	if p.GatesMoved != 1 || p.GatesLeft != 1 {
		t.Errorf("gates moved %d left %d", p.GatesMoved, p.GatesLeft)
	}
	if f.count(`SELECT count(*) FROM v2.decision_gates WHERE space_id = $1 AND status = 'waiting' AND question = 'Which queue?'`, w.team) != 1 {
		t.Error("the V1 gate isn't waiting on the record")
	}
	// Receipts: noted (memories, agent files), switched; and the tenant is
	// the owner now (a project space).
	if f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND object_kind = 'space' AND action = 'noted'`, w.team) != 2 ||
		f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND action = 'switched'`, w.team) != 1 {
		t.Error("the switch's receipts are missing")
	}
	if f.count(`SELECT count(*) FROM hubs WHERE id = $1 AND tenant_id = $2`, w.team, w.owner) != 1 ||
		f.count(`SELECT count(*) FROM v2.space_ledgers WHERE space_id = $1 AND tenant_id = $2`, w.team, w.owner) != 1 {
		t.Error("the project space's tenant isn't its owner")
	}
	// Plan grandfathered.
	if f.count(`SELECT count(*) FROM v2.space_switches WHERE space_id = $1 AND plan = 'pro'`, w.team) != 1 {
		t.Error("the plan isn't recorded")
	}

	// Asking again changes nothing.
	again, err := f.l.StartSwitch(ctx, person(w.owner), policy.ViaWeb, f.scope(w.owner), w.team, ledger.SwitchOptions{Key: "switch-2"})
	if err != nil || again.State != ledger.SwitchStateSwitched {
		t.Fatalf("again: %v %+v", err, again)
	}
	if f.count(`SELECT count(*) FROM v2.note_refs WHERE space_id = $1`, w.team) != 15 {
		t.Error("switching again numbered notes again")
	}

	// The V1 Dream run is read-only history.
	runs, err := f.l.ListV1DreamRuns(ctx, f.scope(w.owner), w.team)
	if err != nil || len(runs) != 1 || runs[0].Merged != 2 || runs[0].Actions != 1 {
		t.Errorf("V1 Dream runs = %+v, %v", runs, err)
	}

	// Switch back: V1 again, exactly.
	back, err := f.l.SwitchBack(ctx, person(w.owner), policy.ViaWeb, f.scope(w.owner), w.team, "back-1")
	if err != nil {
		t.Fatal(err)
	}
	if back.State != ledger.SwitchStateOff || back.Space.V2EnabledAt != nil {
		t.Errorf("back = %s", back.State)
	}
	sameFingerprint(t, "switch back", before, f.v1Fingerprint())
	if f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND action = 'switched_back'`, w.team) != 1 {
		t.Error("no switched_back receipt")
	}
	// Switching again moves only what V1 gained meanwhile.
	newer := f.v1Memory(w.team, w.owner, "Feature flags live in LaunchDarkly.")
	again, err = f.l.StartSwitch(ctx, person(w.owner), policy.ViaWeb, f.scope(w.owner), w.team, ledger.SwitchOptions{Key: "switch-3"})
	if err != nil || again.State != ledger.SwitchStateSwitched {
		t.Fatalf("switch again: %v %+v", err, again)
	}
	if again.Progress.Notes != 1 || again.Progress.Proposed != 1 {
		t.Errorf("switching again: %+v", again.Progress)
	}
	if f.count(`SELECT count(*) FROM v2.note_refs WHERE note_id = $1 AND disposition = 'candidate'`, newer) != 1 {
		t.Error("the new V1 memory isn't a candidate")
	}
}

func TestSwitchPersonalSpace(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	w := newV1World(f)
	ctx := context.Background()
	st, err := f.l.StartSwitch(ctx, person(w.owner), policy.ViaCLI, f.scope(w.owner), w.personal, ledger.SwitchOptions{Key: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if st.State != ledger.SwitchStateSwitched || st.Progress.Personas != 1 || st.Progress.Configs != 1 || st.Progress.Notes != 1 {
		t.Errorf("personal = %+v", st.Progress)
	}
	// Personal spaces get no compile targets from agent files, and the
	// persona waits for Dream.
	if f.count(`SELECT count(*) FROM v2.targets WHERE space_id = $1`, w.personal) != 0 {
		t.Error("a personal space got targets")
	}
	if f.count(`SELECT count(*) FROM v2.note_refs WHERE space_id = $1 AND origin = 'persona' AND disposition = 'fold'`, w.personal) != 1 {
		t.Error("the persona isn't a note for Dream")
	}
	// The cursor grant is bound to the personal hub: connected here.
	if f.count(`SELECT count(*) FROM v2.agent_connections c JOIN v2.agent_connection_spaces s ON s.connection_id = c.id
	             WHERE c.credential_id = $1 AND s.space_id = $2`, w.grant, w.personal) != 1 {
		t.Error("the personal hub's grant isn't connected")
	}
	// A team hub can't switch as a personal space, nor a personal one as a project.
	_, err = f.l.StartSwitch(ctx, person(w.owner), policy.ViaWeb, f.scope(w.owner), w.team,
		ledger.SwitchOptions{Kind: policy.SpacePersonal, Key: "k"})
	var ke *ledger.SpaceKindError
	if !errors.As(err, &ke) {
		t.Errorf("team as personal: %v", err)
	}
}

func TestOnlyTheOwnerSwitches(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	w := newV1World(f)
	ctx := context.Background()
	for _, who := range []uuid.UUID{w.admin, w.contributor, w.viewer} {
		_, err := f.l.StartSwitch(ctx, person(who), policy.ViaWeb, f.scope(who), w.team, ledger.SwitchOptions{Key: "x"})
		var refused *ledger.SpaceRefusedError
		if !errors.As(err, &refused) || refused.Decision.Code != policy.CodeSwitchByOwner {
			t.Errorf("%s: %v", who, err)
		}
		if _, err := f.l.SwitchBack(ctx, person(who), policy.ViaWeb, f.scope(who), w.team, "x"); !errors.As(err, &refused) {
			t.Errorf("switch back by %s: %v", who, err)
		}
	}
	agent := ledger.Actor{Kind: policy.ActorAgent, ID: uuid.New(), Credential: policy.CredentialAPIKey}
	_, err := f.l.StartSwitch(ctx, agent, policy.ViaAPI, f.scope(w.owner), w.team, ledger.SwitchOptions{Key: "x"})
	var refused *ledger.SpaceRefusedError
	if !errors.As(err, &refused) || refused.Decision.Code != policy.CodeSpaceByPerson {
		t.Errorf("agent: %v", err)
	}
	// Any member reads the preview; nobody else sees the space.
	if _, err := f.l.SwitchStatus(ctx, f.scope(w.viewer), w.team); err != nil {
		t.Errorf("viewer preview: %v", err)
	}
	stranger := f.user("st")
	if _, err := f.l.SwitchStatus(ctx, f.scope(stranger), w.team); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("stranger: %v", err)
	}
}

// A switch that fails in the middle resumes at the step that failed, and
// nothing is done twice.
func TestSwitchResumesAfterAFailure(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	w := newV1World(f)
	ctx := context.Background()
	// The import fails: one statement's insert is refused by a trigger.
	f.exec(`CREATE FUNCTION public.fail_import() RETURNS trigger LANGUAGE plpgsql AS $$
	        BEGIN RAISE EXCEPTION 'the database went away'; END $$`)
	f.exec(`CREATE TRIGGER fail_import BEFORE INSERT ON v2.import_items FOR EACH ROW EXECUTE FUNCTION public.fail_import()`)
	_, err := f.l.StartSwitch(ctx, person(w.owner), policy.ViaWeb, f.scope(w.owner), w.team, ledger.SwitchOptions{Key: "s1"})
	if err == nil {
		t.Fatal("the switch didn't fail")
	}
	st, err := f.l.SwitchStatus(ctx, f.scope(w.owner), w.team)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != ledger.SwitchStateFailed || st.Step != ledger.SwitchStepCandidates || st.Error == "" || st.Space.V2EnabledAt != nil {
		t.Fatalf("after the failure: %s at %s (%q)", st.State, st.Step, st.Error)
	}
	notes := f.count(`SELECT count(*) FROM v2.note_refs WHERE space_id = $1`, w.team)
	f.exec(`DROP TRIGGER fail_import ON v2.import_items`)
	st, err = f.l.StartSwitch(ctx, person(w.owner), policy.ViaWeb, f.scope(w.owner), w.team, ledger.SwitchOptions{Key: "s2"})
	if err != nil {
		t.Fatal(err)
	}
	if st.State != ledger.SwitchStateSwitched || st.Attempts != 2 {
		t.Errorf("resumed: %s after %d attempts", st.State, st.Attempts)
	}
	if n := f.count(`SELECT count(*) FROM v2.note_refs WHERE space_id = $1`, w.team); n != notes {
		t.Errorf("resuming numbered %d notes, had %d", n, notes)
	}
	if n := f.count(`SELECT count(*) FROM v2.memories WHERE space_id = $1`, w.team); n != 6 {
		t.Errorf("%d proposals after resuming, want 6", n)
	}
}

// The switch, the notes' search and a note's Forget read and write v2
// tables only inside their transactions, after their scope, as memax_v2
// (netsim.Audit on the wire). The V1 rows it reads, it reads as the login
// role, before any ledger transaction, as identity resolution does.
func TestSwitchIsScopedOnTheWire(t *testing.T) {
	t.Parallel()
	audit := netsim.NewAudit(ledger.DBRole)
	db := testdb.Open(t, testdb.Options{Watch: audit.Observe})
	f := newFixtureOn(t, db)
	w := newV1World(f)
	ctx := context.Background()
	audit.Arm()
	defer audit.Require(t)
	repo := "acme/web"
	if _, err := f.l.SwitchStatus(ctx, f.scope(w.owner), w.team); err != nil {
		t.Fatal(err)
	}
	st, err := f.l.StartSwitch(ctx, person(w.owner), policy.ViaWeb, f.scope(w.owner), w.team,
		ledger.SwitchOptions{Kind: policy.SpaceProject, Repository: &repo, Key: "s"})
	if err != nil || st.State != ledger.SwitchStateSwitched {
		t.Fatalf("switch: %v", err)
	}
	notes, err := f.l.SearchNotes(ctx, f.scope(w.owner), ledger.NoteQuery{Text: "pnpm"})
	if err != nil || len(notes) == 0 {
		t.Fatalf("search: %v %d", err, len(notes))
	}
	scope := f.scope(w.owner).Narrow(w.team)
	res, err := f.l.Apply(ctx, &ledger.ForgetNote{Meta: meta(person(w.owner), scope, policy.ViaWeb), SpaceID: w.team,
		Note: notes[0].Ref, Carries: nil})
	var ce *ledger.ForgetCarriesError
	if errors.As(err, &ce) {
		refs := make([]string, len(ce.Carries))
		for i, c := range ce.Carries {
			refs[i] = c.Ref
		}
		res, err = f.l.Apply(ctx, &ledger.ForgetNote{Meta: meta(person(w.owner), scope, policy.ViaWeb), SpaceID: w.team,
			Note: notes[0].Ref, Carries: refs})
	}
	if err != nil || res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("forget a note: %v", err)
	}
	if _, err := f.l.SwitchBack(ctx, person(w.owner), policy.ViaWeb, f.scope(w.owner), w.team, "b"); err != nil {
		t.Fatal(err)
	}
}

// Notes are their owner's, and a space's notes are invisible from another.
func TestNotesAreTheOwners(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	w := newV1World(f)
	ctx := context.Background()
	repo := "acme/web"
	if _, err := f.l.StartSwitch(ctx, person(w.owner), policy.ViaWeb, f.scope(w.owner), w.team,
		ledger.SwitchOptions{Kind: policy.SpaceProject, Repository: &repo, Key: "s"}); err != nil {
		t.Fatal(err)
	}
	search := func(who uuid.UUID, q string, spaces ...uuid.UUID) []ledger.Note {
		t.Helper()
		ns, err := f.l.SearchNotes(ctx, f.scope(who), ledger.NoteQuery{SpaceIDs: spaces, Text: q})
		if err != nil {
			t.Fatal(err)
		}
		return ns
	}
	// The owner sees every note in the space they own, by V1's index.
	got := search(w.owner, "Neon", w.team)
	if len(got) != 1 || got[0].ID != w.teammate || !strings.HasPrefix(got[0].Ref, "N-") || got[0].Disposition != ledger.NoteCandidate {
		t.Errorf("owner's search = %+v", got)
	}
	// The contributor sees their own notes only.
	if got := search(w.contributor, "Neon", w.team); len(got) != 1 {
		t.Errorf("contributor's own note: %+v", got)
	}
	if got := search(w.contributor, "pnpm", w.team); len(got) != 0 {
		t.Errorf("the contributor sees the owner's notes: %+v", got)
	}
	// Agent files and personas match too.
	if got := search(w.owner, "named exports", w.team); len(got) != 1 || got[0].Origin != ledger.NoteFromAgentConfig {
		t.Errorf("agent file = %+v", got)
	}
	// A note read by its ref; another space's scope finds nothing.
	n, err := f.l.GetNote(ctx, f.scope(w.owner), w.team, got[0].Ref)
	if err != nil || n.ID != got[0].ID {
		t.Errorf("get note: %v", err)
	}
	other := f.user("ot")
	otherSpace := f.space(other, policy.SpaceProject, "other")
	if got := search(other, "pnpm", otherSpace); len(got) != 0 {
		t.Errorf("another person's space sees notes: %+v", got)
	}
	// RLS: memax_v2 scoped to another space sees no note ref, no note, no chunk.
	if err := f.asV2([]uuid.UUID{otherSpace}, []uuid.UUID{other}, func(tx pgx.Tx) error {
		for _, tbl := range []string{"v2.note_refs", "v2.notes", "v2.note_chunks", "v2.space_switches", "v2.v1_dream_runs"} {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+tbl).Scan(&n); err != nil {
				return err
			}
			if n != 0 {
				return fmt.Errorf("%s: %d rows of another space", tbl, n)
			}
		}
		return nil
	}); err != nil {
		t.Error(err)
	}
}

// Forget reaches a note: its V1 row goes as V1's delete takes it, with
// the proposal the switch made of it, and a tombstone stays.
func TestForgetANote(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	w := newV1World(f)
	ctx := context.Background()
	if _, err := f.l.StartSwitch(ctx, person(w.owner), policy.ViaWeb, f.scope(w.owner), w.team, ledger.SwitchOptions{Key: "s"}); err != nil {
		t.Fatal(err)
	}
	scope := f.scope(w.owner).Narrow(w.team)
	var ref string
	var seq int64
	if err := f.pool.QueryRow(ctx, `SELECT seq FROM v2.note_refs WHERE note_id = $1`, w.mine).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	ref = ledger.FormatRef(ledger.PrefixNote, seq)
	pv, err := f.l.PreviewForgetNote(ctx, person(w.owner), policy.ViaWeb, scope, w.team, ref)
	if err != nil || !pv.Allowed || len(pv.Carries) != 1 {
		t.Fatalf("preview = %+v %v", pv, err)
	}
	// Without the proposal it carries: refused, nothing changes.
	_, err = f.l.Apply(ctx, &ledger.ForgetNote{Meta: meta(person(w.owner), scope, policy.ViaWeb), SpaceID: w.team, Note: ref})
	var ce *ledger.ForgetCarriesError
	if !errors.As(err, &ce) || len(ce.Carries) != 1 {
		t.Fatalf("forget without carries: %v", err)
	}
	// A viewer can't; nor can an agent.
	res, err := f.l.Apply(ctx, &ledger.ForgetNote{Meta: meta(person(w.viewer), f.scope(w.viewer).Narrow(w.team), policy.ViaWeb),
		SpaceID: w.team, Note: ref, Carries: []string{ce.Carries[0].Ref}})
	if err == nil && res.Outcome != ledger.OutcomeRefused {
		t.Errorf("a viewer forgot a note: %+v", res)
	}
	m := meta(person(w.owner), scope, policy.ViaWeb)
	res, err = f.l.Apply(ctx, &ledger.ForgetNote{Meta: m, SpaceID: w.team, Note: ref, Carries: []string{ce.Carries[0].Ref}})
	if err != nil || res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("forget: %v %+v", err, res.Policy)
	}
	if res.Tombstone == nil || res.Tombstone.Kind != ledger.ObjectNote || res.Tombstone.Ref != ref || len(res.Memories) != 1 {
		t.Errorf("tombstone = %+v, memories %d", res.Tombstone, len(res.Memories))
	}
	if f.count(`SELECT count(*) FROM memories WHERE id = $1`, w.mine)+f.count(`SELECT count(*) FROM chunks WHERE memory_id = $1`, w.mine) != 0 {
		t.Error("the note's V1 row is still there")
	}
	if f.count(`SELECT count(*) FROM dream_actions WHERE $1 = ANY (source_memory_ids) AND reason <> ''`, w.mine.String()) != 0 {
		t.Error("Dream's reason about it is still there")
	}
	if f.count(`SELECT count(*) FROM v2.note_refs WHERE note_id = $1 AND forgotten_at IS NOT NULL`, w.mine) != 1 {
		t.Error("the note isn't forgotten")
	}
	if f.count(`SELECT count(*) FROM v2.memories WHERE id = $1 AND lifecycle = 'forgotten'`, res.Memories[0].ID) != 1 {
		t.Error("the proposal carrying its words wasn't forgotten")
	}
	// Retrying replays; forgetting again is refused.
	again, err := f.l.Apply(ctx, &ledger.ForgetNote{Meta: m, SpaceID: w.team, Note: ref, Carries: []string{ce.Carries[0].Ref}})
	if err != nil || !again.Replayed || again.Tombstone == nil {
		t.Errorf("replay: %v %+v", err, again)
	}
	_, err = f.l.Apply(ctx, &ledger.ForgetNote{Meta: meta(person(w.owner), scope, policy.ViaWeb), SpaceID: w.team, Note: ref})
	if !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("forget again: %v", err)
	}
	// A note with an attached file: the propagation deletes its object.
	if err := f.pool.QueryRow(ctx, `SELECT seq FROM v2.note_refs WHERE note_id = $1`, w.withFile).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	res, err = f.l.Apply(ctx, &ledger.ForgetNote{Meta: meta(person(w.owner), scope, policy.ViaWeb), SpaceID: w.team,
		Note: ledger.FormatRef(ledger.PrefixNote, seq)})
	if err != nil || res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("forget the file's note: %v", err)
	}
	if f.count(`SELECT count(*) FROM v2.propagations WHERE op_id = $1 AND destination_kind = 'attachments'
	             AND detail -> 'delete' ? $2`, res.Tombstone.OpID, "attachments/"+w.withFile.String()) != 1 {
		t.Error("the attachment's object isn't queued for deletion")
	}
	// A note written after the switch (no number yet) is numbered as it is
	// forgotten.
	later := f.v1Memory(w.team, w.owner, "Written in V1 after the switch.")
	res, err = f.l.Apply(ctx, &ledger.ForgetNote{Meta: meta(person(w.owner), scope, policy.ViaWeb), SpaceID: w.team, Note: later.String()})
	if err != nil || res.Outcome != ledger.OutcomeApplied || !strings.HasPrefix(res.Tombstone.Ref, "N-") {
		t.Fatalf("forget an unnumbered note: %v", err)
	}
}

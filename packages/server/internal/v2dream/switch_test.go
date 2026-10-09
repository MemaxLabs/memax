package v2dream_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/v2dream"
	"github.com/MemaxLabs/memax/packages/server/internal/v2dream/dreamtest"
)

// newV1World is a V1 team hub with its owner, not yet on V2.
func newV1World(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.space = uuid.New()
	w.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, 'acme-web', $2, 'team', $3, 'team')`,
		w.space, "acme-web-"+w.space.String()[:8], w.owner)
	w.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, w.space, w.owner)
	return w
}

// v1Memory writes a V1 memory as V1's push did: a person's when agent is
// empty, else the agent's.
func (w *world) v1Memory(body, agent string, age time.Duration) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	at := time.Now().Add(-age)
	if agent == "" {
		w.exec(`INSERT INTO memories (id, owner_id, hub_id, title, content, source, created_at, updated_at)
		        VALUES ($1, $2, $3, '', $4, 'cli', $5, $5)`, id, w.owner, w.space, body, at)
		return id
	}
	w.exec(`INSERT INTO memories (id, owner_id, hub_id, title, content, created_by_type, created_by_slug, source_agent, source, created_at, updated_at)
	        VALUES ($1, $2, $3, '', $4, 'agent', $5, $5, 'mcp', $6, $6)`, id, w.owner, w.space, body, agent, at)
	return id
}

// A V1 space switched to V2 hands what its agents wrote to Dream: the next
// edition reads those notes by the N- refs the switch gave them (never the
// person's own memories, which went to Review for bulk keep), folds and
// proposes from them at the trust the switch recorded, numbers a note
// written after the switch itself, and every action it took undoes.
func TestSwitchedSpaceDreamsFromItsV1Notes(t *testing.T) {
	t.Parallel()
	w := newV1World(t)
	ctx := context.Background()
	w.v1Memory("Background jobs run on River, not Temporal.", "", 72*time.Hour)
	w.v1Memory("We deploy from main only.", "", 71*time.Hour)
	retries := w.v1Memory("Talked it over again: background jobs run on River, it handles retries.", "claude-code", 48*time.Hour)
	order := w.v1Memory("Decided with Jiahao: the CLI prints receipts in the same order as the web app.", "codex", 47*time.Hour)

	owner := ledger.Actor{Kind: policy.ActorPerson, ID: w.owner, Name: "Ziyang"}
	st, err := w.l.StartSwitchInline(ctx, owner, policy.ViaWeb, w.scope(), w.space,
		ledger.SwitchOptions{Kind: policy.SpaceProject, Key: "switch-" + w.space.String()})
	if err != nil {
		t.Fatal(err)
	}
	if st.State != ledger.SwitchStateSwitched || st.ImportID == nil {
		t.Fatalf("switch: state %s step %s error %q", st.State, st.Step, st.Error)
	}
	// What agents wrote waits for Dream; the person's own went to Review.
	disp := map[uuid.UUID]string{}
	rows, err := w.pool.Query(ctx, `SELECT note_id, disposition FROM v2.note_refs WHERE space_id = $1 AND edition_id IS NULL`, w.space)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id uuid.UUID
		var d string
		if err := rows.Scan(&id, &d); err != nil {
			t.Fatal(err)
		}
		disp[id] = d
	}
	rows.Close()
	if len(disp) != 4 || disp[retries] != "fold" || disp[order] != "fold" {
		t.Fatalf("the switch numbered %v", disp)
	}

	// The person keeps their V1 memories in one go.
	view, err := w.l.GetImport(ctx, w.scope(), w.space, *st.ImportID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Memories) != 2 {
		t.Fatalf("the V1 import proposed %d", len(view.Memories))
	}
	var river *ledger.Memory
	for _, im := range view.Memories {
		m := w.apply(&ledger.Keep{Meta: w.meta(), Memory: im.Memory.Ref}).Memory
		if m.Statement == "Background jobs run on River, not Temporal." {
			river = m
		}
	}
	if river == nil {
		t.Fatal("the River memory wasn't kept")
	}
	// An agent writes a note after the switch: Dream numbers it.
	again := w.note("Again today: the CLI prints receipts in the web app's order.", "codex")

	o := &dreamtest.Oracle{
		Folds: []dreamtest.FoldRule{{Note: "background jobs run on River", Memory: "Background jobs run on River"}},
		Facts: []dreamtest.FactRule{{Notes: []string{"prints receipts"}, Statement: "The CLI prints receipts in the same order as the web app.",
			Section: "conventions"}},
	}
	out := run(t, engine(w, o, v2dream.Config{}), w, time.Now().Add(-time.Minute).Truncate(time.Second))
	if !out.Ran || out.Edition == nil {
		t.Fatalf("no edition: %+v", out)
	}
	ed := out.Edition
	if ed.NotesRead != 3 || ed.Counts[ledger.DreamFold] != 1 || ed.Counts[ledger.DreamPropose] != 1 {
		t.Fatalf("read %d notes (%v), counts %v, skipped %v", ed.NotesRead, ed.NoteRefs, ed.Counts, ed.Stats.Skipped)
	}
	// It cited the switch's refs, and numbered only the note written since.
	refOf := func(id uuid.UUID) string {
		var seq int64
		if err := w.pool.QueryRow(ctx, `SELECT seq FROM v2.note_refs WHERE note_id = $1`, id).Scan(&seq); err != nil {
			t.Fatalf("ref of %s: %v", id, err)
		}
		return ledger.FormatRef(ledger.PrefixNote, seq)
	}
	for _, id := range []uuid.UUID{retries, order, again} {
		if !slices.Contains(ed.NoteRefs, refOf(id)) {
			t.Errorf("the edition's notes %v leave out %s", ed.NoteRefs, refOf(id))
		}
	}
	if n := w.count(`SELECT count(*) FROM v2.note_refs WHERE space_id = $1 AND edition_id = $2`, w.space, ed.ID); n != 1 {
		t.Errorf("the edition numbered %d notes, want the one written since the switch", n)
	}
	if n := w.count(`SELECT count(*) FROM v2.note_refs WHERE space_id = $1 AND edition_id IS NULL`, w.space); n != 4 {
		t.Errorf("the switch's numbering changed: %d", n)
	}
	// The fold links the kept memory to the agent's V1 note; the proposal
	// cites both agents' notes as their own work.
	if n := w.count(`SELECT count(*) FROM v2.memory_links WHERE from_memory_id = $1 AND kind = 'folded_from'
	                  AND to_note_id = $2 AND ended_receipt_id IS NULL`, river.ID, retries); n != 1 {
		t.Errorf("folds into the River memory: %d", n)
	}
	var proposal uuid.UUID
	if err := w.pool.QueryRow(ctx, `SELECT m.id FROM v2.memories m JOIN v2.memory_versions v ON v.memory_id = m.id
	    WHERE m.space_id = $1 AND v.statement = 'The CLI prints receipts in the same order as the web app.'`, w.space).Scan(&proposal); err != nil {
		t.Fatalf("the proposal: %v", err)
	}
	if got := w.get(proposal); got.Lifecycle != lifecycle.Proposed || got.Trust != policy.TrustAgentOwnWork {
		t.Errorf("the proposal is %s at %s", got.Lifecycle, got.Trust)
	}
	if n := w.count(`SELECT count(*) FROM v2.sources s JOIN v2.memory_sources ms ON ms.source_id = s.id
	                  WHERE ms.memory_id = $1 AND s.kind = 'note' AND s.locator ->> 'note' = ANY ($2)`,
		proposal, []string{order.String(), again.String()}); n != 2 {
		t.Errorf("the proposal cites %d of the notes", n)
	}

	// Every action undoes: the fold's link ends and the River memory is as
	// it was; the proposal leaves Review.
	full, err := w.l.GetEdition(ctx, w.scope(), w.space, ed.Ref)
	if err != nil {
		t.Fatal(err)
	}
	before := w.get(river.ID)
	for _, a := range full.Actions {
		res := w.apply(&ledger.UndoDreamAction{Meta: w.meta(), Action: a.ID})
		if res.Outcome != ledger.OutcomeApplied || res.DreamAction == nil || res.DreamAction.Undone == nil {
			t.Errorf("undo %s: %+v", a.Kind, res)
		}
	}
	if n := w.count(`SELECT count(*) FROM v2.memory_links WHERE from_memory_id = $1 AND kind = 'folded_from' AND ended_receipt_id IS NULL`, river.ID); n != 0 {
		t.Errorf("%d folds survived the undo", n)
	}
	if after := w.get(river.ID); after.Lifecycle != lifecycle.Kept || after.Version != before.Version || after.Statement != before.Statement {
		t.Errorf("the River memory after undo: %s v%d", after.Lifecycle, after.Version)
	}
	if got := w.get(proposal); got.Lifecycle != lifecycle.Rejected {
		t.Errorf("the undone proposal is %s", got.Lifecycle)
	}
	// The notes stay, with their words in V1 and their refs.
	if n := w.count(`SELECT count(*) FROM memories WHERE id = ANY ($1)`, []uuid.UUID{retries, order, again}); n != 3 {
		t.Errorf("V1 holds %d of the notes", n)
	}
}

package ledger_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// dreamWorld is a space with one of everything Dream acts on: a kept
// memory the Brief places (notes fold into it), two facts that disagree,
// one past its stale_after date, one nobody reads, two proposals that
// repeat each other, and notes from an agent.
type dreamWorld struct {
	f            *fixture
	owner, space uuid.UUID
	k, x, y, s   *ledger.Memory
	fd, p1, p2   *ledger.Memory
	notes        []uuid.UUID
	brief        *ledger.Brief
}

func newDreamWorld(t *testing.T) *dreamWorld {
	t.Helper()
	return newDreamWorldOn(t, newFixture(t))
}

// newDreamWorldOn builds the world on a fixture (an audited database).
func newDreamWorldOn(t *testing.T, f *fixture) *dreamWorld {
	t.Helper()
	w := &dreamWorld{f: f, owner: f.user("zz")}
	w.space = f.space(w.owner, policy.SpaceProject, "memax-v2")
	w.k = f.remember(w.owner, w.space, "Background jobs run on River, not Temporal.")
	w.y = f.remember(w.owner, w.space, "Deploys go to Railway.")
	w.x = f.remember(w.owner, w.space, "Deploys go to Fly.io in iad and ams.")
	past := time.Now().Add(-48 * time.Hour)
	nm := fact(w.space, "Ask memax answers with the Haiku tier.")
	nm.StaleAfter = &past
	w.s = f.apply(&ledger.Remember{Meta: meta(person(w.owner), f.scope(w.owner), policy.ViaWeb), NewMemory: nm}).Memory
	w.fd = f.remember(w.owner, w.space, "Use tabs, not spaces, in generated Go files.")
	w.p1 = f.apply(&ledger.Propose{Meta: meta(person(w.owner), f.scope(w.owner), policy.ViaWeb),
		NewMemory: fact(w.space, "Review cards show the diff against the memory they replace.")}).Memory
	w.p2 = f.apply(&ledger.Propose{Meta: meta(person(w.owner), f.scope(w.owner), policy.ViaWeb),
		NewMemory: fact(w.space, "Review cards show a diff against the memory they would replace.")}).Memory
	for i, body := range []string{"Agreed: background jobs on River.", "River handles retries for us.", "The CLI prints receipts in web order."} {
		id := uuid.New()
		f.exec(`INSERT INTO memories (id, owner_id, hub_id, title, content, created_by_type, created_by_slug, created_at)
		        VALUES ($1, $2, $3, 'note', $4, 'agent', 'claude-code', now() - make_interval(mins => $5))`,
			id, w.owner, w.space, body, 30-i)
		w.notes = append(w.notes, id)
	}
	w.brief = f.brief(w.owner, w.space, 0, []ledger.BriefSection{{Key: "conventions", Heading: "Conventions",
		Items: []ledger.BriefItem{{Ref: w.k.Ref}, {Text: "Jobs are durable.", Cites: []string{w.k.Ref}}}}})
	return w
}

// dreamScope is the scope Dream acts in: the space, with no membership.
func (w *dreamWorld) dreamScope() ledger.Scope {
	w.f.t.Helper()
	s, err := w.f.l.SpaceScope(context.Background(), w.space)
	if err != nil {
		w.f.t.Fatal(err)
	}
	return s
}

func (w *dreamWorld) dreamMeta(key string) ledger.Meta {
	return ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorDream}, Scope: w.dreamScope(), Via: policy.ViaSystem, IdempotencyKey: key}
}

func (w *dreamWorld) notesRead() []ledger.NoteRead {
	out := make([]ledger.NoteRead, len(w.notes))
	for i, id := range w.notes {
		out[i] = ledger.NoteRead{ID: id, AuthorKind: "agent", Agent: "claude-code", Source: "mcp", CreatedAt: time.Now()}
	}
	return out
}

// publish publishes one edition with the given actions.
func (w *dreamWorld) publish(slot time.Time, actions ...ledger.PlannedAction) ledger.Result {
	w.f.t.Helper()
	now := time.Now()
	res := w.f.apply(&ledger.PublishEdition{Meta: w.dreamMeta("dream:" + slot.String()), SpaceID: w.space, Slot: slot,
		Trigger: ledger.DreamScheduled, Until: now, StartedAt: now.Add(-time.Second), Notes: w.notesRead(), Actions: actions,
		Cursor: &ledger.NoteCursor{At: now, ID: w.notes[len(w.notes)-1]}})
	if res.Outcome != ledger.OutcomeApplied || res.Edition == nil {
		w.f.t.Fatalf("publish: %s %s", res.Outcome, res.Policy.Message)
	}
	return res
}

func (w *dreamWorld) undo(action uuid.UUID) (ledger.Result, error) {
	return w.f.l.Apply(context.Background(), &ledger.UndoDreamAction{
		Meta: meta(person(w.owner), w.f.scope(w.owner), policy.ViaWeb), Action: action})
}

// fingerprint is everything about the space's record an undo must put
// back: every memory's state, version, section and trust, the active
// links, and the Brief in force. Stream versions move (an undo is a
// change with its own receipt), so they are left out.
func (w *dreamWorld) fingerprint() map[string]string {
	w.f.t.Helper()
	ctx := context.Background()
	out := map[string]string{}
	rows, err := w.f.pool.Query(ctx, `SELECT 'M ' || seq, lifecycle || ' ' || array_to_string(flags, ',') || ' v' || current_version
	                                       || ' ' || section || ' ' || trust || ' ' || COALESCE(decision::text, '')
	                                    FROM v2.memories WHERE space_id = $1`, w.space)
	if err != nil {
		w.f.t.Fatal(err)
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			w.f.t.Fatal(err)
		}
		out[k] = v
	}
	rows.Close()
	rows, err = w.f.pool.Query(ctx, `SELECT 'L ' || kind || ' ' || from_memory_id || ' ' || COALESCE(to_memory_id::text, to_note_id::text)
	                                    FROM v2.memory_links WHERE space_id = $1 AND ended_receipt_id IS NULL`, w.space)
	if err != nil {
		w.f.t.Fatal(err)
	}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			w.f.t.Fatal(err)
		}
		out[k] = "active"
	}
	rows.Close()
	var title, structure string
	if err := w.f.pool.QueryRow(ctx, `SELECT v.title, v.structure::text FROM v2.briefs b
	                                    JOIN v2.brief_versions v ON v.brief_id = b.id AND v.version = b.current_version
	                                   WHERE b.space_id = $1`, w.space).Scan(&title, &structure); err != nil {
		w.f.t.Fatal(err)
	}
	out["brief"] = title + " " + structure
	return out
}

func diffPrints(before, after map[string]string) []string {
	var d []string
	for k, v := range before {
		if after[k] != v {
			d = append(d, fmt.Sprintf("%s: %q → %q", k, v, after[k]))
		}
	}
	for k, v := range after {
		if _, ok := before[k]; !ok {
			d = append(d, fmt.Sprintf("%s: (none) → %q", k, v))
		}
	}
	slices.Sort(d)
	return d
}

// Rule 9: every action type → undo → state restored exactly.
func TestDreamEveryActionUndoes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind   ledger.DreamActionKind
		action func(w *dreamWorld) ledger.PlannedAction
		// changed is what the action must change, as fingerprint keys.
		check func(t *testing.T, w *dreamWorld, before, after map[string]string)
	}{
		{ledger.DreamFold, func(w *dreamWorld) ledger.PlannedAction {
			return ledger.PlannedAction{Kind: ledger.DreamFold, Memory: w.k.ID, Version: w.k.Version, Notes: w.notes[:2]}
		}, func(t *testing.T, w *dreamWorld, before, after map[string]string) {
			if d := diffPrints(before, after); len(d) != 2 || !strings.Contains(strings.Join(d, "\n"), "L folded_from "+w.k.ID.String()) {
				t.Errorf("fold changed %v, want two folded_from links", d)
			}
		}},
		{ledger.DreamPropose, func(w *dreamWorld) ledger.PlannedAction {
			return ledger.PlannedAction{Kind: ledger.DreamPropose, Notes: w.notes[2:],
				New: &ledger.DreamFact{Statement: "The CLI prints receipts in the same order as the web app.", Section: ledger.SectionConventions}}
		}, nil},
		{ledger.DreamDedupe, func(w *dreamWorld) ledger.PlannedAction {
			return ledger.PlannedAction{Kind: ledger.DreamDedupe, Memory: w.p2.ID, Version: 1, Related: w.p1.ID, RelatedVersion: 1}
		}, func(t *testing.T, w *dreamWorld, before, after map[string]string) {
			if got := after["M "+fmt.Sprint(seqOf(w.p2.Ref))]; !strings.HasPrefix(got, "merged") {
				t.Errorf("dedupe left %s as %q", w.p2.Ref, got)
			}
		}},
		{ledger.DreamConflict, func(w *dreamWorld) ledger.PlannedAction {
			return ledger.PlannedAction{Kind: ledger.DreamConflict, Memory: w.x.ID, Version: 1, Related: w.y.ID, RelatedVersion: 1}
		}, func(t *testing.T, w *dreamWorld, before, after map[string]string) {
			if got := after["M "+fmt.Sprint(seqOf(w.x.Ref))]; !strings.HasPrefix(got, "kept conflict") {
				t.Errorf("conflict left %s as %q", w.x.Ref, got)
			}
		}},
		{ledger.DreamStale, func(w *dreamWorld) ledger.PlannedAction {
			return ledger.PlannedAction{Kind: ledger.DreamStale, Memory: w.s.ID, Version: 1}
		}, func(t *testing.T, w *dreamWorld, before, after map[string]string) {
			if got := after["M "+fmt.Sprint(seqOf(w.s.Ref))]; !strings.HasPrefix(got, "kept stale") {
				t.Errorf("stale left %s as %q", w.s.Ref, got)
			}
		}},
		{ledger.DreamFade, func(w *dreamWorld) ledger.PlannedAction {
			now := time.Now()
			return ledger.PlannedAction{Kind: ledger.DreamFade, Memory: w.fd.ID, Version: 1, Unread: &now}
		}, func(t *testing.T, w *dreamWorld, before, after map[string]string) {
			if got := after["M "+fmt.Sprint(seqOf(w.fd.Ref))]; !strings.HasPrefix(got, "faded") {
				t.Errorf("fade left %s as %q", w.fd.Ref, got)
			}
		}},
		{ledger.DreamBrief, func(w *dreamWorld) ledger.PlannedAction {
			return ledger.PlannedAction{Kind: ledger.DreamBrief, Brief: &ledger.BriefDelta{BaseVersion: w.brief.Version, Ops: []ledger.BriefOp{
				{Op: ledger.BriefOpPlace, Item: w.y.Ref, Section: "conventions", After: w.k.Ref},
				{Op: ledger.BriefOpReword, Item: "P:conventions:1", Text: "Background jobs are durable and retried by River.", Cites: []string{w.k.Ref}},
			}}}
		}, func(t *testing.T, w *dreamWorld, before, after map[string]string) {
			if before["brief"] == after["brief"] || !strings.Contains(after["brief"], w.y.Ref) {
				t.Errorf("the Brief action changed nothing: %s", after["brief"])
			}
		}},
	}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			t.Parallel()
			w := newDreamWorld(t)
			before := w.fingerprint()
			res := w.publish(time.Now().Add(-time.Hour).Truncate(time.Second), c.action(w))
			e := res.Edition
			if e.Counts[c.kind] != 1 {
				t.Fatalf("applied %v, want one %s", e.Counts, c.kind)
			}
			after := w.fingerprint()
			if c.check != nil {
				c.check(t, w, before, after)
			}
			if len(diffPrints(before, after)) == 0 {
				t.Fatalf("%s changed nothing", c.kind)
			}
			got, err := w.f.l.GetEdition(context.Background(), w.f.scope(w.owner), w.space, e.Ref)
			if err != nil || len(got.Actions) != 1 || !got.Actions[0].Undoable {
				t.Fatalf("edition %+v %v", got, err)
			}
			undone, err := w.undo(got.Actions[0].ID)
			if err != nil || undone.Outcome != ledger.OutcomeApplied {
				t.Fatalf("undo: %v %+v", err, undone.Policy)
			}
			restored := w.fingerprint()
			if c.kind == ledger.DreamPropose {
				// The proposal Dream made is withdrawn: rejected, and out of
				// Review. Memories are never deleted.
				m := got.Actions[0].Memory
				if s := restored["M "+fmt.Sprint(seqOf(m.Ref))]; !strings.HasPrefix(s, "rejected") {
					t.Errorf("the withdrawn proposal is %q", s)
				}
				delete(restored, "M "+fmt.Sprint(seqOf(m.Ref)))
			}
			if d := diffPrints(before, restored); len(d) > 0 {
				t.Errorf("undo of %s left differences:\n%s", c.kind, strings.Join(d, "\n"))
			}
			// Undone once: never twice.
			if _, err := w.undo(got.Actions[0].ID); !errors.Is(err, ledger.ErrUndoRefused) {
				t.Errorf("second undo: %v", err)
			}
			var ue *ledger.UndoError
			if _, err := w.undo(got.Actions[0].ID); !errors.As(err, &ue) || ue.Reason != ledger.UndoAlreadyUndone {
				t.Errorf("second undo: %v", err)
			}
			w.f.everyRowReceipted(t)
		})
	}
}

func seqOf(ref string) int64 {
	_, n, _ := ledger.ParseRef(ref)
	return n
}

// Every action's receipts are Dream's, via system, and cite the edition;
// the edition has its own published receipt, and every row has one.
func TestDreamReceiptsCiteTheEdition(t *testing.T) {
	t.Parallel()
	w := newDreamWorld(t)
	now := time.Now()
	res := w.publish(now.Add(-time.Hour).Truncate(time.Second),
		ledger.PlannedAction{Kind: ledger.DreamFold, Memory: w.k.ID, Version: 1, Notes: w.notes[:2]},
		ledger.PlannedAction{Kind: ledger.DreamPropose, Notes: w.notes[2:], New: &ledger.DreamFact{Statement: "The CLI prints receipts in web order.", Section: ledger.SectionConventions}},
		ledger.PlannedAction{Kind: ledger.DreamDedupe, Memory: w.p2.ID, Version: 1, Related: w.p1.ID, RelatedVersion: 1},
		ledger.PlannedAction{Kind: ledger.DreamConflict, Memory: w.x.ID, Version: 1, Related: w.y.ID, RelatedVersion: 1},
		ledger.PlannedAction{Kind: ledger.DreamStale, Memory: w.s.ID, Version: 1},
		ledger.PlannedAction{Kind: ledger.DreamFade, Memory: w.fd.ID, Version: 1, Unread: &now},
		ledger.PlannedAction{Kind: ledger.DreamBrief, Brief: &ledger.BriefDelta{BaseVersion: w.brief.Version, Ops: []ledger.BriefOp{
			{Op: ledger.BriefOpAdd, Section: "conventions", Text: "Deploy targets are settled in Review.", Cites: []string{w.y.Ref}}}}},
	)
	e := res.Edition
	if len(e.Counts) != 7 {
		t.Fatalf("counts %v, want every kind", e.Counts)
	}
	if e.NotesRead != 3 || len(e.NoteRefs) != 3 || e.NoteRefs[0] != "N-0001" {
		t.Errorf("notes %d %v", e.NotesRead, e.NoteRefs)
	}
	if len(e.FactRefs) != 2 || e.NeedsYou != 2 {
		t.Errorf("facts %v, needs you %d", e.FactRefs, e.NeedsYou)
	}
	published := 0
	for _, rc := range res.Receipts {
		if rc.ActorKind != policy.ActorDream || rc.Via != policy.ViaSystem || rc.ActorID != nil {
			t.Errorf("receipt %s %s by %s via %s", rc.Action, rc.ObjectRef, rc.ActorKind, rc.Via)
		}
		if rc.Action == ledger.ActionPublished {
			published++
			if rc.ObjectKind != ledger.ObjectDream || rc.ObjectRef != e.Ref {
				t.Errorf("published receipt is about %s %s", rc.ObjectKind, rc.ObjectRef)
			}
			continue
		}
		if rc.Source == nil || rc.Source.Kind != "dream" || rc.Source.Ref != e.Ref {
			t.Errorf("%s on %s cites %+v, want the edition %s", rc.Action, rc.ObjectRef, rc.Source, e.Ref)
		}
		if strings.Contains(rc.Reason, "River") || strings.Contains(rc.Reason, "receipts in web order") {
			t.Errorf("a receipt holds words: %q", rc.Reason)
		}
	}
	if published != 1 || len(res.Receipts) != 8 {
		t.Errorf("%d receipts, %d published", len(res.Receipts), published)
	}
	// Every action row's receipts exist, in the space, about its object.
	if n := w.f.count(`SELECT count(*) FROM v2.dream_actions a WHERE a.edition_id = $1 AND NOT EXISTS (
	                     SELECT 1 FROM v2.receipts r WHERE r.id = a.receipt_id AND r.space_id = a.space_id
	                       AND r.object_id IN (a.memory_id, a.brief_id))`, e.ID); n != 0 {
		t.Errorf("%d actions without their receipt", n)
	}
	w.f.everyRowReceipted(t)
	// A replay of the same night changes nothing.
	again, err := w.f.l.Apply(context.Background(), &ledger.PublishEdition{Meta: w.dreamMeta("dream:other"), SpaceID: w.space,
		Slot: now.Add(-time.Hour).Truncate(time.Second), Trigger: ledger.DreamScheduled, Until: now, StartedAt: now})
	if err != nil || !again.Unchanged || again.Edition == nil || again.Edition.ID != e.ID {
		t.Fatalf("second run for the slot: %+v %v", again, err)
	}
}

// The edition's words are the memories' own, read live: Forget takes them
// out of the edition too. A memory forgotten while the run planned is
// left alone, and so is an action whose model call read its words.
func TestDreamForget(t *testing.T) {
	t.Parallel()
	w := newDreamWorld(t)
	ctx := context.Background()
	now := time.Now()
	res := w.publish(now.Add(-2*time.Hour).Truncate(time.Second),
		ledger.PlannedAction{Kind: ledger.DreamFold, Memory: w.k.ID, Version: 1, Notes: w.notes[:2]},
		ledger.PlannedAction{Kind: ledger.DreamFade, Memory: w.fd.ID, Version: 1, Unread: &now})
	// Forget after the edition.
	f := w.f
	if r := f.apply(&ledger.Forget{Meta: meta(person(w.owner), f.scope(w.owner), policy.ViaWeb), Memory: w.fd.Ref, ExpectedVersion: 1}); r.Outcome != ledger.OutcomeApplied {
		t.Fatalf("forget: %+v", r.Policy)
	}
	e, err := f.l.GetEdition(ctx, f.scope(w.owner), w.space, res.Edition.Ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range e.Actions {
		if a.Memory.ID == w.fd.ID && (a.Memory.Statement != "" || a.Memory.Lifecycle != lifecycle.Forgotten || a.Undoable) {
			t.Errorf("a forgotten memory reads %q (%s), undoable %v", a.Memory.Statement, a.Memory.Lifecycle, a.Undoable)
		}
	}
	if n := f.count(`SELECT count(*) FROM v2.dream_editions WHERE stats::text ILIKE '%tabs%' OR surfaced::text ILIKE '%tabs%'`); n != 0 {
		t.Errorf("an edition holds the words")
	}
	if _, err := w.undo(e.Actions[1].ID); !errors.Is(err, ledger.ErrUndoRefused) {
		t.Errorf("undo of a fade of a forgotten memory: %v", err)
	}

	// Forget during a run: the plan read y's words; y is forgotten before
	// the edition lands, so what the model wrote from them is dropped.
	if r := f.apply(&ledger.Forget{Meta: meta(person(w.owner), f.scope(w.owner), policy.ViaWeb), Memory: w.y.Ref, ExpectedVersion: 1}); r.Outcome != ledger.OutcomeApplied {
		t.Fatalf("forget: %+v", r.Policy)
	}
	res = w.publish(now.Add(-time.Hour).Truncate(time.Second),
		ledger.PlannedAction{Kind: ledger.DreamPropose, Notes: w.notes[2:], Saw: []uuid.UUID{w.y.ID},
			New: &ledger.DreamFact{Statement: "Deploys go to Railway, for previews.", Section: ledger.SectionConventions}},
		ledger.PlannedAction{Kind: ledger.DreamConflict, Memory: w.x.ID, Version: 1, Related: w.y.ID, RelatedVersion: 1})
	if len(res.Edition.Counts) != 0 || res.Edition.Stats.Skipped[ledger.DreamSkipForgotten] != 2 {
		t.Errorf("applied %v, skipped %v; want both skipped as forgotten", res.Edition.Counts, res.Edition.Stats.Skipped)
	}
	if n := f.count(`SELECT count(*) FROM v2.memory_versions WHERE space_id = $1 AND statement ILIKE '%railway%'`, w.space); n != 0 {
		t.Errorf("forgotten words came back through Dream: %d", n)
	}
}

// Dream can't raise trust: a new fact is no more trusted than Dream's own
// work, whoever wrote the notes, and a note fetched from the web makes it
// external (quarantined).
func TestDreamCannotRaiseTrust(t *testing.T) {
	t.Parallel()
	w := newDreamWorld(t)
	own, web := uuid.New(), uuid.New()
	w.f.exec(`INSERT INTO memories (id, owner_id, hub_id, title, content, created_by_type) VALUES ($1, $2, $3, 'note', 'mine', 'human')`,
		own, w.owner, w.space)
	w.f.exec(`INSERT INTO memories (id, owner_id, hub_id, title, content, source_path) VALUES ($1, $2, $3, 'page', 'web', 'https://example.com/x')`,
		web, w.owner, w.space)
	w.notes = append(w.notes, own, web)
	res := w.publish(time.Now().Add(-time.Hour).Truncate(time.Second),
		ledger.PlannedAction{Kind: ledger.DreamPropose, Notes: []uuid.UUID{own}, New: &ledger.DreamFact{Statement: "Prefers short commit messages.", Section: ledger.SectionPreferences}},
		ledger.PlannedAction{Kind: ledger.DreamPropose, Notes: []uuid.UUID{web}, New: &ledger.DreamFact{Statement: "The API answers in problem+json.", Section: ledger.SectionConventions}})
	e, err := w.f.l.GetEdition(context.Background(), w.f.scope(w.owner), w.space, res.Edition.Ref)
	if err != nil || len(e.Actions) != 2 {
		t.Fatalf("%+v %v", e, err)
	}
	if got := e.Actions[0].Memory; got.Trust != policy.TrustAgentOwnWork || got.Lifecycle != lifecycle.Proposed {
		t.Errorf("a person's note became %s %s, want a proposal of agent_own_work at most", got.Lifecycle, got.Trust)
	}
	if got := e.Actions[1].Memory; got.Trust != policy.TrustExternal {
		t.Errorf("a web page's note became %s, want external", got.Trust)
	}
	m := w.f.mem(w.owner, e.Actions[1].Memory.ID)
	if len(m.Sources) != 1 || m.Sources[0].Kind != ledger.SourceNote || m.Sources[0].Ref != e.Actions[1].NoteRefs[0] {
		t.Errorf("sources %+v", m.Sources)
	}
	// And a person can't keep the quarantined one from the CLI.
	r := w.f.apply(&ledger.Keep{Meta: meta(person(w.owner), w.f.scope(w.owner), policy.ViaCLI), Memory: m.Ref})
	if r.Outcome != ledger.OutcomeRefused || r.Policy.Code != policy.CodeExternalNeedsReview {
		t.Errorf("keep from the CLI: %s %s", r.Outcome, r.Policy.Code)
	}
}

// Undo is refused when a later change depends on the action, when the
// window has passed, and for anyone who can't keep.
func TestDreamUndoRefusals(t *testing.T) {
	t.Parallel()
	w := newDreamWorld(t)
	ctx := context.Background()
	now := time.Now()
	res := w.publish(now.Add(-time.Hour).Truncate(time.Second),
		ledger.PlannedAction{Kind: ledger.DreamFold, Memory: w.k.ID, Version: 1, Notes: w.notes[:2]},
		ledger.PlannedAction{Kind: ledger.DreamFade, Memory: w.fd.ID, Version: 1, Unread: &now},
		ledger.PlannedAction{Kind: ledger.DreamBrief, Brief: &ledger.BriefDelta{BaseVersion: w.brief.Version, Ops: []ledger.BriefOp{
			{Op: ledger.BriefOpPlace, Item: w.x.Ref, Section: "conventions"}}}})
	e, err := w.f.l.GetEdition(ctx, w.f.scope(w.owner), w.space, res.Edition.Ref)
	if err != nil {
		t.Fatal(err)
	}
	fold, fade, brief := e.Actions[0], e.Actions[1], e.Actions[2]
	refused := func(id uuid.UUID, reason string) {
		t.Helper()
		var ue *ledger.UndoError
		if _, err := w.undo(id); !errors.As(err, &ue) || ue.Reason != reason {
			t.Errorf("undo: %v, want %s", err, reason)
		}
	}
	// A person edits k after the fold: the fold can't be undone.
	w.f.apply(&ledger.Edit{Meta: meta(person(w.owner), w.f.scope(w.owner), policy.ViaWeb), Memory: w.k.Ref, ExpectedVersion: 1,
		Statement: "Background jobs run on River."})
	refused(fold.ID, ledger.UndoLaterChanges)
	// The Brief moved on.
	w.f.brief(w.owner, w.space, brief.Brief.Version, demoSections(w.k.Ref))
	refused(brief.ID, ledger.UndoLaterChanges)
	// Viewers and agents can't undo.
	jy := w.f.user("jy")
	w.f.join(w.space, jy, "viewer")
	if r, err := w.f.l.Apply(ctx, &ledger.UndoDreamAction{Meta: meta(person(jy), w.f.scope(jy), policy.ViaWeb), Action: fade.ID}); err != nil || r.Policy.Code != policy.CodeViewer {
		t.Errorf("viewer: %+v %v", r.Policy, err)
	}
	if r, err := w.f.l.Apply(ctx, &ledger.UndoDreamAction{Meta: meta(agentFor(policy.AutonomyWrite), w.f.scope(w.owner), policy.ViaMCP), Action: fade.ID}); err != nil || r.Outcome != ledger.OutcomeRefused {
		t.Errorf("agent: %+v %v", r.Policy, err)
	}
	// Past the window.
	late := ledger.New(w.f.pool, ledger.WithClock(func() time.Time { return now.Add(31 * 24 * time.Hour) }))
	if _, err := late.Apply(ctx, &ledger.UndoDreamAction{Meta: meta(person(w.owner), w.f.scope(w.owner), policy.ViaWeb), Action: fade.ID}); err == nil {
		t.Error("an undo past the window went through")
	} else if ue := (*ledger.UndoError)(nil); !errors.As(err, &ue) || ue.Reason != ledger.UndoWindowPassed {
		t.Errorf("past the window: %v", err)
	}
	// Restore brings a faded memory back any time, as a person's keep.
	r := w.f.apply(&ledger.Restore{Meta: meta(person(w.owner), w.f.scope(w.owner), policy.ViaWeb), Memory: w.fd.Ref})
	if r.Outcome != ledger.OutcomeApplied || r.Memory.Lifecycle != lifecycle.Kept || r.Receipts[0].Action != ledger.ActionRestored {
		t.Fatalf("restore: %+v", r)
	}
	// … and the fade is now behind a later change.
	refused(fade.ID, ledger.UndoLaterChanges)
	// Only Dream publishes.
	p, err := w.f.l.Apply(ctx, &ledger.PublishEdition{Meta: meta(person(w.owner), w.f.scope(w.owner), policy.ViaWeb), SpaceID: w.space,
		Slot: now, Trigger: ledger.DreamScheduled, Until: now, StartedAt: now})
	if err != nil || p.Policy.Code != policy.CodeDreamByDream {
		t.Errorf("a person published: %+v %v", p.Policy, err)
	}
}

// Rule 13: an edition, its actions and note numbers stay in their space.
func TestDreamRowsStayInTheirSpace(t *testing.T) {
	t.Parallel()
	w := newDreamWorld(t)
	ctx := context.Background()
	now := time.Now()
	res := w.publish(now.Add(-time.Hour).Truncate(time.Second),
		ledger.PlannedAction{Kind: ledger.DreamFold, Memory: w.k.ID, Version: 1, Notes: w.notes[:2]})
	jy := w.f.user("jy")
	theirs := w.f.space(jy, policy.SpaceProject, "theirs")
	tenant := w.k.TenantID
	for _, tbl := range []string{"v2.dream_editions", "v2.dream_actions", "v2.note_refs", "v2.dream_schedules"} {
		for _, s := range []struct {
			spaces []uuid.UUID
			want   bool
		}{{[]uuid.UUID{w.space}, tbl != "v2.dream_schedules"}, {[]uuid.UUID{theirs}, false}, {nil, false}} {
			var n int
			if err := w.f.asV2(s.spaces, []uuid.UUID{tenant}, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT count(*) FROM `+tbl).Scan(&n)
			}); err != nil {
				t.Fatal(err)
			}
			if (n > 0) != s.want {
				t.Errorf("%s in scope %v: %d rows", tbl, s.spaces, n)
			}
		}
	}
	if _, err := w.f.l.GetEdition(ctx, w.f.scope(jy), w.space, res.Edition.Ref); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another person reads the edition: %v", err)
	}
	e, _ := w.f.l.GetEdition(ctx, w.f.scope(w.owner), w.space, res.Edition.Ref)
	if _, err := w.f.l.Apply(ctx, &ledger.UndoDreamAction{Meta: meta(person(jy), w.f.scope(jy), policy.ViaWeb), Action: e.Actions[0].ID}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another person undoes it: %v", err)
	}
	// The email log is the sweeper's alone.
	if err := w.f.asV2([]uuid.UUID{w.space}, []uuid.UUID{tenant}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT count(*) FROM v2.dream_email_sends`)
		return err
	}); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("memax_v2 reads the email log: %v", err)
	}
	// memax_v2 writes nothing of these by hand: no receipt, no row.
	err := w.f.asV2([]uuid.UUID{w.space}, []uuid.UUID{tenant}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE v2.dream_actions SET undone_at = now()`)
		return err
	})
	if err == nil {
		t.Error("an action changed without an undo receipt")
	}
}

// ApplyBriefOps keeps the Brief's changes small and cited.
func TestApplyBriefOps(t *testing.T) {
	t.Parallel()
	base := []ledger.BriefSection{
		{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{{Ref: "M-0001"}, {Text: "Why we chose it.", Cites: []string{"M-0001"}}}},
		{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{{Ref: "M-0002"}}},
	}
	kept := func(r string) bool { return r != "M-0009" }
	out, err := ledger.ApplyBriefOps(base, []ledger.BriefOp{
		{Op: ledger.BriefOpMove, Item: "M-0002", Section: "decisions", After: "M-0001"},
		{Op: ledger.BriefOpPlace, Item: "M-0003", Section: "conventions"},
		{Op: ledger.BriefOpReword, Item: "P:decisions:1", Text: "Chosen for durable retries.", Cites: []string{"M-0001", "m-2"}},
	}, kept)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(out)
	want := `[{"key":"decisions","heading":"Decisions","items":[{"ref":"M-0001"},{"ref":"M-0002"},{"text":"Chosen for durable retries.","cites":["M-0001","M-0002"]}]},{"key":"conventions","heading":"Conventions","items":[{"ref":"M-0003"}]}]`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	for _, bad := range [][]ledger.BriefOp{
		{{Op: ledger.BriefOpAdd, Section: "decisions", Text: "An uncited claim."}},
		{{Op: ledger.BriefOpAdd, Section: "decisions", Text: "Cites what isn't kept.", Cites: []string{"M-0009"}}},
		{{Op: ledger.BriefOpPlace, Item: "M-0001", Section: "decisions"}},
		{{Op: ledger.BriefOpMove, Item: "M-0003", Section: "decisions"}},
		{{Op: ledger.BriefOpPlace, Item: "M-0004", Section: "overview"}},
		{{Op: ledger.BriefOpReword, Item: "M-0001", Text: "x", Cites: []string{"M-0001"}}},
		{{Op: "rewrite"}},
	} {
		if _, err := ledger.ApplyBriefOps(base, bad, kept); !errors.Is(err, ledger.ErrInvalid) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	many := make([]ledger.BriefOp, ledger.MaxBriefOps+1)
	for i := range many {
		many[i] = ledger.BriefOp{Op: ledger.BriefOpAdd, Section: "decisions", Text: "Line.", Cites: []string{"M-0001"}}
	}
	if _, err := ledger.ApplyBriefOps(base, many, kept); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("a rewrite went through: %v", err)
	}
}

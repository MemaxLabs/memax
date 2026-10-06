package ledger_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

func TestRememberKeepsAndReceipts(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	when := time.Date(2026, 10, 3, 9, 30, 0, 0, time.UTC)

	m := meta(person(zz), scope, policy.ViaWeb)
	m.OccurredAt = when
	m.SessionRef = "web-1"
	res := f.apply(&ledger.Remember{Meta: m, NewMemory: ledger.NewMemory{
		SpaceID: space, Statement: "  River is our queue, not Kafka.  ", Section: ledger.SectionDecisions,
		Kind: ledger.KindDecision, Decision: &ledger.DecisionFields{Why: "Postgres-backed", Area: "queues", Status: "in_force"},
		Sources: []ledger.SourceInput{{Kind: ledger.SourcePR, Ref: "PR #212", URI: "https://github.com/MemaxLabs/memax/pull/212"}},
	}})

	if res.Outcome != ledger.OutcomeApplied || res.Replayed {
		t.Fatalf("outcome = %s replayed=%v, want applied", res.Outcome, res.Replayed)
	}
	mem := res.Memory
	if mem.Ref != "M-0001" || mem.Statement != "River is our queue, not Kafka." || mem.Version != 1 {
		t.Errorf("memory = %s %q v%d", mem.Ref, mem.Statement, mem.Version)
	}
	if mem.Lifecycle != lifecycle.Kept || mem.State != lifecycle.MarkKept || len(mem.Flags) != 0 {
		t.Errorf("state = %s/%s/%v, want kept", mem.Lifecycle, mem.State, mem.Flags)
	}
	// A person citing a PR: trust is the minimum of person and repository.
	if mem.Trust != policy.TrustRepository {
		t.Errorf("trust = %s, want repository", mem.Trust)
	}
	if mem.Decision == nil || mem.Decision.Area != "queues" || mem.Kind != ledger.KindDecision {
		t.Errorf("decision fields = %+v", mem.Decision)
	}
	if len(mem.Sources) != 1 || mem.Sources[0].Ref != "PR #212" || mem.Sources[0].External {
		t.Errorf("sources = %+v", mem.Sources)
	}
	if len(res.Receipts) != 1 {
		t.Fatalf("receipts = %d, want 1", len(res.Receipts))
	}
	rc := res.Receipts[0]
	if rc.Action != ledger.ActionKept || rc.ActorKind != policy.ActorPerson || rc.ActorID == nil || *rc.ActorID != zz {
		t.Errorf("receipt actor/action = %s %s %v", rc.Action, rc.ActorKind, rc.ActorID)
	}
	if rc.ObjectRef != "M-0001" || rc.ObjectID != mem.ID || rc.StreamID != mem.ID || rc.StreamVersion != 1 {
		t.Errorf("receipt object = %s %s v%d", rc.ObjectRef, rc.ObjectID, rc.StreamVersion)
	}
	if rc.Assurance != policy.AssuranceHumanWeb || rc.Via != policy.ViaWeb || rc.SessionRef != "web-1" {
		t.Errorf("receipt assurance/via/session = %s %s %s", rc.Assurance, rc.Via, rc.SessionRef)
	}
	if !rc.OccurredAt.Equal(when) || !rc.RecordedAt.After(when) {
		t.Errorf("occurred %v recorded %v: occurred_at must keep the client time", rc.OccurredAt, rc.RecordedAt)
	}
	if rc.Source == nil || rc.Source.Ref != "PR #212" {
		t.Errorf("receipt source = %+v", rc.Source)
	}
	if mem.CreatedReceiptID != rc.ID || mem.LastReceiptID != rc.ID {
		t.Errorf("projection receipts = %s/%s, want %s", mem.CreatedReceiptID, mem.LastReceiptID, rc.ID)
	}
	if v, _ := uuid.Parse(mem.ID.String()); v.Version() != 7 {
		t.Errorf("memory id %s is not a uuidv7", mem.ID)
	}

	got, err := f.l.GetMemory(context.Background(), scope, "m-1")
	if err != nil || got.ID != mem.ID || got.Statement != mem.Statement || len(got.Sources) != 1 {
		t.Fatalf("GetMemory(m-1) = %+v, %v", got, err)
	}
	byID, err := f.l.GetMemory(context.Background(), scope, mem.ID.String())
	if err != nil || byID.Ref != "M-0001" {
		t.Fatalf("GetMemory(uuid) = %+v, %v", byID, err)
	}
	page, err := f.l.ListReceipts(context.Background(), scope, ledger.ReceiptQuery{SpaceID: space})
	if err != nil || len(page.Receipts) != 1 || page.Receipts[0].ID != rc.ID {
		t.Fatalf("ListReceipts = %+v, %v", page, err)
	}
}

// Rule 2: agents propose, people keep; Read writes nothing; the receipt
// names the agent. Rule 3: an external source at Write still proposes.
// Rule 12: viewers propose and can't keep. API keys never keep.
func TestPolicyEndToEnd(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	jy := f.user("jy")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(space, jy, "viewer")
	owner := f.scope(zz)
	viewer := f.scope(jy)

	type want struct {
		outcome   ledger.Outcome
		code      string
		lifecycle lifecycle.Lifecycle
		trust     policy.Trust
	}
	keyActor := person(zz)
	keyActor.Credential, keyActor.Autonomy = policy.CredentialAPIKey, policy.AutonomyPropose
	present := agentFor(policy.AutonomyPropose)
	present.PersonPresent, present.CanElicit = true, true

	cases := []struct {
		name    string
		actor   ledger.Actor
		scope   ledger.Scope
		via     policy.Via
		sources []ledger.SourceInput
		want    want
	}{
		{"agent at read is refused", agentFor(policy.AutonomyRead), owner, policy.ViaMCP, nil,
			want{ledger.OutcomeRefused, policy.CodeReadOnly, "", ""}},
		{"agent at propose proposes", agentFor(policy.AutonomyPropose), owner, policy.ViaMCP, nil,
			want{ledger.OutcomeProposed, policy.CodeAutonomyPropose, lifecycle.Proposed, policy.TrustAgentOwnWork}},
		{"agent at write keeps", agentFor(policy.AutonomyWrite), owner, policy.ViaMCP, nil,
			want{ledger.OutcomeApplied, "", lifecycle.Kept, policy.TrustAgentOwnWork}},
		{"external source at write proposes", agentFor(policy.AutonomyWrite), owner, policy.ViaMCP,
			[]ledger.SourceInput{{Kind: ledger.SourceURL, Ref: "blog post", URI: "https://example.com/post", Trust: policy.TrustPerson}},
			want{ledger.OutcomeProposed, policy.CodeExternalSource, lifecycle.Proposed, policy.TrustExternal}},
		{"person present confirms in the agent", present, owner, policy.ViaMCP, nil,
			want{ledger.OutcomeNeedsConfirmation, policy.CodeConfirm, lifecycle.Proposed, policy.TrustAgentOwnWork}},
		{"API key proposes", keyActor, owner, policy.ViaAPI, nil,
			want{ledger.OutcomeProposed, policy.CodeAPIKey, lifecycle.Proposed, policy.TrustPerson}},
		{"viewer proposes", person(jy), viewer, policy.ViaWeb, nil,
			want{ledger.OutcomeProposed, policy.CodeViewer, lifecycle.Proposed, policy.TrustPerson}},
		{"email is proposed and external", person(zz), owner, policy.ViaEmail, nil,
			want{ledger.OutcomeProposed, policy.CodeIntegration, lifecycle.Proposed, policy.TrustExternal}},
	}
	for _, c := range cases {
		before := f.count(`SELECT count(*) FROM v2.receipts`)
		m := meta(c.actor, c.scope, c.via)
		nm := fact(space, "Statement for: "+c.name)
		nm.Sources = c.sources
		res := f.apply(&ledger.Propose{Meta: m, NewMemory: nm})
		if res.Outcome != c.want.outcome || res.Policy.Code != c.want.code {
			t.Errorf("%s: outcome %s/%s, want %s/%s (%s)", c.name, res.Outcome, res.Policy.Code, c.want.outcome, c.want.code, res.Policy.Message)
			continue
		}
		after := f.count(`SELECT count(*) FROM v2.receipts`)
		if c.want.outcome == ledger.OutcomeRefused {
			if res.Memory != nil || after != before {
				t.Errorf("%s: refused but wrote %d receipts", c.name, after-before)
			}
			if res.Policy.Message == "" {
				t.Errorf("%s: refusal without a message", c.name)
			}
			continue
		}
		if after != before+1 {
			t.Errorf("%s: wrote %d receipts, want 1", c.name, after-before)
		}
		if res.Memory.Lifecycle != c.want.lifecycle || res.Memory.Trust != c.want.trust {
			t.Errorf("%s: memory %s trust %s, want %s trust %s", c.name, res.Memory.Lifecycle, res.Memory.Trust, c.want.lifecycle, c.want.trust)
		}
		rc := res.Receipts[0]
		if rc.ActorKind != c.actor.Kind || rc.ActorID == nil || *rc.ActorID != c.actor.ID || rc.Agent != c.actor.Agent {
			t.Errorf("%s: receipt names %s %v %q, want the actor", c.name, rc.ActorKind, rc.ActorID, rc.Agent)
		}
		if c.want.trust == policy.TrustExternal && !res.Policy.Quarantine {
			t.Errorf("%s: external content not quarantined", c.name)
		}
	}

	// The confirmation flow: the person accepts in the agent → a Keep by
	// the person, via MCP, through the agent, client-attested.
	prop := f.apply(&ledger.Propose{Meta: meta(present, owner, policy.ViaMCP), NewMemory: fact(space, "Use pnpm, not npm.")})
	if prop.Outcome != ledger.OutcomeNeedsConfirmation || prop.Memory.Lifecycle != lifecycle.Proposed {
		t.Fatalf("propose with person present = %s/%s", prop.Outcome, prop.Memory.Lifecycle)
	}
	confirmer := person(zz)
	confirmer.Agent = "claude-code"
	kept := f.apply(&ledger.Keep{Meta: meta(confirmer, owner, policy.ViaMCP), Memory: prop.Memory.Ref, ExpectedVersion: 1})
	if kept.Outcome != ledger.OutcomeApplied || kept.Memory.Lifecycle != lifecycle.Kept {
		t.Fatalf("confirmation keep = %s/%s (%s)", kept.Outcome, kept.Memory.Lifecycle, kept.Policy.Message)
	}
	krc := kept.Receipts[0]
	if krc.ActorKind != policy.ActorPerson || krc.Agent != "claude-code" || krc.Assurance != policy.AssuranceClientAttested || krc.StreamVersion != 2 {
		t.Errorf("confirmation receipt = %s via %q assurance %s v%d", krc.ActorKind, krc.Agent, krc.Assurance, krc.StreamVersion)
	}

	// Keep is for people who can keep.
	another := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), owner, policy.ViaMCP), NewMemory: fact(space, "Tabs, not spaces.")})
	for _, c := range []struct {
		name  string
		actor ledger.Actor
		scope ledger.Scope
		code  string
	}{
		{"API key can't keep", keyActor, owner, policy.CodeKeyCannotReview},
		{"viewer can't keep", person(jy), viewer, policy.CodeViewer},
		{"agent at write can't keep", agentFor(policy.AutonomyWrite), owner, policy.CodePersonMustReview},
	} {
		res := f.apply(&ledger.Keep{Meta: meta(c.actor, c.scope, policy.ViaWeb), Memory: another.Memory.Ref})
		if res.Outcome != ledger.OutcomeRefused || res.Policy.Code != c.code {
			t.Errorf("%s: %s/%s, want refused/%s", c.name, res.Outcome, res.Policy.Code, c.code)
		}
	}
	still, err := f.l.GetMemory(context.Background(), owner, another.Memory.Ref)
	if err != nil || still.Lifecycle != lifecycle.Proposed {
		t.Fatalf("after refused keeps: %v %v", still, err)
	}

	// External content can't be kept in the agent, only on the web.
	ext := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyWrite), owner, policy.ViaMCP), NewMemory: ledger.NewMemory{
		SpaceID: space, Statement: "The API rate limit is 80 writes a minute.", Section: ledger.SectionConventions,
		Sources: []ledger.SourceInput{{Kind: ledger.SourceIssue, Ref: "issue #9"}},
	}})
	if res := f.apply(&ledger.Keep{Meta: meta(confirmer, owner, policy.ViaMCP), Memory: ext.Memory.Ref}); res.Policy.Code != policy.CodeExternalNeedsReview {
		t.Errorf("external keep in agent: %s/%s", res.Outcome, res.Policy.Code)
	}
	if res := f.apply(&ledger.Keep{Meta: meta(person(zz), owner, policy.ViaReview), Memory: ext.Memory.Ref}); res.Outcome != ledger.OutcomeApplied {
		t.Errorf("external keep in Review: %s/%s", res.Outcome, res.Policy.Code)
	}
}

func TestKeepRejectAndTransitions(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	ctx := context.Background()
	propose := func(s string) *ledger.Memory {
		return f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP), NewMemory: fact(space, s)}).Memory
	}

	a := propose("Fly runs in sjc.")
	m := meta(person(zz), scope, policy.ViaReview)
	m.Reason = "duplicate of the region note"
	rej := f.apply(&ledger.Reject{Meta: m, Memory: a.Ref, ExpectedVersion: 1})
	if rej.Outcome != ledger.OutcomeApplied || rej.Memory.Lifecycle != lifecycle.Rejected || rej.Memory.State != lifecycle.MarkRejected {
		t.Fatalf("reject = %s/%s", rej.Outcome, rej.Memory.Lifecycle)
	}
	if rej.Receipts[0].Action != ledger.ActionRejected || rej.Receipts[0].Reason != "duplicate of the region note" || rej.Receipts[0].Assurance != "" {
		t.Errorf("reject receipt = %+v", rej.Receipts[0])
	}

	// Disallowed transitions are typed errors with a readable message.
	_, err := f.l.Apply(ctx, &ledger.Keep{Meta: meta(person(zz), scope, policy.ViaReview), Memory: a.Ref})
	var te *ledger.TransitionError
	if !errors.Is(err, ledger.ErrInvalidTransition) || !errors.As(err, &te) || te.Ref != a.Ref {
		t.Fatalf("keep a rejected memory: %v", err)
	}

	b := propose("Neon is in US-West.")
	if _, err := f.l.Apply(ctx, &ledger.Keep{Meta: meta(person(zz), scope, policy.ViaReview), Memory: b.Ref, ExpectedVersion: 3}); !errors.Is(err, ledger.ErrEditClash) {
		t.Errorf("keep with a stale version: %v, want ErrEditClash", err)
	}
	kept := f.apply(&ledger.Keep{Meta: meta(person(zz), scope, policy.ViaReview), Memory: b.Ref})
	if kept.Memory.Lifecycle != lifecycle.Kept || kept.Receipts[0].StreamVersion != 2 {
		t.Fatalf("keep = %s v%d", kept.Memory.Lifecycle, kept.Receipts[0].StreamVersion)
	}
	if _, err := f.l.Apply(ctx, &ledger.Reject{Meta: meta(person(zz), scope, policy.ViaReview), Memory: b.Ref}); !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Errorf("reject a kept memory: %v", err)
	}
	if _, err := f.l.Apply(ctx, &ledger.Keep{Meta: meta(person(zz), scope, policy.ViaReview), Memory: "M-0999"}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("keep a missing memory: %v", err)
	}
	if _, err := f.l.Apply(ctx, &ledger.Keep{Meta: meta(person(zz), scope, policy.ViaReview), Memory: "N-0001"}); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("keep a note ref: %v", err)
	}

	// A conflict-flagged proposal can't be kept until it's resolved; the
	// SQL guard and the Go table agree. Set the flag directly with a
	// receipt (flag_conflict is a later command).
	c := propose("Deploy on Fridays.")
	f.flag(c, "conflict")
	_, err = f.l.Apply(ctx, &ledger.Keep{Meta: meta(person(zz), scope, policy.ViaReview), Memory: c.Ref})
	if !errors.As(err, &te) {
		t.Fatalf("keep a conflicted proposal: %v", err)
	}
	if rejected := f.apply(&ledger.Reject{Meta: meta(person(zz), scope, policy.ViaReview), Memory: c.Ref}); len(rejected.Memory.Flags) != 0 {
		t.Errorf("rejecting clears flags; got %v", rejected.Memory.Flags)
	}

	history, err := f.l.ListReceipts(ctx, scope, ledger.ReceiptQuery{SpaceID: space, ObjectID: b.ID})
	if err != nil || len(history.Receipts) != 2 || history.Receipts[0].Action != ledger.ActionKept || history.Receipts[1].Action != ledger.ActionProposed {
		t.Fatalf("history of %s = %+v, %v", b.Ref, history, err)
	}
}

// flag sets a flag on a memory with a receipt, the way a later
// flag_conflict / flag_stale command will.
func (f *fixture) flag(m *ledger.Memory, flag string) {
	f.t.Helper()
	f.change(m, `flags = array_append(flags, $2)`, flag)
}

// fade moves a kept memory to faded with a receipt, as Dream will.
func (f *fixture) fade(m *ledger.Memory) {
	f.t.Helper()
	f.change(m, `lifecycle = $2`, "faded")
}

// change applies `set` (with $2 = arg) to a memory together with a
// receipt, standing in for commands later epics add.
func (f *fixture) change(m *ledger.Memory, set string, arg any) {
	f.t.Helper()
	err := f.asV2([]uuid.UUID{m.SpaceID}, []uuid.UUID{m.TenantID}, func(tx pgx.Tx) error {
		ctx := context.Background()
		rid := uuid.Must(uuid.NewV7())
		var version int
		if err := tx.QueryRow(ctx, `SELECT stream_version + 1 FROM v2.memories WHERE id = $1`, m.ID).Scan(&version); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, via, occurred_at, stream_id, stream_version)
			VALUES ($1, $2, $3, 'memory', $4, $5, 'flagged', 'memax', 'system', now(), $4, $6)`,
			rid, m.TenantID, m.SpaceID, m.ID, m.Ref, version); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE v2.memories SET `+set+`, stream_version = $3, last_receipt_id = $4 WHERE id = $1`,
			m.ID, arg, version, rid)
		return err
	})
	if err != nil {
		f.t.Fatalf("change %s: %v", set, err)
	}
}

func TestEdit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	ctx := context.Background()

	m := f.remember(zz, space, "Use Go 1.24.")
	em := meta(person(zz), scope, policy.ViaWeb)
	em.Reason = "go.mod moved on"
	edited := f.apply(&ledger.Edit{Meta: em, Memory: m.Ref, ExpectedVersion: 1, Statement: "Use Go 1.25.", Section: ledger.SectionPreferences})
	if edited.Outcome != ledger.OutcomeApplied || edited.Memory.Version != 2 || edited.Memory.Statement != "Use Go 1.25." || edited.Memory.Section != ledger.SectionPreferences {
		t.Fatalf("edit = %s v%d %q %s", edited.Outcome, edited.Memory.Version, edited.Memory.Statement, edited.Memory.Section)
	}
	if rc := edited.Receipts[0]; rc.Action != ledger.ActionEdited || rc.StreamVersion != 2 || rc.Reason != "go.mod moved on" {
		t.Errorf("edit receipt = %+v", rc)
	}
	if n := f.count(`SELECT count(*) FROM v2.memory_versions WHERE memory_id = $1`, m.ID); n != 2 {
		t.Errorf("versions = %d, want 2 (history is kept)", n)
	}

	// Edit clash: someone edits from version 1 after the change above.
	_, err := f.l.Apply(ctx, &ledger.Edit{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref, ExpectedVersion: 1, Statement: "Use Go 1.26."})
	var clash *ledger.EditClashError
	if !errors.As(err, &clash) || !errors.Is(err, ledger.ErrEditClash) || clash.Expected != 1 || clash.Current != 2 || clash.Ref != m.Ref {
		t.Fatalf("stale edit: %v, want *EditClashError{1, 2}", err)
	}
	if _, err := f.l.Apply(ctx, &ledger.Edit{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref, Statement: "x"}); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("edit without an expected version: %v", err)
	}
	if _, err := f.l.Apply(ctx, &ledger.Edit{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: m.Ref, ExpectedVersion: 2, Statement: " Use Go 1.25. "}); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("edit that changes nothing: %v", err)
	}

	// Review's "edit and keep" on a proposal: two receipts, kept.
	prop := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP), NewMemory: fact(space, "Lint before commit.")}).Memory
	ek := f.apply(&ledger.Edit{Meta: meta(person(zz), scope, policy.ViaReview), Memory: prop.Ref, ExpectedVersion: 1,
		Statement: "Run pnpm format and lint before every commit.", Keep: true})
	if ek.Memory.Lifecycle != lifecycle.Kept || len(ek.Receipts) != 2 || ek.Receipts[0].Action != ledger.ActionEdited || ek.Receipts[1].Action != ledger.ActionKept {
		t.Fatalf("edit+keep = %s %d receipts", ek.Memory.Lifecycle, len(ek.Receipts))
	}
	if ek.Receipts[1].StreamVersion != 3 || ek.Receipts[1].Assurance != policy.AssuranceHumanWeb {
		t.Errorf("keep receipt = v%d %s", ek.Receipts[1].StreamVersion, ek.Receipts[1].Assurance)
	}

	// An agent at Write may edit its own kept work…
	writer := agentFor(policy.AutonomyWrite)
	own := f.apply(&ledger.Propose{Meta: meta(writer, scope, policy.ViaMCP), NewMemory: fact(space, "Cache recall for 60 s.")}).Memory
	if own.Lifecycle != lifecycle.Kept {
		t.Fatalf("write agent's memory = %s", own.Lifecycle)
	}
	ownEdit := f.apply(&ledger.Edit{Meta: meta(writer, scope, policy.ViaMCP), Memory: own.Ref, ExpectedVersion: 1, Statement: "Cache recall for 30 s."})
	if ownEdit.Outcome != ledger.OutcomeApplied || ownEdit.Memory.Version != 2 {
		t.Fatalf("agent edit of its own work = %s", ownEdit.Outcome)
	}

	// …but its edit of a memory a person kept becomes a new proposal that
	// supersedes it, and the kept memory is untouched.
	sup := f.apply(&ledger.Edit{Meta: meta(writer, scope, policy.ViaMCP), Memory: m.Ref, ExpectedVersion: 2, Statement: "Use Go 1.26."})
	if sup.Outcome != ledger.OutcomeProposed || sup.Policy.Code != policy.CodeEditsPersonKept {
		t.Fatalf("agent edit of person-kept = %s/%s", sup.Outcome, sup.Policy.Code)
	}
	if sup.Memory.ID == m.ID || sup.Memory.Lifecycle != lifecycle.Proposed || sup.Memory.Statement != "Use Go 1.26." {
		t.Errorf("successor = %+v", sup.Memory)
	}
	if n := f.count(`SELECT count(*) FROM v2.memory_links WHERE kind = 'supersedes' AND from_memory_id = $1 AND to_memory_id = $2`, sup.Memory.ID, m.ID); n != 1 {
		t.Errorf("supersedes links = %d, want 1", n)
	}
	orig, err := f.l.GetMemory(ctx, scope, m.Ref)
	if err != nil || orig.Version != 2 || orig.Statement != "Use Go 1.25." || orig.Lifecycle != lifecycle.Kept {
		t.Errorf("original after downgraded edit = %+v, %v", orig, err)
	}
	if sup.Receipts[0].Source == nil || sup.Receipts[0].Source.Ref != m.Ref {
		t.Errorf("successor receipt should cite %s: %+v", m.Ref, sup.Receipts[0].Source)
	}

	// Editing a stale memory is Verify's "update": the flag clears.
	f.flag(own, "stale")
	fresh := f.apply(&ledger.Edit{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: own.Ref, ExpectedVersion: 2, Statement: "Cache recall for 45 s."})
	if fresh.Memory.State != lifecycle.MarkKept || slices.Contains(fresh.Memory.Flags, lifecycle.Stale) {
		t.Errorf("edit of a stale memory: state %s flags %v", fresh.Memory.State, fresh.Memory.Flags)
	}

	// A faded memory can't be edited, in place or by a superseding
	// proposal.
	faded := f.remember(zz, space, "Old convention.")
	f.fade(faded)
	before := f.count(`SELECT count(*) FROM v2.memories`)
	for _, actor := range []ledger.Actor{person(zz), writer} {
		_, err := f.l.Apply(ctx, &ledger.Edit{Meta: meta(actor, scope, policy.ViaWeb), Memory: faded.Ref, ExpectedVersion: 1, Statement: "Revived."})
		if !errors.Is(err, ledger.ErrInvalidTransition) {
			t.Errorf("%s edits a faded memory: %v, want ErrInvalidTransition", actor.Kind, err)
		}
	}
	if after := f.count(`SELECT count(*) FROM v2.memories`); after != before {
		t.Errorf("editing a faded memory wrote %d memories", after-before)
	}
}

func TestIdempotentReplay(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	ctx := context.Background()

	m := meta(person(zz), scope, policy.ViaCLI)
	m.IdempotencyKey = "offline-queue-0001"
	m.OccurredAt = time.Now().Add(-48 * time.Hour)
	cmd := &ledger.Remember{Meta: m, NewMemory: fact(space, "Squash migrations quarterly.")}
	first := f.apply(cmd)
	receipts := f.count(`SELECT count(*) FROM v2.receipts`)

	again := *cmd
	again.OccurredAt = time.Time{} // a retry may not resend the client time
	second := f.apply(&again)
	if !second.Replayed || second.Outcome != first.Outcome || second.Memory.ID != first.Memory.ID {
		t.Fatalf("replay = %+v", second)
	}
	if len(second.Receipts) != 1 || second.Receipts[0].ID != first.Receipts[0].ID {
		t.Errorf("replay receipts = %v, want the original", second.Receipts)
	}
	if !second.Receipts[0].OccurredAt.Equal(first.Receipts[0].OccurredAt) {
		t.Errorf("replay changed occurred_at")
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts`); n != receipts {
		t.Errorf("replay wrote %d receipts", n-receipts)
	}
	if n := f.count(`SELECT count(*) FROM v2.memories`); n != 1 {
		t.Errorf("replay wrote a memory: %d", n)
	}

	// Same key, different content: refused, nothing written.
	changed := *cmd
	changed.Statement = "Squash migrations monthly."
	if _, err := f.l.Apply(ctx, &changed); !errors.Is(err, ledger.ErrIdempotencyKeyReused) {
		t.Errorf("key reused with new content: %v", err)
	}
	// Keys are per actor: another actor's identical key is its own.
	other := meta(agentFor(policy.AutonomyPropose), scope, policy.ViaCLI)
	other.IdempotencyKey = m.IdempotencyKey
	if res := f.apply(&ledger.Remember{Meta: other, NewMemory: fact(space, "Squash migrations quarterly.")}); res.Replayed {
		t.Error("another actor's key replayed this actor's command")
	}

	// A refusal isn't stored: the same command is decided again later.
	reader := agentFor(policy.AutonomyRead)
	rm := meta(reader, scope, policy.ViaMCP)
	refusedCmd := &ledger.Propose{Meta: rm, NewMemory: fact(space, "Agents read the Brief first.")}
	if res := f.apply(refusedCmd); res.Outcome != ledger.OutcomeRefused {
		t.Fatalf("read agent: %s", res.Outcome)
	}
	refusedCmd.Actor.Autonomy = policy.AutonomyPropose // the person raised it in Agents
	if res := f.apply(refusedCmd); res.Outcome != ledger.OutcomeProposed || res.Replayed {
		t.Errorf("retry after raising autonomy: %s replayed=%v", res.Outcome, res.Replayed)
	}

	// Replays of review commands too.
	prop := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP), NewMemory: fact(space, "One PR per feature.")}).Memory
	km := meta(person(zz), scope, policy.ViaReview)
	keep := &ledger.Keep{Meta: km, Memory: prop.Ref}
	k1 := f.apply(keep)
	k2 := f.apply(keep)
	if !k2.Replayed || k2.Receipts[0].ID != k1.Receipts[0].ID || k2.Memory.Lifecycle != lifecycle.Kept {
		t.Errorf("keep replay = %+v", k2)
	}
}

// Concurrent retries of one command (a flaky network, two daemons)
// produce one memory: the losers wait on the key and replay.
func TestIdempotentReplayConcurrent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)

	const n = 8
	m := meta(person(zz), scope, policy.ViaCLI)
	results := make(chan ledger.Result, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			cmd := &ledger.Remember{Meta: m, NewMemory: fact(space, "Keep receipts content-free.")}
			res, err := f.l.Apply(context.Background(), cmd)
			results <- res
			errs <- err
		}()
	}
	ids := map[uuid.UUID]int{}
	replays := 0
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent apply: %v", err)
		}
		res := <-results
		ids[res.Memory.ID]++
		if res.Replayed {
			replays++
		}
	}
	if len(ids) != 1 || replays != n-1 {
		t.Fatalf("got %d memories and %d replays, want 1 and %d", len(ids), replays, n-1)
	}
	if c := f.count(`SELECT count(*) FROM v2.memories`); c != 1 {
		t.Errorf("memories = %d", c)
	}
}

func TestTrustIsTheMinimumOfSources(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)

	cases := []struct {
		name    string
		actor   ledger.Actor
		sources []ledger.SourceInput
		want    policy.Trust
		srcWant []policy.Trust
	}{
		{"a person's own words", person(zz), nil, policy.TrustPerson, nil},
		{"a person citing a file", person(zz), []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: "go.mod:14"}},
			policy.TrustRepository, []policy.Trust{policy.TrustRepository}},
		{"an agent's session", agentFor(policy.AutonomyWrite), []ledger.SourceInput{{Kind: ledger.SourceSession, Ref: "cc-7f3a"}},
			policy.TrustAgentOwnWork, []policy.Trust{policy.TrustAgentOwnWork}},
		{"an agent can't claim a person source", agentFor(policy.AutonomyWrite), []ledger.SourceInput{{Kind: ledger.SourceSession, Ref: "cc-1", Trust: policy.TrustPerson}},
			policy.TrustAgentOwnWork, []policy.Trust{policy.TrustAgentOwnWork}},
		{"a URL is external whatever it claims", person(zz), []ledger.SourceInput{{Kind: ledger.SourceURL, Ref: "docs", URI: "https://example.com", Trust: policy.TrustPerson}},
			policy.TrustExternal, []policy.Trust{policy.TrustExternal}},
		{"one external source taints the rest", person(zz), []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: "a.go:1"}, {Kind: ledger.SourceEmail, Ref: "mail from ops"}},
			policy.TrustExternal, []policy.Trust{policy.TrustRepository, policy.TrustExternal}},
	}
	for _, c := range cases {
		nm := fact(space, "Trust case: "+c.name)
		nm.Sources = c.sources
		res := f.apply(&ledger.Remember{Meta: meta(c.actor, scope, policy.ViaWeb), NewMemory: nm})
		if res.Memory == nil || res.Memory.Trust != c.want {
			t.Errorf("%s: trust %v, want %s", c.name, res.Memory, c.want)
			continue
		}
		for i, s := range res.Memory.Sources {
			if s.Trust != c.srcWant[i] || s.External != (c.srcWant[i] == policy.TrustExternal) {
				t.Errorf("%s: source %d trust %s external %v, want %s", c.name, i, s.Trust, s.External, c.srcWant[i])
			}
		}
	}
}

func TestSecretsAreRefusedBeforeStorage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)

	nm := fact(space, "The staging key is AKIAABCDEFGHIJKLMNOP.")
	res := f.apply(&ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: nm})
	if res.Outcome != ledger.OutcomeRefused || res.Policy.Code != policy.CodeSecret {
		t.Fatalf("secret statement: %s/%s", res.Outcome, res.Policy.Code)
	}
	quoted := fact(space, "Staging uses a separate AWS account.")
	quoted.Sources = []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: ".env:3", Quote: "AWS_KEY=AKIAABCDEFGHIJKLMNOP"}}
	if res := f.apply(&ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: quoted}); res.Policy.Code != policy.CodeSecret {
		t.Errorf("secret in a quote: %s/%s", res.Outcome, res.Policy.Code)
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts`) + f.count(`SELECT count(*) FROM v2.memory_versions`); n != 0 {
		t.Errorf("a refused secret left %d rows", n)
	}
}

func TestListMemories(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	ctx := context.Background()

	for _, s := range []string{"one", "two", "three"} {
		f.remember(zz, space, "Kept "+s)
	}
	var proposals []*ledger.Memory
	for _, s := range []string{"four", "five"} {
		proposals = append(proposals, f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP),
			NewMemory: ledger.NewMemory{SpaceID: space, Statement: "Proposed " + s, Section: ledger.SectionOpenQuestion}}).Memory)
	}
	f.apply(&ledger.Reject{Meta: meta(person(zz), scope, policy.ViaReview), Memory: proposals[0].Ref})

	all, err := f.l.ListMemories(ctx, scope, ledger.MemoryQuery{SpaceID: space})
	if err != nil || len(all.Memories) != 4 || all.HasMore {
		t.Fatalf("default list = %d (rejected hidden), %v", len(all.Memories), err)
	}
	if all.Memories[0].Ref != "M-0005" || all.Memories[3].Ref != "M-0001" {
		t.Errorf("order = %s … %s, want newest first", all.Memories[0].Ref, all.Memories[3].Ref)
	}
	review, err := f.l.ListMemories(ctx, scope, ledger.MemoryQuery{SpaceID: space, States: []lifecycle.Mark{lifecycle.MarkProposed}})
	if err != nil || len(review.Memories) != 1 || review.Memories[0].Ref != "M-0005" {
		t.Errorf("proposed = %+v, %v", review, err)
	}
	rejected, err := f.l.ListMemories(ctx, scope, ledger.MemoryQuery{SpaceID: space, States: []lifecycle.Mark{lifecycle.MarkRejected}})
	if err != nil || len(rejected.Memories) != 1 {
		t.Errorf("rejected = %+v, %v", rejected, err)
	}
	open, err := f.l.ListMemories(ctx, scope, ledger.MemoryQuery{SpaceID: space, Sections: []ledger.Section{ledger.SectionOpenQuestion}})
	if err != nil || len(open.Memories) != 1 {
		t.Errorf("open questions = %+v, %v", open, err)
	}

	var refs []string
	cursor := ""
	for pages := 0; pages < 5; pages++ {
		p, err := f.l.ListMemories(ctx, scope, ledger.MemoryQuery{SpaceID: space, Limit: 3, Cursor: cursor})
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		for _, m := range p.Memories {
			refs = append(refs, m.Ref)
		}
		if !p.HasMore {
			break
		}
		cursor = p.NextCursor
	}
	if !slices.Equal(refs, []string{"M-0005", "M-0003", "M-0002", "M-0001"}) {
		t.Errorf("paged refs = %v", refs)
	}
	if _, err := f.l.ListMemories(ctx, scope, ledger.MemoryQuery{SpaceID: space, Cursor: "garbage"}); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("bad cursor: %v", err)
	}
	rcPage, err := f.l.ListReceipts(ctx, scope, ledger.ReceiptQuery{SpaceID: space, Limit: 2})
	if err != nil || !rcPage.HasMore {
		t.Fatalf("receipts page = %+v, %v", rcPage, err)
	}
	if _, err := f.l.ListMemories(ctx, scope, ledger.MemoryQuery{SpaceID: space, Cursor: rcPage.NextCursor}); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("a receipts cursor on memories: %v", err)
	}
	if _, err := f.l.ListMemories(ctx, scope, ledger.MemoryQuery{SpaceID: uuid.New()}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("space outside the scope: %v", err)
	}
}

func TestValidation(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	ok := func() *ledger.Remember {
		return &ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: fact(space, "Fine.")}
	}
	cases := map[string]func(*ledger.Remember){
		"no idempotency key": func(c *ledger.Remember) { c.IdempotencyKey = "" },
		"no via":             func(c *ledger.Remember) { c.Via = "" },
		"unknown actor":      func(c *ledger.Remember) { c.Actor.Kind = "robot" },
		"person without id":  func(c *ledger.Remember) { c.Actor.ID = uuid.Nil },
		"empty statement":    func(c *ledger.Remember) { c.Statement = "   " },
		"long statement":     func(c *ledger.Remember) { c.Statement = string(make([]rune, ledger.MaxStatementRunes+1)) },
		"NUL byte":           func(c *ledger.Remember) { c.Statement = "a\x00b" },
		"no section":         func(c *ledger.Remember) { c.Section = "" },
		"bad kind":           func(c *ledger.Remember) { c.Kind = "rule" },
		"decision on a fact": func(c *ledger.Remember) { c.Decision = &ledger.DecisionFields{Why: "x"} },
		"conditions object":  func(c *ledger.Remember) { c.Conditions = []byte(`{"a":1}`) },
		"bad source kind":    func(c *ledger.Remember) { c.Sources = []ledger.SourceInput{{Kind: "tweet", Ref: "x"}} },
		"source without ref": func(c *ledger.Remember) { c.Sources = []ledger.SourceInput{{Kind: ledger.SourceFile}} },
		"future occurred_at": func(c *ledger.Remember) { c.OccurredAt = time.Now().Add(time.Hour) },
		"no space":           func(c *ledger.Remember) { c.SpaceID = uuid.Nil },
		"bad decision status": func(c *ledger.Remember) {
			c.Kind, c.Decision = ledger.KindDecision, &ledger.DecisionFields{Status: "maybe"}
		},
		"option without label": func(c *ledger.Remember) {
			c.Kind, c.Decision = ledger.KindDecision, &ledger.DecisionFields{Options: []ledger.DecisionOption{{Detail: "x"}}}
		},
		"NUL in decision": func(c *ledger.Remember) {
			c.Kind, c.Decision = ledger.KindDecision, &ledger.DecisionFields{Why: "a\x00b"}
		},
		// Valid JSON, but jsonb can't store \u0000: the database refuses it
		// and the ledger reports a validation error, not a failure.
		"NUL in a locator": func(c *ledger.Remember) {
			c.Sources = []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: "go.mod", Locator: []byte(`{"path":"a\u0000b"}`)}}
		},
	}
	for name, mutate := range cases {
		cmd := ok()
		mutate(cmd)
		_, err := f.l.Apply(context.Background(), cmd)
		var ve *ledger.ValidationError
		if !errors.Is(err, ledger.ErrInvalid) || !errors.As(err, &ve) || ve.Message == "" {
			t.Errorf("%s: %v, want a *ValidationError", name, err)
		}
	}
	if _, err := f.l.Apply(context.Background(), ok()); err != nil {
		t.Errorf("the valid command failed: %v", err)
	}
	var nilLedger *ledger.Ledger
	if _, err := nilLedger.Apply(context.Background(), ok()); !errors.Is(err, ledger.ErrDisabled) {
		t.Errorf("nil ledger: %v", err)
	}
	if ledger.New(nil) != nil {
		t.Error("New(nil) should return nil (nil means disabled)")
	}
}

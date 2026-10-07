package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// An import's disagreements (Cleanup: "6 files, 3 conflicts, 1 Brief").
//
// The judge compares each proposal with what the space already keeps.
// An import brings something more: files that disagree with each other
// before anything is kept ("Run tests with pnpm test" in CLAUDE.md, "Run
// npm run test" in AGENTS.md). The import's conflict check (judge_import,
// internal/judge) finds such groups, and RecordImportCheck writes them as
// Memax: every member is flagged as a conflict, with a `flagged` receipt,
// and linked (conflicts_with) to the group's first member, so Review shows
// them as conflicts and nobody keeps one side by accident. A person
// settles a group once, as a group (SettleImportConflict): keep one and
// reject the rest, keep all of them, keep them as open questions, or keep
// the suggested statement (both, scoped) instead of any of them.

// CommandRecordImportCheck names RecordImportCheck.
const CommandRecordImportCheck CommandName = "record_import_check"

// ImportConflictInput is one disagreement the check found.
type ImportConflictInput struct {
	// Members are the proposals that can't all be true, at least two.
	Members []uuid.UUID
	// Subject, Rationale and Suggestion are the model's words; Suggestion
	// may be empty.
	Subject    string
	Rationale  string
	Suggestion string
	Confidence *float64
}

// RecordImportCheck records an import's conflict check, as Memax.
type RecordImportCheck struct {
	Meta
	SpaceID uuid.UUID
	Import  uuid.UUID
	// State is checked, no_model, failed or skipped.
	State     string
	Tier      string
	Model     string
	Conflicts []ImportConflictInput
}

// Name implements Command.
func (*RecordImportCheck) Name() CommandName { return CommandRecordImportCheck }

// The limits of a conflict's words.
const (
	MaxConflictSubject   = 120
	MaxConflictRationale = 500
)

func (c *RecordImportCheck) validate() error {
	if c.SpaceID == uuid.Nil || c.Import == uuid.Nil {
		return invalid("import", "say which import")
	}
	if !slices.Contains(ImportCheckStates, c.State) || c.State == CheckPending {
		return invalid("state", "use checked, no_model, failed or skipped")
	}
	if c.State != CheckChecked && len(c.Conflicts) > 0 {
		return invalid("conflicts", "only a check that ran finds conflicts")
	}
	if c.Tier != "" && c.Tier != TierPrimary && c.Tier != TierFallback && c.Tier != TierStrong {
		return invalid("tier", "use primary, fallback or strong")
	}
	c.Model = truncateRunes(c.Model, maxVerdictModel)
	for i := range c.Conflicts {
		k := &c.Conflicts[i]
		if len(k.Members) < 2 {
			return invalid("conflicts.members", "a conflict needs at least two proposals")
		}
		k.Subject = truncateRunes(strings.TrimSpace(k.Subject), MaxConflictSubject)
		k.Rationale = truncateRunes(strings.TrimSpace(k.Rationale), MaxConflictRationale)
		k.Suggestion = truncateRunes(strings.TrimSpace(k.Suggestion), MaxStatementRunes)
		for _, s := range []string{k.Subject, k.Rationale, k.Suggestion} {
			if err := checkText("conflicts", s, MaxStatementRunes, false); err != nil {
				return err
			}
		}
		if k.Confidence != nil && (*k.Confidence < 0 || *k.Confidence > 1) {
			return invalid("conflicts.confidence", "must be between 0 and 1")
		}
	}
	return nil
}

// recordImportCheck applies RecordImportCheck: it flags and links each
// group's members still waiting in Review, writes the groups, and marks
// the import checked. A memory joins at most one group; a group left with
// fewer than two waiting members is dropped.
func (w *writer) recordImportCheck(ctx context.Context, c *RecordImportCheck) (Result, error) {
	grant, ok := w.meta.Scope.Grant(c.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, c.SpaceID)
	if err != nil {
		return Result{}, err
	}
	if replay, err := w.claim(ctx, sp.ID); err != nil || replay != nil {
		return deref(replay), err
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionJudge, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	var checked *string
	err = w.tx.QueryRow(ctx, `SELECT check_state FROM v2.imports WHERE id = $1 AND space_id = $2 FOR UPDATE`, c.Import, sp.ID).
		Scan(&checked)
	if errNoRows(err) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, fmt.Errorf("ledger: import check: %w", err)
	}
	if checked != nil {
		return Result{Outcome: OutcomeApplied, Policy: dec, Unchanged: true}, nil
	}
	var mine []uuid.UUID
	rows, err := w.tx.Query(ctx, `SELECT memory_id FROM v2.import_items WHERE import_id = $1 AND space_id = $2 AND outcome = 'proposed'`,
		c.Import, sp.ID)
	if err != nil {
		return Result{}, fmt.Errorf("ledger: import check: %w", err)
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return Result{}, fmt.Errorf("ledger: import check: %w", err)
		}
		mine = append(mine, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("ledger: import check: %w", err)
	}

	var receipts []Receipt
	used := map[uuid.UUID]bool{}
	n := 0
	for _, k := range c.Conflicts {
		var ids []uuid.UUID
		for _, id := range k.Members {
			if slices.Contains(mine, id) && !used[id] && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		if len(ids) < 2 {
			continue
		}
		locked, err := lockMemories(ctx, w.tx, w.meta.Scope, ids, "FOR UPDATE")
		if err != nil {
			return Result{}, err
		}
		members := ids[:0:0]
		for _, id := range ids {
			if m := locked[id]; m != nil && m.SpaceID == sp.ID && m.Lifecycle == lifecycle.Proposed {
				members = append(members, id)
			} else if m != nil && m.Lifecycle == lifecycle.Forgotten {
				// Forgotten since the check read it: the model's words may
				// repeat it, and Forget can't reach words written after it
				// (rule 7). The group keeps its members, without the words.
				k.Subject, k.Rationale, k.Suggestion = "", "", ""
			}
		}
		if len(members) < 2 {
			continue
		}
		n++
		refs := make([]string, len(members))
		for i, id := range members {
			refs[i] = locked[id].Ref
		}
		var first Receipt
		for i, id := range members {
			m := locked[id]
			others := slices.Delete(slices.Clone(refs), i, i+1)
			reason := fmt.Sprintf("Disagrees with %s, from the same import.", strings.Join(others, " and "))
			if k.Subject != "" {
				reason = fmt.Sprintf("Disagrees with %s, from the same import (%s).", strings.Join(others, " and "), k.Subject)
			}
			rc := w.receipt(sp, m.ID, m.Ref, ActionFlagged, m.streamVersion+1, reason)
			rc.Source = &ReceiptSource{Kind: ObjectMemory, Ref: others[0]}
			if err := insertReceipt(ctx, w.tx, &rc); err != nil {
				return Result{}, err
			}
			next := m.state()
			if !m.Flags.Has(lifecycle.Conflict) {
				if next, err = transition(m, lifecycle.VerbFlagConflict); err != nil {
					return Result{}, err
				}
			}
			if err := w.setState(ctx, sp, m, next, rc); err != nil {
				return Result{}, err
			}
			if i > 0 {
				if _, err := w.insertLink(ctx, sp.ID, LinkConflictsWith, m.ID, members[0], rc.ID); err != nil {
					return Result{}, err
				}
			} else {
				first = rc
			}
			used[id] = true
			receipts = append(receipts, rc)
		}
		var conf *float32
		if k.Confidence != nil {
			f := float32(*k.Confidence)
			conf = &f
		}
		if _, err := w.tx.Exec(ctx, `
			INSERT INTO v2.import_conflicts (id, import_id, space_id, n, subject, rationale, suggestion, confidence, members,
			                                 created_receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)`,
			newID(), c.Import, sp.ID, n, nullText(k.Subject), nullText(k.Rationale), nullText(k.Suggestion), conf,
			members, first.ID); err != nil {
			return Result{}, fmt.Errorf("ledger: import conflict: %w", err)
		}
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.imports SET checked_at = now(), check_state = $3, check_tier = $4, check_model = $5
		 WHERE id = $1 AND space_id = $2`, c.Import, sp.ID, c.State, nullText(c.Tier), nullText(c.Model)); err != nil {
		return Result{}, fmt.Errorf("ledger: import check: %w", err)
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}
	if err := w.record(ctx, res, uuid.Nil); err != nil {
		return Result{}, err
	}
	return res, nil
}

// ---------------------------------------------------------------------
// Settling a disagreement
// ---------------------------------------------------------------------

// ImportChoice is how a person settles an import's disagreement.
type ImportChoice string

// The choices.
const (
	// ChooseKeepOne keeps one member and rejects the others.
	ChooseKeepOne ImportChoice = "keep_one"
	// ChooseKeepAll keeps every member: they don't disagree after all.
	ChooseKeepAll ImportChoice = "keep_all"
	// ChooseLeaveOpen keeps every member as an open question, so agents
	// read that it isn't decided.
	ChooseLeaveOpen ImportChoice = "leave_open"
	// ChooseKeepSuggestion keeps one new statement that says what is true
	// (the check's suggestion, or the person's words), citing every
	// member's sources, and rejects the members.
	ChooseKeepSuggestion ImportChoice = "keep_suggestion"
)

// ImportChoices lists every choice.
var ImportChoices = []ImportChoice{ChooseKeepOne, ChooseKeepAll, ChooseLeaveOpen, ChooseKeepSuggestion}

// CommandSettleImportConflict names SettleImportConflict.
const CommandSettleImportConflict CommandName = "settle_import_conflict"

// SettleImportConflict settles one of an import's disagreements.
type SettleImportConflict struct {
	Meta
	SpaceID uuid.UUID
	Import  uuid.UUID
	// N is the conflict's number within the import.
	N      int
	Choice ImportChoice
	// Keep is the member to keep, for keep_one (a display ID or an id).
	Keep string
	// Statement is the words to keep, for keep_suggestion; empty keeps the
	// check's suggestion.
	Statement string
}

// Name implements Command.
func (*SettleImportConflict) Name() CommandName { return CommandSettleImportConflict }

func (c *SettleImportConflict) validate() error {
	if c.SpaceID == uuid.Nil || c.Import == uuid.Nil || c.N < 1 {
		return invalid("conflict", "say which import and which of its conflicts")
	}
	if !slices.Contains(ImportChoices, c.Choice) {
		return invalid("choice", "use keep_one, keep_all, leave_open or keep_suggestion")
	}
	c.Keep, c.Statement = strings.TrimSpace(c.Keep), strings.TrimSpace(c.Statement)
	switch {
	case c.Choice == ChooseKeepOne && c.Keep == "":
		return invalid("keep", "say which memory to keep")
	case c.Choice != ChooseKeepOne && c.Keep != "":
		return invalid("keep", "goes with keep_one only")
	case c.Choice != ChooseKeepSuggestion && c.Statement != "":
		return invalid("statement", "goes with keep_suggestion only")
	}
	return checkText("statement", c.Statement, MaxStatementRunes, false)
}

// settleImportConflict applies SettleImportConflict, as one command: the
// group's flags and links end, then the choice applies, each change with
// its receipt, and the group is marked settled.
func (w *writer) settleImportConflict(ctx context.Context, c *SettleImportConflict) (Result, error) {
	grant, ok := w.meta.Scope.Grant(c.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, c.SpaceID)
	if err != nil {
		return Result{}, err
	}
	if replay, err := w.claim(ctx, sp.ID); err != nil || replay != nil {
		if replay != nil {
			ids := []uuid.UUID{}
			for _, rc := range replay.Receipts {
				if !slices.Contains(ids, rc.ObjectID) {
					ids = append(ids, rc.ObjectID)
				}
			}
			err = w.loadMemories(ctx, replay, ids)
		}
		return deref(replay), err
	}
	var groupID uuid.UUID
	var members []uuid.UUID
	var state, suggestion string
	err = w.tx.QueryRow(ctx, `
		SELECT id, members, state, COALESCE(suggestion, '') FROM v2.import_conflicts
		 WHERE import_id = $1 AND n = $2 AND space_id = $3 FOR UPDATE`, c.Import, c.N, sp.ID).
		Scan(&groupID, &members, &state, &suggestion)
	if errNoRows(err) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, fmt.Errorf("ledger: import conflict: %w", err)
	}
	locked, err := lockMemories(ctx, w.tx, w.meta.Scope, members, "FOR UPDATE")
	if err != nil {
		return Result{}, err
	}
	anchor := locked[members[0]]
	ref := "this conflict"
	if anchor != nil {
		ref = anchor.Ref
	}
	if state != "open" {
		return Result{}, conflictStateError(ref, "This disagreement is already settled. Reload the import.")
	}
	var winner *Memory
	if c.Choice == ChooseKeepOne {
		id, err := resolveRef(ctx, w.tx, w.meta.Scope.Narrow(sp.ID), c.Keep)
		if err != nil {
			return Result{}, err
		}
		if !slices.Contains(members, id) || locked[id] == nil {
			return Result{}, invalid("keep", "%s isn't one of this disagreement's statements", c.Keep)
		}
		winner = locked[id]
	}
	statement := c.Statement
	if c.Choice == ChooseKeepSuggestion && statement == "" {
		if statement = suggestion; statement == "" {
			return Result{}, invalid("statement", "this disagreement has no suggestion; send the words to keep")
		}
	}

	// What policy sees: a person's Keep of what this keeps (a decision, a
	// quarantined proposal), by Keep's rules.
	obj := policy.Object{Ref: ref}
	var open []*Memory
	for _, id := range members {
		m := locked[id]
		if m == nil || m.Lifecycle != lifecycle.Proposed {
			continue
		}
		open = append(open, m)
		obj.Decision = obj.Decision || m.Kind == KindDecision
		kept := c.Choice != ChooseKeepOne || m == winner
		obj.External = obj.External || (kept && m.Trust.External())
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionResolveConflict, obj, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if winner != nil && winner.Lifecycle != lifecycle.Proposed {
		return Result{}, conflictStateError(winner.Ref, fmt.Sprintf("%s isn't waiting in Review any more. Reload the import.", winner.Ref))
	}

	var receipts []Receipt
	reason := w.meta.Reason
	if reason == "" {
		reason = settleReason(c.Choice, winner, statement)
	}
	// The group's flags and links end first.
	links, err := activeLinks(ctx, w.tx, members)
	if err != nil {
		return Result{}, err
	}
	for _, m := range open {
		rc := w.receipt(sp, m.ID, m.Ref, ActionResolved, m.streamVersion+1, reason)
		rc.Source = &ReceiptSource{Kind: ObjectMemory, Ref: ref}
		if err := insertReceipt(ctx, w.tx, &rc); err != nil {
			return Result{}, err
		}
		others := false
		for _, l := range links[m.ID] {
			if l.Kind != LinkConflictsWith {
				continue
			}
			inGroup := slices.Contains(members, l.MemoryID)
			if inGroup && l.Direction == LinkOut {
				if err := w.endLink(ctx, l, rc.ID); err != nil {
					return Result{}, err
				}
			} else if !inGroup && l.Direction == LinkOut {
				others = true
			}
		}
		next := m.state()
		if !others && m.Flags.Has(lifecycle.Conflict) {
			if next, err = transition(m, lifecycle.VerbClearConflict); err != nil {
				return Result{}, err
			}
		}
		if err := w.setState(ctx, sp, m, next, rc); err != nil {
			return Result{}, err
		}
		receipts = append(receipts, rc)
	}

	kept := false
	keep := func(m *Memory) error {
		if m.Flags.Has(lifecycle.Conflict) {
			return w.inConflict(ctx, m)
		}
		rc, err := w.keepIt(ctx, sp, grant, m, "")
		if err != nil {
			return err
		}
		receipts, kept = append(receipts, rc), true
		return nil
	}
	reject := func(m *Memory, why string) error {
		next, err := transition(m, lifecycle.VerbReject)
		if err != nil {
			return err
		}
		rc, err := w.changeState(ctx, sp, grant, m, next, ActionRejected, m.streamVersion+1, why)
		if err != nil {
			return err
		}
		m.Lifecycle, m.Flags, m.streamVersion = next.Lifecycle, next.Flags, rc.StreamVersion
		receipts = append(receipts, rc)
		// A rejected statement is out of its other conflicts too.
		for _, l := range links[m.ID] {
			if l.Kind == LinkConflictsWith && l.Direction == LinkOut && !slices.Contains(members, l.MemoryID) {
				if err := w.endLink(ctx, l, rc.ID); err != nil {
					return err
				}
			}
		}
		return nil
	}
	changed := slices.Clone(members)
	var chosen *uuid.UUID
	switch c.Choice {
	case ChooseKeepOne:
		for _, m := range open {
			if m == winner {
				err = keep(m)
			} else {
				err = reject(m, fmt.Sprintf("Settled for %s.", winner.Ref))
			}
			if err != nil {
				return Result{}, err
			}
		}
		chosen = &winner.ID
	case ChooseKeepAll:
		for _, m := range open {
			if err := keep(m); err != nil {
				return Result{}, err
			}
		}
	case ChooseLeaveOpen:
		for _, m := range open {
			if err := keep(m); err != nil {
				return Result{}, err
			}
			status := ""
			if m.Kind == KindDecision {
				status = DecisionOpen
			}
			rc, err := w.reshape(ctx, sp, m, ActionResolved, "Left open: it is undecided.", SectionOpenQuestion, status, nil)
			if err != nil {
				return Result{}, err
			}
			receipts = append(receipts, rc)
		}
	case ChooseKeepSuggestion:
		if err := attachSources(ctx, w.tx, open); err != nil {
			return Result{}, err
		}
		nm, err := suggestedMemory(sp.ID, statement, open)
		if err != nil {
			return Result{}, err
		}
		res, err := w.writeMemory(ctx, sp, grant, nm, false)
		if err != nil {
			return Result{}, err
		}
		if res.Outcome == OutcomeRefused {
			return res, nil
		}
		receipts = append(receipts, res.Receipts...)
		kept = kept || res.Outcome == OutcomeApplied
		changed = append(changed, res.Memory.ID)
		chosen = &res.Memory.ID
		for _, m := range open {
			if err := reject(m, fmt.Sprintf("Settled for %s.", res.Memory.Ref)); err != nil {
				return Result{}, err
			}
		}
	}
	if len(receipts) == 0 {
		return Result{}, conflictStateError(ref, "Nothing in this disagreement is waiting in Review any more. Reload the import.")
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.import_conflicts
		   SET state = 'settled', choice = $2, chosen_memory_id = $3, settled_at = now(), last_receipt_id = $4
		 WHERE id = $1`, groupID, string(c.Choice), chosen, receipts[0].ID); err != nil {
		return Result{}, fmt.Errorf("ledger: settle import conflict: %w", err)
	}
	if kept {
		if err := w.markDirty(ctx, sp.ID); err != nil {
			return Result{}, err
		}
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}
	if err := w.record(ctx, res, uuid.Nil); err != nil {
		return Result{}, err
	}
	if err := w.loadMemories(ctx, &res, changed); err != nil {
		return Result{}, err
	}
	return res, nil
}

func settleReason(choice ImportChoice, winner *Memory, statement string) string {
	switch choice {
	case ChooseKeepOne:
		return fmt.Sprintf("Settled the disagreement: %s is what holds.", winner.Ref)
	case ChooseKeepAll:
		return "Settled the disagreement: all of them hold."
	case ChooseLeaveOpen:
		return "Left the disagreement open: it is undecided."
	}
	return "Settled the disagreement with one statement that says what holds."
}

// suggestedMemory is the statement a keep_suggestion keeps: the first
// member's section and kind, citing every member's sources once, at the
// trust each had.
func suggestedMemory(spaceID uuid.UUID, statement string, members []*Memory) (NewMemory, error) {
	if len(members) == 0 {
		return NewMemory{}, invalid("conflict", "nothing in this disagreement is waiting in Review any more")
	}
	first := members[0]
	nm := NewMemory{SpaceID: spaceID, Statement: statement, Section: first.Section, Kind: first.Kind}
	if first.Kind == KindDecision && first.Decision != nil {
		d := *first.Decision
		d.Status = ""
		nm.Decision = &d
	}
	seen := map[string]bool{}
	for _, m := range members {
		for _, s := range m.Sources {
			k := string(s.Kind) + "\x00" + s.Ref
			if seen[k] || s.Kind == SourceMemory || len(nm.Sources) >= MaxSources {
				continue
			}
			seen[k] = true
			nm.Sources = append(nm.Sources, SourceInput{Kind: s.Kind, Ref: s.Ref, URI: s.URI,
				Locator: json.RawMessage(s.Locator), Trust: s.Trust, ContentHash: s.ContentHash})
		}
	}
	if err := nm.validate(); err != nil {
		return NewMemory{}, err
	}
	return nm, nil
}

// refuseImportPair refuses settling, two at a time, a conflict that is part
// of an import's open disagreement: those settle as a group, once.
func (w *writer) refuseImportPair(ctx context.Context, spaceID uuid.UUID, this *Memory, other uuid.UUID) error {
	var n int
	err := w.tx.QueryRow(ctx, `
		SELECT n FROM v2.import_conflicts
		 WHERE space_id = $1 AND state = 'open' AND $2 = ANY (members) AND $3 = ANY (members)
		 LIMIT 1`, spaceID, this.ID, other).Scan(&n)
	if errNoRows(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ledger: import conflict: %w", err)
	}
	return conflictStateError(this.Ref, fmt.Sprintf(
		"%s disagrees with other statements from the same import. Settle them together: run memax init again, or open the import in Review.", this.Ref))
}

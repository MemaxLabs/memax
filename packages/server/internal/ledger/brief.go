package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// reviseBrief writes a new Brief version (B-) and recompiles every target
// of the space.
func (w *writer) reviseBrief(ctx context.Context, c *ReviseBrief) (Result, error) {
	grant, ok := w.meta.Scope.Grant(c.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, c.SpaceID)
	if err != nil {
		return Result{}, err
	}
	cur, err := lockBrief(ctx, w.tx, sp.ID)
	if err != nil {
		return Result{}, err
	}
	if replay, err := w.claimKey(ctx, sp.ID); err != nil || replay != nil {
		if err != nil {
			return Result{}, err
		}
		return w.replayBrief(ctx, replay)
	}
	switch {
	case cur == nil && c.ExpectedVersion != 0:
		return Result{}, &EditClashError{Ref: "The Brief", Expected: c.ExpectedVersion, Current: 0}
	case cur != nil && c.ExpectedVersion != cur.version:
		return Result{}, &EditClashError{Ref: FormatRef(PrefixBrief, cur.seq), Expected: c.ExpectedVersion, Current: cur.version}
	}

	texts := []string{c.Title, c.Summary, w.meta.Reason}
	for _, s := range c.Sections {
		texts = append(texts, s.Heading)
		for _, it := range s.Items {
			texts = append(texts, it.Text)
		}
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionReviseBrief, policy.Object{Secrets: findSecrets(texts...)}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if err := checkBriefCites(ctx, w.tx, sp.ID, c.Sections); err != nil {
		return Result{}, err
	}

	seq, err := allocateRef(ctx, w.tx, sp.TenantID, PrefixBrief)
	if err != nil {
		return Result{}, err
	}
	ref := FormatRef(PrefixBrief, seq)
	briefID, version, stream := newID(), 1, 1
	var parent any
	if cur != nil {
		briefID, version, stream, parent = cur.id, cur.version+1, cur.streamVersion+1, cur.version
	}
	rc := w.objectReceipt(sp, ObjectBrief, briefID, ref, ActionRevised, stream, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	if cur == nil {
		if _, err := w.tx.Exec(ctx, `
			INSERT INTO v2.briefs (id, tenant_id, space_id, current_version, stream_version, created_receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, 1, 1, $4, $4)`, briefID, sp.TenantID, sp.ID, rc.ID); err != nil {
			return Result{}, fmt.Errorf("ledger: write Brief: %w", err)
		}
	} else if _, err := w.tx.Exec(ctx, `
		UPDATE v2.briefs SET current_version = $2, stream_version = $3, last_receipt_id = $4, updated_at = now()
		 WHERE id = $1 AND space_id = $5`, briefID, version, stream, rc.ID, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: update Brief: %w", err)
	}
	structure, err := json.Marshal(briefStructure{Sections: c.Sections})
	if err != nil {
		return Result{}, err
	}
	versionID := newID()
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.brief_versions (id, brief_id, version, tenant_id, space_id, seq, parent_version, title, summary,
		                               structure, receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)`,
		versionID, briefID, version, sp.TenantID, sp.ID, seq, parent, c.Title, nullText(c.Summary),
		structure, rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: write Brief version: %w", err)
	}
	if err := w.markDirty(ctx, sp.ID); err != nil {
		return Result{}, err
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}
	if err := w.record(ctx, res, versionID); err != nil {
		return Result{}, err
	}
	if res.Brief, err = loadBriefVersion(ctx, w.tx, w.meta.Scope, versionID); err != nil {
		return Result{}, err
	}
	return res, nil
}

func (w *writer) replayBrief(ctx context.Context, c *claimed) (Result, error) {
	res, err := c.result(ctx, w)
	if err != nil {
		return Result{}, err
	}
	if c.objectID != nil {
		if res.Brief, err = loadBriefVersion(ctx, w.tx, w.meta.Scope, *c.objectID); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

// checkBriefCites holds the Brief to the record: every memory item is a
// kept memory of the space, and every prose line cites at least one kept
// memory and nothing forgotten or rejected (a proposal it rests on, such
// as one side of an open question, may be cited).
func checkBriefCites(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, sections []BriefSection) error {
	refs := briefRefs(sections)
	if len(refs) == 0 {
		return nil
	}
	states, err := memoryStates(ctx, tx, spaceID, refs)
	if err != nil {
		return err
	}
	for _, s := range sections {
		for _, it := range s.Items {
			if it.Ref != "" {
				if states[it.Ref] != lifecycle.Kept {
					return invalid("sections.items.ref", "%s isn't a kept memory of this space; the Brief places only kept memories", it.Ref)
				}
				continue
			}
			kept := false
			for _, ref := range it.Cites {
				switch l, ok := states[ref]; {
				case !ok:
					return invalid("sections.items.cites", "%s isn't a memory of this space", ref)
				case l == lifecycle.Forgotten || l == lifecycle.Rejected:
					return invalid("sections.items.cites", "%s is %s; cite what the line still rests on", ref, l)
				case l == lifecycle.Kept:
					kept = true
				}
			}
			if !kept {
				return invalid("sections.items.cites", "prose must cite at least one kept memory")
			}
		}
	}
	return nil
}

// memoryStates reads the lifecycle of each of the space's memories refs
// names; a ref that names none of them is missing from the map.
func memoryStates(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, refs []string) (map[string]lifecycle.Lifecycle, error) {
	states := map[string]lifecycle.Lifecycle{}
	if len(refs) == 0 {
		return states, nil
	}
	seqs := make([]int64, 0, len(refs))
	for _, r := range refs {
		_, n, _ := ParseRef(r)
		seqs = append(seqs, n)
	}
	rows, err := tx.Query(ctx, `SELECT seq, lifecycle FROM v2.memories WHERE space_id = $1 AND seq = ANY($2)`, spaceID, seqs)
	if err != nil {
		return nil, fmt.Errorf("ledger: check Brief cites: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		var l lifecycle.Lifecycle
		if err := rows.Scan(&seq, &l); err != nil {
			return nil, fmt.Errorf("ledger: check Brief cites: %w", err)
		}
		states[FormatRef(PrefixMemory, seq)] = l
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: check Brief cites: %w", err)
	}
	return states, nil
}

// uncitedClaim is the refusal of a line of prose that cites nothing, for
// Dream's operations and for a restore alike.
const uncitedClaim = "every line cites the memories it rests on; an uncited claim can't go in the Brief"

// ---------------------------------------------------------------------
// Restoring an older version
// ---------------------------------------------------------------------

// CommandRestoreBrief names RestoreBrief.
const CommandRestoreBrief CommandName = "restore_brief"

// RestoreBrief writes an older version of the space's Brief back as a new
// version (B-), by a person, with a `revised` receipt whose source is the
// version restored. The record has moved on since that version, so the
// restore keeps only what can still stand (RestoreBriefSections): a memory
// line whose memory isn't kept any more is left out, and so is a line of
// prose whose words were forgotten, that cites a forgotten memory, or that
// no longer cites a kept one; a citation that can't be made any more (a
// rejected memory) comes off a line that stays. The result says which
// (Result.Dropped). A line of prose that cites nothing is refused, as
// Dream's validation refuses it. Every target of the space recompiles.
type RestoreBrief struct {
	Meta
	SpaceID uuid.UUID
	// Version is the version to restore, by number.
	Version int
	// ExpectedVersion is the version in force the person started from
	// (If-Match); a newer one is an edit clash.
	ExpectedVersion int
}

// Name implements Command.
func (*RestoreBrief) Name() CommandName { return CommandRestoreBrief }

func (c *RestoreBrief) validate() error {
	if c.SpaceID == uuid.Nil {
		return invalid("space_id", "say which space's Brief to restore")
	}
	if c.Version < 1 {
		return invalid("version", "say which version to restore, by its number (from 1)")
	}
	if c.ExpectedVersion < 1 {
		return invalid("expected_version", "send the version in force you started from (If-Match)")
	}
	return nil
}

// What a restore leaves out (BriefDrop.Kind).
const (
	// DropMemory: a memory line, whose memory isn't kept any more.
	DropMemory = "memory"
	// DropProse: a line of prose.
	DropProse = "prose"
	// DropCite: a citation taken off a line of prose that stays.
	DropCite = "cite"
)

// Why a restore left something out (BriefDrop.Reason).
const (
	// DropForgotten: the memory was forgotten, or the line's words were
	// (its prose cited a memory since forgotten).
	DropForgotten = "forgotten"
	// DropNotKept: the memory isn't kept any more (faded, folded into
	// another, rejected), or no longer in the space.
	DropNotKept = "not_kept"
)

// BriefDrop is something a restore left out of the version it restored,
// and why. It names memories by display ID, never by their words.
type BriefDrop struct {
	// Section is the key of the section it was in.
	Section string `json:"section"`
	// Item is the line in the restored version: a memory's ref, or
	// P:<section>:<index> for prose (BriefItemID).
	Item string `json:"item"`
	// Kind is memory, prose or cite.
	Kind string `json:"kind"`
	// Refs are the memories that made it go: the memory line's own, the
	// forgotten ones a line of prose cited, or, when none was forgotten,
	// every memory the line cited; for a citation, the memory cited.
	Refs []string `json:"refs"`
	// Reason is forgotten or not_kept.
	Reason string `json:"reason"`
}

// RestoreBriefSections returns an older version's sections as they can
// stand in the record now, and what it had to leave out. state reads a
// memory's lifecycle in the space (false for a ref that names none of its
// memories). Every section stays, in order, even when it ends up empty:
//
//   - a memory line stays while its memory is kept;
//   - a line of prose goes when its words were forgotten, when it cites a
//     forgotten memory (its words may carry that memory's), or when it
//     cites no kept memory any more; otherwise it stays, without the
//     citations that can't be made any more (a rejected memory, or none
//     of the space's), which checkBriefCites would refuse;
//   - a line of prose that cites nothing is refused (uncitedClaim).
//
// It is pure; the ledger checks the result against the record again when
// it writes the version.
func RestoreBriefSections(base []BriefSection, state func(ref string) (lifecycle.Lifecycle, bool)) ([]BriefSection, []BriefDrop, error) {
	out := make([]BriefSection, 0, len(base))
	drops := []BriefDrop{}
	for _, s := range base {
		sec := BriefSection{Key: s.Key, Heading: s.Heading, Items: []BriefItem{}}
		for i, it := range s.Items {
			item := BriefItemID(s.Key, i, it)
			if it.Ref != "" {
				switch l, ok := state(it.Ref); {
				case ok && l == lifecycle.Kept:
					sec.Items = append(sec.Items, BriefItem{Ref: it.Ref})
				case ok && l == lifecycle.Forgotten:
					drops = append(drops, BriefDrop{Section: s.Key, Item: item, Kind: DropMemory, Refs: []string{it.Ref}, Reason: DropForgotten})
				default:
					drops = append(drops, BriefDrop{Section: s.Key, Item: item, Kind: DropMemory, Refs: []string{it.Ref}, Reason: DropNotKept})
				}
				continue
			}
			if len(it.Cites) == 0 {
				return nil, nil, invalid("sections.items.cites", uncitedClaim)
			}
			var forgotten, cites, gone []string
			kept := false
			for _, ref := range it.Cites {
				switch l, ok := state(ref); {
				case ok && l == lifecycle.Forgotten:
					forgotten = append(forgotten, ref)
				case !ok || l == lifecycle.Rejected:
					gone = append(gone, ref)
				default:
					cites = append(cites, ref)
					kept = kept || l == lifecycle.Kept
				}
			}
			switch {
			case it.Forgotten || len(forgotten) > 0:
				refs := forgotten
				if len(refs) == 0 {
					refs = slices.Clone(it.Cites)
				}
				drops = append(drops, BriefDrop{Section: s.Key, Item: item, Kind: DropProse, Refs: refs, Reason: DropForgotten})
			case !kept:
				drops = append(drops, BriefDrop{Section: s.Key, Item: item, Kind: DropProse, Refs: slices.Clone(it.Cites), Reason: DropNotKept})
			default:
				sec.Items = append(sec.Items, BriefItem{Text: it.Text, Cites: cites})
				for _, ref := range gone {
					drops = append(drops, BriefDrop{Section: s.Key, Item: item, Kind: DropCite, Refs: []string{ref}, Reason: DropNotKept})
				}
			}
		}
		out = append(out, sec)
	}
	return out, drops, nil
}

// restoreBrief applies RestoreBrief.
func (w *writer) restoreBrief(ctx context.Context, c *RestoreBrief) (Result, error) {
	grant, ok := w.meta.Scope.Grant(c.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, c.SpaceID)
	if err != nil {
		return Result{}, err
	}
	cur, err := lockBrief(ctx, w.tx, sp.ID)
	if err != nil {
		return Result{}, err
	}
	if cur == nil {
		return Result{}, ErrNotFound
	}
	replay, err := w.claimKey(ctx, sp.ID)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		res, err := w.replayBrief(ctx, replay)
		if err != nil {
			return Result{}, err
		}
		// What it left out, worked out again from the version it restored.
		from, err := briefVersion(ctx, w.tx, sp.ID, cur.id, c.Version)
		if err != nil {
			return Result{}, err
		}
		_, res.Dropped, err = w.restorable(ctx, sp.ID, from)
		return res, err
	}
	if c.ExpectedVersion != cur.version {
		return Result{}, &EditClashError{Ref: FormatRef(PrefixBrief, cur.seq), Expected: c.ExpectedVersion, Current: cur.version}
	}
	if c.Version == cur.version {
		return Result{}, invalid("version", "%s is the Brief in force; restore an older version", FormatRef(PrefixBrief, cur.seq))
	}
	from, err := briefVersion(ctx, w.tx, sp.ID, cur.id, c.Version)
	if errors.Is(err, ErrNotFound) {
		return Result{}, invalid("version", "the Brief has no version %d; its versions run from 1 to %d", c.Version, cur.version)
	}
	if err != nil {
		return Result{}, err
	}
	sections, dropped, err := w.restorable(ctx, sp.ID, from)
	if err != nil {
		return Result{}, err
	}

	texts := []string{from.Title, from.Summary, w.meta.Reason}
	for _, s := range sections {
		texts = append(texts, s.Heading)
		for _, it := range s.Items {
			texts = append(texts, it.Text)
		}
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionReviseBrief, policy.Object{Secrets: findSecrets(texts...)}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if err := checkBriefCites(ctx, w.tx, sp.ID, sections); err != nil {
		return Result{}, err
	}
	reason := w.meta.Reason
	if reason == "" {
		reason = restoreReason(from.Ref, dropped)
	}
	rc, _, versionID, err := w.writeBriefVersion(ctx, sp, cur, from.Title, from.Summary, sections, reason,
		&ReceiptSource{Kind: ObjectBrief, Ref: from.Ref}, ActionRevised)
	if err != nil {
		return Result{}, err
	}
	if err := w.markDirty(ctx, sp.ID); err != nil {
		return Result{}, err
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}
	if err := w.record(ctx, res, versionID); err != nil {
		return Result{}, err
	}
	if res.Brief, err = loadBriefVersion(ctx, w.tx, w.meta.Scope, versionID); err != nil {
		return Result{}, err
	}
	res.Dropped = dropped
	return res, nil
}

// restorable returns a version's sections as they can stand now, with
// what they leave out (RestoreBriefSections).
func (w *writer) restorable(ctx context.Context, spaceID uuid.UUID, from *Brief) ([]BriefSection, []BriefDrop, error) {
	states, err := memoryStates(ctx, w.tx, spaceID, briefRefs(from.Sections))
	if err != nil {
		return nil, nil, err
	}
	return RestoreBriefSections(from.Sections, func(ref string) (lifecycle.Lifecycle, bool) {
		l, ok := states[ref]
		return l, ok
	})
}

// restoreReason is a restore's receipt reason when the person gave none.
// It names versions and counts lines, never words.
func restoreReason(from string, dropped []BriefDrop) string {
	lines := 0
	for _, d := range dropped {
		if d.Kind != DropCite {
			lines++
		}
	}
	switch lines {
	case 0:
		return fmt.Sprintf("Restored %s.", from)
	case 1:
		return fmt.Sprintf("Restored %s, without 1 line that rests on memories no longer kept.", from)
	}
	return fmt.Sprintf("Restored %s, without %d lines that rest on memories no longer kept.", from, lines)
}

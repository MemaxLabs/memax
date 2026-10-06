package ledger

import (
	"context"
	"encoding/json"
	"fmt"

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
	seqs := make([]int64, 0, len(refs))
	for _, r := range refs {
		_, n, _ := ParseRef(r)
		seqs = append(seqs, n)
	}
	rows, err := tx.Query(ctx, `SELECT seq, lifecycle FROM v2.memories WHERE space_id = $1 AND seq = ANY($2)`, spaceID, seqs)
	if err != nil {
		return fmt.Errorf("ledger: check Brief cites: %w", err)
	}
	states := map[string]lifecycle.Lifecycle{}
	for rows.Next() {
		var seq int64
		var l lifecycle.Lifecycle
		if err := rows.Scan(&seq, &l); err != nil {
			rows.Close()
			return fmt.Errorf("ledger: check Brief cites: %w", err)
		}
		states[FormatRef(PrefixMemory, seq)] = l
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ledger: check Brief cites: %w", err)
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

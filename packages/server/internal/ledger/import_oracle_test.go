package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Import as it ran before it was one transaction: one ImportStatement
// through Apply per statement, between an opening and a closing
// transaction. It is the oracle the batched import is held to
// (imports_batch_test.go), and what a server before it left behind when
// an upload stopped halfway (StopAfter).

// ImportOneByOne is Import, one statement at a time. With stopAfter >= 0
// it stops after that many statements, before closing the import, as an
// upload cut off halfway leaves it.
func ImportOneByOne(ctx context.Context, l *Ledger, req ImportRequest, stopAfter int) (*ImportResult, error) {
	req.Via = policy.ViaImport
	meta := req.Meta
	if err := validateMeta(&meta, l.now()); err != nil {
		return nil, err
	}
	if err := req.validate(); err != nil {
		return nil, err
	}
	for i := range req.Items {
		req.Items[i].capRepositoryTrust()
	}
	return l.importOneByOne(ctx, meta, req, importOriginInit, stopAfter)
}

func (l *Ledger) importOneByOne(ctx context.Context, meta Meta, req ImportRequest, origin importOrigin, stopAfter int) (*ImportResult, error) {
	if _, ok := meta.Scope.Grant(req.SpaceID); !ok {
		return nil, ErrNotFound
	}
	meta.Via = policy.ViaImport
	if err := validateMeta(&meta, l.now()); err != nil {
		return nil, err
	}
	for i := range req.Items {
		req.Items[i].SpaceID = req.SpaceID
		if err := req.Items[i].NewMemory.validate(); err != nil {
			return nil, err
		}
	}
	meta.Scope = meta.Scope.Narrow(req.SpaceID)
	hash, err := req.hash()
	if err != nil {
		return nil, err
	}
	imp, replayed, refusal, err := l.openImportAlone(ctx, meta, &req, hash, origin)
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return &ImportResult{Refused: refusal}, nil
	}

	results := make([]ImportItemResult, len(req.Items))
	var refused []importItemRow
	prefix := itemKeyPrefix(meta.IdempotencyKey)
	for n, g := range foldImport(req.Items) {
		if n == stopAfter {
			return &ImportResult{Items: results}, nil
		}
		lead := req.Items[g[0]]
		cmd := &ImportStatement{Meta: meta, Import: imp.ID, NewMemory: mergeGroup(req.Items, g)}
		cmd.IdempotencyKey = prefix + lead.Key
		for _, p := range g {
			it := req.Items[p]
			cmd.Items = append(cmd.Items, importItemRow{Position: p, Key: it.Key, Ref: it.Ref, Location: it.Location,
				HiddenCharacters: it.HiddenCharacters})
		}
		res, err := l.Apply(ctx, cmd)
		if err != nil {
			return nil, err
		}
		for i, p := range g {
			r := ImportItemResult{Key: req.Items[p].Key, Position: p, Ref: req.Items[p].Ref}
			switch {
			case res.Outcome == OutcomeRefused:
				d := res.Policy
				r.Outcome, r.Policy = ImportRefused, &d
				row := cmd.Items[i]
				row.Outcome, row.Code = ImportRefused, d.Code
				refused = append(refused, row)
			case len(res.Receipts) == 0:
				r.Outcome = ImportExisting
				r.Memory = &MemoryPointer{ID: res.Memory.ID, Ref: res.Memory.Ref}
				r.Lifecycle = string(res.Memory.Lifecycle)
			case i == 0:
				r.Outcome = ImportProposed
				r.Memory = &MemoryPointer{ID: res.Memory.ID, Ref: res.Memory.Ref}
			default:
				r.Outcome, r.FoldedInto = ImportFolded, lead.Key
				r.Memory = &MemoryPointer{ID: res.Memory.ID, Ref: res.Memory.Ref}
			}
			results[p] = r
		}
	}
	if err := l.closeImportAlone(ctx, meta, imp, refused); err != nil {
		return nil, err
	}
	summary, err := l.importSummary(ctx, meta.Scope, imp.SpaceID, imp.ID)
	if err != nil {
		return nil, err
	}
	return &ImportResult{Import: summary, Items: results, Replayed: replayed}, nil
}

func (l *Ledger) openImportAlone(ctx context.Context, meta Meta, req *ImportRequest, hash []byte, origin importOrigin) (*Import, bool, *policy.Decision, error) {
	tx, _, err := l.begin(ctx, meta.Scope, pgx.ReadWrite)
	if err != nil {
		return nil, false, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sp, err := loadSpace(ctx, tx, req.SpaceID)
	if err != nil {
		return nil, false, nil, err
	}
	grant, _ := meta.Scope.Grant(sp.ID)
	if d := policy.Decide(toPolicyActor(meta.Actor, meta.Via, grant), policy.ActionPropose, policy.Object{}, sp.policy()); d.Effect == policy.EffectRefuse {
		return nil, false, &d, nil
	}
	files, err := json.Marshal(nonNilSlice(req.Files))
	if err != nil {
		return nil, false, nil, err
	}
	skipped, err := json.Marshal(nonNilSlice(req.Skipped))
	if err != nil {
		return nil, false, nil, err
	}
	var actorID *uuid.UUID
	if meta.Actor.ID != uuid.Nil {
		id := meta.Actor.ID
		actorID = &id
	}
	imp := &Import{ID: newID(), SpaceID: sp.ID, TenantID: sp.TenantID}
	tag, err := tx.Exec(ctx, `
		INSERT INTO v2.imports (id, tenant_id, space_id, actor_kind, actor_id, agent, idempotency_key, request_sha256,
		                        client, files, skipped, items_total, origin)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT ON CONSTRAINT imports_key DO NOTHING`,
		imp.ID, sp.TenantID, sp.ID, string(meta.Actor.Kind), actorID, nullText(meta.Actor.Agent),
		meta.IdempotencyKey, hash, nullText(req.Client), files, skipped, len(req.Items), string(origin))
	if err != nil {
		return nil, false, nil, fmt.Errorf("ledger: open import: %w", err)
	}
	replayed := tag.RowsAffected() == 0
	if replayed {
		var stored []byte
		if err := tx.QueryRow(ctx, `
			SELECT id, request_sha256 FROM v2.imports
			 WHERE space_id = $1 AND actor_kind = $2 AND actor_id IS NOT DISTINCT FROM $3 AND idempotency_key = $4`,
			sp.ID, string(meta.Actor.Kind), actorID, meta.IdempotencyKey).Scan(&imp.ID, &stored); err != nil {
			return nil, false, nil, fmt.Errorf("ledger: open import: %w", err)
		}
		if !bytes.Equal(stored, hash) {
			return nil, false, nil, fmt.Errorf("%w: key %q opened another import; send a new key for a new import",
				ErrIdempotencyKeyReused, meta.IdempotencyKey)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, nil, mapDBError(fmt.Errorf("ledger: open import: %w", err))
	}
	return imp, replayed, nil, nil
}

func (l *Ledger) closeImportAlone(ctx context.Context, meta Meta, imp *Import, refused []importItemRow) error {
	tx, loginRole, err := l.begin(ctx, meta.Scope, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertImportItems(ctx, tx, imp.ID, imp.SpaceID, refused); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE v2.imports SET uploaded_at = COALESCE(uploaded_at, now()) WHERE id = $1 AND space_id = $2`,
		imp.ID, imp.SpaceID); err != nil {
		return fmt.Errorf("ledger: close import: %w", err)
	}
	w := &writer{tx: tx, meta: &meta, inserter: l.inserter, loginRole: loginRole}
	w.jobs = append(w.jobs, river.InsertManyParams{Args: JudgeImportArgs{ImportID: imp.ID, SpaceID: imp.SpaceID}})
	if err := w.flush(ctx); err != nil {
		return mapDBError(err)
	}
	return mapDBError(tx.Commit(ctx))
}

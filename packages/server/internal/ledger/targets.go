package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// openTarget loads and locks a target, then claims the command's key in
// its space. A non-nil claimed means the key was already applied.
func (w *writer) openTarget(ctx context.Context, id uuid.UUID) (*Target, SpaceGrant, spaceRow, *claimed, error) {
	t, err := loadTarget(ctx, w.tx, w.meta.Scope, id, true)
	if err != nil {
		return nil, SpaceGrant{}, spaceRow{}, nil, err
	}
	grant, ok := w.meta.Scope.Grant(t.SpaceID)
	if !ok {
		return nil, SpaceGrant{}, spaceRow{}, nil, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, t.SpaceID)
	if err != nil {
		return nil, SpaceGrant{}, spaceRow{}, nil, err
	}
	c, err := w.claimKey(ctx, sp.ID)
	return t, grant, sp, c, err
}

// replayTarget answers a replayed target command: the original receipts
// and the target as it is now.
func (w *writer) replayTarget(ctx context.Context, c *claimed, targetID uuid.UUID) (Result, error) {
	res, err := c.result(ctx, w)
	if err != nil {
		return Result{}, err
	}
	if res.Target, err = w.target(ctx, targetID); err != nil {
		return Result{}, err
	}
	return res, nil
}

// target loads a target's projection, with its latest run.
func (w *writer) target(ctx context.Context, id uuid.UUID) (*Target, error) {
	t, err := loadTarget(ctx, w.tx, w.meta.Scope, id, false)
	if err != nil {
		return nil, err
	}
	return t, fillTargets(ctx, w.tx, w.meta.Scope, []*Target{t})
}

// configureTarget adds a target or changes one.
func (w *writer) configureTarget(ctx context.Context, c *ConfigureTarget) (Result, error) {
	if c.Target == uuid.Nil {
		return w.addTarget(ctx, c)
	}
	t, grant, sp, replay, err := w.openTarget(ctx, c.Target)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		return w.replayTarget(ctx, replay, t.ID)
	}
	if c.ExpectedVersion != 0 && c.ExpectedVersion != t.Version {
		return Result{}, &EditClashError{Ref: t.Label, Expected: c.ExpectedVersion, Current: t.Version}
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionConfigureTarget, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}

	path, settings, delivery := t.Path, t.Settings, t.Delivery
	if c.Path != nil {
		path = *c.Path
		if err := checkTargetPath(t.Kind, path); err != nil {
			return Result{}, err
		}
	}
	if settings, err = settings.apply(c.Settings, t.Kind); err != nil {
		return Result{}, err
	}
	if c.Delivery != nil {
		delivery = *c.Delivery
		if err := checkDelivery(t.Kind, delivery); err != nil {
			return Result{}, err
		}
	}
	state := t.SyncState
	action := ActionConfigured
	switch {
	case c.Enabled != nil && !*c.Enabled && state != SyncOff:
		state, action = SyncOff, ActionStopped
	case c.Enabled != nil && *c.Enabled && state == SyncOff:
		state = SyncCompiling
	}
	if path == t.Path && settings == t.Settings && delivery == t.Delivery && state == t.SyncState {
		// Nothing to change: no receipt, nothing written.
		return Result{Outcome: OutcomeApplied, Policy: dec, Target: t, Unchanged: true}, nil
	}

	rc := w.objectReceipt(sp, ObjectTarget, t.ID, targetRef(&Target{Kind: t.Kind, Path: path}), action, t.Version+1, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return Result{}, err
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.targets
		   SET path = $2, settings = $3, delivery = $4, sync_state = $5, stream_version = $6,
		       last_receipt_id = $7, updated_at = now()
		 WHERE id = $1 AND space_id = $8`,
		t.ID, nullText(path), settingsJSON, string(delivery), string(state), rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: update target: %w", err)
	}
	if state == SyncOff {
		// Stopping a file also closes its open hand edits: nothing will
		// compile over them now.
		if err := w.closeObservations(ctx, t.ID, sp.ID, obsFilter{}, ObservationStopped, DriftStop, rc.ID, nil, nil); err != nil {
			return Result{}, err
		}
	} else if err := w.markDirty(ctx, sp.ID, t.ID); err != nil {
		return Result{}, err
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}
	if err := w.record(ctx, res, t.ID); err != nil {
		return Result{}, err
	}
	if res.Target, err = w.target(ctx, t.ID); err != nil {
		return Result{}, err
	}
	return res, nil
}

// addTarget is ConfigureTarget without a target: a new one, compiled at
// once.
func (w *writer) addTarget(ctx context.Context, c *ConfigureTarget) (Result, error) {
	grant, ok := w.meta.Scope.Grant(c.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, c.SpaceID)
	if err != nil {
		return Result{}, err
	}
	replay, err := w.claimKey(ctx, sp.ID)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		if replay.objectID == nil {
			return Result{}, ErrNotFound
		}
		return w.replayTarget(ctx, replay, *replay.objectID)
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionConfigureTarget, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	var hasBrief bool
	if err := w.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM v2.briefs WHERE space_id = $1)`, sp.ID).Scan(&hasBrief); err != nil {
		return Result{}, fmt.Errorf("ledger: check Brief: %w", err)
	}
	if !hasBrief {
		return Result{}, invalid("brief", "write the space's Brief before choosing where it compiles")
	}
	path := c.Kind.DefaultPath()
	if c.Path != nil {
		path = *c.Path
	}
	if err := checkTargetPath(c.Kind, path); err != nil {
		return Result{}, err
	}
	delivery := DeliveryLocal
	if c.Kind == TargetChatGPT {
		delivery = DeliveryCopy
	}
	if c.Delivery != nil {
		delivery = *c.Delivery
	}
	if err := checkDelivery(c.Kind, delivery); err != nil {
		return Result{}, err
	}
	settings, err := defaultSettings(c.Kind).apply(c.Settings, c.Kind)
	if err != nil {
		return Result{}, err
	}
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return Result{}, err
	}
	id := newID()
	rc := w.objectReceipt(sp, ObjectTarget, id, targetRef(&Target{Kind: c.Kind, Path: path}), ActionConfigured, 1, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.targets (id, tenant_id, space_id, kind, path, settings, delivery, sync_state,
		                        dirty_gen, compiled_gen, stream_version, created_receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'compiling', 1, 0, 1, $8, $8)`,
		id, sp.TenantID, sp.ID, string(c.Kind), nullText(path), settingsJSON, string(delivery), rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: write target: %w", err)
	}
	w.enqueueCompile(id, sp.ID)
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}
	if err := w.record(ctx, res, id); err != nil {
		return Result{}, err
	}
	if res.Target, err = w.target(ctx, id); err != nil {
		return Result{}, err
	}
	return res, nil
}

// checkDelivery: ChatGPT is copied out or read over MCP; files are
// written locally, by pull request, or read over MCP.
func checkDelivery(k TargetKind, d Delivery) error {
	switch {
	case k == TargetChatGPT && d != DeliveryCopy && d != DeliveryMCP:
		return invalid("delivery", "ChatGPT has no file: use copy or mcp")
	case k != TargetChatGPT && d == DeliveryCopy:
		return invalid("delivery", "%s is a file: use local, pr or mcp", k)
	}
	return nil
}

func (w *writer) enqueueCompile(targetID, spaceID uuid.UUID) {
	args := CompileTargetArgs{TargetID: targetID, SpaceID: spaceID}
	if !slices.ContainsFunc(w.jobs, func(p river.InsertManyParams) bool { return p.Args == args }) {
		w.jobs = append(w.jobs, river.InsertManyParams{Args: args})
	}
}

// requestCompile is "Compile now".
func (w *writer) requestCompile(ctx context.Context, c *RequestCompile) (Result, error) {
	t, grant, sp, replay, err := w.openTarget(ctx, c.Target)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		return w.replayTarget(ctx, replay, t.ID)
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionRequestCompile, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	if t.SyncState == SyncOff {
		return Result{}, &TargetStateError{Ref: t.Label, Message: "compiling is stopped; turn it back on in the target's settings"}
	}
	rc := w.objectReceipt(sp, ObjectTarget, t.ID, targetRef(t), ActionRequested, t.Version+1, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.targets SET stream_version = $2, last_receipt_id = $3, updated_at = now()
		 WHERE id = $1 AND space_id = $4`, t.ID, rc.StreamVersion, rc.ID, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: update target: %w", err)
	}
	if err := w.markDirty(ctx, sp.ID, t.ID); err != nil {
		return Result{}, err
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}
	if err := w.record(ctx, res, t.ID); err != nil {
		return Result{}, err
	}
	if res.Target, err = w.target(ctx, t.ID); err != nil {
		return Result{}, err
	}
	return res, nil
}

// recordCompile records a compile run as Memax (the compile coordinator).
func (w *writer) recordCompile(ctx context.Context, c *RecordCompile) (Result, error) {
	t, grant, sp, replay, err := w.openTarget(ctx, c.Target)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		res, err := w.replayTarget(ctx, replay, t.ID)
		if err == nil && replay.objectID != nil {
			res.Compile, err = loadCompileRun(ctx, w.tx, w.meta.Scope, *replay.objectID)
		}
		return res, err
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionRecordCompile, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	switch {
	case t.CompiledGen >= c.Generation || t.SyncState == SyncOff:
		// Already recorded (a retried job), or stopped while it compiled:
		// nothing to do.
		if err := fillTargets(ctx, w.tx, w.meta.Scope, []*Target{t}); err != nil {
			return Result{}, err
		}
		return Result{Outcome: OutcomeApplied, Policy: dec, Target: t, Unchanged: true}, nil
	case t.DirtyGen > c.Generation && !c.AllowBehind:
		return Result{}, ErrBehind
	case c.Generation > t.DirtyGen:
		return Result{}, invalid("generation", "%d is ahead of the target (%d)", c.Generation, t.DirtyGen)
	}
	_, seq, _ := ParseRef(c.Ref)
	id := newID()
	rc := w.objectReceipt(sp, ObjectCompile, id, c.Ref, ActionCompiled, 1, "")
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	files, err := json.Marshal(nonNilSlice(c.Files))
	if err != nil {
		return Result{}, err
	}
	warnings, err := json.Marshal(nonNilSlice(c.Warnings))
	if err != nil {
		return Result{}, err
	}
	status := CompileCompiled
	var deliveredAt *time.Time
	failed := c.Error != ""
	if failed {
		status = CompileFailed
	} else if !t.Delivery.writesFiles() {
		// MCP and copy-out targets are read where they are: compiled is
		// delivered.
		status, deliveredAt = CompileDelivered, &c.CompiledAt
	}
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.compile_runs (id, tenant_id, space_id, seq, target_id, brief_id, brief_version, generation, status,
		                             input_sha256, output_sha256, drift_sha256, artifact_key, bytes, lines, refs,
		                             dropped_for_budget, files, warnings, error, enqueued_at, started_at, compiled_at,
		                             delivered_at, created_receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22,
		        $23, $24, $25, $25)`,
		id, sp.TenantID, sp.ID, seq, t.ID, c.BriefID, c.BriefVersion, c.Generation, string(status),
		c.InputSHA256, nullText(c.OutputSHA256), nullText(c.DriftSHA256), nullText(c.ArtifactKey),
		nullIfFailed(failed, c.Bytes), nullIfFailed(failed, c.Lines), nonNilSlice(c.Refs),
		nonNilSlice(c.DroppedForBudget), files, warnings, nullText(c.Error), c.EnqueuedAt, c.StartedAt, c.CompiledAt,
		deliveredAt, rc.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: write compile run: %w", err)
	}

	t.CompiledGen = c.Generation
	lastGood := t.lastGoodID(ctx, w)
	if !failed {
		lastGood = &id
	}
	set := `compiled_gen = $2, last_compile_id = $3, last_receipt_id = $4, updated_at = now()`
	args := []any{t.ID, c.Generation, id, rc.ID, sp.ID}
	if status == CompileDelivered {
		t.Delivered = &Delivered{CompileID: &id, SHA256: c.DriftSHA256, Files: []DeliveredFile{}}
		set += `, delivered_compile_id = $3, delivered_sha256 = $6, delivered_files = '[]'::jsonb, delivered_at = $7`
		args = append(args, c.DriftSHA256, c.CompiledAt)
	}
	if lastGood == nil {
		lastGood = &id
	}
	args = append(args, string(settledState(t, *lastGood)))
	set += fmt.Sprintf(`, sync_state = $%d`, len(args))
	if _, err := w.tx.Exec(ctx, `UPDATE v2.targets SET `+set+` WHERE id = $1 AND space_id = $5`, args...); err != nil {
		return Result{}, fmt.Errorf("ledger: update target: %w", err)
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}
	if err := w.record(ctx, res, id); err != nil {
		return Result{}, err
	}
	if res.Compile, err = loadCompileRun(ctx, w.tx, w.meta.Scope, id); err != nil {
		return Result{}, err
	}
	if res.Target, err = w.target(ctx, t.ID); err != nil {
		return Result{}, err
	}
	return res, nil
}

func nullIfFailed(failed bool, n int) any {
	if failed {
		return nil
	}
	return n
}

// lastGoodID is the target's latest successful run, or nil. Errors read
// as "none": the state it feeds is recomputed on the next compile.
func (t *Target) lastGoodID(ctx context.Context, w *writer) *uuid.UUID {
	r, err := lastGoodRun(ctx, w.tx, w.meta.Scope, t.ID)
	if err != nil || r == nil {
		return nil
	}
	return &r.ID
}

// settledState is where a target stands once its counters and delivery
// are known: drifted and off hold until resolved; a generation not yet
// compiled is compiling; MCP and copy-out targets are in sync once
// compiled; a file is in sync once its latest good output is delivered,
// and pending delivery until then.
func settledState(t *Target, lastGood uuid.UUID) SyncState {
	switch {
	case t.SyncState == SyncOff || t.SyncState == SyncDrifted:
		return t.SyncState
	case t.CompiledGen < t.DirtyGen:
		return SyncCompiling
	case !t.Delivery.writesFiles():
		return SyncInSync
	case t.Delivered != nil && t.Delivered.CompileID != nil && *t.Delivered.CompileID == lastGood:
		return SyncInSync
	}
	return SyncPendingDelivery
}

// recordDelivery acknowledges that a run's output is on disk or merged.
func (w *writer) recordDelivery(ctx context.Context, c *RecordDelivery) (Result, error) {
	t, grant, sp, replay, err := w.openTarget(ctx, c.Target)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		res, err := w.replayTarget(ctx, replay, t.ID)
		if err == nil && replay.objectID != nil {
			res.Compile, err = loadCompileRun(ctx, w.tx, w.meta.Scope, *replay.objectID)
		}
		return res, err
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionReport, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	run, err := findCompileRun(ctx, w.tx, w.meta.Scope, t.ID, c.Compile)
	if err != nil {
		return Result{}, err
	}
	switch {
	case run.Status == CompileFailed:
		return Result{}, invalid("compile", "%s failed, so there is nothing to deliver", run.Ref)
	case c.SHA256 != run.DriftSHA256:
		return Result{}, invalid("sha256",
			"what you wrote doesn't match %s (its drift hash is %s); read the preview and write it again", run.Ref, run.DriftSHA256)
	}
	unchanged := t.Delivered != nil && t.Delivered.CompileID != nil && *t.Delivered.CompileID == run.ID &&
		t.Delivered.SHA256 == run.DriftSHA256
	if !unchanged && t.Delivered != nil && t.Delivered.CompileID != nil {
		// A late acknowledgement of an older run never moves the
		// baseline back.
		if prev, err := loadCompileRun(ctx, w.tx, w.meta.Scope, *t.Delivered.CompileID); err == nil && prev.seq > run.seq {
			unchanged = true
		}
	}
	if unchanged {
		if err := fillTargets(ctx, w.tx, w.meta.Scope, []*Target{t}); err != nil {
			return Result{}, err
		}
		return Result{Outcome: OutcomeApplied, Policy: dec, Target: t, Compile: run, Unchanged: true}, nil
	}
	version, err := nextStreamVersion(ctx, w.tx, run.ID)
	if err != nil {
		return Result{}, err
	}
	rc := w.objectReceipt(sp, ObjectCompile, run.ID, run.Ref, ActionDelivered, version, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.compile_runs SET status = 'delivered', delivered_at = COALESCE(delivered_at, now()), last_receipt_id = $2
		 WHERE id = $1 AND space_id = $3`, run.ID, rc.ID, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: update compile run: %w", err)
	}
	files := make([]DeliveredFile, 0, len(run.Files))
	for _, f := range run.Files {
		if f.Path != "" {
			files = append(files, DeliveredFile{Path: f.Path, SHA256: f.DriftSHA256})
		}
	}
	filesJSON, err := json.Marshal(files)
	if err != nil {
		return Result{}, err
	}
	t.Delivered = &Delivered{CompileID: &run.ID, SHA256: run.DriftSHA256, Files: files}
	lastGood := run.ID
	if g := t.lastGoodID(ctx, w); g != nil {
		lastGood = *g
	}
	if _, err := w.tx.Exec(ctx, `
		UPDATE v2.targets
		   SET delivered_compile_id = $2, delivered_sha256 = $3, delivered_files = $4, delivered_at = now(),
		       sync_state = $5, last_receipt_id = $6, updated_at = now()
		 WHERE id = $1 AND space_id = $7`,
		t.ID, run.ID, run.DriftSHA256, filesJSON, string(settledState(t, lastGood)), rc.ID, sp.ID); err != nil {
		return Result{}, fmt.Errorf("ledger: update target: %w", err)
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}
	if err := w.record(ctx, res, run.ID); err != nil {
		return Result{}, err
	}
	if res.Compile, err = loadCompileRun(ctx, w.tx, w.meta.Scope, run.ID); err != nil {
		return Result{}, err
	}
	if res.Target, err = w.target(ctx, t.ID); err != nil {
		return Result{}, err
	}
	return res, nil
}

// ---------------------------------------------------------------------
// Compile bookkeeping: no receipts (see migration 029)
// ---------------------------------------------------------------------

// ReserveCompileRef allocates the next C- number of the space's tenant.
// The compile coordinator reserves it before compiling, because the
// compiled header carries it. Numbers are gap-tolerant: a compile that
// fails to record leaves a gap.
func (l *Ledger) ReserveCompileRef(ctx context.Context, scope Scope, spaceID uuid.UUID) (string, error) {
	g, ok := scope.Grant(spaceID)
	if !ok {
		return "", ErrNotFound
	}
	var ref string
	err := l.writeBookkeeping(ctx, scope, func(tx pgx.Tx) error {
		n, err := allocateRef(ctx, tx, g.TenantID, PrefixCompile)
		ref = FormatRef(PrefixCompile, n)
		return err
	})
	return ref, err
}

// SettleUnchanged records that generation gen compiled to the same bytes
// as the target's latest run, so there is no new run to record: the
// target's compiled_gen moves to gen, and it leaves "compiling". It is
// ErrBehind when the target was dirtied again meanwhile.
func (l *Ledger) SettleUnchanged(ctx context.Context, scope Scope, targetID uuid.UUID, gen int64) error {
	return l.writeBookkeeping(ctx, scope, func(tx pgx.Tx) error {
		t, err := loadTarget(ctx, tx, scope, targetID, true)
		if err != nil {
			return err
		}
		switch {
		case t.DirtyGen > gen:
			return ErrBehind
		case t.CompiledGen >= gen:
			return nil
		}
		t.CompiledGen = gen
		state := t.SyncState
		if state == SyncCompiling {
			last, err := lastGoodRun(ctx, tx, scope, t.ID)
			if err != nil {
				return err
			}
			lastGood := uuid.Nil
			if last != nil {
				lastGood = last.ID
			}
			state = settledState(t, lastGood)
		}
		_, err = tx.Exec(ctx, `
			UPDATE v2.targets SET compiled_gen = $2, sync_state = $3, updated_at = now()
			 WHERE id = $1 AND space_id = ANY($4)`, t.ID, gen, string(state), scope.SpaceIDs())
		if err != nil {
			return fmt.Errorf("ledger: settle target: %w", err)
		}
		return nil
	})
}

// AdvanceCounter moves a tenant's display-ID counter forward so the next
// ID of that prefix is at least next. It never moves a counter back, so
// IDs never repeat. The dev seeder uses it to reproduce the handoff's IDs
// (M-0219, C-0881); counters are gap-tolerant by design.
func (l *Ledger) AdvanceCounter(ctx context.Context, scope Scope, spaceID uuid.UUID, p Prefix, next int64) error {
	g, ok := scope.Grant(spaceID)
	if !ok {
		return ErrNotFound
	}
	if !slices.Contains(prefixes, p) || next < 1 {
		return invalid("prefix", "use a display-ID prefix and a positive number")
	}
	return l.writeBookkeeping(ctx, scope, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO v2.id_counters AS c (tenant_id, prefix, next) VALUES ($1, $2, $3)
			ON CONFLICT (tenant_id, prefix) DO UPDATE SET next = GREATEST(c.next, EXCLUDED.next)`,
			g.TenantID, string(p), next)
		if err != nil {
			return fmt.Errorf("ledger: advance %s- counter: %w", p, err)
		}
		return nil
	})
}

// writeBookkeeping runs fn in a read-write transaction as memax_v2 in the
// scope. Only bookkeeping that needs no receipt goes through it.
func (l *Ledger) writeBookkeeping(ctx context.Context, scope Scope, fn func(pgx.Tx) error) error {
	if l == nil {
		return ErrDisabled
	}
	tx, _, err := l.begin(ctx, scope, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return mapDBError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDBError(err)
	}
	return nil
}

package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Reads of the Brief, targets, compile runs and observations. Like the
// memory reads, each runs as memax_v2 inside the caller's scope.

// GetBrief returns the space's current Brief version, with its author
// receipt. ErrNotFound when the space has no Brief yet.
func (l *Ledger) GetBrief(ctx context.Context, scope Scope, spaceID uuid.UUID) (*Brief, error) {
	if _, ok := scope.Grant(spaceID); !ok {
		return nil, ErrNotFound
	}
	var out *Brief
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		b, err := scanBrief(tx.QueryRow(ctx, briefSelect+`
			 WHERE v.space_id = $1 AND v.space_id = ANY($2) AND v.version = b.current_version`,
			spaceID, scope.SpaceIDs()))
		if errNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("ledger: load Brief: %w", err)
		}
		out = b
		return attachBriefReceipts(ctx, tx, scope, []*Brief{b})
	})
	return out, err
}

// BriefVersionQuery pages through ListBriefVersions.
type BriefVersionQuery struct {
	SpaceID uuid.UUID
	Cursor  string
	Limit   int
}

// BriefVersionPage is one page of a Brief's versions, newest first.
type BriefVersionPage struct {
	Versions   []Brief
	NextCursor string
	HasMore    bool
}

// ListBriefVersions pages through every version (B-) of the space's
// Brief, newest first, each with its author receipt: BriefHistory.
func (l *Ledger) ListBriefVersions(ctx context.Context, scope Scope, q BriefVersionQuery) (BriefVersionPage, error) {
	if _, ok := scope.Grant(q.SpaceID); !ok {
		return BriefVersionPage{}, ErrNotFound
	}
	limit, err := pageLimit(q.Limit)
	if err != nil {
		return BriefVersionPage{}, err
	}
	before, err := decodeCursor(q.Cursor, 'b')
	if err != nil {
		return BriefVersionPage{}, err
	}
	var page BriefVersionPage
	err = l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, briefSelect+`
			 WHERE v.space_id = $1 AND v.space_id = ANY($2) AND ($3::bigint = 0 OR v.version < $3)
			 ORDER BY v.version DESC
			 LIMIT $4`, q.SpaceID, scope.SpaceIDs(), before, limit+1)
		if err != nil {
			return fmt.Errorf("ledger: list Brief versions: %w", err)
		}
		versions, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Brief, error) {
			b, err := scanBrief(r)
			if err != nil {
				return Brief{}, err
			}
			return *b, nil
		})
		if err != nil {
			return fmt.Errorf("ledger: list Brief versions: %w", err)
		}
		if len(versions) > limit {
			versions = versions[:limit]
			page.HasMore = true
			page.NextCursor = encodeCursor('b', int64(versions[limit-1].Version))
		}
		ptrs := make([]*Brief, len(versions))
		for i := range versions {
			ptrs[i] = &versions[i]
		}
		page.Versions = versions
		return attachBriefReceipts(ctx, tx, scope, ptrs)
	})
	if err != nil {
		return BriefVersionPage{}, err
	}
	return page, nil
}

// ListTargets returns the space's targets in the compiler's order, each
// with its latest run.
func (l *Ledger) ListTargets(ctx context.Context, scope Scope, spaceID uuid.UUID) ([]Target, error) {
	if _, ok := scope.Grant(spaceID); !ok {
		return nil, ErrNotFound
	}
	var out []Target
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, targetSelect+`
			 WHERE t.space_id = $1 AND t.space_id = ANY($2)
			 ORDER BY array_position($3::text[], t.kind), t.path NULLS LAST, t.id`,
			spaceID, scope.SpaceIDs(), kindsOrder())
		if err != nil {
			return fmt.Errorf("ledger: list targets: %w", err)
		}
		targets, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Target, error) { return scanTarget(r) })
		if err != nil {
			return fmt.Errorf("ledger: list targets: %w", err)
		}
		if err := fillTargets(ctx, tx, scope, targets); err != nil {
			return err
		}
		out = make([]Target, len(targets))
		for i, t := range targets {
			out[i] = *t
		}
		return nil
	})
	return out, err
}

func kindsOrder() []string {
	out := make([]string, len(TargetKinds))
	for i, k := range TargetKinds {
		out[i] = string(k)
	}
	return out
}

// GetTarget returns one target in scope, by id.
func (l *Ledger) GetTarget(ctx context.Context, scope Scope, id uuid.UUID) (*Target, error) {
	var out *Target
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		t, err := loadTarget(ctx, tx, scope, id, false)
		if err != nil {
			return err
		}
		out = t
		return fillTargets(ctx, tx, scope, []*Target{t})
	})
	return out, err
}

// CompileRunQuery pages through ListCompileRuns.
type CompileRunQuery struct {
	TargetID uuid.UUID
	Cursor   string
	Limit    int
}

// CompileRunPage is one page of a target's runs, newest first.
type CompileRunPage struct {
	Runs       []CompileRun
	NextCursor string
	HasMore    bool
}

// ListCompileRuns pages through a target's compile runs, newest first.
func (l *Ledger) ListCompileRuns(ctx context.Context, scope Scope, q CompileRunQuery) (CompileRunPage, error) {
	limit, err := pageLimit(q.Limit)
	if err != nil {
		return CompileRunPage{}, err
	}
	before, err := decodeCursor(q.Cursor, 'c')
	if err != nil {
		return CompileRunPage{}, err
	}
	var page CompileRunPage
	err = l.Read(ctx, scope, func(tx pgx.Tx) error {
		if _, err := loadTarget(ctx, tx, scope, q.TargetID, false); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, compileSelect+`
			 WHERE r.target_id = $1 AND r.space_id = ANY($2) AND ($3::bigint = 0 OR r.seq < $3)
			 ORDER BY r.seq DESC
			 LIMIT $4`, q.TargetID, scope.SpaceIDs(), before, limit+1)
		if err != nil {
			return fmt.Errorf("ledger: list compile runs: %w", err)
		}
		page.Runs, err = pgx.CollectRows(rows, scanCompileRun)
		if err != nil {
			return fmt.Errorf("ledger: list compile runs: %w", err)
		}
		return nil
	})
	if err != nil {
		return CompileRunPage{}, err
	}
	if len(page.Runs) > limit {
		page.Runs = page.Runs[:limit]
		page.HasMore = true
		page.NextCursor = encodeCursor('c', page.Runs[limit-1].seq)
	}
	return page, nil
}

// TargetView is a target with what the compile coordinator serves around
// it: the latest good run (the preview), the run whose output is on disk,
// and the open hand edits.
type TargetView struct {
	Target *Target
	// LastGood is the latest run that didn't fail, or nil.
	LastGood *CompileRun
	// Delivered is the run whose output is on disk, or nil.
	Delivered *CompileRun
	// Open are the open observations, oldest first.
	Open []Observation
	// Accepted maps a file to the observation Memax accepted as its
	// baseline (a pulled or overwritten hand edit).
	Accepted map[string]Observation
}

// GetTargetView reads a target and its runs and drift in one snapshot.
func (l *Ledger) GetTargetView(ctx context.Context, scope Scope, id uuid.UUID) (*TargetView, error) {
	var out *TargetView
	err := l.readSnapshot(ctx, scope, func(tx pgx.Tx) error {
		v, err := targetView(ctx, tx, scope, id)
		out = v
		return err
	})
	return out, err
}

func targetView(ctx context.Context, tx pgx.Tx, scope Scope, id uuid.UUID) (*TargetView, error) {
	t, err := loadTarget(ctx, tx, scope, id, false)
	if err != nil {
		return nil, err
	}
	if err := fillTargets(ctx, tx, scope, []*Target{t}); err != nil {
		return nil, err
	}
	v := &TargetView{Target: t, Accepted: map[string]Observation{}}
	if v.LastGood, err = lastGoodRun(ctx, tx, scope, t.ID); err != nil {
		return nil, err
	}
	if t.Delivered != nil && t.Delivered.CompileID != nil {
		if v.Delivered, err = loadCompileRun(ctx, tx, scope, *t.Delivered.CompileID); err != nil {
			return nil, err
		}
	}
	if v.Open, err = loadObservations(ctx, tx, scope, `o.target_id = $1 AND o.status = 'open'`, t.ID); err != nil {
		return nil, err
	}
	var accepted []uuid.UUID
	if t.Delivered != nil {
		for _, f := range t.Delivered.Files {
			if f.Observation != nil {
				accepted = append(accepted, *f.Observation)
			}
		}
	}
	if len(accepted) > 0 {
		obs, err := loadObservations(ctx, tx, scope, `o.target_id = $1 AND o.id = ANY($2)`, t.ID, accepted)
		if err != nil {
			return nil, err
		}
		for _, o := range obs {
			v.Accepted[o.Path] = o
		}
	}
	return v, nil
}

// CompileSnapshot is everything one compile reads, from one snapshot of
// the record: the target, its space, the current Brief, the memories that
// may compile, and the target's latest good run.
type CompileSnapshot struct {
	View  *TargetView
	Space Space
	// Brief is the current version; nil when the space has none.
	Brief *Brief
	// Kept are the space's kept memories, by display number. The compile
	// coordinator decides which of them may compile (quarantined ones
	// never do).
	Kept []Memory
	// Canonical is the path of the space's AGENTS.md target, which shims
	// and scoped rules import or name; "" when it has none.
	Canonical string
}

// CompileSnapshot reads a compile's inputs at REPEATABLE READ. Its
// target's DirtyGen is the generation those inputs are.
func (l *Ledger) CompileSnapshot(ctx context.Context, scope Scope, targetID uuid.UUID) (*CompileSnapshot, error) {
	var out *CompileSnapshot
	err := l.readSnapshot(ctx, scope, func(tx pgx.Tx) error {
		v, err := targetView(ctx, tx, scope, targetID)
		if err != nil {
			return err
		}
		s := &CompileSnapshot{View: v}
		err = tx.QueryRow(ctx, `SELECT id, tenant_id, slug, name, kind, COALESCE(repository, '') FROM v2.spaces WHERE id = $1`,
			v.Target.SpaceID).Scan(&s.Space.ID, &s.Space.TenantID, &s.Space.Slug, &s.Space.Name, &s.Space.Kind, &s.Space.Repository)
		if err != nil {
			return fmt.Errorf("ledger: load space: %w", err)
		}
		b, err := scanBrief(tx.QueryRow(ctx, briefSelect+` WHERE v.space_id = $1 AND v.version = b.current_version`, v.Target.SpaceID))
		switch {
		case errNoRows(err):
		case err != nil:
			return fmt.Errorf("ledger: load Brief: %w", err)
		default:
			s.Brief = b
		}
		rows, err := tx.Query(ctx, memorySelect+` WHERE m.space_id = $1 AND m.lifecycle = 'kept' ORDER BY m.seq`, v.Target.SpaceID)
		if err != nil {
			return fmt.Errorf("ledger: load kept memories: %w", err)
		}
		s.Kept, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Memory, error) {
			m, err := scanMemory(r)
			if err != nil {
				return Memory{}, err
			}
			return *m, nil
		})
		if err != nil {
			return fmt.Errorf("ledger: load kept memories: %w", err)
		}
		err = tx.QueryRow(ctx, `
			SELECT path FROM v2.targets
			 WHERE space_id = $1 AND kind = 'agents_md' AND sync_state <> 'off'
			 ORDER BY (path = 'AGENTS.md') DESC, path LIMIT 1`, v.Target.SpaceID).Scan(&s.Canonical)
		if err != nil && !errNoRows(err) {
			return fmt.Errorf("ledger: load canonical target: %w", err)
		}
		out = s
		return nil
	})
	return out, err
}

package forget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
)

// The forget ledger after a restore (plan 25 §5.13 step 4). A database
// restored to a point in time (Neon PITR, 7 days) brings back the words of
// every memory forgotten after that point. The forget ledger is their
// list, IDs and refs only: each Forget's op is copied to object storage by
// its propagation job, where the restore can't take it back, and kept in
// the tombstones too. Reapply reads both, oldest first, and re-applies
// each op through the ledger (ReapplyForget, as Memax): whatever is back
// is forgotten again, with new receipts and the tombstones the ledger
// recorded; what is still forgotten is left alone. A space that was
// deleted is retired again and its hub deleted, as V1's delete did.

// ReapplyOptions narrows a run.
type ReapplyOptions struct {
	// Spaces limits the run to these spaces (none: every space).
	Spaces []uuid.UUID
	// DryRun lists the ops without writing anything.
	DryRun bool
}

// ReapplyReport is what a run found and did.
type ReapplyReport struct {
	// Ops are the forget-ledger ops read, from either copy.
	Ops int
	// FromObjectStore counts the ops whose object-storage copy was read.
	FromObjectStore int
	// Reapplied are the ops that forgot something again.
	Reapplied []uuid.UUID
	// Unchanged are the ops whose objects were all still forgotten.
	Unchanged []uuid.UUID
	// Gone are the ops of spaces that no longer exist (deleted, and the
	// deletion survived): nothing to re-apply.
	Gone []uuid.UUID
	// HubsDeleted are the retired spaces whose hub the run deleted again.
	HubsDeleted []uuid.UUID
}

// Reapply re-applies the forget ledger. store may be nil (only the
// database's copy is read); it must implement objectstore.Lister to be
// read. pool is the login role's, for deleting a retired space's hub.
func Reapply(ctx context.Context, l *ledger.Ledger, pool *pgxpool.Pool, store objectstore.Store, opts ReapplyOptions, log *slog.Logger) (ReapplyReport, error) {
	var rep ReapplyReport
	if l == nil {
		return rep, ledger.ErrDisabled
	}
	if log == nil {
		log = slog.Default()
	}
	ops := map[uuid.UUID]ledger.ForgetLedgerOp{}
	want := func(space uuid.UUID) bool { return len(opts.Spaces) == 0 || slices.Contains(opts.Spaces, space) }

	// The object-storage copy first: it is the one a restore can't touch.
	if lister, ok := store.(objectstore.Lister); ok && store != nil {
		prefixes := []string{ledger.ForgetLedgerPrefix}
		if len(opts.Spaces) > 0 {
			prefixes = prefixes[:0]
			for _, s := range opts.Spaces {
				prefixes = append(prefixes, ledger.ForgetLedgerPrefix+s.String()+"/")
			}
		}
		for _, prefix := range prefixes {
			keys, err := lister.List(ctx, prefix)
			if err != nil {
				return rep, fmt.Errorf("forget ledger: list %s: %w", prefix, err)
			}
			for _, key := range keys {
				if !strings.HasSuffix(key, ".json") {
					continue
				}
				op, err := readOp(ctx, store, key)
				if err != nil {
					return rep, err
				}
				if want(op.SpaceID) {
					ops[op.OpID] = op
					rep.FromObjectStore++
				}
			}
		}
	} else if store != nil {
		log.WarnContext(ctx, "forget ledger: object storage can't list; reading the database's copy only")
	}

	// Then the tombstones, for any op whose copy wasn't written yet.
	spaces := opts.Spaces
	if len(spaces) == 0 {
		var err error
		if spaces, err = l.ForgottenSpaces(ctx); err != nil {
			return rep, err
		}
	}
	for _, space := range spaces {
		scope, err := l.SpaceScope(ctx, space)
		if errors.Is(err, ledger.ErrNotFound) {
			continue
		}
		if err != nil {
			return rep, err
		}
		dbOps, err := l.ForgetLedgerOps(ctx, scope, space)
		if err != nil {
			return rep, err
		}
		for _, op := range dbOps {
			if _, ok := ops[op.OpID]; !ok {
				ops[op.OpID] = op
			}
		}
	}

	ordered := make([]ledger.ForgetLedgerOp, 0, len(ops))
	for _, op := range ops {
		ordered = append(ordered, op)
	}
	slices.SortFunc(ordered, func(a, b ledger.ForgetLedgerOp) int {
		if c := a.ForgottenAt.Compare(b.ForgottenAt); c != 0 {
			return c
		}
		return strings.Compare(a.OpID.String(), b.OpID.String())
	})
	rep.Ops = len(ordered)

	for _, op := range ordered {
		scope, err := l.SpaceScope(ctx, op.SpaceID)
		if errors.Is(err, ledger.ErrNotFound) {
			rep.Gone = append(rep.Gone, op.OpID)
			continue
		}
		if err != nil {
			return rep, err
		}
		if opts.DryRun {
			log.InfoContext(ctx, "forget ledger: op", "op", op.OpID.String(), "space_id", op.SpaceID.String(),
				"kind", op.Kind, "objects", len(op.Entries), "retired", op.Retired, "forgotten_at", op.ForgottenAt)
			continue
		}
		res, err := l.Apply(ctx, &ledger.ReapplyForget{
			Meta: ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: scope.Narrow(op.SpaceID),
				Via: policy.ViaSystem, IdempotencyKey: "reapply-forget:" + uuid.NewString()},
			Op: op,
		})
		if errors.Is(err, ledger.ErrNotFound) {
			// A deleted space keeps its ledger row (its receipts and
			// tombstones refer to it) but not its hub: it is gone.
			rep.Gone = append(rep.Gone, op.OpID)
			continue
		}
		if err != nil {
			return rep, fmt.Errorf("forget ledger: re-apply %s: %w", op.OpID, err)
		}
		if res.Outcome == ledger.OutcomeRefused {
			return rep, fmt.Errorf("forget ledger: re-apply %s: refused: %s", op.OpID, res.Policy.Message)
		}
		if res.Unchanged {
			rep.Unchanged = append(rep.Unchanged, op.OpID)
		} else {
			rep.Reapplied = append(rep.Reapplied, op.OpID)
			log.InfoContext(ctx, "forget ledger: re-applied", "op", op.OpID.String(), "space_id", op.SpaceID.String(),
				"kind", op.Kind, "receipts", len(res.Receipts))
		}
		if op.Retired && pool != nil {
			// The space was deleted: its V2 rows are retired again, and the
			// hub goes as V1's delete took it.
			tag, err := pool.Exec(ctx, `DELETE FROM public.hubs WHERE id = $1`, op.SpaceID)
			if err != nil {
				return rep, fmt.Errorf("forget ledger: delete the hub of %s: %w", op.SpaceID, err)
			}
			if tag.RowsAffected() > 0 {
				rep.HubsDeleted = append(rep.HubsDeleted, op.SpaceID)
			}
		}
	}
	return rep, nil
}

func readOp(ctx context.Context, store objectstore.Store, key string) (ledger.ForgetLedgerOp, error) {
	var op ledger.ForgetLedgerOp
	obj, err := store.Get(ctx, key)
	if err != nil {
		return op, fmt.Errorf("forget ledger: read %s: %w", key, err)
	}
	defer func() { _ = obj.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(obj.Body, 16<<20))
	if err != nil {
		return op, fmt.Errorf("forget ledger: read %s: %w", key, err)
	}
	if err := json.Unmarshal(body, &op); err != nil {
		return op, fmt.Errorf("forget ledger: %s isn't an op: %w", key, err)
	}
	if op.OpID == uuid.Nil || op.SpaceID == uuid.Nil {
		return op, fmt.Errorf("forget ledger: %s names no op or space", key)
	}
	return op, nil
}

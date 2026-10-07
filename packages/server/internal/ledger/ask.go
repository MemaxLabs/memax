package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// What ⌘K Ask needs from the ledger (plan 25 §5.11, internal/ask). An Ask
// is a read: it never goes through Apply and writes no receipt. Three
// pieces live here because they touch the v2 schema:
//
//   - memory sources: a kept answer cites the memories it came from, and
//     each one's trust is the cited memory's (resolveMemorySources);
//   - the Ask meter: asks answered this month, per person (migration 041),
//     which policy.Decide weighs against the plan's limit;
//   - the receipt each source shows ("kept by ZZ · Oct 2").

// resolveMemorySources settles the sources of kind memory: each must name
// a kept memory in the space being written, and takes that memory's trust
// (never above what the actor could write itself), so a statement built
// from memories is never trusted more than they are. The caller's trust
// for such a source is ignored. Other kinds pass through.
func (w *writer) resolveMemorySources(ctx context.Context, spaceID uuid.UUID, srcs []resolvedSource, actorTrust policy.Trust) ([]resolvedSource, error) {
	for i := range srcs {
		s := &srcs[i]
		if s.Kind != SourceMemory {
			continue
		}
		var id uuid.UUID
		if parsed, err := uuid.Parse(s.Ref); err == nil {
			id = parsed
		} else if p, _, ok := ParseRef(s.Ref); !ok || p != PrefixMemory {
			return nil, invalid("sources.ref", "a memory source names a memory by its display ID, like M-0219")
		}
		var (
			seq   int64
			lc    lifecycle.Lifecycle
			trust policy.Trust
		)
		q := `SELECT id, seq, lifecycle, trust FROM v2.memories WHERE space_id = $1 AND `
		var arg any = id
		if id == uuid.Nil {
			_, n, _ := ParseRef(s.Ref)
			q += `seq = $2`
			arg = n
		} else {
			q += `id = $2`
		}
		err := w.tx.QueryRow(ctx, q, spaceID, arg).Scan(&id, &seq, &lc, &trust)
		if errNoRows(err) {
			return nil, invalid("sources.ref", "%s isn't a memory in this space", s.Ref)
		}
		if err != nil {
			return nil, fmt.Errorf("ledger: resolve memory source: %w", err)
		}
		if lc != lifecycle.Kept {
			return nil, invalid("sources.ref", "%s isn't kept, so nothing can rest on it yet", FormatRef(PrefixMemory, seq))
		}
		locator, _ := json.Marshal(map[string]string{"memory": id.String()})
		s.Ref, s.URI, s.Locator = FormatRef(PrefixMemory, seq), "", locator
		s.trust = policy.MinTrust(trust, actorTrust)
	}
	return srcs, nil
}

// AskPeriod is the month an ask counts in: its first day, UTC.
func AskPeriod(at time.Time) time.Time {
	at = at.UTC()
	return time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// CountAsk counts one ask for scope's person in the month of at and
// returns how many they have now, this one included. It is atomic, so two
// asks at once get different numbers: policy.Decide judges the plan's
// limit on the number before this one, and UncountAsk gives it back when
// the ask never reaches the model or is refused.
func (l *Ledger) CountAsk(ctx context.Context, scope Scope, at time.Time) (int, error) {
	if l == nil {
		return 0, ErrDisabled
	}
	if scope.PersonID == uuid.Nil {
		return 0, invalid("person", "only a person's asks are counted")
	}
	var n int
	err := l.meter(ctx, scope, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO v2.ask_usage (person_id, period, asks) VALUES ($1, $2, 1)
			ON CONFLICT (person_id, period) DO UPDATE SET asks = v2.ask_usage.asks + 1, updated_at = now()
			RETURNING asks`, scope.PersonID, AskPeriod(at)).Scan(&n)
	})
	return n, err
}

// UncountAsk takes back an ask CountAsk counted.
func (l *Ledger) UncountAsk(ctx context.Context, scope Scope, at time.Time) error {
	if l == nil {
		return ErrDisabled
	}
	return l.meter(ctx, scope, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE v2.ask_usage SET asks = GREATEST(asks - 1, 0), updated_at = now()
			 WHERE person_id = $1 AND period = $2`, scope.PersonID, AskPeriod(at))
		return err
	})
}

// AsksThisMonth reads how many asks scope's person has counted in the
// month of at.
func (l *Ledger) AsksThisMonth(ctx context.Context, scope Scope, at time.Time) (int, error) {
	if l == nil {
		return 0, ErrDisabled
	}
	var n int
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT asks FROM v2.ask_usage WHERE person_id = $1 AND period = $2`,
			scope.PersonID, AskPeriod(at)).Scan(&n)
		if errNoRows(err) {
			return nil
		}
		return err
	})
	return n, err
}

// meter runs fn in a short read-write transaction as memax_v2 in scope
// (RLS keys ask_usage on the person). It is bookkeeping, not a command:
// no receipt, no idempotency claim.
func (l *Ledger) meter(ctx context.Context, scope Scope, fn func(pgx.Tx) error) error {
	tx, _, err := l.begin(ctx, scope, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// fn writes one statement: its COMMIT goes in the same round trip.
	if t, ok := tx.(*scopedTx); ok {
		t.commitWithNext()
	}
	if err := fn(tx); err != nil {
		return mapDBError(fmt.Errorf("ledger: ask meter: %w", err))
	}
	return mapDBError(tx.Commit(ctx))
}

// SourceReceiptActions are the receipts a source shows for a kept memory:
// how it came to read as it does.
var SourceReceiptActions = []Action{ActionKept, ActionEdited, ActionResolved, ActionMerged, ActionAnswered}

// LatestReceipts returns, for each memory, its newest receipt among
// actions, in scope. Memories with none are absent from the map.
func (l *Ledger) LatestReceipts(ctx context.Context, scope Scope, memories []uuid.UUID, actions []Action) (map[uuid.UUID]Receipt, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	out := map[uuid.UUID]Receipt{}
	if len(memories) == 0 {
		return out, nil
	}
	verbs := make([]string, len(actions))
	for i, a := range actions {
		verbs[i] = string(a)
	}
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT DISTINCT ON (object_id) id, seq, tenant_id, space_id, object_kind, object_id, object_ref, action,
			       actor_kind, actor_id, COALESCE(agent, ''), via, COALESCE(assurance, ''), COALESCE(session_ref, ''),
			       source, COALESCE(reason, ''), occurred_at, recorded_at, stream_id, stream_version
			  FROM v2.receipts
			 WHERE object_kind = 'memory' AND object_id = ANY($1) AND space_id = ANY($2) AND action = ANY($3)
			 ORDER BY object_id, seq DESC`, memories, scope.SpaceIDs(), verbs)
		if err != nil {
			return err
		}
		rcs, err := pgx.CollectRows(rows, scanReceipt)
		if err != nil {
			return err
		}
		for _, rc := range rcs {
			out[rc.ObjectID] = rc
		}
		return nil
	})
	return out, err
}

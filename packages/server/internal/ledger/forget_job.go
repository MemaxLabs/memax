package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// What the forget propagation job (internal/forget) reads and records.
// Propagation is bookkeeping about delivery, not a change to the record:
// it writes no receipts (the compiles it runs write their own), like a
// gate's delivered_at. Every read and write runs as memax_v2 in the
// space's scope.

// ForgetOpView is one Forget as its propagation job sees it.
type ForgetOpView struct {
	OpID     uuid.UUID
	SpaceID  uuid.UUID
	TenantID uuid.UUID
	Kind     string
	Status   string
	// ForgottenAt is when the Forget committed.
	ForgottenAt time.Time
	// Refs are the forgotten memories' display IDs.
	Refs []string
	// Steps are the op's destinations, in order.
	Steps []ForgetStep
	// Retired is set when the Forget deleted the space.
	Retired bool
}

// ForgetStep is one destination of a Forget.
type ForgetStep struct {
	ID     uuid.UUID
	Kind   string // target | artifacts | caches | ledger
	Target *uuid.UUID
	Label  string
	Status string // pending | done | held | stopped | failed
	Detail map[string]any
}

// The step statuses the job writes.
const (
	PropagationPending = "pending"
	PropagationDone    = "done"
	PropagationHeld    = "held"
	PropagationStopped = "stopped"
	PropagationFailed  = "failed"
)

// ForgetOp reads one Forget for its propagation job.
func (l *Ledger) ForgetOp(ctx context.Context, scope Scope, opID uuid.UUID) (*ForgetOpView, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	var out *ForgetOpView
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		v := &ForgetOpView{OpID: opID}
		var retired *time.Time
		err := tx.QueryRow(ctx, `
			SELECT t.space_id, t.tenant_id, t.object_kind, t.status, t.forgotten_at,
			       (SELECT l.retired_at FROM v2.space_ledgers l WHERE l.space_id = t.space_id)
			  FROM v2.tombstones t WHERE t.id = $1 AND t.space_id = ANY ($2)`, opID, scope.SpaceIDs()).
			Scan(&v.SpaceID, &v.TenantID, &v.Kind, &v.Status, &v.ForgottenAt, &retired)
		if errNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("ledger: forget op: %w", err)
		}
		v.Retired = retired != nil
		rows, err := tx.Query(ctx, `
			SELECT object_ref FROM v2.tombstones WHERE op_id = $1 AND object_kind = 'memory' ORDER BY object_ref`, opID)
		if err != nil {
			return fmt.Errorf("ledger: forget op: %w", err)
		}
		if v.Refs, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return fmt.Errorf("ledger: forget op: %w", err)
		}
		rows, err = tx.Query(ctx, `
			SELECT id, destination_kind, destination_id, COALESCE(label, ''), status, detail
			  FROM v2.propagations WHERE op_id = $1
			 ORDER BY CASE destination_kind WHEN 'ledger' THEN 0 WHEN 'target' THEN 1 WHEN 'artifacts' THEN 2 ELSE 3 END,
			          created_at, id`, opID)
		if err != nil {
			return fmt.Errorf("ledger: forget op: %w", err)
		}
		v.Steps, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ForgetStep, error) {
			var s ForgetStep
			var detail []byte
			if err := r.Scan(&s.ID, &s.Kind, &s.Target, &s.Label, &s.Status, &detail); err != nil {
				return s, err
			}
			s.Detail = map[string]any{}
			return s, json.Unmarshal(detail, &s.Detail)
		})
		if err != nil {
			return fmt.Errorf("ledger: forget op: %w", err)
		}
		out = v
		return nil
	})
	return out, err
}

// MarkForgetStep records how one destination of a Forget went. detail is
// merged into the step's (ids, refs and counts only).
func (l *Ledger) MarkForgetStep(ctx context.Context, scope Scope, stepID uuid.UUID, status string, detail map[string]any) error {
	if l == nil {
		return ErrDisabled
	}
	switch status {
	case PropagationPending, PropagationDone, PropagationHeld, PropagationStopped, PropagationFailed:
	default:
		return invalid("status", "unknown step status %q", status)
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	if detail == nil {
		raw = []byte(`{}`)
	}
	return l.meter(ctx, scope, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE v2.propagations
			   SET status = $2, detail = detail || $3::jsonb, updated_at = now(),
			       done_at = CASE WHEN $2 IN ('done', 'held', 'stopped') THEN COALESCE(done_at, now()) ELSE NULL END
			 WHERE id = $1`, stepID, status, raw)
		return err
	})
}

// CompleteForget marks a Forget's propagation done, with its summary.
func (l *Ledger) CompleteForget(ctx context.Context, scope Scope, opID uuid.UUID, summary map[string]any) error {
	if l == nil {
		return ErrDisabled
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	return l.meter(ctx, scope, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE v2.tombstones SET status = 'done', propagation = $2, completed_at = COALESCE(completed_at, now()), updated_at = now()
			 WHERE op_id = $1 AND status <> 'done'`, opID, raw)
		return err
	})
}

// ForgetLedger is the op as the forget ledger keeps it (ids only).
func (l *Ledger) ForgetLedger(ctx context.Context, scope Scope, spaceID, opID uuid.UUID) (*ForgetLedgerOp, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	var out *ForgetLedgerOp
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		ops, err := forgetLedgerOps(ctx, tx, spaceID, &opID)
		if err != nil {
			return err
		}
		if len(ops) == 0 {
			return ErrNotFound
		}
		out = &ops[0]
		return nil
	})
	return out, err
}

// ArtifactSet is every stored artifact a Forget must re-render: the
// compile runs whose output held a forgotten memory, and every drift
// observation of the space.
type ArtifactSet struct {
	Compiles     []string
	Observations []string
}

// ForgetArtifacts lists the artifacts that may hold the refs.
func (l *Ledger) ForgetArtifacts(ctx context.Context, scope Scope, spaceID uuid.UUID, refs []string) (ArtifactSet, error) {
	var out ArtifactSet
	if l == nil {
		return out, ErrDisabled
	}
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT artifact_key FROM v2.compile_runs
			 WHERE space_id = $1 AND artifact_key IS NOT NULL AND refs && $2 ORDER BY seq`, spaceID, refs)
		if err != nil {
			return err
		}
		if out.Compiles, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT DISTINCT artifact_key FROM v2.target_observations WHERE space_id = $1 ORDER BY 1`, spaceID)
		if err != nil {
			return err
		}
		out.Observations, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return out, fmt.Errorf("ledger: forget artifacts: %w", err)
	}
	return out, nil
}

// ForgottenRefs lists the display IDs of the space's forgotten memories,
// so what comes back from outside (a hand-edited file) never brings their
// lines into Memax again.
func (l *Ledger) ForgottenRefs(ctx context.Context, scope Scope, spaceID uuid.UUID) ([]string, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	var out []string
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT seq FROM v2.memories WHERE space_id = $1 AND lifecycle = 'forgotten' ORDER BY seq`, spaceID)
		if err != nil {
			return err
		}
		seqs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		for _, s := range seqs {
			out = append(out, FormatRef(PrefixMemory, s))
		}
		return err
	})
	return out, err
}

// TakeNotices returns the notices a connection hasn't been told yet and
// marks them told, in one statement: each is delivered once. via names the
// MCP tool (or api) the notices ride on.
func (l *Ledger) TakeNotices(ctx context.Context, scope Scope, connection uuid.UUID, via string, limit int) ([]Notice, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	if connection == uuid.Nil {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if len(via) > 64 {
		via = via[:64]
	}
	var out []Notice
	err := l.meter(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			WITH taken AS (
			    UPDATE v2.agent_notices n SET delivered_at = now(), delivered_via = $2
			     WHERE n.id IN (SELECT id FROM v2.agent_notices
			                     WHERE connection_id = $1 AND delivered_at IS NULL
			                     ORDER BY created_at, id LIMIT $3 FOR UPDATE SKIP LOCKED)
			    RETURNING n.id, n.space_id, n.op_id, n.kind, n.refs, n.read_it, n.created_at
			)
			SELECT t.id, t.space_id, t.op_id, t.kind, t.refs, t.read_it, t.created_at, COALESCE(s.name, '')
			  FROM taken t LEFT JOIN v2.spaces s ON s.id = t.space_id
			 ORDER BY t.created_at, t.id`, connection, via, limit)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Notice, error) {
			var n Notice
			err := r.Scan(&n.ID, &n.SpaceID, &n.OpID, &n.Kind, &n.Refs, &n.ReadIt, &n.At, &n.Space)
			return n, err
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: notices: %w", err)
	}
	return out, nil
}

// PendingNotices lists a connection's notices not yet delivered, without
// marking them (the stdio MCP server reads, then acknowledges).
func (l *Ledger) PendingNotices(ctx context.Context, scope Scope, connection uuid.UUID) ([]Notice, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	var out []Notice
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT n.id, n.space_id, n.op_id, n.kind, n.refs, n.read_it, n.created_at, COALESCE(s.name, '')
			  FROM v2.agent_notices n LEFT JOIN v2.spaces s ON s.id = n.space_id
			 WHERE n.connection_id = $1 AND n.delivered_at IS NULL
			 ORDER BY n.created_at, n.id LIMIT 100`, connection)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Notice, error) {
			var n Notice
			err := r.Scan(&n.ID, &n.SpaceID, &n.OpID, &n.Kind, &n.Refs, &n.ReadIt, &n.At, &n.Space)
			return n, err
		})
		return err
	})
	if out == nil {
		out = []Notice{}
	}
	return out, err
}

// AckNotices marks a connection's notices delivered (only its own, once).
func (l *Ledger) AckNotices(ctx context.Context, scope Scope, connection uuid.UUID, ids []uuid.UUID, via string) (int, error) {
	if l == nil {
		return 0, ErrDisabled
	}
	if len(via) > 64 {
		via = via[:64]
	}
	var n int64
	err := l.meter(ctx, scope, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE v2.agent_notices SET delivered_at = now(), delivered_via = $3
			 WHERE connection_id = $1 AND id = ANY ($2) AND delivered_at IS NULL`, connection, ids, via)
		n = tag.RowsAffected()
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("ledger: notices: %w", err)
	}
	return int(n), nil
}

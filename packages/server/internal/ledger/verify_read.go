package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/conditions"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
)

// StaleCauseKind is why a memory is stale.
type StaleCauseKind string

// The causes.
const (
	// StaleCauseDate: its date to check again passed.
	StaleCauseDate StaleCauseKind = "date"
	// StaleCauseCheck: one of its conditions stopped holding on the default
	// branch (checks.go).
	StaleCauseCheck StaleCauseKind = "check"
)

// StaleCause is one reason a memory is stale, typed so a screen can say it
// in any language. It never holds a file's contents; a check's names the
// condition and the paths that changed, which the memory's words named.
type StaleCause struct {
	Kind StaleCauseKind `json:"kind"`
	// StaleAfter is the date that passed (StaleCauseDate).
	StaleAfter *time.Time `json:"stale_after,omitempty"`

	// A check (StaleCauseCheck): the condition that stopped holding, what
	// changed, where and when: the commit the failure began at, and the
	// run that first saw it.
	Condition *conditions.Condition `json:"condition,omitempty"`
	Evidence  *CheckEvidence        `json:"evidence,omitempty"`
	Branch    string                `json:"branch,omitempty"`
	Commit    string                `json:"commit,omitempty"`
	CommitAt  *time.Time            `json:"commit_at,omitempty"`
	CheckedAt *time.Time            `json:"checked_at,omitempty"`
	Observer  string                `json:"observer,omitempty"`
}

// StaleReport is why a memory is stale, for VerifyStale: its words and
// version (the If-Match a Verify sends), the receipt that flagged it, the
// keep it was flagged after and how often agents read it since, and the
// causes. What a Forget would do is PreviewForget's.
type StaleReport struct {
	MemoryID  uuid.UUID      `json:"memory_id"`
	Ref       string         `json:"ref"`
	SpaceID   uuid.UUID      `json:"space_id"`
	Version   int            `json:"version"`
	Statement string         `json:"statement"`
	Kind      Kind           `json:"kind"`
	State     lifecycle.Mark `json:"state"`
	// Stale says it is flagged now; false when someone answered it since.
	Stale      bool       `json:"stale"`
	StaleAfter *time.Time `json:"stale_after,omitempty"`
	// Flagged is the receipt that set the flag (the stamp in the footer).
	Flagged *Receipt `json:"flagged,omitempty"`
	// Kept is its latest keep or verification, and ReadsSince the reads of
	// it since that day (directly, or in a compile that held it).
	Kept       *Receipt     `json:"kept,omitempty"`
	ReadsSince int          `json:"reads_since"`
	Causes     []StaleCause `json:"causes"`
}

// GetStaleReport says why a memory is stale, in one round trip: BEGIN, the
// scope, five statements that each find the memory by its ref, and
// COMMIT (ReadBatch).
func (l *Ledger) GetStaleReport(ctx context.Context, scope Scope, ref string) (*StaleReport, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	idExpr, args, err := memoryIDExpr(scope, ref)
	if err != nil {
		return nil, err
	}
	var (
		out     *StaleReport
		flags   []string
		conds   []byte
		flagged []Receipt
		kept    []Receipt
		reads   int
		failing []failingCheck
	)
	b := &pgx.Batch{}
	b.Queue(`
		SELECT m.id, m.seq, m.space_id, m.current_version, COALESCE(v.statement, ''), m.kind, m.lifecycle, m.flags, m.stale_after,
		       m.conditions
		  FROM v2.memories m
		  LEFT JOIN v2.memory_versions v ON v.memory_id = m.id AND v.version = m.current_version
		 WHERE m.id = `+idExpr+` AND m.space_id = ANY ($2)`, args...).QueryRow(func(row pgx.Row) error {
		r := &StaleReport{}
		var seq int64
		var lc lifecycle.Lifecycle
		err := row.Scan(&r.MemoryID, &seq, &r.SpaceID, &r.Version, &r.Statement, &r.Kind, &lc, &flags, &r.StaleAfter, &conds)
		if errNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		fs, err := lifecycle.ParseFlags(flags)
		if err != nil {
			return err
		}
		r.Ref, r.State = FormatRef(PrefixMemory, seq), lifecycle.DisplayState(lc, fs)
		r.Stale = lc == lifecycle.Kept && fs.Has(lifecycle.Stale)
		out = r
		return nil
	})
	b.Queue(staleFlagSelect+`
		 WHERE r.stream_id = `+idExpr+` AND r.space_id = ANY ($2) AND `+staleFlagCond+`
		 ORDER BY r.stream_version DESC LIMIT 1`, args...).Query(func(rows pgx.Rows) error {
		flagged, err = pgx.CollectRows(rows, scanReceipt)
		return err
	})
	b.Queue(staleFlagSelect+`
		 WHERE r.stream_id = `+idExpr+` AND r.space_id = ANY ($2) AND r.action IN ('kept', 'verified')
		 ORDER BY r.stream_version DESC LIMIT 1`, args...).Query(func(rows pgx.Rows) error {
		kept, err = pgx.CollectRows(rows, scanReceipt)
		return err
	})
	// Reads since the latest keep: of the memory itself, and of every compile
	// that held it (a session-start load reads each fact in it).
	b.Queue(`
		WITH m AS (SELECT id, space_id, seq FROM v2.memories WHERE id = `+idExpr+` AND space_id = ANY ($2)),
		     k AS (SELECT max(r.occurred_at) AS at FROM v2.receipts r, m
		            WHERE r.stream_id = m.id AND r.space_id = m.space_id AND r.action IN ('kept', 'verified'))
		SELECT COALESCE(sum(rr.reads), 0)::int
		  FROM v2.read_rollups rr, m, k
		 WHERE rr.space_id = m.space_id
		   AND (rr.subject_id = m.id
		        OR rr.subject_id IN (SELECT c.id FROM v2.compile_runs c
		                              WHERE c.space_id = m.space_id
		                                AND c.refs @> ARRAY['M-' || CASE WHEN m.seq < 10000 THEN lpad(m.seq::text, 4, '0') ELSE m.seq::text END]))
		   AND (k.at IS NULL OR rr.day >= (k.at AT TIME ZONE 'UTC')::date)`, args...).QueryRow(func(row pgx.Row) error {
		return row.Scan(&reads)
	})
	// Its armed conditions that fail now on the default branch, with the run
	// that first saw each failure.
	b.Queue(`
		SELECT c.condition_id, c.evidence, c.failed_commit, c.failed_commit_at, r.branch, r.checked_at, r.observer
		  FROM v2.condition_checks c
		  LEFT JOIN v2.check_runs r ON r.id = c.first_failed_run_id AND r.space_id = c.space_id
		 WHERE c.memory_id = `+idExpr+` AND c.space_id = ANY ($2) AND c.context = 'default'
		   AND c.outcome = 'fails' AND c.armed_at IS NOT NULL
		 ORDER BY c.condition_id`, args...).Query(func(rows pgx.Rows) error {
		for rows.Next() {
			var f failingCheck
			var evidence []byte
			var branch, observer *string
			if err := rows.Scan(&f.condition, &evidence, &f.commit, &f.commitAt, &branch, &f.checkedAt, &observer); err != nil {
				return err
			}
			if len(evidence) > 0 {
				f.evidence = &CheckEvidence{}
				if err := json.Unmarshal(evidence, f.evidence); err != nil {
					return err
				}
			}
			f.branch, f.observer = deref0(branch), deref0(observer)
			failing = append(failing, f)
		}
		return rows.Err()
	})
	if err := l.ReadBatch(ctx, scope, b); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == sqlstateCardinality {
			return nil, ErrAmbiguousRef
		}
		return nil, fmt.Errorf("ledger: read why %s is stale: %w", ref, err)
	}
	if out == nil {
		return nil, ErrNotFound
	}
	if len(flagged) > 0 {
		out.Flagged = &flagged[0]
	}
	if len(kept) > 0 {
		out.Kept = &kept[0]
	}
	out.ReadsSince = reads
	out.Causes = staleCauses(out, l.now())
	if out.Stale && len(failing) > 0 {
		cs, err := storedConditions(conds)
		if err != nil {
			return nil, err
		}
		for _, f := range failing {
			for i := range cs {
				if cs[i].ID != f.condition {
					continue
				}
				out.Causes = append(out.Causes, StaleCause{Kind: StaleCauseCheck, Condition: &cs[i], Evidence: f.evidence,
					Branch: f.branch, Commit: deref0(f.commit), CommitAt: f.commitAt, CheckedAt: f.checkedAt, Observer: f.observer})
			}
		}
	}
	return out, nil
}

// failingCheck is a failing condition's row, as the stale report reads it.
type failingCheck struct {
	condition string
	evidence  *CheckEvidence
	commit    *string
	commitAt  *time.Time
	branch    string
	checkedAt *time.Time
	observer  string
}

func deref0[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// staleCauses are the reasons the memory is stale now.
func staleCauses(r *StaleReport, now time.Time) []StaleCause {
	causes := []StaleCause{}
	if !r.Stale {
		return causes
	}
	if r.StaleAfter != nil && !r.StaleAfter.After(now) {
		causes = append(causes, StaleCause{Kind: StaleCauseDate, StaleAfter: r.StaleAfter})
	}
	return causes
}

// memoryIDExpr is SQL that finds a memory by ref as a scalar ($1), with
// the scope's spaces as $2: a uuid as it is, a display ID through a scalar
// subquery that raises cardinality_violation when it names memories in
// more than one space of the scope (ErrAmbiguousRef).
func memoryIDExpr(scope Scope, ref string) (string, []any, error) {
	ref = strings.TrimSpace(ref)
	if id, err := uuid.Parse(ref); err == nil {
		return `$1::uuid`, []any{id, scope.SpaceIDs()}, nil
	}
	n, err := memorySeq(ref)
	if err != nil {
		return "", nil, err
	}
	return `(SELECT id FROM v2.memories WHERE seq = $1 AND space_id = ANY ($2))`, []any{n, scope.SpaceIDs()}, nil
}

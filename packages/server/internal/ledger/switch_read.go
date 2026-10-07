package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// V1DreamRun is a Dream run from V1: read-only edition history (plan 25
// §10). Counts only: V1's report isn't served (its words are V1's, and may
// quote what was forgotten since), and its actions can't be undone.
type V1DreamRun struct {
	ID             uuid.UUID  `json:"id"`
	Status         string     `json:"status"`
	Mode           string     `json:"mode"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Scanned        int        `json:"scanned"`
	Merged         int        `json:"merged"`
	Contradictions int        `json:"contradictions"`
	Archived       int        `json:"archived"`
	Organized      int        `json:"organized"`
	Restructured   int        `json:"restructured"`
	Actions        int        `json:"actions"`
}

// ListV1DreamRuns lists a space's V1 Dream runs, newest first (at most 200).
func (l *Ledger) ListV1DreamRuns(ctx context.Context, scope Scope, spaceID uuid.UUID) ([]V1DreamRun, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	if _, ok := scope.Grant(spaceID); !ok {
		return nil, ErrNotFound
	}
	var out []V1DreamRun
	err := l.Read(ctx, scope.Narrow(spaceID), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, status, mode, started_at, finished_at, memories_scanned, duplicates_merged, contradictions_found,
			       memories_archived, memories_organized, topics_restructured, actions
			  FROM v2.v1_dream_runs WHERE space_id = $1 ORDER BY started_at DESC, id DESC LIMIT 200`, spaceID)
		if err != nil {
			return fmt.Errorf("ledger: V1 Dream runs: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (V1DreamRun, error) {
			var d V1DreamRun
			err := r.Scan(&d.ID, &d.Status, &d.Mode, &d.StartedAt, &d.FinishedAt, &d.Scanned, &d.Merged, &d.Contradictions,
				&d.Archived, &d.Organized, &d.Restructured, &d.Actions)
			return d, err
		})
		return err
	})
	return nonNilSlice(out), err
}

package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Holds (DriftResolve, Pull: "Both lines go to Review as proposals. The
// file stays as it is until you keep or reject them.").
//
// A pull makes the edited file the target's baseline (resolveDrift). While
// any proposal that pull wrote is still proposed, the file is held: no
// compile is delivered over it, and the target shows `held`. Once the last
// one is kept, rejected, folded or forgotten, the hold lifts by itself and
// the latest compile is due over the edited file, which Memax may now
// replace: kept lines come back compiled, rejected ones go.
//
// The hold is derived, never stored: from the pulled observation that is
// the file's baseline, and the lifecycles of the proposals its resolution
// names. So every way a proposal is decided lifts it, with no hook in each
// command, and an Undo that makes one a proposal again holds the file
// again, as long as nothing was delivered over the edit since (a delivery
// replaces the baseline, and the edit is no longer on disk to hold).
//
// A pull that wrote no proposals (removals only) holds nothing: the file
// stays until the next compile delivers over it, as before.

// pulledProposals is, per pulled observation, the proposals its pull
// wrote and the ones still proposed.
type pulledProposals struct {
	since   time.Time
	pending []string
}

// fillHolds sets the targets' holds, their baseline files' Held flags and
// the state they show. It reads, and never changes, the record.
func fillHolds(ctx context.Context, tx pgx.Tx, targets []*Target) error {
	var obs []uuid.UUID
	for _, t := range targets {
		t.Holds, t.shown = nil, ""
		if t.Delivered == nil {
			continue
		}
		for _, f := range t.Delivered.Files {
			if f.Observation != nil {
				obs = append(obs, *f.Observation)
			}
		}
	}
	if len(obs) == 0 {
		return nil
	}
	byObs, err := loadPulledProposals(ctx, tx, obs)
	if err != nil {
		return err
	}
	for _, t := range targets {
		if t.Delivered == nil {
			continue
		}
		lifted := false
		for i := range t.Delivered.Files {
			f := &t.Delivered.Files[i]
			if f.Observation == nil {
				continue
			}
			held := false
			if p := byObs[*f.Observation]; p != nil {
				if len(p.pending) > 0 {
					held = true
					t.Holds = append(t.Holds, TargetHold{Observation: *f.Observation, Path: f.Path, Proposals: p.pending, Since: p.since})
				} else {
					lifted = true
				}
			}
			f.Held = &held
		}
		switch {
		case t.SyncState == SyncOff || t.SyncState == SyncDrifted:
		case len(t.Holds) > 0:
			t.shown = SyncHeld
		case lifted && t.SyncState == SyncInSync && t.Delivery.writesFiles():
			// Every proposal is decided: the latest compile is due over
			// the edited file, even when nothing recompiled meanwhile.
			t.shown = SyncPendingDelivery
		}
	}
	return nil
}

// loadPulledProposals reads the proposals pulled observations wrote, with
// the ones that are still proposed. Observations that weren't pulled (or
// wrote no proposal) are absent.
func loadPulledProposals(ctx context.Context, tx pgx.Tx, obs []uuid.UUID) (map[uuid.UUID]*pulledProposals, error) {
	rows, err := tx.Query(ctx, `
		SELECT o.id, COALESCE(o.resolved_at, o.observed_at), c->>'proposal', COALESCE(m.lifecycle = 'proposed', false)
		  FROM v2.target_observations o
		 CROSS JOIN LATERAL jsonb_array_elements(COALESCE(o.resolution->'changes', '[]'::jsonb)) c
		  LEFT JOIN v2.memories m
		         ON m.space_id = o.space_id
		        AND m.seq = (regexp_match(c->>'proposal', '^[Mm]-0*([0-9]+)$'))[1]::bigint
		 WHERE o.id = ANY ($1) AND o.status = 'pulled' AND c->>'outcome' = 'proposed'
		 ORDER BY o.id, c->>'proposal'`, obs)
	if err != nil {
		return nil, fmt.Errorf("ledger: load holds: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]*pulledProposals{}
	for rows.Next() {
		var id uuid.UUID
		var ref string
		var since time.Time
		var pending bool
		if err := rows.Scan(&id, &since, &ref, &pending); err != nil {
			return nil, fmt.Errorf("ledger: load holds: %w", err)
		}
		p := out[id]
		if p == nil {
			p = &pulledProposals{since: since, pending: []string{}}
			out[id] = p
		}
		if pending {
			p.pending = append(p.pending, ref)
		}
	}
	return out, rows.Err()
}

// heldPath reports whether a pull holds the target's file at path. The
// target must have been through fillHolds.
func (t *Target) heldPath(path string) bool {
	for _, h := range t.Holds {
		if h.Path == path {
			return true
		}
	}
	return false
}

// ShownState is the state the API shows: the stored one, unless a pull
// holds the target, or a lifted hold makes a delivery due.
func (t *Target) ShownState() SyncState {
	if t.shown != "" {
		return t.shown
	}
	return t.SyncState
}

// MarshalJSON shows the target as the API does (ShownState), while the
// ledger keeps working with the stored SyncState.
func (t Target) MarshalJSON() ([]byte, error) {
	type plain Target
	p := plain(t)
	p.SyncState = t.ShownState()
	return json.Marshal(p)
}

// storedFiles are baseline files as the row keeps them: Held is shown,
// never stored.
func storedFiles(files []DeliveredFile) []DeliveredFile {
	out := make([]DeliveredFile, len(files))
	for i, f := range files {
		f.Held = nil
		out[i] = f
	}
	return out
}

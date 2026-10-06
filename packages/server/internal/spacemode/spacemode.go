// Package spacemode decides, per space, whether a surface behaves as V1
// or on the V2 record (plan 25 §10: each space switches to V2 on its own).
//
// The switch is hubs.v2_enabled_at (migration 033). A space without it is
// on V1, and every surface keeps V1's behaviour and response shapes there;
// a space with it is served through internal/ledger. This package is the
// one place that reads the switch, so every surface (MCP today, the /v1
// write paths later) agrees on which spaces moved.
//
// Like ledger.ResolveUserScope, it reads public.hubs as the login role:
// which record a space is on is part of resolving the request, before any
// ledger transaction starts.
package spacemode

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DB is what the resolver needs from a pgx pool.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Resolver reads the switch. A nil *Resolver (no database, as in memory
// mode) puts every space on V1.
type Resolver struct {
	db DB
}

// New returns a Resolver on db, or nil when db is nil.
func New(db DB) *Resolver {
	if db == nil {
		return nil
	}
	return &Resolver{db: db}
}

// V2Spaces returns which of the hubs are on the V2 record. Hub IDs that
// aren't UUIDs, or aren't hubs, are on V1.
func (r *Resolver) V2Spaces(ctx context.Context, hubIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if r == nil || len(hubIDs) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, 0, len(hubIDs))
	for _, h := range hubIDs {
		if id, err := uuid.Parse(h); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx,
		`SELECT id FROM public.hubs WHERE id = ANY($1) AND v2_enabled_at IS NOT NULL`, ids)
	if err != nil {
		return out, fmt.Errorf("spacemode: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return out, fmt.Errorf("spacemode: %w", err)
		}
		out[id.String()] = true
	}
	return out, rows.Err()
}

// IsV2 reports whether one hub is on the V2 record.
func (r *Resolver) IsV2(ctx context.Context, hubID string) (bool, error) {
	m, err := r.V2Spaces(ctx, []string{hubID})
	return m[hubID], err
}

// ErrNoSpace is returned by Enable and Disable for an unknown hub.
var ErrNoSpace = errors.New("spacemode: no such space")

// Enable switches a space to the V2 record at the given time (its first
// switch time is kept). Until the Switch to V2 step (epic 2.8) runs the
// import cleanup, only tests and dev seeding call it.
func (r *Resolver) Enable(ctx context.Context, hubID uuid.UUID, at time.Time) error {
	return r.set(ctx, `UPDATE public.hubs SET v2_enabled_at = COALESCE(v2_enabled_at, $2) WHERE id = $1`, hubID, at)
}

// Disable puts a space back on V1 (dev and tests only: its V2 records stay).
func (r *Resolver) Disable(ctx context.Context, hubID uuid.UUID) error {
	return r.set(ctx, `UPDATE public.hubs SET v2_enabled_at = NULL WHERE id = $1`, hubID)
}

func (r *Resolver) set(ctx context.Context, sql string, args ...any) error {
	if r == nil {
		return errors.New("spacemode: no database")
	}
	tag, err := r.db.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("spacemode: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoSpace
	}
	return nil
}

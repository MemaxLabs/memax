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

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
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

// ConfigSyncOff says which of a person's V1 agent files V1's two-way
// config sync must leave alone: those that belong to a space of theirs on
// V2 (plan 25 §10: "two-way sync is turned off", D10), where Memax compiles
// them instead. The answer takes a file's V1 sync scope ("global",
// "profile:<name>", "project:<url>"). Switching the space back turns sync
// on again: nothing is stored.
func (r *Resolver) ConfigSyncOff(ctx context.Context, userID string) (func(scope string) bool, error) {
	none := func(string) bool { return false }
	id, err := uuid.Parse(userID)
	if r == nil || err != nil {
		return none, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT h.space_kind, COALESCE(h.repository, '')
		  FROM public.hubs h
		 WHERE h.v2_enabled_at IS NOT NULL
		   AND (h.owner_id = $1 OR EXISTS (SELECT 1 FROM public.hub_members m WHERE m.hub_id = h.id AND m.user_id = $1))`, id)
	if err != nil {
		return none, fmt.Errorf("spacemode: %w", err)
	}
	defer rows.Close()
	type sp struct {
		kind policy.SpaceKind
		repo string
	}
	var spaces []sp
	for rows.Next() {
		var s sp
		if err := rows.Scan(&s.kind, &s.repo); err != nil {
			return none, fmt.Errorf("spacemode: %w", err)
		}
		spaces = append(spaces, s)
	}
	if err := rows.Err(); err != nil {
		return none, fmt.Errorf("spacemode: %w", err)
	}
	if len(spaces) == 0 {
		return none, nil
	}
	return func(scope string) bool {
		if scope == "" {
			scope = "global"
		}
		for _, s := range spaces {
			if ledger.ConfigBelongs(scope, s.kind, s.repo) {
				return true
			}
		}
		return false
	}, nil
}

// ErrNoSpace is returned by Enable and Disable for an unknown hub.
var ErrNoSpace = errors.New("spacemode: no such space")

// Enable switches a space to the V2 record at the given time (its first
// switch time is kept), and nothing else: tests and dev seeding. The
// Switch to V2 itself (moving the space's V1 content) is
// ledger.StartSwitch.
func (r *Resolver) Enable(ctx context.Context, hubID uuid.UUID, at time.Time) error {
	return r.set(ctx, `UPDATE public.hubs SET v2_enabled_at = COALESCE(v2_enabled_at, $2) WHERE id = $1`, hubID, at)
}

// Disable puts a space back on V1 (dev and tests only: its V2 records
// stay; ledger.SwitchBack is the receipted way).
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

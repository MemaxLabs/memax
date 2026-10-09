package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SpaceDigest is what the OAuth consent screen (OAuthConsent) says about a
// space on the V2 record beside its name and kind: how many memories are
// kept in it and the files it compiles to.
type SpaceDigest struct {
	// Kept counts the memories kept in it now (not proposals, not faded).
	Kept int
	// Targets are its compile targets that aren't stopped, the canonical
	// file (agents_md) first.
	Targets []DigestTarget
}

// DigestTarget is one compile target: its kind and its repository-relative
// path (empty for ChatGPT, which is copied out).
type DigestTarget struct {
	Kind TargetKind
	Path string
}

// SpaceDigests reads the digest of each of the scope's spaces, in one round
// trip. A space with nothing kept and no targets has a zero digest.
func (l *Ledger) SpaceDigests(ctx context.Context, scope Scope) (map[uuid.UUID]SpaceDigest, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	ids := scope.SpaceIDs()
	out := make(map[uuid.UUID]SpaceDigest, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	b := &pgx.Batch{}
	b.Queue(`
		SELECT space_id, count(*)
		  FROM v2.memories
		 WHERE space_id = ANY ($1) AND lifecycle = 'kept'
		 GROUP BY space_id`, ids).
		Query(func(rows pgx.Rows) error {
			for rows.Next() {
				var id uuid.UUID
				var n int
				if err := rows.Scan(&id, &n); err != nil {
					return fmt.Errorf("ledger: space digests: kept: %w", err)
				}
				d := out[id]
				d.Kept = n
				out[id] = d
			}
			return rows.Err()
		})
	b.Queue(`
		SELECT space_id, kind, COALESCE(path, '')
		  FROM v2.targets
		 WHERE space_id = ANY ($1) AND sync_state <> 'off'
		 ORDER BY space_id, kind <> 'agents_md', kind, path NULLS FIRST`, ids).
		Query(func(rows pgx.Rows) error {
			for rows.Next() {
				var id uuid.UUID
				var t DigestTarget
				if err := rows.Scan(&id, &t.Kind, &t.Path); err != nil {
					return fmt.Errorf("ledger: space digests: targets: %w", err)
				}
				d := out[id]
				d.Targets = append(d.Targets, t)
				out[id] = d
			}
			return rows.Err()
		})
	if err := l.ReadBatch(ctx, scope, b); err != nil {
		return nil, err
	}
	return out, nil
}

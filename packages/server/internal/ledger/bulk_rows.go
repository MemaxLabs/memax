package ledger

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Rows written many at a time, for the commands that apply many items in
// one transaction (bulk.go, imports.go). Each statement here writes every
// row of its kind in one INSERT over unnest'ed arrays, so a command's
// writes take one round trip whatever their number. Arrays of arrays
// (a receipt list, a memory's bands) go as their text literals and are
// cast back row by row: Postgres arrays must be rectangular.

// insertReceiptsSQL writes receipts in array order (seq follows it).
const insertReceiptsSQL = `
	INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action,
	                         actor_kind, actor_id, agent, via, assurance, session_ref, source, reason,
	                         occurred_at, stream_id, stream_version)
	SELECT r.id, r.tenant_id, r.space_id, r.object_kind, r.object_id, r.object_ref, r.action,
	       r.actor_kind, r.actor_id, r.agent, r.via, r.assurance, r.session_ref, r.source::jsonb, r.reason,
	       r.occurred_at, r.stream_id, r.stream_version
	  FROM unnest($1::uuid[], $2::uuid[], $3::uuid[], $4::text[], $5::uuid[], $6::text[], $7::text[],
	              $8::text[], $9::uuid[], $10::text[], $11::text[], $12::text[], $13::text[], $14::text[], $15::text[],
	              $16::timestamptz[], $17::uuid[], $18::int[])
	       WITH ORDINALITY AS r(id, tenant_id, space_id, object_kind, object_id, object_ref, action,
	                            actor_kind, actor_id, agent, via, assurance, session_ref, source, reason,
	                            occurred_at, stream_id, stream_version, ord)
	 ORDER BY r.ord
	RETURNING id, seq, recorded_at`

// queueReceipts queues the INSERT of rcs into b, in their order, and fills
// each one's seq and recorded_at from its answer, as insertReceipt does
// for one.
func queueReceipts(b *pgx.Batch, rcs []*Receipt) error {
	if len(rcs) == 0 {
		return nil
	}
	n := len(rcs)
	var (
		ids, tenants, spaces, objects, streams = make([]uuid.UUID, n), make([]uuid.UUID, n), make([]uuid.UUID, n), make([]uuid.UUID, n), make([]uuid.UUID, n)
		kinds, refs, actions, actorKinds, vias = make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
		actorIDs                               = make([]*uuid.UUID, n)
		agents, assurances, sessions           = make([]*string, n), make([]*string, n), make([]*string, n)
		sources, reasons                       = make([]*string, n), make([]*string, n)
		occurred                               = make([]time.Time, n)
		versions                               = make([]int32, n)
		byID                                   = make(map[uuid.UUID]*Receipt, n)
	)
	for i, rc := range rcs {
		ids[i], tenants[i], spaces[i], objects[i], streams[i] = rc.ID, rc.TenantID, rc.SpaceID, rc.ObjectID, rc.StreamID
		kinds[i], refs[i], actions[i], actorKinds[i], vias[i] = rc.ObjectKind, rc.ObjectRef, string(rc.Action), string(rc.ActorKind), string(rc.Via)
		actorIDs[i] = rc.ActorID
		agents[i], assurances[i], sessions[i] = textOrNil(rc.Agent), textOrNil(string(rc.Assurance)), textOrNil(rc.SessionRef)
		reasons[i] = textOrNil(rc.Reason)
		if rc.Source != nil {
			b, err := json.Marshal(rc.Source)
			if err != nil {
				return err
			}
			s := string(b)
			sources[i] = &s
		}
		occurred[i], versions[i] = rc.OccurredAt, int32(rc.StreamVersion)
		byID[rc.ID] = rc
	}
	b.Queue(insertReceiptsSQL, ids, tenants, spaces, kinds, objects, refs, actions, actorKinds, actorIDs, agents, vias,
		assurances, sessions, sources, reasons, occurred, streams, versions).Query(func(rows pgx.Rows) error {
		defer rows.Close()
		got := 0
		for rows.Next() {
			var id uuid.UUID
			var seq int64
			var at time.Time
			if err := rows.Scan(&id, &seq, &at); err != nil {
				return fmt.Errorf("ledger: write receipts: %w", err)
			}
			if rc := byID[id]; rc != nil {
				rc.Seq, rc.RecordedAt = seq, at
				got++
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("ledger: write receipts: %w", err)
		}
		if got != n {
			return fmt.Errorf("ledger: write receipts: %d of %d written", got, n)
		}
		return nil
	})
	return nil
}

// textOrNil is nullText for an array element.
func textOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// claimsSQL reads the earlier applications of some keys of one actor in a
// space ($4, the keys).
const claimsSQL = `
	SELECT idempotency_key, command, request_hash, outcome, policy, object_id, receipt_ids
	  FROM v2.command_keys
	 WHERE space_id = $1 AND actor_kind = $2 AND actor_id IS NOT DISTINCT FROM $3 AND idempotency_key = ANY ($4)`

// storedClaim is a key's earlier application, as claimsSQL reads it.
type storedClaim struct {
	command string
	hash    []byte
	outcome *string
	claimed
}

// scanClaims reads claimsSQL's answer by key.
func scanClaims(rows pgx.Rows) (map[string]*storedClaim, error) {
	defer rows.Close()
	out := map[string]*storedClaim{}
	for rows.Next() {
		var key string
		c := &storedClaim{}
		if err := rows.Scan(&key, &c.command, &c.hash, &c.outcome, &c.policy, &c.objectID, &c.receiptIDs); err != nil {
			return nil, fmt.Errorf("ledger: read idempotency keys: %w", err)
		}
		out[key] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: read idempotency keys: %w", err)
	}
	return out, nil
}

// matchClaim is what an earlier application of key means for a command
// named command with request hash hash: the original result to replay, or
// ErrIdempotencyKeyReused (earlierClaim's rules).
func matchClaim(key string, command CommandName, hash []byte, stored *storedClaim) (*claimed, error) {
	// Forget purges the stored hash of a command that carried the words it
	// forgot (it could confirm a guess of them); such a key replays its
	// original result for the same command.
	if stored.command != string(command) || (stored.hash != nil && !bytes.Equal(stored.hash, hash)) {
		return nil, fmt.Errorf("%w: key %q was used for another %s; send a new key for a new command",
			ErrIdempotencyKeyReused, key, stored.command)
	}
	if stored.outcome == nil {
		return nil, errors.New("ledger: idempotency record without an outcome")
	}
	c := stored.claimed
	c.outcome = Outcome(*stored.outcome)
	return &c, nil
}

// claimRow is an idempotency record written with its outcome, by a command
// that holds every lock its key's other applications would take first (the
// memory, or the import), so nobody can be claiming the key meanwhile.
type claimRow struct {
	key      string
	hash     []byte
	outcome  Outcome
	policy   []byte
	object   *uuid.UUID
	receipts []uuid.UUID
}

// insertClaimsSQL writes applied keys with their results.
const insertClaimsSQL = `
	INSERT INTO v2.command_keys (space_id, actor_kind, actor_id, idempotency_key, command, request_hash,
	                             outcome, policy, object_id, receipt_ids)
	SELECT $1, $2, $3, k.key, $4, k.hash, k.outcome, k.policy::jsonb, k.object_id, k.receipt_ids::uuid[]
	  FROM unnest($5::text[], $6::bytea[], $7::text[], $8::text[], $9::uuid[], $10::text[])
	       AS k(key, hash, outcome, policy, object_id, receipt_ids)
	ON CONFLICT ON CONSTRAINT command_keys_key DO NOTHING
	RETURNING idempotency_key`

// queueClaims queues the records of rows into b. A key someone else
// recorded first fails the command with ErrBusy: a retry replays theirs.
func (w *writer) queueClaims(b *pgx.Batch, spaceID uuid.UUID, command CommandName, rows []claimRow) {
	if len(rows) == 0 {
		return
	}
	n := len(rows)
	keys, outcomes, policies, receipts := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	hashes, objects := make([][]byte, n), make([]*uuid.UUID, n)
	for i, r := range rows {
		keys[i], hashes[i], outcomes[i], policies[i], objects[i] = r.key, r.hash, string(r.outcome), string(r.policy), r.object
		receipts[i] = uuidArray(r.receipts)
	}
	b.Queue(insertClaimsSQL, spaceID, string(w.meta.Actor.Kind), w.actorID(), string(command),
		keys, hashes, outcomes, policies, objects, receipts).Query(func(rs pgx.Rows) error {
		defer rs.Close()
		got := 0
		for rs.Next() {
			got++
		}
		if err := rs.Err(); err != nil {
			return fmt.Errorf("ledger: record idempotency keys: %w", err)
		}
		if got != n {
			return fmt.Errorf("%w: another request with the same Idempotency-Key is running; try again in a moment", ErrBusy)
		}
		return nil
	})
}

// policyJSON is a decision as the idempotency record stores it (record).
func policyJSON(d any) ([]byte, error) { return json.Marshal(d) }

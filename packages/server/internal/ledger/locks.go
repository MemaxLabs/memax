package ledger

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// # The lock order
//
// Every transaction that writes the V2 record takes its row locks in one
// order, so no two can wait on each other in a cycle (which Postgres
// breaks by aborting one of them with 40P01, deadlock_detected):
//
//  1. The command's own subject, when it isn't a memory: an undo entry, one
//     of Dream's actions, a gate, an import or one of its disagreements, a
//     note, an agent connection, a machine (a space's Forget takes its
//     machines here, before the Brief, as LinkMachine does). Rows that hang
//     off a memory (the gates it answered, its verdicts, links and sources)
//     come after it.
//  2. Memories, in one statement, in id order: every memory the command
//     will write or must hold still. A command that finds some of them
//     through links (what a Keep supersedes, the other side of a conflict,
//     what a Forget carries) reads them in that statement (lockSet), or
//     reads them without locks first; either way it reads them again under
//     the locks, and if the set grew meanwhile it starts again
//     (errLocksMoved: Apply runs it again). Every later lock of a memory is
//     of one it holds already (writer.lockMemories refuses any other).
//  3. The display-ID counters a command allocates from (v2.id_counters),
//     as it writes the new rows: M- and B- after the memories it locks; a
//     note's N- (a note's Forget, Dream's numbering) and an edition's D-
//     before them, since no command takes either after a memory.
//  4. The space's Brief.
//  5. The space's targets, in one statement, in id order (dirtySQL locks
//     them through an ordered subselect; a Forget locks every target of the
//     space at once, before it rewrites any).
//  6. River's tables (InsertManyTx and the finishers), right before COMMIT.
//
// A command whose subject is a target (Compile now, a compile's record, a
// delivery, a hand edit and its resolution, a target's settings) holds
// that target from its first statement and locks no memory, Brief or other
// target after it. What it needs that comes earlier in the order it takes
// before the target, in the same round trip: a pull, which writes
// proposals, the M- counter; turning a home file back on, its machine.
//
// # Lock strength
//
// FOR NO KEY UPDATE is the strongest lock any command needs on a memory,
// the Brief or a target: none changes a key or deletes the row in a
// command, except a space's retirement (v2.retire_space, at the end of a
// space's Forget), which holds what it deletes FOR UPDATE. Unlike FOR
// UPDATE, NO KEY UPDATE lets a foreign key's check (FOR KEY SHARE) through,
// so a row that names a memory, the Brief or a target never waits on a
// command holding it: a compile run naming its Brief (the cycle CI logged
// on Oct 11, 2026: the compile job held its target and waited on the
// Brief's key for its run, while a Forget held the Brief and waited on the
// target), a pulled hand edit's proposal naming the memory it edits, a
// verdict naming a candidate. FOR SHARE (an index job holding the memories
// it embeds, still) conflicts with it, as it should.
//
// # The backstop
//
// A command is one transaction, with nothing outside it (no network call,
// cache purge or recorder write happens before COMMIT), so it can run again
// from the start. Apply, the bookkeeping writes and the index's store run a
// transaction again, up to three times with a few milliseconds' jitter,
// when Postgres aborts it for a deadlock or a serialization failure, or
// when the memories it must lock moved (retry.go). The order above means
// that should not happen; memax.ledger.retries counts it when it does.

// Lock strengths.
const (
	// lockWrite is how a command locks the memories, the Brief and the
	// targets it changes.
	lockWrite = "FOR NO KEY UPDATE"
	// lockDelete is how a space's retirement locks what it deletes.
	lockDelete = "FOR UPDATE"
)

// errLocksMoved: what a command must lock changed between its read and its
// lock (a carry, a conflict or a supersede written meanwhile). Apply runs
// the command again; if it keeps moving, the caller sees ErrBusy.
var errLocksMoved = fmt.Errorf("%w: what this command changes moved while it locked it", ErrBusy)

// memoryLocks are the memories a command holds: the one round it locked
// (writer.lockMemories, lockSet), found or not.
type memoryLocks struct {
	done bool
	held map[uuid.UUID]bool
	mode string
}

func (k *memoryLocks) hold(ids ...uuid.UUID) {
	if k.held == nil {
		k.held = map[uuid.UUID]bool{}
	}
	k.done = true
	for _, id := range ids {
		k.held[id] = true
	}
}

// holds reports whether every id is held.
func (k *memoryLocks) holds(ids ...uuid.UUID) bool {
	for _, id := range ids {
		if !k.held[id] {
			return false
		}
	}
	return true
}

// strength is the round's lock (lockWrite unless set).
func (k *memoryLocks) strength() string {
	if k.mode == "" {
		return lockWrite
	}
	return k.mode
}

// lockMemories locks memories by id, in id order: the command's one round
// of memory locks when it has none yet, else memories it holds already
// (which wait for nothing). A memory it doesn't hold is errLocksMoved: a
// second round could close a cycle with a command that locked in the other
// order.
func (w *writer) lockMemories(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*Memory, error) {
	k := w.memoryLocks()
	if k.done && !k.holds(ids...) {
		return nil, errLocksMoved
	}
	locked, err := lockMemories(ctx, w.tx, w.meta.Scope, ids, k.strength())
	if err != nil {
		return nil, err
	}
	k.hold(ids...)
	return locked, nil
}

// memoryLocks is what the command holds (shared with the writers a command
// derives from its own, such as a pull's author).
func (w *writer) memoryLocks() *memoryLocks {
	if w.locks == nil {
		w.locks = &memoryLocks{}
	}
	return w.locks
}

// lockMemories loads memories by id with a row lock (a strength above), in
// id order. Use writer.lockMemories in a command.
func lockMemories(ctx context.Context, tx pgx.Tx, scope Scope, ids []uuid.UUID, lock string) (map[uuid.UUID]*Memory, error) {
	sorted := sortedIDs(ids)
	rows, err := tx.Query(ctx, memorySelect+` WHERE m.id = ANY ($1) AND m.space_id = ANY ($2) ORDER BY m.id `+lock+` OF m`,
		sorted, scope.SpaceIDs())
	if err != nil {
		return nil, fmt.Errorf("ledger: lock memories: %w", err)
	}
	ms, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Memory, error) { return scanMemory(r) })
	if err != nil {
		return nil, fmt.Errorf("ledger: lock memories: %w", err)
	}
	out := make(map[uuid.UUID]*Memory, len(ms))
	for _, m := range ms {
		out[m.ID] = m
	}
	return out, nil
}

// sortedIDs is ids in Postgres's uuid order (bytewise, which is the order
// of their lowercase text), without repeats.
func sortedIDs(ids []uuid.UUID) []uuid.UUID {
	sorted := slices.Clone(ids)
	slices.SortFunc(sorted, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return slices.Compact(sorted)
}

// lockAlso says which memories a command locks beside the one it names,
// in the same statement (lockSet).
type lockAlso struct {
	// ids are these memories too (a verdict's targets).
	ids []uuid.UUID
	// supersedes: the memories it supersedes (active supersedes links out of
	// it), which keeping it supersedes in turn.
	supersedes bool
	// conflicts: the memories it is in conflict with (active conflicts_with
	// links, either way).
	conflicts bool
}

// lockSetSQL locks the named memories ($1: display-ID numbers, or ids) and
// what also adds, in id order, as one statement: the links are read in
// its snapshot, and the caller reads them again under the locks. Each row
// carries the named memory a display ID resolved to, by a scalar subquery
// that raises cardinality_violation (ErrAmbiguousRef) when the ID names
// memories in more than one space of the scope, and locks nothing.
func lockSetSQL(bySeq bool, also lockAlso, lock string) string {
	named := `SELECT unnest($1::uuid[]) AS id`
	if bySeq {
		named = `SELECT (SELECT id FROM v2.memories WHERE seq = $1 AND space_id = ANY ($2)) AS id`
	}
	want := `SELECT id FROM named UNION SELECT unnest($3::uuid[])`
	if also.supersedes {
		want += `
		UNION SELECT l.to_memory_id FROM v2.memory_links l JOIN named n ON l.from_memory_id = n.id
		       WHERE l.kind = 'supersedes' AND l.ended_receipt_id IS NULL`
	}
	if also.conflicts {
		want += `
		UNION SELECT l.to_memory_id FROM v2.memory_links l JOIN named n ON l.from_memory_id = n.id
		       WHERE l.kind = 'conflicts_with' AND l.ended_receipt_id IS NULL
		UNION SELECT l.from_memory_id FROM v2.memory_links l JOIN named n ON l.to_memory_id = n.id
		       WHERE l.kind = 'conflicts_with' AND l.ended_receipt_id IS NULL`
	}
	return `WITH named AS (` + named + `), want AS (` + want + `)` + lockSetSelect + `
		 WHERE m.id IN (SELECT id FROM want WHERE id IS NOT NULL) AND m.space_id = ANY ($2)
		 ORDER BY m.id ` + lock + ` OF m`
}

// lockSetSelect is memorySelect's columns and one more, the named memory's
// id (withExtra scans it).
var lockSetSelect = func() string {
	from := "\n\t  FROM v2.memories m"
	if !strings.Contains(memorySelect, from) {
		panic("ledger: memorySelect changed shape; update lockSetSelect")
	}
	return strings.Replace(memorySelect, from, ", (SELECT id FROM named LIMIT 1)"+from, 1)
}()

// lockSet is the command's one round of memory locks: the memories ids
// (or the one ref names) and what also adds. It returns them by id, and
// the memory ref named (nil when it names none in scope).
func (w *writer) lockSet(ctx context.Context, ref string, ids []uuid.UUID, also lockAlso) (*Memory, map[uuid.UUID]*Memory, error) {
	k := w.memoryLocks()
	if k.done {
		// A second round is a bug in the command: it must lock once.
		return nil, nil, fmt.Errorf("ledger: %s locked memories twice", w.command)
	}
	lock := k.strength()
	var rows pgx.Rows
	var err error
	var seq int64
	bySeq := false
	if ref != "" {
		ref = strings.TrimSpace(ref)
		if id, perr := uuid.Parse(ref); perr == nil {
			ids = []uuid.UUID{id}
		} else {
			if seq, err = memorySeq(ref); err != nil {
				return nil, nil, err
			}
			bySeq = true
		}
	}
	extra := nonNilSlice(also.ids)
	if bySeq {
		rows, err = w.tx.Query(ctx, lockSetSQL(true, also, lock), seq, w.meta.Scope.SpaceIDs(), extra)
	} else {
		rows, err = w.tx.Query(ctx, lockSetSQL(false, also, lock), sortedIDs(ids), w.meta.Scope.SpaceIDs(), extra)
	}
	if err != nil {
		return nil, nil, lockSetError(err)
	}
	locked := map[uuid.UUID]*Memory{}
	var named *uuid.UUID
	for rows.Next() {
		m, err := scanMemory(withExtra{rows, &named})
		if err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("ledger: lock memories: %w", err)
		}
		locked[m.ID] = m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, lockSetError(err)
	}
	k.hold(ids...)
	k.hold(also.ids...)
	for id := range locked {
		k.hold(id)
	}
	if ref == "" || named == nil {
		return nil, locked, nil
	}
	return locked[*named], locked, nil
}

// lockSetError is lockSet's error: an ambiguous display ID, or the
// statement's failure.
func lockSetError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == sqlstateCardinality {
		return ErrAmbiguousRef
	}
	return fmt.Errorf("ledger: lock memories: %w", err)
}

// withExtra scans a row of memorySelect's columns and one more.
type withExtra struct {
	pgx.Row
	extra any
}

func (r withExtra) Scan(dest ...any) error { return r.Row.Scan(append(dest, r.extra)...) }

// The targets of a space, locked at once (a Forget's one round of target
// locks, before it rewrites any): every target, off ones included, in id
// order.
func lockTargetsSQL(lock string) string {
	return `SELECT id FROM v2.targets WHERE space_id = $1 ORDER BY id ` + lock
}

// lockCounterSQL locks the tenant's counter of a prefix ($2) through the
// target $1, before the target itself (a pull's M- counter, see above).
// Nothing reads its answer: it rides ahead of the target's lock.
const lockCounterSQL = `
	SELECT 1 FROM v2.id_counters c
	 WHERE c.prefix = $2 AND c.tenant_id = (SELECT t.tenant_id FROM v2.targets t WHERE t.id = $1)
	   FOR UPDATE OF c`

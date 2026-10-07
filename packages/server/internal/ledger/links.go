package ledger

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
)

// LinkKind is a typed edge between two memories (plan 25 §5.4).
type LinkKind string

// The link kinds a memory-to-memory link can have. folded_from (to a
// note) is Dream's and is not loaded here.
const (
	LinkMergedInto    LinkKind = "merged_into"    // a proposal folded into another memory
	LinkSupersedes    LinkKind = "supersedes"     // a memory that replaces (or would replace) another
	LinkConflictsWith LinkKind = "conflicts_with" // a memory that contradicts a decision in force
	LinkCloses        LinkKind = "closes"
)

// LinkKinds lists the memory-to-memory kinds.
var LinkKinds = []LinkKind{LinkMergedInto, LinkSupersedes, LinkConflictsWith, LinkCloses}

// The directions of a link, seen from one memory.
const (
	LinkOut = "out" // from this memory to the other
	LinkIn  = "in"  // from the other memory to this one
)

// Link is an active link seen from one memory.
type Link struct {
	ID        uuid.UUID `json:"id"`
	Kind      LinkKind  `json:"kind"`
	Direction string    `json:"direction"`
	MemoryID  uuid.UUID `json:"memory_id"`
	Ref       string    `json:"ref"`
	ReceiptID uuid.UUID `json:"receipt_id"`
	CreatedAt time.Time `json:"created_at"`

	from, to uuid.UUID
}

// LinkedMemory is the other side of a link, with its current words.
type LinkedMemory struct {
	ID        uuid.UUID           `json:"id"`
	Ref       string              `json:"ref"`
	Version   int                 `json:"version"`
	Statement string              `json:"statement"`
	Lifecycle lifecycle.Lifecycle `json:"lifecycle"`
}

// insertLink writes an active link with the receipt that made it.
func (w *writer) insertLink(ctx context.Context, spaceID uuid.UUID, kind LinkKind, from, to, receiptID uuid.UUID) (uuid.UUID, error) {
	id := newID()
	if _, err := w.tx.Exec(ctx, `
		INSERT INTO v2.memory_links (id, space_id, kind, from_memory_id, to_memory_id, receipt_id)
		VALUES ($1, $2, $3, $4, $5, $6)`, id, spaceID, string(kind), from, to, receiptID); err != nil {
		return uuid.Nil, fmt.Errorf("ledger: link %s: %w", kind, err)
	}
	if w.undo != nil {
		w.undo.LinksCreated = append(w.undo.LinksCreated, id)
	}
	return id, nil
}

// endLink ends an active link with the receipt that ended it.
func (w *writer) endLink(ctx context.Context, l Link, receiptID uuid.UUID) error {
	tag, err := w.tx.Exec(ctx, `
		UPDATE v2.memory_links SET ended_receipt_id = $2, ended_at = now()
		 WHERE id = $1 AND ended_receipt_id IS NULL`, l.ID, receiptID)
	if err != nil {
		return fmt.Errorf("ledger: end link: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: the link changed meanwhile; reload and try again", ErrEditClash)
	}
	if w.undo != nil {
		w.undo.LinksEnded = append(w.undo.LinksEnded, undoLink{ID: l.ID, Kind: l.Kind, From: l.from, To: l.to})
	}
	return nil
}

// linksSQL reads the active memory-to-memory links of the memories $1,
// both ways, oldest first.
const linksSQL = `
	SELECT l.id, l.kind, l.from_memory_id, l.to_memory_id, l.receipt_id, l.created_at, f.seq, t.seq
	  FROM v2.memory_links l
	  JOIN v2.memories f ON f.id = l.from_memory_id
	  JOIN v2.memories t ON t.id = l.to_memory_id
	 WHERE l.ended_receipt_id IS NULL AND l.to_memory_id IS NOT NULL
	   AND (l.from_memory_id = ANY ($1) OR l.to_memory_id = ANY ($1))
	 ORDER BY l.created_at, l.id`

// activeLinks loads the active memory-to-memory links of the given
// memories, both ways, oldest first.
func activeLinks(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID][]Link, error) {
	out := map[uuid.UUID][]Link{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, linksSQL, ids)
	if err != nil {
		return nil, fmt.Errorf("ledger: load links: %w", err)
	}
	return out, scanLinks(rows, ids, out)
}

// scanLinks reads linksSQL's rows into out.
func scanLinks(rows pgx.Rows, ids []uuid.UUID, out map[uuid.UUID][]Link) error {
	defer rows.Close()
	for rows.Next() {
		var l Link
		var fromSeq, toSeq int64
		if err := rows.Scan(&l.ID, &l.Kind, &l.from, &l.to, &l.ReceiptID, &l.CreatedAt, &fromSeq, &toSeq); err != nil {
			return fmt.Errorf("ledger: load links: %w", err)
		}
		if slices.Contains(ids, l.from) {
			out[l.from] = append(out[l.from], Link{ID: l.ID, Kind: l.Kind, Direction: LinkOut, MemoryID: l.to,
				Ref: FormatRef(PrefixMemory, toSeq), ReceiptID: l.ReceiptID, CreatedAt: l.CreatedAt, from: l.from, to: l.to})
		}
		if slices.Contains(ids, l.to) {
			out[l.to] = append(out[l.to], Link{ID: l.ID, Kind: l.Kind, Direction: LinkIn, MemoryID: l.from,
				Ref: FormatRef(PrefixMemory, fromSeq), ReceiptID: l.ReceiptID, CreatedAt: l.CreatedAt, from: l.from, to: l.to})
		}
	}
	return rows.Err()
}

// attachDetails fills each memory's links, the memory it updates and the
// judge's verdict, for the whole batch. The links, the verdicts and what
// extra queues (a memory's sources, say) go to the server together, in one
// round trip; the memories a decision updates, when there are any, in a
// second.
func attachDetails(ctx context.Context, tx pgx.Tx, ms []*Memory, extra ...func(*pgx.Batch)) error {
	if len(ms) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	b := &pgx.Batch{}
	d := queueDetails(b, ids)
	for _, queue := range extra {
		queue(b)
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return err
	}
	return d.apply(ctx, tx, ms)
}

// loadFull reads one memory in scope as GetMemory shows it: its row, its
// sources, links and verdict in one round trip, and the memory it
// updates, when there is one, in a second. extra queues more into the
// first.
func loadFull(ctx context.Context, tx pgx.Tx, scope Scope, id uuid.UUID, extra ...func(*pgx.Batch)) (*Memory, error) {
	var m *Memory
	b := &pgx.Batch{}
	b.Queue(memorySelect+` WHERE m.id = $1 AND m.space_id = ANY($2)`, id, scope.SpaceIDs()).
		Query(func(rows pgx.Rows) error {
			ms, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Memory, error) { return scanMemory(r) })
			if err != nil {
				return fmt.Errorf("ledger: load memory: %w", err)
			}
			if len(ms) == 1 {
				m = ms[0]
			}
			return nil
		})
	var sources []Source
	b.Queue(sourcesSQL, id).Query(func(rows pgx.Rows) error {
		var err error
		if sources, err = scanSources(rows); err != nil {
			return fmt.Errorf("ledger: load sources: %w", err)
		}
		return nil
	})
	d := queueDetails(b, []uuid.UUID{id})
	for _, queue := range extra {
		queue(b)
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrNotFound
	}
	m.Sources = sources
	if err := d.apply(ctx, tx, []*Memory{m}); err != nil {
		return nil, err
	}
	return m, nil
}

// details are memories' links and verdicts, read in a batch the caller
// sends (queueDetails), then applied to the memories.
type details struct {
	links    map[uuid.UUID][]Link
	verdicts map[uuid.UUID]*JudgeInfo
}

// queueDetails queues the read of the links and verdicts of ids into b.
func queueDetails(b *pgx.Batch, ids []uuid.UUID) *details {
	d := &details{links: map[uuid.UUID][]Link{}}
	b.Queue(linksSQL, ids).Query(func(rows pgx.Rows) error { return scanLinks(rows, ids, d.links) })
	b.Queue(verdictsSQL, ids).Query(func(rows pgx.Rows) error {
		var err error
		d.verdicts, err = scanVerdicts(rows)
		return err
	})
	return d
}

// apply fills each memory's links, the memory it updates (read now, when
// there is one) and the judge's verdict.
func (d *details) apply(ctx context.Context, tx pgx.Tx, ms []*Memory) error {
	links, verdicts := d.links, d.verdicts
	var updates []uuid.UUID
	for _, m := range ms {
		m.Links = links[m.ID]
		for _, l := range m.Links {
			if l.Kind == LinkSupersedes && l.Direction == LinkOut {
				updates = append(updates, l.MemoryID)
				break
			}
		}
	}
	if len(updates) > 0 {
		rows, err := tx.Query(ctx, memorySelect+` WHERE m.id = ANY ($1)`, updates)
		if err != nil {
			return fmt.Errorf("ledger: load updated memories: %w", err)
		}
		targets, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Memory, error) { return scanMemory(r) })
		if err != nil {
			return fmt.Errorf("ledger: load updated memories: %w", err)
		}
		for _, m := range ms {
			for _, l := range m.Links {
				if l.Kind != LinkSupersedes || l.Direction != LinkOut {
					continue
				}
				for _, t := range targets {
					if t.ID == l.MemoryID {
						m.Updates = &LinkedMemory{ID: t.ID, Ref: t.Ref, Version: t.Version, Statement: t.Statement, Lifecycle: t.Lifecycle}
					}
				}
				break
			}
		}
	}
	applyVerdicts(ms, verdicts)
	return nil
}

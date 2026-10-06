package ledger

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
)

// Page sizes for the list functions.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// UserScope resolves a user's scope with the ledger's pool; see
// ResolveUserScope.
func (l *Ledger) UserScope(ctx context.Context, userID uuid.UUID) (Scope, error) {
	if l == nil {
		return Scope{}, ErrDisabled
	}
	return ResolveUserScope(ctx, l.pool, userID)
}

// GetMemory returns one memory with its sources, by display ID
// ("M-0219") or uuid. A display ID that exists in more than one tenant
// of the scope is ErrAmbiguousRef; narrow the scope to one space.
func (l *Ledger) GetMemory(ctx context.Context, scope Scope, ref string) (*Memory, error) {
	var out *Memory
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		id, err := resolveRef(ctx, tx, scope, ref)
		if err != nil {
			return err
		}
		m, err := loadMemory(ctx, tx, scope, id, false)
		if err != nil {
			return err
		}
		if m.Sources, err = loadSources(ctx, tx, m.ID); err != nil {
			return err
		}
		if err := attachDetails(ctx, tx, []*Memory{m}); err != nil {
			return err
		}
		out = m
		return nil
	})
	return out, err
}

// MemoryQuery filters ListMemories.
type MemoryQuery struct {
	SpaceID uuid.UUID
	// States filters by displayed state. Empty means every state except
	// rejected, which is internal.
	States []lifecycle.Mark
	// Sections filters by section; empty means all.
	Sections []Section
	Cursor   string
	// Limit defaults to DefaultPageSize and is capped at MaxPageSize.
	Limit int
}

// MemoryPage is one page of memories, newest first. Sources are not
// loaded; use GetMemory for one memory's sources.
type MemoryPage struct {
	Memories   []Memory `json:"memories"`
	NextCursor string   `json:"next_cursor,omitempty"`
	HasMore    bool     `json:"has_more"`
}

// ListMemories pages through one space's memories, newest first.
func (l *Ledger) ListMemories(ctx context.Context, scope Scope, q MemoryQuery) (MemoryPage, error) {
	if _, ok := scope.Grant(q.SpaceID); !ok {
		return MemoryPage{}, ErrNotFound
	}
	limit, err := pageLimit(q.Limit)
	if err != nil {
		return MemoryPage{}, err
	}
	after, err := decodeCursor(q.Cursor, 'm')
	if err != nil {
		return MemoryPage{}, err
	}
	states := make([]string, 0, len(lifecycle.Marks))
	for _, m := range lifecycle.Marks {
		if (len(q.States) == 0 && m != lifecycle.MarkRejected) || slices.Contains(q.States, m) {
			states = append(states, string(m))
		}
	}
	for _, m := range q.States {
		if !m.Valid() {
			return MemoryPage{}, invalid("state", "unknown state %q", m)
		}
	}
	sections := make([]string, 0, len(Sections))
	for _, s := range Sections {
		if len(q.Sections) == 0 || slices.Contains(q.Sections, s) {
			sections = append(sections, string(s))
		}
	}
	for _, s := range q.Sections {
		if !s.Valid() {
			return MemoryPage{}, invalid("section", "unknown section %q", s)
		}
	}

	var page MemoryPage
	err = l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, memorySelect+`
			 WHERE m.space_id = $1 AND m.space_id = ANY($2)
			   AND m.state = ANY($3) AND m.section = ANY($4)
			   AND ($5::bigint = 0 OR m.seq < $5)
			 ORDER BY m.seq DESC
			 LIMIT $6`,
			q.SpaceID, scope.SpaceIDs(), states, sections, after, limit+1)
		if err != nil {
			return fmt.Errorf("ledger: list memories: %w", err)
		}
		ms, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Memory, error) {
			m, err := scanMemory(r)
			if err != nil {
				return Memory{}, err
			}
			return *m, nil
		})
		if err != nil {
			return fmt.Errorf("ledger: list memories: %w", err)
		}
		page.Memories = ms
		return attachDetails(ctx, tx, pointers(page.Memories))
	})
	if err != nil {
		return MemoryPage{}, err
	}
	if len(page.Memories) > limit {
		page.Memories = page.Memories[:limit]
		page.HasMore = true
		page.NextCursor = encodeCursor('m', page.Memories[limit-1].seq)
	}
	return page, nil
}

// ReceiptQuery filters ListReceipts.
type ReceiptQuery struct {
	SpaceID uuid.UUID
	// ObjectID narrows to one object's history (a memory's receipts).
	ObjectID uuid.UUID
	Cursor   string
	Limit    int
}

// ReceiptPage is one page of receipts, newest first.
type ReceiptPage struct {
	Receipts   []Receipt `json:"receipts"`
	NextCursor string    `json:"next_cursor,omitempty"`
	HasMore    bool      `json:"has_more"`
}

// ListReceipts pages through one space's receipts (the Activity feed),
// newest first by receipt sequence. Sequence numbers are assigned at
// insert, so a long transaction can commit a receipt slightly below one
// already listed; the SSE replay cursor uses (txid, seq) for that
// reason, and this listing is for people reading Activity.
func (l *Ledger) ListReceipts(ctx context.Context, scope Scope, q ReceiptQuery) (ReceiptPage, error) {
	if _, ok := scope.Grant(q.SpaceID); !ok {
		return ReceiptPage{}, ErrNotFound
	}
	limit, err := pageLimit(q.Limit)
	if err != nil {
		return ReceiptPage{}, err
	}
	after, err := decodeCursor(q.Cursor, 'r')
	if err != nil {
		return ReceiptPage{}, err
	}
	var object *uuid.UUID
	if q.ObjectID != uuid.Nil {
		object = &q.ObjectID
	}
	var page ReceiptPage
	err = l.Read(ctx, scope, func(tx pgx.Tx) error {
		page, err = receiptPage(ctx, tx, scope, q.SpaceID, object, after, limit)
		return err
	})
	if err != nil {
		return ReceiptPage{}, err
	}
	return page, nil
}

// receiptPage reads one page of a space's receipts, newest first,
// optionally for one object.
func receiptPage(ctx context.Context, tx pgx.Tx, scope Scope, spaceID uuid.UUID, object *uuid.UUID, after int64, limit int) (ReceiptPage, error) {
	rows, err := tx.Query(ctx, receiptSelect+`
		 WHERE space_id = $1 AND space_id = ANY($2)
		   AND ($3::uuid IS NULL OR object_id = $3)
		   AND ($4::bigint = 0 OR seq < $4)
		 ORDER BY seq DESC
		 LIMIT $5`,
		spaceID, scope.SpaceIDs(), object, after, limit+1)
	if err != nil {
		return ReceiptPage{}, fmt.Errorf("ledger: list receipts: %w", err)
	}
	receipts, err := pgx.CollectRows(rows, scanReceipt)
	if err != nil {
		return ReceiptPage{}, fmt.Errorf("ledger: list receipts: %w", err)
	}
	page := ReceiptPage{Receipts: receipts}
	if len(page.Receipts) > limit {
		page.Receipts = page.Receipts[:limit]
		page.HasMore = true
		page.NextCursor = encodeCursor('r', page.Receipts[limit-1].Seq)
	}
	return page, nil
}

// ListSpaces returns the scope's spaces with the scope's role in each:
// the personal space first, then by name.
func (l *Ledger) ListSpaces(ctx context.Context, scope Scope) ([]Space, error) {
	var out []Space
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, tenant_id, slug, name, kind, COALESCE(repository, ''), v2_enabled_at
			  FROM v2.spaces
			 WHERE id = ANY($1)
			 ORDER BY kind = 'personal' DESC, lower(name), id`, scope.SpaceIDs())
		if err != nil {
			return fmt.Errorf("ledger: list spaces: %w", err)
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Space, error) {
			var sp Space
			err := r.Scan(&sp.ID, &sp.TenantID, &sp.Slug, &sp.Name, &sp.Kind, &sp.Repository, &sp.V2EnabledAt)
			return sp, err
		})
		if err != nil {
			return fmt.Errorf("ledger: list spaces: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		g, _ := scope.Grant(out[i].ID)
		out[i].Role = g.Role
	}
	return out, nil
}

// MemoryHistory is one memory with every version of its statement and its
// latest receipts.
type MemoryHistory struct {
	// Memory carries its sources.
	Memory *Memory
	// Versions are newest first.
	Versions []MemoryVersion
	// Receipts are the newest DefaultPageSize; NextCursor continues on
	// ListReceipts with ObjectID set to the memory.
	Receipts ReceiptPage
}

// GetMemoryHistory reads one memory, by display ID or uuid, with its
// sources, versions and latest receipts, in one transaction.
func (l *Ledger) GetMemoryHistory(ctx context.Context, scope Scope, ref string) (*MemoryHistory, error) {
	var out *MemoryHistory
	err := l.Read(ctx, scope, func(tx pgx.Tx) error {
		id, err := resolveRef(ctx, tx, scope, ref)
		if err != nil {
			return err
		}
		m, err := loadMemory(ctx, tx, scope, id, false)
		if err != nil {
			return err
		}
		if m.Sources, err = loadSources(ctx, tx, m.ID); err != nil {
			return err
		}
		if err := attachDetails(ctx, tx, []*Memory{m}); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT version, COALESCE(statement, ''), receipt_id, created_at
			  FROM v2.memory_versions
			 WHERE memory_id = $1 AND space_id = $2
			 ORDER BY version DESC`, m.ID, m.SpaceID)
		if err != nil {
			return fmt.Errorf("ledger: list versions: %w", err)
		}
		versions, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (MemoryVersion, error) {
			var v MemoryVersion
			err := r.Scan(&v.Version, &v.Statement, &v.ReceiptID, &v.CreatedAt)
			return v, err
		})
		if err != nil {
			return fmt.Errorf("ledger: list versions: %w", err)
		}
		receipts, err := receiptPage(ctx, tx, scope, m.SpaceID, &m.ID, 0, DefaultPageSize)
		if err != nil {
			return err
		}
		out = &MemoryHistory{Memory: m, Versions: versions, Receipts: receipts}
		return nil
	})
	return out, err
}

// ReviewQuery pages through ReviewQueue.
type ReviewQuery struct {
	SpaceID uuid.UUID
	Cursor  string
	Limit   int
}

// ReviewPage is one page of the Review queue.
type ReviewPage struct {
	Memories   []Memory
	NextCursor string
	HasMore    bool
	// Total is the length of the whole queue.
	Total int
}

// reviewGroup orders the queue: proposals (0), then kept memories in
// conflict (1), then kept memories gone stale (2).
const reviewGroup = `(CASE WHEN m.lifecycle = 'proposed' THEN 0 WHEN 'conflict' = ANY (m.flags) THEN 1 ELSE 2 END)`

// reviewWaiting is what needs a person: every proposal, and every kept
// memory with a flag.
const reviewWaiting = `(m.lifecycle = 'proposed' OR (m.lifecycle = 'kept' AND cardinality(m.flags) > 0))`

// ReviewQueue pages through what needs a person in one space: every
// proposal, then kept memories flagged as a conflict, then kept memories
// flagged as stale. Within each group the oldest comes first, so the queue
// drains in the order things arrived.
func (l *Ledger) ReviewQueue(ctx context.Context, scope Scope, q ReviewQuery) (ReviewPage, error) {
	if _, ok := scope.Grant(q.SpaceID); !ok {
		return ReviewPage{}, ErrNotFound
	}
	limit, err := pageLimit(q.Limit)
	if err != nil {
		return ReviewPage{}, err
	}
	afterGroup, afterSeq, err := decodeReviewCursor(q.Cursor)
	if err != nil {
		return ReviewPage{}, err
	}
	var page ReviewPage
	err = l.Read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, memorySelect+`
			 WHERE m.space_id = $1 AND m.space_id = ANY($2) AND `+reviewWaiting+`
			   AND (`+reviewGroup+`, m.seq) > ($3::int, $4::bigint)
			 ORDER BY `+reviewGroup+`, m.seq
			 LIMIT $5`,
			q.SpaceID, scope.SpaceIDs(), afterGroup, afterSeq, limit+1)
		if err != nil {
			return fmt.Errorf("ledger: review queue: %w", err)
		}
		page.Memories, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Memory, error) {
			m, err := scanMemory(r)
			if err != nil {
				return Memory{}, err
			}
			return *m, nil
		})
		if err != nil {
			return fmt.Errorf("ledger: review queue: %w", err)
		}
		if err := attachDetails(ctx, tx, pointers(page.Memories)); err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `SELECT count(*) FROM v2.memories m
			 WHERE m.space_id = $1 AND m.space_id = ANY($2) AND `+reviewWaiting,
			q.SpaceID, scope.SpaceIDs()).Scan(&page.Total)
		if err != nil {
			return fmt.Errorf("ledger: review queue: %w", err)
		}
		return nil
	})
	if err != nil {
		return ReviewPage{}, err
	}
	if len(page.Memories) > limit {
		page.Memories = page.Memories[:limit]
		page.HasMore = true
		last := page.Memories[limit-1]
		page.NextCursor = encodeReviewCursor(reviewGroupOf(&last), last.seq)
	}
	return page, nil
}

// pointers points at each element of ms, for the batch loaders.
func pointers(ms []Memory) []*Memory {
	out := make([]*Memory, len(ms))
	for i := range ms {
		out[i] = &ms[i]
	}
	return out
}

func reviewGroupOf(m *Memory) int {
	switch {
	case m.Lifecycle == lifecycle.Proposed:
		return 0
	case m.Flags.Has(lifecycle.Conflict):
		return 1
	}
	return 2
}

// A Review cursor carries the group and the sequence number: "v<group>.<seq>".
func encodeReviewCursor(group int, seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte("v" + strconv.Itoa(group) + "." + strconv.FormatInt(seq, 10)))
}

func decodeReviewCursor(c string) (int, int64, error) {
	if c == "" {
		return -1, 0, nil
	}
	bad := invalid("cursor", "is not a cursor from this list; start again without one")
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil || len(raw) < 4 || raw[0] != 'v' {
		return 0, 0, bad
	}
	g, s, ok := strings.Cut(string(raw[1:]), ".")
	group, err1 := strconv.Atoi(g)
	seq, err2 := strconv.ParseInt(s, 10, 64)
	if !ok || err1 != nil || err2 != nil || group < 0 || group > 2 || seq < 1 {
		return 0, 0, bad
	}
	return group, seq, nil
}

func pageLimit(n int) (int, error) {
	switch {
	case n < 0:
		return 0, invalid("limit", "must be positive")
	case n == 0:
		return DefaultPageSize, nil
	case n > MaxPageSize:
		return MaxPageSize, nil
	}
	return n, nil
}

// Cursors are opaque to clients: a kind letter and a sequence number,
// base64url-encoded. The letter keeps a receipts cursor from being used
// on memories.
func encodeCursor(kind byte, seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(string(kind) + strconv.FormatInt(seq, 10)))
}

func decodeCursor(c string, kind byte) (int64, error) {
	if c == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil || len(raw) < 2 || raw[0] != kind {
		return 0, invalid("cursor", "is not a cursor from this list; start again without one")
	}
	n, err := strconv.ParseInt(string(raw[1:]), 10, 64)
	if err != nil || n < 1 {
		return 0, invalid("cursor", "is not a cursor from this list; start again without one")
	}
	return n, nil
}

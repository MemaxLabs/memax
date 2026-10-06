package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Reads (R-, plan 25 §5.3 "Reads are not receipts"): what agents read,
// recorded off recall's latency path. A read changes nothing in the
// record, so it has no receipt and never goes through Apply. Two ways in:
//
//   - RecordReads writes a batch. The API's recorder (internal/reads)
//     buffers what recall, search, get, list and the session-start digest
//     return, and flushes every 250 ms.
//   - RecordCompileLoad writes one compile load at once: a session-start
//     hook reporting the compile (C-) its agent loaded natively. It counts
//     as a read of every fact in that compile (§5.10), is idempotent by
//     key, and is decided by policy (ActionRead), so a credential can't
//     count reads in a space its agent isn't connected to.
//
// Both run as memax_v2 under the spaces they write (migration 037), and
// allocate R- numbers from the tenant's counter in the same transaction.
// Neither stores memory text or query text.

// ReaderKind is who read: an agent connection, or a person reporting their
// agent's session start.
type ReaderKind string

// The reader kinds.
const (
	ReaderAgent  ReaderKind = "agent"
	ReaderPerson ReaderKind = "person"
)

// ReaderKinds lists every reader kind.
var ReaderKinds = []ReaderKind{ReaderAgent, ReaderPerson}

// ReadKind is what a read went through.
type ReadKind string

// The read kinds.
const (
	ReadRecall      ReadKind = "recall"       // memax_recall with a query
	ReadSearch      ReadKind = "search"       // memax_search
	ReadGet         ReadKind = "get"          // one memory (memax_get, GET /v2/memories/{ref})
	ReadList        ReadKind = "list"         // a page of kept memories (memax_list, GET …/memories)
	ReadDigest      ReadKind = "digest"       // memax_recall without a query: the session-start digest
	ReadCompileLoad ReadKind = "compile_load" // a session-start hook reported the compile its agent loaded
)

// ReadKinds lists every read kind.
var ReadKinds = []ReadKind{ReadRecall, ReadSearch, ReadGet, ReadList, ReadDigest, ReadCompileLoad}

// Valid reports whether k is a known read kind.
func (k ReadKind) Valid() bool { return slices.Contains(ReadKinds, k) }

// ReadVias are the surfaces a read comes through.
var ReadVias = []policy.Via{policy.ViaMCP, policy.ViaAPI, policy.ViaCLI}

// MaxReadMemories bounds the memories one read lists by id; a read that
// returned more still counts them all.
const MaxReadMemories = 200

// ReadEvent is one read of one space, as the surface that served it
// reports it. A read across several spaces is one event per space.
type ReadEvent struct {
	SpaceID  uuid.UUID
	TenantID uuid.UUID
	// Reader is an agent (ConnectionID set) or a person.
	Reader       ReaderKind
	ConnectionID uuid.UUID
	// PersonID is the person, or the person the agent works for.
	PersonID uuid.UUID
	// Agent is the agent kind ("claude-code"), when known.
	Agent      string
	Kind       ReadKind
	Via        policy.Via
	SessionRef string
	// CompileID or CompileRef ("C-0012", within the space) is the compile
	// run read, when the read served a compiled file.
	CompileID  uuid.UUID
	CompileRef string
	// Memories are the memories returned directly.
	Memories []uuid.UUID
	// Count is how many memories the read returned, when more than
	// Memories lists (0: len(Memories)).
	Count int
	At    time.Time
}

// ReadRecorder records reads off the request path; Record must never
// block (internal/reads.Recorder).
type ReadRecorder interface {
	Record(ReadEvent)
}

// Read is a recorded read (R-).
type Read struct {
	ID           uuid.UUID  `json:"id"`
	Ref          string     `json:"ref"`
	SpaceID      uuid.UUID  `json:"space_id"`
	ReaderKind   ReaderKind `json:"reader_kind"`
	ConnectionID *uuid.UUID `json:"connection_id,omitempty"`
	PersonID     uuid.UUID  `json:"person_id"`
	Agent        string     `json:"agent,omitempty"`
	Kind         ReadKind   `json:"kind"`
	Via          policy.Via `json:"via"`
	SessionRef   string     `json:"session_ref,omitempty"`
	// Compile is the compile run read (C-0012), for a compiled digest or a
	// compile load.
	Compile string `json:"compile,omitempty"`
	// Memories is how many memories the read covered: for a compile, every
	// fact in it.
	Memories int `json:"memories"`
	// MemoryRefs are the memories returned directly, in display order.
	MemoryRefs []string  `json:"memory_refs"`
	ReadAt     time.Time `json:"read_at"`
	RecordedAt time.Time `json:"recorded_at"`

	seq int64
}

// ReadStats is what RecordReads did with a batch.
type ReadStats struct {
	// Written reads, and Skipped ones: malformed, or in a space that no
	// longer matches its tenant.
	Written, Skipped int
}

// RecordReads writes a batch of reads in one transaction, with their R-
// numbers and their rollups. It is the recorder's sink: off the request
// path, never through Apply (reads are not receipts).
func (l *Ledger) RecordReads(ctx context.Context, events []ReadEvent) (ReadStats, error) {
	if l == nil {
		return ReadStats{}, ErrDisabled
	}
	var stats ReadStats
	batch := make([]ReadEvent, 0, len(events))
	for _, e := range events {
		if e, ok := normalizeRead(e, l.now()); ok {
			batch = append(batch, e)
		} else {
			stats.Skipped++
		}
	}
	if len(batch) == 0 {
		return stats, nil
	}
	if err := l.ensureReadMonths(ctx, batch); err != nil {
		return stats, err
	}

	var scope Scope
	for _, e := range batch {
		if _, ok := scope.Grant(e.SpaceID); !ok {
			scope.Spaces = append(scope.Spaces, SpaceGrant{SpaceID: e.SpaceID, TenantID: e.TenantID})
		}
	}
	tx, _, err := l.begin(ctx, scope, pgx.ReadWrite)
	if err != nil {
		return stats, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The space's tenant is the one public.hubs says (v2.spaces), whatever
	// the caller passed; a read of a space that's gone is skipped.
	tenants := map[uuid.UUID]uuid.UUID{}
	rows, err := tx.Query(ctx, `SELECT id, tenant_id FROM v2.spaces`)
	if err != nil {
		return stats, fmt.Errorf("ledger: record reads: %w", err)
	}
	for rows.Next() {
		var id, tenant uuid.UUID
		if err := rows.Scan(&id, &tenant); err != nil {
			rows.Close()
			return stats, fmt.Errorf("ledger: record reads: %w", err)
		}
		tenants[id] = tenant
	}
	if err := rows.Err(); err != nil {
		return stats, fmt.Errorf("ledger: record reads: %w", err)
	}
	kept := batch[:0]
	for _, e := range batch {
		if tenants[e.SpaceID] == e.TenantID {
			kept = append(kept, e)
		} else {
			stats.Skipped++
		}
	}
	batch = kept
	if len(batch) == 0 {
		return stats, nil
	}

	compiles, err := resolveReadCompiles(ctx, tx, batch)
	if err != nil {
		return stats, err
	}
	reads := make([]pendingRead, 0, len(batch))
	for _, e := range batch {
		r := pendingRead{event: e, id: newID()}
		if c, ok := compiles.find(e); ok {
			r.compile = &c
		} else if e.Kind == ReadCompileLoad {
			stats.Skipped++ // a load of a compile that isn't there
			continue
		}
		reads = append(reads, r)
	}
	if len(reads) == 0 {
		return stats, nil
	}
	if err := writeReads(ctx, tx, reads); err != nil {
		return stats, err
	}
	if err := tx.Commit(ctx); err != nil {
		return stats, mapDBError(fmt.Errorf("ledger: record reads: commit: %w", err))
	}
	stats.Written = len(reads)
	return stats, nil
}

// normalizeRead checks an event and clips what the database bounds. A
// read the database would refuse is dropped here, so one bad event never
// costs the rest of its batch.
func normalizeRead(e ReadEvent, now time.Time) (ReadEvent, bool) {
	if e.SpaceID == uuid.Nil || e.TenantID == uuid.Nil || e.PersonID == uuid.Nil || !e.Kind.Valid() ||
		!slices.Contains(ReadVias, e.Via) {
		return e, false
	}
	switch e.Reader {
	case ReaderAgent:
		if e.ConnectionID == uuid.Nil {
			return e, false
		}
	case ReaderPerson:
		e.ConnectionID = uuid.Nil
	default:
		return e, false
	}
	if e.Agent != "" && !AgentKind(e.Agent).Valid() {
		e.Agent = string(AgentFromV1(e.Agent))
	}
	e.SessionRef = strings.TrimSpace(e.SessionRef)
	if len(e.SessionRef) > 255 {
		e.SessionRef = e.SessionRef[:255]
	}
	if e.At.IsZero() {
		e.At = now
	}
	e.Memories = dedupe(e.Memories)
	if e.Count < len(e.Memories) {
		e.Count = len(e.Memories)
	}
	if len(e.Memories) > MaxReadMemories {
		e.Memories = e.Memories[:MaxReadMemories]
	}
	if e.Kind == ReadCompileLoad && e.CompileID == uuid.Nil && e.CompileRef == "" {
		return e, false
	}
	return e, true
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// ensureReadMonths makes sure every month in the batch has its partition
// (and the month after it has one too, so the next month's first flush
// never creates one). It runs in its own short transaction: creating a
// partition locks v2.reads, and that lock shouldn't last a batch. Months
// already ensured by this process are skipped.
func (l *Ledger) ensureReadMonths(ctx context.Context, batch []ReadEvent) error {
	for _, e := range batch {
		if err := l.ensureReadMonth(ctx, e.At); err != nil {
			return err
		}
	}
	return nil
}

// ensureReadMonth ensures at's month (and the next), once per process.
func (l *Ledger) ensureReadMonth(ctx context.Context, at time.Time) error {
	m := time.Date(at.UTC().Year(), at.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	if _, done := l.readMonths.Load(m); done {
		return nil
	}
	if err := l.EnsureReadPartitions(ctx, m, 1); err != nil {
		return err
	}
	l.readMonths.Store(m, true)
	return nil
}

// EnsureReadPartitions creates the monthly partitions of v2.reads for
// at's month and the `ahead` months after it, when missing.
func (l *Ledger) EnsureReadPartitions(ctx context.Context, at time.Time, ahead int) error {
	if l == nil {
		return ErrDisabled
	}
	tx, _, err := l.begin(ctx, Scope{}, pgx.ReadWrite)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT v2.ensure_reads_partitions($1, $2)`, at, ahead); err != nil {
		return fmt.Errorf("ledger: ensure reads partitions: %w", err)
	}
	return tx.Commit(ctx)
}

// ReadRetentionMonths is how long reads and their rollups are kept: the longest
// activity retention of any plan (D9: Team keeps a year), plus the month
// in progress.
const ReadRetentionMonths = 13

// PruneReads drops the reads partitions that ended before `before`'s
// month and the rollup days before it. before must be at least 90 days
// ago (the database refuses anything newer). It returns the partitions
// dropped.
func (l *Ledger) PruneReads(ctx context.Context, before time.Time) (int, error) {
	if l == nil {
		return 0, ErrDisabled
	}
	tx, _, err := l.begin(ctx, Scope{}, pgx.ReadWrite)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var n int
	if err := tx.QueryRow(ctx, `SELECT v2.prune_reads($1)`, before).Scan(&n); err != nil {
		return 0, mapDBError(fmt.Errorf("ledger: prune reads: %w", err))
	}
	return n, tx.Commit(ctx)
}

// readCompile is a compile run a read covered.
type readCompile struct {
	id    uuid.UUID
	space uuid.UUID
	seq   int64
	facts int
}

type readCompiles []readCompile

func (cs readCompiles) find(e ReadEvent) (readCompile, bool) {
	_, n, isRef := ParseRef(e.CompileRef)
	for _, c := range cs {
		if c.space != e.SpaceID {
			continue
		}
		if (e.CompileID != uuid.Nil && c.id == e.CompileID) || (isRef && c.seq == n) {
			return c, true
		}
	}
	return readCompile{}, false
}

// resolveReadCompiles loads the compile runs a batch read, by id or by
// display ID within the read's space. A compile that isn't found (or has
// failed) is dropped from its read, which keeps its direct memories.
func resolveReadCompiles(ctx context.Context, tx pgx.Tx, batch []ReadEvent) (readCompiles, error) {
	var ids, spaces []uuid.UUID
	var seqs []int64
	for _, e := range batch {
		if e.CompileID != uuid.Nil {
			ids = append(ids, e.CompileID)
		}
		if p, n, ok := ParseRef(e.CompileRef); ok && p == PrefixCompile {
			spaces = append(spaces, e.SpaceID)
			seqs = append(seqs, n)
		}
	}
	if len(ids) == 0 && len(seqs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT c.id, c.space_id, c.seq, cardinality(c.refs)
		  FROM v2.compile_runs c
		 WHERE c.status <> 'failed'
		   AND (c.id = ANY ($1)
		        OR (c.space_id, c.seq) IN (SELECT * FROM unnest($2::uuid[], $3::bigint[])))`,
		ids, spaces, seqs)
	if err != nil {
		return nil, fmt.Errorf("ledger: record reads: resolve compiles: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (readCompile, error) {
		var c readCompile
		err := r.Scan(&c.id, &c.space, &c.seq, &c.facts)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: record reads: resolve compiles: %w", err)
	}
	return out, nil
}

// pendingRead is a read about to be written.
type pendingRead struct {
	event   ReadEvent
	id      uuid.UUID
	seq     int64
	compile *readCompile
}

func (p *pendingRead) memories() int {
	n := p.event.Count
	if p.compile != nil {
		n += p.compile.facts
	}
	return n
}

// writeReads allocates the reads' R- numbers, inserts them and bumps
// their rollups, in the caller's transaction.
func writeReads(ctx context.Context, tx pgx.Tx, reads []pendingRead) error {
	// R- numbers: one block per tenant, tenants in a fixed order so two
	// flushers never wait on each other's counters in a cycle.
	perTenant := map[uuid.UUID][]int{}
	var tenants []uuid.UUID
	for i, r := range reads {
		t := r.event.TenantID
		if _, ok := perTenant[t]; !ok {
			tenants = append(tenants, t)
		}
		perTenant[t] = append(perTenant[t], i)
	}
	slices.SortFunc(tenants, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, t := range tenants {
		idx := perTenant[t]
		var first int64
		err := tx.QueryRow(ctx, `
			INSERT INTO v2.id_counters AS c (tenant_id, prefix, next) VALUES ($1, 'R', $2::bigint + 1)
			ON CONFLICT (tenant_id, prefix) DO UPDATE SET next = c.next + $2::bigint
			RETURNING next - $2::bigint`, t, len(idx)).Scan(&first)
		if err != nil {
			return fmt.Errorf("ledger: allocate R- numbers: %w", err)
		}
		for j, i := range idx {
			reads[i].seq = first + int64(j)
		}
	}

	n := len(reads)
	ids, tenantIDs, spaceIDs, compileIDs, connIDs, persons :=
		make([]uuid.UUID, n), make([]uuid.UUID, n), make([]uuid.UUID, n), make([]pgtype.UUID, n), make([]pgtype.UUID, n), make([]uuid.UUID, n)
	seqs, counts := make([]int64, n), make([]int32, n)
	readerKinds, kinds, vias, agents, sessions, memories :=
		make([]string, n), make([]string, n), make([]string, n), make([]pgtype.Text, n), make([]pgtype.Text, n), make([]string, n)
	at := make([]time.Time, n)
	for i, r := range reads {
		e := r.event
		ids[i], tenantIDs[i], spaceIDs[i], persons[i], seqs[i] = r.id, e.TenantID, e.SpaceID, e.PersonID, r.seq
		readerKinds[i], kinds[i], vias[i] = string(e.Reader), string(e.Kind), string(e.Via)
		if e.ConnectionID != uuid.Nil {
			connIDs[i] = pgtype.UUID{Bytes: e.ConnectionID, Valid: true}
		}
		if r.compile != nil {
			compileIDs[i] = pgtype.UUID{Bytes: r.compile.id, Valid: true}
		}
		agents[i] = pgtype.Text{String: e.Agent, Valid: e.Agent != ""}
		sessions[i] = pgtype.Text{String: e.SessionRef, Valid: e.SessionRef != ""}
		memories[i] = uuidArray(e.Memories)
		counts[i] = int32(r.memories())
		at[i] = e.At
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO v2.reads (id, tenant_id, space_id, seq, reader_kind, connection_id, person_id, agent, kind, via,
		                      session_ref, compile_id, memory_ids, memories, read_at)
		SELECT u.id, u.tenant_id, u.space_id, u.seq, u.reader_kind, u.connection_id, u.person_id, u.agent, u.kind,
		       u.via, u.session_ref, u.compile_id, u.memory_ids::uuid[], u.memories, u.read_at
		  FROM unnest($1::uuid[], $2::uuid[], $3::uuid[], $4::bigint[], $5::text[], $6::uuid[], $7::uuid[],
		              $8::text[], $9::text[], $10::text[], $11::text[], $12::uuid[], $13::text[], $14::int[],
		              $15::timestamptz[])
		       AS u(id, tenant_id, space_id, seq, reader_kind, connection_id, person_id, agent, kind, via,
		            session_ref, compile_id, memory_ids, memories, read_at)`,
		ids, tenantIDs, spaceIDs, seqs, readerKinds, connIDs, persons, agents, kinds, vias, sessions, compileIDs,
		memories, counts, at); err != nil {
		return fmt.Errorf("ledger: write reads: %w", err)
	}
	return bumpRollups(ctx, tx, reads)
}

// rollupKey is one row of v2.read_rollups.
type rollupKey struct {
	space, subject, reader uuid.UUID
	day                    string
	agent                  string
}

type rollupRow struct {
	key         rollupKey
	tenant      uuid.UUID
	subjectKind string
	readerKind  ReaderKind
	person      uuid.UUID
	reads       int32
	last        time.Time
}

// bumpRollups adds the reads to their rollups: one per memory read
// directly, and one for the compile a read covered (its facts are
// resolved through compile_runs.refs when counts are read). Rows go in
// key order, so concurrent flushers lock them in the same order.
func bumpRollups(ctx context.Context, tx pgx.Tx, reads []pendingRead) error {
	byKey := map[rollupKey]*rollupRow{}
	add := func(r pendingRead, kind string, subject uuid.UUID) {
		e := r.event
		reader := e.ConnectionID
		if e.Reader == ReaderPerson {
			reader = e.PersonID
		}
		k := rollupKey{space: e.SpaceID, subject: subject, reader: reader, day: e.At.UTC().Format(time.DateOnly), agent: e.Agent}
		row, ok := byKey[k]
		if !ok {
			row = &rollupRow{key: k, tenant: e.TenantID, subjectKind: kind, readerKind: e.Reader, person: e.PersonID}
			byKey[k] = row
		}
		row.reads++
		if e.At.After(row.last) {
			row.last = e.At
		}
	}
	for _, r := range reads {
		for _, m := range r.event.Memories {
			add(r, "memory", m)
		}
		if r.compile != nil {
			add(r, "compile", r.compile.id)
		}
	}
	if len(byKey) == 0 {
		return nil
	}
	rows := make([]*rollupRow, 0, len(byKey))
	for _, r := range byKey {
		rows = append(rows, r)
	}
	slices.SortFunc(rows, func(a, b *rollupRow) int {
		for _, c := range []int{
			strings.Compare(a.key.space.String(), b.key.space.String()),
			strings.Compare(a.key.subject.String(), b.key.subject.String()),
			strings.Compare(a.key.day, b.key.day),
			strings.Compare(a.key.reader.String(), b.key.reader.String()),
			strings.Compare(a.key.agent, b.key.agent),
		} {
			if c != 0 {
				return c
			}
		}
		return 0
	})
	n := len(rows)
	spaces, tenants, subjects, readers, persons := make([]uuid.UUID, n), make([]uuid.UUID, n), make([]uuid.UUID, n), make([]uuid.UUID, n), make([]uuid.UUID, n)
	kinds, readerKinds, agents, days := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	counts, lasts := make([]int32, n), make([]time.Time, n)
	for i, r := range rows {
		spaces[i], tenants[i], subjects[i], readers[i], persons[i] = r.key.space, r.tenant, r.key.subject, r.key.reader, r.person
		kinds[i], readerKinds[i], agents[i], days[i] = r.subjectKind, string(r.readerKind), r.key.agent, r.key.day
		counts[i], lasts[i] = r.reads, r.last
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO v2.read_rollups AS r (space_id, tenant_id, subject_kind, subject_id, day, reader_key, reader_kind,
		                                  person_id, agent, reads, last_read_at)
		SELECT u.space_id, u.tenant_id, u.subject_kind, u.subject_id, u.day::date, u.reader_key, u.reader_kind,
		       u.person_id, u.agent, u.reads, u.last_read_at
		  FROM unnest($1::uuid[], $2::uuid[], $3::text[], $4::uuid[], $5::text[], $6::uuid[], $7::text[], $8::uuid[],
		              $9::text[], $10::int[], $11::timestamptz[])
		       WITH ORDINALITY AS u(space_id, tenant_id, subject_kind, subject_id, day, reader_key, reader_kind,
		                            person_id, agent, reads, last_read_at, ord)
		 ORDER BY u.ord
		ON CONFLICT (space_id, subject_id, day, reader_key, agent) DO UPDATE
		   SET reads = r.reads + EXCLUDED.reads,
		       last_read_at = greatest(r.last_read_at, EXCLUDED.last_read_at)`,
		spaces, tenants, kinds, subjects, days, readers, readerKinds, persons, agents, counts, lasts); err != nil {
		return fmt.Errorf("ledger: bump read rollups: %w", err)
	}
	return nil
}

// --- Compile loads ---

// CommandLoadCompile names a compile load's idempotency records in
// v2.command_keys (it isn't an Apply command: it writes no receipt).
const CommandLoadCompile CommandName = "load_compile"

// LoadWindow bounds how far back a compile load may be reported: a
// session start that couldn't reach Memax reports it later, within a day.
const LoadWindow = 24 * time.Hour

// CompileLoad is a session-start hook reporting that its agent loaded a
// compile natively (plan 25 §7.5): it counts as a read of every fact in
// that compile.
type CompileLoad struct {
	// Actor is an agent connection (Actor.ID), or a person whose CLI the
	// hook runs under.
	Actor Actor
	Scope Scope
	Via   policy.Via
	// SpaceID is the space the compile belongs to.
	SpaceID uuid.UUID
	// Compile is the compile run: its display ID (C-0012) or id.
	Compile string
	// Agent is the agent whose session loaded it. An agent connection is
	// always its own agent; a person names one.
	Agent      string
	SessionRef string
	// LoadedAt is when the session loaded it: zero means now; at most a
	// day ago (LoadWindow), and never in the future.
	LoadedAt       time.Time
	IdempotencyKey string
}

// CompileLoadResult is RecordCompileLoad's answer.
type CompileLoadResult struct {
	// Read is the recorded read; nil when refused.
	Read *Read
	// Policy says why a load was refused (EffectRefuse), or applied.
	Policy policy.Decision
	// Replayed: the idempotency key was already used for this load.
	Replayed bool
}

// RecordCompileLoad records a compile load at once, idempotently by key.
// Who may report one is policy's ActionRead: a person who can read the
// space, or an agent connected to it (paused agents still read). A space
// outside the scope, or a compile that isn't in it, is ErrNotFound.
func (l *Ledger) RecordCompileLoad(ctx context.Context, c CompileLoad) (CompileLoadResult, error) {
	if l == nil {
		return CompileLoadResult{}, ErrDisabled
	}
	now := l.now()
	// The key covers what the client sent: a retry without loaded_at is
	// the same load, whenever it arrives.
	given := c.LoadedAt
	if err := c.validate(now); err != nil {
		return CompileLoadResult{}, err
	}
	grant, ok := c.Scope.Grant(c.SpaceID)
	if !ok {
		return CompileLoadResult{}, ErrNotFound
	}
	if c.Actor.Kind == policy.ActorAgent {
		c.Agent = c.Actor.Agent
	}
	if c.Agent != "" && !AgentKind(c.Agent).Valid() {
		c.Agent = string(AgentFromV1(c.Agent))
	}
	hash, err := c.hash(given)
	if err != nil {
		return CompileLoadResult{}, err
	}
	if err := l.ensureReadMonth(ctx, c.LoadedAt); err != nil {
		return CompileLoadResult{}, err
	}

	scope := c.Scope.Narrow(c.SpaceID)
	tx, _, err := l.begin(ctx, scope, pgx.ReadWrite)
	if err != nil {
		return CompileLoadResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sp, err := loadSpace(ctx, tx, c.SpaceID)
	if err != nil {
		return CompileLoadResult{}, err
	}
	dec := policy.Decide(toPolicyActor(c.Actor, c.Via, grant), policy.ActionRead, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return CompileLoadResult{Policy: dec}, nil
	}

	w := &writer{tx: tx, meta: &Meta{Actor: c.Actor, Scope: scope, Via: c.Via, IdempotencyKey: c.IdempotencyKey},
		command: CommandLoadCompile, hash: hash, now: now}
	claim, err := w.claimKey(ctx, c.SpaceID)
	if err != nil {
		return CompileLoadResult{}, mapDBError(err)
	}
	if claim != nil {
		if claim.objectID == nil {
			return CompileLoadResult{}, errors.New("ledger: compile load key without its read")
		}
		read, err := loadRead(ctx, tx, *claim.objectID)
		if err != nil {
			return CompileLoadResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return CompileLoadResult{}, mapDBError(err)
		}
		return CompileLoadResult{Read: read, Policy: dec, Replayed: true}, nil
	}

	compile, err := findCompile(ctx, tx, c.SpaceID, c.Compile)
	if err != nil {
		return CompileLoadResult{}, err
	}
	e := ReadEvent{SpaceID: sp.ID, TenantID: sp.TenantID, PersonID: c.Scope.PersonID, Agent: c.Agent,
		Kind: ReadCompileLoad, Via: c.Via, SessionRef: c.SessionRef, CompileID: compile.id, At: c.LoadedAt}
	switch c.Actor.Kind {
	case policy.ActorAgent:
		e.Reader, e.ConnectionID = ReaderAgent, c.Actor.ID
	default:
		e.Reader, e.PersonID = ReaderPerson, c.Actor.ID
	}
	e, ok = normalizeRead(e, now)
	if !ok {
		return CompileLoadResult{}, invalid("compile", "this load can't be recorded")
	}
	p := pendingRead{event: e, id: newID(), compile: &compile}
	if err := writeReads(ctx, tx, []pendingRead{p}); err != nil {
		return CompileLoadResult{}, mapDBError(err)
	}
	if err := w.record(ctx, Result{Outcome: OutcomeApplied, Policy: dec}, p.id); err != nil {
		return CompileLoadResult{}, mapDBError(err)
	}
	read, err := loadRead(ctx, tx, p.id)
	if err != nil {
		return CompileLoadResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CompileLoadResult{}, mapDBError(fmt.Errorf("ledger: record compile load: commit: %w", err))
	}
	l.log.Info("ledger: compile load", "read", read.Ref, "compile", read.Compile, "space_id", sp.ID.String(),
		"reader_kind", string(read.ReaderKind), "agent", read.Agent, "facts", read.Memories)
	return CompileLoadResult{Read: read, Policy: dec}, nil
}

func (c *CompileLoad) validate(now time.Time) error {
	if c.SpaceID == uuid.Nil {
		return invalid("space", "say which space")
	}
	if c.Actor.Kind != policy.ActorAgent && c.Actor.Kind != policy.ActorPerson {
		return invalid("actor", "only an agent or a person reports a compile load")
	}
	if c.Actor.ID == uuid.Nil || c.Scope.PersonID == uuid.Nil {
		return invalid("actor", "is missing")
	}
	if !slices.Contains(ReadVias, c.Via) {
		return invalid("via", "use mcp, api or cli")
	}
	c.IdempotencyKey = strings.TrimSpace(c.IdempotencyKey)
	if n := len(c.IdempotencyKey); n < 1 || n > 255 {
		return invalid("idempotency_key", "send an Idempotency-Key of 1 to 255 characters")
	}
	c.Compile = strings.TrimSpace(c.Compile)
	if _, err := uuid.Parse(c.Compile); err != nil {
		if p, _, ok := ParseRef(c.Compile); !ok || p != PrefixCompile {
			return invalid("compile", "use the compile's display ID, like C-0012, or its id")
		}
	}
	c.SessionRef = strings.TrimSpace(c.SessionRef)
	if len(c.SessionRef) > 255 {
		return invalid("session_ref", "is longer than 255 characters")
	}
	switch {
	case c.LoadedAt.IsZero():
		c.LoadedAt = now
	case c.LoadedAt.After(now.Add(5 * time.Minute)):
		return invalid("loaded_at", "is in the future; send when the session loaded the compile")
	case c.LoadedAt.Before(now.Add(-LoadWindow)):
		return invalid("loaded_at", "is more than a day ago; Memax counts loads reported within a day")
	}
	return nil
}

// hash identifies the load for its idempotency key: what the client
// sent, with loaded_at as given (zero when it left it to the server).
func (c *CompileLoad) hash(loadedAt time.Time) ([]byte, error) {
	var at int64
	if !loadedAt.IsZero() {
		at = loadedAt.UnixMicro()
	}
	raw, err := json.Marshal(struct {
		Space    uuid.UUID `json:"space"`
		Compile  string    `json:"compile"`
		Agent    string    `json:"agent"`
		Session  string    `json:"session"`
		LoadedAt int64     `json:"loaded_at"`
	}{c.SpaceID, c.Compile, c.Agent, c.SessionRef, at})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return sum[:], nil
}

// findCompile resolves a compile run by display ID or id within a space.
// A failed run compiled nothing, so nothing could have been loaded.
func findCompile(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, ref string) (readCompile, error) {
	var c readCompile
	var status string
	id, idErr := uuid.Parse(ref)
	_, n, _ := ParseRef(ref)
	if idErr != nil {
		id = uuid.Nil
	}
	err := tx.QueryRow(ctx, `
		SELECT id, space_id, seq, cardinality(refs), status
		  FROM v2.compile_runs
		 WHERE space_id = $1 AND (id = $2 OR seq = $3)`, spaceID, id, n).
		Scan(&c.id, &c.space, &c.seq, &c.facts, &status)
	if errNoRows(err) {
		return readCompile{}, ErrNotFound
	}
	if err != nil {
		return readCompile{}, fmt.Errorf("ledger: find compile: %w", err)
	}
	if status == string(CompileFailed) {
		return readCompile{}, invalid("compile", "%s failed, so there was nothing to load", FormatRef(PrefixCompile, c.seq))
	}
	return c, nil
}

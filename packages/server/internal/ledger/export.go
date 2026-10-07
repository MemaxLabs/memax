package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
)

// Export (plan 25 §7.2, rule 14; Phase 2 epic 2.4): a person takes a
// space's whole record. Two halves:
//
//   - Export, a command, writes the one `exported` receipt an export is
//     counted by, on the space's own stream (migration 045). It changes
//     nothing else.
//   - ReadExport reads the record in one snapshot (REPEATABLE READ, read
//     only, as memax_v2 in the person's scope) and hands it to an
//     ExportSink part by part: memories and receipts in batches, so a large
//     space streams in bounded memory. internal/export turns the parts
//     into the export's files.
//
// The export is written after the receipt commits, from a snapshot that
// includes it, so an export lists its own receipt. Its reads record no R-
// reads: those measure agents.

// The export's receipt verb (migration 045) and command.
const (
	ActionExported Action      = "exported"
	CommandExport  CommandName = "export"
)

// Export records that a person exported a space's record: one `exported`
// receipt by them, on the space's stream. Only a signed-in person who may
// read the space exports it (policy.ActionExport).
type Export struct {
	Meta
	SpaceID uuid.UUID
}

// Name implements Command.
func (*Export) Name() CommandName { return CommandExport }

// export is Export.
func (w *writer) export(ctx context.Context, c *Export) (Result, error) {
	grant, ok := w.meta.Scope.Grant(c.SpaceID)
	if !ok {
		return Result{}, ErrNotFound
	}
	sp, err := loadSpace(ctx, w.tx, c.SpaceID)
	if err != nil {
		return Result{}, err
	}
	if replay, err := w.claimKey(ctx, sp.ID); err != nil || replay != nil {
		if err != nil {
			return Result{}, err
		}
		return replay.result(ctx, w)
	}
	dec := policy.Decide(w.policyActor(grant), policy.ActionExport, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	stream, err := nextSpaceStreamVersion(ctx, w.tx, sp.ID)
	if err != nil {
		return Result{}, err
	}
	rc := w.objectReceipt(sp, ObjectSpace, sp.ID, SpaceObjectRef, ActionExported, stream, w.meta.Reason)
	if err := insertReceipt(ctx, w.tx, &rc); err != nil {
		return Result{}, err
	}
	res := Result{Outcome: OutcomeApplied, Policy: dec, Receipts: []Receipt{rc}}
	return res, w.record(ctx, res, uuid.Nil)
}

// nextSpaceStreamVersion is the next receipt version on a space's own
// stream (exports and Forgets of the whole space). It takes a transaction
// lock on the stream first: the space has no row of its own whose lock
// would serialise the writers, and two writers reading the same max would
// collide on (stream_id, stream_version).
func nextSpaceStreamVersion(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID) (int, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"memax.v2.space_stream:"+spaceID.String()); err != nil {
		return 0, fmt.Errorf("ledger: lock the space's stream: %w", err)
	}
	return nextStreamVersion(ctx, tx, spaceID)
}

// ---------------------------------------------------------------------
// Reading the record for an export
// ---------------------------------------------------------------------

// ExportSink receives a space's record from ReadExport, in the order of
// its methods: Space first, then Briefs through Reads once each, then
// Memories and Receipts in batches, then Seal. An error stops the read.
type ExportSink interface {
	Space(ExportSpaceInfo) error
	Briefs([]Brief) error
	Gates([]Gate) error
	Targets([]Target) error
	Agents([]ExportAgent) error
	Tombstones([]Tombstone) error
	Reads(ReadSummary) error
	// Memories is every memory of the space, any lifecycle, in display-ID
	// order, in batches.
	Memories([]ExportMemory) error
	// Receipts is every receipt of the space in chain order ((txid, seq),
	// as the sealer seals them), in batches.
	Receipts([]receiptchain.Receipt) error
	Seal(ExportSeal) error
}

// ExportSpaceInfo is the space being exported, as of the snapshot.
type ExportSpaceInfo struct {
	Space
	// AsOf is when the newest receipt in the snapshot was recorded (the
	// export's own receipt, normally); Receipts counts them all.
	AsOf     time.Time
	Receipts int64
}

// ExportMemory is one memory with everything the export says about it.
type ExportMemory struct {
	// Memory carries its sources and its active links.
	Memory Memory
	// Versions are every version of its statement, oldest first (drafts
	// included; the memory's Version is the one in force).
	Versions []MemoryVersion
	// Receipts are the receipts about it, oldest first.
	Receipts []ReceiptStamp
}

// ReceiptStamp is a receipt as a memory's file lists it.
type ReceiptStamp struct {
	ID         uuid.UUID `json:"id"`
	Seq        int64     `json:"seq"`
	Action     Action    `json:"action"`
	OccurredAt time.Time `json:"occurred_at"`
}

// ExportAgent is an agent connection that works in the space, or wrote to
// it.
type ExportAgent struct {
	ID          uuid.UUID `json:"id"`
	Agent       string    `json:"agent"`
	DisplayName string    `json:"display_name"`
	Surface     string    `json:"surface"`
	State       string    `json:"state"`
	// Autonomy is its level in this space; empty when it is no longer
	// connected here.
	Autonomy         policy.Autonomy `json:"autonomy,omitempty"`
	PersonID         uuid.UUID       `json:"person_id"`
	CreatedReceiptID uuid.UUID       `json:"created_receipt_id"`
	LastReceiptID    uuid.UUID       `json:"last_receipt_id"`
	CreatedAt        time.Time       `json:"created_at"`
}

// ReadSummary is the space's reads as counts (from the rollups): never
// what was read for, never words.
type ReadSummary struct {
	Total    int64          `json:"total"`
	ByReader []ReaderReads  `json:"by_reader"`
	Memories []SubjectReads `json:"memories"`
	Compiles []SubjectReads `json:"compiles"`
	Days     []DayReads     `json:"days"`
}

// ReaderReads counts one kind of reader's reads: an agent kind, or people.
type ReaderReads struct {
	ReaderKind string `json:"reader_kind"`
	Agent      string `json:"agent,omitempty"`
	Reads      int64  `json:"reads"`
	Readers    int64  `json:"readers"`
}

// SubjectReads counts the reads of one memory or compile run.
type SubjectReads struct {
	Ref        string    `json:"ref"`
	Reads      int64     `json:"reads"`
	Readers    int64     `json:"readers"`
	FirstDay   string    `json:"first_day"`
	LastReadAt time.Time `json:"last_read_at"`
}

// DayReads counts one UTC day's reads.
type DayReads struct {
	Day   string `json:"day"`
	Reads int64  `json:"reads"`
}

// ExportSeal is how far the space's chain is sealed in the snapshot.
type ExportSeal struct {
	// Head is nil before the first seal.
	Head *ChainHead
	// Checkpoints are every checkpoint, by number.
	Checkpoints []Checkpoint
	// Unsealed counts the receipts after the head.
	Unsealed int64
}

// Batch sizes for ReadExport.
const (
	exportMemoryBatch  = 500
	exportReceiptBatch = 5000
)

// ReadExport reads the space's whole record in one snapshot and hands it
// to sink. A space outside the scope is ErrNotFound.
func (l *Ledger) ReadExport(ctx context.Context, scope Scope, spaceID uuid.UUID, sink ExportSink) error {
	if l == nil {
		return ErrDisabled
	}
	grant, ok := scope.Grant(spaceID)
	if !ok {
		return ErrNotFound
	}
	scope = scope.Narrow(spaceID)
	return l.readSnapshot(ctx, scope, func(tx pgx.Tx) error {
		info, err := exportSpaceInfo(ctx, tx, spaceID)
		if err != nil {
			return err
		}
		info.Role = grant.Role
		if err := sink.Space(info); err != nil {
			return err
		}
		briefs, err := exportBriefs(ctx, tx, spaceID)
		if err != nil {
			return err
		}
		if err := sink.Briefs(briefs); err != nil {
			return err
		}
		gates, err := exportGates(ctx, tx, spaceID)
		if err != nil {
			return err
		}
		if err := sink.Gates(gates); err != nil {
			return err
		}
		targets, err := exportTargets(ctx, tx, scope, spaceID)
		if err != nil {
			return err
		}
		if err := sink.Targets(targets); err != nil {
			return err
		}
		agents, err := exportAgents(ctx, tx, spaceID)
		if err != nil {
			return err
		}
		if err := sink.Agents(agents); err != nil {
			return err
		}
		tombs, err := exportTombstones(ctx, tx, spaceID)
		if err != nil {
			return err
		}
		if err := sink.Tombstones(tombs); err != nil {
			return err
		}
		reads, err := exportReads(ctx, tx, spaceID)
		if err != nil {
			return err
		}
		if err := sink.Reads(reads); err != nil {
			return err
		}
		if err := exportMemories(ctx, tx, spaceID, sink); err != nil {
			return err
		}
		if err := exportReceipts(ctx, tx, spaceID, sink); err != nil {
			return err
		}
		seal, err := exportSeal(ctx, tx, grant, spaceID)
		if err != nil {
			return err
		}
		return sink.Seal(seal)
	})
}

func exportSpaceInfo(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID) (ExportSpaceInfo, error) {
	var info ExportSpaceInfo
	err := tx.QueryRow(ctx, `
		SELECT id, tenant_id, slug, name, kind, COALESCE(repository, ''), v2_enabled_at
		  FROM v2.spaces WHERE id = $1`, spaceID).
		Scan(&info.ID, &info.TenantID, &info.Slug, &info.Name, &info.Kind, &info.Repository, &info.V2EnabledAt)
	if errNoRows(err) {
		return info, ErrNotFound
	}
	if err != nil {
		return info, fmt.Errorf("ledger: export: space: %w", err)
	}
	var asOf *time.Time
	if err := tx.QueryRow(ctx, `SELECT max(recorded_at), count(*) FROM v2.receipts WHERE space_id = $1`, spaceID).
		Scan(&asOf, &info.Receipts); err != nil {
		return info, fmt.Errorf("ledger: export: receipts: %w", err)
	}
	if asOf != nil {
		info.AsOf = asOf.UTC()
	}
	return info, nil
}

func exportBriefs(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID) ([]Brief, error) {
	rows, err := tx.Query(ctx, briefSelect+` WHERE v.space_id = $1 ORDER BY v.version`, spaceID)
	if err != nil {
		return nil, fmt.Errorf("ledger: export: Brief: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Brief, error) {
		b, err := scanBrief(r)
		if err != nil {
			return Brief{}, err
		}
		return *b, nil
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: export: Brief: %w", err)
	}
	return out, nil
}

func exportGates(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID) ([]Gate, error) {
	rows, err := tx.Query(ctx, gateSelect+` WHERE g.space_id = $1 ORDER BY g.seq`, spaceID)
	if err != nil {
		return nil, fmt.Errorf("ledger: export: gates: %w", err)
	}
	// The zero time keeps each gate's stored status: whether a waiting gate
	// has expired depends on when the export is read, so the export says
	// waiting and gives expires_at.
	gates, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Gate, error) { return scanGate(r, time.Time{}) })
	if err != nil {
		return nil, fmt.Errorf("ledger: export: gates: %w", err)
	}
	if err := needsWeb(ctx, tx, gates); err != nil {
		return nil, err
	}
	out := make([]Gate, len(gates))
	for i, g := range gates {
		out[i] = *g
	}
	return out, nil
}

func exportTargets(ctx context.Context, tx pgx.Tx, scope Scope, spaceID uuid.UUID) ([]Target, error) {
	rows, err := tx.Query(ctx, targetSelect+`
		 WHERE t.space_id = $1
		 ORDER BY array_position($2::text[], t.kind), t.path NULLS LAST, t.id`, spaceID, kindsOrder())
	if err != nil {
		return nil, fmt.Errorf("ledger: export: targets: %w", err)
	}
	targets, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Target, error) { return scanTarget(r) })
	if err != nil {
		return nil, fmt.Errorf("ledger: export: targets: %w", err)
	}
	if err := fillTargets(ctx, tx, scope, targets); err != nil {
		return nil, err
	}
	out := make([]Target, len(targets))
	for i, t := range targets {
		out[i] = *t
	}
	return out, nil
}

func exportAgents(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID) ([]ExportAgent, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.id, c.agent, c.display_name, c.surface, c.state, COALESCE(s.autonomy, ''), c.person_id,
		       c.created_receipt_id, c.last_receipt_id, c.created_at
		  FROM v2.agent_connections c
		  LEFT JOIN v2.agent_connection_spaces s ON s.connection_id = c.id AND s.space_id = $1
		 WHERE s.space_id IS NOT NULL
		    OR c.id IN (SELECT DISTINCT actor_id FROM v2.receipts
		                 WHERE space_id = $1 AND actor_kind = 'agent' AND actor_id IS NOT NULL)
		 ORDER BY c.created_at, c.id`, spaceID)
	if err != nil {
		return nil, fmt.Errorf("ledger: export: agents: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ExportAgent, error) {
		var a ExportAgent
		err := r.Scan(&a.ID, &a.Agent, &a.DisplayName, &a.Surface, &a.State, &a.Autonomy, &a.PersonID,
			&a.CreatedReceiptID, &a.LastReceiptID, &a.CreatedAt)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: export: agents: %w", err)
	}
	return out, nil
}

func exportTombstones(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID) ([]Tombstone, error) {
	rows, err := tx.Query(ctx, tombstoneSelect+` WHERE t.space_id = $1 ORDER BY t.forgotten_at, t.id`, spaceID)
	if err != nil {
		return nil, fmt.Errorf("ledger: export: tombstones: %w", err)
	}
	ts, err := pgx.CollectRows(rows, scanTombstone)
	if err != nil {
		return nil, fmt.Errorf("ledger: export: tombstones: %w", err)
	}
	if err := attachWith(ctx, tx, ts); err != nil {
		return nil, err
	}
	return ts, nil
}

func exportReads(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID) (ReadSummary, error) {
	out := ReadSummary{ByReader: []ReaderReads{}, Memories: []SubjectReads{}, Compiles: []SubjectReads{}, Days: []DayReads{}}
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(reads), 0) FROM v2.read_rollups WHERE space_id = $1`, spaceID).
		Scan(&out.Total); err != nil {
		return out, fmt.Errorf("ledger: export: reads: %w", err)
	}
	rows, err := tx.Query(ctx, `
		SELECT reader_kind, agent, sum(reads), count(DISTINCT reader_key)
		  FROM v2.read_rollups WHERE space_id = $1
		 GROUP BY reader_kind, agent ORDER BY reader_kind, agent`, spaceID)
	if err != nil {
		return out, fmt.Errorf("ledger: export: reads: %w", err)
	}
	if out.ByReader, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ReaderReads, error) {
		var x ReaderReads
		err := r.Scan(&x.ReaderKind, &x.Agent, &x.Reads, &x.Readers)
		return x, err
	}); err != nil {
		return out, fmt.Errorf("ledger: export: reads: %w", err)
	}
	subjects := func(kind, table string, prefix Prefix) ([]SubjectReads, error) {
		rows, err := tx.Query(ctx, `
			SELECT s.seq, sum(r.reads), count(DISTINCT r.reader_key), min(r.day)::text, max(r.last_read_at)
			  FROM v2.read_rollups r JOIN `+table+` s ON s.id = r.subject_id
			 WHERE r.space_id = $1 AND r.subject_kind = $2
			 GROUP BY s.seq ORDER BY s.seq`, spaceID, kind)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SubjectReads, error) {
			var x SubjectReads
			var seq int64
			err := r.Scan(&seq, &x.Reads, &x.Readers, &x.FirstDay, &x.LastReadAt)
			x.Ref, x.LastReadAt = FormatRef(prefix, seq), x.LastReadAt.UTC()
			return x, err
		})
	}
	if out.Memories, err = subjects("memory", "v2.memories", PrefixMemory); err != nil {
		return out, fmt.Errorf("ledger: export: reads: %w", err)
	}
	if out.Compiles, err = subjects("compile", "v2.compile_runs", PrefixCompile); err != nil {
		return out, fmt.Errorf("ledger: export: reads: %w", err)
	}
	rows, err = tx.Query(ctx, `
		SELECT day::text, sum(reads) FROM v2.read_rollups WHERE space_id = $1 GROUP BY day ORDER BY day`, spaceID)
	if err != nil {
		return out, fmt.Errorf("ledger: export: reads: %w", err)
	}
	if out.Days, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (DayReads, error) {
		var x DayReads
		err := r.Scan(&x.Day, &x.Reads)
		return x, err
	}); err != nil {
		return out, fmt.Errorf("ledger: export: reads: %w", err)
	}
	return out, nil
}

// exportMemories hands every memory to the sink, in batches by display ID,
// each with its sources, links, versions and receipts.
func exportMemories(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, sink ExportSink) error {
	var after int64
	for {
		rows, err := tx.Query(ctx, memorySelect+`
			 WHERE m.space_id = $1 AND m.seq > $2
			 ORDER BY m.seq LIMIT $3`, spaceID, after, exportMemoryBatch)
		if err != nil {
			return fmt.Errorf("ledger: export: memories: %w", err)
		}
		ms, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Memory, error) { return scanMemory(r) })
		if err != nil {
			return fmt.Errorf("ledger: export: memories: %w", err)
		}
		if len(ms) == 0 {
			return nil
		}
		batch, err := exportMemoryBatchOf(ctx, tx, spaceID, ms)
		if err != nil {
			return err
		}
		if err := sink.Memories(batch); err != nil {
			return err
		}
		if len(ms) < exportMemoryBatch {
			return nil
		}
		after = ms[len(ms)-1].seq
	}
}

func exportMemoryBatchOf(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, ms []*Memory) ([]ExportMemory, error) {
	ids := make([]uuid.UUID, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	links, err := activeLinks(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	sources := map[uuid.UUID][]Source{}
	rows, err := tx.Query(ctx, `
		SELECT ms.memory_id, s.id, s.kind, s.ref, COALESCE(s.uri, ''), s.locator, s.external, s.trust_class,
		       COALESCE(s.quote, ''), COALESCE(s.content_hash, ''), s.created_at
		  FROM v2.memory_sources ms
		  JOIN v2.sources s ON s.id = ms.source_id
		 WHERE ms.memory_id = ANY($1) AND ms.space_id = $2
		 ORDER BY ms.memory_id, s.created_at, s.id`, ids, spaceID)
	if err != nil {
		return nil, fmt.Errorf("ledger: export: sources: %w", err)
	}
	for rows.Next() {
		var memory uuid.UUID
		var s Source
		var locator []byte
		if err := rows.Scan(&memory, &s.ID, &s.Kind, &s.Ref, &s.URI, &locator, &s.External, &s.Trust, &s.Quote,
			&s.ContentHash, &s.CreatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("ledger: export: sources: %w", err)
		}
		s.Locator = json.RawMessage(locator)
		sources[memory] = append(sources[memory], s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: export: sources: %w", err)
	}
	versions := map[uuid.UUID][]MemoryVersion{}
	rows, err = tx.Query(ctx, `
		SELECT memory_id, version, COALESCE(statement, ''), receipt_id, created_at
		  FROM v2.memory_versions
		 WHERE memory_id = ANY($1) AND space_id = $2
		 ORDER BY memory_id, version`, ids, spaceID)
	if err != nil {
		return nil, fmt.Errorf("ledger: export: versions: %w", err)
	}
	for rows.Next() {
		var memory uuid.UUID
		var v MemoryVersion
		if err := rows.Scan(&memory, &v.Version, &v.Statement, &v.ReceiptID, &v.CreatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("ledger: export: versions: %w", err)
		}
		versions[memory] = append(versions[memory], v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: export: versions: %w", err)
	}
	stamps := map[uuid.UUID][]ReceiptStamp{}
	rows, err = tx.Query(ctx, `
		SELECT object_id, id, seq, action, occurred_at
		  FROM v2.receipts
		 WHERE space_id = $1 AND object_id = ANY($2)
		 ORDER BY seq`, spaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("ledger: export: receipts: %w", err)
	}
	for rows.Next() {
		var object uuid.UUID
		var st ReceiptStamp
		if err := rows.Scan(&object, &st.ID, &st.Seq, &st.Action, &st.OccurredAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("ledger: export: receipts: %w", err)
		}
		stamps[object] = append(stamps[object], st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: export: receipts: %w", err)
	}
	out := make([]ExportMemory, len(ms))
	for i, m := range ms {
		m.Sources, m.Links = sources[m.ID], links[m.ID]
		out[i] = ExportMemory{Memory: *m, Versions: versions[m.ID], Receipts: stamps[m.ID]}
	}
	return out, nil
}

// exportReceipts hands every receipt to the sink in chain order: the
// sealed ones exactly as the checkpoints cover them, then the rest.
func exportReceipts(ctx context.Context, tx pgx.Tx, spaceID uuid.UUID, sink ExportSink) error {
	afterTxid, afterSeq := "0", int64(0)
	for {
		rows, err := tx.Query(ctx, sealReceiptSelect+`
			 WHERE space_id = $1 AND (txid, seq) > ($2::xid8, $3)
			 ORDER BY txid, seq
			 LIMIT $4`, spaceID, afterTxid, afterSeq, exportReceiptBatch)
		if err != nil {
			return fmt.Errorf("ledger: export: receipts: %w", err)
		}
		batch, err := pgx.CollectRows(rows, scanChainReceipt)
		if err != nil {
			return fmt.Errorf("ledger: export: receipts: %w", err)
		}
		if len(batch) == 0 {
			return nil
		}
		out := make([]receiptchain.Receipt, len(batch))
		for i, r := range batch {
			out[i] = r.Receipt
		}
		if err := sink.Receipts(out); err != nil {
			return err
		}
		if len(batch) < exportReceiptBatch {
			return nil
		}
		last := batch[len(batch)-1]
		afterTxid, afterSeq = last.txid, last.Seq
	}
}

func exportSeal(ctx context.Context, tx pgx.Tx, grant SpaceGrant, spaceID uuid.UUID) (ExportSeal, error) {
	var out ExportSeal
	head, err := loadHead(ctx, tx, grant, false)
	if err != nil {
		return out, err
	}
	if head.Checkpoints > 0 {
		out.Head = head
	}
	rows, err := tx.Query(ctx, checkpointSelect+` WHERE space_id = $1 ORDER BY number`, spaceID)
	if err != nil {
		return out, fmt.Errorf("ledger: export: checkpoints: %w", err)
	}
	if out.Checkpoints, err = pgx.CollectRows(rows, scanCheckpoint); err != nil {
		return out, fmt.Errorf("ledger: export: checkpoints: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND (txid, seq) > ($2::xid8, $3)`,
		spaceID, head.lastTxid, head.LastSeq).Scan(&out.Unsealed); err != nil {
		return out, fmt.Errorf("ledger: export: unsealed: %w", err)
	}
	return out, nil
}

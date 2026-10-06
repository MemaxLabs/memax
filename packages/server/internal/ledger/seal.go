package ledger

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
)

// The receipt hash chain (plan 25 §5.3, migration 039). Sealing is
// bookkeeping about receipts, not a command: it writes no receipt (a seal
// receipt would need sealing in turn, forever), never goes through Apply,
// and runs as its own role, memax_v2_sealer, which reads receipts under the
// space's RLS and is the only role that may write chain heads and
// checkpoints. Requests run as memax_v2, which may only read them.
//
// Order and the watermark: a space's receipts are sealed in (txid, seq)
// order, and only those whose txid is below pg_snapshot_xmin of the
// sealing statement's snapshot, i.e. whose transactions have all ended. A
// receipt from a transaction still in flight has a txid at or above the
// watermark, so it waits, with everything after it, and is sealed in its
// place once it commits. A transaction that hasn't been assigned a txid yet
// will be assigned one above every txid already sealed. So nothing is ever
// skipped, and the head row (locked FOR UPDATE) makes two sealers of one
// space take turns: each receipt is sealed exactly once.

// SealerRole is the role seals are written as (migration 039).
const SealerRole = "memax_v2_sealer"

// QueueSeal is the River queue the sealer's jobs run on.
const QueueSeal = "seal"

// SealSpaceArgs is the River job that seals one space's new receipts.
// Unique by space over unfinished states, so a space has at most one seal
// job waiting or running (the head lock is the guarantee; this keeps the
// queue short).
type SealSpaceArgs struct {
	SpaceID uuid.UUID `json:"space_id"`
}

// Kind implements river.JobArgs.
func (SealSpaceArgs) Kind() string { return "seal_space" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (SealSpaceArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueSeal,
		MaxAttempts: 10,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
			rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
			rivertype.JobStateScheduled, rivertype.JobStateRetryable,
		}},
	}
}

// ChainHead is where a space's chain stands.
type ChainHead struct {
	SpaceID uuid.UUID `json:"space_id"`
	// Position counts the receipts sealed; LastSeq and LastReceiptID name
	// the last of them.
	Position      int64      `json:"position"`
	LastSeq       int64      `json:"last_seq"`
	LastReceiptID *uuid.UUID `json:"last_receipt_id,omitempty"`
	Head          string     `json:"head_sha256"`
	Checkpoints   int64      `json:"checkpoints"`
	SealedAt      *time.Time `json:"sealed_at,omitempty"`
	// VerifiedAt is the verifier's last full check, and VerifyProblems
	// what it found (0: the chain verified).
	VerifiedAt       *time.Time `json:"verified_at,omitempty"`
	VerifiedPosition *int64     `json:"verified_position,omitempty"`
	VerifyProblems   *int       `json:"verify_problems,omitempty"`

	lastTxid string
	head     receiptchain.Hash
}

// Checkpoint is one checkpoint of a space's chain, as /v2 serves it.
type Checkpoint struct {
	ID             uuid.UUID `json:"id"`
	SpaceID        uuid.UUID `json:"space_id"`
	TenantID       uuid.UUID `json:"tenant_id"`
	Number         int64     `json:"number"`
	PositionFrom   int64     `json:"position_from"`
	PositionTo     int64     `json:"position_to"`
	Receipts       int       `json:"receipts"`
	FirstReceiptID uuid.UUID `json:"first_receipt_id"`
	LastReceiptID  uuid.UUID `json:"last_receipt_id"`
	LastSeq        int64     `json:"last_seq"`
	PrevSHA256     string    `json:"prev_sha256"`
	ChainSHA256    string    `json:"chain_sha256"`
	MerkleRoot     string    `json:"merkle_root"`
	Format         int       `json:"format"`
	Signed         bool      `json:"signed"`
	KeyID          string    `json:"key_id,omitempty"`
	// Signature is the Ed25519 signature, base64.
	Signature string    `json:"signature,omitempty"`
	SealedAt  time.Time `json:"sealed_at"`
	// StoredAt is when its copy reached object storage.
	StoredAt *time.Time `json:"stored_at,omitempty"`

	objectKey      string
	uploadAttempts int
	chain          receiptchain.Checkpoint
}

// Chain is the checkpoint as receiptchain verifies it.
func (c Checkpoint) Chain() receiptchain.Checkpoint { return c.chain }

// ObjectKey is where its copy is in object storage ("" until uploaded).
func (c Checkpoint) ObjectKey() string { return c.objectKey }

// UploadAttempts counts failed uploads so far.
func (c Checkpoint) UploadAttempts() int { return c.uploadAttempts }

// SealResult is what one SealSpace run did.
type SealResult struct {
	SpaceID uuid.UUID
	// Sealed receipts, into Checkpoint; zero (and nil) when nothing was new.
	Sealed     int
	Checkpoint *Checkpoint
	// More: there were more receipts than the batch took; seal again.
	More bool
	// Lag is how long the oldest receipt sealed waited.
	Lag time.Duration
}

// beginRole is beginTx as another role than memax_v2.
func (l *Ledger) beginRole(ctx context.Context, role string, scope Scope, opts pgx.TxOptions) (pgx.Tx, string, error) {
	tx, err := l.pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, "", fmt.Errorf("ledger: begin: %w", err)
	}
	var loginRole string
	err = tx.QueryRow(ctx,
		`SELECT current_setting('role'),
		        set_config('role', $1, true),
		        set_config('app.space_ids', $2, true),
		        set_config('app.tenant_ids', $3, true),
		        set_config('lock_timeout', $4, true)`,
		role, uuidArray(scope.SpaceIDs()), uuidArray(scope.TenantIDs()),
		fmt.Sprintf("%dms", l.lockTimeout.Milliseconds())).Scan(&loginRole, nil, nil, nil, nil)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, "", fmt.Errorf("ledger: switch to %s: %w", role, err)
	}
	if loginRole == role || loginRole == DBRole {
		_ = tx.Rollback(ctx)
		return nil, "", fmt.Errorf("ledger: the connection already runs as %s; connect as the app's login role", loginRole)
	}
	return tx, loginRole, nil
}

const sealReceiptSelect = `
	SELECT id, seq, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id, agent, via,
	       assurance, session_ref, source ->> 'kind', source ->> 'ref', reason, reason_salt, reason_sha256,
	       occurred_at, recorded_at, stream_id, stream_version, txid::text
	  FROM v2.receipts`

func scanChainReceipt(row pgx.CollectableRow) (sealedReceipt, error) {
	var s sealedReceipt
	r := &s.Receipt
	var version int32
	err := row.Scan(&r.ID, &r.Seq, &r.TenantID, &r.SpaceID, &r.ObjectKind, &r.ObjectID, &r.ObjectRef, &r.Action,
		&r.ActorKind, &r.ActorID, &r.Agent, &r.Via, &r.Assurance, &r.SessionRef, &r.SourceKind, &r.SourceRef,
		&r.Reason, &r.ReasonSalt, &r.ReasonSHA256, &r.OccurredAt, &r.RecordedAt, &r.StreamID, &version, &s.txid)
	r.StreamVersion = int64(version)
	return s, err
}

// sealedReceipt is a receipt with its txid, for the watermark.
type sealedReceipt struct {
	receiptchain.Receipt
	txid string
}

// loadHead locks (or reads) a space's chain head, creating it at genesis.
func loadHead(ctx context.Context, tx pgx.Tx, sp SpaceGrant, lock bool) (*ChainHead, error) {
	genesis := receiptchain.Genesis(sp.SpaceID)
	if lock {
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.receipt_chain_heads (space_id, tenant_id, last_txid, last_seq, position, head_sha256, checkpoints)
			VALUES ($1, $2, '0', 0, 0, $3, 0)
			ON CONFLICT (space_id) DO NOTHING`, sp.SpaceID, sp.TenantID, genesis[:]); err != nil {
			return nil, fmt.Errorf("ledger: chain head: %w", err)
		}
	}
	q := `SELECT h.space_id, h.last_txid::text, h.last_seq, h.position, h.head_sha256, h.checkpoints, h.sealed_at,
	             h.verified_at, h.verified_position, h.verify_problems,
	             (SELECT c.last_receipt_id FROM v2.receipt_checkpoints c WHERE c.space_id = h.space_id AND c.number = h.checkpoints)
	        FROM v2.receipt_chain_heads h WHERE h.space_id = $1`
	if lock {
		q += ` FOR UPDATE OF h`
	}
	var h ChainHead
	var head []byte
	err := tx.QueryRow(ctx, q, sp.SpaceID).Scan(&h.SpaceID, &h.lastTxid, &h.LastSeq, &h.Position, &head, &h.Checkpoints,
		&h.SealedAt, &h.VerifiedAt, &h.VerifiedPosition, &h.VerifyProblems, &h.LastReceiptID)
	if errNoRows(err) {
		return &ChainHead{SpaceID: sp.SpaceID, lastTxid: "0", head: genesis, Head: hex.EncodeToString(genesis[:])}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: chain head: %w", err)
	}
	copy(h.head[:], head)
	h.Head = hex.EncodeToString(head)
	return &h, nil
}

// SealSpace seals the space's receipts after its head and below the
// watermark, at most limit of them, into one checkpoint signed by signer
// (unsigned when signer is nil).
func (l *Ledger) SealSpace(ctx context.Context, spaceID uuid.UUID, signer receiptchain.Signer, limit int) (SealResult, error) {
	if l == nil {
		return SealResult{}, ErrDisabled
	}
	if limit < 1 {
		limit = 1
	}
	out := SealResult{SpaceID: spaceID}
	scope, err := l.SpaceScope(ctx, spaceID)
	if err != nil {
		return out, err
	}
	sp := scope.Spaces[0]
	tx, _, err := l.beginRole(ctx, SealerRole, scope, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	head, err := loadHead(ctx, tx, sp, true)
	if err != nil {
		return out, mapDBError(err)
	}
	// A new statement, so a new snapshot: it sees every receipt committed
	// before the head lock was granted, and xmin is this snapshot's.
	rows, err := tx.Query(ctx, sealReceiptSelect+`
		 WHERE space_id = $1 AND (txid, seq) > ($2::xid8, $3)
		   AND txid < pg_snapshot_xmin(pg_current_snapshot())
		 ORDER BY txid, seq
		 LIMIT $4`, spaceID, head.lastTxid, head.LastSeq, limit+1)
	if err != nil {
		return out, fmt.Errorf("ledger: seal: read receipts: %w", err)
	}
	batch, err := pgx.CollectRows(rows, scanChainReceipt)
	if err != nil {
		return out, fmt.Errorf("ledger: seal: read receipts: %w", err)
	}
	if len(batch) == 0 {
		return out, tx.Commit(ctx)
	}
	if len(batch) > limit {
		batch, out.More = batch[:limit], true
	}
	receipts := make([]receiptchain.Receipt, len(batch))
	oldest := batch[0].RecordedAt
	for i, r := range batch {
		receipts[i] = r.Receipt
		if r.RecordedAt.Before(oldest) {
			oldest = r.RecordedAt
		}
	}
	now := l.now()
	c := receiptchain.Seal(sp.SpaceID, sp.TenantID, head.Checkpoints+1, head.Position+1, head.head, receipts, now)
	if signer != nil {
		c.KeyID = signer.KeyID()
		if c.Signature, err = signer.Sign(ctx, c.Statement()); err != nil {
			return out, fmt.Errorf("ledger: seal: sign checkpoint %d: %w", c.Number, err)
		}
	}
	last := batch[len(batch)-1]
	id := newID()
	var keyID *string
	if c.KeyID != "" {
		keyID = &c.KeyID
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO v2.receipt_checkpoints (id, tenant_id, space_id, number, position_from, position_to, receipts,
		                                    first_receipt_id, last_receipt_id, last_seq, last_txid, prev_sha256,
		                                    chain_sha256, merkle_root, format, key_id, signature, sealed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::xid8, $12, $13, $14, $15, $16, $17, $18)`,
		id, sp.TenantID, sp.SpaceID, c.Number, c.PositionFrom, c.PositionTo, len(receipts),
		c.FirstReceiptID, c.LastReceiptID, c.LastSeq, last.txid, c.Prev[:], c.Chain[:], c.MerkleRoot[:],
		receiptchain.Format, keyID, nilIfEmpty(c.Signature), c.SealedAt); err != nil {
		return out, mapDBError(fmt.Errorf("ledger: seal: write checkpoint: %w", err))
	}
	if _, err := tx.Exec(ctx, `
		UPDATE v2.receipt_chain_heads
		   SET last_txid = $2::xid8, last_seq = $3, position = $4, head_sha256 = $5, checkpoints = $6,
		       sealed_at = $7, updated_at = now()
		 WHERE space_id = $1`, sp.SpaceID, last.txid, last.Seq, c.PositionTo, c.Chain[:], c.Number, c.SealedAt); err != nil {
		return out, mapDBError(fmt.Errorf("ledger: seal: move the head: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return out, mapDBError(fmt.Errorf("ledger: seal: commit: %w", err))
	}
	cp := toCheckpoint(id, c)
	out.Sealed, out.Checkpoint, out.Lag = len(receipts), &cp, now.Sub(oldest)
	return out, nil
}

func nilIfEmpty(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return b
}

func toCheckpoint(id uuid.UUID, c receiptchain.Checkpoint) Checkpoint {
	out := Checkpoint{ID: id, SpaceID: c.SpaceID, TenantID: c.TenantID, Number: c.Number,
		PositionFrom: c.PositionFrom, PositionTo: c.PositionTo, Receipts: int(c.Receipts()),
		FirstReceiptID: c.FirstReceiptID, LastReceiptID: c.LastReceiptID, LastSeq: c.LastSeq,
		PrevSHA256: hex.EncodeToString(c.Prev[:]), ChainSHA256: hex.EncodeToString(c.Chain[:]),
		MerkleRoot: hex.EncodeToString(c.MerkleRoot[:]), Format: receiptchain.Format,
		Signed: len(c.Signature) > 0, KeyID: c.KeyID, SealedAt: c.SealedAt, chain: c}
	if out.Signed {
		out.Signature = base64.StdEncoding.EncodeToString(c.Signature)
	}
	return out
}

const checkpointSelect = `
	SELECT id, tenant_id, space_id, number, position_from, position_to, first_receipt_id, last_receipt_id, last_seq,
	       prev_sha256, chain_sha256, merkle_root, COALESCE(key_id, ''), signature, sealed_at, object_key, uploaded_at,
	       upload_attempts
	  FROM v2.receipt_checkpoints`

func scanCheckpoint(row pgx.CollectableRow) (Checkpoint, error) {
	var c receiptchain.Checkpoint
	var id uuid.UUID
	var prev, chain, root []byte
	var objectKey *string
	var uploadedAt *time.Time
	var attempts int
	if err := row.Scan(&id, &c.TenantID, &c.SpaceID, &c.Number, &c.PositionFrom, &c.PositionTo, &c.FirstReceiptID,
		&c.LastReceiptID, &c.LastSeq, &prev, &chain, &root, &c.KeyID, &c.Signature, &c.SealedAt, &objectKey,
		&uploadedAt, &attempts); err != nil {
		return Checkpoint{}, err
	}
	copy(c.Prev[:], prev)
	copy(c.Chain[:], chain)
	copy(c.MerkleRoot[:], root)
	c.SealedAt = c.SealedAt.UTC()
	out := toCheckpoint(id, c)
	out.StoredAt, out.uploadAttempts = uploadedAt, attempts
	if objectKey != nil {
		out.objectKey = *objectKey
	}
	return out, nil
}

// CheckpointQuery pages through a space's checkpoints, newest first.
type CheckpointQuery struct {
	SpaceID uuid.UUID
	Cursor  string
	Limit   int
}

// CheckpointPage is one page of checkpoints, with the head and how many
// receipts wait to be sealed (counted up to MaxUnsealedCount).
type CheckpointPage struct {
	Checkpoints []Checkpoint
	Head        *ChainHead
	Unsealed    int
	NextCursor  string
	HasMore     bool
}

// MaxUnsealedCount bounds the unsealed count a page reports.
const MaxUnsealedCount = 10000

// ListCheckpoints reads a space's checkpoints in scope (as memax_v2).
func (l *Ledger) ListCheckpoints(ctx context.Context, scope Scope, q CheckpointQuery) (CheckpointPage, error) {
	if l == nil {
		return CheckpointPage{}, ErrDisabled
	}
	g, ok := scope.Grant(q.SpaceID)
	if !ok {
		return CheckpointPage{}, ErrNotFound
	}
	limit, err := pageLimit(q.Limit)
	if err != nil {
		return CheckpointPage{}, err
	}
	before, err := decodeCursor(q.Cursor, 'k')
	if err != nil {
		return CheckpointPage{}, err
	}
	var page CheckpointPage
	err = l.Read(ctx, scope.Narrow(q.SpaceID), func(tx pgx.Tx) error {
		head, err := loadHead(ctx, tx, g, false)
		if err != nil {
			return err
		}
		if head.Checkpoints > 0 || head.Position > 0 {
			page.Head = head
		}
		rows, err := tx.Query(ctx, checkpointSelect+`
			 WHERE space_id = $1 AND ($2::bigint = 0 OR number < $2)
			 ORDER BY number DESC
			 LIMIT $3`, q.SpaceID, before, limit+1)
		if err != nil {
			return fmt.Errorf("ledger: list checkpoints: %w", err)
		}
		if page.Checkpoints, err = pgx.CollectRows(rows, scanCheckpoint); err != nil {
			return fmt.Errorf("ledger: list checkpoints: %w", err)
		}
		return tx.QueryRow(ctx, `
			SELECT count(*) FROM (
			    SELECT 1 FROM v2.receipts
			     WHERE space_id = $1 AND (txid, seq) > ($2::xid8, $3)
			     LIMIT $4) u`, q.SpaceID, head.lastTxid, head.LastSeq, MaxUnsealedCount).Scan(&page.Unsealed)
	})
	if err != nil {
		return CheckpointPage{}, err
	}
	if len(page.Checkpoints) > limit {
		page.Checkpoints = page.Checkpoints[:limit]
		page.HasMore = true
		page.NextCursor = encodeCursor('k', page.Checkpoints[limit-1].Number)
	}
	return page, nil
}

// SealSweep finds the spaces with receipts after the sweep's cursor whose
// transactions have all ended (v2.unsealed_spaces, at most limit receipts
// a run), and queues a seal job for each in the same transaction as it
// moves the cursor, so a failed enqueue moves nothing. It returns the
// spaces queued.
func (l *Ledger) SealSweep(ctx context.Context, limit int, jobs Jobs) ([]uuid.UUID, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	tx, loginRole, err := l.beginRole(ctx, SealerRole, Scope{}, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT space_id FROM v2.unsealed_spaces($1)`, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger: seal sweep: %w", err)
	}
	spaces, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("ledger: seal sweep: %w", err)
	}
	if len(spaces) > 0 && jobs != nil {
		slices.SortFunc(spaces, func(a, b uuid.UUID) int { return compareUUID(a, b) })
		params := make([]river.InsertManyParams, len(spaces))
		for i, s := range spaces {
			params[i] = river.InsertManyParams{Args: SealSpaceArgs{SpaceID: s}}
		}
		// River's tables are the login role's (jobs.go); the insert is the
		// transaction's last statement.
		if _, err := tx.Exec(ctx, `SELECT set_config('role', $1, true)`, loginRole); err != nil {
			return nil, fmt.Errorf("ledger: seal sweep: %w", err)
		}
		if _, err := jobs.InsertManyTx(ctx, tx, params); err != nil {
			return nil, fmt.Errorf("ledger: seal sweep: enqueue %d seal job(s): %w", len(params), err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, mapDBError(fmt.Errorf("ledger: seal sweep: commit: %w", err))
	}
	return spaces, nil
}

// FinishSeal ends a seal job without losing a receipt to the sweep's
// deduplication. seal_space is unique over running jobs, so a sweep that
// passes a new receipt of this space while its job runs queues nothing: the
// running job must take it. FinishSeal takes the sweep's cursor FOR SHARE
// (the sweep takes it FOR UPDATE), checks whether the cursor has passed a
// receipt of the space the head hasn't reached, and if not, runs finish
// (the job completing itself, as the login role) in the same transaction.
// Either the sweep moved first, and FinishSeal sees its receipts and
// reports pending (seal again), or FinishSeal commits first, and the
// sweep's insert finds the job completed and queues a new one.
func (l *Ledger) FinishSeal(ctx context.Context, spaceID uuid.UUID, finish Finisher) (pending bool, err error) {
	if l == nil {
		return false, ErrDisabled
	}
	scope, err := l.SpaceScope(ctx, spaceID)
	if err != nil {
		return false, err
	}
	tx, loginRole, err := l.beginRole(ctx, SealerRole, scope, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// The cursor's policy admits the sealer while app.sweep says so.
	if _, err := tx.Exec(ctx, `SELECT set_config('app.sweep', 'unsealed_spaces', true)`); err != nil {
		return false, fmt.Errorf("ledger: finish seal: %w", err)
	}
	var cursorTxid string
	var cursorSeq int64
	if err := tx.QueryRow(ctx, `SELECT last_txid::text, last_seq FROM v2.receipt_seal_cursor WHERE id FOR SHARE`).
		Scan(&cursorTxid, &cursorSeq); err != nil {
		return false, fmt.Errorf("ledger: finish seal: read the cursor: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.sweep', '', true)`); err != nil {
		return false, fmt.Errorf("ledger: finish seal: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM v2.receipts r
		      LEFT JOIN v2.receipt_chain_heads h ON h.space_id = r.space_id
		     WHERE r.space_id = $1
		       AND (r.txid, r.seq) > (COALESCE(h.last_txid, '0'::xid8), COALESCE(h.last_seq, 0))
		       AND (r.txid, r.seq) <= ($2::xid8, $3))`, spaceID, cursorTxid, cursorSeq).Scan(&pending); err != nil {
		return false, fmt.Errorf("ledger: finish seal: %w", err)
	}
	if pending {
		return true, nil
	}
	if finish != nil {
		if _, err := tx.Exec(ctx, `SELECT set_config('role', $1, true)`, loginRole); err != nil {
			return false, fmt.Errorf("ledger: finish seal: %w", err)
		}
		if err := finish(ctx, tx); err != nil {
			return false, fmt.Errorf("ledger: finish seal: %w", err)
		}
	}
	return false, tx.Commit(ctx)
}

func compareUUID(a, b uuid.UUID) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ReceiptSpaces lists every space with receipts (v2.receipt_spaces), for
// the nightly verifier.
func (l *Ledger) ReceiptSpaces(ctx context.Context) ([]uuid.UUID, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	tx, _, err := l.beginRole(ctx, SealerRole, Scope{}, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT space_id FROM v2.receipt_spaces() ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("ledger: receipt spaces: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("ledger: receipt spaces: %w", err)
	}
	return out, tx.Commit(ctx)
}

// Verification is what VerifySpace found.
type Verification struct {
	receiptchain.Report
	// Head is the chain head verified against (nil: never sealed).
	Head *ChainHead
	// Unsealed receipts are after the head; OldestUnsealed is when the
	// oldest of them was written (how far the sealer is behind).
	Unsealed       int
	OldestUnsealed *time.Time
}

// verifyBatch is how many receipts VerifySpace reads at a time.
const verifyBatch = 5000

// VerifySpace recomputes the space's chain from genesis in one snapshot
// and checks it against every checkpoint (receiptchain.Verifier), with
// keys. It writes nothing; RecordVerification stores the outcome.
func (l *Ledger) VerifySpace(ctx context.Context, spaceID uuid.UUID, keys receiptchain.Keyring) (*Verification, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	scope, err := l.SpaceScope(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	sp := scope.Spaces[0]
	tx, _, err := l.beginRole(ctx, SealerRole, scope, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out := &Verification{}
	head, err := loadHead(ctx, tx, sp, false)
	if err != nil {
		return nil, err
	}
	if head.Checkpoints > 0 {
		out.Head = head
	}
	rows, err := tx.Query(ctx, checkpointSelect+` WHERE space_id = $1 ORDER BY number`, spaceID)
	if err != nil {
		return nil, fmt.Errorf("ledger: verify: checkpoints: %w", err)
	}
	cps, err := pgx.CollectRows(rows, scanCheckpoint)
	if err != nil {
		return nil, fmt.Errorf("ledger: verify: checkpoints: %w", err)
	}
	chain := make([]receiptchain.Checkpoint, len(cps))
	for i, c := range cps {
		chain[i] = c.chain
	}
	v := receiptchain.NewVerifier(spaceID, keys, chain)
	rows, err = tx.Query(ctx, `SELECT DISTINCT object_id FROM v2.receipts WHERE space_id = $1 AND action = 'forgot'`, spaceID)
	if err != nil {
		return nil, fmt.Errorf("ledger: verify: forgotten: %w", err)
	}
	forgotten, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("ledger: verify: forgotten: %w", err)
	}
	for _, f := range forgotten {
		v.Forgotten(f)
	}
	afterTxid, afterSeq := "0", int64(0)
	for {
		rows, err := tx.Query(ctx, sealReceiptSelect+`
			 WHERE space_id = $1 AND (txid, seq) > ($2::xid8, $3) AND (txid, seq) <= ($4::xid8, $5)
			 ORDER BY txid, seq
			 LIMIT $6`, spaceID, afterTxid, afterSeq, head.lastTxid, head.LastSeq, verifyBatch)
		if err != nil {
			return nil, fmt.Errorf("ledger: verify: receipts: %w", err)
		}
		batch, err := pgx.CollectRows(rows, scanChainReceipt)
		if err != nil {
			return nil, fmt.Errorf("ledger: verify: receipts: %w", err)
		}
		for _, r := range batch {
			v.Add(r.Receipt)
		}
		if len(batch) < verifyBatch {
			break
		}
		last := batch[len(batch)-1]
		afterTxid, afterSeq = last.txid, last.Seq
	}
	out.Report = v.Finish()
	if err := tx.QueryRow(ctx, `
		SELECT count(*), min(recorded_at) FROM v2.receipts
		 WHERE space_id = $1 AND (txid, seq) > ($2::xid8, $3)`, spaceID, head.lastTxid, head.LastSeq).
		Scan(&out.Unsealed, &out.OldestUnsealed); err != nil {
		return nil, fmt.Errorf("ledger: verify: unsealed: %w", err)
	}
	return out, tx.Commit(ctx)
}

// RecordVerification stores a verification's outcome on the space's head.
func (l *Ledger) RecordVerification(ctx context.Context, spaceID uuid.UUID, v *Verification) error {
	if l == nil {
		return ErrDisabled
	}
	if v == nil || v.Head == nil {
		return nil
	}
	scope, err := l.SpaceScope(ctx, spaceID)
	if err != nil {
		return err
	}
	tx, _, err := l.beginRole(ctx, SealerRole, scope, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		UPDATE v2.receipt_chain_heads
		   SET verified_at = $2, verified_position = $3, verify_problems = $4, updated_at = now()
		 WHERE space_id = $1`, spaceID, l.now(), v.Receipts, len(v.Problems)); err != nil {
		return mapDBError(fmt.Errorf("ledger: record verification: %w", err))
	}
	return tx.Commit(ctx)
}

// PendingUploads lists a space's checkpoints not yet in object storage,
// oldest first, that failed fewer than maxAttempts times (0: any).
func (l *Ledger) PendingUploads(ctx context.Context, spaceID uuid.UUID, limit, maxAttempts int) ([]Checkpoint, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	scope, err := l.SpaceScope(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	tx, _, err := l.beginRole(ctx, SealerRole, scope, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, checkpointSelect+`
		 WHERE space_id = $1 AND object_key IS NULL AND ($2 = 0 OR upload_attempts < $2)
		 ORDER BY number
		 LIMIT $3`, spaceID, maxAttempts, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger: pending uploads: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanCheckpoint)
	if err != nil {
		return nil, fmt.Errorf("ledger: pending uploads: %w", err)
	}
	return out, tx.Commit(ctx)
}

// MarkUploaded records where a checkpoint's copy is; MarkUploadFailed
// counts a failed attempt (key is then "").
func (l *Ledger) MarkUploaded(ctx context.Context, spaceID, checkpointID uuid.UUID, key string, failure error) error {
	if l == nil {
		return ErrDisabled
	}
	scope, err := l.SpaceScope(ctx, spaceID)
	if err != nil {
		return err
	}
	tx, _, err := l.beginRole(ctx, SealerRole, scope, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if failure == nil {
		_, err = tx.Exec(ctx, `
			UPDATE v2.receipt_checkpoints SET object_key = $3, uploaded_at = now(), upload_error = NULL
			 WHERE space_id = $1 AND id = $2 AND object_key IS NULL`, spaceID, checkpointID, key)
	} else {
		msg := failure.Error()
		if len(msg) > 1000 {
			msg = msg[:1000]
		}
		_, err = tx.Exec(ctx, `
			UPDATE v2.receipt_checkpoints SET upload_attempts = upload_attempts + 1, upload_error = $3
			 WHERE space_id = $1 AND id = $2 AND object_key IS NULL`, spaceID, checkpointID, msg)
	}
	if err != nil {
		return mapDBError(fmt.Errorf("ledger: mark checkpoint upload: %w", err))
	}
	return tx.Commit(ctx)
}

// CheckpointKey is where a checkpoint's copy goes in object storage.
func CheckpointKey(spaceID uuid.UUID, number int64) string {
	return "v2/spaces/" + spaceID.String() + "/receipts/checkpoints/" + fmt.Sprintf("%012d", number) + ".json"
}

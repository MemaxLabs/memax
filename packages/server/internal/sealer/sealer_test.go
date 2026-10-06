package sealer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore/mockobjectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
	"github.com/MemaxLabs/memax/packages/server/internal/sealer"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type fixture struct {
	t      *testing.T
	pool   *pgxpool.Pool
	l      *ledger.Ledger
	owner  uuid.UUID
	space  uuid.UUID
	tenant uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	_, pool := testdb.Acquire(t)
	f := &fixture{t: t, pool: pool, l: ledger.New(pool, ledger.WithLogger(quiet)), owner: uuid.New(), space: uuid.New()}
	f.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'zz')`, f.owner, f.owner.String()+"@seal.test")
	f.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, 'memax-v2', $2, 'team', $3, 'project')`,
		f.space, f.space.String(), f.owner)
	f.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, f.space, f.owner)
	f.tenant = f.owner
	return f
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (f *fixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		f.t.Fatalf("count: %v", err)
	}
	return n
}

func (f *fixture) scope() ledger.Scope {
	f.t.Helper()
	s, err := f.l.UserScope(context.Background(), f.owner)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

// remember keeps a memory: one receipt.
func (f *fixture) remember(statement string) *ledger.Memory {
	f.t.Helper()
	res, err := f.l.Apply(context.Background(), &ledger.Remember{
		Meta:      ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: f.owner}, Scope: f.scope(), Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()},
		NewMemory: ledger.NewMemory{SpaceID: f.space, Statement: statement, Section: ledger.SectionConventions}})
	if err != nil || res.Memory == nil {
		f.t.Fatalf("remember: %v %+v", err, res.Policy)
	}
	return res.Memory
}

// rejected proposes and rejects with a reason: two receipts, the second
// with a reason.
func (f *fixture) rejected(statement, reason string) *ledger.Memory {
	f.t.Helper()
	ctx := context.Background()
	meta := func(a ledger.Actor) ledger.Meta {
		return ledger.Meta{Actor: a, Scope: f.scope(), Via: policy.ViaWeb, IdempotencyKey: uuid.NewString(), Reason: reason}
	}
	agent := ledger.Actor{Kind: policy.ActorAgent, ID: uuid.New(), Agent: "codex", Autonomy: policy.AutonomyPropose}
	res, err := f.l.Apply(ctx, &ledger.Propose{Meta: ledger.Meta{Actor: agent, Scope: f.scope(), Via: policy.ViaMCP, IdempotencyKey: uuid.NewString()},
		NewMemory: ledger.NewMemory{SpaceID: f.space, Statement: statement, Section: ledger.SectionConventions}})
	if err != nil || res.Memory == nil {
		f.t.Fatalf("propose: %v %+v", err, res.Policy)
	}
	if _, err := f.l.Apply(ctx, &ledger.Reject{Meta: meta(ledger.Actor{Kind: policy.ActorPerson, ID: f.owner}), Memory: res.Memory.Ref}); err != nil {
		f.t.Fatalf("reject: %v", err)
	}
	return res.Memory
}

// tamper changes receipts as the database owner would, past the
// append-only guard: what the verifier exists to catch.
func (f *fixture) tamper(sql string, args ...any) {
	f.t.Helper()
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, q := range []string{`ALTER TABLE v2.receipts DISABLE TRIGGER receipts_guard`, sql, `ALTER TABLE v2.receipts ENABLE TRIGGER receipts_guard`} {
		a := args
		if q != sql {
			a = nil
		}
		if _, err := tx.Exec(ctx, q, a...); err != nil {
			f.t.Fatalf("tamper %q: %v", q, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		f.t.Fatal(err)
	}
}

func keyed(t *testing.T, seedByte byte) (*receiptchain.Ed25519Signer, receiptchain.Keyring) {
	t.Helper()
	s, err := receiptchain.NewEd25519Signer(bytes.Repeat([]byte{seedByte}, 32))
	if err != nil {
		t.Fatal(err)
	}
	keys := receiptchain.Keyring{}
	keys.Add(s.Public())
	return s, keys
}

func (f *fixture) sealer(signer receiptchain.Signer, keys receiptchain.Keyring, store objectstore.Store) *sealer.Sealer {
	return sealer.New(f.l, sealer.Config{Signer: signer, Keys: keys, Store: store, Batch: 3, Log: quiet})
}

// sealAll seals until every committed receipt of the space is sealed. The
// watermark is the cluster's: a write transaction in flight anywhere (a
// parallel test's, in another database) holds sealing back until it ends,
// so this retries rather than expecting one run to catch up.
func (f *fixture) sealAll(s *sealer.Sealer) int {
	f.t.Helper()
	total := 0
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		n, err := s.SealSpace(context.Background(), f.space)
		total += n
		if err != nil && !errors.Is(err, sealer.ErrMore) {
			f.t.Fatalf("seal: %v", err)
		}
		if err == nil && f.count(`
			SELECT count(*) FROM v2.receipts r
			 WHERE r.space_id = $1 AND NOT EXISTS (
			     SELECT 1 FROM v2.receipt_chain_heads h
			      WHERE h.space_id = r.space_id AND (r.txid, r.seq) <= (h.last_txid, h.last_seq))`, f.space) == 0 {
			return total
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatal("sealing never caught up")
	return 0
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within 20 s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *fixture) verify(s *sealer.Sealer) *ledger.Verification {
	f.t.Helper()
	v, err := s.Verify(context.Background(), f.space)
	if err != nil {
		f.t.Fatalf("verify: %v", err)
	}
	return v
}

func kinds(v *ledger.Verification) []string {
	var out []string
	for _, p := range v.Problems {
		out = append(out, string(p.Kind))
	}
	return out
}

// Receipts are chained into signed checkpoints of at most Batch receipts,
// copied to object storage, and verify from genesis; a run with nothing
// new writes nothing.
func TestSealAndVerify(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	signer, keys := keyed(t, 1)
	store := mockobjectstore.New()
	s := f.sealer(signer, keys, store)
	for i := range 7 {
		f.remember("Fact " + string(rune('A'+i)))
	}
	if n := f.sealAll(s); n != 7 {
		t.Fatalf("sealed %d, want 7", n)
	}
	page, err := f.l.ListCheckpoints(ctx, f.scope(), ledger.CheckpointQuery{SpaceID: f.space})
	if err != nil {
		t.Fatal(err)
	}
	// At most Batch (3) receipts a checkpoint, so at least three of them;
	// where each run stops depends on the watermark.
	n := len(page.Checkpoints)
	if n < 3 || page.Head == nil || page.Head.Position != 7 || page.Head.Checkpoints != int64(n) || page.Unsealed != 0 {
		t.Fatalf("checkpoints = %+v, head %+v, unsealed %d", page.Checkpoints, page.Head, page.Unsealed)
	}
	for i, c := range page.Checkpoints {
		if c.Receipts > 3 || c.Number != int64(n-i) || (i+1 < n && c.PositionFrom != page.Checkpoints[i+1].PositionTo+1) {
			t.Errorf("checkpoint %d = %+v", c.Number, c)
		}
	}
	newest := page.Checkpoints[0]
	if newest.PositionTo != 7 || !newest.Signed || newest.KeyID != signer.KeyID() ||
		newest.StoredAt == nil || newest.ChainSHA256 != page.Head.Head {
		t.Errorf("newest checkpoint = %+v", newest)
	}
	// The copy in object storage is the signed statement and its fields.
	obj, err := store.Get(ctx, ledger.CheckpointKey(f.space, 1))
	if err != nil {
		t.Fatalf("no copy of checkpoint 1: %v", err)
	}
	var doc struct {
		Format     string `json:"format"`
		Number     int64  `json:"number"`
		MerkleRoot string `json:"merkle_root"`
		Signature  string `json:"signature"`
		Statement  string `json:"statement"`
	}
	if err := json.NewDecoder(obj.Body).Decode(&doc); err != nil || doc.Format != "memax.checkpoint.v1" || doc.Number != 1 ||
		doc.Signature == "" || doc.Statement == "" || doc.MerkleRoot != page.Checkpoints[n-1].MerkleRoot {
		t.Errorf("copy = %+v (%v)", doc, err)
	}

	v := f.verify(s)
	if !v.OK() || v.Receipts != 7 || v.Signed != n || v.Unsealed != 0 {
		t.Fatalf("verify = %+v", v)
	}
	// Nothing new: nothing written.
	if sealed := f.sealAll(s); sealed != 0 || f.count(`SELECT count(*) FROM v2.receipt_checkpoints`) != n {
		t.Errorf("an idle run sealed %d", sealed)
	}
	// More receipts continue the chain.
	f.remember("Fact H")
	f.sealAll(s)
	if v := f.verify(s); !v.OK() || v.Receipts != 8 || v.Checkpoints != n+1 {
		t.Errorf("after one more: %+v", v)
	}
	var problems, position int
	if err := f.pool.QueryRow(ctx, `SELECT verify_problems, verified_position FROM v2.receipt_chain_heads WHERE space_id = $1`, f.space).
		Scan(&problems, &position); err != nil || problems != 0 || position != 8 {
		t.Errorf("recorded verification: %d problems at %d (%v)", problems, position, err)
	}
}

// The race the watermark exists for: a transaction that wrote a receipt
// first but commits last. While it is open, neither its receipt nor any
// receipt after it is sealed (the sweep's cursor doesn't pass it either);
// once it commits, both are sealed in (txid, seq) order, exactly once.
func TestInFlightReceiptIsSealedInItsPlace(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	signer, keys := keyed(t, 2)
	s := f.sealer(signer, keys, nil)
	jobs, err := river.NewClient(riverpgxv5.New(f.pool), &river.Config{Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	before := f.remember("Committed before")

	// The long transaction: its receipt gets a txid now, and it stays open.
	long, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = long.Rollback(ctx) }()
	inflight := uuid.Must(uuid.NewV7())
	if _, err := long.Exec(ctx, `SELECT set_config('role', 'memax_v2', true), set_config('app.space_ids', $1, true), set_config('app.tenant_ids', $2, true)`,
		"{"+f.space.String()+"}", "{"+f.tenant.String()+"}"); err != nil {
		t.Fatal(err)
	}
	if _, err := long.Exec(ctx, `
		INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id, via, occurred_at, stream_id, stream_version)
		VALUES ($1, $2, $3, 'space', $3, 'memax-v2', 'configured', 'person', $4, 'web', now(), $1, 1)`,
		inflight, f.tenant, f.space, f.owner); err != nil {
		t.Fatal(err)
	}
	// Later transactions commit meanwhile.
	after := f.remember("Committed after, while the first is open")

	sweep := func() int {
		spaces, err := f.l.SealSweep(ctx, 100, jobs)
		if err != nil {
			t.Fatalf("sweep: %v", err)
		}
		return len(spaces)
	}
	seal := func() {
		if _, err := s.SealSpace(ctx, f.space); err != nil && !errors.Is(err, sealer.ErrMore) {
			t.Fatalf("seal: %v", err)
		}
	}
	var sealed []uuid.UUID
	sealedOrder := func() []uuid.UUID {
		rows, err := f.pool.Query(ctx, `
			SELECT r.id FROM v2.receipts r, v2.receipt_chain_heads h
			 WHERE r.space_id = $1 AND h.space_id = r.space_id AND (r.txid, r.seq) <= (h.last_txid, h.last_seq)
			 ORDER BY r.txid, r.seq`, f.space)
		if err != nil {
			t.Fatal(err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}
	// The receipt before the open transaction is swept and sealed (as soon
	// as no older write transaction anywhere in the cluster is in flight:
	// the watermark is the cluster's) ...
	eventually(t, "the sweep finds the space", func() bool { return sweep() == 1 })
	eventually(t, "the receipt before it is sealed", func() bool { seal(); return len(sealedOrder()) > 0 })
	// ... and however often the sealer and the sweep run, nothing at or
	// after the open transaction is sealed or swept.
	for range 5 {
		seal()
		if n := sweep(); n != 0 {
			t.Errorf("a sweep with a receipt in flight found %d spaces", n)
		}
	}
	sealed = sealedOrder()
	if len(sealed) != 1 || sealed[0] != before.LastReceiptID {
		t.Fatalf("with a receipt in flight, sealed %v; want only the receipt before it", sealed)
	}
	if n := f.count(`SELECT count(*) FROM river_job WHERE kind = 'seal_space'`); n != 1 {
		t.Errorf("seal jobs = %d", n)
	}

	if err := long.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the sweep after the commit", func() bool { return sweep() == 1 })
	f.sealAll(s)
	sealed = sealedOrder()
	if len(sealed) != 3 || sealed[0] != before.LastReceiptID || sealed[1] != inflight || sealed[2] != after.LastReceiptID {
		t.Fatalf("sealed %v; want before, the late commit, after", sealed)
	}
	v := f.verify(s)
	if !v.OK() || v.Receipts != 3 || v.Checkpoints != 2 {
		t.Errorf("verify = %+v", v)
	}
	if n := f.count(`SELECT sum(receipts) FROM v2.receipt_checkpoints WHERE space_id = $1`, f.space); n != 3 {
		t.Errorf("checkpoints cover %d receipts, want each of the 3 once", n)
	}
}

// Two sealers of one space at once, with receipts arriving all along:
// they take turns on the head, so every receipt is sealed once, the
// positions are contiguous, and the chain verifies. River's uniqueness
// keeps a second job for the space from being queued at all.
func TestTwoSealersSealEachReceiptOnce(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	signer, keys := keyed(t, 3)
	a, b := f.sealer(signer, keys, nil), f.sealer(signer, keys, nil)
	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		for i := range 40 {
			f.remember("Concurrent fact " + uuid.NewString()[:8] + string(rune('a'+i%26)))
		}
	}()
	seal := func(s *sealer.Sealer) {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			if _, err := s.SealSpace(ctx, f.space); err != nil && !errors.Is(err, sealer.ErrMore) {
				t.Errorf("seal: %v", err)
				return
			}
		}
	}
	wg.Add(2)
	go seal(a)
	go seal(b)
	wg.Wait()
	f.sealAll(a)
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1`, f.space); n != 40 {
		t.Fatalf("receipts = %d", n)
	}
	if n := f.count(`SELECT sum(receipts) FROM v2.receipt_checkpoints WHERE space_id = $1`, f.space); n != 40 {
		t.Errorf("checkpoints cover %d receipts, want 40", n)
	}
	if n := f.count(`
		SELECT count(*) FROM v2.receipt_checkpoints c
		  JOIN v2.receipt_checkpoints p ON p.space_id = c.space_id AND p.number = c.number - 1
		 WHERE c.space_id = $1 AND (c.position_from <> p.position_to + 1 OR c.prev_sha256 <> p.chain_sha256)`, f.space); n != 0 {
		t.Errorf("%d checkpoints don't continue the one before", n)
	}
	if v := f.verify(a); !v.OK() || v.Receipts != 40 {
		t.Errorf("verify = %+v", v.Problems)
	}

	jobs, err := river.NewClient(riverpgxv5.New(f.pool), &river.Config{Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	first, err := jobs.Insert(ctx, ledger.SealSpaceArgs{SpaceID: f.space}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := jobs.Insert(ctx, ledger.SealSpaceArgs{SpaceID: f.space}, nil)
	if err != nil || !second.UniqueSkippedAsDuplicate || second.Job.ID != first.Job.ID {
		t.Errorf("a second seal job for the space: %+v %v", second, err)
	}
}

// Forget redacts reasons in the transaction of its forgot receipt. The
// sealed leaves committed to the reason's salted hash, not the reason, so
// the chain verifies after the redaction, and the salt is gone with the
// words. A reason that disappears without a Forget is caught.
func TestRedactionKeepsTheChainValid(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	signer, keys := keyed(t, 4)
	s := f.sealer(signer, keys, nil)
	gone := f.rejected("A proposal to forget", "duplicate of the deploy policy")
	kept := f.rejected("Another proposal", "wrong: we deploy from main")
	f.sealAll(s)
	if v := f.verify(s); !v.OK() {
		t.Fatalf("before Forget: %v", v.Problems)
	}

	// Forget: a forgot receipt and the redaction, in one transaction.
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('role', 'memax_v2', true), set_config('app.space_ids', $1, true), set_config('app.tenant_ids', $2, true)`,
		"{"+f.space.String()+"}", "{"+f.tenant.String()+"}"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id, via, occurred_at, stream_id, stream_version)
		VALUES ($1, $2, $3, 'memory', $4, $5, 'forgot', 'person', $6, 'web', now(), $4, 3)`,
		uuid.Must(uuid.NewV7()), f.tenant, f.space, gone.ID, gone.Ref, f.owner); err != nil {
		t.Fatal(err)
	}
	var redacted int
	if err := tx.QueryRow(ctx, `SELECT v2.redact_receipt_reasons($1)`, gone.ID).Scan(&redacted); err != nil || redacted != 1 {
		t.Fatalf("redact: %d %v", redacted, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var salts, commitments int
	if err := f.pool.QueryRow(ctx, `SELECT count(reason_salt), count(reason_sha256) FROM v2.receipts WHERE object_id = $1 AND action = 'rejected'`, gone.ID).
		Scan(&salts, &commitments); err != nil || salts != 0 || commitments != 1 {
		t.Errorf("after redaction: %d salts, %d commitments (%v); want the salt purged and the commitment kept", salts, commitments, err)
	}
	f.sealAll(s)
	if v := f.verify(s); !v.OK() || v.Receipts != 5 {
		t.Fatalf("after Forget: %+v", v.Problems)
	}

	// The same done by hand, with no forgot receipt, is caught.
	f.tamper(`UPDATE v2.receipts SET reason = NULL, reason_salt = NULL WHERE object_id = $1 AND action = 'rejected'`, kept.ID)
	if v := f.verify(s); strings.Join(kinds(v), ",") != "redaction" {
		t.Errorf("a reason removed by hand: %v", v.Problems)
	}
}

// Tampering with a sealed receipt, or with a checkpoint, is caught by the
// verifier, logged and metered; signatures survive a key rotation.
func TestVerifierCatchesTamperingAndKeysRotate(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	oldKey, oldKeys := keyed(t, 5)
	newKey, _ := keyed(t, 6)
	both := receiptchain.Keyring{}
	both.Add(oldKey.Public())
	both.Add(newKey.Public())
	for i := range 4 {
		f.remember("Sealed with the old key " + string(rune('A'+i)))
	}
	f.sealAll(f.sealer(oldKey, oldKeys, nil))
	f.remember("Sealed with the new key")
	rotated := f.sealer(newKey, both, nil)
	f.sealAll(rotated)
	byOld := f.count(`SELECT count(*) FROM v2.receipt_checkpoints WHERE space_id = $1 AND key_id = $2`, f.space, oldKey.KeyID())
	byNew := f.count(`SELECT count(*) FROM v2.receipt_checkpoints WHERE space_id = $1 AND key_id = $2`, f.space, newKey.KeyID())
	if v := f.verify(rotated); !v.OK() || v.Signed != byOld+byNew || byOld < 2 || byNew != 1 {
		t.Fatalf("after rotation, with both keys: %v (old %d, new %d)", v.Problems, byOld, byNew)
	}
	onlyNew := receiptchain.Keyring{}
	onlyNew.Add(newKey.Public())
	if v := f.verify(f.sealer(newKey, onlyNew, nil)); len(v.Problems) != byOld || v.Problems[0].Kind != receiptchain.ProblemSignature {
		t.Errorf("without the retired key: %v", v.Problems)
	}

	var victim uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT id FROM v2.receipts WHERE space_id = $1 ORDER BY seq LIMIT 1 OFFSET 2`, f.space).Scan(&victim); err != nil {
		t.Fatal(err)
	}
	// Single-threaded, so seq order is chain order: the victim is at
	// position 3.
	covering := f.count(`SELECT number FROM v2.receipt_checkpoints WHERE space_id = $1 AND 3 BETWEEN position_from AND position_to`, f.space)
	f.tamper(`UPDATE v2.receipts SET actor_kind = 'memax', actor_id = NULL WHERE id = $1`, victim)
	v := f.verify(rotated)
	if got := strings.Join(kinds(v), ","); got != "merkle,chain" || v.Problems[0].Checkpoint != int64(covering) {
		t.Errorf("a rewritten actor: %s %v", got, v.Problems)
	}
	if rotated.Problems.Load() == 0 {
		t.Error("the mismatch wasn't counted")
	}
	var problems int
	if err := f.pool.QueryRow(ctx, `SELECT verify_problems FROM v2.receipt_chain_heads WHERE space_id = $1`, f.space).Scan(&problems); err != nil || problems != 2 {
		t.Errorf("recorded problems = %d (%v)", problems, err)
	}

	// A checkpoint rewritten in the database fails its signature.
	f.tamper(`UPDATE v2.receipts SET actor_kind = 'person', actor_id = $2 WHERE id = $1`, victim, f.owner)
	if v := f.verify(rotated); !v.OK() {
		t.Fatalf("restored: %v", v.Problems)
	}
	f.exec(`UPDATE v2.receipt_checkpoints SET merkle_root = sha256('forged') WHERE space_id = $1 AND number = 2`, f.space)
	if got := strings.Join(kinds(f.verify(rotated)), ","); got != "signature,merkle" {
		t.Errorf("a forged checkpoint: %s", got)
	}
}

// Uploads are best-effort: a store that fails leaves the checkpoint in the
// database, counted, and a later run (or the nightly verifier) stores it.
func TestUploadsRetry(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	signer, keys := keyed(t, 7)
	store := &flaky{Store: mockobjectstore.New(), fail: true}
	s := f.sealer(signer, keys, store)
	f.remember("One")
	f.sealAll(s)
	if s.UploadFailures.Load() != 1 || f.count(`SELECT upload_attempts FROM v2.receipt_checkpoints WHERE space_id = $1`, f.space) != 1 {
		t.Fatalf("failures = %d", s.UploadFailures.Load())
	}
	store.fail = false
	if _, err := s.Verify(ctx, f.space); err != nil {
		t.Fatal(err)
	}
	if s.Uploaded.Load() != 1 || f.count(`SELECT count(*) FROM v2.receipt_checkpoints WHERE space_id = $1 AND object_key IS NOT NULL`, f.space) != 1 {
		t.Errorf("after recovering: uploaded %d", s.Uploaded.Load())
	}
}

type flaky struct {
	*mockobjectstore.Store
	fail bool
}

func (s *flaky) Put(ctx context.Context, in objectstore.PutInput) error {
	if s.fail {
		return errors.New("R2 is down")
	}
	return s.Store.Put(ctx, in)
}

// Seals are written only by the sealer's role: memax_v2, which every
// request runs as, can read them but not forge or move them.
func TestOnlyTheSealerWritesSeals(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	signer, keys := keyed(t, 8)
	f.remember("One")
	f.sealAll(f.sealer(signer, keys, nil))
	for name, sql := range map[string]string{
		"move the head":       `UPDATE v2.receipt_chain_heads SET position = 0`,
		"forge a checkpoint":  `UPDATE v2.receipt_checkpoints SET merkle_root = sha256('x')`,
		"delete a checkpoint": `DELETE FROM v2.receipt_checkpoints`,
		"move the cursor":     `UPDATE v2.receipt_seal_cursor SET last_seq = 0`,
	} {
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, `SELECT set_config('role', 'memax_v2', true), set_config('app.space_ids', $1, true)`, "{"+f.space.String()+"}")
		if err == nil {
			_, err = tx.Exec(ctx, sql)
		}
		_ = tx.Rollback(ctx)
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("memax_v2 may %s: %v", name, err)
		}
	}
	// It reads them in scope.
	page, err := f.l.ListCheckpoints(ctx, f.scope(), ledger.CheckpointQuery{SpaceID: f.space})
	if err != nil || len(page.Checkpoints) != 1 {
		t.Errorf("list as memax_v2: %v %v", page.Checkpoints, err)
	}
	// And the sealer itself never sees another space's receipts.
	other := uuid.New()
	f.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, 'other', $2, 'team', $3, 'project')`, other, other.String(), f.owner)
	if res, err := f.l.SealSpace(ctx, other, signer, 10); err != nil || res.Sealed != 0 {
		t.Errorf("sealing a space with no receipts: %+v %v", res, err)
	}
}

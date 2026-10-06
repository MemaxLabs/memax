// Package ledger is the one write path for the V2 record (plan 25 §5.3).
//
// Every change to a memory is a Command applied by Ledger.Apply in a
// single Postgres transaction:
//
//  1. switch to the memax_v2 role and set the transaction's scope
//     (app.space_ids, app.tenant_ids), so row-level security holds even
//     when the app logged in as a superuser or a BYPASSRLS role;
//  2. load the memory with SELECT … FOR UPDATE (by uuid or display ID);
//  3. claim the command's idempotency key, or return the original result
//     if the key was already applied;
//  4. ask policy.Decide what the write may do (apply, propose, ask the
//     person in the agent to confirm, or refuse);
//  5. check the lifecycle transition (package lifecycle);
//  6. write the receipt(s), then the projections (memories,
//     memory_versions, sources, links), allocating display IDs from the
//     tenant's counter in the same transaction.
//
// The database backs each step: a deferred constraint trigger refuses
// the commit if a projection row has no receipt from the same
// transaction, receipts are append-only and content-free, a trigger
// re-checks the lifecycle transition, and RLS policies hide every row
// outside the scope (migrations 026–029 and 031).
//
// # The Brief, targets and compiles
//
// ReviseBrief writes a Brief version (B-). ConfigureTarget adds or
// changes a compile target, RequestCompile asks for a fresh compile, and
// the compile coordinator (internal/compile) records each run with
// RecordCompile, as Memax. RecordDelivery acknowledges a run on disk,
// RecordObservation reports a hand edit, and ResolveDrift pulls it back
// as proposals, overwrites it or stops the target (plan 25 §5.7).
//
// Every command that changes what compiles (a kept memory, new words for
// one, a Brief version, a target's settings) bumps the generation of the
// space's targets and inserts their compile_target jobs with River's
// InsertTx, in the command's transaction; see jobs.go for the role switch
// that needs.
//
// # Postgres only, on purpose
//
// V1's store has an InMemoryStore twin of every PostgresStore method,
// about 3.8k lines kept in parity by hand. The ledger has no such twin
// and must not grow one. Its guarantees (receipt-or-refused, row-level
// security, the uniqueness that makes idempotency and display IDs safe
// under concurrency, deferred constraint checks) live in the database;
// an in-memory fake would have to reimplement each of them, and a test
// passing against the fake would prove nothing about the real thing.
// Tests use real Postgres through internal/testdb, which clones a
// migrated template per test in a few milliseconds.
//
// # Words never leave their tables
//
// A memory's statement lives only in memory_versions.statement, and a
// source's quote only in sources.quote, so Forget can purge the words
// without rewriting history. Receipts, idempotency records and log lines
// carry IDs, refs and policy codes, never the text.
package ledger

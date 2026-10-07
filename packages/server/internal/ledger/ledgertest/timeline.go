// Package ledgertest builds V2 records with exact timelines for tests of
// what is computed from them (the product metrics, migration 053).
//
// The ledger stamps every receipt with the database's clock, so a record
// written through commands can't say "this person connected Codex at
// 14:00 on a Monday in August". A Timeline writes the same rows those
// commands write (the same actions, object kinds and actors), at the times
// it is given, in one transaction with session_replication_role = replica,
// which skips the triggers that stamp, guard and check them. That needs a
// superuser, which the test database's login role is. The ledger's own
// tests prove the real commands write rows of these shapes.
package ledgertest

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Timeline writes a record at chosen times. Every method fails the test on
// an error; Commit writes it all.
type Timeline struct {
	t       testing.TB
	ctx     context.Context
	tx      pgx.Tx
	tenants map[uuid.UUID]uuid.UUID // space → tenant
	seq     int64
}

// NewTimeline opens the transaction a Timeline writes in.
func NewTimeline(t testing.TB, pool *pgxpool.Pool) *Timeline {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("ledgertest: begin: %v", err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("ledgertest: replica mode (the test database's role must be a superuser): %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return &Timeline{t: t, ctx: ctx, tx: tx, tenants: map[uuid.UUID]uuid.UUID{}}
}

// Commit writes the timeline.
func (tl *Timeline) Commit() {
	tl.t.Helper()
	if err := tl.tx.Commit(tl.ctx); err != nil {
		tl.t.Fatalf("ledgertest: commit: %v", err)
	}
}

func (tl *Timeline) exec(sql string, args ...any) {
	tl.t.Helper()
	if _, err := tl.tx.Exec(tl.ctx, sql, args...); err != nil {
		tl.t.Fatalf("ledgertest: %s: %v", sql, err)
	}
}

func (tl *Timeline) next() int64 {
	tl.seq++
	return tl.seq
}

// Person creates an account at created.
func (tl *Timeline) Person(name string, created time.Time) uuid.UUID {
	tl.t.Helper()
	id := uuid.New()
	tl.exec(`INSERT INTO public.users (id, email, name, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)`,
		id, fmt.Sprintf("%s-%s@people.test", name, id.String()[:8]), name, created)
	return id
}

// Staff gives the person an admin role.
func (tl *Timeline) Staff(person uuid.UUID) {
	tl.t.Helper()
	tl.exec(`INSERT INTO public.admin_roles (user_id, role) VALUES ($1, 'super_admin')`, person)
}

func (tl *Timeline) hub(owner uuid.UUID, kind string, created time.Time, v2 *time.Time) uuid.UUID {
	tl.t.Helper()
	id := uuid.New()
	hubType := "team"
	if kind == "personal" {
		hubType = "personal"
	}
	tl.exec(`
		INSERT INTO public.hubs (id, name, slug, hub_type, owner_id, space_kind, tenant_id, v2_enabled_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $5, $7, $8, $8)`,
		id, kind+" space", id.String(), hubType, owner, kind, v2, created)
	tl.exec(`INSERT INTO public.hub_members (hub_id, user_id, role, joined_at) VALUES ($1, $2, 'owner', $3)`, id, owner, created)
	tl.tenants[id] = owner
	return id
}

// V1Hub is the personal hub V1 creates at signup, still on V1.
func (tl *Timeline) V1Hub(owner uuid.UUID, created time.Time) uuid.UUID {
	tl.t.Helper()
	return tl.hub(owner, "personal", created, nil)
}

// Space is a project space created on the V2 record (POST /v2/spaces).
func (tl *Timeline) Space(owner uuid.UUID, at time.Time) uuid.UUID {
	tl.t.Helper()
	return tl.hub(owner, "project", at, &at)
}

// V1Memory is a memory the person wrote in V1; a seed is one of V1's
// onboarding seeds.
func (tl *Timeline) V1Memory(owner, hub uuid.UUID, at time.Time, seed bool) {
	tl.t.Helper()
	var kind *string
	if seed {
		s := "onboarding-seed"
		kind = &s
	}
	tl.exec(`INSERT INTO public.memories (hub_id, owner_id, title, content, source_kind, created_at, updated_at)
	         VALUES ($1, $2, 'V1 memory', 'Words written in V1.', $3, $4, $4)`, hub, owner, kind, at)
}

// Join makes person a member of space at.
func (tl *Timeline) Join(space, person uuid.UUID, at time.Time) {
	tl.t.Helper()
	tl.exec(`INSERT INTO public.hub_members (hub_id, user_id, role, joined_at) VALUES ($1, $2, 'contributor', $3)`, space, person, at)
}

// receipt writes one receipt as the ledger would.
func (tl *Timeline) receipt(space uuid.UUID, objectKind string, object uuid.UUID, ref, action, actorKind string,
	actor *uuid.UUID, agent *string, via string, version int, at time.Time) uuid.UUID {
	tl.t.Helper()
	id := uuid.New()
	tl.exec(`
		INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id,
		                         agent, via, occurred_at, recorded_at, stream_id, stream_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12, $5, $13)`,
		id, tl.tenants[space], space, objectKind, object, ref, action, actorKind, actor, agent, via, at, version)
	return id
}

// Switch moves a space to the V2 record at, as its owner asked: the
// `switched` receipt and hubs.v2_enabled_at.
func (tl *Timeline) Switch(space, by uuid.UUID, at time.Time) {
	tl.t.Helper()
	tl.receipt(space, "space", space, "space", "switched", "person", &by, nil, "web", 1, at)
	tl.exec(`UPDATE public.hubs SET v2_enabled_at = $2 WHERE id = $1`, space, at)
}

// Connect creates an agent connection for person in space at: by the
// person consenting to the agent's OAuth sign-in, or (byMemax) by a switch
// or backfill connecting a V1 credential.
func (tl *Timeline) Connect(person, space uuid.UUID, agent string, at time.Time, byMemax bool) uuid.UUID {
	tl.t.Helper()
	id := uuid.New()
	actorKind, actor, via := "person", &person, "mcp"
	if byMemax {
		actorKind, actor, via = "memax", nil, "system"
	}
	rc := tl.receipt(space, "agent", id, agent, "connected", actorKind, actor, nil, via, 1, at)
	tl.exec(`
		INSERT INTO v2.agent_connections (id, tenant_id, person_id, agent, display_name, surface, credential_kind,
		                                  credential_id, state, stream_version, created_receipt_id, last_receipt_id,
		                                  created_at, updated_at)
		VALUES ($1, $2, $2, $3, $3, 'cli', 'oauth_grant', $4, 'active', 1, $5, $5, $6, $6)`,
		id, person, agent, uuid.New(), rc, at)
	tl.exec(`
		INSERT INTO v2.agent_connection_spaces (connection_id, space_id, tenant_id, person_id, autonomy,
		                                        created_receipt_id, last_receipt_id)
		VALUES ($1, $2, $3, $4, 'propose', $5, $5)`, id, space, tl.tenants[space], person, rc)
	return id
}

// Disconnect disconnects a connection at, as its person.
func (tl *Timeline) Disconnect(person, space, conn uuid.UUID, at time.Time) {
	tl.t.Helper()
	rc := tl.receipt(space, "agent", conn, "agent", "disconnected", "person", &person, nil, "web", 2, at)
	tl.exec(`UPDATE v2.agent_connections SET state = 'disconnected', disconnected_at = $2, stream_version = 2,
	                last_receipt_id = $3, updated_at = $2 WHERE id = $1`, conn, at, rc)
}

// Import is an upload by `memax init` (origin init) at.
func (tl *Timeline) Import(person, space uuid.UUID, at time.Time) {
	tl.t.Helper()
	key := uuid.NewString()
	sum := sha256.Sum256([]byte(key))
	tl.exec(`
		INSERT INTO v2.imports (id, tenant_id, space_id, actor_kind, actor_id, idempotency_key, request_sha256,
		                        items_total, uploaded_at, origin, created_at)
		VALUES ($1, $2, $3, 'person', $4, $5, $6, 3, $7, 'init', $7)`,
		uuid.New(), tl.tenants[space], space, person, key, sum[:], at)
}

// Deliver is a compile run written to disk by the person's CLI or daemon
// at: its `delivered` receipt (the `compiled` one is Memax's).
func (tl *Timeline) Deliver(person, space uuid.UUID, at time.Time) {
	tl.t.Helper()
	run := uuid.New()
	ref := fmt.Sprintf("C-%04d", tl.next())
	tl.receipt(space, "compile", run, ref, "compiled", "memax", nil, nil, "system", 1, at.Add(-time.Second))
	tl.receipt(space, "compile", run, ref, "delivered", "person", &person, nil, "cli", 2, at)
}

// Keep is a memory the person remembers and keeps at once, at.
func (tl *Timeline) Keep(person, space uuid.UUID, at time.Time) uuid.UUID {
	tl.t.Helper()
	m := uuid.New()
	tl.receipt(space, "memory", m, fmt.Sprintf("M-%04d", tl.next()), "kept", "person", &person, nil, "web", 1, at)
	return m
}

// AgentKeep is a memory a Write-level agent keeps at once, at: not a
// proposal, and not a person keeping.
func (tl *Timeline) AgentKeep(conn, space uuid.UUID, agent string, at time.Time) {
	tl.t.Helper()
	tl.receipt(space, "memory", uuid.New(), fmt.Sprintf("M-%04d", tl.next()), "kept", "agent", &conn, &agent, "mcp", 1, at)
}

// Propose is a proposal made by the person through an import, at.
func (tl *Timeline) Propose(person, space uuid.UUID, at time.Time) uuid.UUID {
	tl.t.Helper()
	m := uuid.New()
	tl.receipt(space, "memory", m, fmt.Sprintf("M-%04d", tl.next()), "proposed", "person", &person, nil, "import", 1, at)
	return m
}

// Decide is what became of a proposal at: kept, rejected or forgot by a
// person, or merged (folded) by Memax's judge.
func (tl *Timeline) Decide(person, space, memory uuid.UUID, action string, version int, at time.Time) {
	tl.t.Helper()
	if action == "merged" {
		tl.receipt(space, "memory", memory, "M-0000", action, "memax", nil, nil, "system", version, at)
		return
	}
	tl.receipt(space, "memory", memory, "M-0000", action, "person", &person, nil, "web", version, at)
}

// Read is a read by one of the person's agent connections at.
func (tl *Timeline) Read(person, space, conn uuid.UUID, agent string, at time.Time) {
	tl.t.Helper()
	tl.read(person, space, &conn, "agent", agent, "recall", at)
}

// CompileLoad is a session-start hook's report, by the person, that their
// agent loaded a compile at.
func (tl *Timeline) CompileLoad(person, space uuid.UUID, agent string, at time.Time) {
	tl.t.Helper()
	tl.read(person, space, nil, "person", agent, "compile_load", at)
}

func (tl *Timeline) read(person, space uuid.UUID, conn *uuid.UUID, readerKind, agent, kind string, at time.Time) {
	tl.t.Helper()
	tl.exec(`SELECT v2.ensure_reads_partitions($1, 0)`, at)
	var compile *uuid.UUID
	if kind == "compile_load" {
		c := uuid.New()
		compile = &c
	}
	tl.exec(`
		INSERT INTO v2.reads (id, tenant_id, space_id, seq, reader_kind, connection_id, person_id, agent, kind, via,
		                      compile_id, memories, read_at, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'mcp', $10, 1, $11, $11)`,
		uuid.New(), tl.tenants[space], space, tl.next(), readerKind, conn, person, agent, kind, compile, at)
}

package ledger_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// readOf is a read of memories in a space by an agent connection.
func readOf(m *ledger.Memory, conn uuid.UUID, person uuid.UUID, agent string, kind ledger.ReadKind, at time.Time, memories ...uuid.UUID) ledger.ReadEvent {
	return ledger.ReadEvent{SpaceID: m.SpaceID, TenantID: m.TenantID, Reader: ledger.ReaderAgent, ConnectionID: conn,
		PersonID: person, Agent: agent, Kind: kind, Via: policy.ViaMCP, SessionRef: "s-1", Memories: memories, At: at}
}

func (f *fixture) record(events ...ledger.ReadEvent) ledger.ReadStats {
	f.t.Helper()
	st, err := f.l.RecordReads(context.Background(), events)
	if err != nil {
		f.t.Fatalf("RecordReads: %v", err)
	}
	return st
}

// A read holds ids, refs and counts: never a memory's words or the query.
// Its R- number comes from the tenant's counter, and its rollups count it
// per memory.
func TestRecordReadsHoldsNoText(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	m1 := f.remember(zz, space, "Deploys go through the staging lighthouse first.")
	m2 := f.remember(zz, space, "Migrations are numbered by migrate:new, never by hand.")
	conn := uuid.New()
	now := time.Now()
	st := f.record(
		readOf(m1, conn, zz, "claude-code", ledger.ReadRecall, now, m1.ID, m2.ID),
		readOf(m1, conn, zz, "claude-code", ledger.ReadGet, now, m1.ID),
	)
	if st.Written != 2 || st.Skipped != 0 {
		t.Fatalf("stats = %+v", st)
	}

	page, err := f.l.ListReads(context.Background(), f.scope(zz), ledger.ReadQuery{SpaceID: space})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Reads) != 2 || page.Week != 2 {
		t.Fatalf("reads = %+v (week %d)", page.Reads, page.Week)
	}
	get, recall := page.Reads[0], page.Reads[1]
	if !strings.HasPrefix(recall.Ref, "R-") || recall.Ref == get.Ref || recall.Kind != ledger.ReadRecall ||
		recall.Memories != 2 || len(recall.MemoryRefs) != 2 || recall.MemoryRefs[0] != m1.Ref || recall.Agent != "claude-code" ||
		recall.ConnectionID == nil || *recall.ConnectionID != conn || recall.SessionRef != "s-1" {
		t.Errorf("recall read = %+v", recall)
	}

	// No row of reads or rollups carries the words, and no column could:
	// the text columns are the vocabulary, the agent and the session ref.
	for _, table := range []string{"v2.reads", "v2.read_rollups"} {
		n := f.count(`SELECT count(*) FROM ` + table + ` r WHERE row_to_json(r)::text ILIKE '%lighthouse%' OR row_to_json(r)::text ILIKE '%migrate:new%'`)
		if n != 0 {
			t.Errorf("%s holds memory text in %d rows", table, n)
		}
	}
	rows, err := f.pool.Query(context.Background(), `
		SELECT table_name || '.' || column_name FROM information_schema.columns
		 WHERE table_schema = 'v2' AND table_name IN ('reads', 'read_rollups') AND data_type = 'text'
		 ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	want := "read_rollups.agent read_rollups.reader_kind read_rollups.subject_kind reads.agent reads.kind reads.reader_kind reads.session_ref reads.via"
	if got := strings.Join(cols, " "); got != want {
		t.Errorf("text columns = %s, want %s (a new text column must not be able to hold memory or query text)", got, want)
	}

	// The rollups: m1 twice, m2 once, both by this connection today.
	if n := f.count(`SELECT reads FROM v2.read_rollups WHERE subject_id = $1`, m1.ID); n != 2 {
		t.Errorf("m1 rollup = %d, want 2", n)
	}
	reads, err := f.l.GetMemoryReads(context.Background(), f.scope(zz), m2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reads.Reads != 1 || reads.Reads7d != 1 || reads.Agents != 1 || len(reads.Readers) != 1 ||
		*reads.Readers[0].ConnectionID != conn || reads.LastReadAt == nil {
		t.Errorf("m2 reads = %+v", reads)
	}
}

// Malformed reads are skipped one by one, and a read of a space whose
// tenant doesn't match is skipped, never failing the batch.
func TestRecordReadsSkipsBadEvents(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	m := f.remember(zz, space, "A memory.")
	conn := uuid.New()
	good := readOf(m, conn, zz, "codex", ledger.ReadSearch, time.Now(), m.ID)
	noConn := good
	noConn.ConnectionID = uuid.Nil
	badKind := good
	badKind.Kind = "browse"
	wrongTenant := good
	wrongTenant.TenantID = uuid.New()
	loadWithoutCompile := good
	loadWithoutCompile.Kind, loadWithoutCompile.CompileRef = ledger.ReadCompileLoad, "C-9999"
	unknownAgent := good
	unknownAgent.Agent = "Claude Desktop"
	st := f.record(good, noConn, badKind, wrongTenant, loadWithoutCompile, unknownAgent)
	if st.Written != 2 || st.Skipped != 4 {
		t.Fatalf("stats = %+v, want 2 written and 4 skipped", st)
	}
	if n := f.count(`SELECT count(*) FROM v2.reads WHERE agent = 'claude'`); n != 1 {
		t.Errorf("an agent named the V1 way is recorded as its kind: %d", n)
	}
}

// Every month gets its partition the first time a read lands in it (and
// the month after it with it); retention drops whole partitions and old
// rollup days, and refuses to cut into what fading needs.
func TestReadsPartitionsAndRetention(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	m := f.remember(zz, space, "A memory.")
	conn := uuid.New()
	partitions := func() []string {
		rows, err := f.pool.Query(ctx, `
			SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
			 WHERE i.inhparent = 'v2.reads'::regclass ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		names, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return names
	}
	has := func(names []string, at time.Time) bool {
		want := "reads_" + at.UTC().Format("2006_01")
		for _, n := range names {
			if n == want {
				return true
			}
		}
		return false
	}

	now := time.Now().UTC()
	later := now.AddDate(0, 7, 0)
	old := now.AddDate(0, -15, 0)
	if has(partitions(), later) || has(partitions(), old) {
		t.Fatalf("partitions exist before any read: %v", partitions())
	}
	f.record(readOf(m, conn, zz, "codex", ledger.ReadGet, later, m.ID), readOf(m, conn, zz, "codex", ledger.ReadGet, old, m.ID))
	names := partitions()
	if !has(names, later) || !has(names, later.AddDate(0, 1, 0)) || !has(names, old) {
		t.Fatalf("partitions after the reads: %v", names)
	}
	if n := f.count(`SELECT count(*) FROM v2.reads`); n != 2 {
		t.Fatalf("reads = %d", n)
	}

	if _, err := f.l.PruneReads(ctx, now.AddDate(0, 0, -30)); err == nil {
		t.Error("pruning 30 days back was allowed; fading needs 60")
	}
	dropped, err := f.l.PruneReads(ctx, now.AddDate(0, -ledger.ReadRetentionMonths, 0))
	if err != nil {
		t.Fatal(err)
	}
	names = partitions()
	// The old month and the month after it (created with it) are past
	// retention; this month and the future ones stay.
	if dropped != 2 || has(names, old) || has(names, old.AddDate(0, 1, 0)) || !has(names, now) || !has(names, later) {
		t.Errorf("after pruning: dropped %d, %v", dropped, names)
	}
	if n := f.count(`SELECT count(*) FROM v2.reads`); n != 1 {
		t.Errorf("reads after pruning = %d, want 1", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.read_rollups WHERE day < $1::date`, now.AddDate(0, -ledger.ReadRetentionMonths, 0)); n != 0 {
		t.Errorf("rollups past retention = %d", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.read_rollups`); n != 1 {
		t.Errorf("rollups = %d, want the future day's", n)
	}
}

// Reads and rollups are space-scoped under forced RLS, and append-only for
// the app role: no UPDATE of a read, no DELETE of either.
func TestReadsAreIsolatedAndAppendOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	mine := f.space(zz, policy.SpaceProject, "mine")
	other := f.space(zz, policy.SpaceProject, "other")
	theirs := f.space(jy, policy.SpaceProject, "theirs")
	mm := f.remember(zz, mine, "Mine.")
	mo := f.remember(zz, other, "Other.")
	mt := f.remember(jy, theirs, "Theirs.")
	now := time.Now()
	f.record(readOf(mm, uuid.New(), zz, "codex", ledger.ReadGet, now, mm.ID),
		readOf(mo, uuid.New(), zz, "codex", ledger.ReadGet, now, mo.ID),
		readOf(mt, uuid.New(), jy, "codex", ledger.ReadGet, now, mt.ID))

	// Through the ledger: another tenant's space is not found, and a scope
	// narrowed to one space sees only that space's reads.
	if _, err := f.l.ListReads(ctx, f.scope(zz), ledger.ReadQuery{SpaceID: theirs}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another tenant's reads: %v", err)
	}
	if _, err := f.l.GetMemoryReads(ctx, f.scope(zz), mt.ID); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another tenant's memory reads: %v", err)
	}
	// In the database: memax_v2 with one space in scope sees that space only.
	var reads, rollups int
	err := f.rawAs(mine, mm.TenantID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v2.reads`).Scan(&reads); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM v2.read_rollups`).Scan(&rollups)
	})
	if err != nil || reads != 1 || rollups != 1 {
		t.Errorf("one space in scope sees %d reads and %d rollups (%v), want 1 and 1", reads, rollups, err)
	}
	err = f.asV2(nil, nil, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM v2.reads`).Scan(&reads)
	})
	if err != nil || reads != 0 {
		t.Errorf("no scope sees %d reads (%v)", reads, err)
	}
	// Even a direct read of a partition shows nothing.
	part := "v2.reads_" + now.UTC().Format("2006_01")
	err = f.rawAs(mine, mm.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT count(*) FROM `+part)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("a partition read directly: %v", err)
	}
	// A read can't be inserted into a space outside the scope.
	err = f.rawAs(mine, mm.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO v2.reads (id, tenant_id, space_id, seq, reader_kind, person_id, kind, via, memories, read_at)
			VALUES (gen_random_uuid(), $1, $2, 1, 'person', $3, 'get', 'api', 0, now())`, mt.TenantID, theirs, jy)
		return err
	})
	if err == nil {
		t.Error("a read was written into another tenant's space")
	}
	for name, sql := range map[string]string{
		"update a read":     `UPDATE v2.reads SET memories = 9`,
		"delete a read":     `DELETE FROM v2.reads`,
		"delete a rollup":   `DELETE FROM v2.read_rollups`,
		"truncate reads":    `TRUNCATE v2.reads`,
		"move a rollup day": `UPDATE v2.read_rollups SET day = day - 1`,
	} {
		err := f.rawAs(mine, mm.TenantID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s: %v, want permission denied", name, err)
		}
	}
}

// A compile load counts as a read of every fact in the compile, and is how
// a file's loads become observed: fading's question (when was each memory
// last read, and is it in a file whose loads Memax can't see) answers from
// the rollups in one query.
func TestReadStatusForFading(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	m1 := f.remember(zz, space, "Fact one.")
	m2 := f.remember(zz, space, "Fact two.")
	m3 := f.remember(zz, space, "Fact three, read directly.")
	m4 := f.remember(zz, space, "Fact four, in no file and never read.")
	f.brief(zz, space, 0, demoSections(m1.Ref, m2.Ref, m3.Ref, m4.Ref))
	agents := f.target(zz, space, ledger.TargetAgentsMD)
	run := f.compiled(agents, "# AGENTS.md\n- Fact one.\n- Fact two.\n", m1.Ref, m2.Ref)

	status := func() map[string]ledger.MemoryReadStatus {
		t.Helper()
		got, err := f.l.ReadStatus(ctx, f.scope(zz), space)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]ledger.MemoryReadStatus{}
		for _, s := range got {
			out[s.Ref] = s
		}
		if len(out) != 4 {
			t.Fatalf("status for %d memories, want 4", len(out))
		}
		return out
	}
	// Before any load: AGENTS.md is a local file nobody reported loading,
	// so its facts are exempt from fading; m3 and m4 are not, and unread.
	s := status()
	if !s[m1.Ref].UnobservedTarget || !s[m2.Ref].UnobservedTarget || s[m3.Ref].UnobservedTarget || s[m4.Ref].UnobservedTarget {
		t.Errorf("before a load: %+v", s)
	}
	cutoff := time.Now().Add(-60 * 24 * time.Hour)
	if s[m1.Ref].Unread(cutoff) || !s[m3.Ref].Unread(cutoff) || !s[m4.Ref].Unread(cutoff) {
		t.Errorf("fading before a load would fade: m1 %v m3 %v m4 %v", s[m1.Ref].Unread(cutoff), s[m3.Ref].Unread(cutoff), s[m4.Ref].Unread(cutoff))
	}

	// m3 is read directly; then Claude Code's session start reports the
	// compile it loaded, as the person's CLI.
	direct := time.Now().Add(-time.Hour)
	f.record(readOf(m3, uuid.New(), zz, "codex", ledger.ReadRecall, direct, m3.ID))
	res, err := f.l.RecordCompileLoad(ctx, ledger.CompileLoad{Actor: person(zz), Scope: f.scope(zz), Via: policy.ViaCLI,
		SpaceID: space, Compile: run.Ref, Agent: "claude-code", SessionRef: "cc-1", IdempotencyKey: "load-1"})
	if err != nil || res.Read == nil {
		t.Fatalf("load: %v %+v", err, res.Policy)
	}
	if res.Read.Kind != ledger.ReadCompileLoad || res.Read.Compile != run.Ref || res.Read.Memories != 2 ||
		res.Read.ReaderKind != ledger.ReaderPerson || res.Read.Agent != "claude-code" || len(res.Read.MemoryRefs) != 0 {
		t.Errorf("load read = %+v", res.Read)
	}

	s = status()
	if s[m1.Ref].UnobservedTarget || s[m2.Ref].UnobservedTarget {
		t.Errorf("a reported load makes the file observed: %+v", s)
	}
	for _, ref := range []string{m1.Ref, m2.Ref} {
		if s[ref].LastReadAt == nil || s[ref].LastReadAt.Sub(res.Read.ReadAt).Abs() > time.Millisecond {
			t.Errorf("%s last read %v, want the load at %v", ref, s[ref].LastReadAt, res.Read.ReadAt)
		}
	}
	if s[m3.Ref].LastReadAt == nil || s[m3.Ref].LastReadAt.Sub(direct).Abs() > time.Millisecond || s[m4.Ref].LastReadAt != nil {
		t.Errorf("m3 %v, m4 %v", s[m3.Ref].LastReadAt, s[m4.Ref].LastReadAt)
	}
	if !s[m4.Ref].Unread(cutoff) || s[m1.Ref].Unread(cutoff) {
		t.Error("after the load, only m4 may fade")
	}

	// The memory's reads count the load (by the person's Claude Code).
	reads, err := f.l.GetMemoryReads(ctx, f.scope(zz), m1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reads.Reads != 1 || reads.Agents != 1 || reads.UnobservedTarget || len(reads.Readers) != 1 ||
		reads.Readers[0].ReaderKind != ledger.ReaderPerson || reads.Readers[0].Agent != "claude-code" {
		t.Errorf("m1 reads = %+v", reads)
	}
	// With the load gone (past the window, here deleted), the file is
	// unobserved again, and its facts are exempt again.
	f.exec(`DELETE FROM v2.reads`)
	s = status()
	if !s[m1.Ref].UnobservedTarget {
		t.Error("with the load gone, AGENTS.md is unobserved again")
	}
}

// Who may report a compile load is policy's read rule: an agent connected
// to the space (a paused one too), or a person who belongs to it. A space
// the agent isn't connected to is refused, another person's space is not
// found, and the key makes it idempotent.
func TestCompileLoadIsDecidedByPolicy(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	connected := f.space(zz, policy.SpaceProject, "connected")
	elsewhere := f.space(zz, policy.SpaceProject, "elsewhere")
	foreign := f.space(jy, policy.SpaceProject, "foreign")
	run := func(space uuid.UUID, owner uuid.UUID) *ledger.CompileRun {
		m := f.remember(owner, space, "A fact in "+space.String()[:8])
		f.brief(owner, space, 0, demoSections(m.Ref))
		return f.compiled(f.target(owner, space, ledger.TargetAgentsMD), "# AGENTS.md\n", m.Ref)
	}
	inConnected, inElsewhere, inForeign := run(connected, zz), run(elsewhere, zz), run(foreign, jy)
	conn := f.connect(zz, ledger.CredentialOAuthGrant, f.grant(zz, "codex"), ledger.AgentCodex, at(connected, policy.AutonomyRead))
	actor, scope := f.agentActor(zz, conn, policy.AutonomyWrite)
	load := func(space uuid.UUID, compile, key string) (ledger.CompileLoadResult, error) {
		return f.l.RecordCompileLoad(ctx, ledger.CompileLoad{Actor: actor, Scope: scope, Via: policy.ViaMCP,
			SpaceID: space, Compile: compile, Agent: "cursor", IdempotencyKey: key})
	}

	// Read autonomy is enough; the agent is always its own kind.
	res, err := load(connected, inConnected.Ref, "k1")
	if err != nil || res.Read == nil || res.Read.Agent != "codex" || res.Read.ReaderKind != ledger.ReaderAgent || *res.Read.ConnectionID != conn.ID {
		t.Fatalf("a read-only agent's own load: %v %+v %+v", err, res.Read, res.Policy)
	}
	// The same key again is the same read.
	again, err := load(connected, inConnected.Ref, "k1")
	if err != nil || !again.Replayed || again.Read.ID != res.Read.ID {
		t.Errorf("replay: %v %+v", err, again)
	}
	if _, err := load(connected, inConnected.ID.String(), "k1"); !errors.Is(err, ledger.ErrIdempotencyKeyReused) {
		t.Errorf("the key reused for another load: %v", err)
	}
	// Not connected to elsewhere: refused, nothing counted.
	res, err = load(elsewhere, inElsewhere.Ref, "k2")
	if err != nil || res.Read != nil || res.Policy.Code != policy.CodeAgentNotConnected {
		t.Errorf("a space it isn't connected to: %v %+v", err, res)
	}
	// Another person's space, or another space's compile, is not found.
	if _, err := load(foreign, inForeign.Ref, "k3"); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another person's space: %v", err)
	}
	if _, err := load(connected, inElsewhere.ID.String(), "k4"); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("a compile of another space, by id: %v", err)
	}
	// Paused: still reads, so still reports.
	f.apply(&ledger.PauseAgent{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Connection: conn.ID})
	actor, scope = f.agentActor(zz, conn, policy.AutonomyWrite)
	if res, err := load(connected, inConnected.Ref, "k5"); err != nil || res.Read == nil {
		t.Errorf("a paused agent's load: %v %+v", err, res.Policy)
	}
	if n := f.count(`SELECT count(*) FROM v2.reads WHERE kind = 'compile_load'`); n != 2 {
		t.Errorf("compile loads recorded = %d, want 2", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.reads WHERE space_id = $1`, elsewhere); n != 0 {
		t.Errorf("reads counted where the agent isn't connected: %d", n)
	}

	// The person reports for spaces they belong to; bad inputs are invalid.
	for name, c := range map[string]ledger.CompileLoad{
		"a day and more ago": {LoadedAt: time.Now().Add(-25 * time.Hour)},
		"in the future":      {LoadedAt: time.Now().Add(time.Hour)},
		"not a compile ref":  {Compile: "M-0001"},
		"no key":             {IdempotencyKey: " "},
	} {
		c.Actor, c.Scope, c.Via, c.SpaceID = person(zz), f.scope(zz), policy.ViaCLI, connected
		if c.Compile == "" {
			c.Compile = inConnected.Ref
		}
		if c.IdempotencyKey == "" {
			c.IdempotencyKey = uuid.NewString()
		}
		if _, err := f.l.RecordCompileLoad(ctx, c); !errors.Is(err, ledger.ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
}

// The north star counts spaces read by two or more agent kinds in the week
// (two Claude Codes are one kind), with the coverage beside it.
func TestReadMetrics(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	both := f.space(zz, policy.SpaceProject, "both")
	twins := f.space(zz, policy.SpaceProject, "twins")
	quiet := f.space(zz, policy.SpaceProject, "quiet")
	mb, mt, mq := f.remember(zz, both, "B."), f.remember(zz, twins, "T."), f.remember(zz, quiet, "Q.")
	now := time.Now().UTC()
	cc1, cc2, cx := uuid.New(), uuid.New(), uuid.New()
	f.record(
		readOf(mb, cc1, zz, "claude-code", ledger.ReadGet, now, mb.ID),
		readOf(mb, cx, zz, "codex", ledger.ReadGet, now.Add(-3*24*time.Hour), mb.ID),
		readOf(mt, cc1, zz, "claude-code", ledger.ReadGet, now, mt.ID),
		readOf(mt, cc2, zz, "claude-code", ledger.ReadGet, now, mt.ID),
		// Outside the week.
		readOf(mq, cx, zz, "codex", ledger.ReadGet, now.Add(-10*24*time.Hour), mq.ID),
		readOf(mq, cc1, zz, "claude-code", ledger.ReadGet, now.Add(-12*24*time.Hour), mq.ID),
	)
	m, err := f.l.GetReadMetrics(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if m.SpacesRead != 2 || m.SpacesTwoAgents != 1 || m.SpacesTwoConnections != 2 || m.ConnectionsReading != 3 {
		t.Errorf("metrics = %+v", m)
	}
	// The metric is computed across tenants, but only as counts: the app
	// role sees no rows of either table outside the function.
	// Not even by setting the function's sweep value itself: the policies
	// that admit rows inside v2.read_metrics apply to its owner only.
	for _, sweep := range []string{"", "read_metrics", "prune_reads"} {
		var rollups, reads, conns int
		if err := f.asV2(nil, nil, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT set_config('app.sweep', $1, true)`, sweep); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM v2.read_rollups), (SELECT count(*) FROM v2.reads),
			                                (SELECT count(*) FROM v2.agent_connections)`).Scan(&rollups, &reads, &conns)
		}); err != nil || rollups != 0 || reads != 0 || conns != 0 {
			t.Errorf("memax_v2 with app.sweep %q sees %d rollups, %d reads, %d connections (%v)", sweep, rollups, reads, conns, err)
		}
	}
}

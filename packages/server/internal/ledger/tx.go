package ledger

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Transactions over a slow network.
//
// The API runs on Fly in sjc and the database on Neon in us-west-2, so
// every round trip to Postgres costs about 24 ms (measured Oct 7, 2026). A
// ledger transaction used to open in two round trips (BEGIN, then the
// scope) and close in a third (COMMIT): a one-query read cost four, about
// 100 ms, before any real work. A scopedTx pipelines them:
//
//   - BEGIN and the scope statement go to the server in the round trip of
//     the transaction's first statement: one pgx batch, one Sync. Postgres
//     runs a pipeline's statements in order, so the scope is set inside the
//     transaction before that statement runs; and if BEGIN or the scope
//     fails, the server skips the rest of the pipeline, so nothing ever
//     runs unscoped. A transaction that sends no statement costs nothing.
//   - A read-only transaction's COMMIT (or ROLLBACK) goes out once the
//     caller has its answer, and nobody waits for it: ending a transaction
//     that wrote nothing can't change what it read, and no deferred check
//     fires. The connection returns to the pool when the server answers.
//   - Statements a command defers ride with the next statement, or with
//     COMMIT, keeping their order: the role switches around River's insert
//     (jobs.go), and writes whose result nobody reads (execDeferred).
//   - A one-statement write (meter) sends its COMMIT in that statement's
//     pipeline (commitWithNext).
//   - ReadBatch sends a read's independent statements with BEGIN, the scope
//     and COMMIT, all in one round trip.
//
// The scope statement is the same set_config(..., true) as ever, so the
// scope is SET LOCAL and ends with the transaction: a pooled connection
// (Neon's PgBouncer in transaction mode included) never carries it into
// the next one, and a transaction stays on one server connection from its
// BEGIN to its COMMIT, which is all transaction pooling needs. The same
// statement sets lock_timeout, before any statement that could wait.
// netsim.Audit checks the order on the wire (TestScopeComesFirst).

// scopeSQL switches the transaction to its role and sets its scope. The
// login role is read before the switch: Postgres evaluates a SELECT's
// target list left to right.
const scopeSQL = `SELECT current_setting('role'),
		        set_config('role', $1, true),
		        set_config('app.space_ids', $2, true),
		        set_config('app.tenant_ids', $3, true),
		        set_config('app.person_id', $4, true),
		        set_config('lock_timeout', $5, true)`

// endTimeout bounds a read-only transaction's COMMIT or ROLLBACK, sent
// after the caller returned.
const endTimeout = 5 * time.Second

// pending is a statement that goes to the server ahead of the next one, in
// its round trip.
type pending struct {
	sql  string
	args []any
	// scan reads its one row; nil discards its result.
	scan func(pgx.Row) error
	// opening marks BEGIN and the scope: a failure there fails the whole
	// transaction.
	opening bool
}

// scopedTx is a ledger transaction on a pooled connection whose opening,
// deferred statements and (for reads) end ride with other round trips. It
// implements pgx.Tx, so the read helpers and River's InsertManyTx take it
// as they took pgx's own.
type scopedTx struct {
	l        *Ledger
	conn     *pgxpool.Conn
	readOnly bool
	role     string
	// pending go out with the next statement, ahead of it.
	pending []pending
	// begun: BEGIN has gone out.
	begun bool
	// loginRole is the role the transaction began as, once its opening is
	// answered.
	loginRole string
	// failed is why the opening failed; every later statement returns it.
	failed    error
	closed    bool
	savepoint int64
	// commitNext sends COMMIT with the next statement (commitWithNext);
	// committing says it went, and commitTag and commitErr are its answer.
	commitNext, committing bool
	commitTag              pgconn.CommandTag
	commitErr              error
}

// errCommitted refuses a statement after the one that carried COMMIT.
var errCommitted = errors.New("ledger: the transaction committed with its last statement; nothing more runs in it")

var _ pgx.Tx = (*scopedTx)(nil)

// openTx acquires a connection and queues the transaction's opening:
// BEGIN, then the switch to role with the scope. Nothing is sent yet.
func (l *Ledger) openTx(ctx context.Context, role string, scope Scope, opts pgx.TxOptions) (*scopedTx, error) {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("ledger: begin: %w", err)
	}
	t := &scopedTx{l: l, conn: conn, readOnly: opts.AccessMode == pgx.ReadOnly, role: role}
	person := ""
	if scope.PersonID != uuid.Nil {
		person = scope.PersonID.String()
	}
	t.pending = []pending{
		{sql: beginSQL(opts), opening: true},
		{sql: scopeSQL, opening: true,
			args: []any{role, uuidArray(scope.SpaceIDs()), uuidArray(scope.TenantIDs()), person,
				fmt.Sprintf("%dms", l.lockTimeout.Milliseconds())},
			scan: func(r pgx.Row) error {
				if err := r.Scan(&t.loginRole, nil, nil, nil, nil, nil); err != nil {
					return fmt.Errorf("ledger: switch to %s: %w", role, err)
				}
				return l.checkLoginRole(t.loginRole, role)
			}},
	}
	return t, nil
}

// beginSQL is BEGIN with the transaction's options, as pgx writes it.
func beginSQL(o pgx.TxOptions) string {
	var b strings.Builder
	b.WriteString("begin")
	if o.IsoLevel != "" {
		b.WriteString(" isolation level " + string(o.IsoLevel))
	}
	if o.AccessMode != "" {
		b.WriteString(" " + string(o.AccessMode))
	}
	if o.DeferrableMode != "" {
		b.WriteString(" " + string(o.DeferrableMode))
	}
	return b.String()
}

// checkLoginRole refuses a connection that already runs as the ledger's
// role (a misconfigured login), and one that began as another role than
// the pool's others did: River's insert switches back to the login role
// the ledger learned from its first transaction (begin).
func (l *Ledger) checkLoginRole(got, role string) error {
	if got == role || got == DBRole {
		return fmt.Errorf("ledger: the connection already runs as %s; connect as the app's login role", got)
	}
	if known := l.loginRole.Load(); known != nil {
		if *known != got {
			return fmt.Errorf("ledger: this connection began as role %q, where the pool's others began as %q; "+
				"log every connection in the same way", got, *known)
		}
		return nil
	}
	l.loginRole.CompareAndSwap(nil, &got)
	return nil
}

// deferStatement queues a statement whose result nobody reads, to go out
// with the next one (or with COMMIT). Its error, if any, becomes the next
// statement's.
func (t *scopedTx) deferStatement(sql string, args ...any) {
	t.pending = append(t.pending, pending{sql: sql, args: args})
}

// execDeferred runs a write whose result nobody reads (no RETURNING, no
// row count): on a ledger transaction it goes out with the next statement,
// in that round trip, ahead of it. If it fails, Postgres skips the rest of
// that pipeline and the next statement returns its error; the transaction
// is aborted either way, so the command fails as it would have.
func execDeferred(ctx context.Context, tx pgx.Tx, sql string, args ...any) error {
	if t, ok := tx.(*scopedTx); ok && t.usable() == nil && batchable(sql, args) {
		t.deferStatement(sql, args...)
		return nil
	}
	_, err := tx.Exec(ctx, sql, args...)
	return err
}

func (t *scopedTx) usable() error {
	switch {
	case t.closed:
		return pgx.ErrTxClosed
	case t.committing:
		return errCommitted
	}
	return t.failed
}

// commitWithNext sends the transaction's COMMIT in the same round trip as
// its next statement, which must be its last: a write whose answer the
// caller reads, then commits (meter). Postgres skips the COMMIT if the
// statement fails, and Commit reports the COMMIT's answer; any statement
// after that one is refused, so nothing can run outside the transaction.
func (t *scopedTx) commitWithNext() { t.commitNext = true }

// batchClosed records how the batch that carried COMMIT ended.
func (t *scopedTx) batchClosed(err error) {
	if t.committing && t.commitErr == nil {
		t.commitErr = err
	}
}

// send sends the pending statements and then queued, in one batch, and
// reads the pending ones' results. The returned results are positioned at
// queued's first.
func (t *scopedTx) send(ctx context.Context, queued []*pgx.QueuedQuery) (pgx.BatchResults, error) {
	b := &pgx.Batch{}
	for _, p := range t.pending {
		b.Queue(p.sql, p.args...)
	}
	b.QueuedQueries = append(b.QueuedQueries, queued...)
	if t.commitNext && len(queued) > 0 {
		t.commitNext, t.committing = false, true
		b.Queue("commit").Exec(func(ct pgconn.CommandTag) error {
			t.commitTag = ct
			return nil
		})
	}
	pend := t.pending
	t.pending, t.begun = nil, true
	br := t.conn.SendBatch(ctx, b)
	for _, p := range pend {
		var err error
		if p.scan != nil {
			err = p.scan(br.QueryRow())
		} else {
			_, err = br.Exec()
		}
		if err != nil {
			// The caller's statements didn't run for it (the server skips
			// a pipeline's rest after an error); their callbacks mustn't.
			for _, q := range queued {
				q.Fn = nil
			}
			_ = br.Close()
			if p.opening {
				t.failed = err
			}
			return nil, err
		}
	}
	return br, nil
}

// flush sends what is pending, alone.
func (t *scopedTx) flush(ctx context.Context) error {
	if len(t.pending) == 0 {
		return nil
	}
	br, err := t.send(ctx, nil)
	if err != nil {
		return err
	}
	return br.Close()
}

// batchable reports whether a statement can join a pipeline: pgx batches
// take only a QueryRewriter (NamedArgs is one) among the call options, and
// a text of several statements needs the simple protocol.
func batchable(sql string, args []any) bool {
	if len(args) > 0 {
		switch args[0].(type) {
		case pgx.QueryExecMode, pgx.QueryResultFormats, pgx.QueryResultFormatsByOID:
			return false
		}
	}
	return len(args) > 0 || !strings.Contains(strings.TrimRight(strings.TrimSpace(sql), "; \t\n"), ";")
}

// Exec implements pgx.Tx.
func (t *scopedTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if err := t.usable(); err != nil {
		return pgconn.CommandTag{}, err
	}
	if (len(t.pending) > 0 || t.commitNext) && !batchable(sql, args) {
		if err := t.flush(ctx); err != nil {
			return pgconn.CommandTag{}, err
		}
		t.commitNext = false // Commit sends it alone
	}
	if len(t.pending) == 0 && !t.commitNext {
		return t.conn.Exec(ctx, sql, args...)
	}
	br, err := t.send(ctx, []*pgx.QueuedQuery{{SQL: sql, Arguments: args}})
	if err != nil {
		t.batchClosed(err)
		return pgconn.CommandTag{}, err
	}
	tag, err := br.Exec()
	cerr := br.Close()
	t.batchClosed(cerr)
	if err == nil {
		err = cerr
	}
	return tag, err
}

// Query implements pgx.Tx.
func (t *scopedTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if err := t.usable(); err != nil {
		return errRows{err}, err
	}
	if (len(t.pending) > 0 || t.commitNext) && !batchable(sql, args) {
		if err := t.flush(ctx); err != nil {
			return errRows{err}, err
		}
		t.commitNext = false // Commit sends it alone
	}
	if len(t.pending) == 0 && !t.commitNext {
		return t.conn.Query(ctx, sql, args...)
	}
	br, err := t.send(ctx, []*pgx.QueuedQuery{{SQL: sql, Arguments: args}})
	if err != nil {
		t.batchClosed(err)
		return errRows{err}, err
	}
	rows, err := br.Query()
	if err != nil {
		t.batchClosed(br.Close())
		return errRows{err}, err
	}
	return &pipelinedRows{Rows: rows, br: br, closed: t.batchClosed}, nil
}

// QueryRow implements pgx.Tx.
func (t *scopedTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if t.usable() == nil && len(t.pending) == 0 && !t.commitNext {
		return t.conn.QueryRow(ctx, sql, args...)
	}
	rows, _ := t.Query(ctx, sql, args...)
	return rowOf{rows}
}

// SendBatch implements pgx.Tx: the pending statements go first, in the
// same pipeline. b's callbacks run when its results are closed, as ever.
func (t *scopedTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	if err := t.usable(); err != nil {
		return errBatch{err}
	}
	if len(t.pending) == 0 || b.Len() == 0 {
		if err := t.flush(ctx); err != nil {
			return errBatch{err}
		}
		return t.conn.SendBatch(ctx, b)
	}
	br, err := t.send(ctx, b.QueuedQueries)
	if err != nil {
		return errBatch{err}
	}
	return br
}

// CopyFrom implements pgx.Tx (the pending statements go out first, alone).
func (t *scopedTx) CopyFrom(ctx context.Context, table pgx.Identifier, columns []string, src pgx.CopyFromSource) (int64, error) {
	if err := t.usable(); err != nil {
		return 0, err
	}
	if err := t.flush(ctx); err != nil {
		return 0, err
	}
	return t.conn.CopyFrom(ctx, table, columns, src)
}

// Prepare implements pgx.Tx (the pending statements go out first, alone).
func (t *scopedTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	if err := t.usable(); err != nil {
		return nil, err
	}
	if err := t.flush(ctx); err != nil {
		return nil, err
	}
	return t.conn.Conn().Prepare(ctx, name, sql)
}

// LargeObjects implements pgx.Tx; the ledger stores none, and the zero
// value it returns can't be used.
func (t *scopedTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }

// Conn implements pgx.Tx. Everything pending goes out first (the opening
// included), so a statement sent on the connection directly can run
// neither before the scope nor ahead of a deferred write; a failure there
// fails the transaction's next statement.
func (t *scopedTx) Conn() *pgx.Conn {
	if t.usable() == nil {
		_ = t.flush(context.Background())
	}
	return t.conn.Conn()
}

// Config is the pool's configuration. River's driver reads its exec mode
// here before each statement (it would otherwise call Conn, which sends
// what's pending, such as the role switch meant to ride with River's own
// first statement).
func (t *scopedTx) Config() *pgxpool.Config { return t.l.pool.Config() }

// Begin implements pgx.Tx with a savepoint, as pgx does.
func (t *scopedTx) Begin(ctx context.Context) (pgx.Tx, error) {
	if err := t.usable(); err != nil {
		return nil, err
	}
	t.savepoint++
	name := "sp_" + strconv.FormatInt(t.savepoint, 10)
	if _, err := t.Exec(ctx, "savepoint "+name); err != nil {
		return nil, err
	}
	return &savepointTx{scopedTx: t, name: name}, nil
}

// Commit implements pgx.Tx. A read-only transaction's COMMIT is sent after
// Commit returns (see the package note); a writing one's is answered first,
// with whatever is pending in the same round trip.
func (t *scopedTx) Commit(ctx context.Context) error {
	if t.closed {
		return pgx.ErrTxClosed
	}
	t.closed = true
	switch {
	case t.committing:
		return t.committed(ctx)
	case t.failed != nil:
		t.end(ctx, "rollback")
		return t.failed
	case !t.begun:
		t.conn.Release()
		return nil
	case t.readOnly:
		go t.end(context.WithoutCancel(ctx), "commit")
		return nil
	}
	br, err := t.send(ctx, []*pgx.QueuedQuery{{SQL: "commit"}})
	var tag pgconn.CommandTag
	if err == nil {
		tag, err = br.Exec()
		if cerr := br.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		if t.conn.Conn().PgConn().TxStatus() != 'I' {
			_ = t.conn.Conn().Close(ctx) // already have an error to return
		}
		t.conn.Release()
		return err
	}
	t.conn.Release()
	if tag.String() == "ROLLBACK" {
		return pgx.ErrTxCommitRollback
	}
	return nil
}

// committed ends a transaction whose COMMIT went with its last statement:
// it reports the COMMIT's answer, and rolls back if the statement failed
// (Postgres skipped the COMMIT).
func (t *scopedTx) committed(ctx context.Context) error {
	if t.conn.Conn().PgConn().TxStatus() != 'I' {
		_ = t.end(ctx, "rollback")
		if t.commitErr != nil {
			return t.commitErr
		}
		return pgx.ErrTxCommitRollback
	}
	t.conn.Release()
	switch {
	case t.commitErr != nil:
		return t.commitErr
	case t.commitTag.String() == "ROLLBACK":
		return pgx.ErrTxCommitRollback
	}
	return nil
}

// Rollback implements pgx.Tx: safe to call after Commit (ErrTxClosed), as
// pgx's is. A read-only transaction rolls back after Rollback returns.
func (t *scopedTx) Rollback(ctx context.Context) error {
	if t.closed {
		return pgx.ErrTxClosed
	}
	t.closed = true
	t.pending = nil
	if t.committing && t.conn.Conn().PgConn().TxStatus() == 'I' {
		// The COMMIT that went with the last statement ended it.
		t.conn.Release()
		return nil
	}
	switch {
	case !t.begun:
		t.conn.Release()
		return nil
	case t.readOnly:
		go t.end(context.WithoutCancel(ctx), "rollback")
		return nil
	}
	return t.end(ctx, "rollback")
}

// end sends COMMIT or ROLLBACK (after anything pending) and returns the
// connection to the pool. A connection left in a transaction is closed,
// so the pool drops it.
func (t *scopedTx) end(ctx context.Context, word string) error {
	defer t.conn.Release()
	ctx, cancel := context.WithTimeout(ctx, endTimeout)
	defer cancel()
	if word == "rollback" {
		t.pending = nil
	}
	if !t.begun {
		return nil
	}
	br, err := t.send(ctx, []*pgx.QueuedQuery{{SQL: word}})
	if err == nil {
		_, err = br.Exec()
		if cerr := br.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		if t.conn.Conn().PgConn().TxStatus() != 'I' {
			_ = t.conn.Conn().Close(ctx)
		}
		if t.readOnly && ctx.Err() == nil {
			t.l.log.Warn("ledger: ending a read-only transaction failed; the connection is dropped", "error", err)
		}
	}
	return err
}

// readBatch runs b's statements in one read-only transaction, in one round
// trip with its BEGIN, scope and COMMIT (Ledger.ReadBatch).
func (t *scopedTx) readBatch(ctx context.Context, b *pgx.Batch) error {
	t.closed = true
	queued := append(slices.Clone(b.QueuedQueries), &pgx.QueuedQuery{SQL: "commit"})
	br, err := t.send(ctx, queued)
	if err == nil {
		err = br.Close() // b's callbacks, then the COMMIT
	}
	if err != nil {
		// The pipeline stopped at the error, before the COMMIT.
		go t.end(context.WithoutCancel(ctx), "rollback")
		return err
	}
	t.conn.Release()
	return nil
}

// savepointTx is a nested transaction on a savepoint, as pgx's is.
type savepointTx struct {
	*scopedTx
	name   string
	closed bool
}

// Commit releases the savepoint.
func (s *savepointTx) Commit(ctx context.Context) error {
	if s.closed {
		return pgx.ErrTxClosed
	}
	s.closed = true
	_, err := s.scopedTx.Exec(ctx, "release savepoint "+s.name)
	return err
}

// Rollback rolls back to the savepoint.
func (s *savepointTx) Rollback(ctx context.Context) error {
	if s.closed {
		return pgx.ErrTxClosed
	}
	s.closed = true
	_, err := s.scopedTx.Exec(ctx, "rollback to savepoint "+s.name)
	return err
}

// pipelinedRows are a statement's rows that came in a batch: closing them
// (or reading past the last) closes the batch, so the connection is free
// for the next statement.
type pipelinedRows struct {
	pgx.Rows
	br   pgx.BatchResults
	done bool
	err  error
	// closed hears how the batch ended (scopedTx.batchClosed).
	closed func(error)
}

func (r *pipelinedRows) Next() bool {
	if r.Rows.Next() {
		return true
	}
	r.finish()
	return false
}

func (r *pipelinedRows) Close() { r.finish() }

func (r *pipelinedRows) Err() error {
	if err := r.Rows.Err(); err != nil {
		return err
	}
	return r.err
}

func (r *pipelinedRows) finish() {
	if r.done {
		return
	}
	r.done = true
	r.Rows.Close()
	err := r.br.Close()
	if err != nil && r.Rows.Err() == nil {
		r.err = err
	}
	if r.closed != nil {
		r.closed(err)
	}
}

// rowOf is QueryRow's row over Query's rows, scanning as pgx's does.
type rowOf struct{ rows pgx.Rows }

func (r rowOf) Scan(dest ...any) error {
	rows := r.rows
	defer rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return pgx.ErrNoRows
	}
	_ = rows.Scan(dest...)
	rows.Close()
	return rows.Err()
}

// errRows are the rows of a statement that never ran.
type errRows struct{ err error }

func (r errRows) Close()                                       {}
func (r errRows) Err() error                                   { return r.err }
func (r errRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r errRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r errRows) Next() bool                                   { return false }
func (r errRows) Scan(...any) error                            { return r.err }
func (r errRows) Values() ([]any, error)                       { return nil, r.err }
func (r errRows) RawValues() [][]byte                          { return nil }
func (r errRows) Conn() *pgx.Conn                              { return nil }
func (r errRows) TypeMap() *pgtype.Map                         { return nil }

// errBatch is the results of a batch that never went out.
type errBatch struct{ err error }

func (b errBatch) Exec() (pgconn.CommandTag, error) { return pgconn.CommandTag{}, b.err }
func (b errBatch) Query() (pgx.Rows, error)         { return errRows{b.err}, b.err }
func (b errBatch) QueryRow() pgx.Row                { return rowOf{errRows{b.err}} }
func (b errBatch) Close() error                     { return b.err }

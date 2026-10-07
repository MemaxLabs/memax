package netsim

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Kind is what a round trip was.
type Kind string

// The round trips a pool makes.
const (
	// Query is Query, QueryRow or Exec: one round trip (BEGIN and COMMIT
	// are Execs).
	Query Kind = "query"
	// Batch is SendBatch: its queries go in one pipeline, one round trip.
	Batch Kind = "batch"
	// Prepare is a statement the connection hadn't prepared yet (pgx's
	// statement cache), an extra round trip before its first execution
	// there. A steady-state operation prepares nothing.
	Prepare Kind = "prepare"
	// Copy is a COPY FROM.
	Copy Kind = "copy"
	// Ping is the pool's liveness check on acquiring an idle connection.
	Ping Kind = "ping"
)

// Trip is one round trip.
type Trip struct {
	Kind Kind
	// SQL is the statement (a batch's, joined by " ⏎ ").
	SQL string
}

// Counter collects the round trips of one operation: every pgx call made
// with a context from Track (or derived from one) lands here, from any
// goroutine.
type Counter struct {
	mu    sync.Mutex
	trips []Trip
}

type counterKey struct{}

// Track returns a context whose round trips c counts.
func Track(ctx context.Context) (context.Context, *Counter) {
	c := &Counter{}
	return With(ctx, c), c
}

// With returns a context whose round trips c counts.
func With(ctx context.Context, c *Counter) context.Context {
	return context.WithValue(ctx, counterKey{}, c)
}

func counterOf(ctx context.Context) *Counter {
	if ctx == nil {
		return nil
	}
	c, _ := ctx.Value(counterKey{}).(*Counter)
	return c
}

func (c *Counter) add(t Trip) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.trips = append(c.trips, t)
}

// Trips lists the round trips so far, in the order they started.
func (c *Counter) Trips() []Trip {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Trip(nil), c.trips...)
}

// Count counts the round trips of the given kinds (all with none).
func (c *Counter) Count(kinds ...Kind) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.trips {
		if len(kinds) == 0 || containsKind(kinds, t.Kind) {
			n++
		}
	}
	return n
}

// RoundTrips counts the steady-state round trips: everything but the
// statement cache's prepares, which a warm connection doesn't make.
func (c *Counter) RoundTrips() int { return c.Count(Query, Batch, Copy, Ping) }

// Reset forgets the trips so far.
func (c *Counter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.trips = nil
}

// String lists the trips, one per line, for test failures.
func (c *Counter) String() string {
	var b strings.Builder
	for i, t := range c.Trips() {
		fmt.Fprintf(&b, "%2d %-7s %s\n", i+1, t.Kind, t.SQL)
	}
	return b.String()
}

func containsKind(ks []Kind, k Kind) bool {
	for _, x := range ks {
		if x == k {
			return true
		}
	}
	return false
}

// Tracer is a pgx tracer that counts round trips: into the Counter of the
// call's context (Track), and in total.
type Tracer struct {
	total    atomic.Int64
	connects atomic.Int64
	// shouldPing is the pool's own ping policy, which Install wraps.
	shouldPing func(context.Context, pgxpool.ShouldPingParams) bool
	// fallback counts the trips whose context carries no Counter.
	fallback atomic.Pointer[Counter]
}

// SetFallback makes c count every round trip whose context carries no
// Counter (nil stops it): code that queries with context.Background(),
// such as V1's store under the auth middleware, during a window the test
// controls. Anything else running in that window counts too, so use it
// only while the pool is otherwise quiet.
func (t *Tracer) SetFallback(c *Counter) { t.fallback.Store(c) }

// Install puts a new Tracer on a pool config, wrapping its ping policy
// (pgx's default, idle over a second, when it has none) so pings count.
func Install(cfg *pgxpool.Config) *Tracer {
	t := &Tracer{shouldPing: cfg.ShouldPing}
	if t.shouldPing == nil {
		t.shouldPing = func(_ context.Context, p pgxpool.ShouldPingParams) bool { return p.IdleDuration > time.Second }
	}
	cfg.ConnConfig.Tracer = t
	cfg.ShouldPing = t.ShouldPing
	return t
}

// Of returns the Tracer Install put on pool, or nil.
func Of(pool *pgxpool.Pool) *Tracer {
	if pool == nil {
		return nil
	}
	t, _ := pool.Config().ConnConfig.Tracer.(*Tracer)
	return t
}

// Total counts every round trip on the pool so far (every context).
func (t *Tracer) Total() int64 { return t.total.Load() }

// Connects counts the connections the pool opened.
func (t *Tracer) Connects() int64 { return t.connects.Load() }

func (t *Tracer) trip(ctx context.Context, k Kind, sql func() string) {
	t.total.Add(1)
	c := counterOf(ctx)
	if c == nil {
		c = t.fallback.Load()
	}
	if c != nil {
		c.add(Trip{Kind: k, SQL: sql()})
	}
}

func text(sql string) func() string { return func() string { return oneLine(sql) } }

// ShouldPing is the pool's ping policy, counting the pings it asks for.
func (t *Tracer) ShouldPing(ctx context.Context, p pgxpool.ShouldPingParams) bool {
	if !t.shouldPing(ctx, p) {
		return false
	}
	t.trip(ctx, Ping, text("-- ping"))
	return true
}

// TraceQueryStart implements pgx.QueryTracer.
func (t *Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	t.trip(ctx, Query, text(d.SQL))
	return ctx
}

// TraceQueryEnd implements pgx.QueryTracer.
func (t *Tracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// TraceBatchStart implements pgx.BatchTracer.
func (t *Tracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceBatchStartData) context.Context {
	t.trip(ctx, Batch, func() string {
		sqls := make([]string, 0, d.Batch.Len())
		for _, q := range d.Batch.QueuedQueries {
			sqls = append(sqls, oneLine(q.SQL))
		}
		return strings.Join(sqls, " ⏎ ")
	})
	return ctx
}

// TraceBatchQuery implements pgx.BatchTracer.
func (t *Tracer) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {}

// TraceBatchEnd implements pgx.BatchTracer.
func (t *Tracer) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {}

type prepareKey struct{}

// TracePrepareStart implements pgx.PrepareTracer.
func (t *Tracer) TracePrepareStart(ctx context.Context, _ *pgx.Conn, d pgx.TracePrepareStartData) context.Context {
	return context.WithValue(ctx, prepareKey{}, d.SQL)
}

// TracePrepareEnd implements pgx.PrepareTracer: a statement already
// prepared on the connection costs nothing.
func (t *Tracer) TracePrepareEnd(ctx context.Context, _ *pgx.Conn, d pgx.TracePrepareEndData) {
	if d.AlreadyPrepared {
		return
	}
	sql, _ := ctx.Value(prepareKey{}).(string)
	t.trip(ctx, Prepare, text(sql))
}

// TraceCopyFromStart implements pgx.CopyFromTracer.
func (t *Tracer) TraceCopyFromStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceCopyFromStartData) context.Context {
	t.trip(ctx, Copy, text("COPY "+d.TableName.Sanitize()))
	return ctx
}

// TraceCopyFromEnd implements pgx.CopyFromTracer.
func (t *Tracer) TraceCopyFromEnd(context.Context, *pgx.Conn, pgx.TraceCopyFromEndData) {}

// TraceConnectStart implements pgx.ConnectTracer.
func (t *Tracer) TraceConnectStart(ctx context.Context, _ pgx.TraceConnectStartData) context.Context {
	t.connects.Add(1)
	return ctx
}

// TraceConnectEnd implements pgx.ConnectTracer.
func (t *Tracer) TraceConnectEnd(context.Context, pgx.TraceConnectEndData) {}

// oneLine squeezes a statement onto one line, for listings.
func oneLine(sql string) string {
	f := strings.Fields(sql)
	s := strings.Join(f, " ")
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	return s
}

// Settle waits until pool has no connection checked out (work a call left
// running after it returned, such as a read-only COMMIT sent off the
// request path, has finished), or until timeout.
func Settle(pool *pgxpool.Pool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if pool.Stat().AcquiredConns() == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
}

var (
	_ pgx.QueryTracer    = (*Tracer)(nil)
	_ pgx.BatchTracer    = (*Tracer)(nil)
	_ pgx.PrepareTracer  = (*Tracer)(nil)
	_ pgx.CopyFromTracer = (*Tracer)(nil)
	_ pgx.ConnectTracer  = (*Tracer)(nil)
)

// CounterHeader names the Counter an HTTP request's round trips go to,
// for a server a test can't hand a context to (Requests).
const CounterHeader = "X-Netsim-Counter"

// Requests gives each HTTP request the Counter its CounterHeader names.
type Requests struct {
	mu sync.Mutex
	m  map[string]*Counter
	n  int64
}

// Add registers c and returns the header value that names it.
func (r *Requests) Add(c *Counter) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m == nil {
		r.m = map[string]*Counter{}
	}
	r.n++
	id := fmt.Sprintf("c%d", r.n)
	r.m[id] = c
	return id
}

// Wrap attaches the named Counter to each request's context.
func (r *Requests) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if id := req.Header.Get(CounterHeader); id != "" {
			r.mu.Lock()
			c := r.m[id]
			r.mu.Unlock()
			if c != nil {
				req = req.WithContext(With(req.Context(), c))
			}
		}
		next.ServeHTTP(w, req)
	})
}

package reads_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/reads"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// sink records batches; block, when set, holds every flush until closed.
type sink struct {
	mu      sync.Mutex
	batches [][]ledger.ReadEvent
	block   chan struct{}
	fail    atomic.Bool
	skip    int
}

func (s *sink) RecordReads(ctx context.Context, events []ledger.ReadEvent) (ledger.ReadStats, error) {
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return ledger.ReadStats{}, ctx.Err()
		}
	}
	if s.fail.Load() {
		return ledger.ReadStats{}, errors.New("database down")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches = append(s.batches, append([]ledger.ReadEvent(nil), events...))
	return ledger.ReadStats{Written: len(events) - s.skip, Skipped: s.skip}, nil
}

func (s *sink) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, b := range s.batches {
		n += len(b)
	}
	return n
}

func ev() ledger.ReadEvent {
	return ledger.ReadEvent{SpaceID: uuid.New(), Kind: ledger.ReadGet}
}

func waitFor(t *testing.T, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s: not within %v", what, within)
}

func TestRecorder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"flushes every interval", func(t *testing.T) {
			s := &sink{}
			r := reads.New(s, reads.Options{Interval: 20 * time.Millisecond, Logger: quiet})
			defer r.Close()
			for range 5 {
				r.Record(ev())
			}
			waitFor(t, time.Second, "the flush", func() bool { return s.total() == 5 })
			if st := r.Stats(); st.Recorded != 5 || st.Written != 5 || st.Dropped() != 0 || st.Flushes < 1 {
				t.Errorf("stats = %+v", st)
			}
		}},
		{"flushes a full batch before the tick", func(t *testing.T) {
			s := &sink{}
			r := reads.New(s, reads.Options{Interval: time.Hour, MaxBatch: 10, Logger: quiet})
			defer r.Close()
			for range 25 {
				r.Record(ev())
			}
			waitFor(t, time.Second, "two full batches", func() bool { return s.total() == 20 })
		}},
		{"drops instead of blocking when the buffer is full", func(t *testing.T) {
			s := &sink{block: make(chan struct{})}
			r := reads.New(s, reads.Options{Buffer: 8, MaxBatch: 4, Interval: time.Millisecond, Logger: quiet})
			// The flusher takes a batch and blocks in the sink; then the
			// buffer fills and every further read is dropped at once.
			start := time.Now()
			for range 1000 {
				r.Record(ev())
			}
			took := time.Since(start)
			st := r.Stats()
			if st.DroppedFull == 0 || st.Recorded+st.DroppedFull != 1000 || st.Recorded > 8+4 {
				t.Errorf("stats = %+v; want at most a buffer and a batch accepted, the rest dropped", st)
			}
			// The sink is stuck the whole time: had Record waited, this
			// loop would never have finished.
			if took > 500*time.Millisecond {
				t.Errorf("1000 reads took %v to record with the database stuck; Record must never wait", took)
			}
			close(s.block)
			r.Close()
			if got := s.total(); int64(got) != st.Recorded {
				t.Errorf("written %d, want every accepted read (%d)", got, st.Recorded)
			}
		}},
		{"shutdown flushes what is buffered", func(t *testing.T) {
			s := &sink{}
			r := reads.New(s, reads.Options{Interval: time.Hour, Logger: quiet})
			for range 37 {
				r.Record(ev())
			}
			r.Close()
			r.Close() // twice is fine
			if s.total() != 37 {
				t.Errorf("flushed %d at shutdown, want 37", s.total())
			}
			r.Record(ev())
			if st := r.Stats(); st.DroppedClosed != 1 {
				t.Errorf("a read after Close: %+v", st)
			}
		}},
		{"a failed batch is dropped and counted", func(t *testing.T) {
			s := &sink{}
			s.fail.Store(true)
			r := reads.New(s, reads.Options{Interval: 10 * time.Millisecond, Logger: quiet})
			for range 3 {
				r.Record(ev())
			}
			waitFor(t, time.Second, "the failed flush", func() bool { return r.Stats().DroppedFailed == 3 })
			s.fail.Store(false)
			r.Record(ev())
			r.Close()
			if st := r.Stats(); st.Written != 1 || st.FlushErrors != 1 {
				t.Errorf("after recovering: %+v", st)
			}
		}},
		{"skipped reads are counted as dropped", func(t *testing.T) {
			s := &sink{skip: 1}
			r := reads.New(s, reads.Options{Interval: time.Hour, Logger: quiet})
			r.Record(ev())
			r.Record(ev())
			r.Close()
			if st := r.Stats(); st.Written != 1 || st.DroppedSkipped != 1 {
				t.Errorf("stats = %+v", st)
			}
		}},
		{"nil means disabled", func(t *testing.T) {
			var nilLedger *ledger.Ledger
			if reads.New(nil, reads.Options{}) != nil || reads.New(nilLedger, reads.Options{}) != nil {
				t.Fatal("a recorder without a sink")
			}
			var r *reads.Recorder
			r.Record(ev())
			r.Close()
			if r.Stats() != (reads.Stats{}) {
				t.Error("a nil recorder counted something")
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.run(t)
		})
	}
}

// The recorder on a real database under load. A burst far beyond what one
// flusher writes (every goroutine recording as fast as it can) never waits
// on the database: the buffer fills, and the rest is dropped and counted.
// A sustained rate well above production's (about 3,000 reads a second)
// drops nothing. Either way every accepted read is written by the time
// Close returns, with unique R- numbers and rollups that add up.
func TestRecorderUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("load: skipped in -short")
	}
	t.Parallel()
	cases := []struct {
		name       string
		goroutines int
		each       int
		pace       time.Duration // between one goroutine's reads; 0 is a burst
		noDrops    bool
	}{
		{"burst", 32, 2000, 0, false},
		{"sustained", 32, 200, 10 * time.Millisecond, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, pool := testdb.Acquire(t)
			ctx := context.Background()
			l := ledger.New(pool, ledger.WithLogger(quiet))
			user, space := uuid.New(), uuid.New()
			if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, 'load')`, user, user.String()+"@load.test"); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, 'load', $2, 'team', $3, 'project')`,
				space, space.String(), user); err != nil {
				t.Fatal(err)
			}
			memories := make([]uuid.UUID, 10)
			for i := range memories {
				memories[i] = uuid.New() // ids only: rollups hold no foreign key
			}
			r := reads.New(l, reads.Options{Logger: quiet})
			var wg sync.WaitGroup
			var mu sync.Mutex
			var took []time.Duration
			start := time.Now()
			for g := range c.goroutines {
				wg.Add(1)
				go func() {
					defer wg.Done()
					mine := make([]time.Duration, 0, c.each)
					conn := uuid.New()
					for i := range c.each {
						e := ledger.ReadEvent{SpaceID: space, TenantID: user, Reader: ledger.ReaderAgent, ConnectionID: conn,
							PersonID: user, Agent: "codex", Kind: ledger.ReadRecall, Via: policy.ViaMCP,
							Memories: memories[(g+i)%10 : (g+i)%10+1], At: time.Now()}
						t0 := time.Now()
						r.Record(e)
						mine = append(mine, time.Since(t0))
						if c.pace > 0 {
							time.Sleep(c.pace)
						}
					}
					mu.Lock()
					took = append(took, mine...)
					mu.Unlock()
				}()
			}
			wg.Wait()
			offered := time.Since(start)
			closeStart := time.Now()
			r.Close()
			closing := time.Since(closeStart)
			st := r.Stats()
			total := int64(c.goroutines * c.each)
			sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
			p99 := took[len(took)*99/100]
			t.Logf("%d reads offered in %v (%.0f/s): recorded %d, written %d, dropped %d (buffer full %d); %d flushes; Record p99 %v; Close flushed the rest in %v",
				total, offered.Round(time.Millisecond), float64(total)/offered.Seconds(), st.Recorded, st.Written, st.Dropped(),
				st.DroppedFull, st.Flushes, p99, closing.Round(time.Millisecond))
			if st.Recorded+st.Dropped() != total {
				t.Errorf("recorded %d + dropped %d != offered %d", st.Recorded, st.Dropped(), total)
			}
			if st.Written != st.Recorded || st.DroppedFailed != 0 {
				t.Errorf("written %d of %d recorded (failed %d)", st.Written, st.Recorded, st.DroppedFailed)
			}
			if c.noDrops && st.Dropped() != 0 {
				t.Errorf("dropped %d at a sustained %.0f reads a second", st.Dropped(), float64(total)/offered.Seconds())
			}
			if p99 > time.Millisecond {
				t.Errorf("Record p99 %v; it must stay a channel send", p99)
			}
			var rows, distinct, rollup int64
			if err := pool.QueryRow(ctx, `SELECT count(*), count(DISTINCT seq) FROM v2.reads`).Scan(&rows, &distinct); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(reads), 0) FROM v2.read_rollups`).Scan(&rollup); err != nil {
				t.Fatal(err)
			}
			if rows != st.Written || distinct != rows || rollup != rows {
				t.Errorf("rows %d, distinct R- %d, rollup total %d; want all %d", rows, distinct, rollup, st.Written)
			}
		})
	}
}

// Package reads records agent reads (R-, plan 25 §5.3) off the request
// path. Reads are not receipts: they are high-volume and must never sit
// on recall's latency budget. A surface hands each read to Record, which
// never blocks: the read goes into a bounded buffer, and a single
// goroutine flushes the buffer to the ledger (Ledger.RecordReads, one
// transaction per batch) every 250 ms, or sooner when a batch fills.
//
// Back-pressure is a drop, never a wait. When the buffer is full (the
// database is slow or down), Record drops the read and counts it; a batch
// the ledger refuses is dropped and counted too. Drops are exposed as the
// memax.reads.dropped counter (by reason) and logged at most once a
// minute. Close flushes what is buffered, within a deadline, at shutdown.
//
// A nil *Recorder is valid and records nothing (nil means disabled).
package reads

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// The defaults (plan 25 §5.3: flush every 250 ms).
const (
	DefaultBuffer   = 4096
	DefaultInterval = 250 * time.Millisecond
	DefaultMaxBatch = 1000
	// DefaultFlushTimeout bounds one batch's transaction.
	DefaultFlushTimeout = 5 * time.Second
	// DefaultCloseTimeout bounds the flush at shutdown.
	DefaultCloseTimeout = 10 * time.Second
)

// Sink writes a batch of reads: *ledger.Ledger.
type Sink interface {
	RecordReads(ctx context.Context, events []ledger.ReadEvent) (ledger.ReadStats, error)
}

// Options configures a Recorder. Zero values take the defaults.
type Options struct {
	Buffer       int
	Interval     time.Duration
	MaxBatch     int
	FlushTimeout time.Duration
	CloseTimeout time.Duration
	Logger       *slog.Logger
}

// The reasons a read is dropped.
const (
	DropBufferFull = "buffer_full"
	DropFailed     = "write_failed"
	DropSkipped    = "skipped"
	DropClosed     = "closed"
)

// Stats are a Recorder's counters since it started.
type Stats struct {
	// Recorded reads were accepted into the buffer; Written reached the
	// database.
	Recorded, Written int64
	// Dropped because the buffer was full, because their batch failed,
	// because the ledger skipped them (malformed, or a space that's gone),
	// or because they came after Close.
	DroppedFull, DroppedFailed, DroppedSkipped, DroppedClosed int64
	// Flushes is how many batches were written; FlushErrors how many failed.
	Flushes, FlushErrors int64
}

// Dropped is every dropped read.
func (s Stats) Dropped() int64 {
	return s.DroppedFull + s.DroppedFailed + s.DroppedSkipped + s.DroppedClosed
}

// Recorder buffers reads and flushes them in batches. It implements
// ledger.ReadRecorder.
type Recorder struct {
	sink Sink
	o    Options
	log  *slog.Logger
	ch   chan ledger.ReadEvent
	stop chan struct{}
	done chan struct{}
	once sync.Once

	closed                                             atomic.Bool
	recorded, written                                  atomic.Int64
	droppedFull, droppedFailed, droppedSkip, droppedCl atomic.Int64
	flushes, flushErrors                               atomic.Int64
	// lastWarn is when drops were last logged (unix seconds); unwarned
	// counts drops since.
	lastWarn atomic.Int64
	unwarned atomic.Int64

	m meters
}

var _ ledger.ReadRecorder = (*Recorder)(nil)

// New starts a recorder writing to sink. A nil sink returns nil: reads
// aren't recorded.
func New(sink Sink, o Options) *Recorder {
	if sink == nil {
		return nil
	}
	if l, ok := sink.(*ledger.Ledger); ok && l == nil {
		return nil
	}
	if o.Buffer <= 0 {
		o.Buffer = DefaultBuffer
	}
	if o.Interval <= 0 {
		o.Interval = DefaultInterval
	}
	if o.MaxBatch <= 0 {
		o.MaxBatch = DefaultMaxBatch
	}
	if o.FlushTimeout <= 0 {
		o.FlushTimeout = DefaultFlushTimeout
	}
	if o.CloseTimeout <= 0 {
		o.CloseTimeout = DefaultCloseTimeout
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	r := &Recorder{sink: sink, o: o, log: o.Logger.With("component", "reads"),
		ch: make(chan ledger.ReadEvent, o.Buffer), stop: make(chan struct{}), done: make(chan struct{}), m: newMeters()}
	go r.loop()
	return r
}

// Record implements ledger.ReadRecorder. It never blocks: a full buffer
// drops the read and counts it.
func (r *Recorder) Record(e ledger.ReadEvent) {
	if r == nil {
		return
	}
	if r.closed.Load() {
		r.drop(DropClosed, 1)
		return
	}
	select {
	case r.ch <- e:
		r.recorded.Add(1)
		r.m.recorded.Add(context.Background(), 1)
	default:
		r.drop(DropBufferFull, 1)
	}
}

// Stats reports the counters.
func (r *Recorder) Stats() Stats {
	if r == nil {
		return Stats{}
	}
	return Stats{
		Recorded: r.recorded.Load(), Written: r.written.Load(),
		DroppedFull: r.droppedFull.Load(), DroppedFailed: r.droppedFailed.Load(),
		DroppedSkipped: r.droppedSkip.Load(), DroppedClosed: r.droppedCl.Load(),
		Flushes: r.flushes.Load(), FlushErrors: r.flushErrors.Load(),
	}
}

// Close stops accepting reads, flushes what is buffered (within
// CloseTimeout) and returns. It is safe to call more than once.
func (r *Recorder) Close() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		r.closed.Store(true)
		close(r.stop)
		<-r.done
		s := r.Stats()
		r.log.Info("reads: recorder closed", "recorded", s.Recorded, "written", s.Written, "dropped", s.Dropped())
	})
}

func (r *Recorder) loop() {
	defer close(r.done)
	t := time.NewTicker(r.o.Interval)
	defer t.Stop()
	batch := make([]ledger.ReadEvent, 0, r.o.MaxBatch)
	for {
		select {
		case e := <-r.ch:
			batch = append(batch, e)
			if len(batch) >= r.o.MaxBatch {
				batch = r.flush(context.Background(), batch)
			}
		case <-t.C:
			batch = r.flush(context.Background(), batch)
		case <-r.stop:
			ctx, cancel := context.WithTimeout(context.Background(), r.o.CloseTimeout)
			defer cancel()
			for {
				select {
				case e := <-r.ch:
					batch = append(batch, e)
					if len(batch) >= r.o.MaxBatch {
						batch = r.flush(ctx, batch)
					}
				default:
					r.flush(ctx, batch)
					return
				}
			}
		}
	}
}

// flush writes the batch and returns it emptied.
func (r *Recorder) flush(parent context.Context, batch []ledger.ReadEvent) []ledger.ReadEvent {
	if len(batch) == 0 {
		return batch
	}
	ctx, cancel := context.WithTimeout(parent, r.o.FlushTimeout)
	defer cancel()
	start := time.Now()
	stats, err := r.sink.RecordReads(ctx, batch)
	r.m.flushDuration.Record(context.Background(), time.Since(start).Seconds())
	r.m.batchSize.Record(context.Background(), int64(len(batch)))
	if err != nil {
		r.flushErrors.Add(1)
		r.drop(DropFailed, int64(len(batch)))
		r.log.Warn("reads: a batch of reads couldn't be written; dropped", "metric", "reads_dropped",
			"reason", DropFailed, "count", len(batch), "error", err)
		return batch[:0]
	}
	r.flushes.Add(1)
	r.written.Add(int64(stats.Written))
	r.m.written.Add(context.Background(), int64(stats.Written))
	if stats.Skipped > 0 {
		r.drop(DropSkipped, int64(stats.Skipped))
	}
	return batch[:0]
}

// drop counts dropped reads, and logs them at most once a minute.
func (r *Recorder) drop(reason string, n int64) {
	switch reason {
	case DropBufferFull:
		r.droppedFull.Add(n)
	case DropFailed:
		r.droppedFailed.Add(n)
	case DropSkipped:
		r.droppedSkip.Add(n)
	case DropClosed:
		r.droppedCl.Add(n)
	}
	r.m.dropped.Add(context.Background(), n, metric.WithAttributes(attribute.String("reason", reason)))
	r.unwarned.Add(n)
	now := time.Now().Unix()
	last := r.lastWarn.Load()
	if now-last < 60 || !r.lastWarn.CompareAndSwap(last, now) {
		return
	}
	s := r.Stats()
	r.log.Warn("reads: dropping reads", "metric", "reads_dropped", "since_last_warning", r.unwarned.Swap(0),
		"buffer_full", s.DroppedFull, "write_failed", s.DroppedFailed, "skipped", s.DroppedSkipped, "closed", s.DroppedClosed)
}

// meters are the recorder's OpenTelemetry instruments (no-ops when OTel
// isn't configured).
type meters struct {
	recorded, written, dropped metric.Int64Counter
	batchSize                  metric.Int64Histogram
	flushDuration              metric.Float64Histogram
}

func newMeters() meters {
	m := otel.Meter("memax.reads")
	var out meters
	out.recorded, _ = m.Int64Counter("memax.reads.recorded", metric.WithDescription("Reads accepted into the recorder's buffer"))
	out.written, _ = m.Int64Counter("memax.reads.written", metric.WithDescription("Reads written to v2.reads"))
	out.dropped, _ = m.Int64Counter("memax.reads.dropped",
		metric.WithDescription("Reads dropped, by reason: buffer_full, write_failed, skipped, closed"))
	out.batchSize, _ = m.Int64Histogram("memax.reads.batch.size", metric.WithDescription("Reads per flushed batch"))
	out.flushDuration, _ = m.Float64Histogram("memax.reads.flush.duration",
		metric.WithDescription("Time to write one batch of reads"), metric.WithUnit("s"))
	return out
}

package mcpv2

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Reads (R-, plan 25 §5.3) are not receipts: they are high-volume and must
// never sit on recall's latency path. A read is handed to a ReadRecorder,
// which must not block; the plan's recorder buffers them and flushes in
// batches every 250 ms to a monthly-partitioned v2.reads table, dropping
// (and counting) reads when the buffer is full.
//
// TODO(reads table): v2.reads doesn't exist yet. Until it does, NewReads
// with a nil sink counts reads and discards them, and the "read by" counts
// and changes-since-last-read use the connection's last_seen_at instead.

// Read is one agent read of the record.
type Read struct {
	Connection uuid.UUID
	Person     uuid.UUID
	Agent      string
	Tool       string
	SessionRef string
	At         time.Time
	// Memories are the refs read, by space.
	Memories map[uuid.UUID][]string
}

// ReadRecorder records reads off the request path. Record must not block.
type ReadRecorder interface {
	Record(Read)
}

type noReads struct{}

func (noReads) Record(Read) {}

// recordRead hands an agent's read to the recorder, after the work of the
// response is done.
func (s *Server) recordRead(p *v2api.Principal, tool, sessionRef string, spaces []space, out handler.MCPRecallOutput) {
	if p == nil || p.Actor.Kind != policy.ActorAgent {
		return
	}
	r := Read{Connection: p.Actor.ID, Person: p.Scope.PersonID, Agent: p.Actor.Agent, Tool: tool,
		SessionRef: sessionRef, At: s.now(), Memories: map[uuid.UUID][]string{}}
	add := func(items []handler.MCPItem) {
		for _, it := range items {
			if it.Record != handler.MCPRecordV2 || it.Ref == "" {
				continue
			}
			id, err := uuid.Parse(it.SpaceID)
			if err == nil {
				r.Memories[id] = append(r.Memories[id], it.Ref)
			}
		}
	}
	add(out.Results)
	add(out.Proposals)
	for _, d := range out.Digest {
		for _, sec := range d.Sections {
			add(sec.Memories)
		}
	}
	if len(r.Memories) == 0 {
		for _, sp := range spaces {
			r.Memories[sp.ID] = nil
		}
	}
	s.reads.Record(r)
}

// Reads is the buffered, asynchronous ReadRecorder.
type Reads struct {
	ch      chan Read
	sink    func(context.Context, []Read) error
	dropped atomic.Int64
	written atomic.Int64
	log     *slog.Logger
	wg      sync.WaitGroup
	stop    chan struct{}
}

// NewReads starts a recorder that flushes to sink every interval. A nil
// sink discards reads (counting them) until the reads table exists.
func NewReads(sink func(context.Context, []Read) error, buffer int, interval time.Duration, log *slog.Logger) *Reads {
	if log == nil {
		log = slog.Default()
	}
	r := &Reads{ch: make(chan Read, buffer), sink: sink, log: log, stop: make(chan struct{})}
	r.wg.Add(1)
	go r.loop(interval)
	return r
}

// Record implements ReadRecorder: it never blocks.
func (r *Reads) Record(read Read) {
	select {
	case r.ch <- read:
	default:
		r.dropped.Add(1)
	}
}

// Dropped counts reads dropped because the buffer was full.
func (r *Reads) Dropped() int64 { return r.dropped.Load() }

// Written counts reads handed to the sink (or discarded without one).
func (r *Reads) Written() int64 { return r.written.Load() }

// Close flushes what is buffered and stops.
func (r *Reads) Close() {
	close(r.stop)
	r.wg.Wait()
}

func (r *Reads) loop(interval time.Duration) {
	defer r.wg.Done()
	t := time.NewTicker(interval)
	defer t.Stop()
	var batch []Read
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if r.sink != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := r.sink(ctx, batch); err != nil {
				r.log.Warn("mcp: dropped a batch of reads", "count", len(batch), "error", err)
				r.dropped.Add(int64(len(batch)))
				batch = batch[:0]
				cancel()
				return
			}
			cancel()
		}
		r.written.Add(int64(len(batch)))
		batch = batch[:0]
	}
	for {
		select {
		case read := <-r.ch:
			batch = append(batch, read)
		case <-t.C:
			flush()
		case <-r.stop:
			for {
				select {
				case read := <-r.ch:
					batch = append(batch, read)
				default:
					flush()
					return
				}
			}
		}
	}
}

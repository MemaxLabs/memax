package forget

import (
	"context"
	"log/slog"
	"os"
	"sync"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// The caches Forget purges. V2 keeps no memory text in Redis: what holds
// words, or vectors derived from them, is in process memory, in each API
// and worker process:
//
//   - the compiled digest's artifacts (mcpv2): a session-start recall's
//     copy of a space's latest compiled file, by compile ref;
//   - the embeddings cache (v2recall.Vectors): recent query, draft and
//     statement vectors, by a hash of the text.
//
// A forgotten memory's lines were in an older compile ref, which recall
// never serves again (it reads the latest ref from the record on every
// call), and the vectors are keyed by a hash. Still, the copies stay in
// memory until they expire, so the propagation job drops them: the worker
// in its own process, and every API process through a Redis signal on
// Channel (the space's id, nothing else).

// Channel is the Redis channel the signal travels on.
const Channel = "memax:v2:forget"

// Purger drops the cached copies of a space's record.
type Purger interface {
	Purge(ctx context.Context, spaceID uuid.UUID)
}

// NopPurger purges nothing.
type NopPurger struct{}

// Purge implements Purger.
func (NopPurger) Purge(context.Context, uuid.UUID) {}

// Local is this process's purgers.
type Local struct {
	mu  sync.Mutex
	fns []func(spaceID uuid.UUID)
}

// Register adds a cache's purge.
func (l *Local) Register(fn func(spaceID uuid.UUID)) {
	if l == nil || fn == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fns = append(l.fns, fn)
}

// Purge implements Purger.
func (l *Local) Purge(_ context.Context, spaceID uuid.UUID) {
	if l == nil {
		return
	}
	l.mu.Lock()
	fns := append([]func(uuid.UUID){}, l.fns...)
	l.mu.Unlock()
	for _, fn := range fns {
		fn(spaceID)
	}
}

// Bus purges this process's caches and signals every other process.
type Bus struct {
	Local  *Local
	client *redis.Client
	log    *slog.Logger
}

// NewBusFromEnv connects to REDIS_URL; without it the bus purges this
// process only.
func NewBusFromEnv(log *slog.Logger) *Bus {
	if log == nil {
		log = slog.Default()
	}
	b := &Bus{Local: &Local{}, log: log.With("component", "forget")}
	if url := os.Getenv("REDIS_URL"); url != "" {
		if opts, err := redis.ParseURL(url); err == nil {
			b.client = redis.NewClient(opts)
		}
	}
	return b
}

// NewBus is a bus on a client (nil: this process only).
func NewBus(client *redis.Client, log *slog.Logger) *Bus {
	if log == nil {
		log = slog.Default()
	}
	return &Bus{Local: &Local{}, client: client, log: log.With("component", "forget")}
}

// Purge implements Purger.
func (b *Bus) Purge(ctx context.Context, spaceID uuid.UUID) {
	if b == nil {
		return
	}
	b.Local.Purge(ctx, spaceID)
	if b.client == nil {
		return
	}
	if err := b.client.Publish(ctx, Channel, spaceID.String()).Err(); err != nil {
		// The copies expire on their own (10 minutes); log so it is seen.
		b.log.WarnContext(ctx, "forget: cache purge signal failed", "space_id", spaceID.String(), "error", err)
	}
}

// Listen purges this process's caches whenever another process signals,
// until ctx ends.
func (b *Bus) Listen(ctx context.Context) {
	if b == nil || b.client == nil {
		return
	}
	sub := b.client.Subscribe(ctx, Channel)
	go func() {
		defer func() { _ = sub.Close() }()
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				id, err := uuid.Parse(msg.Payload)
				if err != nil {
					continue
				}
				b.Local.Purge(ctx, id)
			}
		}
	}()
}

// Close closes the Redis client.
func (b *Bus) Close() error {
	if b == nil || b.client == nil {
		return nil
	}
	return b.client.Close()
}

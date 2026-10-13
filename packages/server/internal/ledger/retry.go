package ledger

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// The backstop behind the lock order (locks.go): a transaction Postgres
// aborts for a deadlock or a serialization failure, or a command whose
// memories moved while it locked them, runs again from the start.
//
// That is safe only because each such transaction is all there is to it:
// Apply's writer, the bookkeeping writes (writeBookkeeping), an import and
// the index's store only send statements in their transaction, and River's
// insert and the finishers (river.JobCompleteTx) are statements in it too.
// Nothing reaches the network, a cache or the reads recorder before COMMIT:
// the compile job stores its artifact before Apply and drops it after, the
// Forget's cache purge and object-storage copies are the forget_propagate
// job's, and reads are recorded after the response. A retry starts from a
// new writer (its jobs, finishers, undo journal and locks empty), so the
// failed attempt leaves nothing behind.

// DefaultCommandAttempts bounds how often one command runs.
const DefaultCommandAttempts = 3

// The SQLSTATEs a retry answers.
const (
	sqlstateDeadlock      = "40P01"
	sqlstateSerialization = "40001"
)

// retryCode is why err may be retried ("" when it may not): 40P01, 40001,
// or locks_moved.
func retryCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errLocksMoved) {
		return "locks_moved"
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == sqlstateDeadlock || pg.Code == sqlstateSerialization) {
		return pg.Code
	}
	return ""
}

// retryMeter counts retries (no-op when OTel isn't configured).
func retryMeter() metric.Int64Counter {
	c, _ := otel.Meter("memax.ledger").Int64Counter("memax.ledger.retries",
		metric.WithDescription("Ledger transactions run again after a deadlock (40P01), a serialization failure (40001) "+
			"or memories that moved while a command locked them (locks_moved), by command and code"))
	return c
}

// retry runs fn, one transaction, up to l.attempts times while it fails
// in a way retryCode answers, with a few milliseconds' jitter between
// attempts. The first retry of a command is logged at warn (command and
// code, never words); a command that runs out of attempts is logged at
// error and answers ErrBusy, wrapping the last error, so callers say "try
// again in a moment" as they do for a lock timeout.
func (l *Ledger) retry(ctx context.Context, name string, fn func() error) error {
	attempts := max(l.attempts, 1)
	for attempt := 1; ; attempt++ {
		err := fn()
		code := retryCode(err)
		switch {
		case code == "":
			return err
		case attempt >= attempts:
			if attempts > 1 {
				l.log.Error("ledger: a command failed on every attempt", "command", name, "code", code, "attempts", attempt)
			}
			if errors.Is(err, ErrBusy) {
				return err
			}
			return fmt.Errorf("%w: %w", ErrBusy, err)
		case ctx.Err() != nil:
			return err
		}
		l.retries.Add(ctx, 1, metric.WithAttributes(attribute.String("command", name), attribute.String("code", code)))
		if attempt == 1 {
			l.log.Warn("ledger: retrying a command", "command", name, "code", code, "attempt", attempt+1)
		} else {
			l.log.Debug("ledger: retrying a command", "command", name, "code", code, "attempt", attempt+1)
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(retryWait(attempt)):
		}
	}
}

// retryWait is the pause before attempt+1: 2 to 12 ms, times the attempt,
// so two transactions that collided don't meet again in step.
func retryWait(attempt int) time.Duration {
	return time.Duration(attempt) * (2*time.Millisecond + rand.N(10*time.Millisecond))
}

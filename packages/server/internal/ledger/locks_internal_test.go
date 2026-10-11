package ledger

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// A command locks its memories once: after its round, a lock of a memory
// it doesn't hold is refused before anything is sent (a second round
// could close a cycle with a command that locked in the other order), and
// a second round through lockSet is a bug.
func TestOneRoundOfMemoryLocks(t *testing.T) {
	held, other := uuid.New(), uuid.New()
	w := &writer{command: CommandForget, locks: &memoryLocks{}}
	w.locks.hold(held)
	if _, err := w.lockMemories(context.Background(), []uuid.UUID{held, other}); !errors.Is(err, errLocksMoved) || !errors.Is(err, ErrBusy) {
		t.Errorf("a memory it doesn't hold: %v, want errLocksMoved (ErrBusy)", err)
	}
	if _, _, err := w.lockSet(context.Background(), "", []uuid.UUID{other}, lockAlso{}); err == nil ||
		!strings.Contains(err.Error(), "locked memories twice") {
		t.Errorf("a second round: %v", err)
	}
}

func TestRetryCode(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, ""},
		{errors.New("boom"), ""},
		{ErrEditClash, ""},
		{fmt.Errorf("ledger: mark targets dirty: %w", &pgconn.PgError{Code: "40P01"}), "40P01"},
		{&pgconn.PgError{Code: "40001"}, "40001"},
		{&pgconn.PgError{Code: "55P03"}, ""}, // a lock timeout is ErrBusy, not a retry
		{fmt.Errorf("x: %w", errLocksMoved), "locks_moved"},
	} {
		if got := retryCode(tc.err); got != tc.want {
			t.Errorf("retryCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// retry runs a transaction again on a deadlock, at most attempts times,
// and answers ErrBusy (wrapping the last error) when they run out; any
// other error ends it at once.
func TestRetryBounds(t *testing.T) {
	l := &Ledger{log: slog.New(slog.NewTextHandler(io.Discard, nil)), attempts: DefaultCommandAttempts, retries: retryMeter()}
	deadlock := &pgconn.PgError{Code: sqlstateDeadlock}
	n := 0
	err := l.retry(context.Background(), "keep", func() error { n++; return deadlock })
	var pg *pgconn.PgError
	if n != 3 || !errors.Is(err, ErrBusy) || !errors.As(err, &pg) || pg.Code != sqlstateDeadlock {
		t.Errorf("always deadlocked: %d attempts, %v", n, err)
	}
	n = 0
	if err := l.retry(context.Background(), "keep", func() error {
		if n++; n == 1 {
			return deadlock
		}
		return nil
	}); err != nil || n != 2 {
		t.Errorf("deadlocked once: %d attempts, %v", n, err)
	}
	n = 0
	if err := l.retry(context.Background(), "keep", func() error { n++; return ErrEditClash }); !errors.Is(err, ErrEditClash) || n != 1 {
		t.Errorf("a clash: %d attempts, %v", n, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n = 0
	if err := l.retry(ctx, "keep", func() error { n++; return deadlock }); n != 1 || !errors.As(err, &pg) {
		t.Errorf("a cancelled context: %d attempts, %v", n, err)
	}
}

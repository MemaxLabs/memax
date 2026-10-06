package ledger

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
)

// Errors Apply and the read functions return. Match them with
// errors.Is; the typed errors below carry details. A policy refusal is
// not an error: it is a Result with OutcomeRefused.
var (
	// ErrDisabled: the ledger has no database (New was given a nil pool).
	ErrDisabled = errors.New("ledger: not configured")
	// ErrInvalid: the command is malformed (see *ValidationError).
	ErrInvalid = errors.New("ledger: invalid command")
	// ErrNotFound: no such memory or space in the actor's scope.
	ErrNotFound = errors.New("ledger: not found")
	// ErrAmbiguousRef: a display ID matches memories in more than one
	// tenant of the scope; narrow the scope to one space or use the uuid.
	ErrAmbiguousRef = errors.New("ledger: display ID is ambiguous across spaces")
	// ErrEditClash: the memory changed since the caller read it (see
	// *EditClashError).
	ErrEditClash = errors.New("ledger: edit clash")
	// ErrInvalidTransition: the lifecycle doesn't allow the command in
	// the memory's current state (see *TransitionError).
	ErrInvalidTransition = errors.New("ledger: invalid transition")
	// ErrIdempotencyKeyReused: the key was already used for a different
	// command or different content.
	ErrIdempotencyKeyReused = errors.New("ledger: idempotency key reused for a different command")
	// ErrBusy: another transaction held the memory past the lock timeout.
	ErrBusy = errors.New("ledger: the memory is busy")
	// ErrReceiptRequired: the database refused a write without a receipt.
	// It means a bug in this package, never a user error.
	ErrReceiptRequired = errors.New("ledger: write refused without a receipt")
)

// ValidationError says which field is wrong and how to fix it.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

// Is makes errors.Is(err, ErrInvalid) match.
func (e *ValidationError) Is(target error) bool { return target == ErrInvalid }

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// EditClashError is returned when the expected version doesn't match.
type EditClashError struct {
	Ref      string
	Expected int
	Current  int
}

func (e *EditClashError) Error() string {
	return fmt.Sprintf("%s changed since you opened it (you had version %d, it is now version %d). Reload it and try again.",
		e.Ref, e.Expected, e.Current)
}

// Is makes errors.Is(err, ErrEditClash) match.
func (e *EditClashError) Is(target error) bool { return target == ErrEditClash }

// TransitionError wraps a lifecycle refusal with the memory's ref.
type TransitionError struct {
	Ref string
	Err *lifecycle.TransitionError
}

func (e *TransitionError) Error() string { return e.Ref + ": " + e.Err.Message }

// Is makes errors.Is(err, ErrInvalidTransition) match.
func (e *TransitionError) Is(target error) bool { return target == ErrInvalidTransition }

// Unwrap exposes the lifecycle error.
func (e *TransitionError) Unwrap() error { return e.Err }

// SQLSTATEs raised by the v2 schema's triggers (migration 028).
const (
	sqlstateReceiptRequired  = "MXR01"
	sqlstateReceiptImmutable = "MXR02"
	sqlstateLifecycle        = "MXL01"
	sqlstateUniqueViolation  = "23505"
	sqlstateLockNotAvailable = "55P03"
)

// mapDBError turns database refusals into the package's errors.
func mapDBError(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.Code {
	case sqlstateReceiptRequired:
		return fmt.Errorf("%w: %s", ErrReceiptRequired, pg.Message)
	case sqlstateReceiptImmutable:
		return fmt.Errorf("ledger: receipts are append-only: %s", pg.Message)
	case sqlstateLifecycle:
		return fmt.Errorf("%w: %s", ErrInvalidTransition, pg.Message)
	case sqlstateLockNotAvailable:
		return fmt.Errorf("%w: try again in a moment", ErrBusy)
	case sqlstateUniqueViolation:
		if pg.ConstraintName == "receipts_stream_version_key" {
			return fmt.Errorf("%w: another change landed first; reload and try again", ErrEditClash)
		}
	}
	return err
}

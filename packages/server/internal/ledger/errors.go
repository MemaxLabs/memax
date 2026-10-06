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
	// ErrAlreadyConnected: the credential already has an agent connection.
	ErrAlreadyConnected = errors.New("ledger: the credential is already connected")
	// ErrBehind: RecordCompile or SettleUnchanged found the target dirtied
	// again after the compiled generation; compile again (§5.7 step 3).
	ErrBehind = errors.New("ledger: the target changed while it compiled")
)

// TargetStateError is returned when a target's state doesn't allow the
// command: it is stopped, or it has no hand edit to resolve.
type TargetStateError struct {
	Ref     string
	Message string
}

func (e *TargetStateError) Error() string { return e.Ref + ": " + e.Message }

// Is makes errors.Is(err, ErrInvalidTransition) match.
func (e *TargetStateError) Is(target error) bool { return target == ErrInvalidTransition }

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

// InConflictError: Keep (or edit, then keep) on a proposal the judge
// flagged as contradicting a decision in force. It can't be kept until a
// person settles the conflict (ResolveConflict); With is the decision in
// the way, when the flag still has its link.
type InConflictError struct {
	Ref  string
	With string
}

func (e *InConflictError) Error() string {
	if e.With == "" {
		return fmt.Sprintf("%s contradicts a decision in force, so it can't be kept as it is. Settle the conflict first: compare both sides and choose.", e.Ref)
	}
	return fmt.Sprintf("%s contradicts %s, a decision in force, so it can't be kept as it is. Settle the conflict first: compare both sides and choose.", e.Ref, e.With)
}

// Is makes errors.Is(err, ErrInvalidTransition) match: the lifecycle
// refuses Keep in conflict, and older callers check for that.
func (e *InConflictError) Is(target error) bool { return target == ErrInvalidTransition }

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
	sqlstateAgentState       = "MXL02" // migration 029
	sqlstateGateStatus       = "MXL03" // migration 036
	sqlstateUniqueViolation  = "23505"
	sqlstateLockNotAvailable = "55P03"
	// jsonb refuses \u0000, and text refuses bytes outside the encoding.
	sqlstateUntranslatable = "22P05"
	sqlstateNotInRepertory = "22021"
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
	case sqlstateLifecycle, sqlstateAgentState, sqlstateGateStatus:
		return fmt.Errorf("%w: %s", ErrInvalidTransition, pg.Message)
	case sqlstateLockNotAvailable:
		return fmt.Errorf("%w: try again in a moment", ErrBusy)
	case sqlstateUntranslatable, sqlstateNotInRepertory:
		// A NUL (\u0000) inside a JSON field (a source's locator, the
		// conditions, the scope) reaches jsonb, which can't store it.
		return invalid("body", "contains a character that can't be stored (such as \\u0000); remove it and try again")
	case sqlstateUniqueViolation:
		switch pg.ConstraintName {
		case "receipts_stream_version_key":
			return fmt.Errorf("%w: another change landed first; reload and try again", ErrEditClash)
		case "agent_connections_credential_key":
			return ErrAlreadyConnected
		case "targets_path_key":
			return invalid("path", "the space already compiles to that path; change the existing target instead")
		case "briefs_space_key":
			return fmt.Errorf("%w: another Brief was written first; reload it and try again", ErrEditClash)
		}
	}
	return err
}

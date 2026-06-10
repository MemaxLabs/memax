package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// pgQuerier is the shared subset of *pgxpool.Pool and pgx.Tx that the
// store package's tx-aware helpers operate against. Defining it locally
// (pgx does not export one) lets a single SQL helper serve both the
// auto-commit and explicit-transaction call paths.
type pgQuerier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TxRunner is the narrow interface for code that needs to fan multiple
// writes into one transaction (the dreams producers in particular). It is
// intentionally separate from the broader Store interface so consumers
// only depend on the surface they actually need, and so adding new
// tx-aware methods does not expand Store for unrelated callers.
//
// Implemented by *PostgresStore (real transactions) and *InMemoryStore
// (no-op fallthroughs for tests).
type TxRunner interface {
	// WithTx begins a transaction, runs fn against it, and commits on
	// success. Any error from fn rolls back the transaction. The tx
	// passed to fn is non-nil for *PostgresStore and nil for
	// *InMemoryStore (which has no real transaction semantics).
	WithTx(ctx context.Context, fn func(pgx.Tx) error) error

	// CreateDreamActionTx writes a dream_actions row inside an existing
	// transaction.
	CreateDreamActionTx(ctx context.Context, tx pgx.Tx, action *model.DreamAction) error

	// UpdateDreamRunTx updates a dream_runs row inside an existing
	// transaction. Used when a producer needs the run lifecycle change
	// and its companion receipt to commit atomically.
	UpdateDreamRunTx(ctx context.Context, tx pgx.Tx, run *model.DreamRun) error

	// MergeTopicsTx merges sourceIDs into targetID inside an existing
	// transaction. See PostgresStore.MergeTopicsTx for the full
	// flow contract (validation, locking, cycle prevention, rehome,
	// reparent, delete). Returns the number of memories moved.
	MergeTopicsTx(ctx context.Context, tx pgx.Tx, hubID, targetID string, sourceIDs []string) (int, error)

	// ApplyTopicRestructureTx reparents childID under newParentID
	// inside an existing transaction. newParentID may be "" to promote
	// the child to a root topic. See PostgresStore.ApplyTopicRestructureTx
	// for the validation contract.
	ApplyTopicRestructureTx(ctx context.Context, tx pgx.Tx, hubID, childID, newParentID string) error

	// AcceptHubInviteByIDTx accepts an invite addressed to a known user
	// inside an existing transaction. Refuses email-only invites
	// (invitee_user_id NULL) and cross-user accept attempts. See
	// PostgresStore.AcceptHubInviteByIDTx for the contract.
	AcceptHubInviteByIDTx(ctx context.Context, tx pgx.Tx, inviteID, userID string, maxMembers int) (*model.HubInvite, error)

	// DeclineHubInviteByIDTx declines an invite addressed to a known
	// user inside an existing transaction. Reuses the revoke columns
	// (revoked_by = invitee, revoked_at = now()).
	DeclineHubInviteByIDTx(ctx context.Context, tx pgx.Tx, inviteID, userID string) error

	// CreateNotificationTx writes a notification row inside an
	// existing transaction. Used by Phase 3b producers to fan a
	// review / merge / invite write into a companion notification
	// within a single atomic boundary. See
	// PostgresStore.CreateNotificationTx for the upsert contract.
	CreateNotificationTx(ctx context.Context, tx pgx.Tx, n *model.Notification) error
}

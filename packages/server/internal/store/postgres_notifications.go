package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// ErrNotificationNotFound is returned when a notification lookup by id
// finds no row visible to the requester. Visibility follows §4.4 of
// docs/plans/17-inbox-notification-framework.md:
//
//   - audience in (hub, hub_member): visible to any current member of hub_id
//   - audience = user:                visible to the user in recipient_user_id
//
// A mismatch returns NotFound rather than Forbidden so the handler does
// not leak the existence of rows the caller cannot read.
var ErrNotificationNotFound = errors.New("notification not found")

// NotificationListOpts carries the query surface for
// ListNotificationsForUser. Mirrors the Phase 3b /v1/notifications list
// contract (plan §4.4) so when the handler wires up, the store call
// sites do not have to re-shape their arguments.
//
// Scope identity (UserID + HubIDs) is always required — the union of
// "hub rows in one of my hubs" and "user rows addressed to me" is the
// entire visibility surface for a given caller.
type NotificationListOpts struct {
	// UserID is the authenticated caller. Required.
	UserID string
	// HubIDs is the full set of hubs the caller is a current member
	// of. An empty slice means the caller only sees user-audience
	// rows addressed to them.
	HubIDs []string

	// HubID narrows the query to a single hub (must be in HubIDs).
	// When empty, the query spans every hub in HubIDs and every
	// user-audience row addressed to UserID.
	HubID string

	// Status filters to a lifecycle state. Zero value means "any
	// status." Plan §4.4 default behavior at the handler layer is
	// status=pending; the store honors whatever the caller passes.
	Status model.NotificationStatus

	// Kinds narrows to one or more notification kinds. Empty means
	// every kind the caller can see.
	Kinds []string

	// Resolution filters on the resolution discriminator. Only
	// meaningful when combined with Status=resolved.
	Resolutions []model.NotificationResolution

	// UnseenOnly filters to rows where seen_at IS NULL.
	UnseenOnly bool

	// Since lower-bounds created_at. Zero value means no lower bound.
	Since time.Time

	// Limit caps the page size. Zero means default (50); 500 is the
	// max for folding windows per §4.4.
	Limit int

	// Cursor is the opaque next-page token returned by a previous
	// call. Format: RFC3339 timestamp of the last row's created_at.
	Cursor string
}

// NotificationSummaryOpts is the query scope for
// GetNotificationSummary. Same identity as NotificationListOpts.
type NotificationSummaryOpts struct {
	UserID string
	HubIDs []string
	HubID  string
}

// notificationCols is the canonical SELECT clause used by every
// notification read path so scan helpers stay in lockstep.
const notificationCols = `id, audience, COALESCE(hub_id::text, ''), COALESCE(recipient_user_id::text, ''),
	COALESCE(hub_member_role, ''), kind, status, COALESCE(resolution::text, ''), priority,
	source_kind, COALESCE(source_id, ''), dream_run_id::text, COALESCE(payload, '{}'::jsonb), created_at,
	expires_at, resolved_at, seen_at`

// notificationVisibilityClause produces the WHERE predicate enforcing
// the §4.4 visibility rules. Two modes, selected by opts.HubID:
//
//  1. HubID empty (unscoped inbox view). Returns the union of
//     hub-axis rows in any hub the caller is a member of AND
//     audience is 'hub' or 'hub_member'
//     (`audience IN ('hub','hub_member') AND hub_id = ANY(HubIDs)`)
//     and user-axis rows addressed directly to the caller AND
//     audience is 'user'
//     (`audience = 'user' AND recipient_user_id = UserID`).
//     This is the "everything visible to me across every hub" view.
//
//  2. HubID set (single-hub scoped view). Returns rows tied to that
//     hub only:
//     - hub / hub_member rows where hub_id = HubID
//     - user-audience rows addressed to UserID AND carrying
//     hub_id = HubID as explicit hub context
//     This keeps the compact hub inbox aligned with decision rows or
//     receipts explicitly tied to the current hub, while still
//     excluding unrelated direct notifications from other hubs.
//     If the caller is not a current member of HubID the predicate
//     collapses to `1 = 0` so the result is an empty set — callers
//     get the same shape as any other "no matching rows" query
//     without leaking hub existence via a 403.
//
// Audience awareness is load-bearing: without it, any user-audience
// row that carries a hub_id for display context (e.g. a hub_invite
// notification addressed to a specific invitee but decorated with
// the target hub's id) would leak to every member of that hub via
// the hub axis match. The producer could avoid setting hub_id on
// user-audience rows, but the hub_id is useful for grouping and
// filtering in the UI, so the safer fix is to gate each axis on the
// matching audience enum value rather than trusting producers to
// leave routing fields blank.
//
// startParam is the next $N placeholder available to the caller.
// Returns the SQL fragment and the args it consumes, in the same order.
func notificationVisibilityClause(opts NotificationListOpts, startParam int) (string, []any) {
	// Single-hub mode: strict hub narrowing, audience-gated, user
	// axis excluded.
	if opts.HubID != "" {
		matched := false
		for _, id := range opts.HubIDs {
			if id == opts.HubID {
				matched = true
				break
			}
		}
		if !matched {
			// Not a member of the requested hub. Produce an empty
			// result set without a placeholder so the caller's args
			// layout stays predictable.
			return "1 = 0", nil
		}
		return fmt.Sprintf(
			"((audience IN ('hub','hub_member') AND hub_id = $%d::uuid) OR (audience = 'user' AND recipient_user_id = $%d::uuid AND hub_id = $%d::uuid))",
			startParam+1, startParam, startParam+1,
		), []any{opts.UserID, opts.HubID}
	}

	// Unscoped mode: union of (hub-audience ∧ hub_id match) and
	// (user-audience ∧ recipient match). Each axis is gated on the
	// notification_audience enum so a row can match one axis only —
	// a user-audience row with hub_id set for UI context does not
	// leak to hub members, and a hub-audience row does not leak to
	// users outside the hub via a stray recipient_user_id column.
	args := []any{opts.UserID}
	userPred := fmt.Sprintf(
		"(audience = 'user' AND recipient_user_id = $%d::uuid)",
		startParam,
	)
	if len(opts.HubIDs) == 0 {
		// Caller has no hub memberships, so they only see rows
		// addressed to them directly.
		return userPred, args
	}
	startParam++
	args = append(args, opts.HubIDs)
	hubPred := fmt.Sprintf(
		"(audience IN ('hub','hub_member') AND hub_id = ANY($%d::uuid[]))",
		startParam,
	)
	return fmt.Sprintf("(%s OR %s)", hubPred, userPred), args
}

// CreateNotification is the auto-commit entry point for notification
// writes. Producers that need a shared tx boundary (review resolution
// that also writes a notification in the same transaction) should call
// WithTx + CreateNotificationTx instead. Both paths go through
// createNotificationExec so the SQL has one source of truth.
func (s *PostgresStore) CreateNotification(n *model.Notification) error {
	return createNotificationExec(context.Background(), s.pool, n)
}

// CreateNotificationIfAbsent is the insert-only variant used by
// producers that need to know whether a fresh row was written — e.g.
// so the realtime `notification.created` event is only published on
// the first insert, not on every retry. Returns (true, nil) when the
// row was inserted, (false, nil) when an existing row for the same
// (source_kind, source_id) already existed, or (false, err) on a DB
// error.
func (s *PostgresStore) CreateNotificationIfAbsent(n *model.Notification) (bool, error) {
	return createNotificationInsertOnly(context.Background(), s.pool, n)
}

// CreateNotificationTx writes a notification row inside an existing
// transaction. Callers are expected to have already validated the
// audience/routing invariants.
func (s *PostgresStore) CreateNotificationTx(ctx context.Context, tx pgx.Tx, n *model.Notification) error {
	return createNotificationExec(ctx, tx, n)
}

// createNotificationExec is the shared SQL for both the auto-commit
// and transactional notification inserts. Uses INSERT ... ON CONFLICT
// against notifications_source_unique so a producer replay for the
// same (source_kind, source_id) pair is idempotent — matching the
// Phase 3b backfill contract.
func createNotificationExec(ctx context.Context, q pgQuerier, n *model.Notification) error {
	if n.ID == "" {
		return fmt.Errorf("notification: id is required")
	}
	if n.Audience == "" {
		return fmt.Errorf("notification: audience is required")
	}
	if n.Kind == "" {
		return fmt.Errorf("notification: kind is required")
	}
	if n.SourceKind == "" {
		return fmt.Errorf("notification: source_kind is required")
	}
	if n.Status == "" {
		n.Status = model.NotificationStatusPending
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	if err := validateSuperNotifPayloadIfApplicable(n); err != nil {
		return err
	}

	var hubID, recipientID, role, sourceID, resolution, dreamRunID any
	if n.HubID != "" {
		hubID = n.HubID
	}
	if n.RecipientUserID != "" {
		recipientID = n.RecipientUserID
	}
	if n.HubMemberRole != "" {
		role = n.HubMemberRole
	}
	if n.SourceID != "" {
		sourceID = n.SourceID
	}
	if n.Resolution != "" {
		resolution = string(n.Resolution)
	}
	if n.DreamRunID != nil && *n.DreamRunID != "" {
		dreamRunID = *n.DreamRunID
	}

	var payload any = []byte("{}")
	if len(n.Payload) > 0 {
		payload = []byte(n.Payload)
	}

	// ON CONFLICT preserves dream_run_id from the first producer —
	// an upsert (same source_kind+source_id) keeps the originating
	// run's ID. A later producer with a different dream_run_id does
	// not overwrite it, matching the idempotency contract: the first
	// run to detect this pair/suggestion owns the audit trail.
	_, err := q.Exec(ctx,
		`INSERT INTO notifications (
			id, audience, hub_id, recipient_user_id, hub_member_role,
			kind, status, resolution, priority, source_kind, source_id,
			dream_run_id,
			payload, created_at, expires_at, resolved_at, seen_at
		)
		VALUES (
			$1::uuid, $2::notification_audience, $3::uuid, $4::uuid, $5,
			$6, $7, $8::notification_resolution, $9, $10, $11,
			$12::uuid,
			$13::jsonb, $14, $15, $16, $17
		)
		ON CONFLICT (source_kind, source_id) DO UPDATE SET
			payload = EXCLUDED.payload,
			priority = EXCLUDED.priority,
			expires_at = EXCLUDED.expires_at`,
		n.ID, string(n.Audience), hubID, recipientID, role,
		n.Kind, string(n.Status), resolution, n.Priority, n.SourceKind, sourceID,
		dreamRunID,
		payload, n.CreatedAt, n.ExpiresAt, n.ResolvedAt, n.SeenAt)
	return err
}

// createNotificationInsertOnly runs the same insert as
// createNotificationExec but with ON CONFLICT DO NOTHING and a
// RETURNING clause that tells the caller whether a fresh row was
// written. Used by producers that need to suppress realtime
// "created" events on upsert-retry (see CreateNotificationIfAbsent).
func createNotificationInsertOnly(ctx context.Context, q pgQuerier, n *model.Notification) (bool, error) {
	if n.ID == "" {
		return false, fmt.Errorf("notification: id is required")
	}
	if n.Audience == "" {
		return false, fmt.Errorf("notification: audience is required")
	}
	if n.Kind == "" {
		return false, fmt.Errorf("notification: kind is required")
	}
	if n.SourceKind == "" {
		return false, fmt.Errorf("notification: source_kind is required")
	}
	if n.Status == "" {
		n.Status = model.NotificationStatusPending
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	if err := validateSuperNotifPayloadIfApplicable(n); err != nil {
		return false, err
	}

	var hubID, recipientID, role, sourceID, resolution, dreamRunID any
	if n.HubID != "" {
		hubID = n.HubID
	}
	if n.RecipientUserID != "" {
		recipientID = n.RecipientUserID
	}
	if n.HubMemberRole != "" {
		role = n.HubMemberRole
	}
	if n.SourceID != "" {
		sourceID = n.SourceID
	}
	if n.Resolution != "" {
		resolution = string(n.Resolution)
	}
	if n.DreamRunID != nil && *n.DreamRunID != "" {
		dreamRunID = *n.DreamRunID
	}

	var payload any = []byte("{}")
	if len(n.Payload) > 0 {
		payload = []byte(n.Payload)
	}

	var inserted string
	err := q.QueryRow(ctx,
		`INSERT INTO notifications (
			id, audience, hub_id, recipient_user_id, hub_member_role,
			kind, status, resolution, priority, source_kind, source_id,
			dream_run_id,
			payload, created_at, expires_at, resolved_at, seen_at
		)
		VALUES (
			$1::uuid, $2::notification_audience, $3::uuid, $4::uuid, $5,
			$6, $7, $8::notification_resolution, $9, $10, $11,
			$12::uuid,
			$13::jsonb, $14, $15, $16, $17
		)
		ON CONFLICT (source_kind, source_id) DO NOTHING
		RETURNING id::text`,
		n.ID, string(n.Audience), hubID, recipientID, role,
		n.Kind, string(n.Status), resolution, n.Priority, n.SourceKind, sourceID,
		dreamRunID,
		payload, n.CreatedAt, n.ExpiresAt, n.ResolvedAt, n.SeenAt,
	).Scan(&inserted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// ON CONFLICT DO NOTHING path — row already existed.
			return false, nil
		}
		return false, err
	}
	return inserted != "", nil
}

// ListNotificationsForUser returns the page of notifications visible
// to the caller (union of hub membership and direct user address).
// Ordered by created_at DESC; pagination via an RFC3339 cursor on
// created_at. Returns (rows, nextCursor, error). nextCursor is empty
// when there is no next page.
func (s *PostgresStore) ListNotificationsForUser(ctx context.Context, opts NotificationListOpts) ([]model.Notification, string, error) {
	if opts.UserID == "" {
		return nil, "", fmt.Errorf("notifications: user id is required")
	}
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	if opts.Limit > 500 {
		opts.Limit = 500
	}

	visPred, args := notificationVisibilityClause(opts, 1)
	var clauses []string
	clauses = append(clauses, visPred)

	if opts.Status != "" {
		args = append(args, string(opts.Status))
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if len(opts.Kinds) > 0 {
		args = append(args, opts.Kinds)
		clauses = append(clauses, fmt.Sprintf("kind = ANY($%d::text[])", len(args)))
	}
	if len(opts.Resolutions) > 0 {
		resStrs := make([]string, len(opts.Resolutions))
		for i, r := range opts.Resolutions {
			resStrs[i] = string(r)
		}
		args = append(args, resStrs)
		clauses = append(clauses, fmt.Sprintf("resolution = ANY($%d::notification_resolution[])", len(args)))
	}
	if opts.UnseenOnly {
		clauses = append(clauses, "seen_at IS NULL")
	}
	if !opts.Since.IsZero() {
		args = append(args, opts.Since)
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if opts.Cursor != "" {
		t, err := time.Parse(time.RFC3339Nano, opts.Cursor)
		if err != nil {
			return nil, "", fmt.Errorf("notifications: invalid cursor: %w", err)
		}
		args = append(args, t)
		clauses = append(clauses, fmt.Sprintf("created_at < $%d", len(args)))
	}

	args = append(args, opts.Limit+1) // fetch one extra to detect next page
	query := fmt.Sprintf(
		`SELECT %s FROM notifications WHERE %s ORDER BY created_at DESC LIMIT $%d`,
		notificationCols, strings.Join(clauses, " AND "), len(args),
	)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()

	notifs := make([]model.Notification, 0, opts.Limit)
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, "", err
		}
		notifs = append(notifs, n)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(notifs) > opts.Limit {
		nextCursor = notifs[opts.Limit-1].CreatedAt.Format(time.RFC3339Nano)
		notifs = notifs[:opts.Limit]
	}
	return notifs, nextCursor, nil
}

// GetNotification reads a single notification by id, enforcing the
// same visibility union as ListNotificationsForUser. Returns
// ErrNotificationNotFound when the row does not exist or is not visible.
func (s *PostgresStore) GetNotification(ctx context.Context, id string, userID string, hubIDs []string) (*model.Notification, error) {
	visPred, args := notificationVisibilityClause(NotificationListOpts{UserID: userID, HubIDs: hubIDs}, 1)
	args = append(args, id)
	query := fmt.Sprintf(
		`SELECT %s FROM notifications WHERE id = $%d::uuid AND (%s)`,
		notificationCols, len(args), visPred,
	)
	row := s.pool.QueryRow(ctx, query, args...)
	n, err := scanNotification(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotificationNotFound
		}
		return nil, err
	}
	return &n, nil
}

// GetNotificationBySourceKey resolves the unique
// (source_kind, source_id) pair to its notification row. Used
// by chat propose_topic_merge (and any future idempotent
// producer) to surface the existing row's id + status when
// CreateNotificationIfAbsent returns inserted=false. Without
// this, the producer can only echo back the locally-generated
// uuid (which doesn't identify the inbox row) and assume
// "pending" — even if the existing row was actually resolved
// or dismissed. Returns ErrNotificationNotFound when no row
// matches.
func (s *PostgresStore) GetNotificationBySourceKey(ctx context.Context, sourceKind, sourceID string) (*model.Notification, error) {
	if sourceKind == "" || sourceID == "" {
		return nil, fmt.Errorf("notifications: source_kind and source_id are required")
	}
	query := fmt.Sprintf(
		`SELECT %s FROM notifications WHERE source_kind = $1 AND source_id = $2`,
		notificationCols,
	)
	row := s.pool.QueryRow(ctx, query, sourceKind, sourceID)
	n, err := scanNotification(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotificationNotFound
		}
		return nil, err
	}
	return &n, nil
}

// ListNotificationsByDreamRunID returns every notification tagged
// with the given dream_run_id, ordered by creation time. Powers the
// admin dream-run detail view's "what did this cycle produce?"
// audit list. Uses the partial index
// idx_notifications_dream_run_id (migration 017) — the query path
// is cheap even as notifications grows.
func (s *PostgresStore) ListNotificationsByDreamRunID(ctx context.Context, runID string) ([]model.Notification, error) {
	if runID == "" {
		return nil, fmt.Errorf("notifications: dream run id is required")
	}
	query := fmt.Sprintf(
		`SELECT %s FROM notifications WHERE dream_run_id = $1::uuid ORDER BY created_at ASC`,
		notificationCols,
	)
	rows, err := s.pool.Query(ctx, query, runID)
	if err != nil {
		return nil, fmt.Errorf("list notifications by dream run id: %w", err)
	}
	defer rows.Close()

	notifs := make([]model.Notification, 0, 16)
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		notifs = append(notifs, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return notifs, nil
}

// GetNotificationSummary computes the canonical §4.4 summary shape.
// The by_kind map is pre-populated with zero counts for every entry
// in supportedNotificationKinds, then overwritten with the actual
// counts from a single GROUP BY query. This guarantees the contract
// shape "one entry per kind the current user can receive, including
// 0s" regardless of whether any rows exist for that kind yet —
// clients iterate by_kind[kind] without null-checking.
func (s *PostgresStore) GetNotificationSummary(ctx context.Context, opts NotificationSummaryOpts) (*model.NotificationSummary, error) {
	if opts.UserID == "" {
		return nil, fmt.Errorf("notifications: user id is required")
	}
	visPred, args := notificationVisibilityClause(NotificationListOpts{
		UserID: opts.UserID,
		HubIDs: opts.HubIDs,
		HubID:  opts.HubID,
	}, 1)

	// Pre-populate by_kind with zero counts for the canonical kind
	// set so the contract shape holds even when the caller has no
	// rows. Any kind returned by the SQL query that is NOT in the
	// canonical set is still included below (drift safety: a stray
	// row with an unknown kind is surfaced to the caller rather than
	// silently hidden).
	summary := &model.NotificationSummary{
		ByKind: make(map[string]model.NotificationKindCount, len(supportedNotificationKinds)),
	}
	for _, k := range supportedNotificationKinds {
		summary.ByKind[k] = model.NotificationKindCount{}
	}

	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`
			SELECT kind,
				COUNT(*) FILTER (WHERE status = 'pending') AS pending,
				COUNT(*) FILTER (WHERE status = 'pending' AND seen_at IS NULL) AS unseen
			FROM notifications
			WHERE %s
			GROUP BY kind
		`, visPred), args...)
	if err != nil {
		return nil, fmt.Errorf("notification summary: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var kind string
		var pending, unseen int
		if err := rows.Scan(&kind, &pending, &unseen); err != nil {
			return nil, err
		}
		summary.ByKind[kind] = model.NotificationKindCount{Pending: pending, Unseen: unseen}
		if notificationKindBucket(kind) == bucketNeedsAction {
			summary.NeedsActionPending += pending
		} else {
			summary.UpdatesPending += pending
			summary.UpdatesUnseen += unseen
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return summary, nil
}

// MarkNotificationSeen is idempotent: it writes seen_at = now() on a
// row the caller can see that does not already have seen_at set.
// Returns ErrNotificationNotFound when the row does not exist or is
// not visible; returns nil (with no row change) when the row is
// already seen. Status is untouched — read state lives in seen_at.
func (s *PostgresStore) MarkNotificationSeen(ctx context.Context, id string, userID string, hubIDs []string) error {
	visPred, args := notificationVisibilityClause(NotificationListOpts{UserID: userID, HubIDs: hubIDs}, 1)
	args = append(args, id)
	tag, err := s.pool.Exec(ctx,
		fmt.Sprintf(`
			UPDATE notifications
			SET seen_at = now()
			WHERE id = $%d::uuid AND (%s) AND seen_at IS NULL
		`, len(args), visPred), args...)
	if err != nil {
		return fmt.Errorf("mark notification seen: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Row already seen or not visible. Distinguish with a follow-up
		// SELECT so idempotent re-mark does not return NotFound.
		if _, err := s.GetNotification(ctx, id, userID, hubIDs); err != nil {
			return err
		}
	}
	return nil
}

// DismissNotification is the receipt-only dismiss path. Decision kinds
// (review_*, hub_invite) leave pending only via ResolveNotification;
// this endpoint is for receipts. Phase 3a does not enforce the decision
// vs receipt split at the store layer — the handler is the policy
// boundary per plan §6.4. The store accepts any id the caller can see.
func (s *PostgresStore) DismissNotification(ctx context.Context, id string, userID string, hubIDs []string) error {
	visPred, args := notificationVisibilityClause(NotificationListOpts{UserID: userID, HubIDs: hubIDs}, 1)
	args = append(args, id)
	tag, err := s.pool.Exec(ctx,
		fmt.Sprintf(`
			UPDATE notifications
			SET status = 'dismissed', seen_at = COALESCE(seen_at, now())
			WHERE id = $%d::uuid AND (%s) AND status = 'pending'
		`, len(args), visPred), args...)
	if err != nil {
		return fmt.Errorf("dismiss notification: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.GetNotification(ctx, id, userID, hubIDs); err != nil {
			return err
		}
	}
	return nil
}

// ResolveNotification writes status=resolved, resolved_at=now() and
// the discriminator resolution value. Every /resolve action lands in
// this one state, regardless of which action (including dismiss on a
// decision kind) — see plan §6.4 for the lifecycle rule.
func (s *PostgresStore) ResolveNotification(ctx context.Context, id string, userID string, hubIDs []string, resolution model.NotificationResolution) error {
	if resolution == "" {
		return fmt.Errorf("notifications: resolution is required")
	}
	visPred, args := notificationVisibilityClause(NotificationListOpts{UserID: userID, HubIDs: hubIDs}, 1)
	args = append(args, id, string(resolution))
	tag, err := s.pool.Exec(ctx,
		fmt.Sprintf(`
			UPDATE notifications
			SET status = 'resolved',
			    resolved_at = now(),
			    resolution = $%d::notification_resolution,
			    seen_at = COALESCE(seen_at, now())
			WHERE id = $%d::uuid AND (%s) AND status = 'pending'
		`, len(args), len(args)-1, visPred), args...)
	if err != nil {
		return fmt.Errorf("resolve notification: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.GetNotification(ctx, id, userID, hubIDs); err != nil {
			return err
		}
	}
	return nil
}

// BulkNotificationMutationOpts is the filter surface for the bulk
// mutation endpoints (/v1/notifications/seen + /dismiss). Mirrors
// plan §4.4: narrows to a user's visibility axis, optionally a single
// hub, an optional kind list, and an optional since cursor.
//
// Decision-kind refusal is the handler's policy boundary, not the
// store's — the store accepts any kind list the caller provides.
// Passing a decision kind here will mutate real rows, so the handler
// MUST validate the kind list before calling.
type BulkNotificationMutationOpts struct {
	UserID string
	HubIDs []string
	HubID  string
	Kinds  []string
	Since  time.Time
}

// BulkMarkNotificationsSeen writes seen_at=now() on every pending row
// visible to the caller that matches the filter and whose seen_at is
// currently NULL. Returns the rows it touched (id + routing fields
// only — not the full payload) so the handler can emit one
// notification.updated event per affected row per plan §4.5.
func (s *PostgresStore) BulkMarkNotificationsSeen(ctx context.Context, opts BulkNotificationMutationOpts) ([]model.Notification, error) {
	return s.bulkMutateNotifications(ctx, opts,
		`UPDATE notifications SET seen_at = now() WHERE %s AND seen_at IS NULL`,
	)
}

// BulkDismissNotifications writes status='dismissed' on every pending
// row visible to the caller that matches the filter. Returns the rows
// it touched so the handler can emit notification.updated events.
func (s *PostgresStore) BulkDismissNotifications(ctx context.Context, opts BulkNotificationMutationOpts) ([]model.Notification, error) {
	return s.bulkMutateNotifications(ctx, opts,
		`UPDATE notifications SET status = 'dismissed', seen_at = COALESCE(seen_at, now()) WHERE %s AND status = 'pending'`,
	)
}

// bulkMutateNotifications is the shared SQL engine for the bulk seen/
// dismiss paths. queryTemplate must contain one %s placeholder where
// the WHERE predicate goes. The UPDATE clauses in the callers above
// must include their own status/seen_at gate — the predicate below
// only enforces visibility + filter.
func (s *PostgresStore) bulkMutateNotifications(ctx context.Context, opts BulkNotificationMutationOpts, queryTemplate string) ([]model.Notification, error) {
	if opts.UserID == "" {
		return nil, fmt.Errorf("notifications: user id is required")
	}
	visPred, args := notificationVisibilityClause(NotificationListOpts{
		UserID: opts.UserID,
		HubIDs: opts.HubIDs,
		HubID:  opts.HubID,
	}, 1)

	var clauses []string
	clauses = append(clauses, visPred)
	if len(opts.Kinds) > 0 {
		args = append(args, opts.Kinds)
		clauses = append(clauses, fmt.Sprintf("kind = ANY($%d::text[])", len(args)))
	}
	if !opts.Since.IsZero() {
		args = append(args, opts.Since)
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}

	query := fmt.Sprintf(queryTemplate, strings.Join(clauses, " AND "))
	query += fmt.Sprintf(" RETURNING %s", notificationCols)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("bulk mutate notifications: %w", err)
	}
	defer rows.Close()
	out := []model.Notification{}
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ResolveNotificationsBySource flips every pending notification
// matching (source_kind, source_id) to status=resolved with the given
// resolution. Returns the affected rows so the caller can publish
// one notification.resolved event per row per plan §4.5.
//
// Unscoped by user/hub because it is a system-side-effect: it runs
// when a legacy token-based hub invite accept converges with the
// notification surface (handler/hubs.go AcceptInvite) or when any
// future producer needs to retire notifications by their natural
// source identity. Callers are responsible for their own authz.
// Safe to call when there are no matching rows (returns an empty
// slice, not an error).
func (s *PostgresStore) ResolveNotificationsBySource(ctx context.Context, sourceKind, sourceID string, resolution model.NotificationResolution) ([]model.Notification, error) {
	if sourceKind == "" || sourceID == "" {
		return nil, fmt.Errorf("notifications: source_kind and source_id are required")
	}
	if resolution == "" {
		return nil, fmt.Errorf("notifications: resolution is required")
	}
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`
			UPDATE notifications
			SET status = 'resolved',
			    resolved_at = now(),
			    resolution = $3::notification_resolution,
			    seen_at = COALESCE(seen_at, now())
			WHERE source_kind = $1
			  AND source_id = $2
			  AND status = 'pending'
			RETURNING %s
		`, notificationCols), sourceKind, sourceID, string(resolution))
	if err != nil {
		return nil, fmt.Errorf("resolve notifications by source: %w", err)
	}
	defer rows.Close()
	out := []model.Notification{}
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ExpireNotifications is the nightly sweep: flip every pending
// notification whose expires_at is past the given now to status=expired.
// Returns the affected rows (full row shape, including routing
// fields) so the worker can publish one notification.updated event
// per row with change=expired, matching plan §4.5's
// "one event per affected row" rule for lifecycle transitions.
func (s *PostgresStore) ExpireNotifications(ctx context.Context, now time.Time) ([]model.Notification, error) {
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`
			UPDATE notifications
			SET status = 'expired'
			WHERE status = 'pending'
			  AND expires_at IS NOT NULL
			  AND expires_at < $1
			RETURNING %s
		`, notificationCols), now)
	if err != nil {
		return nil, fmt.Errorf("expire notifications: %w", err)
	}
	defer rows.Close()
	out := []model.Notification{}
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// supportedNotificationKinds is the canonical set of kinds the summary
// endpoint pre-populates with zero counts. Every kind acknowledged by
// the inbox notification framework (docs/plans/17-inbox-notification-framework.md
// §3) appears here — including Phase 5 scaffold kinds that have no
// producer yet — so the by_kind shape is stable across the Phase 3a →
// 3b → 4 rollout. Clients can iterate by_kind[kind] without null
// checks because every supported kind is guaranteed to be present.
//
// Drift rule: every entry must ALSO appear in notificationKindBucket
// so NeedsActionPending / UpdatesPending stay correct when the kind
// eventually has rows. The drift-proofing test in
// postgres_notifications_test.go asserts both sides stay coherent.
//
// When adding a new kind: append here AND update
// notificationKindBucket in the same PR. Phase 3b will mirror this
// list in the shared TS package so the client summary iterator stays
// in lockstep.
var supportedNotificationKinds = []string{
	"review_contradiction",
	"review_topic_merge",
	"review_topic_restructure",
	"review_stale",
	"review_low_confidence",
	"dream_run_completed",
	"hub_invite",
	"hub_invite_accepted",
	"hub_invite_declined",
	"hub_invite_declined_by_you",
	"hub_member_joined",
	"hub_ownership_transfer",
	"hub_ownership_transferred",
	"hub_over_limit",
	"hub_frozen",
	"hub_restored",
	"system_notice",
	"gift_invite_link",
}

// notificationBucket is the internal split between the Needs-action
// and Updates buckets from plan §3.1. The authoritative definition for
// the client lives in the web renderer; the server mirrors it here so
// the summary endpoint can compute NeedsActionPending without the
// client having to sum ByKind entries itself.
type notificationBucket int

const (
	bucketUpdates notificationBucket = iota
	bucketNeedsAction
)

func notificationKindBucket(kind string) notificationBucket {
	switch kind {
	case "review_contradiction",
		"review_topic_merge",
		"review_topic_restructure",
		"review_low_confidence",
		"review_stale",
		"hub_invite",
		"hub_ownership_transfer":
		return bucketNeedsAction
	default:
		return bucketUpdates
	}
}

// scanNotification reads one row from a notification SELECT using the
// canonical notificationCols clause.
func scanNotification(row pgx.Row) (model.Notification, error) {
	var n model.Notification
	var audience, status, resolution string
	var dreamRunID *string
	var payload []byte
	if err := row.Scan(
		&n.ID,
		&audience,
		&n.HubID,
		&n.RecipientUserID,
		&n.HubMemberRole,
		&n.Kind,
		&status,
		&resolution,
		&n.Priority,
		&n.SourceKind,
		&n.SourceID,
		&dreamRunID,
		&payload,
		&n.CreatedAt,
		&n.ExpiresAt,
		&n.ResolvedAt,
		&n.SeenAt,
	); err != nil {
		return model.Notification{}, err
	}
	n.Audience = model.NotificationAudience(audience)
	n.Status = model.NotificationStatus(status)
	if resolution != "" {
		n.Resolution = model.NotificationResolution(resolution)
	}
	if dreamRunID != nil && *dreamRunID != "" {
		n.DreamRunID = dreamRunID
	}
	if len(payload) > 0 {
		n.Payload = payload
	}
	return n, nil
}

// QueryAdminNotificationBatches aggregates admin-sent notifications by
// batch (source_id prefix before ':'). The batch_id is extracted via
// split_part(source_id, ':', 1). UUIDs never contain ':' so this is safe.
func (s *PostgresStore) QueryAdminNotificationBatches(ctx context.Context, opts AdminNotifBatchOpts) ([]AdminNotifBatchRow, error) {
	if len(opts.SourceKinds) == 0 {
		return nil, nil
	}
	if opts.Limit <= 0 {
		opts.Limit = 51
	}

	args := []any{opts.SourceKinds}
	clauses := []string{"source_kind = ANY($1::text[])"}

	if opts.KindFilter != "" {
		args = append(args, opts.KindFilter)
		clauses = append(clauses, fmt.Sprintf("kind = $%d", len(args)))
	}
	if opts.Cursor != "" {
		t, err := time.Parse(time.RFC3339Nano, opts.Cursor)
		if err == nil {
			args = append(args, t)
			clauses = append(clauses, fmt.Sprintf("min_created < $%d", len(args)))
		}
	}

	args = append(args, opts.Limit)

	query := fmt.Sprintf(`
		SELECT
			split_part(source_id, ':', 1) AS batch_id,
			kind,
			min(created_at) AS min_created,
			count(*) AS recipient_count,
			(array_agg(payload ORDER BY created_at ASC))[1]::text AS first_payload
		FROM notifications
		WHERE %s
		  AND source_id LIKE '%%:%%'
		GROUP BY batch_id, kind
		ORDER BY min_created DESC
		LIMIT $%d
	`, strings.Join(clauses, " AND "), len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query admin notification batches: %w", err)
	}
	defer rows.Close()

	var result []AdminNotifBatchRow
	for rows.Next() {
		var row AdminNotifBatchRow
		var firstPayload string
		if err := rows.Scan(&row.BatchID, &row.Kind, &row.CreatedAt, &row.RecipientCount, &firstPayload); err != nil {
			return nil, err
		}
		row.PayloadPreview = extractPayloadPreview(firstPayload, row.Kind)
		result = append(result, row)
	}
	return result, rows.Err()
}

// extractPayloadPreview extracts a human-readable preview from the JSON payload.
func extractPayloadPreview(payload, kind string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		return ""
	}
	switch kind {
	case "system_notice":
		if t, ok := m["title"].(string); ok {
			return t
		}
		if b, ok := m["body"].(string); ok {
			if len(b) > 80 {
				return b[:80] + "..."
			}
			return b
		}
	case "gift_invite_link":
		if t, ok := m["token"].(string); ok {
			return "Gift: " + t
		}
	}
	return ""
}

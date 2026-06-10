package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// --- Errors raised by super-notif sub-item mutations (plan 18) ---

// ErrChecklistItemNotFound is returned when the supplied item_id does
// not appear in payload.items[]. Maps to HTTP 404.
var ErrChecklistItemNotFound = errors.New("notification item not found")

// ErrChecklistItemLocked is returned when /complete is called on a
// checklist item whose `locked_by` dependencies are not all complete.
// Maps to HTTP 400 item_locked. Plan 18 §4.3.
var ErrChecklistItemLocked = errors.New("notification item locked by unfinished dependencies")

// ErrInvalidNotificationKindForOp is returned when /complete is called
// on a non-checklist kind (e.g. a digest, which has no completion
// semantics). Maps to HTTP 400 kind_not_supported.
var ErrInvalidNotificationKindForOp = errors.New("notification kind does not support this operation")

// ErrChecklistItemPayloadInvalid is returned by the validators when a
// payload violates the contract from plan 18 §4.2. Producers MUST
// catch this before insert — the handler /resolve path also validates
// on read in case a corrupt row slipped through historical writes.
var ErrChecklistItemPayloadInvalid = errors.New("invalid super-notif payload")

// ErrNotificationNotPending is returned by the item-mutation paths
// when the parent notification is no longer pending (resolved /
// dismissed / expired). Item state is frozen at lifecycle exit;
// allowing mutations on archived rows would emit stray item_updated
// events for terminal rows clients have already retired.
var ErrNotificationNotPending = errors.New("notification not pending")

// ItemMutationResult is the post-update snapshot returned by the
// /view and /complete store methods. The handler consumes this to emit
// the `notification.updated` SSE event (plan §4.5) without a second
// fetch, and to decide whether to fire TryAutoResolveChecklist.
//
// AllRequiredDone reports the current state of the row — true when
// every entry in RequiredIDs has CompletedAt set, regardless of
// whether this call was the one that stamped it. Idempotent re-fires
// preserve the value so a transient resolve failure on the first call
// can be retried by a subsequent /complete (codex pass 1 High 2).
type ItemMutationResult struct {
	ItemID          string
	ViewedAt        *time.Time
	CompletedAt     *time.Time
	Progress        *model.ItemProgress
	AllRequiredDone bool
}

// --- Pure validators (no DB, safe to call from anywhere) ---

// maxSuperNotifItems caps the payload-side items[] slice. JSONB write
// atomicity gets expensive past this; 20 also bounds the rendered
// checklist height to something reasonable on mobile.
const maxSuperNotifItems = 20

// ValidateChecklistPayload enforces plan 18 §4.2 invariants:
//   - Title non-empty.
//   - 1 ≤ len(Items) ≤ 20 — empty checklists make no sense and the
//     20 cap matches the JSONB write budget.
//   - Every Items[].ID non-empty AND globally unique within the slice.
//   - Every RequiredIDs entry corresponds to some Items[].ID.
//   - Every LockedBy entry on every item corresponds to some Items[].ID
//     AND does not include the item's own ID (no self-locks).
//
// Returns nil on success. On violation wraps ErrChecklistItemPayloadInvalid
// with a human-readable cause so the handler can surface it as
// `400 invalid_checklist_payload`.
func ValidateChecklistPayload(p model.ChecklistPayload) error {
	if p.Title == "" {
		return fmt.Errorf("%w: title required", ErrChecklistItemPayloadInvalid)
	}
	if len(p.Items) == 0 {
		return fmt.Errorf("%w: items[] cannot be empty", ErrChecklistItemPayloadInvalid)
	}
	if len(p.Items) > maxSuperNotifItems {
		return fmt.Errorf("%w: items[] cap is %d, got %d",
			ErrChecklistItemPayloadInvalid, maxSuperNotifItems, len(p.Items))
	}
	ids := make(map[string]struct{}, len(p.Items))
	for i, it := range p.Items {
		if it.ID == "" {
			return fmt.Errorf("%w: items[%d].id is empty", ErrChecklistItemPayloadInvalid, i)
		}
		if _, dup := ids[it.ID]; dup {
			return fmt.Errorf("%w: items[].id %q duplicated", ErrChecklistItemPayloadInvalid, it.ID)
		}
		ids[it.ID] = struct{}{}
	}
	for _, rid := range p.RequiredIDs {
		if _, ok := ids[rid]; !ok {
			return fmt.Errorf("%w: required_ids[%q] not in items[].id",
				ErrChecklistItemPayloadInvalid, rid)
		}
	}
	for _, it := range p.Items {
		for _, dep := range it.LockedBy {
			if dep == it.ID {
				return fmt.Errorf("%w: items[%q] self-locks", ErrChecklistItemPayloadInvalid, it.ID)
			}
			if _, ok := ids[dep]; !ok {
				return fmt.Errorf("%w: items[%q].locked_by references unknown %q",
					ErrChecklistItemPayloadInvalid, it.ID, dep)
			}
		}
	}
	return nil
}

// validateSuperNotifPayloadIfApplicable is the create-path hook that
// fans out to the per-kind payload validator. Called by
// createNotificationExec + createNotificationInsertOnly so a malformed
// checklist or digest is refused at INSERT time, not at first /complete.
//
// Non-super-notif kinds skip validation here; their producers do their
// own per-kind shape checks at the call site.
func validateSuperNotifPayloadIfApplicable(n *model.Notification) error {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case model.NotificationKindChecklist:
		var p model.ChecklistPayload
		if err := json.Unmarshal(n.Payload, &p); err != nil {
			return fmt.Errorf("%w: %v", ErrChecklistItemPayloadInvalid, err)
		}
		return ValidateChecklistPayload(p)
	case model.NotificationKindDigest:
		var p model.DigestPayload
		if err := json.Unmarshal(n.Payload, &p); err != nil {
			return fmt.Errorf("%w: %v", ErrChecklistItemPayloadInvalid, err)
		}
		return ValidateDigestPayload(p)
	default:
		return nil
	}
}

// ValidateDigestPayload mirrors ValidateChecklistPayload minus the
// checklist-only fields (required_ids, locked_by).
func ValidateDigestPayload(p model.DigestPayload) error {
	if p.Title == "" {
		return fmt.Errorf("%w: title required", ErrChecklistItemPayloadInvalid)
	}
	if len(p.Items) == 0 {
		return fmt.Errorf("%w: items[] cannot be empty", ErrChecklistItemPayloadInvalid)
	}
	if len(p.Items) > maxSuperNotifItems {
		return fmt.Errorf("%w: items[] cap is %d, got %d",
			ErrChecklistItemPayloadInvalid, maxSuperNotifItems, len(p.Items))
	}
	ids := make(map[string]struct{}, len(p.Items))
	for i, it := range p.Items {
		if it.ID == "" {
			return fmt.Errorf("%w: items[%d].id is empty", ErrChecklistItemPayloadInvalid, i)
		}
		if _, dup := ids[it.ID]; dup {
			return fmt.Errorf("%w: items[].id %q duplicated", ErrChecklistItemPayloadInvalid, it.ID)
		}
		ids[it.ID] = struct{}{}
	}
	return nil
}

// --- Public mutations ---

// ViewNotificationItem implements §4.3 /items/{item_id}/view. Idempotent.
func (s *PostgresStore) ViewNotificationItem(ctx context.Context, id, userID string, hubIDs []string, itemID string) (*ItemMutationResult, error) {
	var result *ItemMutationResult
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		row, err := selectNotificationForUpdate(ctx, tx, id, userID, hubIDs)
		if err != nil {
			return err
		}
		r, perr := applyItemView(ctx, tx, row, itemID, time.Now().UTC())
		if perr != nil {
			return perr
		}
		result = r
		return nil
	})
	return result, err
}

// CompleteNotificationItem implements §4.3 /items/{item_id}/complete.
// Refuses non-checklist kinds, locked items, and already-terminal rows.
// Idempotent.
//
// Returns AllRequiredDone reflecting the CURRENT row state — true when
// every required item has CompletedAt set, regardless of whether this
// call performed the transition. The handler reads the flag and calls
// TryAutoResolveChecklist, which atomically gates the actual flip so
// idempotent re-fires are safe (codex pass 1 High 1+2).
func (s *PostgresStore) CompleteNotificationItem(ctx context.Context, id, userID string, hubIDs []string, itemID string) (*ItemMutationResult, error) {
	var result *ItemMutationResult
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		row, err := selectNotificationForUpdate(ctx, tx, id, userID, hubIDs)
		if err != nil {
			return err
		}
		r, perr := applyItemComplete(ctx, tx, row, itemID, time.Now().UTC())
		if perr != nil {
			return perr
		}
		result = r
		return nil
	})
	return result, err
}

// Recorder note (plan 18 §4.6): the OnboardingRecorder calls
// CompleteNotificationItem directly with the userID it already has
// from the outer handler (memory.Create / ask / configs.Sync /
// hubs.Create+Accept / dream completion). It does NOT use a separate
// unscoped method — the §4.4 visibility predicate is defense in depth
// against a bug where a Recorder path lookup returns a stale id from
// a different user. The Recorder reads the target notification ID via
// LatestPendingChecklistByRecipient(userID, sourceKind) so the lookup
// itself filters by recipient; passing the same userID into the
// mutation gives us two independent checks of the same invariant.

// TryAutoResolveChecklist is the atomic auto-resolve primitive used by
// the /complete handler (and, in P2, the Recorder hot paths) after a
// checklist item flip leaves every required_ids item complete.
//
// Atomicity contract:
//   - Acquires the row lock under WithTx + SELECT FOR UPDATE.
//   - Re-verifies the row is still pending AND the live payload still
//     has every required item complete. (The /complete write committed
//     its tx already, but a concurrent /resolve {dismiss} can sneak in
//     between the two transactions.)
//   - Writes status=resolved + resolution=applied_auto + resolved_at iff
//     both checks pass; returns flipped=true.
//   - Otherwise returns flipped=false with the live row state — the
//     handler reads `post.Resolution` to decide what to advertise.
//
// Idempotent: a second call after a successful flip sees
// status=resolved and returns flipped=false. The retry path (High 2
// from codex pass 1) is therefore safe to invoke from idempotent
// /complete re-fires.
func (s *PostgresStore) TryAutoResolveChecklist(ctx context.Context, id, userID string, hubIDs []string) (bool, *model.Notification, error) {
	var (
		flipped bool
		post    *model.Notification
	)
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		visPred, args := notificationVisibilityClause(NotificationListOpts{UserID: userID, HubIDs: hubIDs}, 1)
		args = append(args, id)
		row := tx.QueryRow(ctx, fmt.Sprintf(`
			SELECT %s
			  FROM notifications
			 WHERE id = $%d::uuid AND (%s)
			 FOR UPDATE
		`, notificationCols, len(args), visPred), args...)
		n, scanErr := scanNotification(row)
		if scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return ErrNotificationNotFound
			}
			return fmt.Errorf("try auto-resolve: lock: %w", scanErr)
		}
		post = &n
		if n.Kind != model.NotificationKindChecklist {
			return ErrInvalidNotificationKindForOp
		}
		if n.Status != model.NotificationStatusPending {
			// Concurrent dismiss / expire — the row already exited
			// pending. Caller decides whether to surface the loss-of-
			// race via the post pointer.
			return nil
		}
		var payload model.ChecklistPayload
		if uerr := json.Unmarshal(n.Payload, &payload); uerr != nil {
			return fmt.Errorf("try auto-resolve: decode payload: %w", uerr)
		}
		// Re-verify every required item is complete on the live row,
		// not just the snapshot the caller saw. Defends against a race
		// where two /complete requests on adjacent required items race
		// each other and both think they were the last one.
		if len(payload.RequiredIDs) == 0 {
			return nil
		}
		for _, rid := range payload.RequiredIDs {
			ridx := findChecklistItemIdx(payload.Items, rid)
			if ridx < 0 || payload.Items[ridx].CompletedAt == nil {
				return nil
			}
		}
		if _, uerr := tx.Exec(ctx, `
			UPDATE notifications
			   SET status = 'resolved',
			       resolution = $2::notification_resolution,
			       resolved_at = now(),
			       seen_at = COALESCE(seen_at, now())
			 WHERE id = $1::uuid AND status = 'pending'
		`, n.ID, string(model.ResolutionAppliedAuto)); uerr != nil {
			return fmt.Errorf("try auto-resolve: update: %w", uerr)
		}
		flipped = true
		return nil
	})
	if err != nil {
		return false, post, err
	}
	if flipped {
		// Re-read so the caller sees the freshly-stamped resolution,
		// resolved_at, and seen_at for the SSE envelope. If the reread
		// fails (e.g. a transient pool error after the tx committed),
		// fall back to synthesizing those fields on the pre-update
		// snapshot — we know exactly what the row looks like now
		// because we wrote it. Codex pass 2 medium: never publish a
		// notification.resolved event whose snapshot is missing the
		// applied_auto resolution.
		fresh, gerr := s.GetNotification(ctx, id, userID, hubIDs)
		if gerr == nil {
			post = fresh
		} else if post != nil {
			now := time.Now().UTC()
			post.Status = model.NotificationStatusResolved
			post.Resolution = model.ResolutionAppliedAuto
			post.ResolvedAt = &now
			if post.SeenAt == nil {
				post.SeenAt = &now
			}
		}
	}
	return flipped, post, nil
}

// UpdateChecklistItemProgress patches payload.items[itemID].progress
// on a pending checklist row WITHOUT completing the item. Used by the
// recorder's RecordMemoryCreate to surface live "1/5", "2/5", ... on
// the five_memories item before the threshold fires (codex P2 M2).
//
// Idempotent: a repeat call with the same {current, target} writes
// the same JSONB and returns the unchanged snapshot.
//
// Refuses non-checklist kinds with ErrInvalidNotificationKindForOp.
// Refuses terminal rows with ErrNotificationNotPending. Returns
// ErrChecklistItemNotFound when item_id is absent.
func (s *PostgresStore) UpdateChecklistItemProgress(ctx context.Context, id, userID string, hubIDs []string, itemID string, current, target int) (*ItemMutationResult, error) {
	var result *ItemMutationResult
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		row, err := selectNotificationForUpdate(ctx, tx, id, userID, hubIDs)
		if err != nil {
			return err
		}
		if row.Kind != model.NotificationKindChecklist {
			return ErrInvalidNotificationKindForOp
		}
		var payload model.ChecklistPayload
		if uerr := json.Unmarshal(row.Payload, &payload); uerr != nil {
			return fmt.Errorf("update progress: decode: %w", uerr)
		}
		idx := findChecklistItemIdx(payload.Items, itemID)
		if idx < 0 {
			return ErrChecklistItemNotFound
		}
		// Don't downgrade progress past the target — once the item
		// completes, leave the {current, target} snapshot in place.
		if payload.Items[idx].CompletedAt != nil {
			result = &ItemMutationResult{
				ItemID:      itemID,
				ViewedAt:    payload.Items[idx].ViewedAt,
				CompletedAt: payload.Items[idx].CompletedAt,
				Progress:    payload.Items[idx].Progress,
			}
			return nil
		}
		payload.Items[idx].Progress = &model.ItemProgress{
			Current: current,
			Target:  target,
		}
		if werr := writeNotificationPayload(ctx, tx, row.ID, payload); werr != nil {
			return werr
		}
		result = &ItemMutationResult{
			ItemID:   itemID,
			ViewedAt: payload.Items[idx].ViewedAt,
			Progress: payload.Items[idx].Progress,
		}
		return nil
	})
	return result, err
}

// SuperNotifFunnelRow is a single status/resolution bucket in the
// parent funnel. Plan 18 §7.3 GROUP BY (status, resolution).
type SuperNotifFunnelRow struct {
	Status     string
	Resolution string // empty for non-resolved rows
	Count      int
}

// SuperNotifItemFunnelRow is a single (cohort_day, item_id) row in
// the item-level funnel. Plan 18 §7.3 second panel.
type SuperNotifItemFunnelRow struct {
	CohortDay time.Time
	ItemID    string
	Recipient int // total rows in this cohort that contain this item id
	Viewed    int
	Completed int
}

// QuerySuperNotifParentFunnel returns (status, resolution) → count
// for every checklist/digest row in the cohort window. Used by the
// admin /admin/notifications/super page (plan 18 §7.3).
//
// Backed by the notifications_super_cohort_idx partial index added
// in migration 012.
func (s *PostgresStore) QuerySuperNotifParentFunnel(ctx context.Context, kind, sourceKind string, since time.Time) ([]SuperNotifFunnelRow, error) {
	if kind == "" {
		return nil, fmt.Errorf("QuerySuperNotifParentFunnel: kind required")
	}
	args := []any{kind, since}
	srcFilter := ""
	if sourceKind != "" {
		srcFilter = " AND source_kind = $3"
		args = append(args, sourceKind)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT status, COALESCE(resolution::text, ''), COUNT(*)
		   FROM notifications
		  WHERE kind = $1
		    AND created_at >= $2`+srcFilter+`
		  GROUP BY status, resolution
		  ORDER BY status, resolution`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("QuerySuperNotifParentFunnel: %w", err)
	}
	defer rows.Close()
	out := []SuperNotifFunnelRow{}
	for rows.Next() {
		var r SuperNotifFunnelRow
		if err := rows.Scan(&r.Status, &r.Resolution, &r.Count); err != nil {
			return nil, fmt.Errorf("QuerySuperNotifParentFunnel: scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// QuerySuperNotifItemFunnel returns per-item (recipients, viewed,
// completed) counts grouped by cohort_day. Plan 18 §7.3 item-funnel
// query — one JSONB expansion via jsonb_array_elements.
//
// Performance: under 50k super-notif rows this runs in under 100ms
// per RFC. A GIN index on payload->'items' is the future escape
// hatch when item-level filter queries become common.
func (s *PostgresStore) QuerySuperNotifItemFunnel(ctx context.Context, kind, sourceKind string, since time.Time) ([]SuperNotifItemFunnelRow, error) {
	if kind == "" {
		return nil, fmt.Errorf("QuerySuperNotifItemFunnel: kind required")
	}
	args := []any{kind, since}
	srcFilter := ""
	if sourceKind != "" {
		srcFilter = " AND n.source_kind = $3"
		args = append(args, sourceKind)
	}
	rows, err := s.pool.Query(ctx,
		`WITH items AS (
		   SELECT
		     n.created_at::date                              AS cohort_day,
		     (item.value->>'id')                              AS item_id,
		     (item.value->>'viewed_at')     IS NOT NULL       AS viewed,
		     (item.value->>'completed_at')  IS NOT NULL       AS completed
		   FROM notifications n,
		        jsonb_array_elements(n.payload->'items') item
		  WHERE n.kind = $1
		    AND n.created_at >= $2`+srcFilter+`
		 )
		 SELECT cohort_day,
		        item_id,
		        COUNT(*)::int                              AS recipients,
		        COUNT(*) FILTER (WHERE viewed)::int        AS viewed,
		        COUNT(*) FILTER (WHERE completed)::int     AS completed
		   FROM items
		  GROUP BY cohort_day, item_id
		  ORDER BY cohort_day DESC, item_id`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("QuerySuperNotifItemFunnel: %w", err)
	}
	defer rows.Close()
	out := []SuperNotifItemFunnelRow{}
	for rows.Next() {
		var r SuperNotifItemFunnelRow
		if err := rows.Scan(&r.CohortDay, &r.ItemID, &r.Recipient, &r.Viewed, &r.Completed); err != nil {
			return nil, fmt.Errorf("QuerySuperNotifItemFunnel: scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountChecklistsCreatedSince counts every checklist row this user
// has accumulated since `since`, regardless of status. Used by the
// restart-onboarding rate limit (plan 18 §6.2 — 3/day per user).
// Covers all transitions: a user who creates v2, dismisses, creates
// v3, dismisses, etc. is counted as 3 even though only one row is
// pending at a time.
func (s *PostgresStore) CountChecklistsCreatedSince(ctx context.Context, recipientUserID, sourceKind string, since time.Time) (int, error) {
	if recipientUserID == "" {
		return 0, fmt.Errorf("CountChecklistsCreatedSince: recipient required")
	}
	if sourceKind == "" {
		return 0, fmt.Errorf("CountChecklistsCreatedSince: source_kind required")
	}
	var count int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		   FROM notifications
		  WHERE kind = 'checklist'
		    AND source_kind = $1
		    AND recipient_user_id = $2::uuid
		    AND created_at >= $3`,
		sourceKind, recipientUserID, since).Scan(&count); err != nil {
		return 0, fmt.Errorf("CountChecklistsCreatedSince: %w", err)
	}
	return count, nil
}

// LatestChecklistByRecipient returns the most recent checklist row
// for the given recipient + source_kind regardless of status. Used by
// the restart-onboarding handler (plan 18 §6.2) to find the highest
// version `{user_id}:v{n}` already in the table so the new row gets
// `:v{n+1}`. Returns ErrNotificationNotFound when no row exists.
func (s *PostgresStore) LatestChecklistByRecipient(ctx context.Context, recipientUserID, sourceKind string) (*model.Notification, error) {
	if recipientUserID == "" {
		return nil, fmt.Errorf("LatestChecklistByRecipient: recipient required")
	}
	if sourceKind == "" {
		return nil, fmt.Errorf("LatestChecklistByRecipient: source_kind required")
	}
	row := s.pool.QueryRow(ctx,
		`SELECT `+notificationCols+`
		   FROM notifications
		  WHERE kind = 'checklist'
		    AND source_kind = $1
		    AND recipient_user_id = $2::uuid
		  ORDER BY created_at DESC
		  LIMIT 1`,
		sourceKind, recipientUserID)
	n, err := scanNotification(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotificationNotFound
		}
		return nil, fmt.Errorf("LatestChecklistByRecipient: %w", err)
	}
	return &n, nil
}

// LatestPendingChecklistByRecipient runs the partial-index-backed query
// from migration 012 that the OnboardingRecorder fires on every memory
// create / ask / sync / invite / dream completion. Returns
// ErrNotificationNotFound when no pending row exists — that's the hot
// path for users past onboarding and must stay fast.
func (s *PostgresStore) LatestPendingChecklistByRecipient(ctx context.Context, recipientUserID, sourceKind string) (*model.Notification, error) {
	if recipientUserID == "" {
		return nil, fmt.Errorf("LatestPendingChecklistByRecipient: recipient required")
	}
	if sourceKind == "" {
		return nil, fmt.Errorf("LatestPendingChecklistByRecipient: source_kind required")
	}
	row := s.pool.QueryRow(ctx,
		`SELECT `+notificationCols+`
		   FROM notifications
		  WHERE kind = 'checklist'
		    AND source_kind = $1
		    AND recipient_user_id = $2::uuid
		    AND status = 'pending'
		  ORDER BY created_at DESC
		  LIMIT 1`,
		sourceKind, recipientUserID)
	n, err := scanNotification(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotificationNotFound
		}
		return nil, fmt.Errorf("LatestPendingChecklistByRecipient: %w", err)
	}
	return &n, nil
}

// --- Internal helpers (tx-scoped) ---

// selectNotificationForUpdate locks the row + enforces the §4.4
// visibility predicate. Returns the parsed Notification, or
// ErrNotificationNotFound when invisible, or ErrNotificationNotPending
// when the caller is visible but the row has already exited pending.
//
// Splitting the two errors lets the handler map "invisible" → 404 and
// "terminal" → 400 with a distinct code, instead of conflating them.
func selectNotificationForUpdate(ctx context.Context, tx pgx.Tx, id, userID string, hubIDs []string) (*model.Notification, error) {
	visPred, args := notificationVisibilityClause(NotificationListOpts{UserID: userID, HubIDs: hubIDs}, 1)
	args = append(args, id)
	row := tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s
		  FROM notifications
		 WHERE id = $%d::uuid AND (%s)
		 FOR UPDATE
	`, notificationCols, len(args), visPred), args...)
	n, err := scanNotification(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotificationNotFound
		}
		return nil, fmt.Errorf("select notification for update: %w", err)
	}
	if n.Status != model.NotificationStatusPending {
		return nil, ErrNotificationNotPending
	}
	return &n, nil
}

// applyItemView mutates payload.items[itemID].viewed_at if currently
// nil, persists the new payload, and returns the snapshot. Works on
// both checklist and digest rows.
func applyItemView(ctx context.Context, tx pgx.Tx, n *model.Notification, itemID string, now time.Time) (*ItemMutationResult, error) {
	switch n.Kind {
	case model.NotificationKindChecklist:
		var payload model.ChecklistPayload
		if err := json.Unmarshal(n.Payload, &payload); err != nil {
			return nil, fmt.Errorf("apply view: decode checklist payload: %w", err)
		}
		idx := findChecklistItemIdx(payload.Items, itemID)
		if idx < 0 {
			return nil, ErrChecklistItemNotFound
		}
		if payload.Items[idx].ViewedAt == nil {
			t := now
			payload.Items[idx].ViewedAt = &t
		}
		if err := writeNotificationPayload(ctx, tx, n.ID, payload); err != nil {
			return nil, err
		}
		it := payload.Items[idx]
		return &ItemMutationResult{
			ItemID:      itemID,
			ViewedAt:    it.ViewedAt,
			CompletedAt: it.CompletedAt,
			Progress:    it.Progress,
		}, nil
	case model.NotificationKindDigest:
		var payload model.DigestPayload
		if err := json.Unmarshal(n.Payload, &payload); err != nil {
			return nil, fmt.Errorf("apply view: decode digest payload: %w", err)
		}
		idx := findDigestItemIdx(payload.Items, itemID)
		if idx < 0 {
			return nil, ErrChecklistItemNotFound
		}
		if payload.Items[idx].ViewedAt == nil {
			t := now
			payload.Items[idx].ViewedAt = &t
		}
		if err := writeNotificationPayload(ctx, tx, n.ID, payload); err != nil {
			return nil, err
		}
		it := payload.Items[idx]
		return &ItemMutationResult{
			ItemID:   itemID,
			ViewedAt: it.ViewedAt,
			Progress: it.Progress,
		}, nil
	default:
		// /view is permissive — any kind that ships items[] can support
		// it. For non-super-notif kinds (no items[]), there's nothing
		// to stamp so this is a logical no-op.
		return nil, ErrInvalidNotificationKindForOp
	}
}

// applyItemComplete is the checklist-only mutator. Refuses non-checklist
// kinds. Refuses locked items. Stamps completed_at + viewed_at, returns
// snapshot + AllRequiredDone for the auto-resolve gate.
//
// The decision/transition logic is factored into the pure helper
// computeChecklistItemComplete so unit tests can cover the state
// machine (idempotency, locked_by, all-required-done detection)
// without a database. This function is the DB-bound wrapper.
func applyItemComplete(ctx context.Context, tx pgx.Tx, n *model.Notification, itemID string, now time.Time) (*ItemMutationResult, error) {
	if n.Kind != model.NotificationKindChecklist {
		return nil, ErrInvalidNotificationKindForOp
	}
	var payload model.ChecklistPayload
	if err := json.Unmarshal(n.Payload, &payload); err != nil {
		return nil, fmt.Errorf("apply complete: decode payload: %w", err)
	}
	if err := ValidateChecklistPayload(payload); err != nil {
		// Belt-and-suspenders: refuse to operate on a corrupt row even
		// if the create-time validator missed something historical.
		return nil, err
	}
	mutated, result, idempotent, err := computeChecklistItemComplete(payload, itemID, now)
	if err != nil {
		return nil, err
	}
	if idempotent {
		// No payload change to persist.
		return result, nil
	}
	if err := writeNotificationPayload(ctx, tx, n.ID, mutated); err != nil {
		return nil, err
	}
	return result, nil
}

// computeChecklistItemComplete is the pure state-machine half of
// applyItemComplete. Returns the post-mutation payload (caller persists
// it), the snapshot the handler / SSE event needs, and an `idempotent`
// flag that's true when the item was already complete and no payload
// write is required.
//
// Pure (no DB, no clock — caller supplies `now`) so the locked_by gate,
// idempotency, and AllRequiredDone gate are unit-testable.
func computeChecklistItemComplete(payload model.ChecklistPayload, itemID string, now time.Time) (model.ChecklistPayload, *ItemMutationResult, bool, error) {
	idx := findChecklistItemIdx(payload.Items, itemID)
	if idx < 0 {
		return payload, nil, false, ErrChecklistItemNotFound
	}

	// Idempotency: if already complete, return current snapshot. Do
	// NOT suppress AllRequiredDone here — the retry path matters when
	// the first call's auto-resolve failed transiently (codex pass 1
	// High 2). TryAutoResolveChecklist gates the actual write under
	// FOR UPDATE + status='pending', so a re-fire after a successful
	// resolve is a safe no-op (flipped=false) and a re-fire after a
	// failed resolve correctly retries.
	if payload.Items[idx].CompletedAt != nil {
		it := payload.Items[idx]
		return payload, &ItemMutationResult{
			ItemID:          itemID,
			ViewedAt:        it.ViewedAt,
			CompletedAt:     it.CompletedAt,
			Progress:        it.Progress,
			AllRequiredDone: allRequiredItemsComplete(payload),
		}, true, nil
	}

	// locked_by gate: every dependency must already be complete.
	for _, dep := range payload.Items[idx].LockedBy {
		depIdx := findChecklistItemIdx(payload.Items, dep)
		if depIdx < 0 || payload.Items[depIdx].CompletedAt == nil {
			return payload, nil, false, ErrChecklistItemLocked
		}
	}

	t := now
	payload.Items[idx].CompletedAt = &t
	if payload.Items[idx].ViewedAt == nil {
		payload.Items[idx].ViewedAt = &t
	}
	// Codex P2 pass-2 M2 — when an item carries a progress signal,
	// completing it always stamps current = target so the SSE
	// snapshot and any subsequent /view never reports a partial
	// progress on a completed item (e.g. 4/5 on a 5-memories item
	// that just flipped). The store is the single source of truth.
	if payload.Items[idx].Progress != nil {
		payload.Items[idx].Progress.Current = payload.Items[idx].Progress.Target
	}

	allRequiredDone := allRequiredItemsComplete(payload)

	it := payload.Items[idx]
	return payload, &ItemMutationResult{
		ItemID:          itemID,
		ViewedAt:        it.ViewedAt,
		CompletedAt:     it.CompletedAt,
		Progress:        it.Progress,
		AllRequiredDone: allRequiredDone,
	}, false, nil
}

// writeNotificationPayload persists the full payload back to the row.
// Whole-document write rather than `jsonb_set` because the payload is
// already small (≤20 items) and the in-memory mutation gives us a
// single canonical encoding without escape edge cases.
func writeNotificationPayload(ctx context.Context, tx pgx.Tx, id string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE notifications SET payload = $2::jsonb WHERE id = $1::uuid`,
		id, raw); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}

// allRequiredItemsComplete returns true when every entry in
// payload.RequiredIDs has a non-nil CompletedAt. An empty / nil
// RequiredIDs slice always returns false — optional-only checklists
// never auto-resolve (plan 18 §3.5: a checklist without required items
// exits pending only via explicit dismiss).
func allRequiredItemsComplete(payload model.ChecklistPayload) bool {
	if len(payload.RequiredIDs) == 0 {
		return false
	}
	for _, rid := range payload.RequiredIDs {
		ridx := findChecklistItemIdx(payload.Items, rid)
		if ridx < 0 || payload.Items[ridx].CompletedAt == nil {
			return false
		}
	}
	return true
}

func findChecklistItemIdx(items []model.ChecklistItem, itemID string) int {
	for i := range items {
		if items[i].ID == itemID {
			return i
		}
	}
	return -1
}

func findDigestItemIdx(items []model.DigestItem, itemID string) int {
	for i := range items {
		if items[i].ID == itemID {
			return i
		}
	}
	return -1
}

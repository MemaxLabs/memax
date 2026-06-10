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

// --- Plans ---

// planColumns is the canonical SELECT list for plan queries. Keep in sync with scanPlan.
const planColumns = `id, scope, display_name, tier_order, entitlement_rank, monthly_price_cents,
	memory_limit, push_limit, recall_limit, ask_limit, max_attachment_bytes, storage_bytes_limit, ask_model,
	dreams_enabled, basic_dream_limit, lucid_dream_limit, review_inbox, max_team_hubs,
	max_owned_free_team_hubs, max_hub_members, seat_minimum, seat_billed,
	rate_limit_rpm, rate_limit_heavy_rpm, rate_limit_light_rpm,
	features, active, created_at, updated_at`

// scanPlan scans a row into a Plan. Column order must match planColumns.
func scanPlan(scan func(dest ...any) error) (model.Plan, error) {
	var p model.Plan
	var featuresJSON []byte
	if err := scan(
		&p.ID, &p.Scope, &p.DisplayName, &p.TierOrder, &p.EntitlementRank, &p.MonthlyPriceCents,
		&p.MemoryLimit, &p.PushLimit, &p.RecallLimit, &p.AskLimit, &p.MaxAttachmentBytes, &p.StorageBytesLimit, &p.AskModel,
		&p.DreamsEnabled, &p.BasicDreamLimit, &p.LucidDreamLimit, &p.ReviewInbox, &p.MaxTeamHubs,
		&p.MaxOwnedFreeTeamHubs, &p.MaxHubMembers, &p.SeatMinimum, &p.SeatBilled,
		&p.RateLimitRPM, &p.RateLimitHeavyRPM, &p.RateLimitLightRPM,
		&featuresJSON, &p.Active, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return p, err
	}
	if len(featuresJSON) > 0 {
		_ = json.Unmarshal(featuresJSON, &p.Features)
	}
	if p.Features == nil {
		p.Features = map[string]any{}
	}
	return p, nil
}

func (s *PostgresStore) ListPlans(ctx context.Context) ([]model.Plan, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+planColumns+` FROM plans WHERE active = true ORDER BY scope, tier_order`)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()

	var plans []model.Plan
	for rows.Next() {
		p, err := scanPlan(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan plan: %w", err)
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

// ListPlansByScope returns active plans filtered by scope.
func (s *PostgresStore) ListPlansByScope(ctx context.Context, scope model.PlanScope) ([]model.Plan, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+planColumns+` FROM plans WHERE active = true AND scope = $1 ORDER BY tier_order`, string(scope))
	if err != nil {
		return nil, fmt.Errorf("list plans by scope: %w", err)
	}
	defer rows.Close()

	var plans []model.Plan
	for rows.Next() {
		p, err := scanPlan(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan plan: %w", err)
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

func (s *PostgresStore) GetPlan(ctx context.Context, planID string) (*model.Plan, error) {
	p, err := scanPlan(s.pool.QueryRow(ctx,
		`SELECT `+planColumns+` FROM plans WHERE id = $1`, planID).Scan)
	if err != nil {
		return nil, fmt.Errorf("get plan: %w", err)
	}
	return &p, nil
}

func (s *PostgresStore) UpdatePlan(ctx context.Context, plan *model.Plan) error {
	featuresJSON, _ := json.Marshal(plan.Features)
	_, err := s.pool.Exec(ctx,
		`UPDATE plans SET
			display_name = $2, tier_order = $3, entitlement_rank = $4, monthly_price_cents = $5,
			memory_limit = $6, push_limit = $7, recall_limit = $8, ask_limit = $9, ask_model = $10,
			dreams_enabled = $11, basic_dream_limit = $12, lucid_dream_limit = $13,
			review_inbox = $14, max_team_hubs = $15,
			max_owned_free_team_hubs = $16, max_hub_members = $17, seat_minimum = $18, seat_billed = $19,
			rate_limit_rpm = $20, rate_limit_heavy_rpm = $21, rate_limit_light_rpm = $22,
			features = $23, active = $24, updated_at = now()
		 WHERE id = $1`,
		plan.ID, plan.DisplayName, plan.TierOrder, plan.EntitlementRank, plan.MonthlyPriceCents,
		plan.MemoryLimit, plan.PushLimit, plan.RecallLimit, plan.AskLimit, plan.AskModel,
		plan.DreamsEnabled, plan.BasicDreamLimit, plan.LucidDreamLimit,
		plan.ReviewInbox, plan.MaxTeamHubs,
		plan.MaxOwnedFreeTeamHubs, plan.MaxHubMembers, plan.SeatMinimum, plan.SeatBilled,
		plan.RateLimitRPM, plan.RateLimitHeavyRPM, plan.RateLimitLightRPM,
		featuresJSON, plan.Active,
	)
	return err
}

// --- Plan Overrides ---

func (s *PostgresStore) GetPlanOverride(ctx context.Context, userID string) (*model.PlanOverride, error) {
	var o model.PlanOverride
	var overridesJSON []byte
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, overrides, reason, COALESCE(set_by::text, ''), created_at, updated_at
		 FROM plan_overrides WHERE user_id = $1`, userID).Scan(
		&o.UserID, &overridesJSON, &o.Reason, &o.SetBy, &o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(overridesJSON) > 0 {
		_ = json.Unmarshal(overridesJSON, &o.Overrides)
	}
	if o.Overrides == nil {
		o.Overrides = map[string]any{}
	}
	return &o, nil
}

func (s *PostgresStore) UpsertPlanOverride(ctx context.Context, override *model.PlanOverride) error {
	overridesJSON, _ := json.Marshal(override.Overrides)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO plan_overrides (user_id, overrides, reason, set_by)
		 VALUES ($1, $2, $3, NULLIF($4, '')::uuid)
		 ON CONFLICT (user_id) DO UPDATE SET
			overrides = EXCLUDED.overrides,
			reason = EXCLUDED.reason,
			set_by = EXCLUDED.set_by,
			updated_at = now()`,
		override.UserID, overridesJSON, override.Reason, override.SetBy,
	)
	return err
}

func (s *PostgresStore) DeletePlanOverride(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM plan_overrides WHERE user_id = $1`, userID)
	return err
}

// --- Usage Events ---

func (s *PostgresStore) InsertUsageEvent(ctx context.Context, event *model.UsageEvent) error {
	metadataJSON, _ := json.Marshal(event.Metadata)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO usage_events (user_id, hub_id, operation, source, agent_name, metadata)
		 VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6)`,
		event.UserID, event.HubID, event.Operation, event.Source, event.AgentName, metadataJSON,
	)
	return err
}

func (s *PostgresStore) SetUsageCounters(ctx context.Context, userID string, periodStart, periodEnd time.Time, push, recall, ask int) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO usage (id, user_id, period_start, period_end, push_count, recall_count, ask_count, updated_at)
		 VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, now())
		 ON CONFLICT (user_id, period_start) DO UPDATE SET
			push_count = GREATEST(usage.push_count, EXCLUDED.push_count),
			recall_count = GREATEST(usage.recall_count, EXCLUDED.recall_count),
			ask_count = GREATEST(usage.ask_count, EXCLUDED.ask_count),
			updated_at = now()`,
		userID, periodStart, periodEnd, push, recall, ask,
	)
	return err
}

func (s *PostgresStore) ResetUsageCounters(ctx context.Context, userID string, periodStart, periodEnd time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO usage (id, user_id, period_start, period_end, push_count, recall_count, ask_count, updated_at)
		 VALUES (gen_random_uuid(), $1, $2, $3, 0, 0, 0, now())
		 ON CONFLICT (user_id, period_start) DO UPDATE SET
			push_count = 0,
			recall_count = 0,
			ask_count = 0,
			updated_at = now()`,
		userID, periodStart, periodEnd,
	)
	return err
}

// --- Usage Sync ---

// ListUsageUserIDs returns distinct user IDs that have metered operations
// in the current billing period. Sources from usage_events (always populated
// by the meter on commit) rather than the usage table, which may not have
// rows for new users yet. This ensures the sync discovers Redis-only users.
func (s *PostgresStore) ListUsageUserIDs(ctx context.Context, periodStart time.Time) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT user_id FROM usage_events
		 WHERE user_id IS NOT NULL
		   AND operation IN ('push', 'recall', 'ask')
		   AND created_at >= $1
		 UNION
		 SELECT user_id FROM usage
		 WHERE period_start = $1
		   AND (push_count > 0 OR recall_count > 0 OR ask_count > 0)`,
		periodStart,
	)
	if err != nil {
		return nil, fmt.Errorf("list usage user IDs: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// --- Admin Hub Queries ---

func (s *PostgresStore) ListTeamHubs(ctx context.Context, opts model.AdminHubListOpts) ([]model.Hub, string, int, error) {
	if opts.Limit <= 0 {
		opts.Limit = 50
	}

	conditions := []string{"hub_type = 'team'"}
	var args []any
	argN := 1

	if q := strings.TrimSpace(opts.Query); q != "" {
		conditions = append(conditions, fmt.Sprintf("(name ILIKE $%d OR slug ILIKE $%d)", argN, argN))
		args = append(args, "%"+q+"%")
		argN++
	}
	if opts.Cursor != "" {
		// Keyset pagination: (created_at, id) < (cursor.created_at, cursor.id).
		// Subselect keeps the API stable as ID-only from the caller's
		// perspective — no need to ship an opaque base64 cursor.
		conditions = append(conditions, fmt.Sprintf(
			"(created_at, id) < (SELECT created_at, id FROM hubs WHERE id = $%d::uuid)", argN))
		args = append(args, opts.Cursor)
		argN++
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	// Total count — reuses the filter predicate but drops the cursor
	// condition so the total reflects the full filtered set, not just
	// the current page's tail.
	countConditions := conditions
	countArgs := args
	if opts.Cursor != "" {
		countConditions = countConditions[:len(countConditions)-1]
		countArgs = countArgs[:len(countArgs)-1]
	}
	countWhere := "WHERE " + strings.Join(countConditions, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM hubs %s", countWhere), countArgs...).Scan(&total); err != nil {
		return nil, "", 0, fmt.Errorf("count team hubs: %w", err)
	}

	query := fmt.Sprintf(
		`SELECT id, name, COALESCE(icon, ''), COALESCE(accent, ''), slug, hub_type,
		        COALESCE(plan, ''), owner_id, allow_contributor_topics, allow_contributor_dreams,
		        contributor_delete_policy, COALESCE(header_aurora_mode, ''),
		        created_at, updated_at
		 FROM hubs %s ORDER BY created_at DESC, id DESC LIMIT %d`,
		where, opts.Limit+1,
	)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", 0, fmt.Errorf("list team hubs: %w", err)
	}
	defer rows.Close()

	var hubs []model.Hub
	for rows.Next() {
		var h model.Hub
		if err := rows.Scan(&h.ID, &h.Name, &h.Icon, &h.Accent, &h.Slug, &h.HubType,
			&h.Plan, &h.OwnerID, &h.AllowContributorTopics, &h.AllowContributorDreams,
			&h.ContributorDeletePolicy, &h.HeaderAuroraMode, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, "", 0, err
		}
		hubs = append(hubs, h)
	}
	if err := rows.Err(); err != nil {
		return nil, "", 0, err
	}

	// +1 row fetched over Limit signals another page exists.
	// The cursor is the last-kept hub's ID (next page will request
	// rows strictly before its (created_at, id)).
	nextCursor := ""
	if len(hubs) > opts.Limit {
		nextCursor = hubs[opts.Limit-1].ID
		hubs = hubs[:opts.Limit]
	}
	return hubs, nextCursor, total, nil
}

// --- Hub Subscriptions ---

func (s *PostgresStore) CreateHubSubscription(ctx context.Context, sub *model.HubSubscription) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO hub_subscriptions (hub_id, plan_id, seat_count, provider, status, billing_user_id, current_period_start, current_period_end)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		sub.HubID, sub.PlanID, sub.SeatCount, sub.Provider, sub.Status, sub.BillingUserID,
		sub.CurrentPeriodStart, sub.CurrentPeriodEnd,
	)
	return err
}

func (s *PostgresStore) GetActiveHubSubscription(ctx context.Context, hubID string) (*model.HubSubscription, error) {
	var sub model.HubSubscription
	err := s.pool.QueryRow(ctx,
		`SELECT id, hub_id, plan_id, seat_count, provider,
		        COALESCE(provider_subscription_id, ''), COALESCE(provider_customer_id, ''),
		        status, billing_user_id, current_period_start, current_period_end,
		        cancelled_at, created_at, updated_at, over_limit_since
		 FROM hub_subscriptions WHERE hub_id = $1 AND status = 'active'`, hubID).Scan(
		&sub.ID, &sub.HubID, &sub.PlanID, &sub.SeatCount, &sub.Provider,
		&sub.ProviderSubscriptionID, &sub.ProviderCustomerID,
		&sub.Status, &sub.BillingUserID, &sub.CurrentPeriodStart, &sub.CurrentPeriodEnd,
		&sub.CancelledAt, &sub.CreatedAt, &sub.UpdatedAt, &sub.OverLimitSince,
	)
	if err != nil {
		// Return (nil, nil) for "no active subscription" — callers check
		// sub == nil without inspecting err.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &sub, nil
}

// SetHubOverLimit records the moment a hub crossed its memory cap.
// Idempotent by design: if the column is already non-null, leaves the
// existing timestamp so the grace window doesn't reset on every push.
// Returns (transitioned, err) — transitioned is true only when this
// call actually changed a row from NULL to non-null, so callers can
// fire a one-shot state-change notification.
func (s *PostgresStore) SetHubOverLimit(ctx context.Context, hubID string, now time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE hub_subscriptions
		 SET over_limit_since = $2, updated_at = now()
		 WHERE hub_id = $1
		   AND status = 'active'
		   AND over_limit_since IS NULL`,
		hubID, now,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ClearHubOverLimit clears the over-limit marker. Called when a
// memory leaves the hub (delete/archive) AND the post-delete count is
// back under the plan's memory_limit. Returns (transitioned, err) —
// transitioned is true when the column actually changed from
// non-null to NULL; callers can chain a hub_restored notification on
// that edge.
func (s *PostgresStore) ClearHubOverLimit(ctx context.Context, hubID string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE hub_subscriptions
		 SET over_limit_since = NULL, updated_at = now()
		 WHERE hub_id = $1
		   AND status = 'active'
		   AND over_limit_since IS NOT NULL`,
		hubID,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ListHubsPastGrace returns hubs whose active subscription has been
// over-limit for longer than the grace window. Used by the daily
// sweep that fires hub_frozen notifications. Returns empty when none
// qualify. The query is an index-friendly partial scan on rows where
// over_limit_since IS NOT NULL — a small set in practice.
func (s *PostgresStore) ListHubsPastGrace(ctx context.Context, graceCutoff time.Time) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT hub_id::text
		 FROM hub_subscriptions
		 WHERE status = 'active'
		   AND over_limit_since IS NOT NULL
		   AND over_limit_since < $1`,
		graceCutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListFrozenHubIDs runs a single query over hub_subscriptions to find
// which of the input hubIDs are frozen. A hub is frozen when:
//
//	status != 'active'      (admin/billing cancelled or past_due), OR
//	over_limit_since < graceCutoff   (grace window expired).
//
// Hubs without any subscription row are not included in the result —
// they run on the free-tier hub_free_team plan and are never "frozen".
// The query is intentionally one SELECT with = ANY() for batch
// efficiency on the recall/ask read path.
func (s *PostgresStore) ListFrozenHubIDs(ctx context.Context, hubIDs []string, graceCutoff time.Time) ([]string, error) {
	if len(hubIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT hub_id::text
		 FROM hub_subscriptions
		 WHERE hub_id = ANY($1::uuid[])
		   AND (status != 'active'
		        OR (over_limit_since IS NOT NULL AND over_limit_since < $2))`,
		hubIDs, graceCutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var frozen []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		frozen = append(frozen, id)
	}
	return frozen, rows.Err()
}

// GetHubSubscriptionAnyStatus returns the most recent subscription
// row for a hub irrespective of status. Used by freeze checks — an
// inactive (cancelled, past_due) row still means the hub is frozen,
// whereas GetActiveHubSubscription would mask it with nil.
func (s *PostgresStore) GetHubSubscriptionAnyStatus(ctx context.Context, hubID string) (*model.HubSubscription, error) {
	var sub model.HubSubscription
	err := s.pool.QueryRow(ctx,
		`SELECT id, hub_id, plan_id, seat_count, provider,
		        COALESCE(provider_subscription_id, ''), COALESCE(provider_customer_id, ''),
		        status, billing_user_id, current_period_start, current_period_end,
		        cancelled_at, created_at, updated_at, over_limit_since
		 FROM hub_subscriptions
		 WHERE hub_id = $1
		 ORDER BY created_at DESC
		 LIMIT 1`, hubID).Scan(
		&sub.ID, &sub.HubID, &sub.PlanID, &sub.SeatCount, &sub.Provider,
		&sub.ProviderSubscriptionID, &sub.ProviderCustomerID,
		&sub.Status, &sub.BillingUserID, &sub.CurrentPeriodStart, &sub.CurrentPeriodEnd,
		&sub.CancelledAt, &sub.CreatedAt, &sub.UpdatedAt, &sub.OverLimitSince,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &sub, nil
}

func (s *PostgresStore) UpdateHubSubscription(ctx context.Context, sub *model.HubSubscription) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE hub_subscriptions SET
			plan_id = $2, seat_count = $3, status = $4,
			billing_user_id = $5, updated_at = now()
		 WHERE id = $1`,
		sub.ID, sub.PlanID, sub.SeatCount, sub.Status, sub.BillingUserID,
	)
	return err
}

func (s *PostgresStore) ListHubSubscriptionsByBillingUser(ctx context.Context, userID string) ([]model.HubSubscription, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, hub_id, plan_id, seat_count, provider, status, billing_user_id,
		        current_period_start, current_period_end, created_at, updated_at
		 FROM hub_subscriptions WHERE billing_user_id = $1 AND status = 'active'
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var subs []model.HubSubscription
	for rows.Next() {
		var sub model.HubSubscription
		if err := rows.Scan(
			&sub.ID, &sub.HubID, &sub.PlanID, &sub.SeatCount, &sub.Provider,
			&sub.Status, &sub.BillingUserID,
			&sub.CurrentPeriodStart, &sub.CurrentPeriodEnd, &sub.CreatedAt, &sub.UpdatedAt,
		); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

// --- Effective Plan Cache ---

func (s *PostgresStore) GetEffectivePlan(ctx context.Context, userID string) (*model.EffectivePlan, error) {
	var ep model.EffectivePlan
	err := s.pool.QueryRow(ctx,
		`SELECT effective_plan, source, computed_at FROM effective_plan_cache WHERE user_id = $1`,
		userID).Scan(&ep.PlanID, &ep.Source, &ep.ComputedAt)
	if err != nil {
		return nil, err
	}
	return &ep, nil
}

func (s *PostgresStore) SetEffectivePlan(ctx context.Context, userID, planID, source string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO effective_plan_cache (user_id, effective_plan, source, computed_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (user_id) DO UPDATE SET
			effective_plan = EXCLUDED.effective_plan,
			source = EXCLUDED.source,
			computed_at = now()`,
		userID, planID, source,
	)
	return err
}

func (s *PostgresStore) GetHubPlanID(ctx context.Context, hubID string) (string, error) {
	var planID string
	err := s.pool.QueryRow(ctx,
		`SELECT plan_id FROM hub_subscriptions WHERE hub_id = $1 AND status = 'active'`,
		hubID).Scan(&planID)
	if err != nil {
		return "", err
	}
	return planID, nil
}

func (s *PostgresStore) GetUserHubPlans(ctx context.Context, userID string) ([]model.HubPlanRef, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT hs.hub_id, hs.plan_id
		 FROM hub_members hm
		 JOIN hub_subscriptions hs ON hm.hub_id = hs.hub_id AND hs.status = 'active'
		 WHERE hm.user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []model.HubPlanRef
	for rows.Next() {
		var ref model.HubPlanRef
		if err := rows.Scan(&ref.HubID, &ref.PlanID); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

// --- User Billing Subscriptions ---

func (s *PostgresStore) UpsertBillingSubscription(ctx context.Context, sub *model.BillingSubscription) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO billing_subscriptions (user_id, plan_id, provider, status, current_period_start, current_period_end)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (user_id) WHERE status = 'active'
		 DO UPDATE SET
			plan_id = EXCLUDED.plan_id,
			provider = EXCLUDED.provider,
			status = EXCLUDED.status,
			current_period_start = COALESCE(EXCLUDED.current_period_start, billing_subscriptions.current_period_start),
			updated_at = now()`,
		sub.UserID, sub.PlanID, sub.Provider, sub.Status, sub.CurrentPeriodStart, sub.CurrentPeriodEnd,
	)
	return err
}

// --- Admin User Management ---

// ListUsers returns a paginated list of users. Pagination uses created_at DESC
// with id as tie-breaker for deterministic ordering, regardless of the display
// sort requested by the admin UI. The cursor is the last user's ID, which maps
// to a (created_at, id) keyset via a subquery.
func (s *PostgresStore) ListUsers(ctx context.Context, opts model.AdminUserListOpts) ([]model.User, string, int, error) {
	if opts.Limit <= 0 {
		opts.Limit = 50
	}

	var conditions []string
	var args []any
	argN := 1

	if opts.Plan != "" {
		// Match against scoped personal_plan_id (canonical) OR legacy plan column
		// for transitional rows where personal_plan_id hasn't been backfilled yet.
		conditions = append(conditions, fmt.Sprintf("(u.personal_plan_id = $%d OR u.plan = $%d)", argN, argN))
		args = append(args, opts.Plan)
		argN++
	}
	if opts.Query != "" {
		conditions = append(conditions, fmt.Sprintf("(u.email ILIKE $%d OR u.name ILIKE $%d OR u.display_name ILIKE $%d)", argN, argN, argN))
		args = append(args, "%"+opts.Query+"%")
		argN++
	}
	if opts.Cursor != "" {
		// Keyset pagination: (created_at, id) < (cursor_created_at, cursor_id)
		conditions = append(conditions, fmt.Sprintf(
			"(u.created_at, u.id) < (SELECT created_at, id FROM users WHERE id = $%d::uuid)", argN))
		args = append(args, opts.Cursor)
		argN++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Total count (without cursor filter for stable totals)
	countConditions := conditions
	if opts.Cursor != "" {
		countConditions = countConditions[:len(countConditions)-1] // strip cursor condition
	}
	countWhere := ""
	if len(countConditions) > 0 {
		countWhere = "WHERE " + strings.Join(countConditions, " AND ")
	}
	countArgs := args
	if opts.Cursor != "" {
		countArgs = countArgs[:len(countArgs)-1]
	}
	var total int
	if err := s.pool.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM users u %s", countWhere), countArgs...).Scan(&total); err != nil {
		return nil, "", 0, fmt.Errorf("count users: %w", err)
	}

	// Fetch with deterministic ordering: created_at DESC, id DESC.
	// COALESCE(u.plan, '') because the legacy plan column is nullable
	// (see migration 001: `plan text` with no NOT NULL) — a single row
	// with NULL plan would otherwise abort the whole Scan loop and
	// surface as "no users" in the admin UI even though COUNT(*) and
	// the stats endpoint both still work (they don't read the column).
	query := fmt.Sprintf(
		`SELECT u.id, u.email, u.name, u.display_name, u.avatar_url,
		        COALESCE(u.plan, ''), u.personal_plan_id, u.can_create_hub,
		        u.created_at, u.updated_at
		 FROM users u %s ORDER BY u.created_at DESC, u.id DESC LIMIT %d`,
		where, opts.Limit+1,
	)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.DisplayName, &u.AvatarURL, &u.Plan, &u.PersonalPlanID, &u.CanCreateHub, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, "", 0, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, "", 0, err
	}

	nextCursor := ""
	if len(users) > opts.Limit {
		nextCursor = users[opts.Limit-1].ID
		users = users[:opts.Limit]
	}

	return users, nextCursor, total, nil
}

func (s *PostgresStore) UpdateUserPlan(ctx context.Context, userID string, planID string) error {
	// Phase 6: personal_plan_id is the sole authority. Legacy users.plan
	// column is no longer written (FK dropped in migration 067).
	scopedID := model.LegacyToScopedPlanID(planID)
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET personal_plan_id = $2, updated_at = now() WHERE id = $1`,
		userID, scopedID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found: %s", userID)
	}
	return nil
}

// CountOwnedFreeTeamHubs returns the number of hub_free_team hubs owned by userID.
// Used for ownership cap enforcement.
func (s *PostgresStore) CountOwnedFreeTeamHubs(ctx context.Context, ownerID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM hubs h
		 JOIN hub_subscriptions hs ON hs.hub_id = h.id AND hs.status = 'active'
		 WHERE h.owner_id = $1
		   AND h.hub_type = 'team'
		   AND hs.plan_id = $2`,
		ownerID, model.HubFreeTeamPlanID,
	).Scan(&count)
	return count, err
}

func (s *PostgresStore) CountMemoriesByOwner(ctx context.Context, ownerID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM memories WHERE owner_id = $1 AND state != 'archived'`,
		ownerID,
	).Scan(&count)
	return count, err
}

// CountMemoriesByOwnerExcludingSeeds returns the owner's active memory
// count with onboarding-seed copies (`source_kind = 'onboarding-seed'`)
// filtered out. Plan 23 §5.5 — usage / quota / activation-trigger code
// should call this so a new user with 6 seed memories isn't immediately
// at 6/100 free-tier quota or auto-firing plan-18 first_memory triggers.
// Use the raw `CountMemoriesByOwner` for admin/analytics surfaces that
// genuinely want every row.
func (s *PostgresStore) CountMemoriesByOwnerExcludingSeeds(ctx context.Context, ownerID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM memories
		 WHERE owner_id = $1
		   AND state != 'archived'
		   AND (source_kind IS NULL OR source_kind != 'onboarding-seed')`,
		ownerID,
	).Scan(&count)
	return count, err
}

func (s *PostgresStore) CountMemoriesByHub(ctx context.Context, hubID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM memories WHERE hub_id = $1::uuid AND state != 'archived'`,
		hubID,
	).Scan(&count)
	return count, err
}

// SumStorageBytesByOwner returns the authoritative cumulative byte count for
// every persisted artifact the owner has under the storage-bytes quota.
// Currently this is just memory_attachments — every row, since attachments
// are deleted via cascade when the memory is deleted.
//
// This is the source of truth the meter backfills from when the Redis
// gate/committed keys are missing; it is also what a full recount job
// would use.
func (s *PostgresStore) SumStorageBytesByOwner(ctx context.Context, ownerID string) (int64, error) {
	const q = `
        SELECT COALESCE(SUM(size_bytes), 0)
        FROM memory_attachments
        WHERE owner_id = $1`
	var total int64
	if err := s.pool.QueryRow(ctx, q, ownerID).Scan(&total); err != nil {
		return 0, fmt.Errorf("sum storage bytes: %w", err)
	}
	return total, nil
}

func (s *PostgresStore) GetSystemStats(ctx context.Context) (*model.SystemStats, error) {
	stats := &model.SystemStats{
		UsersByPlan: make(map[string]int),
	}

	// Total users
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&stats.TotalUsers); err != nil {
		return nil, fmt.Errorf("count users: %w", err)
	}

	// Users by plan (use scoped personal_plan_id for accurate grouping)
	rows, err := s.pool.Query(ctx, `SELECT personal_plan_id, COUNT(*) FROM users GROUP BY personal_plan_id`)
	if err != nil {
		return nil, fmt.Errorf("users by plan: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var plan string
		var count int
		if err := rows.Scan(&plan, &count); err != nil {
			return nil, err
		}
		stats.UsersByPlan[plan] = count
	}

	// Total memories
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memories WHERE state != 'archived'`).Scan(&stats.TotalMemories); err != nil {
		return nil, fmt.Errorf("count memories: %w", err)
	}

	// Active today
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT user_id) FROM usage_events WHERE created_at >= CURRENT_DATE AND user_id IS NOT NULL`,
	).Scan(&stats.ActiveToday); err != nil {
		// Not fatal — table may be empty initially
		stats.ActiveToday = 0
	}

	return stats, nil
}

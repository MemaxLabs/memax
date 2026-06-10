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

// --- Hubs ---

func (s *PostgresStore) CreateHub(hub *model.Hub) error {
	ctx := context.Background()
	settings := hub.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO hubs (
			id,
			name,
			icon,
			accent,
			slug,
			hub_type,
			plan,
			owner_id,
			allow_contributor_topics,
			allow_contributor_dreams,
			contributor_delete_policy,
			header_aurora_mode,
			settings,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		hub.ID,
		hub.Name,
		hub.Icon,
		hub.Accent,
		hub.Slug,
		hub.HubType,
		hub.Plan,
		hub.OwnerID,
		hub.AllowContributorTopics,
		hub.AllowContributorDreams,
		hub.ContributorDeletePolicy,
		nullIfEmpty(hub.HeaderAuroraMode),
		settings,
		hub.CreatedAt,
		hub.UpdatedAt,
	)
	return err
}

// Advisory lock namespaces for cap enforcement. These are arbitrary int4 values
// that partition the pg_advisory_xact_lock keyspace so ownership locks and
// member locks don't collide.
const (
	advisoryLockNSOwnership = 0x4D_45_4D_01 // "MEM" + 01
	advisoryLockNSMemberCap = 0x4D_45_4D_02 // "MEM" + 02
)

// ErrHubNotFound is returned when a hub lookup finds no matching row.
var ErrHubNotFound = errors.New("hub not found")

// ErrOwnershipCapExceeded is returned when a user tries to create more
// free team hubs than their personal plan allows.
var ErrOwnershipCapExceeded = errors.New("ownership cap exceeded")

// ErrMemberCapExceeded is returned when a hub invite/add would exceed
// the hub plan's member cap.
var ErrMemberCapExceeded = errors.New("member cap exceeded")

// CreateTeamHub atomically creates a team hub with ownership cap enforcement.
//
// Inside a single transaction:
//  1. Acquires per-owner advisory lock (prevents concurrent hub creation races)
//  2. Counts the owner's existing free team hubs
//  3. Checks against maxFreeTeamHubs (from the plan)
//  4. Creates hub + owner membership + hub_free_team subscription
//
// If maxFreeTeamHubs is 0, creation is always rejected.
// Returns ErrOwnershipCapExceeded if the cap is reached.
func (s *PostgresStore) CreateTeamHub(ctx context.Context, hub *model.Hub, ownerID string, maxFreeTeamHubs int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 1. Advisory lock scoped to this owner's free-team-hub creation.
	// Prevents two concurrent CreateTeamHub calls from both passing the
	// count check before either commits.
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock($1, hashtext($2))`,
		advisoryLockNSOwnership, ownerID,
	); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}

	// 2. Count existing free team hubs owned by this user
	var currentCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM hubs h
		 JOIN hub_subscriptions hs ON hs.hub_id = h.id AND hs.status = 'active'
		 WHERE h.owner_id = $1 AND h.hub_type = 'team' AND hs.plan_id = $2`,
		ownerID, model.HubFreeTeamPlanID,
	).Scan(&currentCount); err != nil {
		return fmt.Errorf("count owned free team hubs: %w", err)
	}

	// 3. Cap check
	if currentCount >= maxFreeTeamHubs {
		return ErrOwnershipCapExceeded
	}

	// 4. Create hub. settings mirrors CreateHub — include explicitly
	// so a caller that sets hub.Settings at creation time (e.g. hub
	// template with pre-populated dream toggles) persists correctly
	// instead of silently falling back to the DB default.
	settings := hub.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO hubs (id, name, icon, accent, slug, hub_type, plan, owner_id,
			allow_contributor_topics, allow_contributor_dreams, contributor_delete_policy,
			header_aurora_mode, settings, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		hub.ID, hub.Name, hub.Icon, hub.Accent, hub.Slug, hub.HubType, hub.Plan,
		hub.OwnerID, hub.AllowContributorTopics, hub.AllowContributorDreams,
		hub.ContributorDeletePolicy, nullIfEmpty(hub.HeaderAuroraMode),
		settings, hub.CreatedAt, hub.UpdatedAt,
	); err != nil {
		return fmt.Errorf("create hub: %w", err)
	}

	// 5. Add owner as member
	if _, err := tx.Exec(ctx,
		`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`,
		hub.ID, ownerID,
	); err != nil {
		return fmt.Errorf("add hub owner: %w", err)
	}

	// 6. Create authoritative hub_free_team subscription
	if _, err := tx.Exec(ctx,
		`INSERT INTO hub_subscriptions (hub_id, plan_id, seat_count, provider, status, billing_user_id)
		 VALUES ($1, $2, 1, 'admin', 'active', $3)`,
		hub.ID, hub.Plan, ownerID,
	); err != nil {
		return fmt.Errorf("create hub subscription: %w", err)
	}

	return tx.Commit(ctx)
}

// AddHubMemberWithCapCheck atomically adds a member to a hub with member cap
// enforcement. Uses a per-hub advisory lock to prevent concurrent accepts
// from exceeding the cap.
//
// maxMembers <= 0 means unlimited (no cap). Returns ErrMemberCapExceeded if
// the hub is at or over its member cap.
func (s *PostgresStore) AddHubMemberWithCapCheck(ctx context.Context, hubID, userID, role string, maxMembers int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 1. Advisory lock scoped to this hub's member mutations
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock($1, hashtext($2))`,
		advisoryLockNSMemberCap, hubID,
	); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}

	// 2. Count current members
	if maxMembers > 0 {
		var currentCount int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM hub_members WHERE hub_id = $1`, hubID,
		).Scan(&currentCount); err != nil {
			return fmt.Errorf("count hub members: %w", err)
		}
		if currentCount >= maxMembers {
			return ErrMemberCapExceeded
		}
	}

	// 3. Add member
	if _, err := tx.Exec(ctx,
		`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, $3)`,
		hubID, userID, role,
	); err != nil {
		return fmt.Errorf("add hub member: %w", err)
	}

	return tx.Commit(ctx)
}

// GetHubMemberCap returns the max_hub_members for the hub's active subscription
// plan. Returns -1 if the plan has no cap (unlimited) or if the hub has no
// active subscription (no-subscription is not a DB error). Real DB errors
// are returned so callers can fail closed.
func (s *PostgresStore) GetHubMemberCap(ctx context.Context, hubID string) (int, error) {
	var cap *int
	err := s.pool.QueryRow(ctx,
		`SELECT p.max_hub_members
		 FROM hub_subscriptions hs
		 JOIN plans p ON p.id = hs.plan_id
		 WHERE hs.hub_id = $1 AND hs.status = 'active'`, hubID,
	).Scan(&cap)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return -1, nil // no subscription → unlimited (expected state for personal hubs)
		}
		return 0, fmt.Errorf("get hub member cap: %w", err) // real DB error → fail closed
	}
	if cap == nil {
		return -1, nil // NULL = unlimited
	}
	return *cap, nil
}

func (s *PostgresStore) GetHub(id string) (*model.Hub, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT id, name, COALESCE(icon, ''), COALESCE(accent, ''), slug, hub_type, COALESCE(plan, ''), owner_id,
			allow_contributor_topics, allow_contributor_dreams,
			COALESCE(contributor_delete_policy, 'own'),
			COALESCE(header_aurora_mode, ''),
			COALESCE(settings, '{}'::jsonb),
			created_at, updated_at
		FROM hubs WHERE id = $1`, id)
	var h model.Hub
	err := row.Scan(&h.ID, &h.Name, &h.Icon, &h.Accent, &h.Slug, &h.HubType, &h.Plan, &h.OwnerID,
		&h.AllowContributorTopics, &h.AllowContributorDreams,
		&h.ContributorDeletePolicy,
		&h.HeaderAuroraMode,
		&h.Settings,
		&h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("hub not found: %w", err)
	}
	return &h, nil
}

func (s *PostgresStore) GetHubBySlug(slug string) (*model.Hub, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT id, name, COALESCE(icon, ''), COALESCE(accent, ''), slug, hub_type, COALESCE(plan, ''), owner_id,
			allow_contributor_topics, allow_contributor_dreams,
			COALESCE(contributor_delete_policy, 'own'),
			COALESCE(header_aurora_mode, ''),
			COALESCE(settings, '{}'::jsonb),
			created_at, updated_at
		FROM hubs WHERE slug = $1`, slug)
	var h model.Hub
	err := row.Scan(&h.ID, &h.Name, &h.Icon, &h.Accent, &h.Slug, &h.HubType, &h.Plan, &h.OwnerID,
		&h.AllowContributorTopics, &h.AllowContributorDreams,
		&h.ContributorDeletePolicy,
		&h.HeaderAuroraMode,
		&h.Settings,
		&h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrHubNotFound
		}
		return nil, fmt.Errorf("hub slug lookup failed: %w", err)
	}
	return &h, nil
}

func (s *PostgresStore) UpdateHub(hub *model.Hub) error {
	ctx := context.Background()
	// settings is NOT updated here. The dedicated atomic merge path
	// (PatchHubSettings) owns that column so two concurrent
	// callers — one touching name/accent, one touching settings —
	// can't clobber each other. If UpdateHub wrote settings back
	// too, a full-row PATCH racing a settings PATCH would write a
	// stale snapshot on top of the atomic merge.
	_, err := s.pool.Exec(ctx,
		`UPDATE hubs
		SET name = $2,
			icon = $3,
			accent = $4,
			slug = $5,
			allow_contributor_topics = $6,
			allow_contributor_dreams = $7,
			contributor_delete_policy = $8,
			header_aurora_mode = $9,
			updated_at = now()
		WHERE id = $1`,
		hub.ID, hub.Name, hub.Icon, hub.Accent, hub.Slug, hub.AllowContributorTopics, hub.AllowContributorDreams, hub.ContributorDeletePolicy, nullIfEmpty(hub.HeaderAuroraMode))
	return err
}

// PatchHubSettings merges `patch` into hubs.settings atomically in
// a single UPDATE. jsonb_strip_nulls turns null values in the patch
// into deletes (the `||` operator maps the key to JSON null first;
// strip_nulls then removes top-level nulls). Non-null values set
// the key.
//
// Atomic merge matters: the PATCH handler would otherwise do
// read-then-write, and two rapid concurrent toggles on the same
// hub (e.g. the user tapping merge + archive within a second in
// the overrides panel) could both read `{}`, apply their key, and
// overwrite each other — last-writer-wins on settings as a whole.
// This SQL lets them compose, and matches what the engine treats
// as authoritative.
func (s *PostgresStore) PatchHubSettings(ctx context.Context, hubID string, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	payload, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal hub settings patch: %w", err)
	}
	// RowsAffected == 0 means the hub vanished between the
	// handler's GetHub and this patch (concurrent delete, or the
	// caller passed a bogus id). Surfacing ErrHubNotFound lets the
	// handler return 404 instead of a misleading 200; without this
	// check the UPDATE would look successful.
	tag, err := s.pool.Exec(ctx,
		`UPDATE hubs
		SET settings = jsonb_strip_nulls(COALESCE(settings, '{}'::jsonb) || $2::jsonb),
		    updated_at = now()
		WHERE id = $1::uuid`,
		hubID, payload)
	if err != nil {
		return fmt.Errorf("patch hub settings: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrHubNotFound
	}
	return nil
}

func (s *PostgresStore) ListUserHubs(userID string) ([]model.HubWithRole, error) {
	ctx := context.Background()
	// settings is included in the list response so the per-hub
	// overrides UI (Settings → Intelligence) can render without
	// issuing N GET /v1/hubs/{id} requests. Payload is ~5 booleans
	// per hub; storage size is negligible.
	rows, err := s.pool.Query(ctx,
		`SELECT h.id, h.name, COALESCE(h.icon, ''), COALESCE(h.accent, ''), h.slug, h.hub_type, COALESCE(h.plan, ''), h.owner_id,
			h.allow_contributor_topics, h.allow_contributor_dreams,
			COALESCE(h.contributor_delete_policy, 'own'),
			COALESCE(h.header_aurora_mode, ''),
			COALESCE(h.settings, '{}'::jsonb),
			h.created_at, h.updated_at, hm.role,
			(SELECT COUNT(*) FROM memories m WHERE m.hub_id = h.id AND m.state != 'archived')
		FROM hubs h
		JOIN hub_members hm ON h.id = hm.hub_id
		WHERE hm.user_id = $1
		ORDER BY h.hub_type ASC, h.name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []model.HubWithRole
	for rows.Next() {
		var h model.Hub
		var role string
		var memoryCount int
		if err := rows.Scan(&h.ID, &h.Name, &h.Icon, &h.Accent, &h.Slug, &h.HubType, &h.Plan, &h.OwnerID,
			&h.AllowContributorTopics, &h.AllowContributorDreams,
			&h.ContributorDeletePolicy,
			&h.HeaderAuroraMode,
			&h.Settings,
			&h.CreatedAt, &h.UpdatedAt, &role, &memoryCount); err != nil {
			return nil, fmt.Errorf("scan hub row: %w", err)
		}
		result = append(result, model.HubWithRole{Hub: h, Role: role, MemoryCount: memoryCount})
	}
	return result, rows.Err()
}

// HubMembershipIDs returns just the hub-id list a user belongs to.
// Used by the agent runtime's scope resolver on the chat hot path
// (every message-send for user_all_hubs and hub_subset sessions).
//
// The query is intentionally narrow: hub_members has a composite
// primary key on (hub_id, user_id) and no soft-delete column, so a
// single index probe on user_id returns exactly the rows we need.
// No JOIN to hubs is necessary — we never read hub metadata here,
// just (hub_id, role).
//
// Ordering is deterministic by hub_id so retries don't change the
// observed list. Callers that care about original-pin order
// (hub_subset scope) impose their own ordering downstream.
//
// Phase 3.7 step 1 added the role column to the projection so the
// ScopeResolver can derive WritableHubIDs (hubs where role !=
// "viewer") in the same single-query path it already uses for
// HubIDs. Replaces the prior IDs-only HubMembershipIDs.
func (s *PostgresStore) HubMembershipsForUser(ctx context.Context, userID string) ([]model.HubMembership, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT hub_id, role FROM hub_members WHERE user_id = $1::uuid ORDER BY hub_id`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("hub memberships: query: %w", err)
	}
	defer rows.Close()

	// Pre-size for the typical case of a few-to-a-dozen hubs. The
	// final cap is bounded by hub_members.user_id index cardinality;
	// in production this stays small.
	out := make([]model.HubMembership, 0, 8)
	for rows.Next() {
		var m model.HubMembership
		if err := rows.Scan(&m.HubID, &m.Role); err != nil {
			return nil, fmt.Errorf("hub memberships: scan: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("hub memberships: rows: %w", err)
	}
	return out, nil
}

func (s *PostgresStore) GetPersonalHub(userID string) (*model.Hub, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT id, name, COALESCE(icon, ''), COALESCE(accent, ''), slug, hub_type, COALESCE(plan, ''), owner_id,
			allow_contributor_topics, allow_contributor_dreams,
			COALESCE(contributor_delete_policy, 'own'),
			COALESCE(header_aurora_mode, ''),
			created_at, updated_at
		FROM hubs WHERE owner_id = $1::uuid AND hub_type = 'personal'`, userID)
	var h model.Hub
	err := row.Scan(&h.ID, &h.Name, &h.Icon, &h.Accent, &h.Slug, &h.HubType, &h.Plan, &h.OwnerID,
		&h.AllowContributorTopics, &h.AllowContributorDreams,
		&h.ContributorDeletePolicy,
		&h.HeaderAuroraMode,
		&h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("personal hub not found: %w", err)
	}
	return &h, nil
}

func (s *PostgresStore) ListDreamableHubs() ([]model.Hub, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `
		SELECT h.id, h.name, COALESCE(h.icon, ''), COALESCE(h.accent, ''), h.slug, h.hub_type, COALESCE(h.plan, ''), h.owner_id,
			h.allow_contributor_topics, h.allow_contributor_dreams,
			COALESCE(h.contributor_delete_policy, 'own'),
			COALESCE(h.header_aurora_mode, ''),
			h.created_at, h.updated_at
		FROM hubs h
		LEFT JOIN hub_subscriptions hs ON hs.hub_id = h.id AND hs.status = 'active'
		LEFT JOIN users u ON h.owner_id = u.id
		WHERE (
			h.hub_type = 'personal'
			AND (
				(NULLIF(COALESCE(u.personal_plan_id, ''), '') <> '' AND u.personal_plan_id <> $1)
				OR
				(NULLIF(COALESCE(u.personal_plan_id, ''), '') = '' AND COALESCE(u.plan, $2) <> $2)
			)
		)
		   OR (
			h.hub_type = 'team'
			AND COALESCE(hs.plan_id, h.plan, '') IN ($3, $4, $5, $6)
		)
		ORDER BY h.created_at ASC`,
		model.PersonalFreePlanID,
		model.LegacyFreePlanID,
		model.HubTeamPlanID,
		model.HubEnterprisePlanID,
		model.LegacyTeamPlanID,
		model.LegacyEnterprisePlanID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hubs []model.Hub
	for rows.Next() {
		var h model.Hub
		if err := rows.Scan(&h.ID, &h.Name, &h.Icon, &h.Accent, &h.Slug, &h.HubType, &h.Plan, &h.OwnerID,
			&h.AllowContributorTopics, &h.AllowContributorDreams,
			&h.ContributorDeletePolicy,
			&h.HeaderAuroraMode,
			&h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		hubs = append(hubs, h)
	}
	if hubs == nil {
		hubs = []model.Hub{}
	}
	return hubs, rows.Err()
}

// --- Hub Members ---

func (s *PostgresStore) AddHubMember(hubID, userID, role string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (hub_id, user_id) DO UPDATE SET role = $3`,
		hubID, userID, role)
	return err
}

func (s *PostgresStore) RemoveHubMember(hubID, userID string) error {
	ctx := context.Background()
	result, err := s.pool.Exec(ctx,
		`DELETE FROM hub_members WHERE hub_id = $1 AND user_id = $2`, hubID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("member not found")
	}
	return nil
}

func (s *PostgresStore) ListHubMembers(hubID string) ([]model.HubMember, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT hm.hub_id, hm.user_id, hm.role, hm.joined_at,
			u.name, u.email, u.avatar_url
		FROM hub_members hm
		JOIN users u ON hm.user_id = u.id
		WHERE hm.hub_id = $1
		ORDER BY hm.role ASC, hm.joined_at ASC`, hubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []model.HubMember
	for rows.Next() {
		var m model.HubMember
		if err := rows.Scan(&m.HubID, &m.UserID, &m.Role, &m.JoinedAt,
			&m.UserName, &m.UserEmail, &m.UserAvatarURL); err != nil {
			continue
		}
		members = append(members, m)
	}
	return members, nil
}

// ListHubMembersPaginated is the paginated variant used by the admin
// hub detail member table. Sort order is (joined_at, user_id) ASC
// so the roster reads chronologically — oldest first — which gives
// admins a stable view anchored on the hub's early contributors.
// Cursor is the last user_id of the previous page.
//
// Query applies an ILIKE on u.name and u.email. A LIMIT of 50 is
// the default (capped at 100 at the handler) matching the other
// admin lists.
func (s *PostgresStore) ListHubMembersPaginated(ctx context.Context, hubID string, opts model.AdminHubMemberListOpts) ([]model.HubMember, string, int, error) {
	if opts.Limit <= 0 {
		opts.Limit = 50
	}

	conditions := []string{"hm.hub_id = $1"}
	args := []any{hubID}
	argN := 2

	if q := strings.TrimSpace(opts.Query); q != "" {
		conditions = append(conditions,
			fmt.Sprintf("(u.name ILIKE $%d OR u.email ILIKE $%d)", argN, argN))
		args = append(args, "%"+q+"%")
		argN++
	}
	if opts.Cursor != "" {
		// Keyset: (hm.joined_at, hm.user_id) > cursor row. Strict
		// forward-only pagination; Prev is client-side history.
		conditions = append(conditions, fmt.Sprintf(
			"(hm.joined_at, hm.user_id) > (SELECT joined_at, user_id FROM hub_members WHERE hub_id = $1 AND user_id = $%d::uuid)", argN))
		args = append(args, opts.Cursor)
		argN++
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	countConditions := conditions
	countArgs := args
	if opts.Cursor != "" {
		countConditions = countConditions[:len(countConditions)-1]
		countArgs = countArgs[:len(countArgs)-1]
	}
	countWhere := "WHERE " + strings.Join(countConditions, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM hub_members hm JOIN users u ON hm.user_id = u.id %s`,
		countWhere), countArgs...).Scan(&total); err != nil {
		return nil, "", 0, fmt.Errorf("count hub members: %w", err)
	}

	query := fmt.Sprintf(
		`SELECT hm.hub_id, hm.user_id, hm.role, hm.joined_at,
			u.name, u.email, u.avatar_url
		 FROM hub_members hm
		 JOIN users u ON hm.user_id = u.id
		 %s
		 ORDER BY hm.joined_at ASC, hm.user_id ASC
		 LIMIT %d`,
		where, opts.Limit+1,
	)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", 0, fmt.Errorf("list hub members: %w", err)
	}
	defer rows.Close()

	var members []model.HubMember
	for rows.Next() {
		var m model.HubMember
		if err := rows.Scan(&m.HubID, &m.UserID, &m.Role, &m.JoinedAt,
			&m.UserName, &m.UserEmail, &m.UserAvatarURL); err != nil {
			return nil, "", 0, err
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, "", 0, err
	}

	nextCursor := ""
	if len(members) > opts.Limit {
		nextCursor = members[opts.Limit-1].UserID
		members = members[:opts.Limit]
	}
	return members, nextCursor, total, nil
}

func (s *PostgresStore) GetHubMemberRole(hubID, userID string) (string, error) {
	ctx := context.Background()
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT role FROM hub_members WHERE hub_id = $1 AND user_id = $2`,
		hubID, userID).Scan(&role)
	if err != nil {
		return "", nil // not a member
	}
	return role, nil
}

func (s *PostgresStore) UpdateHubMemberRole(hubID, userID, role string) error {
	ctx := context.Background()
	result, err := s.pool.Exec(ctx,
		`UPDATE hub_members SET role = $3 WHERE hub_id = $1 AND user_id = $2`,
		hubID, userID, role)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("member not found")
	}
	return nil
}

func (s *PostgresStore) CountHubMembers(hubID string) (int, error) {
	ctx := context.Background()
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM hub_members WHERE hub_id = $1`, hubID).Scan(&count)
	return count, err
}

func (s *PostgresStore) GetHubVisit(userID, hubID string) (*model.HubVisit, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx, `
		SELECT user_id, hub_id, first_visited_at, last_visited_at, created_at, updated_at
		FROM hub_visits
		WHERE user_id = $1::uuid AND hub_id = $2::uuid
	`, userID, hubID)

	var visit model.HubVisit
	if err := row.Scan(
		&visit.UserID,
		&visit.HubID,
		&visit.FirstVisitedAt,
		&visit.LastVisitedAt,
		&visit.CreatedAt,
		&visit.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &visit, nil
}

func (s *PostgresStore) UpsertHubVisit(userID, hubID string, visitedAt time.Time) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO hub_visits (
			user_id,
			hub_id,
			first_visited_at,
			last_visited_at,
			created_at,
			updated_at
		)
		VALUES ($1::uuid, $2::uuid, $3, $3, now(), now())
		ON CONFLICT (user_id, hub_id)
		DO UPDATE SET
			last_visited_at = EXCLUDED.last_visited_at,
			updated_at = now()
	`, userID, hubID, visitedAt)
	return err
}

func (s *PostgresStore) DeleteHub(hubID string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1::uuid`, hubID)
	return err
}

// --- Hub Invites ---

func (s *PostgresStore) CreateHubInvite(invite *model.HubInvite) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO hub_invites (id, hub_id, token, invited_by, role, expires_at, created_at, invitee_user_id, invitee_email, email_enqueued_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		invite.ID, invite.HubID, invite.Token, invite.InvitedBy, invite.Role, invite.ExpiresAt, invite.CreatedAt, invite.InviteeUserID, invite.InviteeEmail, invite.EmailEnqueuedAt)
	return err
}
func (s *PostgresStore) GetHubInvite(id string) (*model.HubInvite, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT id, hub_id, token, invited_by, role, expires_at, accepted_by, accepted_at, revoked_by, revoked_at, created_at, invitee_user_id, invitee_email, email_enqueued_at
		FROM hub_invites WHERE id = $1::uuid`, id)
	return scanHubInvite(row)
}

func (s *PostgresStore) GetHubInviteByToken(token string) (*model.HubInvite, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT id, hub_id, token, invited_by, role, expires_at, accepted_by, accepted_at, revoked_by, revoked_at, created_at, invitee_user_id, invitee_email, email_enqueued_at
		FROM hub_invites WHERE token = $1 AND revoked_at IS NULL`, token)
	return scanHubInvite(row)
}

func (s *PostgresStore) ListOutstandingHubInvites(hubID string) ([]model.HubInvite, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT id, hub_id, token, invited_by, role, expires_at, accepted_by, accepted_at, revoked_by, revoked_at, created_at, invitee_user_id, invitee_email, email_enqueued_at
		FROM hub_invites
		WHERE hub_id = $1::uuid
		  AND accepted_by IS NULL
		  AND revoked_at IS NULL
		  AND expires_at > NOW()
		ORDER BY created_at DESC`, hubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invites []model.HubInvite
	for rows.Next() {
		invite, err := scanHubInvite(rows)
		if err != nil {
			return nil, err
		}
		invites = append(invites, *invite)
	}
	if invites == nil {
		invites = []model.HubInvite{}
	}
	return invites, rows.Err()
}

// enforceMemberCap acquires a per-hub advisory lock and checks the member count
// against the cap. Must be called inside an existing transaction. Returns
// ErrMemberCapExceeded if at or over cap. maxMembers <= 0 skips the check.
func enforceMemberCap(ctx context.Context, tx pgx.Tx, hubID string, maxMembers int) error {
	if maxMembers <= 0 {
		return nil // unlimited
	}
	// Advisory lock scoped to this hub's member mutations
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock($1, hashtext($2))`,
		advisoryLockNSMemberCap, hubID,
	); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	var currentCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM hub_members WHERE hub_id = $1`, hubID,
	).Scan(&currentCount); err != nil {
		return fmt.Errorf("count hub members: %w", err)
	}
	if currentCount >= maxMembers {
		return ErrMemberCapExceeded
	}
	return nil
}

// AcceptHubInvite accepts a hub invite by token with atomic member cap enforcement.
// maxMembers <= 0 means unlimited. Returns ErrMemberCapExceeded if at cap.
func (s *PostgresStore) AcceptHubInvite(token string, userID string, maxMembers int) (*model.HubInvite, error) {
	ctx := context.Background()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	invite, err := scanHubInvite(tx.QueryRow(ctx,
		`SELECT id, hub_id, token, invited_by, role, expires_at, accepted_by, accepted_at, revoked_by, revoked_at, created_at, invitee_user_id, invitee_email, email_enqueued_at
		FROM hub_invites
		WHERE token = $1
		FOR UPDATE`,
		token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrHubInviteNotFound
		}
		return nil, err
	}

	now := time.Now()
	if invite.RevokedAt != nil {
		return nil, ErrHubInviteRevoked
	}
	if now.After(invite.ExpiresAt) {
		return nil, ErrHubInviteExpired
	}
	if invite.AcceptedBy != nil {
		return nil, ErrHubInviteUsed
	}
	if invite.InviteeUserID != nil && *invite.InviteeUserID != "" && *invite.InviteeUserID != userID {
		return nil, ErrHubInviteWrongInvitee
	}

	// Enforce member cap inside the transaction with advisory lock.
	// This prevents two concurrent accepts from both passing the count check.
	if err := enforceMemberCap(ctx, tx, invite.HubID, maxMembers); err != nil {
		return nil, err
	}

	result, err := tx.Exec(ctx,
		`INSERT INTO hub_members (hub_id, user_id, role)
		VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT (hub_id, user_id) DO NOTHING`,
		invite.HubID, userID, invite.Role)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		return nil, ErrHubAlreadyMember
	}

	result, err = tx.Exec(ctx,
		`UPDATE hub_invites
		SET accepted_by = $1::uuid, accepted_at = $2
		WHERE id = $3::uuid
		  AND accepted_by IS NULL
		  AND revoked_at IS NULL`,
		userID, now, invite.ID)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		return nil, ErrHubInviteUsed
	}

	invite.AcceptedBy = &userID
	invite.AcceptedAt = &now

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return invite, nil
}

func (s *PostgresStore) RevokeHubInvite(hubID, inviteID, userID string) error {
	ctx := context.Background()
	now := time.Now()
	_, err := s.pool.Exec(ctx,
		`UPDATE hub_invites
		SET revoked_by = $1::uuid, revoked_at = $2
		WHERE id = $3::uuid
		  AND hub_id = $4::uuid
		  AND accepted_by IS NULL
		  AND revoked_at IS NULL`,
		userID, now, inviteID, hubID)
	return err
}

type hubInviteScanner interface {
	Scan(dest ...any) error
}

func scanHubInvite(scanner hubInviteScanner) (*model.HubInvite, error) {
	var inv model.HubInvite
	err := scanner.Scan(&inv.ID, &inv.HubID, &inv.Token, &inv.InvitedBy, &inv.Role,
		&inv.ExpiresAt, &inv.AcceptedBy, &inv.AcceptedAt, &inv.RevokedBy, &inv.RevokedAt, &inv.CreatedAt, &inv.InviteeUserID,
		&inv.InviteeEmail, &inv.EmailEnqueuedAt)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// CountActiveHubInvites returns the number of outstanding (active) invites
// for a hub — not expired, not accepted, not revoked. Used for quota enforcement.
func (s *PostgresStore) CountActiveHubInvites(hubID string) (int, error) {
	ctx := context.Background()
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM hub_invites
		WHERE hub_id = $1::uuid
		  AND accepted_by IS NULL
		  AND revoked_at IS NULL
		  AND expires_at > now()`,
		hubID).Scan(&count)
	return count, err
}

// UpdateHubInviteEmailEnqueuedAt sets the email_enqueued_at timestamp for
// an invite. Called after enqueuing an email job (create or resend).
// Best-effort: callers log failures but do not propagate them since the
// field is advisory (UI convenience), not load-bearing.
func (s *PostgresStore) UpdateHubInviteEmailEnqueuedAt(inviteID string, t time.Time) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`UPDATE hub_invites SET email_enqueued_at = $1 WHERE id = $2::uuid`,
		t, inviteID)
	return err
}

// ListExpiringHubInvites returns hub invites with an email address that
// expire within the given duration. Used by the reminder worker to
// send "expiring soon" emails.
func (s *PostgresStore) ListExpiringHubInvites(within time.Duration) ([]model.HubInvite, error) {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT id, hub_id, token, invited_by, role, expires_at, accepted_by, accepted_at, revoked_by, revoked_at, created_at, invitee_user_id, invitee_email, email_enqueued_at
		FROM hub_invites
		WHERE invitee_email IS NOT NULL
		  AND accepted_by IS NULL
		  AND revoked_at IS NULL
		  AND expires_at > now()
		  AND expires_at <= now() + $1::interval
		ORDER BY expires_at ASC`,
		within)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invites []model.HubInvite
	for rows.Next() {
		invite, err := scanHubInvite(rows)
		if err != nil {
			return nil, err
		}
		invites = append(invites, *invite)
	}
	return invites, rows.Err()
}

type hubOwnershipTransferScanner interface {
	Scan(dest ...any) error
}

func (s *PostgresStore) CreateHubOwnershipTransfer(transfer *model.HubOwnershipTransfer) error {
	ctx := context.Background()
	if existing, err := s.GetActiveHubOwnershipTransfer(transfer.HubID); err == nil && existing != nil {
		return ErrHubTransferExists
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO hub_ownership_transfers (id, hub_id, initiated_by, target_user_id, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		transfer.ID, transfer.HubID, transfer.InitiatedBy, transfer.TargetUserID, transfer.ExpiresAt, transfer.CreatedAt)
	return err
}

func (s *PostgresStore) GetHubOwnershipTransfer(id string) (*model.HubOwnershipTransfer, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT hot.id, hot.hub_id, hot.initiated_by, hot.target_user_id, hot.accepted_at, hot.cancelled_at,
			hot.expires_at, hot.created_at, COALESCE(u.name, ''), COALESCE(u.email, '')
		FROM hub_ownership_transfers hot
		JOIN users u ON u.id = hot.target_user_id
		WHERE hot.id = $1::uuid`, id)
	return scanHubOwnershipTransfer(row)
}

func (s *PostgresStore) GetActiveHubOwnershipTransfer(hubID string) (*model.HubOwnershipTransfer, error) {
	ctx := context.Background()
	row := s.pool.QueryRow(ctx,
		`SELECT hot.id, hot.hub_id, hot.initiated_by, hot.target_user_id, hot.accepted_at, hot.cancelled_at,
			hot.expires_at, hot.created_at, COALESCE(u.name, ''), COALESCE(u.email, '')
		FROM hub_ownership_transfers hot
		JOIN users u ON u.id = hot.target_user_id
		WHERE hot.hub_id = $1::uuid
		  AND hot.accepted_at IS NULL
		  AND hot.cancelled_at IS NULL
		  AND hot.expires_at > NOW()
		ORDER BY hot.created_at DESC
		LIMIT 1`, hubID)
	return scanHubOwnershipTransfer(row)
}

// AcceptHubOwnershipTransfer completes a pending transfer with atomic ownership
// cap enforcement. maxFreeTeamHubs is the target user's cap from their personal
// plan. If the hub is hub_free_team and the target is at or over cap, returns
// ErrOwnershipCapExceeded. Pass -1 to skip the cap check (for paid hubs or
// when the caller has already verified the hub is not hub_free_team).
func (s *PostgresStore) AcceptHubOwnershipTransfer(hubID, transferID, userID string, maxFreeTeamHubs int) (*model.HubOwnershipTransfer, error) {
	ctx := context.Background()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	transfer, err := scanHubOwnershipTransfer(tx.QueryRow(ctx,
		`SELECT hot.id, hot.hub_id, hot.initiated_by, hot.target_user_id, hot.accepted_at, hot.cancelled_at,
			hot.expires_at, hot.created_at, COALESCE(u.name, ''), COALESCE(u.email, '')
		FROM hub_ownership_transfers hot
		JOIN users u ON u.id = hot.target_user_id
		WHERE hot.id = $1::uuid AND hot.hub_id = $2::uuid
		FOR UPDATE`, transferID, hubID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrHubTransferNotFound
		}
		return nil, err
	}

	now := time.Now()
	if transfer.CancelledAt != nil {
		return nil, ErrHubTransferCanceled
	}
	if transfer.AcceptedAt != nil {
		return nil, ErrHubTransferAccepted
	}
	if now.After(transfer.ExpiresAt) {
		return nil, ErrHubTransferExpired
	}
	if transfer.TargetUserID != userID {
		return nil, ErrHubTransferNotFound
	}

	var oldOwnerID string
	err = tx.QueryRow(ctx, `SELECT owner_id FROM hubs WHERE id = $1::uuid FOR UPDATE`, hubID).Scan(&oldOwnerID)
	if err != nil {
		return nil, err
	}

	// Enforce ownership cap for hub_free_team hubs.
	// Check the hub's subscription plan inside the transaction, then acquire
	// the target owner's advisory lock and count their free team hubs.
	// DB errors fail closed — a missing subscription or transient failure
	// must not silently skip the cap check.
	if maxFreeTeamHubs >= 0 {
		var hubPlanID string
		planErr := tx.QueryRow(ctx,
			`SELECT plan_id FROM hub_subscriptions WHERE hub_id = $1 AND status = 'active'`,
			hubID,
		).Scan(&hubPlanID)
		if planErr != nil {
			if errors.Is(planErr, pgx.ErrNoRows) {
				// Every team hub should have an active subscription (invariant
				// from CreateTeamHub + migration 066 backfill). If missing,
				// fail the transfer rather than silently skipping the cap.
				return nil, fmt.Errorf("hub %s has no active subscription", hubID)
			}
			return nil, fmt.Errorf("get hub subscription plan: %w", planErr)
		}
		if hubPlanID == model.HubFreeTeamPlanID {
			// Lock the target owner's ownership namespace
			if _, err := tx.Exec(ctx,
				`SELECT pg_advisory_xact_lock($1, hashtext($2))`,
				advisoryLockNSOwnership, userID,
			); err != nil {
				return nil, fmt.Errorf("advisory lock: %w", err)
			}

			var currentCount int
			if err := tx.QueryRow(ctx,
				`SELECT COUNT(*)
				 FROM hubs h
				 JOIN hub_subscriptions hs ON hs.hub_id = h.id AND hs.status = 'active'
				 WHERE h.owner_id = $1 AND h.hub_type = 'team' AND hs.plan_id = $2`,
				userID, model.HubFreeTeamPlanID,
			).Scan(&currentCount); err != nil {
				return nil, fmt.Errorf("count owned free team hubs: %w", err)
			}
			if currentCount >= maxFreeTeamHubs {
				return nil, ErrOwnershipCapExceeded
			}
		}
	}

	demotion, err := tx.Exec(ctx,
		`UPDATE hub_members SET role = 'admin' WHERE hub_id = $1::uuid AND user_id = $2::uuid`,
		hubID, oldOwnerID)
	if err != nil {
		return nil, err
	}
	if demotion.RowsAffected() != 1 {
		return nil, ErrHubTransferInvalid
	}
	promotion, err := tx.Exec(ctx,
		`UPDATE hub_members SET role = 'owner' WHERE hub_id = $1::uuid AND user_id = $2::uuid`,
		hubID, userID)
	if err != nil {
		return nil, err
	}
	if promotion.RowsAffected() != 1 {
		return nil, ErrHubTransferInvalid
	}
	if _, err := tx.Exec(ctx,
		`UPDATE hubs SET owner_id = $2::uuid, updated_at = NOW() WHERE id = $1::uuid`,
		hubID, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE hub_ownership_transfers SET accepted_at = $3 WHERE id = $1::uuid AND hub_id = $2::uuid`,
		transferID, hubID, now); err != nil {
		return nil, err
	}

	transfer.AcceptedAt = &now
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return transfer, nil
}

// CancelHubOwnershipTransfer marks a pending ownership transfer as
// cancelled. Returns typed sentinels so the handler can distinguish
// "not found", "already accepted", and "already cancelled" from a
// genuine state change — the previous version silently no-oped on
// terminal state and returned nil, which let the handler report
// status=canceled for a transfer that was actually already accepted
// and fire the notification convergence path for a no-op.
//
// Reads state inside a SELECT ... FOR UPDATE to avoid racing a
// concurrent accept.
func (s *PostgresStore) CancelHubOwnershipTransfer(hubID, transferID string) error {
	ctx := context.Background()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var acceptedAt, cancelledAt *time.Time
	err = tx.QueryRow(ctx,
		`SELECT accepted_at, cancelled_at
		FROM hub_ownership_transfers
		WHERE id = $1::uuid AND hub_id = $2::uuid
		FOR UPDATE`, transferID, hubID).Scan(&acceptedAt, &cancelledAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrHubTransferNotFound
		}
		return err
	}
	if acceptedAt != nil {
		return ErrHubTransferAccepted
	}
	if cancelledAt != nil {
		return ErrHubTransferCanceled
	}
	if _, err := tx.Exec(ctx,
		`UPDATE hub_ownership_transfers
		SET cancelled_at = NOW()
		WHERE id = $1::uuid AND hub_id = $2::uuid`,
		transferID, hubID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanHubOwnershipTransfer(scanner hubOwnershipTransferScanner) (*model.HubOwnershipTransfer, error) {
	var transfer model.HubOwnershipTransfer
	err := scanner.Scan(
		&transfer.ID,
		&transfer.HubID,
		&transfer.InitiatedBy,
		&transfer.TargetUserID,
		&transfer.AcceptedAt,
		&transfer.CancelledAt,
		&transfer.ExpiresAt,
		&transfer.CreatedAt,
		&transfer.TargetUserName,
		&transfer.TargetUserEmail,
	)
	if err != nil {
		return nil, err
	}
	return &transfer, nil
}

// --- Users (extended) ---

func (s *PostgresStore) GetUser(id string) (*model.User, error) {
	ctx := context.Background()
	// github_id is nullable for google-only accounts; COALESCE to 0
	// so Scan into int64 doesn't fail on the common no-github case.
	// Mirrors GetUserByCanonicalEmail / GetUserByAuthIdentity which
	// already handle this.
	row := s.pool.QueryRow(ctx,
		`SELECT id, COALESCE(github_id, 0), email, name, COALESCE(display_name, ''), avatar_url,
			COALESCE(plan, 'free'), personal_plan_id, created_at, updated_at
		FROM users WHERE id = $1`, id)
	var u model.User
	err := row.Scan(&u.ID, &u.GitHubID, &u.Email, &u.Name, &u.DisplayName,
		&u.AvatarURL, &u.Plan, &u.PersonalPlanID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}
	return &u, nil
}
func (s *PostgresStore) GetUsersByIDs(ids []string) (map[string]*model.User, error) {
	if len(ids) == 0 {
		return map[string]*model.User{}, nil
	}
	ctx := context.Background()
	// Build $1, $2, ... placeholders
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	query := fmt.Sprintf(
		`SELECT id, COALESCE(github_id, 0), email, name, COALESCE(display_name, ''), avatar_url,
			COALESCE(plan, 'free'), personal_plan_id, created_at, updated_at
		FROM users WHERE id IN (%s)`,
		strings.Join(placeholders, ","),
	)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]*model.User, len(ids))
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.GitHubID, &u.Email, &u.Name, &u.DisplayName,
			&u.AvatarURL, &u.Plan, &u.PersonalPlanID, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		result[u.ID] = &u
	}
	return result, nil
}

func (s *PostgresStore) UpdateUserProfile(id string, displayName string) error {
	ctx := context.Background()
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET display_name = $2, updated_at = now() WHERE id = $1`,
		id, displayName)
	return err
}

// --- Usage ---

func (s *PostgresStore) IncrementUsage(userID string, field string) error {
	ctx := context.Background()
	// Upsert current month's usage row, increment the specified field
	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, -1)

	var col string
	switch field {
	case "push_count":
		col = "push_count"
	case "recall_count":
		col = "recall_count"
	case "ask_count":
		col = "ask_count"
	default:
		return fmt.Errorf("unknown usage field: %s", field)
	}

	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO usage (id, user_id, period_start, period_end, %s)
		VALUES (gen_random_uuid(), $1, $2, $3, 1)
		ON CONFLICT (user_id, period_start) DO UPDATE SET %s = usage.%s + 1, updated_at = now()`,
		col, col, col),
		userID, periodStart, periodEnd)
	return err
}

func (s *PostgresStore) IncrementUsageReturning(userID string, field string) (int, error) {
	ctx := context.Background()
	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, -1)

	var col string
	switch field {
	case "push_count":
		col = "push_count"
	case "recall_count":
		col = "recall_count"
	case "ask_count":
		col = "ask_count"
	default:
		return 0, fmt.Errorf("unknown usage field: %s", field)
	}

	var newCount int
	err := s.pool.QueryRow(ctx, fmt.Sprintf(
		`INSERT INTO usage (id, user_id, period_start, period_end, %s)
		VALUES (gen_random_uuid(), $1, $2, $3, 1)
		ON CONFLICT (user_id, period_start) DO UPDATE SET %s = usage.%s + 1, updated_at = now()
		RETURNING %s`,
		col, col, col, col),
		userID, periodStart, periodEnd).Scan(&newCount)
	return newCount, err
}

func (s *PostgresStore) DecrementUsage(userID string, field string) error {
	ctx := context.Background()
	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	var col string
	switch field {
	case "push_count":
		col = "push_count"
	case "recall_count":
		col = "recall_count"
	case "ask_count":
		col = "ask_count"
	default:
		return fmt.Errorf("unknown usage field: %s", field)
	}

	// Decrement, clamped at zero to prevent negative values.
	_, err := s.pool.Exec(ctx, fmt.Sprintf(
		`UPDATE usage SET %s = GREATEST(%s - 1, 0), updated_at = now()
		WHERE user_id = $1 AND period_start = $2`,
		col, col),
		userID, periodStart)
	return err
}

func (s *PostgresStore) GetCurrentUsage(userID string) (*model.Usage, error) {
	ctx := context.Background()
	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	var u model.Usage
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, period_start::text, period_end::text, push_count, recall_count, ask_count
		FROM usage WHERE user_id = $1 AND period_start = $2`,
		userID, periodStart).Scan(&u.UserID, &u.PeriodStart, &u.PeriodEnd,
		&u.PushCount, &u.RecallCount, &u.AskCount)
	if err != nil {
		// No usage this period yet
		return &model.Usage{
			UserID:      userID,
			PeriodStart: periodStart.Format("2006-01-02"),
			PeriodEnd:   periodStart.AddDate(0, 1, -1).Format("2006-01-02"),
		}, nil
	}
	return &u, nil
}

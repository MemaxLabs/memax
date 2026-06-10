package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// --- Waitlist Entries ---

// CreateWaitlistEntry inserts a new waitlist entry with normalized email.
func (s *PostgresStore) CreateWaitlistEntry(ctx context.Context, entry *model.WaitlistEntry) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO waitlist (email, use_case, ai_tools, role, status, created_at, updated_at)
		 VALUES (LOWER(BTRIM($1)), $2, $3, $4, $5, now(), now())
		 RETURNING id, email, created_at, updated_at`,
		entry.Email, entry.UseCase, entry.AITools, entry.Role, model.WaitlistStatusPending,
	).Scan(&entry.ID, &entry.Email, &entry.CreatedAt, &entry.UpdatedAt)
}

// GetWaitlistEntry returns a waitlist entry by ID.
func (s *PostgresStore) GetWaitlistEntry(ctx context.Context, id string) (*model.WaitlistEntry, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, email, use_case, ai_tools, role, status, wave, notes,
		        approved_by, approved_at, created_at, updated_at
		 FROM waitlist WHERE id = $1::uuid`, id)
	return scanWaitlistEntry(row)
}

// GetWaitlistEntryByEmail returns a waitlist entry by normalized email.
func (s *PostgresStore) GetWaitlistEntryByEmail(ctx context.Context, email string) (*model.WaitlistEntry, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	row := s.pool.QueryRow(ctx,
		`SELECT id, email, use_case, ai_tools, role, status, wave, notes,
		        approved_by, approved_at, created_at, updated_at
		 FROM waitlist WHERE LOWER(BTRIM(email)) = $1`, normalized)
	return scanWaitlistEntry(row)
}

// ListWaitlistEntries returns paginated, filtered waitlist entries for admin listing.
func (s *PostgresStore) ListWaitlistEntries(ctx context.Context, opts model.WaitlistListOpts) ([]model.WaitlistEntry, string, int, error) {
	// Build dynamic WHERE clause
	var conditions []string
	var args []any
	paramN := 1

	if opts.Status != "" {
		conditions = append(conditions, fmt.Sprintf("w.status = $%d", paramN))
		args = append(args, opts.Status)
		paramN++
	}
	if opts.UseCase != "" {
		conditions = append(conditions, fmt.Sprintf("w.use_case = $%d", paramN))
		args = append(args, opts.UseCase)
		paramN++
	}
	if opts.Query != "" {
		normalized := strings.ToLower(strings.TrimSpace(opts.Query))
		conditions = append(conditions, fmt.Sprintf("LOWER(BTRIM(w.email)) LIKE '%%' || $%d || '%%'", paramN))
		args = append(args, normalized)
		paramN++
	}
	// Sort — cursor pagination only supported for created_at sort.
	sortCol := "w.created_at"
	if opts.Sort == "email" {
		sortCol = "LOWER(BTRIM(w.email))"
	}
	order := "DESC"
	if opts.Order == "asc" {
		order = "ASC"
	}

	if opts.Cursor != "" {
		// Cursor pagination only works with created_at sort.
		if sortCol != "w.created_at" {
			return nil, "", 0, fmt.Errorf("cursor pagination requires sort=created_at")
		}
		// Direction-aware: DESC uses <, ASC uses >
		op := "<"
		if order == "ASC" {
			op = ">"
		}
		conditions = append(conditions, fmt.Sprintf("w.created_at %s $%d", op, paramN))
		args = append(args, opts.Cursor)
		paramN++
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	// Count total (unfiltered by cursor for total_count)
	countWhere := where
	if opts.Cursor != "" && len(conditions) > 1 {
		// Remove cursor condition for total count
		countConditions := conditions[:len(conditions)-1]
		countWhere = "WHERE " + strings.Join(countConditions, " AND ")
	} else if opts.Cursor != "" && len(conditions) == 1 {
		countWhere = ""
	}

	var totalCount int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM waitlist w %s", countWhere)
	countArgs := args
	if opts.Cursor != "" {
		countArgs = args[:len(args)-1] // exclude cursor arg
	}
	if err := s.pool.QueryRow(ctx, countQuery, countArgs...).Scan(&totalCount); err != nil {
		return nil, "", 0, err
	}

	// Fetch entries with latest invite status via lateral join, plus the
	// invite-consumer's user row (name/email) so the admin list can
	// crosslink "used by ..." without a per-row fetch.
	//
	// Keep `li.used_by` typed as uuid inside the lateral subquery so
	// the outer LEFT JOIN on `u.id = li.used_by` can use the
	// users_pkey index directly. Casting either side to text would
	// force a sequential scan on users for every waitlist page load.
	// The cast happens only in the SELECT projection so we can scan
	// into a Go *string.
	query := fmt.Sprintf(
		`SELECT w.id, w.email, w.use_case, w.ai_tools, w.role, w.status, w.wave, w.notes,
		        w.approved_by, w.approved_at, w.created_at, w.updated_at,
		        li.status, li.used_at, li.used_by::text, u.email, u.name
		 FROM waitlist w
		 LEFT JOIN LATERAL (
		   SELECT status, used_at, used_by
		   FROM waitlist_invites
		   WHERE waitlist_id = w.id
		   ORDER BY created_at DESC LIMIT 1
		 ) li ON true
		 LEFT JOIN users u ON u.id = li.used_by
		 %s
		 ORDER BY %s %s
		 LIMIT %d`,
		where, sortCol, order, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", 0, err
	}
	defer rows.Close()

	entries, err := scanWaitlistEntriesWithInvite(rows)
	if err != nil {
		return nil, "", 0, err
	}

	var nextCursor string
	if len(entries) > limit {
		entries = entries[:limit]
		last := entries[len(entries)-1]
		nextCursor = last.CreatedAt.Format(time.RFC3339Nano)
	}

	return entries, nextCursor, totalCount, nil
}

// UpdateWaitlistEntry updates mutable fields on a waitlist entry.
func (s *PostgresStore) UpdateWaitlistEntry(ctx context.Context, id string, status string, notes string, wave *int, approvedBy string) (*model.WaitlistEntry, error) {
	var setClauses []string
	var args []any
	paramN := 1

	// ID is always the last param
	setClauses = append(setClauses, "updated_at = now()")

	if status != "" {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", paramN))
		args = append(args, status)
		paramN++
	}
	if notes != "" {
		setClauses = append(setClauses, fmt.Sprintf("notes = $%d", paramN))
		args = append(args, notes)
		paramN++
	}
	if wave != nil {
		setClauses = append(setClauses, fmt.Sprintf("wave = $%d", paramN))
		args = append(args, *wave)
		paramN++
	}
	if approvedBy != "" && status == model.WaitlistStatusApproved {
		setClauses = append(setClauses, fmt.Sprintf("approved_by = $%d::uuid", paramN))
		args = append(args, approvedBy)
		paramN++
		setClauses = append(setClauses, "approved_at = now()")
	}

	args = append(args, id)
	idParam := fmt.Sprintf("$%d", paramN)

	query := fmt.Sprintf(
		`UPDATE waitlist SET %s
		 WHERE id = %s::uuid
		 RETURNING id, email, use_case, ai_tools, role, status, wave, notes,
		           approved_by, approved_at, created_at, updated_at`,
		strings.Join(setClauses, ", "), idParam)

	row := s.pool.QueryRow(ctx, query, args...)
	return scanWaitlistEntry(row)
}

// BatchApproveWaitlist sets status=approved for the given IDs and returns the count updated.
func (s *PostgresStore) BatchApproveWaitlist(ctx context.Context, ids []string, wave int, approvedBy string, notes string) (int, error) {
	result, err := s.pool.Exec(ctx,
		`UPDATE waitlist SET status = $1, wave = $2, approved_by = $3::uuid,
		        notes = CASE WHEN $6 = '' THEN notes ELSE $6 END,
		        approved_at = now(), updated_at = now()
		 WHERE id = ANY($4::uuid[]) AND status = $5`,
		model.WaitlistStatusApproved, wave, approvedBy, ids, model.WaitlistStatusPending, notes)
	if err != nil {
		return 0, err
	}
	return int(result.RowsAffected()), nil
}

// GetWaitlistStats returns aggregate counts for the admin dashboard.
func (s *PostgresStore) GetWaitlistStats(ctx context.Context) (*model.WaitlistStats, error) {
	stats := &model.WaitlistStats{}

	err := s.pool.QueryRow(ctx,
		`SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'pending'),
			COUNT(*) FILTER (WHERE status = 'approved'),
			COUNT(*) FILTER (WHERE status = 'registered'),
			COUNT(*) FILTER (WHERE status = 'rejected')
		 FROM waitlist`).Scan(
		&stats.Total, &stats.Pending, &stats.Approved, &stats.Registered, &stats.Rejected)
	if err != nil {
		return nil, err
	}

	stats.CapacityUsed = stats.Registered

	// Wave stats
	rows, err := s.pool.Query(ctx,
		`SELECT wave,
			COUNT(*) FILTER (WHERE status = 'approved' OR status = 'registered'),
			COUNT(*) FILTER (WHERE status = 'registered'),
			MIN(approved_at)
		 FROM waitlist
		 WHERE wave IS NOT NULL
		 GROUP BY wave
		 ORDER BY wave`)
	if err != nil {
		return stats, nil // partial result OK
	}
	defer rows.Close()

	for rows.Next() {
		var ws model.WaveStats
		if err := rows.Scan(&ws.Wave, &ws.Approved, &ws.Registered, &ws.StartedAt); err != nil {
			continue
		}
		stats.Waves = append(stats.Waves, ws)
	}

	return stats, nil
}

// --- Waitlist Invites ---

// CreateWaitlistInvite inserts a new invite token for a waitlist entry.
func (s *PostgresStore) CreateWaitlistInvite(ctx context.Context, invite *model.WaitlistInvite) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO waitlist_invites (waitlist_id, email, token, status, expires_at, created_at)
		 VALUES ($1::uuid, LOWER(BTRIM($2)), $3, $4, $5, now())
		 RETURNING id, created_at`,
		invite.WaitlistID, invite.Email, invite.Token, model.WaitlistInviteStatusActive, invite.ExpiresAt,
	).Scan(&invite.ID, &invite.CreatedAt)
}

// GetWaitlistInviteByToken returns an invite by its token value.
func (s *PostgresStore) GetWaitlistInviteByToken(ctx context.Context, token string) (*model.WaitlistInvite, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, waitlist_id, email, token, status, expires_at, used_at, used_by, created_at
		 FROM waitlist_invites WHERE token = $1`, token)
	var inv model.WaitlistInvite
	err := row.Scan(&inv.ID, &inv.WaitlistID, &inv.Email, &inv.Token,
		&inv.Status, &inv.ExpiresAt, &inv.UsedAt, &inv.UsedBy, &inv.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("invite not found: %w", err)
	}
	return &inv, nil
}

// ListOrphanWaitlistCandidates finds users with active, not-yet-used
// waitlist invites that never got consumed — the stuck state from the
// 2026-04 regression where the web client dropped the invite token on
// the OAuth redirect. An orphan is:
//
//   - a user row whose personal_plan_id is empty or personal_free
//     (never upgraded by consume),
//   - and users.invite_id IS NULL (never linked),
//   - matching an active waitlist_invites row on lower+trimmed email,
//   - where the invite hasn't expired.
//
// Email matches use LOWER(BTRIM(...)) on both sides because both
// tables normalize that way at write time (GetUserByCanonicalEmail
// docstring in store.go; CreateWaitlistInvite normalizes via
// LOWER(BTRIM) too). created_at filter (opts.Since) is required so a
// dashboard accident can't trigger a full-users scan.
//
// ORDER BY u.created_at DESC so the most recent offenders surface
// first — operators typically want to see "what's broken today" over
// "what got stuck months ago." Limit defaults to 100, capped at 500.
func (s *PostgresStore) ListOrphanWaitlistCandidates(ctx context.Context, opts model.OrphanWaitlistCandidateOpts) ([]model.OrphanWaitlistCandidate, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx,
		`SELECT u.id, u.email, u.created_at,
		        COALESCE(u.personal_plan_id, ''),
		        wi.id, wi.token, wi.expires_at, wi.waitlist_id
		 FROM users u
		 JOIN waitlist_invites wi
		   ON LOWER(BTRIM(wi.email)) = LOWER(BTRIM(u.email))
		 WHERE wi.status = $1
		   AND wi.expires_at > now()
		   AND u.invite_id IS NULL
		   AND (u.personal_plan_id IS NULL
		        OR u.personal_plan_id = ''
		        OR u.personal_plan_id = $2)
		   AND u.created_at >= $3
		 ORDER BY u.created_at DESC
		 LIMIT $4`,
		model.WaitlistInviteStatusActive, model.PersonalFreePlanID, opts.Since, limit)
	if err != nil {
		return nil, fmt.Errorf("list orphan waitlist candidates: %w", err)
	}
	defer rows.Close()

	var out []model.OrphanWaitlistCandidate
	for rows.Next() {
		var c model.OrphanWaitlistCandidate
		if err := rows.Scan(&c.UserID, &c.UserEmail, &c.UserCreatedAt,
			&c.UserPlanID, &c.InviteID, &c.InviteToken,
			&c.InviteExpiresAt, &c.WaitlistID); err != nil {
			return nil, fmt.Errorf("scan orphan row: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetOrphanCandidateForUser is the per-user variant of
// ListOrphanWaitlistCandidates: same JOIN predicates, scoped to a
// specific users.id, no created_at window and no LIMIT. Backs the
// POST /v1/admin/waitlist/reconcile path, which must never
// false-negative "not_an_orphan" because a newer orphan exceeded
// a bulk limit. Returns (nil, nil) when the user is not an orphan
// — the handler treats that as a skip, not an error.
func (s *PostgresStore) GetOrphanCandidateForUser(ctx context.Context, userID string) (*model.OrphanWaitlistCandidate, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT u.id, u.email, u.created_at,
		        COALESCE(u.personal_plan_id, ''),
		        wi.id, wi.token, wi.expires_at, wi.waitlist_id
		 FROM users u
		 JOIN waitlist_invites wi
		   ON LOWER(BTRIM(wi.email)) = LOWER(BTRIM(u.email))
		 WHERE u.id = $1::uuid
		   AND wi.status = $2
		   AND wi.expires_at > now()
		   AND u.invite_id IS NULL
		   AND (u.personal_plan_id IS NULL
		        OR u.personal_plan_id = ''
		        OR u.personal_plan_id = $3)
		 ORDER BY wi.created_at DESC
		 LIMIT 1`,
		userID, model.WaitlistInviteStatusActive, model.PersonalFreePlanID)
	var c model.OrphanWaitlistCandidate
	err := row.Scan(&c.UserID, &c.UserEmail, &c.UserCreatedAt,
		&c.UserPlanID, &c.InviteID, &c.InviteToken,
		&c.InviteExpiresAt, &c.WaitlistID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get orphan candidate for user: %w", err)
	}
	return &c, nil
}

// ConsumeWaitlistInvite marks an invite as used by a specific user.
func (s *PostgresStore) ConsumeWaitlistInvite(ctx context.Context, token string, userID string) error {
	result, err := s.pool.Exec(ctx,
		`UPDATE waitlist_invites SET status = $1, used_at = now(), used_by = $2::uuid
		 WHERE token = $3 AND status = $4`,
		model.WaitlistInviteStatusUsed, userID, token, model.WaitlistInviteStatusActive)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("invite not found or already consumed")
	}
	return nil
}

// ExpireWaitlistInvites bulk-expires invites past their expiration time.
// Returns the number of invites expired.
func (s *PostgresStore) ExpireWaitlistInvites(ctx context.Context) (int, error) {
	result, err := s.pool.Exec(ctx,
		`UPDATE waitlist_invites SET status = $1
		 WHERE status = $2 AND expires_at < now()`,
		model.WaitlistInviteStatusExpired, model.WaitlistInviteStatusActive)
	if err != nil {
		return 0, err
	}
	return int(result.RowsAffected()), nil
}

// ListWaitlistInvitesByEntry returns all invites for a specific waitlist entry.
func (s *PostgresStore) ListWaitlistInvitesByEntry(ctx context.Context, waitlistID string) ([]model.WaitlistInvite, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, waitlist_id, email, token, status, expires_at, used_at, used_by, created_at
		 FROM waitlist_invites WHERE waitlist_id = $1::uuid
		 ORDER BY created_at DESC`, waitlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invites []model.WaitlistInvite
	for rows.Next() {
		var inv model.WaitlistInvite
		if err := rows.Scan(&inv.ID, &inv.WaitlistID, &inv.Email, &inv.Token,
			&inv.Status, &inv.ExpiresAt, &inv.UsedAt, &inv.UsedBy, &inv.CreatedAt); err != nil {
			continue
		}
		invites = append(invites, inv)
	}
	return invites, nil
}

// RevokeWaitlistInvite marks an active invite as expired (admin revocation).
func (s *PostgresStore) RevokeWaitlistInvite(ctx context.Context, inviteID string) error {
	result, err := s.pool.Exec(ctx,
		`UPDATE waitlist_invites SET status = $1
		 WHERE id = $2::uuid AND status = $3`,
		model.WaitlistInviteStatusExpired, inviteID, model.WaitlistInviteStatusActive)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("invite not found or already consumed/expired")
	}
	return nil
}

// GetActiveInviteForEntry returns the active invite for a waitlist entry, if any.
// Returns (nil, nil) when no active invite exists — callers can distinguish
// "no invite" from real DB errors by checking both return values.
func (s *PostgresStore) GetActiveInviteForEntry(ctx context.Context, waitlistID string) (*model.WaitlistInvite, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, waitlist_id, email, token, status, expires_at, used_at, used_by, created_at
		 FROM waitlist_invites
		 WHERE waitlist_id = $1::uuid AND status = $2 AND expires_at > now()
		 ORDER BY created_at DESC LIMIT 1`,
		waitlistID, model.WaitlistInviteStatusActive)
	var inv model.WaitlistInvite
	err := row.Scan(&inv.ID, &inv.WaitlistID, &inv.Email, &inv.Token,
		&inv.Status, &inv.ExpiresAt, &inv.UsedAt, &inv.UsedBy, &inv.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // no active invite — not an error
		}
		return nil, err // real DB error
	}
	return &inv, nil
}

// ListExpiringWaitlistInvites returns active invites expiring within the given hours.
// Used by the reminder job to send "your invite expires soon" emails.
func (s *PostgresStore) ListExpiringWaitlistInvites(ctx context.Context, withinHours int) ([]model.WaitlistInvite, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, waitlist_id, email, token, status, expires_at, used_at, used_by, created_at
		 FROM waitlist_invites
		 WHERE status = $1
		   AND expires_at > now()
		   AND expires_at <= now() + make_interval(hours => $2)
		 ORDER BY expires_at ASC`,
		model.WaitlistInviteStatusActive, withinHours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invites []model.WaitlistInvite
	for rows.Next() {
		var inv model.WaitlistInvite
		if err := rows.Scan(&inv.ID, &inv.WaitlistID, &inv.Email, &inv.Token,
			&inv.Status, &inv.ExpiresAt, &inv.UsedAt, &inv.UsedBy, &inv.CreatedAt); err != nil {
			continue
		}
		invites = append(invites, inv)
	}
	return invites, nil
}

// --- User can_create_hub ---

// SetCanCreateHub updates the can_create_hub flag for a user.
func (s *PostgresStore) SetCanCreateHub(ctx context.Context, userID string, canCreate bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET can_create_hub = $1, updated_at = now() WHERE id = $2::uuid`,
		canCreate, userID)
	return err
}

// GetCanCreateHub returns whether a user can create hubs.
func (s *PostgresStore) GetCanCreateHub(ctx context.Context, userID string) (bool, error) {
	var canCreate bool
	err := s.pool.QueryRow(ctx,
		`SELECT can_create_hub FROM users WHERE id = $1::uuid`, userID).Scan(&canCreate)
	if err != nil {
		return false, err
	}
	return canCreate, nil
}

// --- Scan helpers ---

func scanWaitlistEntry(row pgx.Row) (*model.WaitlistEntry, error) {
	var e model.WaitlistEntry
	err := row.Scan(&e.ID, &e.Email, &e.UseCase, &e.AITools, &e.Role, &e.Status,
		&e.Wave, &e.Notes, &e.ApprovedBy, &e.ApprovedAt, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("waitlist entry not found: %w", err)
	}
	return &e, nil
}

func scanWaitlistEntries(rows pgx.Rows) ([]model.WaitlistEntry, error) {
	var entries []model.WaitlistEntry
	for rows.Next() {
		var e model.WaitlistEntry
		if err := rows.Scan(&e.ID, &e.Email, &e.UseCase, &e.AITools, &e.Role, &e.Status,
			&e.Wave, &e.Notes, &e.ApprovedBy, &e.ApprovedAt, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan waitlist entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate waitlist entries: %w", err)
	}
	return entries, nil
}

// scanWaitlistEntriesWithInvite scans the extended column set from the admin list query
// (base 12 + 3 invite columns + 2 invite-consumer user columns).
func scanWaitlistEntriesWithInvite(rows pgx.Rows) ([]model.WaitlistEntry, error) {
	var entries []model.WaitlistEntry
	for rows.Next() {
		var e model.WaitlistEntry
		if err := rows.Scan(&e.ID, &e.Email, &e.UseCase, &e.AITools, &e.Role, &e.Status,
			&e.Wave, &e.Notes, &e.ApprovedBy, &e.ApprovedAt, &e.CreatedAt, &e.UpdatedAt,
			&e.InviteStatus, &e.InviteUsedAt, &e.InviteUsedBy,
			&e.InviteUsedByEmail, &e.InviteUsedByName); err != nil {
			return nil, fmt.Errorf("scan waitlist entry with invite: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate waitlist entries with invite: %w", err)
	}
	return entries, nil
}

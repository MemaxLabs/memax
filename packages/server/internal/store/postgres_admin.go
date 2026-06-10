package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// --- Admin Roles ---

// GetAdminRole returns the highest admin role for a user, or "" if not an admin.
// Returns an error only on real DB failures, not on "no rows" (which means not an admin).
func (s *PostgresStore) GetAdminRole(ctx context.Context, userID string) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT role FROM admin_roles WHERE user_id = $1::uuid ORDER BY granted_at LIMIT 1`,
		userID).Scan(&role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil // not an admin
		}
		return "", err // real DB error
	}
	return role, nil
}

// ListAdminRoles returns all admin role assignments.
func (s *PostgresStore) ListAdminRoles(ctx context.Context) ([]model.AdminRole, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, role, granted_by, granted_at FROM admin_roles ORDER BY granted_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []model.AdminRole
	for rows.Next() {
		var r model.AdminRole
		if err := rows.Scan(&r.UserID, &r.Role, &r.GrantedBy, &r.GrantedAt); err != nil {
			continue
		}
		roles = append(roles, r)
	}
	return roles, nil
}

// EnsureAdminRolesByEmail idempotently grants admin roles to users matching
// the given emails. Used for bootstrap seeding from ADMIN_EMAILS env var.
// Returns the number of roles actually inserted.
func (s *PostgresStore) EnsureAdminRolesByEmail(ctx context.Context, emails []string, role string) (int, error) {
	if len(emails) == 0 {
		return 0, nil
	}

	// Normalize emails
	normalized := make([]string, len(emails))
	for i, e := range emails {
		normalized[i] = strings.ToLower(strings.TrimSpace(e))
	}

	result, err := s.pool.Exec(ctx,
		`INSERT INTO admin_roles (user_id, role, granted_at)
		 SELECT id, $1, now() FROM users
		 WHERE LOWER(BTRIM(email)) = ANY($2)
		 ON CONFLICT DO NOTHING`,
		role, normalized)
	if err != nil {
		return 0, err
	}
	return int(result.RowsAffected()), nil
}

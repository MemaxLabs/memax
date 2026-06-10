package model

import "time"

// AdminRole represents a user's admin role assignment.
type AdminRole struct {
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"` // "super_admin", "operator" (future)
	GrantedBy *string   `json:"granted_by,omitempty"`
	GrantedAt time.Time `json:"granted_at"`
}

// Admin role constants.
const (
	AdminRoleSuperAdmin = "super_admin"
)

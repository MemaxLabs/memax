package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

const adminRoleKey contextKey = "admin_role"

// AdminMiddleware checks that the authenticated user has an admin role.
// Must be chained after authMiddleware (which sets userIDKey in context).
func AdminMiddleware(s store.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := GetUserID(r)
			if userID == "" {
				writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
				return
			}

			role, err := s.GetAdminRole(r.Context(), userID)
			if err != nil {
				slog.Error("admin role lookup failed", "error", err, "user_id", userID)
				writeError(w, http.StatusInternalServerError, "internal", "Failed to check admin access.")
				return
			}
			if role == "" {
				writeError(w, http.StatusForbidden, "forbidden", "Admin access required.")
				return
			}

			ctx := context.WithValue(r.Context(), adminRoleKey, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetAdminRole returns the admin role from request context, or "" if not an admin.
func GetAdminRole(r *http.Request) string {
	if v, ok := r.Context().Value(adminRoleKey).(string); ok {
		return v
	}
	return ""
}

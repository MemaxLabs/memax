package handler

import (
	"net/http"

	"github.com/MemaxLabs/memax/packages/server/internal/analytics"
)

// Analytics is the package-level PostHog client.
// Set via SetAnalytics() from main.go. Nil-safe — all Track calls are no-ops when nil.
var Analytics *analytics.Client

// SetAnalytics sets the package-level analytics client.
func SetAnalytics(c *analytics.Client) {
	Analytics = c
}

// track is a convenience wrapper for handler-level event tracking.
func track(userID string, event string, props map[string]any) {
	if Analytics == nil {
		return
	}
	Analytics.Track(userID, event, props)
}

// trackRequest is like track but reads user identity from the request context
// and automatically includes impersonator_id when the request is impersonated.
func trackRequest(r *http.Request, event string, props map[string]any) {
	if Analytics == nil {
		return
	}
	userID := GetUserID(r)
	if userID == "" {
		return
	}
	if impersonatorID := GetImpersonatorID(r); impersonatorID != "" {
		if props == nil {
			props = make(map[string]any)
		}
		props["impersonator_id"] = impersonatorID
	}
	Analytics.Track(userID, event, props)
}

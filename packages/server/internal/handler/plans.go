package handler

import (
	"net/http"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
)

// PlansHandler serves public plan information.
type PlansHandler struct {
	registry *plans.Registry
}

// NewPlansHandler creates a PlansHandler. Returns nil if registry is nil.
func NewPlansHandler(registry *plans.Registry) *PlansHandler {
	if registry == nil {
		return nil
	}
	return &PlansHandler{registry: registry}
}

// List returns all active plans sorted by tier order.
// GET /v1/plans (public, no auth required)
func (h *PlansHandler) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: h.registry.AllPlans()})
}

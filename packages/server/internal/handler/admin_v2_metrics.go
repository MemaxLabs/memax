package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// V2Metrics is what the admin metrics panel reads from the V2 record:
// the product metrics that judge the phase gates and the north star, both
// computed across spaces as counts only (*ledger.Ledger).
type V2Metrics interface {
	GetProductMetrics(ctx context.Context, from, to time.Time) (ledger.ProductMetrics, error)
	GetReadMetrics(ctx context.Context, day time.Time) (ledger.ReadMetrics, error)
}

// AdminV2MetricsHandler serves GET /v1/admin/v2/metrics: plan 25's gate
// metrics (§5.18, §12) by weekly signup cohort, for the admin ops panel.
// Admin-only (AdminMiddleware), and never in the public SDK.
type AdminV2MetricsHandler struct {
	metrics V2Metrics
	now     func() time.Time
}

// NewAdminV2MetricsHandler returns the handler, or nil when there is no
// V2 record (the route is then not registered).
func NewAdminV2MetricsHandler(m V2Metrics) *AdminV2MetricsHandler {
	if m == nil {
		return nil
	}
	return &AdminV2MetricsHandler{metrics: m, now: time.Now}
}

// DefaultMetricsWeeks is how many signup weeks the panel shows unless
// asked, the current one included.
const DefaultMetricsWeeks = 8

// AdminV2Metrics is the response: the cohorts, review health, the gates
// judged on them, and the north star for the week that ended yesterday.
type AdminV2Metrics struct {
	ledger.ProductMetrics
	Gates     []ledger.PhaseGate `json:"gates"`
	NorthStar AdminNorthStar     `json:"north_star"`
}

// AdminNorthStar is ledger.ReadMetrics for the panel.
type AdminNorthStar struct {
	WeekEnding           string  `json:"week_ending"`
	SpacesRead           int64   `json:"spaces_read"`
	SpacesTwoAgents      int64   `json:"spaces_two_agents"`
	SpacesTwoConnections int64   `json:"spaces_two_connections"`
	SpacesHookLoads      int64   `json:"spaces_hook_loads"`
	ConnectionsReading   int64   `json:"connections_reading"`
	ConnectionsSeen      int64   `json:"connections_seen"`
	Coverage             float64 `json:"coverage"`
}

// Get returns the metrics of people who signed up from `from` (a date,
// inclusive) up to `to` (a date, exclusive). Without them: the last
// DefaultMetricsWeeks weeks, this one included.
// GET /v1/admin/v2/metrics?from=2026-10-12&to=2026-12-07
func (h *AdminV2MetricsHandler) Get(w http.ResponseWriter, r *http.Request) {
	now := h.now().UTC()
	to := ledger.WeekStart(now).AddDate(0, 0, 7)
	from := to.AddDate(0, 0, -7*DefaultMetricsWeeks)
	for name, dst := range map[string]*time.Time{"from": &from, "to": &to} {
		v := strings.TrimSpace(r.URL.Query().Get(name))
		if v == "" {
			continue
		}
		d, err := time.Parse(time.DateOnly, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_range", "Give "+name+" as a date, like 2026-10-12.")
			return
		}
		*dst = d
	}
	pm, err := h.metrics.GetProductMetrics(r.Context(), from, to)
	var invalid *ledger.ValidationError
	switch {
	case errors.As(err, &invalid):
		writeError(w, http.StatusBadRequest, "invalid_range", "Choose another range: "+invalid.Message+".")
		return
	case errors.Is(err, ledger.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_range", "Choose a range that starts before it ends and spans at most 53 weeks.")
		return
	case errors.Is(err, ledger.ErrDisabled):
		writeError(w, http.StatusServiceUnavailable, "unavailable", "The V2 record isn't configured on this server.")
		return
	case err != nil:
		slog.ErrorContext(r.Context(), "admin: product metrics", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Couldn't compute the metrics. Try again, and check the API logs if it persists.")
		return
	}
	ns, err := h.metrics.GetReadMetrics(r.Context(), now.AddDate(0, 0, -1))
	if err != nil {
		slog.ErrorContext(r.Context(), "admin: north star", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "Couldn't compute the north star. Try again, and check the API logs if it persists.")
		return
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: AdminV2Metrics{
		ProductMetrics: pm,
		Gates:          pm.PhaseGates(),
		NorthStar: AdminNorthStar{
			WeekEnding: ns.Day.Format(time.DateOnly), SpacesRead: ns.SpacesRead, SpacesTwoAgents: ns.SpacesTwoAgents,
			SpacesTwoConnections: ns.SpacesTwoConnections, SpacesHookLoads: ns.SpacesHookLoads,
			ConnectionsReading: ns.ConnectionsReading, ConnectionsSeen: ns.ConnectionsSeen, Coverage: ns.Coverage(),
		},
	}})
}

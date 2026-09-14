package meter

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/meterctx"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Middleware returns an HTTP middleware that enforces operation quotas.
//
// For metered operations (push, recall, ask), it:
//  1. Resolves the user's plan limits
//  2. Reserves a slot on the gate counter (Redis INCR)
//  3. Injects a MeterToken + UserLimits into the request context
//  4. Defers a safety-net rollback (fires if handler doesn't commit)
//
// Non-metered operations pass through with zero overhead.
// No ResponseWriter wrapping — preserves http.Flusher for SSE.
func (m *Meter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			op := ClassifyOperation(r.Method, r.URL.Path)
			if op == "" {
				next.ServeHTTP(w, r)
				return
			}

			userID := handler.GetUserID(r)
			if userID == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Resolve limits using scoped entitlement resolution.
			// Each operation type routes to the correct resolver method
			// per the design doc's operation matrix.
			billingHubID := resolveBillingHub(r, op)
			limits, ok := m.resolveOpLimits(r, userID, op, billingHubID)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			// Determine the quota limit for this operation
			limit := operationLimit(op, &limits)

			// Reserve a slot
			token, allowed, current := m.Reserve(r.Context(), userID, op, limit)
			if !allowed {
				writeQuotaExceeded(w, op, int(current), limit, limits.PlanID, limits.PlanDisplayName)
				return
			}

			// Inject token + limits into context
			ctx := meterctx.WithToken(r.Context(), token)
			ctx = meterctx.WithUserLimits(ctx, &limits)
			ctx = meterctx.WithUsageEventInfo(ctx, meterctx.NewUsageEventInfo())

			// Safety net: if handler doesn't commit, rollback the reservation
			defer token.Rollback()

			// After handler returns, if committed, log the usage event.
			// Use the billing hub (not read hub) so usage_events.hub_id
			// matches the plan context that allowed the operation (Codex finding #4).
			defer func() {
				if token.IsCommitted() {
					agentName := handler.GetAgentName(r)
					source := detectSource(r)
					m.LogEvent(userID, op, billingHubID, source, agentName, meterctx.UsageEventInfoFromContext(ctx).Snapshot())
				}
			}()

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// resolveByOperation routes to the correct scoped resolver method based on
// the operation type and target hub, per the design doc's operation matrix:
//   - recall/ask → ResolveReadEntitlements (max of personal + best hub)
//   - push to personal hub → ResolvePersonalWriteEntitlements
//   - push to team hub → ResolveHubWriteEntitlements (target hub plan only)
func resolveByOperation(r *http.Request, resolver planResolver, userID, op, billingHubID string) model.UserLimits {
	switch op {
	case "recall", "ask":
		return resolver.ResolveReadEntitlements(r.Context(), userID).Limits
	case "push":
		if billingHubID != "" {
			// Check if this is a team hub (write hub was set)
			return resolver.ResolveHubWriteEntitlements(r.Context(), userID, billingHubID).Limits
		}
		return resolver.ResolvePersonalWriteEntitlements(r.Context(), userID).Limits
	default:
		return resolver.ResolveForRequest(r.Context(), userID, billingHubID)
	}
}

// resolveBillingHub determines which hub's plan to use for billing limits.
// Writes (push) use the write hub; reads (recall, ask) use the read hub.
func resolveBillingHub(r *http.Request, op string) string {
	switch op {
	case "push":
		// Writes target the write hub — a push to a Team hub should
		// resolve Team limits, not the read/boost hub.
		return handler.GetWriteHubID(r)
	case "recall", "ask":
		// Reads use the active read hub (X-Hub-ID or boost hub).
		return handler.GetHubID(r)
	default:
		return handler.GetHubID(r)
	}
}

// operationLimit returns the quota limit for a given operation from the user's limits.
func operationLimit(op string, limits *model.UserLimits) int {
	switch op {
	case "push":
		return limits.PushLimit
	case "recall":
		return limits.RecallLimit
	case "ask":
		return limits.AskLimit
	default:
		return -1 // unlimited for unknown ops
	}
}

// detectSource determines the request source from headers and auth context.
func detectSource(r *http.Request) string {
	// `/mcp` is a reserved prefix for the MCP transport in this server. Keep
	// this prefix match in sync if new non-MCP endpoints are ever added under it.
	if strings.HasPrefix(r.URL.Path, "/mcp") {
		return "mcp"
	}
	ua := r.Header.Get("User-Agent")
	switch {
	case strings.Contains(ua, "memax-cli"):
		return "cli"
	case strings.Contains(ua, "memax-sdk"):
		return "api"
	case handler.GetAgentName(r) != "":
		return "api"
	default:
		return "web"
	}
}

// writeQuotaExceeded writes a 402 Payment Required response with machine-readable
// details for client-side quota UI, upgrade CTAs, and usage bars.
func writeQuotaExceeded(w http.ResponseWriter, op string, current, limit int, planID, planName string) {
	upgrade := upgradeHint(planID)
	now := time.Now().UTC()
	resetAt := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)

	msg := fmt.Sprintf("Monthly %s limit reached (%d/%d).", op, current, limit)
	if upgrade != "" {
		msg += " Upgrade to " + upgrade + " for higher limits."
	}

	handler.WriteErrorWithDetails(w, http.StatusPaymentRequired, "quota_exceeded", msg, map[string]any{
		"operation":    op,
		"current":      current,
		"limit":        limit,
		"plan":         planID,
		"plan_name":    planName,
		"reset_at":     resetAt.Format(time.RFC3339),
		"upgrade_plan": upgrade,
	})
}

func upgradeHint(currentPlan string) string {
	switch currentPlan {
	case model.PersonalFreePlanID, model.LegacyFreePlanID:
		return model.PersonalProPlanID
	case model.PersonalProPlanID, model.LegacyProPlanID:
		return model.PersonalProPlusPlanID
	case model.PersonalProPlusPlanID, model.LegacyProPlusPlanID:
		return model.HubTeamPlanID
	case model.HubFreeTeamPlanID:
		return model.HubTeamPlanID
	default:
		return ""
	}
}

// resolveOpLimits is the one limit-resolution path shared by the HTTP
// middleware and BeginOp. ok=false means the user row could not be
// read in registry-only mode — historic middleware behavior is to
// fail open (pass the request through unmetered).
func (m *Meter) resolveOpLimits(r *http.Request, userID, op, billingHubID string) (model.UserLimits, bool) {
	if m.resolver != nil {
		return resolveByOperation(r, m.resolver, userID, op, billingHubID), true
	}
	user, err := m.store.GetUser(userID)
	if err != nil || user == nil {
		return model.UserLimits{}, false
	}
	// Prefer scoped personal_plan_id for limit resolution
	planID := user.PersonalPlanID
	if planID == "" {
		planID = user.Plan
	}
	return m.registry.GetUserLimits(r.Context(), userID, planID), true
}

// BeginOp enforces ONE metered operation for a transport the HTTP
// middleware cannot classify. Every /mcp tool call arrives as POST
// /mcp, so ClassifyOperation returns "" and the middleware waves the
// request through — which meant MCP pushes and recalls hit NO quota
// at all while the same user's web pushes were 402'd at the plan cap
// (founder repro, 2026-09-14: "CLI/MCP 能 push,网页不行").
//
// Same resolution, same gate counter, same fail-open posture as the
// middleware. The caller receives finish(committed): commit charges
// the op, rollback un-reserves it (the tool errored — don't bill).
// finish is never nil and is idempotent-safe via the token's own
// guards. Usage-event LOGGING stays with the caller — MCP tools
// already write their own events via SetLogEvent, and logging here
// too would double-count rows.
func (m *Meter) BeginOp(
	r *http.Request,
	userID, op, billingHubID string,
) (func(committed bool), *meterctx.OpDenial) {
	noop := func(bool) {}
	if userID == "" || op == "" {
		return noop, nil
	}
	limits, ok := m.resolveOpLimits(r, userID, op, billingHubID)
	if !ok {
		return noop, nil // fail open, mirroring the middleware
	}
	limit := operationLimit(op, &limits)
	token, allowed, current := m.Reserve(r.Context(), userID, op, limit)
	if !allowed {
		return noop, &meterctx.OpDenial{
			Op:       op,
			Current:  int(current),
			Limit:    limit,
			PlanID:   limits.PlanID,
			PlanName: limits.PlanDisplayName,
		}
	}
	return func(committed bool) {
		if committed {
			token.Commit()
		} else {
			token.Rollback()
		}
	}, nil
}

// OpDenialMessage phrases a denial for a text-protocol client (MCP
// IsError content) with the same facts writeQuotaExceeded puts in
// the 402 payload, so an agent can relay an actionable message.
func OpDenialMessage(d *meterctx.OpDenial) string {
	msg := fmt.Sprintf("Monthly %s limit reached (%d/%d) on plan %s.", d.Op, d.Current, d.Limit, d.PlanName)
	if hint := upgradeHint(d.PlanID); hint != "" {
		msg += " Upgrade to " + hint + " for higher limits."
	}
	msg += " Quota resets on the 1st (UTC)."
	return msg
}

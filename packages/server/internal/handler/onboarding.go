package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// OnboardingHandler serves the plan-18 /v1/onboarding surface. Only
// one endpoint today (restart); the rest of the onboarding lifecycle
// lives on /v1/notifications via the super-notif primitive.
type OnboardingHandler struct {
	store   store.Store
	emitter onboardingChecklistEmitter
	events  events.Publisher
	rateMax int // max restart calls per 24h window
}

// onboardingChecklistEmitter is the narrow interface this handler
// needs from the onboarding package. Defined locally so handler
// doesn't import internal/onboarding (preserves package layering).
type onboardingChecklistEmitter interface {
	EmitChecklist(ctx context.Context, userID string, version int) (*model.Notification, error)
}

// onboardingVersionParser parses {user_id}:v{n} → n; 0 when absent.
// Wired at app boot to onboarding.ParseChecklistVersion so the
// handler stays decoupled from onboarding internals.
var onboardingVersionParser func(sourceID string) int

// SetOnboardingVersionParser is the package-level injection point for
// the version parser. Called once from serverapp/app.go.
func SetOnboardingVersionParser(fn func(sourceID string) int) {
	onboardingVersionParser = fn
}

// NewOnboardingHandler constructs the handler. emitter may be nil
// (dev/test) — the Restart endpoint returns 503 in that case.
//
// rateMax defaults to 10 restarts / 24h. Override per-env via the
// ONBOARDING_RESTART_RATE_MAX env var (parsed in serverapp/app.go);
// staging runs higher so the founder can iterate without tripping it.
func NewOnboardingHandler(s store.Store, e onboardingChecklistEmitter, pub events.Publisher) *OnboardingHandler {
	return &OnboardingHandler{store: s, emitter: e, events: pub, rateMax: 10}
}

// SetRateMax overrides the per-user restart cap. <=0 disables the
// limit entirely. Called from serverapp/app.go when the
// ONBOARDING_RESTART_RATE_MAX env var is set.
func (h *OnboardingHandler) SetRateMax(n int) {
	h.rateMax = n
}

// Restart — POST /v1/onboarding/restart (plan 18 §3.3 + §6.2)
//
// Resolves the user's current pending checklist (if any) with
// resolution=dismissed, then emits a fresh checklist with the next
// version. Rate-limited to 3 calls per 24h per user to defeat spam.
//
// Body: none. Returns the new notification row in the canonical
// ApiResponse envelope.
//
// Initial vs restart: a user with no prior onboarding row gets a v1
// checklist (the same shape as signup-time emit). The frontend
// distinguishes the two only for the button label ("Start" vs
// "Restart"); server-side they are the same path.
func (h *OnboardingHandler) Restart(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	if h.emitter == nil {
		writeError(w, http.StatusServiceUnavailable, "onboarding_disabled",
			"Onboarding emitter is not configured on this instance")
		return
	}
	ctx := r.Context()

	// Find the latest checklist row (any status) so we can bump the
	// version + (if it's still pending) resolve it as dismissed.
	prior, err := h.store.LatestChecklistByRecipient(ctx, userID, model.NotificationSourceOnboarding)
	if err != nil && !errors.Is(err, store.ErrNotificationNotFound) {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	priorVersion := 0
	if prior != nil && onboardingVersionParser != nil {
		priorVersion = onboardingVersionParser(prior.SourceID)
	}
	nextVersion := priorVersion + 1

	// Rate limit: how many onboarding checklist rows has this user
	// created in the last 24h? Counts ALL statuses (pending +
	// resolved + dismissed + expired) so a spam-restart loop trips
	// even if each prior row is immediately dismissed.
	if h.rateLimitExceeded(ctx, userID) {
		writeError(w, http.StatusTooManyRequests, "rate_limited",
			fmt.Sprintf("Onboarding restart is rate-limited to %d times per 24 hours.", h.rateMax))
		return
	}

	// Dismiss the current pending row (if any). Status check guards
	// against double-resolving a row that's already terminal.
	if prior != nil && prior.Status == model.NotificationStatusPending {
		if rerr := h.store.ResolveNotification(
			ctx, prior.ID, userID, nil, model.ResolutionDismissed,
		); rerr != nil {
			slog.Warn("onboarding restart: dismiss prior failed",
				"user_id", userID, "notification_id", prior.ID, "err", rerr)
			// Continue — the new emit is the user-visible value.
		} else if updated, gerr := h.store.GetNotification(ctx, prior.ID, userID, nil); gerr == nil {
			events.PublishNotificationResolved(ctx, h.events, updated)
		}
	}

	fresh, err := h.emitter.EmitChecklist(ctx, userID, nextVersion)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "emit_failed", err.Error())
		return
	}
	if fresh != nil {
		events.PublishNotificationCreated(ctx, h.events, fresh)
	}

	track(userID, "api.onboarding.restart", map[string]any{
		"prior_version": priorVersion,
		"next_version":  nextVersion,
		"had_prior":     prior != nil,
	})

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: fresh})
}

// rateLimitExceeded counts how many onboarding checklist rows this
// user has created in the trailing 24h window. Plan 18 §6.2 calls
// for 3/day. Counts ALL statuses (pending + resolved + dismissed +
// expired) so a spam-restart loop trips even when each prior row is
// instantly dismissed by the next restart.
func (h *OnboardingHandler) rateLimitExceeded(ctx context.Context, userID string) bool {
	if h.rateMax <= 0 {
		return false
	}
	since := time.Now().UTC().Add(-24 * time.Hour)
	count, err := h.store.CountChecklistsCreatedSince(
		ctx, userID, model.NotificationSourceOnboarding, since,
	)
	if err != nil {
		// Failure path is fail-open — better to let the user retry
		// than to block them entirely on a transient count error.
		// The notifications_source_unique constraint catches the
		// pathological same-version retry case anyway.
		slog.Warn("onboarding restart: rate-limit count failed",
			"user_id", userID, "err", err)
		return false
	}
	return count >= h.rateMax
}

// OnboardingState — GET /v1/onboarding/state
//
// Returns the small surface the Settings → Getting started row needs
// to decide button label + helper text without fetching the full
// notifications list. `has_prior` distinguishes "never had a
// checklist" (Start) from "had one and dismissed/resolved/expired"
// (Restart). `current_version` is the highest version observed (0
// when no prior row).
func (h *OnboardingHandler) State(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	ctx := r.Context()
	row, err := h.store.LatestChecklistByRecipient(ctx, userID, model.NotificationSourceOnboarding)
	if err != nil && !errors.Is(err, store.ErrNotificationNotFound) {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	resp := struct {
		HasPrior       bool                     `json:"has_prior"`
		CurrentVersion int                      `json:"current_version"`
		CurrentStatus  model.NotificationStatus `json:"current_status,omitempty"`
		NextRestartAt  *time.Time               `json:"next_restart_at,omitempty"`
		RateLimitedFor string                   `json:"rate_limited_for,omitempty"`
	}{}
	if row != nil {
		resp.HasPrior = true
		resp.CurrentStatus = row.Status
		if onboardingVersionParser != nil {
			resp.CurrentVersion = onboardingVersionParser(row.SourceID)
		}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: resp})
}

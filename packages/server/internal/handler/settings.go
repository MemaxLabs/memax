package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// uuidShapeRe — RFC 4122 shape for hub ids in settings payloads.
var uuidShapeRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// planLimitsResolver resolves a user's effective plan limits. Avoids
// importing plans package directly (breaks import cycle with meter).
type planLimitsResolver interface {
	GetUserLimits(ctx context.Context, userID, planID string) model.UserLimits
}

// effectivePlanResolver resolves limits with per-hub elevation.
type effectivePlanResolver interface {
	ResolveForRequest(ctx context.Context, userID, billingHubID string) model.UserLimits
}

// usageReader reads live usage counters from the meter (Redis committed counters).
type usageReader interface {
	GetCommittedUsage(ctx context.Context, userID string) (push, recall, ask int)
}

// SettingsHandler handles user preferences.
type SettingsHandler struct {
	store             store.Store
	planResolver      planLimitsResolver    // legacy: user-plan-only
	effectiveResolver effectivePlanResolver // new: per-hub elevation (takes precedence)
	usageReader       usageReader           // nil = read from Postgres only
}

func NewSettingsHandler(s store.Store) *SettingsHandler {
	return &SettingsHandler{store: s}
}

// SetPlanResolver wires the plan registry for UsageWithLimits responses.
func (h *SettingsHandler) SetPlanResolver(r planLimitsResolver) {
	h.planResolver = r
}

// SetEffectiveResolver wires the hub-aware plan resolver for /v1/usage.
func (h *SettingsHandler) SetEffectiveResolver(r effectivePlanResolver) {
	h.effectiveResolver = r
}

// SetUsageReader wires the meter for live usage counters in the /v1/usage response.
func (h *SettingsHandler) SetUsageReader(u usageReader) {
	h.usageReader = u
}

// Get returns the user's settings merged with defaults.
// GET /v1/settings
func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)

	prefs, err := h.store.GetUserPreferences(ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	settings := prefs.MergedSettings()
	delete(settings, "active_hub_id")

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: settings,
	})
}

// GetUsage returns the current billing period usage for the authenticated user.
// When a plan resolver is configured, returns UsageWithLimits (usage + plan limits).
// GET /v1/usage
func (h *SettingsHandler) GetUsage(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	usage, err := h.store.GetCurrentUsage(ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if usage == nil {
		usage = &model.Usage{}
	}

	// Prefer live Redis committed counters over (possibly stale) Postgres.
	// Redis is the real-time authority; Postgres is the durable sync target.
	if h.usageReader != nil {
		push, recall, ask := h.usageReader.GetCommittedUsage(r.Context(), ownerID)
		if push > 0 || recall > 0 || ask > 0 {
			usage.PushCount = max(usage.PushCount, push)
			usage.RecallCount = max(usage.RecallCount, recall)
			usage.AskCount = max(usage.AskCount, ask)
		}
	}

	// Resolve limits: prefer effective resolver (per-hub elevation), fall back to
	// legacy plan resolver (user-plan-only), or skip limits entirely.
	if h.effectiveResolver != nil {
		// Use effective personal plan (no hub context for /v1/usage — it's a personal endpoint)
		limits := h.effectiveResolver.ResolveForRequest(r.Context(), ownerID, "")
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.UsageWithLimits{
			Usage:           *usage,
			Limits:          limits,
			Plan:            limits.PlanID,
			PlanDisplayName: limits.PlanDisplayName,
		}})
		return
	}
	if h.planResolver != nil {
		user, userErr := h.store.GetUser(ownerID)
		if userErr == nil && user != nil {
			// Prefer scoped personal_plan_id for limit resolution
			planID := user.PersonalPlanID
			if planID == "" {
				planID = user.Plan
			}
			limits := h.planResolver.GetUserLimits(r.Context(), ownerID, planID)
			writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.UsageWithLimits{
				Usage:           *usage,
				Limits:          limits,
				Plan:            user.PersonalPlanID,
				PlanDisplayName: limits.PlanDisplayName,
			}})
			return
		}
	}
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: usage})
}

// Update patches the user's settings. Only provided keys are updated;
// omitted keys keep their current (or default) values.
// PATCH /v1/settings
//
// Body: { "dreams_enabled": false, "theme": "dark" }
func (h *SettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
		return
	}

	var patch map[string]any
	if err := json.Unmarshal(body, &patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}

	// Validate known keys. Must stay in lockstep with
	// model.DefaultSettings() — any default that isn't listed here
	// is silently unwritable, which means the UI toggle renders and
	// swallows clicks that return 400 from the store.
	//
	// The five dream-phase keys (dreams_enabled, dreams_merge_enabled,
	// dreams_archive_enabled, dreams_organize_enabled,
	// dreams_restructure_enabled) are NOT here despite living in
	// DefaultSettings — they moved to per-hub settings in migration
	// 018 / the per-hub intelligence release. Callers that try to
	// set them here get a targeted error pointing to the right
	// endpoint, rather than silently succeeding (the old behavior)
	// or the generic "unknown_setting" (which would be misleading
	// for a key that still appears in the GET response).
	//
	// The three tuning keys (dreams_excluded_kinds,
	// dreams_similarity_threshold, dreams_staleness_days) stay
	// account-scoped — no UI today, and collapsing them into hub
	// settings would expand that API's surface for no benefit.
	knownKeys := map[string]bool{
		"dreams_excluded_kinds":       true,
		"dreams_similarity_threshold": true,
		"dreams_staleness_days":       true,
		"hub_header_aurora_mode":      true,
		"dev_flags":                   true,
		"notifications_enabled":       true,
		"theme":                       true,
		// UI locale — validated below ("", "en", "zh").
		"locale": true,
		// Default persona for the memax agent (Agent Chat). Validated
		// below: "" (none) or a persona owned by the caller.
		"chat_default_persona_id": true,
		// Hubs the user muted from the personal board's aggregated
		// pulse view (array of hub id strings). Validated below.
		"pulse_hidden_hub_ids": true,
	}
	// Phase keys moved to hub settings in 018 — same allowlist the
	// engine now reads from in MergedHubDreamSettings.
	phaseKeysMovedToHub := map[string]bool{
		"dreams_enabled":             true,
		"dreams_merge_enabled":       true,
		"dreams_archive_enabled":     true,
		"dreams_organize_enabled":    true,
		"dreams_restructure_enabled": true,
	}
	for k := range patch {
		if phaseKeysMovedToHub[k] {
			writeError(w, http.StatusBadRequest, "moved_to_hub_settings",
				"Dream phase toggles are now per-hub. Send this patch to PATCH /v1/hubs/{hub_id} with a `settings` body instead.")
			return
		}
		if !knownKeys[k] {
			writeError(w, http.StatusBadRequest, "unknown_setting", "Unknown setting: "+k)
			return
		}
	}

	// Get existing, merge patch
	prefs, err := h.store.GetUserPreferences(ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	for k, v := range patch {
		if k == "locale" {
			loc, ok := v.(string)
			// "" clears the preference back to client-side detection.
			if !ok || (loc != "" && loc != "en" && loc != "zh") {
				writeError(w, http.StatusBadRequest, "invalid_setting", `locale must be "", "en" or "zh"`)
				return
			}
			prefs.Settings[k] = loc
			continue
		}
		if k == "chat_default_persona_id" {
			pid, ok := v.(string)
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid_setting", "chat_default_persona_id must be a string")
				return
			}
			if pid != "" {
				if _, err := h.store.GetPersona(pid, ownerID); err != nil {
					writeError(w, http.StatusBadRequest, "invalid_setting", "chat_default_persona_id must be one of your personas")
					return
				}
			}
			prefs.Settings[k] = pid
			continue
		}
		if k == "pulse_hidden_hub_ids" {
			raw, ok := v.([]any)
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid_setting", "pulse_hidden_hub_ids must be an array of hub id strings")
				return
			}
			// Bounded + deduped + UUID-shaped: a hostile client must
			// not be able to bloat the settings row or persist junk
			// (codex PR review 2026-08-11). 200 hubs is far above any
			// real membership count.
			if len(raw) > 200 {
				writeError(w, http.StatusBadRequest, "invalid_setting", "pulse_hidden_hub_ids: too many entries (max 200)")
				return
			}
			seen := make(map[string]bool, len(raw))
			ids := make([]string, 0, len(raw))
			for _, entry := range raw {
				id, ok := entry.(string)
				if !ok || !uuidShapeRe.MatchString(id) {
					writeError(w, http.StatusBadRequest, "invalid_setting", "pulse_hidden_hub_ids must be an array of hub id strings")
					return
				}
				if seen[id] {
					continue
				}
				seen[id] = true
				ids = append(ids, id)
			}
			prefs.Settings[k] = ids
			continue
		}
		if k == "dev_flags" {
			nextFlags, ok := v.(map[string]any)
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid_setting", "dev_flags must be an object")
				return
			}
			mergedFlags := map[string]any{}
			if currentFlags, ok := prefs.Settings[k].(map[string]any); ok {
				for flagKey, flagValue := range currentFlags {
					mergedFlags[flagKey] = flagValue
				}
			}
			for flagKey, flagValue := range nextFlags {
				mergedFlags[flagKey] = flagValue
			}
			prefs.Settings[k] = mergedFlags
			continue
		}
		prefs.Settings[k] = v
	}

	if err := h.store.UpsertUserPreferences(ownerID, prefs.Settings); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	trackRequest(r, "api.settings.update", patch)

	settings := prefs.MergedSettings()
	delete(settings, "active_hub_id")

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: settings,
	})
}

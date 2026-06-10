package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/MemaxLabs/memax/packages/server/internal/meterctx"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	memaxWarningClaimRejected   = "agent_identity_claim_rejected"
	memaxWarningReconnectNeeded = "reconnect_required"
)

var (
	memoryProvenanceTelemetryOnce sync.Once
	memoryClaimRejectedCounter    metric.Int64Counter
)

type attributionValidationError struct {
	code    string
	message string
}

func (e *attributionValidationError) Error() string {
	return e.message
}

func newAttributionValidationError(code, message string) error {
	return &attributionValidationError{code: code, message: message}
}

func asAttributionValidationError(err error) (*attributionValidationError, bool) {
	var target *attributionValidationError
	if !errors.As(err, &target) {
		return nil, false
	}
	return target, true
}

func initMemoryProvenanceTelemetry() {
	memoryProvenanceTelemetryOnce.Do(func() {
		meter := otel.Meter("memax.memories")
		memoryClaimRejectedCounter, _ = meter.Int64Counter(
			"memax.write.claim_rejected",
			metric.WithDescription("Memory writes where an agent identity claim was rejected"),
		)
	})
}

func resolveRequestedSourceAgent(req model.PushRequest) string {
	return model.NormalizeAgentSlug(req.SourceAgent)
}

func resolveAuthSourceAgent(r *http.Request) string {
	return model.NormalizeAgentSlug(GetAgentName(r))
}

func requestNeedsReconnectWarning(r *http.Request) bool {
	grant := grantFromRequest(r)
	if grant.PrincipalType != "oauth_grant" {
		return false
	}
	agentSlug := model.NormalizeAgentSlug(grant.AgentName)
	return agentSlug == "" || agentSlug == "unknown"
}

func setMemaxWarningHeader(w http.ResponseWriter, warning string) {
	if warning == "" || w.Header().Get("X-Memax-Warning") != "" {
		return
	}
	w.Header().Set("X-Memax-Warning", warning)
}

func resolveMemoryProvenance(s store.Store, ownerID string, req model.PushRequest, r *http.Request) (*model.MemoryProvenance, string, bool, error) {
	authAgent := resolveAuthSourceAgent(r)
	claimedAgent := resolveRequestedSourceAgent(req)
	if authAgent != "" && claimedAgent != "" && authAgent != claimedAgent {
		return nil, "", false, newAttributionValidationError(
			"attribution_conflict",
			fmt.Sprintf("agent attribution conflict: auth=%s body=%s", authAgent, claimedAgent),
		)
	}

	createdVia := strings.TrimSpace(req.Source)
	if createdVia == "" {
		createdVia = "api"
	}

	createdBySlug := authAgent
	attributionSource := model.MemoryAttributionSourceHuman
	claimRejected := false
	if createdBySlug != "" {
		attributionSource = model.MemoryAttributionSourceAuth
	} else if claimedAgent != "" {
		if canClaimAgentFromRequest(r, createdVia) {
			createdBySlug = claimedAgent
			attributionSource = model.MemoryAttributionSourceClaim
		} else {
			markAgentClaimRejected(r, true, strings.TrimSpace(req.InitiationType) != "")
			req.InitiationType = ""
			claimRejected = true
		}
	}

	createdByType := model.MemoryCreatedByHuman
	if createdBySlug != "" {
		createdByType = model.MemoryCreatedByAgent
	}

	assistedByAgent, err := resolveAssistedByAgent(s, ownerID, req, createdByType)
	if err != nil {
		return nil, "", false, err
	}

	initiationType := resolveInitiationType(req.InitiationType, createdVia, createdByType)
	displayName := ""
	if createdBySlug != "" {
		displayName = displayNameForAgentSlug(s, ownerID, createdBySlug)
	}

	return &model.MemoryProvenance{
		CreatedByType:        createdByType,
		CreatedBySlug:        createdBySlug,
		CreatedByDisplayName: displayName,
		CreatedVia:           createdVia,
		AssistedByAgent:      assistedByAgent,
		InitiationType:       initiationType,
		AttributionSource:    attributionSource,
	}, createdBySlug, claimRejected, nil
}

func resolveAssistedByAgent(s store.Store, ownerID string, req model.PushRequest, createdByType string) (string, error) {
	assistedByAgent := model.NormalizeAgentSlug(req.AssistedByAgent)
	if assistedByAgent == "" {
		return "", nil
	}
	if createdByType != model.MemoryCreatedByHuman {
		return "", newAttributionValidationError(
			"collaborator_requires_human_actor",
			"assisted_by_agent is only valid for human-authored memories",
		)
	}
	if claimedAgent := resolveRequestedSourceAgent(req); claimedAgent != "" {
		return "", newAttributionValidationError(
			"conflicting_attribution_fields",
			"source_agent and assisted_by_agent cannot both be set",
		)
	}
	if isKnownAgentSlug(assistedByAgent) {
		return assistedByAgent, nil
	}
	if s != nil {
		if agent, err := s.GetConnectedAgent(ownerID, assistedByAgent); err == nil && agent != nil {
			return assistedByAgent, nil
		}
	}
	return "", newAttributionValidationError(
		"unknown_collaborator_agent",
		"assisted_by_agent must match a connected or known agent slug",
	)
}

func canClaimAgentFromRequest(r *http.Request, createdVia string) bool {
	if createdVia == "web" {
		return false
	}
	grant := grantFromRequest(r)
	switch grant.PrincipalType {
	case "api_key", "oauth_grant", "local":
		return true
	default:
		return false
	}
}

func markAgentClaimRejected(r *http.Request, hadAgentClaim, hadInitiationClaim bool) {
	if r == nil {
		return
	}
	initMemoryProvenanceTelemetry()
	memoryClaimRejectedCounter.Add(r.Context(), 1, metric.WithAttributes(
		attribute.String("path", r.URL.Path),
		attribute.Bool("had_agent_claim", hadAgentClaim),
		attribute.Bool("had_initiation_claim", hadInitiationClaim),
	))
}

func resolveInitiationType(requested, createdVia, createdByType string) string {
	requested = model.NormalizeMemoryInitiationType(strings.TrimSpace(requested))
	if requested != model.MemoryInitiationUnknown {
		// initiation_type is caller-asserted nuance, not an auth-derived fact.
		// We trust authenticated clients to report it accurately enough for UX,
		// while actor identity itself remains server-resolved from auth/claim rules.
		return requested
	}
	switch createdVia {
	case "import":
		return model.MemoryInitiationImport
	case "hook", "extraction":
		return model.MemoryInitiationAgentAutomatic
	}
	if createdByType == model.MemoryCreatedByAgent {
		return model.MemoryInitiationUnknown
	}
	return model.MemoryInitiationHumanDirect
}

func displayNameForAgentSlug(s store.Store, ownerID, slug string) string {
	if s != nil {
		if agent, err := s.GetConnectedAgent(ownerID, slug); err == nil && agent != nil {
			if strings.TrimSpace(agent.DisplayName) != "" {
				return agent.DisplayName
			}
		}
	}
	parts := strings.Split(slug, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func setUsageEventSummary(r *http.Request, summary string) {
	summary = compactActivitySummary(summary, 96)
	if summary == "" {
		return
	}
	meterctx.SetUsageEventField(r.Context(), "summary", summary)
}

func compactActivitySummary(value string, max int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

// tryAutoHealAPIKeyAgent backfills api_keys.agent_name when an API-key
// principal pushes with a source_agent claim and the key is still
// unassigned. Called AFTER resolveMemoryProvenance has accepted the
// claim. Returns true if the caller should reject the request with
// attribution_conflict — i.e. someone else already pinned the key to a
// different slug.
//
// When the heal succeeds (or the stored slug agrees with the claim),
// also upserts connected_agents so the agent card appears immediately.
func tryAutoHealAPIKeyAgent(ctx context.Context, s store.Store, r *http.Request, claimedSlug string, attributionSource string) (bool, error) {
	if claimedSlug == "" || attributionSource != model.MemoryAttributionSourceClaim {
		return false, nil
	}
	authCtx := GetAuthContext(r)
	if authCtx == nil {
		return false, nil
	}
	if authCtx.PrincipalType != "api_key" {
		return false, nil
	}
	// AgentName is set from the grant row. If it's already populated,
	// resolveMemoryProvenance would have used the auth path, not the
	// claim path. Defensive guard only.
	if authCtx.AgentName != "" {
		return false, nil
	}
	if authCtx.GrantID == "" || authCtx.UserID == "" {
		return false, nil
	}

	effective, err := s.HealAPIKeyAgent(ctx, authCtx.GrantID, authCtx.UserID, claimedSlug)
	if err != nil {
		// Log but don't block the push — the claim was already
		// accepted; auto-heal is self-healing polish.
		slog.Warn("auto-heal api_key.agent_name failed",
			"key_id", authCtx.GrantID, "err", err)
		return false, nil
	}
	// effective == "" means the row is standalone / revoked / missing.
	// Let the memory proceed — the claim path already recorded the
	// agent on the memory itself, and subsequent requests will either
	// hit the same standalone guard or a fresh permission check.
	if effective == "" {
		return false, nil
	}
	// effective != claimedSlug means someone else won the race with a
	// different agent — surface attribution_conflict so the caller
	// returns the standard error shape.
	if effective != claimedSlug {
		return true, nil
	}

	// We pinned the key (or confirmed a matching slug). Make sure the
	// connected_agents registry reflects the new identity.
	EnsureConnectedAgent(s, authCtx.UserID, effective)
	return false, nil
}

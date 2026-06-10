package dreams

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// callLLM invokes the Haiku (default) path and returns the full
// CompleteResponse so callers can read resp.Text AND accumulate
// token usage into phase metrics. Returning the struct rather than
// (text, tokens_in, tokens_out, err) keeps signatures small and
// avoids a copy on successful paths.
func (e *Engine) callLLM(ctx context.Context, prompt string, maxTokens int, purpose string) (*anthropic.CompleteResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, dreamLLMTimeout)
	defer cancel()
	return e.client.Complete(ctx, anthropic.CompleteRequest{
		Model:     e.model,
		MaxTokens: maxTokens,
		Prompt:    prompt,
		Purpose:   purpose,
	})
}

// callLLMWithModelTimeout is the explicit-model-and-timeout variant
// used by organize / restructure. Returns the full CompleteResponse
// for the same reason as callLLM.
func (e *Engine) callLLMWithModelTimeout(ctx context.Context, model, system, prompt string, maxTokens int, timeout time.Duration, purpose string) (*anthropic.CompleteResponse, error) {
	if timeout <= 0 {
		timeout = dreamLLMTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return e.client.Complete(ctx, anthropic.CompleteRequest{
		Model:     model,
		System:    system,
		MaxTokens: maxTokens,
		Prompt:    prompt,
		Purpose:   purpose,
	})
}

// addLLMUsage accumulates a CompleteResponse's token counts onto a
// phase metrics struct. Centralizes the nil-resp guard so every
// call site stays one-line.
func addLLMUsage(metrics *model.DreamPhaseMetrics, resp *anthropic.CompleteResponse) {
	if metrics == nil || resp == nil {
		return
	}
	metrics.TokensIn += int64(resp.InputTokens)
	metrics.TokensOut += int64(resp.OutputTokens)
}

func (e *Engine) trackingContextForMemories(ctx context.Context, memories []model.Memory, actorID string, flow string) context.Context {
	if len(memories) == 0 {
		return ctx
	}
	return e.trackingContextForHub(ctx, memories[0].HubID, memories[0].OwnerID, actorID, flow)
}

func (e *Engine) trackingContextForMemory(ctx context.Context, memory *model.Memory, actorID string, flow string) context.Context {
	if memory == nil {
		return ctx
	}
	return e.trackingContextForHub(ctx, memory.HubID, memory.OwnerID, actorID, flow)
}

func (e *Engine) trackingContextForHub(ctx context.Context, hubID string, fallbackOwnerID string, actorID string, flow string) context.Context {
	distinctID := actorID
	if distinctID == "" {
		distinctID = fallbackOwnerID
	}
	ownerID := fallbackOwnerID
	hubType := ""
	if hubID != "" {
		if hub, err := e.store.GetHub(hubID); err == nil && hub != nil {
			ownerID = hub.OwnerID
			hubType = hub.HubType
			if actorID == "" {
				if hub.HubType == "team" {
					distinctID = hubDistinctID(hub.ID)
				} else {
					distinctID = hub.OwnerID
				}
			}
		}
	}
	if distinctID == "" && hubID != "" {
		distinctID = hubDistinctID(hubID)
	}
	metadata := map[string]any{
		"hub_id":   hubID,
		"hub_type": hubType,
		"llm_flow": flow,
	}
	if ownerID != "" {
		metadata["owner_id"] = ownerID
	}
	if actorID != "" {
		metadata["triggered_by"] = actorID
	}
	return anthropic.WithTracking(ctx, anthropic.Tracking{
		DistinctID: distinctID,
		Metadata:   metadata,
	})
}

func hubDistinctID(hubID string) string {
	hubID = strings.TrimSpace(hubID)
	if hubID == "" {
		return ""
	}
	if strings.HasPrefix(hubID, "hub:") {
		return hubID
	}
	return "hub:" + hubID
}

func truncateForLLM(content string) string {
	if len(content) > 6000 {
		return content[:6000] + "\n\n[truncated]"
	}
	return content
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		b = []byte(fmt.Sprintf("%016x", time.Now().UnixNano()))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

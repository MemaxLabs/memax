package mcpv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// forget is memax_forget (and forget_memory) on the V2 record. An agent
// can never forget (plan 25 §5.12): this is a request a person confirms.
// The ledger's Forget command, with propagation and the tombstone, is epic
// 2.4; until it lands the person confirms on the web, so the in-agent
// elicitation for Forget waits for it too.
func (s *Server) forget(ctx context.Context, c *handler.MCPToolCall, v *view) (*mcp.CallToolResult, bool) {
	var a struct {
		ID      string `json:"id"`
		SpaceID string `json:"space_id"`
	}
	_ = json.Unmarshal(c.Args, &a)
	ref := strings.TrimSpace(a.ID)
	_, _, isRef := ledger.ParseRef(ref)
	_, uuidErr := uuid.Parse(ref)
	if !isRef && uuidErr != nil {
		return nil, false
	}
	spaces, _, res := v.readTarget(ctx, a.SpaceID)
	if res != nil {
		return res, true
	}
	if len(spaces) == 0 {
		if isRef {
			return errorResult(fmt.Sprintf("Memory not found: %s", ref)), true
		}
		return c.RunV1(nil, v.exclude()), true
	}
	m, err := s.ledger.GetMemory(ctx, v.p.Scope.Narrow(ids(spaces)...), ref)
	if errors.Is(err, ledger.ErrNotFound) && !isRef {
		return c.RunV1(nil, v.exclude()), true
	}
	if err != nil {
		return s.ledgerError(ctx, err), true
	}
	sp := spaceOf(spaces, m.SpaceID)
	if m.Lifecycle == lifecycle.Forgotten {
		return textResult(fmt.Sprintf("%s is already forgotten.", m.Ref), nil), true
	}
	d := policy.Decide(policyActor(v.p, sp), policy.ActionForget, policy.Object{Ref: m.Ref, Lifecycle: m.Lifecycle,
		Decision: m.Kind == ledger.KindDecision, External: m.Trust.External()}, policy.Space{Name: sp.Hub.Name, Kind: sp.Grant.Kind})
	if d.Effect == policy.EffectRefuse && d.Code != policy.CodePersonMustForget {
		return errorResult(d.Message), true
	}
	where := s.memoryURL(sp.Hub, m.Ref)
	text := fmt.Sprintf("%s wasn't forgotten: an agent can't forget in %s. Forget needs a person; ask them to forget it on the web", m.Ref, sp.Hub.Name)
	if where != "" {
		text += " at " + where
	}
	text += ". Until they do, it stays kept."
	return textResult(text, nil), true
}

// noteWrite is memax_capture in a space on V2. It keeps V1's path, which
// writes raw material rather than the record: a capture becomes notes for
// Dream to fold (never proposals, so Review stays quiet). The agent must
// still be allowed to write in the space, and a capture is scanned for
// credentials first.
//
// TODO(v2 notes): v2.notes is a view over V1 memories until cutover
// (plan §5.4), so V1's memory path is the note path. Write through the
// ledger once notes are V2 records.
func (s *Server) noteWrite(ctx context.Context, c *handler.MCPToolCall, v *view) (*mcp.CallToolResult, bool) {
	hubID := handler.GetWriteHubID(c.HTTP)
	if hubID == "" || !v.onV2(hubID) {
		return nil, false
	}
	hub, err := v.hub(hubID)
	if err != nil {
		return nil, false
	}
	p, res := v.principal(ctx)
	if res != nil {
		return res, true
	}
	if p.Impersonated {
		return errorResult("Impersonation sessions can read the record but not change it: a receipt must name who acted."), true
	}
	sp, res := v.spaceFor(ctx, hub.Hub, false)
	if res != nil {
		return res, true
	}
	if res := s.guardWrite(sp, p, handler.MCPCaptureContent(c.Args)); res != nil {
		return res, true
	}
	out := c.RunV1(nil, nil)
	if out == nil || out.IsError {
		return out, true
	}
	text := resultText(out)
	if i := strings.Index(text, "). "); i > 0 {
		text = text[:i+1] + fmt.Sprintf(" Saved as notes in %s: Dream folds them into proposals for Review, and nothing is kept until a person keeps it.", sp.Hub.Name)
	}
	return textResult(text, nil), true
}

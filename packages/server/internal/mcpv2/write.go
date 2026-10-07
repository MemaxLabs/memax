package mcpv2

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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
// never forgets (plan 25 §5.12, rule 7): this records a forget request,
// receipted, which a person forgets or keeps on the web (the memory's page
// and Review show it). There is no in-agent confirmation: a hook can
// answer an elicitation by itself, and Forget can't be undone.
func (s *Server) forget(ctx context.Context, c *handler.MCPToolCall, v *view) (*mcp.CallToolResult, bool) {
	var a struct {
		ID         string `json:"id"`
		SpaceID    string `json:"space_id"`
		Reason     string `json:"reason"`
		SessionRef string `json:"session_ref"`
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
		return textResult(fmt.Sprintf("%s is already forgotten: its words are gone from Memax, every compiled file and every agent.", m.Ref), nil), true
	}
	p, res := v.principal(ctx)
	if res != nil {
		return res, true
	}
	if p.Impersonated {
		return errorResult("Impersonation sessions can read the record but not change it: a receipt must name who acted."), true
	}
	where := s.memoryURL(sp.Hub, m.Ref)
	if p.Actor.Kind == policy.ActorPerson {
		text := fmt.Sprintf("%s wasn't forgotten: forget it yourself on the web", m.Ref)
		if where != "" {
			text += " at " + where
		}
		return textResult(text+", where you see everything it goes with.", nil), true
	}
	reason := strings.TrimSpace(a.Reason)
	sessionRef := sessionRefOf(c, a.SessionRef)
	day := s.now().UTC().Format("2006-01-02")
	key := sha256.Sum256([]byte(strings.Join([]string{"forget", p.Actor.ID.String(), m.ID.String(), reason, day}, "\x00")))
	out, err := s.ledger.Apply(ctx, &ledger.RequestForget{
		Meta: ledger.Meta{Actor: p.Actor, Scope: p.Scope.Narrow(sp.ID), Via: policy.ViaMCP, SessionRef: sessionRef,
			IdempotencyKey: "mcp-forget:" + base64.RawURLEncoding.EncodeToString(key[:]), Reason: reason},
		Memory: m.ID.String(),
	})
	if err != nil {
		return s.ledgerError(ctx, err), true
	}
	if out.Outcome == ledger.OutcomeRefused {
		return errorResult(out.Policy.Message), true
	}
	text := fmt.Sprintf("%s wasn't forgotten yet: an agent can't forget. It now waits for a person to forget it", m.Ref)
	if out.ForgetRequest != nil && out.ForgetRequest.Status == ledger.ForgetRequestDeclined {
		text = fmt.Sprintf("A person kept %s when it was asked before", m.Ref)
	}
	if where != "" {
		text += " on the web: " + where
	}
	text += fmt.Sprintf(". Until then it stays kept in %s. Forgetting it removes it from Memax, every compiled file and every agent, and can't be undone.", sp.Hub.Name)
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

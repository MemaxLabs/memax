package mcpv2

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/secrets"
)

type pushArgs struct {
	Content    string `json:"content"`
	HubID      string `json:"hub_id"`
	SpaceID    string `json:"space_id"`
	HubReason  string `json:"hub_reason"`
	Section    string `json:"section"`
	SessionRef string `json:"session_ref"`
	Sources    []struct {
		Kind string `json:"kind"`
		Ref  string `json:"ref"`
		URI  string `json:"uri"`
	} `json:"sources"`
}

// The ways a client can be asked to confirm a keep.
const (
	askNone    = ""
	askMRTR    = "mrtr"    // 2026-07-28: input_required, then a retry
	askSession = "session" // legacy: elicitation/create on the session
)

// push is memax_push (and save_memory) in a space on V2.
func (s *Server) push(ctx context.Context, c *handler.MCPToolCall, v *view) (*mcp.CallToolResult, bool) {
	var a pushArgs
	_ = json.Unmarshal(c.Args, &a)
	hub, err := v.hub(firstNonEmpty(a.HubID, a.SpaceID))
	if err != nil || !v.onV2(hub.Hub.ID) {
		return nil, false
	}
	if c.MCP != nil && c.MCP.Params != nil && c.MCP.Params.RequestState != "" {
		return s.confirmRetry(ctx, c, v), true
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
	statement := strings.TrimSpace(a.Content)
	if statement == "" {
		return errorResult("Content is required."), true
	}
	if utf8.RuneCountInString(statement) > ledger.MaxStatementRunes {
		return s.pushNote(ctx, c, v, sp, p, statement), true
	}

	section := ledger.Section(strings.TrimSpace(a.Section))
	if section == "" {
		section = ledger.SectionConventions
	}
	if !section.Valid() {
		return errorResult("section must be decisions, conventions, preferences or open_question."), true
	}
	kind := ledger.KindFact
	if section == ledger.SectionDecisions {
		kind = ledger.KindDecision
	}
	var sources []ledger.SourceInput
	for _, src := range a.Sources {
		k := ledger.SourceKind(strings.TrimSpace(src.Kind))
		if !k.Valid() || k == ledger.SourceImport {
			return errorResult(fmt.Sprintf("Source kind %q isn't one of session, pr, file, url, issue, email or note.", src.Kind)), true
		}
		sources = append(sources, ledger.SourceInput{Kind: k, Ref: src.Ref, URI: src.URI})
	}

	ask := askMode(c)
	actor := p.Actor
	// A client that can elicit may have a person at it: policy then asks
	// for the keep in the agent instead of only proposing.
	actor.PersonPresent, actor.CanElicit = ask != askNone, ask != askNone
	sessionRef := sessionRefOf(c, a.SessionRef)
	cmd := &ledger.Propose{
		Meta: ledger.Meta{
			Actor: actor, Scope: p.Scope.Narrow(sp.ID), Via: policy.ViaMCP, SessionRef: sessionRef,
			IdempotencyKey: pushKey(actor.ID, sp.ID, sessionRef, statement, section, sources, a.HubReason),
			Reason:         strings.TrimSpace(a.HubReason),
		},
		NewMemory: ledger.NewMemory{SpaceID: sp.ID, Statement: statement, Section: section, Kind: kind, Sources: sources},
	}
	out, err := s.ledger.Apply(ctx, cmd)
	if err != nil {
		return s.ledgerError(ctx, err), true
	}
	s.logPush(c, p, sp, out)
	if out.Outcome == ledger.OutcomeProposed && out.Policy.Code == policy.CodeAutonomyPropose && ask != askNone && kind == ledger.KindDecision {
		// policy.Decide asks in the agent unless the statement contradicts
		// a decision (never claimed here) or is a decision in a space whose
		// decisions need a person on the web (D15): this is the latter.
		out.Policy.Code = policy.CodeDecisionNeedsWeb
	}
	switch out.Outcome {
	case ledger.OutcomeRefused:
		return errorResult(out.Policy.Message), true
	case ledger.OutcomeApplied:
		return s.keptResult(sp, out.Memory, "", fmt.Sprintf("Kept %s in %s.", out.Memory.Ref, sp.Hub.Name)), true
	case ledger.OutcomeNeedsConfirmation:
		if out.Memory.Lifecycle != "proposed" {
			// A replay of a push someone already answered.
			return s.stateResult(sp, out.Memory), true
		}
		return s.askToKeep(ctx, c, sp, p, out.Memory, ask), true
	}
	return s.proposedResult(sp, out.Memory, out.Policy, ask), true
}

// askMode is how this client can be asked to confirm, if at all.
func askMode(c *handler.MCPToolCall) string {
	if c.MCP == nil {
		return askNone
	}
	caps := c.MCP.ClientCapabilities()
	if caps == nil || caps.Elicitation == nil {
		return askNone
	}
	if caps.Elicitation.Form == nil && caps.Elicitation.URL != nil {
		return askNone // URL mode only: no form to keep in
	}
	if c.MCP.ProtocolVersion() >= "2026-07-28" {
		return askMRTR
	}
	if c.MCP.Session != nil && c.MCP.Session.ID() != "" {
		return askSession
	}
	return askNone
}

// sessionRefOf is the agent session a call belongs to: what the call says,
// or the legacy MCP session it came on.
func sessionRefOf(c *handler.MCPToolCall, given string) string {
	if ref := strings.TrimSpace(given); ref != "" {
		if utf8.RuneCountInString(ref) > ledger.MaxSessionRef {
			ref = string([]rune(ref)[:ledger.MaxSessionRef])
		}
		return ref
	}
	if c.MCP != nil && c.MCP.Session != nil && c.MCP.Session.ID() != "" {
		return "mcp:" + c.MCP.Session.ID()
	}
	return ""
}

// pushKey is a push's idempotency key: the same statement pushed again by
// the same agent in the same session and space is the same command, so a
// retried tool call never proposes twice.
func pushKey(actor, space uuid.UUID, session, statement string, section ledger.Section, sources []ledger.SourceInput, reason string) string {
	b, _ := json.Marshal([]any{actor, space, session, statement, section, sources, strings.TrimSpace(reason)})
	sum := sha256.Sum256(b)
	return "mcp-push:" + base64.RawURLEncoding.EncodeToString(sum[:])
}

// pushNote saves content too long for one statement as a note, V1's
// memory path, which Dream folds into proposals.
func (s *Server) pushNote(ctx context.Context, c *handler.MCPToolCall, v *view, sp space, p *v2api.Principal, content string) *mcp.CallToolResult {
	if res := s.guardWrite(sp, p, content); res != nil {
		return res
	}
	res := c.RunV1(nil, nil)
	if res == nil || res.IsError {
		return res
	}
	out, _ := res.StructuredContent.(handler.MCPPushOutput)
	out.Status = handler.MCPPushNote
	out.Message = fmt.Sprintf("Saved as a note in %s: a memory is one statement of at most %d characters. Dream folds notes into proposals for Review.",
		sp.Hub.Name, ledger.MaxStatementRunes)
	return textResult(fmt.Sprintf("Saved as a note (id: %s). %s", out.ID, out.Message), out)
}

// guardWrite refuses a write the agent may not make in the space (read
// autonomy, not connected, paused) or that carries a credential, using
// the same policy as the ledger. It is for writes outside the ledger
// (notes, board cards).
func (s *Server) guardWrite(sp space, p *v2api.Principal, texts ...string) *mcp.CallToolResult {
	var found []string
	for _, t := range texts {
		for _, name := range secrets.DetectCredentials(t) {
			found = append(found, name)
		}
	}
	d := policy.Decide(policyActor(p, sp), policy.ActionPropose, policy.Object{Secrets: found},
		policy.Space{Name: sp.Hub.Name, Kind: sp.Grant.Kind})
	if d.Effect == policy.EffectRefuse {
		return errorResult(d.Message)
	}
	return nil
}

// policyActor is the principal as policy sees it in one space.
func policyActor(p *v2api.Principal, sp space) policy.Actor {
	autonomy := p.Actor.Autonomy
	if sp.Grant.Autonomy != "" {
		autonomy = sp.Grant.Autonomy
	}
	return policy.Actor{
		Kind: p.Actor.Kind, Name: p.Actor.Name, Role: sp.Grant.Role, CanForget: sp.Grant.CanForget,
		Autonomy: autonomy, AgentStatus: sp.Grant.AgentStatus, Credential: p.Actor.Credential, Via: policy.ViaMCP,
	}
}

// --- Results ---

func (s *Server) keptResult(sp space, m *ledger.Memory, assurance, text string) *mcp.CallToolResult {
	return textResult(text, handler.MCPPushOutput{
		Status: handler.MCPPushKept, ID: m.Ref, SpaceID: sp.ID.String(), Assurance: assurance, Message: text,
	})
}

func (s *Server) proposedResult(sp space, m *ledger.Memory, d policy.Decision, ask string) *mcp.CallToolResult {
	review := s.reviewURL(sp.Hub, m.Ref)
	var b strings.Builder
	fmt.Fprintf(&b, "Proposed %s in %s.", m.Ref, sp.Hub.Name)
	switch {
	case d.Code == policy.CodeDecisionNeedsWeb:
		fmt.Fprintf(&b, " Decisions in %s need a person on the web, so it can't be kept from the agent.", sp.Hub.Name)
	case d.Quarantine:
		b.WriteString(" It cites an outside source, so it is quarantined until a person keeps it on the web.")
	case d.Message != "" && d.Code != policy.CodeAutonomyPropose:
		b.WriteString(" " + d.Message)
	case ask == askNone:
		b.WriteString(" Nobody can confirm it here, so it waits in Review.")
	}
	if review != "" {
		fmt.Fprintf(&b, " Keep it in Review: %s", review)
	}
	text := b.String()
	return textResult(text, handler.MCPPushOutput{
		Status: handler.MCPPushProposed, ID: m.Ref, SpaceID: sp.ID.String(), ReviewURL: review, Message: text,
	})
}

// stateResult reports a memory's current state (a replayed push).
func (s *Server) stateResult(sp space, m *ledger.Memory) *mcp.CallToolResult {
	if m.Lifecycle == "kept" {
		return s.keptResult(sp, m, "", fmt.Sprintf("%s is kept in %s.", m.Ref, sp.Hub.Name))
	}
	text := fmt.Sprintf("%s is %s in %s.", m.Ref, m.State, sp.Hub.Name)
	return textResult(text, handler.MCPPushOutput{Status: handler.MCPPushProposed, ID: m.Ref, SpaceID: sp.ID.String(),
		ReviewURL: s.reviewURL(sp.Hub, m.Ref), Message: text})
}

// ledgerError turns a ledger error into a tool error the model can act on.
func (s *Server) ledgerError(ctx context.Context, err error) *mcp.CallToolResult {
	var ve *ledger.ValidationError
	var te *ledger.TransitionError
	var clash *ledger.EditClashError
	switch {
	case errors.As(err, &ve):
		return errorResult(ve.Error())
	case errors.As(err, &clash):
		return errorResult(clash.Error())
	case errors.As(err, &te):
		return errorResult(te.Error())
	case errors.Is(err, ledger.ErrNotFound):
		return errorResult("Nothing by that reference in the spaces this connection can read.")
	case errors.Is(err, ledger.ErrAmbiguousRef):
		return errorResult("That display ID exists in more than one of your spaces. Pass space_id.")
	case errors.Is(err, ledger.ErrBusy):
		return errorResult("Another change is holding this memory. Try again in a moment.")
	}
	s.log.ErrorContext(ctx, "mcp: ledger command failed", "error", err)
	return errorResult("Something went wrong on our side. Try again; if it keeps happening, contact support@memax.app.")
}

func (s *Server) logPush(c *handler.MCPToolCall, p *v2api.Principal, sp space, out ledger.Result) {
	if out.Memory == nil {
		return
	}
	c.LogEvent("push", sp.ID.String(), "mcp", p.Actor.Agent, map[string]any{"summary": out.Memory.Ref})
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

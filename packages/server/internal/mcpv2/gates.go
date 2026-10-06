package mcpv2

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Decision gates (plan 25 §5.12, epic 1.11). In a space on the V2 record
// memax_request_decision asks through the ledger (RequestDecision) and
// returns the gate's G- ID at once. The answer reaches the connection that
// asked in its next memax_recall, once (ledger.TakeGateNews).
//
// On 2026-07-28 clients that can elicit, the question also goes to the
// person in the agent, as a multi round-trip request: the gate is written
// first, then the tool returns input_required with the options, and the
// retry carries the person's choice. An answer there is the person's, via
// the agent, client-attested, so it is never offered where the space's
// decisions need a person on the web (D15), nor to an agent on an API key
// (a key never answers). Legacy sessions aren't asked over
// elicitation/create: that would hold the call open for minutes, and the
// tool returns at once.

type decisionArgs struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
	Context  string   `json:"context"`
	SpaceID  string   `json:"space_id"`
}

// gateElicitID names the multi round-trip question; gateLater is its
// "answer later" choice.
const (
	gateElicitID = "answer"
	gateLater    = "later"
)

// requestDecision is memax_request_decision in a space on V2.
func (s *Server) requestDecision(ctx context.Context, c *handler.MCPToolCall, v *view) (*mcp.CallToolResult, bool) {
	var a decisionArgs
	_ = json.Unmarshal(c.Args, &a)
	// space_id names a space on V2 to ask in; V1's tool has no such
	// argument, so a V1 space named here leaves the request to the write
	// hub, as V1 would.
	hubID := handler.GetWriteHubID(c.HTTP)
	if ref := strings.TrimSpace(a.SpaceID); ref != "" {
		hub, err := v.hub(ref)
		if err != nil {
			return errorResult("Space not found or not accessible."), true
		}
		if v.onV2(hub.Hub.ID) {
			hubID = hub.Hub.ID
		}
	}
	if hubID == "" || !v.onV2(hubID) {
		return nil, false
	}
	if c.MCP != nil && c.MCP.Params != nil && c.MCP.Params.RequestState != "" {
		return s.gateRetry(ctx, c, v), true
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
	question := strings.TrimSpace(a.Question)
	var options []ledger.DecisionOption
	for _, o := range a.Options {
		if label := strings.TrimSpace(o); label != "" {
			options = append(options, ledger.DecisionOption{Label: label})
		}
	}
	if question == "" || len(options) < ledger.MinGateOptions {
		return errorResult("memax_request_decision requires 'question' and 2-4 'options'."), true
	}
	sessionRef := sessionRefOf(c, "")
	why := strings.TrimSpace(a.Context)
	out, err := s.ledger.Apply(ctx, &ledger.RequestDecision{
		Meta: ledger.Meta{
			Actor: p.Actor, Scope: p.Scope.Narrow(sp.ID), Via: policy.ViaMCP, SessionRef: sessionRef,
			IdempotencyKey: gateKey(p.Actor.ID, sp.ID, sessionRef, question, why, options),
		},
		SpaceID: sp.ID, Question: question, Context: why, Options: options,
	})
	if err != nil {
		return s.ledgerError(ctx, err), true
	}
	if out.Outcome == ledger.OutcomeRefused {
		return errorResult(out.Policy.Message), true
	}
	g := out.Gate
	c.LogEvent("push", sp.ID.String(), "mcp/request_decision", p.Actor.Agent, map[string]any{"gate": g.Ref})
	if g.Status == ledger.GateWaiting && askMode(c) == askMRTR && !g.NeedsWeb && p.Actor.Credential != policy.CredentialAPIKey {
		if res := s.askGate(c, sp, p, g); res != nil {
			return res, true
		}
	}
	return s.gateResult(sp, g, ""), true
}

// gateKey is a question's idempotency key: the same question asked again
// by the same agent in the same session and space is the same gate, so a
// retried tool call never asks twice.
func gateKey(actor, space uuid.UUID, session, question, why string, options []ledger.DecisionOption) string {
	b, _ := json.Marshal([]any{actor, space, session, question, why, options})
	sum := sha256.Sum256(b)
	return "mcp-gate:" + base64.RawURLEncoding.EncodeToString(sum[:])
}

func gateForm(spaceName, agent string, g *ledger.Gate) *mcp.ElicitParams {
	values := make([]string, 0, len(g.Options)+1)
	names := make([]string, 0, len(g.Options)+1)
	for i, o := range g.Options {
		values = append(values, strconv.Itoa(i+1))
		names = append(names, o.Label)
	}
	values = append(values, gateLater)
	names = append(names, "Answer later in Memax")
	msg := fmt.Sprintf("%s asks in %s (%s):\n\n%s", agent, spaceName, g.Ref, g.Question)
	if g.Context != "" {
		msg += "\n\n" + g.Context
	}
	return &mcp.ElicitParams{
		Mode:    "form",
		Message: msg,
		RequestedSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"choice": map[string]any{
					"type":      "string",
					"title":     "Your answer",
					"enum":      values,
					"enumNames": names,
				},
			},
			"required": []string{"choice"},
		},
	}
}

// askGate asks the person in the agent: an input_required result whose
// signed state names the gate, this caller and this call.
func (s *Server) askGate(c *handler.MCPToolCall, sp space, p *v2api.Principal, g *ledger.Gate) *mcp.CallToolResult {
	state, err := s.state.sign(confirmState{
		Memory: g.ID.String(), Ref: g.Ref, Version: g.Version, Space: sp.ID.String(),
		Actor: p.Actor.ID.String(), Person: p.Scope.PersonID.String(), Tool: c.Tool,
		Digest: argsDigest(c.Args), Expires: s.now().Add(s.stateTTL).Unix(),
	})
	if err != nil {
		return nil
	}
	return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{gateElicitID: gateForm(sp.Hub.Name, actorName(p), g)}, RequestState: state}
}

// gateRetry is the retry of an asked question: the person's choice, or
// none (later, declined, cancelled), and the signed state.
func (s *Server) gateRetry(ctx context.Context, c *handler.MCPToolCall, v *view) *mcp.CallToolResult {
	p, st, sp, failed := s.verifyRetry(ctx, c, v, "Nothing was answered; the question waits for a person in Memax.")
	if failed != nil {
		return failed
	}
	gateID, _ := uuid.Parse(st.Memory)
	choice := ""
	if r, ok := c.MCP.Params.InputResponses[gateElicitID].(*mcp.ElicitResult); ok && r != nil && r.Action == "accept" {
		choice, _ = r.Content["choice"].(string)
	}
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 {
		return s.currentGate(ctx, p, sp, gateID, "")
	}
	agent := p.Actor.Agent
	if p.Connection != nil {
		agent = string(p.Connection.Agent)
	}
	out, err := s.ledger.Apply(ctx, &ledger.AnswerGate{
		Meta: ledger.Meta{
			// The person answered in the agent: the answer is theirs, via the
			// agent, client-attested (policy derives the assurance from via).
			Actor: ledger.Actor{Kind: policy.ActorPerson, ID: p.Scope.PersonID, Agent: agent, Credential: p.Actor.Credential},
			Scope: p.Scope.Narrow(sp.ID), Via: policy.ViaMCP,
			IdempotencyKey: fmt.Sprintf("mcp-gate-answer:%s:%d", gateID, n),
		},
		Gate: gateID.String(), ExpectedVersion: st.Version, Option: n, Delivered: true,
	})
	var ended *ledger.GateStateError
	switch {
	case errors.As(err, &ended), errors.Is(err, ledger.ErrEditClash):
		// Answered on the web in the meantime, withdrawn, or expired.
		return s.currentGate(ctx, p, sp, gateID, "")
	case err != nil:
		return s.ledgerError(ctx, err)
	case out.Outcome == ledger.OutcomeRefused:
		return s.currentGate(ctx, p, sp, gateID, out.Policy.Message)
	}
	who := "you"
	if p.Connection != nil {
		who = "you via " + p.Connection.DisplayName
	}
	return s.gateResult(sp, out.Gate, "Answered by "+who+".")
}

// currentGate reports the gate as it is now.
func (s *Server) currentGate(ctx context.Context, p *v2api.Principal, sp space, id uuid.UUID, note string) *mcp.CallToolResult {
	g, err := s.ledger.GetGate(ctx, p.Scope.Narrow(sp.ID), id.String())
	if err != nil {
		return s.ledgerError(ctx, err)
	}
	return s.gateResult(sp, g, note)
}

// gateResult is memax_request_decision's answer: the gate's ID, where a
// person answers it, and how it stands.
func (s *Server) gateResult(sp space, g *ledger.Gate, note string) *mcp.CallToolResult {
	item := s.gateItem(sp, g)
	var b strings.Builder
	switch g.Status {
	case ledger.GateAnswered:
		fmt.Fprintf(&b, "%s in %s is answered: %s. It is kept as %s, a decision by the person.", g.Ref, sp.Hub.Name, item.Answer, item.Memory)
	case ledger.GateWithdrawn:
		fmt.Fprintf(&b, "%s in %s was withdrawn, so no answer is coming.", g.Ref, sp.Hub.Name)
	case ledger.GateExpired:
		fmt.Fprintf(&b, "%s in %s expired without an answer.", g.Ref, sp.Hub.Name)
	default:
		fmt.Fprintf(&b, "Asked in %s as %s.", sp.Hub.Name, g.Ref)
		if g.NeedsWeb {
			fmt.Fprintf(&b, " Decisions in %s need a person on the web, so it is answered there.", sp.Hub.Name)
		}
		if item.URL != "" {
			fmt.Fprintf(&b, " A person answers it in Review: %s.", item.URL)
		}
		fmt.Fprintf(&b, " The answer comes back in this connection's next memax_recall; it waits until %s.",
			g.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if note != "" {
		b.WriteString(" " + note)
	}
	item.Message = b.String()
	return textResult(item.Message, item)
}

// gateItem is a gate as MCP results carry it.
func (s *Server) gateItem(sp space, g *ledger.Gate) handler.MCPGate {
	item := handler.MCPGate{
		ID: g.Ref, SpaceID: sp.ID.String(), Space: sp.Hub.Name, Question: g.Question, Status: string(g.Status),
		ExpiresAt: g.ExpiresAt.UTC().Format(time.RFC3339), URL: s.reviewURL(sp.Hub, g.Ref),
	}
	if a := g.Answer; a != nil {
		item.Option, item.Answer, item.Memory = a.Option, a.Label, a.Memory.Ref
	}
	return item
}

// gateNews adds how this connection's gates ended (once), and without a
// query the ones still waiting, to a recall.
func (s *Server) gateNews(ctx context.Context, p *v2api.Principal, scope ledger.Scope, bySpace map[uuid.UUID]space, digest bool, part *v2Part, b *strings.Builder) {
	if p.Actor.Kind != policy.ActorAgent || p.Connection == nil {
		return
	}
	news, err := s.ledger.TakeGateNews(ctx, scope, p.Actor.ID, digest)
	if err != nil {
		part.out.Partial = true
		s.logReadError(ctx, "gate news", err)
		return
	}
	if len(news.Ended)+len(news.Waiting) == 0 {
		return
	}
	b.WriteString("Decisions you asked for:\n")
	for _, list := range [][]ledger.Gate{news.Ended, news.Waiting} {
		for i := range list {
			g := &list[i]
			sp := bySpace[g.SpaceID]
			item := s.gateItem(sp, g)
			part.out.Gates = append(part.out.Gates, item)
			switch g.Status {
			case ledger.GateAnswered:
				fmt.Fprintf(b, "- %s (%s) answered: %s. Kept as %s.\n", g.Ref, sp.Hub.Name, item.Answer, item.Memory)
			case ledger.GateWithdrawn:
				fmt.Fprintf(b, "- %s (%s) was withdrawn by a person.\n", g.Ref, sp.Hub.Name)
			case ledger.GateExpired:
				fmt.Fprintf(b, "- %s (%s) expired without an answer.\n", g.Ref, sp.Hub.Name)
			default:
				fmt.Fprintf(b, "- %s (%s) is still waiting on a person.\n", g.Ref, sp.Hub.Name)
			}
		}
	}
	b.WriteString("\n")
}

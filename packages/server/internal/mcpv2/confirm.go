package mcpv2

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// In-agent Keep (plan 25 §5.12, HANDOFF rule 4). A Propose-level agent's
// proposal is written first, then the person is asked in the agent:
//
//   - On 2026-07-28 clients the tool returns resultType "input_required"
//     with a form-mode elicitation and a requestState, and the client
//     retries the same call with the answer. The state is HMAC-signed and
//     binds the proposal to the principal (the connection and its person),
//     the tool, a digest of the arguments and an expiry, so a retry can
//     only answer this question, for this caller, soon.
//   - On a legacy session the server sends elicitation/create and waits
//     for the answer on the same call.
//
// Only accept with "keep" (or "edit" with new words) is a Keep, recorded
// as kept by the person via the agent with assurance client_attested:
// Claude Code's hooks can auto-accept an elicitation, so it attests the
// client's consent, not a human click. Decline, cancel, "leave", a
// timeout, or no answer leave the proposal waiting in Review.

const elicitID = "keep"

// The choices of the Keep form.
const (
	choiceKeep  = "keep"
	choiceEdit  = "edit"
	choiceLeave = "leave"
)

func keepForm(spaceName, statement string) *mcp.ElicitParams {
	return &mcp.ElicitParams{
		Mode:    "form",
		Message: fmt.Sprintf("Keep this in %s?\n\n“%s”", spaceName, statement),
		RequestedSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"choice": map[string]any{
					"type":      "string",
					"title":     "Keep this memory?",
					"enum":      []string{choiceKeep, choiceEdit, choiceLeave},
					"enumNames": []string{"Keep", "Edit", "Leave as proposal"},
					"default":   choiceKeep,
				},
				"statement": map[string]any{
					"type":        "string",
					"title":       "Edited statement",
					"description": "For Edit: the words to keep instead.",
				},
			},
			"required": []string{"choice"},
		},
	}
}

// askToKeep asks the person to keep a fresh proposal.
func (s *Server) askToKeep(ctx context.Context, c *handler.MCPToolCall, sp space, p *v2api.Principal, m *ledger.Memory, ask string) *mcp.CallToolResult {
	form := keepForm(sp.Hub.Name, m.Statement)
	switch ask {
	case askMRTR:
		state, err := s.state.sign(confirmState{
			Memory: m.ID.String(), Ref: m.Ref, Version: m.Version, Space: sp.ID.String(),
			Actor: p.Actor.ID.String(), Person: p.Scope.PersonID.String(), Tool: c.Tool,
			Digest: argsDigest(c.Args), Expires: s.now().Add(s.stateTTL).Unix(),
		})
		if err != nil {
			return s.proposedResult(sp, m, policy.Decision{}, askNone)
		}
		return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{elicitID: form}, RequestState: state}
	case askSession:
		ectx, cancel := context.WithTimeout(ctx, s.elicitTimeout)
		defer cancel()
		answer, err := c.MCP.Session.Elicit(ectx, form)
		if err != nil {
			s.log.InfoContext(ctx, "mcp: no answer to a keep elicitation", "memory", m.Ref, "error", err)
			answer = nil
		}
		return s.applyAnswer(ctx, sp, p, m.ID, m.Ref, m.Version, answer)
	}
	return s.proposedResult(sp, m, policy.Decision{}, askNone)
}

// confirmRetry is the retry of a multi round-trip push: the answer and the
// signed state of the question.
func (s *Server) confirmRetry(ctx context.Context, c *handler.MCPToolCall, v *view) *mcp.CallToolResult {
	p, st, sp, failed := s.verifyRetry(ctx, c, v, "Nothing was kept; the proposal waits in Review.")
	if failed != nil {
		return failed
	}
	memID, _ := uuid.Parse(st.Memory)
	var answer *mcp.ElicitResult
	if r, ok := c.MCP.Params.InputResponses[elicitID].(*mcp.ElicitResult); ok {
		answer = r
	}
	return s.applyAnswer(ctx, sp, p, memID, st.Ref, st.Version, answer)
}

// verifyRetry checks a multi round-trip retry's signed state: issued to
// this caller, for this call, not expired, about a space the caller still
// reaches. When it can't be used, failed is the error result, ending with
// nothing (what didn't happen).
func (s *Server) verifyRetry(ctx context.Context, c *handler.MCPToolCall, v *view, nothing string) (*v2api.Principal, confirmState, space, *mcp.CallToolResult) {
	p, res := v.principal(ctx)
	if res != nil {
		return nil, confirmState{}, space{}, res
	}
	st, err := s.state.verify(c.MCP.Params.RequestState, s.now())
	if err == nil {
		switch {
		case st.Actor != p.Actor.ID.String() || st.Person != p.Scope.PersonID.String():
			err = errors.New("it was issued to another connection")
		case st.Tool != c.Tool || st.Digest != argsDigest(c.Args):
			err = errors.New("it was issued for a different call")
		}
	}
	if err != nil {
		s.log.WarnContext(ctx, "mcp: refused a confirmation", "error", err)
		return nil, st, space{}, errorResult("This confirmation can't be used: " + err.Error() + ". " + nothing)
	}
	spaceID, _ := uuid.Parse(st.Space)
	for _, x := range v.spaces {
		if x.ID == spaceID {
			return p, st, x, nil
		}
	}
	return nil, st, space{}, errorResult("This connection no longer reaches the space of " + st.Ref + ". " + nothing)
}

// applyAnswer keeps (or edits and keeps) the proposal when the person
// accepted, and leaves it in Review otherwise.
func (s *Server) applyAnswer(ctx context.Context, sp space, p *v2api.Principal, memID uuid.UUID, ref string, version int, answer *mcp.ElicitResult) *mcp.CallToolResult {
	leave := func(why string) *mcp.CallToolResult {
		review := s.reviewURL(sp.Hub, ref)
		text := fmt.Sprintf("Left %s as a proposal in %s%s.", ref, sp.Hub.Name, why)
		if review != "" {
			text += " It waits in Review: " + review
		}
		return textResult(text, handler.MCPPushOutput{Status: handler.MCPPushProposed, ID: ref, SpaceID: sp.ID.String(),
			ReviewURL: review, Message: text})
	}
	if answer == nil {
		return leave(": no answer came back")
	}
	if answer.Action != "accept" {
		return leave("")
	}
	choice, _ := answer.Content["choice"].(string)
	if choice == "" {
		choice = choiceKeep
	}
	edited, _ := answer.Content["statement"].(string)
	edited = strings.TrimSpace(edited)
	switch {
	case choice == choiceLeave:
		return leave("")
	case choice == choiceEdit && edited == "":
		return leave(": Edit came back without new words")
	case choice != choiceKeep && choice != choiceEdit:
		return leave(": the answer wasn't one of keep, edit or leave")
	}

	agent := p.Actor.Agent
	if p.Connection != nil {
		agent = string(p.Connection.Agent)
	}
	meta := ledger.Meta{
		// The person confirmed in the agent: the Keep is theirs, via the
		// agent, client-attested (policy derives the assurance from via).
		Actor: ledger.Actor{Kind: policy.ActorPerson, ID: p.Scope.PersonID, Agent: agent, Credential: p.Actor.Credential},
		Scope: p.Scope.Narrow(sp.ID), Via: policy.ViaMCP,
		IdempotencyKey: fmt.Sprintf("mcp-confirm:%s:%d:%s", memID, version, choice),
	}
	var cmd ledger.Command = &ledger.Keep{Meta: meta, Memory: memID.String(), ExpectedVersion: version}
	if choice == choiceEdit {
		meta.IdempotencyKey += ":" + argsDigest(json.RawMessage(fmt.Sprintf("%q", edited)))
		cmd = &ledger.Edit{Meta: meta, Memory: memID.String(), ExpectedVersion: version, Statement: edited, Keep: true}
	}
	out, err := s.ledger.Apply(ctx, cmd)
	var te *ledger.TransitionError
	switch {
	case errors.As(err, &te), errors.Is(err, ledger.ErrEditClash):
		return leave(": it changed in Review in the meantime")
	case err != nil:
		return s.ledgerError(ctx, err)
	case out.Outcome == ledger.OutcomeRefused:
		return leave(". " + strings.TrimSuffix(out.Policy.Message, "."))
	}
	who := "you"
	if p.Connection != nil {
		who = "you via " + p.Connection.DisplayName
	}
	return s.keptResult(sp, out.Memory, string(policy.AssuranceClientAttested),
		fmt.Sprintf("Kept by %s · %s in %s.", who, out.Memory.Ref, sp.Hub.Name))
}

// --- Request state ---

// confirmState is what a multi round-trip question carries. It names IDs
// only, never the statement.
type confirmState struct {
	Memory  string `json:"m"`
	Ref     string `json:"r"`
	Version int    `json:"n"`
	Space   string `json:"s"`
	Actor   string `json:"a"`
	Person  string `json:"p"`
	Tool    string `json:"t"`
	Digest  string `json:"d"`
	Expires int64  `json:"e"`
}

type stateSigner struct{ key []byte }

const stateVersion = "v1"

func (s *stateSigner) sign(st confirmState) (string, error) {
	b, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	body := stateVersion + "." + base64.RawURLEncoding.EncodeToString(b)
	return body + "." + base64.RawURLEncoding.EncodeToString(s.mac(body)), nil
}

func (s *stateSigner) verify(token string, now time.Time) (confirmState, error) {
	var st confirmState
	i := strings.LastIndexByte(token, '.')
	if i < 0 || !strings.HasPrefix(token, stateVersion+".") {
		return st, errors.New("it isn't a Memax confirmation")
	}
	body, sig := token[:i], token[i+1:]
	want := s.mac(body)
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, want) {
		return st, errors.New("its signature doesn't verify")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(body, stateVersion+"."))
	if err != nil || json.Unmarshal(raw, &st) != nil {
		return st, errors.New("it is malformed")
	}
	if now.Unix() > st.Expires {
		return st, errors.New("it expired")
	}
	return st, nil
}

func (s *stateSigner) mac(body string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(body))
	return m.Sum(nil)
}

// argsDigest fingerprints a call's arguments independently of key order
// and whitespace, so a client that re-encodes them on retry still matches.
func argsDigest(args json.RawMessage) string {
	var v any
	if err := json.Unmarshal(args, &v); err != nil {
		v = string(args)
	}
	b, _ := json.Marshal(v) // maps marshal with sorted keys
	sum := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

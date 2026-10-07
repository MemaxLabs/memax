package ledger

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Agent connections (plan 25 §5.15, epic 1.8). Every API key and OAuth
// grant that acts on the V2 record resolves to one connection: the
// agent's identity on receipts, plus its autonomy in each space. The
// person decides that autonomy; agents never change it (policy
// .DecideConnection), and raising it needs a person on the web.

// AgentKind is which agent a connection is (HANDOFF §4).
type AgentKind string

// The agents. "other" covers anything Memax doesn't know by name.
const (
	AgentClaudeCode AgentKind = "claude-code"
	AgentCodex      AgentKind = "codex"
	AgentCursor     AgentKind = "cursor"
	AgentChatGPT    AgentKind = "chatgpt"
	AgentClaude     AgentKind = "claude"
	AgentGeminiCLI  AgentKind = "gemini-cli"
	AgentCopilot    AgentKind = "copilot"
	AgentOpenCode   AgentKind = "opencode"
	AgentWindsurf   AgentKind = "windsurf"
	AgentOther      AgentKind = "other"
)

// AgentKinds lists every agent.
var AgentKinds = []AgentKind{AgentClaudeCode, AgentCodex, AgentCursor, AgentChatGPT, AgentClaude,
	AgentGeminiCLI, AgentCopilot, AgentOpenCode, AgentWindsurf, AgentOther}

// Valid reports whether k is a known agent.
func (k AgentKind) Valid() bool { return slices.Contains(AgentKinds, k) }

// Name is how people see the agent ("Claude Code").
func (k AgentKind) Name() string {
	switch k {
	case AgentClaudeCode:
		return "Claude Code"
	case AgentCodex:
		return "Codex"
	case AgentCursor:
		return "Cursor"
	case AgentChatGPT:
		return "ChatGPT"
	case AgentClaude:
		return "Claude"
	case AgentGeminiCLI:
		return "Gemini CLI"
	case AgentCopilot:
		return "Copilot"
	case AgentOpenCode:
		return "OpenCode"
	case AgentWindsurf:
		return "Windsurf"
	}
	return "Agent"
}

// Surface is where the agent usually runs, for a new connection.
func (k AgentKind) Surface() AgentSurface {
	switch k {
	case AgentCursor, AgentCopilot, AgentWindsurf:
		return SurfaceIDE
	case AgentChatGPT, AgentClaude:
		return SurfaceChat
	}
	return SurfaceCLI
}

// StartAutonomy is the level a newly connected agent of this kind starts
// at in a space, below the space's default when the design flows say so
// (plan 25 §7.3 step 3, the Connect board): Cursor and Gemini CLI read
// until a person raises them; every other agent starts at the space's
// default (empty). A person changes it in Agents.
func (k AgentKind) StartAutonomy() policy.Autonomy {
	switch k {
	case AgentCursor, AgentGeminiCLI:
		return policy.AutonomyRead
	}
	return ""
}

// AgentFromV1 maps a V1 agent slug (api_keys.agent_name,
// oauth_grants.agent_name, as `memax setup` and the MCP OAuth flow write
// them) onto an agent.
func AgentFromV1(slug string) AgentKind {
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(slug)), " ", "-") {
	case "claude-code", "claude_code", "claudecode":
		return AgentClaudeCode
	case "codex", "codex-cli":
		return AgentCodex
	case "cursor":
		return AgentCursor
	case "chatgpt", "openai":
		return AgentChatGPT
	case "claude", "claude-ai", "claude-desktop":
		return AgentClaude
	case "gemini", "gemini-cli":
		return AgentGeminiCLI
	case "copilot", "github-copilot", "vscode":
		return AgentCopilot
	case "opencode":
		return AgentOpenCode
	case "windsurf":
		return AgentWindsurf
	}
	return AgentOther
}

// AgentSurface is where an agent runs.
type AgentSurface string

// The surfaces.
const (
	SurfaceCLI   AgentSurface = "cli"
	SurfaceIDE   AgentSurface = "ide"
	SurfaceCloud AgentSurface = "cloud"
	SurfaceChat  AgentSurface = "chat"
)

// AgentSurfaces lists every surface.
var AgentSurfaces = []AgentSurface{SurfaceCLI, SurfaceIDE, SurfaceCloud, SurfaceChat}

// Valid reports whether s is a known surface.
func (s AgentSurface) Valid() bool { return slices.Contains(AgentSurfaces, s) }

// CredentialKind is what a connection is bound to.
type CredentialKind string

// The credential kinds.
const (
	CredentialAPIKey     CredentialKind = "api_key"
	CredentialOAuthGrant CredentialKind = "oauth_grant"
)

// CredentialKinds lists every credential kind.
var CredentialKinds = []CredentialKind{CredentialAPIKey, CredentialOAuthGrant}

// Valid reports whether k is a known credential kind.
func (k CredentialKind) Valid() bool { return k == CredentialAPIKey || k == CredentialOAuthGrant }

// Policy is the credential as policy knows it.
func (k CredentialKind) Policy() policy.Credential {
	if k == CredentialAPIKey {
		return policy.CredentialAPIKey
	}
	return policy.CredentialOAuth
}

// ConnectionState is active, paused or disconnected (terminal).
type ConnectionState string

// The states.
const (
	ConnectionActive       ConnectionState = "active"
	ConnectionPaused       ConnectionState = "paused"
	ConnectionDisconnected ConnectionState = "disconnected"
)

// ConnectionStates lists every state.
var ConnectionStates = []ConnectionState{ConnectionActive, ConnectionPaused, ConnectionDisconnected}

// ConnectionTransitionAllowed is the state machine, in Go; migration 029's
// v2.agent_state_transition_allowed is the same table in SQL (from ""
// means a new connection).
func ConnectionTransitionAllowed(from, to ConnectionState) bool {
	switch {
	case from == "":
		return to == ConnectionActive
	case from == ConnectionDisconnected:
		return false
	case from == to:
		return true
	}
	return to == ConnectionDisconnected || (from == ConnectionActive && to == ConnectionPaused) ||
		(from == ConnectionPaused && to == ConnectionActive)
}

// The receipt actions of agent commands, and their object kind.
const (
	ActionConnected       Action = "connected"
	ActionAutonomyChanged Action = "autonomy_changed"
	ActionPaused          Action = "paused"
	ActionResumed         Action = "resumed"
	ActionDisconnected    Action = "disconnected"

	ObjectAgent = "agent"
	// SourceAutonomy is the ReceiptSource kind on connected and
	// autonomy_changed receipts: its ref is the level the agent now has in
	// the receipt's space.
	SourceAutonomy = "autonomy"
)

// Connection is the projection of an agent connection. Field names
// follow the /v2 contract (AgentConnection).
type Connection struct {
	ID          uuid.UUID            `json:"id"`
	PersonID    uuid.UUID            `json:"person_id"`
	Agent       AgentKind            `json:"agent"`
	DisplayName string               `json:"display_name"`
	Surface     AgentSurface         `json:"surface"`
	Credential  ConnectionCredential `json:"credential"`
	// MaxAutonomy is the most this credential allows: an API key proposes
	// at most, and a credential without write access only reads.
	MaxAutonomy policy.Autonomy `json:"max_autonomy"`
	ClientID    string          `json:"client_id,omitempty"`
	State       ConnectionState `json:"state"`
	// Spaces are the spaces in the reader's scope it is connected to.
	Spaces []ConnectionSpace `json:"spaces"`
	// ReadsWeek and WritesWeek count the last 7 days in those spaces:
	// reads (R-, one per space read) and writes.
	ReadsWeek  int        `json:"reads_7d"`
	WritesWeek int        `json:"writes_7d"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	// ConnectedBy is who connected it, when the reader can see that
	// receipt: a person's id, or empty with ConnectedByKind memax for the
	// V1 backfill.
	ConnectedByKind  policy.ActorKind `json:"connected_by_kind,omitempty"`
	ConnectedBy      *uuid.UUID       `json:"connected_by,omitempty"`
	DisconnectedAt   *time.Time       `json:"disconnected_at,omitempty"`
	CreatedReceiptID uuid.UUID        `json:"created_receipt_id"`
	LastReceiptID    uuid.UUID        `json:"last_receipt_id"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`

	streamVersion int
}

// ConnectionCredential is the credential a connection is bound to.
type ConnectionCredential struct {
	Kind CredentialKind `json:"kind"`
	ID   uuid.UUID      `json:"id"`
	// Active is false once the credential is revoked, expired or gone.
	Active bool `json:"active"`
}

// ConnectionSpace is a connection's autonomy in one space.
type ConnectionSpace struct {
	SpaceID    uuid.UUID        `json:"space_id"`
	Slug       string           `json:"slug"`
	Name       string           `json:"name"`
	Kind       policy.SpaceKind `json:"kind"`
	Autonomy   policy.Autonomy  `json:"autonomy"`
	ReadsWeek  int              `json:"reads_7d"`
	WritesWeek int              `json:"writes_7d"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// AutonomyIn returns the connection's level in a space, if it is
// connected there.
func (c *Connection) AutonomyIn(spaceID uuid.UUID) (policy.Autonomy, bool) {
	for _, s := range c.Spaces {
		if s.SpaceID == spaceID {
			return s.Autonomy, true
		}
	}
	return "", false
}

// WithConnection sets each space's agent autonomy and status from the
// agent's connection, for an agent's scope: its level in the space capped
// by limit (what the credential allows), and not connected wherever it
// has no level. Without a connection, or once it is disconnected, the
// agent isn't connected anywhere; while it is paused, it is paused
// everywhere. Either way it only reads (policy.Actor.AgentStatus).
func (s Scope) WithConnection(c *Connection, limit policy.Autonomy) Scope {
	out := Scope{PersonID: s.PersonID, Spaces: make([]SpaceGrant, len(s.Spaces))}
	for i, g := range s.Spaces {
		g.Autonomy, g.AgentStatus = policy.AutonomyRead, policy.AgentNotConnected
		switch {
		case c == nil:
		case c.State == ConnectionPaused:
			g.AgentStatus = policy.AgentPaused
		case c.State == ConnectionActive:
			if level, ok := c.AutonomyIn(g.SpaceID); ok {
				g.Autonomy, g.AgentStatus = policy.MinAutonomy(level, limit), policy.AgentConnected
			}
		}
		out.Spaces[i] = g
	}
	return out
}

// The agent commands.
const (
	CommandConnectAgent    CommandName = "connect_agent"
	CommandSetAutonomy     CommandName = "set_autonomy"
	CommandPauseAgent      CommandName = "pause_agent"
	CommandResumeAgent     CommandName = "resume_agent"
	CommandDisconnectAgent CommandName = "disconnect_agent"
)

func isAgentCommand(n CommandName) bool {
	switch n {
	case CommandConnectAgent, CommandSetAutonomy, CommandPauseAgent, CommandResumeAgent, CommandDisconnectAgent:
		return true
	}
	return false
}

// SpaceAutonomy is one space a connection is made in, at a level; an
// empty Autonomy means the space's default for new agents.
type SpaceAutonomy struct {
	SpaceID  uuid.UUID
	Autonomy policy.Autonomy
}

// ConnectAgent binds a credential to a new agent connection, connected to
// the given spaces. The credential must be the person's and active. A
// person connects their own agents; Memax connects V1 credentials in the
// backfill (BackfillConnections). The Scope's PersonID must be the person.
type ConnectAgent struct {
	Meta
	// Person is who the agent works for. It defaults to the actor, when
	// the actor is a person, and must be that person.
	Person       uuid.UUID
	Credential   CredentialKind
	CredentialID uuid.UUID
	Agent        AgentKind
	// DisplayName defaults to the agent's name; Surface to where the agent
	// usually runs.
	DisplayName string
	Surface     AgentSurface
	// ClientID is the OAuth client's CIMD client_id URL, when it has one.
	ClientID string
	Spaces   []SpaceAutonomy
	// Cap, when set, bounds every space's level: the backfill passes
	// Propose, or Read for a credential that can't write.
	Cap policy.Autonomy
}

// Name implements Command.
func (*ConnectAgent) Name() CommandName { return CommandConnectAgent }

// SetAutonomy sets what a connected agent may do in one space,
// connecting it there if it isn't yet.
type SetAutonomy struct {
	Meta
	Connection uuid.UUID
	SpaceID    uuid.UUID
	Autonomy   policy.Autonomy
}

// Name implements Command.
func (*SetAutonomy) Name() CommandName { return CommandSetAutonomy }

// PauseAgent stops an agent writing anywhere until it is resumed. It can
// still read.
type PauseAgent struct {
	Meta
	Connection uuid.UUID
}

// Name implements Command.
func (*PauseAgent) Name() CommandName { return CommandPauseAgent }

// ResumeAgent undoes PauseAgent.
type ResumeAgent struct {
	Meta
	Connection uuid.UUID
}

// Name implements Command.
func (*ResumeAgent) Name() CommandName { return CommandResumeAgent }

// DisconnectAgent ends a connection for good and revokes its credential
// in the same transaction, so the agent stops working at once.
type DisconnectAgent struct {
	Meta
	Connection uuid.UUID
}

// Name implements Command.
func (*DisconnectAgent) Name() CommandName { return CommandDisconnectAgent }

// MaxDisplayName bounds a connection's display name.
const MaxDisplayName = 100

func (c *ConnectAgent) validate() error {
	if !c.Credential.Valid() {
		return invalid("credential", "use api_key or oauth_grant")
	}
	if c.CredentialID == uuid.Nil {
		return invalid("credential_id", "say which credential to connect")
	}
	if !c.Agent.Valid() {
		return invalid("agent", "use one of %s", strings.Join(strs(AgentKinds), ", "))
	}
	c.DisplayName = strings.TrimSpace(c.DisplayName)
	if c.DisplayName == "" {
		c.DisplayName = c.Agent.Name()
	}
	if err := checkText("display_name", c.DisplayName, MaxDisplayName, true); err != nil {
		return err
	}
	if c.Surface == "" {
		c.Surface = c.Agent.Surface()
	}
	if !c.Surface.Valid() {
		return invalid("surface", "use cli, ide, cloud or chat")
	}
	if c.ClientID != "" && (!strings.HasPrefix(c.ClientID, "https://") || len(c.ClientID) > MaxSourceURIRunes) {
		return invalid("client_id", "must be an https URL (a Client ID Metadata Document)")
	}
	if c.Cap != "" && !c.Cap.Valid() {
		return invalid("cap", "use read, propose or write")
	}
	if len(c.Spaces) == 0 {
		return invalid("spaces", "connect the agent to at least one space")
	}
	seen := map[uuid.UUID]bool{}
	for _, s := range c.Spaces {
		if s.SpaceID == uuid.Nil || seen[s.SpaceID] {
			return invalid("spaces", "list each space once, by id")
		}
		seen[s.SpaceID] = true
		if s.Autonomy != "" && !s.Autonomy.Valid() {
			return invalid("autonomy", "use read, propose or write")
		}
	}
	slices.SortFunc(c.Spaces, func(a, b SpaceAutonomy) int { return strings.Compare(a.SpaceID.String(), b.SpaceID.String()) })
	return nil
}

func validateConnectionTarget(id uuid.UUID) error {
	if id == uuid.Nil {
		return invalid("agent", "say which agent connection, by id")
	}
	return nil
}

func strs[T ~string](vs []T) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = string(v)
	}
	return out
}

// ConnectionStateError is a command the connection's state doesn't allow:
// pausing a paused agent, anything on a disconnected one. It matches
// ErrInvalidTransition.
type ConnectionStateError struct {
	Agent   string
	State   ConnectionState
	Command CommandName
}

func (e *ConnectionStateError) Error() string {
	switch {
	case e.State == ConnectionDisconnected:
		return fmt.Sprintf("%s was disconnected. Connect it again to use it.", e.Agent)
	case e.Command == CommandPauseAgent:
		return fmt.Sprintf("%s is already paused.", e.Agent)
	case e.Command == CommandResumeAgent:
		return fmt.Sprintf("%s isn't paused.", e.Agent)
	}
	return fmt.Sprintf("%s is %s, which doesn't allow that.", e.Agent, e.State)
}

// Is makes errors.Is(err, ErrInvalidTransition) match.
func (e *ConnectionStateError) Is(target error) bool { return target == ErrInvalidTransition }

package policy

import "fmt"

// ConnectionAction is a change to an agent connection (plan 25 §5.15,
// epic 1.8).
type ConnectionAction string

// The connection actions.
const (
	// ConnectionConnect connects an agent to a space: a new connection, or
	// an existing one in a space it isn't connected to yet.
	ConnectionConnect     ConnectionAction = "connect"
	ConnectionSetAutonomy ConnectionAction = "set_autonomy"
	ConnectionPause       ConnectionAction = "pause"
	ConnectionResume      ConnectionAction = "resume"
	ConnectionDisconnect  ConnectionAction = "disconnect"
)

// Connection is the connection a change applies to, as DecideConnection
// needs it.
type Connection struct {
	// Name is the agent's display name, for messages ("Codex").
	Name string
	// Mine is set when the actor is the person the agent works for.
	Mine bool
	// Credential is the connection's credential. An API key's agent
	// proposes at most.
	Credential Credential
	// From is the agent's autonomy in the space now ("" when it isn't
	// connected there), and To the level asked for (Connect and
	// SetAutonomy).
	From, To Autonomy
}

// AgentCeiling is the most a person with role r may let their agent do in
// a space with these rules. Agents act with the role of the person they
// work for: Write keeps directly, so only someone who can keep there may
// grant it; anyone else's agent proposes at most.
func AgentCeiling(r Role, rules Rules) Autonomy {
	switch {
	case canKeep(r, rules):
		return AutonomyWrite
	case r == RoleMember || r == RoleViewer:
		return AutonomyPropose
	}
	return AutonomyRead
}

// DefaultAutonomy is the level an agent starts at when it is connected to
// a space: the space's rule for new agents (Propose unless it says
// otherwise), capped by the person's ceiling.
func DefaultAutonomy(r Role, s Space) Autonomy {
	return MinAutonomy(s.Rules.AgentAutonomy(), AgentCeiling(r, s.Rules))
}

// quietLevel is the most an agent may be connected at without a person
// on the web: the space's default, and never above Propose. That is what
// `memax connect` does from a terminal, and what the V1 backfill does.
func quietLevel(r Role, s Space) Autonomy {
	return MinAutonomy(AutonomyPropose, DefaultAutonomy(r, s))
}

// DecideConnection rules on a change to an agent connection. The rules:
//
//   - Only a person, signed in, changes a connection. An agent can never
//     change one, its own included, and neither can an API key. Memax may
//     connect an agent (the V1 backfill) at no more than quietLevel.
//   - Pause, resume and disconnect are for the person the agent works for.
//   - Lowering what your own agent may do is always allowed. A space owner
//     may lower anyone's agent in that space, but never raise it.
//   - Raising is capped by AgentCeiling, and an API key's agent never goes
//     above Propose. Viewers can't raise an agent at all (they may still
//     connect theirs at Propose, which is what they can do themselves).
//   - Raising needs a person on the web (assurance human_web), so an agent
//     driving the CLI with the person's login can't raise itself, and with
//     their passkey when they have one (human_web_verified), so one holding
//     the browser's cookies can't either. The one exception is connecting at
//     no more than quietLevel. Resume counts as raising.
//
// Decide's rules for writes apply on top: an agent at Write still
// proposes what cites an external source.
func DecideConnection(a Actor, act ConnectionAction, c Connection, s Space) Decision {
	if !a.Kind.Valid() {
		return refuse(CodeUnknownActor, "Memax doesn't recognise who is making this change. Sign in again.")
	}
	name := c.Name
	if name == "" {
		name = "this agent"
	}
	if a.Kind == ActorMemax && act == ConnectionConnect && !c.To.Above(quietLevel(a.Role, s)) {
		return apply()
	}
	if a.Kind != ActorPerson || a.Credential != CredentialSession {
		return refuse(CodePersonMustManage, "Only a person can change what an agent may do. Change it in Agents.")
	}

	switch act {
	case ConnectionPause, ConnectionDisconnect, ConnectionResume:
		if !c.Mine {
			return refuse(CodeNotYourAgent, fmt.Sprintf("Only the person %s works for can %s it.", name, act))
		}
		if act == ConnectionResume {
			if d := needsPerson(a, CodeAutonomyNeedsWeb, fmt.Sprintf(
				"Resuming %s needs you on the web, so an agent can't resume itself. Resume it in Agents at memax.app.", name),
				"resuming "+name); d != nil {
				return *d
			}
			return applyChecked(a)
		}
		return apply()
	case ConnectionConnect, ConnectionSetAutonomy:
	default:
		return refuse(CodeUnknownAction, fmt.Sprintf("Memax doesn't know how to %q an agent.", act))
	}

	if a.Role != RoleOwner && a.Role != RoleMember && a.Role != RoleViewer {
		return refuse(CodeNotMember, fmt.Sprintf("Only members of %s can connect agents to it.", spaceName(s)))
	}
	if !c.To.Valid() {
		return refuse(CodeAutonomyNotAllowed, "Choose read, propose or write.")
	}
	raise := c.To.Above(c.From)
	if !c.Mine {
		if act == ConnectionSetAutonomy && a.Role == RoleOwner && !raise {
			return apply()
		}
		return refuse(CodeNotYourAgent, fmt.Sprintf(
			"Only the person %s works for can raise what it may do. Owners of %s can lower it.", name, spaceName(s)))
	}
	if !raise {
		return apply()
	}
	if act == ConnectionSetAutonomy && a.Role == RoleViewer {
		return refuse(CodeAutonomyNotAllowed, fmt.Sprintf(
			"Viewers can't raise what agents may do in %s. Ask a member.", spaceName(s)))
	}
	if c.Credential == CredentialAPIKey && c.To == AutonomyWrite {
		return refuse(CodeKeyMaxPropose, fmt.Sprintf(
			"API keys can propose but never keep, so %s can't write. Connect it over OAuth to let it write.", name))
	}
	if c.To.Above(AgentCeiling(a.Role, s.Rules)) {
		return refuse(CodeAutonomyNotAllowed, fmt.Sprintf(
			"Only people who keep in %s can let an agent write there, so %s can propose at most.", spaceName(s), name))
	}
	quiet := act == ConnectionConnect && !c.To.Above(quietLevel(a.Role, s))
	if quiet {
		return apply()
	}
	if d := needsPerson(a, CodeAutonomyNeedsWeb, fmt.Sprintf(
		"Raising what %s may do needs you on the web, so an agent can't raise itself. Change it in Agents at memax.app.", name),
		"raising what "+name+" may do"); d != nil {
		return *d
	}
	return applyChecked(a)
}

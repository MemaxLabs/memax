package policy

import (
	"strings"
	"testing"
)

func TestDecideConnection(t *testing.T) {
	t.Parallel()
	web := func(r Role) Actor { return person(r, ViaWeb) }
	cli := func(r Role) Actor { return person(r, ViaCLI) }
	memax := Actor{Kind: ActorMemax, Role: RoleOwner, Via: ViaSystem}
	key := with(person(RoleOwner, ViaAPI), func(a *Actor) { a.Credential = CredentialAPIKey })
	defaultWrite := Space{Name: "memax-v2", Kind: SpaceProject, Rules: Rules{NewAgentAutonomy: AutonomyWrite}}
	defaultRead := Space{Name: "memax-v2", Kind: SpaceProject, Rules: Rules{NewAgentAutonomy: AutonomyRead}}
	mine := func(from, to Autonomy) Connection {
		return Connection{Name: "Codex", Mine: true, Credential: CredentialOAuth, From: from, To: to}
	}
	theirs := func(from, to Autonomy) Connection {
		return Connection{Name: "Codex", Credential: CredentialOAuth, From: from, To: to}
	}
	keyConn := func(from, to Autonomy) Connection {
		return Connection{Name: "CI", Mine: true, Credential: CredentialAPIKey, From: from, To: to}
	}

	cases := []struct {
		name    string
		actor   Actor
		action  ConnectionAction
		conn    Connection
		space   Space
		code    string // "" = applied
		message string
	}{
		// Who may change a connection at all.
		{"an agent can't raise itself", agent(RoleOwner, AutonomyPropose), ConnectionSetAutonomy, mine(AutonomyPropose, AutonomyWrite), project, CodePersonMustManage, "Only a person"},
		{"an agent can't even lower itself", agent(RoleOwner, AutonomyWrite), ConnectionSetAutonomy, mine(AutonomyWrite, AutonomyRead), project, CodePersonMustManage, ""},
		{"an agent can't resume itself", agent(RoleOwner, AutonomyPropose), ConnectionResume, mine("", ""), project, CodePersonMustManage, ""},
		{"an API key can't manage agents", key, ConnectionPause, mine("", ""), project, CodePersonMustManage, ""},
		{"Dream can't connect agents", Actor{Kind: ActorDream, Role: RoleOwner}, ConnectionConnect, mine("", AutonomyPropose), project, CodePersonMustManage, ""},
		{"unknown actor", Actor{Kind: "robot"}, ConnectionPause, mine("", ""), project, CodeUnknownActor, ""},

		// Memax's backfill connects V1 credentials quietly, never above Propose.
		{"backfill connects at propose", memax, ConnectionConnect, mine("", AutonomyPropose), project, "", ""},
		{"backfill can't connect at write", memax, ConnectionConnect, mine("", AutonomyWrite), defaultWrite, CodePersonMustManage, ""},
		{"backfill respects a read default", memax, ConnectionConnect, mine("", AutonomyPropose), defaultRead, CodePersonMustManage, ""},
		{"backfill can't set autonomy", memax, ConnectionSetAutonomy, mine(AutonomyPropose, AutonomyRead), project, CodePersonMustManage, ""},

		// Pause, resume, disconnect.
		{"pause your agent from the CLI", cli(RoleOwner), ConnectionPause, mine("", ""), project, "", ""},
		{"disconnect your agent from the CLI", cli(RoleViewer), ConnectionDisconnect, mine("", ""), project, "", ""},
		{"resume needs the web", cli(RoleOwner), ConnectionResume, mine("", ""), project, CodeAutonomyNeedsWeb, "on the web"},
		{"resume on the web", web(RoleOwner), ConnectionResume, mine("", ""), project, "", ""},
		{"pause someone else's agent", web(RoleOwner), ConnectionPause, theirs("", ""), project, CodeNotYourAgent, "Only the person Codex works for can pause it."},
		{"disconnect someone else's agent", web(RoleOwner), ConnectionDisconnect, theirs("", ""), project, CodeNotYourAgent, ""},

		// Connecting.
		{"connect from the CLI at the default", cli(RoleOwner), ConnectionConnect, mine("", AutonomyPropose), project, "", ""},
		{"connect from the CLI at read", cli(RoleMember), ConnectionConnect, mine("", AutonomyRead), project, "", ""},
		{"connect from the CLI at write needs the web", cli(RoleOwner), ConnectionConnect, mine("", AutonomyWrite), defaultWrite, CodeAutonomyNeedsWeb, ""},
		{"connect from the CLI above a read default needs the web", cli(RoleOwner), ConnectionConnect, mine("", AutonomyPropose), defaultRead, CodeAutonomyNeedsWeb, ""},
		{"connect on the web at write", web(RoleOwner), ConnectionConnect, mine("", AutonomyWrite), project, "", ""},
		{"a viewer connects at propose", cli(RoleViewer), ConnectionConnect, mine("", AutonomyPropose), project, "", ""},
		{"a viewer's agent can't write", web(RoleViewer), ConnectionConnect, mine("", AutonomyWrite), project, CodeAutonomyNotAllowed, "propose at most"},
		{"owners keep: a member's agent can't write", web(RoleMember), ConnectionConnect, mine("", AutonomyWrite), ownersKeep, CodeAutonomyNotAllowed, ""},
		{"an API key can't write", web(RoleOwner), ConnectionConnect, keyConn("", AutonomyWrite), project, CodeKeyMaxPropose, "API keys can propose"},
		{"connect someone else's credential", web(RoleOwner), ConnectionConnect, theirs("", AutonomyRead), project, CodeNotYourAgent, ""},
		{"connect where you aren't a member", web(RoleNone), ConnectionConnect, mine("", AutonomyRead), project, CodeNotMember, ""},

		// Setting autonomy.
		{"raise on the web", web(RoleOwner), ConnectionSetAutonomy, mine(AutonomyPropose, AutonomyWrite), project, "", ""},
		{"a member raises where members keep", web(RoleMember), ConnectionSetAutonomy, mine(AutonomyPropose, AutonomyWrite), project, "", ""},
		{"raise from the CLI needs the web", cli(RoleOwner), ConnectionSetAutonomy, mine(AutonomyPropose, AutonomyWrite), project, CodeAutonomyNeedsWeb, "can't raise itself"},
		{"read to propose from the CLI needs the web too", cli(RoleOwner), ConnectionSetAutonomy, mine(AutonomyRead, AutonomyPropose), project, CodeAutonomyNeedsWeb, ""},
		{"lower from the CLI", cli(RoleOwner), ConnectionSetAutonomy, mine(AutonomyWrite, AutonomyRead), project, "", ""},
		{"a viewer lowers their own agent", cli(RoleViewer), ConnectionSetAutonomy, mine(AutonomyPropose, AutonomyRead), project, "", ""},
		{"a viewer can't raise", web(RoleViewer), ConnectionSetAutonomy, mine(AutonomyRead, AutonomyPropose), project, CodeAutonomyNotAllowed, "Viewers"},
		{"the same level is fine", cli(RoleOwner), ConnectionSetAutonomy, mine(AutonomyPropose, AutonomyPropose), project, "", ""},
		{"an API key's agent stays below write", web(RoleOwner), ConnectionSetAutonomy, keyConn(AutonomyPropose, AutonomyWrite), project, CodeKeyMaxPropose, ""},
		{"owners keep: a member can't raise to write", web(RoleMember), ConnectionSetAutonomy, mine(AutonomyPropose, AutonomyWrite), ownersKeep, CodeAutonomyNotAllowed, "propose at most"},
		{"a space owner lowers someone else's agent", cli(RoleOwner), ConnectionSetAutonomy, theirs(AutonomyWrite, AutonomyRead), project, "", ""},
		{"a space owner can't raise someone else's agent", web(RoleOwner), ConnectionSetAutonomy, theirs(AutonomyRead, AutonomyWrite), project, CodeNotYourAgent, "Owners of memax-v2 can lower it"},
		{"a member can't lower someone else's agent", web(RoleMember), ConnectionSetAutonomy, theirs(AutonomyWrite, AutonomyRead), project, CodeNotYourAgent, ""},
		{"an unknown level", web(RoleOwner), ConnectionSetAutonomy, mine(AutonomyRead, "admin"), project, CodeAutonomyNotAllowed, ""},
		{"an unknown action", web(RoleOwner), "promote", mine("", ""), project, CodeUnknownAction, ""},
	}
	for _, c := range cases {
		d := DecideConnection(c.actor, c.action, c.conn, c.space)
		if c.code == "" {
			if d.Effect != EffectApply {
				t.Errorf("%s: %s %s (%s), want apply", c.name, d.Effect, d.Code, d.Message)
			}
			continue
		}
		if d.Effect != EffectRefuse || d.Code != c.code {
			t.Errorf("%s: %s %s, want refuse %s", c.name, d.Effect, d.Code, c.code)
		}
		if c.message != "" && !strings.Contains(d.Message, c.message) {
			t.Errorf("%s: message %q, want it to contain %q", c.name, d.Message, c.message)
		}
		if d.Message == "" || strings.Contains(d.Message, "!") {
			t.Errorf("%s: message %q is empty or shouts", c.name, d.Message)
		}
	}
}

func TestAgentCeilingAndDefault(t *testing.T) {
	t.Parallel()
	cases := []struct {
		role         Role
		space        Space
		ceiling, def Autonomy
	}{
		{RoleOwner, project, AutonomyWrite, AutonomyPropose},
		{RoleMember, project, AutonomyWrite, AutonomyPropose},
		{RoleMember, ownersKeep, AutonomyPropose, AutonomyPropose},
		{RoleViewer, project, AutonomyPropose, AutonomyPropose},
		{RoleNone, project, AutonomyRead, AutonomyRead},
		{RoleOwner, Space{Rules: Rules{NewAgentAutonomy: AutonomyWrite}}, AutonomyWrite, AutonomyWrite},
		{RoleViewer, Space{Rules: Rules{NewAgentAutonomy: AutonomyWrite}}, AutonomyPropose, AutonomyPropose},
		{RoleOwner, Space{Rules: Rules{NewAgentAutonomy: AutonomyRead}}, AutonomyWrite, AutonomyRead},
		{RoleOwner, Space{Rules: Rules{NewAgentAutonomy: "admin"}}, AutonomyWrite, AutonomyPropose},
	}
	for _, c := range cases {
		if got := AgentCeiling(c.role, c.space.Rules); got != c.ceiling {
			t.Errorf("ceiling(%q, %+v) = %s, want %s", c.role, c.space.Rules, got, c.ceiling)
		}
		if got := DefaultAutonomy(c.role, c.space); got != c.def {
			t.Errorf("default(%q, %+v) = %s, want %s", c.role, c.space.Rules, got, c.def)
		}
	}
}

func TestMinAutonomy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   []Autonomy
		want Autonomy
	}{
		{nil, AutonomyRead},
		{[]Autonomy{AutonomyWrite}, AutonomyWrite},
		{[]Autonomy{AutonomyWrite, AutonomyPropose}, AutonomyPropose},
		{[]Autonomy{AutonomyPropose, AutonomyRead, AutonomyWrite}, AutonomyRead},
		{[]Autonomy{AutonomyWrite, "root"}, AutonomyRead},
		{[]Autonomy{""}, AutonomyRead},
	}
	for _, c := range cases {
		if got := MinAutonomy(c.in...); got != c.want {
			t.Errorf("MinAutonomy(%v) = %s, want %s", c.in, got, c.want)
		}
	}
	if !AutonomyWrite.Above(AutonomyPropose) || !AutonomyRead.Above("") || AutonomyPropose.Above(AutonomyPropose) {
		t.Error("Above orders read < propose < write, with unknown below read")
	}
}

// An agent that isn't connected, or is paused, only reads, whatever its
// autonomy says, and the refusal says how to fix it.
func TestAgentStatusMakesItReadOnly(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		status  AgentStatus
		code    string
		message string
	}{
		{AgentPaused, CodeAgentPaused, "Codex is paused, so it can only read. Resume it in Agents."},
		{AgentNotConnected, CodeAgentNotConnected, "Codex isn't connected to memax-v2, so it can only read. Connect it in Agents."},
		{"gone", CodeAgentNotConnected, "isn't connected"},
	} {
		a := with(agent(RoleOwner, AutonomyWrite), func(a *Actor) { a.AgentStatus = c.status })
		for _, act := range []Action{ActionRemember, ActionPropose, ActionEdit, ActionForget} {
			d := Decide(a, act, kept, project)
			if d.Effect != EffectRefuse || d.Code != c.code || !strings.Contains(d.Message, c.message) {
				t.Errorf("%s agent %s: %s %s %q", c.status, act, d.Effect, d.Code, d.Message)
			}
		}
		// An API key's agent says the same.
		key := with(a, func(a *Actor) { a.Credential = CredentialAPIKey })
		if d := Decide(key, ActionPropose, newFact, project); d.Code != c.code {
			t.Errorf("%s key agent: %s", c.status, d.Code)
		}
	}
	// A connected agent is unaffected.
	if d := Decide(agent(RoleOwner, AutonomyWrite), ActionPropose, newFact, project); d.Effect != EffectApply {
		t.Errorf("connected agent at write: %s %s", d.Effect, d.Code)
	}
	// People have no agent status.
	p := with(person(RoleOwner, ViaWeb), func(a *Actor) { a.AgentStatus = AgentPaused })
	if d := Decide(p, ActionRemember, newFact, project); d.Effect != EffectApply {
		t.Errorf("a person with a stray agent status: %s %s", d.Effect, d.Code)
	}
}

package policy

import "testing"

// Who may link a machine, change what it holds, report from it, or take
// the personal space off it: its person, in their personal space, on a
// session, from any surface.
func TestDecideMachine(t *testing.T) {
	owner := person(RoleOwner, ViaCLI)
	acts := []MachineAction{MachineList, MachineLink, MachineConfigure, MachineUnlink, MachineReport}
	cases := []struct {
		name  string
		actor Actor
		space Space
		want  string // "" applies
	}{
		{"the owner on the CLI", owner, personal, ""},
		{"the owner on the web", person(RoleOwner, ViaWeb), personal, ""},
		{"an agent at Write", agent(RoleOwner, AutonomyWrite), personal, CodeMachinesByPerson},
		{"an API key", with(owner, func(a *Actor) { a.Credential = CredentialAPIKey }), personal, CodeMachinesByPerson},
		{"an OAuth grant", with(owner, func(a *Actor) { a.Credential = CredentialOAuth }), personal, CodeMachinesByPerson},
		{"Memax", Actor{Kind: ActorMemax, Name: "Memax"}, personal, CodeMachinesByPerson},
		{"a project space", owner, project, CodeMachinesPersonal},
		{"a team space", owner, team, CodeMachinesPersonal},
		{"a member, not the owner", person(RoleMember, ViaWeb), personal, CodeMachinesByOwner},
	}
	for _, c := range cases {
		for _, act := range acts {
			d := DecideMachine(c.actor, act, c.space)
			if c.want == "" && d.Effect != EffectApply {
				t.Errorf("%s, %s: %s %s, want apply", c.name, act, d.Effect, d.Code)
			}
			if c.want != "" && (d.Effect != EffectRefuse || d.Code != c.want || d.Message == "") {
				t.Errorf("%s, %s: %s %s, want refused %s", c.name, act, d.Effect, d.Code, c.want)
			}
		}
	}
	if d := DecideMachine(owner, "teleport", personal); d.Code != CodeUnknownAction {
		t.Errorf("an unknown action: %s", d.Code)
	}
}

// A personal space still on V1 compiles nothing, so its owner can't link a
// machine or change one's files there, and is told to switch first; taking
// the layer off, reading and reporting still work. Its switch's own steps
// pass.
func TestDecideMachineOnV1(t *testing.T) {
	owner := person(RoleOwner, ViaCLI)
	onV1 := Space{Name: "Personal", Kind: SpacePersonal}
	for act, refused := range map[MachineAction]bool{
		MachineLink: true, MachineConfigure: true, MachineList: false, MachineUnlink: false, MachineReport: false,
	} {
		d := DecideMachine(owner, act, onV1)
		switch {
		case refused && (d.Effect != EffectRefuse || d.Code != CodeSpaceOnV1):
			t.Errorf("%s on V1: %s %s, want refused %s", act, d.Effect, d.Code, CodeSpaceOnV1)
		case refused && d.Message != "Personal is still on V1, so its V2 record can't change yet. "+
			"Switch it to V2 first, from its Today on memax.app or with memax switch.":
			t.Errorf("%s on V1 says %q", act, d.Message)
		case !refused && d.Effect != EffectApply:
			t.Errorf("%s on V1: %s %s, want apply", act, d.Effect, d.Code)
		}
		switching := onV1
		switching.Switching = true
		if d := DecideMachine(owner, act, switching); d.Effect != EffectApply {
			t.Errorf("%s in a switch step: %s %s", act, d.Effect, d.Code)
		}
	}
	// Who may act comes first: an agent hears it can't, not that the space is on V1.
	if d := DecideMachine(agent(RoleOwner, AutonomyWrite), MachineLink, onV1); d.Code != CodeMachinesByPerson {
		t.Errorf("an agent on V1: %s", d.Code)
	}
}

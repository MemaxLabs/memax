package policy

import (
	"strings"
	"testing"
)

// everyAction is every action Decide knows. One added later is refused on
// V1 until onV1 lists it (TestSpaceOnV1 fails here until it is classified
// either way).
var everyAction = []Action{
	ActionRemember, ActionPropose, ActionKeep, ActionEdit, ActionReject, ActionForget,
	ActionForgetSpace, ActionRequestForget, ActionDeclineForget, ActionReapplyForget,
	ActionReviseBrief, ActionConfigureTarget, ActionStopTarget, ActionRequestCompile, ActionRecordCompile, ActionReport, ActionPullDrift,
	ActionJudge, ActionResolveConflict, ActionUndo,
	ActionRequestDecision, ActionAnswerGate, ActionWithdrawGate,
	ActionRead, ActionAsk,
	ActionPublishEdition, ActionUndoDream, ActionRestore, ActionRunDream,
	ActionExport, ActionScope, ActionVerify, ActionReportChecks, ActionFlagStale,
}

// passOnV1 is what a space still on V1 lets through, and why. It repeats
// onV1 on purpose: letting another action through is a decision, made in
// both places.
var passOnV1 = map[Action]string{
	ActionRead:          "reads change nothing",
	ActionForget:        "rule 7: words go wherever they are, a switched-back record's included",
	ActionForgetSpace:   "V1's own space delete and account wipe go through it (handler.V2Forgetter)",
	ActionReapplyForget: "the forget ledger is re-applied after a restore, whichever record serves the space",
	ActionExport:        "rule 14: a person's copy of the record as it stands",
	ActionRecordCompile: "Memax finishes compiles queued on V2 or by the switch",
	ActionJudge:         "Memax finishes verdicts queued on V2 or by the switch's import",
	ActionReport:        "a device reports what it did with files already compiled",
	ActionStopTarget:    "stopping a file takes Memax off it and writes nothing, like unlinking a machine",
}

// TestSpaceOnV1 holds the rule for a space still on V1: every action is
// refused with CodeSpaceOnV1 except those passOnV1 lists, which decide
// exactly as they would on V2, and the switch's own steps decide as on V2
// whatever they are.
func TestSpaceOnV1(t *testing.T) {
	t.Parallel()
	for act := range onV1 {
		if _, ok := passOnV1[act]; !ok {
			t.Errorf("onV1 lets %s through, but passOnV1 doesn't say why", act)
		}
	}
	for act := range passOnV1 {
		if !onV1[act] {
			t.Errorf("passOnV1 lists %s, but onV1 refuses it", act)
		}
	}
	actors := map[string]Actor{
		"owner on the web":    person(RoleOwner, ViaWeb),
		"owner from the CLI":  person(RoleOwner, ViaCLI),
		"member on the web":   person(RoleMember, ViaWeb),
		"viewer":              person(RoleViewer, ViaWeb),
		"agent at write":      agent(RoleOwner, AutonomyWrite),
		"agent at propose":    agent(RoleMember, AutonomyPropose),
		"api key":             with(person(RoleOwner, ViaAPI), func(a *Actor) { a.Credential, a.Autonomy = CredentialAPIKey, AutonomyPropose }),
		"memax":               {Kind: ActorMemax, Via: ViaSystem},
		"dream":               {Kind: ActorDream, Via: ViaSystem},
		"repository":          {Kind: ActorRepository, Via: ViaCLI},
		"owner with passkey":  with(person(RoleOwner, ViaWeb), func(a *Actor) { a.Passkey, a.Verified = true, true }),
		"member who forgets":  with(person(RoleMember, ViaWeb), func(a *Actor) { a.CanForget = true }),
		"paused agent":        with(agent(RoleOwner, AutonomyWrite), func(a *Actor) { a.AgentStatus = AgentPaused }),
		"unconnected agent":   with(agent(RoleOwner, AutonomyWrite), func(a *Actor) { a.AgentStatus = AgentNotConnected }),
		"agent asking a gate": with(agent(RoleOwner, AutonomyPropose), func(a *Actor) { a.PersonPresent, a.CanElicit = true, true }),
	}
	objects := []Object{newFact, newDecision, kept, proposal, propExt, keptDec, gate, mineGate,
		{UndoOwn: true}, {UndoSystem: true}, {Secrets: []string{"AWS access key"}}}
	pairs := []struct{ v1, v2 Space }{
		{v1Project, Space{Name: v1Project.Name, Kind: v1Project.Kind, OnV2: true}},
		{v1Team, Space{Name: v1Team.Name, Kind: v1Team.Kind, OnV2: true}},
		{Space{Name: "acme-web", Kind: SpaceProject, Rules: Rules{Keep: WhoOwners, Forget: WhoMembers}},
			Space{Name: "acme-web", Kind: SpaceProject, Rules: Rules{Keep: WhoOwners, Forget: WhoMembers}, OnV2: true}},
	}
	for name, a := range actors {
		for _, act := range everyAction {
			for _, o := range objects {
				for _, p := range pairs {
					onV2 := Decide(a, act, o, p.v2)
					got := Decide(a, act, o, p.v1)
					switching := p.v1
					switching.Switching = true
					if s := Decide(a, act, o, switching); s != onV2 {
						t.Fatalf("%s %s %+v while switching %s: %+v, want what V2 decides: %+v", name, act, o, p.v1.Name, s, onV2)
					}
					if _, pass := passOnV1[act]; pass {
						if got != onV2 {
							t.Fatalf("%s %s %+v on V1 %s: %+v, want what V2 decides: %+v", name, act, o, p.v1.Name, got, onV2)
						}
						continue
					}
					if onV2.Code == CodeNotMember || onV2.Code == CodeUnknownActor {
						// Who isn't a member learns nothing about the space.
						if got != onV2 {
							t.Fatalf("%s %s on V1: %+v, want %+v", name, act, got, onV2)
						}
						continue
					}
					if got.Effect != EffectRefuse || got.Code != CodeSpaceOnV1 {
						t.Fatalf("%s %s %+v on V1 %s: %+v, want refused %s", name, act, o, p.v1.Name, got, CodeSpaceOnV1)
					}
				}
			}
		}
	}
}

// TestSpaceOnV1Says: the refusal names the space and tells the person what
// to do, in the product voice: its owner switches it, anyone else asks the
// owner, and Ask says why there is no answer.
func TestSpaceOnV1Says(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		actor  Actor
		action Action
		space  Space
		want   []string
	}{
		{"owner remembers", person(RoleOwner, ViaWeb), ActionRemember, v1Project,
			[]string{"acme-web is still on V1", "Switch it to V2 first", "memax switch"}},
		{"owner's agent proposes", agent(RoleOwner, AutonomyWrite), ActionPropose, v1Project,
			[]string{"acme-web is still on V1", "Switch it to V2 first"}},
		{"member keeps", person(RoleMember, ViaWeb), ActionKeep, v1Team,
			[]string{"acme-team is still on V1", "Ask its owner to switch it to V2."}},
		{"owner asks", person(RoleOwner, ViaWeb), ActionAsk, v1Project,
			[]string{"Ask answers from the V2 record, and acme-web is still on V1.", "Switch it to V2 first"}},
		{"viewer asks", person(RoleViewer, ViaWeb), ActionAsk, v1Project,
			[]string{"Ask answers from the V2 record", "Ask its owner"}},
		{"a space with no name", person(RoleOwner, ViaWeb), ActionReviseBrief, Space{Kind: SpaceProject},
			[]string{"This space is still on V1"}},
	}
	banned := []string{"!", " AI", "magic", "smart", "delete", "Delete"}
	for _, c := range cases {
		d := Decide(c.actor, c.action, Object{}, c.space)
		if d.Effect != EffectRefuse || d.Code != CodeSpaceOnV1 {
			t.Errorf("%s: %+v, want refused %s", c.name, d, CodeSpaceOnV1)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(d.Message, w) {
				t.Errorf("%s: %q doesn't say %q", c.name, d.Message, w)
			}
		}
		for _, b := range banned {
			if strings.Contains(d.Message, b) {
				t.Errorf("%s: %q breaks the voice rules (%s)", c.name, d.Message, b)
			}
		}
	}
}

// TestSpaceOnV1IsTheZeroValue: a Space that doesn't say it is on V2 is on
// V1, so a caller that forgets to read the fact refuses rather than writes.
func TestSpaceOnV1IsTheZeroValue(t *testing.T) {
	t.Parallel()
	if d := Decide(person(RoleOwner, ViaWeb), ActionRemember, newFact, Space{Name: "x", Kind: SpaceProject}); d.Code != CodeSpaceOnV1 {
		t.Errorf("zero OnV2: %+v, want %s", d, CodeSpaceOnV1)
	}
	if d := DecideConnection(person(RoleOwner, ViaWeb), ConnectionConnect,
		Connection{Name: "Codex", Mine: true, Credential: CredentialOAuth, To: AutonomyPropose}, v1Project); d.Effect != EffectApply {
		t.Errorf("connecting an agent to a space on V1: %+v, want applied (connections aren't the space's record)", d)
	}
}

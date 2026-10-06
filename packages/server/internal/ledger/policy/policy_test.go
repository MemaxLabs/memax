package policy

import (
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
)

var (
	project    = Space{Name: "memax-v2", Kind: SpaceProject}
	personal   = Space{Name: "Personal", Kind: SpacePersonal}
	team       = Space{Name: "memax-team", Kind: SpaceTeam}
	ownersKeep = Space{Name: "memax-v2", Kind: SpaceProject, Rules: Rules{Keep: WhoOwners}}
	membersFgt = Space{Name: "memax-v2", Kind: SpaceProject, Rules: Rules{Forget: WhoMembers}}
	teamNoWeb  = Space{Name: "memax-team", Kind: SpaceTeam, Rules: Rules{DecisionsNeedWeb: ptr(false)}}
)

func ptr[T any](v T) *T { return &v }

func person(role Role, via Via) Actor {
	return Actor{Kind: ActorPerson, Name: "Ziyang", Role: role, Via: via}
}

func agent(role Role, level Autonomy) Actor {
	return Actor{Kind: ActorAgent, Name: "Codex", Role: role, Autonomy: level, Via: ViaMCP}
}

func with(a Actor, f func(*Actor)) Actor { f(&a); return a }

var (
	newFact     = Object{}
	newDecision = Object{Decision: true}
	newExternal = Object{External: true}
	contradicts = Object{ContradictsDecision: true}
	proposal    = Object{Ref: "M-0219", Lifecycle: lifecycle.Proposed}
	kept        = Object{Ref: "M-0219", Lifecycle: lifecycle.Kept}
	personKept  = Object{Ref: "M-0219", Lifecycle: lifecycle.Kept, PersonKept: true}
	keptExt     = Object{Ref: "M-0219", Lifecycle: lifecycle.Kept, External: true}
	propExt     = Object{Ref: "M-0219", Lifecycle: lifecycle.Proposed, External: true}
	propDec     = Object{Ref: "M-0219", Lifecycle: lifecycle.Proposed, Decision: true}
	keptDec     = Object{Ref: "M-0219", Lifecycle: lifecycle.Kept, Decision: true}
)

func TestDecide(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		actor      Actor
		action     Action
		object     Object
		space      Space
		effect     Effect
		code       string
		quarantine bool
		message    string // substring the message must contain, if set
	}{
		// --- writing a new statement: people ---
		{"owner remembers", person(RoleOwner, ViaWeb), ActionRemember, newFact, project, EffectApply, "", false, ""},
		{"member remembers", person(RoleMember, ViaWeb), ActionRemember, newFact, project, EffectApply, "", false, ""},
		{"member remembers from the CLI", person(RoleMember, ViaCLI), ActionRemember, newFact, project, EffectApply, "", false, ""},
		{"viewer remembers → proposal", person(RoleViewer, ViaWeb), ActionRemember, newFact, project, EffectPropose, CodeViewer, false, "viewers propose"},
		{"member proposes on purpose", person(RoleMember, ViaWeb), ActionPropose, newFact, project, EffectPropose, CodePersonProposed, false, ""},
		{"non-member refused", person(RoleNone, ViaWeb), ActionRemember, newFact, project, EffectRefuse, CodeNotMember, false, "memax-v2"},
		{"unknown role refused", person(Role("admin"), ViaWeb), ActionRemember, newFact, project, EffectRefuse, CodeNotMember, false, ""},
		{"person cites an outside source → kept", person(RoleMember, ViaWeb), ActionRemember, newExternal, project, EffectApply, "", false, ""},
		{"members in an owners-keep space propose", person(RoleMember, ViaWeb), ActionRemember, newFact, ownersKeep, EffectPropose, CodeOwnersKeep, false, ""},
		{"owners in an owners-keep space keep", person(RoleOwner, ViaWeb), ActionRemember, newFact, ownersKeep, EffectApply, "", false, ""},
		{"team decision on the web → kept", person(RoleMember, ViaWeb), ActionRemember, newDecision, team, EffectApply, "", false, ""},
		{"team decision from the CLI → proposal (D15)", person(RoleMember, ViaCLI), ActionRemember, newDecision, team, EffectPropose, CodeDecisionNeedsWeb, false, "on the web"},
		{"team decision via MCP → proposal (D15)", person(RoleMember, ViaMCP), ActionRemember, newDecision, team, EffectPropose, CodeDecisionNeedsWeb, false, ""},
		{"team rule off → decision kept", person(RoleMember, ViaCLI), ActionRemember, newDecision, teamNoWeb, EffectApply, "", false, ""},
		{"personal decision from the CLI → kept", person(RoleOwner, ViaCLI), ActionRemember, newDecision, personal, EffectApply, "", false, ""},
		{"email is always proposed and external", person(RoleOwner, ViaEmail), ActionRemember, newFact, project, EffectPropose, CodeIntegration, true, "email"},
		{"slack is always proposed and external", person(RoleOwner, ViaSlack), ActionRemember, newFact, project, EffectPropose, CodeIntegration, true, "slack"},
		{"imports are proposed", person(RoleOwner, ViaImport), ActionRemember, newFact, project, EffectPropose, CodeImport, false, ""},
		{"secrets refused", person(RoleOwner, ViaWeb), ActionRemember, Object{Secrets: []string{"AWS access key"}}, project, EffectRefuse, CodeSecret, false, "AWS access key"},
		{"unknown actor refused", Actor{Kind: "robot"}, ActionRemember, newFact, project, EffectRefuse, CodeUnknownActor, false, ""},
		{"unknown action refused", person(RoleOwner, ViaWeb), Action("delete"), newFact, project, EffectRefuse, CodeUnknownAction, false, ""},

		// --- API keys: read or propose only, never keep or forget ---
		{"key at propose → proposal", with(person(RoleOwner, ViaAPI), func(a *Actor) { a.Credential, a.Autonomy = CredentialAPIKey, AutonomyPropose }), ActionRemember, newFact, project, EffectPropose, CodeAPIKey, false, ""},
		{"key claiming write → still a proposal", with(person(RoleOwner, ViaAPI), func(a *Actor) { a.Credential, a.Autonomy = CredentialAPIKey, AutonomyWrite }), ActionRemember, newFact, project, EffectPropose, CodeAPIKey, false, ""},
		{"key at read refused", with(person(RoleOwner, ViaAPI), func(a *Actor) { a.Credential, a.Autonomy = CredentialAPIKey, AutonomyRead }), ActionRemember, newFact, project, EffectRefuse, CodeKeyReadOnly, false, "Settings"},
		{"key with no scope refused", with(person(RoleOwner, ViaAPI), func(a *Actor) { a.Credential = CredentialAPIKey }), ActionRemember, newFact, project, EffectRefuse, CodeKeyReadOnly, false, ""},
		{"agent on a key at write → proposal", with(agent(RoleOwner, AutonomyWrite), func(a *Actor) { a.Credential = CredentialAPIKey }), ActionPropose, newFact, project, EffectPropose, CodeAPIKey, false, ""},
		{"agent on a key never confirms", with(agent(RoleOwner, AutonomyPropose), func(a *Actor) { a.Credential, a.PersonPresent, a.CanElicit = CredentialAPIKey, true, true }), ActionPropose, newFact, project, EffectPropose, CodeAPIKey, false, ""},
		{"key can't keep", with(person(RoleOwner, ViaAPI), func(a *Actor) { a.Credential, a.Autonomy = CredentialAPIKey, AutonomyPropose }), ActionKeep, proposal, project, EffectRefuse, CodeKeyCannotReview, false, ""},
		{"key can't reject", with(person(RoleOwner, ViaAPI), func(a *Actor) { a.Credential, a.Autonomy = CredentialAPIKey, AutonomyPropose }), ActionReject, proposal, project, EffectRefuse, CodeKeyCannotReview, false, ""},
		{"key can't forget", with(person(RoleOwner, ViaAPI), func(a *Actor) { a.Credential, a.Autonomy = CredentialAPIKey, AutonomyPropose }), ActionForget, kept, project, EffectRefuse, CodeKeyCannotForget, false, ""},
		{"key edit → proposal", with(person(RoleOwner, ViaAPI), func(a *Actor) { a.Credential, a.Autonomy = CredentialAPIKey, AutonomyPropose }), ActionEdit, kept, project, EffectPropose, CodeAPIKey, false, ""},

		// --- agents writing a new statement ---
		{"agent at read refused", agent(RoleOwner, AutonomyRead), ActionPropose, newFact, project, EffectRefuse, CodeReadOnly, false, "Codex is read-only in memax-v2. Change it in Agents."},
		{"agent with no autonomy refused", agent(RoleOwner, ""), ActionPropose, newFact, project, EffectRefuse, CodeReadOnly, false, ""},
		{"agent at propose → proposal", agent(RoleOwner, AutonomyPropose), ActionPropose, newFact, project, EffectPropose, CodeAutonomyPropose, false, "Codex proposes"},
		{"agent remember is a propose", agent(RoleOwner, AutonomyPropose), ActionRemember, newFact, project, EffectPropose, CodeAutonomyPropose, false, ""},
		{"agent at propose, person present → confirm", with(agent(RoleOwner, AutonomyPropose), func(a *Actor) { a.PersonPresent, a.CanElicit = true, true }), ActionPropose, newFact, project, EffectConfirm, CodeConfirm, false, "Keep this in memax-v2?"},
		{"person present but no elicitation → proposal", with(agent(RoleOwner, AutonomyPropose), func(a *Actor) { a.PersonPresent = true }), ActionPropose, newFact, project, EffectPropose, CodeAutonomyPropose, false, ""},
		{"elicitation but nobody present → proposal", with(agent(RoleOwner, AutonomyPropose), func(a *Actor) { a.CanElicit = true }), ActionPropose, newFact, project, EffectPropose, CodeAutonomyPropose, false, ""},
		{"external never confirms in agent", with(agent(RoleOwner, AutonomyPropose), func(a *Actor) { a.PersonPresent, a.CanElicit = true, true }), ActionPropose, newExternal, project, EffectPropose, CodeExternalSource, true, ""},
		{"contradiction never confirms in agent", with(agent(RoleOwner, AutonomyPropose), func(a *Actor) { a.PersonPresent, a.CanElicit = true, true }), ActionPropose, contradicts, project, EffectPropose, CodeAutonomyPropose, false, ""},
		{"team decision never confirms in agent", with(agent(RoleOwner, AutonomyPropose), func(a *Actor) { a.PersonPresent, a.CanElicit = true, true }), ActionPropose, newDecision, team, EffectPropose, CodeAutonomyPropose, false, ""},
		{"viewer's agent never confirms", with(agent(RoleViewer, AutonomyPropose), func(a *Actor) { a.PersonPresent, a.CanElicit = true, true }), ActionPropose, newFact, project, EffectPropose, CodeViewer, false, ""},
		{"agent at write → kept", agent(RoleOwner, AutonomyWrite), ActionPropose, newFact, project, EffectApply, "", false, ""},
		{"agent at write, member → kept", agent(RoleMember, AutonomyWrite), ActionPropose, newFact, project, EffectApply, "", false, ""},
		{"agent at write, external → proposal", agent(RoleOwner, AutonomyWrite), ActionPropose, newExternal, project, EffectPropose, CodeExternalSource, true, "outside source"},
		{"agent at write, contradiction → proposal", agent(RoleOwner, AutonomyWrite), ActionPropose, contradicts, project, EffectPropose, CodeContradicts, false, ""},
		{"agent at write, team decision → proposal", agent(RoleOwner, AutonomyWrite), ActionPropose, newDecision, team, EffectPropose, CodeDecisionNeedsWeb, false, ""},
		{"agent at write, project decision → kept", agent(RoleOwner, AutonomyWrite), ActionPropose, newDecision, project, EffectApply, "", false, ""},
		{"agent at write for a viewer → proposal", agent(RoleViewer, AutonomyWrite), ActionPropose, newFact, project, EffectPropose, CodeViewer, false, ""},
		{"agent at write for a member, owners keep → proposal", agent(RoleMember, AutonomyWrite), ActionPropose, newFact, ownersKeep, EffectPropose, CodeOwnersKeep, false, ""},
		{"agent at write for an owner, owners keep → kept", agent(RoleOwner, AutonomyWrite), ActionPropose, newFact, ownersKeep, EffectApply, "", false, ""},
		{"agent for a non-member refused", agent(RoleNone, AutonomyWrite), ActionPropose, newFact, project, EffectRefuse, CodeNotMember, false, ""},
		{"agent through slack is external", with(agent(RoleOwner, AutonomyWrite), func(a *Actor) { a.Via = ViaSlack }), ActionPropose, newFact, project, EffectPropose, CodeIntegration, true, ""},
		{"agent through github is external", with(agent(RoleOwner, AutonomyWrite), func(a *Actor) { a.Via = ViaGitHub }), ActionPropose, newFact, project, EffectPropose, CodeIntegration, true, ""},

		// --- Dream, Memax, the repository ---
		{"dream proposes", Actor{Kind: ActorDream, Via: ViaSystem}, ActionPropose, newFact, project, EffectPropose, CodeSystem, false, ""},
		{"dream remember is a proposal", Actor{Kind: ActorDream, Via: ViaSystem}, ActionRemember, newFact, project, EffectPropose, CodeSystem, false, ""},
		{"memax proposes", Actor{Kind: ActorMemax, Via: ViaSystem}, ActionPropose, newFact, project, EffectPropose, CodeSystem, false, ""},
		{"repository proposes", Actor{Kind: ActorRepository, Via: ViaGitHub}, ActionPropose, newFact, project, EffectPropose, CodeIntegration, true, ""},
		{"repository through the CLI proposes", Actor{Kind: ActorRepository, Via: ViaCLI}, ActionPropose, newFact, project, EffectPropose, CodeRepository, false, ""},
		{"dream can't keep", Actor{Kind: ActorDream, Via: ViaSystem}, ActionKeep, proposal, project, EffectRefuse, CodePersonMustReview, false, ""},
		{"dream can't reject", Actor{Kind: ActorDream, Via: ViaSystem}, ActionReject, proposal, project, EffectRefuse, CodePersonMustReview, false, ""},
		{"dream can't forget", Actor{Kind: ActorDream, Via: ViaSystem}, ActionForget, kept, project, EffectRefuse, CodePersonMustForget, false, ""},
		{"dream edit → proposal", Actor{Kind: ActorDream, Via: ViaSystem}, ActionEdit, kept, project, EffectPropose, CodeSystem, false, ""},
		{"dream can't edit a proposal", Actor{Kind: ActorDream, Via: ViaSystem}, ActionEdit, proposal, project, EffectRefuse, CodeProposalInReview, false, ""},

		// --- keep ---
		{"member keeps on the web", person(RoleMember, ViaReview), ActionKeep, proposal, project, EffectApply, "", false, ""},
		{"member keeps in the agent", person(RoleMember, ViaMCP), ActionKeep, proposal, project, EffectApply, "", false, ""},
		{"owner keeps", person(RoleOwner, ViaWeb), ActionKeep, proposal, project, EffectApply, "", false, ""},
		{"viewer can't keep", person(RoleViewer, ViaWeb), ActionKeep, proposal, project, EffectRefuse, CodeViewer, false, "Ask a member"},
		{"non-member can't keep", person(RoleNone, ViaWeb), ActionKeep, proposal, project, EffectRefuse, CodeNotMember, false, ""},
		{"agent at write can't keep", agent(RoleOwner, AutonomyWrite), ActionKeep, proposal, project, EffectRefuse, CodePersonMustReview, false, "M-0219 is waiting in Review"},
		{"member can't keep where owners keep", person(RoleMember, ViaWeb), ActionKeep, proposal, ownersKeep, EffectRefuse, CodeOwnersKeep, false, ""},
		{"owner keeps where owners keep", person(RoleOwner, ViaWeb), ActionKeep, proposal, ownersKeep, EffectApply, "", false, ""},
		{"team decision kept in the agent refused (D15)", person(RoleMember, ViaMCP), ActionKeep, propDec, team, EffectRefuse, CodeDecisionNeedsWeb, false, "Keep it in Review"},
		{"team decision kept from the CLI refused (D15)", person(RoleMember, ViaCLI), ActionKeep, propDec, team, EffectRefuse, CodeDecisionNeedsWeb, false, ""},
		{"team decision kept on the web", person(RoleMember, ViaReview), ActionKeep, propDec, team, EffectApply, "", false, ""},
		{"project decision kept in the agent", person(RoleMember, ViaMCP), ActionKeep, propDec, project, EffectApply, "", false, ""},
		{"external kept in the agent refused", person(RoleOwner, ViaMCP), ActionKeep, propExt, project, EffectRefuse, CodeExternalNeedsReview, false, "outside source"},
		{"external kept on the web", person(RoleOwner, ViaWeb), ActionKeep, propExt, project, EffectApply, "", false, ""},

		// --- edit ---
		{"member edits a kept memory", person(RoleMember, ViaWeb), ActionEdit, personKept, project, EffectApply, "", false, ""},
		{"member edits a proposal", person(RoleMember, ViaReview), ActionEdit, proposal, project, EffectApply, "", false, ""},
		{"member edits an external memory", person(RoleMember, ViaWeb), ActionEdit, keptExt, project, EffectApply, "", false, ""},
		{"viewer edit → proposal", person(RoleViewer, ViaWeb), ActionEdit, kept, project, EffectPropose, CodeViewer, false, ""},
		{"viewer can't edit a proposal", person(RoleViewer, ViaWeb), ActionEdit, proposal, project, EffectRefuse, CodeProposalInReview, false, "Propose a new memory instead"},
		{"team decision edit from the CLI → proposal", person(RoleMember, ViaCLI), ActionEdit, keptDec, team, EffectPropose, CodeDecisionNeedsWeb, false, ""},
		{"agent at write edits agent-kept work", agent(RoleOwner, AutonomyWrite), ActionEdit, kept, project, EffectApply, "", false, ""},
		{"agent at write edits person-kept → proposal", agent(RoleOwner, AutonomyWrite), ActionEdit, personKept, project, EffectPropose, CodeEditsPersonKept, false, "a person kept M-0219"},
		{"agent at write edits external → proposal", agent(RoleOwner, AutonomyWrite), ActionEdit, keptExt, project, EffectPropose, CodeExternalSource, true, ""},
		{"agent at write edit contradicts → proposal", agent(RoleOwner, AutonomyWrite), ActionEdit, Object{Ref: "M-1", Lifecycle: lifecycle.Kept, ContradictsDecision: true}, project, EffectPropose, CodeContradicts, false, ""},
		{"agent at write edits team decision → proposal", agent(RoleOwner, AutonomyWrite), ActionEdit, keptDec, team, EffectPropose, CodeDecisionNeedsWeb, false, ""},
		{"agent at propose edit → proposal", agent(RoleOwner, AutonomyPropose), ActionEdit, kept, project, EffectPropose, CodeAutonomyPropose, false, ""},
		{"agent can't edit a proposal", agent(RoleOwner, AutonomyWrite), ActionEdit, proposal, project, EffectRefuse, CodeProposalInReview, false, "M-0219 is waiting in Review"},
		{"agent at read can't edit", agent(RoleOwner, AutonomyRead), ActionEdit, kept, project, EffectRefuse, CodeReadOnly, false, ""},
		{"viewer's agent edit → proposal", agent(RoleViewer, AutonomyWrite), ActionEdit, kept, project, EffectPropose, CodeViewer, false, ""},
		{"secrets in an edit refused", person(RoleOwner, ViaWeb), ActionEdit, Object{Ref: "M-1", Lifecycle: lifecycle.Kept, Secrets: []string{"JWT"}}, project, EffectRefuse, CodeSecret, false, ""},

		// --- reject ---
		{"member rejects", person(RoleMember, ViaReview), ActionReject, proposal, project, EffectApply, "", false, ""},
		{"rejecting needs no web", person(RoleMember, ViaMCP), ActionReject, propDec, team, EffectApply, "", false, ""},
		{"viewer can't reject", person(RoleViewer, ViaReview), ActionReject, proposal, project, EffectRefuse, CodeViewer, false, ""},
		{"agent can't reject", agent(RoleOwner, AutonomyWrite), ActionReject, proposal, project, EffectRefuse, CodePersonMustReview, false, ""},
		{"member can't reject where owners keep", person(RoleMember, ViaReview), ActionReject, proposal, ownersKeep, EffectRefuse, CodeOwnersKeep, false, ""},

		// --- forget ---
		{"owner forgets", person(RoleOwner, ViaWeb), ActionForget, kept, project, EffectApply, "", false, ""},
		{"member can't forget by default", person(RoleMember, ViaWeb), ActionForget, kept, project, EffectRefuse, CodeForgetNotAllowed, false, "Ask an owner"},
		{"V1 admin (member + can forget) forgets", with(person(RoleMember, ViaWeb), func(a *Actor) { a.CanForget = true }), ActionForget, kept, project, EffectApply, "", false, ""},
		{"members forget where the rule allows", person(RoleMember, ViaWeb), ActionForget, kept, membersFgt, EffectApply, "", false, ""},
		{"viewer can't forget", person(RoleViewer, ViaWeb), ActionForget, kept, membersFgt, EffectRefuse, CodeForgetNotAllowed, false, ""},
		{"agent forget asks the person", with(agent(RoleOwner, AutonomyWrite), func(a *Actor) { a.PersonPresent, a.CanElicit = true, true }), ActionForget, kept, project, EffectConfirm, CodeConfirm, false, "can't be undone"},
		{"agent forget with nobody present refused", agent(RoleOwner, AutonomyWrite), ActionForget, kept, project, EffectRefuse, CodePersonMustForget, false, "on the web"},
		{"member's agent can't forget", with(agent(RoleMember, AutonomyWrite), func(a *Actor) { a.PersonPresent, a.CanElicit = true, true }), ActionForget, kept, project, EffectRefuse, CodeForgetNotAllowed, false, ""},
		{"agent at read can't forget", agent(RoleOwner, AutonomyRead), ActionForget, kept, project, EffectRefuse, CodeReadOnly, false, ""},
		{"repository can't forget", Actor{Kind: ActorRepository, Via: ViaGitHub}, ActionForget, kept, project, EffectRefuse, CodePersonMustForget, false, ""},

		// --- the Brief ---
		{"owner revises the Brief", person(RoleOwner, ViaWeb), ActionReviseBrief, newFact, project, EffectApply, "", false, ""},
		{"member revises the Brief", person(RoleMember, ViaCLI), ActionReviseBrief, newFact, project, EffectApply, "", false, ""},
		{"viewer can't revise the Brief", person(RoleViewer, ViaWeb), ActionReviseBrief, newFact, project, EffectRefuse, CodeViewer, false, "Ask a member"},
		{"members in an owners-keep space can't revise", person(RoleMember, ViaWeb), ActionReviseBrief, newFact, ownersKeep, EffectRefuse, CodeOwnersKeep, false, ""},
		{"Dream rewrites the Brief", Actor{Kind: ActorDream, Via: ViaSystem}, ActionReviseBrief, newFact, project, EffectApply, "", false, ""},
		{"an agent at Write can't revise the Brief", agent(RoleOwner, AutonomyWrite), ActionReviseBrief, newFact, project, EffectRefuse, CodeBriefByPerson, false, "people edit the Brief"},
		{"a secret in the Brief is refused", person(RoleOwner, ViaWeb), ActionReviseBrief, Object{Secrets: []string{"GitHub token"}}, project, EffectRefuse, CodeSecret, false, ""},

		// --- targets and compiles ---
		{"member configures a target", person(RoleMember, ViaWeb), ActionConfigureTarget, newFact, project, EffectApply, "", false, ""},
		{"viewer can't configure a target", person(RoleViewer, ViaWeb), ActionConfigureTarget, newFact, project, EffectRefuse, CodeViewer, false, ""},
		{"members in an owners-keep space can't configure", person(RoleMember, ViaWeb), ActionConfigureTarget, newFact, ownersKeep, EffectRefuse, CodeOwnersKeep, false, ""},
		{"an agent can't configure a target", agent(RoleOwner, AutonomyWrite), ActionConfigureTarget, newFact, project, EffectRefuse, CodeTargetsByPerson, false, ""},
		{"viewer asks for a compile", person(RoleViewer, ViaWeb), ActionRequestCompile, newFact, project, EffectApply, "", false, ""},
		{"agent asks for a compile", agent(RoleOwner, AutonomyPropose), ActionRequestCompile, newFact, project, EffectApply, "", false, ""},
		{"read-only agent can't ask for a compile", agent(RoleOwner, AutonomyRead), ActionRequestCompile, newFact, project, EffectRefuse, CodeReadOnly, false, ""},
		{"Memax records compiles", Actor{Kind: ActorMemax, Via: ViaSystem}, ActionRecordCompile, newFact, project, EffectApply, "", false, ""},
		{"a person can't record a compile", person(RoleOwner, ViaWeb), ActionRecordCompile, newFact, project, EffectRefuse, CodeCompileByMemax, false, ""},
		{"a device reports", person(RoleViewer, ViaCLI), ActionReport, newFact, project, EffectApply, "", false, ""},
		{"the repository reports", Actor{Kind: ActorRepository, Via: ViaGitHub}, ActionReport, newFact, project, EffectApply, "", false, ""},
		{"a read-only key can't report", with(agent(RoleOwner, AutonomyRead), func(a *Actor) { a.Credential = CredentialAPIKey }), ActionReport, newFact, project, EffectRefuse, CodeKeyReadOnly, false, ""},
		{"Dream doesn't report", Actor{Kind: ActorDream, Via: ViaSystem}, ActionReport, newFact, project, EffectRefuse, CodeTargetsByPerson, false, ""},
		{"a viewer pulls a hand edit", person(RoleViewer, ViaWeb), ActionPullDrift, newFact, project, EffectApply, "", false, ""},
		{"an agent can't pull a hand edit", agent(RoleOwner, AutonomyWrite), ActionPullDrift, newFact, project, EffectRefuse, CodeTargetsByPerson, false, ""},
		{"non-member can't pull", person(RoleNone, ViaWeb), ActionPullDrift, newFact, project, EffectRefuse, CodeNotMember, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Decide(c.actor, c.action, c.object, c.space)
			if got.Effect != c.effect || got.Code != c.code || got.Quarantine != c.quarantine {
				t.Fatalf("Decide = {%s %s quarantine=%v %q}, want {%s %s quarantine=%v}",
					got.Effect, got.Code, got.Quarantine, got.Message, c.effect, c.code, c.quarantine)
			}
			if c.message != "" && !strings.Contains(got.Message, c.message) {
				t.Errorf("message %q does not contain %q", got.Message, c.message)
			}
		})
	}
}

// TestDecideMatrix walks every actor kind × role × autonomy × credential
// × surface × action and checks the invariants that must hold whatever
// the combination: a key never keeps or forgets, only people keep,
// refusals and downgrades always explain themselves in the product
// voice, and a read-only agent writes nothing.
func TestDecideMatrix(t *testing.T) {
	t.Parallel()
	roles := []Role{RoleNone, RoleOwner, RoleMember, RoleViewer}
	levels := []Autonomy{"", AutonomyRead, AutonomyPropose, AutonomyWrite}
	creds := []Credential{CredentialSession, CredentialOAuth, CredentialAPIKey}
	actions := []Action{ActionRemember, ActionPropose, ActionKeep, ActionEdit, ActionReject, ActionForget}
	objects := []Object{newFact, newDecision, newExternal, contradicts, proposal, kept, personKept, keptExt, propExt, propDec}
	spaces := []Space{project, personal, team, ownersKeep, membersFgt}
	banned := []string{"!", " AI", "magic", "smart", "delete", "Delete"}

	n := 0
	for _, kind := range ActorKinds {
		for _, role := range roles {
			for _, level := range levels {
				for _, cred := range creds {
					for _, via := range Vias {
						for _, present := range []bool{false, true} {
							a := Actor{Kind: kind, Role: role, Autonomy: level, Credential: cred, Via: via, PersonPresent: present, CanElicit: present}
							for _, act := range actions {
								for _, o := range objects {
									for _, s := range spaces {
										n++
										d := Decide(a, act, o, s)
										check := func(ok bool, what string) {
											if !ok {
												t.Fatalf("%s: %+v %s %+v %+v → %+v", what, a, act, o, s, d)
											}
										}
										switch d.Effect {
										case EffectApply, EffectPropose, EffectConfirm, EffectRefuse:
										default:
											check(false, "unknown effect")
										}
										if d.Effect != EffectApply {
											check(d.Code != "" && d.Message != "", "non-apply decision without code and message")
										}
										for _, b := range banned {
											check(!strings.Contains(d.Message, b), "message breaks the voice rules ("+b+")")
										}
										if cred == CredentialAPIKey && (act == ActionKeep || act == ActionReject || act == ActionForget) {
											check(d.Effect == EffectRefuse, "API key kept, rejected or forgot")
										}
										if cred == CredentialAPIKey && d.Effect == EffectApply {
											check(false, "API key write applied")
										}
										if act == ActionKeep && d.Effect == EffectApply {
											check(kind == ActorPerson && (role == RoleOwner || role == RoleMember), "keep applied for a non-member or non-person")
										}
										if kind == ActorAgent && level != AutonomyPropose && level != AutonomyWrite {
											check(d.Effect == EffectRefuse, "read-only agent wrote")
										}
										if role == RoleViewer && d.Effect == EffectApply {
											check(false, "viewer's write applied")
										}
										if (via.Integration() || o.External) && kind != ActorPerson && d.Effect == EffectApply {
											check(false, "external content applied for a non-person")
										}
										if role == RoleNone && (kind == ActorPerson || kind == ActorAgent) {
											check(d.Effect == EffectRefuse, "non-member not refused")
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("matrix is empty")
	}
}

func TestRules(t *testing.T) {
	t.Parallel()
	var zero Rules
	if zero.KeepBy() != WhoMembers || zero.ForgetBy() != WhoOwners || zero.AgentAutonomy() != AutonomyPropose {
		t.Errorf("zero rules = keep %s, forget %s, autonomy %s", zero.KeepBy(), zero.ForgetBy(), zero.AgentAutonomy())
	}
	if !zero.DecisionsNeedPersonOnWeb(SpaceTeam) || zero.DecisionsNeedPersonOnWeb(SpaceProject) || zero.DecisionsNeedPersonOnWeb(SpacePersonal) {
		t.Error("D15 default: on for team spaces only")
	}
	if (Rules{DecisionsNeedWeb: ptr(true)}).DecisionsNeedPersonOnWeb(SpaceProject) != true {
		t.Error("explicit decisions_need_web ignored")
	}
	if (Rules{Keep: "admins"}).KeepBy() != WhoOwners {
		t.Error("an unknown keep rule must fall back to the strictest (owners)")
	}
	if (Rules{Forget: "everyone"}).ForgetBy() != WhoOwners {
		t.Error("an unknown forget rule must fall back to owners")
	}
	if (Rules{NewAgentAutonomy: "admin"}).AgentAutonomy() != AutonomyPropose {
		t.Error("an unknown autonomy must fall back to propose")
	}
}

func TestRoleFromV1(t *testing.T) {
	t.Parallel()
	cases := []struct {
		v1     string
		role   Role
		forget bool
	}{
		{"owner", RoleOwner, true},
		{"admin", RoleMember, true},
		{"contributor", RoleMember, false},
		{"viewer", RoleViewer, false},
		{"superuser", RoleViewer, false},
		{"", RoleViewer, false},
	}
	for _, c := range cases {
		role, forget := RoleFromV1(c.v1)
		if role != c.role || forget != c.forget {
			t.Errorf("RoleFromV1(%q) = %s, %v; want %s, %v", c.v1, role, forget, c.role, c.forget)
		}
	}
}

func TestAssuranceIsDerivedFromTheSurface(t *testing.T) {
	t.Parallel()
	cases := map[Via]Assurance{
		ViaWeb: AssuranceHumanWeb, ViaReview: AssuranceHumanWeb,
		ViaMCP: AssuranceClientAttested, ViaCLI: AssuranceClientAttested, ViaAPI: AssuranceClientAttested,
		ViaEmail: AssuranceClientAttested,
	}
	for via, want := range cases {
		if got := person(RoleOwner, via).Assurance(); got != want {
			t.Errorf("person via %s: assurance %q, want %q", via, got, want)
		}
	}
	if got := agent(RoleOwner, AutonomyWrite).Assurance(); got != "" {
		t.Errorf("agents carry no assurance, got %q", got)
	}
}

func TestMinTrust(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   []Trust
		want Trust
	}{
		{nil, ""},
		{[]Trust{TrustPerson}, TrustPerson},
		{[]Trust{TrustPerson, TrustAgentOwnWork}, TrustAgentOwnWork},
		{[]Trust{TrustAgentOwnWork, TrustRepository, TrustPerson}, TrustRepository},
		{[]Trust{TrustRepository, TrustExternal}, TrustExternal},
		{[]Trust{TrustPerson, "made-up"}, TrustExternal},
		{[]Trust{TrustExternal, TrustPerson, TrustPerson}, TrustExternal},
	}
	for _, c := range cases {
		if got := MinTrust(c.in...); got != c.want {
			t.Errorf("MinTrust(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestActorTrust(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind ActorKind
		via  Via
		want Trust
	}{
		{ActorPerson, ViaWeb, TrustPerson},
		{ActorPerson, ViaEmail, TrustExternal},
		{ActorAgent, ViaMCP, TrustAgentOwnWork},
		{ActorAgent, ViaSlack, TrustExternal},
		{ActorDream, ViaSystem, TrustAgentOwnWork},
		{ActorMemax, ViaSystem, TrustAgentOwnWork},
		{ActorRepository, ViaCLI, TrustRepository},
		{ActorKind("robot"), ViaWeb, TrustExternal},
	}
	for _, c := range cases {
		if got := ActorTrust(c.kind, c.via); got != c.want {
			t.Errorf("ActorTrust(%s, %s) = %s, want %s", c.kind, c.via, got, c.want)
		}
	}
	if !TrustExternal.External() || TrustRepository.External() || !Trust("x").External() {
		t.Error("External(): external and unknown classes are quarantined, others aren't")
	}
}

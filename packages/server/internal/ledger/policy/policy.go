// Package policy decides what a write to the V2 record does: apply it,
// send it to Review as a proposal, ask the person in the agent to
// confirm, or refuse it. Decide is the one place autonomy, roles,
// quarantine and (later) plan limits are enforced, for every surface
// (plan 25 §5.6, HANDOFF rules 2, 3, 4 and 12).
//
// Decide is a pure function of its inputs. The ledger loads the facts
// (the actor's role in the space, the memory's trust, whether a person
// kept it) and Decide applies the rules:
//
//   - Agent at Read: refused, with a message saying where to change it.
//   - Agent at Propose: proposed; when a person is present and the client
//     can elicit, NeedsConfirmation (an accepted confirmation is a Keep by
//     that person, "via" the agent).
//   - Agent at Write: kept, unless a source is external, it contradicts a
//     decision in force, it touches one (the ledger's inline pre-check; the
//     judge decides after), it edits a memory a person kept, or it is a
//     decision in a space where decisions need a person on the web.
//   - Viewer: proposed. Member and owner: kept (and Review is theirs).
//   - API key: read or propose only; never keeps, rejects or forgets.
//   - An agent that isn't connected to the space, or is paused, only reads.
//   - Dream, Memax and the repository: new statements are proposals.
//   - Integrations (email, Slack, GitHub, Linear): proposed and external.
//   - The Brief: people who may keep edit it, and Dream rewrites it;
//     agents never do. Targets: people who may keep configure them,
//     overwrite a hand edit or stop compiling; anyone who can see the
//     space may ask for a compile (not a read-only agent), any person may
//     pull a hand edit back as proposals, devices and the repository
//     report deliveries and hand edits, and only Memax records compiles.
//   - The judge: only Memax records its verdicts. Settling a conflict
//     follows Keep's rules. Undo is the decider's own; any person who may
//     keep can undo one of the judge's folds.
//
// Messages follow the product voice (sentence case, actionable, no
// exclamation marks). Clients localise by Code; Message is the English
// fallback.
package policy

import (
	"fmt"
	"slices"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
)

// ActorKind is who acted, as receipts record it.
type ActorKind string

// The actor kinds.
const (
	ActorPerson     ActorKind = "person"
	ActorAgent      ActorKind = "agent"
	ActorDream      ActorKind = "dream"
	ActorMemax      ActorKind = "memax"
	ActorRepository ActorKind = "repository"
)

// ActorKinds lists every actor kind.
var ActorKinds = []ActorKind{ActorPerson, ActorAgent, ActorDream, ActorMemax, ActorRepository}

// Valid reports whether k is a known actor kind.
func (k ActorKind) Valid() bool { return slices.Contains(ActorKinds, k) }

// Role is a person's role in a space. Agents act with the role of the
// person they work for.
type Role string

// The roles. RoleNone means "not a member".
const (
	RoleNone   Role = ""
	RoleOwner  Role = "owner"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

// RoleFromV1 maps a V1 hub_members.role onto a V2 role (plan 25 §10):
// owner → owner; admin → member who can forget; contributor → member;
// viewer → viewer. Anything unknown maps to viewer, the least privilege.
func RoleFromV1(v1 string) (role Role, canForget bool) {
	switch v1 {
	case "owner":
		return RoleOwner, true
	case "admin":
		return RoleMember, true
	case "contributor", "member":
		return RoleMember, false
	}
	return RoleViewer, false
}

// Autonomy is how much an agent (or an API key) may write in a space.
type Autonomy string

// The autonomy levels.
const (
	AutonomyRead    Autonomy = "read"
	AutonomyPropose Autonomy = "propose"
	AutonomyWrite   Autonomy = "write"
)

// Valid reports whether a is a known autonomy level.
func (a Autonomy) Valid() bool {
	return a == AutonomyRead || a == AutonomyPropose || a == AutonomyWrite
}

// Autonomies lists the levels, lowest first.
var Autonomies = []Autonomy{AutonomyRead, AutonomyPropose, AutonomyWrite}

// rank orders the levels; unknown values rank below read.
func (a Autonomy) rank() int {
	switch a {
	case AutonomyRead:
		return 0
	case AutonomyPropose:
		return 1
	case AutonomyWrite:
		return 2
	}
	return -1
}

// Above reports whether a allows more than b.
func (a Autonomy) Above(b Autonomy) bool { return a.rank() > b.rank() }

// MinAutonomy returns the lowest of the levels. An unknown level counts
// as read, and so does an empty list.
func MinAutonomy(as ...Autonomy) Autonomy {
	if len(as) == 0 {
		return AutonomyRead
	}
	low := AutonomyWrite
	for _, a := range as {
		if !a.Valid() {
			return AutonomyRead
		}
		if a.rank() < low.rank() {
			low = a
		}
	}
	return low
}

// AgentStatus is whether an agent is connected to a space (plan 25
// §5.15: an agent connection is identity plus autonomy per space).
type AgentStatus string

// The statuses. The zero value means connected (or not an agent).
const (
	AgentConnected AgentStatus = ""
	// AgentNotConnected: the credential has no connection, it was
	// disconnected, or it isn't connected to this space. It only reads.
	AgentNotConnected AgentStatus = "not_connected"
	// AgentPaused: the person paused it. It only reads.
	AgentPaused AgentStatus = "paused"
)

// Credential is how the actor authenticated. API keys are capped at
// Propose whatever else is configured.
type Credential string

// The credentials. The zero value is a signed-in session.
const (
	CredentialSession Credential = ""
	CredentialOAuth   Credential = "oauth"
	CredentialAPIKey  Credential = "api_key"
)

// Valid reports whether c is a known credential.
func (c Credential) Valid() bool {
	return c == CredentialSession || c == CredentialOAuth || c == CredentialAPIKey
}

// Via is the surface a change came through.
type Via string

// The surfaces.
const (
	ViaWeb    Via = "web"
	ViaCLI    Via = "cli"
	ViaMCP    Via = "mcp"
	ViaReview Via = "review"
	ViaAPI    Via = "api"
	ViaEmail  Via = "email"
	ViaSlack  Via = "slack"
	ViaGitHub Via = "github"
	ViaLinear Via = "linear"
	ViaImport Via = "import"
	ViaSystem Via = "system"
)

// Vias lists every surface.
var Vias = []Via{ViaWeb, ViaCLI, ViaMCP, ViaReview, ViaAPI, ViaEmail, ViaSlack, ViaGitHub, ViaLinear, ViaImport, ViaSystem}

// Valid reports whether v is a known surface.
func (v Via) Valid() bool { return slices.Contains(Vias, v) }

// Integration reports whether content through v is third-party content
// (always proposed and external).
func (v Via) Integration() bool {
	return v == ViaEmail || v == ViaSlack || v == ViaGitHub || v == ViaLinear
}

// Assurance is how sure we are that a person, not an automation, made a
// Keep (plan 25 §5.12): Claude Code hooks can auto-accept elicitations,
// and an agent can run the CLI with the person's login.
type Assurance string

// The assurance levels.
const (
	AssuranceHumanWeb       Assurance = "human_web"
	AssuranceClientAttested Assurance = "client_attested"
)

// SpaceKind is personal, project or team.
type SpaceKind string

// The space kinds.
const (
	SpacePersonal SpaceKind = "personal"
	SpaceProject  SpaceKind = "project"
	SpaceTeam     SpaceKind = "team"
)

// Valid reports whether k is a known space kind.
func (k SpaceKind) Valid() bool { return k == SpacePersonal || k == SpaceProject || k == SpaceTeam }

// Who names a set of members for a space rule.
type Who string

// The rule values.
const (
	WhoOwners  Who = "owners"
	WhoMembers Who = "members"
)

// Rules are a space's rules, stored in hubs.rules. The zero value means
// the defaults: members keep, owners forget, new agents start at
// Propose, and decisions need a person on the web in team spaces only
// (D15).
type Rules struct {
	Keep             Who      `json:"keep,omitempty"`
	Forget           Who      `json:"forget,omitempty"`
	NewAgentAutonomy Autonomy `json:"new_agent_autonomy,omitempty"`
	DecisionsNeedWeb *bool    `json:"decisions_need_web,omitempty"`
}

// KeepBy is who may keep and reject. Unknown values mean owners only.
func (r Rules) KeepBy() Who {
	if r.Keep == "" || r.Keep == WhoMembers {
		return WhoMembers
	}
	return WhoOwners
}

// ForgetBy is who may forget, besides members a V1 admin role carried
// over (SpaceGrant.CanForget). Only an explicit "members" widens it.
func (r Rules) ForgetBy() Who {
	if r.Forget == WhoMembers {
		return WhoMembers
	}
	return WhoOwners
}

// AgentAutonomy is the autonomy a newly connected agent starts at.
func (r Rules) AgentAutonomy() Autonomy {
	if r.NewAgentAutonomy.Valid() {
		return r.NewAgentAutonomy
	}
	return AutonomyPropose
}

// DecisionsNeedPersonOnWeb reports whether keeping a decision needs a
// person on the web (assurance human_web) in a space of this kind.
func (r Rules) DecisionsNeedPersonOnWeb(kind SpaceKind) bool {
	if r.DecisionsNeedWeb != nil {
		return *r.DecisionsNeedWeb
	}
	return kind == SpaceTeam
}

// Action is what the actor asks to do.
type Action string

// The actions.
const (
	ActionRemember Action = "remember" // write a statement, kept if allowed
	ActionPropose  Action = "propose"  // write a statement for Review (agents: kept at Write)
	ActionKeep     Action = "keep"
	ActionEdit     Action = "edit"
	ActionReject   Action = "reject"
	ActionForget   Action = "forget"

	// The Brief and compile actions (plan 25 §5.7).
	ActionReviseBrief     Action = "revise_brief"     // write a new Brief version
	ActionConfigureTarget Action = "configure_target" // add a target, change it, overwrite a hand edit, stop compiling
	ActionRequestCompile  Action = "request_compile"  // ask for a fresh compile
	ActionRecordCompile   Action = "record_compile"   // record a compile run
	ActionReport          Action = "report"           // report a delivery or a hand edit from a device or GitHub
	ActionPullDrift       Action = "pull_drift"       // turn a hand edit into proposals

	// The judge and Review (plan 25 §5.8, epics 1.3 and 1.4).
	ActionJudge           Action = "judge"            // record the judge's verdict on a memory (fold, link, flag)
	ActionResolveConflict Action = "resolve_conflict" // settle a conflict: one side wins, both narrow, or it stays open
	ActionUndo            Action = "undo"             // undo a person's last decision, or one of the judge's folds
)

// Actor is everything Decide needs to know about who is acting.
type Actor struct {
	Kind ActorKind
	// Name is shown in messages ("Codex is read-only in memax-v2").
	Name string
	// Role is the person's role in the space (for an agent, the role of
	// the person it works for). Ignored for Dream, Memax and repository.
	Role Role
	// CanForget carries V1's admin role (member + can_forget).
	CanForget bool
	// Autonomy applies to agents, and to API keys as the key's scope.
	Autonomy Autonomy
	// AgentStatus says whether an agent is connected to the space; one
	// that isn't, or is paused, only reads.
	AgentStatus AgentStatus
	Credential  Credential
	Via         Via
	// PersonPresent and CanElicit describe the agent's client: a person
	// is at the keyboard, and the client supports MCP elicitation.
	PersonPresent bool
	CanElicit     bool
}

// Assurance is the assurance a Keep by this actor carries: human_web
// only for the web app and Review; everything else (MCP confirmations,
// the CLI) is client_attested. It is derived, never claimed. Non-person
// actors have none.
func (a Actor) Assurance() Assurance {
	if a.Kind != ActorPerson {
		return ""
	}
	if a.Via == ViaWeb || a.Via == ViaReview {
		return AssuranceHumanWeb
	}
	return AssuranceClientAttested
}

// autonomy is the effective write level: people write on their own
// authority, API keys are capped at Propose, and system actors propose.
// An unknown level counts as Read.
func (a Actor) autonomy() Autonomy {
	level := AutonomyWrite
	switch a.Kind {
	case ActorAgent:
		level = a.Autonomy
	case ActorDream, ActorMemax, ActorRepository:
		level = AutonomyPropose
	}
	if a.Credential == CredentialAPIKey {
		level = a.Autonomy
		if level == AutonomyWrite {
			level = AutonomyPropose
		}
	}
	if a.Kind == ActorAgent && a.AgentStatus != AgentConnected {
		level = AutonomyRead
	}
	if !level.Valid() {
		return AutonomyRead
	}
	return level
}

// Object is what the action applies to. For Remember and Propose it
// describes the new statement.
type Object struct {
	// Ref is the display ID ("M-0219"), for messages; empty when new.
	Ref string
	// Lifecycle is the memory's current lifecycle (None when new).
	Lifecycle lifecycle.Lifecycle
	// Decision is set for kind = decision.
	Decision bool
	// External is set when any source (or the memory's trust) is external.
	External bool
	// ContradictsDecision is the judge's verdict: the statement
	// contradicts a decision in force (rule 11).
	ContradictsDecision bool
	// TouchesDecision is the ledger's inline check for a Write-level
	// agent: the statement names or overlaps a decision in force, so it
	// waits for the judge and a person instead of being kept at once.
	TouchesDecision bool
	// UndoOwn is set when the actor made the decision being undone;
	// UndoSystem when Memax did (one of the judge's folds).
	UndoOwn    bool
	UndoSystem bool
	// PersonKept is set when a person kept or edited the memory.
	PersonKept bool
	// Secrets names any credential patterns found in the new words.
	Secrets []string
}

// Space is the space the action happens in.
type Space struct {
	Name  string
	Kind  SpaceKind
	Rules Rules
}

// Effect is what happens to the write.
type Effect string

// The effects.
const (
	EffectApply   Effect = "apply"   // kept / applied as asked
	EffectPropose Effect = "propose" // sent to Review as a proposal
	EffectConfirm Effect = "confirm" // proposed, and the agent should ask the person to keep it
	EffectRefuse  Effect = "refuse"  // nothing is written
)

// Decision is Decide's answer. Code is stable and machine-readable;
// Message is the English sentence shown to people.
type Decision struct {
	Effect     Effect `json:"effect"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	Quarantine bool   `json:"quarantine,omitempty"`
}

// Decision codes. Refusals, then downgrades to a proposal, then the
// confirmation request.
const (
	CodeUnknownActor        = "unknown_actor"
	CodeUnknownAction       = "unknown_action"
	CodeSecret              = "secret_detected"
	CodeNotMember           = "not_member"
	CodeReadOnly            = "read_only"
	CodeKeyReadOnly         = "key_read_only"
	CodeKeyCannotReview     = "key_cannot_review"
	CodeKeyCannotForget     = "key_cannot_forget"
	CodePersonMustReview    = "person_must_review"
	CodePersonMustForget    = "person_must_forget"
	CodeForgetNotAllowed    = "forget_not_allowed"
	CodeExternalNeedsReview = "external_needs_review"
	CodeProposalInReview    = "proposal_in_review"
	CodeAgentNotConnected   = "agent_not_connected"
	CodeAgentPaused         = "agent_paused"
	CodeBriefByPerson       = "brief_by_person"
	CodeTargetsByPerson     = "targets_by_person"
	CodeCompileByMemax      = "compile_by_memax"
	CodeJudgeByMemax        = "judge_by_memax"
	CodeUndoByDecider       = "undo_by_decider"

	// Changes to agent connections (DecideConnection); all refusals.
	CodePersonMustManage   = "person_must_manage"
	CodeNotYourAgent       = "not_your_agent"
	CodeAutonomyNotAllowed = "autonomy_not_allowed"
	CodeKeyMaxPropose      = "key_max_propose"
	CodeAutonomyNeedsWeb   = "autonomy_needs_web"

	CodeViewer           = "viewer"             // refused (keep, reject) or downgraded (write)
	CodeOwnersKeep       = "owners_keep"        // refused (keep, reject) or downgraded (write)
	CodeDecisionNeedsWeb = "decision_needs_web" // refused (keep) or downgraded (write)

	CodeAPIKey          = "api_key"
	CodeExternalSource  = "external_source"
	CodeContradicts     = "contradicts_decision"
	CodeTouchesDecision = "touches_decision"
	CodeEditsPersonKept = "edits_person_kept"
	CodeAutonomyPropose = "autonomy_propose"
	CodeIntegration     = "integration"
	CodeImport          = "import"
	CodeSystem          = "system_proposes"
	CodeRepository      = "repository"
	CodePersonProposed  = "person_proposed"

	CodeConfirm = "confirm_in_agent"
)

// Decide applies the space's rules to one action.
func Decide(a Actor, act Action, o Object, s Space) Decision {
	if !a.Kind.Valid() {
		return refuse(CodeUnknownActor, "Memax doesn't recognise who is writing. Sign in again.")
	}
	if (a.Kind == ActorPerson || a.Kind == ActorAgent) && !slices.Contains([]Role{RoleOwner, RoleMember, RoleViewer}, a.Role) {
		return refuse(CodeNotMember, fmt.Sprintf("Only members of %s can change its record.", spaceName(s)))
	}
	if len(o.Secrets) > 0 && (act == ActionRemember || act == ActionPropose || act == ActionEdit || act == ActionReviseBrief) {
		return refuse(CodeSecret, fmt.Sprintf(
			"This looks like a credential (%s). Memax never stores secrets. Remove it and try again.",
			strings.Join(o.Secrets, ", ")))
	}
	switch act {
	case ActionRemember, ActionPropose:
		return decideWrite(a, act, o, s)
	case ActionEdit:
		return decideEdit(a, o, s)
	case ActionKeep:
		return decideKeep(a, o, s)
	case ActionReject:
		return decideReject(a, o, s)
	case ActionForget:
		return decideForget(a, o, s)
	case ActionReviseBrief:
		return decideReviseBrief(a, s)
	case ActionConfigureTarget:
		return decideConfigureTarget(a, s)
	case ActionRequestCompile:
		return decideRequestCompile(a, s)
	case ActionRecordCompile:
		if a.Kind == ActorMemax {
			return apply()
		}
		return refuse(CodeCompileByMemax, "Only Memax records compiles. Ask for one with Compile now.")
	case ActionReport:
		return decideReport(a, s)
	case ActionJudge:
		if a.Kind == ActorMemax {
			return apply()
		}
		return refuse(CodeJudgeByMemax, "Only Memax records the judge's verdicts.")
	case ActionResolveConflict:
		return decideResolve(a, o, s)
	case ActionUndo:
		return decideUndo(a, o, s)
	case ActionPullDrift:
		if a.Kind == ActorPerson {
			return apply()
		}
		return refuse(CodeTargetsByPerson,
			"A person decides what happens to a hand edit. Resolve it on the web or with the CLI.")
	}
	return refuse(CodeUnknownAction, fmt.Sprintf("Memax doesn't know how to %q.", act))
}

func decideWrite(a Actor, act Action, o Object, s Space) Decision {
	if a.autonomy() == AutonomyRead {
		return refuseReadOnly(a, s)
	}
	quarantine := o.External || a.Via.Integration()
	propose := func(code string) Decision { return downgrade(code, a, o, s, quarantine) }
	needsWeb := o.Decision && s.Rules.DecisionsNeedPersonOnWeb(s.Kind)

	switch {
	case a.Via.Integration():
		return propose(CodeIntegration)
	case a.Via == ViaImport:
		return propose(CodeImport)
	}
	switch a.Kind {
	case ActorDream, ActorMemax:
		return propose(CodeSystem)
	case ActorRepository:
		return propose(CodeRepository)
	}
	if code := keepCap(a, s); code != "" {
		return propose(code)
	}

	if a.Kind == ActorPerson {
		if act == ActionPropose {
			return propose(CodePersonProposed)
		}
		if needsWeb && a.Assurance() != AssuranceHumanWeb {
			return propose(CodeDecisionNeedsWeb)
		}
		return apply()
	}

	// An agent. External content is quarantined at any autonomy.
	if o.External {
		return propose(CodeExternalSource)
	}
	if a.autonomy() == AutonomyWrite {
		switch {
		case o.ContradictsDecision:
			return propose(CodeContradicts)
		case o.TouchesDecision:
			return propose(CodeTouchesDecision)
		case needsWeb:
			return propose(CodeDecisionNeedsWeb)
		}
		return apply()
	}
	if a.PersonPresent && a.CanElicit && !o.ContradictsDecision && !needsWeb {
		return Decision{Effect: EffectConfirm, Code: CodeConfirm, Message: fmt.Sprintf("Keep this in %s?", spaceName(s))}
	}
	return propose(CodeAutonomyPropose)
}

func decideEdit(a Actor, o Object, s Space) Decision {
	if a.autonomy() == AutonomyRead {
		return refuseReadOnly(a, s)
	}
	quarantine := o.External || a.Via.Integration()
	propose := func(code string) Decision { return downgrade(code, a, o, s, quarantine) }
	needsWeb := o.Decision && s.Rules.DecisionsNeedPersonOnWeb(s.Kind)

	var d Decision
	switch {
	case a.Via.Integration():
		d = propose(CodeIntegration)
	case a.Kind == ActorDream || a.Kind == ActorMemax:
		d = propose(CodeSystem)
	case a.Kind == ActorRepository:
		d = propose(CodeRepository)
	case a.Kind == ActorAgent && o.Lifecycle == lifecycle.Proposed:
		return refuse(CodeProposalInReview, fmt.Sprintf(
			"%s is waiting in Review. Propose a new memory instead.", refOr(o, "This proposal")))
	case keepCap(a, s) != "":
		d = propose(keepCap(a, s))
	case a.Kind == ActorPerson:
		if needsWeb && a.Assurance() != AssuranceHumanWeb {
			d = propose(CodeDecisionNeedsWeb)
		} else {
			d = apply()
		}
	case a.autonomy() == AutonomyWrite:
		switch {
		case o.External:
			d = propose(CodeExternalSource)
		case o.PersonKept:
			d = propose(CodeEditsPersonKept)
		case o.ContradictsDecision:
			d = propose(CodeContradicts)
		case o.TouchesDecision:
			d = propose(CodeTouchesDecision)
		case needsWeb:
			d = propose(CodeDecisionNeedsWeb)
		default:
			d = apply()
		}
	default:
		d = propose(CodeAutonomyPropose)
	}
	// A downgraded edit becomes a new proposal that supersedes the kept
	// memory. A proposal is already in Review, so there is nothing to
	// supersede: send the actor to Review instead.
	if d.Effect == EffectPropose && o.Lifecycle == lifecycle.Proposed {
		return refuse(CodeProposalInReview, fmt.Sprintf(
			"%s is waiting in Review. Propose a new memory instead.", refOr(o, "This proposal")))
	}
	return d
}

func decideKeep(a Actor, o Object, s Space) Decision {
	if a.Kind != ActorPerson {
		return refuse(CodePersonMustReview, fmt.Sprintf(
			"Agents propose and people keep. %s is waiting in Review.", refOr(o, "This proposal")))
	}
	if a.Credential == CredentialAPIKey {
		return refuse(CodeKeyCannotReview, "API keys can propose but never keep or reject. Review it on the web.")
	}
	if a.Role == RoleViewer {
		return refuse(CodeViewer, fmt.Sprintf(
			"Viewers can propose but not keep in %s. Ask a member to keep it.", spaceName(s)))
	}
	if !canKeep(a.Role, s.Rules) {
		return refuse(CodeOwnersKeep, fmt.Sprintf("Only owners keep in %s. Ask an owner to keep it.", spaceName(s)))
	}
	if o.Decision && s.Rules.DecisionsNeedPersonOnWeb(s.Kind) && a.Assurance() != AssuranceHumanWeb {
		return refuse(CodeDecisionNeedsWeb, fmt.Sprintf(
			"Decisions in %s need a person on the web. Keep it in Review.", spaceName(s)))
	}
	if o.External && a.Assurance() != AssuranceHumanWeb {
		return refuse(CodeExternalNeedsReview, fmt.Sprintf(
			"%s comes from an outside source. Keep it in Review on the web.", refOr(o, "This proposal")))
	}
	return apply()
}

func decideReject(a Actor, o Object, s Space) Decision {
	if a.Kind != ActorPerson {
		return refuse(CodePersonMustReview, fmt.Sprintf(
			"Only a person can reject %s. Review it on the web.", refOr(o, "this proposal")))
	}
	if a.Credential == CredentialAPIKey {
		return refuse(CodeKeyCannotReview, "API keys can propose but never keep or reject. Review it on the web.")
	}
	if a.Role == RoleViewer {
		return refuse(CodeViewer, fmt.Sprintf(
			"Viewers can propose but not reject in %s. Ask a member to review it.", spaceName(s)))
	}
	if !canKeep(a.Role, s.Rules) {
		return refuse(CodeOwnersKeep, fmt.Sprintf("Only owners review proposals in %s. Ask an owner.", spaceName(s)))
	}
	return apply()
}

func decideForget(a Actor, o Object, s Space) Decision {
	if a.Credential == CredentialAPIKey {
		return refuse(CodeKeyCannotForget, "API keys can't forget. Forget it on the web.")
	}
	switch a.Kind {
	case ActorPerson:
		if canForget(a, s) {
			return apply()
		}
		return refuse(CodeForgetNotAllowed, forgetNotAllowed(s))
	case ActorAgent:
		if a.autonomy() == AutonomyRead {
			return refuseReadOnly(a, s)
		}
		if !canForget(a, s) {
			return refuse(CodeForgetNotAllowed, forgetNotAllowed(s))
		}
		if a.PersonPresent && a.CanElicit {
			return Decision{Effect: EffectConfirm, Code: CodeConfirm, Message: fmt.Sprintf(
				"Forget %s everywhere? This can't be undone.", refOr(o, "this memory"))}
		}
	}
	return refuse(CodePersonMustForget, "Forget needs a person. Forget it on the web.")
}

// decideResolve: settling a conflict is a person's Keep of one answer
// (rule 11: one answer must win), so it follows Keep's rules: members and
// owners per the space's rules, a person on the web for decisions where
// the space needs one, and for keeping a quarantined side.
func decideResolve(a Actor, o Object, s Space) Decision {
	switch {
	case a.Kind != ActorPerson:
		return refuse(CodePersonMustReview, fmt.Sprintf(
			"Agents propose and people settle conflicts. %s waits in Review.", refOr(o, "This conflict")))
	case a.Credential == CredentialAPIKey:
		return refuse(CodeKeyCannotReview, "API keys can propose but never settle a conflict. Settle it in Review on the web.")
	case a.Role == RoleViewer:
		return refuse(CodeViewer, fmt.Sprintf("Viewers can't settle conflicts in %s. Ask a member.", spaceName(s)))
	case !canKeep(a.Role, s.Rules):
		return refuse(CodeOwnersKeep, fmt.Sprintf("Only owners settle conflicts in %s. Ask an owner.", spaceName(s)))
	case o.Decision && s.Rules.DecisionsNeedPersonOnWeb(s.Kind) && a.Assurance() != AssuranceHumanWeb:
		return refuse(CodeDecisionNeedsWeb, fmt.Sprintf(
			"Decisions in %s need a person on the web. Settle it in Review.", spaceName(s)))
	case o.External && a.Assurance() != AssuranceHumanWeb:
		return refuse(CodeExternalNeedsReview, fmt.Sprintf(
			"%s comes from an outside source. Settle it in Review on the web.", refOr(o, "This proposal")))
	}
	return apply()
}

// decideUndo: a person undoes their own decision (Review's ⌘Z), and any
// person who may keep can undo one of the judge's folds. Undo only puts
// back what was there, so it needs no more assurance than the person's
// role; Forget is never undoable and never reaches here.
func decideUndo(a Actor, o Object, s Space) Decision {
	switch {
	case a.Kind != ActorPerson:
		return refuse(CodePersonMustReview, "Only a person can undo a decision. Undo it in Review.")
	case a.Credential == CredentialAPIKey:
		return refuse(CodeKeyCannotReview, "API keys can't undo decisions. Undo it in Review.")
	case !o.UndoOwn && !o.UndoSystem:
		return refuse(CodeUndoByDecider, fmt.Sprintf(
			"Only the person who decided %s can undo it. Change it instead, or ask them.", refOr(o, "this")))
	case a.Role == RoleViewer:
		return refuse(CodeViewer, fmt.Sprintf("Viewers can't undo decisions in %s.", spaceName(s)))
	case !canKeep(a.Role, s.Rules):
		return refuse(CodeOwnersKeep, fmt.Sprintf("Only owners undo decisions in %s. Ask an owner.", spaceName(s)))
	}
	return apply()
}

// decideReviseBrief: people who may keep edit the Brief, and Dream
// rewrites it in the open (plan 25 §5.6). Agents propose memories; they
// never edit the Brief.
func decideReviseBrief(a Actor, s Space) Decision {
	switch a.Kind {
	case ActorPerson:
		switch {
		case a.Role == RoleViewer:
			return refuse(CodeViewer, fmt.Sprintf(
				"Viewers can read the Brief but not edit it in %s. Ask a member to edit it.", spaceName(s)))
		case !canKeep(a.Role, s.Rules):
			return refuse(CodeOwnersKeep, fmt.Sprintf("Only owners edit the Brief in %s. Ask an owner.", spaceName(s)))
		}
		return apply()
	case ActorDream:
		return apply()
	}
	return refuse(CodeBriefByPerson,
		"Agents propose memories and people edit the Brief. Edit it on the web or with the CLI.")
}

// decideConfigureTarget: people who may keep decide where a space
// compiles, overwrite a hand edit, or stop compiling a file.
func decideConfigureTarget(a Actor, s Space) Decision {
	if a.Kind != ActorPerson {
		return refuse(CodeTargetsByPerson, fmt.Sprintf(
			"Only people change where %s compiles. Change it on the web or with the CLI.", spaceName(s)))
	}
	switch {
	case a.Role == RoleViewer:
		return refuse(CodeViewer, fmt.Sprintf(
			"Viewers can't change where %s compiles. Ask a member.", spaceName(s)))
	case !canKeep(a.Role, s.Rules):
		return refuse(CodeOwnersKeep, fmt.Sprintf("Only owners change where %s compiles. Ask an owner.", spaceName(s)))
	}
	return apply()
}

// decideRequestCompile: a compile changes no words, so anyone who can see
// the space may ask for one, except an agent or key that can only read.
func decideRequestCompile(a Actor, s Space) Decision {
	if a.Kind == ActorAgent && a.autonomy() == AutonomyRead {
		return refuseReadOnly(a, s)
	}
	return apply()
}

// decideReport: a device (the person's daemon, or an agent's key it runs
// with) or the repository reports what it wrote or what it saw. Reports
// change no words: a hand edit becomes proposals only when a person pulls
// it.
func decideReport(a Actor, s Space) Decision {
	switch a.Kind {
	case ActorPerson, ActorRepository, ActorMemax:
		return apply()
	case ActorAgent:
		if a.autonomy() == AutonomyRead {
			return refuseReadOnly(a, s)
		}
		return apply()
	}
	return refuse(CodeTargetsByPerson, "Deliveries and hand edits are reported by a device or the repository.")
}

// keepCap is why a person (or the agent working for them) can't keep at
// all in this space, or "" when they can.
func keepCap(a Actor, s Space) string {
	switch {
	case a.Credential == CredentialAPIKey:
		return CodeAPIKey
	case a.Role == RoleViewer:
		return CodeViewer
	case !canKeep(a.Role, s.Rules):
		return CodeOwnersKeep
	}
	return ""
}

func canKeep(r Role, rules Rules) bool {
	switch r {
	case RoleOwner:
		return true
	case RoleMember:
		return rules.KeepBy() == WhoMembers
	}
	return false
}

func canForget(a Actor, s Space) bool {
	switch a.Role {
	case RoleOwner:
		return true
	case RoleMember:
		return a.CanForget || s.Rules.ForgetBy() == WhoMembers
	}
	return false
}

func apply() Decision { return Decision{Effect: EffectApply} }

func refuse(code, msg string) Decision {
	return Decision{Effect: EffectRefuse, Code: code, Message: msg}
}

func refuseReadOnly(a Actor, s Space) Decision {
	if a.Kind == ActorAgent {
		switch a.AgentStatus {
		case AgentConnected:
		case AgentPaused:
			return refuse(CodeAgentPaused, fmt.Sprintf("%s is paused, so it can only read. Resume it in Agents.", actorName(a)))
		default:
			return refuse(CodeAgentNotConnected, fmt.Sprintf(
				"%s isn't connected to %s, so it can only read. Connect it in Agents.", actorName(a), spaceName(s)))
		}
	}
	if a.Credential == CredentialAPIKey {
		return refuse(CodeKeyReadOnly, fmt.Sprintf(
			"This API key can only read %s. Create a key that can propose in Settings.", spaceName(s)))
	}
	return refuse(CodeReadOnly, fmt.Sprintf("%s is read-only in %s. Change it in Agents.", actorName(a), spaceName(s)))
}

func downgrade(code string, a Actor, o Object, s Space, quarantine bool) Decision {
	var msg string
	switch code {
	case CodeIntegration:
		msg = fmt.Sprintf("Sent to Review: what arrives by %s is always reviewed.", a.Via)
	case CodeImport:
		msg = "Sent to Review: imported memories are reviewed before they're kept."
	case CodeSystem:
		msg = "Sent to Review: Dream proposes and people keep."
	case CodeRepository:
		msg = "Sent to Review: changes from the repository are reviewed before they're kept."
	case CodeAPIKey:
		msg = "Sent to Review: API keys propose and people keep."
	case CodeViewer:
		msg = fmt.Sprintf("Sent to Review: viewers propose and members keep in %s.", spaceName(s))
	case CodeOwnersKeep:
		msg = fmt.Sprintf("Sent to Review: only owners keep in %s.", spaceName(s))
	case CodeDecisionNeedsWeb:
		msg = fmt.Sprintf("Sent to Review: decisions in %s need a person on the web.", spaceName(s))
	case CodeExternalSource:
		msg = "Sent to Review: it cites an outside source."
	case CodeContradicts:
		msg = "Sent to Review: it contradicts a decision in force."
	case CodeTouchesDecision:
		msg = "Sent to Review: it touches a decision in force, so a person checks it first."
	case CodeEditsPersonKept:
		msg = fmt.Sprintf("Sent to Review: a person kept %s, so changes to it need a person.", refOr(o, "this memory"))
	case CodeAutonomyPropose:
		msg = fmt.Sprintf("Sent to Review: %s proposes in %s.", actorName(a), spaceName(s))
	case CodePersonProposed:
		msg = "Sent to Review."
	}
	return Decision{Effect: EffectPropose, Code: code, Message: msg, Quarantine: quarantine}
}

func forgetNotAllowed(s Space) string {
	return fmt.Sprintf("Only owners can forget in %s. Ask an owner to forget it.", spaceName(s))
}

func actorName(a Actor) string {
	if a.Name != "" {
		return a.Name
	}
	if a.Kind == ActorAgent {
		return "This agent"
	}
	return "You"
}

func spaceName(s Space) string {
	if s.Name != "" {
		return s.Name
	}
	return "this space"
}

func refOr(o Object, fallback string) string {
	if o.Ref != "" {
		return o.Ref
	}
	return fallback
}

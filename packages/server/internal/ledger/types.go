package ledger

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Section is where a memory sits in the Brief.
type Section string

// The sections.
const (
	SectionDecisions    Section = "decisions"
	SectionConventions  Section = "conventions"
	SectionPreferences  Section = "preferences"
	SectionOpenQuestion Section = "open_question"
)

// Sections lists every section in Brief order.
var Sections = []Section{SectionDecisions, SectionConventions, SectionPreferences, SectionOpenQuestion}

// Valid reports whether s is a known section.
func (s Section) Valid() bool { return slices.Contains(Sections, s) }

// Kind is fact or decision. A decision carries DecisionFields.
type Kind string

// The kinds.
const (
	KindFact     Kind = "fact"
	KindDecision Kind = "decision"
)

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool { return k == KindFact || k == KindDecision }

// DecisionFields are the extra fields of a decision (HANDOFF §4).
type DecisionFields struct {
	Why          string           `json:"why,omitempty"`
	Options      []DecisionOption `json:"options,omitempty"`
	Consequences string           `json:"consequences,omitempty"`
	Area         string           `json:"area,omitempty"`
	// Status is in_force, superseded or open.
	Status string `json:"status,omitempty"`
}

// DecisionOption is one option a decision considered.
type DecisionOption struct {
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// The decision statuses.
const (
	DecisionInForce    = "in_force"
	DecisionSuperseded = "superseded"
	DecisionOpen       = "open"
)

// SourceKind is what a source points at.
type SourceKind string

// The source kinds.
const (
	SourceSession SourceKind = "session"
	SourcePR      SourceKind = "pr"
	SourceFile    SourceKind = "file"
	SourceURL     SourceKind = "url"
	SourceIssue   SourceKind = "issue"
	SourceEmail   SourceKind = "email"
	SourceNote    SourceKind = "note"
	SourceImport  SourceKind = "import"
	// SourceMemory cites a memory in the same space ("M-0219"): a kept
	// Ask answer cites the memories it came from. Its trust is the cited
	// memory's, whatever the caller says (migration 041).
	SourceMemory SourceKind = "memory"
)

// SourceKinds lists every source kind.
var SourceKinds = []SourceKind{SourceSession, SourcePR, SourceFile, SourceURL, SourceIssue, SourceEmail, SourceNote, SourceImport, SourceMemory}

// Valid reports whether k is a known source kind.
func (k SourceKind) Valid() bool { return slices.Contains(SourceKinds, k) }

// alwaysExternal reports whether content of this kind is third-party
// whatever the caller says: a web page, an email, an issue comment.
func (k SourceKind) alwaysExternal() bool {
	return k == SourceURL || k == SourceEmail || k == SourceIssue
}

// defaultTrust is the class assumed when the caller doesn't give one.
// It errs low: an import is external until someone says otherwise.
func (k SourceKind) defaultTrust(actor policy.Trust) policy.Trust {
	switch k {
	case SourceURL, SourceEmail, SourceIssue, SourceImport:
		return policy.TrustExternal
	case SourcePR, SourceFile:
		return policy.TrustRepository
	case SourceNote:
		return policy.TrustAgentOwnWork
	}
	return actor // a session: the actor's own work
}

// SourceInput is a source cited by a new memory.
type SourceInput struct {
	Kind SourceKind `json:"kind"`
	// Ref is what people see: "PR #212", "go.mod:14", "N-0882".
	Ref string `json:"ref"`
	// URI is the machine locator, if any: a URL, a repo path.
	URI string `json:"uri,omitempty"`
	// Locator holds structured position data ({path, line, commit}, …).
	Locator json.RawMessage `json:"locator,omitempty"`
	// Trust is the caller's class for the source. It can lower the
	// default but never raise a source above what the actor could write
	// itself, and URL, email and issue sources are always external.
	Trust policy.Trust `json:"trust,omitempty"`
	// Quote is the supporting excerpt. It is purged when the memory is
	// forgotten.
	Quote       string `json:"quote,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

// Source is a stored source.
type Source struct {
	ID          uuid.UUID       `json:"id"`
	Kind        SourceKind      `json:"kind"`
	Ref         string          `json:"ref"`
	URI         string          `json:"uri,omitempty"`
	Locator     json.RawMessage `json:"locator"`
	External    bool            `json:"external"`
	Trust       policy.Trust    `json:"trust"`
	Quote       string          `json:"quote,omitempty"`
	ContentHash string          `json:"content_hash,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// Memory is the projection of one memory: its current statement and
// state. Field names follow the planned /v2 JSON contract.
type Memory struct {
	ID       uuid.UUID `json:"id"`
	Ref      string    `json:"ref"`
	SpaceID  uuid.UUID `json:"space_id"`
	TenantID uuid.UUID `json:"tenant_id"`
	// Statement is the current version's words; empty once forgotten.
	Statement string  `json:"statement"`
	Section   Section `json:"section"`
	Kind      Kind    `json:"kind"`
	// State is the displayed state, derived from Lifecycle and Flags.
	State     lifecycle.Mark      `json:"state"`
	Lifecycle lifecycle.Lifecycle `json:"lifecycle"`
	Flags     lifecycle.Flags     `json:"flags"`
	Trust     policy.Trust        `json:"trust"`
	// Version is the current statement version. Send it back as the
	// expected version (If-Match) to edit.
	Version          int             `json:"version"`
	StaleAfter       *time.Time      `json:"stale_after,omitempty"`
	Conditions       json.RawMessage `json:"conditions"`
	Decision         *DecisionFields `json:"decision,omitempty"`
	Applies          json.RawMessage `json:"scope"`
	ValidFrom        *time.Time      `json:"valid_from,omitempty"`
	ValidTo          *time.Time      `json:"valid_to,omitempty"`
	CreatedReceiptID uuid.UUID       `json:"created_receipt_id"`
	LastReceiptID    uuid.UUID       `json:"last_receipt_id"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	// Sources is filled by GetMemory and Apply, not by ListMemories.
	Sources []Source `json:"sources,omitempty"`
	// Links are the memory's active links, both ways: what it was folded
	// into, what it supersedes or conflicts with, and what points at it.
	Links []Link `json:"links,omitempty"`
	// Updates is the memory this one would replace (an active
	// `supersedes` link from it), with its current words: Review renders
	// the pair as a diff ("Updates M-0156").
	Updates *LinkedMemory `json:"updates,omitempty"`
	// Judge is the judge's verdict on the current version, or, for a
	// proposal it hasn't judged yet, state "working".
	Judge *JudgeInfo `json:"judge,omitempty"`

	seq           int64
	streamVersion int
}

func (m *Memory) state() lifecycle.State {
	return lifecycle.State{Lifecycle: m.Lifecycle, Flags: m.Flags}
}

// MemoryVersion is one version of a memory's statement.
type MemoryVersion struct {
	Version int `json:"version"`
	// Statement is empty once the memory is forgotten.
	Statement string    `json:"statement"`
	ReceiptID uuid.UUID `json:"receipt_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Space is a space in a scope, with the scope's role in it.
type Space struct {
	ID       uuid.UUID        `json:"id"`
	TenantID uuid.UUID        `json:"tenant_id"`
	Slug     string           `json:"slug"`
	Name     string           `json:"name"`
	Kind     policy.SpaceKind `json:"kind"`
	Role     policy.Role      `json:"role"`
	// Repository is the repository the space compiles for, if any.
	Repository string `json:"repository,omitempty"`
	// V2EnabledAt is when the space switched to the V2 record (migration
	// 033); nil while it is on V1 (internal/spacemode).
	V2EnabledAt *time.Time `json:"v2_enabled_at,omitempty"`
}

// Action is the past-tense verb a receipt records.
type Action string

// The receipt actions this package writes. The SQL CHECK on
// receipts.action also admits the verbs of later commands (merged,
// flagged, verified, forgot, compiled, …).
const (
	ActionProposed Action = "proposed"
	ActionKept     Action = "kept"
	ActionEdited   Action = "edited"
	ActionRejected Action = "rejected"

	// The Brief, target and compile verbs (migration 029).
	ActionRevised     Action = "revised"     // a Brief: a new version
	ActionConfigured  Action = "configured"  // a target: added or changed
	ActionRequested   Action = "requested"   // a target: a compile asked for
	ActionCompiled    Action = "compiled"    // a compile run: recorded
	ActionDelivered   Action = "delivered"   // a compile run: on disk or merged
	ActionObserved    Action = "observed"    // a target: a hand edit seen
	ActionPulled      Action = "pulled"      // a target: a hand edit turned into proposals
	ActionOverwritten Action = "overwritten" // a target: a hand edit overwritten
	ActionStopped     Action = "stopped"     // a target: compiling stopped

	// The judge, conflicts and Undo (migration 035; merged, flagged,
	// resolved, faded and undid were in 028's list).
	ActionMerged     Action = "merged"     // a proposal folded into another memory (a duplicate, a re-proposal)
	ActionFlagged    Action = "flagged"    // the conflict flag set
	ActionLinked     Action = "linked"     // linked to the memory it updates or supersedes
	ActionJudged     Action = "judged"     // the judge's verdict, when it changed nothing
	ActionResolved   Action = "resolved"   // a conflict settled
	ActionSuperseded Action = "superseded" // a kept decision gave way to a newer one
	ActionFaded      Action = "faded"      // a kept memory faded (here: a fact that lost a conflict)
	ActionUndid      Action = "undid"      // a command undone; source names the receipt

	// Rule 11's two holes, closed (migration 042).
	ActionReturned Action = "returned" // the judge put a Write agent's write back in Review; source names the decision
	ActionDrafted  Action = "drafted"  // words for a kept memory that wait for the judge before they replace the words in force
)

// The receipts.object_kind values this package writes.
const (
	ObjectMemory  = "memory"
	ObjectBrief   = "brief"
	ObjectTarget  = "target"
	ObjectCompile = "compile"
)

// ReceiptSource is a receipt's reference to where a change came from.
// It is a pointer, never a quote.
type ReceiptSource struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

// Receipt is one entry of the append-only log. It never holds memory
// text.
type Receipt struct {
	ID            uuid.UUID        `json:"id"`
	Seq           int64            `json:"seq"`
	TenantID      uuid.UUID        `json:"tenant_id"`
	SpaceID       uuid.UUID        `json:"space_id"`
	ObjectKind    string           `json:"object_kind"`
	ObjectID      uuid.UUID        `json:"object_id"`
	ObjectRef     string           `json:"object_ref"`
	Action        Action           `json:"action"`
	ActorKind     policy.ActorKind `json:"actor_kind"`
	ActorID       *uuid.UUID       `json:"actor_id,omitempty"`
	Agent         string           `json:"agent,omitempty"`
	Via           policy.Via       `json:"via"`
	Assurance     policy.Assurance `json:"assurance,omitempty"`
	SessionRef    string           `json:"session_ref,omitempty"`
	Source        *ReceiptSource   `json:"source,omitempty"`
	Reason        string           `json:"reason,omitempty"`
	OccurredAt    time.Time        `json:"occurred_at"`
	RecordedAt    time.Time        `json:"recorded_at"`
	StreamID      uuid.UUID        `json:"stream_id"`
	StreamVersion int              `json:"stream_version"`
}

// Actor is who issues a command.
type Actor struct {
	Kind policy.ActorKind
	// ID is the user (person) or agent connection (agent). It is uuid.Nil
	// for Dream, Memax and the repository.
	ID uuid.UUID
	// Name is shown in policy messages ("Codex is read-only in …").
	Name string
	// Agent is the agent slug the action came through ("claude-code"):
	// the agent itself, or the agent a person confirmed a Keep in. It
	// lets a receipt say "kept by you via CC".
	Agent string
	// Autonomy is the agent's level in the space (or an API key's scope).
	Autonomy   policy.Autonomy
	Credential policy.Credential
	// PersonPresent and CanElicit describe the agent's client, for
	// in-agent confirmation.
	PersonPresent bool
	CanElicit     bool
}

// Outcome is what Apply did.
type Outcome string

// The outcomes.
const (
	// OutcomeApplied: the command took effect as asked (a Write-level
	// agent's propose is kept and reports applied).
	OutcomeApplied Outcome = "applied"
	// OutcomeProposed: the write went to Review as a proposal.
	OutcomeProposed Outcome = "proposed"
	// OutcomeNeedsConfirmation: the proposal is written, and the agent
	// should ask the person to keep it (MCP elicitation). An accepted
	// confirmation is a Keep by that person.
	OutcomeNeedsConfirmation Outcome = "needs_confirmation"
	// OutcomeRefused: nothing was written; Policy says why.
	OutcomeRefused Outcome = "refused"
)

// Result is Apply's answer.
type Result struct {
	Outcome Outcome `json:"outcome"`
	// Policy is the decision that shaped the outcome: why a write was
	// refused or sent to Review, and whether it is quarantined.
	Policy policy.Decision `json:"policy"`
	// Memory is the memory's projection after the command: for an edit
	// downgraded to a proposal, the new proposal. Nil when refused.
	Memory *Memory `json:"memory,omitempty"`
	// Connection is the agent connection's projection after an agent
	// command. Nil when refused, and for memory commands.
	Connection *Connection `json:"connection,omitempty"`
	// Receipts are the receipts the command wrote, in order.
	Receipts []Receipt `json:"receipts,omitempty"`
	// Replayed is set when the idempotency key had already been applied:
	// nothing new was written, and Receipts are the original ones.
	Replayed bool `json:"replayed,omitempty"`

	// The Brief, target and compile commands set these.
	Brief   *Brief      `json:"brief,omitempty"`
	Target  *Target     `json:"target,omitempty"`
	Compile *CompileRun `json:"compile,omitempty"`
	// Observations are the hand edits a command recorded or resolved.
	Observations []Observation `json:"observations,omitempty"`
	// Proposals are the memories a drift pull proposed, in file order.
	Proposals []Memory `json:"proposals,omitempty"`
	// Memories are every memory a conflict resolution or an undo changed,
	// this side first.
	Memories []Memory `json:"memories,omitempty"`
	// Gate is the decision gate's projection after a gate command.
	Gate *Gate `json:"gate,omitempty"`
	// Tombstone is what a Forget did (the forgotten memory's tombstone).
	Tombstone *Tombstone `json:"tombstone,omitempty"`
	// ForgetRequest is an agent's request to forget, after RequestForget
	// or DeclineForget.
	ForgetRequest *ForgetRequest `json:"forget_request,omitempty"`
	// Edition is a Dream edition, after PublishEdition (dream.go).
	Edition *DreamEdition `json:"edition,omitempty"`
	// DreamAction is one of Dream's actions, after UndoDreamAction.
	DreamAction *DreamAction `json:"dream_action,omitempty"`
	// Unchanged is set when the command found nothing to do (an
	// observation that matches what was delivered, a delivery already
	// acknowledged): nothing was written and no receipt exists.
	Unchanged bool `json:"unchanged,omitempty"`
}

func outcomeFor(e policy.Effect) Outcome {
	switch e {
	case policy.EffectApply:
		return OutcomeApplied
	case policy.EffectPropose:
		return OutcomeProposed
	case policy.EffectConfirm:
		return OutcomeNeedsConfirmation
	}
	return OutcomeRefused
}

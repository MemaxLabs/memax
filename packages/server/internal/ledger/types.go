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
)

// SourceKinds lists every source kind.
var SourceKinds = []SourceKind{SourceSession, SourcePR, SourceFile, SourceURL, SourceIssue, SourceEmail, SourceNote, SourceImport}

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

	seq           int64
	streamVersion int
}

func (m *Memory) state() lifecycle.State {
	return lifecycle.State{Lifecycle: m.Lifecycle, Flags: m.Flags}
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
)

// ObjectMemory is the receipts.object_kind of a memory.
const ObjectMemory = "memory"

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
	// Receipts are the receipts the command wrote, in order.
	Receipts []Receipt `json:"receipts,omitempty"`
	// Replayed is set when the idempotency key had already been applied:
	// nothing new was written, and Receipts are the original ones.
	Replayed bool `json:"replayed,omitempty"`
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

package ledger

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Forget (plan 25 §5.13, rule 7; migration 043). See forget.go.

// The Forget receipt verbs. forgot was in 028's list.
const (
	ActionForgot          Action = "forgot"           // a memory (or a gate's words, or a whole space) forgotten
	ActionPurged          Action = "purged"           // a forgotten memory's words left another object (a target's drift evidence, an older Brief)
	ActionForgetRequested Action = "forget_requested" // an agent asked a person to forget a memory
	ActionForgetDeclined  Action = "forget_declined"  // a person kept it instead
)

// ObjectSpace is the receipts' object_kind for a whole space, and SpaceObjectRef
// its object_ref (a space has no display ID, and its name is the person's
// words, which receipts keep forever).
const (
	ObjectSpace    = "space"
	SpaceObjectRef = "space"
)

// The Forget commands.
const (
	CommandForget        CommandName = "forget"
	CommandForgetSpace   CommandName = "forget_space"
	CommandRequestForget CommandName = "request_forget"
	CommandDeclineForget CommandName = "decline_forget"
	CommandReapplyForget CommandName = "reapply_forget"
)

// MaxNoteRunes bounds the note a person leaves on a tombstone.
const MaxNoteRunes = 500

// MaxCarried bounds how many memories one Forget takes with it.
const MaxCarried = 200

// MaxWaitingForgetRequests is how many forget requests one agent may have
// waiting in a space at a time (fair use, like MaxWaitingGates).
const MaxWaitingForgetRequests = 20

// Forget forgets a memory everywhere (rule 7): its words leave the record,
// every compiled file and every cache, every agent that read it is told,
// and a tombstone stays. Nothing undoes it.
type Forget struct {
	Meta
	// Memory is a display ID ("M-0201") or a uuid.
	Memory string
	// ExpectedVersion is required (If-Match): the version the person saw,
	// so nobody forgets words they haven't read.
	ExpectedVersion int
	// Note is the person's own note. It stays on the tombstone, so it must
	// not repeat the words (the confirmation says so).
	Note string
	// Carries are the display IDs the person saw going with it: proposals
	// folded into it or that would change it, and memories built from it
	// (a kept Ask answer citing it). It must name exactly those Forget
	// finds, or the command is refused with *ForgetCarriesError, which
	// lists them.
	Carries []string
}

// Name implements Command.
func (*Forget) Name() CommandName { return CommandForget }

// ForgetSpace forgets everything in a space: every memory, as Forget
// does, and the words in its Brief, gates, targets' drift evidence and
// receipts' reasons. Retire also deletes the space's V2 rows, keeping only
// what is content-free (receipts, seals, tombstones), so V1 can delete the
// hub.
type ForgetSpace struct {
	Meta
	SpaceID uuid.UUID
	Retire  bool
}

// Name implements Command.
func (*ForgetSpace) Name() CommandName { return CommandForgetSpace }

// RequestForget is an agent's memax_forget on V2: it asks a person, who
// forgets the memory or keeps it, on the web. The reason goes into the
// receipt (and goes with the memory if it is forgotten).
type RequestForget struct {
	Meta
	Memory string
}

// Name implements Command.
func (*RequestForget) Name() CommandName { return CommandRequestForget }

// DeclineForget is a person keeping a memory an agent asked to forget.
type DeclineForget struct {
	Meta
	Memory string
}

// Name implements Command.
func (*DeclineForget) Name() CommandName { return CommandDeclineForget }

// ReapplyForget re-runs the purge of one Forget from the forget ledger,
// after a restore brought back what it forgot. Memax only.
type ReapplyForget struct {
	Meta
	Op ForgetLedgerOp
}

// Name implements Command.
func (*ReapplyForget) Name() CommandName { return CommandReapplyForget }

// Why a memory went with another one.
const (
	CarryFolded  = "folded"  // a proposal folded into it: a copy of its words
	CarryUpdates = "updates" // a proposal that would change it: its words, edited
	CarryCites   = "cites"   // built from it: a kept Ask answer (or any memory) citing it
	CarrySpace   = "space"   // everything in the space was forgotten
)

// Carried is a memory that goes with another one's Forget.
type Carried struct {
	ID        uuid.UUID           `json:"id"`
	Ref       string              `json:"ref"`
	Reason    string              `json:"reason"`
	With      string              `json:"with"`
	Lifecycle lifecycle.Lifecycle `json:"lifecycle"`
	Kind      Kind                `json:"kind"`
}

// ErrForgetCarries: the Forget didn't name exactly the memories that go
// with it (see *ForgetCarriesError).
var ErrForgetCarries = errors.New("ledger: forget carries other memories")

// ForgetCarriesError lists the memories a Forget would take with it, when
// the command didn't name exactly those. Show them, then send the Forget
// again with Carries set to their refs.
type ForgetCarriesError struct {
	Ref     string
	Carries []Carried
}

func (e *ForgetCarriesError) Error() string {
	refs := make([]string, len(e.Carries))
	for i, c := range e.Carries {
		refs[i] = c.Ref
	}
	return fmt.Sprintf("Forgetting %s also forgets %s, which carry its words. Confirm them too: send carries with those IDs.",
		e.Ref, strings.Join(refs, ", "))
}

// Is makes errors.Is(err, ErrForgetCarries) match.
func (e *ForgetCarriesError) Is(target error) bool { return target == ErrForgetCarries }

// sameRefs reports whether two lists name the same display IDs.
func sameRefs(a, b []string) bool {
	norm := func(in []string) []string {
		out := make([]string, 0, len(in))
		for _, r := range in {
			if p, n, ok := ParseRef(r); ok {
				out = append(out, FormatRef(p, n))
			} else {
				out = append(out, strings.TrimSpace(r))
			}
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	return slices.Equal(norm(a), norm(b))
}

// The tombstone statuses.
const (
	TombstonePropagating = "propagating"
	TombstoneDone        = "done"
)

// Tombstone is what Forget did to one object (rule 7): its ID, who asked
// and when, what went with it, and how the forget reached each copy.
// Never words.
type Tombstone struct {
	ID       uuid.UUID `json:"id"`
	OpID     uuid.UUID `json:"op_id"`
	Ref      string    `json:"ref"`
	Kind     string    `json:"kind"`
	ObjectID uuid.UUID `json:"object_id"`
	SpaceID  uuid.UUID `json:"space_id"`
	TenantID uuid.UUID `json:"tenant_id"`
	// Carried says why it went with another memory's Forget (folded,
	// updates, cites, space), and Primary is that memory's ref; both empty
	// for the memory a person forgot.
	Carried string `json:"carried,omitempty"`
	Primary string `json:"primary,omitempty"`
	// With are the other refs forgotten in the same Forget.
	With        []string          `json:"with"`
	Note        string            `json:"note,omitempty"`
	By          TombstoneActor    `json:"by"`
	RequestedBy *TombstoneAgent   `json:"requested_by,omitempty"`
	Via         policy.Via        `json:"via"`
	ReceiptID   uuid.UUID         `json:"receipt_id"`
	ForgottenAt time.Time         `json:"forgotten_at"`
	KeptAt      *time.Time        `json:"kept_at,omitempty"`
	ReadsBefore int               `json:"reads_before"`
	Gone        TombstoneGone     `json:"gone"`
	Status      string            `json:"status"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	DurationMS  *int64            `json:"duration_ms,omitempty"`
	ReappliedAt *time.Time        `json:"reapplied_at,omitempty"`
	Steps       []TombstoneStep   `json:"steps"`
	Unreachable []UnreachableCopy `json:"unreachable"`
}

// TombstoneActor is who asked to forget it: a person, or Memax
// re-applying the forget ledger.
type TombstoneActor struct {
	Kind string     `json:"kind"`
	ID   *uuid.UUID `json:"id,omitempty"`
}

// TombstoneAgent is an agent connection on a tombstone.
type TombstoneAgent struct {
	ConnectionID uuid.UUID `json:"connection_id"`
	Agent        string    `json:"agent,omitempty"`
	DisplayName  string    `json:"display_name,omitempty"`
	State        string    `json:"state,omitempty"`
}

// TombstoneGone counts what Forget removed from Memax.
type TombstoneGone struct {
	Versions   int `json:"versions"`
	Sources    int `json:"sources"`
	Embeddings int `json:"embeddings"`
	Verdicts   int `json:"verdicts"`
	// ModelVerdicts are the verdicts a model gave (the judge's LLM saw
	// the words).
	ModelVerdicts int `json:"model_verdicts"`
	Gates         int `json:"gates"`
	// Files counts the compiled files that held it when it was forgotten.
	Files int `json:"files"`
	// Memories counts the memories forgotten whole (a space's Forget).
	Memories int `json:"memories,omitempty"`
}

// The step kinds and statuses of a tombstone.
const (
	StepAsked     = "asked"
	StepRemoved   = "removed"
	StepTarget    = "target"
	StepArtifacts = "artifacts"
	StepCaches    = "caches"
	StepLedger    = "ledger"
	StepAgent     = "agent"

	StepDone        = "done"
	StepWaiting     = "waiting"
	StepHeld        = "held"
	StepFailed      = "failed"
	StepUnreachable = "unreachable"
)

// Why a step waits, is held or can't be reached.
const (
	StepReasonHandEdit     = "hand_edit"
	StepReasonStopped      = "stopped"
	StepReasonCopy         = "copy"
	StepReasonDelivery     = "delivery"
	StepReasonCompiling    = "compiling"
	StepReasonPaused       = "paused"
	StepReasonDisconnected = "disconnected"
	StepReasonNotYet       = "next_read"
)

// TombstoneStep is one step of how it was forgotten.
type TombstoneStep struct {
	Kind    string           `json:"kind"`
	Status  string           `json:"status"`
	At      *time.Time       `json:"at,omitempty"`
	Target  *TombstoneTarget `json:"target,omitempty"`
	Compile string           `json:"compile,omitempty"`
	Agent   *TombstoneAgent  `json:"agent,omitempty"`
	Reason  string           `json:"reason,omitempty"`
	Count   *int             `json:"count,omitempty"`
	// ReadIt is set on an agent step when the agent read it (else it is
	// connected to the space, and may have it from a compiled file).
	ReadIt bool `json:"read_it,omitempty"`
}

// TombstoneTarget is the target a step rewrote.
type TombstoneTarget struct {
	ID       uuid.UUID  `json:"id"`
	Kind     TargetKind `json:"kind"`
	Label    string     `json:"label"`
	Delivery Delivery   `json:"delivery"`
}

// The kinds of copy Memax can't reach (the Tombstone page says so).
const (
	UnreachableGitHistory  = "git_history"
	UnreachableAgentMemory = "agent_memory"
	UnreachableBackups     = "backups"
	UnreachableLLM         = "llm"
	UnreachableHandEdits   = "hand_edits"
	UnreachableCopies      = "copies"
)

// UnreachableCopy is a copy Memax can't reach, as data the Tombstone page
// renders.
type UnreachableCopy struct {
	Kind string `json:"kind"`
	// Repositories and Files: git history of the committed files that held
	// it, and files with a hand edit Memax won't write over.
	Repositories []string `json:"repositories,omitempty"`
	Files        []string `json:"files,omitempty"`
	// Agents are the agents whose own memories may hold it.
	Agents []string `json:"agents,omitempty"`
	// Days is how long backups keep it.
	Days int `json:"days,omitempty"`
	// Processors are the model providers that saw the words.
	Processors []Processor `json:"processors,omitempty"`
	// Targets are copy-out targets a person pasted somewhere (ChatGPT).
	Targets []string `json:"targets,omitempty"`
}

// Processor is an outside model provider that processed a memory's words.
type Processor struct {
	Name          string `json:"name"`
	Purpose       string `json:"purpose"`
	ZeroRetention bool   `json:"zero_retention"`
}

// ForgetHonesty is what the Tombstone page says about copies Memax can't
// reach, from configuration (WithForgetHonesty).
type ForgetHonesty struct {
	// BackupDays is the point-in-time restore window (Neon PITR: 7 days).
	BackupDays int
	// Embeddings, Judge and Ask are the providers each purpose uses; a
	// zero Name means the purpose is off.
	Embeddings Processor
	Judge      Processor
	Ask        Processor
}

// DefaultBackupDays is Neon's point-in-time restore window (plan §5.13).
const DefaultBackupDays = 7

// WithForgetHonesty sets what tombstones say about copies Memax can't
// reach.
func WithForgetHonesty(h ForgetHonesty) Option { return func(l *Ledger) { l.honesty = h } }

// ForgetRequest is an agent's request that a person forget a memory.
type ForgetRequest struct {
	ID          uuid.UUID      `json:"id"`
	MemoryID    uuid.UUID      `json:"memory_id"`
	Ref         string         `json:"ref"`
	SpaceID     uuid.UUID      `json:"space_id"`
	Agent       TombstoneAgent `json:"agent"`
	SessionRef  string         `json:"session_ref,omitempty"`
	Reason      string         `json:"reason,omitempty"`
	Status      string         `json:"status"`
	DecidedBy   *uuid.UUID     `json:"decided_by,omitempty"`
	DecidedAt   *time.Time     `json:"decided_at,omitempty"`
	ReceiptID   uuid.UUID      `json:"receipt_id"`
	RequestedAt time.Time      `json:"requested_at"`
}

// The forget request statuses.
const (
	ForgetRequestWaiting   = "waiting"
	ForgetRequestForgotten = "forgotten"
	ForgetRequestDeclined  = "declined"
)

// The notice kinds.
const (
	NoticeForgotten      = "forgotten"
	NoticeSpaceForgotten = "space_forgotten"
)

// Notice is something an agent connection is told once, on its next MCP
// response: memories forgotten since it read them.
type Notice struct {
	ID      uuid.UUID `json:"id"`
	SpaceID uuid.UUID `json:"space_id"`
	OpID    uuid.UUID `json:"op_id"`
	Kind    string    `json:"kind"`
	Refs    []string  `json:"refs"`
	ReadIt  bool      `json:"read_it"`
	At      time.Time `json:"at"`
	Space   string    `json:"space,omitempty"`
}

// QueueForget is the River queue forget propagation runs on.
const QueueForget = "forget"

// ForgetPropagateArgs is the River job that carries a Forget to every
// copy (plan §5.13 step 2): recompile each target, re-render the old
// artifacts, purge the caches, copy the forget ledger. Ids only.
type ForgetPropagateArgs struct {
	OpID    uuid.UUID `json:"op_id"`
	SpaceID uuid.UUID `json:"space_id"`
}

// Kind implements river.JobArgs.
func (ForgetPropagateArgs) Kind() string { return "forget_propagate" }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (ForgetPropagateArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueForget,
		// The SLO is a minute; retries back off from seconds, and a step
		// that keeps failing shows as failed on the tombstone.
		MaxAttempts: 20,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
			rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
			rivertype.JobStateScheduled, rivertype.JobStateRetryable,
		}},
	}
}

// ForgetLedgerOp is one Forget as the forget ledger keeps it: ids, refs
// and times, never words. The propagation job copies it to object storage,
// where a database restore can't take it back, and cmd/v2-reapply-forgets
// re-applies it.
type ForgetLedgerOp struct {
	Version     int                 `json:"version"`
	OpID        uuid.UUID           `json:"op_id"`
	SpaceID     uuid.UUID           `json:"space_id"`
	TenantID    uuid.UUID           `json:"tenant_id"`
	Kind        string              `json:"kind"`
	Retired     bool                `json:"retired,omitempty"`
	ForgottenAt time.Time           `json:"forgotten_at"`
	Entries     []ForgetLedgerEntry `json:"entries"`
}

// ForgetLedgerEntry is one forgotten object of an op.
type ForgetLedgerEntry struct {
	TombstoneID uuid.UUID `json:"tombstone_id"`
	ObjectKind  string    `json:"object_kind"`
	ObjectID    uuid.UUID `json:"object_id"`
	Ref         string    `json:"ref"`
	Carried     string    `json:"carried,omitempty"`
}

// ForgetLedgerKey is where an op's copy lives in object storage.
func ForgetLedgerKey(spaceID, opID uuid.UUID) string {
	return fmt.Sprintf("v2/forget-ledger/%s/%s.json", spaceID, opID)
}

// ForgetLedgerPrefix lists every op of the forget ledger.
const ForgetLedgerPrefix = "v2/forget-ledger/"

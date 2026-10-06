package ledger

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The Brief, compile targets, compile runs and drift observations (plan 25
// §5.7, migration 029). Field names follow the /v2 JSON contract.

// BriefItem is one item of a Brief section: a memory reference, or a short
// line of connective prose that cites the memories it rests on. Exactly
// one of Ref and Text is set.
type BriefItem struct {
	Ref   string   `json:"ref,omitempty"`
	Text  string   `json:"text,omitempty"`
	Cites []string `json:"cites,omitempty"`
}

// BriefSection is one `## heading` of the Brief, in order.
type BriefSection struct {
	// Key is the compiler's section key: decisions, conventions,
	// preferences, open, overview, or a custom key.
	Key     string      `json:"key"`
	Heading string      `json:"heading"`
	Items   []BriefItem `json:"items"`
}

// Brief is one version (B-) of a space's Brief.
type Brief struct {
	// ID is the Brief's id: one per space, the same for every version.
	ID uuid.UUID `json:"id"`
	// VersionID is this version's own id.
	VersionID uuid.UUID `json:"version_id"`
	Ref       string    `json:"ref"`
	Version   int       `json:"version"`
	SpaceID   uuid.UUID `json:"space_id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	// ParentVersion is the version it was revised from (0 for the first).
	ParentVersion int            `json:"parent_version,omitempty"`
	Title         string         `json:"title"`
	Summary       string         `json:"summary,omitempty"`
	Sections      []BriefSection `json:"sections"`
	// Facts counts the distinct memories the version references or cites.
	Facts int `json:"facts"`
	// Current is set on the version in force.
	Current   bool      `json:"current"`
	ReceiptID uuid.UUID `json:"receipt_id"`
	// Receipt is the author receipt: who wrote this version, and why.
	Receipt   *Receipt  `json:"receipt,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// TargetKind is a compile target's adapter kind (packages/compiler).
type TargetKind string

// The target kinds, in the compiler's order.
const (
	TargetAgentsMD    TargetKind = "agents_md"
	TargetClaudeMD    TargetKind = "claude_md"
	TargetCursorMDC   TargetKind = "cursor_mdc"
	TargetChatGPT     TargetKind = "chatgpt"
	TargetGeminiMD    TargetKind = "gemini_md"
	TargetCopilot     TargetKind = "copilot"
	TargetWindsurf    TargetKind = "windsurf"
	TargetClaudeRules TargetKind = "claude_rules"
)

// TargetKinds lists every kind.
var TargetKinds = []TargetKind{TargetAgentsMD, TargetClaudeMD, TargetCursorMDC, TargetChatGPT,
	TargetGeminiMD, TargetCopilot, TargetWindsurf, TargetClaudeRules}

// DefaultTargetKinds are the Phase 1 targets: the compiler's
// DEFAULT_TARGET_KINDS (the canonical AGENTS.md, the CLAUDE.md shim,
// scoped Cursor rules and the ChatGPT copy-out).
var DefaultTargetKinds = []TargetKind{TargetAgentsMD, TargetClaudeMD, TargetCursorMDC, TargetChatGPT}

// Valid reports whether k is a known kind.
func (k TargetKind) Valid() bool { return slices.Contains(TargetKinds, k) }

// role is the adapter's role in the compiler: the canonical file, a shim
// importing it, path-scoped files, or copy-out text.
func (k TargetKind) role() string {
	switch k {
	case TargetAgentsMD:
		return "canonical"
	case TargetClaudeMD, TargetGeminiMD:
		return "shim"
	case TargetChatGPT:
		return "copy"
	}
	return "scoped"
}

// DefaultPath is where the compiler writes the kind by default: a file
// for AGENTS.md and the shims, a directory for scoped rules, "" for the
// ChatGPT copy-out.
func (k TargetKind) DefaultPath() string {
	switch k {
	case TargetAgentsMD:
		return "AGENTS.md"
	case TargetClaudeMD:
		return "CLAUDE.md"
	case TargetGeminiMD:
		return "GEMINI.md"
	case TargetCursorMDC:
		return ".cursor/rules"
	case TargetCopilot:
		return ".github/instructions"
	case TargetWindsurf:
		return ".devin/rules"
	case TargetClaudeRules:
		return ".claude/rules"
	}
	return ""
}

// Delivery is how a target's output reaches its readers.
type Delivery string

// The deliveries.
const (
	// DeliveryLocal: the daemon writes the file and acknowledges it.
	DeliveryLocal Delivery = "local"
	// DeliveryPR: the GitHub App opens a pull request (Phase 4).
	DeliveryPR Delivery = "pr"
	// DeliveryMCP: agents read the artifact over MCP; in sync once compiled.
	DeliveryMCP Delivery = "mcp"
	// DeliveryCopy: a person copies it out (ChatGPT); in sync once compiled.
	DeliveryCopy Delivery = "copy"
)

// Deliveries lists every delivery.
var Deliveries = []Delivery{DeliveryLocal, DeliveryPR, DeliveryMCP, DeliveryCopy}

// Valid reports whether d is a known delivery.
func (d Delivery) Valid() bool { return slices.Contains(Deliveries, d) }

// writesFiles reports whether a delivery waits for an acknowledgement.
func (d Delivery) writesFiles() bool { return d == DeliveryLocal || d == DeliveryPR }

// SyncState is where a target stands.
type SyncState string

// The sync states.
const (
	SyncInSync          SyncState = "in_sync"
	SyncCompiling       SyncState = "compiling"
	SyncPendingDelivery SyncState = "pending_delivery"
	SyncDrifted         SyncState = "drifted"
	SyncOff             SyncState = "off"
)

// SyncStates lists every state.
var SyncStates = []SyncState{SyncInSync, SyncCompiling, SyncPendingDelivery, SyncDrifted, SyncOff}

// IncludeMode is whether open questions compile.
type IncludeMode string

// The include modes.
const (
	IncludeKeptOnly    IncludeMode = "kept_only"
	IncludeKeptAndOpen IncludeMode = "kept_and_open"
)

// StaleMode is how stale facts compile.
type StaleMode string

// The stale modes.
const (
	StaleMark StaleMode = "mark"
	StaleOmit StaleMode = "omit"
)

// ScopedMode is whether AGENTS.md and the ChatGPT copy inline path-scoped
// facts in `### In <globs>` subsections, or leave them to scoped targets.
type ScopedMode string

// The scoped modes.
const (
	ScopedInline ScopedMode = "inline"
	ScopedOmit   ScopedMode = "omit"
)

// Size budgets, in bytes per file. The compiler caps the budget at each
// tool's own limit (Codex reads at most 32 KiB of AGENTS.md).
const (
	MinSizeBudget     = 1024
	MaxSizeBudget     = 32768
	DefaultSizeBudget = 25600
)

// TargetSettings is how a target is written.
type TargetSettings struct {
	Include    IncludeMode `json:"include"`
	Stale      StaleMode   `json:"stale"`
	SizeBudget int         `json:"size_budget"`
	// Scoped applies to agents_md and chatgpt only.
	Scoped ScopedMode `json:"scoped,omitempty"`
	// UserOwned applies to the shims only: the person owns the file, and
	// Memax manages one marked block in it.
	UserOwned bool `json:"user_owned,omitempty"`
}

// DeliveredFile is one file of a target's baseline: what Memax believes
// is on disk, by drift hash. Observation is set when the baseline is a
// hand edit Memax accepted (pulled or overwritten) rather than its own
// output.
type DeliveredFile struct {
	Path        string     `json:"path"`
	SHA256      string     `json:"sha256"`
	Observation *uuid.UUID `json:"observation,omitempty"`
}

// Delivered is what Memax believes is on disk for a target.
type Delivered struct {
	// Compile is the run whose output was delivered, if Memax wrote it.
	CompileID *uuid.UUID      `json:"compile_id,omitempty"`
	Compile   string          `json:"compile,omitempty"`
	SHA256    string          `json:"sha256"`
	Files     []DeliveredFile `json:"files"`
	At        *time.Time      `json:"at,omitempty"`
}

// Target is a compiled file (or copy-out) of a space.
type Target struct {
	ID       uuid.UUID  `json:"id"`
	SpaceID  uuid.UUID  `json:"space_id"`
	TenantID uuid.UUID  `json:"tenant_id"`
	Kind     TargetKind `json:"kind"`
	// Path is repository-relative; empty for the ChatGPT copy-out.
	Path string `json:"path,omitempty"`
	// Label is what people see: the path, or "ChatGPT project".
	Label     string         `json:"label"`
	Settings  TargetSettings `json:"settings"`
	Delivery  Delivery       `json:"delivery"`
	SyncState SyncState      `json:"sync_state"`
	// Version is the target's version (its ETag): send it back as If-Match.
	Version     int       `json:"version"`
	DirtyGen    int64     `json:"dirty_gen"`
	CompiledGen int64     `json:"compiled_gen"`
	DirtyAt     time.Time `json:"dirty_at"`
	// LastCompile is the latest run, whatever its status.
	LastCompile *CompileRun `json:"last_compile,omitempty"`
	Delivered   *Delivered  `json:"delivered,omitempty"`
	// OpenDrift counts files with a hand edit waiting to be resolved.
	OpenDrift        int       `json:"open_drift"`
	CreatedReceiptID uuid.UUID `json:"created_receipt_id"`
	LastReceiptID    uuid.UUID `json:"last_receipt_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	lastCompileID *uuid.UUID
}

// CompileStatus is a compile run's status.
type CompileStatus string

// The statuses.
const (
	CompileCompiled  CompileStatus = "compiled"
	CompileDelivered CompileStatus = "delivered"
	CompileFailed    CompileStatus = "failed"
)

// CompileStatuses lists every status.
var CompileStatuses = []CompileStatus{CompileCompiled, CompileDelivered, CompileFailed}

// CompiledOutput is one output of a compile run, without its content: a
// file (Path) or copy-out text (Label).
type CompiledOutput struct {
	Path             string   `json:"path,omitempty"`
	Label            string   `json:"label,omitempty"`
	SHA256           string   `json:"sha256"`
	DriftSHA256      string   `json:"drift_sha256"`
	Bytes            int      `json:"bytes"`
	Lines            int      `json:"lines"`
	Refs             []string `json:"refs"`
	Cites            []string `json:"cites"`
	DroppedForBudget []string `json:"dropped_for_budget"`
	UserOwned        bool     `json:"user_owned,omitempty"`
}

// CompileWarning is one warning the compiler gave.
type CompileWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

// CompileRun is one compile (C-) of one target.
type CompileRun struct {
	ID           uuid.UUID     `json:"id"`
	Ref          string        `json:"ref"`
	TargetID     uuid.UUID     `json:"target_id"`
	SpaceID      uuid.UUID     `json:"space_id"`
	Brief        string        `json:"brief"`
	BriefVersion int           `json:"brief_version"`
	Generation   int64         `json:"generation"`
	Status       CompileStatus `json:"status"`
	InputSHA256  string        `json:"input_sha256"`
	OutputSHA256 string        `json:"output_sha256,omitempty"`
	// DriftSHA256 is what a delivery is acknowledged against.
	DriftSHA256      string           `json:"drift_sha256,omitempty"`
	Bytes            int              `json:"bytes"`
	Lines            int              `json:"lines"`
	Refs             []string         `json:"refs"`
	DroppedForBudget []string         `json:"dropped_for_budget"`
	Files            []CompiledOutput `json:"files"`
	Warnings         []CompileWarning `json:"warnings"`
	Error            string           `json:"error,omitempty"`
	EnqueuedAt       time.Time        `json:"enqueued_at"`
	StartedAt        time.Time        `json:"started_at"`
	CompiledAt       time.Time        `json:"compiled_at"`
	DeliveredAt      *time.Time       `json:"delivered_at,omitempty"`
	ReceiptID        uuid.UUID        `json:"receipt_id"`

	// ArtifactKey is the object storage key of the content. It is never
	// sent to clients: content is served through the API, under scope.
	ArtifactKey string `json:"-"`
	seq         int64
}

// DriftChange is one change the compiler's parseBack found in a hand
// edit: an edit of a cited line, a new line, or a removed cited line.
// The JSON shape is the compiler's Change.
type DriftChange struct {
	Kind    string   `json:"kind"`
	Ref     string   `json:"ref,omitempty"`
	Refs    []string `json:"refs,omitempty"`
	OldText string   `json:"old_text,omitempty"`
	NewText string   `json:"new_text,omitempty"`
	OldLine int      `json:"old_line,omitempty"`
	NewLine int      `json:"new_line,omitempty"`
	Text    string   `json:"text,omitempty"`
	Line    int      `json:"line,omitempty"`
	Section *string  `json:"section,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Cites   []string `json:"cites,omitempty"`
}

// The change kinds.
const (
	ChangeEdit   = "edit"
	ChangeNew    = "new"
	ChangeRemove = "remove"
)

// DriftInfo is parseBack's drift metadata.
type DriftInfo struct {
	Changed           bool    `json:"changed"`
	HeaderEdited      bool    `json:"header_edited"`
	FrontmatterEdited bool    `json:"frontmatter_edited"`
	LayoutEdited      bool    `json:"layout_edited"`
	ManagedBlock      *string `json:"managed_block"`
	HiddenCharacters  int     `json:"hidden_characters"`
}

// ChangeSet is parseBack's answer for one file.
type ChangeSet struct {
	Changes []DriftChange `json:"changes"`
	Drift   DriftInfo     `json:"drift"`
}

// ObservationStatus is where an observation stands.
type ObservationStatus string

// The statuses.
const (
	ObservationOpen        ObservationStatus = "open"
	ObservationPulled      ObservationStatus = "pulled"
	ObservationOverwritten ObservationStatus = "overwritten"
	ObservationStopped     ObservationStatus = "stopped"
	ObservationDismissed   ObservationStatus = "dismissed"
)

// ObservationStatuses lists every status.
var ObservationStatuses = []ObservationStatus{ObservationOpen, ObservationPulled, ObservationOverwritten,
	ObservationStopped, ObservationDismissed}

// The observers.
const (
	ObserverDevice = "device"
	ObserverGitHub = "github"
)

// ChangeOutcome is what resolving a drift did with one change.
type ChangeOutcome struct {
	Kind string `json:"kind"`
	Line int    `json:"line"`
	// Ref is the memory the change was about (edits and removals).
	Ref string `json:"ref,omitempty"`
	// Outcome: proposed (a proposal was written), review (a removal waits
	// for a person to forget or exclude the memory), or skipped.
	Outcome string `json:"outcome"`
	// Proposal is the new proposal's display ID.
	Proposal string `json:"proposal,omitempty"`
	// Reason says why a change was skipped (a policy code, or too_long).
	Reason string `json:"reason,omitempty"`
}

// The change outcomes.
const (
	OutcomeChangeProposed = "proposed"
	OutcomeChangeReview   = "review"
	OutcomeChangeSkipped  = "skipped"
)

// DriftResolution records how an observation was resolved.
type DriftResolution struct {
	Mode      DriftMode       `json:"mode,omitempty"`
	ReceiptID uuid.UUID       `json:"receipt_id"`
	Changes   []ChangeOutcome `json:"changes,omitempty"`
	// SupersededBy is set when a newer observation of the same file
	// dismissed this one.
	SupersededBy *uuid.UUID `json:"superseded_by,omitempty"`
}

// Observation is a compiled file seen changed outside Memax.
type Observation struct {
	ID           uuid.UUID `json:"id"`
	TargetID     uuid.UUID `json:"target_id"`
	SpaceID      uuid.UUID `json:"space_id"`
	Path         string    `json:"path"`
	ObservedSHA  string    `json:"observed_sha256"`
	ObserverKind string    `json:"observer_kind"`
	ObserverID   string    `json:"observer_id"`
	Commit       string    `json:"commit,omitempty"`
	Bytes        int       `json:"bytes"`
	// BaseCompile is the compile the edit was compared against, if any.
	BaseCompileID *uuid.UUID        `json:"base_compile_id,omitempty"`
	BaseCompile   string            `json:"base_compile,omitempty"`
	BaseSHA256    string            `json:"base_sha256,omitempty"`
	Changes       ChangeSet         `json:"changeset"`
	Status        ObservationStatus `json:"status"`
	Resolution    *DriftResolution  `json:"resolution,omitempty"`
	ObservedAt    time.Time         `json:"observed_at"`
	ResolvedAt    *time.Time        `json:"resolved_at,omitempty"`
	ReceiptID     uuid.UUID         `json:"receipt_id"`

	// ArtifactKey is the object storage key of the observed content.
	ArtifactKey string `json:"-"`
	// actorID is the person who reported it (the person on the device),
	// and via the surface they reported it through.
	actorID *uuid.UUID
	via     policy.Via
}

// DriftMode is how a hand edit is resolved.
type DriftMode string

// The modes (plan 25 §5.7, HANDOFF rule 6).
const (
	// DriftPull turns the edit into proposals and accepts the file as it
	// is until they are decided.
	DriftPull DriftMode = "pull"
	// DriftOverwrite writes the compiled file over the edit.
	DriftOverwrite DriftMode = "overwrite"
	// DriftStop stops compiling the target.
	DriftStop DriftMode = "stop"
)

// TargetRef names a target for the compile sweeper.
type TargetRef struct {
	TargetID uuid.UUID
	SpaceID  uuid.UUID
}

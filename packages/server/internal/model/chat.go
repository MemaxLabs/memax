package model

import (
	"encoding/json"
	"time"
)

// Chat MVP types — plan 24 §"Feature 2 — Agent Chat" + §"Data
// model". Mirrors of the four chat-foundation tables. Constants
// are the Go-side canonical source for the SQL CHECK constraints
// in 007_chat_phase3_foundation.up.sql; tests pin them in
// lockstep so a typo on either side fails loud.

// ChatScopeType discriminates the agent's operating boundary.
// Mirror of internal/agent.ScopeType — the chat handler's
// create-session validator translates between the two.
const (
	ChatScopeTypeUserAllHubs = "user_all_hubs"
	ChatScopeTypeSingleHub   = "single_hub"
	ChatScopeTypeHubSubset   = "hub_subset"
)

// ChatSessionStatus is the per-session lifecycle. The lock
// primitive (status + active_message_id + locked_at) transitions
// via UPDATE ... WHERE status='idle'.
const (
	ChatSessionStatusIdle             = "idle"
	ChatSessionStatusRunning          = "running"
	ChatSessionStatusAwaitingApproval = "awaiting_approval"
	ChatSessionStatusCanceling        = "canceling"
)

// ChatMessageRole — user / assistant / system. System rows are
// engine-emitted (e.g. tool-set change announcements) and are
// fed back to the model as system context on the next turn.
const (
	ChatMessageRoleUser      = "user"
	ChatMessageRoleAssistant = "assistant"
	ChatMessageRoleSystem    = "system"
)

// ChatMessageStatus is the per-turn lifecycle. User rows go
// straight to 'completed' at insert; assistant rows transition
// through queued → running → completed/partial_failed/failed/
// canceled, optionally via 'canceling' (a transient state when
// a cancel request lands while a tool is mid-flight).
const (
	ChatMessageStatusQueued        = "queued"
	ChatMessageStatusRunning       = "running"
	ChatMessageStatusCompleted     = "completed"
	ChatMessageStatusPartialFailed = "partial_failed"
	ChatMessageStatusFailed        = "failed"
	ChatMessageStatusCanceled      = "canceled"
	ChatMessageStatusCanceling     = "canceling"
)

// ChatToolApprovalStatus tracks the mutating-tool approval
// workflow per plan 24 §"Approval flow".
const (
	ChatToolApprovalStatusPending  = "pending"
	ChatToolApprovalStatusApproved = "approved"
	ChatToolApprovalStatusDenied   = "denied"
	ChatToolApprovalStatusTimedOut = "timed_out"
)

// Per-session message caps. Soft surfaces a UI nudge ("Continue
// in new session"); hard returns chat_session_msg_cap from the
// send handler. Plan 24 §"Per-session message cap".
const (
	ChatMessageSoftCap = 200
	ChatMessageHardCap = 250
)

// ChatToolApprovalTTL is the per-approval expiry window. After
// this elapses, the sweeper marks the row 'timed_out' and the
// agent resumes with the approval rejected. Plan 24 §
// "chat_tool_approvals" sets 10 minutes.
const ChatToolApprovalTTL = 10 * time.Minute

// ChatSession is the conversation row. Most fields map 1:1 to
// the chat_sessions table; a few are derived for the wire shape
// (Tools is JSON-decoded; ScopeHubIDs comes back as a []string
// from pgx).
// ChatPersonaNone is the session persona sentinel for "explicitly no
// persona" — distinct from "" which inherits chat_default_persona_id.
const ChatPersonaNone = "none"

type ChatSession struct {
	ID              string   `json:"id"`
	OwnerID         string   `json:"owner_id"`
	ScopeType       string   `json:"scope_type"`
	ScopeHubIDs     []string `json:"scope_hub_ids"`
	WriteHubID      string   `json:"write_hub_id,omitempty"` // empty string → SQL NULL on write
	Title           string   `json:"title"`
	Model           string   `json:"model"`
	Tools           []string `json:"tools"`
	ToolsetVersion  int      `json:"toolset_version"`
	Status          string   `json:"status"`
	ActiveMessageID string   `json:"active_message_id,omitempty"`
	// PersonaID binds the memax agent's identity for this session:
	// "" inherits the chat_default_persona_id setting, "none" explicitly
	// disables the persona, anything else is a personas.id owned by the user.
	PersonaID     string     `json:"persona_id,omitempty"`

	// PinnedMemoryIDs seeds the session with specific memories the
	// agent must see (plan 25 P3): a chat opened from a board card
	// carries that card's citations, so the agent starts already
	// knowing what the user is looking at instead of having to
	// rediscover it through recall. Rendered into the system prompt
	// by the chat worker, capped at creation.
	PinnedMemoryIDs []string `json:"pinned_memory_ids,omitempty"`
	LockedAt      *time.Time `json:"locked_at,omitempty"`
	MessageCount  int        `json:"message_count"`
	PinnedAt      *time.Time `json:"pinned_at,omitempty"`
	ArchivedAt    *time.Time `json:"archived_at,omitempty"`
	LastMessageAt *time.Time `json:"last_message_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
}

// ChatMessage is one turn in a conversation. Tool calls and
// citations are JSONB on disk; we keep them as raw json.RawMessage
// here so the chat-list read path doesn't pay decode cost when
// the consumer just wants metadata. The send/regenerate path
// decodes into typed sub-structs as needed.
type ChatMessage struct {
	ID                 string          `json:"id"`
	SessionID          string          `json:"session_id"`
	Sequence           int             `json:"sequence"`
	Role               string          `json:"role"`
	AuthorUserID       string          `json:"author_user_id"`
	Status             string          `json:"status"`
	ScopeHubIDsUsed    []string        `json:"scope_hub_ids_used"`
	Content            string          `json:"content"`
	ToolCalls          json.RawMessage `json:"tool_calls,omitempty"`
	Citations          json.RawMessage `json:"citations,omitempty"`
	Usage              json.RawMessage `json:"usage,omitempty"`
	Error              json.RawMessage `json:"error,omitempty"`
	IdempotencyKey     string          `json:"idempotency_key,omitempty"`
	ParentMessageID    string          `json:"parent_message_id,omitempty"`
	ToolsetVersionUsed int             `json:"toolset_version_used"`
	StartedAt          *time.Time      `json:"started_at,omitempty"`
	FinishedAt         *time.Time      `json:"finished_at,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
}

// ChatMessageEvent is one row in the SSE replay buffer. Stored
// for 24h after creation; resume reads `WHERE message_id = $1
// AND seq > $last_event_id ORDER BY seq`.
type ChatMessageEvent struct {
	ID        int64           `json:"id"`
	MessageID string          `json:"message_id"`
	Seq       int             `json:"seq"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// ChatToolApproval is one mutating-tool approval request. The
// agent goroutine blocks at SDK's RequestApproval until the user
// decides (status moves to approved/denied) or expires_at
// passes (the sweeper flips status to timed_out and the agent
// resumes with approval rejected).
type ChatToolApproval struct {
	ID          string          `json:"id"`
	MessageID   string          `json:"message_id"`
	ToolName    string          `json:"tool_name"`
	Args        json.RawMessage `json:"args"`
	ArgsHash    string          `json:"args_hash"`
	Status      string          `json:"status"`
	DecidedBy   string          `json:"decided_by,omitempty"`
	DecidedAt   *time.Time      `json:"decided_at,omitempty"`
	RequestedAt time.Time       `json:"requested_at"`
	ExpiresAt   time.Time       `json:"expires_at"`
	CreatedAt   time.Time       `json:"created_at"`
}

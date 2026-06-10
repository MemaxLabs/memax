package model

import (
	"encoding/json"
	"time"
)

// Ops types back the admin-only operations dashboard at
// /v1/admin/ops/*. Read-mostly — they join River's job/client tables
// with our own (memories, dream_runs, campaign_deliveries, etc.) to
// surface ops health in product terms, not raw queue primitives.
//
// Everything here is admin-facing. These types MUST NOT leak into
// memax-sdk or any public client — see CLAUDE.md "Admin Surface
// Boundary" rule.

// Job states reported by River. Kept as string constants (not enum)
// because River treats them as text in river_job.state and returning
// the raw string lets the admin UI render unknown future states
// gracefully instead of 500ing on an unrecognised enum value.
const (
	JobStateAvailable = "available"
	JobStateRunning   = "running"
	JobStateScheduled = "scheduled"
	JobStateCompleted = "completed"
	JobStateDiscarded = "discarded"
	JobStateCancelled = "cancelled"
	JobStateRetryable = "retryable"
	JobStatePending   = "pending"
)

// OpsPulse is the one-shot summary rendered on the dashboard
// landing page. Cheap to compute — single-digit COUNT(*) queries —
// so it's re-fetched on the SSE tick to drive the live counters.
type OpsPulse struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Workers     OpsWorkers        `json:"workers"`
	Queues      []OpsQueueDepth   `json:"queues"`
	Throughput  OpsThroughput     `json:"throughput"`
	RedFlags    []OpsRedFlag      `json:"red_flags"`
	Ingestion   OpsIngestionCards `json:"ingestion"`
}

// OpsWorkers summarizes the River clients we can see heartbeating.
type OpsWorkers struct {
	// Healthy is the count of client rows whose heartbeat_at is within
	// the staleness budget (see opsWorkerHealthyWindow in the store).
	Healthy int `json:"healthy"`
	// Stale is clients whose heartbeat_at is outside the healthy
	// window but haven't been evicted yet. Non-zero means a worker
	// crashed or lost network and River hasn't swept it up.
	Stale int `json:"stale"`
	// Leader is the client ID currently holding the River leader
	// election (runs periodic jobs). Empty string if no leader —
	// rare and transient, but worth surfacing.
	Leader string `json:"leader"`
	// Clients is the per-client breakdown. Returned even in the
	// pulse so the dashboard can render the worker list inline
	// without a second round-trip.
	Clients []OpsWorkerClient `json:"clients"`
}

// OpsWorkerClient is one row from river_client joined against
// river_client_queue.
type OpsWorkerClient struct {
	ClientID    string                 `json:"client_id"`
	Queues      []OpsWorkerClientQueue `json:"queues"`
	HeartbeatAt time.Time              `json:"heartbeat_at"`
	AgeSeconds  int                    `json:"age_seconds"`
	Healthy     bool                   `json:"healthy"`
	IsLeader    bool                   `json:"is_leader"`
}

// OpsWorkerClientQueue is one queue subscription on a client.
type OpsWorkerClientQueue struct {
	Queue      string `json:"queue"`
	MaxWorkers int    `json:"max_workers"`
}

// OpsQueueDepth is per-queue counts by state. Covers the states
// that matter on a dashboard — completed/cancelled are excluded
// from depth because they're terminal and uninteresting at a
// glance.
type OpsQueueDepth struct {
	Queue     string `json:"queue"`
	Available int    `json:"available"`
	Scheduled int    `json:"scheduled"`
	Running   int    `json:"running"`
	Retryable int    `json:"retryable"`
	Pending   int    `json:"pending"`
	Discarded int    `json:"discarded"`
}

// OpsThroughput counts events in a rolling window. All counts are
// over the same WindowMinutes to keep the card comparable.
type OpsThroughput struct {
	WindowMinutes int `json:"window_minutes"`
	// MemoriesProcessed = memory_process jobs finalized_at >= window.
	MemoriesProcessed int `json:"memories_processed"`
	// MemoriesFailed = memory_process jobs discarded_at >= window
	// (retryable→discarded transition).
	MemoriesFailed int `json:"memories_failed"`
	// DreamsCompleted = dream_cycle jobs finalized_at >= window with
	// no error.
	DreamsCompleted int `json:"dreams_completed"`
	// EmailsSent = send_email + send_campaign_email jobs finalized
	// without error.
	EmailsSent int `json:"emails_sent"`
}

// OpsIngestionCards is the dashboard's memory-ingestion summary card.
// Split from OpsIngestionDetail so the pulse response stays small.
type OpsIngestionCards struct {
	InProcessing     int `json:"in_processing"`
	StuckOverFiveMin int `json:"stuck_over_five_min"`
	FailedLastHour   int `json:"failed_last_hour"`
}

// OpsRedFlag is a product-framed warning the dashboard should surface
// visibly. Kind gives a category the UI can render differently;
// Severity hints at the visual treatment ("info" / "warn" / "error").
// Message is already human-readable (server-side i18n isn't set up,
// so the UI swaps it by matching Kind instead of translating Message).
type OpsRedFlag struct {
	Kind     string          `json:"kind"`     // worker_offline | stuck_memories | no_successful_dream | queue_backlog
	Severity string          `json:"severity"` // info | warn | error
	Message  string          `json:"message"`
	Meta     json.RawMessage `json:"meta,omitempty"`
}

// JobListOpts controls the /v1/admin/ops/jobs filter.
type JobListOpts struct {
	States []string
	// Kinds is an exact-set filter (IN). Useful for "show only
	// these types"; callers pick from a known set.
	Kinds []string
	// KindSearch is a substring match (ILIKE %value%) on
	// river_job.kind. Preferred for free-text input in the admin
	// UI because operators rarely remember exact kind names.
	KindSearch string
	// Queues / QueueSearch — same pattern as Kinds / KindSearch.
	Queues      []string
	QueueSearch string
	// CreatedAfter / CreatedBefore restrict by created_at.
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	// Cursor is the last (created_at, id) pair from the previous
	// page, base64-encoded by the handler. Zero value means first
	// page.
	Cursor string
	Limit  int // server caps this
}

// JobListResponse is a paginated job list.
type JobListResponse struct {
	Jobs       []JobSummary `json:"jobs"`
	NextCursor string       `json:"next_cursor"`
	HasMore    bool         `json:"has_more"`
}

// JobSummary is the compact job row rendered in lists.
type JobSummary struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	Queue       string     `json:"queue"`
	State       string     `json:"state"`
	Attempt     int        `json:"attempt"`
	MaxAttempts int        `json:"max_attempts"`
	Priority    int        `json:"priority"`
	CreatedAt   time.Time  `json:"created_at"`
	AttemptedAt *time.Time `json:"attempted_at,omitempty"`
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`
	ScheduledAt time.Time  `json:"scheduled_at"`
	// LastError is the most recent error message, truncated to keep
	// list payloads small. Full history comes through JobDetail.
	LastError string `json:"last_error,omitempty"`
	// ProductContext adds product-framed metadata extracted from
	// args — e.g., for memory_process jobs, the memory title and
	// owner. Nil for job kinds we don't enrich.
	ProductContext *JobProductContext `json:"product_context,omitempty"`
}

// JobProductContext is the memax-side projection of a job. Populated
// only for kinds we know how to enrich.
type JobProductContext struct {
	Kind       string `json:"kind"`  // memory_process | dream_cycle | send_email | ...
	Label      string `json:"label"` // "Memory: Sprint planning notes" or "Dream: Platform Team"
	SubLabel   string `json:"sub_label,omitempty"`
	ResourceID string `json:"resource_id,omitempty"`
}

// JobDetail is the full inspector payload for one job.
type JobDetail struct {
	JobSummary
	Args     json.RawMessage  `json:"args"`
	Errors   []JobErrorRecord `json:"errors"`
	Metadata json.RawMessage  `json:"metadata,omitempty"`
	Tags     []string         `json:"tags,omitempty"`
}

// JobErrorRecord is one row from river_job.errors (jsonb array).
type JobErrorRecord struct {
	At      time.Time `json:"at"`
	Attempt int       `json:"attempt"`
	Error   string    `json:"error"`
	Trace   string    `json:"trace,omitempty"`
}

// StuckMemory is a memory stuck in state='processing' long enough to
// warrant operator attention. Returned by the ingestion view.
type StuckMemory struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"owner_id"`
	HubID       string    `json:"hub_id"`
	Title       string    `json:"title"`
	ContentType string    `json:"content_type"`
	Source      string    `json:"source"`
	SourceAgent string    `json:"source_agent,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	AgeSeconds  int       `json:"age_seconds"`
	// AssociatedJob is the most recent memory_process job for this
	// memory, if one can be found in river_job. Empty when the job
	// has already been pruned or was never inserted.
	AssociatedJob *JobSummary `json:"associated_job,omitempty"`
}

// FailedIngestion is a memory where the processing fallback
// kicked in (state='active' but summary starts with "Processing
// failed after N attempts"). Used to surface silent partial
// failures that don't block the user but do indicate a regression.
type FailedIngestion struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"owner_id"`
	HubID       string    `json:"hub_id"`
	Title       string    `json:"title"`
	ContentType string    `json:"content_type"`
	Source      string    `json:"source"`
	SourceAgent string    `json:"source_agent,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	// Summary is the failure explanation (server-written by
	// finalizeFailedProcessing).
	Summary string `json:"summary"`
}

// IngestionBySource is a per-source breakdown for the ingestion
// view. Attention is drawn to rows with a high error_count relative
// to memories_created.
type IngestionBySource struct {
	Source          string `json:"source"`
	MemoriesCreated int    `json:"memories_created"`
	StillProcessing int    `json:"still_processing"`
	FailedSilent    int    `json:"failed_silent"`
	WindowMinutes   int    `json:"window_minutes"`
}

// OpsStreamEvent is the SSE envelope pushed over
// /v1/admin/ops/stream. Type discriminates which payload field is
// populated.
type OpsStreamEvent struct {
	Type        string    `json:"type"` // pulse | heartbeat
	GeneratedAt time.Time `json:"generated_at"`
	Pulse       *OpsPulse `json:"pulse,omitempty"`
}

// AdminAuditEntry is one append-only row in the admin_audit table.
// Used by the ops dashboard to record retry / cancel / force-active
// actions with actor + metadata. resource_id is text so bigint job
// IDs and uuid memory IDs share one log without a polymorphic
// column.
type AdminAuditEntry struct {
	ID           string          `json:"id"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Action       string          `json:"action"`
	ActorID      *string         `json:"actor_id,omitempty"`
	Metadata     json.RawMessage `json:"metadata"`
	CreatedAt    time.Time       `json:"created_at"`
}

// JobLogLine is the product-side shape of one log record. Mirrors
// observability.LogLine but lives in model so the handler layer
// doesn't leak its dependency on the observability package into
// API response types (which the web client consumes). Interop is
// trivial since both sides marshal the same JSON.
type JobLogLine struct {
	Timestamp time.Time      `json:"timestamp"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// JobLogsResponse is the one-shot logs query payload. `environment`
// is surfaced so the UI can display "Showing production logs" —
// sourced server-side, not request-side, so the UI can trust it.
type JobLogsResponse struct {
	Environment string    `json:"environment"`
	Since       time.Time `json:"since"`
	Until       time.Time `json:"until"`
	Lines       any       `json:"lines"` // []observability.LogLine; `any` keeps model free of observability dep
}

// JobLogsStreamEvent is one SSE frame from GET
// /v1/admin/ops/jobs/{id}/logs/stream. Type of the event is carried
// on the SSE envelope ("snapshot" | "append" | "error"); this struct
// is always the `data:` payload for snapshot/append events.
type JobLogsStreamEvent struct {
	Environment string `json:"environment"`
	Lines       any    `json:"lines"`
}

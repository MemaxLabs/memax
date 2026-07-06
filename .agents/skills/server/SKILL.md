---
name: server
description: "Use when adding, modifying, or fixing Go backend code — API server, worker, handlers, store, queue, ingest pipeline, dreams engine. ALWAYS trigger on any work touching packages/server/, including handler changes, store queries, queue jobs, migrations, and LLM processor modules. Security rules (owner_id filtering, ApiResponse envelope) are non-negotiable."
---

# Memax Server — Development Skill

The memax server is a Go API + background worker backed by PostgreSQL, pgvector, and River queue. It serves the REST API, MCP protocol, retrieval pipeline, and memory processing. This skill covers every established pattern.

## Architecture

```
packages/server/
  cmd/
    server/           # API process bootstrap — listener, health/readiness, shutdown
    worker/           # Worker process bootstrap — signal handling, shutdown
  internal/
    serverapp/        # API composition root — dependencies, queue enqueue wiring, routes
    workerapp/        # Worker composition root — dependencies, River client, health metrics
    anthropic/        # Shared LLM client (single source of truth for Claude calls)
    handler/          # HTTP handlers + middleware (REST + MCP + OAuth)
    store/            # Storage interface + PostgreSQL + InMemory implementations
    model/            # Domain types, request/response structs, ApiResponse envelope
    queue/            # River job definitions + worker implementations
    ingest/           # Memory processing pipeline
      categorize/     # LLM auto-classification
      chunker/        # Markdown splitting
      classify/       # Content type detection (deterministic)
      embed/          # Vector embeddings (Voyage AI)
      extract/        # Fact extraction from transcripts
      fileproc/       # PDF/image text extraction (Claude Vision)
      link/           # URL fetch + summarize
      summarize/      # Memory summarization
    retrieval/        # Query processing
      decay/          # Time-based relevance decay
      distill/        # Query shortening via LLM
      expand/         # Question reformulation
      intent/         # Query intent classification
    dreams/           # Memory consolidation engine
    auth/             # JWT utilities
    migrate/          # Migration runner with retry
    analytics/        # PostHog tracking
  migrations/         # SQL schema files (001_*.up.sql, 001_*.down.sql)
  eval/               # Retrieval quality evaluation tests
```

**Two processes, one database:**

- **API server** (`cmd/server/`) — serves HTTP, enqueues jobs (insert-only River client)
- **Worker** (`cmd/worker/`) — processes River jobs (chunking, embedding, dreams, extraction)

Both read the same env vars and connect to the same PostgreSQL. Deploy independently, scale independently.

## Adding a New Handler

### 1. Define the handler struct

```go
// internal/handler/widgets.go
package handler

type WidgetsHandler struct {
    store store.Store
    // Optional deps — nil means feature disabled
    llm *anthropic.Client
}

func NewWidgetsHandler(s store.Store, llm *anthropic.Client) *WidgetsHandler {
    return &WidgetsHandler{store: s, llm: llm}
}
```

### 2. Implement handler methods

```go
// POST /v1/widgets
func (h *WidgetsHandler) Create(w http.ResponseWriter, r *http.Request) {
    ownerID := GetUserID(r)
    hubID := GetHubID(r)

    // Parse request
    var req struct {
        Name string `json:"name"`
    }
    body, err := io.ReadAll(r.Body)
    if err != nil {
        writeError(w, http.StatusBadRequest, "invalid_body", "Could not read request body")
        return
    }
    if err := json.Unmarshal(body, &req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
        return
    }

    // Validate
    if req.Name == "" {
        writeError(w, http.StatusBadRequest, "missing_name", "Widget name is required")
        return
    }

    // Create and persist
    widget := &model.Widget{ID: generateID(), OwnerID: ownerID, Name: req.Name}
    if err := h.store.CreateWidget(widget); err != nil {
        writeError(w, http.StatusInternalServerError, "store_error", err.Error())
        return
    }

    // Track + respond
    track(ownerID, "widget_created", map[string]any{"widget_id": widget.ID})
    writeJSON(w, http.StatusCreated, model.ApiResponse{Data: widget})
}
```

### 3. Register routes in main.go

```go
// cmd/server/main.go
widgetsH := handler.NewWidgetsHandler(s, llm)

protected.HandleFunc("POST /v1/widgets", widgetsH.Create)
protected.HandleFunc("GET /v1/widgets", widgetsH.List)
protected.HandleFunc("GET /v1/widgets/{id}", widgetsH.Get)
protected.HandleFunc("DELETE /v1/widgets/{id}", widgetsH.Delete)

mux.Handle("/v1/widgets", withAuth(protected))
mux.Handle("/v1/widgets/", withAuth(protected))
```

## Response Envelope (CRITICAL)

**Every REST response MUST use `model.ApiResponse{Data: ...}`.**

```go
// Success — pass your payload as Data
writeJSON(w, http.StatusOK, model.ApiResponse{Data: result})

// Error — use writeError helper (wraps automatically)
writeError(w, http.StatusBadRequest, "invalid_input", "Name is required")
```

`writeJSON` only accepts `model.ApiResponse` (not `any`) — the compiler rejects raw JSON. This prevents the class of bugs where a handler returns unwrapped data and the CLI/SDK/web get `undefined` from `.data`.

**Never use `json.NewEncoder(w).Encode(...)` for REST endpoints.** Only for non-REST protocols (MCP JSON-RPC, OAuth token responses).

## Admin Routes Stay Off the SDK (CRITICAL)

When you add a handler under `/v1/admin/*`, do NOT create or update a matching resource in the public `memax-sdk` (`MemaxLabs/memax/packages/sdk/`). Admin is operator-only and never ships to the npm-published `memax-sdk`.

The web-side companion lives in `packages/web/src/lib/admin-client/` — if the new route needs a client call, add it there (not here, not in the SDK). See AGENTS.md → "Admin Surface Boundary (CRITICAL)" for the full rationale. `scripts/check-sdk-boundary.mjs` enforces the internal web side at CI by blocking non-admin raw `/v1/*` calls that bypass `memax-sdk`.

## Owner Isolation (CRITICAL)

**Every database query that touches user data MUST filter by `owner_id`.**

```go
// Store method signature — ownerID is always a required parameter
GetMemory(id string, ownerID string) (*model.Memory, error)

// SQL — always includes AND owner_id = $N
`SELECT ... FROM memories WHERE id = $1 AND owner_id = $2`
```

No exceptions. Application-level checks are not enough — a single missed call leaks all data.

**Before merging any Store or handler change:**

1. Grep every `h.store.` call — does each one pass `ownerID`?
2. Check the SQL — does every `SELECT`, `DELETE` have `AND owner_id = $N`?

## Shared LLM Client (CRITICAL)

**All LLM calls go through `internal/anthropic.Client`.** Never write raw HTTP calls to the Anthropic API.

```go
// Create once in main.go
llm := anthropic.NewFromEnv()  // nil if ANTHROPIC_API_KEY not set

// Inject into modules
cat := categorize.New(llm)
sum := summarize.New(llm)

// In module — use Complete() for text, CompleteMessages() for multimodal
resp, err := h.llm.Complete(ctx, anthropic.CompleteRequest{
    Model:     anthropic.ModelFromEnv("MY_MODEL"),
    MaxTokens: 200,
    Prompt:    prompt,
})

// For images/PDFs — multimodal messages
resp, err := h.llm.CompleteMessages(ctx, model, 4096, messages)
```

**Utilities for parsing LLM output:**

- `anthropic.ExtractJSONObject(text)` — extract `{...}` from markdown fences
- `anthropic.ExtractJSONArray(text)` — extract `[...]` from markdown fences
- `anthropic.StripMarkdownFences(text)` — remove ``` wrapping

**Nil means disabled:** If `llm == nil`, module's `New()` returns `nil`. Callers check before using. No feature flags needed.

## Dependency Injection Pattern

Modules accept dependencies in constructors, not by reading env vars in methods.

```go
// GOOD — dependency injected
func New(client *anthropic.Client) *Summarizer {
    if client == nil { return nil }
    return &Summarizer{client: client, model: anthropic.ModelFromEnv("SUMMARIZE_MODEL")}
}

// BAD — reads env var at call time
func (s *Summarizer) Summarize(content string) string {
    key := os.Getenv("ANTHROPIC_API_KEY")  // Don't do this
}
```

Env vars are read **once** at startup in `cmd/server/main.go` and `cmd/worker/main.go`.

## Store Interface Pattern

### Adding a new entity

1. **Add model** in `internal/model/`:

```go
type Widget struct {
    ID        string    `json:"id"`
    OwnerID   string    `json:"owner_id"`
    Name      string    `json:"name"`
    CreatedAt time.Time `json:"created_at"`
}
```

2. **Add interface methods** in `store/store.go`:

```go
CreateWidget(widget *model.Widget) error
GetWidget(id string, ownerID string) (*model.Widget, error)
ListWidgets(ownerID string) ([]model.Widget, error)
DeleteWidget(id string, ownerID string) error
```

3. **Implement in PostgreSQL** (`store/postgres.go`):

```go
func (s *PostgresStore) CreateWidget(widget *model.Widget) error {
    ctx := context.Background()
    _, err := s.pool.Exec(ctx,
        `INSERT INTO widgets (id, owner_id, name, created_at)
         VALUES ($1, $2, $3, $4)`,
        widget.ID, widget.OwnerID, widget.Name, widget.CreatedAt)
    return err
}
```

4. **Implement in InMemoryStore** (`store/memory.go`):

```go
func (s *InMemoryStore) CreateWidget(widget *model.Widget) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.widgets == nil { s.widgets = make(map[string]*model.Widget) }
    s.widgets[widget.ID] = widget
    return nil
}
```

5. **Add migration** (`migrations/NNN_widgets.up.sql`):

```sql
CREATE TABLE widgets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id   TEXT NOT NULL,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_widgets_owner ON widgets(owner_id);
```

## Migration Conventions

- Files: `NNN_description.up.sql` and `NNN_description.down.sql`
- Sequential numbering: `001_`, `002_`, ..., `013_`
- Up: CREATE TABLE, ALTER TABLE, CREATE INDEX
- Down: DROP TABLE, DROP INDEX, ALTER TABLE DROP COLUMN
- Always provide both up and down
- Migrations run automatically on server startup (with retry on lock contention)

## Queue & Worker Pattern

### Adding a new background job

1. **Define job args** in `queue/jobs.go`:

```go
type MyJobArgs struct {
    ItemID  string `json:"item_id"`
    OwnerID string `json:"owner_id"`
}

func (MyJobArgs) Kind() string { return "my_job" }

func (MyJobArgs) InsertOpts() river.InsertOpts {
    return river.InsertOpts{
        Queue:       "default",
        MaxAttempts: 3,
        UniqueOpts: river.UniqueOpts{
            ByArgs:   true,
            ByPeriod: 1 * time.Hour,
        },
    }
}
```

2. **Register stub** in `queue/queue.go` (for insert-only server):

```go
river.AddWorker(workers, &stubMyJobWorker{})
// ... plus the stub struct + Work method
```

3. **Implement worker** in `queue/workers.go`:

```go
type MyJobWorker struct {
    river.WorkerDefaults[MyJobArgs]
    Store store.Store
}

func (w *MyJobWorker) Timeout(*river.Job[MyJobArgs]) time.Duration {
    return 3 * time.Minute
}

func (w *MyJobWorker) Work(ctx context.Context, job *river.Job[MyJobArgs]) error {
    slog.Info("my job started", "item_id", job.Args.ItemID)
    // ... do work ...
    return nil
}
```

4. **Register real worker** in `cmd/worker/main.go`:

```go
river.AddWorker(workers, &queue.MyJobWorker{Store: s})
```

5. **Enqueue from handler** (via SetEnqueue pattern):

```go
// In handler
h.enqueue(itemID, ownerID)

// Wired in cmd/server/main.go
myHandler.SetEnqueue(func(itemID, ownerID string) {
    queueClient.Insert(ctx, queue.MyJobArgs{ItemID: itemID, OwnerID: ownerID}, nil)
})
```

**Two queues:** `default` (20 workers) for fast jobs, `dreams` (3 workers) for heavy consolidation.

## Ingest Pipeline

Memory processing is a multi-stage pipeline running in the worker:

```
Content → File Extraction → Link Processing → Auto-Categorize
       → Chunk → Embed → Summarize → Fact Extraction
```

Each stage is optional (nil = skip). The pipeline is in `queue/workers.go` `MemoryProcessWorker.Work()`.

**Adding a new ingest stage:**

1. Create module in `internal/ingest/yourstage/`
2. Accept `*anthropic.Client` in constructor (if LLM-based)
3. Return nil from `New()` if client is nil
4. Add to `MemoryProcessWorker` struct
5. Add processing step in `Work()` method
6. Inject in `cmd/worker/main.go`

## Middleware Chain

All protected routes go through: `authMiddleware → hubMiddleware → handler`

```
Request → RequireAuth (JWT/API key → userID) → HubContext (resolve hubIDs) → Handler
```

**Context extraction in handlers:**

```go
ownerID := GetUserID(r)           // Always available after auth
hubID := GetHubID(r)              // Active hub (may be empty)
hubIDs := GetAccessibleHubIDs(r)  // All hubs user can access
```

## MCP Tools (parity with CLI)

Both the Go server MCP handler (`handler/mcp.go`) and the CLI MCP server (`cli/src/commands/mcp.ts`) must expose **identical** tools. When adding or modifying an MCP tool, update BOTH implementations.

| Tool              | Parameters                                              | Description                         |
| ----------------- | ------------------------------------------------------- | ----------------------------------- |
| memax_recall      | query\*, limit, category, project_context               | Semantic search                     |
| memax_push        | content\*, title, hint, category, tags, project_context | Save knowledge                      |
| memax_get         | id\*                                                    | Read full memory                    |
| memax_list        | limit, cursor, sort, hub_id, topic_id                   | Browse/paginate                     |
| memax_hubs        | none                                                    | List accessible hubs                |
| memax_hub_members | hub_id\*                                                | List hub members                    |
| memax_forget      | id\*                                                    | Delete memory                       |
| memax_capture     | summary\*, decisions, learnings                         | Session capture                     |
| memax_topics      | topic_id, hub_id                                        | Browse topic tree or topic memories |

## Topics Endpoints

13 REST endpoints for topic management:

- `GET /v1/topics` — list ACTIVE topics as tree with memory counts (archived excluded)
- `POST /v1/topics` — create (validates depth ≤ 5, unique names, rejects archived parent)
- `GET /v1/topics/archived` — flat archived list, most recently archived first
- `GET /v1/topics/{id}` — single topic (works for archived too)
- `PATCH /v1/topics/{id}` — update (sets user_modified on name/desc/icon change; 409 `topic_archived` on archived topics)
- `DELETE /v1/topics/{id}` — delete (re-parents children)
- `GET /v1/topics/{id}/memories` — paginated memory list
- `POST /v1/topics/{id}/memories` — assign with confidence (0.0-1.0); 409 on archived target
- `DELETE /v1/topics/{id}/memories/{mid}` — unassign
- `POST /v1/topics/{id}/visit` — record visit (clears dream-delta signals)
- `POST /v1/topics/{id}/archive` — archive subtree (idempotent; memory assignments survive)
- `POST /v1/topics/{id}/restore` — restore subtree (re-plants at root if parent still archived)
- `POST /v1/topics/reorder` — batch reorder

Archive invariant: `store.ListTopics` returns active-only rows, so every
tree consumer (handlers, dreams organize/restructure, inline classification,
agent browse tools, MCP) excludes archived topics by construction. The
recall topic-name boost and ask synthesis maps filter `archived_at IS NULL`.

## Error Handling

### Handler errors

```go
// Client errors (4xx) — specific error code + actionable message
writeError(w, http.StatusBadRequest, "missing_name", "Widget name is required")
writeError(w, http.StatusNotFound, "not_found", "Widget not found")
writeError(w, http.StatusConflict, "slug_taken", "A widget with this slug already exists")

// Server errors (5xx) — generic code + underlying error
writeError(w, http.StatusInternalServerError, "store_error", err.Error())
```

### Store errors

```go
// Return errors, don't log — let the handler decide
func (s *PostgresStore) GetWidget(id, ownerID string) (*model.Widget, error) {
    // ...
    if err != nil {
        return nil, fmt.Errorf("widget not found: %w", err)
    }
}
```

### Worker errors

```go
// Return error = retry the job (up to MaxAttempts)
// Return river.JobCancel(err) = permanently fail, don't retry
// Return nil = success
func (w *MyWorker) Work(ctx context.Context, job *river.Job[MyArgs]) error {
    if permanentFailure {
        return river.JobCancel(fmt.Errorf("cannot process: %v", reason))
    }
    return nil // success
}
```

## Testing

- **Table-driven tests** for deterministic logic (intent classification, decay calculation, chunking)
- **Testcontainers** for database integration tests (when needed)
- **Eval tests** (`eval/`) for retrieval quality measurement
- Run: `cd packages/server && go test ./...`
- Run with verbose: `go test -v -run TestSpecificFunction ./internal/retrieval/intent/`

## Anti-Patterns

| Anti-Pattern                                  | Instead                                                               |
| --------------------------------------------- | --------------------------------------------------------------------- |
| `json.NewEncoder(w).Encode(rawData)` for REST | `writeJSON(w, status, model.ApiResponse{Data: rawData})`              |
| Raw HTTP calls to Anthropic API               | Use `internal/anthropic.Client`                                       |
| `os.Getenv()` in module methods               | Read once in main.go, inject via constructor                          |
| Store method without `ownerID` param          | Always filter by owner — no exceptions                                |
| `http.DefaultClient.Do(req)`                  | Use the shared anthropic client or create a dedicated client          |
| Duplicating scan logic across queries         | Create `scanWidget(row)` helper                                       |
| `context.TODO()` in production code           | Use `context.Background()` or request context                         |
| Swallowing errors in workers                  | Return error (for retry) or `river.JobCancel` (for permanent failure) |
| Adding a River job without a stub in queue.go | Insert-only client validates job types — stub is required             |

## Checklist: Before Submitting Server Changes

1. Does every new Store method accept and filter by `ownerID`?
2. Does every handler response use `model.ApiResponse{Data: ...}`?
3. Are LLM calls using `internal/anthropic.Client` (not raw HTTP)?
4. Is the new migration paired (`.up.sql` + `.down.sql`)?
5. Is the InMemoryStore implementation updated alongside PostgresStore?
6. Are River job stubs registered in `queue.go` for the insert-only client?
7. Are dependencies injected via constructor (not read from env at call time)?
8. Does `go vet ./...` pass?
9. Are API changes reflected in all consumers (CLI, SDK, web, MCP)?

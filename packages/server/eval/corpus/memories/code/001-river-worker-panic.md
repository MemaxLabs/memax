## Stack Trace: River Worker Panic — Nil Pointer in Memory Processing

Hit a production panic in the River worker at 2026-04-09 03:14:22 UTC. The worker crashed processing a memory that had no `ProjectContext` set — the ingest path from the MCP `memax_push` tool didn't populate it when the caller omitted the optional `project_context` parameter.

### Full Stack Trace

```
goroutine 847 [running]:
runtime/debug.Stack()
	/usr/local/go/src/runtime/debug/stack.go:24 +0x5e
github.com/riverqueue/river.(*Client[...]).handlePanic(...)
	/go/pkg/mod/github.com/riverqueue/river@v0.6.0/client.go:1482 +0x82
runtime/debug.(*panicError).recover(...)
	/usr/local/go/src/runtime/debug/panic.go:14 +0x32
github.com/memaxlabs/memax/internal/worker.(*MemoryProcessWorker).Work(0xc000412e00, {0x1d08a80, 0xc000b7a1e0}, 0xc000cf4500)
	/app/internal/worker/memory_process.go:67 +0x3a2
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x1 addr=0x18 pc=0x1456a8f]

goroutine 847 [running]:
github.com/memaxlabs/memax/internal/worker.(*MemoryProcessWorker).buildEmbedInput(0xc000412e00, 0xc000cf4500)
	/app/internal/worker/memory_process.go:134 +0x2af
github.com/memaxlabs/memax/internal/worker.(*MemoryProcessWorker).Work(0xc000412e00, {0x1d08a80, 0xc000b7a1e0}, 0xc000cf4500)
	/app/internal/worker/memory_process.go:67 +0x352
github.com/riverqueue/river.(*Client[...]).workUnit(...)
	/go/pkg/mod/github.com/riverqueue/river@v0.6.0/client.go:1534 +0x1d8
```

### Root Cause

In `memory_process.go:134`, the `buildEmbedInput` method accessed `job.Args.ProjectContext.Repo` without a nil check:

```go
// BEFORE (panics when ProjectContext is nil)
func (w *MemoryProcessWorker) buildEmbedInput(job *river.Job[MemoryProcessArgs]) string {
    parts := []string{job.Args.Title, job.Args.Body}
    parts = append(parts, "repo:"+job.Args.ProjectContext.Repo)  // line 134 — BOOM
    // ...
}
```

### Fix

Added a nil guard before accessing ProjectContext fields:

```go
// AFTER
func (w *MemoryProcessWorker) buildEmbedInput(job *river.Job[MemoryProcessArgs]) string {
    parts := []string{job.Args.Title, job.Args.Body}
    if job.Args.ProjectContext != nil {
        if job.Args.ProjectContext.Repo != "" {
            parts = append(parts, "repo:"+job.Args.ProjectContext.Repo)
        }
        if job.Args.ProjectContext.Project != "" {
            parts = append(parts, "project:"+job.Args.ProjectContext.Project)
        }
    }
    // ...
}
```

The fix was deployed in commit `a3f7e21` on 2026-04-09. We also added a regression test in `memory_process_test.go` that enqueues a job with `ProjectContext: nil` to ensure it doesn't panic.

### Lesson

All optional struct pointer fields coming from external input (MCP, REST API) must be nil-checked before dereferencing. The `MemoryProcessArgs` struct has `ProjectContext *model.ProjectContext` — the pointer type signals it's optional, but the worker code assumed it was always populated.

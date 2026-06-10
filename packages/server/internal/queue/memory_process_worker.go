package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/categorize"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/extract"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/fileproc"
	ingestformat "github.com/MemaxLabs/memax/packages/server/internal/ingest/format"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/link"
	ingestprocess "github.com/MemaxLabs/memax/packages/server/internal/ingest/process"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/summarize"
	ingesttitle "github.com/MemaxLabs/memax/packages/server/internal/ingest/title"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// --- Memory Processing Worker ---
// Handles chunking, embedding, classification, summarization, and fact extraction.

type MemoryProcessWorker struct {
	river.WorkerDefaults[MemoryProcessArgs]
	Store         store.Store
	Events        events.Publisher
	Embedder      embed.Embedder
	Summarizer    *summarize.Summarizer
	Extractor     *extract.Extractor
	Categorizer   *categorize.Categorizer
	LinkProcessor *link.Processor
	FileProcessor *fileproc.Processor
	Formatter     *ingestformat.Formatter
	TitleResolver *ingesttitle.Resolver
	ObjectStore   objectstore.Store
	Processor     *ingestprocess.Processor
}

func (w *MemoryProcessWorker) Timeout(*river.Job[MemoryProcessArgs]) time.Duration {
	return 5 * time.Minute
}

func (w *MemoryProcessWorker) Work(ctx context.Context, job *river.Job[MemoryProcessArgs]) error {
	processor := w.Processor
	if processor == nil {
		processor = ingestprocess.New(w.Store, w.Events, w.Embedder, w.Summarizer, w.Extractor, w.Categorizer, w.LinkProcessor, w.FileProcessor, w.Formatter, w.TitleResolver, w.ObjectStore)
	}

	args := job.Args
	err := processor.Process(ctx, args.MemoryID, args.OwnerID, model.PushRequest{
		Content:             args.Content,
		Title:               args.Title,
		Hint:                args.Hint,
		Kind:                args.MemoryKind,
		Stability:           args.Stability,
		Tags:                args.Tags,
		Source:              args.Source,
		SourceAgent:         args.SourceAgent,
		SourcePath:          args.SourcePath,
		HubReason:           args.HubReason,
		ProjectContext:      args.ProjectContext,
		BatchID:             args.BatchID,
		ContentType:         args.ContentType,
		FileRef:             args.FileRef,
		AllowRelatedContext: args.AllowRelatedContext,
	})
	if err != nil && job.Attempt >= job.MaxAttempts {
		w.finalizeFailedProcessing(ctx, args, job.Attempt, err)
	}
	return err
}

func (w *MemoryProcessWorker) finalizeFailedProcessing(ctx context.Context, args MemoryProcessArgs, attempts int, cause error) {
	if w.Store == nil {
		return
	}
	memory, err := w.Store.GetMemory(args.MemoryID, args.OwnerID)
	if err != nil {
		slog.WarnContext(ctx, "failed memory processing could not load memory", "memory_id", args.MemoryID, "error", err, "cause", cause)
		return
	}
	if memory.State != "processing" {
		return
	}
	if memory.Content == "" {
		memory.Content = args.Content
	}
	if memory.ContentType == "" {
		memory.ContentType = args.ContentType
	}
	if memory.Summary == "" {
		memory.Summary = fmt.Sprintf("Processing failed after %d attempts: %v", attempts, cause)
	}
	memory.State = "active"
	memory.UpdatedAt = time.Now()
	if err := w.Store.UpdateMemory(memory); err != nil {
		slog.WarnContext(ctx, "failed to mark memory active after processing failure", "memory_id", args.MemoryID, "error", err, "cause", cause)
		return
	}
	// Mirror the processor/handler diagnostic log so a single grep across
	// staging can answer "did publish fire?" for this memory, regardless
	// of which path finalized it (normal completion vs worker terminal
	// failure vs handler fallback).
	slog.InfoContext(ctx, "memory.publish",
		"memory_id", memory.ID,
		"state", memory.State,
		"has_summary", memory.Summary != "",
		"summary_len", len(memory.Summary),
		"boundary", memory.Boundary,
		"private_only", memory.Boundary == "private",
		"publisher_enabled", w.Events != nil,
		"site", "worker_finalize",
	)
	events.PublishMemoryChanged(ctx, w.Events, memory, args.OwnerID)
	slog.WarnContext(ctx, "memory processing failed permanently; marked memory active with original content", "memory_id", args.MemoryID, "error", cause)
}

func (w *MemoryProcessWorker) embedChunks(ctx context.Context, chunks []model.Chunk) {
	if w.Embedder == nil || len(chunks) == 0 {
		return
	}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		text := c.HeadingChain + "\n" + c.Content
		if c.Hint != "" {
			text = c.Hint + "\n" + text
		}
		texts[i] = text
	}
	embeddings, err := w.Embedder.EmbedContext(ctx, texts, "document")
	if err != nil {
		slog.ErrorContext(ctx, "embedding failed", "error", err)
		return
	}
	for i := range chunks {
		if i < len(embeddings) && embeddings[i] != nil {
			chunks[i].Embedding = embeddings[i]
		}
	}
}

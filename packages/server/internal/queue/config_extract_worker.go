package queue

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/categorize"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/chunker"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/chunkmeta"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/language"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// --- Config Extraction Worker ---
// Distills knowledge items from agent config files into proper memories.

type ConfigExtractWorker struct {
	river.WorkerDefaults[ConfigExtractArgs]
	Store       store.Store
	Categorizer *categorize.Categorizer
	Embedder    embed.Embedder
}

func (w *ConfigExtractWorker) Timeout(*river.Job[ConfigExtractArgs]) time.Duration {
	return 3 * time.Minute
}

func (w *ConfigExtractWorker) Work(ctx context.Context, job *river.Job[ConfigExtractArgs]) error {
	args := job.Args
	ctx = anthropic.WithTracking(ctx, anthropic.Tracking{
		DistinctID: args.OwnerID,
		Metadata: map[string]any{
			"owner_id":  args.OwnerID,
			"config_id": args.ConfigID,
			"llm_flow":  "config_extract",
		},
	})
	slog.InfoContext(ctx, "config extraction started", "config_id", args.ConfigID, "attempt", job.Attempt)

	config, err := w.Store.GetAgentConfig(args.ConfigID, args.OwnerID)
	if err != nil {
		return fmt.Errorf("get config: %w", err)
	}

	// Use the categorizer's LLM to extract knowledge items
	if w.Categorizer == nil {
		slog.WarnContext(ctx, "config extraction skipped: no categorizer (ANTHROPIC_API_KEY not set)")
		return nil
	}

	decision := decideConfigExtraction(config.Content, config.Agent, config.FilePath)
	if decision.mode == configExtractNever {
		slog.InfoContext(ctx, "config extraction skipped", "config_id", args.ConfigID, "reason", decision.reason, "agent", config.Agent, "file_path", config.FilePath)
		return nil
	}

	items := extractKnowledgeItems(config.Content, config.Agent, config.FilePath)
	if len(items) == 0 {
		slog.InfoContext(ctx, "no knowledge items extracted", "config_id", args.ConfigID, "mode", decision.mode)
		return nil
	}

	now := time.Now()
	authorName := ""
	if user, err := w.Store.GetUser(args.OwnerID); err == nil && user != nil {
		authorName = user.Name
		if user.DisplayName != "" {
			authorName = user.DisplayName
		}
	}
	personalHub, err := w.Store.GetPersonalHub(args.OwnerID)
	if err != nil {
		return fmt.Errorf("get personal hub for config extraction: %w", err)
	}
	created := 0
	for _, item := range items {
		// Dedup by content hash
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(item.content)))
		if _, err := w.Store.GetMemoryByContentHash(hash, args.OwnerID, ""); err == nil {
			continue // already exists
		}

		// Classify with invisible retrieval axes.
		kind := model.MemoryKindSemantic
		stability := model.MemoryStabilityEvolving
		tags := item.tags
		if w.Categorizer != nil {
			result := w.Categorizer.ClassifyContext(ctx, item.title, item.content, "", time.Time{}, "")
			if result != nil {
				kind = result.Kind
				stability = result.Stability
				if len(result.Tags) > 0 {
					tags = result.Tags
				}
			}
		}

		memID := generateID()
		mem := &model.Memory{
			ID:                             memID,
			HubID:                          personalHub.ID,
			OwnerID:                        args.OwnerID,
			Title:                          item.title,
			Content:                        item.content,
			ContentType:                    "text",
			ContentHash:                    hash,
			Kind:                           model.NormalizeMemoryKind(kind),
			Stability:                      model.NormalizeMemoryStability(stability),
			RetrievalWeight:                1.0,
			Tags:                           tags,
			Boundary:                       "private",
			State:                          "active",
			Source:                         "extraction",
			SourceAgent:                    model.NormalizeAgentSlug(config.Agent),
			SourcePath:                     "config:" + args.ConfigID,
			ProvenanceCreatedByType:        model.MemoryCreatedByAgent,
			ProvenanceCreatedBySlug:        model.NormalizeAgentSlug(config.Agent),
			ProvenanceCreatedByDisplayName: strings.TrimSpace(config.Agent),
			ProvenanceCreatedVia:           "extraction",
			ProvenanceInitiationType:       model.MemoryInitiationImport,
			ProvenanceAttributionSource:    model.MemoryAttributionSourceServerDefault,
			Version:                        1,
			CreatedAt:                      now,
			UpdatedAt:                      now,
			AccessedAt:                     now,
		}
		model.NormalizeMemoryProvenanceFields(mem)
		mem.Provenance = model.BuildMemoryProvenance(mem)

		if err := w.Store.CreateMemory(mem); err != nil {
			slog.WarnContext(ctx, "failed to create extracted memory", "error", err)
			continue
		}

		// Chunk and embed
		chunks := chunker.ChunkMarkdown(item.content)
		tagsText := chunkmeta.TagsText(tags)
		metadataText := chunkmeta.MetadataText(authorName, personalHub.Name, personalHub.Slug, mem.Source, model.EffectiveMemoryAgentSlug(mem), "")
		var chunkModels []model.Chunk
		for _, cr := range chunks {
			lang := language.DetectChunk(cr.HeadingChain, cr.Content, "")
			chunkModels = append(chunkModels, model.Chunk{
				ID:              generateID(),
				MemoryID:        memID,
				Content:         cr.Content,
				HeadingChain:    cr.HeadingChain,
				ChunkIndex:      cr.Index,
				TokenCount:      cr.TokenCount,
				Language:        lang.Code,
				SearchConfig:    lang.Config,
				Kind:            mem.Kind,
				Stability:       mem.Stability,
				RetrievalWeight: mem.RetrievalWeight,
				TagsText:        tagsText,
				MetadataText:    metadataText,
				CreatedAt:       now,
			})
		}
		w.embedChunks(ctx, chunkModels)
		if err := w.Store.CreateChunks(chunkModels); err != nil {
			slog.WarnContext(ctx, "failed to create chunks for extracted memory", "error", err)
		}
		created++
	}

	slog.InfoContext(ctx, "config extraction complete", "config_id", args.ConfigID, "items", len(items), "created", created)
	return nil
}

func (w *ConfigExtractWorker) embedChunks(ctx context.Context, chunks []model.Chunk) {
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
		slog.ErrorContext(ctx, "embedding failed for config extraction", "error", err)
		return
	}
	for i := range chunks {
		if i < len(embeddings) && embeddings[i] != nil {
			chunks[i].Embedding = embeddings[i]
		}
	}
}

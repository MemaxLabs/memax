package dreams

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ingest/chunker"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/chunkmeta"
	"github.com/MemaxLabs/memax/packages/server/internal/language"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// rechunkAndEmbed rebuilds chunks for a merged memory and, when an
// embedder is configured, attaches fresh embeddings.
//
// Returns (chunks, embedFailed). embedFailed=true means chunks are
// valid but embeddings could not be computed (embedder error). The
// caller should still persist the merge but bump metrics.Errors so
// the run finalizes as partial_failed — the merged memory is now
// findable by text but not by vector search until the next
// retouching run.
func (e *Engine) rechunkAndEmbed(ctx context.Context, memory *model.Memory) ([]model.Chunk, bool) {
	chunkResults := chunker.ChunkMarkdown(memory.Content)
	now := time.Now()
	tagsText := chunkmeta.TagsText(memory.Tags)
	authorName := memory.AuthorName
	if authorName == "" {
		if user, err := e.store.GetUser(memory.OwnerID); err == nil && user != nil {
			authorName = user.Name
			if user.DisplayName != "" {
				authorName = user.DisplayName
			}
		}
	}
	hubName, hubSlug := "", ""
	if hub, err := e.store.GetHub(memory.HubID); err == nil && hub != nil {
		hubName = hub.Name
		hubSlug = hub.Slug
	}
	projectRepo := ""
	if memory.ProjectContext != nil {
		projectRepo = memory.ProjectContext["repo"]
	}
	metadataText := chunkmeta.MetadataText(authorName, hubName, hubSlug, memory.Source, model.EffectiveMemoryAgentSlug(memory), projectRepo)
	var chunks []model.Chunk
	for _, cr := range chunkResults {
		lang := language.DetectChunk(cr.HeadingChain, cr.Content, memory.Hint)
		chunks = append(chunks, model.Chunk{
			ID:              generateID(),
			MemoryID:        memory.ID,
			Content:         cr.Content,
			HeadingChain:    cr.HeadingChain,
			ChunkIndex:      cr.Index,
			TokenCount:      cr.TokenCount,
			Language:        lang.Code,
			SearchConfig:    lang.Config,
			Kind:            memory.Kind,
			Stability:       memory.Stability,
			RetrievalWeight: memory.RetrievalWeight,
			Hint:            memory.Hint,
			TagsText:        tagsText,
			MetadataText:    metadataText,
			CreatedAt:       now,
		})
	}

	// Embed if embedder is available.
	embedFailed := false
	if e.embedder != nil && len(chunks) > 0 {
		texts := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = c.HeadingChain + "\n" + c.Content
		}
		embeddings, err := e.embedder.EmbedContext(ctx, texts, "document")
		if err != nil {
			slog.WarnContext(ctx, "dream: embedding failed for merged memory", "error", err, "memory_id", memory.ID)
			embedFailed = true
		} else {
			for i := range chunks {
				if i < len(embeddings) {
					chunks[i].Embedding = embeddings[i]
				}
			}
		}
	}
	return chunks, embedFailed
}

// resynthesizeMetadata regenerates the title, summary, and tags for
// a merged memory. Called after merge so the metadata reflects the
// combined content, not just the keeper's original.
//
// Takes a metrics pointer so the LLM call count and token usage
// attribute to the caller's phase. The prior version discarded
// those numbers, which made merge's LLMCalls metric undercount by
// half and hid a real chunk of dream LLM cost from billing.
//
// Returns true on success, false if the LLM call or JSON parse
// failed — the caller bumps metrics.Errors on false so operators
// can see that the merge landed with stale metadata.
func (e *Engine) resynthesizeMetadata(ctx context.Context, memory *model.Memory, actorID string, metrics *model.DreamPhaseMetrics) bool {
	prompt := fmt.Sprintf(`A memory was just updated by merging two similar entries. Generate new metadata for the merged content.

Rules:
1. Title: concise, descriptive, under 80 characters
2. Summary: 2-3 sentences capturing the key information
3. Tags: 2-5 lowercase tags, comma-separated
4. Respond with ONLY a JSON object: {"title": "...", "summary": "...", "tags": "tag1, tag2, tag3"}

Classification: %s/%s
Content:
---
%s
---

JSON:`, memory.Kind, memory.Stability, truncateForLLM(memory.Content))

	trackCtx := e.trackingContextForMemory(ctx, memory, actorID, "dreams")
	// Count the LLM call regardless of success — if we don't, the
	// merge phase's LLMCalls metric will report half the actual
	// number of API round-trips. Accumulate tokens on success.
	if metrics != nil {
		metrics.LLMCalls++
	}
	resp, err := e.callLLM(trackCtx, prompt, 500, "dreams.resynthesize_metadata")
	if err != nil {
		if metrics != nil {
			metrics.LLMErrors++
			if isLLMTimeout(err) {
				metrics.LLMTimeouts++
			}
		}
		slog.WarnContext(ctx, "dream: failed to resynthesize metadata", "error", err, "memory_id", memory.ID)
		return false
	}
	addLLMUsage(metrics, resp)

	// Parse JSON from response
	jsonStr := resp.Text
	if idx := strings.Index(jsonStr, "{"); idx >= 0 {
		if end := strings.LastIndex(jsonStr, "}"); end >= idx {
			jsonStr = jsonStr[idx : end+1]
		}
	}

	var meta struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Tags    string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &meta); err != nil {
		slog.WarnContext(ctx, "dream: failed to parse resynthesize response", "error", err, "memory_id", memory.ID)
		return false
	}

	if meta.Title != "" {
		memory.Title = meta.Title
	}
	if meta.Summary != "" {
		memory.Summary = meta.Summary
	}
	if meta.Tags != "" {
		tags := strings.Split(meta.Tags, ",")
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
		memory.Tags = tags
	}

	memory.UpdatedAt = time.Now()
	slog.InfoContext(ctx, "dream: resynthesized metadata", "memory_id", memory.ID, "title", memory.Title)
	return true
}

// llmMerge uses Claude to intelligently merge two similar memories into one.

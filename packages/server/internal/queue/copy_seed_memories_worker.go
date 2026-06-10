package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// CopySeedMemoriesWorker — plan 23 §5.3.
//
// Reads enabled seed templates from the system tutorial hub (memories
// with source_kind = "onboarding-seed" + state = "active") and inserts
// per-user copies into the new user's personal hub. The recipient
// memory keeps a `metadata.seed_origin_id` reference back to the
// template so the partial unique index in migration 002 can dedupe
// concurrent re-enqueues at the DB level.
//
// After insert, the copy goes through the same MemoryProcessArgs
// pipeline as a real push so it gets chunked, embedded, classified,
// and surfaceable via recall (otherwise the user could SEE seeds
// in their personal hub but `recall("what is memax")` would return
// nothing — defeating the "memories ARE the tutorial" principle).
//
// Error tolerance: a failure to copy ONE seed doesn't fail the job —
// the user gets the rest, and the next signup still gets all of them
// via the templates that succeeded the previous time. unique_violation
// from the DB partial index is treated as success ("already copied").
type CopySeedMemoriesWorker struct {
	river.WorkerDefaults[CopySeedMemoriesArgs]

	Store       store.Store
	RiverClient *Client
}

// errPgUniqueViolation is the SQLSTATE code Postgres returns when an
// INSERT collides with a unique constraint or partial unique index.
const errPgUniqueViolation = "23505"

func (w *CopySeedMemoriesWorker) Work(ctx context.Context, job *river.Job[CopySeedMemoriesArgs]) error {
	if w.Store == nil {
		return fmt.Errorf("CopySeedMemoriesWorker: store is nil")
	}
	userID := strings.TrimSpace(job.Args.UserID)
	if userID == "" {
		return fmt.Errorf("CopySeedMemoriesWorker: empty user_id")
	}
	_, err := RunSeedCopy(ctx, w.Store, w.RiverClient, userID)
	return err
}

// RunSeedCopy is the reusable body of CopySeedMemoriesWorker. The
// worker calls it after pulling user_id from the job args; the admin
// "sync seeds to my account" handler calls it synchronously so the
// admin gets immediate feedback without round-tripping through River
// (and without bumping into the worker's 24h unique-arg dedup window).
//
// Returns the number of templates actually copied (i.e. NOT counting
// rows that already existed and tripped the partial unique index).
//
// Idempotency contract is unchanged from the worker path — call it
// repeatedly for the same user and only NEW templates land. Callers
// that want a true "reset" must DELETE the prior seed copies first
// (see Store.DeleteSeedCopiesForOwner).
func RunSeedCopy(ctx context.Context, st store.Store, riverClient *Client, userID string) (int, error) {
	if st == nil {
		return 0, fmt.Errorf("RunSeedCopy: store is nil")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return 0, fmt.Errorf("RunSeedCopy: empty user_id")
	}

	personalHub, err := st.GetPersonalHub(userID)
	if err != nil {
		return 0, fmt.Errorf("RunSeedCopy: get personal hub for %s: %w", userID, err)
	}
	if personalHub == nil {
		// New user signup raced ahead of ensurePersonalHub or the hub
		// hasn't landed yet. River will retry per InsertOpts.MaxAttempts;
		// admin sync-self surfaces the error to the operator.
		return 0, fmt.Errorf("RunSeedCopy: personal hub not found for %s", userID)
	}

	templates, err := st.ListSeedMemoryTemplates(ctx)
	if err != nil {
		return 0, fmt.Errorf("RunSeedCopy: list seed templates: %w", err)
	}

	now := time.Now().UTC()
	copiedCount := 0
	for _, template := range templates {
		copy := buildSeedCopy(template, userID, personalHub.ID, now)
		if cerr := st.CreateMemory(&copy); cerr != nil {
			var pgErr *pgconn.PgError
			if errors.As(cerr, &pgErr) && pgErr.Code == errPgUniqueViolation {
				// Already copied — partial unique index from
				// migration 002 caught a concurrent insert. Idempotent
				// success.
				continue
			}
			slog.Warn("RunSeedCopy: copy template failed",
				"user_id", userID, "template_id", template.ID, "error", cerr)
			continue
		}
		copiedCount++

		// Chunk + embed + classify the copy so recall surfaces it.
		// Failure here is not fatal — the memory is in the user's hub
		// even if processing lags.
		if riverClient != nil {
			if perr := riverClient.Insert(ctx, MemoryProcessArgs{
				MemoryID:    copy.ID,
				OwnerID:     copy.OwnerID,
				Content:     copy.Content,
				Title:       copy.Title,
				ContentType: copy.ContentType,
				Source:      copy.Source,
				MemoryKind:  copy.Kind,
				Stability:   copy.Stability,
				Tags:        copy.Tags,
			}, nil); perr != nil {
				slog.Warn("RunSeedCopy: enqueue process failed",
					"user_id", userID, "memory_id", copy.ID, "error", perr)
			}
		}
	}

	slog.Info("RunSeedCopy: seeds copied",
		"user_id", userID, "copied", copiedCount, "templates", len(templates))
	return copiedCount, nil
}

// buildSeedCopy returns a memory ready for CreateMemory: a fresh UUID,
// the recipient as owner + their personal hub, the seed body verbatim,
// and metadata.seed_origin_id set to the template's id so future
// re-enqueues collapse at the DB partial unique index.
//
// Attribution: the copy is system-sourced but the *actor* is the Memax
// agent — these memories are pushed by the platform on the user's
// behalf, not by the user themselves. Wire CreatedByType=agent +
// CreatedBySlug="memax" so the memory-card attribution surface
// renders the Memax brand identity (instead of "you") and so the
// connected_agents stats query attributes the per-agent memory_count
// to memax via its `created_by_slug` join. SourceAgent mirrors the
// slug for older legacy attribution paths that fall back to it.
func buildSeedCopy(template model.Memory, userID string, hubID string, now time.Time) model.Memory {
	contentHash := sha256.Sum256([]byte(template.Content))
	return model.Memory{
		ID:                             uuid.NewString(),
		HubID:                          hubID,
		OwnerID:                        userID,
		Title:                          template.Title,
		Content:                        template.Content,
		ContentType:                    template.ContentType,
		ContentHash:                    hex.EncodeToString(contentHash[:]),
		Summary:                        template.Summary,
		Hint:                           template.Hint,
		Kind:                           template.Kind,
		Stability:                      template.Stability,
		RetrievalWeight:                template.RetrievalWeight,
		Tags:                           append([]string(nil), template.Tags...),
		Boundary:                       "private",
		State:                          "active",
		Source:                         model.MemorySourceSystem,
		SourceKind:                     model.MemorySourceKindOnboard,
		SourceAgent:                    "memax",
		Metadata:                       map[string]any{"seed_origin_id": template.ID},
		Version:                        1,
		CreatedAt:                      now,
		UpdatedAt:                      now,
		AccessedAt:                     now,
		ProvenanceCreatedByType: model.MemoryCreatedByAgent,
		ProvenanceCreatedBySlug: "memax",
		// DisplayName intentionally NOT stamped here — the frontend
		// resolves the visible name from AGENT_IDENTITIES.memax (the
		// single source of truth for the "Memax Agent" identity).
		// Stamping a literal here would race the rename UX on future
		// admin-side identity edits (codex pass 1 review).
		ProvenanceInitiationType: model.MemoryInitiationAgentAutomatic,
	}
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// campaignTemplateCols is the canonical select list. Kept in sync with
// scanCampaignTemplate below.
const campaignTemplateCols = `id::text, slug, name, description, subject,
	body_html, body_text, is_archived,
	created_by::text, updated_by::text, created_at, updated_at`

// pgUniqueViolation is the SQLSTATE code for a unique constraint
// violation. The Postgres driver exposes it via *pgconn.PgError.
const pgUniqueViolation = "23505"

func (s *PostgresStore) ListCampaignTemplates(ctx context.Context, opts model.CampaignTemplateListOpts) ([]model.CampaignTemplate, error) {
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		// 500 is a generous upper bound — the picker UI isn't meant for
		// directory-scale lists; admins should archive old templates.
		limit = 100
	}
	where := "WHERE is_archived = false"
	if opts.IncludeArchived {
		where = ""
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+campaignTemplateCols+`
		 FROM campaign_templates
		 `+where+`
		 ORDER BY is_archived ASC, updated_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list campaign templates: %w", err)
	}
	defer rows.Close()
	out := make([]model.CampaignTemplate, 0)
	for rows.Next() {
		tmpl, err := scanCampaignTemplate(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *tmpl)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetCampaignTemplate(ctx context.Context, id string) (*model.CampaignTemplate, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+campaignTemplateCols+`
		 FROM campaign_templates WHERE id = $1::uuid`, id)
	tmpl, err := scanCampaignTemplate(row.Scan)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrCampaignTemplateNotFound
		}
		return nil, fmt.Errorf("get campaign template: %w", err)
	}
	return tmpl, nil
}

func (s *PostgresStore) GetCampaignTemplateBySlug(ctx context.Context, slug string) (*model.CampaignTemplate, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+campaignTemplateCols+`
		 FROM campaign_templates WHERE slug = $1`, slug)
	tmpl, err := scanCampaignTemplate(row.Scan)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrCampaignTemplateNotFound
		}
		return nil, fmt.Errorf("get campaign template by slug: %w", err)
	}
	return tmpl, nil
}

// CreateCampaignTemplate inserts a new row. The caller is responsible
// for: slug validation (handler enforces the regex), name non-empty,
// and populating the CreatedBy/UpdatedBy fields. Returns
// ErrCampaignTemplateSlugTaken on unique-slug conflict.
func (s *PostgresStore) CreateCampaignTemplate(ctx context.Context, tmpl *model.CampaignTemplate) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO campaign_templates
			(slug, name, description, subject, body_html, body_text,
			 created_by, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6,
		         NULLIF($7, '')::uuid, NULLIF($8, '')::uuid)
		 RETURNING id::text, created_at, updated_at`,
		tmpl.Slug, tmpl.Name, tmpl.Description,
		tmpl.Subject, tmpl.BodyHTML, tmpl.BodyText,
		ptrStringValue(tmpl.CreatedBy), ptrStringValue(tmpl.UpdatedBy),
	).Scan(&tmpl.ID, &tmpl.CreatedAt, &tmpl.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return ErrCampaignTemplateSlugTaken
		}
		return fmt.Errorf("create campaign template: %w", err)
	}
	return nil
}

// UpdateCampaignTemplate applies editable fields. Slug is immutable by
// design — renames are a separate archive-and-recreate flow. Returns
// ErrCampaignTemplateNotFound when no row exists.
func (s *PostgresStore) UpdateCampaignTemplate(ctx context.Context, id string, patch model.CampaignTemplateUpsert, updatedBy *string) (*model.CampaignTemplate, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE campaign_templates SET
			name = $2,
			description = $3,
			subject = $4,
			body_html = $5,
			body_text = $6,
			updated_by = NULLIF($7, '')::uuid,
			updated_at = now()
		 WHERE id = $1::uuid`,
		id, patch.Name, patch.Description,
		patch.Subject, patch.BodyHTML, patch.BodyText,
		ptrStringValue(updatedBy),
	)
	if err != nil {
		return nil, fmt.Errorf("update campaign template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrCampaignTemplateNotFound
	}
	return s.GetCampaignTemplate(ctx, id)
}

// SetCampaignTemplateArchived toggles the soft-delete flag. Archived
// templates stay queryable in the management view but disappear from
// pickers. Idempotent — no error if already in the requested state.
func (s *PostgresStore) SetCampaignTemplateArchived(ctx context.Context, id string, archived bool, updatedBy *string) (*model.CampaignTemplate, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE campaign_templates SET
			is_archived = $2,
			updated_by = NULLIF($3, '')::uuid,
			updated_at = CASE WHEN is_archived <> $2 THEN now() ELSE updated_at END
		 WHERE id = $1::uuid`,
		id, archived, ptrStringValue(updatedBy),
	)
	if err != nil {
		return nil, fmt.Errorf("set campaign template archived: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrCampaignTemplateNotFound
	}
	return s.GetCampaignTemplate(ctx, id)
}

// DeleteCampaignTemplate hard-deletes. Callers should generally prefer
// archive over delete — delete is for "definitely never again" rows.
// Existing campaigns scaffolded from this template are untouched;
// the relationship was authoring-time only.
func (s *PostgresStore) DeleteCampaignTemplate(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM campaign_templates WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("delete campaign template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCampaignTemplateNotFound
	}
	return nil
}

// scanCampaignTemplate mirrors campaignTemplateCols field order. Both
// audit user columns are nullable (users can be deleted); we scan into
// sql.NullString and only populate the model pointers when set.
func scanCampaignTemplate(scan func(dest ...any) error) (*model.CampaignTemplate, error) {
	var t model.CampaignTemplate
	var createdBy, updatedBy sql.NullString
	if err := scan(
		&t.ID, &t.Slug, &t.Name, &t.Description, &t.Subject,
		&t.BodyHTML, &t.BodyText, &t.IsArchived,
		&createdBy, &updatedBy, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if createdBy.Valid && createdBy.String != "" {
		v := createdBy.String
		t.CreatedBy = &v
	}
	if updatedBy.Valid && updatedBy.String != "" {
		v := updatedBy.String
		t.UpdatedBy = &v
	}
	return &t, nil
}

// --- InMemoryStore -------------------------------------------------------

// campaignTemplateStore is the in-memory backing for CampaignTemplate
// CRUD. Kept on InMemoryStore via a lazy-init helper so tests don't
// need to initialize it explicitly.
type campaignTemplateStore struct {
	mu   sync.RWMutex
	byID map[string]*model.CampaignTemplate
	// bySlug mirrors byID under the slug key for O(1) slug lookup +
	// uniqueness enforcement.
	bySlug map[string]*model.CampaignTemplate
}

func (s *InMemoryStore) campaignTemplates() *campaignTemplateStore {
	s.campaignTemplatesOnce.Do(func() {
		s.campaignTemplatesStore = &campaignTemplateStore{
			byID:   make(map[string]*model.CampaignTemplate),
			bySlug: make(map[string]*model.CampaignTemplate),
		}
	})
	return s.campaignTemplatesStore
}

func (s *InMemoryStore) ListCampaignTemplates(_ context.Context, opts model.CampaignTemplateListOpts) ([]model.CampaignTemplate, error) {
	st := s.campaignTemplates()
	st.mu.RLock()
	defer st.mu.RUnlock()
	out := make([]model.CampaignTemplate, 0, len(st.byID))
	for _, t := range st.byID {
		if t.IsArchived && !opts.IncludeArchived {
			continue
		}
		out = append(out, cloneCampaignTemplate(t))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsArchived != out[j].IsArchived {
			return !out[i].IsArchived // active first
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	limit := opts.Limit
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *InMemoryStore) GetCampaignTemplate(_ context.Context, id string) (*model.CampaignTemplate, error) {
	st := s.campaignTemplates()
	st.mu.RLock()
	defer st.mu.RUnlock()
	t, ok := st.byID[id]
	if !ok {
		return nil, ErrCampaignTemplateNotFound
	}
	cp := cloneCampaignTemplate(t)
	return &cp, nil
}

func (s *InMemoryStore) GetCampaignTemplateBySlug(_ context.Context, slug string) (*model.CampaignTemplate, error) {
	st := s.campaignTemplates()
	st.mu.RLock()
	defer st.mu.RUnlock()
	t, ok := st.bySlug[slug]
	if !ok {
		return nil, ErrCampaignTemplateNotFound
	}
	cp := cloneCampaignTemplate(t)
	return &cp, nil
}

func (s *InMemoryStore) CreateCampaignTemplate(_ context.Context, tmpl *model.CampaignTemplate) error {
	st := s.campaignTemplates()
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, exists := st.bySlug[tmpl.Slug]; exists {
		return ErrCampaignTemplateSlugTaken
	}
	if tmpl.ID == "" {
		tmpl.ID = newCampaignTemplateID()
	}
	now := time.Now().UTC()
	tmpl.CreatedAt = now
	tmpl.UpdatedAt = now
	cp := cloneCampaignTemplate(tmpl)
	st.byID[tmpl.ID] = &cp
	st.bySlug[tmpl.Slug] = &cp
	return nil
}

func (s *InMemoryStore) UpdateCampaignTemplate(_ context.Context, id string, patch model.CampaignTemplateUpsert, updatedBy *string) (*model.CampaignTemplate, error) {
	st := s.campaignTemplates()
	st.mu.Lock()
	defer st.mu.Unlock()
	existing, ok := st.byID[id]
	if !ok {
		return nil, ErrCampaignTemplateNotFound
	}
	existing.Name = patch.Name
	existing.Description = patch.Description
	existing.Subject = patch.Subject
	existing.BodyHTML = patch.BodyHTML
	existing.BodyText = patch.BodyText
	if updatedBy != nil {
		v := *updatedBy
		existing.UpdatedBy = &v
	}
	existing.UpdatedAt = time.Now().UTC()
	cp := cloneCampaignTemplate(existing)
	return &cp, nil
}

func (s *InMemoryStore) SetCampaignTemplateArchived(_ context.Context, id string, archived bool, updatedBy *string) (*model.CampaignTemplate, error) {
	st := s.campaignTemplates()
	st.mu.Lock()
	defer st.mu.Unlock()
	existing, ok := st.byID[id]
	if !ok {
		return nil, ErrCampaignTemplateNotFound
	}
	if existing.IsArchived != archived {
		existing.IsArchived = archived
		existing.UpdatedAt = time.Now().UTC()
	}
	if updatedBy != nil {
		v := *updatedBy
		existing.UpdatedBy = &v
	}
	cp := cloneCampaignTemplate(existing)
	return &cp, nil
}

func (s *InMemoryStore) DeleteCampaignTemplate(_ context.Context, id string) error {
	st := s.campaignTemplates()
	st.mu.Lock()
	defer st.mu.Unlock()
	existing, ok := st.byID[id]
	if !ok {
		return ErrCampaignTemplateNotFound
	}
	delete(st.byID, id)
	delete(st.bySlug, existing.Slug)
	return nil
}

func cloneCampaignTemplate(src *model.CampaignTemplate) model.CampaignTemplate {
	cp := *src
	if src.CreatedBy != nil {
		v := *src.CreatedBy
		cp.CreatedBy = &v
	}
	if src.UpdatedBy != nil {
		v := *src.UpdatedBy
		cp.UpdatedBy = &v
	}
	return cp
}

// newCampaignTemplateID generates a UUID for newly-created rows in
// the InMemory path. Postgres generates its own via gen_random_uuid(),
// so this only fires in dev/test where we need something unique and
// stable. google/uuid is already a transitive dep of the server; using
// it here keeps the format consistent with production rows.
func newCampaignTemplateID() string {
	return uuid.NewString()
}

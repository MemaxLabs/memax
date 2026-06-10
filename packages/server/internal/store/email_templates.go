package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// selectEmailTemplateCols is the canonical column list for override
// SELECTs. Draft columns (draft_*) land in DraftSubject/DraftHTML/
// DraftText; subject/html/text are the PUBLISHED content since
// migration 009.
const selectEmailTemplateCols = `name, subject, html, text,
	draft_subject, draft_html, draft_text, published_at,
	notes, editor_kind, editor_state, updated_by::text, created_at, updated_at`

func (s *PostgresStore) ListEmailTemplateOverrides(ctx context.Context) ([]model.EmailTemplateOverride, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+selectEmailTemplateCols+`
		 FROM email_template_overrides
		 ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list email template overrides: %w", err)
	}
	defer rows.Close()

	var overrides []model.EmailTemplateOverride
	for rows.Next() {
		override, err := scanEmailTemplateOverride(rows.Scan)
		if err != nil {
			return nil, err
		}
		overrides = append(overrides, *override)
	}
	return overrides, rows.Err()
}

func (s *PostgresStore) GetEmailTemplateOverride(ctx context.Context, name string) (*model.EmailTemplateOverride, error) {
	override, err := scanEmailTemplateOverride(s.pool.QueryRow(ctx,
		`SELECT `+selectEmailTemplateCols+`
		 FROM email_template_overrides
		 WHERE name = $1`, name).Scan)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get email template override: %w", err)
	}
	return override, nil
}

// UpsertEmailTemplateOverride writes DRAFT content. Published content
// (subject/html/text) only advances when PublishEmailTemplateOverride
// runs. On the very first save (INSERT path) we mirror draft → published
// so a never-published override still produces legible real sends — any
// later save is draft-only until Publish fires.
func (s *PostgresStore) UpsertEmailTemplateOverride(ctx context.Context, override *model.EmailTemplateOverride) error {
	stateJSON, _ := json.Marshal(override.EditorState)
	err := s.pool.QueryRow(ctx,
		`INSERT INTO email_template_overrides (
			name,
			subject, html, text,
			draft_subject, draft_html, draft_text,
			published_at,
			notes, editor_kind, editor_state, updated_by
		 )
		 VALUES (
			$1,
			$2, $3, $4,
			$2, $3, $4,
			NULL,
			$5, $6, $7::jsonb, NULLIF($8, '')::uuid
		 )
		 ON CONFLICT (name) DO UPDATE SET
			draft_subject = EXCLUDED.draft_subject,
			draft_html = EXCLUDED.draft_html,
			draft_text = EXCLUDED.draft_text,
			notes = EXCLUDED.notes,
			editor_kind = EXCLUDED.editor_kind,
			editor_state = EXCLUDED.editor_state,
			updated_by = EXCLUDED.updated_by,
			updated_at = now()
		 RETURNING created_at, updated_at`,
		override.Name,
		override.DraftSubject, override.DraftHTML, override.DraftText,
		override.Notes, override.EditorKind, stateJSON, ptrStringValue(override.UpdatedBy),
	).Scan(&override.CreatedAt, &override.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert email template override: %w", err)
	}
	// Audit rev captures the draft snapshot — what the admin just saved.
	// We reuse the revision table's subject/html/text columns to hold
	// whatever content this "saved" action wrote (the draft), since
	// that's the admin-visible change.
	revSnap := *override
	revSnap.Subject = override.DraftSubject
	revSnap.HTML = override.DraftHTML
	revSnap.Text = override.DraftText
	if err := s.insertEmailTemplateRevision(ctx, &revSnap, "saved", override.UpdatedBy); err != nil {
		return err
	}
	return nil
}

// PublishEmailTemplateOverride atomically promotes draft_* → published
// columns and stamps published_at. Returns ErrEmailTemplateNotFound
// when no override row exists for `name` (admin must save a draft
// first).
//
// Idempotent: the UPDATE's WHERE clause only fires when the draft
// actually differs from the currently-published content. A
// double-click, a retry, or a "Publish" with no outstanding edits
// returns the current row unchanged — no new revision entry, no
// published_at drift. That keeps the audit history meaningful
// ("published" entries always mark a real content change).
func (s *PostgresStore) PublishEmailTemplateOverride(ctx context.Context, name string, publishedBy *string) (*model.EmailTemplateOverride, error) {
	existing, err := s.GetEmailTemplateOverride(ctx, name)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrEmailTemplateNotFound
	}
	if !existing.HasUnpublishedDraft() && existing.PublishedAt != nil {
		// Draft matches published and we've published before — no-op.
		// Return the current row so callers don't need to special-case.
		return existing, nil
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE email_template_overrides SET
			subject = draft_subject,
			html = draft_html,
			text = draft_text,
			published_at = now(),
			updated_by = NULLIF($2, '')::uuid,
			updated_at = now()
		 WHERE name = $1
		   AND (subject IS DISTINCT FROM draft_subject
		     OR html IS DISTINCT FROM draft_html
		     OR text IS DISTINCT FROM draft_text
		     OR published_at IS NULL)`,
		name, ptrStringValue(publishedBy),
	)
	if err != nil {
		return nil, fmt.Errorf("publish email template override: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either (a) a concurrent publisher beat us to it — draft ==
		// published now and the WHERE guard short-circuited; or (b) the
		// row was deleted in the window between our read and the
		// UPDATE. Re-read to distinguish: present → idempotent no-op,
		// missing → signal not-found so the handler returns 404 rather
		// than masking a delete as a successful publish.
		latest, rerr := s.GetEmailTemplateOverride(ctx, name)
		if rerr != nil {
			return nil, rerr
		}
		if latest == nil {
			return nil, ErrEmailTemplateNotFound
		}
		return latest, nil
	}
	published, err := s.GetEmailTemplateOverride(ctx, name)
	if err != nil {
		return nil, err
	}
	if published == nil {
		// Extremely unlikely race (row deleted between UPDATE and
		// SELECT in the same request); treat as not-found for caller.
		return nil, ErrEmailTemplateNotFound
	}
	if err := s.insertEmailTemplateRevision(ctx, published, "published", publishedBy); err != nil {
		return nil, err
	}
	return published, nil
}

func (s *PostgresStore) DeleteEmailTemplateOverride(ctx context.Context, name string, deletedBy *string) error {
	existing, err := s.GetEmailTemplateOverride(ctx, name)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM email_template_overrides WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("delete email template override: %w", err)
	}
	if existing != nil {
		if err := s.insertEmailTemplateRevision(ctx, existing, "reset", deletedBy); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) ListEmailTemplateRevisions(ctx context.Context, name string, limit int) ([]model.EmailTemplateRevision, error) {
	if limit <= 0 {
		limit = 12
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, name, action, subject, html, text, notes, editor_kind, editor_state, created_by::text, created_at
		 FROM email_template_override_revisions
		 WHERE name = $1
		 ORDER BY created_at DESC
		 LIMIT $2`, name, limit)
	if err != nil {
		return nil, fmt.Errorf("list email template revisions: %w", err)
	}
	defer rows.Close()

	revisions := make([]model.EmailTemplateRevision, 0, limit)
	for rows.Next() {
		revision, err := scanEmailTemplateRevision(rows.Scan)
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, *revision)
	}
	return revisions, rows.Err()
}

func scanEmailTemplateOverride(scan func(dest ...any) error) (*model.EmailTemplateOverride, error) {
	var override model.EmailTemplateOverride
	var stateJSON []byte
	// updated_by is nullable — rows seeded by a deleted admin or legacy
	// backfill paths can be NULL. Scan into NullString so the whole
	// endpoint doesn't 500 on a single historical row.
	var updatedBy sql.NullString
	// published_at is nullable — a freshly-saved override that hasn't
	// been promoted yet has draft_* populated but no published stamp.
	var publishedAt sql.NullTime
	if err := scan(
		&override.Name,
		&override.Subject,
		&override.HTML,
		&override.Text,
		&override.DraftSubject,
		&override.DraftHTML,
		&override.DraftText,
		&publishedAt,
		&override.Notes,
		&override.EditorKind,
		&stateJSON,
		&updatedBy,
		&override.CreatedAt,
		&override.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(stateJSON) > 0 {
		_ = json.Unmarshal(stateJSON, &override.EditorState)
	}
	if updatedBy.Valid && updatedBy.String != "" {
		value := updatedBy.String
		override.UpdatedBy = &value
	}
	if publishedAt.Valid {
		ts := publishedAt.Time
		override.PublishedAt = &ts
	}
	return &override, nil
}

func scanEmailTemplateRevision(scan func(dest ...any) error) (*model.EmailTemplateRevision, error) {
	var revision model.EmailTemplateRevision
	// Same nullable-scan rationale as scanEmailTemplateOverride — created_by
	// may be NULL for legacy rows or deleted users. See note there.
	var createdBy sql.NullString
	var stateJSON []byte
	if err := scan(
		&revision.ID,
		&revision.Name,
		&revision.Action,
		&revision.Subject,
		&revision.HTML,
		&revision.Text,
		&revision.Notes,
		&revision.EditorKind,
		&stateJSON,
		&createdBy,
		&revision.CreatedAt,
	); err != nil {
		return nil, err
	}
	if len(stateJSON) > 0 {
		_ = json.Unmarshal(stateJSON, &revision.EditorState)
	}
	if createdBy.Valid && createdBy.String != "" {
		value := createdBy.String
		revision.CreatedBy = &value
	}
	return &revision, nil
}

func (s *PostgresStore) insertEmailTemplateRevision(ctx context.Context, override *model.EmailTemplateOverride, action string, createdBy *string) error {
	stateJSON, _ := json.Marshal(override.EditorState)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO email_template_override_revisions
		 (name, action, subject, html, text, notes, editor_kind, editor_state, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, NULLIF($9, '')::uuid)`,
		override.Name, action, override.Subject, override.HTML, override.Text, override.Notes, override.EditorKind, stateJSON, ptrStringValue(createdBy),
	)
	if err != nil {
		return fmt.Errorf("insert email template revision: %w", err)
	}
	return nil
}

func ptrStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *InMemoryStore) ListEmailTemplateOverrides(_ context.Context) ([]model.EmailTemplateOverride, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	overrides := make([]model.EmailTemplateOverride, 0, len(s.emailTemplateOverrides))
	for _, override := range s.emailTemplateOverrides {
		overrides = append(overrides, *cloneEmailTemplateOverride(override))
	}
	return overrides, nil
}

func (s *InMemoryStore) GetEmailTemplateOverride(_ context.Context, name string) (*model.EmailTemplateOverride, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	override, ok := s.emailTemplateOverrides[name]
	if !ok {
		return nil, nil
	}
	return cloneEmailTemplateOverride(override), nil
}

// UpsertEmailTemplateOverride mirrors the Postgres semantics (see the
// doc on the Postgres method): the DRAFT columns advance on every save;
// published_* only change when Publish runs. First insert mirrors draft
// → published so a never-published row still produces legible real
// sends; subsequent saves are draft-only.
func (s *InMemoryStore) UpsertEmailTemplateOverride(_ context.Context, override *model.EmailTemplateOverride) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	existing, existed := s.emailTemplateOverrides[override.Name]
	if existed {
		override.CreatedAt = existing.CreatedAt
		// Preserve published snapshot across draft saves — only Publish
		// can advance it. Without this, each draft save in InMemoryStore
		// would erroneously mirror draft → published.
		override.Subject = existing.Subject
		override.HTML = existing.HTML
		override.Text = existing.Text
		override.PublishedAt = existing.PublishedAt
	} else {
		override.CreatedAt = now
		// First save on this template: mirror draft → published so real
		// sends have *something* resembling the admin's intent even if
		// they haven't clicked Publish yet. Matches the Postgres INSERT
		// branch. PublishedAt stays nil so the UI correctly surfaces
		// "never published".
		override.Subject = override.DraftSubject
		override.HTML = override.DraftHTML
		override.Text = override.DraftText
		override.PublishedAt = nil
	}
	override.UpdatedAt = now
	s.emailTemplateOverrides[override.Name] = cloneEmailTemplateOverride(override)
	// Revision records the draft snapshot (what the admin just saved).
	revSnap := *override
	revSnap.Subject = override.DraftSubject
	revSnap.HTML = override.DraftHTML
	revSnap.Text = override.DraftText
	s.appendEmailTemplateRevisionLocked(&revSnap, "saved", override.UpdatedBy, now)
	return nil
}

// PublishEmailTemplateOverride promotes draft_* → published fields.
// Stamps published_at = now. Returns ErrEmailTemplateNotFound if the
// admin hasn't saved a draft yet (no row exists). Idempotent: a
// republish with no content delta returns the current row unchanged —
// no revision entry, no published_at drift. Matches Postgres semantics.
func (s *InMemoryStore) PublishEmailTemplateOverride(_ context.Context, name string, publishedBy *string) (*model.EmailTemplateOverride, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	override, ok := s.emailTemplateOverrides[name]
	if !ok {
		return nil, ErrEmailTemplateNotFound
	}
	if !override.HasUnpublishedDraft() && override.PublishedAt != nil {
		return cloneEmailTemplateOverride(override), nil
	}
	now := time.Now()
	override.Subject = override.DraftSubject
	override.HTML = override.DraftHTML
	override.Text = override.DraftText
	override.PublishedAt = &now
	override.UpdatedAt = now
	if publishedBy != nil {
		value := *publishedBy
		override.UpdatedBy = &value
	}
	s.appendEmailTemplateRevisionLocked(override, "published", publishedBy, now)
	return cloneEmailTemplateOverride(override), nil
}

func (s *InMemoryStore) DeleteEmailTemplateOverride(_ context.Context, name string, deletedBy *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.emailTemplateOverrides[name]; ok {
		s.appendEmailTemplateRevisionLocked(existing, "reset", deletedBy, time.Now())
	}
	delete(s.emailTemplateOverrides, name)
	return nil
}

func cloneEmailTemplateOverride(override *model.EmailTemplateOverride) *model.EmailTemplateOverride {
	if override == nil {
		return nil
	}
	copy := *override
	if override.UpdatedBy != nil {
		value := *override.UpdatedBy
		copy.UpdatedBy = &value
	}
	if override.EditorState != nil {
		state := make(map[string]any, len(override.EditorState))
		for key, value := range override.EditorState {
			state[key] = value
		}
		copy.EditorState = state
	}
	return &copy
}

func (s *InMemoryStore) ListEmailTemplateRevisions(_ context.Context, name string, limit int) ([]model.EmailTemplateRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	history := s.emailTemplateRevisionHistory[name]
	if limit <= 0 || limit > len(history) {
		limit = len(history)
	}
	revisions := make([]model.EmailTemplateRevision, 0, limit)
	for i := len(history) - 1; i >= 0 && len(revisions) < limit; i-- {
		revisions = append(revisions, *cloneEmailTemplateRevision(history[i]))
	}
	return revisions, nil
}

func cloneEmailTemplateRevision(revision *model.EmailTemplateRevision) *model.EmailTemplateRevision {
	if revision == nil {
		return nil
	}
	copy := *revision
	if revision.CreatedBy != nil {
		value := *revision.CreatedBy
		copy.CreatedBy = &value
	}
	if revision.EditorState != nil {
		state := make(map[string]any, len(revision.EditorState))
		for key, value := range revision.EditorState {
			state[key] = value
		}
		copy.EditorState = state
	}
	return &copy
}

func (s *InMemoryStore) appendEmailTemplateRevisionLocked(override *model.EmailTemplateOverride, action string, createdBy *string, createdAt time.Time) {
	revision := &model.EmailTemplateRevision{
		ID:         fmt.Sprintf("%s-%d", override.Name, createdAt.UnixNano()),
		Name:       override.Name,
		Action:     action,
		Subject:    override.Subject,
		HTML:       override.HTML,
		Text:       override.Text,
		Notes:      override.Notes,
		EditorKind: override.EditorKind,
		CreatedAt:  createdAt,
	}
	if createdBy != nil {
		value := *createdBy
		revision.CreatedBy = &value
	}
	if override.EditorState != nil {
		state := make(map[string]any, len(override.EditorState))
		for key, value := range override.EditorState {
			state[key] = value
		}
		revision.EditorState = state
	}
	s.emailTemplateRevisionHistory[override.Name] = append(s.emailTemplateRevisionHistory[override.Name], revision)
}

package model

import "time"

// EmailTemplateOverride stores admin-authored overrides for a built-in
// email template. Since migration 009 the Subject/HTML/Text columns
// represent the PUBLISHED content — what real sends use. Admin edits
// write to DraftSubject/DraftHTML/DraftText and only promote to the
// published columns when the admin clicks Publish.
//
// Invariants after migration 009:
//   - PublishedAt == nil → never published (brand-new override); real
//     sends fall back to file-based defaults. Test-send uses draft.
//   - PublishedAt != nil, draft == published → clean; no unpublished
//     changes pending. Test-send and real send produce identical output.
//   - PublishedAt != nil, draft != published → admin has edits in
//     flight; test-send uses draft (so admin can preview), real sends
//     still use published (so mistakes don't leak to production).
type EmailTemplateOverride struct {
	Name         string         `json:"name"`
	Subject      string         `json:"subject"`
	HTML         string         `json:"html"`
	Text         string         `json:"text"`
	DraftSubject string         `json:"draft_subject"`
	DraftHTML    string         `json:"draft_html"`
	DraftText    string         `json:"draft_text"`
	PublishedAt  *time.Time     `json:"published_at,omitempty"`
	Notes        string         `json:"notes"`
	EditorKind   string         `json:"editor_kind"`
	EditorState  map[string]any `json:"editor_state,omitempty"`
	UpdatedBy    *string        `json:"updated_by,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// HasUnpublishedDraft is true when the admin has saved edits that
// haven't been promoted to the published columns yet. The UI shows a
// "draft — not published" badge based on this, and real sends continue
// to ignore the draft until Publish fires.
func (o *EmailTemplateOverride) HasUnpublishedDraft() bool {
	if o == nil {
		return false
	}
	return o.DraftSubject != o.Subject ||
		o.DraftHTML != o.HTML ||
		o.DraftText != o.Text
}

type EmailTemplateRevision struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Action      string         `json:"action"`
	Subject     string         `json:"subject"`
	HTML        string         `json:"html"`
	Text        string         `json:"text"`
	Notes       string         `json:"notes"`
	EditorKind  string         `json:"editor_kind"`
	EditorState map[string]any `json:"editor_state,omitempty"`
	CreatedBy   *string        `json:"created_by,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// EmailTemplateVariable describes one supported template variable.
type EmailTemplateVariable struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	Example     string `json:"example"`
}

// AdminEmailTemplateSummary is the lightweight card/list payload for admin UI.
type AdminEmailTemplateSummary struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Subject     string `json:"subject"`
	HasOverride bool   `json:"has_override"`
	// HasUnpublishedDraft surfaces the "you edited but didn't publish"
	// state in the list view so admins can spot stale drafts at a
	// glance without opening each editor. Mirrors the per-row badge.
	HasUnpublishedDraft bool       `json:"has_unpublished_draft"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
	UpdatedBy           *string    `json:"updated_by,omitempty"`
	VariableCount       int        `json:"variable_count"`
}

// AdminEmailTemplatePreview is a fully rendered preview variant.
type AdminEmailTemplatePreview struct {
	Subject string            `json:"subject"`
	HTML    string            `json:"html"`
	Text    string            `json:"text"`
	Data    map[string]string `json:"data"`
}

type AdminEmailTemplateSendResult struct {
	Status   string `json:"status"`
	Name     string `json:"name"`
	To       string `json:"to"`
	Subject  string `json:"subject"`
	QueuedAt string `json:"queued_at"`
}

// AdminEmailTemplateDetail is the full editor payload for one template.
// Two parallel content fieldsets are surfaced:
//   - effective_* is DRAFT-aware (prefers DraftSubject/HTML/Text, falls
//     back to published, then file default). Drives the editor and
//     editor-side preview. Never use these for anything that reaches
//     real recipients through any path.
//   - published_* mirrors what real transactional sends would render
//     right now (no draft involvement). Callers that need a "safe to
//     ship" snapshot — the campaign scaffold picker, any future reuse
//     flows — should read these instead.
type AdminEmailTemplateDetail struct {
	Name                string                  `json:"name"`
	Category            string                  `json:"category"`
	Description         string                  `json:"description"`
	Variables           []EmailTemplateVariable `json:"variables"`
	SampleData          map[string]string       `json:"sample_data"`
	DefaultSubject      string                  `json:"default_subject"`
	DefaultHTML         string                  `json:"default_html"`
	DefaultText         string                  `json:"default_text"`
	EffectiveSubject    string                  `json:"effective_subject"`
	EffectiveHTML       string                  `json:"effective_html"`
	EffectiveText       string                  `json:"effective_text"`
	EffectiveEditorKind string                  `json:"effective_editor_kind"`
	// Published fieldset — draft-free. Matches what Render() emits for
	// the real send path; safe for cross-context reuse (scaffold picker).
	PublishedSubject string `json:"published_subject"`
	PublishedHTML    string `json:"published_html"`
	PublishedText    string `json:"published_text"`
	// PublishedAvailable is false for templates that have never had a
	// published override — admin is editing the file default. UI uses
	// this to grey out "use as scaffold" for unpublished-draft templates.
	PublishedAvailable bool                      `json:"published_available"`
	Override           *EmailTemplateOverride    `json:"override,omitempty"`
	Revisions          []EmailTemplateRevision   `json:"revisions"`
	Preview            AdminEmailTemplatePreview `json:"preview"`
}

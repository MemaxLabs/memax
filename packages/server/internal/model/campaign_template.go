package model

import "time"

// CampaignTemplate is an admin-authored, reusable email campaign body.
// The relationship to campaigns is authoring-time only: the picker
// COPIES subject/body_html/body_text into a campaign's content at
// create-time, so editing a template afterwards does NOT retroactively
// mutate any campaign that was scaffolded from it. Deleting a template
// also doesn't touch those campaigns.
type CampaignTemplate struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Subject     string    `json:"subject"`
	BodyHTML    string    `json:"body_html"`
	BodyText    string    `json:"body_text"`
	IsArchived  bool      `json:"is_archived"`
	CreatedBy   *string   `json:"created_by,omitempty"`
	UpdatedBy   *string   `json:"updated_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CampaignTemplateListOpts filters the admin list. Archived templates
// are excluded by default to keep pickers clean; the management view
// toggles IncludeArchived=true so admins can unarchive.
type CampaignTemplateListOpts struct {
	IncludeArchived bool
	Limit           int
}

// CampaignTemplateUpsert carries the fields an admin can modify.
// Slug is set at creation only — renames are "new template with new
// slug, then archive/delete the old one" to keep URL stability in
// audit trails and the picker's option list stable for bookmarks.
type CampaignTemplateUpsert struct {
	Slug        string // create-only; ignored on Update
	Name        string
	Description string
	Subject     string
	BodyHTML    string
	BodyText    string
}

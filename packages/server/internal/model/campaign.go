package model

import (
	"encoding/json"
	"time"
)

// Campaign lifecycle statuses.
const (
	CampaignStatusDraft     = "draft"
	CampaignStatusScheduled = "scheduled"
	CampaignStatusSending   = "sending"
	CampaignStatusSent      = "sent"
	CampaignStatusCancelled = "cancelled"
	CampaignStatusFailed    = "failed"
)

// Campaign channels.
const (
	CampaignChannelInapp = "inapp"
	CampaignChannelEmail = "email"
)

// Campaign kinds. system_notice and gift_invite_link are in-app only.
// email_announcement is the single kind used for channel=email campaigns:
// admin-authored subject + body, wrapped in the shared email base layout
// at send time.
const (
	CampaignKindSystemNotice      = "system_notice"
	CampaignKindGiftInviteLink    = "gift_invite_link"
	CampaignKindEmailAnnouncement = "email_announcement"
)

// ValidCampaignChannel reports whether c is a supported channel.
func ValidCampaignChannel(c string) bool {
	switch c {
	case CampaignChannelInapp, CampaignChannelEmail:
		return true
	}
	return false
}

// EmailAnnouncementContent is the canonical shape for channel=email
// campaigns. Subject + body are admin-authored plain HTML/text that
// TemplateManager wraps with the current brand layout at render time.
type EmailAnnouncementContent struct {
	Subject  string `json:"subject"`
	BodyHTML string `json:"body_html"`
	BodyText string `json:"body_text"`
}

// Audience rule types.
const (
	AudienceRuleAll   = "all"
	AudienceRuleUsers = "users"
	AudienceRuleHub   = "hub"
)

// Comms audit actions. Writes are append-only.
const (
	CommsAuditActionCreated       = "created"
	CommsAuditActionUpdated       = "updated"
	CommsAuditActionScheduled     = "scheduled"
	CommsAuditActionSent          = "send_requested"
	CommsAuditActionCancelled     = "cancelled"
	CommsAuditActionSendCompleted = "send_completed"
	CommsAuditActionSendFailed    = "send_failed"
	CommsAuditActionDeleted       = "deleted"
	// TestSend is distinct from Sent so timelines show founder dry-runs
	// separately from the actual broadcast — easy to scan for "did we
	// test before we fired it?".
	CommsAuditActionTestSend = "test_send"

	CommsAuditResourceCampaign = "campaign"
	CommsAuditResourceAudience = "audience"
)

// Audience is a saved, reusable recipient rule. Campaigns can either
// reference one by ID or embed an inline snapshot.
type Audience struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	RuleType    string       `json:"rule_type"`
	Rule        AudienceRule `json:"rule"`
	CreatedBy   string       `json:"created_by"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// AudienceRule carries the fields each rule type uses. Only the fields
// relevant to the rule type are populated.
//
//	all   — no fields required
//	users — UserIDs + Emails
//	hub   — HubID (required), HubMemberRole (optional)
type AudienceRule struct {
	UserIDs       []string `json:"user_ids,omitempty"`
	Emails        []string `json:"emails,omitempty"`
	HubID         string   `json:"hub_id,omitempty"`
	HubMemberRole string   `json:"hub_member_role,omitempty"`
}

// AudienceEstimate is the result of resolving a rule against current
// user state. Count is the total recipient count; Sample is up to a
// handful of user IDs for UI preview.
type AudienceEstimate struct {
	Count  int      `json:"count"`
	Sample []string `json:"sample"`
}

// Campaign is a persistent admin communication.
//
// Invariants enforced at the store layer:
//   - status transitions are explicit (no free-form UpdateCampaign)
//   - content is opaque JSON validated against Kind at handler layer
//   - audience_snapshot is the source of truth at send time; audience_id
//     is preserved for UI display but can be nil for inline campaigns
type Campaign struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Channel          string            `json:"channel"`
	Kind             string            `json:"kind"`
	Status           string            `json:"status"`
	AudienceID       *string           `json:"audience_id,omitempty"`
	AudienceSnapshot *AudienceSnapshot `json:"audience_snapshot,omitempty"`
	Content          json.RawMessage   `json:"content"`
	ScheduledAt      *time.Time        `json:"scheduled_at,omitempty"`
	SendStartedAt    *time.Time        `json:"send_started_at,omitempty"`
	SendFinishedAt   *time.Time        `json:"send_finished_at,omitempty"`
	SentCount        int               `json:"sent_count"`
	FailedCount      int               `json:"failed_count"`
	BatchID          *string           `json:"batch_id,omitempty"`
	ErrorMessage     *string           `json:"error_message,omitempty"`
	CreatedBy        string            `json:"created_by"`
	UpdatedBy        *string           `json:"updated_by,omitempty"`
	CancelledBy      *string           `json:"cancelled_by,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// AudienceSnapshot captures the rule to execute at send time, even if
// the underlying audience has since been edited or deleted.
type AudienceSnapshot struct {
	AudienceID *string      `json:"audience_id,omitempty"`
	RuleType   string       `json:"rule_type"`
	Rule       AudienceRule `json:"rule"`
}

// CommsAuditEntry is one append-only row in the audit log. ActorID is
// nil for worker-authored entries (send_completed, send_failed).
type CommsAuditEntry struct {
	ID           string          `json:"id"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Action       string          `json:"action"`
	ActorID      *string         `json:"actor_id,omitempty"`
	Metadata     json.RawMessage `json:"metadata"`
	CreatedAt    time.Time       `json:"created_at"`
}

// CampaignListOpts filters the admin campaigns list.
type CampaignListOpts struct {
	Status string
	Limit  int
	Cursor string
}

// ValidCampaignStatus reports whether s is one of the six canonical
// lifecycle statuses.
func ValidCampaignStatus(s string) bool {
	switch s {
	case CampaignStatusDraft,
		CampaignStatusScheduled,
		CampaignStatusSending,
		CampaignStatusSent,
		CampaignStatusCancelled,
		CampaignStatusFailed:
		return true
	}
	return false
}

// ValidAudienceRuleType reports whether t is a supported rule.
func ValidAudienceRuleType(t string) bool {
	switch t {
	case AudienceRuleAll, AudienceRuleUsers, AudienceRuleHub:
		return true
	}
	return false
}

// CampaignIsTerminal reports whether the status is a terminal state
// (no further transitions allowed).
func CampaignIsTerminal(s string) bool {
	return s == CampaignStatusSent ||
		s == CampaignStatusCancelled ||
		s == CampaignStatusFailed
}

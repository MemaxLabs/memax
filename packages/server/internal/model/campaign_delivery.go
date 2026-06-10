package model

import "time"

// CampaignDelivery tracks one recipient's lifecycle within an email
// campaign send. One row per (campaign_id, recipient_email). Populated
// by SendCampaignEmailWorker; status transitions driven by the Resend
// webhook handler (delivered/bounced/complained/opened).
type CampaignDelivery struct {
	ID                string     `json:"id"`
	CampaignID        string     `json:"campaign_id"`
	BatchID           string     `json:"batch_id"`
	RecipientUserID   *string    `json:"recipient_user_id,omitempty"`
	RecipientEmail    string     `json:"recipient_email"`
	Status            string     `json:"status"`
	ProviderMessageID string     `json:"provider_message_id"`
	ErrorCode         string     `json:"error_code"`
	ErrorMessage      string     `json:"error_message"`
	QueuedAt          time.Time  `json:"queued_at"`
	SentAt            *time.Time `json:"sent_at,omitempty"`
	DeliveredAt       *time.Time `json:"delivered_at,omitempty"`
	OpenedAt          *time.Time `json:"opened_at,omitempty"`
	BouncedAt         *time.Time `json:"bounced_at,omitempty"`
	ComplainedAt      *time.Time `json:"complained_at,omitempty"`
}

// CampaignDelivery statuses.
const (
	DeliveryStatusQueued     = "queued"
	DeliveryStatusSent       = "sent"
	DeliveryStatusDelivered  = "delivered"
	DeliveryStatusBounced    = "bounced"
	DeliveryStatusComplained = "complained"
	DeliveryStatusFailed     = "failed"
	DeliveryStatusSuppressed = "suppressed" // opted-out, never sent
)

// CampaignDeliveryStats is the rollup shown on the campaign detail page.
type CampaignDeliveryStats struct {
	Total      int `json:"total"`
	Queued     int `json:"queued"`
	Sent       int `json:"sent"`
	Delivered  int `json:"delivered"`
	Opened     int `json:"opened"`
	Bounced    int `json:"bounced"`
	Complained int `json:"complained"`
	Failed     int `json:"failed"`
	Suppressed int `json:"suppressed"`
}

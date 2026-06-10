package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/email"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// ResendWebhookHandler processes delivery-lifecycle events from Resend.
// Events arrive with a Svix-signed payload; verification is mandatory
// when the secret is configured. Without a configured secret the handler
// returns 503 — we refuse to accept unsigned deliveries in production.
type ResendWebhookHandler struct {
	store  store.Store
	secret *email.ResendWebhookSecret
}

func NewResendWebhookHandler(s store.Store, secret *email.ResendWebhookSecret) *ResendWebhookHandler {
	return &ResendWebhookHandler{store: s, secret: secret}
}

type resendEventPayload struct {
	Type      string          `json:"type"`
	CreatedAt string          `json:"created_at"`
	Data      resendEventData `json:"data"`
}

type resendEventData struct {
	EmailID string       `json:"email_id"`
	To      []string     `json:"to"`
	Subject string       `json:"subject"`
	Tags    []resendTag  `json:"tags"`
	Bounce  *resendError `json:"bounce,omitempty"`
}

type resendTag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type resendError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Receive handles POST /v1/webhooks/resend. The handler returns 2xx on
// every valid-signature event (even ones we can't match to a delivery
// row) so Resend doesn't retry indefinitely.
func (h *ResendWebhookHandler) Receive(w http.ResponseWriter, r *http.Request) {
	if h.secret == nil {
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	if err := h.secret.Verify(
		r.Header.Get("svix-id"),
		r.Header.Get("svix-timestamp"),
		r.Header.Get("svix-signature"),
		body,
		0,
	); err != nil {
		slog.Warn("resend webhook: signature verification failed", "error", err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var event resendEventPayload
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if err := h.dispatchEvent(r.Context(), event); err != nil {
		slog.Error("resend webhook: dispatch failed",
			"error", err, "type", event.Type, "email_id", event.Data.EmailID)
		// Intentionally 2xx even on store errors — we don't want Resend
		// to retry on our internal issues. Observability surfaces the
		// error via logs.
	}

	w.WriteHeader(http.StatusOK)
}

func (h *ResendWebhookHandler) dispatchEvent(ctx context.Context, event resendEventPayload) error {
	campaignID := tagValue(event.Data.Tags, "campaign_id")
	if campaignID == "" {
		// Not a campaign email (transactional hub invite, waitlist, etc.)
		// — we don't track individual deliveries for those yet.
		return nil
	}
	if len(event.Data.To) == 0 {
		return errors.New("event missing recipient")
	}
	recipient := event.Data.To[0]

	delivery, err := h.store.GetCampaignDeliveryByCampaignAndEmail(ctx, campaignID, recipient)
	if err != nil {
		if errors.Is(err, store.ErrCampaignDeliveryNotFound) {
			slog.Info("resend webhook: no matching delivery row",
				"campaign_id", campaignID, "email", recipient, "type", event.Type)
			return nil
		}
		return err
	}

	// Prefer event `created_at` over server clock to avoid clock skew
	// between our worker and Resend's API. Fall back to now on parse
	// failure — better to stamp a slightly-off time than drop the event.
	stamp := time.Now().UTC()
	if event.CreatedAt != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, event.CreatedAt); err == nil {
			stamp = parsed
		}
	}

	switch event.Type {
	case "email.delivered":
		return h.store.MarkCampaignDeliveryDelivered(ctx, delivery.ID, stamp)
	case "email.bounced":
		errCode, errMsg := "", ""
		if event.Data.Bounce != nil {
			errCode = event.Data.Bounce.Type
			errMsg = event.Data.Bounce.Message
		}
		return h.store.MarkCampaignDeliveryBounced(ctx, delivery.ID, stamp, errCode, errMsg)
	case "email.complained":
		return h.store.MarkCampaignDeliveryComplained(ctx, delivery.ID, stamp)
	case "email.opened":
		return h.store.MarkCampaignDeliveryOpened(ctx, delivery.ID, stamp)
	}
	// Unknown / future event types are silently acknowledged.
	return nil
}

func tagValue(tags []resendTag, name string) string {
	for _, tag := range tags {
		if tag.Name == name {
			return tag.Value
		}
	}
	return ""
}

// Ensure model.CampaignDelivery import is used (helps when the file is
// trimmed during edits).
var _ = model.DeliveryStatusDelivered

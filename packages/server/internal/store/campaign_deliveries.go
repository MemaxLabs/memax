package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// ErrCampaignDeliveryNotFound is returned by LookupDeliveryByProviderID
// when no row matches the provider message ID. Webhook handlers use
// this to silently drop stale or unknown provider events.
var ErrCampaignDeliveryNotFound = errors.New("campaign delivery not found")

// CreateCampaignDelivery inserts a queued delivery row for one recipient.
// Uses INSERT ... ON CONFLICT DO NOTHING on (campaign_id, recipient_email)
// so retries of the send worker are idempotent. Returns whether a new row
// was inserted (true) or an existing row was preserved (false).
func (s *PostgresStore) CreateCampaignDelivery(ctx context.Context, delivery *model.CampaignDelivery) (bool, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO campaign_deliveries
		   (campaign_id, batch_id, recipient_user_id, recipient_email, status)
		 VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5)
		 ON CONFLICT (campaign_id, recipient_email) DO NOTHING
		 RETURNING id::text`,
		delivery.CampaignID,
		delivery.BatchID,
		ptrStringValue(delivery.RecipientUserID),
		delivery.RecipientEmail,
		pickStatus(delivery.Status),
	).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("create campaign delivery: %w", err)
	}
	delivery.ID = id
	return true, nil
}

// MarkCampaignDeliverySent flips status to "sent", records the provider
// message id, and stamps sent_at. Called by the email send worker.
func (s *PostgresStore) MarkCampaignDeliverySent(ctx context.Context, deliveryID, providerMessageID string, sentAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaign_deliveries SET
		   status = 'sent',
		   provider_message_id = $2,
		   sent_at = $3
		 WHERE id = $1 AND status = 'queued'`,
		deliveryID, providerMessageID, sentAt,
	)
	if err != nil {
		return fmt.Errorf("mark delivery sent: %w", err)
	}
	return nil
}

// MarkCampaignDeliveryFailed records a terminal send failure.
func (s *PostgresStore) MarkCampaignDeliveryFailed(ctx context.Context, deliveryID, errorCode, errorMessage string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaign_deliveries SET
		   status = 'failed',
		   error_code = $2,
		   error_message = $3
		 WHERE id = $1 AND status IN ('queued', 'sent')`,
		deliveryID, errorCode, errorMessage,
	)
	if err != nil {
		return fmt.Errorf("mark delivery failed: %w", err)
	}
	return nil
}

// GetCampaignDeliveryByCampaignAndEmail looks up a delivery row by the
// (campaign_id, recipient_email) unique key. Used by the Resend webhook
// handler to match incoming delivery events to the row we created at
// send time. Returns ErrCampaignDeliveryNotFound when the event is for
// a recipient we don't have a row for (stale/foreign event).
func (s *PostgresStore) GetCampaignDeliveryByCampaignAndEmail(ctx context.Context, campaignID, email string) (*model.CampaignDelivery, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id::text, campaign_id::text, batch_id,
		        recipient_user_id::text, recipient_email, status,
		        provider_message_id, error_code, error_message, queued_at,
		        sent_at, delivered_at, opened_at, bounced_at, complained_at
		 FROM campaign_deliveries
		 WHERE campaign_id = $1::uuid AND recipient_email = $2`,
		campaignID, email)

	delivery := &model.CampaignDelivery{}
	var userID string
	if err := row.Scan(
		&delivery.ID,
		&delivery.CampaignID,
		&delivery.BatchID,
		&userID,
		&delivery.RecipientEmail,
		&delivery.Status,
		&delivery.ProviderMessageID,
		&delivery.ErrorCode,
		&delivery.ErrorMessage,
		&delivery.QueuedAt,
		&delivery.SentAt,
		&delivery.DeliveredAt,
		&delivery.OpenedAt,
		&delivery.BouncedAt,
		&delivery.ComplainedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrCampaignDeliveryNotFound
		}
		return nil, fmt.Errorf("get delivery by campaign+email: %w", err)
	}
	if userID != "" {
		delivery.RecipientUserID = &userID
	}
	return delivery, nil
}

// MarkCampaignDeliveryDelivered records a Resend-reported delivery.
// Status transitions to 'delivered'; terminal bounced/complained states
// are preserved (we never demote them back to delivered).
func (s *PostgresStore) MarkCampaignDeliveryDelivered(ctx context.Context, deliveryID string, deliveredAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaign_deliveries SET
		   status = 'delivered',
		   delivered_at = $2
		 WHERE id = $1 AND status IN ('queued', 'sent')`,
		deliveryID, deliveredAt,
	)
	if err != nil {
		return fmt.Errorf("mark delivery delivered: %w", err)
	}
	return nil
}

// MarkCampaignDeliveryBounced records a Resend-reported hard bounce.
// Takes effect from any non-terminal state; downstream opens will be
// ignored because the status is no longer 'delivered'.
func (s *PostgresStore) MarkCampaignDeliveryBounced(ctx context.Context, deliveryID string, bouncedAt time.Time, errorCode, errorMessage string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaign_deliveries SET
		   status = 'bounced',
		   bounced_at = $2,
		   error_code = $3,
		   error_message = $4
		 WHERE id = $1 AND status IN ('queued', 'sent', 'delivered')`,
		deliveryID, bouncedAt, errorCode, errorMessage,
	)
	if err != nil {
		return fmt.Errorf("mark delivery bounced: %w", err)
	}
	return nil
}

// MarkCampaignDeliveryComplained records a spam complaint. Transitions
// from any non-terminal state; preserves the row for compliance audit.
func (s *PostgresStore) MarkCampaignDeliveryComplained(ctx context.Context, deliveryID string, complainedAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaign_deliveries SET
		   status = 'complained',
		   complained_at = $2
		 WHERE id = $1 AND status IN ('queued', 'sent', 'delivered')`,
		deliveryID, complainedAt,
	)
	if err != nil {
		return fmt.Errorf("mark delivery complained: %w", err)
	}
	return nil
}

// MarkCampaignDeliveryOpened stamps opened_at WITHOUT changing status.
// A recipient can open an email after 'delivered' and still later bounce
// (rare but possible with multi-address forwarding); we only care about
// the first-open timestamp for analytics.
func (s *PostgresStore) MarkCampaignDeliveryOpened(ctx context.Context, deliveryID string, openedAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaign_deliveries SET
		   opened_at = COALESCE(opened_at, $2)
		 WHERE id = $1`,
		deliveryID, openedAt,
	)
	if err != nil {
		return fmt.Errorf("mark delivery opened: %w", err)
	}
	return nil
}

// AnnotateCampaignDelivery attaches an error_code + error_message to a
// delivery row WITHOUT changing its status. Used for informational
// annotations like opt_out_unknown on rows that stay 'suppressed'.
// Status-preserving by design: callers that want to flip to 'failed'
// must use MarkCampaignDeliveryFailed.
func (s *PostgresStore) AnnotateCampaignDelivery(ctx context.Context, deliveryID, errorCode, errorMessage string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaign_deliveries SET
		   error_code = $2,
		   error_message = $3
		 WHERE id = $1`,
		deliveryID, errorCode, errorMessage,
	)
	if err != nil {
		return fmt.Errorf("annotate delivery: %w", err)
	}
	return nil
}

// GetCampaignDeliveryStats returns the rollup shown on campaign detail.
func (s *PostgresStore) GetCampaignDeliveryStats(ctx context.Context, campaignID string) (*model.CampaignDeliveryStats, error) {
	stats := &model.CampaignDeliveryStats{}
	rows, err := s.pool.Query(ctx,
		`SELECT status, COUNT(*)::int
		   FROM campaign_deliveries
		  WHERE campaign_id = $1
		  GROUP BY status`, campaignID)
	if err != nil {
		return nil, fmt.Errorf("get delivery stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan delivery stats: %w", err)
		}
		stats.Total += count
		switch status {
		case model.DeliveryStatusQueued:
			stats.Queued = count
		case model.DeliveryStatusSent:
			stats.Sent = count
		case model.DeliveryStatusDelivered:
			stats.Delivered = count
		case model.DeliveryStatusBounced:
			stats.Bounced = count
		case model.DeliveryStatusComplained:
			stats.Complained = count
		case model.DeliveryStatusFailed:
			stats.Failed = count
		case model.DeliveryStatusSuppressed:
			stats.Suppressed = count
		}
	}
	// Opens are tracked separately (opened_at is non-null, status can be
	// any of sent/delivered) — count that in a second query.
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)::int FROM campaign_deliveries
		  WHERE campaign_id = $1 AND opened_at IS NOT NULL`, campaignID,
	).Scan(&stats.Opened); err != nil {
		return nil, fmt.Errorf("get delivery opens: %w", err)
	}
	return stats, rows.Err()
}

func pickStatus(status string) string {
	if status == "" {
		return model.DeliveryStatusQueued
	}
	return status
}

// --- Opt-out helpers -----------------------------------------------------

// ErrUnsubscribeTokenNotFound signals an unknown/rotated unsubscribe
// token. Returned as a sentinel so the public handler can map it to a
// 404 without leaking token presence.
var ErrUnsubscribeTokenNotFound = errors.New("unsubscribe token not found")

// newUnsubscribeToken mints a URL-safe random token. 32 bytes of
// entropy → 43-char base64 identifier; resistant to guessing.
func newUnsubscribeToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate unsubscribe token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// GetOrCreateEmailOptOutToken returns the user's current unsubscribe
// token, minting a new one if the column is empty. Called per-send
// so every campaign email carries a per-recipient unsubscribe URL.
// Idempotent: re-callers get the same token until the user actually
// unsubscribes and the token is rotated.
func (s *PostgresStore) GetOrCreateEmailOptOutToken(ctx context.Context, userID string) (string, error) {
	token, err := newUnsubscribeToken()
	if err != nil {
		return "", err
	}
	// Atomic get-or-assign: RETURNING yields the final token regardless
	// of whether the UPDATE actually wrote (SET only fires when the
	// column is empty, so concurrent callers converge on the first
	// successful write).
	var stored string
	err = s.pool.QueryRow(ctx,
		`UPDATE users SET email_opt_out_token = $2
		   WHERE id = $1 AND email_opt_out_token = ''
		 RETURNING email_opt_out_token`,
		userID, token,
	).Scan(&stored)
	if err == nil {
		return stored, nil
	}
	if err != pgx.ErrNoRows {
		return "", fmt.Errorf("assign opt-out token: %w", err)
	}
	// No row updated — token already present OR user doesn't exist.
	// Re-read to disambiguate.
	err = s.pool.QueryRow(ctx,
		`SELECT email_opt_out_token FROM users WHERE id = $1`, userID,
	).Scan(&stored)
	if err != nil {
		return "", fmt.Errorf("read opt-out token: %w", err)
	}
	if stored == "" {
		return "", fmt.Errorf("opt-out token assignment failed for user %s", userID)
	}
	return stored, nil
}

// UnsubscribeByToken flips email_opt_out_marketing=true for the user
// whose token matches, rotates the token (so the old URL stops working),
// and returns the affected user id for audit logging. Returns
// ErrUnsubscribeTokenNotFound when the token doesn't match any user.
func (s *PostgresStore) UnsubscribeByToken(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", ErrUnsubscribeTokenNotFound
	}
	newToken, err := newUnsubscribeToken()
	if err != nil {
		return "", err
	}
	var userID string
	err = s.pool.QueryRow(ctx,
		`UPDATE users SET
		   email_opt_out_marketing = true,
		   email_opt_out_token = $2
		 WHERE email_opt_out_token = $1
		 RETURNING id::text`,
		token, newToken,
	).Scan(&userID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", ErrUnsubscribeTokenNotFound
		}
		return "", fmt.Errorf("unsubscribe by token: %w", err)
	}
	return userID, nil
}

// IsUserEmailOptedOut reports whether the user has opted out of marketing
// emails. Transactional emails ignore this flag.
func (s *PostgresStore) IsUserEmailOptedOut(ctx context.Context, userID string) (bool, error) {
	var optedOut bool
	err := s.pool.QueryRow(ctx,
		`SELECT email_opt_out_marketing FROM users WHERE id = $1`, userID,
	).Scan(&optedOut)
	if err != nil {
		if err == pgx.ErrNoRows {
			// Unknown user — treat as opted-in so we don't silently drop sends.
			return false, nil
		}
		return false, fmt.Errorf("check opt-out: %w", err)
	}
	return optedOut, nil
}

// --- InMemoryStore implementation (dev/test) -----------------------------

func (s *InMemoryStore) CreateCampaignDelivery(_ context.Context, delivery *model.CampaignDelivery) (bool, error) {
	s.campaignDeliveriesMu.Lock()
	defer s.campaignDeliveriesMu.Unlock()
	if s.campaignDeliveries == nil {
		s.campaignDeliveries = map[string]*model.CampaignDelivery{}
	}
	key := delivery.CampaignID + "|" + delivery.RecipientEmail
	if _, exists := s.campaignDeliveries[key]; exists {
		return false, nil
	}
	if delivery.ID == "" {
		delivery.ID = fmt.Sprintf("del_%d", time.Now().UnixNano())
	}
	if delivery.Status == "" {
		delivery.Status = model.DeliveryStatusQueued
	}
	if delivery.QueuedAt.IsZero() {
		delivery.QueuedAt = time.Now()
	}
	copy := *delivery
	s.campaignDeliveries[key] = &copy
	return true, nil
}

func (s *InMemoryStore) MarkCampaignDeliverySent(_ context.Context, deliveryID, providerMessageID string, sentAt time.Time) error {
	s.campaignDeliveriesMu.Lock()
	defer s.campaignDeliveriesMu.Unlock()
	for _, d := range s.campaignDeliveries {
		if d.ID == deliveryID && d.Status == model.DeliveryStatusQueued {
			d.Status = model.DeliveryStatusSent
			d.ProviderMessageID = providerMessageID
			stamp := sentAt
			d.SentAt = &stamp
			return nil
		}
	}
	return nil
}

func (s *InMemoryStore) MarkCampaignDeliveryFailed(_ context.Context, deliveryID, errorCode, errorMessage string) error {
	s.campaignDeliveriesMu.Lock()
	defer s.campaignDeliveriesMu.Unlock()
	for _, d := range s.campaignDeliveries {
		if d.ID == deliveryID && (d.Status == model.DeliveryStatusQueued || d.Status == model.DeliveryStatusSent) {
			d.Status = model.DeliveryStatusFailed
			d.ErrorCode = errorCode
			d.ErrorMessage = errorMessage
			return nil
		}
	}
	return nil
}

func (s *InMemoryStore) GetCampaignDeliveryByCampaignAndEmail(_ context.Context, campaignID, email string) (*model.CampaignDelivery, error) {
	s.campaignDeliveriesMu.RLock()
	defer s.campaignDeliveriesMu.RUnlock()
	for _, d := range s.campaignDeliveries {
		if d.CampaignID == campaignID && d.RecipientEmail == email {
			copy := *d
			return &copy, nil
		}
	}
	return nil, ErrCampaignDeliveryNotFound
}

func (s *InMemoryStore) MarkCampaignDeliveryDelivered(_ context.Context, deliveryID string, deliveredAt time.Time) error {
	s.campaignDeliveriesMu.Lock()
	defer s.campaignDeliveriesMu.Unlock()
	for _, d := range s.campaignDeliveries {
		if d.ID == deliveryID && (d.Status == model.DeliveryStatusQueued || d.Status == model.DeliveryStatusSent) {
			d.Status = model.DeliveryStatusDelivered
			stamp := deliveredAt
			d.DeliveredAt = &stamp
			return nil
		}
	}
	return nil
}

func (s *InMemoryStore) MarkCampaignDeliveryBounced(_ context.Context, deliveryID string, bouncedAt time.Time, errorCode, errorMessage string) error {
	s.campaignDeliveriesMu.Lock()
	defer s.campaignDeliveriesMu.Unlock()
	for _, d := range s.campaignDeliveries {
		if d.ID == deliveryID && (d.Status == model.DeliveryStatusQueued || d.Status == model.DeliveryStatusSent || d.Status == model.DeliveryStatusDelivered) {
			d.Status = model.DeliveryStatusBounced
			stamp := bouncedAt
			d.BouncedAt = &stamp
			d.ErrorCode = errorCode
			d.ErrorMessage = errorMessage
			return nil
		}
	}
	return nil
}

func (s *InMemoryStore) MarkCampaignDeliveryComplained(_ context.Context, deliveryID string, complainedAt time.Time) error {
	s.campaignDeliveriesMu.Lock()
	defer s.campaignDeliveriesMu.Unlock()
	for _, d := range s.campaignDeliveries {
		if d.ID == deliveryID && (d.Status == model.DeliveryStatusQueued || d.Status == model.DeliveryStatusSent || d.Status == model.DeliveryStatusDelivered) {
			d.Status = model.DeliveryStatusComplained
			stamp := complainedAt
			d.ComplainedAt = &stamp
			return nil
		}
	}
	return nil
}

func (s *InMemoryStore) MarkCampaignDeliveryOpened(_ context.Context, deliveryID string, openedAt time.Time) error {
	s.campaignDeliveriesMu.Lock()
	defer s.campaignDeliveriesMu.Unlock()
	for _, d := range s.campaignDeliveries {
		if d.ID == deliveryID {
			if d.OpenedAt == nil {
				stamp := openedAt
				d.OpenedAt = &stamp
			}
			return nil
		}
	}
	return nil
}

func (s *InMemoryStore) AnnotateCampaignDelivery(_ context.Context, deliveryID, errorCode, errorMessage string) error {
	s.campaignDeliveriesMu.Lock()
	defer s.campaignDeliveriesMu.Unlock()
	for _, d := range s.campaignDeliveries {
		if d.ID == deliveryID {
			d.ErrorCode = errorCode
			d.ErrorMessage = errorMessage
			return nil
		}
	}
	return nil
}

func (s *InMemoryStore) GetCampaignDeliveryStats(_ context.Context, campaignID string) (*model.CampaignDeliveryStats, error) {
	s.campaignDeliveriesMu.RLock()
	defer s.campaignDeliveriesMu.RUnlock()
	stats := &model.CampaignDeliveryStats{}
	for _, d := range s.campaignDeliveries {
		if d.CampaignID != campaignID {
			continue
		}
		stats.Total++
		switch d.Status {
		case model.DeliveryStatusQueued:
			stats.Queued++
		case model.DeliveryStatusSent:
			stats.Sent++
		case model.DeliveryStatusDelivered:
			stats.Delivered++
		case model.DeliveryStatusBounced:
			stats.Bounced++
		case model.DeliveryStatusComplained:
			stats.Complained++
		case model.DeliveryStatusFailed:
			stats.Failed++
		case model.DeliveryStatusSuppressed:
			stats.Suppressed++
		}
		if d.OpenedAt != nil {
			stats.Opened++
		}
	}
	return stats, nil
}

func (s *InMemoryStore) IsUserEmailOptedOut(_ context.Context, userID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.emailOptOut[userID], nil
}

func (s *InMemoryStore) GetOrCreateEmailOptOutToken(_ context.Context, userID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emailOptOutTokens == nil {
		s.emailOptOutTokens = map[string]string{}
	}
	if token, ok := s.emailOptOutTokens[userID]; ok && token != "" {
		return token, nil
	}
	token, err := newUnsubscribeToken()
	if err != nil {
		return "", err
	}
	s.emailOptOutTokens[userID] = token
	return token, nil
}

func (s *InMemoryStore) UnsubscribeByToken(_ context.Context, token string) (string, error) {
	if token == "" {
		return "", ErrUnsubscribeTokenNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emailOptOutTokens == nil {
		return "", ErrUnsubscribeTokenNotFound
	}
	for userID, stored := range s.emailOptOutTokens {
		if stored == token {
			newToken, err := newUnsubscribeToken()
			if err != nil {
				return "", err
			}
			s.emailOptOutTokens[userID] = newToken
			if s.emailOptOut == nil {
				s.emailOptOut = map[string]bool{}
			}
			s.emailOptOut[userID] = true
			return userID, nil
		}
	}
	return "", ErrUnsubscribeTokenNotFound
}

// SetUserEmailOptedOut is a test helper for the in-memory store only.
// Production opt-out writes go through a user-facing unsubscribe handler
// (not yet wired; tracked separately).
func (s *InMemoryStore) SetUserEmailOptedOut(userID string, optedOut bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emailOptOut == nil {
		s.emailOptOut = map[string]bool{}
	}
	s.emailOptOut[userID] = optedOut
}

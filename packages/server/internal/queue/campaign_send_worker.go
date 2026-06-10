package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/email"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// CampaignSendWorker is the terminal worker in the campaign lifecycle.
// It picks up a campaign, transitions draft/scheduled → sending via a
// CAS claim, resolves the audience snapshot, fans out notifications,
// then finalizes to sent or failed with partial counts.
//
// Cancellation: a cancelled campaign is left unchanged. The worker
// logs a no-op and returns nil; the River job is marked complete so
// it doesn't retry.
type CampaignSendWorker struct {
	river.WorkerDefaults[CampaignSendArgs]
	Store store.Store
	// Templates wraps ad-hoc email bodies in the current brand layout.
	// Optional — only needed for channel=email campaigns.
	Templates *email.TemplateManager
	// EnqueueRenderedEmail inserts a pre-rendered email send job. Optional,
	// only needed for channel=email campaigns. Without it, email sends
	// fail at worker time with a clear error.
	EnqueueRenderedEmail func(ctx context.Context, to, subject, html, text string, tags map[string]string, headers map[string]string) error
	// UnsubscribeURLBase is the public origin that serves /v1/unsubscribe.
	// Empty string disables unsubscribe link injection — the worker
	// still ships the email, but without List-Unsubscribe headers.
	UnsubscribeURLBase string
	Events             events.Publisher
}

// pageSize governs how many users the worker materializes at once for
// the "all" audience rule. Tuned low to keep memory flat; total
// recipients cap out via the River job budget, not this value.
const campaignSendUserPageSize = 200

func (w *CampaignSendWorker) Work(ctx context.Context, job *river.Job[CampaignSendArgs]) error {
	campaignID := job.Args.CampaignID
	if campaignID == "" {
		return fmt.Errorf("campaign_send: empty campaign_id")
	}

	campaign, err := w.Store.GetCampaign(ctx, campaignID)
	if err != nil {
		return fmt.Errorf("load campaign: %w", err)
	}

	// Idempotency / cancellation gates. The handler CAS-claims the
	// campaign to "sending" before enqueuing for immediate sends, so
	// the worker must accept status=sending as a pre-claimed ready
	// state (not "another worker is running"). For scheduled sends
	// the worker is the one that CAS-claims, at execution time.
	switch campaign.Status {
	case model.CampaignStatusCancelled,
		model.CampaignStatusSent,
		model.CampaignStatusFailed:
		slog.InfoContext(ctx, "campaign_send: skipping non-runnable campaign",
			"campaign_id", campaignID, "status", campaign.Status)
		return nil
	case model.CampaignStatusDraft, model.CampaignStatusScheduled:
		// Worker-driven claim path.
	case model.CampaignStatusSending:
		// Handler-driven claim path; proceed directly to fanout.
	default:
		slog.WarnContext(ctx, "campaign_send: unexpected status",
			"campaign_id", campaignID, "status", campaign.Status)
		return nil
	}

	if campaign.AudienceSnapshot == nil {
		return w.finalizeFailure(ctx, campaign, "missing audience snapshot")
	}

	// sourceKind is only used by the in-app fanout; email campaigns skip
	// notification creation entirely.
	var sourceKind string
	if campaign.Channel != model.CampaignChannelEmail {
		var ok bool
		sourceKind, ok = campaignSourceKind(campaign.Kind)
		if !ok {
			return w.finalizeFailure(ctx, campaign, fmt.Sprintf("unsupported kind %q", campaign.Kind))
		}
	}

	var batchID string
	if campaign.Status == model.CampaignStatusSending {
		// Handler already populated batch_id.
		if campaign.BatchID == nil || *campaign.BatchID == "" {
			return w.finalizeFailure(ctx, campaign,
				"campaign claimed without batch_id")
		}
		batchID = *campaign.BatchID
	} else {
		batchID = fmt.Sprintf("campaign:%s:%s", campaign.ID, uuid.New().String()[:8])
		startedAt := time.Now().UTC()
		if err := w.Store.ClaimCampaignForSend(ctx, campaign.ID, campaign.Status, startedAt, batchID); err != nil {
			// State mismatch implies someone cancelled or re-ran;
			// treat as benign no-op so River doesn't retry forever.
			if strings.Contains(err.Error(), "state") {
				slog.InfoContext(ctx, "campaign_send: could not claim, benign no-op",
					"campaign_id", campaignID, "error", err)
				return nil
			}
			return fmt.Errorf("claim campaign: %w", err)
		}
	}

	sentCount, failedCount, fanoutErr := w.fanOut(ctx, campaign, batchID, sourceKind)

	finishedAt := time.Now().UTC()
	errMsg := ""
	if fanoutErr != nil {
		errMsg = fanoutErr.Error()
	}
	if err := w.Store.FinalizeCampaignSend(ctx, campaign.ID, sentCount, failedCount, finishedAt, errMsg); err != nil {
		slog.ErrorContext(ctx, "campaign_send: finalize failed",
			"error", err, "campaign_id", campaignID)
		return nil
	}

	// Audit the terminal state with actor = nil (worker-authored).
	action := model.CommsAuditActionSendCompleted
	meta := map[string]any{
		"sent_count":   sentCount,
		"failed_count": failedCount,
		"batch_id":     batchID,
	}
	if fanoutErr != nil {
		action = model.CommsAuditActionSendFailed
		meta["error"] = errMsg
	}
	w.writeWorkerAudit(ctx, campaign.ID, action, meta)

	slog.InfoContext(ctx, "campaign_send: completed",
		"campaign_id", campaignID,
		"batch_id", batchID,
		"sent_count", sentCount,
		"failed_count", failedCount,
		"error", errMsg,
	)
	return nil
}

// fanOut iterates the audience, creates a notification per user (for
// in-app campaigns) or enqueues a rendered email per user (for email
// campaigns), and returns (sentCount, failedCount, error-if-fatal).
// Individual per-recipient errors count as failures but do not abort.
func (w *CampaignSendWorker) fanOut(ctx context.Context, campaign *model.Campaign, batchID, sourceKind string) (int, int, error) {
	// For email campaigns, pre-render the body once and reuse for every
	// recipient. Pagination + per-recipient dispatch is identical to
	// in-app; the per-recipient callback diverges.
	var emailContext *campaignEmailContext
	if campaign.Channel == model.CampaignChannelEmail {
		ec, err := w.prepareEmailContext(ctx, campaign, batchID)
		if err != nil {
			return 0, 0, err
		}
		emailContext = ec
	}

	dispatch := func(userID string) dispatchOutcome {
		if emailContext != nil {
			return w.sendCampaignEmail(ctx, campaign, emailContext, userID)
		}
		if w.createNotification(ctx, campaign, batchID, sourceKind, userID) {
			return dispatchSent
		}
		return dispatchFailed
	}

	switch campaign.AudienceSnapshot.RuleType {
	case model.AudienceRuleAll:
		return w.fanOutAll(ctx, dispatch)
	case model.AudienceRuleUsers:
		sent, failed := w.fanOutInline(dispatch, resolveUsersUserIDs(w.Store, campaign.AudienceSnapshot.Rule))
		return sent, failed, nil
	case model.AudienceRuleHub:
		members, err := w.Store.ListHubMembers(campaign.AudienceSnapshot.Rule.HubID)
		if err != nil {
			return 0, 0, fmt.Errorf("list hub members: %w", err)
		}
		userIDs := make([]string, 0, len(members))
		for _, m := range members {
			if r := campaign.AudienceSnapshot.Rule.HubMemberRole; r != "" && m.Role != r {
				continue
			}
			userIDs = append(userIDs, m.UserID)
		}
		sent, failed := w.fanOutInline(dispatch, userIDs)
		return sent, failed, nil
	default:
		return 0, 0, fmt.Errorf("unsupported rule type %q", campaign.AudienceSnapshot.RuleType)
	}
}

// dispatchOutcome distinguishes a successful send from a silent skip
// (suppressed opt-outs, already-delivered on retry) and from a real
// failure. Campaign-level counters only count "sent" vs "failed";
// "skipped" recipients do not pollute the failure count.
type dispatchOutcome int

const (
	dispatchSent dispatchOutcome = iota
	dispatchFailed
	dispatchSkipped // suppressed / already-delivered on retry
)

type campaignEmailContext struct {
	Subject            string
	HTML               string
	Text               string
	BatchID            string
	IncludeUnsubscribe bool
	Tags               map[string]string
}

// prepareEmailContext wraps the admin-authored body in the brand layout
// once, so per-recipient dispatch is a pure enqueue. Returns an error
// only on fatal misconfiguration (missing TemplateManager, malformed
// content) — treated as a send-wide failure by fanOut's caller.
//
// When UnsubscribeURLBase is set, the layout is rendered with an
// unsubscribe placeholder that `sendCampaignEmail` string-replaces
// per-recipient with the user's token URL. This keeps the expensive
// html/template render to once-per-campaign while still producing
// per-recipient unsubscribe links.
func (w *CampaignSendWorker) prepareEmailContext(ctx context.Context, campaign *model.Campaign, batchID string) (*campaignEmailContext, error) {
	if w.Templates == nil {
		return nil, fmt.Errorf("email campaigns require a TemplateManager")
	}
	if w.EnqueueRenderedEmail == nil {
		return nil, fmt.Errorf("email campaigns require an EnqueueRenderedEmail callback")
	}
	var payload model.EmailAnnouncementContent
	if err := json.Unmarshal(campaign.Content, &payload); err != nil {
		return nil, fmt.Errorf("parse email content: %w", err)
	}
	includeUnsub := w.UnsubscribeURLBase != ""
	var rendered email.RenderResult
	var err error
	if includeUnsub {
		rendered, err = w.Templates.WrapAdHocWithUnsubscribe(ctx, payload.Subject, payload.BodyHTML, payload.BodyText)
	} else {
		rendered, err = w.Templates.RenderAdHoc(ctx, payload.Subject, payload.BodyHTML, payload.BodyText)
	}
	if err != nil {
		return nil, fmt.Errorf("render email body: %w", err)
	}
	return &campaignEmailContext{
		Subject:            rendered.Subject,
		HTML:               rendered.HTML,
		Text:               rendered.Text,
		BatchID:            batchID,
		IncludeUnsubscribe: includeUnsub,
		Tags: map[string]string{
			"campaign_id": campaign.ID,
			"batch_id":    batchID,
		},
	}, nil
}

// sendCampaignEmail is the per-recipient dispatch for email campaigns.
// It honors the opt-out flag (fail-closed on lookup error), writes a
// delivery row, and enqueues the rendered email send. Returns the
// appropriate dispatchOutcome so campaign counters reflect reality.
func (w *CampaignSendWorker) sendCampaignEmail(ctx context.Context, campaign *model.Campaign, ec *campaignEmailContext, userID string) dispatchOutcome {
	u, err := w.Store.GetUser(userID)
	if err != nil || u == nil || u.Email == "" {
		slog.WarnContext(ctx, "campaign_send: email recipient lookup failed",
			"user_id", userID, "campaign_id", campaign.ID, "error", err)
		return dispatchFailed
	}

	// Fail-closed: if we cannot verify opt-out status, treat the recipient
	// as suppressed. Marketing email compliance requires never sending when
	// we're uncertain — a false-negative send is far worse than a
	// false-positive suppression.
	optOutUnknown := false
	optedOut, err := w.Store.IsUserEmailOptedOut(ctx, userID)
	if err != nil {
		slog.WarnContext(ctx, "campaign_send: opt-out check failed, treating as suppressed",
			"user_id", userID, "error", err)
		optedOut = true
		optOutUnknown = true
	}

	delivery := &model.CampaignDelivery{
		CampaignID:      campaign.ID,
		BatchID:         ec.BatchID,
		RecipientUserID: &userID,
		RecipientEmail:  u.Email,
	}
	if optedOut {
		delivery.Status = model.DeliveryStatusSuppressed
	}
	inserted, err := w.Store.CreateCampaignDelivery(ctx, delivery)
	if err != nil {
		slog.WarnContext(ctx, "campaign_send: delivery insert failed",
			"user_id", userID, "campaign_id", campaign.ID, "error", err)
		return dispatchFailed
	}
	// Idempotency: if a row already exists for this (campaign, recipient),
	// we already handled this recipient on a prior worker attempt. Skip
	// silently — do NOT double-enqueue the email.
	if !inserted {
		return dispatchSkipped
	}
	if optedOut {
		// Record an explicit reason for suppressed-unknown so operators
		// can distinguish "user opted out" from "lookup failed". Uses
		// Annotate (not MarkFailed) so the row stays status=suppressed.
		if optOutUnknown {
			_ = w.Store.AnnotateCampaignDelivery(ctx, delivery.ID, "opt_out_unknown", "opt-out status could not be verified")
		}
		return dispatchSkipped
	}

	// Per-recipient token URL + body substitution. We do this only when
	// UnsubscribeURLBase is configured AND the user has a stable token.
	// Token mint failure is non-fatal: the email ships without the
	// footer link / headers (better than dropping the whole send).
	//
	// Three distinct escapings for the same URL:
	//   - HTML body: attribute-context (e.g. `&` → `&amp;`) via html.EscapeString
	//   - Plain text body: raw URL
	//   - List-Unsubscribe header: raw URL inside <angle> brackets
	htmlOut, textOut := ec.HTML, ec.Text
	var headers map[string]string
	if ec.IncludeUnsubscribe {
		token, terr := w.Store.GetOrCreateEmailOptOutToken(ctx, userID)
		if terr != nil {
			slog.WarnContext(ctx, "campaign_send: unsubscribe token mint failed, shipping without footer link",
				"user_id", userID, "error", terr)
		} else {
			unsubURL, urlErr := buildUnsubscribeURL(w.UnsubscribeURLBase, token)
			if urlErr != nil {
				slog.WarnContext(ctx, "campaign_send: unsubscribe URL build failed, shipping without footer link",
					"user_id", userID, "error", urlErr)
			} else {
				htmlOut = strings.ReplaceAll(htmlOut, email.UnsubscribePlaceholder, html.EscapeString(unsubURL))
				textOut = strings.ReplaceAll(textOut, email.UnsubscribePlaceholder, unsubURL)
				// RFC 8058 one-click: server MUST accept POST with this body.
				headers = map[string]string{
					"List-Unsubscribe":      "<" + unsubURL + ">",
					"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
				}
			}
		}
	}

	if err := w.EnqueueRenderedEmail(ctx, u.Email, ec.Subject, htmlOut, textOut, ec.Tags, headers); err != nil {
		_ = w.Store.MarkCampaignDeliveryFailed(ctx, delivery.ID, "enqueue_failed", err.Error())
		slog.WarnContext(ctx, "campaign_send: email enqueue failed",
			"user_id", userID, "campaign_id", campaign.ID, "error", err)
		return dispatchFailed
	}
	_ = w.Store.MarkCampaignDeliverySent(ctx, delivery.ID, "", time.Now().UTC())
	return dispatchSent
}

// buildUnsubscribeURL composes the per-recipient one-click URL using
// net/url so fragments, existing query params, and odd trailing
// slashes are handled correctly. Accepts either a bare origin
// (https://memax.app) or a pre-suffixed URL (https://memax.app/v1/unsubscribe).
func buildUnsubscribeURL(base, token string) (string, error) {
	if base == "" {
		return "", fmt.Errorf("empty unsubscribe base URL")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}
	// Normalize trailing slash once so `/v1/unsubscribe` and
	// `/v1/unsubscribe/` both short-circuit the "append suffix" path.
	// Without this, the latter would double-up the suffix.
	normalized := strings.TrimRight(u.Path, "/")
	if normalized == "" || !strings.HasSuffix(normalized, "/v1/unsubscribe") {
		u.Path = normalized + "/v1/unsubscribe"
	} else {
		u.Path = normalized
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	// Fragments are not meaningful for a POST-able webhook URL; drop
	// them so they don't break one-click semantics in any client.
	u.Fragment = ""
	return u.String(), nil
}

func (w *CampaignSendWorker) fanOutAll(ctx context.Context, dispatch func(string) dispatchOutcome) (int, int, error) {
	sent, failed := 0, 0
	var cursor string
	for {
		users, nextCursor, _, err := w.Store.ListUsers(ctx, model.AdminUserListOpts{
			Limit:  campaignSendUserPageSize,
			Cursor: cursor,
		})
		if err != nil {
			return sent, failed, fmt.Errorf("list users: %w", err)
		}
		for _, u := range users {
			switch dispatch(u.ID) {
			case dispatchSent:
				sent++
			case dispatchFailed:
				failed++
			case dispatchSkipped:
				// Deliberately excluded from both counters; suppression
				// stats surface via campaign_deliveries rollups instead.
			}
		}
		if nextCursor == "" || len(users) < campaignSendUserPageSize {
			break
		}
		cursor = nextCursor
	}
	return sent, failed, nil
}

func (w *CampaignSendWorker) fanOutInline(dispatch func(string) dispatchOutcome, userIDs []string) (int, int) {
	sent, failed := 0, 0
	for _, uid := range userIDs {
		switch dispatch(uid) {
		case dispatchSent:
			sent++
		case dispatchFailed:
			failed++
		case dispatchSkipped:
			// see fanOutAll comment
		}
	}
	return sent, failed
}

func (w *CampaignSendWorker) createNotification(ctx context.Context, campaign *model.Campaign, batchID, sourceKind, userID string) bool {
	n := &model.Notification{
		ID:              uuid.New().String(),
		Audience:        model.AudienceUser,
		RecipientUserID: userID,
		Kind:            campaign.Kind,
		Status:          model.NotificationStatusPending,
		SourceKind:      sourceKind,
		SourceID:        batchID + ":" + userID,
		Payload:         json.RawMessage(campaign.Content),
		CreatedAt:       time.Now(),
	}
	if err := w.Store.CreateNotification(n); err != nil {
		slog.WarnContext(ctx, "campaign_send: create notification failed",
			"error", err, "campaign_id", campaign.ID, "user_id", userID)
		return false
	}
	events.PublishNotificationCreated(ctx, w.Events, n)
	return true
}

// resolveUsersUserIDs is intentionally narrow: only resolves user IDs,
// dropping emails that don't map. The "failed_emails" surface is for
// the inline admin notifications flow; campaigns snapshot the rule at
// authoring time and do not carry it through to the worker.
func resolveUsersUserIDs(s store.Store, rule model.AudienceRule) []string {
	seen := make(map[string]bool, len(rule.UserIDs)+len(rule.Emails))
	out := make([]string, 0, len(rule.UserIDs)+len(rule.Emails))
	for _, uid := range rule.UserIDs {
		if uid == "" || seen[uid] {
			continue
		}
		if _, err := s.GetUser(uid); err != nil {
			continue
		}
		seen[uid] = true
		out = append(out, uid)
	}
	for _, email := range rule.Emails {
		if email == "" {
			continue
		}
		u, err := s.GetUserByCanonicalEmail(email)
		if err != nil || u == nil {
			continue
		}
		if seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		out = append(out, u.ID)
	}
	return out
}

func (w *CampaignSendWorker) finalizeFailure(ctx context.Context, campaign *model.Campaign, msg string) error {
	if err := w.Store.FinalizeCampaignSend(ctx, campaign.ID, 0, 0, time.Now().UTC(), msg); err != nil {
		slog.ErrorContext(ctx, "campaign_send: finalize failure record failed",
			"error", err, "campaign_id", campaign.ID)
	}
	w.writeWorkerAudit(ctx, campaign.ID, model.CommsAuditActionSendFailed, map[string]any{
		"error": msg,
	})
	return nil
}

func (w *CampaignSendWorker) writeWorkerAudit(ctx context.Context, campaignID, action string, metadata map[string]any) {
	var md json.RawMessage
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			md = b
		}
	}
	entry := &model.CommsAuditEntry{
		ID:           uuid.New().String(),
		ResourceType: model.CommsAuditResourceCampaign,
		ResourceID:   campaignID,
		Action:       action,
		Metadata:     md,
	}
	if err := w.Store.AppendCommsAudit(ctx, entry); err != nil {
		slog.WarnContext(ctx, "campaign_send: audit append failed",
			"error", err, "action", action, "campaign_id", campaignID)
	}
}

func campaignSourceKind(kind string) (string, bool) {
	switch kind {
	case model.NotificationKindSystemNotice:
		return model.NotificationSourceSystemNotice, true
	case model.NotificationKindGiftInviteLink:
		return model.NotificationSourceGift, true
	}
	return "", false
}

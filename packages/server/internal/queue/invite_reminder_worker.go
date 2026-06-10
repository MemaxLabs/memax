package queue

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// reminderDedup is the unique period for reminder emails. Set to 24h so
// the 12h periodic job can't enqueue the same reminder twice for the same
// invite. Dedup is by args (template + to + variables): if the exact same
// email with the exact same variables was enqueued in the last 24h, River
// silently skips the duplicate insert.
var reminderDedup = &river.InsertOpts{
	UniqueOpts: river.UniqueOpts{
		ByArgs:   true,
		ByPeriod: 24 * time.Hour,
	},
}

// reminderWindow is how far ahead of expiry we look for invites to remind.
//
// Reminder cadence: the periodic job runs every 12h with a 48h eligibility
// window and 24h dedup window. An invite pending for the full window may
// receive up to two reminder emails — one when it enters the window
// (~48h before expiry) and a second ~24h later when the dedup period
// resets. This is intentional and acceptable for the current product.
//
// If product later requires exactly one reminder per invite lifetime,
// add a durable `reminder_sent_at` column on hub_invites (or a shared
// delivery table) and check it here instead of relying on River dedup.
const reminderWindow = 48 * time.Hour

// SendInviteRemindersWorker finds invites expiring within reminderWindow
// and enqueues a reminder email for each. Covers both waitlist invites
// and hub email invites. Runs periodically (every 12 hours).
type SendInviteRemindersWorker struct {
	river.WorkerDefaults[SendInviteRemindersArgs]
	Store       store.Store
	RiverClient *Client // for enqueuing SendEmailArgs
}

func (w *SendInviteRemindersWorker) Timeout(_ *river.Job[SendInviteRemindersArgs]) time.Duration {
	return 60 * time.Second
}

func (w *SendInviteRemindersWorker) Work(ctx context.Context, _ *river.Job[SendInviteRemindersArgs]) error {
	appBaseURL := os.Getenv("APP_BASE_URL")
	if appBaseURL == "" {
		slog.WarnContext(ctx, "skipping invite reminders — APP_BASE_URL not set")
		return nil
	}

	enqueued := 0

	// --- Waitlist invite reminders ---
	waitlistInvites, err := w.Store.ListExpiringWaitlistInvites(ctx, int(reminderWindow.Hours()))
	if err != nil {
		return fmt.Errorf("list expiring waitlist invites: %w", err)
	}
	for _, inv := range waitlistInvites {
		registerURL := appBaseURL + "/register?invite=" + inv.Token
		err := w.RiverClient.Insert(ctx, SendEmailArgs{
			Template: "waitlist_invite_reminder",
			To:       inv.Email,
			Variables: map[string]string{
				"RegisterURL": registerURL,
				"ExpiresAt":   inv.ExpiresAt.Format("January 2, 2006"),
			},
		}, reminderDedup)
		if err != nil {
			slog.WarnContext(ctx, "failed to enqueue waitlist invite reminder", "error", err, "invite_id", inv.ID)
			continue
		}
		enqueued++
	}

	// --- Hub email invite reminders ---
	// ListExpiringHubInvites returns invites with invitee_email IS NOT NULL
	// that are still active (not accepted, not revoked) and expire within
	// the given duration. Link-only invites (invitee_email IS NULL) are
	// excluded at the query level — no email address to remind.
	hubInvites, err := w.Store.ListExpiringHubInvites(reminderWindow)
	if err != nil {
		slog.WarnContext(ctx, "failed to list expiring hub invites", "error", err)
		// Non-fatal: waitlist reminders already sent, don't fail the whole job.
	} else {
		for _, inv := range hubInvites {
			inviteURL := appBaseURL + "/invite/" + inv.Token

			// Resolve hub name and inviter name for the email template.
			// Best-effort: fall back to IDs if lookups fail.
			hubName := inv.HubID
			if hub, err := w.Store.GetHub(inv.HubID); err == nil && hub != nil {
				hubName = hub.Name
			}
			inviterName := ""
			if u, err := w.Store.GetUser(inv.InvitedBy); err == nil && u != nil {
				inviterName = u.DisplayName
				if inviterName == "" {
					inviterName = u.Name
				}
			}

			// Dedup: River's ByArgs+ByPeriod(24h) ensures the same
			// (template, to, variables) combination is enqueued at most
			// once per 24h. Since the worker runs every 12h, each
			// qualifying invite gets at most one reminder email.
			err := w.RiverClient.Insert(ctx, SendEmailArgs{
				Template: "hub_invite_reminder",
				To:       *inv.InviteeEmail,
				Variables: map[string]string{
					"InviterName": inviterName,
					"HubName":     hubName,
					"Role":        inv.Role,
					"InviteURL":   inviteURL,
					"ExpiresAt":   inv.ExpiresAt.Format("January 2, 2006"),
				},
			}, reminderDedup)
			if err != nil {
				slog.WarnContext(ctx, "failed to enqueue hub invite reminder",
					"error", err, "invite_id", inv.ID, "email", *inv.InviteeEmail)
				continue
			}
			enqueued++
		}
	}

	if enqueued > 0 {
		slog.InfoContext(ctx, "enqueued invite reminders", "count", enqueued)
	}
	return nil
}

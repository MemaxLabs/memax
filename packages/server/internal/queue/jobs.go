// Package queue defines River job types for all async work in Memax.
// Job args are defined here; workers are registered in queue.go.
package queue

import (
	"encoding/json"
	"time"

	"github.com/riverqueue/river"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// --- Note Processing Job ---
// Replaces the goroutine in notes.go. Runs chunking, embedding,
// classification, summarization, and fact extraction.

type MemoryProcessArgs struct {
	MemoryID            string            `json:"memory_id"`
	OwnerID             string            `json:"owner_id"`
	Content             string            `json:"content"`
	Title               string            `json:"title"`
	Hint                string            `json:"hint,omitempty"`
	MemoryKind          string            `json:"kind,omitempty"`
	Stability           string            `json:"stability,omitempty"`
	Tags                []string          `json:"tags,omitempty"`
	ContentType         string            `json:"content_type"`
	Source              string            `json:"source"`
	SourceAgent         string            `json:"source_agent,omitempty"`
	SourcePath          string            `json:"source_path"`
	HubReason           string            `json:"hub_reason,omitempty"`
	ProjectContext      map[string]string `json:"project_context,omitempty"`
	BatchID             string            `json:"batch_id,omitempty"`
	FileRef             *model.FileRef    `json:"file_ref,omitempty"`
	AllowRelatedContext bool              `json:"allow_related_context,omitempty"`
}

func (MemoryProcessArgs) Kind() string { return "memory_process" }

func (MemoryProcessArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "default",
		MaxAttempts: 3,
	}
}

// --- Dream Cycle Job ---
// Runs the full Memory Dreams consolidation pipeline for a hub.
// Triggered on-demand via API or nightly via periodic job.
//
// Uniqueness: the `river:"unique"` tag on HubID narrows the River
// uniqueness hash to the hub alone. Without it, the nightly sweep
// (TriggeredBy="") and a manual trigger (TriggeredBy=<userID>) would
// hash to different keys and both get scheduled, breaking the
// "at most one dream per hub" guarantee. The DB-level partial unique
// index on dream_runs (migration 012) is the second line of defense
// against the three dreams workers racing each other.
type DreamCycleArgs struct {
	HubID       string `json:"hub_id" river:"unique"`
	TriggeredBy string `json:"triggered_by,omitempty"`
}

func (DreamCycleArgs) Kind() string { return "dream_cycle" }

func (DreamCycleArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "dreams",
		MaxAttempts: 2,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 1 * time.Hour, // at most one dream per hub per hour
		},
	}
}

// --- Nightly Dream Sweep Job ---
// Periodic job that enqueues DreamCycleArgs for every dreamable hub.
// Runs once per day at 3 AM UTC.

// --- Config Extraction Job ---
// Distills knowledge items from an agent config file into proper memories.
// Triggered after a config upsert where content changed.

type ConfigExtractArgs struct {
	ConfigID string `json:"config_id"`
	OwnerID  string `json:"owner_id"`
}

func (ConfigExtractArgs) Kind() string { return "config_extract" }

func (ConfigExtractArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "default",
		MaxAttempts: 2,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 1 * time.Hour,
		},
	}
}

// --- Copy Onboarding Seed Memories Job ---
// Plan 23 §5.3 — fires once after a new user is created. Copies every
// enabled seed memory (rows in the system tutorial hub with
// source_kind = 'onboarding-seed') into the user's personal hub.
//
// Idempotency: triple-layered.
//   1. River unique-arg dedup (UserID hashed; same arg in window
//      collapses) prevents the same job firing twice from the rare
//      double-mount or retry loop.
//   2. The DB-level partial unique index from migration 002
//      (`memories_seed_origin_per_owner_uidx` on owner_id +
//      metadata->>'seed_origin_id' WHERE source_kind =
//      'onboarding-seed') catches concurrent inserts that beat the
//      worker's existence check.
//   3. The worker treats unique_violation on insert as success —
//      already-copied seeds aren't an error.
type CopySeedMemoriesArgs struct {
	UserID string `json:"user_id" river:"unique"`
}

func (CopySeedMemoriesArgs) Kind() string { return "copy_seed_memories" }

func (CopySeedMemoriesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "default",
		MaxAttempts: 3,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 24 * time.Hour, // a user's seeds copy at most once a day
		},
	}
}

type NightlyDreamSweepArgs struct{}

func (NightlyDreamSweepArgs) Kind() string { return "nightly_dream_sweep" }

func (NightlyDreamSweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "dreams",
		MaxAttempts: 1,
	}
}

// --- Send Email Job ---
// Renders a named template with variables and sends via the email.Sender.

type SendEmailArgs struct {
	Template  string            `json:"template"`  // template name (e.g., "waitlist_invite")
	To        string            `json:"to"`        // recipient email
	Variables map[string]string `json:"variables"` // template data
}

func (SendEmailArgs) Kind() string { return "send_email" }

func (SendEmailArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "default",
		MaxAttempts: 3,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 5 * time.Minute, // don't re-send the same email within 5 minutes
		},
	}
}

type SendRenderedEmailArgs struct {
	To      string            `json:"to"`
	Subject string            `json:"subject"`
	HTML    string            `json:"html"`
	Text    string            `json:"text"`
	Tags    map[string]string `json:"tags,omitempty"`
	// Headers are RFC 5322 mail headers (e.g., List-Unsubscribe). Used
	// by campaign emails to make the client show an unsubscribe UI.
	Headers map[string]string `json:"headers,omitempty"`
}

func (SendRenderedEmailArgs) Kind() string { return "send_rendered_email" }

func (SendRenderedEmailArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "default",
		MaxAttempts: 3,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 5 * time.Minute,
		},
	}
}

// --- Send Invite Reminder Job ---
// Periodic job that enqueues reminder emails for invites expiring within 48 hours.

type SendInviteRemindersArgs struct{}

func (SendInviteRemindersArgs) Kind() string { return "send_invite_reminders" }

func (SendInviteRemindersArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "default",
		MaxAttempts: 1,
	}
}

// --- Expire Waitlist Invites Job ---
// Periodic job that marks past-due invites as expired.

type ExpireWaitlistInvitesArgs struct{}

func (ExpireWaitlistInvitesArgs) Kind() string { return "expire_waitlist_invites" }

func (ExpireWaitlistInvitesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "default",
		MaxAttempts: 1,
	}
}

// --- Expire Notifications Job ---
// Periodic job that closes the notification lifecycle.

type ExpireNotificationsArgs struct{}

func (ExpireNotificationsArgs) Kind() string { return "expire_notifications" }

func (ExpireNotificationsArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "default",
		MaxAttempts: 1,
	}
}

// --- Broadcast Notification Job ---
// Sends a notification to all users by paging through the user list.
// Created by the admin notifications handler for mode=all.

type SendBroadcastNotificationArgs struct {
	NotifKind string          `json:"notif_kind"`
	Payload   json.RawMessage `json:"payload"`
	BatchID   string          `json:"batch_id"`
	AdminID   string          `json:"admin_id"`
}

func (SendBroadcastNotificationArgs) Kind() string { return "send_broadcast_notification" }

func (SendBroadcastNotificationArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "default",
		MaxAttempts: 3,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 1 * time.Hour, // at most one broadcast per batch per hour
		},
	}
}

// --- Campaign Send Job ---
// Runs the full send lifecycle for a persistent Campaign: loads the
// row, CAS-claims it into "sending", resolves its audience snapshot,
// fans out notifications, and finalizes status + counts.
//
// MaxAttempts is 1: a partial send on retry would double-deliver
// because we don't track per-recipient idempotency. On crash the
// campaign is left in "sending" — an operator can reset it to draft
// or hard-fail it rather than risk duplicates.

type CampaignSendArgs struct {
	CampaignID  string `json:"campaign_id"`
	InitiatedBy string `json:"initiated_by,omitempty"`
}

func (CampaignSendArgs) Kind() string { return "campaign_send" }

func (CampaignSendArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "default",
		MaxAttempts: 1,
	}
}

// --- Hub Freeze Sweep Job ---
// Periodic job that finds hubs whose grace window has expired and
// fires hub_frozen notifications to the owner + admins. Idempotent:
// the (source_kind, source_id) unique constraint dedupes already-sent
// rows, so running the sweep more often than strictly necessary is
// safe. Runs hourly in practice; the grace window is 7 days so
// missing a sweep is also safe — the hub is already frozen in
// enforcement, just waiting on the notification side-effect.

type HubFreezeSweepArgs struct{}

func (HubFreezeSweepArgs) Kind() string { return "hub_freeze_sweep" }

func (HubFreezeSweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "default",
		MaxAttempts: 1,
	}
}

// --- Usage Sync Job ---
// Periodic job that syncs committed Redis usage counters to Postgres.
// Runs every 5 minutes to keep the durable usage table roughly current
// for the /v1/usage endpoint and billing analytics.

type UsageSyncArgs struct{}

func (UsageSyncArgs) Kind() string { return "usage_sync" }

func (UsageSyncArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: "default",
		UniqueOpts: river.UniqueOpts{
			ByPeriod: 5 * time.Minute,
		},
		MaxAttempts: 1,
	}
}

// --- Chat Message Run Job ---
// Phase 3.4a — drives the agent runtime for one queued assistant
// message. The handler enqueues this after BeginChatMessageTurn's
// Fresh outcome commits; the worker loads the message + transcript,
// runs agent.Run, and finalizes the message via FinalizeChatMessage.
//
// MaxAttempts=1 because chat messages are user-visible — a retry
// would re-spend the user's quota on a turn the user has likely
// already moved on from. The lease sweeper (Phase 3.5) finalizes
// orphans as failed{server_crashed} instead of retrying.
type ChatMessageRunArgs struct {
	AssistantMessageID string `json:"assistant_message_id"`
	OwnerID            string `json:"owner_id"`
	SessionID          string `json:"session_id"`
}

func (ChatMessageRunArgs) Kind() string { return "chat_message_run" }

func (ChatMessageRunArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		// Dedicated queue so a backlog of chat turns can't starve
		// memory_process / dreams. Worker concurrency is tuned in
		// workerapp.
		Queue:       "chat",
		MaxAttempts: 1,
	}
}

// --- Chat Lease Sweep Job ---
// Phase 3.5a — periodic worker that catches chat sessions whose
// lease has expired (status running/canceling > 5min). Each
// orphaned assistant message is finalized as failed{server_crashed}
// via FinalizeChatMessage, which (per Phase 3.4b2.b v2) writes
// the terminal SSE row + resets the session lock in one tx.
//
// Runs on the default queue every minute via configurePeriodicJobs.
// Cheap: indexed SELECT bounded by limit, plus one finalize tx
// per stuck session.
type ChatLeaseSweepArgs struct{}

func (ChatLeaseSweepArgs) Kind() string { return "chat_lease_sweep" }

func (ChatLeaseSweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: "default",
		// MaxAttempts=2 — a transient store hiccup shouldn't
		// leave stuck sessions waiting an extra minute for the
		// next periodic firing.
		MaxAttempts: 2,
		// Unique by-period guards against double-fires from
		// overlapping periodic schedules; one sweep per minute
		// is plenty.
		UniqueOpts: river.UniqueOpts{ByPeriod: 30 * time.Second},
	}
}

// --- Chat Approval Timeout Sweep Job ---
// Phase 3.6b1 — periodic worker that flips pending approvals
// past their expires_at to `timed_out` and publishes
// approval.decided{state=timed_out} per row. The SDK-host
// approver (Phase 3.6b2) subscribes for these so the agent
// goroutine wakes at the expires_at boundary rather than the
// approver's own poll cadence.
//
// Plan 24 §"Approval flow" pins this as the timeout path
// alongside the user-decide path. Cadence is short (every
// minute) so a 10-minute approval that nobody answers expires
// within ~1m of the deadline.
type ChatApprovalSweepArgs struct{}

func (ChatApprovalSweepArgs) Kind() string { return "chat_approval_sweep" }

func (ChatApprovalSweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "default",
		MaxAttempts: 2,
		UniqueOpts:  river.UniqueOpts{ByPeriod: 30 * time.Second},
	}
}

// --- Chat Replay-Buffer Purge Job ---
// Phase 3.5a — periodic worker that deletes chat_message_events
// rows older than 24h. Plan 24 §"Send-message + resume" pins a
// 24h replay window; after that, a client reconnecting to a
// terminal message sees `410 chat_replay_expired` and re-fetches
// the final content via GET /messages/{id}.
//
// Runs hourly on the default queue. The store query is a single
// indexed DELETE WHERE created_at < cutoff.
type ChatEventPurgeArgs struct{}

func (ChatEventPurgeArgs) Kind() string { return "chat_event_purge" }

func (ChatEventPurgeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "default",
		MaxAttempts: 2,
		UniqueOpts:  river.UniqueOpts{ByPeriod: 30 * time.Minute},
	}
}

// --- Board Refresh Jobs (plan 25, Lane A) ---
// BoardSweepArgs fans out BoardRefreshArgs for every dreamable hub
// once a day; BoardRefreshArgs recomputes one hub's Lane A cards.
// Deterministic and cheap (pure SQL), so failures just wait for the
// next sweep. Rides the dreams queue: board refreshes are part of the
// same nightly intelligence cycle and don't need their own capacity.

type BoardSweepArgs struct{}

func (BoardSweepArgs) Kind() string { return "board_sweep" }

func (BoardSweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "dreams",
		MaxAttempts: 1,
	}
}

type BoardRefreshArgs struct {
	HubID string `json:"hub_id" river:"unique"`
}

func (BoardRefreshArgs) Kind() string { return "board_refresh" }

func (BoardRefreshArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "dreams",
		MaxAttempts: 2,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 10 * time.Minute, // dedupe sweep vs dream-chain triggers
		},
	}
}

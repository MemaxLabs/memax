// Package onboarding owns the plan 18 super-notif onboarding journey:
// the founder welcome note + first-week activation checklist that
// land on /memories for a new user and disappear once required items
// complete (or the user dismisses).
//
// The emitter creates the two notification rows on signup. The
// recorder (P2 next chunk) watches the five hot paths
// (memory.Create, ask, configs.Sync, hubs.Create+Accept,
// writeDreamReceipt) and ticks checklist items as the user crosses
// each milestone. The handler (already in place from P0) atomically
// fires applied_auto when every required item completes.
package onboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Checklist v1 item IDs. Kept in lockstep with the i18n keys in
// packages/web/src/i18n/locales/{en,zh}.ts under
// onboarding.checklist.items.{id} — adding a new item requires a copy
// edit in both locales OR the handler will fall back to rendering the
// raw item id (plan 18 §4.2 unknown-kind safety net).
const (
	ItemWelcome        = "welcome"
	ItemConnectAgent   = "connect_agent"
	ItemFirstMemory    = "first_memory"
	ItemFirstAsk       = "first_ask"
	ItemFiveMemories   = "five_memories"
	ItemFirstHubInvite = "first_hub_invite"
	ItemFirstDream     = "first_dream"
)

// checklistVersion identifies the v1 item set. When the curriculum
// changes (e.g. add a step, reorder, retire an item) bump the version
// + extend the restart handler so existing users keep their stable
// historical row and new signups get the new shape.
const checklistVersion = 1

// expiresAfter is the 14-day inactivity window from plan 18 §3.2.
// Past this the nightly sweep moves the row to status=expired and the
// user can re-enter via Settings → Getting started → Restart.
const expiresAfter = 14 * 24 * time.Hour

// Emitter is the plan-18 producer surface. Construct it once at
// startup with a Store reference; signup wiring calls EmitWelcome on
// new-user creation. Errors are logged but not propagated — the
// signup flow already treats onboarding seed copy the same way.
type Emitter struct {
	store store.Store
}

// New constructs an Emitter. Returns nil when store is nil so callers
// can use the "nil means disabled" pattern from CLAUDE.md.
func New(s store.Store) *Emitter {
	if s == nil {
		return nil
	}
	return &Emitter{store: s}
}

// EmitChecklist inserts a checklist row for the given user at the
// requested version. Used by both EmitWelcome (v1 on signup) and the
// restart handler (v{n+1}). Returns the inserted notification's ID +
// the version actually written.
func (e *Emitter) EmitChecklist(ctx context.Context, userID string, version int) (*model.Notification, error) {
	if e == nil {
		return nil, nil
	}
	if userID == "" {
		return nil, fmt.Errorf("EmitChecklist: empty user_id")
	}
	if version < 1 {
		version = 1
	}
	checklist := buildChecklistV1Payload()
	n := &model.Notification{
		ID:              uuid.NewString(),
		Audience:        model.AudienceUser,
		RecipientUserID: userID,
		Kind:            model.NotificationKindChecklist,
		Status:          model.NotificationStatusPending,
		SourceKind:      model.NotificationSourceOnboarding,
		SourceID:        checklistSourceID(userID, version),
		Payload:         mustMarshal(checklist),
		CreatedAt:       time.Now().UTC(),
		ExpiresAt:       expiresAt(),
	}
	if _, err := e.store.CreateNotificationIfAbsent(n); err != nil {
		return nil, err
	}
	// Re-fetch to get the row that landed (may have been upserted on
	// a retry; the caller wants the actual ID).
	got, err := e.store.GetNotificationBySourceKey(
		context.WithoutCancel(ctx),
		model.NotificationSourceOnboarding,
		n.SourceID,
	)
	if err != nil {
		// Couldn't reread; return the locally-built copy.
		return n, nil
	}
	return got, nil
}

// ParseChecklistVersion extracts the trailing `:vN` from a
// source_id of the form `{user_id}:v{n}`. Returns 0 when the format
// doesn't match — callers treat 0 as "no prior version" and emit v1.
func ParseChecklistVersion(sourceID string) int {
	idx := strings.LastIndex(sourceID, ":v")
	if idx < 0 {
		return 0
	}
	suffix := sourceID[idx+2:]
	v, err := strconv.Atoi(suffix)
	if err != nil || v < 1 {
		return 0
	}
	return v
}

// EmitWelcome inserts the two onboarding notification rows for a new
// user: a one-shot `system_notice` (founder voice) and a
// `checklist:v1` (first-week activation).
//
// Both rows are upserted via store.CreateNotificationIfAbsent so a
// signup retry that lands twice in the queue collapses cleanly on
// notifications_source_unique. The founder note keys on
// (onboarding_welcome, user_id) and the checklist keys on
// (onboarding, user_id:v1). Restart-onboarding (P3) bumps the v
// suffix; the founder note is one-shot per plan 18 L10.
//
// Returns nil on success, including the "row already existed" path —
// the caller doesn't need the inserted/upserted distinction.
func (e *Emitter) EmitWelcome(ctx context.Context, userID string) error {
	if e == nil {
		return nil
	}
	if userID == "" {
		return fmt.Errorf("EmitWelcome: empty user_id")
	}

	// Founder note — system_notice, body lives in i18n on the client.
	// The payload still carries title + body strings so the inbox
	// unknown-kind fallback (plan 18 §4.2 safety net) and the admin
	// preview surfaces have something readable. Voice mirrors the v3
	// founder-authored copy in
	// packages/web/src/i18n/locales/en.ts → onboarding.welcome.
	welcomePayload := model.SystemNoticePayload{
		Title: "Hey, welcome to memax.",
		Body: "We're a two-person team building the shared brain for you, " +
			"your AI, and your team. Connect your agents, or just start " +
			"dumping — memax does the organizing. — Ziyang & Jiahao",
	}
	if err := e.insertNotification(ctx, &model.Notification{
		ID:              uuid.NewString(),
		Audience:        model.AudienceUser,
		RecipientUserID: userID,
		Kind:            model.NotificationKindSystemNotice,
		Status:          model.NotificationStatusPending,
		SourceKind:      model.NotificationSourceOnboardingWelcome,
		SourceID:        userID, // one-shot per user
		Payload:         mustMarshal(pinnedPayload(welcomePayload, "memories_hero", "personal")),
		CreatedAt:       time.Now().UTC(),
	}); err != nil {
		slog.Warn("EmitWelcome: founder note insert failed",
			"user_id", userID, "err", err)
		// Continue — checklist is independent of the note.
	}

	// Checklist — v1 item set with required_ids matching the
	// auto-resolve at 5/7 rule we locked in pre-implementation
	// (plan 18 §3.5).
	checklist := buildChecklistV1Payload()
	if err := e.insertNotification(ctx, &model.Notification{
		ID:              uuid.NewString(),
		Audience:        model.AudienceUser,
		RecipientUserID: userID,
		Kind:            model.NotificationKindChecklist,
		Status:          model.NotificationStatusPending,
		SourceKind:      model.NotificationSourceOnboarding,
		SourceID:        checklistSourceID(userID, checklistVersion),
		Payload:         mustMarshal(checklist),
		CreatedAt:       time.Now().UTC(),
		ExpiresAt:       expiresAt(),
	}); err != nil {
		slog.Warn("EmitWelcome: checklist insert failed",
			"user_id", userID, "err", err)
		return err
	}
	return nil
}

// insertNotification wraps CreateNotificationIfAbsent so we can
// inspect the (inserted, err) tuple without leaking it to callers.
// "Already existed" is silent success; we just don't re-fire the
// realtime created event for the duplicate path.
func (e *Emitter) insertNotification(ctx context.Context, n *model.Notification) error {
	_ = ctx
	_, err := e.store.CreateNotificationIfAbsent(n)
	return err
}

// pinnedPayload re-emits a SystemNoticePayload with a pin_context
// hint so the client's PinnedNotifications host picks it up. The
// system_notice payload type doesn't have a pin_context field, so we
// shim it onto the raw JSON. Clients read it via a string lookup at
// payload.pin_context (the same path checklist + digest payloads
// declare it natively).
//
// pinScopeHubKind narrows the pin to a specific hub-view kind —
// "personal" keeps the onboarding founder note off team hub views.
// See model.ChecklistPayload.PinScopeHubKind for the full semantic.
func pinnedPayload(body model.SystemNoticePayload, pinContext, pinScopeHubKind string) map[string]any {
	p := map[string]any{
		"title":       body.Title,
		"body":        body.Body,
		"link":        body.Link,
		"link_text":   body.LinkText,
		"pin_context": pinContext,
	}
	if pinScopeHubKind != "" {
		p["pin_scope_hub_kind"] = pinScopeHubKind
	}
	return p
}

// checklistSourceID returns the source_id for a checklist row.
// Restart bumps `version`; the partial unique index on (source_kind,
// source_id) keeps each version a distinct row.
func checklistSourceID(userID string, version int) string {
	return fmt.Sprintf("%s:v%d", userID, version)
}

// expiresAt returns a 14-day expiry from now. Nightly sweep flips
// status=expired past this.
func expiresAt() *time.Time {
	t := time.Now().UTC().Add(expiresAfter)
	return &t
}

func mustMarshal(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		// Programmer error — fixed payload shapes should never fail
		// marshal. Falling back to "{}" keeps the row insertable so a
		// runtime panic doesn't take down signup, and the admin can
		// surface the breakage.
		slog.Error("onboarding: mustMarshal panic recovered", "err", err)
		return json.RawMessage("{}")
	}
	return raw
}

// buildChecklistV1Payload returns the v1 item set the emitter emits
// on new-user signup. Item titles + descriptions are placeholder
// strings — the client renders from i18n by item id, falling back to
// the payload strings only when the locale lookup misses (plan 18
// §4.2). RFC kept these as plain English so the unknown-kind safety
// net produces readable output instead of bare item ids.
func buildChecklistV1Payload() model.ChecklistPayload {
	return model.ChecklistPayload{
		Title:           "Your first week",
		Description:     "Get oriented in 5 minutes.",
		PinContext:      "memories_hero",
		PinScopeHubKind: "personal",
		RequiredIDs: []string{
			ItemConnectAgent,
			ItemFirstMemory,
			ItemFirstAsk,
			ItemFiveMemories,
			ItemFirstDream,
		},
		Items: []model.ChecklistItem{
			{
				Item: model.Item{
					ID:          ItemWelcome,
					Title:       "Read the welcome note",
					Description: "90 seconds. Worth it.",
					Icon:        "BookOpen",
					CTALabel:    "Read it",
					// No CTAURL: client opens the FounderNoteModal
					// inline and fires /complete on modal close. The
					// CTA stays visible even after completion so the
					// note is always re-viewable.
				},
			},
			{
				Item: model.Item{
					ID:          ItemConnectAgent,
					Title:       "Connect your first agent",
					Description: "Claude Code, Cursor, Codex — one command, memory flows both ways.",
					Icon:        "Bot",
					CTALabel:    "Set up",
					CTAURL:      "/agents",
				},
				Trigger: "configs_sync",
			},
			{
				Item: model.Item{
					ID:          ItemFirstMemory,
					Title:       "Dump your first memory",
					Description: "Type anything in the bar, hit ⌘↵. A wiki, a link, a half-thought.",
					Icon:        "PenLine",
					CTALabel:    "Try the bar",
				},
				Trigger: "memory_count_gte:1",
			},
			{
				Item: model.Item{
					ID:          ItemFirstAsk,
					Title:       "Ask memax a question",
					Description: "Press ↵ instead of ⌘↵ — memax answers from everything you've dumped.",
					Icon:        "MessageCircle",
					CTALabel:    "Try asking",
				},
				Trigger: "ask_count_gte:1",
			},
			{
				Item: model.Item{
					ID:          ItemFiveMemories,
					Title:       "Dump 5 memories",
					Description: "memax needs a critical mass to start seeing patterns. Unlocks dreams.",
					Icon:        "Library",
					Progress:    &model.ItemProgress{Current: 0, Target: 5},
				},
				Trigger: "memory_count_gte:5",
			},
			{
				Item: model.Item{
					ID:          ItemFirstHubInvite,
					Title:       "Join or start a team hub",
					Description: "Shared brain with people you work with. Jump in or start your own.",
					Icon:        "UsersRound",
					CTALabel:    "Start a hub",
					// No CTAURL: there is no /hubs landing page. Client
					// intercepts the click and opens HubCreateDialog
					// inline so users can start a hub without leaving
					// /memories.
				},
				Trigger: "hub_event",
			},
			{
				Item: model.Item{
					ID:          ItemFirstDream,
					Title:       "Let memax dream",
					Description: "Dreams stitch your memories together. Tap to run your first one.",
					Icon:        "Moon",
					CTALabel:    "Run a dream",
					// No CTAURL: client intercepts the click and fires
					// POST /v1/dreams/trigger directly, then
					// optimistically marks the item complete on
					// success (single-use). NO auto-tick from
					// dream_run_completed — that hook was dropped
					// 2026-05-19 per user direction. Only this CTA
					// completes the item.
				},
				LockedBy: []string{ItemFiveMemories},
				Trigger:  "user_click",
			},
		},
	}
}

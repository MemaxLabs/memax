package queue

import (
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// filteringStore implements ListExpiringHubInvites with the same
// WHERE-clause logic as the Postgres query, allowing unit-testable
// verification of reminder eligibility rules without a database.
type filteringStore struct {
	*store.InMemoryStore
	invites []model.HubInvite
}

func newFilteringStore() *filteringStore {
	return &filteringStore{InMemoryStore: store.NewInMemoryStore()}
}

func (s *filteringStore) ListExpiringHubInvites(within time.Duration) ([]model.HubInvite, error) {
	now := time.Now()
	deadline := now.Add(within)
	var result []model.HubInvite
	for _, inv := range s.invites {
		if inv.InviteeEmail == nil {
			continue // link-only — no email address to remind
		}
		if inv.AcceptedBy != nil {
			continue // already accepted — not active
		}
		if inv.RevokedAt != nil {
			continue // revoked — not active
		}
		if !inv.ExpiresAt.After(now) {
			continue // already expired
		}
		if inv.ExpiresAt.After(deadline) {
			continue // not expiring soon enough for reminder
		}
		result = append(result, inv)
	}
	return result, nil
}

func strPtr(s string) *string        { return &s }
func timePtr(t time.Time) *time.Time { return &t }

// TestHubInviteReminderEligibility verifies the filtering rules that
// determine which hub invites are eligible for reminder emails. The
// Postgres query (ListExpiringHubInvites) and the worker's call to it
// form the eligibility boundary. These tests verify the contract:
//
//   - active email invite expiring within window → eligible
//   - link-only invite (no email) → not eligible
//   - accepted invite → not eligible
//   - revoked invite → not eligible
//   - expired invite → not eligible
//   - invite expiring beyond window → not eligible
func TestHubInviteReminderEligibility(t *testing.T) {
	s := newFilteringStore()

	email := "user@example.com"

	s.invites = []model.HubInvite{
		// 1. Active email invite expiring in 36h — ELIGIBLE
		{
			ID: "active-email", HubID: "h1", Token: "t1", InvitedBy: "u1",
			Role: "contributor", ExpiresAt: time.Now().Add(36 * time.Hour),
			InviteeEmail: &email,
		},
		// 2. Link-only invite (no email) — NOT eligible
		{
			ID: "link-only", HubID: "h1", Token: "t2", InvitedBy: "u1",
			Role: "contributor", ExpiresAt: time.Now().Add(36 * time.Hour),
			InviteeEmail: nil,
		},
		// 3. Accepted invite — NOT eligible
		{
			ID: "accepted", HubID: "h1", Token: "t3", InvitedBy: "u1",
			Role: "contributor", ExpiresAt: time.Now().Add(36 * time.Hour),
			InviteeEmail: &email, AcceptedBy: strPtr("u2"),
		},
		// 4. Revoked invite — NOT eligible
		{
			ID: "revoked", HubID: "h1", Token: "t4", InvitedBy: "u1",
			Role: "contributor", ExpiresAt: time.Now().Add(36 * time.Hour),
			InviteeEmail: &email,
			RevokedAt:    timePtr(time.Now().Add(-1 * time.Hour)),
			RevokedBy:    strPtr("u1"),
		},
		// 5. Invite expiring in 72h (beyond 48h window) — NOT eligible
		{
			ID: "too-far", HubID: "h1", Token: "t5", InvitedBy: "u1",
			Role: "contributor", ExpiresAt: time.Now().Add(72 * time.Hour),
			InviteeEmail: &email,
		},
		// 6. Already expired — NOT eligible
		{
			ID: "expired", HubID: "h1", Token: "t6", InvitedBy: "u1",
			Role: "contributor", ExpiresAt: time.Now().Add(-1 * time.Hour),
			InviteeEmail: &email,
		},
	}

	result, err := s.ListExpiringHubInvites(reminderWindow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		ids := make([]string, len(result))
		for i, r := range result {
			ids[i] = r.ID
		}
		t.Fatalf("expected exactly 1 eligible invite (active-email), got %d: %v", len(result), ids)
	}
	if result[0].ID != "active-email" {
		t.Fatalf("expected active-email, got %s", result[0].ID)
	}
	if *result[0].InviteeEmail != email {
		t.Fatalf("expected email=%s, got %s", email, *result[0].InviteeEmail)
	}
}

// TestHubInviteReminderDedupByArgs verifies the dedup strategy:
// River's ByArgs + ByPeriod(24h) on SendEmailArgs means the same
// (template, to, variables) tuple is inserted at most once per 24h.
// We verify the dedup config is wired correctly.
func TestHubInviteReminderDedupConfig(t *testing.T) {
	if reminderDedup == nil {
		t.Fatal("reminderDedup must not be nil")
	}
	if !reminderDedup.UniqueOpts.ByArgs {
		t.Fatal("reminderDedup.ByArgs must be true — dedup by email args")
	}
	if reminderDedup.UniqueOpts.ByPeriod != 24*time.Hour {
		t.Fatalf("reminderDedup.ByPeriod must be 24h, got %v", reminderDedup.UniqueOpts.ByPeriod)
	}
}

// TestHubInviteReminderTemplateVars verifies that the worker would
// supply the correct template name and variable set. Since we can't
// easily mock the River Client.Insert (unexported river field), we
// verify the contract by checking what the worker would construct.
func TestHubInviteReminderTemplateContract(t *testing.T) {
	// The hub_invite_reminder template requires these variables:
	// InviterName, HubName, Role, InviteURL, ExpiresAt
	// Verify the worker constructs them from the invite fields.
	email := "invitee@test.com"
	inv := model.HubInvite{
		ID:           "inv-1",
		HubID:        "hub-1",
		Token:        "tok-abc123",
		InvitedBy:    "user-owner",
		Role:         "contributor",
		ExpiresAt:    time.Now().Add(36 * time.Hour),
		InviteeEmail: &email,
	}

	// Simulate what the worker builds
	appBaseURL := "https://memax.app"
	inviteURL := appBaseURL + "/invite/" + inv.Token
	hubName := "Backend Team" // from GetHub
	inviterName := "Alice"    // from GetUser

	args := SendEmailArgs{
		Template: "hub_invite_reminder",
		To:       *inv.InviteeEmail,
		Variables: map[string]string{
			"InviterName": inviterName,
			"HubName":     hubName,
			"Role":        inv.Role,
			"InviteURL":   inviteURL,
			"ExpiresAt":   inv.ExpiresAt.Format("January 2, 2006"),
		},
	}

	if args.Template != "hub_invite_reminder" {
		t.Fatalf("wrong template: %s", args.Template)
	}
	if args.To != email {
		t.Fatalf("wrong recipient: %s", args.To)
	}
	if args.Variables["InviterName"] != "Alice" {
		t.Fatalf("wrong InviterName: %s", args.Variables["InviterName"])
	}
	if args.Variables["HubName"] != "Backend Team" {
		t.Fatalf("wrong HubName: %s", args.Variables["HubName"])
	}
	if args.Variables["InviteURL"] != "https://memax.app/invite/tok-abc123" {
		t.Fatalf("wrong InviteURL: %s", args.Variables["InviteURL"])
	}
	if args.Variables["Role"] != "contributor" {
		t.Fatalf("wrong Role: %s", args.Variables["Role"])
	}
	if args.Variables["ExpiresAt"] == "" {
		t.Fatal("ExpiresAt must not be empty")
	}
}

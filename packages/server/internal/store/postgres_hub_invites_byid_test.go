package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresAcceptHubInviteByID covers the by-id accept/decline path
// used by the inbox notification resolver. Skipped without DATABASE_URL.
func TestPostgresAcceptHubInviteByID(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping Postgres invite by-id test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool)

	inviter := "00000000-0000-0000-0000-aaaaaa200001"
	invitee := "00000000-0000-0000-0000-aaaaaa200002"
	other := "00000000-0000-0000-0000-aaaaaa200003"
	hubID := "00000000-0000-0000-0000-aaaaaa200004"
	now := time.Now()

	cleanup := func() {
		pool.Exec(ctx, `DELETE FROM hub_invites WHERE hub_id = $1::uuid`, hubID)
		pool.Exec(ctx, `DELETE FROM hub_members WHERE hub_id = $1::uuid`, hubID)
		pool.Exec(ctx, `DELETE FROM hubs WHERE id = $1::uuid`, hubID)
		pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1::uuid[])`, []string{inviter, invitee, other})
	}
	cleanup()
	t.Cleanup(cleanup)

	for _, u := range []struct{ id, email string }{
		{inviter, "inviter@invite-test.local"},
		{invitee, "invitee@invite-test.local"},
		{other, "other@invite-test.local"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, name, created_at, updated_at) VALUES ($1::uuid, $2, $3, $4, $4)`,
			u.id, u.email, u.email, now); err != nil {
			t.Fatalf("insert user %s: %v", u.email, err)
		}
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO hubs (id, name, slug, hub_type, owner_id, created_at, updated_at) VALUES ($1::uuid, 'invite-test-hub', 'invite-test-hub', 'team', $2::uuid, $3, $3)`,
		hubID, inviter, now); err != nil {
		t.Fatalf("insert hub: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'owner')`,
		hubID, inviter); err != nil {
		t.Fatalf("insert hub member: %v", err)
	}

	createInvite := func(t *testing.T, id, token string, invitee *string, expiresAt time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO hub_invites (id, hub_id, token, invited_by, role, expires_at, created_at, invitee_user_id, invitee_email, email_enqueued_at)
			 VALUES ($1::uuid, $2::uuid, $3, $4::uuid, 'contributor', $5, now(), $6, NULL, NULL)`,
			id, hubID, token, inviter, expiresAt, invitee); err != nil {
			t.Fatalf("insert invite %s: %v", token, err)
		}
	}
	resetInvitesAndMembers := func(t *testing.T) {
		t.Helper()
		pool.Exec(ctx, `DELETE FROM hub_invites WHERE hub_id = $1::uuid`, hubID)
		pool.Exec(ctx, `DELETE FROM hub_members WHERE hub_id = $1::uuid AND user_id <> $2::uuid`, hubID, inviter)
	}

	t.Run("happy path: invitee accepts, becomes a member", func(t *testing.T) {
		resetInvitesAndMembers(t)
		inviteID := "00000000-0000-0000-0000-bbbbbb200001"
		createInvite(t, inviteID, "tok-happy", &invitee, time.Now().Add(24*time.Hour))

		got, err := s.AcceptHubInviteByID(ctx, inviteID, invitee, -1)
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		if got.AcceptedBy == nil || *got.AcceptedBy != invitee {
			t.Errorf("accepted_by: got %v, want %s", got.AcceptedBy, invitee)
		}
		var role string
		if err := pool.QueryRow(ctx,
			`SELECT role FROM hub_members WHERE hub_id = $1::uuid AND user_id = $2::uuid`,
			hubID, invitee,
		).Scan(&role); err != nil {
			t.Fatalf("scan member: %v", err)
		}
		if role != "contributor" {
			t.Errorf("role: got %s, want contributor", role)
		}
	})

	t.Run("refuse email-only invite (invitee_user_id null)", func(t *testing.T) {
		resetInvitesAndMembers(t)
		inviteID := "00000000-0000-0000-0000-bbbbbb200002"
		createInvite(t, inviteID, "tok-emailonly", nil, time.Now().Add(24*time.Hour))
		_, err := s.AcceptHubInviteByID(ctx, inviteID, invitee, -1)
		if !errors.Is(err, ErrHubInviteNotAddressable) {
			t.Errorf("got %v, want ErrHubInviteNotAddressable", err)
		}
	})

	t.Run("refuse wrong invitee", func(t *testing.T) {
		resetInvitesAndMembers(t)
		inviteID := "00000000-0000-0000-0000-bbbbbb200003"
		createInvite(t, inviteID, "tok-wrong", &invitee, time.Now().Add(24*time.Hour))
		_, err := s.AcceptHubInviteByID(ctx, inviteID, other, -1)
		if !errors.Is(err, ErrHubInviteWrongInvitee) {
			t.Errorf("got %v, want ErrHubInviteWrongInvitee", err)
		}
	})

	t.Run("refuse expired", func(t *testing.T) {
		resetInvitesAndMembers(t)
		inviteID := "00000000-0000-0000-0000-bbbbbb200004"
		createInvite(t, inviteID, "tok-expired", &invitee, time.Now().Add(-1*time.Hour))
		_, err := s.AcceptHubInviteByID(ctx, inviteID, invitee, -1)
		if !errors.Is(err, ErrHubInviteExpired) {
			t.Errorf("got %v, want ErrHubInviteExpired", err)
		}
	})

	t.Run("refuse already accepted", func(t *testing.T) {
		resetInvitesAndMembers(t)
		inviteID := "00000000-0000-0000-0000-bbbbbb200005"
		createInvite(t, inviteID, "tok-double", &invitee, time.Now().Add(24*time.Hour))
		if _, err := s.AcceptHubInviteByID(ctx, inviteID, invitee, -1); err != nil {
			t.Fatalf("first accept: %v", err)
		}
		_, err := s.AcceptHubInviteByID(ctx, inviteID, invitee, -1)
		if !errors.Is(err, ErrHubInviteUsed) {
			t.Errorf("got %v, want ErrHubInviteUsed", err)
		}
	})

	t.Run("refuse not found", func(t *testing.T) {
		resetInvitesAndMembers(t)
		_, err := s.AcceptHubInviteByID(ctx, "00000000-0000-0000-0000-cccccc200099", invitee, -1)
		if !errors.Is(err, ErrHubInviteNotFound) {
			t.Errorf("got %v, want ErrHubInviteNotFound", err)
		}
	})

	t.Run("decline happy path: marks revoked_by = invitee", func(t *testing.T) {
		resetInvitesAndMembers(t)
		inviteID := "00000000-0000-0000-0000-bbbbbb200006"
		createInvite(t, inviteID, "tok-decline", &invitee, time.Now().Add(24*time.Hour))
		if err := s.DeclineHubInviteByID(ctx, inviteID, invitee); err != nil {
			t.Fatalf("decline: %v", err)
		}
		var revokedBy *string
		if err := pool.QueryRow(ctx,
			`SELECT revoked_by::text FROM hub_invites WHERE id = $1::uuid`, inviteID,
		).Scan(&revokedBy); err != nil {
			t.Fatalf("scan revoked_by: %v", err)
		}
		if revokedBy == nil || *revokedBy != invitee {
			t.Errorf("revoked_by: got %v, want %s", revokedBy, invitee)
		}
		// No hub_members row should exist for the invitee.
		var memberExists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM hub_members WHERE hub_id = $1::uuid AND user_id = $2::uuid)`,
			hubID, invitee,
		).Scan(&memberExists); err != nil {
			t.Fatalf("check member: %v", err)
		}
		if memberExists {
			t.Errorf("decline must not create a hub_members row")
		}
	})

	t.Run("decline refuses wrong invitee", func(t *testing.T) {
		resetInvitesAndMembers(t)
		inviteID := "00000000-0000-0000-0000-bbbbbb200007"
		createInvite(t, inviteID, "tok-decline-wrong", &invitee, time.Now().Add(24*time.Hour))
		err := s.DeclineHubInviteByID(ctx, inviteID, other)
		if !errors.Is(err, ErrHubInviteWrongInvitee) {
			t.Errorf("got %v, want ErrHubInviteWrongInvitee", err)
		}
	})

	t.Run("decline refuses email-only invite", func(t *testing.T) {
		resetInvitesAndMembers(t)
		inviteID := "00000000-0000-0000-0000-bbbbbb200008"
		createInvite(t, inviteID, "tok-decline-email", nil, time.Now().Add(24*time.Hour))
		err := s.DeclineHubInviteByID(ctx, inviteID, invitee)
		if !errors.Is(err, ErrHubInviteNotAddressable) {
			t.Errorf("got %v, want ErrHubInviteNotAddressable", err)
		}
	})

	t.Run("decline refuses already accepted", func(t *testing.T) {
		resetInvitesAndMembers(t)
		inviteID := "00000000-0000-0000-0000-bbbbbb200009"
		createInvite(t, inviteID, "tok-decline-accepted", &invitee, time.Now().Add(24*time.Hour))
		if _, err := s.AcceptHubInviteByID(ctx, inviteID, invitee, -1); err != nil {
			t.Fatalf("accept: %v", err)
		}
		err := s.DeclineHubInviteByID(ctx, inviteID, invitee)
		if !errors.Is(err, ErrHubInviteUsed) {
			t.Errorf("got %v, want ErrHubInviteUsed", err)
		}
	})
}

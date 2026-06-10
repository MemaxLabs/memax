package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Errors specific to the by-id invite path. The token-based path keeps
// the existing Err* set in store.go untouched.
var (
	// ErrHubInviteNotAddressable is returned when the invite has no
	// invitee_user_id (legacy email-only invite). Notifications never
	// fire for these — the email reminder path stays the only way to
	// redeem them.
	ErrHubInviteNotAddressable = errors.New("hub invite is not addressed to a known user")

	// ErrHubInviteWrongInvitee is returned when the caller is not the
	// invitee_user_id pinned on the invite. Prevents a notification
	// resolver from accepting on behalf of someone else.
	ErrHubInviteWrongInvitee = errors.New("hub invite is addressed to a different user")
)

// AcceptHubInviteByID accepts an invite by primary key, requiring the
// caller to be the invitee_user_id pinned on the invite at creation
// time. Used by the notification resolver in the inbox framework
// (docs/plans/17-inbox-notification-framework.md §6.4) where the
// notification carries source_id = hub_invite.id, not a token.
//
// The legacy token-based AcceptHubInvite is unchanged and remains the
// sole path for the email-redeem flow.
func (s *PostgresStore) AcceptHubInviteByID(ctx context.Context, inviteID, userID string, maxMembers int) (*model.HubInvite, error) {
	var out *model.HubInvite
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		invite, err := s.AcceptHubInviteByIDTx(ctx, tx, inviteID, userID, maxMembers)
		if err != nil {
			return err
		}
		out = invite
		return nil
	})
	return out, err
}

// AcceptHubInviteByIDTx is the tx-aware variant. The flow mirrors the
// guard logic in the legacy token-based AcceptHubInvite (revoked /
// expired / already-accepted refusal under FOR UPDATE) but pivots on
// the invite's primary key and adds two checks:
//
//  1. The invite must have invitee_user_id set (no email-only invites
//     reach this path — those keep the existing token-redeem flow).
//  2. invitee_user_id must equal userID (no cross-user accept).
//
// On success: inserts the hub_members row, marks the invite accepted,
// and returns the updated invite. The caller publishes any
// hub-members-changed event after commit.
func (s *PostgresStore) AcceptHubInviteByIDTx(ctx context.Context, tx pgx.Tx, inviteID, userID string, maxMembers int) (*model.HubInvite, error) {
	invite, err := scanHubInvite(tx.QueryRow(ctx,
		`SELECT id, hub_id, token, invited_by, role, expires_at, accepted_by, accepted_at, revoked_by, revoked_at, created_at, invitee_user_id, invitee_email, email_enqueued_at
		FROM hub_invites
		WHERE id = $1::uuid
		FOR UPDATE`,
		inviteID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrHubInviteNotFound
		}
		return nil, err
	}

	if invite.InviteeUserID == nil {
		return nil, ErrHubInviteNotAddressable
	}
	if *invite.InviteeUserID != userID {
		return nil, ErrHubInviteWrongInvitee
	}

	now := time.Now()
	if invite.RevokedAt != nil {
		return nil, ErrHubInviteRevoked
	}
	if now.After(invite.ExpiresAt) {
		return nil, ErrHubInviteExpired
	}
	if invite.AcceptedBy != nil {
		return nil, ErrHubInviteUsed
	}

	// Enforce member cap inside the transaction with advisory lock
	if err := enforceMemberCap(ctx, tx, invite.HubID, maxMembers); err != nil {
		return nil, err
	}

	result, err := tx.Exec(ctx,
		`INSERT INTO hub_members (hub_id, user_id, role)
		VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT (hub_id, user_id) DO NOTHING`,
		invite.HubID, userID, invite.Role)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		return nil, ErrHubAlreadyMember
	}

	result, err = tx.Exec(ctx,
		`UPDATE hub_invites
		SET accepted_by = $1::uuid, accepted_at = $2
		WHERE id = $3::uuid
		  AND accepted_by IS NULL
		  AND revoked_at IS NULL`,
		userID, now, invite.ID)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		// Lost the race against another path (legacy token accept,
		// concurrent revoke, etc). The FOR UPDATE earlier should make
		// this unreachable, but treat it as a hard error so the tx
		// rolls back and the caller can retry.
		return nil, ErrHubInviteUsed
	}

	invite.AcceptedBy = &userID
	invite.AcceptedAt = &now
	return invite, nil
}

// DeclineHubInviteByID is the auto-commit decline entry point.
// The invitee chooses to refuse the invite from the notification
// resolver path. Flow:
//
//  1. Lock the invite by id.
//  2. Refuse if the invite has no invitee_user_id (email-only invite —
//     those don't surface as notifications and shouldn't be reached).
//  3. Refuse if invitee_user_id != caller.
//  4. Refuse if already accepted, revoked, or expired.
//  5. Mark the invite as revoked, with revoked_by = caller. Reusing
//     the revoke columns avoids a schema-change-just-for-decline; the
//     semantic distinction (admin revoke vs invitee decline) is
//     recoverable as `revoked_by == invitee_user_id`. If a future UI
//     needs a clean separation we can add a declined_by column then.
func (s *PostgresStore) DeclineHubInviteByID(ctx context.Context, inviteID, userID string) error {
	return s.WithTx(ctx, func(tx pgx.Tx) error {
		return s.DeclineHubInviteByIDTx(ctx, tx, inviteID, userID)
	})
}

// DeclineHubInviteByIDTx is the tx-aware variant of DeclineHubInviteByID.
func (s *PostgresStore) DeclineHubInviteByIDTx(ctx context.Context, tx pgx.Tx, inviteID, userID string) error {
	invite, err := scanHubInvite(tx.QueryRow(ctx,
		`SELECT id, hub_id, token, invited_by, role, expires_at, accepted_by, accepted_at, revoked_by, revoked_at, created_at, invitee_user_id, invitee_email, email_enqueued_at
		FROM hub_invites
		WHERE id = $1::uuid
		FOR UPDATE`,
		inviteID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrHubInviteNotFound
		}
		return err
	}

	if invite.InviteeUserID == nil {
		return ErrHubInviteNotAddressable
	}
	if *invite.InviteeUserID != userID {
		return ErrHubInviteWrongInvitee
	}
	if invite.AcceptedBy != nil {
		return ErrHubInviteUsed
	}
	if invite.RevokedAt != nil {
		return ErrHubInviteRevoked
	}
	if time.Now().After(invite.ExpiresAt) {
		return ErrHubInviteExpired
	}

	tag, err := tx.Exec(ctx,
		`UPDATE hub_invites
		SET revoked_by = $1::uuid, revoked_at = now()
		WHERE id = $2::uuid
		  AND accepted_by IS NULL
		  AND revoked_at IS NULL`,
		userID, inviteID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		// Defensive: the FOR UPDATE earlier pinned the row.
		return ErrHubInviteUsed
	}
	return nil
}

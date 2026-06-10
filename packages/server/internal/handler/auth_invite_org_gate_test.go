package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// The registration gate is a pure function — we test it in isolation
// so we don't need a real pgx pool for the "gate opens" cases. The
// separate integration test below exercises the existing-user login
// path via InMemoryStore to make sure a known identity bypasses the
// gate regardless of org membership or invite token.

func TestCheckRegistrationGate(t *testing.T) {
	cases := []struct {
		name string
		mode string
		opts loginOpts
		want error
	}{
		{
			name: "invite_only without any credential — reject",
			mode: "invite_only",
			opts: loginOpts{},
			want: ErrRegistrationRequired,
		},
		{
			name: "invite_only with invite token — pass",
			mode: "invite_only",
			opts: loginOpts{InviteToken: "hub-invite-for-friend-of-friend"},
			want: nil,
		},
		{
			name: "invite_only with org membership and no invite — pass (the bug fix)",
			mode: "invite_only",
			opts: loginOpts{ProviderOrgMember: true},
			want: nil,
		},
		{
			name: "invite_only with both signals — pass",
			mode: "invite_only",
			opts: loginOpts{InviteToken: "tok", ProviderOrgMember: true},
			want: nil,
		},
		{
			name: "open mode — always pass",
			mode: "open",
			opts: loginOpts{},
			want: nil,
		},
		{
			name: "org_gated without org membership — reject (hard gate)",
			mode: "org_gated",
			opts: loginOpts{},
			want: ErrRegistrationRequired,
		},
		{
			name: "org_gated with org membership — pass",
			mode: "org_gated",
			opts: loginOpts{ProviderOrgMember: true},
			want: nil,
		},
		{
			name: "org_gated with invite token only — reject (invite doesn't substitute for org)",
			mode: "org_gated",
			opts: loginOpts{InviteToken: "whatever"},
			want: ErrRegistrationRequired,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkRegistrationGate(tc.mode, tc.opts)
			if !errors.Is(got, tc.want) && !(got == nil && tc.want == nil) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoginOrCreateUser_ExistingIdentity_BypassesGate(t *testing.T) {
	// Existing identity must log in regardless of the registration
	// gate. Left-the-org employees and legacy accounts predate any
	// invite-mode changes. GetUserByAuthIdentity fires before the
	// gate; this lock-in test catches a future refactor that
	// accidentally moves the gate above the identity lookup.
	s := store.NewInMemoryStore()
	h := &AuthHandler{store: s, registrationMode: "invite_only"}

	pu := providerUser{
		Provider:      "github",
		ProviderID:    "gh-42",
		Email:         "former-employee@example.com",
		EmailVerified: true,
		Name:          "Former Employee",
	}
	u := &model.User{
		ID:    "11111111-1111-1111-1111-111111111111",
		Email: pu.Email,
		Name:  pu.Name,
	}
	s.AddUser(u)
	if err := s.CreateAuthIdentity(u.ID, pu.Provider, pu.ProviderID, pu.Email, pu.Name, pu.AvatarURL); err != nil {
		t.Fatalf("seed CreateAuthIdentity: %v", err)
	}

	// No invite token, no org membership — the gate would normally
	// reject, but the identity lookup short-circuits first.
	user, err := h.loginOrCreateUser(context.Background(), pu, loginOpts{})
	if err != nil {
		t.Fatalf("existing user should log in regardless of gate, got %v", err)
	}
	if user == nil || user.ID != u.ID {
		t.Fatalf("expected existing user %q, got %+v", u.ID, user)
	}
}

// TestTryLoginOrCreate_HubInvite_OrgGated locks in the Codex finding
// that a valid hub invite must NOT substitute for org membership in
// `org_gated` mode. A non-member whose only signal is a hub invite
// must hit ErrRegistrationRequired at the gate, before any pool tx
// is attempted. This is the new-user path: existing identities
// short-circuit earlier and are covered separately.
//
// The opposite case (invite_only: valid hub invite authorizes the
// signup and skips the waitlist-claim tx) can't be proven with
// InMemoryStore because the flow reaches h.pool.Begin(ctx) on a nil
// pool; we verify that path by reading the diff and through the gate
// unit test in TestCheckRegistrationGate (the hub-invite branch
// promotes gateOpts.ProviderOrgMember = true, which that table
// already exercises under "invite_only with org membership and no
// invite — pass"). A Postgres-backed integration test would pin down
// the full hub-invite success path; tracked as follow-up.
func TestTryLoginOrCreate_HubInvite_OrgGated(t *testing.T) {
	invitee := "friend-of-friend@example.com"
	active := time.Now().Add(24 * time.Hour)

	s := store.NewInMemoryStore()
	s.AddHubInvite(&model.HubInvite{
		ID: "addr-1", HubID: "hub-1", Token: "hub-tok",
		ExpiresAt: active, InviteeEmail: &invitee,
	})
	h := &AuthHandler{store: s, registrationMode: "org_gated"}

	pu := providerUser{
		Provider:      "github",
		ProviderID:    "gh-999",
		Email:         invitee,
		EmailVerified: true,
		Name:          "Friend of Friend",
	}

	_, err := h.loginOrCreateUser(context.Background(), pu,
		loginOpts{InviteToken: "hub-tok"})
	if !errors.Is(err, ErrRegistrationRequired) {
		t.Fatalf("org_gated must reject hub-invite-only signup; got %v", err)
	}
}

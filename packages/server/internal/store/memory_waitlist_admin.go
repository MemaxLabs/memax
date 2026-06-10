package store

import (
	"context"
	"fmt"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// Stub implementations for InMemoryStore (dev mode).
// These are not production-ready — they exist only to satisfy the Store interface.

func (s *InMemoryStore) CreateWaitlistEntry(_ context.Context, _ *model.WaitlistEntry) error {
	return fmt.Errorf("waitlist: not supported in memory store")
}

func (s *InMemoryStore) GetWaitlistEntry(_ context.Context, _ string) (*model.WaitlistEntry, error) {
	return nil, fmt.Errorf("waitlist: not supported in memory store")
}

func (s *InMemoryStore) GetWaitlistEntryByEmail(_ context.Context, _ string) (*model.WaitlistEntry, error) {
	return nil, fmt.Errorf("waitlist: not supported in memory store")
}

func (s *InMemoryStore) ListWaitlistEntries(_ context.Context, _ model.WaitlistListOpts) ([]model.WaitlistEntry, string, int, error) {
	return nil, "", 0, fmt.Errorf("waitlist: not supported in memory store")
}

func (s *InMemoryStore) UpdateWaitlistEntry(_ context.Context, _ string, _ string, _ string, _ *int, _ string) (*model.WaitlistEntry, error) {
	return nil, fmt.Errorf("waitlist: not supported in memory store")
}

func (s *InMemoryStore) BatchApproveWaitlist(_ context.Context, _ []string, _ int, _ string, _ string) (int, error) {
	return 0, fmt.Errorf("waitlist: not supported in memory store")
}

func (s *InMemoryStore) GetWaitlistStats(_ context.Context) (*model.WaitlistStats, error) {
	return &model.WaitlistStats{}, nil
}

func (s *InMemoryStore) CreateWaitlistInvite(_ context.Context, _ *model.WaitlistInvite) error {
	return fmt.Errorf("waitlist: not supported in memory store")
}

func (s *InMemoryStore) GetWaitlistInviteByToken(_ context.Context, _ string) (*model.WaitlistInvite, error) {
	return nil, fmt.Errorf("waitlist: not supported in memory store")
}

func (s *InMemoryStore) ConsumeWaitlistInvite(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("waitlist: not supported in memory store")
}

func (s *InMemoryStore) ExpireWaitlistInvites(_ context.Context) (int, error) {
	return 0, nil
}

func (s *InMemoryStore) ListWaitlistInvitesByEntry(_ context.Context, _ string) ([]model.WaitlistInvite, error) {
	return nil, nil
}

func (s *InMemoryStore) ListOrphanWaitlistCandidates(_ context.Context, _ model.OrphanWaitlistCandidateOpts) ([]model.OrphanWaitlistCandidate, error) {
	return nil, nil
}

func (s *InMemoryStore) GetOrphanCandidateForUser(_ context.Context, _ string) (*model.OrphanWaitlistCandidate, error) {
	return nil, nil
}

func (s *InMemoryStore) RevokeWaitlistInvite(_ context.Context, _ string) error {
	return fmt.Errorf("waitlist: not supported in memory store")
}

func (s *InMemoryStore) GetActiveInviteForEntry(_ context.Context, _ string) (*model.WaitlistInvite, error) {
	return nil, nil // no active invite in dev mode
}

func (s *InMemoryStore) ListExpiringWaitlistInvites(_ context.Context, _ int) ([]model.WaitlistInvite, error) {
	return nil, nil
}

func (s *InMemoryStore) SetCanCreateHub(_ context.Context, _ string, _ bool) error {
	return nil
}

func (s *InMemoryStore) GetCanCreateHub(_ context.Context, _ string) (bool, error) {
	return true, nil // dev mode: always allowed
}

func (s *InMemoryStore) GetAdminRole(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (s *InMemoryStore) ListAdminRoles(_ context.Context) ([]model.AdminRole, error) {
	return nil, nil
}

func (s *InMemoryStore) EnsureAdminRolesByEmail(_ context.Context, _ []string, _ string) (int, error) {
	return 0, nil
}

package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// InMemoryStore is an in-memory implementation of Store for development.
// Swap to PostgresStore for production.
type InMemoryStore struct {
	mu                           sync.RWMutex
	memories                     map[string]*model.Memory
	attachments                  map[string][]model.MemoryAttachment
	chunks                       map[string][]model.Chunk // memoryID -> chunks
	topics                       map[string]*model.Topic
	memoryTopics                 map[string]model.MemoryTopic
	hubs                         map[string]*model.Hub
	hubVisits                    map[string]*model.HubVisit
	agentConfigs                 map[string]*model.AgentConfig
	personas                     map[string]*model.Persona
	personaRevisions             map[string][]*model.PersonaRevision // personaID -> revisions
	configStates                 map[string]*model.AgentConfigSyncState
	tombstones                   map[string]*model.AgentConfigTombstone
	emailTemplateOverrides       map[string]*model.EmailTemplateOverride
	emailTemplateRevisionHistory map[string][]*model.EmailTemplateRevision

	// hubInvites: test-only storage so auth flow tests can seed a
	// hub invite and exercise the registration-authorization path.
	// Shares the outer mu.
	hubInvites map[string]*model.HubInvite

	brandMu            sync.RWMutex
	emailBrandSettings *model.EmailBrandSettings

	campaignDeliveriesMu sync.RWMutex
	campaignDeliveries   map[string]*model.CampaignDelivery

	// emailOptOut tracks marketing opt-out status for tests. Production
	// reads from the users.email_opt_out_marketing column; see migration
	// 008 and PostgresStore.IsUserEmailOptedOut.
	emailOptOut map[string]bool

	// emailOptOutTokens tracks per-user unsubscribe tokens for tests.
	// Production stores in users.email_opt_out_token. Shares the outer
	// mu mutex — tokens live alongside other user-scoped state.
	emailOptOutTokens map[string]string

	// Auth identity and OAuth state (for handler test parity)
	authIdentities map[string][]model.AuthIdentity // userID -> identities
	oauthStates    map[string]*model.OAuthState    // stateHash -> state
	users          map[string]*model.User          // userID -> user
	identitySeq    int                             // auto-increment for identity IDs

	// Email OTP codes — ordered insertion (newest last) so test
	// assertions can index by recency. otpSeq powers stable IDs.
	emailOTPCodes []*model.EmailOTPCode
	otpSeq        int

	// Campaign templates store is lazily initialized on first use via
	// the campaignTemplates() helper — avoids touching every
	// NewInMemoryStore caller in tests when the feature isn't exercised.
	campaignTemplatesOnce  sync.Once
	campaignTemplatesStore *campaignTemplateStore
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		memories:                     make(map[string]*model.Memory),
		attachments:                  make(map[string][]model.MemoryAttachment),
		chunks:                       make(map[string][]model.Chunk),
		topics:                       make(map[string]*model.Topic),
		memoryTopics:                 make(map[string]model.MemoryTopic),
		hubs:                         make(map[string]*model.Hub),
		hubVisits:                    make(map[string]*model.HubVisit),
		agentConfigs:                 make(map[string]*model.AgentConfig),
		configStates:                 make(map[string]*model.AgentConfigSyncState),
		tombstones:                   make(map[string]*model.AgentConfigTombstone),
		emailTemplateOverrides:       make(map[string]*model.EmailTemplateOverride),
		emailTemplateRevisionHistory: make(map[string][]*model.EmailTemplateRevision),
		emailOptOut:                  make(map[string]bool),
		emailOptOutTokens:            make(map[string]string),
		authIdentities:               make(map[string][]model.AuthIdentity),
		oauthStates:                  make(map[string]*model.OAuthState),
		users:                        make(map[string]*model.User),
	}
}

func (s *InMemoryStore) CreateMemory(memory *model.Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memories[memory.ID] = memory
	return nil
}

func (s *InMemoryStore) GetMemory(id string, ownerID string) (*model.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mem, ok := s.memories[id]
	if !ok || (ownerID != "local" && mem.OwnerID != ownerID) {
		return nil, fmt.Errorf("memory not found: %s", id)
	}
	return mem, nil
}

func (s *InMemoryStore) GetMemoryForAdmin(id string) (*model.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mem, ok := s.memories[id]
	if !ok {
		return nil, fmt.Errorf("memory not found: %s", id)
	}
	return mem, nil
}

func (s *InMemoryStore) FindSuspiciousMetadata(_ context.Context, _ int) ([]SuspiciousMetadataRow, error) {
	return nil, nil
}

func (s *InMemoryStore) GetAccessibleMemory(id string, userID string, hubIDs []string) (*model.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mem, ok := s.memories[id]
	if !ok {
		return nil, fmt.Errorf("memory not found: %s", id)
	}
	if userID == "local" || mem.OwnerID == userID {
		return mem, nil
	}
	for _, hubID := range hubIDs {
		if mem.HubID == hubID {
			return mem, nil
		}
	}
	return nil, fmt.Errorf("memory not found: %s", id)
}

// GetMemoryInHubs mirrors the Postgres impl: strict-hub-only
// filter, no owner-OR fallback. ErrEmptyHubIDsForStrictSearch
// when called with empty hubIDs (the scope-aware handler helper
// short-circuits this path; the store-side guard is defense in
// depth).
func (s *InMemoryStore) GetMemoryInHubs(_ context.Context, id string, hubIDs []string) (*model.Memory, error) {
	if len(hubIDs) == 0 {
		return nil, ErrEmptyHubIDsForStrictSearch
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	mem, ok := s.memories[id]
	if !ok {
		return nil, fmt.Errorf("memory not found: %s", id)
	}
	for _, hubID := range hubIDs {
		if mem.HubID == hubID {
			return mem, nil
		}
	}
	return nil, fmt.Errorf("memory not found: %s", id)
}

func (s *InMemoryStore) FindRelatedMemories(_ string, _ string, _ int) ([]model.RelatedMemory, error) {
	return nil, nil // No embeddings in dev store
}

func (s *InMemoryStore) FindRelatedForEnrichment(_ context.Context, _ []float64, _ string, _ []string, _ string, _ int) ([]model.EnrichmentCandidate, error) {
	return nil, nil // No embeddings in dev store
}

func (s *InMemoryStore) GetUserByCanonicalEmail(email string) (*model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	canonical := strings.ToLower(strings.TrimSpace(email))
	for _, u := range s.users {
		if strings.ToLower(strings.TrimSpace(u.Email)) == canonical {
			return u, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (s *InMemoryStore) GetUserByAuthIdentity(provider, providerID string) (*model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, identities := range s.authIdentities {
		for _, ai := range identities {
			if ai.Provider == provider && ai.ProviderID == providerID {
				u, ok := s.users[ai.UserID]
				if !ok {
					return nil, fmt.Errorf("not found")
				}
				return u, nil
			}
		}
	}
	return nil, fmt.Errorf("not found")
}

func (s *InMemoryStore) CreateAuthIdentity(userID, provider, providerID, email, name, avatar string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check for conflict: does this provider+providerID already exist for a different user?
	for ownerID, identities := range s.authIdentities {
		for _, ai := range identities {
			if ai.Provider == provider && ai.ProviderID == providerID {
				if ownerID == userID {
					return nil // idempotent — same user already owns it
				}
				return ErrIdentityConflict
			}
		}
	}
	s.identitySeq++
	s.authIdentities[userID] = append(s.authIdentities[userID], model.AuthIdentity{
		ID:            fmt.Sprintf("%d", s.identitySeq),
		UserID:        userID,
		Provider:      provider,
		ProviderID:    providerID,
		ProviderEmail: email,
		ProviderName:  name,
		CreatedAt:     time.Now(),
	})
	return nil
}

func (s *InMemoryStore) ListAuthIdentities(userID string) ([]model.AuthIdentity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.authIdentities[userID], nil
}

func (s *InMemoryStore) DeleteAuthIdentity(userID, provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	identities := s.authIdentities[userID]
	for i, ai := range identities {
		if ai.Provider == provider {
			s.authIdentities[userID] = append(identities[:i], identities[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (s *InMemoryStore) CreateOAuthState(_ context.Context, state model.OAuthState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.oauthStates[state.StateHash] = &state
	return nil
}

func (s *InMemoryStore) ConsumeOAuthState(_ context.Context, stateHash string) (*model.OAuthState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.oauthStates[stateHash]
	if !ok {
		return nil, fmt.Errorf("invalid or expired OAuth state")
	}
	if state.ConsumedAt != nil {
		return nil, fmt.Errorf("invalid or expired OAuth state")
	}
	if time.Now().After(state.ExpiresAt) {
		delete(s.oauthStates, stateHash)
		return nil, fmt.Errorf("invalid or expired OAuth state")
	}
	now := time.Now()
	state.ConsumedAt = &now
	return state, nil
}

func (s *InMemoryStore) CleanupExpiredOAuthStates(_ context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	for hash, state := range s.oauthStates {
		if time.Now().After(state.ExpiresAt) {
			delete(s.oauthStates, hash)
			count++
		}
	}
	return count, nil
}

// --- Email OTP codes (in-memory) ---

func (s *InMemoryStore) CreateEmailOTPCode(_ context.Context, code model.EmailOTPCode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.otpSeq++
	if code.ID == "" {
		code.ID = fmt.Sprintf("otp_%d", s.otpSeq)
	}
	if code.CreatedAt.IsZero() {
		code.CreatedAt = time.Now()
	}
	if code.Purpose == "" {
		code.Purpose = "login"
	}
	c := code
	s.emailOTPCodes = append(s.emailOTPCodes, &c)
	return nil
}

func (s *InMemoryStore) GetActiveEmailOTPCode(_ context.Context, email string) (*model.EmailOTPCode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	// Walk newest → oldest so we return the most recent active row.
	for i := len(s.emailOTPCodes) - 1; i >= 0; i-- {
		c := s.emailOTPCodes[i]
		if c.Email != email {
			continue
		}
		if c.ConsumedAt != nil {
			continue
		}
		if now.After(c.ExpiresAt) {
			continue
		}
		out := *c
		return &out, nil
	}
	return nil, ErrEmailOTPCodeNotFound
}

func (s *InMemoryStore) IncrementEmailOTPCodeAttempts(_ context.Context, id string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.emailOTPCodes {
		if c.ID == id {
			c.Attempts++
			return c.Attempts, nil
		}
	}
	return 0, fmt.Errorf("email OTP code not found: %s", id)
}

func (s *InMemoryStore) ConsumeEmailOTPCode(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.emailOTPCodes {
		if c.ID == id {
			if c.ConsumedAt != nil {
				return nil
			}
			now := time.Now()
			c.ConsumedAt = &now
			return nil
		}
	}
	return fmt.Errorf("email OTP code not found: %s", id)
}

func (s *InMemoryStore) CountRecentEmailOTPCodesForEmail(_ context.Context, email string, since time.Time) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, c := range s.emailOTPCodes {
		if c.Email == email && !c.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (s *InMemoryStore) CountRecentEmailOTPCodesForIP(_ context.Context, ip string, since time.Time) (int, error) {
	if ip == "" {
		return 0, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, c := range s.emailOTPCodes {
		if c.RequestIP == ip && !c.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (s *InMemoryStore) CleanupExpiredEmailOTPCodes(_ context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	kept := s.emailOTPCodes[:0]
	var removed int64
	for _, c := range s.emailOTPCodes {
		if now.After(c.ExpiresAt) {
			removed++
			continue
		}
		kept = append(kept, c)
	}
	s.emailOTPCodes = kept
	return removed, nil
}

// AddUser adds a user to the in-memory store for testing.
func (s *InMemoryStore) AddUser(user *model.User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[user.ID] = user
}

func (s *InMemoryStore) GetAccessibleMemories(ids []string, userID string, hubIDs []string) (map[string]*model.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	allowedHubs := make(map[string]struct{}, len(hubIDs))
	for _, hubID := range hubIDs {
		allowedHubs[hubID] = struct{}{}
	}

	result := make(map[string]*model.Memory, len(ids))
	for _, id := range ids {
		m, ok := s.memories[id]
		if !ok {
			continue
		}
		if m.OwnerID == userID {
			result[id] = m
			continue
		}
		if _, ok := allowedHubs[m.HubID]; ok {
			result[id] = m
		}
	}
	return result, nil
}

func (s *InMemoryStore) GetMemoryBySourcePath(sourcePath string, ownerID string, hubID string) (*model.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.memories {
		if m.SourcePath == sourcePath && sourcePath != "" &&
			(ownerID == "local" || m.OwnerID == ownerID) &&
			(hubID == "" || m.HubID == hubID) {
			return m, nil
		}
	}
	return nil, fmt.Errorf("memory not found for source_path: %s", sourcePath)
}

func (s *InMemoryStore) GetMemoryByContentHash(hash string, ownerID string, hubID string) (*model.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.memories {
		if m.ContentHash == hash &&
			(ownerID == "local" || m.OwnerID == ownerID) &&
			(hubID == "" || m.HubID == hubID) {
			return m, nil
		}
	}
	return nil, fmt.Errorf("memory not found for content_hash: %s", hash)
}

func (s *InMemoryStore) ListMemories(ownerID string, limit int) ([]model.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]model.Memory, 0)
	for _, m := range s.memories {
		if m.State == "archived" {
			continue
		}
		if ownerID != "local" && m.OwnerID != ownerID {
			continue
		}
		result = append(result, *m)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *InMemoryStore) UpdateMemory(memory *model.Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memories[memory.ID] = memory
	return nil
}

// SetUserFollowupMarker mirrors the Postgres semantics: ownerID
// must match the memory's owner; the marker (possibly empty
// string) replaces whatever was there. Tests of the ingest
// pipeline can hit this without a real Postgres.
func (s *InMemoryStore) SetUserFollowupMarker(_ context.Context, memoryID string, ownerID string, marker string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mem, ok := s.memories[memoryID]
	if !ok {
		return nil // Postgres equivalent: 0 rows affected, no error
	}
	if mem.OwnerID != ownerID {
		return nil
	}
	mem.UserFollowupMarker = marker
	return nil
}

func (s *InMemoryStore) IncrementMemoryShownBatch(_ context.Context, memoryIDs []string, _ string, _ []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range memoryIDs {
		if mem, ok := s.memories[id]; ok {
			mem.ShownCount++
		}
	}
	return nil
}

func (s *InMemoryStore) IncrementMemoryAccessedBatch(_ context.Context, memoryIDs []string, _ string, _ []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, id := range memoryIDs {
		if mem, ok := s.memories[id]; ok {
			mem.AccessCount++
			mem.AccessedAt = now
		}
	}
	return nil
}

func (s *InMemoryStore) IncrementMemoryAccessed(_ context.Context, memoryID string, _ string, _ []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mem, ok := s.memories[memoryID]; ok {
		mem.AccessCount++
		mem.AccessedAt = time.Now()
	}
	return nil
}

func (s *InMemoryStore) ListMemoriesPaginated(opts ListOptions) ([]model.Memory, string, int, error) {
	// Simple implementation — filter + delegate for in-memory store
	memories, err := s.ListMemories(opts.Scope.OwnerID, 0)
	if err != nil {
		return nil, "", 0, err
	}
	// Apply filters
	filtered := memories[:0]
	for _, m := range memories {
		if opts.HubID != "" && m.HubID != opts.HubID {
			continue
		}
		if opts.TopicID != "" {
			topic, ok := s.memoryTopics[m.ID]
			if !ok || topic.TopicID != opts.TopicID {
				continue
			}
		}
		if opts.CreatedAfter != nil && m.CreatedAt.Before(*opts.CreatedAfter) {
			continue
		}
		if opts.Actor != "" && opts.Actor != "all" {
			actor := "self"
			hubType := ""
			if hub := s.hubs[m.HubID]; hub != nil {
				hubType = hub.HubType
			}
			if hubType == "team" && m.AuthorName != "" {
				actor = "author:" + m.AuthorName
			} else if m.SourceAgent != "" {
				actor = "agent:" + m.SourceAgent
			} else if m.AuthorName != "" {
				actor = "author:" + m.AuthorName
			}
			if actor != opts.Actor {
				continue
			}
		}
		if opts.Kind != "" && m.Kind != opts.Kind {
			continue
		}
		filtered = append(filtered, m)
	}
	total := len(filtered)
	limit := opts.Limit
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, "", total, nil
}

func (s *InMemoryStore) ListMemoriesInHubs(_ context.Context, opts StrictHubListOptions) ([]model.Memory, string, int, error) {
	if len(opts.HubIDs) == 0 {
		return nil, "", 0, ErrEmptyHubIDsForStrictSearch
	}
	allowed := make(map[string]struct{}, len(opts.HubIDs))
	for _, hubID := range opts.HubIDs {
		allowed[hubID] = struct{}{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	memories := make([]model.Memory, 0)
	for _, m := range s.memories {
		if m.State == "archived" {
			continue
		}
		if _, ok := allowed[m.HubID]; !ok {
			continue
		}
		if opts.HubID != "" && m.HubID != opts.HubID {
			continue
		}
		if opts.TopicID != "" {
			topic, ok := s.memoryTopics[m.ID]
			if !ok || topic.TopicID != opts.TopicID {
				continue
			}
		}
		if !opts.Since.IsZero() && m.UpdatedAt.Before(opts.Since) {
			continue
		}
		memories = append(memories, *m)
	}
	sort.Slice(memories, func(i, j int) bool {
		if opts.Sort == "relevant" && memories[i].AccessCount != memories[j].AccessCount {
			return memories[i].AccessCount > memories[j].AccessCount
		}
		return memories[i].CreatedAt.After(memories[j].CreatedAt)
	})
	total := len(memories)
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	if len(memories) > limit {
		memories = memories[:limit]
	}
	return memories, "", total, nil
}

func (s *InMemoryStore) ListActorCounts(opts ListOptions) (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	counts := make(map[string]int)
	for _, m := range s.memories {
		if m.State == "archived" {
			continue
		}
		if opts.Scope.OwnerID != "local" && m.OwnerID != opts.Scope.OwnerID {
			allowed := false
			for _, hubID := range opts.Scope.HubIDs {
				if m.HubID == hubID {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}
		if opts.HubID != "" && m.HubID != opts.HubID {
			continue
		}
		if opts.TopicID != "" {
			topic, ok := s.memoryTopics[m.ID]
			if !ok || topic.TopicID != opts.TopicID {
				continue
			}
		}
		if opts.CreatedAfter != nil && m.CreatedAt.Before(*opts.CreatedAfter) {
			continue
		}

		actor := "self"
		hubType := ""
		if hub := s.hubs[m.HubID]; hub != nil {
			hubType = hub.HubType
		}
		if hubType == "team" && m.AuthorName != "" {
			actor = "author:" + m.AuthorName
		} else if m.SourceAgent != "" {
			actor = "agent:" + m.SourceAgent
		} else if m.AuthorName != "" {
			actor = "author:" + m.AuthorName
		}
		counts[actor]++
	}
	return counts, nil
}

func (s *InMemoryStore) DeleteMemory(id string, ownerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.memories, id)
	delete(s.attachments, id)
	delete(s.chunks, id)
	return nil
}

func (s *InMemoryStore) BatchDeleteMemories(ids []string, ownerID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := make([]string, 0, len(ids))
	for _, id := range ids {
		mem, ok := s.memories[id]
		if !ok {
			continue
		}
		// Match Postgres: owner scoping is enforced. "local" bypasses
		// the check for local-dev tests that don't track ownership.
		if ownerID != "local" && mem.OwnerID != ownerID {
			continue
		}
		delete(s.memories, id)
		delete(s.attachments, id)
		delete(s.chunks, id)
		deleted = append(deleted, id)
	}
	return deleted, nil
}

func (s *InMemoryStore) DeleteHubMemory(id string, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.memories, id)
	delete(s.attachments, id)
	delete(s.chunks, id)
	return nil
}

func (s *InMemoryStore) BatchDeleteHubMemories(ids []string, hubID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := make([]string, 0, len(ids))
	for _, id := range ids {
		mem, ok := s.memories[id]
		if !ok {
			continue
		}
		// Match Postgres: hub scoping is enforced.
		if hubID != "" && mem.HubID != hubID {
			continue
		}
		delete(s.memories, id)
		delete(s.attachments, id)
		delete(s.chunks, id)
		deleted = append(deleted, id)
	}
	return deleted, nil
}

func (s *InMemoryStore) BatchMoveToTopic(_ []string, _ string, _ string, _ float64) (int, error) {
	return 0, nil
}

func (s *InMemoryStore) BatchMoveMemories(ids []string, targetHubID string, targetTopicID string, ownerID string) (*model.BatchMoveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := &model.BatchMoveResult{Skipped: []model.SkippedMemory{}}
	for _, id := range ids {
		mem, ok := s.memories[id]
		if !ok {
			result.Skipped = append(result.Skipped, model.SkippedMemory{
				ID:     id,
				Reason: model.BatchMoveSkipNotFound,
			})
			continue
		}
		if ownerID != "local" && mem.OwnerID != ownerID {
			result.Skipped = append(result.Skipped, model.SkippedMemory{
				ID:     id,
				Reason: model.BatchMoveSkipNotOwned,
			})
			continue
		}
		currentTopicID := ""
		if existing, found := s.memoryTopics[id]; found {
			currentTopicID = existing.TopicID
		}
		if mem.HubID == targetHubID && currentTopicID == targetTopicID {
			result.Skipped = append(result.Skipped, model.SkippedMemory{
				ID:     id,
				Reason: model.BatchMoveSkipAlreadyAtTarget,
			})
			continue
		}
		mem.HubID = targetHubID
		if targetTopicID == "" {
			delete(s.memoryTopics, id)
		} else {
			s.memoryTopics[id] = model.MemoryTopic{
				MemoryID:   id,
				TopicID:    targetTopicID,
				Confidence: model.ConfidenceUserMove,
				CreatedAt:  time.Now(),
			}
		}
		result.Moved++
	}
	return result, nil
}

func (s *InMemoryStore) BatchMoveToHub(_ []string, _ string, _ string) (int, error) {
	return 0, nil
}

func (s *InMemoryStore) CreateMemoryAttachment(attachment *model.MemoryAttachment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attachments[attachment.MemoryID] = append(s.attachments[attachment.MemoryID], *attachment)
	return nil
}

func (s *InMemoryStore) ListMemoryAttachments(memoryID string, ownerID string) ([]model.MemoryAttachment, error) {
	if _, err := s.GetMemory(memoryID, ownerID); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := s.attachments[memoryID]
	if items == nil {
		return []model.MemoryAttachment{}, nil
	}
	out := make([]model.MemoryAttachment, len(items))
	copy(out, items)
	return out, nil
}

func (s *InMemoryStore) ListMemoryAttachmentsByIDs(memoryIDs []string, _ string) ([]model.MemoryAttachment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []model.MemoryAttachment
	for _, id := range memoryIDs {
		out = append(out, s.attachments[id]...)
	}
	return out, nil
}

func (s *InMemoryStore) GetMemoryAttachment(id string, memoryID string, ownerID string) (*model.MemoryAttachment, error) {
	items, err := s.ListMemoryAttachments(memoryID, ownerID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].ID == id {
			item := items[i]
			return &item, nil
		}
	}
	return nil, fmt.Errorf("attachment not found: %s", id)
}

// GetAttachmentByID is the unfiltered lookup used by the signed view
// endpoint. See the interface docstring for the security rationale.
func (s *InMemoryStore) GetAttachmentByID(id string) (*model.MemoryAttachment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, items := range s.attachments {
		for i := range items {
			if items[i].ID == id {
				item := items[i]
				return &item, nil
			}
		}
	}
	return nil, fmt.Errorf("attachment not found: %s", id)
}

func (s *InMemoryStore) ListOwnerMemoryAttachments(ownerID string) ([]model.MemoryAttachment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []model.MemoryAttachment
	for memoryID, items := range s.attachments {
		mem, ok := s.memories[memoryID]
		if !ok || (ownerID != "local" && mem.OwnerID != ownerID) {
			continue
		}
		out = append(out, items...)
	}
	if out == nil {
		out = []model.MemoryAttachment{}
	}
	return out, nil
}

func (s *InMemoryStore) CreateChunks(chunks []model.Chunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(chunks) == 0 {
		return nil
	}
	memoryID := chunks[0].MemoryID
	s.chunks[memoryID] = chunks
	return nil
}

func (s *InMemoryStore) UpdateChunk(chunk *model.Chunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	chunks, ok := s.chunks[chunk.MemoryID]
	if !ok {
		return fmt.Errorf("memory not found: %s", chunk.MemoryID)
	}
	for i, c := range chunks {
		if c.ID == chunk.ID {
			chunks[i] = *chunk
			return nil
		}
	}
	return fmt.Errorf("chunk not found: %s", chunk.ID)
}

func (s *InMemoryStore) GetChunksByMemory(memoryID string, ownerID string) ([]model.Chunk, error) {
	if _, err := s.GetMemory(memoryID, ownerID); err != nil {
		return nil, err
	}
	return s.getChunksByMemory(memoryID)
}

func (s *InMemoryStore) GetAccessibleChunksByMemory(memoryID string, userID string, hubIDs []string) ([]model.Chunk, error) {
	if _, err := s.GetAccessibleMemory(memoryID, userID, hubIDs); err != nil {
		return nil, err
	}
	return s.getChunksByMemory(memoryID)
}

func (s *InMemoryStore) getChunksByMemory(memoryID string) ([]model.Chunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	chunks, ok := s.chunks[memoryID]
	if !ok {
		return nil, fmt.Errorf("memory not found: %s", memoryID)
	}
	// Return a copy to avoid concurrent modification issues
	copied := make([]model.Chunk, len(chunks))
	copy(copied, chunks)
	return copied, nil
}

func (s *InMemoryStore) DeleteChunksByMemory(memoryID string, ownerID string) error {
	if _, err := s.GetMemory(memoryID, ownerID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.chunks, memoryID)
	return nil
}

func (s *InMemoryStore) AllChunks() []model.Chunk {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []model.Chunk
	for _, chunks := range s.chunks {
		all = append(all, chunks...)
	}
	return all
}

// SearchChunks performs a basic TF-IDF-like keyword search across chunks.
// In production, this is replaced by pgvector cosine similarity on embeddings.
func (s *InMemoryStore) SearchChunks(ctx context.Context, query string, _ []float64, ownerID string, limit int, _ *model.SearchFilters, _ *SearchOptions) ([]model.Chunk, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return nil, nil
	}

	type scored struct {
		chunk model.Chunk
		score float64
	}
	var results []scored

	for _, chunks := range s.chunks {
		for _, chunk := range chunks {
			mem, ok := s.memories[chunk.MemoryID]
			if !ok || mem.OwnerID != ownerID || mem.State == "archived" || mem.State == "processing" {
				continue
			}
			score := termMatchScore(queryTerms, chunkSearchText(chunk))
			if score > 0 {
				results = append(results, scored{chunk: chunk, score: score})
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	var out []model.Chunk
	for _, r := range results {
		r.chunk.RelevanceScore = r.score
		out = append(out, r.chunk)
	}
	return out, nil
}

func (s *InMemoryStore) ListDistinctOwners() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[string]bool)
	for _, m := range s.memories {
		if m.OwnerID != "local" {
			seen[m.OwnerID] = true
		}
	}
	owners := make([]string, 0, len(seen))
	for id := range seen {
		owners = append(owners, id)
	}
	return owners, nil
}

// --- Hubs (in-memory) ---

func (s *InMemoryStore) CreateHub(hub *model.Hub) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := cloneHub(hub)
	s.hubs[hub.ID] = stored
	return nil
}

// cloneHub deep-copies the reference fields on Hub so the stored
// value and any returned value don't alias. Notably, Hub.Settings
// is a map — a plain struct copy would share the same backing map
// between the store and every caller, letting mutations silently
// cross the boundary.
func cloneHub(hub *model.Hub) *model.Hub {
	if hub == nil {
		return nil
	}
	out := *hub
	if hub.Settings != nil {
		out.Settings = make(map[string]any, len(hub.Settings))
		for k, v := range hub.Settings {
			out.Settings[k] = v
		}
	}
	return &out
}

// CreateTeamHub matches the Postgres transactional contract: creates hub,
// adds owner as member, and creates hub_free_team subscription.
// In-memory store calls through to each individual method for consistency.
// Cap check is best-effort (no advisory lock in memory).
func (s *InMemoryStore) CreateTeamHub(ctx context.Context, hub *model.Hub, ownerID string, _ int) error {
	if err := s.CreateHub(hub); err != nil {
		return err
	}
	if err := s.AddHubMember(hub.ID, ownerID, "owner"); err != nil {
		return err
	}
	return s.CreateHubSubscription(ctx, &model.HubSubscription{
		HubID:         hub.ID,
		PlanID:        hub.Plan,
		SeatCount:     1,
		Provider:      "admin",
		Status:        "active",
		BillingUserID: ownerID,
	})
}

func (s *InMemoryStore) AddHubMemberWithCapCheck(_ context.Context, hubID, userID, role string, _ int) error {
	return s.AddHubMember(hubID, userID, role)
}

func (s *InMemoryStore) GetHubMemberCap(_ context.Context, _ string) (int, error) {
	return -1, nil // unlimited in memory
}

func (s *InMemoryStore) GetHub(id string) (*model.Hub, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hub, ok := s.hubs[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return cloneHub(hub), nil
}

func (s *InMemoryStore) GetHubBySlug(slug string) (*model.Hub, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, hub := range s.hubs {
		if hub.Slug == slug {
			return cloneHub(hub), nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (s *InMemoryStore) UpdateHub(hub *model.Hub) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hubs[hub.ID] = cloneHub(hub)
	return nil
}

// PatchHubSettings mirrors the Postgres atomic merge: null values
// in patch delete the key, non-nulls set it. Held under the
// store's RW lock so concurrent patches on the same hub compose
// rather than drop each other — matches the SQL guarantee the
// handler relies on.
func (s *InMemoryStore) PatchHubSettings(_ context.Context, hubID string, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hub, ok := s.hubs[hubID]
	if !ok {
		return ErrHubNotFound
	}
	if hub.Settings == nil {
		hub.Settings = make(map[string]any, len(patch))
	}
	for k, v := range patch {
		if v == nil {
			delete(hub.Settings, k)
		} else {
			hub.Settings[k] = v
		}
	}
	return nil
}

func (s *InMemoryStore) ListUserHubs(_ string) ([]model.HubWithRole, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]model.HubWithRole, 0, len(s.hubs))
	for _, hub := range s.hubs {
		result = append(result, model.HubWithRole{Hub: *hub, Role: "owner"})
	}
	return result, nil
}

// HubMembershipsForUser mirrors ListUserHubs's stub semantics:
// the in-memory store doesn't track per-user membership (every
// method returns "owner"), so this returns every hub in the
// store with role="owner". Tests that need precise
// membership/role-respecting behavior should use a focused
// fake at the agent-package level rather than the in-memory
// store. Sorted ascending by hub_id for determinism.
func (s *InMemoryStore) HubMembershipsForUser(_ context.Context, _ string) ([]model.HubMembership, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.hubs))
	for id := range s.hubs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]model.HubMembership, 0, len(ids))
	for _, id := range ids {
		out = append(out, model.HubMembership{HubID: id, Role: "owner"})
	}
	return out, nil
}

func (s *InMemoryStore) GetPersonalHub(userID string) (*model.Hub, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, hub := range s.hubs {
		if hub.OwnerID == userID && hub.HubType == "personal" {
			copy := *hub
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (s *InMemoryStore) AddHubMember(_, _, _ string) error                  { return nil }
func (s *InMemoryStore) RemoveHubMember(_, _ string) error                  { return nil }
func (s *InMemoryStore) UpdateHubMemberRole(_, _, _ string) error           { return nil }
func (s *InMemoryStore) ListHubMembers(_ string) ([]model.HubMember, error) { return nil, nil }
func (s *InMemoryStore) ListHubMembersPaginated(_ context.Context, _ string, _ model.AdminHubMemberListOpts) ([]model.HubMember, string, int, error) {
	return nil, "", 0, nil
}
func (s *InMemoryStore) GetHubMemberRole(_, _ string) (string, error) { return "owner", nil }
func (s *InMemoryStore) CountHubMembers(_ string) (int, error)        { return 1, nil }
func (s *InMemoryStore) GetHubVisit(userID, hubID string) (*model.HubVisit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	visit, ok := s.hubVisits[userID+":"+hubID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	copy := *visit
	return &copy, nil
}
func (s *InMemoryStore) UpsertHubVisit(userID, hubID string, visitedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := userID + ":" + hubID
	if visit, ok := s.hubVisits[key]; ok {
		visit.LastVisitedAt = visitedAt
		visit.UpdatedAt = visitedAt
		return nil
	}
	s.hubVisits[key] = &model.HubVisit{
		UserID:         userID,
		HubID:          hubID,
		FirstVisitedAt: visitedAt,
		LastVisitedAt:  visitedAt,
		CreatedAt:      visitedAt,
		UpdatedAt:      visitedAt,
	}
	return nil
}

// TopicVisit / lifecycle resolvers — minimal in-memory stubs so the dev
// InMemoryStore satisfies the Store interface. Lifecycle resolution in
// the in-memory store intentionally returns empty results; dream
// infrastructure is exercised against the Postgres store in tests and
// production, and in-memory runs don't schedule dream runs.
func (s *InMemoryStore) GetTopicVisit(userID, topicID string) (*model.TopicVisit, error) {
	return nil, fmt.Errorf("not found")
}
func (s *InMemoryStore) UpsertTopicVisit(userID, topicID, hubID string, visitedAt time.Time) error {
	return nil
}
func (s *InMemoryStore) ResolveMemoryLifecycleForList(
	_ context.Context, _ VisibilityScope, _ string, _ []string,
) (map[string]*model.MemoryLifecycle, error) {
	return map[string]*model.MemoryLifecycle{}, nil
}
func (s *InMemoryStore) ResolveMemoryLifecycleForDetail(
	_ context.Context, _ VisibilityScope, _, _ string,
) (*model.MemoryLifecycle, error) {
	return &model.MemoryLifecycle{DreamHistory: []model.DreamActionRef{}}, nil
}
func (s *InMemoryStore) ResolveTopicLifecycle(
	_ context.Context, _ VisibilityScope, _ string, _ []string,
) (map[string]*model.TopicLifecycle, error) {
	return map[string]*model.TopicLifecycle{}, nil
}

func (s *InMemoryStore) CountRecentMemoriesByHub(scope VisibilityScope, hubID string, createdAfter time.Time) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	allowedHubs := make(map[string]struct{}, len(scope.HubIDs))
	for _, id := range scope.HubIDs {
		allowedHubs[id] = struct{}{}
	}

	count := 0
	for _, memory := range s.memories {
		if memory.State == "archived" || memory.HubID != hubID {
			continue
		}
		if memory.CreatedAt.Before(createdAfter) {
			continue
		}
		if memory.OwnerID != scope.OwnerID {
			if _, ok := allowedHubs[memory.HubID]; !ok {
				continue
			}
		}
		count++
	}
	return count, nil
}
func (s *InMemoryStore) DeleteHub(_ string) error { return nil }

// hubInvites is populated by AddHubInvite (test helper). Production uses
// the full hub-invite schema in PostgresStore; the in-memory stub keeps
// just enough shape to exercise the auth/registration hub-invite lookup.
func (s *InMemoryStore) CreateHubInvite(inv *model.HubInvite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hubInvites == nil {
		s.hubInvites = map[string]*model.HubInvite{}
	}
	s.hubInvites[inv.Token] = inv
	return nil
}

// AddHubInvite is a test helper — seeds a hub invite so
// GetHubInviteByToken can return it from auth flow tests.
func (s *InMemoryStore) AddHubInvite(inv *model.HubInvite) {
	_ = s.CreateHubInvite(inv)
}

func (s *InMemoryStore) GetHubInvite(_ string) (*model.HubInvite, error) {
	return nil, fmt.Errorf("not found")
}
func (s *InMemoryStore) GetHubInviteByToken(token string) (*model.HubInvite, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inv, ok := s.hubInvites[token]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	// Mirror PostgresStore: revoked invites don't return.
	if inv.RevokedAt != nil {
		return nil, fmt.Errorf("not found")
	}
	return inv, nil
}
func (s *InMemoryStore) ListOutstandingHubInvites(_ string) ([]model.HubInvite, error) {
	return []model.HubInvite{}, nil
}
func (s *InMemoryStore) AcceptHubInvite(_, _ string, _ int) (*model.HubInvite, error) {
	return nil, nil
}
func (s *InMemoryStore) RevokeHubInvite(_, _, _ string) error { return nil }
func (s *InMemoryStore) CountActiveHubInvites(_ string) (int, error) {
	return 0, nil
}
func (s *InMemoryStore) UpdateHubInviteEmailEnqueuedAt(_ string, _ time.Time) error {
	return nil
}
func (s *InMemoryStore) ListExpiringHubInvites(_ time.Duration) ([]model.HubInvite, error) {
	return nil, nil
}
func (s *InMemoryStore) CreateHubOwnershipTransfer(_ *model.HubOwnershipTransfer) error {
	return nil
}
func (s *InMemoryStore) GetHubOwnershipTransfer(_ string) (*model.HubOwnershipTransfer, error) {
	return nil, fmt.Errorf("not found")
}
func (s *InMemoryStore) GetActiveHubOwnershipTransfer(_ string) (*model.HubOwnershipTransfer, error) {
	return nil, fmt.Errorf("not found")
}
func (s *InMemoryStore) AcceptHubOwnershipTransfer(_, _, _ string, _ int) (*model.HubOwnershipTransfer, error) {
	return nil, fmt.Errorf("not found")
}
func (s *InMemoryStore) CancelHubOwnershipTransfer(_, _ string) error { return nil }
func (s *InMemoryStore) GetUser(id string) (*model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	copy := *u
	return &copy, nil
}
func (s *InMemoryStore) GetUsersByIDs(_ []string) (map[string]*model.User, error) {
	return map[string]*model.User{}, nil
}
func (s *InMemoryStore) UpdateUserProfile(_, _ string) error                     { return nil }
func (s *InMemoryStore) IncrementUsage(_ string, _ string) error                 { return nil }
func (s *InMemoryStore) DecrementUsage(_ string, _ string) error                 { return nil }
func (s *InMemoryStore) IncrementUsageReturning(_ string, _ string) (int, error) { return 0, nil }
func (s *InMemoryStore) GetCurrentUsage(_ string) (*model.Usage, error)          { return &model.Usage{}, nil }

// Dream-quota stubs. The InMemoryStore is for unit tests of code
// that doesn't drive the dream-quota path; integration tests that
// actually exercise atomic-consume use testdb (real Postgres).
// These stubs return zero values so callers don't panic but no
// real bookkeeping happens.
func (s *InMemoryStore) GetUserDreamUsage(_ context.Context, _ string, _ time.Time) (basic, lucid int, err error) {
	return 0, 0, nil
}

func (s *InMemoryStore) GetHubDreamUsage(_ context.Context, _ string, _ time.Time) (lucid int, err error) {
	return 0, nil
}

func (s *InMemoryStore) ConsumeDreamRun(_ context.Context, params model.DreamConsumeParams) (model.DreamConsumeResult, error) {
	// Pretend to count countable runs so unit tests can verify
	// callers handle Counted=true paths. Real concurrency / race-loss
	// behavior is exercised by the postgres tests.
	if params.TerminalState == "completed" || params.TerminalState == "partial_failed" || params.TerminalState == "loop_cap_hit" {
		if params.ResolvedLimit != 0 {
			return model.DreamConsumeResult{
				Counted:       true,
				UsedAfter:     1,
				TerminalState: params.TerminalState,
			}, nil
		}
	}
	return model.DreamConsumeResult{
		Counted:       false,
		TerminalState: params.TerminalState,
	}, nil
}
func (s *InMemoryStore) SearchChunksForHubs(ctx context.Context, query string, _ []float64, ownerID string, hubIDs []string, limit int, _ *model.SearchFilters, _ *SearchOptions) ([]model.Chunk, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return nil, nil
	}

	allowedHubs := make(map[string]struct{}, len(hubIDs))
	for _, hubID := range hubIDs {
		allowedHubs[hubID] = struct{}{}
	}

	type scored struct {
		chunk model.Chunk
		score float64
	}
	var results []scored

	for _, chunks := range s.chunks {
		for _, chunk := range chunks {
			mem, ok := s.memories[chunk.MemoryID]
			if !ok || mem.State == "archived" || mem.State == "processing" {
				continue
			}
			if mem.OwnerID != ownerID {
				if _, ok := allowedHubs[mem.HubID]; !ok {
					continue
				}
			}
			score := termMatchScore(queryTerms, chunkSearchText(chunk))
			if score > 0 {
				results = append(results, scored{chunk: chunk, score: score})
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	var out []model.Chunk
	for _, r := range results {
		r.chunk.RelevanceScore = r.score
		out = append(out, r.chunk)
	}
	return out, nil
}

// SearchChunksInHubs is the strict-hub variant. Mirrors
// SearchChunksForHubs but skips the owner-fallback branch:
// matches require chunk.memory.hub_id ∈ hubIDs. Used by the
// agent runtime's narrow-recall paths where user-owned content
// in non-target hubs MUST NOT leak.
//
// Returns ErrEmptyHubIDsForStrictSearch when called with empty
// hubIDs — falling through to owner-only would be the wrong
// fail-open, matching the Postgres impl.
func (s *InMemoryStore) SearchChunksInHubs(ctx context.Context, query string, _ []float64, hubIDs []string, limit int, _ *model.SearchFilters, _ *SearchOptions) ([]model.Chunk, error) {
	if len(hubIDs) == 0 {
		return nil, ErrEmptyHubIDsForStrictSearch
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return nil, nil
	}

	allowedHubs := make(map[string]struct{}, len(hubIDs))
	for _, hubID := range hubIDs {
		allowedHubs[hubID] = struct{}{}
	}

	type scored struct {
		chunk model.Chunk
		score float64
	}
	var results []scored

	for _, chunks := range s.chunks {
		for _, chunk := range chunks {
			mem, ok := s.memories[chunk.MemoryID]
			if !ok || mem.State == "archived" || mem.State == "processing" {
				continue
			}
			// Strict: require hub membership; do NOT fall back to
			// owner-equality. This is the entire point — see
			// SearchChunksForHubs above for the OR-style version.
			if _, ok := allowedHubs[mem.HubID]; !ok {
				continue
			}
			score := termMatchScore(queryTerms, chunkSearchText(chunk))
			if score > 0 {
				results = append(results, scored{chunk: chunk, score: score})
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	var out []model.Chunk
	for _, r := range results {
		r.chunk.RelevanceScore = r.score
		out = append(out, r.chunk)
	}
	return out, nil
}

// --- User Preferences (in-memory) ---

func (s *InMemoryStore) GetUserPreferences(_ string) (*model.UserPreferences, error) {
	return &model.UserPreferences{Settings: map[string]any{}}, nil
}

func (s *InMemoryStore) UpsertUserPreferences(_ string, _ map[string]any) error { return nil }

func (s *InMemoryStore) ListDreamableHubs() ([]model.Hub, error) { return nil, nil }

// --- TxRunner (in-memory stubs) ---
//
// InMemoryStore has no real transaction semantics. WithTx invokes fn
// with a nil pgx.Tx; the *Tx-suffixed sibling helpers ignore the tx
// argument and fall through to no-ops. Test code that exercises
// producer fan-out with InMemoryStore relies on these no-ops; tests
// that need to assert real durability must run against a real Postgres.

func (s *InMemoryStore) WithTx(_ context.Context, fn func(pgx.Tx) error) error {
	return fn(nil)
}

func (s *InMemoryStore) CreateDreamActionTx(_ context.Context, _ pgx.Tx, _ *model.DreamAction) error {
	return nil
}

func (s *InMemoryStore) UpdateDreamRunTx(_ context.Context, _ pgx.Tx, _ *model.DreamRun) error {
	return nil
}

// MergeTopics / MergeTopicsTx — InMemoryStore stubs. The in-memory
// topic store is too thin (no parent_id traversal, no memory_topics
// join) to faithfully model the merge semantics, so both helpers
// no-op and return 0 moved. Tests that need real merge behavior must
// run against a Postgres backend (DATABASE_URL set).
func (s *InMemoryStore) MergeTopics(_ context.Context, _ string, _ string, _ []string) (int, error) {
	return 0, nil
}

func (s *InMemoryStore) MergeTopicsTx(_ context.Context, _ pgx.Tx, _ string, _ string, _ []string) (int, error) {
	return 0, nil
}

// ApplyTopicRestructure / ApplyTopicRestructureTx — InMemoryStore stubs.
// Same rationale as MergeTopics: the in-memory tree is too thin to
// model reparent semantics faithfully. Tests asserting real reparenting
// must run against Postgres.
func (s *InMemoryStore) ApplyTopicRestructure(_ context.Context, _ string, _ string, _ string) error {
	return nil
}

func (s *InMemoryStore) ApplyTopicRestructureTx(_ context.Context, _ pgx.Tx, _ string, _ string, _ string) error {
	return nil
}

// AcceptHubInviteByID / DeclineHubInviteByID — InMemoryStore stubs.
// The in-memory hub-invite store doesn't track the by-id flow; tests
// that exercise the notification-driven accept/decline path must run
// against Postgres.
func (s *InMemoryStore) AcceptHubInviteByID(_ context.Context, _ string, _ string, _ int) (*model.HubInvite, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *InMemoryStore) AcceptHubInviteByIDTx(_ context.Context, _ pgx.Tx, _ string, _ string, _ int) (*model.HubInvite, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *InMemoryStore) DeclineHubInviteByID(_ context.Context, _ string, _ string) error {
	return nil
}

func (s *InMemoryStore) DeclineHubInviteByIDTx(_ context.Context, _ pgx.Tx, _ string, _ string) error {
	return nil
}

func (s *InMemoryStore) ListArchiveCandidates(_ string, _ int, _ int) ([]model.Memory, error) {
	return []model.Memory{}, nil
}

func (s *InMemoryStore) ListSeedMemoryTemplates(_ context.Context) ([]model.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.Memory{}
	for _, m := range s.memories {
		if m.HubID == model.TutorialHubID &&
			m.SourceKind == model.MemorySourceKindOnboard &&
			m.State == "active" {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (s *InMemoryStore) ListSeedMemoryTemplatesAdmin(_ context.Context) ([]model.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.Memory{}
	for _, m := range s.memories {
		if m.HubID == model.TutorialHubID &&
			m.SourceKind == model.MemorySourceKindOnboard {
			out = append(out, *m)
		}
	}
	return out, nil
}

func (s *InMemoryStore) DeleteSeedCopiesForOwner(ownerID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var removed int64
	for id, m := range s.memories {
		if m.OwnerID == ownerID && m.SourceKind == model.MemorySourceKindOnboard {
			delete(s.memories, id)
			delete(s.chunks, id)
			delete(s.attachments, id)
			removed++
		}
	}
	return removed, nil
}

func (s *InMemoryStore) DeleteSeedMemoryTemplate(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.memories[id]
	if !ok {
		return false, nil
	}
	if m.OwnerID != model.SystemUserID ||
		m.HubID != model.TutorialHubID ||
		m.SourceKind != model.MemorySourceKindOnboard {
		return false, nil
	}
	delete(s.memories, id)
	delete(s.chunks, id)
	delete(s.attachments, id)
	return true, nil
}

func (s *InMemoryStore) DeleteAllUserData(_ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memories = make(map[string]*model.Memory)
	s.attachments = make(map[string][]model.MemoryAttachment)
	s.chunks = make(map[string][]model.Chunk)
	s.agentConfigs = make(map[string]*model.AgentConfig)
	s.configStates = make(map[string]*model.AgentConfigSyncState)
	s.tombstones = make(map[string]*model.AgentConfigTombstone)
	return nil
}

// --- Dream Engine Store Methods (in-memory stubs) ---

func (s *InMemoryStore) FindRelevantTopicIDs(_ string, _ []string, _ int) ([]string, error) {
	return nil, nil // no vector search in memory store
}

func (s *InMemoryStore) FindRelevantTopicIDsByHub(_ string, _ []string, _ int) ([]string, error) {
	return nil, nil
}

func (s *InMemoryStore) FindSimilarMemories(_ string, _ float64, _ int) ([]model.SimilarMemoryPair, error) {
	return nil, nil // no vector search in memory store
}

func (s *InMemoryStore) FindSimilarMemoriesByHub(_ string, _ float64, _ int) ([]model.SimilarMemoryPair, error) {
	return nil, nil
}

func (s *InMemoryStore) ArchiveMemory(id string, ownerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mem, ok := s.memories[id]
	if !ok || (ownerID != "local" && mem.OwnerID != ownerID) {
		return fmt.Errorf("memory not found: %s", id)
	}
	mem.State = "archived"
	return nil
}

func (s *InMemoryStore) ArchiveMemoryInHub(id string, hubID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mem, ok := s.memories[id]
	if !ok || (hubID != "" && mem.HubID != hubID) {
		return fmt.Errorf("memory not found: %s", id)
	}
	mem.State = "archived"
	return nil
}

func (s *InMemoryStore) ExecuteDreamMerge(keeper *model.Memory, absorbedID string, ownerID string, chunks []model.Chunk) error {
	if err := s.UpdateMemory(keeper); err != nil {
		return err
	}
	if err := s.DeleteChunksByMemory(keeper.ID, ownerID); err != nil {
		return err
	}
	if err := s.CreateChunks(chunks); err != nil {
		return err
	}
	s.mu.Lock()
	if absorbedAttachments, ok := s.attachments[absorbedID]; ok {
		existing := s.attachments[keeper.ID]
		seen := make(map[string]bool, len(existing))
		for _, attachment := range existing {
			key := attachment.SHA256
			if key == "" {
				key = attachment.StorageKey
			}
			seen[key] = true
		}
		for _, attachment := range absorbedAttachments {
			key := attachment.SHA256
			if key == "" {
				key = attachment.StorageKey
			}
			if seen[key] {
				continue
			}
			attachment.MemoryID = keeper.ID
			existing = append(existing, attachment)
			seen[key] = true
		}
		s.attachments[keeper.ID] = existing
		delete(s.attachments, absorbedID)
	}
	s.mu.Unlock()
	return s.ArchiveMemory(absorbedID, ownerID)
}

func (s *InMemoryStore) ExecuteDreamMergeInHub(keeper *model.Memory, absorbedID string, hubID string, chunks []model.Chunk) error {
	if err := s.UpdateMemory(keeper); err != nil {
		return err
	}
	s.mu.Lock()
	s.chunks[keeper.ID] = chunks
	if absorbedAttachments, ok := s.attachments[absorbedID]; ok {
		existing := s.attachments[keeper.ID]
		seen := make(map[string]bool, len(existing))
		for _, attachment := range existing {
			key := attachment.SHA256
			if key == "" {
				key = attachment.StorageKey
			}
			seen[key] = true
		}
		for _, attachment := range absorbedAttachments {
			key := attachment.SHA256
			if key == "" {
				key = attachment.StorageKey
			}
			if seen[key] {
				continue
			}
			attachment.MemoryID = keeper.ID
			existing = append(existing, attachment)
			seen[key] = true
		}
		s.attachments[keeper.ID] = existing
		delete(s.attachments, absorbedID)
	}
	s.mu.Unlock()
	return s.ArchiveMemoryInHub(absorbedID, hubID)
}

func (s *InMemoryStore) CreateDreamRun(_ *model.DreamRun) error { return nil }
func (s *InMemoryStore) UpdateDreamRun(_ *model.DreamRun) error { return nil }
func (s *InMemoryStore) ClaimStaleDreamRun(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return false, nil
}
func (s *InMemoryStore) HeartbeatDreamRun(_ context.Context, _ string) error { return nil }
func (s *InMemoryStore) CreateDreamAction(_ *model.DreamAction) error        { return nil }

func (s *InMemoryStore) GetLatestDreamRun(_ string) (*model.DreamRun, error) {
	return nil, fmt.Errorf("no dream runs found")
}

func (s *InMemoryStore) GetLatestDreamRunByHub(_ string) (*model.DreamRun, error) {
	return nil, fmt.Errorf("no dream runs found")
}

func (s *InMemoryStore) ListDreamRuns(_ string, _ int) ([]model.DreamRun, error) {
	return nil, nil
}

func (s *InMemoryStore) ListDreamRunsByHub(_ string, _ int) ([]model.DreamRun, error) {
	return nil, nil
}

func (s *InMemoryStore) ListDreamRunsForUser(_ context.Context, _ DreamRunListForUserOpts) ([]model.DreamRun, string, error) {
	return nil, "", nil
}

func (s *InMemoryStore) GetDreamRunByID(_ context.Context, _ string) (*model.DreamRun, error) {
	return nil, fmt.Errorf("not found")
}

func (s *InMemoryStore) ListNotificationsByDreamRunID(_ context.Context, _ string) ([]model.Notification, error) {
	return nil, nil
}

func (s *InMemoryStore) GetDreamActions(_ string) ([]model.DreamAction, error) {
	return nil, nil
}

// LoadDreamTriggerInputs returns one row per active memory in
// hubID. Contradiction and topic-sibling counts are returned as
// zero — the InMemoryStore doesn't track dream_actions or
// memory_topics in enough detail to compute them, and the only
// callers that need real values run against Postgres anyway. Tests
// that exercise the trigger pipeline against the in-memory store
// can rely on URL-drift and user-followup signals.
func (s *InMemoryStore) LoadDreamTriggerInputs(_ context.Context, hubID string, _ time.Duration) ([]DreamTriggerInputRow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []DreamTriggerInputRow
	for _, m := range s.memories {
		if m.HubID != hubID || m.State != "active" {
			continue
		}
		// InMemoryStore doesn't preserve column-level nullability,
		// so we approximate UserFollowupScanned by treating any
		// non-empty marker as proof of a scan. Empty marker maps
		// to !scanned — accurate for tests that exercise the
		// trigger path against in-memory fixtures (they always
		// either set a non-empty marker or never touch the field).
		// Postgres callers get the real three-state semantics via
		// the IS NOT NULL projection.
		out = append(out, DreamTriggerInputRow{
			MemoryID:            m.ID,
			ContentHash:         m.ContentHash,
			SourceFetchHash:     m.SourceFetchHash,
			UserFollowupScanned: m.UserFollowupMarker != "",
			UserFollowupMarker:  m.UserFollowupMarker,
		})
	}
	return out, nil
}

// LogTriggerDecisions is a no-op on the InMemoryStore. The
// soft-mode calibration log only matters against the production
// Postgres path; tests that need to assert decision shape inspect
// triggers.Decision directly.
func (s *InMemoryStore) LogTriggerDecisions(_ context.Context, _, _ string, _ []DreamTriggerDecisionRow) error {
	return nil
}

// --- Topics (stubs) ---

func (s *InMemoryStore) CreateTopic(topic *model.Topic) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *topic
	s.topics[topic.ID] = &cp
	return nil
}

func (s *InMemoryStore) GetTopic(id string, hubID string) (*model.Topic, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	topic, ok := s.topics[id]
	if !ok || (hubID != "" && topic.HubID != hubID) {
		return nil, fmt.Errorf("not found")
	}
	cp := *topic
	return &cp, nil
}

// GetTopicAccessible — in-memory equivalent of the scoped postgres
// lookup. Matches owner or hub membership from the viewer's
// VisibilityScope.
func (s *InMemoryStore) GetTopicAccessible(id string, scope VisibilityScope) (*model.Topic, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	topic, ok := s.topics[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	if topic.OwnerID == scope.OwnerID {
		cp := *topic
		return &cp, nil
	}
	for _, hid := range scope.HubIDs {
		if topic.HubID == hid {
			cp := *topic
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (s *InMemoryStore) ListTopics(hubID string) ([]model.Topic, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	topics := make([]model.Topic, 0)
	for _, topic := range s.topics {
		if hubID != "" && topic.HubID != hubID {
			continue
		}
		if topic.ArchivedAt != nil {
			continue
		}
		topics = append(topics, *topic)
	}
	sort.Slice(topics, func(i, j int) bool {
		if topics[i].Position != topics[j].Position {
			return topics[i].Position < topics[j].Position
		}
		return topics[i].CreatedAt.Before(topics[j].CreatedAt)
	})
	return topics, nil
}

// ListArchivedTopics mirrors the Postgres implementation: the hub's archived
// topics as a flat list, most recently archived first.
func (s *InMemoryStore) ListArchivedTopics(hubID string) ([]model.Topic, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	topics := make([]model.Topic, 0)
	for _, topic := range s.topics {
		if hubID != "" && topic.HubID != hubID {
			continue
		}
		if topic.ArchivedAt == nil {
			continue
		}
		topics = append(topics, *topic)
	}
	sort.Slice(topics, func(i, j int) bool {
		if !topics[i].ArchivedAt.Equal(*topics[j].ArchivedAt) {
			return topics[i].ArchivedAt.After(*topics[j].ArchivedAt)
		}
		return topics[i].CreatedAt.Before(topics[j].CreatedAt)
	})
	return topics, nil
}

// subtreeTopicIDs collects id and every descendant within the hub.
// Callers must hold the lock.
func (s *InMemoryStore) subtreeTopicIDs(id, hubID string) map[string]struct{} {
	ids := map[string]struct{}{id: {}}
	for changed := true; changed; {
		changed = false
		for _, topic := range s.topics {
			if hubID != "" && topic.HubID != hubID {
				continue
			}
			if _, seen := ids[topic.ID]; seen || topic.ParentID == nil {
				continue
			}
			if _, ok := ids[*topic.ParentID]; ok {
				ids[topic.ID] = struct{}{}
				changed = true
			}
		}
	}
	return ids
}

// ArchiveTopicSubtree mirrors the Postgres implementation: archives the
// topic and all descendants; already-archived rows keep their archived_at.
func (s *InMemoryStore) ArchiveTopicSubtree(id, hubID string, archivedAt time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, ok := s.topics[id]
	if !ok || (hubID != "" && root.HubID != hubID) {
		return 0, fmt.Errorf("not found")
	}
	count := 0
	for tid := range s.subtreeTopicIDs(id, hubID) {
		topic := s.topics[tid]
		if topic.ArchivedAt == nil {
			at := archivedAt
			topic.ArchivedAt = &at
			topic.UpdatedAt = archivedAt
			count++
		}
	}
	return count, nil
}

// RestoreTopicSubtree mirrors the Postgres implementation: clears
// archived_at on the topic and all descendants, re-planting the topic at
// root when its parent is still archived.
func (s *InMemoryStore) RestoreTopicSubtree(id, hubID string, restoredAt time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, ok := s.topics[id]
	if !ok || (hubID != "" && root.HubID != hubID) {
		return 0, fmt.Errorf("not found")
	}
	count := 0
	for tid := range s.subtreeTopicIDs(id, hubID) {
		topic := s.topics[tid]
		if topic.ArchivedAt != nil {
			topic.ArchivedAt = nil
			topic.UpdatedAt = restoredAt
			count++
		}
	}
	if root.ParentID != nil {
		if parent, ok := s.topics[*root.ParentID]; ok && parent.ArchivedAt != nil {
			maxPos := -1
			for _, topic := range s.topics {
				if topic.HubID == root.HubID && topic.ParentID == nil && topic.ArchivedAt == nil && topic.ID != root.ID && topic.Position > maxPos {
					maxPos = topic.Position
				}
			}
			root.ParentID = nil
			root.Position = maxPos + 1
			root.UpdatedAt = restoredAt
		}
	}
	return count, nil
}

// SearchAccessibleTopics mirrors the Postgres implementation: returns topics
// whose hub is in hubIDs and whose name or description contains every
// whitespace-separated token in query (case-insensitive AND).
func (s *InMemoryStore) SearchAccessibleTopics(_ context.Context, query string, hubIDs []string, limit int) ([]model.Topic, error) {
	if len(hubIDs) == 0 || strings.TrimSpace(query) == "" || limit <= 0 {
		return []model.Topic{}, nil
	}
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 {
		return []model.Topic{}, nil
	}
	allowed := make(map[string]struct{}, len(hubIDs))
	for _, id := range hubIDs {
		allowed[id] = struct{}{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Topic, 0)
	for _, topic := range s.topics {
		if _, ok := allowed[topic.HubID]; !ok {
			continue
		}
		if topic.ArchivedAt != nil {
			continue
		}
		haystack := strings.ToLower(topic.Name + " " + topic.Description)
		matched := true
		for _, token := range tokens {
			if !strings.Contains(haystack, token) {
				matched = false
				break
			}
		}
		if matched {
			out = append(out, *topic)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *InMemoryStore) UpdateTopic(topic *model.Topic) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *topic
	s.topics[topic.ID] = &cp
	return nil
}

func (s *InMemoryStore) DeleteTopic(id string, hubID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	topic, ok := s.topics[id]
	if !ok || (hubID != "" && topic.HubID != hubID) {
		return fmt.Errorf("not found")
	}
	delete(s.topics, id)
	for memoryID, mt := range s.memoryTopics {
		if mt.TopicID == id {
			delete(s.memoryTopics, memoryID)
		}
	}
	return nil
}

func (s *InMemoryStore) AssignMemoryToTopic(memoryID, topicID, hubID string, confidence float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mem, ok := s.memories[memoryID]
	if !ok {
		return fmt.Errorf("memory not found")
	}
	if mem.HubID != hubID {
		return fmt.Errorf("memory does not belong to this hub")
	}

	topic, ok := s.topics[topicID]
	if !ok {
		return fmt.Errorf("topic not found")
	}
	if topic.HubID != hubID {
		return fmt.Errorf("topic does not belong to this hub")
	}

	existing, exists := s.memoryTopics[memoryID]
	switch {
	case !exists:
		s.memoryTopics[memoryID] = model.MemoryTopic{
			MemoryID:   memoryID,
			TopicID:    topicID,
			Confidence: confidence,
		}
	case existing.TopicID == topicID:
		if confidence > existing.Confidence {
			existing.Confidence = confidence
			s.memoryTopics[memoryID] = existing
		}
	case confidence > existing.Confidence:
		existing.TopicID = topicID
		existing.Confidence = confidence
		s.memoryTopics[memoryID] = existing
	}

	return nil
}

func (s *InMemoryStore) UnassignMemoryFromTopic(memoryID, topicID, hubID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	topic, ok := s.topics[topicID]
	if !ok {
		return fmt.Errorf("topic not found")
	}
	if topic.HubID != hubID {
		return fmt.Errorf("topic does not belong to this hub")
	}
	if mt, ok := s.memoryTopics[memoryID]; ok && mt.TopicID == topicID {
		delete(s.memoryTopics, memoryID)
	}
	return nil
}
func (s *InMemoryStore) CountMemoriesByTopic(_ VisibilityScope, hubID string) (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := make(map[string]int)
	for memoryID, mt := range s.memoryTopics {
		mem, ok := s.memories[memoryID]
		if !ok || mem.State == "archived" || (hubID != "" && mem.HubID != hubID) {
			continue
		}
		counts[mt.TopicID]++
	}
	return counts, nil
}
func (s *InMemoryStore) CountUnassignedMemories(_ VisibilityScope, hubID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, mem := range s.memories {
		if mem.State == "archived" || (hubID != "" && mem.HubID != hubID) {
			continue
		}
		if _, ok := s.memoryTopics[mem.ID]; !ok {
			count++
		}
	}
	return count, nil
}
func (s *InMemoryStore) CountTopicMemories(scope VisibilityScope, hubID string) (map[string]int, int, error) {
	counts, _ := s.CountMemoriesByTopic(scope, hubID)
	unassigned, _ := s.CountUnassignedMemories(scope, hubID)
	return counts, unassigned, nil
}
func (s *InMemoryStore) ReorderTopics(_ string, _ []model.ReorderOperation) error { return nil }
func (s *InMemoryStore) GetTopicDepth(topicID, hubID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	depth := 0
	currentID := topicID
	for depth < 5 {
		t, ok := s.topics[currentID]
		if !ok || t.HubID != hubID || t.ParentID == nil {
			break
		}
		currentID = *t.ParentID
		depth++
	}
	return depth, nil
}

// IsTopicDescendant reports whether candidateID equals ancestorID or is a
// transitive descendant of ancestorID inside hubID. Used by the Update
// handler to prevent reparent cycles.
func (s *InMemoryStore) IsTopicDescendant(hubID, ancestorID, candidateID string) (bool, error) {
	if ancestorID == candidateID {
		return true, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	// BFS down from ancestor, scoped to hubID.
	visited := map[string]struct{}{ancestorID: {}}
	frontier := []string{ancestorID}
	for len(frontier) > 0 {
		next := make([]string, 0)
		for _, parentID := range frontier {
			for _, t := range s.topics {
				if t.HubID != hubID || t.ParentID == nil {
					continue
				}
				if *t.ParentID != parentID {
					continue
				}
				if _, seen := visited[t.ID]; seen {
					continue
				}
				if t.ID == candidateID {
					return true, nil
				}
				visited[t.ID] = struct{}{}
				next = append(next, t.ID)
			}
		}
		frontier = next
	}
	return false, nil
}

// GetSubtreeMaxDepth returns the depth of the deepest descendant relative to
// topicID (0 means topicID has no children). Capped at 5.
func (s *InMemoryStore) GetSubtreeMaxDepth(hubID, topicID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	maxDepth := 0
	type entry struct {
		id    string
		depth int
	}
	frontier := []entry{{id: topicID, depth: 0}}
	for len(frontier) > 0 {
		next := make([]entry, 0)
		for _, parent := range frontier {
			if parent.depth > maxDepth {
				maxDepth = parent.depth
			}
			if parent.depth >= 5 {
				continue
			}
			for _, t := range s.topics {
				if t.HubID != hubID || t.ParentID == nil {
					continue
				}
				if *t.ParentID == parent.id {
					next = append(next, entry{id: t.ID, depth: parent.depth + 1})
				}
			}
		}
		frontier = next
	}
	return maxDepth, nil
}
func (s *InMemoryStore) GetTopicActivitySummary(scope VisibilityScope, topicID, hubID string, createdAfter time.Time, previewLimit int) (*model.TopicActivitySummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if previewLimit <= 0 {
		previewLimit = 3
	}

	descendants := map[string]struct{}{topicID: {}}
	for changed := true; changed; {
		changed = false
		for _, topic := range s.topics {
			if topic.HubID != hubID || topic.ParentID == nil {
				continue
			}
			if _, ok := descendants[*topic.ParentID]; ok {
				if _, seen := descendants[topic.ID]; !seen {
					descendants[topic.ID] = struct{}{}
					changed = true
				}
			}
		}
	}

	type contributorStats struct {
		count      int
		lastActive time.Time
	}
	statsByOwner := make(map[string]*contributorStats)
	memoryCount := 0
	for memoryID, assignment := range s.memoryTopics {
		if _, ok := descendants[assignment.TopicID]; !ok {
			continue
		}
		memory, ok := s.memories[memoryID]
		if !ok || memory.State == "archived" || memory.HubID != hubID || memory.CreatedAt.Before(createdAfter) {
			continue
		}
		if memory.OwnerID != scope.OwnerID {
			visible := false
			for _, visibleHubID := range scope.HubIDs {
				if memory.HubID == visibleHubID {
					visible = true
					break
				}
			}
			if !visible {
				continue
			}
		}
		memoryCount++
		stats := statsByOwner[memory.OwnerID]
		if stats == nil {
			stats = &contributorStats{}
			statsByOwner[memory.OwnerID] = stats
		}
		stats.count++
		if memory.CreatedAt.After(stats.lastActive) {
			stats.lastActive = memory.CreatedAt
		}
	}

	if memoryCount == 0 {
		return nil, nil
	}

	type contributorRow struct {
		id         string
		name       string
		avatarURL  string
		count      int
		lastActive time.Time
	}
	contributors := make([]contributorRow, 0, len(statsByOwner))
	for ownerID, stats := range statsByOwner {
		contributors = append(contributors, contributorRow{
			id:         ownerID,
			name:       ownerID,
			count:      stats.count,
			lastActive: stats.lastActive,
		})
	}
	sort.Slice(contributors, func(i, j int) bool {
		if contributors[i].count != contributors[j].count {
			return contributors[i].count > contributors[j].count
		}
		return contributors[i].lastActive.After(contributors[j].lastActive)
	})

	preview := make([]model.TopicActivityContributor, 0, min(previewLimit, len(contributors)))
	for _, contributor := range contributors[:min(previewLimit, len(contributors))] {
		preview = append(preview, model.TopicActivityContributor{
			UserID:        contributor.id,
			UserName:      contributor.name,
			UserAvatarURL: contributor.avatarURL,
		})
	}

	return &model.TopicActivitySummary{
		MemoryCount:         memoryCount,
		ContributorCount:    len(statsByOwner),
		ContributorsPreview: preview,
	}, nil
}
func (s *InMemoryStore) ListUnassignedMemories(_ VisibilityScope, _ int) ([]model.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	memories := make([]model.Memory, 0)
	for _, mem := range s.memories {
		if mem.State == "archived" {
			continue
		}
		if _, ok := s.memoryTopics[mem.ID]; ok {
			continue
		}
		memories = append(memories, *mem)
	}
	return memories, nil
}

func (s *InMemoryStore) ListUnassignedMemoriesByHub(hubID string, limit int) ([]model.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	memories := make([]model.Memory, 0)
	for _, mem := range s.memories {
		if mem.State == "archived" || mem.HubID != hubID {
			continue
		}
		if _, ok := s.memoryTopics[mem.ID]; ok {
			continue
		}
		memories = append(memories, *mem)
	}
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].CreatedAt.After(memories[j].CreatedAt)
	})
	if limit > 0 && len(memories) > limit {
		memories = memories[:limit]
	}
	return memories, nil
}
func (s *InMemoryStore) GetMemoryTopicNameMap(_ VisibilityScope) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]string)
	for memoryID, mt := range s.memoryTopics {
		if topic, ok := s.topics[mt.TopicID]; ok && topic.ArchivedAt == nil {
			result[memoryID] = topic.Name
		}
	}
	return result, nil
}

func (s *InMemoryStore) GetMemoryTopicNameMapForMemories(_ VisibilityScope, memoryIDs []string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]string)
	for _, memoryID := range memoryIDs {
		mt, ok := s.memoryTopics[memoryID]
		if !ok {
			continue
		}
		if topic, ok := s.topics[mt.TopicID]; ok && topic.ArchivedAt == nil {
			result[memoryID] = topic.Name
		}
	}
	return result, nil
}

func (s *InMemoryStore) GetMemoryTopicIDMap(_ VisibilityScope) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]string)
	for memoryID, mt := range s.memoryTopics {
		result[memoryID] = mt.TopicID
	}
	return result, nil
}

func (s *InMemoryStore) GetMemoryTopicIDMapForMemories(_ VisibilityScope, memoryIDs []string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]string)
	for _, memoryID := range memoryIDs {
		mt, ok := s.memoryTopics[memoryID]
		if !ok {
			continue
		}
		result[memoryID] = mt.TopicID
	}
	return result, nil
}
func (s *InMemoryStore) GetTopicKinds(_ string) (map[string][]string, error) {
	return map[string][]string{}, nil
}
func (s *InMemoryStore) ListMemoriesByTopic(_ VisibilityScope, topicID string, limit int, _ string) ([]model.Memory, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	memories := make([]model.Memory, 0)
	for memoryID, mt := range s.memoryTopics {
		if mt.TopicID != topicID {
			continue
		}
		mem, ok := s.memories[memoryID]
		if !ok || mem.State == "archived" {
			continue
		}
		memories = append(memories, *mem)
	}
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].CreatedAt.After(memories[j].CreatedAt)
	})
	if limit > 0 && len(memories) > limit {
		memories = memories[:limit]
	}
	return memories, "", nil
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	words := strings.Fields(s)
	var tokens []string
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'()[]{}/-")
		if len(w) > 1 {
			tokens = append(tokens, w)
		}
	}
	return tokens
}

func chunkSearchText(chunk model.Chunk) string {
	return strings.Join([]string{
		chunk.HeadingChain,
		chunk.Hint,
		chunk.TagsText,
		chunk.MetadataText,
		chunk.Content,
	}, " ")
}

func termMatchScore(queryTerms []string, content string) float64 {
	lower := strings.ToLower(content)
	var score float64
	for _, term := range queryTerms {
		count := strings.Count(lower, term)
		if count > 0 {
			score += 1.0 + math.Log(float64(count))
		}
	}
	// Normalize by content length to avoid bias toward long chunks
	contentLen := float64(len(strings.Fields(lower)))
	if contentLen > 0 {
		score = score / math.Sqrt(contentLen)
	}
	return score
}

// --- Agent Configs (in-memory) ---

func (s *InMemoryStore) UpsertAgentConfig(config *model.AgentConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentConfigs == nil {
		s.agentConfigs = make(map[string]*model.AgentConfig)
	}
	// Check for existing by unique key
	for _, c := range s.agentConfigs {
		if c.OwnerID == config.OwnerID && c.Agent == config.Agent &&
			c.FilePath == config.FilePath && c.Scope == config.Scope {
			c.Content = config.Content
			c.ContentHash = config.ContentHash
			c.Version++
			c.UpdatedAt = config.UpdatedAt
			return nil
		}
	}
	s.agentConfigs[config.ID] = config
	return nil
}

func (s *InMemoryStore) GetAgentConfig(id string, ownerID string) (*model.AgentConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.agentConfigs[id]
	if !ok || (ownerID != "local" && c.OwnerID != ownerID) {
		return nil, fmt.Errorf("agent config not found: %s", id)
	}
	return c, nil
}

func (s *InMemoryStore) GetAgentConfigByPath(agent, filePath, scope, ownerID string) (*model.AgentConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.agentConfigs {
		if c.Agent == agent && c.FilePath == filePath && c.Scope == scope &&
			(ownerID == "local" || c.OwnerID == ownerID) {
			return c, nil
		}
	}
	return nil, fmt.Errorf("agent config not found: %s/%s/%s", agent, filePath, scope)
}

func (s *InMemoryStore) ListAgentConfigs(ownerID string) ([]model.AgentConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var configs []model.AgentConfig
	for _, c := range s.agentConfigs {
		if ownerID == "local" || c.OwnerID == ownerID {
			configs = append(configs, *c)
		}
	}
	sort.Slice(configs, func(i, j int) bool {
		if configs[i].Agent != configs[j].Agent {
			return configs[i].Agent < configs[j].Agent
		}
		return configs[i].FilePath < configs[j].FilePath
	})
	return configs, nil
}

func (s *InMemoryStore) DeleteAgentConfig(id string, ownerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.agentConfigs[id]
	if !ok || (ownerID != "local" && c.OwnerID != ownerID) {
		return fmt.Errorf("agent config not found: %s", id)
	}
	delete(s.agentConfigs, id)
	return nil
}

func (s *InMemoryStore) ListAgentConfigSyncStates(ownerID string, deviceID string) ([]model.AgentConfigSyncState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var states []model.AgentConfigSyncState
	for _, state := range s.configStates {
		if state.OwnerID == ownerID && state.DeviceID == deviceID {
			states = append(states, *state)
		}
	}
	sort.Slice(states, func(i, j int) bool {
		if states[i].Agent != states[j].Agent {
			return states[i].Agent < states[j].Agent
		}
		if states[i].Scope != states[j].Scope {
			return states[i].Scope < states[j].Scope
		}
		return states[i].FilePath < states[j].FilePath
	})
	return states, nil
}

func (s *InMemoryStore) UpsertAgentConfigSyncState(state *model.AgentConfigSyncState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s|%s|%s|%s|%s", state.OwnerID, state.DeviceID, state.Agent, state.Scope, state.FilePath)
	copyState := *state
	s.configStates[key] = &copyState
	return nil
}

func (s *InMemoryStore) ListAgentConfigTombstones(ownerID string) ([]model.AgentConfigTombstone, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var tombstones []model.AgentConfigTombstone
	for _, tombstone := range s.tombstones {
		if tombstone.OwnerID == ownerID {
			tombstones = append(tombstones, *tombstone)
		}
	}
	sort.Slice(tombstones, func(i, j int) bool {
		if tombstones[i].Agent != tombstones[j].Agent {
			return tombstones[i].Agent < tombstones[j].Agent
		}
		if tombstones[i].Scope != tombstones[j].Scope {
			return tombstones[i].Scope < tombstones[j].Scope
		}
		return tombstones[i].FilePath < tombstones[j].FilePath
	})
	return tombstones, nil
}

func (s *InMemoryStore) GetAgentConfigTombstone(agent, filePath, scope, ownerID string) (*model.AgentConfigTombstone, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := fmt.Sprintf("%s|%s|%s|%s", ownerID, agent, scope, filePath)
	tombstone, ok := s.tombstones[key]
	if !ok {
		return nil, fmt.Errorf("agent config tombstone not found: %s/%s/%s", agent, filePath, scope)
	}
	copyTombstone := *tombstone
	return &copyTombstone, nil
}

func (s *InMemoryStore) CreateAgentConfigTombstone(tombstone *model.AgentConfigTombstone) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s|%s|%s|%s", tombstone.OwnerID, tombstone.Agent, tombstone.Scope, tombstone.FilePath)
	copyTombstone := *tombstone
	s.tombstones[key] = &copyTombstone
	return nil
}

func (s *InMemoryStore) DeleteAgentConfigTombstone(agent, filePath, scope, ownerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s|%s|%s|%s", ownerID, agent, scope, filePath)
	delete(s.tombstones, key)
	return nil
}

func (s *InMemoryStore) PurgeExpiredAgentConfigTombstoneContent(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, tombstone := range s.tombstones {
		if tombstone.ContentExpiresAt != nil && !tombstone.ContentExpiresAt.After(now) {
			tombstone.DeletedContent = ""
			tombstone.DeletedContentHash = ""
			tombstone.ContentExpiresAt = nil
		}
	}
	return nil
}

func (s *InMemoryStore) CountExtractedMemories(ownerID string) (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := make(map[string]int)
	for _, m := range s.memories {
		if m.OwnerID == ownerID && strings.HasPrefix(m.SourcePath, "config:") && m.State == "active" {
			configID := strings.TrimPrefix(m.SourcePath, "config:")
			counts[configID]++
		}
	}
	return counts, nil
}

// ── Connected Agents (stubs for InMemoryStore) ──

func (s *InMemoryStore) UpsertConnectedAgent(_ *model.ConnectedAgent) error { return nil }
func (s *InMemoryStore) GetConnectedAgent(_, _ string) (*model.ConnectedAgent, error) {
	return nil, fmt.Errorf("not found")
}
func (s *InMemoryStore) ListConnectedAgentsWithStats(_ string) ([]model.ConnectedAgentWithStats, error) {
	return nil, nil
}
func (s *InMemoryStore) UpdateConnectedAgent(_ *model.ConnectedAgent) error { return nil }
func (s *InMemoryStore) DeleteConnectedAgent(_, _ string) error             { return nil }
func (s *InMemoryStore) CountAgentApiKeys(_, _ string) (int, error)         { return 0, nil }
func (s *InMemoryStore) CountAgentConfigs(_, _ string) (int, error)         { return 0, nil }

// HealAPIKeyAgent is a no-op in the in-memory store. We return the
// caller's claimed slug so tests that exercise the push path stay on
// the happy path without needing to thread a real api_keys row.
func (s *InMemoryStore) HealAPIKeyAgent(_ context.Context, _, _, claimedSlug string) (string, error) {
	return claimedSlug, nil
}

// ── Plans (stubs for InMemoryStore) ──

func (s *InMemoryStore) ListPlans(_ context.Context) ([]model.Plan, error) { return nil, nil }
func (s *InMemoryStore) ListPlansByScope(_ context.Context, _ model.PlanScope) ([]model.Plan, error) {
	return nil, nil
}
func (s *InMemoryStore) CountOwnedFreeTeamHubs(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (s *InMemoryStore) GetPlan(_ context.Context, _ string) (*model.Plan, error) {
	return nil, fmt.Errorf("not found")
}
func (s *InMemoryStore) UpdatePlan(_ context.Context, _ *model.Plan) error { return nil }
func (s *InMemoryStore) GetPlanOverride(_ context.Context, _ string) (*model.PlanOverride, error) {
	return nil, fmt.Errorf("not found")
}
func (s *InMemoryStore) UpsertPlanOverride(_ context.Context, _ *model.PlanOverride) error {
	return nil
}
func (s *InMemoryStore) DeletePlanOverride(_ context.Context, _ string) error { return nil }
func (s *InMemoryStore) ListUsageUserIDs(_ context.Context, _ time.Time) ([]string, error) {
	return nil, nil
}
func (s *InMemoryStore) UpsertBillingSubscription(_ context.Context, _ *model.BillingSubscription) error {
	return nil
}
func (s *InMemoryStore) InsertUsageEvent(_ context.Context, _ *model.UsageEvent) error {
	return nil
}
func (s *InMemoryStore) SetUsageCounters(_ context.Context, _ string, _, _ time.Time, _, _, _ int) error {
	return nil
}
func (s *InMemoryStore) ResetUsageCounters(_ context.Context, _ string, _, _ time.Time) error {
	return nil
}
func (s *InMemoryStore) ListUsers(_ context.Context, _ model.AdminUserListOpts) ([]model.User, string, int, error) {
	return nil, "", 0, nil
}
func (s *InMemoryStore) UpdateUserPlan(_ context.Context, _, _ string) error { return nil }
func (s *InMemoryStore) CountMemoriesByOwner(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (s *InMemoryStore) CountMemoriesByOwnerExcludingSeeds(_ context.Context, ownerID string) (int, error) {
	// Mirror PostgresStore.CountMemoriesByOwnerExcludingSeeds:
	//   - owner_id match
	//   - state != 'archived' (archived memories don't count toward
	//     quota / activation triggers, just like the Postgres path)
	//   - source_kind != 'onboarding-seed' (plan 23 seeds excluded
	//     so a new user isn't immediately at 6/100 / auto-firing
	//     first_memory)
	// Keeping the fake aligned with prod prevents tests passing
	// against semantics production doesn't share.
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, m := range s.memories {
		if m.OwnerID != ownerID {
			continue
		}
		if m.State == "archived" {
			continue
		}
		if m.SourceKind == "onboarding-seed" {
			continue
		}
		n++
	}
	return n, nil
}
func (s *InMemoryStore) CountMemoriesByHub(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (s *InMemoryStore) SumStorageBytesByOwner(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (s *InMemoryStore) GetSystemStats(_ context.Context) (*model.SystemStats, error) {
	return &model.SystemStats{UsersByPlan: map[string]int{}}, nil
}

// --- Notifications (in-memory stubs) ---
//
// Phase 3a of the inbox notification framework. InMemoryStore is used
// by unit tests that never exercise the notifications surface; all
// stubs return zero-value success so the Store interface is satisfied
// without pulling the full audience/visibility model into test infra.
// Tests that need real notification behavior must run against Postgres.

func (s *InMemoryStore) CreateNotification(_ *model.Notification) error { return nil }
func (s *InMemoryStore) CreateNotificationIfAbsent(_ *model.Notification) (bool, error) {
	return true, nil
}

func (s *InMemoryStore) CreateNotificationTx(_ context.Context, _ pgx.Tx, _ *model.Notification) error {
	return nil
}

func (s *InMemoryStore) ListNotificationsForUser(_ context.Context, _ NotificationListOpts) ([]model.Notification, string, error) {
	return nil, "", nil
}

func (s *InMemoryStore) GetNotification(_ context.Context, _ string, _ string, _ []string) (*model.Notification, error) {
	return nil, ErrNotificationNotFound
}

func (s *InMemoryStore) GetNotificationBySourceKey(_ context.Context, _, _ string) (*model.Notification, error) {
	return nil, ErrNotificationNotFound
}

func (s *InMemoryStore) GetNotificationSummary(_ context.Context, _ NotificationSummaryOpts) (*model.NotificationSummary, error) {
	return &model.NotificationSummary{ByKind: map[string]model.NotificationKindCount{}}, nil
}

func (s *InMemoryStore) MarkNotificationSeen(_ context.Context, _ string, _ string, _ []string) error {
	return nil
}

func (s *InMemoryStore) DismissNotification(_ context.Context, _ string, _ string, _ []string) error {
	return nil
}

func (s *InMemoryStore) ResolveNotification(_ context.Context, _ string, _ string, _ []string, _ model.NotificationResolution) error {
	return nil
}

func (s *InMemoryStore) ResolveNotificationsBySource(_ context.Context, _, _ string, _ model.NotificationResolution) ([]model.Notification, error) {
	return nil, nil
}

func (s *InMemoryStore) ExpireNotifications(_ context.Context, _ time.Time) ([]model.Notification, error) {
	return nil, nil
}

func (s *InMemoryStore) BulkMarkNotificationsSeen(_ context.Context, _ BulkNotificationMutationOpts) ([]model.Notification, error) {
	return nil, nil
}

func (s *InMemoryStore) BulkDismissNotifications(_ context.Context, _ BulkNotificationMutationOpts) ([]model.Notification, error) {
	return nil, nil
}

func (s *InMemoryStore) QueryAdminNotificationBatches(_ context.Context, _ AdminNotifBatchOpts) ([]AdminNotifBatchRow, error) {
	return nil, nil
}

// --- Super-notif sub-item mutation stubs (plan 18) ---
//
// InMemoryStore is exercised by smoke tests that don't yet model
// notification payloads. The stubs preserve the interface contract:
// return zero-value results so callers can compile/run, but they don't
// emulate state. Real coverage of /view + /complete lives in the
// Postgres-backed test in postgres_notifications_items_test.go.

func (s *InMemoryStore) ViewNotificationItem(_ context.Context, _, _ string, _ []string, _ string) (*ItemMutationResult, error) {
	return nil, ErrNotificationNotFound
}
func (s *InMemoryStore) CompleteNotificationItem(_ context.Context, _, _ string, _ []string, _ string) (*ItemMutationResult, error) {
	return nil, ErrNotificationNotFound
}
func (s *InMemoryStore) TryAutoResolveChecklist(_ context.Context, _, _ string, _ []string) (bool, *model.Notification, error) {
	return false, nil, ErrNotificationNotFound
}
func (s *InMemoryStore) UpdateChecklistItemProgress(_ context.Context, _, _ string, _ []string, _ string, _, _ int) (*ItemMutationResult, error) {
	return nil, ErrNotificationNotFound
}
func (s *InMemoryStore) LatestChecklistByRecipient(_ context.Context, _, _ string) (*model.Notification, error) {
	return nil, ErrNotificationNotFound
}
func (s *InMemoryStore) CountChecklistsCreatedSince(_ context.Context, _, _ string, _ time.Time) (int, error) {
	return 0, nil
}
func (s *InMemoryStore) QuerySuperNotifParentFunnel(_ context.Context, _, _ string, _ time.Time) ([]SuperNotifFunnelRow, error) {
	return nil, nil
}
func (s *InMemoryStore) QuerySuperNotifItemFunnel(_ context.Context, _, _ string, _ time.Time) ([]SuperNotifItemFunnelRow, error) {
	return nil, nil
}
func (s *InMemoryStore) LatestPendingChecklistByRecipient(_ context.Context, _, _ string) (*model.Notification, error) {
	return nil, ErrNotificationNotFound
}

// ── Hub Subscriptions (stubs) ──

func (s *InMemoryStore) ListTeamHubs(_ context.Context, _ model.AdminHubListOpts) ([]model.Hub, string, int, error) {
	return nil, "", 0, nil
}
func (s *InMemoryStore) CreateHubSubscription(_ context.Context, _ *model.HubSubscription) error {
	return nil
}
func (s *InMemoryStore) GetActiveHubSubscription(_ context.Context, _ string) (*model.HubSubscription, error) {
	return nil, nil // no subscription
}
func (s *InMemoryStore) GetHubSubscriptionAnyStatus(_ context.Context, _ string) (*model.HubSubscription, error) {
	return nil, nil
}
func (s *InMemoryStore) UpdateHubSubscription(_ context.Context, _ *model.HubSubscription) error {
	return nil
}
func (s *InMemoryStore) SetHubOverLimit(_ context.Context, _ string, _ time.Time) (bool, error) {
	return false, nil
}
func (s *InMemoryStore) ClearHubOverLimit(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *InMemoryStore) ListFrozenHubIDs(_ context.Context, _ []string, _ time.Time) ([]string, error) {
	return nil, nil
}
func (s *InMemoryStore) ListHubsPastGrace(_ context.Context, _ time.Time) ([]string, error) {
	return nil, nil
}
func (s *InMemoryStore) ListHubSubscriptionsByBillingUser(_ context.Context, _ string) ([]model.HubSubscription, error) {
	return nil, nil
}
func (s *InMemoryStore) GetEffectivePlan(_ context.Context, _ string) (*model.EffectivePlan, error) {
	return nil, fmt.Errorf("not found")
}
func (s *InMemoryStore) SetEffectivePlan(_ context.Context, _, _, _ string) error {
	return nil
}
func (s *InMemoryStore) GetHubPlanID(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("not found")
}
func (s *InMemoryStore) GetUserHubPlans(_ context.Context, _ string) ([]model.HubPlanRef, error) {
	return nil, nil
}

// ── Communications (stubs — require Postgres) ──
//
// The admin-communications feature (campaigns, audiences, audit) is
// intentionally Postgres-only. In-memory mode exists for unit tests and
// fallback dev, where cross-linking persistence isn't available.
// Returning a descriptive error lets handlers surface a clean 503 when
// DATABASE_URL is not configured.

var errCommunicationsRequiresPostgres = fmt.Errorf("communications features require DATABASE_URL")

func (s *InMemoryStore) CreateAudience(_ context.Context, _ *model.Audience) error {
	return errCommunicationsRequiresPostgres
}
func (s *InMemoryStore) GetAudience(_ context.Context, _ string) (*model.Audience, error) {
	return nil, ErrAudienceNotFound
}
func (s *InMemoryStore) ListAudiences(_ context.Context) ([]model.Audience, error) {
	return nil, nil
}
func (s *InMemoryStore) UpdateAudience(_ context.Context, _ *model.Audience) error {
	return errCommunicationsRequiresPostgres
}
func (s *InMemoryStore) DeleteAudience(_ context.Context, _ string) error {
	return errCommunicationsRequiresPostgres
}

func (s *InMemoryStore) CreateCampaign(_ context.Context, _ *model.Campaign) error {
	return errCommunicationsRequiresPostgres
}
func (s *InMemoryStore) GetCampaign(_ context.Context, _ string) (*model.Campaign, error) {
	return nil, ErrCampaignNotFound
}
func (s *InMemoryStore) ListCampaigns(_ context.Context, _ model.CampaignListOpts) ([]model.Campaign, string, error) {
	return nil, "", nil
}
func (s *InMemoryStore) UpdateCampaignDraft(_ context.Context, _ string, _ CampaignDraftPatch) error {
	return errCommunicationsRequiresPostgres
}
func (s *InMemoryStore) DeleteCampaign(_ context.Context, _ string, _ string) error {
	return errCommunicationsRequiresPostgres
}
func (s *InMemoryStore) ScheduleCampaign(_ context.Context, _ string, _ time.Time, _ string) error {
	return errCommunicationsRequiresPostgres
}
func (s *InMemoryStore) CancelCampaign(_ context.Context, _ string, _ string) error {
	return errCommunicationsRequiresPostgres
}
func (s *InMemoryStore) ClaimCampaignForSend(_ context.Context, _, _ string, _ time.Time, _ string) error {
	return errCommunicationsRequiresPostgres
}
func (s *InMemoryStore) RevertCampaignToDraft(_ context.Context, _, _ string) error {
	return errCommunicationsRequiresPostgres
}
func (s *InMemoryStore) FinalizeCampaignSend(_ context.Context, _ string, _, _ int, _ time.Time, _ string) error {
	return errCommunicationsRequiresPostgres
}

func (s *InMemoryStore) AppendCommsAudit(_ context.Context, _ *model.CommsAuditEntry) error {
	return nil
}
func (s *InMemoryStore) ListCommsAudit(_ context.Context, _, _ string) ([]model.CommsAuditEntry, error) {
	return nil, nil
}

// --- Chat (Phase 3) — InMemory stubs ---
//
// Chat tests use the real Postgres store via testdb; the InMemory
// store doesn't model session locking, replay buffer ordering,
// or transcript transactionality with enough fidelity for the
// chat handler's invariants. These stubs return errors so a
// test that accidentally wires InMemory at the chat path fails
// loudly with a clear message instead of silently passing on a
// half-implemented mock.
//
// errChatRequiresPostgres is the same shape errCommunicationsRequiresPostgres
// uses for the campaigns surface — keep it consistent with that
// pattern.

var errChatRequiresPostgres = errors.New("chat: in-memory store unsupported; tests must use Postgres via testdb")

func (s *InMemoryStore) CreateChatSession(_ context.Context, _ *model.ChatSession) error {
	return errChatRequiresPostgres
}
func (s *InMemoryStore) GetChatSession(_ context.Context, _, _ string) (*model.ChatSession, error) {
	return nil, errChatRequiresPostgres
}
func (s *InMemoryStore) ListChatSessionsByOwner(_ context.Context, _ ChatSessionListOpts) ([]model.ChatSession, string, error) {
	return nil, "", errChatRequiresPostgres
}
func (s *InMemoryStore) UpdateChatSessionMeta(_ context.Context, _, _ string, _ ChatSessionMetaPatch) error {
	return errChatRequiresPostgres
}
func (s *InMemoryStore) SoftDeleteChatSession(_ context.Context, _, _ string) error {
	return errChatRequiresPostgres
}
func (s *InMemoryStore) AppendChatMessage(_ context.Context, _ string, _ *model.ChatMessage) error {
	return errChatRequiresPostgres
}
func (s *InMemoryStore) BeginChatMessageTurn(_ context.Context, _ BeginChatMessageTurnInput) (BeginChatMessageTurnResult, error) {
	return BeginChatMessageTurnResult{}, errChatRequiresPostgres
}
func (s *InMemoryStore) GetChatMessage(_ context.Context, _, _ string) (*model.ChatMessage, error) {
	return nil, errChatRequiresPostgres
}
func (s *InMemoryStore) GetChatMessageByIdempotencyKey(_ context.Context, _, _ string) (*model.ChatMessage, error) {
	return nil, errChatRequiresPostgres
}
func (s *InMemoryStore) ListChatMessagesBySession(_ context.Context, _ ChatMessageListOpts) ([]model.ChatMessage, string, error) {
	return nil, "", errChatRequiresPostgres
}
func (s *InMemoryStore) UpdateChatMessageStatus(_ context.Context, _, _, _ string) error {
	return errChatRequiresPostgres
}
func (s *InMemoryStore) FinalizeChatMessage(_ context.Context, _, _ string, _ ChatMessageFinalize) error {
	return errChatRequiresPostgres
}
func (s *InMemoryStore) CancelChatMessage(_ context.Context, _, _ string) (ChatMessageCancelOutcome, error) {
	return ChatMessageCancelOutcome{}, errChatRequiresPostgres
}
func (s *InMemoryStore) BeginChatRegenerate(_ context.Context, _ BeginChatRegenerateInput) (BeginChatRegenerateResult, error) {
	return BeginChatRegenerateResult{}, errChatRequiresPostgres
}
func (s *InMemoryStore) AppendChatMessageEvent(_ context.Context, _ string, _ *model.ChatMessageEvent) error {
	return errChatRequiresPostgres
}
func (s *InMemoryStore) ListChatMessageEvents(_ context.Context, _, _ string, _ int) ([]model.ChatMessageEvent, error) {
	return nil, errChatRequiresPostgres
}
func (s *InMemoryStore) DeleteChatMessageEventsOlderThan(_ context.Context, _ time.Time) (int64, error) {
	return 0, errChatRequiresPostgres
}
func (s *InMemoryStore) ListExpiredChatLeases(_ context.Context, _ time.Time, _ int) ([]ExpiredChatLease, error) {
	return nil, errChatRequiresPostgres
}
func (s *InMemoryStore) CreateChatToolApproval(_ context.Context, _ string, _ *model.ChatToolApproval) error {
	return errChatRequiresPostgres
}
func (s *InMemoryStore) GetChatToolApproval(_ context.Context, _, _ string) (*model.ChatToolApproval, error) {
	return nil, errChatRequiresPostgres
}
func (s *InMemoryStore) DecideChatToolApproval(_ context.Context, _, _, _, _ string) error {
	return errChatRequiresPostgres
}
func (s *InMemoryStore) ExpireChatToolApproval(_ context.Context, _, _ string) error {
	return errChatRequiresPostgres
}
func (s *InMemoryStore) ListPendingChatToolApprovals(_ context.Context, _, _ string) ([]model.ChatToolApproval, error) {
	return nil, errChatRequiresPostgres
}
func (s *InMemoryStore) ExpirePendingChatToolApprovals(_ context.Context) ([]ExpiredChatToolApproval, error) {
	return nil, errChatRequiresPostgres
}

// --- Personas (in-memory) ---

func (s *InMemoryStore) UpsertPersona(p *model.Persona) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.personas == nil {
		s.personas = make(map[string]*model.Persona)
	}
	if s.personaRevisions == nil {
		s.personaRevisions = make(map[string][]*model.PersonaRevision)
	}
	record := func(id string, version int) {
		s.personaRevisions[id] = append(s.personaRevisions[id], &model.PersonaRevision{
			ID:          id + "-v" + fmt.Sprint(version),
			PersonaID:   id,
			OwnerID:     p.OwnerID,
			Version:     version,
			Content:     p.Content,
			ContentHash: p.ContentHash,
			CreatedAt:   p.UpdatedAt,
		})
	}
	for _, existing := range s.personas {
		if existing.OwnerID == p.OwnerID && existing.SourceAgent == p.SourceAgent &&
			existing.SourceScope == p.SourceScope && existing.SourceFilePath == p.SourceFilePath {
			if existing.ContentHash == p.ContentHash {
				return nil // unchanged content — no-op
			}
			existing.Name = p.Name
			existing.Content = p.Content
			existing.ContentHash = p.ContentHash
			existing.Version++
			existing.UpdatedAt = p.UpdatedAt
			record(existing.ID, existing.Version)
			return nil
		}
	}
	s.personas[p.ID] = p
	record(p.ID, p.Version)
	return nil
}

func (s *InMemoryStore) ListPersonas(ownerID string) ([]model.Persona, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var personas []model.Persona
	for _, p := range s.personas {
		if ownerID == "local" || p.OwnerID == ownerID {
			personas = append(personas, *p)
		}
	}
	sort.Slice(personas, func(i, j int) bool {
		return personas[i].UpdatedAt.After(personas[j].UpdatedAt)
	})
	return personas, nil
}

func (s *InMemoryStore) GetPersona(id string, ownerID string) (*model.Persona, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.personas[id]
	if !ok || (ownerID != "local" && p.OwnerID != ownerID) {
		return nil, fmt.Errorf("persona not found: %s", id)
	}
	return p, nil
}

func (s *InMemoryStore) DeletePersona(id string, ownerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.personas[id]
	if !ok || (ownerID != "local" && p.OwnerID != ownerID) {
		return nil
	}
	delete(s.personas, id)
	delete(s.personaRevisions, id)
	return nil
}

func (s *InMemoryStore) DeletePersonaBySource(agent, filePath, scope, ownerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.personas {
		if p.SourceAgent == agent && p.SourceFilePath == filePath && p.SourceScope == scope &&
			(ownerID == "local" || p.OwnerID == ownerID) {
			delete(s.personas, id)
			delete(s.personaRevisions, id)
		}
	}
	return nil
}

func (s *InMemoryStore) ListPersonaRevisions(personaID, ownerID string) ([]model.PersonaRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var revisions []model.PersonaRevision
	for _, rev := range s.personaRevisions[personaID] {
		if ownerID == "local" || rev.OwnerID == ownerID {
			// List omits content — matches the Postgres implementation.
			meta := *rev
			meta.Content = ""
			revisions = append(revisions, meta)
		}
	}
	sort.Slice(revisions, func(i, j int) bool {
		return revisions[i].Version > revisions[j].Version
	})
	return revisions, nil
}

func (s *InMemoryStore) GetPersonaRevision(personaID string, version int, ownerID string) (*model.PersonaRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rev := range s.personaRevisions[personaID] {
		if rev.Version == version && (ownerID == "local" || rev.OwnerID == ownerID) {
			return rev, nil
		}
	}
	return nil, fmt.Errorf("persona revision not found: %s v%d", personaID, version)
}

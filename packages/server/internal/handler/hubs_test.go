package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type hubsTestStore struct {
	*store.InMemoryStore
	hubs                map[string]*model.Hub
	roles               map[string]string
	invites             map[string]*model.HubInvite
	transfers           map[string]*model.HubOwnershipTransfer
	users               map[string]*model.User
	usersByEmail        map[string]*model.User
	membersByHub        map[string][]model.HubMember
	topicsByHub         map[string][]model.Topic
	topicCountsByHub    map[string]map[string]int
	unassignedByHub     map[string]int
	pendingReviewsByHub map[string]int
	dreamRunsByHub      map[string]*model.DreamRun
	recentActivityByHub map[string]int
	visits              map[string]*model.HubVisit
	notifications       map[string]*model.Notification
	// afterGetHub, if set, fires after GetHub has returned a copy
	// but before the handler has done anything with it. One-shot:
	// consumed on first trigger. Used by the race-regression test
	// to inject a concurrent write between the handler's GetHub
	// and its next store call.
	afterGetHub func(id string)
	// subscriptions lets a test seed the result of
	// GetHubSubscriptionAnyStatus so the handler's
	// IsHubFrozen-derived fields can be exercised. Nil means "no
	// subscription" (free hub; never frozen).
	subscriptions map[string]*model.HubSubscription
}

func (s *hubsTestStore) GetHubSubscriptionAnyStatus(_ context.Context, hubID string) (*model.HubSubscription, error) {
	return s.subscriptions[hubID], nil
}

// allowAllOwnership is a mock ownership resolver that allows all hub creation.
type allowAllOwnership struct{}

func (a *allowAllOwnership) ResolveOwnershipEntitlements(_ context.Context, _ string) (model.OwnershipEntitlements, error) {
	return model.OwnershipEntitlements{
		PersonalPlanID:       model.PersonalProPlanID,
		MaxOwnedFreeTeamHubs: 100,
		CurrentOwnedCount:    0,
		CanCreateFreeTeamHub: true,
	}, nil
}

func newHubsTestStore() *hubsTestStore {
	return &hubsTestStore{
		InMemoryStore:       store.NewInMemoryStore(),
		hubs:                make(map[string]*model.Hub),
		roles:               make(map[string]string),
		invites:             make(map[string]*model.HubInvite),
		transfers:           make(map[string]*model.HubOwnershipTransfer),
		users:               make(map[string]*model.User),
		usersByEmail:        make(map[string]*model.User),
		membersByHub:        make(map[string][]model.HubMember),
		topicsByHub:         make(map[string][]model.Topic),
		topicCountsByHub:    make(map[string]map[string]int),
		unassignedByHub:     make(map[string]int),
		pendingReviewsByHub: make(map[string]int),
		dreamRunsByHub:      make(map[string]*model.DreamRun),
		recentActivityByHub: make(map[string]int),
		visits:              make(map[string]*model.HubVisit),
		notifications:       make(map[string]*model.Notification),
		subscriptions:       make(map[string]*model.HubSubscription),
	}
}

func (s *hubsTestStore) addTestUser(user *model.User) {
	copy := *user
	s.users[user.ID] = &copy
	if email := strings.ToLower(strings.TrimSpace(user.Email)); email != "" {
		s.usersByEmail[email] = &copy
	}
}

func (s *hubsTestStore) GetUserByCanonicalEmail(email string) (*model.User, error) {
	key := strings.ToLower(strings.TrimSpace(email))
	if user, ok := s.usersByEmail[key]; ok {
		copy := *user
		return &copy, nil
	}
	return nil, http.ErrMissingFile
}

func (s *hubsTestStore) CreateNotification(n *model.Notification) error {
	// Mirror production's notifications_source_unique (source_kind,
	// source_id) uniqueness + ON CONFLICT DO UPDATE payload/priority
	// /expires_at semantics. Without this, the in-memory test store
	// silently accepted duplicates and masked source-id collision
	// bugs like the pre-fix hub_member_joined keying on user id.
	if n.SourceKind != "" && n.SourceID != "" {
		for _, existing := range s.notifications {
			if existing.SourceKind == n.SourceKind && existing.SourceID == n.SourceID {
				existing.Payload = n.Payload
				existing.Priority = n.Priority
				existing.ExpiresAt = n.ExpiresAt
				return nil
			}
		}
	}
	copy := *n
	s.notifications[n.ID] = &copy
	return nil
}

func (s *hubsTestStore) ResolveNotificationsBySource(_ context.Context, sourceKind, sourceID string, resolution model.NotificationResolution) ([]model.Notification, error) {
	now := time.Now()
	out := []model.Notification{}
	for _, n := range s.notifications {
		if n.SourceKind != sourceKind || n.SourceID != sourceID {
			continue
		}
		if n.Status != model.NotificationStatusPending {
			continue
		}
		n.Status = model.NotificationStatusResolved
		n.Resolution = resolution
		n.ResolvedAt = &now
		if n.SeenAt == nil {
			n.SeenAt = &now
		}
		out = append(out, *n)
	}
	return out, nil
}

func (s *hubsTestStore) AcceptHubInviteByID(_ context.Context, inviteID, userID string, _ int) (*model.HubInvite, error) {
	invite, ok := s.invites[inviteID]
	if !ok {
		return nil, store.ErrHubInviteNotFound
	}
	if invite.RevokedAt != nil {
		return nil, store.ErrHubInviteRevoked
	}
	if time.Now().After(invite.ExpiresAt) {
		return nil, store.ErrHubInviteExpired
	}
	if invite.AcceptedBy != nil {
		return nil, store.ErrHubInviteUsed
	}
	if invite.InviteeUserID == nil || *invite.InviteeUserID == "" {
		return nil, store.ErrHubInviteNotAddressable
	}
	if *invite.InviteeUserID != userID {
		return nil, store.ErrHubInviteWrongInvitee
	}
	if s.roles[invite.HubID+":"+userID] != "" {
		return nil, store.ErrHubAlreadyMember
	}
	now := time.Now()
	s.roles[invite.HubID+":"+userID] = invite.Role
	invite.AcceptedBy = &userID
	invite.AcceptedAt = &now
	copy := *invite
	return &copy, nil
}

func (s *hubsTestStore) DeclineHubInviteByID(_ context.Context, inviteID, userID string) error {
	invite, ok := s.invites[inviteID]
	if !ok {
		return store.ErrHubInviteNotFound
	}
	if invite.InviteeUserID == nil || *invite.InviteeUserID != userID {
		return store.ErrHubInviteWrongInvitee
	}
	now := time.Now()
	invite.RevokedBy = &userID
	invite.RevokedAt = &now
	return nil
}

func timezoneForBucket(t *testing.T, bucket model.HubTimeBucket) string {
	t.Helper()

	candidates := []string{
		"UTC",
		"Pacific/Kiritimati",
		"Pacific/Auckland",
		"Asia/Tokyo",
		"Asia/Singapore",
		"Asia/Dubai",
		"Europe/Berlin",
		"Europe/London",
		"America/Sao_Paulo",
		"America/New_York",
		"America/Chicago",
		"America/Denver",
		"America/Los_Angeles",
		"Pacific/Honolulu",
	}

	now := time.Now().UTC()
	for _, name := range candidates {
		loc, err := time.LoadLocation(name)
		if err != nil {
			continue
		}
		if getHubTimeBucket(now.In(loc)) == bucket {
			return name
		}
	}

	t.Fatalf("could not find timezone for bucket %s", bucket)
	return "UTC"
}

func (s *hubsTestStore) CreateHub(hub *model.Hub) error {
	copy := *hub
	s.hubs[hub.ID] = &copy
	return nil
}

func (s *hubsTestStore) CreateTeamHub(_ context.Context, hub *model.Hub, ownerID string, _ int) error {
	copy := *hub
	s.hubs[hub.ID] = &copy
	s.membersByHub[hub.ID] = append(s.membersByHub[hub.ID], model.HubMember{
		HubID: hub.ID, UserID: ownerID, Role: "owner",
	})
	return nil
}

func (s *hubsTestStore) AddHubMemberWithCapCheck(_ context.Context, hubID, userID, role string, _ int) error {
	s.membersByHub[hubID] = append(s.membersByHub[hubID], model.HubMember{
		HubID: hubID, UserID: userID, Role: role,
	})
	return nil
}

func (s *hubsTestStore) GetHubMemberCap(_ context.Context, _ string) (int, error) {
	return -1, nil
}

func (s *hubsTestStore) GetHub(id string) (*model.Hub, error) {
	hub, ok := s.hubs[id]
	if !ok {
		return nil, http.ErrMissingFile
	}
	copy := *hub
	if hook := s.afterGetHub; hook != nil {
		s.afterGetHub = nil // one-shot so refetches don't re-fire
		hook(id)
	}
	return &copy, nil
}

func (s *hubsTestStore) GetHubBySlug(slug string) (*model.Hub, error) {
	for _, hub := range s.hubs {
		if hub.Slug == slug {
			copy := *hub
			return &copy, nil
		}
	}
	return nil, store.ErrHubNotFound
}

func (s *hubsTestStore) UpdateHub(hub *model.Hub) error {
	copy := *hub
	s.hubs[hub.ID] = &copy
	return nil
}

func (s *hubsTestStore) PatchHubSettings(_ context.Context, hubID string, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	hub, ok := s.hubs[hubID]
	if !ok {
		return store.ErrHubNotFound
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

func (s *hubsTestStore) AddHubMember(hubID, userID, role string) error {
	s.roles[hubID+":"+userID] = role
	return nil
}

func (s *hubsTestStore) RemoveHubMember(hubID, userID string) error {
	delete(s.roles, hubID+":"+userID)
	return nil
}

func (s *hubsTestStore) UpdateHubMemberRole(hubID, userID, role string) error {
	s.roles[hubID+":"+userID] = role
	return nil
}

func (s *hubsTestStore) GetHubMemberRole(hubID, userID string) (string, error) {
	return s.roles[hubID+":"+userID], nil
}

func (s *hubsTestStore) CountHubMembers(hubID string) (int, error) {
	if members, ok := s.membersByHub[hubID]; ok {
		return len(members), nil
	}
	count := 0
	for key := range s.roles {
		if len(key) >= len(hubID)+1 && key[:len(hubID)+1] == hubID+":" {
			count++
		}
	}
	return count, nil
}

func (s *hubsTestStore) ListHubMembers(hubID string) ([]model.HubMember, error) {
	if members, ok := s.membersByHub[hubID]; ok {
		return append([]model.HubMember(nil), members...), nil
	}
	return nil, nil
}

func (s *hubsTestStore) ListTopics(hubID string) ([]model.Topic, error) {
	return append([]model.Topic(nil), s.topicsByHub[hubID]...), nil
}

func (s *hubsTestStore) CountTopicMemories(_ store.VisibilityScope, hubID string) (map[string]int, int, error) {
	counts := map[string]int{}
	for topicID, count := range s.topicCountsByHub[hubID] {
		counts[topicID] = count
	}
	return counts, s.unassignedByHub[hubID], nil
}

func (s *hubsTestStore) GetNotificationSummary(_ context.Context, opts store.NotificationSummaryOpts) (*model.NotificationSummary, error) {
	count := 0
	if opts.HubID != "" {
		count = s.pendingReviewsByHub[opts.HubID]
	}
	return &model.NotificationSummary{NeedsActionPending: count}, nil
}

func (s *hubsTestStore) CountRecentMemoriesByHub(_ store.VisibilityScope, hubID string, _ time.Time) (int, error) {
	return s.recentActivityByHub[hubID], nil
}

func (s *hubsTestStore) GetLatestDreamRunByHub(hubID string) (*model.DreamRun, error) {
	run, ok := s.dreamRunsByHub[hubID]
	if !ok {
		return nil, http.ErrMissingFile
	}
	copy := *run
	return &copy, nil
}

func (s *hubsTestStore) GetUser(userID string) (*model.User, error) {
	user, ok := s.users[userID]
	if !ok {
		return nil, http.ErrMissingFile
	}
	copy := *user
	return &copy, nil
}

func (s *hubsTestStore) GetHubVisit(userID, hubID string) (*model.HubVisit, error) {
	visit, ok := s.visits[hubID+":"+userID]
	if !ok {
		return nil, http.ErrMissingFile
	}
	copy := *visit
	return &copy, nil
}

func (s *hubsTestStore) UpsertHubVisit(userID, hubID string, visitedAt time.Time) error {
	key := hubID + ":" + userID
	if visit, ok := s.visits[key]; ok {
		visit.LastVisitedAt = visitedAt
		visit.UpdatedAt = visitedAt
		return nil
	}
	s.visits[key] = &model.HubVisit{
		UserID:         userID,
		HubID:          hubID,
		FirstVisitedAt: visitedAt,
		LastVisitedAt:  visitedAt,
		CreatedAt:      visitedAt,
		UpdatedAt:      visitedAt,
	}
	return nil
}

func (s *hubsTestStore) DeleteHub(hubID string) error {
	delete(s.hubs, hubID)
	for key := range s.roles {
		if len(key) >= len(hubID)+1 && key[:len(hubID)+1] == hubID+":" {
			delete(s.roles, key)
		}
	}
	return nil
}

func (s *hubsTestStore) CreateHubInvite(invite *model.HubInvite) error {
	copy := *invite
	s.invites[invite.ID] = &copy
	return nil
}

func (s *hubsTestStore) GetHubInvite(id string) (*model.HubInvite, error) {
	invite, ok := s.invites[id]
	if !ok {
		return nil, http.ErrMissingFile
	}
	copy := *invite
	return &copy, nil
}

func (s *hubsTestStore) GetHubInviteByToken(token string) (*model.HubInvite, error) {
	for _, invite := range s.invites {
		if invite.Token == token && invite.RevokedAt == nil {
			copy := *invite
			return &copy, nil
		}
	}
	return nil, http.ErrMissingFile
}

func (s *hubsTestStore) ListOutstandingHubInvites(hubID string) ([]model.HubInvite, error) {
	var invites []model.HubInvite
	now := time.Now()
	for _, invite := range s.invites {
		if invite.HubID != hubID || invite.AcceptedBy != nil || invite.RevokedAt != nil || !invite.ExpiresAt.After(now) {
			continue
		}
		invites = append(invites, *invite)
	}
	return invites, nil
}

func (s *hubsTestStore) CountActiveHubInvites(hubID string) (int, error) {
	invites, _ := s.ListOutstandingHubInvites(hubID)
	return len(invites), nil
}

func (s *hubsTestStore) AcceptHubInvite(token string, userID string, _ int) (*model.HubInvite, error) {
	now := time.Now()
	for _, invite := range s.invites {
		if invite.Token != token {
			continue
		}
		if invite.RevokedAt != nil {
			return nil, store.ErrHubInviteRevoked
		}
		if now.After(invite.ExpiresAt) {
			return nil, store.ErrHubInviteExpired
		}
		if invite.AcceptedBy != nil {
			return nil, store.ErrHubInviteUsed
		}
		// Mirror production: addressed invites must be accepted by
		// their specific invitee, even on the token path.
		if invite.InviteeUserID != nil && *invite.InviteeUserID != "" && *invite.InviteeUserID != userID {
			return nil, store.ErrHubInviteWrongInvitee
		}
		if s.roles[invite.HubID+":"+userID] != "" {
			return nil, store.ErrHubAlreadyMember
		}
		s.roles[invite.HubID+":"+userID] = invite.Role
		invite.AcceptedBy = &userID
		invite.AcceptedAt = &now
		copy := *invite
		return &copy, nil
	}
	return nil, store.ErrHubInviteNotFound
}

func (s *hubsTestStore) RevokeHubInvite(hubID, inviteID, userID string) error {
	invite, ok := s.invites[inviteID]
	if !ok || invite.HubID != hubID {
		return http.ErrMissingFile
	}
	now := time.Now()
	invite.RevokedBy = &userID
	invite.RevokedAt = &now
	return nil
}

func (s *hubsTestStore) CreateHubOwnershipTransfer(transfer *model.HubOwnershipTransfer) error {
	for _, existing := range s.transfers {
		if existing.HubID == transfer.HubID && existing.AcceptedAt == nil && existing.CancelledAt == nil && existing.ExpiresAt.After(time.Now()) {
			return store.ErrHubTransferExists
		}
	}
	copy := *transfer
	s.transfers[transfer.ID] = &copy
	return nil
}

func (s *hubsTestStore) GetHubOwnershipTransfer(id string) (*model.HubOwnershipTransfer, error) {
	transfer, ok := s.transfers[id]
	if !ok {
		return nil, http.ErrMissingFile
	}
	copy := *transfer
	return &copy, nil
}

func (s *hubsTestStore) GetActiveHubOwnershipTransfer(hubID string) (*model.HubOwnershipTransfer, error) {
	for _, transfer := range s.transfers {
		if transfer.HubID == hubID && transfer.AcceptedAt == nil && transfer.CancelledAt == nil && transfer.ExpiresAt.After(time.Now()) {
			copy := *transfer
			return &copy, nil
		}
	}
	return nil, http.ErrMissingFile
}

func (s *hubsTestStore) AcceptHubOwnershipTransfer(hubID, transferID, userID string, _ int) (*model.HubOwnershipTransfer, error) {
	transfer, ok := s.transfers[transferID]
	if !ok || transfer.HubID != hubID {
		return nil, store.ErrHubTransferNotFound
	}
	now := time.Now()
	if transfer.CancelledAt != nil {
		return nil, store.ErrHubTransferCanceled
	}
	if transfer.AcceptedAt != nil {
		return nil, store.ErrHubTransferAccepted
	}
	if now.After(transfer.ExpiresAt) {
		return nil, store.ErrHubTransferExpired
	}
	if transfer.TargetUserID != userID {
		return nil, store.ErrHubTransferNotFound
	}
	hub := s.hubs[hubID]
	oldOwnerID := hub.OwnerID
	hub.OwnerID = userID
	s.roles[hubID+":"+oldOwnerID] = "admin"
	s.roles[hubID+":"+userID] = "owner"
	transfer.AcceptedAt = &now
	copy := *transfer
	return &copy, nil
}

func (s *hubsTestStore) CancelHubOwnershipTransfer(hubID, transferID string) error {
	transfer, ok := s.transfers[transferID]
	if !ok || transfer.HubID != hubID {
		return store.ErrHubTransferNotFound
	}
	if transfer.AcceptedAt != nil {
		return store.ErrHubTransferAccepted
	}
	if transfer.CancelledAt != nil {
		return store.ErrHubTransferCanceled
	}
	now := time.Now()
	transfer.CancelledAt = &now
	return nil
}

func TestHubsCreateRejectsReservedPersonalSlug(t *testing.T) {
	// Reserved slugs are masked as the same `slug_taken` 409 that a
	// real name collision returns, so a caller cannot enumerate the
	// reserved set by probing names. The server-side reserved list
	// is the source of truth; the frontend sees an indistinguishable
	// "already exists" answer.
	s := newHubsTestStore()
	h := NewHubsHandler(s)
	h.SetOwnershipResolver(&allowAllOwnership{})

	req := httptest.NewRequest(http.MethodPost, "/v1/hubs", bytes.NewBufferString(`{"name":"Personal","slug":"personal"}`))
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !bytes.Contains([]byte(got), []byte("slug_taken")) {
		t.Fatalf("expected slug_taken error, got %s", got)
	}
}

func TestHubsCreateRejectsShortSlug(t *testing.T) {
	// Slugs shorter than 4 characters fail the regex with
	// invalid_slug — length is public (mentioned in the error
	// message and in the frontend) so no masking here.
	s := newHubsTestStore()
	h := NewHubsHandler(s)
	h.SetOwnershipResolver(&allowAllOwnership{})

	req := httptest.NewRequest(http.MethodPost, "/v1/hubs", bytes.NewBufferString(`{"name":"AI Team","slug":"ai"}`))
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !bytes.Contains([]byte(got), []byte("invalid_slug")) {
		t.Fatalf("expected invalid_slug error, got %s", got)
	}
}

func TestHubsCheckSlugMasksReservedAsTaken(t *testing.T) {
	// CheckSlug must NOT distinguish reserved names from real
	// collisions — returning "reserved" would let the frontend
	// enumerate the whole list by probing common words.
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	for _, slug := range []string{"personal", "shared", "admin", "memax"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/hubs/check-slug?slug="+slug, nil)
		req = withTestIdentity(req, "u1")
		rec := httptest.NewRecorder()
		h.CheckSlug(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("slug=%q: expected 200, got %d", slug, rec.Code)
			continue
		}
		body := rec.Body.String()
		if !bytes.Contains([]byte(body), []byte(`"available":false`)) {
			t.Errorf("slug=%q: expected available=false, got %s", slug, body)
		}
		if !bytes.Contains([]byte(body), []byte(`"reason":"taken"`)) {
			t.Errorf("slug=%q: expected reason=taken (masked), got %s", slug, body)
		}
		if bytes.Contains([]byte(body), []byte(`"reason":"reserved"`)) {
			t.Errorf("slug=%q: reserved reason leaked through masking: %s", slug, body)
		}
	}
}

func TestHubsUpdateRejectsSlugField(t *testing.T) {
	// Slug is immutable after creation — any slug in PATCH body is rejected
	// before other validation (reserved, format, etc.) is reached.
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Team",
		Slug:    "team",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/v1/hubs/"+hub.ID, bytes.NewBufferString(`{"slug":"personal"}`))
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "slug_immutable") {
		t.Fatalf("expected slug_immutable error, got %s", got)
	}
}

func TestHubsUpdateAllowsAdminToChangeTeamIdentity(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Platform",
		Icon:    "🛠",
		Accent:  model.DefaultHubAccent("team"),
		Slug:    "platform",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u2", "admin"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/v1/hubs/"+hub.ID, bytes.NewBufferString(`{"name":"Platform Ops","icon":"🚀"}`))
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u2")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, err := s.GetHub(hub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Platform Ops" {
		t.Fatalf("expected updated name, got %q", updated.Name)
	}
	if updated.Icon != "🚀" {
		t.Fatalf("expected updated icon, got %q", updated.Icon)
	}
	if updated.Accent != model.DefaultHubAccent("team") {
		t.Fatalf("expected default team accent %q, got %q", model.DefaultHubAccent("team"), updated.Accent)
	}
}

func TestHubsUpdateRejectsLongIcon(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/v1/hubs/"+hub.ID, bytes.NewBufferString(`{"icon":"toolongicon"}`))
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !bytes.Contains([]byte(got), []byte("invalid_icon")) {
		t.Fatalf("expected invalid_icon error, got %s", got)
	}
}

func TestHubsUpdateAllowsAccentChange(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: "u1",
		Accent:  model.DefaultHubAccent("team"),
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/v1/hubs/"+hub.ID, bytes.NewBufferString(`{"accent":"blue"}`))
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, err := s.GetHub(hub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Accent != "blue" {
		t.Fatalf("expected updated accent, got %q", updated.Accent)
	}
}

func TestHubsUpdateRejectsInvalidAccent(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/v1/hubs/"+hub.ID, bytes.NewBufferString(`{"accent":"purple"}`))
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !bytes.Contains([]byte(got), []byte("invalid_accent")) {
		t.Fatalf("expected invalid_accent error, got %s", got)
	}
}

func TestHubsUpdateAllowsTeamHeaderAuroraMode(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/v1/hubs/"+hub.ID, bytes.NewBufferString(`{"header_aurora_mode":"time"}`))
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, err := s.GetHub(hub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.HeaderAuroraMode != "time" {
		t.Fatalf("expected header_aurora_mode=time, got %q", updated.HeaderAuroraMode)
	}
}

func TestHubsUpdateClearsTeamHeaderAuroraMode(t *testing.T) {
	// Whitespace / empty input must land as cleared so the hub reverts
	// to the inherit-default banner rather than becoming a free-text value.
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:               "11111111-1111-1111-1111-111111111111",
		Name:             "Platform",
		Slug:             "platform",
		HubType:          "team",
		OwnerID:          "u1",
		HeaderAuroraMode: "time",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/v1/hubs/"+hub.ID, bytes.NewBufferString(`{"header_aurora_mode":"  "}`))
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, err := s.GetHub(hub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.HeaderAuroraMode != "" {
		t.Fatalf("expected cleared header_aurora_mode, got %q", updated.HeaderAuroraMode)
	}
}

func TestHubsUpdateRejectsInvalidHeaderAuroraMode(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/v1/hubs/"+hub.ID, bytes.NewBufferString(`{"header_aurora_mode":"neon"}`))
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !bytes.Contains([]byte(got), []byte("invalid_header_aurora_mode")) {
		t.Fatalf("expected invalid_header_aurora_mode error, got %s", got)
	}
}

func TestHubsUpdateRejectsHeaderAuroraModeOnPersonalHub(t *testing.T) {
	// Personal hubs take their aurora mode from user settings. The hub
	// endpoint must refuse the ambiguous write rather than silently drop
	// it — otherwise a client could think the write succeeded and render
	// a mode the server isn't storing.
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Memory Hub",
		Slug:    "u1-personal",
		HubType: "personal",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/v1/hubs/"+hub.ID, bytes.NewBufferString(`{"header_aurora_mode":"time"}`))
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !bytes.Contains([]byte(got), []byte("personal_hub_header_mode")) {
		t.Fatalf("expected personal_hub_header_mode error, got %s", got)
	}
	updated, err := s.GetHub(hub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.HeaderAuroraMode != "" {
		t.Fatalf("expected personal hub aurora mode to stay empty, got %q", updated.HeaderAuroraMode)
	}
}

// Commit 3.6: per-hub dream settings. PATCH /v1/hubs/{id} gained a
// `settings` partial — boolean overrides on top of account-level
// user prefs. These tests lock down the contract: allow-listed keys
// pass through, unknown keys 400, null deletes, merges are additive.
func TestHubsUpdateAcceptsDreamSettingsPartial(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(
		http.MethodPatch,
		"/v1/hubs/"+hub.ID,
		bytes.NewBufferString(`{"settings":{"dreams_merge_enabled":false,"dreams_restructure_enabled":true}}`),
	)
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, err := s.GetHub(hub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.Settings["dreams_merge_enabled"]; got != false {
		t.Fatalf("dreams_merge_enabled = %v, want false", got)
	}
	if got := updated.Settings["dreams_restructure_enabled"]; got != true {
		t.Fatalf("dreams_restructure_enabled = %v, want true", got)
	}
}

func TestHubsUpdateRejectsUnknownSettingKey(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(
		http.MethodPatch,
		"/v1/hubs/"+hub.ID,
		bytes.NewBufferString(`{"settings":{"theme":"dark"}}`),
	)
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "unknown_setting") {
		t.Fatalf("expected unknown_setting error, got %s", got)
	}
	// Nothing should have been persisted — hub.Settings stays nil.
	got, err := s.GetHub(hub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Settings != nil && len(got.Settings) > 0 {
		t.Fatalf("expected hub.Settings untouched on 400, got %v", got.Settings)
	}
}

func TestHubsUpdateRejectsNonBooleanSettingValue(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(
		http.MethodPatch,
		"/v1/hubs/"+hub.ID,
		bytes.NewBufferString(`{"settings":{"dreams_enabled":"sometimes"}}`),
	)
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "invalid_setting_value") {
		t.Fatalf("expected invalid_setting_value error, got %s", got)
	}
}

func TestHubsUpdateSettingsPartialMergesIntoExisting(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: "u1",
		Settings: map[string]any{
			"dreams_merge_enabled":   false,
			"dreams_archive_enabled": true,
		},
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(
		http.MethodPatch,
		"/v1/hubs/"+hub.ID,
		bytes.NewBufferString(`{"settings":{"dreams_merge_enabled":true}}`),
	)
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, err := s.GetHub(hub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.Settings["dreams_merge_enabled"]; got != true {
		t.Fatalf("dreams_merge_enabled should flip to true, got %v", got)
	}
	if got := updated.Settings["dreams_archive_enabled"]; got != true {
		t.Fatalf("dreams_archive_enabled should be preserved as true, got %v", got)
	}
}

func TestHubsUpdateSettingsNullDeletesOverride(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: "u1",
		Settings: map[string]any{
			"dreams_merge_enabled":   false,
			"dreams_archive_enabled": false,
		},
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(
		http.MethodPatch,
		"/v1/hubs/"+hub.ID,
		bytes.NewBufferString(`{"settings":{"dreams_merge_enabled":null}}`),
	)
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, err := s.GetHub(hub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := updated.Settings["dreams_merge_enabled"]; present {
		t.Fatalf("dreams_merge_enabled should be deleted, still present: %v", updated.Settings)
	}
	if got := updated.Settings["dreams_archive_enabled"]; got != false {
		t.Fatalf("dreams_archive_enabled should be preserved as false, got %v", got)
	}
}

// TestHubsUpdateSettingsOnlyDoesNotTouchNonSettingsFields covers
// the 3.6 pass-3 fix: a settings-only PATCH must skip the
// UpdateHub (non-settings) write path entirely, otherwise the
// pre-PATCH GetHub snapshot of name/icon/accent could clobber a
// concurrent non-settings PATCH on the same hub.
//
// We install a one-shot hook that fires AFTER the handler's
// GetHub has returned its copy of the hub (so the handler sees
// the original name "Original"), and mutates the stored row to
// "Racer" before any subsequent store call. If the handler
// wrongly calls UpdateHub, it writes back its stale copy
// ("Original") and clobbers "Racer". With the fix, UpdateHub is
// skipped entirely on settings-only payloads and "Racer" survives.
func TestHubsUpdateSettingsOnlyDoesNotTouchNonSettingsFields(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Original",
		Slug:    "platform",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	// One-shot hook: fires on the handler's first GetHub, after
	// it's already copied the hub. Stands in for a concurrent
	// rename arriving between handler steps — the only place a
	// stale-snapshot clobber could happen.
	s.afterGetHub = func(_ string) {
		s.hubs[hub.ID].Name = "Racer"
	}

	req := httptest.NewRequest(
		http.MethodPatch,
		"/v1/hubs/"+hub.ID,
		bytes.NewBufferString(`{"settings":{"dreams_merge_enabled":false}}`),
	)
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	updated, err := s.GetHub(hub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Racer" {
		t.Fatalf("settings-only PATCH leaked non-settings write: name = %q, want %q (handler must not call UpdateHub when only settings is in the body)", updated.Name, "Racer")
	}
	if got := updated.Settings["dreams_merge_enabled"]; got != false {
		t.Fatalf("settings patch did not apply, got %v", got)
	}
}

func TestHubsUpdateSettingsRequiresOwnerOrAdmin(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u2", "contributor"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(
		http.MethodPatch,
		"/v1/hubs/"+hub.ID,
		bytes.NewBufferString(`{"settings":{"dreams_enabled":false}}`),
	)
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u2")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin contributor, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Commit 3.8: GET /v1/hubs/{id} surfaces a derived is_frozen
// boolean so the web client can disable the manual dream trigger
// preemptively (tooltip UX) without round-tripping through a 409.
// Locks down the three cases IsHubFrozen cares about: no
// subscription (free hub, never frozen), non-active status
// (cancelled / past_due → frozen), and active but past the
// over-limit grace window (frozen).
func TestHubsGetIsFrozenFalseWhenNoSubscription(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Free hub",
		Slug:    "free-hub",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/"+hub.ID, nil)
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			IsFrozen bool `json:"is_frozen"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.IsFrozen {
		t.Fatalf("no subscription = never frozen, got is_frozen=true")
	}
}

func TestHubsGetIsFrozenTrueWhenSubscriptionNotActive(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Cancelled hub",
		Slug:    "cancelled-hub",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}
	// past_due is one of the statuses IsHubFrozen treats as frozen.
	// GetHubSubscriptionAnyStatus (unlike GetActiveHubSubscription)
	// surfaces non-active rows so the handler can see them.
	s.subscriptions[hub.ID] = &model.HubSubscription{
		HubID:  hub.ID,
		Status: "past_due",
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/"+hub.ID, nil)
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			IsFrozen bool `json:"is_frozen"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Data.IsFrozen {
		t.Fatalf("past_due status should be frozen, got is_frozen=false")
	}
}

func TestHubsGetIsFrozenTrueWhenOverLimitGraceExpired(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Over-limit hub",
		Slug:    "overlimit-hub",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}
	// Active subscription but over-limit for well beyond the grace
	// period — IsHubFrozen kicks in regardless of status.
	past := time.Now().Add(-90 * 24 * time.Hour)
	s.subscriptions[hub.ID] = &model.HubSubscription{
		HubID:          hub.ID,
		Status:         "active",
		OverLimitSince: &past,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/"+hub.ID, nil)
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			IsFrozen bool `json:"is_frozen"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Data.IsFrozen {
		t.Fatalf("expired over-limit grace should be frozen, got is_frozen=false")
	}
}

func TestHubsListOutstandingInvites(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)
	hubID := "11111111-1111-1111-1111-111111111111"
	if err := s.CreateHub(&model.Hub{ID: hubID, Name: "Team", Slug: "team", HubType: "team", OwnerID: "u1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}
	active := &model.HubInvite{
		ID:        "22222222-2222-2222-2222-222222222222",
		HubID:     hubID,
		Token:     "active",
		InvitedBy: "u1",
		Role:      "contributor",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}
	expired := &model.HubInvite{
		ID:        "33333333-3333-3333-3333-333333333333",
		HubID:     hubID,
		Token:     "expired",
		InvitedBy: "u1",
		Role:      "contributor",
		ExpiresAt: time.Now().Add(-24 * time.Hour),
		CreatedAt: time.Now(),
	}
	if err := s.CreateHubInvite(active); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateHubInvite(expired); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/"+hubID+"/invites", nil)
	req.SetPathValue("id", hubID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.ListInvites(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatal(err)
	}
	var invites []model.HubInvite
	if err := json.Unmarshal(data, &invites); err != nil {
		t.Fatal(err)
	}
	if len(invites) != 1 || invites[0].ID != active.ID {
		t.Fatalf("expected only active invite, got %+v", invites)
	}
}

func TestHubsRevokeInvite(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)
	hubID := "11111111-1111-1111-1111-111111111111"
	inviteID := "22222222-2222-2222-2222-222222222222"
	if err := s.CreateHub(&model.Hub{ID: hubID, Name: "Team", Slug: "team", HubType: "team", OwnerID: "u1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:        inviteID,
		HubID:     hubID,
		Token:     "active",
		InvitedBy: "u1",
		Role:      "contributor",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/v1/hubs/"+hubID+"/invites/"+inviteID, nil)
	req.SetPathValue("id", hubID)
	req.SetPathValue("invite_id", inviteID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.RevokeInvite(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	invite, err := s.GetHubInvite(inviteID)
	if err != nil {
		t.Fatal(err)
	}
	if invite.RevokedAt == nil || invite.RevokedBy == nil || *invite.RevokedBy != "u1" {
		t.Fatalf("expected invite to be revoked, got %+v", invite)
	}
}

func TestHubsRegenerateInvite(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)
	hubID := "11111111-1111-1111-1111-111111111111"
	inviteID := "22222222-2222-2222-2222-222222222222"
	otherInviteID := "33333333-3333-3333-3333-333333333333"
	otherToken := "untouched"
	if err := s.CreateHub(&model.Hub{ID: hubID, Name: "Team", Slug: "team", HubType: "team", OwnerID: "u1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:        inviteID,
		HubID:     hubID,
		Token:     "active",
		InvitedBy: "u1",
		Role:      "viewer",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:        otherInviteID,
		HubID:     hubID,
		Token:     otherToken,
		InvitedBy: "u1",
		Role:      "contributor",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/hubs/"+hubID+"/invites/"+inviteID+"/regenerate", bytes.NewBufferString(`{}`))
	req.SetPathValue("id", hubID)
	req.SetPathValue("invite_id", inviteID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.RegenerateInvite(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	oldInvite, err := s.GetHubInvite(inviteID)
	if err != nil {
		t.Fatal(err)
	}
	if oldInvite.RevokedAt == nil {
		t.Fatalf("expected old invite to be revoked, got %+v", oldInvite)
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatal(err)
	}
	var replacement model.HubInvite
	if err := json.Unmarshal(data, &replacement); err != nil {
		t.Fatal(err)
	}
	if replacement.ID == inviteID {
		t.Fatalf("expected replacement invite id to differ from original")
	}
	if replacement.Role != "viewer" {
		t.Fatalf("expected replacement role to be preserved, got %q", replacement.Role)
	}
	if replacement.Token == otherToken {
		t.Fatalf("expected replacement token to differ from untouched invite token")
	}

	otherInvite, err := s.GetHubInvite(otherInviteID)
	if err != nil {
		t.Fatal(err)
	}
	if otherInvite.Token != otherToken {
		t.Fatalf("expected untouched invite token %q, got %q", otherToken, otherInvite.Token)
	}
	if otherInvite.RevokedAt != nil {
		t.Fatalf("expected untouched invite not to be revoked, got %+v", otherInvite)
	}

	invites, err := s.ListOutstandingHubInvites(hubID)
	if err != nil {
		t.Fatal(err)
	}
	if len(invites) != 2 {
		t.Fatalf("expected 2 outstanding invites after regenerate, got %d", len(invites))
	}
	seenUntouched := false
	seenReplacement := false
	for _, invite := range invites {
		if invite.ID == otherInviteID {
			seenUntouched = invite.Token == otherToken
		}
		if invite.ID == replacement.ID {
			seenReplacement = true
		}
	}
	if !seenUntouched {
		t.Fatalf("expected untouched invite to remain in outstanding invite list")
	}
	if !seenReplacement {
		t.Fatalf("expected replacement invite to appear in outstanding invite list")
	}
}

func TestHubsAcceptInviteMarksInviteUsed(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)
	hubID := "11111111-1111-1111-1111-111111111111"
	token := "accept-me"
	if err := s.CreateHub(&model.Hub{ID: hubID, Name: "Team", Slug: "team", HubType: "team", OwnerID: "u1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:        "22222222-2222-2222-2222-222222222222",
		HubID:     hubID,
		Token:     token,
		InvitedBy: "u1",
		Role:      "contributor",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/invites/"+token+"/accept", nil)
	req.SetPathValue("token", token)
	req = withTestIdentity(req, "u2")
	rec := httptest.NewRecorder()

	h.AcceptInvite(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	invite, err := s.GetHubInviteByToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if invite.AcceptedBy == nil || *invite.AcceptedBy != "u2" {
		t.Fatalf("expected invite accepted by u2, got %+v", invite)
	}
	if role, _ := s.GetHubMemberRole(hubID, "u2"); role != "contributor" {
		t.Fatalf("expected hub member role contributor, got %q", role)
	}
}

func TestHubsAcceptInviteRejectsUsedInvite(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)
	hubID := "11111111-1111-1111-1111-111111111111"
	token := "used-token"
	acceptedBy := "u2"
	acceptedAt := time.Now().Add(-time.Hour)
	if err := s.CreateHub(&model.Hub{ID: hubID, Name: "Team", Slug: "team", HubType: "team", OwnerID: "u1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:         "22222222-2222-2222-2222-222222222222",
		HubID:      hubID,
		Token:      token,
		InvitedBy:  "u1",
		Role:       "contributor",
		ExpiresAt:  time.Now().Add(24 * time.Hour),
		AcceptedBy: &acceptedBy,
		AcceptedAt: &acceptedAt,
		CreatedAt:  time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/invites/"+token+"/accept", nil)
	req.SetPathValue("token", token)
	req = withTestIdentity(req, "u3")
	rec := httptest.NewRecorder()

	h.AcceptInvite(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !bytes.Contains([]byte(got), []byte("used")) {
		t.Fatalf("expected used error, got %s", got)
	}
}

func TestHubsAcceptInviteRejectsAlreadyMember(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)
	hubID := "11111111-1111-1111-1111-111111111111"
	token := "member-token"
	if err := s.CreateHub(&model.Hub{ID: hubID, Name: "Team", Slug: "team", HubType: "team", OwnerID: "u1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubID, "u3", "contributor"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:        "22222222-2222-2222-2222-222222222222",
		HubID:     hubID,
		Token:     token,
		InvitedBy: "u1",
		Role:      "viewer",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/invites/"+token+"/accept", nil)
	req.SetPathValue("token", token)
	req = withTestIdentity(req, "u3")
	rec := httptest.NewRecorder()

	h.AcceptInvite(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !bytes.Contains([]byte(got), []byte("already_member")) {
		t.Fatalf("expected already_member error, got %s", got)
	}
}

func TestHubsUpdateMemberRole(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)
	hubID := "11111111-1111-1111-1111-111111111111"
	if err := s.CreateHub(&model.Hub{ID: hubID, Name: "Team", Slug: "team", HubType: "team", OwnerID: "u1"}); err != nil {
		t.Fatal(err)
	}
	_ = s.AddHubMember(hubID, "u1", "owner")
	_ = s.AddHubMember(hubID, "u2", "viewer")

	req := httptest.NewRequest(http.MethodPatch, "/v1/hubs/"+hubID+"/members/u2", bytes.NewBufferString(`{"role":"admin"}`))
	req.SetPathValue("id", hubID)
	req.SetPathValue("user_id", "u2")
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.UpdateMemberRole(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if role, _ := s.GetHubMemberRole(hubID, "u2"); role != "admin" {
		t.Fatalf("expected admin role, got %q", role)
	}
}

func TestHubsLeaveRemovesContributor(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)
	hubID := "11111111-1111-1111-1111-111111111111"
	if err := s.CreateHub(&model.Hub{ID: hubID, Name: "Team", Slug: "team", HubType: "team", OwnerID: "u1"}); err != nil {
		t.Fatal(err)
	}
	_ = s.AddHubMember(hubID, "u1", "owner")
	_ = s.AddHubMember(hubID, "u2", "contributor")

	req := httptest.NewRequest(http.MethodPost, "/v1/hubs/"+hubID+"/leave", bytes.NewBufferString(`{}`))
	req.SetPathValue("id", hubID)
	req = withTestIdentity(req, "u2")
	rec := httptest.NewRecorder()

	h.Leave(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if role, _ := s.GetHubMemberRole(hubID, "u2"); role != "" {
		t.Fatalf("expected user removed from hub, got role %q", role)
	}
}

func TestHubsOwnershipTransferAcceptDemotesPreviousOwner(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)
	hubID := "11111111-1111-1111-1111-111111111111"
	if err := s.CreateHub(&model.Hub{ID: hubID, Name: "Team", Slug: "team", HubType: "team", OwnerID: "u1"}); err != nil {
		t.Fatal(err)
	}
	_ = s.AddHubMember(hubID, "u1", "owner")
	_ = s.AddHubMember(hubID, "u2", "admin")

	createReq := httptest.NewRequest(http.MethodPost, "/v1/hubs/"+hubID+"/ownership-transfer", bytes.NewBufferString(`{"target_user_id":"u2"}`))
	createReq.SetPathValue("id", hubID)
	createReq = withTestIdentity(createReq, "u1")
	createRec := httptest.NewRecorder()
	h.CreateOwnershipTransfer(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatal(err)
	}
	var transfer model.HubOwnershipTransfer
	if err := json.Unmarshal(data, &transfer); err != nil {
		t.Fatal(err)
	}

	acceptReq := httptest.NewRequest(http.MethodPost, "/v1/hubs/"+hubID+"/ownership-transfer/"+transfer.ID+"/accept", bytes.NewBufferString(`{}`))
	acceptReq.SetPathValue("id", hubID)
	acceptReq.SetPathValue("transfer_id", transfer.ID)
	acceptReq = withTestIdentity(acceptReq, "u2")
	acceptRec := httptest.NewRecorder()
	h.AcceptOwnershipTransfer(acceptRec, acceptReq)
	if acceptRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", acceptRec.Code, acceptRec.Body.String())
	}
	if s.hubs[hubID].OwnerID != "u2" {
		t.Fatalf("expected hub owner u2, got %s", s.hubs[hubID].OwnerID)
	}
	if role, _ := s.GetHubMemberRole(hubID, "u1"); role != "admin" {
		t.Fatalf("expected previous owner downgraded to admin, got %q", role)
	}
	if role, _ := s.GetHubMemberRole(hubID, "u2"); role != "owner" {
		t.Fatalf("expected target promoted to owner, got %q", role)
	}
}

func TestHubsDeleteRequiresOwner(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)
	hubID := "11111111-1111-1111-1111-111111111111"
	if err := s.CreateHub(&model.Hub{ID: hubID, Name: "Team", Slug: "team", HubType: "team", OwnerID: "u1"}); err != nil {
		t.Fatal(err)
	}
	_ = s.AddHubMember(hubID, "u1", "owner")
	_ = s.AddHubMember(hubID, "u2", "admin")

	req := httptest.NewRequest(http.MethodDelete, "/v1/hubs/"+hubID, nil)
	req.SetPathValue("id", hubID)
	req = withTestIdentity(req, "u2")
	rec := httptest.NewRecorder()
	h.Delete(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHubsGetSummaryBuildsCanonicalModel(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hubID := "11111111-1111-1111-1111-111111111111"
	userID := "u1"
	now := time.Now().Add(-8 * 24 * time.Hour)

	s.hubs[hubID] = &model.Hub{
		ID:      hubID,
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: userID,
	}
	s.roles[hubID+":"+userID] = "owner"
	s.users[userID] = &model.User{ID: userID, Name: "Derek Xu", DisplayName: "Derek Xu"}
	s.membersByHub[hubID] = []model.HubMember{
		{HubID: hubID, UserID: userID, UserName: "Derek Xu", Role: "owner"},
		{HubID: hubID, UserID: "u2", UserName: "Ava Li", Role: "admin"},
	}
	s.topicsByHub[hubID] = []model.Topic{
		{ID: "t1", HubID: hubID, Name: "Auth"},
		{ID: "t2", HubID: hubID, Name: "Infra"},
	}
	s.topicCountsByHub[hubID] = map[string]int{"t1": 2, "t2": 1}
	s.unassignedByHub[hubID] = 4
	s.pendingReviewsByHub[hubID] = 2
	s.dreamRunsByHub[hubID] = &model.DreamRun{
		ID:                 "run-1",
		HubID:              hubID,
		Status:             "completed",
		TopicsRestructured: 3,
	}
	s.visits[hubID+":"+userID] = &model.HubVisit{
		UserID:         userID,
		HubID:          hubID,
		FirstVisitedAt: now,
		LastVisitedAt:  now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/"+hubID+"/summary", nil)
	req.SetPathValue("id", hubID)
	req = withTestIdentity(req, userID)
	rec := httptest.NewRecorder()

	h.GetSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatal(err)
	}
	var summary model.HubSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}

	if summary.Stats.Memories != 7 {
		t.Fatalf("expected 7 memories, got %d", summary.Stats.Memories)
	}
	if summary.Stats.Topics != 2 {
		t.Fatalf("expected 2 topics, got %d", summary.Stats.Topics)
	}
	if summary.Stats.Inbox != 4 {
		t.Fatalf("expected inbox 4, got %d", summary.Stats.Inbox)
	}
	if summary.Stats.PendingReview != 2 {
		t.Fatalf("expected 2 pending reviews, got %d", summary.Stats.PendingReview)
	}
	if summary.Stats.DreamTopics != 3 {
		t.Fatalf("expected 3 dream topics, got %d", summary.Stats.DreamTopics)
	}
	if summary.Header.State != model.HubHeaderStateReviewNeeded {
		t.Fatalf("expected review_needed state, got %s", summary.Header.State)
	}
	if summary.Header.GreetingKey != model.HubGreetingKeyTeamReviewA &&
		summary.Header.GreetingKey != model.HubGreetingKeyTeamReviewB {
		t.Fatalf("expected team review greeting key, got %s", summary.Header.GreetingKey)
	}
	if summary.Header.GreetingParams["name"] != "Derek" {
		t.Fatalf("expected first-name greeting param, got %#v", summary.Header.GreetingParams)
	}
	if summary.Header.GreetingParams["n"] != "2" {
		t.Fatalf("expected pending review count in params, got %#v", summary.Header.GreetingParams)
	}
	if len(summary.MembersPreview) != 2 {
		t.Fatalf("expected 2 members preview, got %d", len(summary.MembersPreview))
	}
}

func TestHubsGetSummaryUsesTeamActivityGreetingWhenCalm(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hubID := "11111111-1111-1111-1111-111111111111"
	userID := "u1"
	lastVisit := time.Now().Add(-48 * time.Hour)

	s.hubs[hubID] = &model.Hub{
		ID:      hubID,
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: userID,
	}
	s.roles[hubID+":"+userID] = "owner"
	s.users[userID] = &model.User{ID: userID, Name: "Derek Xu", DisplayName: "Derek Xu"}
	s.membersByHub[hubID] = []model.HubMember{
		{HubID: hubID, UserID: userID, UserName: "Derek Xu", Role: "owner"},
		{HubID: hubID, UserID: "u2", UserName: "Ava Li", Role: "admin"},
	}
	s.topicsByHub[hubID] = []model.Topic{{ID: "t1", HubID: hubID, Name: "Auth"}}
	s.topicCountsByHub[hubID] = map[string]int{"t1": 2}
	s.recentActivityByHub[hubID] = 6
	s.visits[hubID+":"+userID] = &model.HubVisit{
		UserID:         userID,
		HubID:          hubID,
		FirstVisitedAt: lastVisit,
		LastVisitedAt:  lastVisit,
		CreatedAt:      lastVisit,
		UpdatedAt:      lastVisit,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/"+hubID+"/summary", nil)
	req.SetPathValue("id", hubID)
	req = withTestIdentity(req, userID)
	req = req.WithContext(context.WithValue(req.Context(), timezoneKey, timezoneForBucket(t, model.HubTimeBucketMorning)))
	rec := httptest.NewRecorder()

	h.GetSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		t.Fatal(err)
	}
	var summary model.HubSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}

	if summary.Header.State != model.HubHeaderStateTeamActivity {
		t.Fatalf("expected team_activity state, got %s", summary.Header.State)
	}
	if summary.Stats.TeamActivity != 6 {
		t.Fatalf("expected team activity 6, got %d", summary.Stats.TeamActivity)
	}
	if summary.Header.GreetingKey != model.HubGreetingKeyTeamMorningA &&
		summary.Header.GreetingKey != model.HubGreetingKeyTeamMorningB {
		t.Fatalf("expected team morning greeting key, got %s", summary.Header.GreetingKey)
	}
	if summary.Header.GreetingParams["n"] != "6" {
		t.Fatalf("expected team activity count in params, got %#v", summary.Header.GreetingParams)
	}
}

func TestHubsRecordVisitPersistsHubVisit(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hubID := "11111111-1111-1111-1111-111111111111"
	userID := "u1"
	s.hubs[hubID] = &model.Hub{
		ID:      hubID,
		Name:    "Platform",
		Slug:    "platform",
		HubType: "team",
		OwnerID: userID,
	}
	s.roles[hubID+":"+userID] = "owner"

	req := httptest.NewRequest(http.MethodPost, "/v1/hubs/"+hubID+"/visit", bytes.NewBufferString(`{}`))
	req.SetPathValue("id", hubID)
	req = withTestIdentity(req, userID)
	rec := httptest.NewRecorder()

	h.RecordVisit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	visit, err := s.GetHubVisit(userID, hubID)
	if err != nil {
		t.Fatal(err)
	}
	if visit.LastVisitedAt.IsZero() {
		t.Fatalf("expected visit timestamp to be stored")
	}
}

// ── Addressed invite path + notification convergence ────────────────────

// seedInviteHub creates a team hub, makes the caller (owner) a member,
// and registers a lookupable invitee user. Returns the store and the
// canonical ids used by the invite-convergence tests below.
func seedInviteHub(t *testing.T) (s *hubsTestStore, hubID, ownerID, inviteeID, inviteeEmail string) {
	t.Helper()
	s = newHubsTestStore()
	hubID = "11111111-1111-1111-1111-111111111111"
	ownerID = "11111111-1111-1111-1111-111111111101"
	inviteeID = "11111111-1111-1111-1111-111111111102"
	inviteeEmail = "invitee@example.com"
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "Team", Slug: "team", HubType: "team", OwnerID: ownerID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubID, ownerID, "owner"); err != nil {
		t.Fatal(err)
	}
	s.addTestUser(&model.User{
		ID:    ownerID,
		Name:  "Owner",
		Email: "owner@example.com",
	})
	s.addTestUser(&model.User{
		ID:    inviteeID,
		Name:  "Invitee",
		Email: inviteeEmail,
	})
	return s, hubID, ownerID, inviteeID, inviteeEmail
}

func TestCreateInviteAddressedByEmailFiresNotification(t *testing.T) {
	s, hubID, ownerID, inviteeID, inviteeEmail := seedInviteHub(t)
	h := NewHubsHandler(s)

	body := []byte(`{"role":"contributor","invitee":{"email":"` + inviteeEmail + `"}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/hubs/"+hubID+"/invites", bytes.NewReader(body))
	req.SetPathValue("id", hubID)
	req = withTestIdentity(req, ownerID)
	rec := httptest.NewRecorder()

	h.CreateInvite(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(resp.Data)
	var invite model.HubInvite
	if err := json.Unmarshal(raw, &invite); err != nil {
		t.Fatal(err)
	}
	if invite.InviteeUserID == nil || *invite.InviteeUserID != inviteeID {
		t.Fatalf("expected invitee_user_id=%q, got %+v", inviteeID, invite.InviteeUserID)
	}
	// Exactly one hub_invite notification should have fanned out.
	var match *model.Notification
	for _, n := range s.notifications {
		if n.Kind == model.NotificationKindHubInvite && n.SourceID == invite.ID {
			match = n
			break
		}
	}
	if match == nil {
		t.Fatalf("expected a hub_invite notification, got %d total: %+v",
			len(s.notifications), s.notifications)
	}
	if match.RecipientUserID != inviteeID {
		t.Fatalf("expected notification recipient=%q, got %q", inviteeID, match.RecipientUserID)
	}
	if match.Audience != model.AudienceUser {
		t.Fatalf("expected audience=user, got %q", match.Audience)
	}
	if match.SourceKind != model.NotificationSourceHubInvite {
		t.Fatalf("expected source_kind=%q, got %q",
			model.NotificationSourceHubInvite, match.SourceKind)
	}
}

func TestCreateInviteAddressedByUnknownEmailCreatesEmailInvite(t *testing.T) {
	s, hubID, ownerID, _, _ := seedInviteHub(t)
	h := NewHubsHandler(s)

	// When the client passes an explicit `invitee` with an email that
	// does not resolve to an existing Memax user, the server creates
	// an email-only invite: invitee_email set, invitee_user_id nil,
	// no in-app notification (no user to deliver to). The email
	// delivery is handled separately by the enqueueEmail callback.
	body := []byte(`{"role":"contributor","invitee":{"email":"nobody@example.com"}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/hubs/"+hubID+"/invites", bytes.NewReader(body))
	req.SetPathValue("id", hubID)
	req = withTestIdentity(req, ownerID)
	rec := httptest.NewRecorder()

	h.CreateInvite(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(resp.Data)
	var invite model.HubInvite
	if err := json.Unmarshal(raw, &invite); err != nil {
		t.Fatal(err)
	}

	// invitee_user_id must be nil — email did not resolve.
	if invite.InviteeUserID != nil {
		t.Fatalf("expected nil invitee_user_id for unknown email, got %+v", invite.InviteeUserID)
	}
	// invitee_email must be set to the input email.
	if invite.InviteeEmail == nil || *invite.InviteeEmail != "nobody@example.com" {
		t.Fatalf("expected invitee_email=nobody@example.com, got %+v", invite.InviteeEmail)
	}
	// Exactly one invite row should exist.
	if len(s.invites) != 1 {
		t.Fatalf("expected 1 invite row, got %d", len(s.invites))
	}
	// No in-app notification — there is no Memax user to deliver to.
	if len(s.notifications) != 0 {
		t.Fatalf("expected no notification for unknown-email invite, got %d", len(s.notifications))
	}
}

func TestCreateInviteAddressedToExistingMemberReturns409(t *testing.T) {
	s, hubID, ownerID, inviteeID, inviteeEmail := seedInviteHub(t)
	if err := s.AddHubMember(hubID, inviteeID, "contributor"); err != nil {
		t.Fatal(err)
	}
	h := NewHubsHandler(s)

	body := []byte(`{"role":"contributor","invitee":{"email":"` + inviteeEmail + `"}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/hubs/"+hubID+"/invites", bytes.NewReader(body))
	req.SetPathValue("id", hubID)
	req = withTestIdentity(req, ownerID)
	rec := httptest.NewRecorder()

	h.CreateInvite(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invitee_already_member") {
		t.Fatalf("expected invitee_already_member error, got %s", rec.Body.String())
	}
}

func TestCreateInviteWithoutInviteePreservesLegacyLinkFlow(t *testing.T) {
	s, hubID, ownerID, _, _ := seedInviteHub(t)
	h := NewHubsHandler(s)

	body := []byte(`{"role":"contributor"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/hubs/"+hubID+"/invites", bytes.NewReader(body))
	req.SetPathValue("id", hubID)
	req = withTestIdentity(req, ownerID)
	rec := httptest.NewRecorder()

	h.CreateInvite(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp model.ApiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(resp.Data)
	var invite model.HubInvite
	if err := json.Unmarshal(raw, &invite); err != nil {
		t.Fatal(err)
	}
	if invite.InviteeUserID != nil {
		t.Fatalf("expected nil invitee_user_id for legacy path, got %v", *invite.InviteeUserID)
	}
	if invite.Token == "" {
		t.Fatalf("expected a token on the link invite")
	}
	if len(s.notifications) != 0 {
		t.Fatalf("expected no notification for legacy link invite, got %d", len(s.notifications))
	}
}

func TestAcceptInviteTokenPathResolvesNotificationAndEmitsJoinReceipt(t *testing.T) {
	s, hubID, ownerID, inviteeID, _ := seedInviteHub(t)
	h := NewHubsHandler(s)

	// Addressed invite with a matching pending hub_invite notification.
	inviteID := "22222222-2222-2222-2222-222222222202"
	inviteeIDCopy := inviteeID
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:            inviteID,
		HubID:         hubID,
		Token:         "tok-addressed",
		InvitedBy:     ownerID,
		Role:          "contributor",
		ExpiresAt:     time.Now().Add(24 * time.Hour),
		CreatedAt:     time.Now(),
		InviteeUserID: &inviteeIDCopy,
	}); err != nil {
		t.Fatal(err)
	}
	pendingNotifID := "33333333-3333-3333-3333-333333333303"
	if err := s.CreateNotification(&model.Notification{
		ID:              pendingNotifID,
		Audience:        model.AudienceUser,
		RecipientUserID: inviteeID,
		HubID:           hubID,
		Kind:            model.NotificationKindHubInvite,
		Status:          model.NotificationStatusPending,
		Priority:        1,
		SourceKind:      model.NotificationSourceHubInvite,
		SourceID:        inviteID,
		CreatedAt:       time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/invites/tok-addressed/accept", nil)
	req.SetPathValue("token", "tok-addressed")
	req = withTestIdentity(req, inviteeID)
	rec := httptest.NewRecorder()

	h.AcceptInvite(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// The pending hub_invite notification should be resolved=accepted.
	pending := s.notifications[pendingNotifID]
	if pending == nil {
		t.Fatalf("pending notification row disappeared")
	}
	if pending.Status != model.NotificationStatusResolved {
		t.Fatalf("expected notification status=resolved, got %q", pending.Status)
	}
	if pending.Resolution != model.ResolutionAccepted {
		t.Fatalf("expected resolution=accepted, got %q", pending.Resolution)
	}

	// A hub_member_joined receipt should fan out to the hub owner.
	var joined *model.Notification
	for _, n := range s.notifications {
		if n.Kind == model.NotificationKindHubMemberJoined {
			joined = n
			break
		}
	}
	if joined == nil {
		t.Fatalf("expected hub_member_joined receipt, got none")
	}
	if joined.RecipientUserID != ownerID {
		t.Fatalf("expected join receipt recipient=%q, got %q", ownerID, joined.RecipientUserID)
	}
	if joined.SourceKind != model.NotificationSourceHubMember {
		t.Fatalf("expected source_kind=%q, got %q",
			model.NotificationSourceHubMember, joined.SourceKind)
	}
	if joined.SourceID != inviteID {
		t.Fatalf("expected source_id=%q (invite id, not member id), got %q", inviteID, joined.SourceID)
	}

	// The invitee should ALSO get their own self-oriented accept
	// receipt ("You joined X"). Without this the invitee has no
	// durable positive feedback after clicking accept — the decision
	// row just disappears.
	var selfReceipt *model.Notification
	for _, n := range s.notifications {
		if n.Kind == model.NotificationKindHubInviteAccepted {
			selfReceipt = n
			break
		}
	}
	if selfReceipt == nil {
		t.Fatalf("expected hub_invite_accepted self-receipt, got none")
	}
	if selfReceipt.RecipientUserID != inviteeID {
		t.Fatalf("expected self-receipt recipient=%q (invitee), got %q",
			inviteeID, selfReceipt.RecipientUserID)
	}
	if selfReceipt.SourceKind != model.NotificationSourceHubInviteAccepted {
		t.Fatalf("expected source_kind=%q, got %q",
			model.NotificationSourceHubInviteAccepted, selfReceipt.SourceKind)
	}
	if selfReceipt.SourceID != inviteID {
		t.Fatalf("expected self-receipt source_id=%q, got %q",
			inviteID, selfReceipt.SourceID)
	}

	// Regression guard: the owner-only hub_member_joined receipt
	// must NEVER be routed to the invitee. The two receipts exist
	// precisely so each side reads their own view of the same
	// event.
	for _, n := range s.notifications {
		if n.Kind == model.NotificationKindHubMemberJoined &&
			n.RecipientUserID == inviteeID {
			t.Fatalf("hub_member_joined must not target the invitee, got %+v", n)
		}
	}
}

func TestAcceptInviteLegacyLinkStillEmitsJoinReceipt(t *testing.T) {
	s, hubID, ownerID, _, _ := seedInviteHub(t)
	h := NewHubsHandler(s)

	// Legacy link-only invite — no InviteeUserID, no pending notification.
	joinerID := "11111111-1111-1111-1111-111111111103"
	s.addTestUser(&model.User{ID: joinerID, Name: "Joiner", Email: "joiner@example.com"})

	if err := s.CreateHubInvite(&model.HubInvite{
		ID:        "22222222-2222-2222-2222-222222222203",
		HubID:     hubID,
		Token:     "tok-legacy",
		InvitedBy: ownerID,
		Role:      "contributor",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/invites/tok-legacy/accept", nil)
	req.SetPathValue("token", "tok-legacy")
	req = withTestIdentity(req, joinerID)
	rec := httptest.NewRecorder()

	h.AcceptInvite(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Legacy path still fires hub_member_joined so the owner gets a receipt.
	var joined *model.Notification
	for _, n := range s.notifications {
		if n.Kind == model.NotificationKindHubMemberJoined {
			joined = n
			break
		}
	}
	if joined == nil {
		t.Fatalf("expected hub_member_joined even for legacy link invite, got none")
	}
	if joined.RecipientUserID != ownerID {
		t.Fatalf("expected recipient=%q, got %q", ownerID, joined.RecipientUserID)
	}
}

func TestAcceptInviteTokenPathRefusesWrongUserOnAddressedInvite(t *testing.T) {
	s, hubID, ownerID, inviteeID, _ := seedInviteHub(t)
	h := NewHubsHandler(s)

	// Addressed invite targeted at inviteeID — the token exists but
	// is only valid for the specific invitee.
	inviteeIDCopy := inviteeID
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:            "22222222-2222-2222-2222-222222222205",
		HubID:         hubID,
		Token:         "tok-addressed-leak",
		InvitedBy:     ownerID,
		Role:          "contributor",
		ExpiresAt:     time.Now().Add(24 * time.Hour),
		CreatedAt:     time.Now(),
		InviteeUserID: &inviteeIDCopy,
	}); err != nil {
		t.Fatal(err)
	}

	// A third user holding a leaked copy of the token tries to
	// accept the addressed invite via the legacy /v1/invites/{token}
	// path. This must be refused even though the token is valid,
	// otherwise addressed-invite ownership is only enforced on the
	// by-id path and any leaked token defeats it.
	attackerID := "11111111-1111-1111-1111-1111111111ff"
	s.addTestUser(&model.User{ID: attackerID, Name: "Attacker", Email: "attacker@example.com"})

	req := httptest.NewRequest(http.MethodPost, "/v1/invites/tok-addressed-leak/accept", nil)
	req.SetPathValue("token", "tok-addressed-leak")
	req = withTestIdentity(req, attackerID)
	rec := httptest.NewRecorder()

	h.AcceptInvite(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "wrong_invitee") {
		t.Fatalf("expected wrong_invitee error, got %s", rec.Body.String())
	}
	// Membership must not have been written.
	if role, _ := s.GetHubMemberRole(hubID, attackerID); role != "" {
		t.Fatalf("attacker should not be a hub member, got role=%q", role)
	}
	// No join receipt should have fired to the owner.
	for _, n := range s.notifications {
		if n.Kind == model.NotificationKindHubMemberJoined {
			t.Fatalf("refused accept should not emit hub_member_joined, got %+v", n)
		}
	}
}

func TestAcceptInviteTokenPathAllowsAddressedInviteeOnSameToken(t *testing.T) {
	s, hubID, ownerID, inviteeID, _ := seedInviteHub(t)
	h := NewHubsHandler(s)

	// Same scenario as above, but the addressed invitee themself
	// follows the token link (common when the invitee opens the
	// emailed URL rather than navigating to /inbox first).
	inviteeIDCopy := inviteeID
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:            "22222222-2222-2222-2222-222222222206",
		HubID:         hubID,
		Token:         "tok-addressed-match",
		InvitedBy:     ownerID,
		Role:          "contributor",
		ExpiresAt:     time.Now().Add(24 * time.Hour),
		CreatedAt:     time.Now(),
		InviteeUserID: &inviteeIDCopy,
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/invites/tok-addressed-match/accept", nil)
	req.SetPathValue("token", "tok-addressed-match")
	req = withTestIdentity(req, inviteeID)
	rec := httptest.NewRecorder()

	h.AcceptInvite(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if role, _ := s.GetHubMemberRole(hubID, inviteeID); role == "" {
		t.Fatalf("invitee should be a hub member after accept")
	}
}

func TestRegenerateInvitePreservesAddressedInvitee(t *testing.T) {
	s, hubID, ownerID, inviteeID, _ := seedInviteHub(t)
	h := NewHubsHandler(s)

	// Create an addressed invite first (via the handler so the
	// create-side notification fires too).
	createBody := []byte(`{"role":"contributor","invitee":{"user_id":"` + inviteeID + `"}}`)
	createReq := httptest.NewRequest(http.MethodPost, "/v1/hubs/"+hubID+"/invites", bytes.NewReader(createBody))
	createReq.SetPathValue("id", hubID)
	createReq = withTestIdentity(createReq, ownerID)
	createRec := httptest.NewRecorder()
	h.CreateInvite(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("seed create failed: %d %s", createRec.Code, createRec.Body.String())
	}
	var createResp model.ApiResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatal(err)
	}
	createRaw, _ := json.Marshal(createResp.Data)
	var original model.HubInvite
	if err := json.Unmarshal(createRaw, &original); err != nil {
		t.Fatal(err)
	}
	if original.InviteeUserID == nil {
		t.Fatalf("seed invite should be addressed, got nil invitee_user_id")
	}

	// Regenerate the invite — this should mint a new token + id but
	// preserve the addressed recipient so the invitee keeps getting
	// a hub_invite notification and no one else can accept.
	regenReq := httptest.NewRequest(http.MethodPost,
		"/v1/hubs/"+hubID+"/invites/"+original.ID+"/regenerate", nil)
	regenReq.SetPathValue("id", hubID)
	regenReq.SetPathValue("invite_id", original.ID)
	regenReq = withTestIdentity(regenReq, ownerID)
	regenRec := httptest.NewRecorder()
	h.RegenerateInvite(regenRec, regenReq)

	if regenRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", regenRec.Code, regenRec.Body.String())
	}
	var regenResp model.ApiResponse
	if err := json.Unmarshal(regenRec.Body.Bytes(), &regenResp); err != nil {
		t.Fatal(err)
	}
	regenRaw, _ := json.Marshal(regenResp.Data)
	var replacement model.HubInvite
	if err := json.Unmarshal(regenRaw, &replacement); err != nil {
		t.Fatal(err)
	}

	if replacement.ID == original.ID {
		t.Fatalf("replacement should have a new id, got same as original")
	}
	if replacement.Token == original.Token {
		t.Fatalf("replacement should have a new token, got same as original")
	}
	if replacement.InviteeUserID == nil || *replacement.InviteeUserID != inviteeID {
		t.Fatalf("replacement should preserve addressed invitee=%q, got %+v",
			inviteeID, replacement.InviteeUserID)
	}

	// After regenerate: the invitee should see EXACTLY ONE pending
	// hub_invite notification — the replacement. The original
	// notification must be resolved=dismissed because RegenerateInvite
	// revokes the old invite row and the revoke path now converges
	// the notification. Two pending rows would be a duplicate-state
	// bug: the first would point at a dead (revoked) invite id and
	// fail with invite_revoked the moment the user clicked accept.
	pendingCount := 0
	resolvedCount := 0
	var resolvedReason model.NotificationResolution
	for _, n := range s.notifications {
		if n.Kind != model.NotificationKindHubInvite {
			continue
		}
		if n.RecipientUserID != inviteeID {
			t.Fatalf("hub_invite notif should be addressed to invitee, got %q", n.RecipientUserID)
		}
		switch n.Status {
		case model.NotificationStatusPending:
			pendingCount++
			if n.SourceID != replacement.ID {
				t.Fatalf("pending notification should point at the replacement invite %q, got %q",
					replacement.ID, n.SourceID)
			}
		case model.NotificationStatusResolved:
			resolvedCount++
			resolvedReason = n.Resolution
			if n.SourceID != original.ID {
				t.Fatalf("resolved notification should point at the original invite %q, got %q",
					original.ID, n.SourceID)
			}
		}
	}
	if pendingCount != 1 {
		t.Fatalf("expected 1 pending hub_invite notification (new row), got %d", pendingCount)
	}
	if resolvedCount != 1 {
		t.Fatalf("expected 1 resolved hub_invite notification (revoked original), got %d", resolvedCount)
	}
	if resolvedReason != model.ResolutionDismissed {
		t.Fatalf("expected resolved original to carry resolution=dismissed, got %q", resolvedReason)
	}

	// Wrong-user accept on the replacement token is still refused.
	attackerID := "11111111-1111-1111-1111-1111111111fe"
	s.addTestUser(&model.User{ID: attackerID, Name: "Attacker", Email: "attacker2@example.com"})
	attackReq := httptest.NewRequest(http.MethodPost, "/v1/invites/"+replacement.Token+"/accept", nil)
	attackReq.SetPathValue("token", replacement.Token)
	attackReq = withTestIdentity(attackReq, attackerID)
	attackRec := httptest.NewRecorder()
	h.AcceptInvite(attackRec, attackReq)
	if attackRec.Code != http.StatusForbidden {
		t.Fatalf("expected attacker to be refused (403), got %d: %s",
			attackRec.Code, attackRec.Body.String())
	}
}

func TestAcceptInviteOwnerJoiningOwnInviteDoesNotSelfNotify(t *testing.T) {
	s, hubID, ownerID, _, _ := seedInviteHub(t)
	h := NewHubsHandler(s)

	// Pathological: the owner somehow accepts their own legacy invite
	// from another session. The join should succeed but not produce a
	// self-notification.
	// Remove the owner from member map so AcceptHubInvite doesn't error.
	delete(s.roles, hubID+":"+ownerID)

	if err := s.CreateHubInvite(&model.HubInvite{
		ID:        "22222222-2222-2222-2222-222222222204",
		HubID:     hubID,
		Token:     "tok-selfjoin",
		InvitedBy: ownerID,
		Role:      "contributor",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/invites/tok-selfjoin/accept", nil)
	req.SetPathValue("token", "tok-selfjoin")
	req = withTestIdentity(req, ownerID)
	rec := httptest.NewRecorder()

	h.AcceptInvite(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, n := range s.notifications {
		if n.Kind == model.NotificationKindHubMemberJoined {
			t.Fatalf("owner should not receive a self-join notification, got %+v", n)
		}
	}
}

// ── Hub ownership transfer + notification convergence ───────────────────

// seedTransferHub creates a team hub with an owner and a second
// member (the transfer target). Returns the test store and the
// canonical ids used by the transfer-convergence tests below.
func seedTransferHub(t *testing.T) (s *hubsTestStore, hubID, ownerID, targetID string) {
	t.Helper()
	s = newHubsTestStore()
	hubID = "11111111-1111-1111-1111-111111111111"
	ownerID = "11111111-1111-1111-1111-111111111101"
	targetID = "11111111-1111-1111-1111-111111111102"
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "Team", Slug: "team", HubType: "team", OwnerID: ownerID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubID, ownerID, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubID, targetID, "admin"); err != nil {
		t.Fatal(err)
	}
	s.addTestUser(&model.User{ID: ownerID, Name: "Owner", Email: "owner@example.com"})
	s.addTestUser(&model.User{ID: targetID, Name: "Target", Email: "target@example.com"})
	return s, hubID, ownerID, targetID
}

func findNotificationByKind(s *hubsTestStore, kind string) *model.Notification {
	for _, n := range s.notifications {
		if n.Kind == kind {
			return n
		}
	}
	return nil
}

func findNotificationsByKind(s *hubsTestStore, kind string) []*model.Notification {
	out := make([]*model.Notification, 0)
	for _, n := range s.notifications {
		if n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}

func TestCreateOwnershipTransferFiresNotificationToTarget(t *testing.T) {
	s, hubID, ownerID, targetID := seedTransferHub(t)
	h := NewHubsHandler(s)

	body := []byte(`{"target_user_id":"` + targetID + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/hubs/"+hubID+"/ownership-transfer", bytes.NewReader(body))
	req.SetPathValue("id", hubID)
	req = withTestIdentity(req, ownerID)
	rec := httptest.NewRecorder()

	h.CreateOwnershipTransfer(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	notif := findNotificationByKind(s, model.NotificationKindHubOwnershipTransfer)
	if notif == nil {
		t.Fatalf("expected hub_ownership_transfer notification, got none")
	}
	if notif.RecipientUserID != targetID {
		t.Fatalf("expected recipient=%q, got %q", targetID, notif.RecipientUserID)
	}
	if notif.Audience != model.AudienceUser {
		t.Fatalf("expected audience=user, got %q", notif.Audience)
	}
	if notif.SourceKind != model.NotificationSourceHubOwnershipTransfer {
		t.Fatalf("expected source_kind=%q, got %q",
			model.NotificationSourceHubOwnershipTransfer, notif.SourceKind)
	}
}

func TestAcceptOwnershipTransferSettingsPathEmitsReceiptAndResolvesNotification(t *testing.T) {
	s, hubID, ownerID, targetID := seedTransferHub(t)
	h := NewHubsHandler(s)

	// Seed a pending transfer + its inbox notification (simulating
	// what the create flow would have done).
	transferID := "22222222-2222-2222-2222-222222222211"
	if err := s.CreateHubOwnershipTransfer(&model.HubOwnershipTransfer{
		ID:           transferID,
		HubID:        hubID,
		InitiatedBy:  ownerID,
		TargetUserID: targetID,
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	pendingID := "33333333-3333-3333-3333-333333333311"
	if err := s.CreateNotification(&model.Notification{
		ID:              pendingID,
		Audience:        model.AudienceUser,
		RecipientUserID: targetID,
		HubID:           hubID,
		Kind:            model.NotificationKindHubOwnershipTransfer,
		Status:          model.NotificationStatusPending,
		Priority:        2,
		SourceKind:      model.NotificationSourceHubOwnershipTransfer,
		SourceID:        transferID,
		CreatedAt:       time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	// Target accepts via the legacy settings endpoint.
	req := httptest.NewRequest(http.MethodPost,
		"/v1/hubs/"+hubID+"/ownership-transfer/"+transferID+"/accept", nil)
	req.SetPathValue("id", hubID)
	req.SetPathValue("transfer_id", transferID)
	req = withTestIdentity(req, targetID)
	rec := httptest.NewRecorder()

	h.AcceptOwnershipTransfer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// Role swap happened.
	if role, _ := s.GetHubMemberRole(hubID, targetID); role != "owner" {
		t.Fatalf("expected target=owner, got %q", role)
	}
	if role, _ := s.GetHubMemberRole(hubID, ownerID); role != "admin" {
		t.Fatalf("expected previous owner=admin, got %q", role)
	}
	// Pending inbox notification is resolved=accepted.
	pending := s.notifications[pendingID]
	if pending == nil {
		t.Fatalf("pending notification row disappeared")
	}
	if pending.Status != model.NotificationStatusResolved {
		t.Fatalf("expected notification status=resolved, got %q", pending.Status)
	}
	if pending.Resolution != model.ResolutionAccepted {
		t.Fatalf("expected resolution=accepted, got %q", pending.Resolution)
	}
	// Both sides get a durable ownership-changed receipt.
	receipts := findNotificationsByKind(s, model.NotificationKindHubOwnershipTransferred)
	if len(receipts) != 2 {
		t.Fatalf("expected 2 hub_ownership_transferred receipts, got %d", len(receipts))
	}
	seenRecipients := map[string]string{}
	for _, receipt := range receipts {
		seenRecipients[receipt.RecipientUserID] = receipt.SourceID
	}
	if seenRecipients[ownerID] != transferID+":old_owner" {
		t.Fatalf("expected old-owner receipt source_id=%q, got %q",
			transferID+":old_owner", seenRecipients[ownerID])
	}
	if seenRecipients[targetID] != transferID+":new_owner" {
		t.Fatalf("expected new-owner receipt source_id=%q, got %q",
			transferID+":new_owner", seenRecipients[targetID])
	}
}

func TestCancelOwnershipTransferByOwnerResolvesTargetNotification(t *testing.T) {
	s, hubID, ownerID, targetID := seedTransferHub(t)
	h := NewHubsHandler(s)

	transferID := "22222222-2222-2222-2222-222222222212"
	if err := s.CreateHubOwnershipTransfer(&model.HubOwnershipTransfer{
		ID:           transferID,
		HubID:        hubID,
		InitiatedBy:  ownerID,
		TargetUserID: targetID,
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	pendingID := "33333333-3333-3333-3333-333333333312"
	if err := s.CreateNotification(&model.Notification{
		ID:              pendingID,
		Audience:        model.AudienceUser,
		RecipientUserID: targetID,
		HubID:           hubID,
		Kind:            model.NotificationKindHubOwnershipTransfer,
		Status:          model.NotificationStatusPending,
		Priority:        2,
		SourceKind:      model.NotificationSourceHubOwnershipTransfer,
		SourceID:        transferID,
		CreatedAt:       time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	// Owner cancels from settings.
	req := httptest.NewRequest(http.MethodPost,
		"/v1/hubs/"+hubID+"/ownership-transfer/"+transferID+"/cancel", nil)
	req.SetPathValue("id", hubID)
	req.SetPathValue("transfer_id", transferID)
	req = withTestIdentity(req, ownerID)
	rec := httptest.NewRecorder()

	h.CancelOwnershipTransfer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	pending := s.notifications[pendingID]
	if pending == nil {
		t.Fatalf("pending notification row disappeared")
	}
	if pending.Status != model.NotificationStatusResolved {
		t.Fatalf("expected notification status=resolved, got %q", pending.Status)
	}
	if pending.Resolution != model.ResolutionDismissed {
		t.Fatalf("expected resolution=dismissed (cancelled), got %q", pending.Resolution)
	}
	// No receipt — cancel does not fire hub_ownership_transferred.
	if receipt := findNotificationByKind(s, model.NotificationKindHubOwnershipTransferred); receipt != nil {
		t.Fatalf("cancel should not emit hub_ownership_transferred, got %+v", receipt)
	}
}

// Smoke test via NotificationsHandler: the notification-native
// /resolve accept path must also run the transfer + receipt emit.
func TestNotificationResolveAcceptHubOwnershipTransfer(t *testing.T) {
	s, hubID, ownerID, targetID := seedTransferHub(t)

	// Seed transfer + the inbox row addressed to the target.
	transferID := "22222222-2222-2222-2222-222222222213"
	if err := s.CreateHubOwnershipTransfer(&model.HubOwnershipTransfer{
		ID:           transferID,
		HubID:        hubID,
		InitiatedBy:  ownerID,
		TargetUserID: targetID,
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	notifID := "33333333-3333-3333-3333-333333333313"
	if err := s.CreateNotification(&model.Notification{
		ID:              notifID,
		Audience:        model.AudienceUser,
		RecipientUserID: targetID,
		HubID:           hubID,
		Kind:            model.NotificationKindHubOwnershipTransfer,
		Status:          model.NotificationStatusPending,
		Priority:        2,
		SourceKind:      model.NotificationSourceHubOwnershipTransfer,
		SourceID:        transferID,
		CreatedAt:       time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	// Directly invoke the dispatch function via the notifications
	// handler. We don't build a real /resolve HTTP call because the
	// test store's GetNotification isn't wired for the full
	// visibility plumbing; the unit under test here is the dispatch
	// behavior + side-effects fan-out.
	nh := NewNotificationsHandler(s, nil)
	notifRow := &model.Notification{
		ID:         notifID,
		Kind:       model.NotificationKindHubOwnershipTransfer,
		SourceKind: model.NotificationSourceHubOwnershipTransfer,
		SourceID:   transferID,
	}
	if err := nh.dispatchHubOwnershipTransferAction(context.Background(), notifRow, targetID, "accept"); err != nil {
		t.Fatalf("dispatch accept failed: %v", err)
	}

	if role, _ := s.GetHubMemberRole(hubID, targetID); role != "owner" {
		t.Fatalf("expected target=owner after accept, got %q", role)
	}
	receipts := findNotificationsByKind(s, model.NotificationKindHubOwnershipTransferred)
	if len(receipts) != 2 {
		t.Fatalf("expected 2 hub_ownership_transferred receipts, got %d", len(receipts))
	}
	seenRecipients := map[string]bool{}
	for _, receipt := range receipts {
		seenRecipients[receipt.RecipientUserID] = true
	}
	if !seenRecipients[ownerID] {
		t.Fatalf("expected old owner to receive receipt")
	}
	if !seenRecipients[targetID] {
		t.Fatalf("expected new owner to receive self receipt")
	}
}

// Wrong-user accept via the notification dispatch is refused even
// though the attacker holds a notification id — the dispatch
// verifies target_user_id matches the caller before touching the
// store.
func TestNotificationResolveAcceptHubOwnershipTransferWrongUserRefused(t *testing.T) {
	s, hubID, ownerID, targetID := seedTransferHub(t)
	_ = targetID
	attackerID := "11111111-1111-1111-1111-1111111111fe"
	s.addTestUser(&model.User{ID: attackerID, Name: "Attacker", Email: "attacker@example.com"})
	if err := s.AddHubMember(hubID, attackerID, "contributor"); err != nil {
		t.Fatal(err)
	}

	transferID := "22222222-2222-2222-2222-222222222214"
	if err := s.CreateHubOwnershipTransfer(&model.HubOwnershipTransfer{
		ID:           transferID,
		HubID:        hubID,
		InitiatedBy:  ownerID,
		TargetUserID: targetID,
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	nh := NewNotificationsHandler(s, nil)
	notifRow := &model.Notification{
		ID:         "dummy",
		Kind:       model.NotificationKindHubOwnershipTransfer,
		SourceKind: model.NotificationSourceHubOwnershipTransfer,
		SourceID:   transferID,
	}
	err := nh.dispatchHubOwnershipTransferAction(context.Background(), notifRow, attackerID, "accept")
	if err == nil {
		t.Fatalf("expected attacker accept to be refused, got nil")
	}
	// Ownership should be unchanged.
	if role, _ := s.GetHubMemberRole(hubID, ownerID); role != "owner" {
		t.Fatalf("expected previous owner still owner, got %q", role)
	}
	if role, _ := s.GetHubMemberRole(hubID, attackerID); role != "contributor" {
		t.Fatalf("expected attacker role unchanged, got %q", role)
	}
}

// ── Review-follow-up fixes ──────────────────────────────────────────────

// Regression: CancelOwnershipTransfer used to return 200 "canceled"
// even when the store UPDATE affected zero rows (already accepted
// or already cancelled), because the store swallowed RowsAffected.
// A follow-up cancel on an accepted transfer is now a hard 409.
func TestCancelOwnershipTransferOnAlreadyAcceptedReturns409(t *testing.T) {
	s, hubID, ownerID, targetID := seedTransferHub(t)
	h := NewHubsHandler(s)

	transferID := "22222222-2222-2222-2222-222222222221"
	acceptedAt := time.Now().Add(-1 * time.Minute)
	if err := s.CreateHubOwnershipTransfer(&model.HubOwnershipTransfer{
		ID:           transferID,
		HubID:        hubID,
		InitiatedBy:  ownerID,
		TargetUserID: targetID,
		AcceptedAt:   &acceptedAt, // already accepted
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost,
		"/v1/hubs/"+hubID+"/ownership-transfer/"+transferID+"/cancel", nil)
	req.SetPathValue("id", hubID)
	req.SetPathValue("transfer_id", transferID)
	req = withTestIdentity(req, ownerID)
	rec := httptest.NewRecorder()

	h.CancelOwnershipTransfer(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-200 on cancel of already-accepted transfer, got 200: %s", rec.Body.String())
	}
	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("expected 4xx on already-accepted cancel, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Regression: the target clicking "Decline" in settings used to
// resolve the notification as `dismissed` while the inbox decline
// path resolved the same kind as `declined`. Both entry points
// must now converge on `declined` so history / filters do not
// diverge by entry point.
func TestCancelOwnershipTransferByTargetResolvesAsDeclined(t *testing.T) {
	s, hubID, ownerID, targetID := seedTransferHub(t)
	h := NewHubsHandler(s)

	transferID := "22222222-2222-2222-2222-222222222222"
	if err := s.CreateHubOwnershipTransfer(&model.HubOwnershipTransfer{
		ID:           transferID,
		HubID:        hubID,
		InitiatedBy:  ownerID,
		TargetUserID: targetID,
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	pendingID := "33333333-3333-3333-3333-333333333322"
	if err := s.CreateNotification(&model.Notification{
		ID:              pendingID,
		Audience:        model.AudienceUser,
		RecipientUserID: targetID,
		HubID:           hubID,
		Kind:            model.NotificationKindHubOwnershipTransfer,
		Status:          model.NotificationStatusPending,
		Priority:        2,
		SourceKind:      model.NotificationSourceHubOwnershipTransfer,
		SourceID:        transferID,
		CreatedAt:       time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost,
		"/v1/hubs/"+hubID+"/ownership-transfer/"+transferID+"/cancel", nil)
	req.SetPathValue("id", hubID)
	req.SetPathValue("transfer_id", transferID)
	// Target hits the cancel endpoint (settings labels this "Decline").
	req = withTestIdentity(req, targetID)
	rec := httptest.NewRecorder()

	h.CancelOwnershipTransfer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	pending := s.notifications[pendingID]
	if pending == nil {
		t.Fatalf("notification row disappeared")
	}
	if pending.Status != model.NotificationStatusResolved {
		t.Fatalf("expected status=resolved, got %q", pending.Status)
	}
	if pending.Resolution != model.ResolutionDeclined {
		t.Fatalf("target-initiated decline should persist as declined, got %q",
			pending.Resolution)
	}
}

// Regression: the owner hitting cancel (not the target) should
// still resolve the target's notification, but as `dismissed`
// because the target never made a choice. This is the
// initiator-cancel half of the decline/dismiss split.
func TestCancelOwnershipTransferByOwnerResolvesAsDismissed(t *testing.T) {
	s, hubID, ownerID, targetID := seedTransferHub(t)
	h := NewHubsHandler(s)

	transferID := "22222222-2222-2222-2222-222222222223"
	if err := s.CreateHubOwnershipTransfer(&model.HubOwnershipTransfer{
		ID:           transferID,
		HubID:        hubID,
		InitiatedBy:  ownerID,
		TargetUserID: targetID,
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
		CreatedAt:    time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	pendingID := "33333333-3333-3333-3333-333333333323"
	if err := s.CreateNotification(&model.Notification{
		ID:              pendingID,
		Audience:        model.AudienceUser,
		RecipientUserID: targetID,
		HubID:           hubID,
		Kind:            model.NotificationKindHubOwnershipTransfer,
		Status:          model.NotificationStatusPending,
		Priority:        2,
		SourceKind:      model.NotificationSourceHubOwnershipTransfer,
		SourceID:        transferID,
		CreatedAt:       time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost,
		"/v1/hubs/"+hubID+"/ownership-transfer/"+transferID+"/cancel", nil)
	req.SetPathValue("id", hubID)
	req.SetPathValue("transfer_id", transferID)
	req = withTestIdentity(req, ownerID)
	rec := httptest.NewRecorder()

	h.CancelOwnershipTransfer(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	pending := s.notifications[pendingID]
	if pending == nil {
		t.Fatalf("notification row disappeared")
	}
	if pending.Resolution != model.ResolutionDismissed {
		t.Fatalf("owner-initiated cancel should persist as dismissed, got %q",
			pending.Resolution)
	}
}

// Decline via the notification-native /resolve dispatch must fan
// out two outcome receipts: one to the inviter telling them their
// invite was declined, and one to the invitee themself confirming
// their own action. Both keyed on the invite id via distinct
// source_kinds so neither clashes against notifications_source_unique.
func TestNotificationResolveDeclineHubInviteEmitsOutcomeReceipts(t *testing.T) {
	s, hubID, ownerID, inviteeID, _ := seedInviteHub(t)

	// Seed an addressed invite so GetHubInvite + DeclineHubInviteByID
	// work against the same row.
	inviteeIDCopy := inviteeID
	inviteID := "22222222-2222-2222-2222-2222222222de"
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:            inviteID,
		HubID:         hubID,
		Token:         "tok-decline-dispatch",
		InvitedBy:     ownerID,
		Role:          "contributor",
		ExpiresAt:     time.Now().Add(24 * time.Hour),
		CreatedAt:     time.Now(),
		InviteeUserID: &inviteeIDCopy,
	}); err != nil {
		t.Fatal(err)
	}

	nh := NewNotificationsHandler(s, nil)
	notifRow := &model.Notification{
		ID:         "dummy",
		Kind:       model.NotificationKindHubInvite,
		SourceKind: model.NotificationSourceHubInvite,
		SourceID:   inviteID,
	}
	if err := nh.dispatchHubInviteAction(context.Background(), notifRow, inviteeID, "decline"); err != nil {
		t.Fatalf("dispatch decline failed: %v", err)
	}

	// Inviter-side receipt → hub_invite_declined, recipient = inviter.
	var inviterReceipt *model.Notification
	for _, n := range s.notifications {
		if n.Kind == model.NotificationKindHubInviteDeclined {
			inviterReceipt = n
			break
		}
	}
	if inviterReceipt == nil {
		t.Fatalf("expected hub_invite_declined receipt to inviter, got none")
	}
	if inviterReceipt.RecipientUserID != ownerID {
		t.Fatalf("expected inviter receipt recipient=%q (invited_by), got %q",
			ownerID, inviterReceipt.RecipientUserID)
	}
	if inviterReceipt.SourceKind != model.NotificationSourceHubInviteDeclined {
		t.Fatalf("expected source_kind=%q, got %q",
			model.NotificationSourceHubInviteDeclined, inviterReceipt.SourceKind)
	}
	if inviterReceipt.SourceID != inviteID {
		t.Fatalf("expected inviter receipt source_id=%q, got %q",
			inviteID, inviterReceipt.SourceID)
	}

	// Invitee-side self-receipt → hub_invite_declined_by_you,
	// recipient = invitee.
	var selfReceipt *model.Notification
	for _, n := range s.notifications {
		if n.Kind == model.NotificationKindHubInviteDeclinedByYou {
			selfReceipt = n
			break
		}
	}
	if selfReceipt == nil {
		t.Fatalf("expected hub_invite_declined_by_you self-receipt, got none")
	}
	if selfReceipt.RecipientUserID != inviteeID {
		t.Fatalf("expected self-receipt recipient=%q (invitee), got %q",
			inviteeID, selfReceipt.RecipientUserID)
	}
	if selfReceipt.SourceKind != model.NotificationSourceHubInviteDeclinedByYou {
		t.Fatalf("expected source_kind=%q, got %q",
			model.NotificationSourceHubInviteDeclinedByYou, selfReceipt.SourceKind)
	}

	// Regression: the inviter's receipt must NOT be routed to the
	// invitee (and vice versa).
	for _, n := range s.notifications {
		if n.Kind == model.NotificationKindHubInviteDeclined &&
			n.RecipientUserID == inviteeID {
			t.Fatalf("inviter receipt must not target invitee, got %+v", n)
		}
		if n.Kind == model.NotificationKindHubInviteDeclinedByYou &&
			n.RecipientUserID == ownerID {
			t.Fatalf("self-receipt must not target inviter, got %+v", n)
		}
	}
}

// Wrong-user decline via dispatch is refused — the attacker cannot
// decline someone else's invite even if they somehow held a
// notification id pointing at it.
func TestNotificationResolveDeclineHubInviteWrongUserRefused(t *testing.T) {
	s, hubID, ownerID, inviteeID, _ := seedInviteHub(t)

	inviteeIDCopy := inviteeID
	inviteID := "22222222-2222-2222-2222-2222222222df"
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:            inviteID,
		HubID:         hubID,
		Token:         "tok-decline-wrong",
		InvitedBy:     ownerID,
		Role:          "contributor",
		ExpiresAt:     time.Now().Add(24 * time.Hour),
		CreatedAt:     time.Now(),
		InviteeUserID: &inviteeIDCopy,
	}); err != nil {
		t.Fatal(err)
	}
	attackerID := "11111111-1111-1111-1111-1111111111fd"
	s.addTestUser(&model.User{ID: attackerID, Name: "Attacker", Email: "attacker3@example.com"})

	nh := NewNotificationsHandler(s, nil)
	notifRow := &model.Notification{
		ID:         "dummy",
		Kind:       model.NotificationKindHubInvite,
		SourceKind: model.NotificationSourceHubInvite,
		SourceID:   inviteID,
	}
	err := nh.dispatchHubInviteAction(context.Background(), notifRow, attackerID, "decline")
	if err == nil {
		t.Fatalf("expected wrong-user decline to be refused, got nil")
	}
	// No outcome receipts should have fired.
	for _, n := range s.notifications {
		if n.Kind == model.NotificationKindHubInviteDeclined ||
			n.Kind == model.NotificationKindHubInviteDeclinedByYou {
			t.Fatalf("refused decline should not emit outcome receipts, got %+v", n)
		}
	}
}

// Regression: RevokeInvite used to tombstone the hub_invites row
// without touching the pending hub_invite notification, leaving the
// invitee with a phantom accept/decline row in their inbox pointing
// at a dead invite. The revoke path now resolves the notification
// with resolution=dismissed.
func TestRevokeInviteResolvesPendingNotification(t *testing.T) {
	s, hubID, ownerID, inviteeID, _ := seedInviteHub(t)
	h := NewHubsHandler(s)

	// Seed an addressed invite through the handler so the producer
	// runs and writes the notification row.
	inviteeIDCopy := inviteeID
	inviteID := "22222222-2222-2222-2222-22222222aaaa"
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:            inviteID,
		HubID:         hubID,
		Token:         "tok-revoke-test",
		InvitedBy:     ownerID,
		Role:          "contributor",
		ExpiresAt:     time.Now().Add(24 * time.Hour),
		CreatedAt:     time.Now(),
		InviteeUserID: &inviteeIDCopy,
	}); err != nil {
		t.Fatal(err)
	}
	pendingID := "33333333-3333-3333-3333-33333333aaaa"
	if err := s.CreateNotification(&model.Notification{
		ID:              pendingID,
		Audience:        model.AudienceUser,
		RecipientUserID: inviteeID,
		HubID:           hubID,
		Kind:            model.NotificationKindHubInvite,
		Status:          model.NotificationStatusPending,
		Priority:        1,
		SourceKind:      model.NotificationSourceHubInvite,
		SourceID:        inviteID,
		CreatedAt:       time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete,
		"/v1/hubs/"+hubID+"/invites/"+inviteID, nil)
	req.SetPathValue("id", hubID)
	req.SetPathValue("invite_id", inviteID)
	req = withTestIdentity(req, ownerID)
	rec := httptest.NewRecorder()

	h.RevokeInvite(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	pending := s.notifications[pendingID]
	if pending == nil {
		t.Fatalf("notification row disappeared")
	}
	if pending.Status != model.NotificationStatusResolved {
		t.Fatalf("expected notification status=resolved after revoke, got %q",
			pending.Status)
	}
	if pending.Resolution != model.ResolutionDismissed {
		t.Fatalf("expected resolution=dismissed after revoke, got %q",
			pending.Resolution)
	}
}

// Regression: hub_member_joined used to be keyed on
// (source_kind=hub_member, source_id=newMemberID). Since
// notifications_source_unique is (source_kind, source_id) globally
// and NOT hub-scoped, once user U produced one hub_member_joined
// row in any hub, a later join by the same user in any OTHER hub
// would conflict on the uniqueness constraint. The ON CONFLICT
// DO UPDATE path would mutate the original row instead of creating
// a new one, silently suppressing the receipt for every owner
// after the first. The fix is to key source_id on the invite id
// so each join event gets its own row.
func TestAcceptInviteHubMemberJoinedIsDistinctAcrossHubs(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	// Two hubs, two owners, one shared invitee.
	hubAID := "11111111-1111-1111-1111-11111111aaaa"
	hubBID := "11111111-1111-1111-1111-11111111bbbb"
	ownerAID := "11111111-1111-1111-1111-11111111a001"
	ownerBID := "11111111-1111-1111-1111-11111111b001"
	inviteeID := "11111111-1111-1111-1111-11111111cccc"
	if err := s.CreateHub(&model.Hub{ID: hubAID, Name: "Team A", Slug: "team-a", HubType: "team", OwnerID: ownerAID}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateHub(&model.Hub{ID: hubBID, Name: "Team B", Slug: "team-b", HubType: "team", OwnerID: ownerBID}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubAID, ownerAID, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hubBID, ownerBID, "owner"); err != nil {
		t.Fatal(err)
	}
	s.addTestUser(&model.User{ID: ownerAID, Name: "Owner A", Email: "a@example.com"})
	s.addTestUser(&model.User{ID: ownerBID, Name: "Owner B", Email: "b@example.com"})
	s.addTestUser(&model.User{ID: inviteeID, Name: "Shared", Email: "shared@example.com"})

	// Seed + accept the first invite (hub A).
	inviteAID := "22222222-2222-2222-2222-2222222222a0"
	inviteeCopyA := inviteeID
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:            inviteAID,
		HubID:         hubAID,
		Token:         "tok-a",
		InvitedBy:     ownerAID,
		Role:          "contributor",
		ExpiresAt:     time.Now().Add(24 * time.Hour),
		CreatedAt:     time.Now(),
		InviteeUserID: &inviteeCopyA,
	}); err != nil {
		t.Fatal(err)
	}
	reqA := httptest.NewRequest(http.MethodPost, "/v1/invites/tok-a/accept", nil)
	reqA.SetPathValue("token", "tok-a")
	reqA = withTestIdentity(reqA, inviteeID)
	recA := httptest.NewRecorder()
	h.AcceptInvite(recA, reqA)
	if recA.Code != http.StatusOK {
		t.Fatalf("hub A accept failed: %d %s", recA.Code, recA.Body.String())
	}

	// Seed + accept the second invite (hub B) — same user, different hub.
	inviteBID := "22222222-2222-2222-2222-2222222222b0"
	inviteeCopyB := inviteeID
	if err := s.CreateHubInvite(&model.HubInvite{
		ID:            inviteBID,
		HubID:         hubBID,
		Token:         "tok-b",
		InvitedBy:     ownerBID,
		Role:          "contributor",
		ExpiresAt:     time.Now().Add(24 * time.Hour),
		CreatedAt:     time.Now(),
		InviteeUserID: &inviteeCopyB,
	}); err != nil {
		t.Fatal(err)
	}
	reqB := httptest.NewRequest(http.MethodPost, "/v1/invites/tok-b/accept", nil)
	reqB.SetPathValue("token", "tok-b")
	reqB = withTestIdentity(reqB, inviteeID)
	recB := httptest.NewRecorder()
	h.AcceptInvite(recB, reqB)
	if recB.Code != http.StatusOK {
		t.Fatalf("hub B accept failed: %d %s", recB.Code, recB.Body.String())
	}

	// Both hub owners must end up with their own hub_member_joined
	// row. Before the fix, only owner A got one — owner B's row
	// collided on (hub_member, inviteeID) and the producer silently
	// no-op'd.
	var ownerAReceipt, ownerBReceipt *model.Notification
	for _, n := range s.notifications {
		if n.Kind != model.NotificationKindHubMemberJoined {
			continue
		}
		switch n.RecipientUserID {
		case ownerAID:
			ownerAReceipt = n
		case ownerBID:
			ownerBReceipt = n
		}
	}
	if ownerAReceipt == nil {
		t.Fatalf("hub A owner is missing hub_member_joined receipt")
	}
	if ownerBReceipt == nil {
		t.Fatalf("hub B owner is missing hub_member_joined receipt — cross-hub source_id collision bug")
	}
	if ownerAReceipt.SourceID != inviteAID {
		t.Fatalf("hub A receipt source_id=%q, want %q (invite id)", ownerAReceipt.SourceID, inviteAID)
	}
	if ownerBReceipt.SourceID != inviteBID {
		t.Fatalf("hub B receipt source_id=%q, want %q (invite id)", ownerBReceipt.SourceID, inviteBID)
	}
	if ownerAReceipt.HubID != hubAID {
		t.Fatalf("hub A receipt hub_id=%q, want %q", ownerAReceipt.HubID, hubAID)
	}
	if ownerBReceipt.HubID != hubBID {
		t.Fatalf("hub B receipt hub_id=%q, want %q", ownerBReceipt.HubID, hubBID)
	}
}

// ── Slug immutability ──

func TestHubsUpdateRejectsSlugMutation(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{
		ID:      "11111111-1111-1111-1111-111111111111",
		Name:    "Team",
		Slug:    "original-slug",
		HubType: "team",
		OwnerID: "u1",
	}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHubMember(hub.ID, "u1", "owner"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/v1/hubs/"+hub.ID, bytes.NewBufferString(`{"slug":"new-slug"}`))
	req.SetPathValue("id", hub.ID)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "slug_immutable") {
		t.Fatalf("expected slug_immutable error, got %s", rec.Body.String())
	}
}

// ── CheckSlug endpoint ──

func TestCheckSlugAvailable(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/check-slug?slug=acme-engineering", nil)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.CheckSlug(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Data struct {
			Available bool   `json:"available"`
			Reason    string `json:"reason"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Data.Available {
		t.Fatalf("expected available=true, got false (reason=%s)", resp.Data.Reason)
	}
}

func TestCheckSlugTaken(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	hub := &model.Hub{ID: "h1", Name: "Taken", Slug: "taken-hub", HubType: "team", OwnerID: "u1"}
	if err := s.CreateHub(hub); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/check-slug?slug=taken-hub", nil)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.CheckSlug(rec, req)

	var resp struct {
		Data struct {
			Available bool   `json:"available"`
			Reason    string `json:"reason"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Available {
		t.Fatal("expected available=false for taken slug")
	}
	if resp.Data.Reason != "taken" {
		t.Fatalf("expected reason=taken, got %s", resp.Data.Reason)
	}
}

func TestCheckSlugReserved(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	req := httptest.NewRequest(http.MethodGet, "/v1/hubs/check-slug?slug=personal", nil)
	req = withTestIdentity(req, "u1")
	rec := httptest.NewRecorder()

	h.CheckSlug(rec, req)

	var resp struct {
		Data struct {
			Available bool   `json:"available"`
			Reason    string `json:"reason"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Available {
		t.Fatal("expected available=false for reserved slug")
	}
	// Reserved slugs are intentionally masked as "taken" in the
	// response so the frontend cannot enumerate the reserved set
	// by probing. See TestHubsCheckSlugMasksReservedAsTaken for
	// additional coverage across multiple reserved names.
	if resp.Data.Reason != "taken" {
		t.Fatalf("expected reason=taken (masked), got %s", resp.Data.Reason)
	}
}

func TestCheckSlugInvalid(t *testing.T) {
	s := newHubsTestStore()
	h := NewHubsHandler(s)

	cases := []string{"", "ab", "-bad", "BAD_SLUG", "has%20spaces"}
	for _, slug := range cases {
		req := httptest.NewRequest(http.MethodGet, "/v1/hubs/check-slug?slug="+slug, nil)
		req = withTestIdentity(req, "u1")
		rec := httptest.NewRecorder()

		h.CheckSlug(rec, req)

		var resp struct {
			Data struct {
				Available bool   `json:"available"`
				Reason    string `json:"reason"`
			} `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("slug=%q: %v", slug, err)
		}
		if resp.Data.Available {
			t.Fatalf("slug=%q: expected available=false", slug)
		}
		if resp.Data.Reason != "invalid" {
			t.Fatalf("slug=%q: expected reason=invalid, got %s", slug, resp.Data.Reason)
		}
	}
}

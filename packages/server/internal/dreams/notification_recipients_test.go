package dreams

import (
	"fmt"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// prefFakeStore embeds InMemoryStore and lets the test control what
// GetUserPreferences returns per user. Anything else delegates to
// the embedded store's defaults (or panics if unused here).
type prefFakeStore struct {
	*store.InMemoryStore
	prefs     map[string]map[string]any
	lookupErr map[string]error
}

func (p *prefFakeStore) GetUserPreferences(userID string) (*model.UserPreferences, error) {
	if err, ok := p.lookupErr[userID]; ok {
		return nil, err
	}
	settings, ok := p.prefs[userID]
	if !ok {
		// Return an empty prefs row — MergedSettings gives defaults.
		return &model.UserPreferences{UserID: userID, Settings: map[string]any{}}, nil
	}
	return &model.UserPreferences{UserID: userID, Settings: settings}, nil
}

func newPrefFakeStore() *prefFakeStore {
	return &prefFakeStore{
		InMemoryStore: store.NewInMemoryStore(),
		prefs:         map[string]map[string]any{},
		lookupErr:     map[string]error{},
	}
}

func TestUserNotifiable(t *testing.T) {
	s := newPrefFakeStore()
	s.prefs["muted"] = map[string]any{"notifications_enabled": false}
	s.prefs["allowed"] = map[string]any{"notifications_enabled": true}
	s.lookupErr["db-blip"] = fmt.Errorf("transient db error")

	cases := []struct {
		name   string
		userID string
		want   bool
	}{
		{"explicit false → drop", "muted", false},
		{"explicit true → keep", "allowed", true},
		{"no stored prefs (defaults) → keep", "never-configured", true},
		{"db error → fail-open (keep)", "db-blip", true},
		{"empty userID → keep (nothing to look up)", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := userNotifiable(s, tc.userID); got != tc.want {
				t.Fatalf("userNotifiable(%q) = %v, want %v", tc.userID, got, tc.want)
			}
		})
	}
}

func TestFilterNotifiableRecipientsPreservesOrderAndDropsMuted(t *testing.T) {
	s := newPrefFakeStore()
	s.prefs["muted"] = map[string]any{"notifications_enabled": false}
	// Default prefs for "loud-1", "loud-2", "default-user"

	got := filterNotifiableRecipients(s, []string{"loud-1", "muted", "loud-2", "default-user"})
	if len(got) != 3 {
		t.Fatalf("expected 3 recipients (muted dropped), got %v", got)
	}
	want := []string{"loud-1", "loud-2", "default-user"}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("order drift: want %v, got %v", want, got)
		}
	}
}

func TestTopicDecisionRecipients(t *testing.T) {
	hub := &model.Hub{AllowContributorTopics: true}
	got := topicDecisionRecipients(hub, []model.HubMember{
		{UserID: "owner", Role: "owner"},
		{UserID: "admin", Role: "admin"},
		{UserID: "contrib", Role: "contributor"},
		{UserID: "viewer", Role: "viewer"},
	})
	if len(got) != 3 {
		t.Fatalf("expected 3 recipients, got %v", got)
	}
}

func TestContradictionDecisionRecipientsOwnPolicyRequiresOwnershipOfBothSides(t *testing.T) {
	hub := &model.Hub{ContributorDeletePolicy: "own"}
	got := contradictionDecisionRecipients(hub, []model.HubMember{
		{UserID: "owner", Role: "owner"},
		{UserID: "contrib-both", Role: "contributor"},
		{UserID: "contrib-one", Role: "contributor"},
	}, "contrib-both", "contrib-both")
	if len(got) != 2 || got[0] != "owner" || got[1] != "contrib-both" {
		t.Fatalf("unexpected recipients: %v", got)
	}
}

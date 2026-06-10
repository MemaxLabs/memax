package quota

import (
	"errors"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// stubHubLookup implements HubLookup with a configurable response.
type stubHubLookup struct {
	hubs map[string]*model.Hub
	err  error
}

func (s stubHubLookup) GetHub(id string) (*model.Hub, error) {
	if s.err != nil {
		return nil, s.err
	}
	h, ok := s.hubs[id]
	if !ok {
		return nil, nil
	}
	return h, nil
}

func TestSelectScope(t *testing.T) {
	t.Parallel()

	personalHub := &model.Hub{ID: "h-personal", HubType: "personal", OwnerID: "u-owner"}
	teamHub := &model.Hub{ID: "h-team", HubType: "team", OwnerID: "u-other"}
	lookup := stubHubLookup{hubs: map[string]*model.Hub{
		"h-personal": personalHub,
		"h-team":     teamHub,
	}}

	cases := []struct {
		name      string
		hubID     string
		userID    string
		want      Scope
		wantError bool
	}{
		{
			name:   "no hub, user set — API-key style",
			hubID:  "", userID: "u-caller",
			want: Scope{Type: ScopeUser, UserID: "u-caller"},
		},
		{
			name:   "personal hub — charges hub owner not caller",
			hubID:  "h-personal", userID: "u-caller",
			want: Scope{Type: ScopeUser, UserID: "u-owner"},
		},
		{
			name:   "team hub — hub-scope regardless of who triggered",
			hubID:  "h-team", userID: "u-caller",
			want: Scope{Type: ScopeHub, HubID: "h-team"},
		},
		{
			name:      "neither hub nor user — error",
			hubID:     "", userID: "",
			wantError: true,
		},
		{
			name:      "missing hub — error",
			hubID:     "h-missing", userID: "u-caller",
			wantError: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SelectScope(lookup, c.hubID, c.userID)
			if c.wantError {
				if err == nil {
					t.Errorf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestSelectScope_HubLookupError(t *testing.T) {
	t.Parallel()
	lookup := stubHubLookup{err: errors.New("db down")}
	if _, err := SelectScope(lookup, "h-x", "u"); err == nil {
		t.Fatal("expected error from hub lookup, got nil")
	}
}

func TestSelectScope_NilLookupWithHub(t *testing.T) {
	t.Parallel()
	if _, err := SelectScope(nil, "h-x", "u"); err == nil {
		t.Fatal("expected error when hub lookup is nil but hub_id is set")
	}
}

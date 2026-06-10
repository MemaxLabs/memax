package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type settingsTestStore struct {
	*store.InMemoryStore
	prefs map[string]any
}

func (s *settingsTestStore) GetUserPreferences(_ string) (*model.UserPreferences, error) {
	return &model.UserPreferences{Settings: s.prefs}, nil
}

func (s *settingsTestStore) UpsertUserPreferences(_ string, settings map[string]any) error {
	s.prefs = settings
	return nil
}

func TestSettingsGetStripsActiveHubID(t *testing.T) {
	h := NewSettingsHandler(&settingsTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		prefs: map[string]any{
			"theme":         "dark",
			"active_hub_id": "22222222-2222-2222-2222-222222222222",
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/settings", nil)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()

	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if _, ok := resp.Data["active_hub_id"]; ok {
		t.Fatalf("expected active_hub_id to be stripped, got %#v", resp.Data)
	}
}

func TestSettingsUpdatePersistsHubHeaderAuroraMode(t *testing.T) {
	testStore := &settingsTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		prefs: map[string]any{
			"theme": "dark",
		},
	}
	h := NewSettingsHandler(testStore)

	req := httptest.NewRequest(
		http.MethodPatch,
		"/v1/settings",
		strings.NewReader(`{"hub_header_aurora_mode":"time"}`),
	)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()

	h.Update(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if got := testStore.prefs["hub_header_aurora_mode"]; got != "time" {
		t.Fatalf("expected hub_header_aurora_mode to persist as time, got %#v", got)
	}

	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if got := resp.Data["hub_header_aurora_mode"]; got != "time" {
		t.Fatalf("expected response to include updated hub_header_aurora_mode, got %#v", got)
	}
}

func TestSettingsUpdatePersistsDevFlags(t *testing.T) {
	testStore := &settingsTestStore{
		InMemoryStore: store.NewInMemoryStore(),
		prefs: map[string]any{
			"theme": "dark",
			"dev_flags": map[string]any{
				"mockDreams": true,
			},
		},
	}
	h := NewSettingsHandler(testStore)

	req := httptest.NewRequest(
		http.MethodPatch,
		"/v1/settings",
		strings.NewReader(`{"dev_flags":{"debuggerEnabled":true,"skipRerank":true}}`),
	)
	req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
	rec := httptest.NewRecorder()

	h.Update(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	flags, ok := testStore.prefs["dev_flags"].(map[string]any)
	if !ok {
		t.Fatalf("expected dev_flags map to persist, got %#v", testStore.prefs["dev_flags"])
	}
	if got := flags["debuggerEnabled"]; got != true {
		t.Fatalf("expected debuggerEnabled true, got %#v", got)
	}
	if got := flags["skipRerank"]; got != true {
		t.Fatalf("expected skipRerank true, got %#v", got)
	}
	if got := flags["mockDreams"]; got != true {
		t.Fatalf("expected existing mockDreams flag to be preserved, got %#v", got)
	}
}

// TestSettingsUpdateRejectsDreamPhaseToggles regression-guards the
// per-hub intelligence migration (018). The five dream-phase
// toggles moved from user_preferences to hub.settings; PATCH
// /v1/settings must reject them with a targeted error pointing to
// the hub-settings endpoint, NOT silently accept and drop them.
// A silent accept would let the UI think the toggle persisted
// while the engine (which now reads hub.Settings alone) kept
// serving the old value.
func TestSettingsUpdateRejectsDreamPhaseToggles(t *testing.T) {
	for _, key := range []string{
		"dreams_enabled",
		"dreams_merge_enabled",
		"dreams_archive_enabled",
		"dreams_organize_enabled",
		"dreams_restructure_enabled",
	} {
		t.Run(key, func(t *testing.T) {
			s := &settingsTestStore{
				InMemoryStore: store.NewInMemoryStore(),
				prefs:         map[string]any{},
			}
			h := NewSettingsHandler(s)
			body := `{"` + key + `":false}`
			req := httptest.NewRequest(http.MethodPatch, "/v1/settings", strings.NewReader(body))
			req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
			rec := httptest.NewRecorder()
			h.Update(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d: %s", key, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "moved_to_hub_settings") {
				t.Fatalf("expected moved_to_hub_settings error code for %s, got: %s", key, rec.Body.String())
			}
			if _, persisted := s.prefs[key]; persisted {
				t.Fatalf("%s should not have persisted after rejection: %+v", key, s.prefs)
			}
		})
	}
}

// TestSettingsUpdateAcceptsTuningKeys guards that non-phase dream
// keys (engine tuning params with no UI) stay account-scoped. They
// intentionally did NOT move to hub settings in migration 018.
func TestSettingsUpdateAcceptsTuningKeys(t *testing.T) {
	tests := []struct {
		key string
		val any
	}{
		{"dreams_similarity_threshold", 0.9},
		{"dreams_staleness_days", 45.0}, // JSON numbers unmarshal to float64
		{"dreams_excluded_kinds", []any{"note"}},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			s := &settingsTestStore{
				InMemoryStore: store.NewInMemoryStore(),
				prefs:         map[string]any{},
			}
			h := NewSettingsHandler(s)
			valJSON, err := json.Marshal(tc.val)
			if err != nil {
				t.Fatalf("marshal test value: %v", err)
			}
			body := `{"` + tc.key + `":` + string(valJSON) + `}`
			req := httptest.NewRequest(http.MethodPatch, "/v1/settings", strings.NewReader(body))
			req = req.WithContext(context.WithValue(req.Context(), userIDKey, "u1"))
			rec := httptest.NewRecorder()
			h.Update(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 for %s, got %d: %s", tc.key, rec.Code, rec.Body.String())
			}
			if _, persisted := s.prefs[tc.key]; !persisted {
				t.Fatalf("%s did not persist: %+v", tc.key, s.prefs)
			}
		})
	}
}

package model

import "testing"

func TestBuildMemoryProvenanceIsPure(t *testing.T) {
	memory := &Memory{
		SourceAgent:              "codex",
		ProvenanceCreatedByType:  MemoryCreatedByHuman,
		ProvenanceInitiationType: MemoryInitiationUnknown,
	}

	prov := BuildMemoryProvenance(memory)
	if prov == nil {
		t.Fatal("expected provenance")
	}
	if prov.CreatedByType != MemoryCreatedByAgent {
		t.Fatalf("created_by_type = %q, want %q", prov.CreatedByType, MemoryCreatedByAgent)
	}
	if prov.CreatedBySlug != "codex" {
		t.Fatalf("created_by_slug = %q, want codex", prov.CreatedBySlug)
	}
	if memory.ProvenanceCreatedBySlug != "" {
		t.Fatalf("BuildMemoryProvenance should not mutate memory, got slug %q", memory.ProvenanceCreatedBySlug)
	}
}

func TestNormalizeMemoryProvenanceFieldsUsesEffectiveSlug(t *testing.T) {
	memory := &Memory{
		SourceAgent:              "claude-code",
		ProvenanceCreatedBySlug:  "codex",
		ProvenanceCreatedByType:  MemoryCreatedByHuman,
		ProvenanceInitiationType: MemoryInitiationUnknown,
	}

	NormalizeMemoryProvenanceFields(memory)

	if memory.ProvenanceCreatedBySlug != "codex" {
		t.Fatalf("created_by_slug = %q, want codex", memory.ProvenanceCreatedBySlug)
	}
	if memory.ProvenanceCreatedByType != MemoryCreatedByAgent {
		t.Fatalf("created_by_type = %q, want %q", memory.ProvenanceCreatedByType, MemoryCreatedByAgent)
	}
	if memory.ProvenanceInitiationType != MemoryInitiationUnknown {
		t.Fatalf("initiation_type = %q, want unknown", memory.ProvenanceInitiationType)
	}
}

func TestMergedHubDreamSettings(t *testing.T) {
	// Per-hub intelligence (migration 018): MergedHubDreamSettings
	// resolves from hub.Settings alone, layered on DefaultSettings.
	// There is no user-prefs layer anymore — personal hubs' phase
	// keys were backfilled into hub.Settings and the engine stopped
	// consulting user_preferences for them.

	t.Run("no hub → defaults", func(t *testing.T) {
		got := MergedHubDreamSettings(nil)
		if got["dreams_enabled"] != true {
			t.Fatalf("expected default dreams_enabled=true, got %v", got["dreams_enabled"])
		}
	})

	t.Run("hub settings override defaults", func(t *testing.T) {
		hub := &Hub{
			HubType:  "personal",
			Settings: map[string]any{"dreams_merge_enabled": false},
		}
		got := MergedHubDreamSettings(hub)
		if got["dreams_merge_enabled"] != false {
			t.Fatalf("hub setting should override default, got %v", got["dreams_merge_enabled"])
		}
		if got["dreams_enabled"] != true {
			t.Fatalf("untouched defaults preserved, got %v", got["dreams_enabled"])
		}
	})

	t.Run("team hub settings override defaults", func(t *testing.T) {
		hub := &Hub{
			HubType:  "team",
			Settings: map[string]any{"dreams_organize_enabled": false},
		}
		got := MergedHubDreamSettings(hub)
		if got["dreams_organize_enabled"] != false {
			t.Fatalf("hub setting should override default, got %v", got["dreams_organize_enabled"])
		}
	})

	t.Run("nil hub.Settings does not panic", func(t *testing.T) {
		hub := &Hub{HubType: "team"}
		got := MergedHubDreamSettings(hub)
		if got["dreams_enabled"] != true {
			t.Fatalf("empty hub settings → defaults, got %v", got["dreams_enabled"])
		}
	})

	t.Run("same resolution for personal and team hubs", func(t *testing.T) {
		// Sanity: before 018 personal and team used different
		// call shapes. Post-018 the resolution is hub-type-
		// independent.
		personal := &Hub{
			HubType:  "personal",
			Settings: map[string]any{"dreams_enabled": false},
		}
		team := &Hub{
			HubType:  "team",
			Settings: map[string]any{"dreams_enabled": false},
		}
		if MergedHubDreamSettings(personal)["dreams_enabled"] !=
			MergedHubDreamSettings(team)["dreams_enabled"] {
			t.Fatalf("personal and team hub resolution diverged")
		}
	})
}

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

// TestBuildMemoryProvenanceLegacyHumanInference — rows the old resolver
// stamped "human" with nothing behind the label (no agent, machine
// entrypoint) read back as unknown / legacy_inferred; rows with real
// evidence (web, a caller assertion, a collaborator) keep "human". The
// stored row is never rewritten.
func TestBuildMemoryProvenanceLegacyHumanInference(t *testing.T) {
	tests := []struct {
		name       string
		memory     Memory
		wantType   string
		wantSource string
		wantInit   string
	}{
		{
			name: "cli row stamped human by the old default",
			memory: Memory{
				ProvenanceCreatedByType:     MemoryCreatedByHuman,
				ProvenanceCreatedVia:        "cli",
				ProvenanceInitiationType:    MemoryInitiationHumanDirect,
				ProvenanceAttributionSource: MemoryAttributionSourceHuman,
			},
			wantType:   MemoryCreatedByUnknown,
			wantSource: MemoryAttributionSourceLegacyInferred,
			wantInit:   MemoryInitiationUnknown,
		},
		{
			name: "pre-provenance row via mcp",
			memory: Memory{
				ProvenanceCreatedVia: "mcp",
			},
			wantType:   MemoryCreatedByUnknown,
			wantSource: MemoryAttributionSourceLegacyInferred,
			wantInit:   MemoryInitiationUnknown,
		},
		{
			name: "pre-created_via row falls back to source",
			memory: Memory{
				Source:                  "cli",
				ProvenanceCreatedByType: MemoryCreatedByHuman,
			},
			wantType:   MemoryCreatedByUnknown,
			wantSource: MemoryAttributionSourceLegacyInferred,
			wantInit:   MemoryInitiationUnknown,
		},
		{
			name: "web row keeps human",
			memory: Memory{
				ProvenanceCreatedByType:     MemoryCreatedByHuman,
				ProvenanceCreatedVia:        "web",
				ProvenanceAttributionSource: MemoryAttributionSourceHuman,
			},
			wantType:   MemoryCreatedByHuman,
			wantSource: MemoryAttributionSourceHuman,
			wantInit:   MemoryInitiationHumanDirect,
		},
		{
			name: "row with no created_via keeps human (no evidence either way)",
			memory: Memory{
				ProvenanceCreatedByType: MemoryCreatedByHuman,
			},
			wantType:   MemoryCreatedByHuman,
			wantSource: "",
			wantInit:   MemoryInitiationHumanDirect,
		},
		{
			name: "asserted human_direct from the cli keeps human",
			memory: Memory{
				ProvenanceCreatedByType:     MemoryCreatedByHuman,
				ProvenanceCreatedVia:        "cli",
				ProvenanceInitiationType:    MemoryInitiationHumanDirect,
				ProvenanceAttributionSource: MemoryAttributionSourceClaim,
			},
			wantType:   MemoryCreatedByHuman,
			wantSource: MemoryAttributionSourceClaim,
			wantInit:   MemoryInitiationHumanDirect,
		},
		{
			name: "human with a collaborator keeps human",
			memory: Memory{
				ProvenanceCreatedByType:     MemoryCreatedByHuman,
				ProvenanceCreatedVia:        "cli",
				ProvenanceInitiationType:    MemoryInitiationHumanRequestedAgent,
				ProvenanceAssistedByAgent:   "codex",
				ProvenanceAttributionSource: MemoryAttributionSourceHuman,
			},
			wantType:   MemoryCreatedByHuman,
			wantSource: MemoryAttributionSourceHuman,
			wantInit:   MemoryInitiationHumanRequestedAgent,
		},
		{
			name: "unknown written by the new resolver stays unknown",
			memory: Memory{
				ProvenanceCreatedByType:     MemoryCreatedByUnknown,
				ProvenanceCreatedVia:        "sdk",
				ProvenanceInitiationType:    MemoryInitiationUnknown,
				ProvenanceAttributionSource: MemoryAttributionSourceUnknown,
			},
			wantType:   MemoryCreatedByUnknown,
			wantSource: MemoryAttributionSourceUnknown,
			wantInit:   MemoryInitiationUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.memory
			prov := BuildMemoryProvenance(&m)
			if prov.CreatedByType != tt.wantType {
				t.Fatalf("created_by_type = %q, want %q", prov.CreatedByType, tt.wantType)
			}
			if prov.AttributionSource != tt.wantSource {
				t.Fatalf("attribution_source = %q, want %q", prov.AttributionSource, tt.wantSource)
			}
			if prov.InitiationType != tt.wantInit {
				t.Fatalf("initiation_type = %q, want %q", prov.InitiationType, tt.wantInit)
			}
			if m.ProvenanceCreatedByType != tt.memory.ProvenanceCreatedByType {
				t.Fatal("BuildMemoryProvenance must not mutate the row")
			}
		})
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

// The write-path normalizer must NOT persist the read-time inference:
// a legacy human row updated later keeps its stored label.
func TestNormalizeMemoryProvenanceFieldsDoesNotPersistLegacyInference(t *testing.T) {
	m := &Memory{
		ProvenanceCreatedByType:     MemoryCreatedByHuman,
		ProvenanceCreatedVia:        "cli",
		ProvenanceInitiationType:    MemoryInitiationHumanDirect,
		ProvenanceAttributionSource: MemoryAttributionSourceHuman,
	}
	NormalizeMemoryProvenanceFields(m)
	if m.ProvenanceCreatedByType != MemoryCreatedByHuman || m.ProvenanceAttributionSource != MemoryAttributionSourceHuman {
		t.Fatalf("normalizer rewrote a legacy row: type=%q source=%q", m.ProvenanceCreatedByType, m.ProvenanceAttributionSource)
	}
}

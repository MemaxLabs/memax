package queue

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

func seedPersona(t *testing.T, s store.Store, owner, name, content string) *model.Persona {
	t.Helper()
	now := time.Now()
	p := &model.Persona{
		ID: "11111111-1111-1111-1111-111111111111", OwnerID: owner,
		SourceAgent: "openclaw", SourceScope: "global", SourceFilePath: "SOUL.md",
		Name: name, Content: content, ContentHash: "h1", Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.UpsertPersona(p); err != nil {
		t.Fatalf("seed persona: %v", err)
	}
	personas, _ := s.ListPersonas(owner)
	return &personas[0]
}

func TestResolveChatPersona(t *testing.T) {
	s := store.NewInMemoryStore()
	w := &ChatMessageRunWorker{Store: s}
	persona := seedPersona(t, s, "owner-a", "warm", "# Soul\nWarm and playful.")

	tests := []struct {
		name      string
		sess      *model.ChatSession
		defaultID string
		want      bool // persona resolved?
	}{
		{"session binding wins", &model.ChatSession{PersonaID: persona.ID}, "", true},
		{"none disables even with default", &model.ChatSession{PersonaID: model.ChatPersonaNone}, persona.ID, false},
		{"empty inherits default", &model.ChatSession{}, persona.ID, true},
		{"empty with no default resolves none", &model.ChatSession{}, "", false},
		{"dangling id resolves none", &model.ChatSession{PersonaID: "99999999-9999-9999-9999-999999999999"}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.UpsertUserPreferences("owner-a", map[string]any{"chat_default_persona_id": tt.defaultID}); err != nil {
				t.Fatalf("prefs: %v", err)
			}
			got := w.resolveChatPersona("owner-a", tt.sess)
			if (got != nil) != tt.want {
				t.Fatalf("resolveChatPersona() = %v, want resolved=%v", got, tt.want)
			}
		})
	}

	// Owner isolation: another user's session must not resolve owner-a's persona.
	if got := w.resolveChatPersona("owner-b", &model.ChatSession{PersonaID: persona.ID}); got != nil {
		t.Fatalf("expected owner isolation, resolved %v", got)
	}
}

func TestBuildChatSystemPromptInjectsPersona(t *testing.T) {
	s := store.NewInMemoryStore()
	w := &ChatMessageRunWorker{Store: s}
	persona := seedPersona(t, s, "owner-a", "warm", "# Soul\nWarm and playful.")

	prompt := w.buildChatSystemPrompt(t.Context(), "owner-a", nil,
		&model.ChatSession{PersonaID: persona.ID})
	if !strings.Contains(prompt, "## Persona: warm") || !strings.Contains(prompt, "Warm and playful.") {
		t.Fatalf("persona block missing from prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "You are memax") {
		t.Fatalf("base prompt missing")
	}
	if strings.Index(prompt, "## Persona:") > strings.Index(prompt, "You are memax") {
		t.Fatalf("persona block must precede the base prompt")
	}

	// Oversized persona is truncated, not injected wholesale.
	long := strings.Repeat("x", chatPersonaMaxChars+500)
	if err := s.UpsertPersona(&model.Persona{
		ID: "22222222-2222-2222-2222-222222222222", OwnerID: "owner-a",
		SourceAgent: "hermes", SourceScope: "global", SourceFilePath: "SOUL.md",
		Name: "big", Content: long, ContentHash: "h2", Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed big persona: %v", err)
	}
	personas, _ := s.ListPersonas("owner-a")
	var bigID string
	for _, p := range personas {
		if p.Name == "big" {
			bigID = p.ID
		}
	}
	prompt = w.buildChatSystemPrompt(t.Context(), "owner-a", nil,
		&model.ChatSession{PersonaID: bigID})
	if !strings.Contains(prompt, "[persona truncated]") {
		t.Fatalf("expected truncation marker for oversized persona")
	}

	// Multibyte content crossing the cap must truncate on a rune boundary.
	cjk := strings.Repeat("\u6c49", chatPersonaMaxChars) // 3 bytes each
	if err := s.UpsertPersona(&model.Persona{
		ID: "33333333-3333-3333-3333-333333333333", OwnerID: "owner-a",
		SourceAgent: "openclaw", SourceScope: "profile:cjk", SourceFilePath: "SOUL.md",
		Name: "cjk", Content: cjk, ContentHash: "h3", Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed cjk persona: %v", err)
	}
	personas, _ = s.ListPersonas("owner-a")
	var cjkID string
	for _, p := range personas {
		if p.Name == "cjk" {
			cjkID = p.ID
		}
	}
	prompt = w.buildChatSystemPrompt(t.Context(), "owner-a", nil,
		&model.ChatSession{PersonaID: cjkID})
	if !utf8.ValidString(prompt) {
		t.Fatalf("truncation produced invalid UTF-8")
	}
}

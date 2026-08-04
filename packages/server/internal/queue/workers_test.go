package queue

import "testing"

func TestClassifyConfigExtractionMode(t *testing.T) {
	tests := []struct {
		name     string
		agent    string
		filePath string
		want     configExtractionMode
	}{
		{
			name:     "claude global memory always extracts",
			agent:    "claude-code",
			filePath: "MEMORY.md",
			want:     configExtractAlways,
		},
		{
			name:     "claude project memory always extracts",
			agent:    "claude-code",
			filePath: "memory/feedback.md",
			want:     configExtractAlways,
		},
		{
			name:     "claude instructions selective",
			agent:    "claude-code",
			filePath: "CLAUDE.md",
			want:     configExtractSelective,
		},
		{
			name:     "codex instructions selective",
			agent:    "codex",
			filePath: "instructions.md",
			want:     configExtractSelective,
		},
		// Identity surfaces are never knowledge-extracted — synced verbatim,
		// owned by the persona pipeline (personal-agent sync, phase 2).
		{
			name:     "openclaw soul never extracts",
			agent:    "openclaw",
			filePath: "SOUL.md",
			want:     configExtractNever,
		},
		{
			name:     "openclaw workspace soul never extracts",
			agent:    "openclaw",
			filePath: "workspace/SOUL.md",
			want:     configExtractNever,
		},
		{
			name:     "hermes identity never extracts",
			agent:    "hermes",
			filePath: "IDENTITY.md",
			want:     configExtractNever,
		},
		{
			name:     "persona files never extract",
			agent:    "hermes",
			filePath: "personas/writer.md",
			want:     configExtractNever,
		},
		{
			name:     "openclaw memory still selective",
			agent:    "openclaw",
			filePath: "workspace/memory/2026-08-01.md",
			want:     configExtractSelective,
		},
		{
			name:     "hermes memory still selective",
			agent:    "hermes",
			filePath: "memory/notes.md",
			want:     configExtractSelective,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyConfigExtractionMode(tt.agent, tt.filePath); got != tt.want {
				t.Fatalf("classifyConfigExtractionMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecideConfigExtraction(t *testing.T) {
	tests := []struct {
		name     string
		agent    string
		filePath string
		content  string
		wantMode configExtractionMode
	}{
		{
			name:     "short claude instructions skipped",
			agent:    "claude-code",
			filePath: "CLAUDE.md",
			content:  "Be concise.\nUse bullet points.\nExplain your reasoning.",
			wantMode: configExtractNever,
		},
		{
			name:     "pointer-only config skipped",
			agent:    "codex",
			filePath: "instructions.md",
			content:  "See AGENTS.md for the full instructions.\nFollow .codex/instructions.md there instead.",
			wantMode: configExtractNever,
		},
		{
			name:     "claude memory file kept",
			agent:    "claude-code",
			filePath: "MEMORY.md",
			content:  "Deployment convention:\nUse Fly release_command for migrations and keep callback URLs pinned to staging domains.",
			wantMode: configExtractAlways,
		},
		{
			name:     "durable claude instructions kept",
			agent:    "claude-code",
			filePath: "CLAUDE.md",
			content:  "# Deployment\nUse Fly.io release machines for migrations. Keep OAuth callback URLs in staging and production env vars. Document every migration timeout explicitly.",
			wantMode: configExtractSelective,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := decideConfigExtraction(tt.content, tt.agent, tt.filePath)
			if decision.mode != tt.wantMode {
				t.Fatalf("decideConfigExtraction() mode = %q (%s), want %q", decision.mode, decision.reason, tt.wantMode)
			}
		})
	}
}

func TestExtractKnowledgeItems(t *testing.T) {
	t.Run("skips presentation only config", func(t *testing.T) {
		content := "# Style\nBe concise and use bullet points.\nExplain your reasoning clearly.\nUse markdown formatting."
		items := extractKnowledgeItems(content, "claude-code", "CLAUDE.md")
		if len(items) != 0 {
			t.Fatalf("expected no items, got %#v", items)
		}
	})

	t.Run("extracts only durable sections from selective config", func(t *testing.T) {
		content := `# Style
Be concise and use bullet points. Explain your reasoning clearly.

# Deployment
Use Fly.io release machines for migrations. Keep OAuth callback URLs in env vars and document migration timeouts.

# Security
Store API keys in environment variables only. Never commit secrets to git.`
		items := extractKnowledgeItems(content, "claude-code", "CLAUDE.md")
		if len(items) != 2 {
			t.Fatalf("expected 2 extracted items, got %d (%#v)", len(items), items)
		}
		if items[0].title != "Deployment" || items[1].title != "Security" {
			t.Fatalf("unexpected extracted titles: %#v", items)
		}
	})

	t.Run("extracts memory style notes even when not highly structured", func(t *testing.T) {
		content := `Deployment runbook:
Always run Fly release migrations before scaling the app.
Use the staging callback URL when validating GitHub OAuth.
If release machines stall, check migration statement timeouts.`
		items := extractKnowledgeItems(content, "claude-code", "MEMORY.md")
		if len(items) != 1 {
			t.Fatalf("expected 1 extracted item, got %d (%#v)", len(items), items)
		}
	})
}

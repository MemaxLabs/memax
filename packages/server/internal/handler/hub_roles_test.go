package handler

import (
	"strings"
	"testing"
)

func TestBuildHintPreservesUserHintDetection(t *testing.T) {
	tests := []struct {
		name             string
		userHint         string
		source           string
		wantUserProvided bool
		wantContains     string
	}{
		{"no user hint - system only", "", "cli", false, "Pushed via CLI"},
		{"user provided hint", "my custom context", "cli", true, "my custom context"},
		{"whitespace-only is not user-provided", "   ", "web", false, "Note taken on web app"},
		{"mcp source no user hint", "", "mcp", false, "Saved by AI agent via MCP"},
		{"sdk source no user hint", "", "sdk", false, "Pushed via API"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This mirrors the handler logic at memories.go:265-275
			userProvided := strings.TrimSpace(tt.userHint) != ""
			if userProvided != tt.wantUserProvided {
				t.Fatalf("userProvidedHint = %v, want %v", userProvided, tt.wantUserProvided)
			}
			built := buildHint(tt.userHint, tt.source, "", nil)
			if tt.wantContains != "" && !strings.Contains(built, tt.wantContains) {
				t.Fatalf("buildHint result %q does not contain %q", built, tt.wantContains)
			}
		})
	}
}

func TestCanReadMemories(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{"owner", true},
		{"admin", true},
		{"contributor", true},
		{"viewer", true},
		{"", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			if got := canReadMemories(tt.role); got != tt.want {
				t.Fatalf("canReadMemories(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

func TestCanWriteMemories(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{"owner", true},
		{"admin", true},
		{"contributor", true},
		{"viewer", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			if got := canWriteMemories(tt.role); got != tt.want {
				t.Fatalf("canWriteMemories(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

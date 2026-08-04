package model

import (
	"strings"
	"time"
)

// Persona is a first-class identity object derived from a synced identity
// config (SOUL.md, IDENTITY.md, persona files). One row per source file,
// upserted whenever the source config changes. Applying a persona writes it
// into a target agent's identity config through the normal config-sync
// machinery — personas never bypass agent_configs.
type Persona struct {
	ID             string    `json:"id"`
	OwnerID        string    `json:"owner_id"`
	SourceAgent    string    `json:"source_agent"`
	SourceScope    string    `json:"source_scope"` // "global" | "profile:<name>"
	SourceFilePath string    `json:"source_file_path"`
	Name           string    `json:"name"`
	Content        string    `json:"content"`
	ContentHash    string    `json:"content_hash"`
	Version        int       `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PersonaRevision is one immutable version of a persona. List responses
// omit Content (potentially large); the single-revision endpoint includes it.
type PersonaRevision struct {
	ID          string    `json:"id"`
	PersonaID   string    `json:"persona_id"`
	OwnerID     string    `json:"owner_id"`
	Version     int       `json:"version"`
	Content     string    `json:"content,omitempty"`
	ContentHash string    `json:"content_hash"`
	CreatedAt   time.Time `json:"created_at"`
}

// IsIdentityConfigPath reports whether a config file path is an agent
// identity surface (personality, tone, values — who the agent IS).
// Identity files sync verbatim, are never knowledge-extracted, and feed
// the personas table. Mirrors the identity patterns in memax-sdk's
// classifyAgentConfigFile — keep both in sync (see AGENTS.md).
func IsIdentityConfigPath(filePath string) bool {
	path := strings.ToLower(strings.ReplaceAll(filePath, "\\", "/"))
	if strings.Contains(path, "personas/") {
		return true
	}
	// Files under a memory/ directory are accumulated knowledge, never
	// identity — claude-code project memory legitimately contains files
	// named user.md/identity.md and must keep its always-extract guarantee.
	if strings.HasPrefix(path, "memory/") || strings.Contains(path, "/memory/") {
		return false
	}
	base := path
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	switch base {
	case "soul.md", "identity.md", "user.md", "persona.md":
		return true
	}
	return false
}

// DerivePersonaName picks a human name for a persona from its source:
// a personas/<name>.md file wins, then the profile name, then the agent slug.
func DerivePersonaName(agent, scope, filePath string) string {
	path := strings.ToLower(strings.ReplaceAll(filePath, "\\", "/"))
	if i := strings.Index(path, "personas/"); i >= 0 {
		stem := strings.TrimSuffix(path[i+len("personas/"):], ".md")
		if stem != "" && !strings.Contains(stem, "/") {
			return stem
		}
	}
	if name, ok := strings.CutPrefix(scope, "profile:"); ok && name != "" {
		return name
	}
	return agent
}

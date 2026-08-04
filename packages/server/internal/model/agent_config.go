package model

import "time"

// AgentConfig stores an agent configuration file for cross-device sync.
// Separate from Memory — configs need exact content preservation and bidirectional sync.
type AgentConfig struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"owner_id"`
	Agent       string    `json:"agent"`     // "claude-code", "cursor", "gemini", etc.
	FilePath    string    `json:"file_path"` // relative: "CLAUDE.md", "projects/memax/memory/feedback.md"
	Scope       string    `json:"scope"`     // Scope: "global" | "project:<repo-url>" | "profile:<name>"
	Content     string    `json:"content"`
	ContentHash string    `json:"content_hash"` // SHA256 for change detection
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AgentConfigSyncState tracks one device's last known synced state for a logical config.
// This enables correct pull/push/conflict planning without relying on timestamps.
type AgentConfigSyncState struct {
	OwnerID         string    `json:"owner_id"`
	DeviceID        string    `json:"device_id"`
	Agent           string    `json:"agent"`
	FilePath        string    `json:"file_path"`
	Scope           string    `json:"scope"`
	LocalPath       string    `json:"local_path,omitempty"`
	LastSeenVersion int       `json:"last_seen_version"`
	LastSeenHash    string    `json:"last_seen_hash"`
	Suppressed      bool      `json:"suppressed"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// AgentConfigTombstone records a logical delete for a config key.
// Devices compare their last seen state to tombstones to decide whether to
// delete locally, resurrect, or prompt for conflict resolution.
type AgentConfigTombstone struct {
	OwnerID            string     `json:"owner_id"`
	Agent              string     `json:"agent"`
	FilePath           string     `json:"file_path"`
	Scope              string     `json:"scope"`
	Version            int        `json:"version"`
	DeletedAt          time.Time  `json:"deleted_at"`
	DeletedContent     string     `json:"-"`
	DeletedContentHash string     `json:"deleted_content_hash,omitempty"`
	ContentExpiresAt   *time.Time `json:"content_expires_at,omitempty"`
}

type RecoverableAgentConfigTombstone struct {
	Agent              string     `json:"agent"`
	FilePath           string     `json:"file_path"`
	Scope              string     `json:"scope"`
	Version            int        `json:"version"`
	DeletedAt          time.Time  `json:"deleted_at"`
	DeletedContentHash string     `json:"deleted_content_hash,omitempty"`
	ContentExpiresAt   *time.Time `json:"content_expires_at,omitempty"`
}

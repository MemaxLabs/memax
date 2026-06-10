package model

import "time"

// ConnectedAgent is a first-class entity representing an AI agent connected to memax.
// Auto-populated when API keys or configs are created with an agent_name.
type ConnectedAgent struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"owner_id"`
	AgentName   string    `json:"agent_name"`
	DisplayName string    `json:"display_name"`
	Icon        string    `json:"icon"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ConnectedAgentWithStats extends ConnectedAgent with computed aggregates.
// Stats are calculated at query time via LEFT JOINs, not stored.
type ConnectedAgentWithStats struct {
	ConnectedAgent
	KeyCount            int        `json:"key_count"`
	ConfigCount         int        `json:"config_count"`
	MemoryCount         int        `json:"memory_count"`
	NeedsReconnect      bool       `json:"needs_reconnect,omitempty"`
	LastActiveAt        *time.Time `json:"last_active_at,omitempty"`
	LastObservedAt      *time.Time `json:"last_observed_at,omitempty"`
	LastOperation       *string    `json:"last_operation,omitempty"`
	LastActivitySummary *string    `json:"last_activity_summary,omitempty"`
}

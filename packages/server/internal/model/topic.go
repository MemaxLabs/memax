package model

import "time"

// Topic is a user-facing knowledge grouping. Topics form a tree (max depth 5)
// and memories keep at most one active topic assignment.
// Kind and stability remain the invisible machine-facing retrieval layer.
//
// UserModified indicates a human has edited this topic's name, description, or icon.
// The dream engine respects user-modified topics: it never renames them, but CAN
// create subtopics under them or suggest merges (via Review items).
type Topic struct {
	ID              string                `json:"id"`
	OwnerID         string                `json:"owner_id"`
	HubID           string                `json:"hub_id,omitempty"`
	ParentID        *string               `json:"parent_id"`
	Name            string                `json:"name"`
	Description     string                `json:"description,omitempty"`
	Icon            string                `json:"icon"`
	Position        int                   `json:"position"`
	Pinned          bool                  `json:"pinned"`
	UserModified    bool                  `json:"user_modified"`
	CreatedAt       time.Time             `json:"created_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
	ActivitySummary *TopicActivitySummary `json:"activity_summary,omitempty"`
	// Lifecycle carries server-resolved dream-activity aggregate scoped
	// to the viewer's last visit of this topic. Populated on topic reads;
	// nil on writes.
	Lifecycle *TopicLifecycle `json:"lifecycle,omitempty"`
}

// TopicLifecycle holds the "delta since last visit" aggregate for a
// topic card. Clears when viewer calls POST /v1/topics/:id/visit;
// server resolves DeltaSinceVisit to nil on next read.
type TopicLifecycle struct {
	DeltaSinceVisit *TopicDeltaSummary `json:"delta_since_visit"`
}

// TopicDeltaSummary is the dream-activity aggregate surfaced on topic
// cards. Counts dream actions where from_topic_id == this.id OR
// to_topic_id == this.id, scoped to viewer's topic_visits.last_visited_at.
type TopicDeltaSummary struct {
	SinceVisitAt time.Time `json:"since_visit_at"`
	RunIDs       []string  `json:"run_ids"`
	Added        int       `json:"added"`       // to_topic_id == this
	Reorganized  int       `json:"reorganized"` // from_topic_id == this OR to_topic_id == this (total activity)
}

// TopicVisit mirrors HubVisit — per-user-per-topic visit tracking that
// anchors the "clear on visit" semantics for scan-surface lifecycle
// signals. Writes happen on real topic page entry only (not prefetch,
// not hover — enforced client-side via the useMarkTopicVisit hook).
type TopicVisit struct {
	UserID         string    `json:"user_id"`
	TopicID        string    `json:"topic_id"`
	HubID          string    `json:"hub_id"`
	FirstVisitedAt time.Time `json:"first_visited_at"`
	LastVisitedAt  time.Time `json:"last_visited_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type TopicActivityContributor struct {
	UserID        string `json:"user_id"`
	UserName      string `json:"user_name"`
	UserAvatarURL string `json:"user_avatar_url,omitempty"`
}

type TopicActivitySummary struct {
	WindowDays          int                        `json:"window_days"`
	MemoryCount         int                        `json:"memory_count"`
	ContributorCount    int                        `json:"contributor_count"`
	ContributorsPreview []TopicActivityContributor `json:"contributors_preview,omitempty"`
}

// TopicTree is a Topic with children and memory counts for API responses.
type TopicTree struct {
	Topic
	MemoryCount      int         `json:"memory_count"`       // direct memories in this topic
	TotalMemoryCount int         `json:"total_memory_count"` // includes all descendant topics recursively
	Children         []TopicTree `json:"children"`
	KindDots         []string    `json:"kind_dots,omitempty"` // top memory kinds represented in this topic
}

// TopicListResponse is the envelope for GET /v1/topics.
type TopicListResponse struct {
	Topics          []TopicTree `json:"topics"`
	UnassignedCount int         `json:"unassigned_count"`
}

// MemoryTopic represents the single active topic assignment for a memory.
//
// Confidence controls how the dream engine treats this assignment:
//
//	0.0-0.5  auto-assigned, low confidence → freely re-organize
//	0.5-0.8  auto-assigned, high confidence → re-organize only with strong reason
//	0.8-0.9  user dragged it here → only re-organize if topic structure changes significantly
//	1.0      user explicitly locked → never move
type MemoryTopic struct {
	MemoryID   string    `json:"memory_id"`
	TopicID    string    `json:"topic_id"`
	Confidence float64   `json:"confidence"`
	Position   int       `json:"position"`
	CreatedAt  time.Time `json:"created_at"`
}

// Confidence constants for memory-topic assignments.
const (
	ConfidenceAutoLow  = 0.4  // dream engine auto-assigned, uncertain
	ConfidenceAutoHigh = 0.7  // dream engine auto-assigned, confident
	ConfidenceUserMove = 0.85 // user dragged memory to topic
	ConfidenceUserLock = 1.0  // user explicitly locked it
)

// ReorderOperation is a single instruction in a batch topic reorder.
type ReorderOperation struct {
	TopicID  string  `json:"topic_id"`
	Position int     `json:"position"`
	ParentID *string `json:"parent_id"`
}

//memax:lint-skip:tool-store
//
// This file is the production sink for chat browse tools. The
// wrappers in browse.go resolve SessionScope and validate hub_id
// args before calling this service, so repeating Resolve here would
// add no security. The store methods used here are strict-hub reads:
// memory list/detail filters are `hub_id = ANY(hubIDs)` exactly.

package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type AgentBrowseStore interface {
	ListMemoriesInHubs(ctx context.Context, opts store.StrictHubListOptions) ([]model.Memory, string, int, error)
	GetMemoryInHubs(ctx context.Context, id string, hubIDs []string) (*model.Memory, error)
	ListUserHubs(userID string) ([]model.HubWithRole, error)
	GetHub(id string) (*model.Hub, error)
	ListTopics(hubID string) ([]model.Topic, error)
	CountMemoriesByTopic(scope store.VisibilityScope, hubID string) (map[string]int, error)
	CountUnassignedMemories(scope store.VisibilityScope, hubID string) (int, error)
}

type AgentBrowseService struct {
	store AgentBrowseStore
}

func NewAgentBrowseService(s AgentBrowseStore) (*AgentBrowseService, error) {
	if s == nil {
		return nil, errors.New("agent/tools: AgentBrowseService requires a store")
	}
	return &AgentBrowseService{store: s}, nil
}

type AgentListMemoriesRequest struct {
	OwnerID string
	HubIDs  []string
	HubID   string
	TopicID string
	Sort    string
	Limit   int
	Cursor  string
	Since   time.Time
}

type AgentListMemoriesResponse struct {
	Memories   []MemoryListResult `json:"memories"`
	NextCursor string             `json:"next_cursor,omitempty"`
	TotalCount int                `json:"total_count"`
}

type MemoryListResult struct {
	MemoryID    string   `json:"memory_id"`
	HubID       string   `json:"hub_id"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Stability   string   `json:"stability,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Source      string   `json:"source,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	AccessCount int      `json:"access_count,omitempty"`
}

func (s *AgentBrowseService) AgentListMemories(ctx context.Context, req AgentListMemoriesRequest) (AgentListMemoriesResponse, error) {
	if len(req.HubIDs) == 0 {
		return AgentListMemoriesResponse{}, errEmptyScope
	}
	memories, nextCursor, total, err := s.store.ListMemoriesInHubs(ctx, store.StrictHubListOptions{
		HubIDs:  req.HubIDs,
		HubID:   req.HubID,
		TopicID: req.TopicID,
		Sort:    req.Sort,
		Limit:   req.Limit,
		Cursor:  req.Cursor,
		Since:   req.Since,
	})
	if err != nil {
		return AgentListMemoriesResponse{}, fmt.Errorf("list memories: %w", err)
	}
	out := make([]MemoryListResult, 0, len(memories))
	for _, m := range memories {
		out = append(out, MemoryListResult{
			MemoryID:    m.ID,
			HubID:       m.HubID,
			Title:       m.Title,
			Summary:     m.Summary,
			Kind:        m.Kind,
			Stability:   m.Stability,
			Tags:        m.Tags,
			Source:      m.Source,
			CreatedAt:   m.CreatedAt.Format(timeRFC3339),
			AccessCount: m.AccessCount,
		})
	}
	return AgentListMemoriesResponse{
		Memories:   out,
		NextCursor: nextCursor,
		TotalCount: total,
	}, nil
}

type AgentGetMemoryRequest struct {
	OwnerID  string
	HubIDs   []string
	MemoryID string
}

type MemoryDetailResult struct {
	MemoryID  string   `json:"memory_id"`
	HubID     string   `json:"hub_id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Summary   string   `json:"summary,omitempty"`
	Hint      string   `json:"hint,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	Stability string   `json:"stability,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Source    string   `json:"source,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

func (s *AgentBrowseService) AgentGetMemory(ctx context.Context, req AgentGetMemoryRequest) (MemoryDetailResult, error) {
	if len(req.HubIDs) == 0 {
		return MemoryDetailResult{}, errEmptyScope
	}
	mem, err := s.store.GetMemoryInHubs(ctx, req.MemoryID, req.HubIDs)
	if err != nil {
		return MemoryDetailResult{}, fmt.Errorf("get memory: %w", err)
	}
	return MemoryDetailResult{
		MemoryID:  mem.ID,
		HubID:     mem.HubID,
		Title:     mem.Title,
		Content:   mem.Content,
		Summary:   mem.Summary,
		Hint:      mem.Hint,
		Kind:      mem.Kind,
		Stability: mem.Stability,
		Tags:      mem.Tags,
		Source:    mem.Source,
		CreatedAt: mem.CreatedAt.Format(timeRFC3339),
		UpdatedAt: mem.UpdatedAt.Format(timeRFC3339),
	}, nil
}

type AgentListHubsRequest struct {
	OwnerID string
	HubIDs  []string
}

type HubResult struct {
	HubID       string `json:"hub_id"`
	Name        string `json:"name"`
	Slug        string `json:"slug,omitempty"`
	HubType     string `json:"hub_type,omitempty"`
	Role        string `json:"role,omitempty"`
	MemoryCount int    `json:"memory_count,omitempty"`
}

func (s *AgentBrowseService) AgentListHubs(_ context.Context, req AgentListHubsRequest) ([]HubResult, error) {
	if len(req.HubIDs) == 0 {
		return nil, errEmptyScope
	}
	hubs, err := s.store.ListUserHubs(req.OwnerID)
	if err != nil {
		return nil, fmt.Errorf("list hubs: %w", err)
	}
	out := make([]HubResult, 0, len(hubs))
	for _, item := range hubs {
		if !slices.Contains(req.HubIDs, item.Hub.ID) {
			continue
		}
		out = append(out, HubResult{
			HubID:       item.Hub.ID,
			Name:        item.Hub.Name,
			Slug:        item.Hub.Slug,
			HubType:     item.Hub.HubType,
			Role:        item.Role,
			MemoryCount: item.MemoryCount,
		})
	}
	return out, nil
}

type AgentGetHubRequest struct {
	OwnerID string
	HubIDs  []string
	HubID   string
}

func (s *AgentBrowseService) AgentGetHub(_ context.Context, req AgentGetHubRequest) (HubResult, error) {
	if len(req.HubIDs) == 0 {
		return HubResult{}, errEmptyScope
	}
	if !slices.Contains(req.HubIDs, req.HubID) {
		return HubResult{}, errHubNotInScope
	}
	hub, err := s.store.GetHub(req.HubID)
	if err != nil {
		return HubResult{}, fmt.Errorf("get hub: %w", err)
	}
	return HubResult{
		HubID:   hub.ID,
		Name:    hub.Name,
		Slug:    hub.Slug,
		HubType: hub.HubType,
	}, nil
}

type AgentListTopicsRequest struct {
	OwnerID string
	HubIDs  []string
	HubID   string
}

type AgentListTopicsResponse struct {
	HubID      string        `json:"hub_id"`
	Topics     []TopicResult `json:"topics"`
	Unassigned int           `json:"unassigned_memories,omitempty"`
}

type TopicResult struct {
	TopicID     string  `json:"topic_id"`
	HubID       string  `json:"hub_id"`
	ParentID    *string `json:"parent_id,omitempty"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Icon        string  `json:"icon,omitempty"`
	Pinned      bool    `json:"pinned,omitempty"`
	Memories    int     `json:"memories,omitempty"`
}

func (s *AgentBrowseService) AgentListTopics(ctx context.Context, req AgentListTopicsRequest) (AgentListTopicsResponse, error) {
	if len(req.HubIDs) == 0 {
		return AgentListTopicsResponse{}, errEmptyScope
	}
	if !slices.Contains(req.HubIDs, req.HubID) {
		return AgentListTopicsResponse{}, errHubNotInScope
	}
	topics, err := s.store.ListTopics(req.HubID)
	if err != nil {
		return AgentListTopicsResponse{}, fmt.Errorf("list topics: %w", err)
	}
	scope := store.VisibilityScope{OwnerID: req.OwnerID, HubIDs: []string{req.HubID}}
	counts, _ := s.store.CountMemoriesByTopic(scope, req.HubID)
	unassigned, _ := s.store.CountUnassignedMemories(scope, req.HubID)
	out := make([]TopicResult, 0, len(topics))
	for _, t := range topics {
		out = append(out, TopicResult{
			TopicID:     t.ID,
			HubID:       t.HubID,
			ParentID:    t.ParentID,
			Name:        t.Name,
			Description: t.Description,
			Icon:        t.Icon,
			Pinned:      t.Pinned,
			Memories:    counts[t.ID],
		})
	}
	return AgentListTopicsResponse{
		HubID:      req.HubID,
		Topics:     out,
		Unassigned: unassigned,
	}, nil
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"

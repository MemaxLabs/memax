package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// TopicsHandler handles topic CRUD and memory-topic assignment.
type TopicsHandler struct {
	store  store.Store
	events events.Publisher
}

func NewTopicsHandler(s store.Store, publisher events.Publisher) *TopicsHandler {
	return &TopicsHandler{store: s, events: publisher}
}

// resolveRequestedHub resolves the effective hub for topic list surfaces.
// The middleware validates only the hub it resolves into GetHubID — a raw
// ?hub_id= query param bypasses that, so any param that differs from the
// validated hub must be checked against the viewer's accessible set.
// Returns "" when the requested hub is outside the viewer's reach.
func resolveRequestedHub(r *http.Request) string {
	requested := r.URL.Query().Get("hub_id")
	validated := GetHubID(r)
	if requested == "" || requested == validated {
		return validated
	}
	for _, id := range GetAccessibleHubIDs(r) {
		if id == requested {
			return requested
		}
	}
	return ""
}

func (h *TopicsHandler) requireTopicWriteAccess(userID, hubID string) error {
	if hubID == "" {
		return fmt.Errorf("missing hub")
	}
	role, err := h.store.GetHubMemberRole(hubID, userID)
	if err != nil {
		return err
	}
	hub, err := h.store.GetHub(hubID)
	if err != nil {
		return err
	}
	if !canWriteTopics(role, hub) {
		return fmt.Errorf("write access required")
	}
	return nil
}

// List returns all topics as a tree with memory counts.
func (h *TopicsHandler) List(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	hubID := resolveRequestedHub(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Topics require an explicit or resolved hub you can access")
		return
	}

	topics, err := h.store.ListTopics(hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	scope := store.VisibilityScope{OwnerID: ownerID, HubIDs: GetAccessibleHubIDs(r)}

	counts, unassigned, err := h.store.CountTopicMemories(scope, hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	kindDots, _ := h.store.GetTopicKinds(hubID)

	// Attach lifecycle (delta_since_visit aggregate) to the flat topic
	// slice BEFORE buildTopicTree so each node's value-copy carries the
	// pointer through. Failures degrade gracefully — topics render
	// without delta chips.
	if len(topics) > 0 {
		topicIDs := make([]string, 0, len(topics))
		for i := range topics {
			topicIDs = append(topicIDs, topics[i].ID)
		}
		if lifecycleByID, lcErr := h.store.ResolveTopicLifecycle(r.Context(), scope, ownerID, topicIDs); lcErr == nil {
			for i := range topics {
				if lifecycle := lifecycleByID[topics[i].ID]; lifecycle != nil {
					topics[i].Lifecycle = lifecycle
				}
			}
		}
	}

	tree := buildTopicTree(topics, counts, kindDots)

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.TopicListResponse{
		Topics:          tree,
		UnassignedCount: unassigned,
	}})
}

// buildTopicTree assembles a flat list of topics into a tree with memory counts and kind dots.
// TotalMemoryCount includes all descendant memories recursively.
func buildTopicTree(topics []model.Topic, counts map[string]int, kindDots map[string][]string) []TopicTree {
	// Index by ID (pointers so children see parent updates)
	nodes := make(map[string]*TopicTree, len(topics))
	for _, t := range topics {
		nodes[t.ID] = &TopicTree{
			Topic:       t,
			MemoryCount: counts[t.ID],
			Children:    []TopicTree{},
			KindDots:    kindDots[t.ID],
		}
	}

	// Build tree: attach children to parents
	var rootIDs []string
	for _, t := range topics {
		if t.ParentID != nil {
			if parent, ok := nodes[*t.ParentID]; ok {
				parent.Children = append(parent.Children, *nodes[t.ID])
				continue
			}
		}
		rootIDs = append(rootIDs, t.ID)
	}

	// Compute TotalMemoryCount bottom-up
	roots := make([]TopicTree, 0, len(rootIDs))
	for _, id := range rootIDs {
		node := nodes[id]
		computeTotalCount(node)
		roots = append(roots, *node)
	}
	if roots == nil {
		roots = []TopicTree{}
	}
	return roots
}

// computeTotalCount sets TotalMemoryCount = MemoryCount + sum of children's TotalMemoryCount.
func computeTotalCount(node *TopicTree) int {
	total := node.MemoryCount
	for i := range node.Children {
		total += computeTotalCount(&node.Children[i])
	}
	node.TotalMemoryCount = total
	return total
}

// TopicTree mirrors model.TopicTree but lives in the handler package for tree assembly.
type TopicTree = model.TopicTree

// Get returns a single topic.
func (h *TopicsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ownerID := GetUserID(r)
	hubID := GetHubID(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Topic lookup requires an explicit or resolved hub")
		return
	}

	topic, err := h.store.GetTopic(id, hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Topic not found")
		return
	}

	scope := store.VisibilityScope{OwnerID: ownerID, HubIDs: GetAccessibleHubIDs(r)}
	activitySummary, err := h.store.GetTopicActivitySummary(
		scope,
		topic.ID,
		hubID,
		time.Now().AddDate(0, 0, -7),
		3,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	if activitySummary != nil {
		activitySummary.WindowDays = 7
		topic.ActivitySummary = activitySummary
	}

	// Attach lifecycle (delta_since_visit) to the single topic. Failures
	// degrade gracefully.
	if lifecycleByID, lcErr := h.store.ResolveTopicLifecycle(r.Context(), scope, ownerID, []string{topic.ID}); lcErr == nil {
		if lifecycle := lifecycleByID[topic.ID]; lifecycle != nil {
			topic.Lifecycle = lifecycle
		}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: topic})
}

// RecordVisit persists the current user's visit to a topic, anchoring
// the clear-on-visit semantics for scan-surface dream-delta signals.
// POST /v1/topics/{id}/visit
//
// Returns a plain ack. Clients are responsible for invalidating topic
// and memory queries after a successful write so lifecycle signals
// resolve against the updated last_visited_at on the next read.
func (h *TopicsHandler) RecordVisit(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	topicID := r.PathValue("id")
	if !isValidUUID(topicID) {
		writeError(w, http.StatusBadRequest, "invalid_topic_id", "topic_id must be a valid UUID")
		return
	}

	// Resolve the topic by id scoped to the viewer's VisibilityScope —
	// GetTopicAccessible returns the row only if the viewer owns it or
	// has hub membership, so we can derive the hub_id for the visit
	// write without a separate role lookup and without the caller
	// having to pre-resolve the hub. Not-found covers both "no such
	// topic" and "outside viewer's scope" to avoid leaking existence.
	scope := store.VisibilityScope{OwnerID: userID, HubIDs: GetAccessibleHubIDs(r)}
	topic, err := h.store.GetTopicAccessible(topicID, scope)
	if err != nil || topic == nil {
		writeError(w, http.StatusNotFound, "not_found", "Topic not found")
		return
	}

	if err := h.store.UpsertTopicVisit(userID, topicID, topic.HubID, time.Now().UTC()); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{
		Data: map[string]string{"status": "ok", "topic_id": topicID},
	})
}

// ListArchived returns the hub's archived topics as a flat list, most
// recently archived first. Flat because subtrees archive atomically —
// hierarchy adds no information on this surface.
// GET /v1/topics/archived
func (h *TopicsHandler) ListArchived(w http.ResponseWriter, r *http.Request) {
	hubID := resolveRequestedHub(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Archived topics require an explicit or resolved hub you can access")
		return
	}

	topics, err := h.store.ListArchivedTopics(hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.TopicArchivedListResponse{
		Topics: topics,
	}})
}

// Archive soft-archives a topic and its entire subtree. Memory assignments
// are kept so restore is lossless; archived topics disappear from the
// active tree and from AI organization (dreams, inline classification).
// Idempotent: archiving an already-archived topic is a no-op success.
// POST /v1/topics/{id}/archive
func (h *TopicsHandler) Archive(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")
	hubID := GetHubID(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Topic archive requires an explicit or resolved hub")
		return
	}
	if err := h.requireTopicWriteAccess(ownerID, hubID); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Write access to this hub is required")
		return
	}
	if _, err := h.store.GetTopic(id, hubID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Topic not found")
		return
	}

	archived, err := h.store.ArchiveTopicSubtree(id, hubID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	events.PublishTopicsChanged(r.Context(), h.events, hubID, ownerID, id)
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.TopicArchiveResult{
		TopicID:       id,
		ArchivedCount: archived,
	}})
}

// Restore un-archives a topic and its subtree. If the topic's parent is
// still archived, the topic is re-planted at the root so it never hangs
// under an invisible node. Idempotent: restoring an active topic is a
// no-op success.
// POST /v1/topics/{id}/restore
func (h *TopicsHandler) Restore(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")
	hubID := GetHubID(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Topic restore requires an explicit or resolved hub")
		return
	}
	if err := h.requireTopicWriteAccess(ownerID, hubID); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Write access to this hub is required")
		return
	}
	if _, err := h.store.GetTopic(id, hubID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Topic not found")
		return
	}

	restored, err := h.store.RestoreTopicSubtree(id, hubID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	events.PublishTopicsChanged(r.Context(), h.events, hubID, ownerID, id)
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: model.TopicArchiveResult{
		TopicID:       id,
		RestoredCount: restored,
	}})
}

// Create creates a new topic.
func (h *TopicsHandler) Create(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)

	var req struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		Icon        string  `json:"icon"`
		ParentID    *string `json:"parent_id"`
		HubID       string  `json:"hub_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "Topic name is required")
		return
	}
	if len(req.Name) > 100 {
		writeError(w, http.StatusBadRequest, "name_too_long", "Topic name must be under 100 characters")
		return
	}
	if req.Icon == "" {
		req.Icon = "folder"
	}
	hubID := req.HubID
	if hubID == "" {
		hubID = GetHubID(r)
	}
	if err := h.requireTopicWriteAccess(ownerID, hubID); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Write access to this hub is required")
		return
	}

	// Validate parent exists and check max depth (5 levels)
	if req.ParentID != nil {
		parent, err := h.store.GetTopic(*req.ParentID, hubID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_parent", "Parent topic not found")
			return
		}
		if parent.ArchivedAt != nil {
			writeError(w, http.StatusConflict, "parent_archived", "Parent topic is archived — restore it first")
			return
		}
		depth, _ := h.store.GetTopicDepth(parent.ID, hubID)
		if depth >= 4 { // parent is at depth 4 → child would be depth 5 (max)
			writeError(w, http.StatusBadRequest, "max_depth", "Topics can only be nested 5 levels deep")
			return
		}
	}

	// Determine position (max+1 among siblings)
	siblings, err := h.store.ListTopics(hubID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	maxPos := -1
	for _, s := range siblings {
		sameParent := (s.ParentID == nil && req.ParentID == nil) ||
			(s.ParentID != nil && req.ParentID != nil && *s.ParentID == *req.ParentID)
		if sameParent && s.Position > maxPos {
			maxPos = s.Position
		}
	}

	now := time.Now()
	topic := &model.Topic{
		ID:          generateID(),
		OwnerID:     ownerID,
		HubID:       hubID,
		ParentID:    req.ParentID,
		Name:        req.Name,
		Description: req.Description,
		Icon:        req.Icon,
		Position:    maxPos + 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := h.store.CreateTopic(topic); err != nil {
		// Check for unique constraint violation
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "duplicate_name",
				fmt.Sprintf("A topic named '%s' already exists at this level", req.Name))
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	events.PublishTopicsChanged(r.Context(), h.events, hubID, ownerID, topic.ID)
	writeJSON(w, http.StatusCreated, model.ApiResponse{Data: topic})
}

// isUniqueViolation checks if a PostgreSQL error is a unique constraint violation.
func isUniqueViolation(err error) bool {
	return err != nil && (contains(err.Error(), "unique") || contains(err.Error(), "duplicate") || contains(err.Error(), "23505"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Update modifies an existing topic.
func (h *TopicsHandler) Update(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")
	hubID := GetHubID(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Topic update requires an explicit or resolved hub")
		return
	}
	if err := h.requireTopicWriteAccess(ownerID, hubID); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Write access to this hub is required")
		return
	}

	existing, err := h.store.GetTopic(id, hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Topic not found")
		return
	}
	// Archived topics are read-only: restore first, then edit. This keeps
	// the archived surface a faithful snapshot and avoids edits landing in
	// a tree the user cannot see.
	if existing.ArchivedAt != nil {
		writeError(w, http.StatusConflict, "topic_archived", "This topic is archived — restore it to make changes")
		return
	}

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Icon        *string `json:"icon"`
		Position    *int    `json:"position"`
		ParentID    *string `json:"parent_id"`
		Pinned      *bool   `json:"pinned"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}

	// Track whether user is modifying topic identity (name/description/icon).
	// user_modified topics are respected by the dream engine — it won't rename them,
	// but CAN create subtopics under them or suggest merges via Review items.
	if req.Name != nil {
		if *req.Name == "" {
			writeError(w, http.StatusBadRequest, "missing_name", "Topic name cannot be empty")
			return
		}
		if len(*req.Name) > 100 {
			writeError(w, http.StatusBadRequest, "name_too_long", "Topic name must be under 100 characters")
			return
		}
		existing.Name = *req.Name
		existing.UserModified = true
	}
	if req.Description != nil {
		existing.Description = *req.Description
		existing.UserModified = true
	}
	if req.Icon != nil {
		existing.Icon = *req.Icon
		existing.UserModified = true
	}
	if req.Position != nil {
		existing.Position = *req.Position
	}
	if req.Pinned != nil {
		existing.Pinned = *req.Pinned
	}
	if req.ParentID != nil {
		// Reparent validation sequence (strict and explicit):
		//   1. root reparent (empty string or null sentinel) → ParentID = nil
		//   2. parent lookup scoped to the moving topic's hub → invalid_parent
		//      (also covers cross-hub parents without disclosing hub existence)
		//   3. self-parent → cycle_detected
		//   4. descendant check → cycle_detected
		//   5. subtree depth cap (parent depth + 1 + subtree max depth ≤ 4)
		//      → max_depth_subtree
		// A successful parent change also flips UserModified = true so the
		// dream engine treats the new location as user intent (parity with
		// the rename path above).
		newParentID := *req.ParentID
		if newParentID == "" {
			// Root reparent: moving topic becomes depth 0, subtree max depth
			// must stay within the 5-level (0-indexed cap = 4) hard limit.
			subtreeDepth, err := h.store.GetSubtreeMaxDepth(hubID, existing.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "store_error", err.Error())
				return
			}
			if subtreeDepth > 4 {
				writeError(w, http.StatusBadRequest, "max_depth_subtree", "Moving this topic's subtree would exceed the 5-level depth cap")
				return
			}
			existing.ParentID = nil
		} else {
			// Parent lookup scoped to the moving topic's hub. A miss here
			// collapses "no such topic" and "topic in a different hub" into a
			// single `invalid_parent` response — the client cannot distinguish
			// without a cross-hub read anyway, and collapsing avoids leaking
			// hub existence.
			parent, err := h.store.GetTopic(newParentID, existing.HubID)
			if err != nil || parent == nil {
				writeError(w, http.StatusBadRequest, "invalid_parent", "Parent topic not found in this hub")
				return
			}
			if parent.ArchivedAt != nil {
				writeError(w, http.StatusConflict, "parent_archived", "Parent topic is archived — restore it first")
				return
			}
			// Trivial cycle: self-parent.
			if newParentID == existing.ID {
				writeError(w, http.StatusBadRequest, "cycle_detected", "A topic cannot be its own parent")
				return
			}
			// Transitive cycle: new parent is a descendant of the moving topic.
			isDescendant, err := h.store.IsTopicDescendant(hubID, existing.ID, newParentID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "store_error", err.Error())
				return
			}
			if isDescendant {
				writeError(w, http.StatusBadRequest, "cycle_detected", "Cannot move a topic into one of its own descendants")
				return
			}
			// Subtree depth cap: parent depth + 1 (the moving topic itself)
			// + GetSubtreeMaxDepth(moving) must not exceed 4 (5 levels, 0-indexed).
			parentDepth, err := h.store.GetTopicDepth(newParentID, hubID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "store_error", err.Error())
				return
			}
			subtreeDepth, err := h.store.GetSubtreeMaxDepth(hubID, existing.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "store_error", err.Error())
				return
			}
			if parentDepth+1+subtreeDepth > 4 {
				writeError(w, http.StatusBadRequest, "max_depth_subtree", "Moving this topic's subtree would exceed the 5-level depth cap")
				return
			}
			existing.ParentID = &newParentID
		}
		// Manual reparent is a user-modified signal — dream engine must
		// respect it, same as rename/description/icon above.
		existing.UserModified = true
	}
	existing.UpdatedAt = time.Now()

	if err := h.store.UpdateTopic(existing); err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "duplicate_name",
				fmt.Sprintf("A topic named '%s' already exists at this level", existing.Name))
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	events.PublishTopicsChanged(r.Context(), h.events, hubID, ownerID, existing.ID)
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: existing})
}

// Delete removes a topic. Children are re-parented to the deleted topic's parent.
func (h *TopicsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	id := r.PathValue("id")
	hubID := GetHubID(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Topic delete requires an explicit or resolved hub")
		return
	}
	if err := h.requireTopicWriteAccess(ownerID, hubID); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Write access to this hub is required")
		return
	}

	if _, err := h.store.GetTopic(id, hubID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Topic not found")
		return
	}

	if err := h.store.DeleteTopic(id, hubID); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	events.PublishTopicsChanged(r.Context(), h.events, hubID, ownerID, id)
	w.WriteHeader(http.StatusNoContent)
}

// AddMemory assigns a memory to a topic.
func (h *TopicsHandler) AddMemory(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	topicID := r.PathValue("id")
	hubID := GetHubID(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Topic assignment requires an explicit or resolved hub")
		return
	}
	if err := h.requireTopicWriteAccess(ownerID, hubID); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Write access to this hub is required")
		return
	}

	topic, err := h.store.GetTopic(topicID, hubID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Topic not found")
		return
	}
	if topic.ArchivedAt != nil {
		writeError(w, http.StatusConflict, "topic_archived", "This topic is archived — restore it before assigning memories")
		return
	}

	var req struct {
		MemoryID   string   `json:"memory_id"`
		Confidence *float64 `json:"confidence"` // nil defaults to ConfidenceUserMove (0.85)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}
	if req.MemoryID == "" {
		writeError(w, http.StatusBadRequest, "missing_memory_id", "memory_id is required")
		return
	}

	// Default: user-initiated assignments get ConfidenceUserMove (0.85).
	// Dream engine passes its own confidence (0.3-0.7).
	// User can explicitly pass 1.0 to lock.
	confidence := model.ConfidenceUserMove
	if req.Confidence != nil {
		confidence = *req.Confidence
	}

	if err := h.store.AssignMemoryToTopic(req.MemoryID, topicID, hubID, confidence); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	events.PublishTopicsChanged(r.Context(), h.events, hubID, ownerID, topicID)
	w.WriteHeader(http.StatusCreated)
}

// RemoveMemory removes a memory from a topic.
func (h *TopicsHandler) RemoveMemory(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	topicID := r.PathValue("id")
	memoryID := r.PathValue("mid")
	hubID := GetHubID(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Topic removal requires an explicit or resolved hub")
		return
	}
	if err := h.requireTopicWriteAccess(ownerID, hubID); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Write access to this hub is required")
		return
	}

	if err := h.store.UnassignMemoryFromTopic(memoryID, topicID, hubID); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	events.PublishTopicsChanged(r.Context(), h.events, hubID, ownerID, topicID)
	w.WriteHeader(http.StatusNoContent)
}

// ListMemories returns paginated memories within a topic.
func (h *TopicsHandler) ListMemories(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	topicID := r.PathValue("id")
	hubID := GetHubID(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Topic browsing requires an explicit or resolved hub")
		return
	}

	if _, err := h.store.GetTopic(topicID, hubID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Topic not found")
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := fmt.Sscanf(l, "%d", &limit); n == 0 || err != nil {
			limit = 20
		}
	}
	if limit > 50 {
		limit = 50
	}
	cursor := r.URL.Query().Get("cursor")

	scope := store.VisibilityScope{OwnerID: ownerID, HubIDs: GetAccessibleHubIDs(r)}
	memories, nextCursor, err := h.store.ListMemoriesByTopic(scope, topicID, limit, cursor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	// Attach topic_id so frontend can group by subtopic
	if topicMap, err := h.store.GetMemoryTopicIDMap(scope); err == nil && len(topicMap) > 0 {
		for i := range memories {
			if topicID, ok := topicMap[memories[i].ID]; ok {
				memories[i].TopicID = topicID
			}
		}
	}

	// Attach lifecycle (pending_dream_action scoped to topic_visits) so
	// the topic-detail memory list — a core scan surface — renders
	// breadcrumb tint and halo from server data. Without this wire, the
	// row resolver sees memory.lifecycle == undefined and renders
	// "neutral" regardless of actual dream activity. Failures degrade
	// gracefully: rows render without lifecycle signals.
	if len(memories) > 0 {
		ids := make([]string, 0, len(memories))
		for i := range memories {
			ids = append(ids, memories[i].ID)
		}
		if lifecycleByID, lcErr := h.store.ResolveMemoryLifecycleForList(r.Context(), scope, ownerID, ids); lcErr == nil {
			for i := range memories {
				if lifecycle := lifecycleByID[memories[i].ID]; lifecycle != nil {
					memories[i].Lifecycle = lifecycle
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]any{
		"memories":    memories,
		"next_cursor": nextCursor,
		"has_more":    nextCursor != "",
	}})
}

// Reorder batch-updates topic positions and parents.
func (h *TopicsHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	hubID := GetHubID(r)
	if hubID == "" {
		writeError(w, http.StatusBadRequest, "missing_hub", "Topic reorder requires an explicit or resolved hub")
		return
	}
	if err := h.requireTopicWriteAccess(ownerID, hubID); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Write access to this hub is required")
		return
	}

	var req struct {
		Operations []model.ReorderOperation `json:"operations"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "Could not parse JSON")
		return
	}

	if len(req.Operations) == 0 {
		writeError(w, http.StatusBadRequest, "empty_operations", "At least one operation is required")
		return
	}

	// Archive guards: reorder must not touch archived topics nor plant an
	// active topic under an archived parent — the tree UI only offers
	// active nodes, so any such op is a stale or hostile client.
	for _, op := range req.Operations {
		topic, err := h.store.GetTopic(op.TopicID, hubID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_topic", "Reorder target topic not found in this hub")
			return
		}
		if topic.ArchivedAt != nil {
			writeError(w, http.StatusConflict, "topic_archived", "Cannot reorder an archived topic — restore it first")
			return
		}
		if op.ParentID != nil && *op.ParentID != "" {
			parent, err := h.store.GetTopic(*op.ParentID, hubID)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_parent", "Parent topic not found in this hub")
				return
			}
			if parent.ArchivedAt != nil {
				writeError(w, http.StatusConflict, "parent_archived", "Parent topic is archived — restore it first")
				return
			}
		}
	}

	if err := h.store.ReorderTopics(hubID, req.Operations); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	events.PublishTopicsChanged(r.Context(), h.events, hubID, ownerID, "")
	writeJSON(w, http.StatusOK, model.ApiResponse{Data: map[string]string{"status": "ok"}})
}

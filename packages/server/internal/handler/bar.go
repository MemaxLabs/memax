package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// BarHandler is the single endpoint behind the web bar's pre-Enter
// "quick matches" layer. It unifies three surfaces into one response:
// memory FTS (same chunk-search path as /v1/memories/search), topic
// name/description substring match, and hub display-name match. The
// client stops stitching three hooks and gets consistent contracts for
// all three row types.
type BarHandler struct {
	store    store.Store
	memories *MemoriesHandler
}

func NewBarHandler(s store.Store, m *MemoriesHandler) *BarHandler {
	return &BarHandler{store: s, memories: m}
}

type barTopicRow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	HubID       string `json:"hub_id"`
}

type barHubRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type barSearchResponse struct {
	Query    string            `json:"query"`
	Memories []memorySearchRow `json:"memories"`
	Topics   []barTopicRow     `json:"topics"`
	Hubs     []barHubRow       `json:"hubs"`
}

// Search handles GET /v1/bar/search?q=<query>&limit=<n>&topic_id=<uuid>.
//
// limit applies to memory matches (1..50, default 10). Topic and hub
// matches are capped at 5 each — the bar surfaces them as Jump-to chips
// above the memory list, and any more than a handful of each is noise
// for that surface.
func (h *BarHandler) Search(w http.ResponseWriter, r *http.Request) {
	ownerID := GetUserID(r)
	rawQuery := r.URL.Query().Get("q")
	query := strings.TrimSpace(rawQuery)

	// Empty query returns an empty-but-well-formed payload rather than
	// a 400 — the client shows "Recent" / "Jump to" layers from its own
	// cache when the bar is focused with no text, and hitting this
	// endpoint with q="" is a legitimate no-op on the server side.
	if query == "" {
		writeJSON(w, http.StatusOK, model.ApiResponse{Data: barSearchResponse{
			Query:    "",
			Memories: []memorySearchRow{},
			Topics:   []barTopicRow{},
			Hubs:     []barHubRow{},
		}})
		return
	}

	memLimit := 10
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 50 {
			memLimit = n
		}
	}

	topicID := r.URL.Query().Get("topic_id")
	if topicID != "" && !isValidUUID(topicID) {
		writeError(w, http.StatusBadRequest, "invalid_topic_id", "topic_id must be a valid UUID")
		return
	}

	hubIDs := GetAccessibleHubIDs(r)

	memories, err := h.memories.searchMemoriesCore(r.Context(), query, ownerID, hubIDs, topicID, memLimit, isHubScopeBounded(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search_error", err.Error())
		return
	}

	const jumpToLimit = 5
	topicRows := make([]barTopicRow, 0, jumpToLimit)
	if len(hubIDs) > 0 {
		topics, err := h.store.SearchAccessibleTopics(r.Context(), query, hubIDs, jumpToLimit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "topic_search_error", err.Error())
			return
		}
		for _, t := range topics {
			topicRows = append(topicRows, barTopicRow{
				ID:          t.ID,
				Name:        t.Name,
				Description: t.Description,
				HubID:       t.HubID,
			})
		}
	}

	// Hub matches must respect the request's authorized hub scope —
	// using `ListUserHubs(ownerID)` verbatim would leak hub names
	// outside the scope that memories and topics already honor. A
	// restricted API key could otherwise probe `q` to enumerate hubs
	// the grant does not permit. Build an allow-set from `hubIDs`
	// (the same scope the memory + topic paths use) and skip any
	// membership outside it before running the name match.
	hubRows := make([]barHubRow, 0, jumpToLimit)
	if len(hubIDs) > 0 {
		allowed := make(map[string]struct{}, len(hubIDs))
		for _, id := range hubIDs {
			allowed[id] = struct{}{}
		}
		if userHubs, hubErr := h.store.ListUserHubs(ownerID); hubErr == nil {
			tokens := strings.Fields(strings.ToLower(query))
			for _, entry := range userHubs {
				if len(hubRows) >= jumpToLimit {
					break
				}
				if _, ok := allowed[entry.Hub.ID]; !ok {
					continue
				}
				haystack := strings.ToLower(entry.Hub.Name)
				matched := true
				for _, token := range tokens {
					if !strings.Contains(haystack, token) {
						matched = false
						break
					}
				}
				if matched {
					hubRows = append(hubRows, barHubRow{ID: entry.Hub.ID, Name: entry.Hub.Name})
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, model.ApiResponse{Data: barSearchResponse{
		Query:    query,
		Memories: memories,
		Topics:   topicRows,
		Hubs:     hubRows,
	}})
}

package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type EventsHandler struct {
	store  store.Store
	broker *events.Broker
}

func NewEventsHandler(s store.Store, broker *events.Broker) *EventsHandler {
	return &EventsHandler{store: s, broker: broker}
}

// Stream streams invalidation events to authenticated clients.
// GET /v1/events/stream
func (h *EventsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	if h.broker == nil {
		writeError(w, http.StatusServiceUnavailable, "events_unavailable", "Event streaming is not configured.")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream_unavailable", "Streaming not supported by server.")
		return
	}

	userID := GetUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
		return
	}

	hubRoles := map[string]string{}
	accessible := GetAccessibleHubIDs(r)
	if len(accessible) > 0 {
		for _, hubID := range accessible {
			role, _ := h.store.GetHubMemberRole(hubID, userID)
			if role != "" {
				hubRoles[hubID] = role
			}
		}
	} else {
		hubs, err := h.store.ListUserHubs(userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
		for _, h := range hubs {
			hubRoles[h.Hub.ID] = h.Role
		}
	}

	hubFilter := strings.TrimSpace(r.URL.Query().Get("hub_id"))
	if hubFilter != "" {
		if _, ok := hubRoles[hubFilter]; !ok {
			writeError(w, http.StatusForbidden, "forbidden", "You do not have access to this hub.")
			return
		}
	}

	typeFilter := parseTypeFilter(r.URL.Query().Get("types"))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sub := h.broker.Subscribe(userID, hubRoles)
	if sub == nil {
		writeError(w, http.StatusServiceUnavailable, "events_unavailable", "Event streaming is not configured.")
		return
	}
	defer sub.Close()

	writeStreamSSE(w, flusher, "ready", map[string]any{
		"user_id": userID,
		"hub_ids": mapKeys(hubRoles),
	})

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			writeStreamSSE(w, flusher, "ping", map[string]string{"ts": time.Now().UTC().Format(time.RFC3339)})
		case evt, ok := <-sub.Events:
			if !ok {
				return
			}
			if hubFilter != "" && evt.HubID != hubFilter {
				continue
			}
			if len(typeFilter) > 0 && !typeFilter[evt.Type] {
				continue
			}
			writeStreamSSE(w, flusher, evt.Type, evt)
		}
	}
}

func writeStreamSSE(w http.ResponseWriter, flusher http.Flusher, event string, data any) {
	payload, _ := json.Marshal(data)
	w.Write([]byte("event: " + event + "\n"))
	w.Write([]byte("data: " + string(payload) + "\n\n"))
	flusher.Flush()
}

func parseTypeFilter(raw string) map[string]bool {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		out[name] = true
	}
	return out
}

func mapKeys(m map[string]string) []string {
	if len(m) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

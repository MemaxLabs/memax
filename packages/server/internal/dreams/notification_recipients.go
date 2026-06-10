package dreams

import (
	"log/slog"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// userNotifiable reports whether the user's notifications_enabled
// preference allows dream-generated notifications. True when the
// setting is missing, the store lookup fails (fail-open — a silent
// store error should not silence every notification), or the value
// is explicitly true. False only when the pref is explicitly false.
//
// The fail-open default matches the rest of the engine: dreams
// degrade gracefully when Postgres hiccups, they don't turn into a
// global notification blackout.
func userNotifiable(s store.Store, userID string) bool {
	if s == nil || userID == "" {
		return true
	}
	prefs, err := s.GetUserPreferences(userID)
	if err != nil {
		slog.Warn("dream: prefs lookup for notification gate failed", "user_id", userID, "error", err)
		return true
	}
	merged := prefs.MergedSettings()
	enabled, ok := merged["notifications_enabled"].(bool)
	if !ok {
		return true
	}
	return enabled
}

// filterNotifiableRecipients drops userIDs whose notifications_enabled
// preference is explicitly false. Order-preserving. O(N) preference
// lookups — fine for hub fan-out which is typically single-digit
// member counts.
func filterNotifiableRecipients(s store.Store, userIDs []string) []string {
	out := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		if userNotifiable(s, id) {
			out = append(out, id)
		}
	}
	return out
}

func canWriteTopicsForDreamRecipient(role string, hub *model.Hub) bool {
	if role == "owner" || role == "admin" {
		return true
	}
	if role != "contributor" || hub == nil {
		return false
	}
	return hub.AllowContributorTopics
}

func canResolveContradictionForDreamRecipient(role string, hub *model.Hub, memoryAOwnerID, memoryBOwnerID, userID string) bool {
	if role == "owner" || role == "admin" {
		return true
	}
	if role != "contributor" || hub == nil {
		return false
	}
	switch hub.ContributorDeletePolicy {
	case "any":
		return true
	case "own":
		return memoryAOwnerID == userID && memoryBOwnerID == userID
	default:
		return false
	}
}

func topicDecisionRecipients(hub *model.Hub, members []model.HubMember) []string {
	recipients := make([]string, 0, len(members))
	for _, member := range members {
		if canWriteTopicsForDreamRecipient(member.Role, hub) {
			recipients = append(recipients, member.UserID)
		}
	}
	return recipients
}

func contradictionDecisionRecipients(hub *model.Hub, members []model.HubMember, memoryAOwnerID, memoryBOwnerID string) []string {
	recipients := make([]string, 0, len(members))
	for _, member := range members {
		if canResolveContradictionForDreamRecipient(member.Role, hub, memoryAOwnerID, memoryBOwnerID, member.UserID) {
			recipients = append(recipients, member.UserID)
		}
	}
	return recipients
}

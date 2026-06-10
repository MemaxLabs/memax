package handler

import "github.com/MemaxLabs/memax/packages/server/internal/model"

func isHubAdmin(role string) bool {
	return role == "owner" || role == "admin"
}

func canReadMemories(role string) bool {
	return role == "owner" || role == "admin" || role == "contributor" || role == "viewer"
}

func canWriteMemories(role string) bool {
	return role == "owner" || role == "admin" || role == "contributor"
}

func canWriteTopics(role string, hub *model.Hub) bool {
	if role == "owner" || role == "admin" {
		return true
	}
	if role != "contributor" || hub == nil {
		return false
	}
	return hub.AllowContributorTopics
}

func canTriggerDreams(role string, hub *model.Hub) bool {
	if role == "owner" || role == "admin" {
		return true
	}
	if role != "contributor" || hub == nil {
		return false
	}
	return hub.AllowContributorDreams
}

// canDeleteMemory checks whether a hub member can delete a specific memory.
// isMemoryOwner indicates whether the requesting user is the memory's owner_id.
// Owner/admin can always delete any memory in their hub.
// Contributors are governed by the hub's ContributorDeletePolicy:
//   - "none" → cannot delete anything
//   - "own"  → can only delete memories they contributed
//   - "any"  → can delete any memory in the hub
func canDeleteMemory(role string, hub *model.Hub, isMemoryOwner bool) bool {
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
		return isMemoryOwner
	default: // "none" or unrecognized
		return false
	}
}

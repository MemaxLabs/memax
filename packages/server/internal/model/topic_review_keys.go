package model

import (
	"sort"
	"strings"
)

// BuildTopicMergeNotificationSourceID returns the canonical
// dedup key for a review_topic_merge notification. Used by:
//
//   - The dream engine when a dream cycle's topic-organize
//     phase suggests a merge (internal/dreams/restructure.go).
//   - The chat agent's propose_topic_merge tool when the user
//     asks the agent to surface a merge for review
//     (internal/workerapp/app.go: chatProposeTopicMergeAdapter).
//
// **Why this lives in `model`.** Both producers have to compute
// EXACTLY the same source_id for the same merge so the
// notifications_source_unique global UNIQUE constraint on
// (source_kind, source_id) dedupes them onto
// the same inbox row. Inlining the construction in each call
// site let dreams and chat drift apart (codex Phase 3.6b3f v2
// caught this — dreams included the target in its source list
// while chat filtered it out, so the same merge produced two
// rows). Centralizing the key here pins the contract.
//
// **Filtering rule.** sourceIDs MUST NOT include the target.
// Defense in depth: this function strips it if a caller
// accidentally includes it. Empty ids and duplicates are also
// stripped — the resulting list is sorted before joining so
// the key is order-independent.
//
// **Recipient suffix.** The unique constraint is on
// (source_kind, source_id) and the source_id includes the
// recipient so per-user inbox rows for the same merge stay
// distinct. (A future hub-shared notification audience would
// drop the recipient suffix.)
//
// Format:
//
//	topic_merge:<targetID>:<sorted-filtered-sources>:<recipientID>
func BuildTopicMergeNotificationSourceID(targetID string, sourceIDs []string, recipientID string) string {
	filtered := make([]string, 0, len(sourceIDs))
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, id := range sourceIDs {
		id = strings.TrimSpace(id)
		if id == "" || id == targetID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		filtered = append(filtered, id)
	}
	sort.Strings(filtered)
	return "topic_merge:" + targetID + ":" + strings.Join(filtered, ":") + ":" + recipientID
}

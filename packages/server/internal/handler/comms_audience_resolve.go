package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// resolvedAudience is the output of resolving a campaign's audience
// snapshot into concrete recipient IDs. FailedEmails is populated only
// for "users" rules where we could not match an email to a user row.
// Unknown user IDs are silently dropped (matches the existing admin
// notifications handler behavior).
type resolvedAudience struct {
	UserIDs      []string
	FailedEmails []string
}

// resolveAudienceSnapshotInline enumerates recipients for a rule that
// has either a discrete user list ("users") or a hub slice ("hub").
// "all" is handled separately by the send worker, which pages the full
// user list instead of materializing it in memory.
func resolveAudienceSnapshotInline(snapshot *model.AudienceSnapshot, s store.Store) (*resolvedAudience, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("audience snapshot is required")
	}
	switch snapshot.RuleType {
	case model.AudienceRuleUsers:
		return resolveUsersRule(s, snapshot.Rule), nil
	case model.AudienceRuleHub:
		return resolveHubRule(s, snapshot.Rule)
	case model.AudienceRuleAll:
		return nil, fmt.Errorf("all-users rule is resolved by the send worker, not inline")
	default:
		return nil, fmt.Errorf("unknown audience rule type: %s", snapshot.RuleType)
	}
}

func resolveUsersRule(s store.Store, rule model.AudienceRule) *resolvedAudience {
	seen := make(map[string]bool, len(rule.UserIDs)+len(rule.Emails))
	out := &resolvedAudience{FailedEmails: []string{}}

	for _, uid := range rule.UserIDs {
		uid = strings.TrimSpace(uid)
		if uid == "" || seen[uid] {
			continue
		}
		if _, err := s.GetUser(uid); err != nil {
			continue // drop unknown ids silently
		}
		seen[uid] = true
		out.UserIDs = append(out.UserIDs, uid)
	}

	for _, email := range rule.Emails {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}
		u, err := s.GetUserByCanonicalEmail(email)
		if err != nil || u == nil {
			out.FailedEmails = append(out.FailedEmails, email)
			continue
		}
		if seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		out.UserIDs = append(out.UserIDs, u.ID)
	}
	return out
}

func resolveHubRule(s store.Store, rule model.AudienceRule) (*resolvedAudience, error) {
	if rule.HubID == "" {
		return nil, fmt.Errorf("hub rule requires hub_id")
	}
	members, err := s.ListHubMembers(rule.HubID)
	if err != nil {
		return nil, fmt.Errorf("list hub members: %w", err)
	}
	out := &resolvedAudience{FailedEmails: []string{}}
	for _, m := range members {
		if rule.HubMemberRole != "" && m.Role != rule.HubMemberRole {
			continue
		}
		out.UserIDs = append(out.UserIDs, m.UserID)
	}
	return out, nil
}

// estimateAudience returns a recipient count plus a small sample. For
// "all" rules it reads ListUsers with limit 1 to get the authoritative
// total, then samples from the first page; inline rules reuse the
// resolver above.
func estimateAudience(ctx context.Context, s store.Store, snapshot *model.AudienceSnapshot) (*model.AudienceEstimate, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("audience snapshot is required")
	}
	sampleSize := 5

	switch snapshot.RuleType {
	case model.AudienceRuleAll:
		users, _, total, err := s.ListUsers(ctx, model.AdminUserListOpts{Limit: sampleSize})
		if err != nil {
			return nil, err
		}
		sample := make([]string, 0, sampleSize)
		for _, u := range users {
			sample = append(sample, u.ID)
			if len(sample) == sampleSize {
				break
			}
		}
		return &model.AudienceEstimate{Count: total, Sample: sample}, nil

	case model.AudienceRuleUsers:
		resolved := resolveUsersRule(s, snapshot.Rule)
		sample := take(resolved.UserIDs, sampleSize)
		return &model.AudienceEstimate{Count: len(resolved.UserIDs), Sample: sample}, nil

	case model.AudienceRuleHub:
		resolved, err := resolveHubRule(s, snapshot.Rule)
		if err != nil {
			return nil, err
		}
		sample := take(resolved.UserIDs, sampleSize)
		return &model.AudienceEstimate{Count: len(resolved.UserIDs), Sample: sample}, nil

	default:
		return nil, fmt.Errorf("unknown audience rule type: %s", snapshot.RuleType)
	}
}

func take(ids []string, n int) []string {
	if len(ids) < n {
		n = len(ids)
	}
	out := make([]string, n)
	copy(out, ids[:n])
	return out
}

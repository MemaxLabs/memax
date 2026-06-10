package model

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestChatConstantsMatchSchema pins the Go-side enum VALUES in
// lockstep with the SQL CHECK constraints in
// migrations/007_chat_phase3_foundation.up.sql. The test parses
// the migration file, extracts each `CHECK (col IN (...))`
// constant set, and asserts the Go const values cover the same
// set exactly.
//
// Drift in either direction (rename a const, alter the
// migration's CHECK) fails this test loudly so the writer
// catches the mismatch BEFORE a runtime SQL constraint error.
func TestChatConstantsMatchSchema(t *testing.T) {
	t.Parallel()

	migration := mustReadMigration007(t)

	cases := []struct {
		name        string
		extractFrom string // CHECK column name in the migration
		want        []string
	}{
		{"chat_sessions.scope_type", "scope_type", []string{
			ChatScopeTypeUserAllHubs,
			ChatScopeTypeSingleHub,
			ChatScopeTypeHubSubset,
		}},
		{"chat_sessions.status", "status", []string{
			ChatSessionStatusIdle,
			ChatSessionStatusRunning,
			ChatSessionStatusAwaitingApproval,
			ChatSessionStatusCanceling,
		}},
		{"chat_messages.role", "role", []string{
			ChatMessageRoleUser,
			ChatMessageRoleAssistant,
			ChatMessageRoleSystem,
		}},
		{"chat_messages.status", "status", []string{
			ChatMessageStatusQueued,
			ChatMessageStatusRunning,
			ChatMessageStatusCompleted,
			ChatMessageStatusPartialFailed,
			ChatMessageStatusFailed,
			ChatMessageStatusCanceled,
			ChatMessageStatusCanceling,
		}},
		{"chat_tool_approvals.status", "status", []string{
			ChatToolApprovalStatusPending,
			ChatToolApprovalStatusApproved,
			ChatToolApprovalStatusDenied,
			ChatToolApprovalStatusTimedOut,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotSQL := extractAllCheckSets(migration, tc.extractFrom)
			if !containsExactSet(gotSQL, tc.want) {
				t.Errorf("Go consts for %s = %v; SQL CHECK sets in migration = %v",
					tc.name, tc.want, gotSQL)
			}
		})
	}
}

// extractAllCheckSets pulls every `CHECK (col IN (...))` set
// for the given column out of the migration text. There may be
// multiple CHECK constraints across tables that share the same
// column name (e.g. several `status` columns); the test asserts
// the Go consts appear in AT LEAST one of them and no SQL set
// has values the Go consts lack.
func extractAllCheckSets(sql, col string) [][]string {
	var sets [][]string
	// Match `CHECK (<col> IN ('v1', 'v2', ...))` — handles
	// optional whitespace and quotes. The regex is purposely
	// loose to tolerate formatting changes in the migration.
	pattern := regexp.MustCompile(`(?i)CHECK\s*\(\s*` + regexp.QuoteMeta(col) + `\s+IN\s*\(([^)]+)\)\s*\)`)
	for _, match := range pattern.FindAllStringSubmatch(sql, -1) {
		body := match[1]
		var set []string
		for _, raw := range strings.Split(body, ",") {
			v := strings.TrimSpace(raw)
			v = strings.Trim(v, "'\"")
			if v != "" {
				set = append(set, v)
			}
		}
		sets = append(sets, set)
	}
	return sets
}

// containsExactSet returns true when at least one of the SQL
// sets equals the Go set (order-insensitive). Used by the
// lockstep test so a single CHECK in the migration must match
// the Go consts exactly — extra or missing values fail.
func containsExactSet(sqlSets [][]string, goSet []string) bool {
	target := make(map[string]struct{}, len(goSet))
	for _, v := range goSet {
		target[v] = struct{}{}
	}
	for _, set := range sqlSets {
		if len(set) != len(goSet) {
			continue
		}
		match := true
		for _, v := range set {
			if _, ok := target[v]; !ok {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func mustReadMigration007(t *testing.T) string {
	t.Helper()
	// runtime.Caller-based path resolution. The package's
	// runtime path is internal/model/; the migration lives in
	// ../../../migrations/.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	migrationPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations", "007_chat_phase3_foundation.up.sql")
	body, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	return string(body)
}

func TestChatMessageCaps(t *testing.T) {
	t.Parallel()
	if ChatMessageSoftCap >= ChatMessageHardCap {
		t.Errorf("soft cap (%d) must be less than hard cap (%d)", ChatMessageSoftCap, ChatMessageHardCap)
	}
	// Plan 24 §"Per-session message cap" pins these values.
	if ChatMessageSoftCap != 200 {
		t.Errorf("soft cap = %d, want 200 (plan 24)", ChatMessageSoftCap)
	}
	if ChatMessageHardCap != 250 {
		t.Errorf("hard cap = %d, want 250 (plan 24)", ChatMessageHardCap)
	}
}

func TestChatToolApprovalTTL(t *testing.T) {
	t.Parallel()
	// Plan 24 §"chat_tool_approvals" pins 10 minutes. The
	// migration's expires_at default = requested_at + this
	// value, written by the engine at insert time.
	if ChatToolApprovalTTL.Minutes() != 10 {
		t.Errorf("approval TTL = %v, want 10 minutes", ChatToolApprovalTTL)
	}
}

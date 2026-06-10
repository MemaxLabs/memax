package store_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// Phase 3.1 chat foundation — Postgres round-trip tests.
//
// These tests exercise the store interface against a live
// Postgres so the SQL CHECK constraints, JSONB encoding, and
// owner-scoping all run end-to-end. Skipped when DATABASE_URL is
// unset; the migration must already be at version >=5.

func acquireChatTestDB(t *testing.T) (*pgxpool.Pool, store.Store, string) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping chat store integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	owner := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'C', 'personal_early_access', now(), now())`,
		owner, "chat-"+owner[:8]+"@x"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		// Cascade-deletes chat_sessions → chat_messages →
		// chat_message_events / chat_tool_approvals.
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1::uuid`, owner)
	})

	return pool, store.NewPostgresStore(pool), owner
}

// TestPostgresChat_SessionRoundTrip pins create + get +
// list-sessions + soft-delete invariants for the simplest
// scope (single-hub, with one tool).
func TestPostgresChat_SessionRoundTrip(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	hubID := uuid.NewString()
	// Seed a hub so the FK doesn't reject the scope_hub_ids array element.
	// (We don't actually require an FK on scope_hub_ids per the design,
	// but creating the hub keeps the test self-consistent.)
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "ChatTest", Slug: "ct-" + hubID[:6],
		HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	sess := &model.ChatSession{
		ID:             uuid.NewString(),
		OwnerID:        owner,
		ScopeType:      model.ChatScopeTypeSingleHub,
		ScopeHubIDs:    []string{hubID},
		Title:          "First chat",
		Model:          "claude-sonnet-4-6",
		Tools:          []string{"recall_memories"},
		ToolsetVersion: 1,
		Status:         model.ChatSessionStatusIdle,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.CreateChatSession(ctx, sess); err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}

	got, err := s.GetChatSession(ctx, sess.ID, owner)
	if err != nil {
		t.Fatalf("GetChatSession: %v", err)
	}
	if got.Title != "First chat" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.ScopeType != model.ChatScopeTypeSingleHub {
		t.Errorf("ScopeType = %q", got.ScopeType)
	}
	if len(got.ScopeHubIDs) != 1 || got.ScopeHubIDs[0] != hubID {
		t.Errorf("ScopeHubIDs = %v, want [%s]", got.ScopeHubIDs, hubID)
	}
	if len(got.Tools) != 1 || got.Tools[0] != "recall_memories" {
		t.Errorf("Tools = %v", got.Tools)
	}

	// List should include this session.
	list, _, err := s.ListChatSessionsByOwner(ctx, store.ChatSessionListOpts{OwnerID: owner})
	if err != nil {
		t.Fatalf("ListChatSessionsByOwner: %v", err)
	}
	if len(list) != 1 || list[0].ID != sess.ID {
		t.Errorf("list returned %d sessions, expected 1 with id %s", len(list), sess.ID)
	}

	// Wrong-owner read returns ErrChatSessionNotFound.
	_, err = s.GetChatSession(ctx, sess.ID, uuid.NewString())
	if err != store.ErrChatSessionNotFound {
		t.Errorf("wrong-owner GetChatSession = %v, want ErrChatSessionNotFound", err)
	}

	// Patch: rename + pin.
	pinned := time.Now().UTC().Truncate(time.Microsecond)
	newTitle := "Renamed"
	if err := s.UpdateChatSessionMeta(ctx, sess.ID, owner, store.ChatSessionMetaPatch{
		Title:    &newTitle,
		PinnedAt: &pinned,
	}); err != nil {
		t.Fatalf("UpdateChatSessionMeta: %v", err)
	}
	got, _ = s.GetChatSession(ctx, sess.ID, owner)
	if got.Title != "Renamed" {
		t.Errorf("after patch Title = %q", got.Title)
	}
	if got.PinnedAt == nil || !got.PinnedAt.Equal(pinned) {
		t.Errorf("after patch PinnedAt = %v, want %v", got.PinnedAt, pinned)
	}

	// Soft delete.
	if err := s.SoftDeleteChatSession(ctx, sess.ID, owner); err != nil {
		t.Fatalf("SoftDeleteChatSession: %v", err)
	}
	// Default list excludes deleted.
	list, _, _ = s.ListChatSessionsByOwner(ctx, store.ChatSessionListOpts{OwnerID: owner})
	if len(list) != 0 {
		t.Errorf("after soft delete list returned %d, want 0", len(list))
	}
	// IncludeDeleted brings it back.
	list, _, _ = s.ListChatSessionsByOwner(ctx, store.ChatSessionListOpts{OwnerID: owner, IncludeDeleted: true})
	if len(list) != 1 {
		t.Errorf("with IncludeDeleted got %d, want 1", len(list))
	}
}

// TestPostgresChat_ArchivedOnlyFilter pins the user-facing
// "show me my archived chats" recovery surface contract:
//
//	?archived=true → ChatSessionListOpts.ArchivedOnly → query
//	  filters with archived_at IS NOT NULL.
//
// Default list and ArchivedOnly list must be disjoint — that's
// what makes the sidebar's "Archived" accordion work as
// recovery without showing the active group. ArchivedOnly takes
// precedence over IncludeArchived when both flags are passed.
func TestPostgresChat_ArchivedOnlyFilter(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "ArchTest", Slug: "at-" + hubID[:6],
		HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	// Seed two sessions: one stays active, one gets archived.
	now := time.Now().UTC().Truncate(time.Microsecond)
	active := &model.ChatSession{
		ID: uuid.NewString(), OwnerID: owner,
		ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
		Title: "Active", Model: "claude-sonnet-4-6", Tools: []string{"recall_memories"},
		ToolsetVersion: 1, Status: model.ChatSessionStatusIdle,
		CreatedAt: now, UpdatedAt: now,
	}
	archived := &model.ChatSession{
		ID: uuid.NewString(), OwnerID: owner,
		ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
		Title: "Archived", Model: "claude-sonnet-4-6", Tools: []string{"recall_memories"},
		ToolsetVersion: 1, Status: model.ChatSessionStatusIdle,
		CreatedAt: now, UpdatedAt: now,
	}
	for _, sess := range []*model.ChatSession{active, archived} {
		if err := s.CreateChatSession(ctx, sess); err != nil {
			t.Fatalf("CreateChatSession: %v", err)
		}
	}
	archivedAt := now
	if err := s.UpdateChatSessionMeta(ctx, archived.ID, owner, store.ChatSessionMetaPatch{
		ArchivedAt: &archivedAt,
	}); err != nil {
		t.Fatalf("UpdateChatSessionMeta(archive): %v", err)
	}

	// Default list returns the active row only.
	list, _, err := s.ListChatSessionsByOwner(ctx, store.ChatSessionListOpts{OwnerID: owner})
	if err != nil {
		t.Fatalf("default list: %v", err)
	}
	if len(list) != 1 || list[0].ID != active.ID {
		t.Errorf("default list = %d rows %v, want 1 row [%s]", len(list), idsOf(list), active.ID)
	}

	// ArchivedOnly returns the archived row only.
	list, _, err = s.ListChatSessionsByOwner(ctx, store.ChatSessionListOpts{
		OwnerID:      owner,
		ArchivedOnly: true,
	})
	if err != nil {
		t.Fatalf("archived-only list: %v", err)
	}
	if len(list) != 1 || list[0].ID != archived.ID {
		t.Errorf("ArchivedOnly list = %d rows %v, want 1 row [%s]", len(list), idsOf(list), archived.ID)
	}

	// IncludeArchived (combined admin view) returns both.
	list, _, err = s.ListChatSessionsByOwner(ctx, store.ChatSessionListOpts{
		OwnerID:         owner,
		IncludeArchived: true,
	})
	if err != nil {
		t.Fatalf("include-archived list: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("IncludeArchived list = %d rows %v, want 2", len(list), idsOf(list))
	}

	// Both flags passed → ArchivedOnly wins (strict NOT NULL).
	list, _, err = s.ListChatSessionsByOwner(ctx, store.ChatSessionListOpts{
		OwnerID:         owner,
		IncludeArchived: true,
		ArchivedOnly:    true,
	})
	if err != nil {
		t.Fatalf("both flags list: %v", err)
	}
	if len(list) != 1 || list[0].ID != archived.ID {
		t.Errorf("ArchivedOnly+IncludeArchived = %d rows %v, want 1 row [%s]", len(list), idsOf(list), archived.ID)
	}
}

func idsOf(list []model.ChatSession) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.ID
	}
	return out
}

// TestPostgresChat_ArchivedOnlyPaginates pins keyset pagination
// composing with the archive filter — the archived-only WHERE
// predicate must NOT regress the cursor branch's compound
// inequality on (last_message_at, id). A two-page archived list
// must return a stable, non-overlapping ordering across pages
// with the cursor handed back from page 1.
func TestPostgresChat_ArchivedOnlyPaginates(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "ArchPage", Slug: "ap-" + hubID[:6],
		HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}

	// Seed 5 sessions, each with one message so last_message_at
	// is set and ordering is deterministic. Older first so we
	// get distinct timestamps in DESC order on read.
	now := time.Now().UTC().Truncate(time.Microsecond)
	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		ts := now.Add(-time.Duration(i) * time.Minute)
		sess := &model.ChatSession{
			ID: uuid.NewString(), OwnerID: owner,
			ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
			Title: "p", Model: "claude-sonnet-4-6", Tools: []string{"recall_memories"},
			ToolsetVersion: 1, Status: model.ChatSessionStatusIdle,
			CreatedAt: ts, UpdatedAt: ts,
		}
		if err := s.CreateChatSession(ctx, sess); err != nil {
			t.Fatalf("CreateChatSession: %v", err)
		}
		msg := &model.ChatMessage{
			ID: uuid.NewString(), SessionID: sess.ID, Sequence: 1,
			Role: model.ChatMessageRoleUser, AuthorUserID: owner,
			Status: model.ChatMessageStatusCompleted,
			Content: "p", ToolsetVersionUsed: 1, CreatedAt: ts,
		}
		if err := s.AppendChatMessage(ctx, owner, msg); err != nil {
			t.Fatalf("AppendChatMessage: %v", err)
		}
		// Archive everything so ArchivedOnly returns the full set.
		at := ts
		if err := s.UpdateChatSessionMeta(ctx, sess.ID, owner, store.ChatSessionMetaPatch{
			ArchivedAt: &at,
		}); err != nil {
			t.Fatalf("UpdateChatSessionMeta(archive): %v", err)
		}
		ids = append(ids, sess.ID)
	}

	// Page 1: limit 2 → 2 rows + cursor.
	page1, cursor1, err := s.ListChatSessionsByOwner(ctx, store.ChatSessionListOpts{
		OwnerID:      owner,
		Limit:        2,
		ArchivedOnly: true,
	})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page 1 len = %d, want 2", len(page1))
	}
	if cursor1 == "" {
		t.Fatalf("page 1 cursor empty; expected more pages")
	}

	// Page 2: hand back the cursor; expect the next 2 archived
	// rows with no overlap.
	page2, cursor2, err := s.ListChatSessionsByOwner(ctx, store.ChatSessionListOpts{
		OwnerID:      owner,
		Limit:        2,
		ArchivedOnly: true,
		Cursor:       cursor1,
	})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page 2 len = %d, want 2", len(page2))
	}
	for _, p2 := range page2 {
		for _, p1 := range page1 {
			if p2.ID == p1.ID {
				t.Errorf("page 2 overlaps page 1 on %s", p2.ID)
			}
		}
	}

	// Page 3: last row + empty cursor (or short page indicating done).
	page3, _, err := s.ListChatSessionsByOwner(ctx, store.ChatSessionListOpts{
		OwnerID:      owner,
		Limit:        2,
		ArchivedOnly: true,
		Cursor:       cursor2,
	})
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if len(page3) != 1 {
		t.Errorf("page 3 len = %d, want 1 (final)", len(page3))
	}

	// Sanity: union of pages exactly covers the seeded set.
	gotAll := make(map[string]bool)
	for _, p := range append(append(page1, page2...), page3...) {
		gotAll[p.ID] = true
	}
	if len(gotAll) != len(ids) {
		t.Errorf("union of pages = %d, want %d", len(gotAll), len(ids))
	}
	for _, id := range ids {
		if !gotAll[id] {
			t.Errorf("missing %s from paginated archive set", id)
		}
	}
}

// TestPostgresChat_AppendMessageBumpsCounters pins the
// transactional invariant that AppendChatMessage updates
// session.message_count + last_message_at atomically.
func TestPostgresChat_AppendMessageBumpsCounters(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "x", Slug: "x-" + hubID[:6], HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sess := &model.ChatSession{
		ID: uuid.NewString(), OwnerID: owner,
		ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
		Title: "x", Model: "m", Tools: []string{},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateChatSession(ctx, sess); err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}

	for i := 1; i <= 3; i++ {
		msg := &model.ChatMessage{
			ID:                 uuid.NewString(),
			SessionID:          sess.ID,
			Sequence:           i,
			Role:               model.ChatMessageRoleUser,
			AuthorUserID:       owner,
			Status:             model.ChatMessageStatusCompleted,
			Content:            "hello",
			ToolsetVersionUsed: 1,
			CreatedAt:          time.Now().UTC().Truncate(time.Microsecond),
		}
		if err := s.AppendChatMessage(ctx, owner, msg); err != nil {
			t.Fatalf("AppendChatMessage[%d]: %v", i, err)
		}
	}
	got, _ := s.GetChatSession(ctx, sess.ID, owner)
	if got.MessageCount != 3 {
		t.Errorf("MessageCount = %d, want 3", got.MessageCount)
	}
	if got.LastMessageAt == nil {
		t.Errorf("LastMessageAt nil after appending")
	}
}

// TestPostgresChat_IdempotencyConflict pins ErrChatMessageIdempotencyConflict
// when two messages share the same (session_id, idempotency_key).
func TestPostgresChat_IdempotencyConflict(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "x", Slug: "x-" + hubID[:6], HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sess := &model.ChatSession{
		ID: uuid.NewString(), OwnerID: owner,
		ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
		Title: "x", Model: "m",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateChatSession(ctx, sess); err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	makeMsg := func(seq int) *model.ChatMessage {
		return &model.ChatMessage{
			ID:                 uuid.NewString(),
			SessionID:          sess.ID,
			Sequence:           seq,
			Role:               model.ChatMessageRoleUser,
			AuthorUserID:       owner,
			Status:             model.ChatMessageStatusCompleted,
			Content:            "x",
			IdempotencyKey:     "key-1",
			ToolsetVersionUsed: 1,
			CreatedAt:          time.Now().UTC().Truncate(time.Microsecond),
		}
	}
	if err := s.AppendChatMessage(ctx, owner, makeMsg(1)); err != nil {
		t.Fatalf("first append: %v", err)
	}
	err := s.AppendChatMessage(ctx, owner, makeMsg(2))
	if err != store.ErrChatMessageIdempotencyConflict {
		t.Errorf("second append err = %v, want ErrChatMessageIdempotencyConflict", err)
	}
}

// TestPostgresChat_ScopeCheckRejectsBadShape pins the SQL-side
// CHECK that rejects scope_type='single_hub' with len != 1.
// The handler validates the same shape Go-side; this test is
// belt+braces against a buggy direct-SQL writer.
func TestPostgresChat_ScopeCheckRejectsBadShape(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	bad := &model.ChatSession{
		ID:        uuid.NewString(),
		OwnerID:   owner,
		ScopeType: model.ChatScopeTypeSingleHub, // requires exactly 1
		// ScopeHubIDs: 0 → CHECK fails.
		Title:     "x",
		Model:     "m",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateChatSession(ctx, bad); err == nil {
		t.Errorf("CreateChatSession should reject single_hub with 0 hub_ids; got nil")
	}
}

// TestPostgresChat_FinalizeMessageResetsSessionLock pins the
// invariant that FinalizeChatMessage atomically writes the
// terminal message state AND resets the session's lock state
// (status='idle', active_message_id=NULL, locked_at=NULL).
func TestPostgresChat_FinalizeMessageResetsSessionLock(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "x", Slug: "x-" + hubID[:6], HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sess := &model.ChatSession{
		ID: uuid.NewString(), OwnerID: owner,
		ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
		Title: "x", Model: "m",
		Status:    model.ChatSessionStatusIdle,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateChatSession(ctx, sess); err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}

	// User msg.
	userMsg := &model.ChatMessage{
		ID: uuid.NewString(), SessionID: sess.ID, Sequence: 1,
		Role: model.ChatMessageRoleUser, AuthorUserID: owner,
		Status: model.ChatMessageStatusCompleted, Content: "ping",
		ToolsetVersionUsed: 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := s.AppendChatMessage(ctx, owner, userMsg); err != nil {
		t.Fatalf("append user msg: %v", err)
	}

	// Assistant msg in 'running' state. The handler would also
	// flip the session's status='running' + active_message_id;
	// we set those directly via SQL to match production order.
	asstMsg := &model.ChatMessage{
		ID: uuid.NewString(), SessionID: sess.ID, Sequence: 2,
		Role: model.ChatMessageRoleAssistant, AuthorUserID: owner,
		Status: model.ChatMessageStatusRunning, Content: "",
		ToolsetVersionUsed: 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := s.AppendChatMessage(ctx, owner, asstMsg); err != nil {
		t.Fatalf("append asst msg: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE chat_sessions SET status='running', active_message_id=$1::uuid, locked_at=now()
		 WHERE id=$2::uuid`,
		asstMsg.ID, sess.ID); err != nil {
		t.Fatalf("set session running: %v", err)
	}

	// Finalize.
	usage := json.RawMessage(`{"input_tokens":10,"output_tokens":20}`)
	if err := s.FinalizeChatMessage(ctx, asstMsg.ID, owner, store.ChatMessageFinalize{
		Status:     model.ChatMessageStatusCompleted,
		Content:    "pong",
		Usage:      usage,
		FinishedAt: time.Now().UTC().Truncate(time.Microsecond),
	}); err != nil {
		t.Fatalf("FinalizeChatMessage: %v", err)
	}

	got, _ := s.GetChatSession(ctx, sess.ID, owner)
	if got.Status != model.ChatSessionStatusIdle {
		t.Errorf("after finalize Status = %q, want idle", got.Status)
	}
	if got.ActiveMessageID != "" {
		t.Errorf("after finalize ActiveMessageID = %q, want empty", got.ActiveMessageID)
	}
	if got.LockedAt != nil {
		t.Errorf("after finalize LockedAt = %v, want nil", got.LockedAt)
	}

	gotMsg, _ := s.GetChatMessage(ctx, asstMsg.ID, owner)
	if gotMsg.Status != model.ChatMessageStatusCompleted {
		t.Errorf("msg Status = %q", gotMsg.Status)
	}
	if gotMsg.Content != "pong" {
		t.Errorf("msg Content = %q", gotMsg.Content)
	}
}

// TestPostgresChat_EventReplay pins (a) AppendChatMessageEvent +
// (b) ListChatMessageEvents-with-since-seq + (c) the seq-conflict
// sentinel.
func TestPostgresChat_EventReplay(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "x", Slug: "x-" + hubID[:6], HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sess := &model.ChatSession{
		ID: uuid.NewString(), OwnerID: owner,
		ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
		Title: "x", Model: "m", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateChatSession(ctx, sess); err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	asstMsg := &model.ChatMessage{
		ID: uuid.NewString(), SessionID: sess.ID, Sequence: 1,
		Role: model.ChatMessageRoleAssistant, AuthorUserID: owner,
		Status: model.ChatMessageStatusRunning, Content: "",
		ToolsetVersionUsed: 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := s.AppendChatMessage(ctx, owner, asstMsg); err != nil {
		t.Fatalf("AppendChatMessage: %v", err)
	}

	for i := 1; i <= 3; i++ {
		ev := &model.ChatMessageEvent{
			MessageID: asstMsg.ID,
			Seq:       i,
			EventType: "model.delta",
			Payload:   json.RawMessage(`{"delta":"x"}`),
			CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		}
		if err := s.AppendChatMessageEvent(ctx, owner, ev); err != nil {
			t.Fatalf("AppendChatMessageEvent[%d]: %v", i, err)
		}
	}
	// Conflict on seq=2.
	dup := &model.ChatMessageEvent{
		MessageID: asstMsg.ID, Seq: 2, EventType: "model.delta",
		Payload: json.RawMessage(`{}`), CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := s.AppendChatMessageEvent(ctx, owner, dup); err != store.ErrChatMessageEventSeqConflict {
		t.Errorf("dup append err = %v, want ErrChatMessageEventSeqConflict", err)
	}

	// Replay since seq=1 → seqs 2,3.
	evs, err := s.ListChatMessageEvents(ctx, asstMsg.ID, owner, 1)
	if err != nil {
		t.Fatalf("ListChatMessageEvents: %v", err)
	}
	if len(evs) != 2 {
		t.Errorf("replay returned %d events, want 2", len(evs))
	}
	if evs[0].Seq != 2 || evs[1].Seq != 3 {
		t.Errorf("replay seqs = [%d, %d], want [2, 3]", evs[0].Seq, evs[1].Seq)
	}
}

// TestPostgresChat_OwnerScopedWritesRejectCrossOwner pins the
// codex v1 High fix: AppendChatMessage / AppendChatMessageEvent /
// CreateChatToolApproval all enforce owner-scoping at write
// time. A buggy handler that passes another user's ownerID
// must NOT smuggle a row into someone else's session.
func TestPostgresChat_OwnerScopedWritesRejectCrossOwner(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	// Seed a SECOND user; the test will try to write into THIS
	// user's session using the wrong owner id.
	otherOwner := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, name, personal_plan_id, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'O', 'personal_early_access', now(), now())`,
		otherOwner, "other-"+otherOwner[:8]+"@x"); err != nil {
		t.Fatalf("seed other user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1::uuid`, otherOwner)
	})

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "x", Slug: "x-" + hubID[:6], HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sess := &model.ChatSession{
		ID: uuid.NewString(), OwnerID: owner,
		ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
		Title: "x", Model: "m", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateChatSession(ctx, sess); err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}

	// AppendChatMessage with wrong owner → ErrChatSessionNotFound.
	msg := &model.ChatMessage{
		ID: uuid.NewString(), SessionID: sess.ID, Sequence: 1,
		Role: model.ChatMessageRoleUser, AuthorUserID: owner,
		Status: model.ChatMessageStatusCompleted, Content: "hi",
		ToolsetVersionUsed: 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := s.AppendChatMessage(ctx, otherOwner, msg); err != store.ErrChatSessionNotFound {
		t.Errorf("AppendChatMessage with wrong owner = %v, want ErrChatSessionNotFound", err)
	}
	// Verify nothing landed.
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM chat_messages WHERE session_id = $1::uuid`, sess.ID).Scan(&count); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if count != 0 {
		t.Errorf("wrong-owner append should have inserted 0 rows; got %d", count)
	}

	// AppendChatMessage with right owner → succeeds, so we have a
	// real message id for the next two checks.
	if err := s.AppendChatMessage(ctx, owner, msg); err != nil {
		t.Fatalf("right-owner append failed: %v", err)
	}

	// AppendChatMessageEvent with wrong owner → ErrChatMessageNotFound.
	ev := &model.ChatMessageEvent{
		MessageID: msg.ID, Seq: 1, EventType: "model.delta",
		Payload:   json.RawMessage(`{}`),
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := s.AppendChatMessageEvent(ctx, otherOwner, ev); err != store.ErrChatMessageNotFound {
		t.Errorf("AppendChatMessageEvent with wrong owner = %v, want ErrChatMessageNotFound", err)
	}

	// CreateChatToolApproval with wrong owner → ErrChatMessageNotFound.
	ap := &model.ChatToolApproval{
		ID: uuid.NewString(), MessageID: msg.ID,
		ToolName: "forget_memory", Args: json.RawMessage(`{}`),
		ArgsHash: "x", RequestedAt: time.Now().UTC().Truncate(time.Microsecond),
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := s.CreateChatToolApproval(ctx, otherOwner, ap); err != store.ErrChatMessageNotFound {
		t.Errorf("CreateChatToolApproval with wrong owner = %v, want ErrChatMessageNotFound", err)
	}
}

// TestPostgresChat_SameSessionFK pins the codex v1 Medium fix:
// chat_messages.parent_message_id and chat_sessions.active_
// message_id BOTH have composite FKs that enforce same-session.
// A regenerate row's parent must be in the SAME session; an
// active_message_id must point at a message in the SAME session.
func TestPostgresChat_SameSessionFK(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "x", Slug: "x-" + hubID[:6], HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)

	// Two sessions, one message in each.
	sessA := &model.ChatSession{
		ID: uuid.NewString(), OwnerID: owner,
		ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
		Title: "A", Model: "m", CreatedAt: now, UpdatedAt: now,
	}
	sessB := &model.ChatSession{
		ID: uuid.NewString(), OwnerID: owner,
		ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
		Title: "B", Model: "m", CreatedAt: now, UpdatedAt: now,
	}
	for _, s2 := range []*model.ChatSession{sessA, sessB} {
		if err := s.CreateChatSession(ctx, s2); err != nil {
			t.Fatalf("CreateChatSession: %v", err)
		}
	}
	msgA := &model.ChatMessage{
		ID: uuid.NewString(), SessionID: sessA.ID, Sequence: 1,
		Role: model.ChatMessageRoleUser, AuthorUserID: owner,
		Status: model.ChatMessageStatusCompleted, Content: "a",
		ToolsetVersionUsed: 1, CreatedAt: now,
	}
	msgB := &model.ChatMessage{
		ID: uuid.NewString(), SessionID: sessB.ID, Sequence: 1,
		Role: model.ChatMessageRoleUser, AuthorUserID: owner,
		Status: model.ChatMessageStatusCompleted, Content: "b",
		ToolsetVersionUsed: 1, CreatedAt: now,
	}
	if err := s.AppendChatMessage(ctx, owner, msgA); err != nil {
		t.Fatalf("append A: %v", err)
	}
	if err := s.AppendChatMessage(ctx, owner, msgB); err != nil {
		t.Fatalf("append B: %v", err)
	}

	// Try to insert a regenerate in session B whose parent is
	// msg A (cross-session). Direct SQL — bypassing the store
	// helper — to prove the FK catches it.
	_, err := pool.Exec(ctx,
		`INSERT INTO chat_messages (id, session_id, sequence, role, author_user_id, status,
			content, parent_message_id, toolset_version_used, created_at)
		 VALUES (gen_random_uuid(), $1::uuid, 99, 'assistant', $2::uuid, 'completed',
		         '', $3::uuid, 1, now())`,
		sessB.ID, owner, msgA.ID)
	if err == nil {
		t.Errorf("cross-session parent_message_id should be rejected by FK; got nil")
	}

	// Try to set sessA.active_message_id to msgB (cross-session).
	_, err = pool.Exec(ctx,
		`UPDATE chat_sessions SET active_message_id = $1::uuid WHERE id = $2::uuid`,
		msgB.ID, sessA.ID)
	if err == nil {
		t.Errorf("cross-session active_message_id should be rejected by FK; got nil")
	}
}

// TestPostgresChat_OnDeleteSetNullColumnTargeted pins the codex
// v2 Medium fix: the composite same-session FKs use column-
// targeted `ON DELETE SET NULL (col)` so a default cascade
// doesn't try to NULL a non-null identity column (chat_messages.
// session_id or chat_sessions.id) and fail.
//
// Two scenarios:
//  1. Delete a regenerate parent → child's parent_message_id
//     becomes NULL but child's session_id stays intact.
//  2. Delete an active message → session's active_message_id
//     becomes NULL but session.id stays intact.
func TestPostgresChat_OnDeleteSetNullColumnTargeted(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "x", Slug: "x-" + hubID[:6], HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sess := &model.ChatSession{
		ID: uuid.NewString(), OwnerID: owner,
		ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
		Title: "x", Model: "m", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateChatSession(ctx, sess); err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}

	// Two messages: parent + regenerate child.
	parent := &model.ChatMessage{
		ID: uuid.NewString(), SessionID: sess.ID, Sequence: 1,
		Role: model.ChatMessageRoleAssistant, AuthorUserID: owner,
		Status:  model.ChatMessageStatusCompleted,
		Content: "v1", ToolsetVersionUsed: 1, CreatedAt: now,
	}
	if err := s.AppendChatMessage(ctx, owner, parent); err != nil {
		t.Fatalf("append parent: %v", err)
	}
	child := &model.ChatMessage{
		ID: uuid.NewString(), SessionID: sess.ID, Sequence: 2,
		Role: model.ChatMessageRoleAssistant, AuthorUserID: owner,
		Status:  model.ChatMessageStatusCompleted,
		Content: "v2", ParentMessageID: parent.ID,
		ToolsetVersionUsed: 1, CreatedAt: now,
	}
	if err := s.AppendChatMessage(ctx, owner, child); err != nil {
		t.Fatalf("append child: %v", err)
	}

	// Set the session's active_message_id to point at the parent.
	if _, err := pool.Exec(ctx,
		`UPDATE chat_sessions SET active_message_id = $1::uuid WHERE id = $2::uuid`,
		parent.ID, sess.ID); err != nil {
		t.Fatalf("set active_message_id: %v", err)
	}

	// Delete the parent — both cascades should fire.
	if _, err := pool.Exec(ctx,
		`DELETE FROM chat_messages WHERE id = $1::uuid`, parent.ID); err != nil {
		t.Fatalf("delete parent: %v", err)
	}

	// Child should still exist with parent_message_id = NULL,
	// session_id intact (NOT NULLed by the cascade).
	var childSessionID *string
	var childParent *string
	if err := pool.QueryRow(ctx,
		`SELECT session_id::text, parent_message_id::text FROM chat_messages WHERE id = $1::uuid`,
		child.ID).Scan(&childSessionID, &childParent); err != nil {
		t.Fatalf("scan child: %v (cascade may have deleted the child instead of nulling)", err)
	}
	if childSessionID == nil || *childSessionID != sess.ID {
		t.Errorf("after parent delete, child session_id = %v, want %s — cascade NULLed the wrong column",
			childSessionID, sess.ID)
	}
	if childParent != nil {
		t.Errorf("after parent delete, child parent_message_id = %v, want NULL", *childParent)
	}

	// Session should still exist with active_message_id = NULL,
	// id intact (NOT NULLed by the cascade — id is the PK).
	got, err := s.GetChatSession(ctx, sess.ID, owner)
	if err != nil {
		t.Fatalf("after parent delete, GetChatSession: %v (cascade may have NULLed session.id)", err)
	}
	if got.ActiveMessageID != "" {
		t.Errorf("after parent delete, ActiveMessageID = %q, want empty", got.ActiveMessageID)
	}
	if got.ID != sess.ID {
		t.Errorf("session.id changed after cascade: %q vs %q", got.ID, sess.ID)
	}
}

// TestPostgresChat_ApprovalDecisionIdempotent pins (a) decide
// pending → approved + (b) duplicate decide → ErrChatApprovalAlreadyDecided.
func TestPostgresChat_ApprovalDecisionIdempotent(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "x", Slug: "x-" + hubID[:6], HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sess := &model.ChatSession{
		ID: uuid.NewString(), OwnerID: owner,
		ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
		Title: "x", Model: "m", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateChatSession(ctx, sess); err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	asstMsg := &model.ChatMessage{
		ID: uuid.NewString(), SessionID: sess.ID, Sequence: 1,
		Role: model.ChatMessageRoleAssistant, AuthorUserID: owner,
		Status:             model.ChatMessageStatusRunning,
		ToolsetVersionUsed: 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := s.AppendChatMessage(ctx, owner, asstMsg); err != nil {
		t.Fatalf("AppendChatMessage: %v", err)
	}

	ap := &model.ChatToolApproval{
		ID:          uuid.NewString(),
		MessageID:   asstMsg.ID,
		ToolName:    "forget_memory",
		Args:        json.RawMessage(`{"id":"abc"}`),
		ArgsHash:    "deadbeef",
		Status:      model.ChatToolApprovalStatusPending,
		RequestedAt: time.Now().UTC().Truncate(time.Microsecond),
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := s.CreateChatToolApproval(ctx, owner, ap); err != nil {
		t.Fatalf("CreateChatToolApproval: %v", err)
	}
	// Default ExpiresAt = RequestedAt + 10m.
	got, err := s.GetChatToolApproval(ctx, ap.ID, owner)
	if err != nil {
		t.Fatalf("GetChatToolApproval: %v", err)
	}
	if got.Status != model.ChatToolApprovalStatusPending {
		t.Errorf("Status = %q, want pending", got.Status)
	}
	wantTTL := got.RequestedAt.Add(model.ChatToolApprovalTTL)
	if !got.ExpiresAt.Equal(wantTTL) {
		t.Errorf("ExpiresAt = %v, want %v (req+10m)", got.ExpiresAt, wantTTL)
	}

	// Decide.
	if err := s.DecideChatToolApproval(ctx, ap.ID, owner, "", model.ChatToolApprovalStatusApproved); err != nil {
		t.Fatalf("DecideChatToolApproval: %v", err)
	}
	got, _ = s.GetChatToolApproval(ctx, ap.ID, owner)
	if got.Status != model.ChatToolApprovalStatusApproved {
		t.Errorf("after decide Status = %q", got.Status)
	}

	// Duplicate decide → already-decided sentinel.
	err = s.DecideChatToolApproval(ctx, ap.ID, owner, "", model.ChatToolApprovalStatusDenied)
	if err != store.ErrChatApprovalAlreadyDecided {
		t.Errorf("duplicate decide err = %v, want ErrChatApprovalAlreadyDecided", err)
	}
}

// TestPostgresChat_ExpireChatToolApproval pins the single-row
// expiry path used by the SDK-host approver's local deadline
// timer (Phase 3.6b2 codex v2 follow-up). Three scenarios:
// (a) pending row → flips to timed_out + nil err; (b) row already
// approved (user won the race) → ErrChatApprovalAlreadyDecided +
// no mutation; (c) wrong owner → ErrChatApprovalNotFound.
func TestPostgresChat_ExpireChatToolApproval(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "x", Slug: "x-" + hubID[:6], HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sess := &model.ChatSession{
		ID: uuid.NewString(), OwnerID: owner,
		ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
		Title: "x", Model: "m", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateChatSession(ctx, sess); err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	asstMsg := &model.ChatMessage{
		ID: uuid.NewString(), SessionID: sess.ID, Sequence: 1,
		Role: model.ChatMessageRoleAssistant, AuthorUserID: owner,
		Status:             model.ChatMessageStatusRunning,
		ToolsetVersionUsed: 1, CreatedAt: now,
	}
	if err := s.AppendChatMessage(ctx, owner, asstMsg); err != nil {
		t.Fatalf("AppendChatMessage: %v", err)
	}

	mkApproval := func(id string) *model.ChatToolApproval {
		return &model.ChatToolApproval{
			ID:          id,
			MessageID:   asstMsg.ID,
			ToolName:    "forget_memory",
			Args:        json.RawMessage(`{"id":"abc"}`),
			ArgsHash:    "deadbeef",
			Status:      model.ChatToolApprovalStatusPending,
			RequestedAt: time.Now().UTC().Truncate(time.Microsecond),
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
		}
	}

	// (a) pending → timed_out.
	pending := mkApproval(uuid.NewString())
	if err := s.CreateChatToolApproval(ctx, owner, pending); err != nil {
		t.Fatalf("CreateChatToolApproval: %v", err)
	}
	if err := s.ExpireChatToolApproval(ctx, pending.ID, owner); err != nil {
		t.Fatalf("ExpireChatToolApproval (pending): %v", err)
	}
	got, _ := s.GetChatToolApproval(ctx, pending.ID, owner)
	if got.Status != model.ChatToolApprovalStatusTimedOut {
		t.Errorf("after expire Status = %q, want timed_out", got.Status)
	}

	// (b) Already-decided → ErrChatApprovalAlreadyDecided.
	decided := mkApproval(uuid.NewString())
	if err := s.CreateChatToolApproval(ctx, owner, decided); err != nil {
		t.Fatalf("CreateChatToolApproval: %v", err)
	}
	if err := s.DecideChatToolApproval(ctx, decided.ID, owner, "", model.ChatToolApprovalStatusApproved); err != nil {
		t.Fatalf("DecideChatToolApproval: %v", err)
	}
	if err := s.ExpireChatToolApproval(ctx, decided.ID, owner); err != store.ErrChatApprovalAlreadyDecided {
		t.Errorf("expire already-decided err = %v, want ErrChatApprovalAlreadyDecided", err)
	}
	got, _ = s.GetChatToolApproval(ctx, decided.ID, owner)
	if got.Status != model.ChatToolApprovalStatusApproved {
		t.Errorf("after expire-on-approved Status = %q, want approved (must not clobber)", got.Status)
	}

	// (c) Wrong owner → ErrChatApprovalNotFound. Pre-flip so we
	// know the row exists for the real owner; cross-owner read
	// must NOT find it.
	bystander := mkApproval(uuid.NewString())
	if err := s.CreateChatToolApproval(ctx, owner, bystander); err != nil {
		t.Fatalf("CreateChatToolApproval: %v", err)
	}
	if err := s.ExpireChatToolApproval(ctx, bystander.ID, "00000000-0000-0000-0000-000000000000"); err != store.ErrChatApprovalNotFound {
		t.Errorf("expire wrong-owner err = %v, want ErrChatApprovalNotFound", err)
	}
}

// TestPostgresChat_DecideRejectsExpiredPending pins the
// expiry-aware decide path (Phase 3.6b2 v2 codex follow-up).
// A user click that arrives after expires_at on a row that's
// still status=pending (e.g., the approver crashed before its
// timer fired) must NOT commit; instead, return
// ErrChatApprovalExpired so the handler can map to 408
// chat_approval_timeout.
func TestPostgresChat_DecideRejectsExpiredPending(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "x", Slug: "x-" + hubID[:6], HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sess := &model.ChatSession{
		ID: uuid.NewString(), OwnerID: owner,
		ScopeType: model.ChatScopeTypeSingleHub, ScopeHubIDs: []string{hubID},
		Title: "x", Model: "m", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateChatSession(ctx, sess); err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	asstMsg := &model.ChatMessage{
		ID: uuid.NewString(), SessionID: sess.ID, Sequence: 1,
		Role: model.ChatMessageRoleAssistant, AuthorUserID: owner,
		Status:             model.ChatMessageStatusRunning,
		ToolsetVersionUsed: 1, CreatedAt: now,
	}
	if err := s.AppendChatMessage(ctx, owner, asstMsg); err != nil {
		t.Fatalf("AppendChatMessage: %v", err)
	}
	ap := &model.ChatToolApproval{
		ID:          uuid.NewString(),
		MessageID:   asstMsg.ID,
		ToolName:    "forget_memory",
		Args:        json.RawMessage(`{"id":"abc"}`),
		ArgsHash:    "deadbeef",
		Status:      model.ChatToolApprovalStatusPending,
		RequestedAt: time.Now().UTC().Truncate(time.Microsecond),
		CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := s.CreateChatToolApproval(ctx, owner, ap); err != nil {
		t.Fatalf("CreateChatToolApproval: %v", err)
	}
	// Backdate expires_at into the past — simulates "approver
	// crashed before its local timer fired" or a user-decide
	// landing right at the boundary of a healthy approver.
	if _, err := pool.Exec(ctx,
		`UPDATE chat_tool_approvals SET expires_at = now() - interval '1 second' WHERE id = $1::uuid`,
		ap.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// Decide should return ErrChatApprovalExpired (not
	// AlreadyDecided — the row is still pending, just expired).
	err := s.DecideChatToolApproval(ctx, ap.ID, owner, "", model.ChatToolApprovalStatusApproved)
	if err != store.ErrChatApprovalExpired {
		t.Fatalf("err = %v, want ErrChatApprovalExpired", err)
	}
	// Row was NOT mutated.
	got, _ := s.GetChatToolApproval(ctx, ap.ID, owner)
	if got.Status != model.ChatToolApprovalStatusPending {
		t.Errorf("Status = %q, want pending (decide must not commit on expired row)", got.Status)
	}
}

// seedChatTurnSession is a tiny helper for the BeginChatMessageTurn
// suite — every test starts the same way: create hub, create idle
// chat session scoped to that hub. Returns (sessionID, hubID) so
// tests can pass hubID through CallerAccessibleHubs.
func seedChatTurnSession(t *testing.T, ctx context.Context, s store.Store, owner string) (string, string) {
	t.Helper()
	hubID := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubID, Name: "TurnTest", Slug: "tt-" + hubID[:6],
		HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatalf("CreateHub: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sess := &model.ChatSession{
		ID:             uuid.NewString(),
		OwnerID:        owner,
		ScopeType:      model.ChatScopeTypeSingleHub,
		ScopeHubIDs:    []string{hubID},
		Model:          "claude-sonnet-4-6",
		Tools:          []string{"recall_memories"},
		ToolsetVersion: 1,
		Status:         model.ChatSessionStatusIdle,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.CreateChatSession(ctx, sess); err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	return sess.ID, hubID
}

// TestPostgresChat_BeginTurn_FreshHappyPath verifies the canonical
// path: idle session, no idempotency hit → both rows inserted with
// the right sequence numbers, session flipped to running with
// active_message_id pinned to the assistant.
func TestPostgresChat_BeginTurn_FreshHappyPath(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()
	sessionID, hubID := seedChatTurnSession(t, ctx, s, owner)

	in := store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              owner,
		UserContent:          "Hello chat",
		IdempotencyKey:       uuid.NewString(),
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubID},
		Now:                  time.Now().UTC().Truncate(time.Microsecond),
	}
	res, err := s.BeginChatMessageTurn(ctx, in)
	if err != nil {
		t.Fatalf("BeginChatMessageTurn: %v", err)
	}
	if res.Outcome != store.ChatTurnFresh {
		t.Errorf("Outcome = %q, want fresh", res.Outcome)
	}
	if res.UserMessage == nil || res.UserMessage.Sequence != 1 {
		t.Errorf("UserMessage = %+v, want sequence=1", res.UserMessage)
	}
	if res.AssistantMessage == nil || res.AssistantMessage.Sequence != 2 {
		t.Errorf("AssistantMessage = %+v, want sequence=2", res.AssistantMessage)
	}
	if res.AssistantMessage.Status != model.ChatMessageStatusQueued {
		t.Errorf("Assistant status = %q, want queued", res.AssistantMessage.Status)
	}
	if res.AssistantMessage.ParentMessageID != res.UserMessage.ID {
		t.Errorf("Assistant.parent_message_id = %q, want user.id %q",
			res.AssistantMessage.ParentMessageID, res.UserMessage.ID)
	}

	// Session row reflects: status=running, active_message_id=assistant,
	// message_count=2.
	got, err := s.GetChatSession(ctx, sessionID, owner)
	if err != nil {
		t.Fatalf("GetChatSession: %v", err)
	}
	if got.Status != model.ChatSessionStatusRunning {
		t.Errorf("session.Status = %q, want running", got.Status)
	}
	if got.ActiveMessageID != res.AssistantMessage.ID {
		t.Errorf("session.ActiveMessageID = %q, want %q",
			got.ActiveMessageID, res.AssistantMessage.ID)
	}
	if got.MessageCount != 2 {
		t.Errorf("session.MessageCount = %d, want 2", got.MessageCount)
	}
	if got.Title != "Hello chat" {
		t.Errorf("session.Title = %q, want auto-title from first prompt", got.Title)
	}
}

func TestPostgresChat_BeginTurn_PreservesExistingTitle(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()
	sessionID, hubID := seedChatTurnSession(t, ctx, s, owner)
	title := "User named session"
	if err := s.UpdateChatSessionMeta(ctx, sessionID, owner, store.ChatSessionMetaPatch{Title: &title}); err != nil {
		t.Fatalf("UpdateChatSessionMeta: %v", err)
	}

	_, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              owner,
		UserContent:          "this should not replace the title",
		IdempotencyKey:       uuid.NewString(),
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubID},
		Now:                  time.Now().UTC().Truncate(time.Microsecond),
	})
	if err != nil {
		t.Fatalf("BeginChatMessageTurn: %v", err)
	}
	got, err := s.GetChatSession(ctx, sessionID, owner)
	if err != nil {
		t.Fatalf("GetChatSession: %v", err)
	}
	if got.Title != title {
		t.Errorf("session.Title = %q, want preserved title %q", got.Title, title)
	}
}

// TestPostgresChat_BeginTurn_RejectsLockedSession pins the
// "another turn already running" branch. Two BeginChatMessageTurn
// calls with DIFFERENT idempotency keys against the same session:
// first wins (fresh), second loses (ErrChatSessionLocked).
func TestPostgresChat_BeginTurn_RejectsLockedSession(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()
	sessionID, hubID := seedChatTurnSession(t, ctx, s, owner)

	mk := func(content, key string) store.BeginChatMessageTurnInput {
		return store.BeginChatMessageTurnInput{
			SessionID:            sessionID,
			OwnerID:              owner,
			UserContent:          content,
			IdempotencyKey:       key,
			UserMessageID:        uuid.NewString(),
			AssistantMessageID:   uuid.NewString(),
			CallerAccessibleHubs: []string{hubID},
			Now:                  time.Now().UTC().Truncate(time.Microsecond),
		}
	}
	if _, err := s.BeginChatMessageTurn(ctx, mk("first", uuid.NewString())); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	_, err := s.BeginChatMessageTurn(ctx, mk("second", uuid.NewString()))
	if err != store.ErrChatSessionLocked {
		t.Errorf("second turn err = %v, want ErrChatSessionLocked", err)
	}
}

// TestPostgresChat_BeginTurn_IdempotencyReplay seeds an in-flight
// turn, then finalizes the assistant to a terminal status, then
// re-issues the same idempotency key — outcome should be Replay
// with the SAME assistant message returned (not a new one).
func TestPostgresChat_BeginTurn_IdempotencyReplay(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()
	sessionID, hubID := seedChatTurnSession(t, ctx, s, owner)

	key := uuid.NewString()
	first, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              owner,
		UserContent:          "first",
		IdempotencyKey:       key,
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubID},
		Now:                  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// Drive the assistant to a terminal status; this is what the
	// runtime would do at the end of a successful turn.
	if err := s.FinalizeChatMessage(ctx, first.AssistantMessage.ID, owner, store.ChatMessageFinalize{
		Status:     model.ChatMessageStatusCompleted,
		Content:    "ack",
		ToolCalls:  json.RawMessage(`[]`),
		Citations:  json.RawMessage(`[]`),
		Usage:      json.RawMessage(`{}`),
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	// Replay path — same idempotency_key, fresh user/assistant ids
	// (handler-side they'd be re-generated).
	replay, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              owner,
		UserContent:          "first", // same content
		IdempotencyKey:       key,
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubID},
		Now:                  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.Outcome != store.ChatTurnReplay {
		t.Errorf("Outcome = %q, want replay", replay.Outcome)
	}
	if replay.AssistantMessage.ID != first.AssistantMessage.ID {
		t.Errorf("replay returned different assistant id %q (want %q)",
			replay.AssistantMessage.ID, first.AssistantMessage.ID)
	}
	if replay.UserMessage != nil {
		t.Errorf("replay UserMessage = %+v, want nil (no new insert)", replay.UserMessage)
	}
}

// TestPostgresChat_BeginTurn_IdempotencyInFlight catches the same
// idempotency_key arriving while the prior turn's assistant is
// still queued/running. Outcome is InFlight and we get back the
// original assistant id so the client can resume the SSE stream.
func TestPostgresChat_BeginTurn_IdempotencyInFlight(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()
	sessionID, hubID := seedChatTurnSession(t, ctx, s, owner)

	key := uuid.NewString()
	first, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              owner,
		UserContent:          "first",
		IdempotencyKey:       key,
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubID},
		Now:                  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	// Don't finalize — assistant stays in 'queued'. The replay
	// against the same key should now report InFlight.
	again, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              owner,
		UserContent:          "first",
		IdempotencyKey:       key,
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubID},
		Now:                  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("replay-while-in-flight: %v", err)
	}
	if again.Outcome != store.ChatTurnInFlight {
		t.Errorf("Outcome = %q, want in_flight", again.Outcome)
	}
	if again.AssistantMessage.ID != first.AssistantMessage.ID {
		t.Errorf("in_flight returned different assistant id")
	}
}

// TestPostgresChat_BeginTurn_RejectsCrossOwner pins owner-isolation:
// a caller passing someone else's session_id gets
// ErrChatSessionNotFound, not "session locked" or any other code
// that would leak existence.
func TestPostgresChat_BeginTurn_RejectsCrossOwner(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()
	sessionID, hubID := seedChatTurnSession(t, ctx, s, owner)

	other := uuid.NewString()
	_, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              other,
		UserContent:          "wrong owner",
		IdempotencyKey:       uuid.NewString(),
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubID},
		Now:                  time.Now().UTC(),
	})
	if err != store.ErrChatSessionNotFound {
		t.Errorf("cross-owner err = %v, want ErrChatSessionNotFound", err)
	}
}

// TestPostgresChat_BeginTurn_HardCap simulates a session that's
// 1 row away from the cap. The next BeginChatMessageTurn must
// reject with ErrChatSessionMsgCapReached because (count + 2 > 250).
// We can't actually insert 248 messages in a unit test — use a
// raw UPDATE to fast-forward the counter.
func TestPostgresChat_BeginTurn_HardCap(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatTestDB(t)
	ctx := context.Background()
	sessionID, hubID := seedChatTurnSession(t, ctx, s, owner)

	// Fast-forward to message_count = 249 — the next turn would
	// land at 251 which is past the 250 hard cap.
	if _, err := pool.Exec(ctx,
		`UPDATE chat_sessions SET message_count = $1 WHERE id = $2::uuid`,
		model.ChatMessageHardCap-1, sessionID); err != nil {
		t.Fatalf("fast-forward: %v", err)
	}
	_, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              owner,
		UserContent:          "one too many",
		IdempotencyKey:       uuid.NewString(),
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubID},
		Now:                  time.Now().UTC(),
	})
	if err != store.ErrChatSessionMsgCapReached {
		t.Errorf("over-cap err = %v, want ErrChatSessionMsgCapReached", err)
	}
}

// TestPostgresChat_BeginTurn_NoAccessibleHubs covers the case
// where the caller's live hub set has no overlap with the session's
// scope_hub_ids — e.g., user got kicked out of the only hub the
// session was scoped to. Send must reject so the agent can't run a
// turn with empty scope_hub_ids_used (which would silently bypass
// Phase 3.4's read-time redaction). Codex caught this on the
// Phase 3.3 v1 review.
func TestPostgresChat_BeginTurn_NoAccessibleHubs(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()
	sessionID, _ := seedChatTurnSession(t, ctx, s, owner)

	_, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              owner,
		UserContent:          "no scope overlap",
		IdempotencyKey:       uuid.NewString(),
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{uuid.NewString()}, // some other hub, NOT the session's
		Now:                  time.Now().UTC(),
	})
	if err != store.ErrChatSessionNoAccessibleHubs {
		t.Errorf("err = %v, want ErrChatSessionNoAccessibleHubs", err)
	}
}

// TestPostgresChat_BeginTurn_ToolsetVersionFromTx pins that the
// per-message toolset_version_used reflects the row's current
// toolset_version, NOT whatever the handler may have read before
// the tx. The fix in Phase 3.3 v2 was to read it inside FOR UPDATE.
// This test bumps the column AFTER seeding then issues a turn —
// the persisted message must carry the bumped version.
func TestPostgresChat_BeginTurn_ToolsetVersionFromTx(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatTestDB(t)
	ctx := context.Background()
	sessionID, hubID := seedChatTurnSession(t, ctx, s, owner)

	// Bump toolset_version directly on the row (simulating a
	// PATCH that landed between the handler's hypothetical
	// pre-fetch and the lock-flip).
	if _, err := pool.Exec(ctx,
		`UPDATE chat_sessions SET toolset_version = 7 WHERE id = $1::uuid`,
		sessionID); err != nil {
		t.Fatalf("bump: %v", err)
	}

	res, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              owner,
		UserContent:          "hi",
		IdempotencyKey:       uuid.NewString(),
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubID},
		Now:                  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("BeginChatMessageTurn: %v", err)
	}
	if res.UserMessage.ToolsetVersionUsed != 7 {
		t.Errorf("user.toolset_version_used = %d, want 7 (read inside tx)",
			res.UserMessage.ToolsetVersionUsed)
	}
	if res.AssistantMessage.ToolsetVersionUsed != 7 {
		t.Errorf("assistant.toolset_version_used = %d, want 7", res.AssistantMessage.ToolsetVersionUsed)
	}
}

// TestPostgresChat_FinalizeChatMessage_AlreadyFinalized pins the
// CAS-safe finalize gate (codex Phase 3.4a v1). Once a message
// reaches a terminal status, a second FinalizeChatMessage call
// MUST NOT overwrite it — the first finalize wins. Codex flagged
// this as a High because a sweeper or cancel path racing the
// worker could otherwise corrupt the real terminal state.
func TestPostgresChat_FinalizeChatMessage_AlreadyFinalized(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()
	sessionID, hubID := seedChatTurnSession(t, ctx, s, owner)

	res, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              owner,
		UserContent:          "first",
		IdempotencyKey:       uuid.NewString(),
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubID},
		Now:                  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	// First finalize wins.
	if err := s.FinalizeChatMessage(ctx, res.AssistantMessage.ID, owner, store.ChatMessageFinalize{
		Status:     model.ChatMessageStatusCompleted,
		Content:    "first finalize wins",
		ToolCalls:  json.RawMessage(`[]`),
		Citations:  json.RawMessage(`[]`),
		Usage:      json.RawMessage(`{"input_tokens":10}`),
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("first finalize: %v", err)
	}

	// Second finalize MUST be rejected with ErrChatMessageAlreadyFinalized
	// AND the row's content/status must be unchanged.
	err = s.FinalizeChatMessage(ctx, res.AssistantMessage.ID, owner, store.ChatMessageFinalize{
		Status:     model.ChatMessageStatusFailed,
		Content:    "should NOT overwrite",
		ToolCalls:  json.RawMessage(`[]`),
		Citations:  json.RawMessage(`[]`),
		Usage:      json.RawMessage(`{}`),
		FinishedAt: time.Now().UTC(),
	})
	if err != store.ErrChatMessageAlreadyFinalized {
		t.Errorf("second finalize err = %v, want ErrChatMessageAlreadyFinalized", err)
	}

	got, err := s.GetChatMessage(ctx, res.AssistantMessage.ID, owner)
	if err != nil {
		t.Fatalf("read after race: %v", err)
	}
	if got.Status != model.ChatMessageStatusCompleted {
		t.Errorf("status = %q, want completed (first finalize)", got.Status)
	}
	if got.Content != "first finalize wins" {
		t.Errorf("content = %q, want first finalize content", got.Content)
	}
}

// TestPostgresChat_FinalizeChatMessage_CancelingProtectedFromFailed
// pins the codex Phase 3.7b fix: a 'canceling' row must NOT be
// overwritten by a non-canceled finalize. Without this gate, a
// worker bottoming out via failMessage / the deferred safety net
// could stomp the user's cancel intent with status='failed'. The
// store CAS now rejects the failed write with
// ErrChatMessageCancelPending; the worker is expected to retry
// with status='canceled'.
func TestPostgresChat_FinalizeChatMessage_CancelingProtectedFromFailed(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()
	sessionID, hubID := seedChatTurnSession(t, ctx, s, owner)

	res, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              owner,
		UserContent:          "hello",
		IdempotencyKey:       uuid.NewString(),
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubID},
		Now:                  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	asstID := res.AssistantMessage.ID

	// Move the row to 'canceling' via the cancel path.
	if _, err := s.CancelChatMessage(ctx, asstID, owner); err != nil {
		t.Fatalf("CancelChatMessage: %v", err)
	}

	// A 'failed' finalize MUST be rejected with
	// ErrChatMessageCancelPending — the user asked to cancel, and
	// the only legal terminal for a canceling row is 'canceled'.
	err = s.FinalizeChatMessage(ctx, asstID, owner, store.ChatMessageFinalize{
		Status:     model.ChatMessageStatusFailed,
		Content:    "",
		ToolCalls:  json.RawMessage(`[]`),
		Citations:  json.RawMessage(`[]`),
		Usage:      json.RawMessage(`{}`),
		Error:      json.RawMessage(`{"code":"some_error"}`),
		FinishedAt: time.Now().UTC(),
	})
	if err != store.ErrChatMessageCancelPending {
		t.Errorf("failed finalize over canceling = %v, want ErrChatMessageCancelPending", err)
	}

	// Row should still be 'canceling' — failed write was a no-op.
	got, err := s.GetChatMessage(ctx, asstID, owner)
	if err != nil {
		t.Fatalf("read after race: %v", err)
	}
	if got.Status != model.ChatMessageStatusCanceling {
		t.Errorf("status = %q, want canceling (failed write must be a no-op)", got.Status)
	}

	// Now a 'canceled' finalize MUST succeed.
	if err := s.FinalizeChatMessage(ctx, asstID, owner, store.ChatMessageFinalize{
		Status:     model.ChatMessageStatusCanceled,
		Content:    "",
		ToolCalls:  json.RawMessage(`[]`),
		Citations:  json.RawMessage(`[]`),
		Usage:      json.RawMessage(`{}`),
		Error:      json.RawMessage(`{"code":"user_canceled"}`),
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("canceled finalize over canceling: %v", err)
	}
	got, _ = s.GetChatMessage(ctx, asstID, owner)
	if got.Status != model.ChatMessageStatusCanceled {
		t.Errorf("after canceled finalize, status = %q, want canceled", got.Status)
	}
}

// TestPostgresChat_BeginTurn_RejectsArchivedUnderLock pins the
// archived-race fix (codex Phase 3.3 v2): if the session is
// archived between the handler's pre-fetch and the store's FOR
// UPDATE, the store must still reject. Without the inside-tx
// archived check, BeginChatMessageTurn would silently insert.
func TestPostgresChat_BeginTurn_RejectsArchivedUnderLock(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatTestDB(t)
	ctx := context.Background()
	sessionID, hubID := seedChatTurnSession(t, ctx, s, owner)

	// Archive directly via SQL so we don't go through PATCH and
	// can simulate the race precisely.
	if _, err := pool.Exec(ctx,
		`UPDATE chat_sessions SET archived_at = now() WHERE id = $1::uuid`, sessionID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	_, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sessionID,
		OwnerID:              owner,
		UserContent:          "hi",
		IdempotencyKey:       uuid.NewString(),
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubID},
		Now:                  time.Now().UTC(),
	})
	if err != store.ErrChatSessionArchived {
		t.Errorf("err = %v, want ErrChatSessionArchived", err)
	}
}

// TestPostgresChat_UpdateChatSessionMeta_ToolsAtomic pins the
// PATCH-vs-Send race fix (codex Phase 3.3 v2). With the session
// flipped to 'running' (Send is in flight), a tools PATCH must
// return ErrChatSessionLocked from the store level, not silently
// win. The handler's pre-check is no longer the only line of
// defense.
func TestPostgresChat_UpdateChatSessionMeta_ToolsAtomic(t *testing.T) {
	t.Parallel()
	pool, s, owner := acquireChatTestDB(t)
	ctx := context.Background()
	sessionID, _ := seedChatTurnSession(t, ctx, s, owner)

	// Flip to 'running' directly — same shape as a real Send
	// having just acquired the lock.
	if _, err := pool.Exec(ctx,
		`UPDATE chat_sessions SET status='running', locked_at=now() WHERE id = $1::uuid`,
		sessionID); err != nil {
		t.Fatalf("flip running: %v", err)
	}

	newTools := []string{"recall_memories"}
	err := s.UpdateChatSessionMeta(ctx, sessionID, owner, store.ChatSessionMetaPatch{
		Tools: &newTools,
	})
	if err != store.ErrChatSessionLocked {
		t.Errorf("tools PATCH against running = %v, want ErrChatSessionLocked", err)
	}

	// The same patch with the session idle should succeed —
	// proves the gate is surgical.
	if _, err := pool.Exec(ctx,
		`UPDATE chat_sessions SET status='idle' WHERE id = $1::uuid`, sessionID); err != nil {
		t.Fatalf("flip idle: %v", err)
	}
	if err := s.UpdateChatSessionMeta(ctx, sessionID, owner, store.ChatSessionMetaPatch{
		Tools: &newTools,
	}); err != nil {
		t.Errorf("tools PATCH against idle: %v", err)
	}
}

// TestPostgresChat_BeginTurn_UserAllHubsSnapshotsCallerHubs pins
// the user_all_hubs branch of computeChatTurnScope: the persisted
// scope_hub_ids_used array snapshots the caller's live hub set
// (the resolver behavior at Phase 3.4 mirrors this), NOT the
// session's frozen scope_hub_ids (which is empty for this scope
// type by construction).
func TestPostgresChat_BeginTurn_UserAllHubsSnapshotsCallerHubs(t *testing.T) {
	t.Parallel()
	_, s, owner := acquireChatTestDB(t)
	ctx := context.Background()

	// Seed a user_all_hubs session by hand (the seed helper
	// uses single_hub).
	hubA := uuid.NewString()
	hubB := uuid.NewString()
	if err := s.CreateHub(&model.Hub{
		ID: hubA, Name: "A", Slug: "a-" + hubA[:6], HubType: "personal", OwnerID: owner,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateHub(&model.Hub{
		ID: hubB, Name: "B", Slug: "b-" + hubB[:6], HubType: "team", OwnerID: owner,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sess := &model.ChatSession{
		ID:             uuid.NewString(),
		OwnerID:        owner,
		ScopeType:      model.ChatScopeTypeUserAllHubs,
		ScopeHubIDs:    []string{},
		Model:          "claude-sonnet-4-6",
		Tools:          []string{"recall_memories"},
		ToolsetVersion: 1,
		Status:         model.ChatSessionStatusIdle,
		CreatedAt:      now, UpdatedAt: now,
	}
	if err := s.CreateChatSession(ctx, sess); err != nil {
		t.Fatal(err)
	}

	res, err := s.BeginChatMessageTurn(ctx, store.BeginChatMessageTurnInput{
		SessionID:            sess.ID,
		OwnerID:              owner,
		UserContent:          "hi",
		IdempotencyKey:       uuid.NewString(),
		UserMessageID:        uuid.NewString(),
		AssistantMessageID:   uuid.NewString(),
		CallerAccessibleHubs: []string{hubA, hubB},
		Now:                  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("BeginChatMessageTurn: %v", err)
	}
	if len(res.AssistantMessage.ScopeHubIDsUsed) != 2 {
		t.Errorf("ScopeHubIDsUsed = %v, want [hubA, hubB]", res.AssistantMessage.ScopeHubIDsUsed)
	}
}

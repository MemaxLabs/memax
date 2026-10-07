package v2api_test

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
)

// Rule 7 reaches a note (plan 25 §10, D13). A V1 memory is switched to V2
// with everything V1 kept of it: its chunk, a topic link, an attached
// file in object storage, the activity log's summary of its title, a Dream
// action's reason, a board card citing it, a notification about it. The
// switch offers it for bulk keep, so a proposal carries its words, which
// the judge and the index see. Forgetting the note takes the words out of
// every column of every schema, every stored object and every Redis key,
// on the real stack, within the minute.
func TestForgetANoteLeavesTheWordsNowhere(t *testing.T) {
	t.Parallel()
	s := newForgetStack(t)
	ctx := context.Background()
	zz := s.user("zz")
	sp := s.space(zz, policy.SpaceTeam, "acme")
	tok := s.session(zz)
	token := word("zn")
	title := "Vault rotation by " + token
	content := "The staging vault rotates every Thursday; ask " + token + " first."
	note := uuid.New()
	s.exec(`INSERT INTO memories (id, hub_id, owner_id, title, content, content_type, state, summary, tags)
	        VALUES ($1, $2, $3, $4, $5, 'text', 'active', $6, $7)`, note, sp.id, zz, title, content, "About "+token, []string{token})
	s.exec(`INSERT INTO chunks (memory_id, content, chunk_index, search_text, language, search_config, heading_chain)
	        VALUES ($1, $2, 0, $2, 'en', 'simple', $3)`, note, content, token)
	topic := uuid.New()
	s.exec(`INSERT INTO topics (id, owner_id, hub_id, name) VALUES ($1, $2, $3, 'Ops')`, topic, zz, sp.id)
	s.exec(`INSERT INTO memory_topics (memory_id, topic_id) VALUES ($1, $2)`, note, topic)
	key := "attachments/" + note.String() + "/vault.txt"
	if err := s.store.Put(ctx, objectstore.PutInput{Key: key, Body: bytes.NewReader([]byte("vault notes of " + token)),
		ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	s.exec(`INSERT INTO memory_attachments (id, memory_id, owner_id, filename, content_type, storage_key)
	        VALUES ($1, $2, $3, 'vault.txt', 'text/plain', $4)`, uuid.New(), note, zz, key)
	s.exec(`INSERT INTO usage_events (user_id, hub_id, operation, metadata) VALUES ($1, $2, 'push', jsonb_build_object('summary', $3::text))`,
		zz, sp.id, title)
	run := uuid.New()
	s.exec(`INSERT INTO dream_runs (id, owner_id, hub_id, status) VALUES ($1, $2, $3, 'completed')`, run, zz, sp.id)
	s.exec(`INSERT INTO dream_actions (run_id, action_type, source_memory_ids, reason) VALUES ($1, 'merge', $2, $3)`,
		run, []string{note.String()}, "Merged into "+token+"'s vault note")
	board := uuid.New()
	s.exec(`INSERT INTO boards (id, hub_id, created_by) VALUES ($1, $2, $3)`, board, sp.id, zz)
	s.exec(`INSERT INTO board_slots (board_id, slot_key, kind, title, payload, cite_memory_ids)
	        VALUES ($1, 'echo:a', 'echo', $2, jsonb_build_object('quote', $2::text), $3)`, board, "Ask "+token, []uuid.UUID{note})
	s.exec(`INSERT INTO notifications (audience, hub_id, kind, source_kind, source_id, payload)
	        VALUES ('hub', $1, 'review_contradiction', 'memory', $2, jsonb_build_object('title', $3::text, 'memory_id', $2::text))`,
		sp.id, note.String(), title)
	// Another V1 memory, which must keep its words.
	s.exec(`INSERT INTO memories (hub_id, owner_id, title, content, state) VALUES ($1, $2, 'Other', 'Use pnpm workspaces only.', 'active')`,
		sp.id, zz)

	// The switch: the note is a bulk-keep candidate, proposed through the
	// V1 import; the judge and the index see the proposal's words.
	scope, err := s.ledger.UserScope(ctx, zz)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.ledger.StartSwitchInline(ctx, ledger.Actor{Kind: policy.ActorPerson, ID: zz}, policy.ViaWeb, scope, sp.id,
		ledger.SwitchOptions{Key: "switch"})
	if err != nil || st.State != ledger.SwitchStateSwitched || st.Progress.Proposed != 2 {
		t.Fatalf("switch: %v %+v", err, st)
	}
	var proposal uuid.UUID
	if err := s.pool.QueryRow(ctx, `
		SELECT ms.memory_id FROM v2.memory_sources ms JOIN v2.sources s ON s.id = ms.source_id
		 WHERE s.locator ->> 'note' = $1`, note.String()).Scan(&proposal); err != nil {
		t.Fatal(err)
	}
	s.awaitEmbedded(proposal)

	// The grep sees the words where they are.
	before := s.grepDB(token)
	for _, want := range []string{"public.memories.content", "public.chunks.content", "public.usage_events.metadata",
		"public.dream_actions.reason", "public.board_slots.payload", "public.notifications.payload", "v2.memory_versions.statement"} {
		if !slices.ContainsFunc(before, func(h string) bool { return strings.HasPrefix(h, want+" ") }) {
			t.Errorf("before the Forget, the grep doesn't see the words in %s: %v", want, before)
		}
	}
	if len(s.grepObjects(token)) == 0 {
		t.Fatal("before the Forget, object storage doesn't hold the attachment")
	}

	var notes page[noteOut]
	base := "/v2/spaces/" + sp.slug
	s.do(call{method: "GET", path: base + "/notes?q=" + token, token: tok}).ok(http.StatusOK, &notes)
	if len(notes.Items) != 1 || notes.Items[0].ID != note {
		t.Fatalf("notes = %+v", notes.Items)
	}
	ref := notes.Items[0].Ref
	var pv struct {
		Carries []struct {
			Ref string `json:"ref"`
		} `json:"carries"`
	}
	s.do(call{method: "GET", path: base + "/notes/" + ref + "/forget-preview", token: tok}).ok(http.StatusOK, &pv)
	if len(pv.Carries) != 1 {
		t.Fatalf("carries = %+v", pv.Carries)
	}
	var res struct {
		Tombstone struct {
			OpID uuid.UUID `json:"op_id"`
		} `json:"tombstone"`
	}
	s.do(call{method: "POST", path: base + "/notes/" + ref + ":forget", token: tok,
		body: map[string]any{"carries": []string{pv.Carries[0].Ref}}}).ok(http.StatusOK, &res)
	forgotAt := time.Now()
	for s.count(`SELECT count(*) FROM v2.tombstones WHERE id = $1 AND status = 'done'`, res.Tombstone.OpID) == 0 {
		if time.Since(forgotAt) > time.Minute {
			t.Fatal("propagation didn't finish within the minute")
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Logf("propagation done %v after the note's Forget", time.Since(forgotAt).Round(time.Millisecond))

	if hits := s.grepDB(token); len(hits) != 0 {
		t.Errorf("after the Forget, the words are still in: %v", hits)
	}
	if hits := s.grepObjects(token); len(hits) != 0 {
		t.Errorf("after the Forget, object storage still holds them in: %v", hits)
	}
	if hits := s.grepRedis(token); len(hits) != 0 {
		t.Errorf("after the Forget, Redis still holds them in: %v", hits)
	}
	if s.count(`SELECT count(*) FROM memories WHERE content = 'Use pnpm workspaces only.'`) != 1 {
		t.Error("the Forget took another V1 memory")
	}
	if s.count(`SELECT count(*) FROM v2.note_refs WHERE note_id = $1 AND forgotten_at IS NOT NULL`, note) != 1 {
		t.Error("the note isn't recorded as forgotten")
	}
	s.sealAndVerify(sp.id)
}

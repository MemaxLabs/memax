package migrate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// conditionsVersion is migration 059 (typed conditions). Renumber it with
// the file if a merge moves it.
const conditionsVersion = 59

// TestConditionsMigrationTypesWhatWasThere: conditions written before 059
// (the judge's file_exists, a client's sentence) are typed where they can
// be and set aside otherwise, each memory with Memax's conditions_changed
// receipt on its stream; typed ones and empty lists are left alone; and
// from then on the database refuses an untyped list. It steps back and up
// again.
func TestConditionsMigrationTypesWhatWasThere(t *testing.T) {
	cs := withFreshDB(t)
	m := newMigrator(t, cs)
	if err := m.Migrate(conditionsVersion - 1); err != nil {
		t.Fatalf("migrate to %03d: %v", conditionsVersion-1, err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cs)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	const (
		person = "11111111-1111-1111-1111-111111111111"
		space  = "22222222-2222-2222-2222-222222222222"
	)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, name) VALUES ('`+person+`', 'rt@test', 'rt');
		INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ('`+space+`', 'P', 'rt-p', 'team', '`+person+`', 'project')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	legacy := map[int]string{
		1: `[{"kind": "file_exists", "path": "go.mod"}, "River is still the queue", {"kind": "before", "stale_after": "2027-01-01T00:00:00Z"}]`,
		2: `[{"kind": "dep_present", "manifest": "go.mod", "name": "github.com/riverqueue/river"}, {"kind": "pr_state", "repo": "a/b", "number": 7, "state": "merged"}]`,
		3: `[]`,
		4: `[{"id": "c1", "kind": "path_exists", "path": "go.mod", "origin": "person"}]`,
	}
	ids := map[int]string{}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.space_ids', '{`+space+`}', true), set_config('app.tenant_ids', '{`+person+`}', true)`); err != nil {
		t.Fatal(err)
	}
	for seq, conds := range legacy {
		var id, rc string
		if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text, gen_random_uuid()::text`).Scan(&id, &rc); err != nil {
			t.Fatal(err)
		}
		ids[seq] = id
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id,
			                         via, occurred_at, stream_id, stream_version)
			VALUES ($1, $2, $3, 'memory', $4, 'M-000' || $5::int, 'kept', 'person', $2, 'web', now(), $4, 1)`,
			rc, person, space, id, seq); err != nil {
			t.Fatalf("receipt: %v", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.memories (id, tenant_id, space_id, seq, section, kind, lifecycle, trust, conditions,
			                         created_receipt_id, last_receipt_id)
			VALUES ($1, $2, $3, $4, 'conventions', 'fact', 'kept', 'person', $5::jsonb, $6, $6)`,
			id, person, space, seq, conds, rc); err != nil {
			t.Fatalf("memory %d: %v", seq, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("seed commit: %v", err)
	}

	if err := m.Migrate(conditionsVersion); err != nil {
		t.Fatalf("migrate to %03d: %v", conditionsVersion, err)
	}
	read := func(seq int) (conds []map[string]any, version int, receipts int) {
		t.Helper()
		var raw []byte
		if err := pool.QueryRow(ctx, `SELECT conditions, stream_version FROM v2.memories WHERE id = $1`, ids[seq]).Scan(&raw, &version); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &conds); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM v2.receipts WHERE stream_id = $1 AND action = 'conditions_changed'
		                                AND actor_kind = 'memax' AND via = 'system' AND stream_version = 2 AND reason IS NOT NULL`,
			ids[seq]).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		return conds, version, receipts
	}
	c1, v1, r1 := read(1)
	if len(c1) != 1 || c1[0]["kind"] != "path_exists" || c1[0]["path"] != "go.mod" || c1[0]["id"] != "c1" ||
		c1[0]["origin"] != "judge" || v1 != 2 || r1 != 1 {
		t.Errorf("the judge's file_exists and a sentence: %v (stream %d, receipts %d)", c1, v1, r1)
	}
	c2, v2, r2 := read(2)
	if len(c2) != 2 || c2[0]["kind"] != "dep_present" || c2[1]["kind"] != "pr_state" || c2[1]["id"] != "c2" || v2 != 2 || r2 != 1 {
		t.Errorf("dep_present and pr_state: %v (stream %d, receipts %d)", c2, v2, r2)
	}
	for _, seq := range []int{3, 4} {
		if _, v, r := read(seq); v != 1 || r != 0 {
			t.Errorf("memory %d was already fine, but changed: stream %d, receipts %d", seq, v, r)
		}
	}
	// From here on an untyped list is refused, by the database itself.
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT v2.conditions_valid('[{"kind": "file_exists", "path": "go.mod"}]')`).Scan(&valid); err != nil || valid {
		t.Errorf("an untyped list is valid: %v %v", valid, err)
	}

	if err := migrateDownTo(t, m, conditionsVersion-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := m.Migrate(conditionsVersion); err != nil {
		t.Fatalf("up again: %v", err)
	}
}

package ledger_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/conditions"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// TestConditionsMatchSQL: the Go validator and v2.conditions_valid agree on
// every list, so the database holds the record to the same rules as the
// ledger. (Only Go runs the secret scan over file_contains' text.)
func TestConditionsMatchSQL(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	el := func(s string) string { return `[` + s + `]` }
	corpus := []string{
		`[]`,
		el(`{"id":"c1","kind":"path_exists","path":"packages/server/internal/ask/config.go","origin":"derived","from":"statement"}`),
		el(`{"id":"c1","kind":"path_exists","glob":"packages/web/**","origin":"derived","from":"scope"}`),
		el(`{"id":"c1","kind":"path_exists","path":"app/(ledger)/[space]/page.tsx","origin":"person"}`),
		el(`{"id":"c1","kind":"file_contains","path":"a/config.go","text":"TierHaiku","word":true,"origin":"derived","from":"statement"}`),
		el(`{"id":"c1","kind":"file_contains","path":"a/config.go","text":"Tier: Haiku","origin":"person"}`),
		el(`{"id":"c1","kind":"dep_present","manifest":"go.mod","name":"github.com/riverqueue/river","origin":"judge"}`),
		el(`{"id":"c1","kind":"dep_present","manifest":"packages/web/package.json","name":"@memaxlabs/ledger","range":"^2","origin":"person"}`),
		el(`{"id":"c1","kind":"file_unchanged","path":"docs/adr/004-queues.md","blob":"` + strings.Repeat("9f", 20) + `","origin":"person"}`),
		el(`{"id":"c1","kind":"pr_state","repo":"MemaxLabs/memax","number":198,"state":"merged","origin":"dream"}`),
		el(`{"id":"c1","kind":"path_exists","path":"a.go","origin":"person"},{"id":"c2","kind":"path_exists","path":"b.go","origin":"person"}`),
		// Refused by both.
		`{}`,
		`["go.mod is there"]`,
		el(`{"kind":"path_exists","path":"go.mod","origin":"person"}`),
		el(`{"id":"c0","kind":"path_exists","path":"go.mod","origin":"person"}`),
		el(`{"id":"c1","kind":"path_exists","path":"a.go","origin":"person"},{"id":"c1","kind":"path_exists","path":"b.go","origin":"person"}`),
		el(`{"id":"c1","kind":"file_exists","path":"go.mod","origin":"person"}`),
		el(`{"id":"c1","kind":"path_exists","path":"go.mod","origin":"agent"}`),
		el(`{"id":"c1","kind":"path_exists","path":"go.mod","origin":"derived"}`),
		el(`{"id":"c1","kind":"path_exists","path":"go.mod","origin":"person","from":"statement"}`),
		el(`{"id":"c1","kind":"path_exists","path":"go.mod","glob":"*.mod","origin":"person"}`),
		el(`{"id":"c1","kind":"path_exists","origin":"person"}`),
		el(`{"id":"c1","kind":"path_exists","path":"/etc/passwd","origin":"person"}`),
		el(`{"id":"c1","kind":"path_exists","path":"a/../b","origin":"person"}`),
		el(`{"id":"c1","kind":"path_exists","path":"a//b","origin":"person"}`),
		el(`{"id":"c1","kind":"path_exists","path":"a/b/","origin":"person"}`),
		el(`{"id":"c1","kind":"path_exists","path":"my file.go","origin":"person"}`),
		el(`{"id":"c1","kind":"path_exists","path":"` + strings.Repeat("a", 301) + `","origin":"person"}`),
		el(`{"id":"c1","kind":"path_exists","glob":"src/{a,b}","origin":"person"}`),
		el(`{"id":"c1","kind":"path_exists","path":"go.mod","origin":"person","note":"x"}`),
		el(`{"id":"c1","kind":"path_exists","path":1,"origin":"person"}`),
		el(`{"id":"c1","kind":"file_contains","path":"a.go","origin":"person"}`),
		el(`{"id":"c1","kind":"file_contains","path":"a.go","text":"` + strings.Repeat("x", 201) + `","origin":"person"}`),
		el(`{"id":"c1","kind":"file_contains","path":"a.go","text":"a\nb","origin":"person"}`),
		el(`{"id":"c1","kind":"file_contains","path":"a.go","text":"x","word":"yes","origin":"person"}`),
		el(`{"id":"c1","kind":"dep_present","manifest":"deps.txt","name":"x","origin":"person"}`),
		el(`{"id":"c1","kind":"dep_present","manifest":"go.mod","name":"a b","origin":"person"}`),
		el(`{"id":"c1","kind":"dep_present","manifest":"go.mod","origin":"person"}`),
		el(`{"id":"c1","kind":"file_unchanged","path":"a.md","blob":"abc","origin":"person"}`),
		el(`{"id":"c1","kind":"pr_state","repo":"memax","number":1,"state":"open","origin":"person"}`),
		el(`{"id":"c1","kind":"pr_state","repo":"a/b","number":0,"state":"open","origin":"person"}`),
		el(`{"id":"c1","kind":"pr_state","repo":"a/b","number":1.5,"state":"open","origin":"person"}`),
		el(`{"id":"c1","kind":"pr_state","repo":"a/b","number":"7","state":"open","origin":"person"}`),
		el(`{"id":"c1","kind":"pr_state","repo":"a/b","number":7,"state":"draft","origin":"person"}`),
	}
	nine := make([]string, 9)
	for i := range nine {
		nine[i] = `{"id":"c` + string(rune('1'+i)) + `","kind":"path_exists","path":"f` + string(rune('a'+i)) + `.go","origin":"person"}`
	}
	corpus = append(corpus, `[`+strings.Join(nine, ",")+`]`, `[`+strings.Join(nine[:8], ",")+`]`)

	ctx := context.Background()
	for _, raw := range corpus {
		_, goErr := conditions.Parse(json.RawMessage(raw))
		var sqlOK bool
		if err := f.pool.QueryRow(ctx, `SELECT v2.conditions_valid($1::jsonb)`, raw).Scan(&sqlOK); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if (goErr == nil) != sqlOK {
			t.Errorf("Go says %v, SQL says %v: %s", goErr, sqlOK, raw)
		}
	}
}

// project is a project space with a repository: Memax derives conditions
// for its memories.
func (f *fixture) project(owner uuid.UUID, name string) uuid.UUID {
	f.t.Helper()
	id := f.space(owner, policy.SpaceProject, name)
	f.exec(`UPDATE hubs SET repository = 'MemaxLabs/memax' WHERE id = $1`, id)
	return id
}

func condsOf(t *testing.T, m *ledger.Memory) []conditions.Condition {
	t.Helper()
	cs, err := conditions.Parse(m.Conditions)
	if err != nil {
		t.Fatalf("%s's conditions: %v (%s)", m.Ref, err, m.Conditions)
	}
	return cs
}

// Derived conditions ride on every command that writes a version, in a
// project space with a repository: Remember (with what its sources cite),
// an edit (the words' paths change, the sources' stay, ids hold), Undo
// (back to the old words' conditions), and Verify's update. A person's
// own conditions stay through all of it.
func TestConditionsAreDerivedWithEveryVersion(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.project(zz, "memax-v2")
	scope := f.scope(zz)
	nm := fact(sp, "Ask's tiers are set in `packages/server/internal/ask/config.go` as `TierHaiku`.")
	nm.Sources = []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: "go.mod:14"}, {Kind: ledger.SourceFile, Ref: "CLAUDE.md:12"}}
	nm.Conditions = json.RawMessage(`[{"kind":"pr_state","repo":"MemaxLabs/memax","number":198,"state":"merged"}]`)
	res := f.apply(&ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: nm})
	got := condsOf(t, res.Memory)
	want := []conditions.Condition{
		{ID: "c1", Kind: conditions.PRState, Repo: "MemaxLabs/memax", Number: 198, State: "merged", Origin: conditions.OriginPerson},
		{ID: "c2", Kind: conditions.PathExists, Path: "packages/server/internal/ask/config.go", Origin: conditions.OriginDerived, From: conditions.FromStatement},
		{ID: "c3", Kind: conditions.FileContains, Path: "packages/server/internal/ask/config.go", Text: "TierHaiku", Word: true,
			Origin: conditions.OriginDerived, From: conditions.FromStatement},
		{ID: "c4", Kind: conditions.PathExists, Path: "go.mod", Origin: conditions.OriginDerived, From: conditions.FromSource},
	}
	if len(got) != len(want) {
		t.Fatalf("on Remember: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("on Remember [%d]: %+v, want %+v", i, got[i], want[i])
		}
	}

	// An edit: the file moved. The person's and the source's stay; the
	// words' change; what checks the same keeps its id.
	edit := f.apply(&ledger.Edit{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: res.Memory.Ref, ExpectedVersion: 1,
		Statement: "Ask's tiers are set in `packages/server/internal/ask/tiers.go` as `TierHaiku`."})
	got = condsOf(t, edit.Memory)
	paths := map[string]string{}
	for _, c := range got {
		paths[string(c.Kind)+" "+c.Path+c.Repo] = c.ID
	}
	if len(got) != 4 || paths["pr_state MemaxLabs/memax"] != "c1" || paths["path_exists go.mod"] != "c4" ||
		paths["path_exists packages/server/internal/ask/tiers.go"] != "c2" || paths["file_contains packages/server/internal/ask/tiers.go"] != "c3" {
		t.Errorf("after the edit: %v", paths)
	}
	// Undo: the old words, and their conditions back, ids and all.
	undo := f.apply(&ledger.Undo{Meta: meta(person(zz), scope, policy.ViaWeb), Receipt: edit.Receipts[0].ID})
	back, err := f.l.GetMemory(context.Background(), scope, res.Memory.Ref)
	if err != nil || undo.Outcome != ledger.OutcomeApplied {
		t.Fatal(err, undo.Policy)
	}
	if string(back.Conditions) != string(res.Memory.Conditions) {
		t.Errorf("after undo: %s, want %s", back.Conditions, res.Memory.Conditions)
	}
	f.everyRowReceipted(t)
}

// Who attaches what: people may send conditions; agents may not (Memax
// derives them); a malformed or secret-bearing one is refused before
// anything is written; and nothing is derived in a space with no
// repository to check against.
func TestSentConditions(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.project(zz, "memax-v2")
	scope := f.scope(zz)
	ctx := context.Background()
	for name, raw := range map[string]string{
		"an untyped sentence": `["it stays true while River is the queue"]`,
		"an unknown field":    `[{"kind":"path_exists","path":"go.mod","note":"x"}]`,
		"an absolute path":    `[{"kind":"path_exists","path":"/etc/hosts"}]`,
		"a secret":            `[{"kind":"file_contains","path":".env","text":"AKIAIOSFODNN7EXAMPLE"}]`,
	} {
		nm := fact(sp, "A fact with "+name+".")
		nm.Conditions = json.RawMessage(raw)
		if _, err := f.l.Apply(ctx, &ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: nm}); !errors.Is(err, ledger.ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
	nm := fact(sp, "Agents can't attach conditions.")
	nm.Conditions = json.RawMessage(`[{"kind":"path_exists","path":"go.mod"}]`)
	if _, err := f.l.Apply(ctx, &ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP), NewMemory: nm}); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("an agent's conditions: %v", err)
	}
	// A project with no repository, and a personal space: nothing derived.
	bare := f.space(zz, policy.SpaceProject, "no-repo")
	m := f.remember(zz, bare, "Jobs live in `packages/server/internal/queue`.")
	if string(m.Conditions) != "[]" {
		t.Errorf("derived without a repository: %s", m.Conditions)
	}
}

// The database refuses untyped conditions from any writer, the ledger's
// path aside: a raw update with its receipt still fails the CHECK.
func TestDatabaseRefusesUntypedConditions(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.project(zz, "memax-v2")
	m := f.remember(zz, sp, "Receipts are enforced by the database.")
	err := f.asV2([]uuid.UUID{sp}, []uuid.UUID{m.TenantID}, func(tx pgx.Tx) error {
		ctx := context.Background()
		rid := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, via, occurred_at, stream_id, stream_version)
			VALUES ($1, $2, $3, 'memory', $4, $5, 'conditions_changed', 'memax', 'system', now(), $4, 2)`,
			rid, m.TenantID, sp, m.ID, m.Ref); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE v2.memories SET conditions = '[{"kind":"file_exists","path":"go.mod"}]', stream_version = 2, last_receipt_id = $2 WHERE id = $1`,
			m.ID, rid)
		return err
	})
	if sqlstate(err) != "23514" {
		t.Errorf("an untyped list: %v (sqlstate %s), want a CHECK violation", err, sqlstate(err))
	}
}

// Verify's update writes a version too: its words' conditions come with it.
func TestVerifyUpdateDerivesConditions(t *testing.T) {
	t.Parallel()
	w := newStaleWorld(t)
	w.f.exec(`UPDATE hubs SET repository = 'MemaxLabs/memax' WHERE id = $1`, w.space)
	cmd := w.verifyCmd(person(w.owner), policy.ViaWeb, ledger.VerifyUpdate)
	cmd.Statement = "Ask's tiers are set in `packages/server/internal/ask/tiers.go`."
	res := w.f.apply(cmd)
	got := condsOf(t, res.Memory)
	if len(got) != 1 || got[0].Kind != conditions.PathExists || got[0].Path != "packages/server/internal/ask/tiers.go" ||
		got[0].Origin != conditions.OriginDerived || got[0].ID != "c1" {
		t.Errorf("after Verify's update: %+v", got)
	}
}

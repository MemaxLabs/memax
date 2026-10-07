package ledger_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// A person's own Remember is one Undo away for their window: undoing it
// withdraws the memory (rejected, with an undid receipt that names the
// Remember), and nothing else may.
func TestUndoAPersonsRemember(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(sp, jy, "member")
	f.brief(zz, sp, 0, nil)
	target := f.target(zz, sp, ledger.TargetAgentsMD)
	refusal := func(err error) string {
		var ue *ledger.UndoError
		if errors.As(err, &ue) {
			return ue.Reason
		}
		return ""
	}

	t.Run("withdrawn, with its receipt, and recompiled", func(t *testing.T) {
		res := f.apply(&ledger.Remember{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), NewMemory: fact(sp, "Pin Node 24 in CI.")})
		m, rc := res.Memory, res.Receipts[0]
		if m.Lifecycle != lifecycle.Kept {
			t.Fatalf("remember = %s", m.Lifecycle)
		}
		gen := f.get(zz, target.ID).DirtyGen
		key := meta(person(zz), f.scope(zz), policy.ViaWeb)
		u, err := f.l.Apply(ctx, &ledger.Undo{Meta: key, Receipt: rc.ID})
		if err != nil || u.Outcome != ledger.OutcomeApplied {
			t.Fatalf("undo = %v %s", err, u.Outcome)
		}
		got := u.Receipts[0]
		if got.Action != ledger.ActionUndid || got.ObjectID != m.ID || got.Source == nil ||
			*got.Source != (ledger.ReceiptSource{Kind: "receipt", Ref: rc.ID.String()}) || got.Reason != "Withdrawn: undid remembering it." {
			t.Errorf("undo receipt = %+v", got)
		}
		after := f.mem(zz, m.ID)
		if after.Lifecycle != lifecycle.Rejected || after.State != lifecycle.MarkRejected || len(after.Flags) != 0 || after.Statement != m.Statement {
			t.Errorf("after = %s %s %v %q", after.Lifecycle, after.State, after.Flags, after.Statement)
		}
		if g := f.get(zz, target.ID).DirtyGen; g <= gen {
			t.Errorf("dirty_gen = %d, was %d: a withdrawn memory leaves the compiled files", g, gen)
		}
		// Once.
		if again, err := f.l.Apply(ctx, &ledger.Undo{Meta: key, Receipt: rc.ID}); err != nil || !again.Replayed {
			t.Errorf("replay = %v %+v", err, again)
		}
		if _, err := undo(f, person(zz), f.scope(zz), rc.ID); refusal(err) != ledger.UndoAlreadyUndone {
			t.Errorf("twice: %v", err)
		}
	})
	t.Run("a Remember that went to Review is withdrawn too", func(t *testing.T) {
		// A team decision remembered from the CLI waits for a web sign-in.
		team := f.space(zz, policy.SpaceTeam, "platform")
		res := f.apply(&ledger.Remember{Meta: meta(person(zz), f.scope(zz).Narrow(team), policy.ViaCLI),
			NewMemory: decisionIn(team, "Deploy the v2 API to Fly.io.", "deploy target")})
		if res.Memory.Lifecycle != lifecycle.Proposed {
			t.Fatalf("a team decision from the CLI = %s", res.Memory.Lifecycle)
		}
		if _, err := undo(f, person(zz), f.scope(zz), res.Receipts[0].ID); err != nil {
			t.Fatal(err)
		}
		if m := f.mem(zz, res.Memory.ID); m.Lifecycle != lifecycle.Rejected {
			t.Errorf("after = %s", m.Lifecycle)
		}
	})
	t.Run("refusals", func(t *testing.T) {
		remember := func(s string) ledger.Result {
			return f.apply(&ledger.Remember{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), NewMemory: fact(sp, s)})
		}
		// Someone else's Remember.
		r := remember("One.")
		if res, err := undo(f, person(jy), f.scope(jy), r.Receipts[0].ID); err != nil || res.Policy.Code != policy.CodeUndoByDecider {
			t.Errorf("not yours: %v %s", err, res.Policy.Code)
		}
		// An agent's write isn't a person's Remember.
		agent := f.apply(&ledger.Remember{Meta: meta(agentFor(policy.AutonomyWrite), f.scope(zz), policy.ViaMCP), NewMemory: fact(sp, "Two.")})
		if _, err := undo(f, person(zz), f.scope(zz), agent.Receipts[0].ID); refusal(err) != ledger.UndoNotUndoable {
			t.Errorf("an agent's write: %v", err)
		}
		// A later change depends on it.
		r = remember("Three.")
		f.apply(&ledger.Edit{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), Memory: r.Memory.Ref, ExpectedVersion: 1, Statement: "Three, edited."})
		if _, err := undo(f, person(zz), f.scope(zz), r.Receipts[0].ID); refusal(err) != ledger.UndoLaterChanges {
			t.Errorf("later change: %v", err)
		}
		// The Brief places it now.
		r = remember("Four.")
		cur, err := f.l.GetBrief(ctx, f.scope(zz), sp)
		if err != nil {
			t.Fatal(err)
		}
		f.brief(zz, sp, cur.Version, demoSections(r.Memory.Ref))
		if _, err := undo(f, person(zz), f.scope(zz), r.Receipts[0].ID); refusal(err) != ledger.UndoLaterChanges {
			t.Errorf("in the Brief: %v", err)
		}
		// Past the window.
		short := ledger.New(f.pool, ledger.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
			ledger.WithUndoWindows(time.Millisecond, time.Hour))
		old, err := short.Apply(ctx, &ledger.Remember{Meta: meta(person(zz), f.scope(zz), policy.ViaWeb), NewMemory: fact(sp, "Five.")})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
		if _, err := undo(f, person(zz), f.scope(zz), old.Receipts[0].ID); refusal(err) != ledger.UndoWindowPassed {
			t.Errorf("past the window: %v", err)
		}
	})
	f.everyRowReceipted(t)
}

// The database admits kept → rejected only as the undo of the Remember
// that created the memory: beside an undid receipt, written in the same
// transaction, whose source is the memory's creating receipt.
func TestDatabaseAdmitsWithdrawingOnlyAsUndo(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.space(zz, policy.SpaceProject, "memax-v2")
	withdraw := func(m *ledger.Memory, action, source string) error {
		return f.asV2([]uuid.UUID{sp}, []uuid.UUID{m.TenantID}, func(tx pgx.Tx) error {
			ctx := context.Background()
			rid := uuid.Must(uuid.NewV7())
			var src any
			if source != "" {
				src = `{"kind": "receipt", "ref": "` + source + `"}`
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id, via,
				                         source, occurred_at, stream_id, stream_version)
				VALUES ($1, $2, $3, 'memory', $4, $5, $6, 'person', $7, 'web', $8::jsonb, now(), $4, 2)`,
				rid, m.TenantID, m.SpaceID, m.ID, m.Ref, action, zz, src); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE v2.memories SET lifecycle = 'rejected', last_receipt_id = $2, stream_version = 2 WHERE id = $1`,
				m.ID, rid)
			return err
		})
	}
	for _, c := range []struct {
		name   string
		action string
		source func(m *ledger.Memory) string
	}{
		{"a rejected receipt", "rejected", func(*ledger.Memory) string { return "" }},
		{"an undid receipt of nothing", "undid", func(*ledger.Memory) string { return "" }},
		{"an undid receipt of another command", "undid", func(*ledger.Memory) string { return uuid.NewString() }},
	} {
		m := f.remember(zz, sp, "Kept "+c.name+".")
		if err := withdraw(m, c.action, c.source(m)); sqlstate(err) != "MXL01" {
			t.Errorf("%s: %v, want MXL01", c.name, err)
		}
	}
	m := f.remember(zz, sp, "Withdrawn.")
	if err := withdraw(m, "undid", m.CreatedReceiptID.String()); err != nil {
		t.Errorf("the undo of its Remember: %v", err)
	}
}

// v2.lifecycle_undo_allowed is lifecycle.UndoAllowed.
func TestUndoMatchesSQL(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for _, a := range lifecycle.Lifecycles {
		for _, b := range lifecycle.Lifecycles {
			var got bool
			if err := f.pool.QueryRow(context.Background(), `SELECT v2.lifecycle_undo_allowed($1, $2)`, string(a), string(b)).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if want := lifecycle.UndoAllowed(a, b); got != want {
				t.Errorf("undo %q → %q: SQL %v, Go %v", a, b, got, want)
			}
		}
	}
}

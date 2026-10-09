package forget_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/compile/compiletest"
	"github.com/MemaxLabs/memax/packages/server/internal/forget"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore/mockobjectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type fixture struct {
	t     *testing.T
	pool  *pgxpool.Pool
	l     *ledger.Ledger
	store *mockobjectstore.Store
	svc   *compile.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	_, pool := testdb.Acquire(t)
	f := &fixture{t: t, pool: pool, store: mockobjectstore.New()}
	f.l = ledger.New(pool, ledger.WithLogger(quiet))
	f.svc = compile.New(f.l, &compiletest.Fake{}, f.store, compile.Config{Log: quiet})
	return f
}

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("exec: %v", err)
	}
}

func (f *fixture) user(name string) uuid.UUID {
	id := uuid.New()
	f.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`, id, id.String()[:8]+"@"+name+".test", name)
	return id
}

func (f *fixture) space(owner uuid.UUID) uuid.UUID {
	id := uuid.New()
	f.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind) VALUES ($1, 'Memax V2', $2, 'team', $3, 'project')`,
		id, "memax-v2-"+id.String()[:6], owner)
	f.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, id, owner)
	return id
}

func (f *fixture) meta(user uuid.UUID, via policy.Via) ledger.Meta {
	f.t.Helper()
	s, err := f.l.UserScope(context.Background(), user)
	if err != nil {
		f.t.Fatal(err)
	}
	return ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: user}, Scope: s, Via: via, IdempotencyKey: uuid.NewString()}
}

func (f *fixture) apply(cmd ledger.Command) ledger.Result {
	f.t.Helper()
	res, err := f.l.Apply(context.Background(), cmd)
	if err != nil {
		f.t.Fatalf("Apply %s: %v", cmd.Name(), err)
	}
	if res.Outcome == ledger.OutcomeRefused {
		f.t.Fatalf("Apply %s refused: %s", cmd.Name(), res.Policy.Message)
	}
	return res
}

func (f *fixture) run(t *ledger.Target) {
	f.t.Helper()
	if _, err := f.svc.Run(context.Background(), ledger.CompileTargetArgs{TargetID: t.ID, SpaceID: t.SpaceID}, compile.RunOptions{NoWait: true}); err != nil {
		f.t.Fatalf("Run: %v", err)
	}
}

// everything is every stored object's content.
func (f *fixture) everything() string {
	f.t.Helper()
	var b strings.Builder
	for _, k := range f.store.Keys() {
		r, err := f.store.Get(context.Background(), k)
		if err != nil {
			f.t.Fatal(err)
		}
		raw, _ := io.ReadAll(r.Body)
		b.WriteString(k + "\n" + string(raw) + "\n")
	}
	return b.String()
}

// The propagation job recompiles every target that held the memory, takes
// its lines out of every stored artifact (the old compiles and a hand
// edit's stored copy), purges the caches, writes the forget ledger's copy
// (ids only), and completes the tombstone, well inside the minute.
func TestPropagationReachesEveryCopy(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz)
	const words = "Zanzibar quokka rotates the staging keys every Thursday"
	m := f.apply(&ledger.Remember{Meta: f.meta(zz, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: space, Statement: words + ".", Section: ledger.SectionConventions}}).Memory
	o := f.apply(&ledger.Remember{Meta: f.meta(zz, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: space, Statement: "Use pnpm workspaces only.", Section: ledger.SectionConventions}}).Memory
	f.apply(&ledger.ReviseBrief{Meta: f.meta(zz, policy.ViaWeb), SpaceID: space, Title: "Memax V2", Sections: []ledger.BriefSection{
		{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{{Ref: m.Ref}, {Ref: o.Ref}}}}})
	agents := f.apply(&ledger.ConfigureTarget{Meta: f.meta(zz, policy.ViaWeb), SpaceID: space, Kind: ledger.TargetAgentsMD}).Target
	chatgpt := f.apply(&ledger.ConfigureTarget{Meta: f.meta(zz, policy.ViaWeb), SpaceID: space, Kind: ledger.TargetChatGPT}).Target
	f.run(agents)
	f.run(chatgpt)
	scope, err := f.l.SpaceScope(ctx, space)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.svc.Preview(ctx, scope, agents.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.apply(&ledger.RecordDelivery{Meta: f.meta(zz, policy.ViaCLI), Target: agents.ID, Compile: p.Compile.Ref, SHA256: p.Compile.DriftSHA256})
	// A hand edit on the device: it keeps the line, and adds one.
	edited := p.Files[0].Content + "- Prefer named exports.\n"
	if _, err := f.svc.Observe(ctx, f.meta(zz, policy.ViaCLI), agents.ID, compile.ObserveInput{
		Path: "AGENTS.md", Content: edited, ObserverKind: ledger.ObserverDevice, ObserverID: "zz-laptop"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.everything(), "Zanzibar quokka") {
		t.Fatal("the seed didn't store the words")
	}

	res := f.apply(&ledger.Forget{Meta: f.meta(zz, policy.ViaWeb), Memory: m.Ref, ExpectedVersion: 1})
	var purged atomic.Int32
	bus := forget.NewBus(nil, quiet)
	bus.Local.Register(func(id uuid.UUID) {
		if id == space {
			purged.Add(1)
		}
	})
	prop := forget.New(f.l, f.svc, bus, quiet)
	rep, err := prop.Run(ctx, ledger.ForgetPropagateArgs{OpID: res.Tombstone.ID, SpaceID: space})
	if err != nil {
		t.Fatalf("propagate: %v", err)
	}
	if !rep.Done || rep.Targets != 2 || rep.Held != 1 || rep.Redacted < 3 {
		t.Errorf("report = %+v", rep)
	}
	if rep.Duration > time.Minute {
		t.Errorf("propagation took %s, over the minute", rep.Duration)
	}
	t.Logf("propagation: %s from the forget's commit, %d targets, %d artifacts re-rendered", rep.Duration, rep.Targets, rep.Redacted)
	if purged.Load() != 1 {
		t.Errorf("caches purged %d times", purged.Load())
	}
	if all := f.everything(); strings.Contains(all, "Zanzibar") || strings.Contains(all, "quokka") {
		t.Errorf("object storage still holds the words:\n%s", all)
	}
	if all := f.everything(); !strings.Contains(all, "Use pnpm workspaces only") {
		t.Error("the re-render took the other memory's line too")
	}
	// The forget ledger's copy: ids and refs only.
	r, err := f.store.Get(ctx, ledger.ForgetLedgerKey(space, res.Tombstone.ID))
	if err != nil {
		t.Fatalf("the forget ledger's copy: %v", err)
	}
	raw, _ := io.ReadAll(r.Body)
	var op ledger.ForgetLedgerOp
	if err := json.Unmarshal(raw, &op); err != nil || op.OpID != res.Tombstone.ID || len(op.Entries) != 1 || op.Entries[0].ObjectID != m.ID {
		t.Errorf("forget ledger = %s (%v)", raw, err)
	}
	ts, err := f.l.GetTombstone(ctx, scope, m.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Status != ledger.TombstoneDone || ts.CompletedAt == nil {
		t.Errorf("tombstone %s", ts.Status)
	}
	byLabel := map[string]ledger.TombstoneStep{}
	for _, s := range ts.Steps {
		if s.Target != nil {
			byLabel[s.Target.Label] = s
		}
	}
	if s := byLabel["AGENTS.md"]; s.Status != ledger.StepHeld || s.Reason != ledger.StepReasonHandEdit {
		t.Errorf("AGENTS.md step = %+v (it has a hand edit Memax won't write over)", s)
	}
	if s := byLabel["ChatGPT project"]; s.Status != ledger.StepDone || s.Compile == "" {
		t.Errorf("ChatGPT step = %+v", s)
	}
	kinds := map[string]bool{}
	for _, u := range ts.Unreachable {
		kinds[u.Kind] = true
	}
	for _, k := range []string{ledger.UnreachableGitHistory, ledger.UnreachableHandEdits, ledger.UnreachableCopies,
		ledger.UnreachableAgentMemory, ledger.UnreachableBackups} {
		if !kinds[k] {
			t.Errorf("the tombstone doesn't say %s can't be reached: %+v", k, ts.Unreachable)
		}
	}
	// Running again changes nothing.
	again, err := prop.Run(ctx, ledger.ForgetPropagateArgs{OpID: res.Tombstone.ID, SpaceID: space})
	if err != nil || !again.Done {
		t.Errorf("again: %+v %v", again, err)
	}
	// A hand edit reported after the forget can't bring the line back.
	if _, err := f.svc.Observe(ctx, f.meta(zz, policy.ViaCLI), agents.ID, compile.ObserveInput{
		Path: "AGENTS.md", Content: edited + "- One more.\n", ObserverKind: ledger.ObserverDevice, ObserverID: "zz-laptop"}); err != nil {
		t.Fatal(err)
	}
	if all := f.everything(); strings.Contains(all, "Zanzibar") {
		t.Error("a later observation stored the forgotten line")
	}
	var n int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM v2.target_observations WHERE changeset::text LIKE '%Zanzibar%'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d observations quote it (%v)", n, err)
	}
}

package ledger_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

// newCompileFixture is newFixture with River wired in, the way the API
// server wires it: commands InsertTx their compile jobs.
func newCompileFixture(t *testing.T) *fixture {
	t.Helper()
	_, pool := testdb.Acquire(t)
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	return &fixture{
		t: t, pool: pool, logs: logs,
		l: ledger.New(pool, ledger.WithLogger(slog.New(slog.NewTextHandler(logs, nil))), ledger.WithJobs(client)),
	}
}

func demoSections(refs ...string) []ledger.BriefSection {
	items := make([]ledger.BriefItem, 0, len(refs))
	for _, r := range refs {
		items = append(items, ledger.BriefItem{Ref: r})
	}
	return []ledger.BriefSection{{Key: "conventions", Heading: "Conventions", Items: items}}
}

// brief writes a Brief version as the owner, from the given version.
func (f *fixture) brief(owner, space uuid.UUID, from int, sections []ledger.BriefSection) *ledger.Brief {
	f.t.Helper()
	res := f.apply(&ledger.ReviseBrief{Meta: meta(person(owner), f.scope(owner), policy.ViaWeb), SpaceID: space,
		ExpectedVersion: from, Title: "Memax V2 engineering brief", Summary: "What every agent reads.", Sections: sections})
	if res.Outcome != ledger.OutcomeApplied || res.Brief == nil {
		f.t.Fatalf("revise Brief: %s %s", res.Outcome, res.Policy.Message)
	}
	return res.Brief
}

// target adds a target of the kind, with its defaults.
func (f *fixture) target(owner, space uuid.UUID, kind ledger.TargetKind) *ledger.Target {
	f.t.Helper()
	res := f.apply(&ledger.ConfigureTarget{Meta: meta(person(owner), f.scope(owner), policy.ViaWeb), SpaceID: space, Kind: kind})
	if res.Outcome != ledger.OutcomeApplied || res.Target == nil {
		f.t.Fatalf("add target: %s %s", res.Outcome, res.Policy.Message)
	}
	return res.Target
}

// get reads a target as its owner.
func (f *fixture) get(owner, id uuid.UUID) *ledger.Target {
	f.t.Helper()
	t, err := f.l.GetTarget(context.Background(), f.scope(owner), id)
	if err != nil {
		f.t.Fatalf("GetTarget: %v", err)
	}
	return t
}

// jobs counts the River jobs of a kind waiting for a target.
func (f *fixture) jobs(target uuid.UUID) int {
	f.t.Helper()
	return f.count(`SELECT count(*) FROM river_job WHERE kind = 'compile_target' AND args->>'target_id' = $1
	                 AND state IN ('available', 'pending', 'running', 'scheduled', 'retryable')`, target.String())
}

// compiled records a compile of the target's current generation, as
// Memax, with a one-file output carrying the given memory refs.
func (f *fixture) compiled(t *ledger.Target, content string, refs ...string) *ledger.CompileRun {
	f.t.Helper()
	ctx := context.Background()
	scope, err := f.l.SpaceScope(ctx, t.SpaceID)
	if err != nil {
		f.t.Fatal(err)
	}
	cur, err := f.l.GetTarget(ctx, scope, t.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	ref, err := f.l.ReserveCompileRef(ctx, scope, t.SpaceID)
	if err != nil {
		f.t.Fatal(err)
	}
	brief, err := f.briefOf(scope, t.SpaceID)
	if err != nil {
		f.t.Fatal(err)
	}
	out := ledger.CompiledOutput{Path: t.Path, SHA256: sha(content), DriftSHA256: sha(content), Bytes: len(content),
		Lines: strings.Count(content, "\n"), Refs: append([]string{}, refs...), Cites: []string{}, DroppedForBudget: []string{}}
	if t.Kind == ledger.TargetChatGPT {
		out.Path, out.Label = "", "ChatGPT project instructions"
	}
	outSHA, drift := ledger.OutputManifests([]ledger.CompiledOutput{out})
	now := time.Now()
	res := f.apply(&ledger.RecordCompile{
		Meta:   ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: scope, Via: policy.ViaSystem, IdempotencyKey: uuid.NewString()},
		Target: t.ID, Ref: ref, Generation: cur.DirtyGen, BriefID: brief.ID, BriefVersion: brief.Version,
		InputSHA256: sha("input " + content), OutputSHA256: outSHA, DriftSHA256: drift,
		ArtifactKey: "test/" + ref, Bytes: out.Bytes, Lines: out.Lines, Files: []ledger.CompiledOutput{out}, Refs: refs,
		EnqueuedAt: cur.DirtyAt, StartedAt: now, CompiledAt: now,
	})
	if res.Compile == nil {
		f.t.Fatalf("record compile: unchanged=%v %s", res.Unchanged, res.Policy.Message)
	}
	return res.Compile
}

func (f *fixture) briefOf(scope ledger.Scope, space uuid.UUID) (*ledger.Brief, error) {
	return f.l.GetBrief(context.Background(), scope, space)
}

// rawAs runs fn as memax_v2 in the space's scope and returns the commit
// error (see asV2).
func (f *fixture) rawAs(space, tenant uuid.UUID, fn func(tx pgx.Tx) error) error {
	f.t.Helper()
	return f.asV2([]uuid.UUID{space}, []uuid.UUID{tenant}, fn)
}

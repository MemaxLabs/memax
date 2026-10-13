package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// checkRig is a project space on MemaxLabs/memax with one kept memory that
// names a path, and its owner's machine.
type checkRig struct {
	f      *fixture
	owner  uuid.UUID
	sp     uuid.UUID
	scope  ledger.Scope
	mem    *ledger.Memory
	hash   string
	device string
}

const askConfig = "packages/server/internal/ask/config.go"

func newCheckRig(t *testing.T) *checkRig {
	t.Helper()
	f := newFixture(t)
	zz := f.user("zz")
	sp := f.project(zz, "memax-v2")
	r := &checkRig{f: f, owner: zz, sp: sp, scope: f.scope(zz), device: "dev-" + uuid.NewString()[:8]}
	r.mem = f.remember(zz, sp, "Ask's tiers are set in `"+askConfig+"`.")
	r.hash = r.conditionHash(t, r.mem.Ref, "c1")
	return r
}

// conditionHash is a condition's hash as GET …/conditions gives it.
func (r *checkRig) conditionHash(t *testing.T, ref, id string) string {
	t.Helper()
	list, err := r.f.l.ListConditions(context.Background(), r.scope, r.sp)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range list.Items {
		for _, c := range it.Conditions {
			if it.Memory == ref && c.Condition.ID == id {
				return c.Hash
			}
		}
	}
	t.Fatalf("%s has no %s to check: %+v", ref, id, list.Items)
	return ""
}

// commit is a commit id and its time, hours ago.
func commit(n int, hoursAgo int) (string, time.Time) {
	return fmt.Sprintf("%040x", n), time.Now().Add(-time.Duration(hoursAgo) * time.Hour).UTC().Truncate(time.Second)
}

func (r *checkRig) report(t *testing.T, sha string, at time.Time, results ...ledger.CheckResult) ledger.CheckReportResult {
	t.Helper()
	res, err := r.f.l.ReportChecks(context.Background(), r.reportOf(sha, at, results...))
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if res.Run == nil {
		t.Fatalf("report refused: %+v", res.Policy)
	}
	return res
}

func (r *checkRig) reportOf(sha string, at time.Time, results ...ledger.CheckResult) ledger.CheckReport {
	return ledger.CheckReport{Actor: person(r.owner), Scope: r.scope, Via: policy.ViaCLI, SpaceID: r.sp,
		IdempotencyKey: uuid.NewString(), Repository: "MemaxLabs/memax", Branch: "main", Commit: sha, CommitAt: at,
		Trigger: ledger.TriggerHeadMoved, Device: r.device, Complete: true, Results: results}
}

func (r *checkRig) holds(ref string) ledger.CheckResult {
	return ledger.CheckResult{Memory: ref, Condition: "c1", Hash: r.hash, Outcome: ledger.CheckHolds}
}

func (r *checkRig) removed(ref string) ledger.CheckResult {
	return ledger.CheckResult{Memory: ref, Condition: "c1", Hash: r.hash, Outcome: ledger.CheckFails,
		Evidence: &ledger.CheckEvidence{Change: ledger.ChangeRemoved, Path: askConfig}}
}

func (r *checkRig) memory(t *testing.T, ref string) *ledger.Memory {
	t.Helper()
	m, err := r.f.l.GetMemory(context.Background(), r.scope, ref)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// A condition arms when it holds on the default branch, and a later
// failure flags its memory, as Memax: one flagged receipt, sourced to the
// run and never naming a path, whose reason (which Forget redacts) says
// what changed. The stale report says it typed; the same report again
// changes nothing.
func TestCheckRunArmsThenFlags(t *testing.T) {
	t.Parallel()
	r := newCheckRig(t)
	ctx := context.Background()
	a, aAt := commit(1, 3)
	b, bAt := commit(2, 1)

	first := r.report(t, a, aAt, r.holds(r.mem.Ref))
	if len(first.Flagged) != 0 || first.Run.Counts.Holds != 1 {
		t.Fatalf("a hold flagged %v (%+v)", first.Flagged, first.Run.Counts)
	}
	list, _ := r.f.l.ListConditions(ctx, r.scope, r.sp)
	if len(list.Items) != 1 || !list.Items[0].Conditions[0].Armed || list.Repository != "MemaxLabs/memax" ||
		list.Items[0].Conditions[0].HeldCommit != a {
		t.Fatalf("after a hold: %+v", list)
	}

	rep := r.reportOf(b, bAt, r.removed(r.mem.Ref))
	second, err := r.f.l.ReportChecks(ctx, rep)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(second.Flagged) != "["+r.mem.Ref+"]" || second.Run.Counts.Fails != 1 || second.Run.Capped {
		t.Fatalf("the failure: %+v", second)
	}
	m := r.memory(t, r.mem.Ref)
	if !m.Flags.Has(lifecycle.Stale) || m.Lifecycle != lifecycle.Kept {
		t.Fatalf("not stale: %+v", m.Flags)
	}
	receipts, err := r.f.l.ListReceipts(ctx, r.scope, ledger.ReceiptQuery{SpaceID: r.sp, ObjectID: m.ID})
	if err != nil {
		t.Fatal(err)
	}
	flag := receipts.Receipts[0] // newest first
	if flag.Action != ledger.ActionFlagged || flag.ActorKind != policy.ActorMemax || flag.Via != policy.ViaSystem ||
		flag.Source == nil || flag.Source.Kind != ledger.StaleSourceCheck || flag.Source.Ref != second.Run.ID.String() {
		t.Fatalf("flag receipt: %+v", flag)
	}
	if !strings.Contains(flag.Reason, askConfig) || !strings.Contains(flag.Reason, "main") || !strings.Contains(flag.Reason, b[:7]) {
		t.Errorf("reason: %q", flag.Reason)
	}
	if strings.Contains(flag.Source.Ref, "ask") || strings.Contains(flag.ObjectRef, "ask") {
		t.Errorf("a sealed field names a path: %+v", flag)
	}

	why, err := r.f.l.GetStaleReport(ctx, r.scope, r.mem.Ref)
	if err != nil {
		t.Fatal(err)
	}
	var check *ledger.StaleCause
	for i := range why.Causes {
		if why.Causes[i].Kind == ledger.StaleCauseCheck {
			check = &why.Causes[i]
		}
	}
	if check == nil || check.Condition.Path != askConfig || check.Evidence.Change != ledger.ChangeRemoved ||
		check.Commit != b || check.Branch != "main" || check.Observer != "device" || check.CommitAt == nil || !check.CommitAt.Equal(bAt) {
		t.Fatalf("stale report: %+v", why.Causes)
	}

	// The same report again: the same answer, nothing new.
	again, err := r.f.l.ReportChecks(ctx, rep)
	if err != nil || !again.Replayed || again.Run.ID != second.Run.ID || fmt.Sprint(again.Flagged) != fmt.Sprint(second.Flagged) {
		t.Fatalf("replay: %+v %v", again, err)
	}
	// The same results under a new key: nothing changes either.
	if third := r.report(t, b, bAt, r.removed(r.mem.Ref)); len(third.Flagged) != 0 {
		t.Errorf("flagged twice: %v", third.Flagged)
	}
	if n := r.f.count(`SELECT count(*) FROM v2.receipts WHERE stream_id = $1 AND action = 'flagged'`, m.ID); n != 1 {
		t.Errorf("%d flags", n)
	}
	r.f.everyRowReceipted(t)
}

// Arming and order: a condition that never held never flags; at one
// commit holds wins; an older commit's result is ignored; unknown is never
// a failure; and a result about another definition of a condition, or one
// it doesn't have, is ignored.
func TestCheckResultsAreArmedAndOrdered(t *testing.T) {
	t.Parallel()
	r := newCheckRig(t)
	a, aAt := commit(1, 5)
	b, bAt := commit(2, 3)
	c, cAt := commit(3, 1)

	if res := r.report(t, a, aAt, r.removed(r.mem.Ref)); len(res.Flagged) != 0 {
		t.Fatalf("an unarmed condition flagged: %v", res.Flagged)
	}
	r.report(t, b, bAt, r.holds(r.mem.Ref)) // arms
	if res := r.report(t, b, bAt, r.removed(r.mem.Ref)); len(res.Flagged) != 0 || res.Run.Counts.Ignored != 1 {
		t.Fatalf("at the same commit, holds wins: %+v", res)
	}
	if res := r.report(t, a, aAt, r.removed(r.mem.Ref)); len(res.Flagged) != 0 || res.Run.Counts.Ignored != 1 {
		t.Fatalf("an older commit's result: %+v", res)
	}
	unknown := r.holds(r.mem.Ref)
	unknown.Outcome = ledger.CheckUnknown
	if res := r.report(t, c, cAt, unknown); len(res.Flagged) != 0 || res.Run.Counts.Unknown != 1 {
		t.Fatalf("unknown: %+v", res)
	}
	other := r.removed(r.mem.Ref)
	other.Hash = strings.Repeat("ab", 32)
	missing := r.removed(r.mem.Ref)
	missing.Condition = "c9"
	if res := r.report(t, c, cAt, other, missing); len(res.Flagged) != 0 || res.Run.Counts.Ignored != 2 {
		t.Fatalf("another definition: %+v", res)
	}
	if r.memory(t, r.mem.Ref).Flags.Has(lifecycle.Stale) {
		t.Fatal("stale")
	}
	// The real failure, at a later commit than the hold: flagged.
	if res := r.report(t, c, cAt, r.removed(r.mem.Ref)); len(res.Flagged) != 1 {
		t.Fatalf("the failure: %+v", res)
	}
}

// A person who verified a flagged memory knew the repository as it was:
// the same failure doesn't flag it again on later commits. Once the
// condition holds again, a new failure does.
func TestVerifiedFailuresDontFlagAgain(t *testing.T) {
	t.Parallel()
	r := newCheckRig(t)
	ctx := context.Background()
	a, aAt := commit(1, 6)
	b, bAt := commit(2, 5)
	c, cAt := commit(3, 4)
	d, dAt := commit(4, 3)
	e, eAt := commit(5, 2)
	r.report(t, a, aAt, r.holds(r.mem.Ref))
	r.report(t, b, bAt, r.removed(r.mem.Ref))
	res := r.f.apply(&ledger.Verify{Meta: meta(person(r.owner), r.scope, policy.ViaWeb), Memory: r.mem.Ref,
		ExpectedVersion: 1, Answer: ledger.VerifyStillTrue})
	if res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("verify: %+v", res.Policy)
	}
	if v := res.Receipts[len(res.Receipts)-1]; v.Source == nil || v.Source.Kind != ledger.StaleSourceCheck {
		t.Errorf("verified cites %+v", v.Source)
	}
	if got := r.report(t, c, cAt, r.removed(r.mem.Ref)); len(got.Flagged) != 0 {
		t.Fatalf("the verified failure flagged again: %v", got.Flagged)
	}
	r.report(t, d, dAt, r.holds(r.mem.Ref))
	if got := r.report(t, e, eAt, r.removed(r.mem.Ref)); len(got.Flagged) != 1 {
		t.Fatalf("a new failure: %+v", got)
	}
	_ = ctx
}

// A rename of a file only a source cites isn't flagged (the condition
// follows the move); a run past the bulk cap flags nothing one by one.
func TestCheckRenamesAndTheBulkCap(t *testing.T) {
	t.Parallel()
	r := newCheckRig(t)
	ctx := context.Background()
	nm := fact(r.sp, "Ask answers with the Sonnet tier.")
	nm.Sources = []ledger.SourceInput{{Kind: ledger.SourceFile, Ref: "packages/server/internal/ask/tiers.go:12"}}
	cited := r.f.apply(&ledger.Remember{Meta: meta(person(r.owner), r.scope, policy.ViaWeb), NewMemory: nm}).Memory
	hash := r.conditionHash(t, cited.Ref, "c1")
	a, aAt := commit(1, 3)
	b, bAt := commit(2, 1)
	held := ledger.CheckResult{Memory: cited.Ref, Condition: "c1", Hash: hash, Outcome: ledger.CheckHolds}
	r.report(t, a, aAt, held)
	moved := ledger.CheckResult{Memory: cited.Ref, Condition: "c1", Hash: hash, Outcome: ledger.CheckFails,
		Evidence: &ledger.CheckEvidence{Change: ledger.ChangeRenamed, Path: "packages/server/internal/ask/tiers.go",
			RenamedTo: "packages/server/internal/ask/models.go"}}
	if res := r.report(t, b, bAt, moved); len(res.Flagged) != 0 {
		t.Fatalf("a source's rename flagged: %v", res.Flagged)
	}

	// Eleven armed memories fail at once: a bulk move, not eleven flags.
	var holds, fails []ledger.CheckResult
	for i := range 11 {
		m := r.f.remember(r.owner, r.sp, fmt.Sprintf("Handler %d lives in `packages/server/internal/h%d/handler.go`.", i, i))
		h := r.conditionHash(t, m.Ref, "c1")
		holds = append(holds, ledger.CheckResult{Memory: m.Ref, Condition: "c1", Hash: h, Outcome: ledger.CheckHolds})
		fails = append(fails, ledger.CheckResult{Memory: m.Ref, Condition: "c1", Hash: h, Outcome: ledger.CheckFails,
			Evidence: &ledger.CheckEvidence{Change: ledger.ChangeRemoved}})
	}
	r.report(t, a, aAt, holds...)
	res := r.report(t, b, bAt, fails...)
	if !res.Run.Capped || len(res.Flagged) != 0 {
		t.Fatalf("eleven at once: %+v", res)
	}
	if n := r.f.count(`SELECT count(*) FROM v2.memories WHERE space_id = $1 AND 'stale' = ANY (flags)`, r.sp); n != 0 {
		t.Errorf("%d stale", n)
	}
	_ = ctx
}

// Who reports: a member's or owner's own session, for a project space on
// the clone's repository. Agents, API keys and viewers are refused, and a
// fork's clone or a space with no repository is a mistake to say.
func TestWhoReportsChecks(t *testing.T) {
	t.Parallel()
	r := newCheckRig(t)
	ctx := context.Background()
	a, aAt := commit(1, 1)
	for name, actor := range map[string]ledger.Actor{
		"an agent":   agentFor(policy.AutonomyWrite),
		"an API key": {Kind: policy.ActorPerson, ID: r.owner, Credential: policy.CredentialAPIKey},
	} {
		rep := r.reportOf(a, aAt, r.holds(r.mem.Ref))
		rep.Actor = actor
		res, err := r.f.l.ReportChecks(ctx, rep)
		if err != nil || res.Run != nil || res.Policy.Code != policy.CodeChecksByPerson {
			t.Errorf("%s: %+v %v", name, res, err)
		}
	}
	viewer := r.f.user("viewer")
	r.f.join(r.sp, viewer, "viewer")
	rep := r.reportOf(a, aAt, r.holds(r.mem.Ref))
	rep.Actor, rep.Scope = person(viewer), r.f.scope(viewer)
	if res, err := r.f.l.ReportChecks(ctx, rep); err != nil || res.Policy.Code != policy.CodeViewer {
		t.Errorf("a viewer: %+v %v", res, err)
	}
	fork := r.reportOf(a, aAt, r.holds(r.mem.Ref))
	fork.Repository = "someone/memax"
	if _, err := r.f.l.ReportChecks(ctx, fork); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("a fork: %v", err)
	}
	plain := r.f.space(r.owner, policy.SpaceProject, "no-repo")
	norepo := r.reportOf(a, aAt)
	norepo.SpaceID, norepo.Scope = plain, r.f.scope(r.owner)
	if _, err := r.f.l.ReportChecks(ctx, norepo); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("no repository: %v", err)
	}
	other := r.f.user("other")
	if _, err := r.f.l.ReportChecks(ctx, ledger.CheckReport{Actor: person(other), Scope: r.f.scope(other), Via: policy.ViaCLI,
		SpaceID: r.sp, IdempotencyKey: "k", Repository: "MemaxLabs/memax", Branch: "main", Commit: a, CommitAt: aAt,
		Trigger: ledger.TriggerSchedule, Device: "d1"}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("someone else's space: %v", err)
	}
	for name, bad := range map[string]func(*ledger.CheckReport){
		"a short commit":     func(c *ledger.CheckReport) { c.Commit = "a1b2c3d" },
		"a feature branch..": func(c *ledger.CheckReport) { c.Branch = "a..b" },
		"evidence on a hold": func(c *ledger.CheckReport) {
			c.Results[0].Evidence = &ledger.CheckEvidence{Change: ledger.ChangeRemoved}
		},
		"a failure untold": func(c *ledger.CheckReport) { c.Results[0].Outcome = ledger.CheckFails },
		"an absolute path": func(c *ledger.CheckReport) {
			c.Results[0] = r.removed(r.mem.Ref)
			c.Results[0].Evidence.Path = "/etc/passwd"
		},
		"a rename to nowhere": func(c *ledger.CheckReport) {
			c.Results[0] = r.removed(r.mem.Ref)
			c.Results[0].Evidence.Change = ledger.ChangeRenamed
		},
		"a week-old report": func(c *ledger.CheckReport) { c.CheckedAt = time.Now().Add(-8 * 24 * time.Hour) },
		"no device":         func(c *ledger.CheckReport) { c.Device = "" },
	} {
		rep := r.reportOf(a, aAt, r.holds(r.mem.Ref))
		bad(&rep)
		if _, err := r.f.l.ReportChecks(ctx, rep); !errors.Is(err, ledger.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Forget takes a memory's check results with it (their evidence names its
// paths), and a later report about it is ignored.
func TestForgetTakesCheckResults(t *testing.T) {
	t.Parallel()
	r := newCheckRig(t)
	a, aAt := commit(1, 3)
	b, bAt := commit(2, 1)
	r.report(t, a, aAt, r.holds(r.mem.Ref))
	r.report(t, b, bAt, r.removed(r.mem.Ref))
	if n := r.f.count(`SELECT count(*) FROM v2.condition_checks WHERE memory_id = $1`, r.mem.ID); n != 1 {
		t.Fatalf("%d check rows", n)
	}
	// memax_v2 never deletes in v2, and the function Forget uses takes
	// only a memory this same transaction forgot.
	for _, sql := range []string{
		`DELETE FROM v2.condition_checks WHERE memory_id = $1`,
		`SELECT v2.remove_condition_checks(ARRAY[$1::uuid])`,
	} {
		err := r.f.asV2([]uuid.UUID{r.sp}, []uuid.UUID{r.mem.TenantID}, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), sql, r.mem.ID)
			return err
		})
		if err == nil {
			t.Errorf("%s: allowed outside a Forget", sql)
		}
	}
	res := r.f.apply(&ledger.Forget{Meta: meta(person(r.owner), r.scope, policy.ViaWeb), Memory: r.mem.Ref, ExpectedVersion: 1})
	if res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("forget: %+v", res.Policy)
	}
	if n := r.f.count(`SELECT count(*) FROM v2.condition_checks WHERE memory_id = $1`, r.mem.ID); n != 0 {
		t.Errorf("%d check rows after Forget", n)
	}
	if n := r.f.count(`SELECT count(*) FROM v2.receipts WHERE stream_id = $1 AND reason LIKE '%config.go%'`, r.mem.ID); n != 0 {
		t.Errorf("%d reasons still name the path", n)
	}
	if got := r.report(t, b, bAt, r.removed(r.mem.Ref)); got.Run.Counts.Ignored != 1 {
		t.Errorf("a report about a forgotten memory: %+v", got.Run.Counts)
	}
	// And the database refuses a result for it outright.
	err := r.f.asV2([]uuid.UUID{r.sp}, []uuid.UUID{r.mem.TenantID}, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `
			INSERT INTO v2.condition_checks (memory_id, condition_id, context, space_id, tenant_id, condition_hash, outcome,
			                                 last_run_id, last_commit, last_commit_at)
			VALUES ($1, 'c1', 'default', $2, $3, $4, 'holds', $5, $6, now())`,
			r.mem.ID, r.sp, r.mem.TenantID, r.hash, uuid.New(), a)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "forgotten") {
		t.Errorf("a result for a forgotten memory: %v", err)
	}
}

// A Forget's tombstone names the check results members' machines kept
// (migration 065): a checks step, waiting on each machine that checked the
// space until it checks it again (its daemon dropped them on hearing),
// then done. A memory no machine checked has none.
func TestForgetNamesTheMachinesChecks(t *testing.T) {
	t.Parallel()
	r := newCheckRig(t)
	ctx := context.Background()
	a, aAt := commit(1, 3)
	other := r.f.remember(r.owner, r.sp, "Handlers live in `packages/server/internal/handler/v2api/api.go`.")
	otherHash := r.conditionHash(t, other.Ref, "c1")
	r.report(t, a, aAt, r.holds(r.mem.Ref))
	// A second machine checked it too.
	laptop := r.reportOf(a, aAt, r.holds(r.mem.Ref))
	laptop.Device = "dev-laptop"
	if _, err := r.f.l.ReportChecks(ctx, laptop); err != nil {
		t.Fatal(err)
	}
	never := r.f.remember(r.owner, r.sp, "Releases are cut on Thursdays.")

	res := r.f.apply(&ledger.Forget{Meta: meta(person(r.owner), r.scope, policy.ViaWeb), Memory: r.mem.Ref, ExpectedVersion: 1})
	if res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("forget: %+v", res.Policy)
	}
	step := func() *ledger.TombstoneStep {
		t.Helper()
		tomb, err := r.f.l.GetTombstone(ctx, r.scope, r.mem.Ref)
		if err != nil {
			t.Fatal(err)
		}
		for i := range tomb.Steps {
			if tomb.Steps[i].Kind == ledger.StepChecks {
				return &tomb.Steps[i]
			}
		}
		return nil
	}
	s := step()
	if s == nil || s.Status != ledger.StepWaiting || s.Reason != ledger.StepReasonNextCheck || s.Count == nil || *s.Count != 2 {
		t.Fatalf("after the Forget: %+v", s)
	}
	// Each machine checks the space again, after the Forget.
	later := func(device string) {
		t.Helper()
		b, bAt := commit(2, 0)
		rep := r.reportOf(b, bAt, ledger.CheckResult{Memory: other.Ref, Condition: "c1", Hash: otherHash, Outcome: ledger.CheckHolds})
		rep.Device, rep.CheckedAt = device, time.Now().Add(time.Second)
		if _, err := r.f.l.ReportChecks(ctx, rep); err != nil {
			t.Fatal(err)
		}
	}
	later(r.device)
	if s := step(); s.Status != ledger.StepWaiting || *s.Count != 1 {
		t.Fatalf("one machine since: %+v", s)
	}
	later("dev-laptop")
	if s := step(); s.Status != ledger.StepDone || *s.Count != 0 || s.At == nil || s.Reason != "" {
		t.Fatalf("both since: %+v", s)
	}

	res = r.f.apply(&ledger.Forget{Meta: meta(person(r.owner), r.scope, policy.ViaWeb), Memory: never.Ref, ExpectedVersion: 1})
	if res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("forget: %+v", res.Policy)
	}
	tomb, err := r.f.l.GetTombstone(ctx, r.scope, never.Ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range tomb.Steps {
		if s.Kind == ledger.StepChecks {
			t.Errorf("a memory no machine checked names their checks: %+v", s)
		}
	}
}

// Check state is space-scoped: another space's scope sees none of it.
func TestCheckStateIsScoped(t *testing.T) {
	t.Parallel()
	r := newCheckRig(t)
	a, aAt := commit(1, 1)
	r.report(t, a, aAt, r.holds(r.mem.Ref))
	other := r.f.user("other")
	theirs := r.f.project(other, "theirs")
	var runs, checks int
	err := r.f.asV2([]uuid.UUID{theirs}, []uuid.UUID{r.mem.TenantID}, func(tx pgx.Tx) error {
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM v2.check_runs`).Scan(&runs); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM v2.condition_checks`).Scan(&checks)
	})
	if err != nil || runs != 0 || checks != 0 {
		t.Errorf("another space sees %d runs and %d checks (%v)", runs, checks, err)
	}
	if _, err := r.f.l.ListConditions(context.Background(), r.f.scope(other), r.sp); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("someone else's conditions: %v", err)
	}
}

// FlagStale is Memax's alone: it flags kept memories, leaves the rest, and
// a person can't apply it.
func TestFlagStaleIsMemaxs(t *testing.T) {
	t.Parallel()
	r := newCheckRig(t)
	ctx := context.Background()
	other := r.f.remember(r.owner, r.sp, "Releases go out on Thursdays.")
	memax := ledger.Actor{Kind: policy.ActorMemax}
	cmd := func(a ledger.Actor) *ledger.FlagStale {
		return &ledger.FlagStale{Meta: meta(a, r.scope, policy.ViaSystem), SpaceID: r.sp,
			Source: ledger.ReceiptSource{Kind: ledger.StaleSourceCommit, Ref: strings.Repeat("a", 40)},
			Flags:  []ledger.StaleFlag{{Memory: r.mem.ID, Reason: "Its cited path " + askConfig + " is gone. Verify it."}, {Memory: other.ID, Reason: "Gone. Verify it."}}}
	}
	if res := r.f.apply(cmd(person(r.owner))); res.Outcome != ledger.OutcomeRefused || res.Policy.Code != policy.CodeFlagByMemax {
		t.Fatalf("a person: %+v", res)
	}
	res := r.f.apply(cmd(memax))
	if res.Outcome != ledger.OutcomeApplied || len(res.Receipts) != 2 {
		t.Fatalf("memax: %+v", res)
	}
	for _, rc := range res.Receipts {
		if rc.ActorKind != policy.ActorMemax || rc.Action != ledger.ActionFlagged || rc.Source.Kind != ledger.StaleSourceCommit {
			t.Errorf("receipt %+v", rc)
		}
	}
	if again := r.f.apply(cmd(memax)); !again.Unchanged {
		t.Errorf("flagging stale memories again: %+v", again)
	}
	if !r.memory(t, other.Ref).Flags.Has(lifecycle.Stale) {
		t.Error("not stale")
	}
	_ = ctx
	r.f.everyRowReceipted(t)
}

// The lock order (locks.go) with a flag beside a Forget: the flag holds
// the memory and waits on the space's targets while Forget waits on the
// memory; in one order, neither deadlocks. Both as the FlagStale command
// and inside a check run.
func TestFlagsBesideForget(t *testing.T) {
	t.Parallel()
	for _, viaReport := range []bool{false, true} {
		t.Run(fmt.Sprintf("report=%v", viaReport), func(t *testing.T) {
			t.Parallel()
			f := newCompileFixture(t)
			zz := f.user("zz")
			sp := f.project(zz, "memax-v2")
			r := &checkRig{f: f, owner: zz, sp: sp, scope: f.scope(zz), device: "dev-1"}
			r.mem = f.remember(zz, sp, "Ask's tiers are set in `"+askConfig+"`.")
			f.brief(zz, sp, 0, demoSections(r.mem.Ref))
			f.target(zz, sp, ledger.TargetAgentsMD)
			r.hash = r.conditionHash(t, r.mem.Ref, "c1")
			a, aAt := commit(1, 3)
			b, bAt := commit(2, 1)
			r.report(t, a, aAt, r.holds(r.mem.Ref))

			targets := f.hold(`SELECT 1 FROM v2.targets WHERE space_id = $1 FOR UPDATE`, sp)
			l := f.oneTry()
			var done <-chan error
			if viaReport {
				ch := make(chan error, 1)
				go func() {
					_, err := l.ReportChecks(context.Background(), r.reportOf(b, bAt, r.removed(r.mem.Ref)))
					ch <- err
				}()
				done = ch
			} else {
				done = f.applyAsync(l, &ledger.FlagStale{Meta: meta(ledger.Actor{Kind: policy.ActorMemax}, r.scope, policy.ViaSystem),
					SpaceID: sp, Source: ledger.ReceiptSource{Kind: ledger.StaleSourceCommit, Ref: b},
					Flags: []ledger.StaleFlag{{Memory: r.mem.ID, Reason: "Gone. Verify it."}}})
			}
			f.awaitWaiting(1)
			forget := f.applyAsync(l, forgetCmd(person(zz), r.scope, policy.ViaWeb, r.mem.Ref, 1))
			f.awaitWaiting(2)
			targets.release()
			noDeadlock(t, "flag", f.result("flag", done))
			noDeadlock(t, "forget", f.result("forget", forget))
			if n := f.count(`SELECT count(*) FROM v2.condition_checks WHERE memory_id = $1`, r.mem.ID); n != 0 {
				t.Errorf("%d check rows outlived the Forget", n)
			}
		})
	}
}

// The conditions list says what's being verified, for the machines'
// session-start hooks: each kept memory marked stale, the latest flag
// first, with why (a check's typed cause; nothing for a flag Memax can't
// explain), and no words; a verified one leaves it.
func TestConditionsListWhatsBeingVerified(t *testing.T) {
	t.Parallel()
	r := newCheckRig(t)
	ctx := context.Background()
	a, aAt := commit(1, 3)
	b, bAt := commit(2, 1)
	r.report(t, a, aAt, r.holds(r.mem.Ref))
	r.report(t, b, bAt, r.removed(r.mem.Ref))
	other := r.f.remember(r.owner, r.sp, "Releases go out on Thursdays.")
	if res := r.f.apply(&ledger.FlagStale{Meta: meta(ledger.Actor{Kind: policy.ActorMemax}, r.scope, policy.ViaSystem), SpaceID: r.sp,
		Source: ledger.ReceiptSource{Kind: ledger.StaleSourceCommit, Ref: strings.Repeat("a", 40)},
		Flags:  []ledger.StaleFlag{{Memory: other.ID, Reason: "Its source changed. Verify it."}}}); res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("flag: %+v", res)
	}

	list, err := r.f.l.ListConditions(ctx, r.scope, r.sp)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Stale) != 2 || list.Stale[0].Memory != other.Ref || list.Stale[1].Memory != r.mem.Ref ||
		!list.Stale[0].Since.After(list.Stale[1].Since) && !list.Stale[0].Since.Equal(list.Stale[1].Since) {
		t.Fatalf("stale list: %+v", list.Stale)
	}
	if len(list.Stale[0].Causes) != 0 {
		t.Errorf("a flag without a check has causes: %+v", list.Stale[0].Causes)
	}
	c := list.Stale[1].Causes
	if len(c) != 1 || c[0].Kind != ledger.StaleCauseCheck || c[0].Condition.Path != askConfig ||
		c[0].Evidence.Change != ledger.ChangeRemoved || c[0].Commit != b || c[0].Branch != "main" {
		t.Fatalf("the check's cause: %+v", c)
	}

	if res := r.f.apply(&ledger.Verify{Meta: meta(person(r.owner), r.scope, policy.ViaWeb), Memory: r.mem.Ref,
		ExpectedVersion: 1, Answer: ledger.VerifyStillTrue}); res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("verify: %+v", res)
	}
	list, err = r.f.l.ListConditions(ctx, r.scope, r.sp)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Stale) != 1 || list.Stale[0].Memory != other.Ref {
		t.Errorf("after Verify: %+v", list.Stale)
	}
}

// Memax clears its own flag when a later run finds what failed holding
// again (a revert): an `unflagged` receipt by Memax, via system, citing
// the run, and every target recompiling; never a flag Dream or a date set.
func TestCheckRunUnflagsARevert(t *testing.T) {
	t.Parallel()
	r := newCheckRig(t)
	r.f.brief(r.owner, r.sp, 0, nil)
	r.f.target(r.owner, r.sp, ledger.TargetAgentsMD)
	ctx := context.Background()
	a, aAt := commit(1, 4)
	b, bAt := commit(2, 3)
	c, cAt := commit(3, 2)
	r.report(t, a, aAt, r.holds(r.mem.Ref))
	if got := r.report(t, b, bAt, r.removed(r.mem.Ref)); fmt.Sprint(got.Flagged) != "["+r.mem.Ref+"]" {
		t.Fatalf("the failure: %+v", got)
	}
	dirty := r.f.count(`SELECT COALESCE(sum(dirty_gen), 0)::int FROM v2.targets WHERE space_id = $1`, r.sp)

	back := r.report(t, c, cAt, r.holds(r.mem.Ref))
	if fmt.Sprint(back.Unflagged) != "["+r.mem.Ref+"]" || len(back.Flagged) != 0 {
		t.Fatalf("the revert: %+v", back)
	}
	m := r.memory(t, r.mem.Ref)
	if m.Flags.Has(lifecycle.Stale) {
		t.Errorf("still stale after the revert: %v", m.Flags)
	}
	receipts, err := r.f.l.ListReceipts(ctx, r.scope, ledger.ReceiptQuery{SpaceID: r.sp, ObjectID: m.ID})
	if err != nil {
		t.Fatal(err)
	}
	un := receipts.Receipts[0] // newest first
	if un.Action != ledger.ActionUnflagged || un.ActorKind != policy.ActorMemax || un.Via != policy.ViaSystem ||
		un.Source == nil || un.Source.Kind != ledger.StaleSourceCheck || un.Source.Ref != back.Run.ID.String() {
		t.Fatalf("unflagged receipt: %+v", un)
	}
	if n := r.f.count(`SELECT COALESCE(sum(dirty_gen), 0)::int FROM v2.targets WHERE space_id = $1`, r.sp); n <= dirty {
		t.Errorf("the targets didn't recompile: dirty_gen %d → %d", dirty, n)
	}
	// The same result again changes nothing.
	d, dAt := commit(4, 1)
	if again := r.report(t, d, dAt, r.holds(r.mem.Ref)); len(again.Unflagged)+len(again.Flagged) != 0 {
		t.Errorf("a hold after the revert: %+v", again)
	}
	r.f.everyRowReceipted(t)
}

// A flag no check set (a date's, here) isn't a check's to clear.
func TestCheckRunLeavesADatesFlag(t *testing.T) {
	t.Parallel()
	r := newCheckRig(t)
	a, aAt := commit(1, 4)
	r.report(t, a, aAt, r.holds(r.mem.Ref))
	if res := r.f.apply(&ledger.FlagStale{Meta: meta(ledger.Actor{Kind: policy.ActorMemax}, r.scope, policy.ViaSystem), SpaceID: r.sp,
		Source: ledger.ReceiptSource{Kind: ledger.StaleSourceDate, Ref: "2026-10-01"},
		Flags:  []ledger.StaleFlag{{Memory: r.mem.ID, Reason: "Its date to check again passed."}}}); res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("date flag: %+v", res)
	}
	// A failure and its revert, both after the date's flag.
	b, bAt := commit(2, 3)
	c, cAt := commit(3, 2)
	r.report(t, b, bAt, r.removed(r.mem.Ref))
	if back := r.report(t, c, cAt, r.holds(r.mem.Ref)); len(back.Unflagged) != 0 {
		t.Fatalf("cleared a date's flag: %+v", back)
	}
	if m := r.memory(t, r.mem.Ref); !m.Flags.Has(lifecycle.Stale) {
		t.Error("a date's flag went with a check's hold")
	}
}

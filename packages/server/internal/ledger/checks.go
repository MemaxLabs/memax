package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/conditions"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/stale"
)

// Stale checks (epic 3.1, step 3; the stale-detection design §1.3, §1.4,
// §2.1, §2.4; migration 060).
//
// A memory's conditions are checked against the repository's committed
// tree on its default branch: today by a member's machine (the CLI's
// daemon), later by the GitHub App. A check run's results are check state
// beside the record (v2.check_runs, v2.condition_checks), never part of
// it: they write no receipt. What they change in the record is the stale
// flag, which Memax sets (FlagStale) with a `flagged` receipt whose source
// is the run ({kind: check, ref: <run id>}) and whose reason, which Forget
// redacts, says what changed in words, paths included. A receipt's other
// fields are sealed and survive Forget, so they never name a path.
//
// # Arming
//
// A condition flags only after it held once on the default branch (it is
// armed), and only in a failure that began at a commit later than its last
// hold, so a path that never existed there (a module path that looks like
// a file, an ignored build output) never flags anything. Unknown is never
// a failure. Per condition, the result at the newest commit wins; at the
// same commit, holds wins. A condition redefined in place (its hash
// changed) starts over.
//
// # What a report costs
//
// One transaction, in round trips that don't grow with its results: the
// space with the idempotency claim; the reported memories locked (FOR NO
// KEY UPDATE, one statement, in id order, the lock order of locks.go) with
// their check state, the latest acts of people on them and the space's
// armed count; the run, every changed result (one multi-row upsert) and,
// when it flags, the receipts, the flags and the space's targets; River's
// insert; COMMIT.

// The commands' names (their idempotency records in v2.command_keys).
const (
	// CommandReportChecks is a report; it isn't an Apply command, since a
	// run is check state, not the record.
	CommandReportChecks CommandName = "report_checks"
	// CommandFlagStale names FlagStale.
	CommandFlagStale CommandName = "flag_stale"
)

// ActionUnflagged is Memax clearing its own stale flag when a check finds
// what failed holding again (a revert): via system, its source the run.
const ActionUnflagged Action = "unflagged"

// Check outcomes.
const (
	CheckHolds   = string(stale.Holds)
	CheckFails   = string(stale.Fails)
	CheckUnknown = string(stale.Unknown)
)

// CheckChange is what changed when a condition stopped holding (its
// evidence), typed so a screen can say it in any language.
type CheckChange string

// The changes a check can report.
const (
	ChangeRemoved           CheckChange = "removed"
	ChangeRenamed           CheckChange = "renamed"
	ChangeNoMatch           CheckChange = "no_match"
	ChangeTextMissing       CheckChange = "text_missing"
	ChangeDependencyRemoved CheckChange = "dependency_removed"
	ChangeConstraintChanged CheckChange = "constraint_changed"
	ChangeBlobChanged       CheckChange = "blob_changed"
)

// changesOf are the changes each kind's failure may report.
var changesOf = map[conditions.Kind][]CheckChange{
	conditions.PathExists:    {ChangeRemoved, ChangeRenamed, ChangeNoMatch},
	conditions.FileContains:  {ChangeTextMissing, ChangeRemoved, ChangeRenamed},
	conditions.DepPresent:    {ChangeDependencyRemoved, ChangeConstraintChanged, ChangeRemoved},
	conditions.FileUnchanged: {ChangeBlobChanged, ChangeRemoved, ChangeRenamed},
}

// CheckEvidence is why a condition fails: what changed, and the paths or
// the declared constraint that say so. Never a file's contents.
type CheckEvidence struct {
	Change CheckChange `json:"change"`
	// Path is the path that changed (a glob's literal prefix for no_match;
	// for a bare manifest name, the manifest that changed).
	Path string `json:"path,omitempty"`
	// RenamedTo is where a renamed file went.
	RenamedTo string `json:"renamed_to,omitempty"`
	// Constraint is the dependency's declared constraint now.
	Constraint string `json:"constraint,omitempty"`
}

// CheckTrigger is what started a run.
type CheckTrigger string

// The triggers a person's machine reports. The App's (push, pull_request,
// reconcile) and the date sweep's come later.
const (
	TriggerHeadMoved    CheckTrigger = "head_moved"
	TriggerSessionStart CheckTrigger = "session_start"
	TriggerSchedule     CheckTrigger = "schedule"
	TriggerConditions   CheckTrigger = "conditions"
	TriggerVerify       CheckTrigger = "verify"
)

var deviceTriggers = []CheckTrigger{TriggerHeadMoved, TriggerSessionStart, TriggerSchedule, TriggerConditions, TriggerVerify}

// Limits of a report.
const (
	MaxCheckResults = 5000
	// CheckReportWindow bounds how old a queued report may be: a machine
	// that was offline replays its report with its original checked_at.
	CheckReportWindow = 7 * 24 * time.Hour
	// NeverHeldAfter is how long an unarmed condition is checked before
	// Memax stops asking (never_held): it is shown as "Couldn't check".
	NeverHeldAfter = 30 * 24 * time.Hour
	// maxEvidenceBytes bounds one result's evidence (as the column does).
	maxEvidenceBytes = 2048
)

// CheckResult is one condition's result at the run's commit.
type CheckResult struct {
	// Memory is the memory's display ID ("M-0219").
	Memory    string
	Condition string
	// Hash is the condition's hash as the machine read it (GET
	// …/conditions): a result about another definition is ignored.
	Hash     string
	Outcome  string
	Evidence *CheckEvidence
}

// CheckReport is a person's machine reporting a check run of a space's
// conditions on the repository's default branch, as committed at Commit.
type CheckReport struct {
	Actor          Actor
	Scope          Scope
	Via            policy.Via
	SpaceID        uuid.UUID
	IdempotencyKey string
	// Repository is the owner/name the clone's origin names: it must be the
	// space's, so a fork's clone never reports.
	Repository string
	// Branch is the default branch's name, Commit its commit and CommitAt
	// the commit's time.
	Branch   string
	Commit   string
	CommitAt time.Time
	// CheckedAt is the machine's clock when it ran (zero: now).
	CheckedAt time.Time
	Trigger   CheckTrigger
	// Agent is the agent whose session start asked for the run, if one did.
	Agent string
	// Device is the CLI's device id, never a host name.
	Device string
	// Complete: every condition at that commit was evaluated.
	Complete bool
	Results  []CheckResult
}

// CheckCounts are a run's results by outcome, and those it ignored (about
// a memory that isn't kept here, a condition it no longer has or another
// definition of it, or older than what Memax has).
type CheckCounts struct {
	Holds   int `json:"holds"`
	Fails   int `json:"fails"`
	Unknown int `json:"unknown"`
	Ignored int `json:"ignored"`
}

// CheckRun is one recorded run.
type CheckRun struct {
	ID         uuid.UUID    `json:"id"`
	SpaceID    uuid.UUID    `json:"space_id"`
	Observer   string       `json:"observer"`
	PersonID   *uuid.UUID   `json:"person_id,omitempty"`
	Device     string       `json:"device,omitempty"`
	Repository string       `json:"repository"`
	Branch     string       `json:"branch"`
	Commit     string       `json:"commit"`
	CommitAt   time.Time    `json:"commit_at"`
	Trigger    CheckTrigger `json:"trigger"`
	Agent      string       `json:"agent,omitempty"`
	CheckedAt  time.Time    `json:"checked_at"`
	RecordedAt time.Time    `json:"recorded_at"`
	Complete   bool         `json:"complete"`
	Counts     CheckCounts  `json:"counts"`
	// Capped: the run would have flagged more than the bulk cap allows, so
	// it flagged nothing (stale.Decide).
	Capped bool `json:"capped"`
}

// CheckReportResult is ReportChecks' answer.
type CheckReportResult struct {
	// Run is the recorded run; nil when refused.
	Run *CheckRun
	// Flagged are the memories the run marked stale (display IDs).
	Flagged []string
	// Unflagged are the memories whose stale flag (Memax's, from a check)
	// the run cleared: what failed holds again.
	Unflagged []string
	Policy    policy.Decision
	// Replayed: the idempotency key was already used for this report.
	Replayed bool
}

var (
	commitRE  = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
	repoRE    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)
	branchRE  = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,100}$`)
	deviceRE  = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)
	agentRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	condIDRE  = regexp.MustCompile(`^c[1-9][0-9]{0,3}$`)
	condHashR = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func (c *CheckReport) validate(now time.Time) error {
	switch {
	case c.SpaceID == uuid.Nil:
		return invalid("space", "say which space")
	case c.Actor.ID == uuid.Nil:
		// Who may report is policy's (ActionReportChecks): a refusal, not
		// a malformed report.
		return invalid("actor", "is missing")
	}
	c.IdempotencyKey = strings.TrimSpace(c.IdempotencyKey)
	if n := len(c.IdempotencyKey); n < 1 || n > 255 {
		return invalid("idempotency_key", "send an Idempotency-Key of 1 to 255 characters")
	}
	c.Repository = strings.TrimSpace(c.Repository)
	if !repoRE.MatchString(c.Repository) {
		return invalid("repository", "send the owner/name of the repository the clone's origin names")
	}
	if !branchRE.MatchString(c.Branch) || strings.Contains(c.Branch, "..") {
		return invalid("branch", "send the default branch's name")
	}
	c.Commit = strings.ToLower(strings.TrimSpace(c.Commit))
	if !commitRE.MatchString(c.Commit) {
		return invalid("commit", "send the full commit id the run checked")
	}
	if c.CommitAt.IsZero() || c.CommitAt.After(now.Add(5*time.Minute)) {
		return invalid("commit_at", "send the commit's time")
	}
	switch {
	case c.CheckedAt.IsZero():
		c.CheckedAt = now
	case c.CheckedAt.After(now.Add(5 * time.Minute)):
		return invalid("checked_at", "is in the future; send when the run ran")
	case c.CheckedAt.Before(now.Add(-CheckReportWindow)):
		return invalid("checked_at", "is more than a week ago; run the check again")
	}
	if !slices.Contains(deviceTriggers, c.Trigger) {
		return invalid("trigger", "use head_moved, session_start, schedule, conditions or verify")
	}
	if c.Agent != "" {
		if !AgentKind(c.Agent).Valid() {
			c.Agent = string(AgentFromV1(c.Agent))
		}
		if !agentRE.MatchString(c.Agent) {
			return invalid("agent", "name the agent whose session asked for the run")
		}
	}
	if !deviceRE.MatchString(c.Device) {
		return invalid("device", "send the CLI's device id")
	}
	if len(c.Results) > MaxCheckResults {
		return invalid("results", "send at most %d results in one report", MaxCheckResults)
	}
	seen := map[[2]string]bool{}
	for i := range c.Results {
		r := &c.Results[i]
		at := fmt.Sprintf("results[%d]", i)
		if _, err := memorySeq(r.Memory); err != nil {
			return invalid(at+".memory", "use the memory's display ID, like M-0219")
		}
		if !condIDRE.MatchString(r.Condition) {
			return invalid(at+".condition", "use the condition's id, like c1")
		}
		if !condHashR.MatchString(r.Hash) {
			return invalid(at+".hash", "send the condition's hash as GET …/conditions gave it")
		}
		key := [2]string{strings.ToUpper(r.Memory), r.Condition}
		if seen[key] {
			return invalid(at, "reports %s's %s twice", r.Memory, r.Condition)
		}
		seen[key] = true
		switch r.Outcome {
		case CheckHolds, CheckUnknown:
			if r.Evidence != nil {
				return invalid(at+".evidence", "only a failure carries evidence")
			}
		case CheckFails:
			if err := r.Evidence.validate(at + ".evidence"); err != nil {
				return err
			}
		default:
			return invalid(at+".outcome", "use holds, fails or unknown")
		}
	}
	return nil
}

func (e *CheckEvidence) validate(at string) error {
	if e == nil {
		return invalid(at, "a failure says what changed")
	}
	known := false
	for _, cs := range changesOf {
		known = known || slices.Contains(cs, e.Change)
	}
	if !known {
		return invalid(at+".change", "use removed, renamed, no_match, text_missing, dependency_removed, constraint_changed or blob_changed")
	}
	for name, p := range map[string]string{"path": e.Path, "renamed_to": e.RenamedTo} {
		if p != "" && !conditions.ValidPath(p) {
			return invalid(at+"."+name, "use a path relative to the repository's root")
		}
	}
	if (e.Change == ChangeRenamed) != (e.RenamedTo != "") {
		return invalid(at+".renamed_to", "a rename, and only a rename, says where the file went")
	}
	if e.Constraint != "" {
		if e.Change != ChangeConstraintChanged {
			return invalid(at+".constraint", "only a changed constraint carries one")
		}
		if utf8.RuneCountInString(e.Constraint) > conditions.MaxRangeRunes || strings.ContainsFunc(e.Constraint, isControl) {
			return invalid(at+".constraint", "is longer than %d characters", conditions.MaxRangeRunes)
		}
	}
	if b, _ := json.Marshal(e); len(b) > maxEvidenceBytes {
		return invalid(at, "is larger than %d bytes", maxEvidenceBytes)
	}
	return nil
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// hash identifies a report for its idempotency key: what the machine sent.
func (c *CheckReport) hash(checkedAt time.Time) ([]byte, error) {
	raw, err := json.Marshal(struct {
		Space      uuid.UUID     `json:"space"`
		Repository string        `json:"repository"`
		Branch     string        `json:"branch"`
		Commit     string        `json:"commit"`
		CommitAt   int64         `json:"commit_at"`
		CheckedAt  int64         `json:"checked_at"`
		Trigger    CheckTrigger  `json:"trigger"`
		Agent      string        `json:"agent"`
		Device     string        `json:"device"`
		Complete   bool          `json:"complete"`
		Results    []CheckResult `json:"results"`
	}{c.SpaceID, c.Repository, c.Branch, c.Commit, c.CommitAt.UnixMicro(), unixMicroOrZero(checkedAt),
		c.Trigger, c.Agent, c.Device, c.Complete, c.Results})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return sum[:], nil
}

func unixMicroOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMicro()
}

// ReportChecks records a person's machine's check run, and flags what
// stopped holding, as Memax, in the same transaction. Who may report is
// policy's ActionReportChecks: a member or owner on their own session. A
// space outside the scope is ErrNotFound; one that isn't a project space
// on this repository is ErrInvalid.
func (l *Ledger) ReportChecks(ctx context.Context, c CheckReport) (CheckReportResult, error) {
	if l == nil {
		return CheckReportResult{}, ErrDisabled
	}
	now := l.now()
	given := c.CheckedAt
	if err := c.validate(now); err != nil {
		return CheckReportResult{}, err
	}
	grant, ok := c.Scope.Grant(c.SpaceID)
	if !ok {
		return CheckReportResult{}, ErrNotFound
	}
	hash, err := c.hash(given)
	if err != nil {
		return CheckReportResult{}, err
	}
	scope := c.Scope.Narrow(c.SpaceID)
	var res CheckReportResult
	err = l.retry(ctx, string(CommandReportChecks), func() error {
		var err error
		res, err = l.reportOnce(ctx, &c, scope, grant, hash, now)
		return err
	})
	if err != nil {
		return CheckReportResult{}, mapDBError(err)
	}
	if res.Run != nil && !res.Replayed {
		l.log.Info("ledger: check run", "run", res.Run.ID.String(), "space_id", c.SpaceID.String(),
			"trigger", string(c.Trigger), "holds", res.Run.Counts.Holds, "fails", res.Run.Counts.Fails,
			"unknown", res.Run.Counts.Unknown, "ignored", res.Run.Counts.Ignored, "flagged", len(res.Flagged),
			"capped", res.Run.Capped)
	}
	return res, nil
}

// reportOnce is ReportChecks' one transaction.
func (l *Ledger) reportOnce(ctx context.Context, c *CheckReport, scope Scope, grant SpaceGrant, hash []byte, now time.Time) (CheckReportResult, error) {
	tx, loginRole, err := l.begin(ctx, scope, pgx.ReadWrite)
	if err != nil {
		return CheckReportResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	w := &writer{tx: tx, meta: &Meta{Actor: c.Actor, Scope: scope, Via: c.Via, IdempotencyKey: c.IdempotencyKey, OccurredAt: now},
		command: CommandReportChecks, hash: hash, now: now, inserter: l.inserter, loginRole: loginRole, locks: &memoryLocks{}}

	// 1. The space, with the key's claim.
	sp, claim, err := w.spaceAndClaimed(ctx, c.SpaceID)
	if err != nil {
		return CheckReportResult{}, err
	}
	dec := policy.Decide(toPolicyActor(c.Actor, c.Via, grant, l.plans), policy.ActionReportChecks, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return CheckReportResult{Policy: dec}, nil
	}
	if claim != nil {
		return l.replayReport(ctx, w, claim, dec)
	}
	if sp.Kind != policy.SpaceProject || sp.Repository == "" {
		return CheckReportResult{}, invalid("space", "%s isn't a project space on a repository, so it has nothing to check", spaceLabel(sp))
	}
	if !strings.EqualFold(sp.Repository, c.Repository) {
		return CheckReportResult{}, invalid("repository", "%s compiles for %s, and this clone's origin is %s; report from a clone of %s",
			spaceLabel(sp), sp.Repository, c.Repository, sp.Repository)
	}

	// 2. The memories reported on, locked, with their check state.
	st, err := w.checkState(ctx, sp, c.Results)
	if err != nil {
		return CheckReportResult{}, err
	}

	// In Go: each result against what Memax has, then what to flag.
	run := &CheckRun{ID: newID(), SpaceID: sp.ID, Observer: "device", PersonID: &c.Actor.ID, Device: c.Device,
		Repository: sp.Repository, Branch: c.Branch, Commit: c.Commit, CommitAt: c.CommitAt.UTC(), Trigger: c.Trigger,
		Agent: c.Agent, CheckedAt: c.CheckedAt.UTC(), RecordedAt: now, Complete: c.Complete}
	changed, decided := st.apply(c.Results, run, now)
	out := stale.Decide(stale.Input{Memories: decided, Armed: st.armed, Authoritative: true})
	run.Capped = out.Capped
	var flags, unflags []pendingFlag
	for _, f := range out.Flags {
		m := st.memories[f.Memory]
		flags = append(flags, pendingFlag{mem: m, reason: st.reason(m, f.Conditions, run)})
	}
	for _, id := range out.Unflags {
		unflags = append(unflags, pendingFlag{mem: st.memories[id], reason: fmt.Sprintf(
			"Every condition that stopped holding holds again on %s at %s.", run.Branch, shortCommit(run.Commit))})
	}

	// 3. The run, its changed results and its flags, together.
	b := &pgx.Batch{}
	counts, err := json.Marshal(run.Counts)
	if err != nil {
		return CheckReportResult{}, err
	}
	b.Queue(`
		INSERT INTO v2.check_runs (id, tenant_id, space_id, observer, person_id, device, repository, branch, commit_sha,
		                           commit_at, trigger, agent, checked_at, recorded_at, complete, counts, capped)
		VALUES ($1, $2, $3, 'device', $4, $5, $6, $7, $8, $9, $10, NULLIF($11, ''), $12, $13, $14, $15, $16)`,
		run.ID, sp.TenantID, sp.ID, c.Actor.ID, c.Device, run.Repository, run.Branch, run.Commit, run.CommitAt,
		string(run.Trigger), run.Agent, run.CheckedAt, run.RecordedAt, run.Complete, counts, run.Capped)
	queueCheckRows(b, sp, changed)
	receipts := w.queueFlags(b, sp, flags, unflags, policy.ViaSystem, ReceiptSource{Kind: StaleSourceCheck, Ref: run.ID.String()})
	if err := w.record(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}, run.ID); err != nil {
		return CheckReportResult{}, err
	}
	if err := w.tx.SendBatch(ctx, b).Close(); err != nil {
		return CheckReportResult{}, fmt.Errorf("ledger: record check run: %w", err)
	}
	if err := w.flush(ctx); err != nil {
		return CheckReportResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CheckReportResult{}, fmt.Errorf("ledger: record check run: commit: %w", err)
	}
	res := CheckReportResult{Run: run, Policy: dec, Flagged: []string{}, Unflagged: []string{}}
	for _, rc := range receipts {
		if rc.Action == ActionUnflagged {
			res.Unflagged = append(res.Unflagged, rc.ObjectRef)
		} else {
			res.Flagged = append(res.Flagged, rc.ObjectRef)
		}
	}
	return res, nil
}

// replayReport answers a key used before: the run as recorded, and what it
// flagged.
func (l *Ledger) replayReport(ctx context.Context, w *writer, claim *claimed, dec policy.Decision) (CheckReportResult, error) {
	if claim.objectID == nil {
		return CheckReportResult{}, errors.New("ledger: check report key without its run")
	}
	run, err := loadCheckRun(ctx, w.tx, *claim.objectID)
	if err != nil {
		return CheckReportResult{}, err
	}
	receipts, err := loadReceipts(ctx, w.tx, w.meta.Scope, claim.receiptIDs)
	if err != nil {
		return CheckReportResult{}, err
	}
	if err := w.tx.Commit(ctx); err != nil {
		return CheckReportResult{}, err
	}
	res := CheckReportResult{Run: run, Policy: dec, Replayed: true, Flagged: []string{}, Unflagged: []string{}}
	for _, rc := range receipts {
		if rc.Action == ActionUnflagged {
			res.Unflagged = append(res.Unflagged, rc.ObjectRef)
		} else {
			res.Flagged = append(res.Flagged, rc.ObjectRef)
		}
	}
	return res, nil
}

func spaceLabel(sp spaceRow) string {
	if sp.Name != "" {
		return sp.Name
	}
	return "This space"
}

const checkRunSelect = `
	SELECT id, space_id, observer, person_id, COALESCE(device, ''), repository, branch, commit_sha, commit_at,
	       trigger, COALESCE(agent, ''), checked_at, recorded_at, complete, counts, capped
	  FROM v2.check_runs`

func scanCheckRun(row pgx.Row) (*CheckRun, error) {
	var r CheckRun
	var counts []byte
	if err := row.Scan(&r.ID, &r.SpaceID, &r.Observer, &r.PersonID, &r.Device, &r.Repository, &r.Branch, &r.Commit,
		&r.CommitAt, &r.Trigger, &r.Agent, &r.CheckedAt, &r.RecordedAt, &r.Complete, &counts, &r.Capped); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(counts, &r.Counts); err != nil {
		return nil, fmt.Errorf("ledger: check run %s: counts: %w", r.ID, err)
	}
	return &r, nil
}

func loadCheckRun(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*CheckRun, error) {
	r, err := scanCheckRun(tx.QueryRow(ctx, checkRunSelect+` WHERE id = $1`, id))
	if errNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: read check run: %w", err)
	}
	return r, nil
}

// --- The check state a report reads ---

// checkRow is one condition's state (v2.condition_checks, context
// "default").
type checkRow struct {
	memory         uuid.UUID
	condition      string
	hash           string
	outcome        string
	armedAt        *time.Time
	armedCommit    *string
	heldCommit     *string
	heldCommitAt   *time.Time
	failedCommit   *string
	failedCommitAt *time.Time
	failedAt       *time.Time
	firstFailedRun *uuid.UUID
	lastRun        uuid.UUID
	lastCommit     string
	lastCommitAt   time.Time
	evidence       *CheckEvidence
	createdAt      time.Time
}

// checkState is what a report decides on: the kept memories it names,
// locked, with their conditions, their check rows, when a person last
// acted on each, and how many of the space's memories are armed.
type checkState struct {
	sp       spaceRow
	memories map[uuid.UUID]*Memory
	bySeq    map[int64]*Memory
	conds    map[uuid.UUID][]conditions.Condition
	rows     map[uuid.UUID]map[string]*checkRow
	acted    map[uuid.UUID]time.Time
	// byCheck: the memory's stale flag is Memax's, from a check.
	byCheck map[uuid.UUID]bool
	armed   int
	counts  *CheckCounts
}

// checkState reads it in one round trip, locking the memories first (the
// lock order: memories in one statement, in id order).
func (w *writer) checkState(ctx context.Context, sp spaceRow, results []CheckResult) (*checkState, error) {
	st := &checkState{sp: sp, memories: map[uuid.UUID]*Memory{}, bySeq: map[int64]*Memory{},
		conds: map[uuid.UUID][]conditions.Condition{}, rows: map[uuid.UUID]map[string]*checkRow{}, acted: map[uuid.UUID]time.Time{},
		byCheck: map[uuid.UUID]bool{}}
	seqs := []int64{}
	for _, r := range results {
		n, _ := memorySeq(r.Memory)
		if !slices.Contains(seqs, n) {
			seqs = append(seqs, n)
		}
	}
	slices.Sort(seqs)
	k := w.memoryLocks()
	if k.done {
		return nil, fmt.Errorf("ledger: %s locked memories twice", w.command)
	}
	b := &pgx.Batch{}
	b.Queue(memorySelect+`
		 WHERE m.space_id = $1 AND m.seq = ANY ($2) AND m.lifecycle = 'kept'
		 ORDER BY m.id `+k.strength()+` OF m`, sp.ID, seqs).Query(func(rows pgx.Rows) error {
		ms, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Memory, error) { return scanMemory(r) })
		if err != nil {
			return err
		}
		for _, m := range ms {
			cs, err := storedConditions(m.Conditions)
			if err != nil {
				return err
			}
			st.memories[m.ID], st.bySeq[m.seq], st.conds[m.ID] = m, m, cs
			k.hold(m.ID)
		}
		k.done = true
		return nil
	})
	b.Queue(`
		SELECT c.memory_id, c.condition_id, c.condition_hash, c.outcome, c.armed_at, c.armed_commit, c.held_commit,
		       c.held_commit_at, c.failed_commit, c.failed_commit_at, c.failed_at, c.first_failed_run_id, c.last_run_id,
		       c.last_commit, c.last_commit_at, c.evidence, c.created_at
		  FROM v2.condition_checks c JOIN v2.memories m ON m.id = c.memory_id
		 WHERE c.space_id = $1 AND m.space_id = $1 AND m.seq = ANY ($2) AND c.context = 'default'`,
		sp.ID, seqs).Query(func(rows pgx.Rows) error {
		for rows.Next() {
			r := &checkRow{}
			var evidence []byte
			if err := rows.Scan(&r.memory, &r.condition, &r.hash, &r.outcome, &r.armedAt, &r.armedCommit, &r.heldCommit,
				&r.heldCommitAt, &r.failedCommit, &r.failedCommitAt, &r.failedAt, &r.firstFailedRun, &r.lastRun,
				&r.lastCommit, &r.lastCommitAt, &evidence, &r.createdAt); err != nil {
				return err
			}
			if len(evidence) > 0 {
				r.evidence = &CheckEvidence{}
				if err := json.Unmarshal(evidence, r.evidence); err != nil {
					return err
				}
			}
			if st.rows[r.memory] == nil {
				st.rows[r.memory] = map[string]*checkRow{}
			}
			st.rows[r.memory][r.condition] = r
		}
		return rows.Err()
	})
	// When a person last kept, edited, verified, restored or settled each:
	// whoever did knew the repository as it was then (stale.Decide's rule 3).
	b.Queue(`
		SELECT r.stream_id, max(r.recorded_at)
		  FROM v2.receipts r
		 WHERE r.space_id = $1 AND r.actor_kind = 'person'
		   AND r.action IN ('kept', 'edited', 'verified', 'restored', 'resolved')
		   AND r.stream_id IN (SELECT id FROM v2.memories WHERE space_id = $1 AND seq = ANY ($2))
		 GROUP BY r.stream_id`, sp.ID, seqs).Query(func(rows pgx.Rows) error {
		for rows.Next() {
			var id uuid.UUID
			var at time.Time
			if err := rows.Scan(&id, &at); err != nil {
				return err
			}
			st.acted[id] = at
		}
		return rows.Err()
	})
	b.Queue(`
		SELECT count(DISTINCT c.memory_id)::int FROM v2.condition_checks c
		 WHERE c.space_id = $1 AND c.context = 'default' AND c.armed_at IS NOT NULL`, sp.ID).QueryRow(func(row pgx.Row) error {
		return row.Scan(&st.armed)
	})
	// Who set each one's stale flag: only Memax's, from a check, is a
	// check's to clear (stale.Decide).
	b.Queue(`
		SELECT DISTINCT ON (r.stream_id) r.stream_id, r.actor_kind = 'memax' AND r.source ->> 'kind' = 'check'
		  FROM v2.receipts r
		 WHERE r.space_id = $1 AND `+staleFlagCond+`
		   AND r.stream_id IN (SELECT id FROM v2.memories WHERE space_id = $1 AND seq = ANY ($2) AND 'stale' = ANY (flags))
		 ORDER BY r.stream_id, r.stream_version DESC`, sp.ID, seqs).Query(func(rows pgx.Rows) error {
		for rows.Next() {
			var id uuid.UUID
			var byCheck bool
			if err := rows.Scan(&id, &byCheck); err != nil {
				return err
			}
			st.byCheck[id] = byCheck
		}
		return rows.Err()
	})
	if err := w.tx.SendBatch(ctx, b).Close(); err != nil {
		return nil, fmt.Errorf("ledger: read check state: %w", lockSetError(err))
	}
	return st, nil
}

// apply takes each result against the stored state: the rows to write
// (only those that changed), and what stale.Decide needs, by memory, in
// the order the results came.
func (st *checkState) apply(results []CheckResult, run *CheckRun, now time.Time) ([]*checkRow, []stale.Memory) {
	var changed []*checkRow
	var decided []stale.Memory
	index := map[uuid.UUID]int{}
	for _, r := range results {
		n, _ := memorySeq(r.Memory)
		m := st.bySeq[n]
		var cond *conditions.Condition
		if m != nil {
			for i := range st.conds[m.ID] {
				if st.conds[m.ID][i].ID == r.Condition {
					cond = &st.conds[m.ID][i]
				}
			}
		}
		// Ignored: not a kept memory here, a condition it doesn't have, or
		// another definition of it; or a pull request's state, which only
		// GitHub can say.
		if cond == nil || conditions.Hash(*cond) != r.Hash || cond.Kind == conditions.PRState ||
			(r.Evidence != nil && !slices.Contains(changesOf[cond.Kind], r.Evidence.Change)) {
			run.Counts.Ignored++
			continue
		}
		old := st.rows[m.ID][r.Condition]
		if old != nil && old.hash != r.Hash {
			old = nil // redefined: it starts over
		}
		row, write, ok := nextCheck(old, m.ID, r, run, now)
		if !ok {
			run.Counts.Ignored++
			continue
		}
		switch r.Outcome {
		case CheckHolds:
			run.Counts.Holds++
		case CheckFails:
			run.Counts.Fails++
		default:
			run.Counts.Unknown++
		}
		if write {
			changed = append(changed, row)
			if st.rows[m.ID] == nil {
				st.rows[m.ID] = map[string]*checkRow{}
			}
			st.rows[m.ID][r.Condition] = row // as the run leaves it (reason reads it)
		}
		i, seen := index[m.ID]
		if !seen {
			i = len(decided)
			index[m.ID] = i
			sm := stale.Memory{ID: m.ID, Kept: m.Lifecycle == lifecycle.Kept, Stale: m.Flags.Has(lifecycle.Stale)}
			if at, ok := st.acted[m.ID]; ok {
				sm.ActedAt = &at
			}
			decided = append(decided, sm)
		}
		c := stale.Check{Condition: r.Condition, Origin: cond.Origin, From: cond.From, Outcome: stale.Outcome(row.outcome),
			Armed: row.armedAt != nil, HeldCommitAt: row.heldCommitAt, FailedCommitAt: row.failedCommitAt, FailedAt: row.failedAt,
			Recovered: old != nil && old.outcome == CheckFails && row.outcome == CheckHolds}
		if row.evidence != nil {
			c.Change = string(row.evidence.Change)
		}
		decided[i].Checks = append(decided[i].Checks, c)
	}
	// What a flag's clearing needs: whose flag it is, and what of it still
	// fails, this run's results or not.
	for i := range decided {
		id := decided[i].ID
		decided[i].FlaggedByCheck = st.byCheck[id]
		for _, cond := range st.conds[id] {
			if row := st.rows[id][cond.ID]; row != nil && row.hash == conditions.Hash(cond) &&
				row.armedAt != nil && row.outcome == CheckFails {
				decided[i].StillFailing++
			}
		}
	}
	return changed, decided
}

// nextCheck is a condition's state after a result: write says it changed,
// and ok false that the result is older than what Memax has (the newest
// commit wins; at the same commit, holds wins, and a known outcome wins
// over unknown).
func nextCheck(old *checkRow, memory uuid.UUID, r CheckResult, run *CheckRun, now time.Time) (*checkRow, bool, bool) {
	if old != nil {
		switch {
		case run.CommitAt.Before(old.lastCommitAt):
			return nil, false, false
		case run.CommitAt.Equal(old.lastCommitAt):
			if r.Outcome == old.outcome && (r.Outcome != CheckFails || sameEvidence(r.Evidence, old.evidence)) {
				return old, false, true // the same result again
			}
			if old.outcome == CheckHolds || (r.Outcome == CheckUnknown && old.outcome != CheckUnknown) {
				return nil, false, false
			}
		}
	}
	row := &checkRow{memory: memory, condition: r.Condition, createdAt: now}
	if old != nil {
		cp := *old
		row = &cp
	}
	commit := run.Commit
	commitAt := run.CommitAt
	row.hash, row.outcome, row.lastRun, row.lastCommit, row.lastCommitAt, row.evidence = r.Hash, r.Outcome, run.ID, commit, commitAt, nil
	switch r.Outcome {
	case CheckHolds:
		if row.armedAt == nil {
			row.armedAt, row.armedCommit = &now, &commit
		}
		row.heldCommit, row.heldCommitAt = &commit, &commitAt
		row.failedCommit, row.failedCommitAt, row.failedAt, row.firstFailedRun = nil, nil, nil, nil
	case CheckFails:
		row.evidence = r.Evidence
		if row.failedCommitAt == nil {
			id := run.ID
			row.failedCommit, row.failedCommitAt, row.failedAt, row.firstFailedRun = &commit, &commitAt, &now, &id
		}
	}
	return row, true, true
}

func sameEvidence(a, b *CheckEvidence) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// queueCheckRows writes the changed rows in one statement.
func queueCheckRows(b *pgx.Batch, sp spaceRow, rows []*checkRow) {
	if len(rows) == 0 {
		return
	}
	n := len(rows)
	var (
		memory, firstFailed, lastRun                                  = make([]uuid.UUID, n), make([]*uuid.UUID, n), make([]uuid.UUID, n)
		condition, hash, outcome, lastCommit                          = make([]string, n), make([]string, n), make([]string, n), make([]string, n)
		armedCommit, heldCommit, failedCommit                         = make([]*string, n), make([]*string, n), make([]*string, n)
		armedAt, heldCommitAt, failedCommitAt, failedAt, lastCommitAt = make([]*time.Time, n), make([]*time.Time, n), make([]*time.Time, n), make([]*time.Time, n), make([]time.Time, n)
		evidence                                                      = make([]*string, n)
		createdAt                                                     = make([]time.Time, n)
	)
	for i, r := range rows {
		memory[i], condition[i], hash[i], outcome[i] = r.memory, r.condition, r.hash, r.outcome
		armedAt[i], armedCommit[i], heldCommit[i], heldCommitAt[i] = r.armedAt, r.armedCommit, r.heldCommit, r.heldCommitAt
		failedCommit[i], failedCommitAt[i], failedAt[i], firstFailed[i] = r.failedCommit, r.failedCommitAt, r.failedAt, r.firstFailedRun
		lastRun[i], lastCommit[i], lastCommitAt[i], createdAt[i] = r.lastRun, r.lastCommit, r.lastCommitAt, r.createdAt
		if r.evidence != nil {
			raw, _ := json.Marshal(r.evidence)
			s := string(raw)
			evidence[i] = &s
		}
	}
	b.Queue(`
		INSERT INTO v2.condition_checks (memory_id, condition_id, context, space_id, tenant_id, condition_hash, outcome,
		       armed_at, armed_commit, held_commit, held_commit_at, failed_commit, failed_commit_at, failed_at,
		       first_failed_run_id, last_run_id, last_commit, last_commit_at, evidence, created_at, updated_at)
		SELECT u.memory_id, u.condition_id, 'default', $1, $2, u.hash, u.outcome, u.armed_at, u.armed_commit, u.held_commit,
		       u.held_commit_at, u.failed_commit, u.failed_commit_at, u.failed_at, u.first_failed, u.last_run, u.last_commit,
		       u.last_commit_at, u.evidence::jsonb, u.created_at, now()
		  FROM unnest($3::uuid[], $4::text[], $5::text[], $6::text[], $7::timestamptz[], $8::text[], $9::text[],
		              $10::timestamptz[], $11::text[], $12::timestamptz[], $13::timestamptz[], $14::uuid[], $15::uuid[],
		              $16::text[], $17::timestamptz[], $18::text[], $19::timestamptz[])
		       AS u(memory_id, condition_id, hash, outcome, armed_at, armed_commit, held_commit, held_commit_at,
		            failed_commit, failed_commit_at, failed_at, first_failed, last_run, last_commit, last_commit_at,
		            evidence, created_at)
		ON CONFLICT (memory_id, condition_id, context) DO UPDATE
		   SET condition_hash = EXCLUDED.condition_hash, outcome = EXCLUDED.outcome, armed_at = EXCLUDED.armed_at,
		       armed_commit = EXCLUDED.armed_commit, held_commit = EXCLUDED.held_commit,
		       held_commit_at = EXCLUDED.held_commit_at, failed_commit = EXCLUDED.failed_commit,
		       failed_commit_at = EXCLUDED.failed_commit_at, failed_at = EXCLUDED.failed_at,
		       first_failed_run_id = EXCLUDED.first_failed_run_id, last_run_id = EXCLUDED.last_run_id,
		       last_commit = EXCLUDED.last_commit, last_commit_at = EXCLUDED.last_commit_at,
		       evidence = EXCLUDED.evidence, created_at = EXCLUDED.created_at, updated_at = now()`,
		sp.ID, sp.TenantID, memory, condition, hash, outcome, armedAt, armedCommit, heldCommit, heldCommitAt,
		failedCommit, failedCommitAt, failedAt, firstFailed, lastRun, lastCommit, lastCommitAt, evidence, createdAt)
}

// reason is Memax's words on a flag: the first failing condition in words,
// paths included (Forget redacts a receipt's reason; nothing else on it
// names a path).
func (st *checkState) reason(m *Memory, failed []string, run *CheckRun) string {
	var cond conditions.Condition
	for _, c := range st.conds[m.ID] {
		if c.ID == failed[0] {
			cond = c
		}
	}
	var e CheckEvidence
	if r := st.rows[m.ID][failed[0]]; r != nil && r.evidence != nil {
		e = *r.evidence
	}
	where := fmt.Sprintf("on %s at %s", run.Branch, shortCommit(run.Commit))
	path := firstNonEmpty(e.Path, cond.Path, cond.Manifest, cond.Glob)
	var s string
	switch e.Change {
	case ChangeRenamed:
		s = fmt.Sprintf("Its cited file %s was renamed to %s %s.", path, e.RenamedTo, where)
	case ChangeNoMatch:
		s = fmt.Sprintf("Nothing matches %s %s.", cond.Glob, where)
	case ChangeTextMissing:
		s = fmt.Sprintf("%s no longer contains %s %s.", path, cond.Text, where)
	case ChangeDependencyRemoved:
		s = fmt.Sprintf("%s no longer lists %s %s.", path, cond.Name, where)
	case ChangeConstraintChanged:
		s = fmt.Sprintf("%s now asks for %s %s %s.", path, cond.Name, e.Constraint, where)
	case ChangeBlobChanged:
		s = fmt.Sprintf("%s changed %s.", path, where)
	default:
		s = fmt.Sprintf("Its cited path %s is gone %s.", path, where)
	}
	if n := len(failed) - 1; n == 1 {
		s += " One more of its conditions stopped holding too."
	} else if n > 1 {
		s += fmt.Sprintf(" %d more of its conditions stopped holding too.", n)
	}
	return s + " Verify it."
}

func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// --- Flags ---

// pendingFlag is a locked memory to flag, with Memax's words on it and,
// when it isn't the command's, its own source (a date flag's date).
type pendingFlag struct {
	mem    *Memory
	reason string
	source *ReceiptSource
}

// queueFlags queues Memax's stale flags on locked memories, and the
// clearing of its own flags (unflags): a `flagged` or `unflagged` receipt
// each, by Memax (source and via as given), the flag itself, and every
// target recompiling once.
func (w *writer) queueFlags(b *pgx.Batch, sp spaceRow, flags, unflags []pendingFlag, via policy.Via, source ReceiptSource) []Receipt {
	n := len(flags) + len(unflags)
	if n == 0 {
		return nil
	}
	receipts := make([]Receipt, 0, n)
	ids, fl, versions, rcs := make([]uuid.UUID, 0, n), make([]string, 0, n), make([]int, 0, n), make([]uuid.UUID, 0, n)
	add := func(f pendingFlag, verb lifecycle.Verb, action Action) {
		next, _ := lifecycle.Transition(f.mem.state(), verb)
		src := source
		if f.source != nil {
			src = *f.source
		}
		rc := Receipt{ID: newID(), TenantID: sp.TenantID, SpaceID: sp.ID, ObjectKind: ObjectMemory,
			ObjectID: f.mem.ID, ObjectRef: f.mem.Ref, Action: action, ActorKind: policy.ActorMemax, Via: via,
			Source: &src, Reason: f.reason, OccurredAt: w.now, StreamID: f.mem.ID, StreamVersion: f.mem.streamVersion + 1}
		receipts = append(receipts, rc)
		queueReceipt(b, &receipts[len(receipts)-1])
		ids, fl = append(ids, f.mem.ID), append(fl, strings.Join(next.Flags.Strings(), ","))
		versions, rcs = append(versions, rc.StreamVersion), append(rcs, rc.ID)
	}
	for _, f := range flags {
		add(f, lifecycle.VerbFlagStale, ActionFlagged)
	}
	for _, f := range unflags {
		add(f, lifecycle.VerbClearStale, ActionUnflagged)
	}
	b.Queue(`
		UPDATE v2.memories m
		   SET flags = COALESCE(string_to_array(NULLIF(u.flags, ''), ','), '{}'), stream_version = u.version,
		       last_receipt_id = u.receipt, updated_at = now()
		  FROM unnest($1::uuid[], $2::text[], $3::int[], $4::uuid[]) AS u(id, flags, version, receipt)
		 WHERE m.id = u.id AND m.space_id = $5`, ids, fl, versions, rcs, sp.ID)
	b.Queue(dirtySQL, sp.ID, nil).Query(func(rows pgx.Rows) error {
		dirty, err := dirtyTargets(rows, nil)
		if err != nil {
			return err
		}
		w.queueCompiles(sp.ID, dirty)
		return nil
	})
	return receipts
}

// FlagStale marks kept memories stale, as Memax: a `flagged` receipt each,
// with the source that found the change (a check run, a commit or a pull
// request; never a path) and Memax's words on it, which Forget redacts.
// Every target recompiles. A memory that is no longer kept, or is stale
// already, is left as it is. It is what a check run applies in its own
// transaction (ReportChecks); the GitHub App's worker will apply it as a
// command. The date sweep's is FlagDue.
type FlagStale struct {
	Meta
	SpaceID uuid.UUID
	Source  ReceiptSource
	Flags   []StaleFlag
}

// StaleFlag is one memory to flag and why.
type StaleFlag struct {
	Memory uuid.UUID
	Reason string
}

// Name implements Command.
func (*FlagStale) Name() CommandName { return CommandFlagStale }

// MaxStaleFlags bounds one FlagStale (a run's flags are under the bulk cap).
const MaxStaleFlags = 200

func (c *FlagStale) validate() error {
	if c.SpaceID == uuid.Nil {
		return invalid("space", "say which space")
	}
	switch c.Source.Kind {
	case StaleSourceCheck, StaleSourceCommit, StaleSourcePR, StaleSourceDate:
	default:
		return invalid("source", "use check, commit, pr or date")
	}
	if c.Source.Ref == "" || utf8.RuneCountInString(c.Source.Ref) > 200 {
		return invalid("source", "say which run, commit, pull request or date")
	}
	if len(c.Flags) == 0 || len(c.Flags) > MaxStaleFlags {
		return invalid("flags", "flag 1 to %d memories", MaxStaleFlags)
	}
	seen := map[uuid.UUID]bool{}
	for i, f := range c.Flags {
		if f.Memory == uuid.Nil || seen[f.Memory] {
			return invalid(fmt.Sprintf("flags[%d].memory", i), "name each memory once")
		}
		seen[f.Memory] = true
		if err := checkText(fmt.Sprintf("flags[%d].reason", i), f.Reason, MaxReasonRunes, true); err != nil {
			return err
		}
	}
	return nil
}

// flagStale applies FlagStale.
func (w *writer) flagStale(ctx context.Context, c *FlagStale) (Result, error) {
	sp, replay, err := w.spaceAndClaim(ctx, c.SpaceID)
	if err != nil || replay != nil {
		return deref(replay), err
	}
	grant, _ := w.meta.Scope.Grant(sp.ID)
	dec := policy.Decide(w.policyActor(grant), policy.ActionFlagStale, policy.Object{}, sp.policy())
	if dec.Effect == policy.EffectRefuse {
		return refused(dec), nil
	}
	ids := make([]uuid.UUID, len(c.Flags))
	for i, f := range c.Flags {
		ids[i] = f.Memory
	}
	locked, err := w.lockMemories(ctx, ids)
	if err != nil {
		return Result{}, err
	}
	var flags []pendingFlag
	for _, f := range c.Flags {
		m := locked[f.Memory]
		if m == nil || m.SpaceID != sp.ID || m.Lifecycle != lifecycle.Kept || m.Flags.Has(lifecycle.Stale) {
			continue
		}
		flags = append(flags, pendingFlag{mem: m, reason: f.Reason})
	}
	if len(flags) == 0 {
		return Result{Outcome: OutcomeApplied, Policy: dec, Unchanged: true}, nil
	}
	b := &pgx.Batch{}
	receipts := w.queueFlags(b, sp, flags, nil, w.meta.Via, c.Source)
	if err := w.record(ctx, Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}, uuid.Nil); err != nil {
		return Result{}, err
	}
	if err := w.tx.SendBatch(ctx, b).Close(); err != nil {
		return Result{}, fmt.Errorf("ledger: flag stale: %w", err)
	}
	return Result{Outcome: OutcomeApplied, Policy: dec, Receipts: receipts}, nil
}

// --- The conditions a machine checks ---

// CheckedCondition is a condition as a machine checks it: its typed form,
// its hash (sent back with a result), whether it is armed and, when it is,
// the commit of its latest hold that Memax heard of: where a machine that
// didn't see it hold looks for a rename when its path is gone.
type CheckedCondition struct {
	Condition  conditions.Condition `json:"condition"`
	Hash       string               `json:"hash"`
	Armed      bool                 `json:"armed"`
	HeldCommit string               `json:"held_commit,omitempty"`
}

// MemoryConditions are one kept memory's conditions to check.
type MemoryConditions struct {
	Memory     string             `json:"memory"`
	Conditions []CheckedCondition `json:"conditions"`
}

// SpaceConditions is what a space's machines check: every kept memory's
// conditions, in compact form, without its words. A condition that never
// held in NeverHeldAfter is left out: Memax stops asking (never_held).
type SpaceConditions struct {
	SpaceID    uuid.UUID          `json:"space_id"`
	Repository string             `json:"repository"`
	Items      []MemoryConditions `json:"items"`
	// Stale is the space's kept memories marked stale now, the latest flag
	// first (at most MaxStaleListed), with why and no words: what a
	// machine's session-start hook says is being verified.
	Stale []StaleMemory `json:"stale"`
}

// StaleMemory is a memory marked stale, as a space's machines hear of it.
type StaleMemory struct {
	Memory string `json:"memory"`
	// Since is its flag's receipt time (when the memory last changed, if
	// that receipt is gone).
	Since  time.Time    `json:"since"`
	Causes []StaleCause `json:"causes"`
}

// MaxStaleListed bounds the stale memories a conditions list carries.
const MaxStaleListed = 100

// ListConditions reads a space's conditions for its machines, in one
// round trip (ReadBatch). pr_state isn't a machine's to check, so it is
// left out. An armed condition says the commit of its latest hold that
// Memax heard of (HeldCommit), so a machine with no hold of its own (one
// that didn't arm it) looks for a rename from there.
func (l *Ledger) ListConditions(ctx context.Context, scope Scope, spaceID uuid.UUID) (*SpaceConditions, error) {
	if l == nil {
		return nil, ErrDisabled
	}
	if _, ok := scope.Grant(spaceID); !ok {
		return nil, ErrNotFound
	}
	now := l.now()
	out := &SpaceConditions{SpaceID: spaceID, Items: []MemoryConditions{}, Stale: []StaleMemory{}}
	type kept struct {
		id    uuid.UUID
		ref   string
		conds []conditions.Condition
	}
	var memories []kept
	rows := map[uuid.UUID]map[string]*checkRow{}
	found := false
	b := &pgx.Batch{}
	b.Queue(`SELECT COALESCE(repository, '') FROM v2.spaces WHERE id = $1`, spaceID).QueryRow(func(row pgx.Row) error {
		err := row.Scan(&out.Repository)
		if errNoRows(err) {
			return nil
		}
		found = err == nil
		return err
	})
	b.Queue(`
		SELECT id, seq, conditions FROM v2.memories
		 WHERE space_id = $1 AND lifecycle = 'kept' AND conditions <> '[]'::jsonb
		 ORDER BY seq`, spaceID).Query(func(r pgx.Rows) error {
		for r.Next() {
			var k kept
			var seq int64
			var raw []byte
			if err := r.Scan(&k.id, &seq, &raw); err != nil {
				return err
			}
			cs, err := storedConditions(raw)
			if err != nil {
				return err
			}
			k.ref, k.conds = FormatRef(PrefixMemory, seq), cs
			memories = append(memories, k)
		}
		return r.Err()
	})
	b.Queue(`
		SELECT memory_id, condition_id, condition_hash, armed_at, held_commit, created_at FROM v2.condition_checks
		 WHERE space_id = $1 AND context = 'default'`, spaceID).Query(func(r pgx.Rows) error {
		for r.Next() {
			c := &checkRow{}
			if err := r.Scan(&c.memory, &c.condition, &c.hash, &c.armedAt, &c.heldCommit, &c.createdAt); err != nil {
				return err
			}
			if rows[c.memory] == nil {
				rows[c.memory] = map[string]*checkRow{}
			}
			rows[c.memory][c.condition] = c
		}
		return r.Err()
	})
	// The memories marked stale now, and the failing checks that say why.
	type staleRow struct {
		id         uuid.UUID
		ref        string
		staleAfter *time.Time
		since      time.Time
	}
	var stale []staleRow
	failing := map[uuid.UUID][]failingCheck{}
	b.Queue(`
		SELECT m.id, m.seq, m.stale_after,
		       COALESCE((SELECT max(r.occurred_at) FROM v2.receipts r
		                  WHERE r.stream_id = m.id AND r.space_id = m.space_id AND `+staleFlagCond+`), m.updated_at) AS since
		  FROM v2.memories m
		 WHERE m.space_id = $1 AND m.lifecycle = 'kept' AND 'stale' = ANY (m.flags)
		 ORDER BY since DESC, m.seq
		 LIMIT $2`, spaceID, MaxStaleListed).Query(func(r pgx.Rows) error {
		for r.Next() {
			var s staleRow
			var seq int64
			if err := r.Scan(&s.id, &seq, &s.staleAfter, &s.since); err != nil {
				return err
			}
			s.ref = FormatRef(PrefixMemory, seq)
			stale = append(stale, s)
		}
		return r.Err()
	})
	b.Queue(`
		SELECT c.memory_id, c.condition_id, c.evidence, c.failed_commit, c.failed_commit_at, r.branch, r.checked_at, r.observer
		  FROM v2.condition_checks c
		  JOIN v2.memories m ON m.id = c.memory_id AND m.space_id = c.space_id
		  LEFT JOIN v2.check_runs r ON r.id = c.first_failed_run_id AND r.space_id = c.space_id
		 WHERE c.space_id = $1 AND c.context = 'default' AND c.outcome = 'fails' AND c.armed_at IS NOT NULL
		   AND m.lifecycle = 'kept' AND 'stale' = ANY (m.flags)
		 ORDER BY c.memory_id, c.condition_id`, spaceID).Query(func(r pgx.Rows) error {
		for r.Next() {
			var memory uuid.UUID
			var f failingCheck
			var evidence []byte
			var branch, observer *string
			if err := r.Scan(&memory, &f.condition, &evidence, &f.commit, &f.commitAt, &branch, &f.checkedAt, &observer); err != nil {
				return err
			}
			if len(evidence) > 0 {
				f.evidence = &CheckEvidence{}
				if err := json.Unmarshal(evidence, f.evidence); err != nil {
					return err
				}
			}
			f.branch, f.observer = deref0(branch), deref0(observer)
			failing[memory] = append(failing[memory], f)
		}
		return r.Err()
	})
	if err := l.ReadBatch(ctx, scope.Narrow(spaceID), b); err != nil {
		return nil, fmt.Errorf("ledger: read conditions: %w", err)
	}
	if !found {
		return nil, ErrNotFound
	}
	conds := map[uuid.UUID][]conditions.Condition{}
	for _, m := range memories {
		conds[m.id] = m.conds
	}
	for _, s := range stale {
		causes := []StaleCause{}
		if s.staleAfter != nil && !s.staleAfter.After(now) {
			causes = append(causes, StaleCause{Kind: StaleCauseDate, StaleAfter: s.staleAfter})
		}
		causes = append(causes, checkCauses(conds[s.id], failing[s.id])...)
		out.Stale = append(out.Stale, StaleMemory{Memory: s.ref, Since: s.since, Causes: causes})
	}
	for _, m := range memories {
		item := MemoryConditions{Memory: m.ref, Conditions: []CheckedCondition{}}
		for _, c := range m.conds {
			if c.Kind == conditions.PRState {
				continue
			}
			h := conditions.Hash(c)
			cc := CheckedCondition{Condition: c, Hash: h}
			if r := rows[m.id][c.ID]; r != nil && r.hash == h {
				cc.Armed = r.armedAt != nil
				if !cc.Armed && now.Sub(r.createdAt) > NeverHeldAfter {
					continue // never held: Memax stops asking
				}
				if cc.Armed && r.heldCommit != nil {
					cc.HeldCommit = *r.heldCommit
				}
			}
			item.Conditions = append(item.Conditions, cc)
		}
		if len(item.Conditions) > 0 {
			out.Items = append(out.Items, item)
		}
	}
	return out, nil
}

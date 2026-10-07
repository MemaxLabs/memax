package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The deploy question the tests ask (the handoff's DecisionGate preview).
const deployQuestion = "Which deploy target should the v2 API use?"

func deployOptions() []ledger.DecisionOption {
	return []ledger.DecisionOption{
		{Label: "Fly.io, iad and ams", Detail: "Matches the current API and workers."},
		{Label: "Railway", Detail: "Simpler preview environments."},
		{Label: "Decide later", Detail: "Codex continues behind a flag."},
	}
}

func ask(actor ledger.Actor, scope ledger.Scope, space uuid.UUID) *ledger.RequestDecision {
	m := meta(actor, scope, policy.ViaMCP)
	m.SessionRef = "cx-9f1c"
	return &ledger.RequestDecision{Meta: m, SpaceID: space, Question: deployQuestion,
		Context: "A kept note and a proposal disagree: M-0174 (Railway) and M-0431 (Fly.io).", Options: deployOptions()}
}

// asked asks the deploy question and returns the waiting gate.
func (f *fixture) asked(actor ledger.Actor, scope ledger.Scope, space uuid.UUID) *ledger.Gate {
	f.t.Helper()
	res := f.apply(ask(actor, scope, space))
	if res.Outcome != ledger.OutcomeApplied || res.Gate == nil {
		f.t.Fatalf("ask: %s %s (%s)", res.Outcome, res.Policy.Code, res.Policy.Message)
	}
	return res.Gate
}

func answer(actor ledger.Actor, scope ledger.Scope, via policy.Via, gate string, option int) *ledger.AnswerGate {
	return &ledger.AnswerGate{Meta: meta(actor, scope, via), Gate: gate, Option: option}
}

// clockAt returns a ledger on the same database whose clock reads t.
func (f *fixture) clockAt(t time.Time) *ledger.Ledger {
	return ledger.New(f.pool, ledger.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		ledger.WithClock(func() time.Time { return t }))
}

func gateStateIs(t *testing.T, err error, status ledger.GateStatus) {
	t.Helper()
	var ge *ledger.GateStateError
	if !errors.As(err, &ge) || ge.Status != status || !errors.Is(err, ledger.ErrInvalidTransition) {
		t.Fatalf("err = %v, want a gate that is %s", err, status)
	}
}

// An agent that may propose asks; the gate waits, with a G- display ID, a
// receipt that names the connection and session but not the words, and
// the default expiry. The same command again is the same gate.
func TestRequestDecision(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	codex := agentFor(policy.AutonomyPropose)
	before := time.Now()
	cmd := ask(codex, f.scope(zz), space)
	res := f.apply(cmd)
	g := res.Gate
	if res.Outcome != ledger.OutcomeApplied || g == nil {
		t.Fatalf("ask: %+v", res)
	}
	if g.Ref != "G-0001" || g.Status != ledger.GateWaiting || g.Version != 1 || g.SpaceID != space || g.Answer != nil {
		t.Errorf("gate = %+v", g)
	}
	if g.Question != deployQuestion || len(g.Options) != 3 || g.Options[1].Detail != "Simpler preview environments." {
		t.Errorf("words = %q %+v", g.Question, g.Options)
	}
	if g.AskedBy != codex.ID || g.Agent != "codex" || g.SessionRef != "cx-9f1c" || g.NeedsWeb {
		t.Errorf("asker = %s %s %s needs_web=%v", g.AskedBy, g.Agent, g.SessionRef, g.NeedsWeb)
	}
	if d := g.ExpiresAt.Sub(before); d < ledger.DefaultGateTTL-time.Minute || d > ledger.DefaultGateTTL+time.Minute {
		t.Errorf("expires in %s, want %s", d, ledger.DefaultGateTTL)
	}
	if len(res.Receipts) != 1 {
		t.Fatalf("receipts = %+v", res.Receipts)
	}
	rc := res.Receipts[0]
	if rc.ObjectKind != ledger.ObjectGate || rc.ObjectID != g.ID || rc.ObjectRef != "G-0001" || rc.Action != ledger.ActionAsked ||
		rc.ActorKind != policy.ActorAgent || rc.ActorID == nil || *rc.ActorID != codex.ID || rc.Agent != "codex" ||
		rc.Via != policy.ViaMCP || rc.SessionRef != "cx-9f1c" || rc.ID != g.CreatedReceiptID || rc.StreamVersion != 1 {
		t.Errorf("receipt = %+v", rc)
	}

	replay := f.apply(cmd)
	if !replay.Replayed || replay.Gate == nil || replay.Gate.ID != g.ID || len(replay.Receipts) != 1 || replay.Receipts[0].ID != rc.ID {
		t.Errorf("replay = %+v", replay)
	}
	if n := f.count(`SELECT count(*) FROM v2.decision_gates`); n != 1 {
		t.Errorf("%d gates after a replay, want 1", n)
	}

	// A team space's gate says it needs the web to be answered (D15), and
	// numbers continue per tenant.
	team := f.space(zz, policy.SpaceTeam, "memax-team")
	if tg := f.asked(codex, f.scope(zz), team); !tg.NeedsWeb {
		t.Errorf("team gate needs_web = false")
	}
	if g2 := f.asked(codex, f.scope(zz), space); g2.Ref != "G-0002" {
		t.Errorf("second gate in the tenant = %s, want G-0002", g2.Ref)
	}
}

// Who may ask, what a question may hold, and the cap on waiting
// questions.
func TestRequestDecisionRefusals(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	ctx := context.Background()

	refusals := []struct {
		name  string
		actor ledger.Actor
		edit  func(*ledger.RequestDecision)
		code  string
	}{
		{"a read-only agent", agentFor(policy.AutonomyRead), nil, policy.CodeReadOnly},
		{"an agent with no autonomy", agentFor(""), nil, policy.CodeReadOnly},
		{"a person", person(zz), nil, policy.CodeGateByAgent},
		{"Dream", ledger.Actor{Kind: policy.ActorDream}, nil, policy.CodeGateByAgent},
		{"a secret in the context", agentFor(policy.AutonomyWrite), func(c *ledger.RequestDecision) {
			c.Context = "Deploy with AKIAIOSFODNN7EXAMPLE as the key."
		}, policy.CodeSecret},
		{"a secret in an option", agentFor(policy.AutonomyWrite), func(c *ledger.RequestDecision) {
			c.Options[1].Detail = "token ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789"
		}, policy.CodeSecret},
	}
	for _, c := range refusals {
		cmd := ask(c.actor, scope, space)
		if c.edit != nil {
			c.edit(cmd)
		}
		res, err := f.l.Apply(ctx, cmd)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		refusedWith(t, res, c.code)
	}

	invalids := []struct {
		name  string
		field string
		edit  func(*ledger.RequestDecision)
	}{
		{"no question", "question", func(c *ledger.RequestDecision) { c.Question = "  " }},
		{"a long question", "question", func(c *ledger.RequestDecision) { c.Question = strings.Repeat("q", ledger.MaxGateQuestionRunes+1) }},
		{"one option", "options", func(c *ledger.RequestDecision) { c.Options = c.Options[:1] }},
		{"five options", "options", func(c *ledger.RequestDecision) {
			c.Options = append(c.Options, ledger.DecisionOption{Label: "Render"}, ledger.DecisionOption{Label: "Heroku"})
		}},
		{"an empty option", "options.label", func(c *ledger.RequestDecision) { c.Options[2].Label = " " }},
		{"the same option twice", "options.label", func(c *ledger.RequestDecision) { c.Options[1].Label = "fly.io, IAD and AMS" }},
		{"a long label", "options.label", func(c *ledger.RequestDecision) { c.Options[0].Label = strings.Repeat("l", ledger.MaxGateLabelRunes+1) }},
		{"no space", "space_id", func(c *ledger.RequestDecision) { c.SpaceID = uuid.Nil }},
		{"expiring in a minute", "expires_at", func(c *ledger.RequestDecision) { e := time.Now().Add(time.Minute); c.ExpiresAt = &e }},
		{"expiring in a year", "expires_at", func(c *ledger.RequestDecision) { e := time.Now().AddDate(1, 0, 0); c.ExpiresAt = &e }},
	}
	for _, c := range invalids {
		cmd := ask(agentFor(policy.AutonomyWrite), scope, space)
		c.edit(cmd)
		_, err := f.l.Apply(ctx, cmd)
		var ve *ledger.ValidationError
		if !errors.As(err, &ve) || ve.Field != c.field {
			t.Errorf("%s: %v, want invalid %s", c.name, err, c.field)
		}
	}
	if n := f.count(`SELECT count(*) FROM v2.decision_gates`); n != 0 {
		t.Fatalf("%d gates written by refused commands", n)
	}

	// A space outside the scope is not found.
	other := f.space(f.user("jy"), policy.SpaceProject, "elsewhere")
	if _, err := f.l.Apply(ctx, ask(agentFor(policy.AutonomyWrite), scope, other)); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("asking outside the scope: %v", err)
	}

	// One agent has at most three waiting in a space; an expiry of its own
	// is kept; another agent, or another space, has room.
	codex := agentFor(policy.AutonomyPropose)
	in := time.Now().Add(48 * time.Hour).Truncate(time.Microsecond)
	for i := range policy.MaxWaitingGates {
		cmd := ask(codex, scope, space)
		cmd.Question = fmt.Sprintf("Question %d?", i)
		cmd.ExpiresAt = &in
		if g := f.apply(cmd).Gate; g == nil || !g.ExpiresAt.Equal(in) {
			t.Fatalf("gate %d: %+v", i, g)
		}
	}
	res := f.apply(ask(codex, scope, space))
	refusedWith(t, res, policy.CodeGateLimit)
	f.asked(agentFor(policy.AutonomyPropose), scope, space)
	side := f.space(zz, policy.SpaceProject, "side")
	f.asked(codex, f.scope(zz), side)
	// An answered (or expired) gate frees its place.
	page, err := f.l.ListGates(ctx, scope, ledger.GateQuery{SpaceID: space, Statuses: []ledger.GateStatus{ledger.GateWaiting}})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range page.Gates {
		if g.AskedBy == codex.ID {
			f.apply(answer(person(zz), scope, policy.ViaWeb, g.Ref, 1))
			break
		}
	}
	f.asked(codex, scope, space)
	later := f.clockAt(time.Now().Add(72 * time.Hour))
	cmd := ask(codex, scope, space)
	cmd.Question = "After the others expired?"
	if res, err := later.Apply(ctx, cmd); err != nil || res.Outcome != ledger.OutcomeApplied {
		t.Errorf("asking after the waiting ones expired: %v %+v", err, res.Policy)
	}
}

// Two agents racing for the last place: one asks, the other is refused.
func TestRequestDecisionCapUnderConcurrency(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	codex := agentFor(policy.AutonomyWrite)
	for i := range policy.MaxWaitingGates - 1 {
		cmd := ask(codex, scope, space)
		cmd.Question = fmt.Sprintf("Question %d?", i)
		f.apply(cmd)
	}
	var wg sync.WaitGroup
	results := make([]ledger.Result, 4)
	errs := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := ask(codex, scope, space)
			cmd.Question = fmt.Sprintf("Racing %d?", i)
			results[i], errs[i] = f.l.Apply(context.Background(), cmd)
		}()
	}
	wg.Wait()
	asked := 0
	for i, res := range results {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if res.Outcome == ledger.OutcomeApplied {
			asked++
		} else if res.Policy.Code != policy.CodeGateLimit {
			t.Errorf("racer %d: %+v", i, res.Policy)
		}
	}
	if asked != 1 {
		t.Errorf("%d racers asked, want exactly 1", asked)
	}
	if n := f.count(`SELECT count(*) FROM v2.decision_gates WHERE asked_by = $1`, codex.ID); n != policy.MaxWaitingGates {
		t.Errorf("%d gates waiting, want %d", n, policy.MaxWaitingGates)
	}
}

// A person's answer becomes a kept decision they authored, in the same
// transaction: the question and the chosen option as its words, the gate
// as its source (the agent's own work), receipts both ways, and every
// target recompiles.
func TestAnswerGateKeepsADecision(t *testing.T) {
	t.Parallel()
	f := newCompileFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.brief(zz, space, 0, demoSections())
	target := f.target(zz, space, ledger.TargetAgentsMD)
	before := f.get(zz, target.ID).DirtyGen
	g := f.asked(agentFor(policy.AutonomyPropose), f.scope(zz), space)

	cmd := answer(person(zz), f.scope(zz), policy.ViaCLI, g.Ref, 1)
	cmd.Reason = "The workers already run there."
	cmd.ExpectedVersion = 1
	res := f.apply(cmd)
	if res.Outcome != ledger.OutcomeApplied || res.Gate == nil || res.Memory == nil {
		t.Fatalf("answer: %+v", res)
	}
	m := res.Memory
	if m.Statement != deployQuestion+" Fly.io, iad and ams" || m.Kind != ledger.KindDecision || m.Section != ledger.SectionDecisions ||
		m.Lifecycle != "kept" || m.Trust != policy.TrustAgentOwnWork {
		t.Errorf("decision = %+v", m)
	}
	if m.Decision == nil || m.Decision.Status != ledger.DecisionInForce || len(m.Decision.Options) != 3 || !strings.Contains(m.Decision.Why, "M-0174") {
		t.Errorf("decision fields = %+v", m.Decision)
	}
	if len(m.Sources) != 1 || m.Sources[0].Kind != ledger.SourceSession || m.Sources[0].Ref != g.Ref || m.Sources[0].Trust != policy.TrustAgentOwnWork ||
		!strings.Contains(string(m.Sources[0].Locator), g.ID.String()) || !strings.Contains(string(m.Sources[0].Locator), "cx-9f1c") {
		t.Errorf("sources = %+v", m.Sources)
	}
	a := res.Gate.Answer
	if res.Gate.Status != ledger.GateAnswered || res.Gate.Version != 2 || a == nil || a.Option != 1 || a.Label != "Fly.io, iad and ams" ||
		a.Memory.ID != m.ID || a.Memory.Ref != m.Ref || a.AnsweredBy != zz || a.Assurance != policy.AssuranceClientAttested {
		t.Errorf("gate = %+v answer %+v", res.Gate, a)
	}
	if res.Gate.DeliveredAt != nil {
		t.Errorf("the agent hasn't been told yet: delivered %v", res.Gate.DeliveredAt)
	}
	if len(res.Receipts) != 2 {
		t.Fatalf("receipts = %+v", res.Receipts)
	}
	kept, answered := res.Receipts[0], res.Receipts[1]
	if kept.ObjectKind != ledger.ObjectMemory || kept.ObjectID != m.ID || kept.Action != ledger.ActionKept || kept.ActorKind != policy.ActorPerson ||
		*kept.ActorID != zz || kept.Assurance != policy.AssuranceClientAttested || kept.Source == nil || kept.Source.Ref != g.Ref ||
		kept.Source.Kind != ledger.ObjectGate || kept.Via != policy.ViaCLI || kept.Reason != cmd.Reason {
		t.Errorf("kept receipt = %+v", kept)
	}
	if answered.ObjectKind != ledger.ObjectGate || answered.ObjectID != g.ID || answered.Action != ledger.ActionAnswered ||
		answered.StreamVersion != 2 || answered.Assurance != policy.AssuranceClientAttested || answered.Source == nil ||
		answered.Source.Ref != m.Ref || answered.ID != res.Gate.LastReceiptID {
		t.Errorf("answered receipt = %+v", answered)
	}
	if after := f.get(zz, target.ID); after.DirtyGen != before+1 || f.jobs(target.ID) != 1 {
		t.Errorf("target generation %d → %d, %d jobs; an answer recompiles", before, after.DirtyGen, f.jobs(target.ID))
	}
	// A person's kept decision isn't judged (the plan's log), like any of
	// their remembers.
	if n := f.count(`SELECT count(*) FROM river_job WHERE kind = 'judge_proposal'`); n != 0 {
		t.Errorf("%d judge jobs for a person's answer", n)
	}

	// The replay answers the same; a second answer refuses cleanly.
	if again := f.apply(cmd); !again.Replayed || again.Memory == nil || again.Memory.ID != m.ID || again.Gate.Status != ledger.GateAnswered {
		t.Errorf("replay = %+v", again)
	}
	_, err := f.l.Apply(context.Background(), answer(person(zz), f.scope(zz), policy.ViaWeb, g.Ref, 2))
	gateStateIs(t, err, ledger.GateAnswered)
	// Sent with the version the person read, it still says why (not 412).
	stale := answer(person(zz), f.scope(zz), policy.ViaWeb, g.Ref, 2)
	stale.ExpectedVersion = 1
	_, err = f.l.Apply(context.Background(), stale)
	gateStateIs(t, err, ledger.GateAnswered)
	if n := f.count(`SELECT count(*) FROM v2.memories WHERE kind = 'decision'`); n != 1 {
		t.Errorf("%d decisions, want 1", n)
	}
}

// D15: in a space whose decisions need a person on the web, a
// client-attested answer is refused and the gate keeps waiting; on the web
// it is kept, human_web.
func TestAnswerGateNeedsTheWebForTeamDecisions(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	jy := f.user("jy")
	team := f.space(zz, policy.SpaceTeam, "memax-team")
	f.join(team, jy, "contributor")
	g := f.asked(agentFor(policy.AutonomyPropose), f.scope(zz), team)
	ctx := context.Background()

	for _, via := range []policy.Via{policy.ViaCLI, policy.ViaMCP, policy.ViaAPI} {
		res, err := f.l.Apply(ctx, answer(person(jy), f.scope(jy), via, g.Ref, 2))
		if err != nil {
			t.Fatal(err)
		}
		refusedWith(t, res, policy.CodeDecisionNeedsWeb)
		if !strings.Contains(res.Policy.Message, g.Ref) {
			t.Errorf("message %q doesn't name the gate", res.Policy.Message)
		}
	}
	if got, _ := f.l.GetGate(ctx, f.scope(jy), g.ID.String()); got.Status != ledger.GateWaiting || got.Answer != nil {
		t.Fatalf("after refused answers: %+v", got)
	}
	if n := f.count(`SELECT count(*) FROM v2.memories`); n != 0 {
		t.Errorf("%d memories written by refused answers", n)
	}
	res := f.apply(answer(person(jy), f.scope(jy), policy.ViaWeb, g.Ref, 2))
	if res.Gate.Answer.Assurance != policy.AssuranceHumanWeb || res.Receipts[0].Assurance != policy.AssuranceHumanWeb || res.Memory.Statement != deployQuestion+" Railway" {
		t.Errorf("web answer = %+v %+v", res.Gate.Answer, res.Receipts)
	}

	// With the team's rule off, the CLI answers.
	off := f.space(zz, policy.SpaceTeam, "relaxed")
	f.setRules(off, `{"decisions_need_web": false}`)
	g2 := f.asked(agentFor(policy.AutonomyPropose), f.scope(zz), off)
	if g2.NeedsWeb {
		t.Error("needs_web with the rule off")
	}
	if res := f.apply(answer(person(zz), f.scope(zz).Narrow(off), policy.ViaCLI, g2.Ref, 1)); res.Outcome != ledger.OutcomeApplied {
		t.Errorf("CLI answer with the rule off: %+v", res.Policy)
	}
}

// Who may answer, which option, and which version.
func TestAnswerGateRefusals(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	viewer := f.user("viewer")
	member := f.user("member")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(space, viewer, "viewer")
	f.join(space, member, "contributor")
	strict := f.space(zz, policy.SpaceProject, "owners-only")
	f.join(strict, member, "contributor")
	f.setRules(strict, `{"keep": "owners"}`)
	codex := agentFor(policy.AutonomyWrite)
	g := f.asked(codex, f.scope(zz), space)
	gs := f.asked(codex, f.scope(zz), strict)
	ctx := context.Background()

	key := person(zz)
	key.Credential = policy.CredentialAPIKey
	refusals := []struct {
		name  string
		actor ledger.Actor
		scope ledger.Scope
		gate  string
		code  string
	}{
		{"the asking agent", codex, f.scope(zz), g.Ref, policy.CodePersonMustAnswer},
		{"another agent", agentFor(policy.AutonomyWrite), f.scope(zz), g.Ref, policy.CodePersonMustAnswer},
		{"an API key", key, f.scope(zz), g.Ref, policy.CodeKeyCannotReview},
		{"a viewer", person(viewer), f.scope(viewer), g.Ref, policy.CodeViewer},
		{"a member where owners keep", person(member), f.scope(member), gs.Ref, policy.CodeOwnersKeep},
		{"a secret in the reason", person(zz), f.scope(zz), g.Ref, policy.CodeSecret},
	}
	for _, c := range refusals {
		cmd := answer(c.actor, c.scope, policy.ViaWeb, c.gate, 1)
		if c.code == policy.CodeSecret {
			cmd.Reason = "We use AKIAIOSFODNN7EXAMPLE there."
		}
		res, err := f.l.Apply(ctx, cmd)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		refusedWith(t, res, c.code)
	}
	if res := f.apply(answer(person(member), f.scope(member), policy.ViaWeb, g.Ref, 3)); res.Outcome != ledger.OutcomeApplied {
		t.Errorf("a member answers: %+v", res.Policy)
	}

	g2 := f.asked(codex, f.scope(zz), space)
	for _, n := range []int{0, 4, -1} {
		_, err := f.l.Apply(ctx, answer(person(zz), f.scope(zz), policy.ViaWeb, g2.Ref, n))
		var ve *ledger.ValidationError
		if !errors.As(err, &ve) || ve.Field != "option" {
			t.Errorf("option %d: %v", n, err)
		}
	}
	stale := answer(person(zz), f.scope(zz), policy.ViaWeb, g2.Ref, 1)
	stale.ExpectedVersion = 2
	if _, err := f.l.Apply(ctx, stale); !errors.Is(err, ledger.ErrEditClash) {
		t.Errorf("If-Match 2 on a version-1 gate: %v", err)
	}
	for _, ref := range []string{"M-0001", "G-x", ""} {
		_, err := f.l.Apply(ctx, answer(person(zz), f.scope(zz), policy.ViaWeb, ref, 1))
		if !errors.Is(err, ledger.ErrInvalid) {
			t.Errorf("ref %q: %v", ref, err)
		}
	}
	if _, err := f.l.Apply(ctx, answer(person(zz), f.scope(zz), policy.ViaWeb, "G-0999", 1)); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("an unknown gate: %v", err)
	}
}

// Two people answering at once: one answer wins, the other refuses, and
// exactly one decision is kept.
func TestAnswerGateOnceUnderConcurrency(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	jy := f.user("jy")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(space, jy, "contributor")
	g := f.asked(agentFor(policy.AutonomyPropose), f.scope(zz), space)

	type outcome struct {
		res ledger.Result
		err error
	}
	people := []uuid.UUID{zz, jy, zz, jy}
	out := make([]outcome, len(people))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, who := range people {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := f.l.Apply(context.Background(), answer(person(who), f.scope(who), policy.ViaWeb, g.Ref, i%3+1))
			out[i] = outcome{res, err}
		}()
	}
	close(start)
	wg.Wait()
	won := 0
	for i, o := range out {
		switch {
		case o.err == nil && o.res.Outcome == ledger.OutcomeApplied:
			won++
		case o.err != nil:
			gateStateIs(t, o.err, ledger.GateAnswered)
		default:
			t.Errorf("answer %d: %+v", i, o.res)
		}
	}
	if won != 1 {
		t.Fatalf("%d answers won, want 1", won)
	}
	if n := f.count(`SELECT count(*) FROM v2.memories WHERE kind = 'decision'`); n != 1 {
		t.Errorf("%d decisions kept, want 1", n)
	}
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE object_id = $1 AND action = 'answered'`, g.ID); n != 1 {
		t.Errorf("%d answered receipts, want 1", n)
	}
}

// A waiting gate past its time reads as expired, can't be answered or
// withdrawn, and lists under expired.
func TestGateExpiry(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	codex := agentFor(policy.AutonomyPropose)
	cmd := ask(codex, scope, space)
	soon := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	cmd.ExpiresAt = &soon
	g := f.apply(cmd).Gate
	fresh := f.asked(codex, scope, space)
	ctx := context.Background()

	later := f.clockAt(soon.Add(time.Second))
	got, err := later.GetGate(ctx, scope, g.Ref)
	if err != nil || got.Status != ledger.GateExpired {
		t.Fatalf("after its time: %+v %v", got, err)
	}
	if now, _ := f.l.GetGate(ctx, scope, g.Ref); now.Status != ledger.GateWaiting {
		t.Errorf("before its time: %s", now.Status)
	}
	_, err = later.Apply(ctx, answer(person(zz), scope, policy.ViaWeb, g.Ref, 1))
	gateStateIs(t, err, ledger.GateExpired)
	_, err = later.Apply(ctx, &ledger.WithdrawGate{Meta: meta(codex, scope, policy.ViaMCP), Gate: g.Ref})
	gateStateIs(t, err, ledger.GateExpired)

	for _, c := range []struct {
		status ledger.GateStatus
		want   []string
	}{
		{ledger.GateExpired, []string{g.Ref}},
		{ledger.GateWaiting, []string{fresh.Ref}},
		{ledger.GateAnswered, nil},
	} {
		page, err := later.ListGates(ctx, scope, ledger.GateQuery{SpaceID: space, Statuses: []ledger.GateStatus{c.status}})
		if err != nil {
			t.Fatal(err)
		}
		var refs []string
		for _, x := range page.Gates {
			refs = append(refs, x.Ref)
		}
		if fmt.Sprint(refs) != fmt.Sprint(c.want) {
			t.Errorf("%s: %v, want %v", c.status, refs, c.want)
		}
	}
	if _, err := f.l.ListGates(ctx, scope, ledger.GateQuery{SpaceID: space, Statuses: []ledger.GateStatus{"open"}}); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("an unknown status: %v", err)
	}
}

// The asking agent, the person it works for, or anyone who could answer
// withdraws a waiting question; nobody else.
func TestWithdrawGate(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	viewer := f.user("viewer")
	member := f.user("member")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	f.join(space, viewer, "viewer")
	f.join(space, member, "contributor")
	ctx := context.Background()
	// The viewer's own agent, through a real connection.
	conn := f.connect(viewer, ledger.CredentialOAuthGrant, f.grant(viewer, "codex"), ledger.AgentCodex, at(space, policy.AutonomyPropose))
	viewersAgent, viewerScope := f.agentActor(viewer, conn, policy.AutonomyWrite)
	codex := agentFor(policy.AutonomyPropose)

	withdraw := func(actor ledger.Actor, scope ledger.Scope, ref string) (ledger.Result, error) {
		return f.l.Apply(ctx, &ledger.WithdrawGate{Meta: meta(actor, scope, policy.ViaMCP), Gate: ref})
	}

	// The asking agent: withdrawn, and it knows already.
	g := f.asked(codex, f.scope(zz), space)
	res, err := withdraw(codex, f.scope(zz), g.Ref)
	if err != nil || res.Outcome != ledger.OutcomeApplied {
		t.Fatalf("the asker withdraws: %v %+v", err, res.Policy)
	}
	w := res.Gate
	if w.Status != ledger.GateWithdrawn || w.Version != 2 || w.Withdrawn == nil || w.Withdrawn.ByKind != policy.ActorAgent ||
		w.Withdrawn.By != codex.ID || w.DeliveredAt == nil {
		t.Errorf("withdrawn = %+v %+v", w, w.Withdrawn)
	}
	if rc := res.Receipts; len(rc) != 1 || rc[0].Action != ledger.ActionWithdrawn || rc[0].ObjectRef != g.Ref || rc[0].StreamVersion != 2 {
		t.Errorf("receipts = %+v", rc)
	}
	_, err = withdraw(codex, f.scope(zz), g.Ref)
	gateStateIs(t, err, ledger.GateWithdrawn)
	_, err = f.l.Apply(ctx, answer(person(zz), f.scope(zz), policy.ViaWeb, g.Ref, 1))
	gateStateIs(t, err, ledger.GateWithdrawn)

	// Another agent can't; a viewer can't take someone else's back; a
	// member and the viewer whose agent asked can.
	g2 := f.asked(codex, f.scope(zz), space)
	res, _ = withdraw(agentFor(policy.AutonomyWrite), f.scope(zz), g2.Ref)
	refusedWith(t, res, policy.CodeNotYourGate)
	res, _ = withdraw(person(viewer), f.scope(viewer), g2.Ref)
	refusedWith(t, res, policy.CodeNotYourGate)
	res, _ = withdraw(person(member), f.scope(member), g2.Ref)
	if res.Outcome != ledger.OutcomeApplied || res.Gate.Withdrawn.ByKind != policy.ActorPerson || res.Gate.DeliveredAt != nil {
		t.Errorf("a member withdraws: %+v", res.Gate)
	}
	g3 := f.asked(viewersAgent, viewerScope, space)
	res, _ = withdraw(person(viewer), f.scope(viewer), g3.Ref)
	if res.Outcome != ledger.OutcomeApplied {
		t.Errorf("the person whose agent asked withdraws: %+v", res.Policy)
	}

	// A paused agent only reads, its own question included.
	g4 := f.asked(viewersAgent, viewerScope, space)
	f.apply(&ledger.PauseAgent{Meta: meta(person(viewer), f.scope(viewer), policy.ViaWeb), Connection: conn.ID})
	paused, pausedScope := f.agentActor(viewer, conn, policy.AutonomyWrite)
	res, _ = withdraw(paused, pausedScope, g4.Ref)
	refusedWith(t, res, policy.CodeAgentPaused)

	// An answered gate can't be withdrawn.
	g5 := f.asked(codex, f.scope(zz), space)
	f.apply(answer(person(zz), f.scope(zz), policy.ViaWeb, g5.Ref, 2))
	_, err = withdraw(codex, f.scope(zz), g5.Ref)
	gateStateIs(t, err, ledger.GateAnswered)
}

// How a gate ended reaches the agent that asked once: an answer, a
// person's withdrawal, an expiry. Its own withdrawal and an answer it
// received in the same call aren't repeated; waiting ones are listed for
// the digest; another agent hears nothing.
func TestTakeGateNews(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	codex := agentFor(policy.AutonomyPropose)
	ctx := context.Background()

	answered := f.asked(codex, scope, space)
	f.apply(answer(person(zz), scope, policy.ViaWeb, answered.Ref, 2))
	withdrawnByPerson := f.asked(codex, scope, space)
	f.apply(&ledger.WithdrawGate{Meta: meta(person(zz), scope, policy.ViaWeb), Gate: withdrawnByPerson.Ref})
	withdrawnByAgent := f.asked(codex, scope, space)
	f.apply(&ledger.WithdrawGate{Meta: meta(codex, scope, policy.ViaMCP), Gate: withdrawnByAgent.Ref})
	inAgent := f.asked(codex, scope, space)
	told := answer(person(zz), scope, policy.ViaMCP, inAgent.Ref, 1)
	told.Delivered = true
	f.apply(told)
	expiring := ask(codex, scope, space)
	soon := time.Now().Add(10 * time.Minute)
	expiring.ExpiresAt = &soon
	expired := f.apply(expiring).Gate
	waiting := f.asked(codex, scope, space)
	f.asked(agentFor(policy.AutonomyPropose), scope, space) // someone else's

	later := f.clockAt(soon.Add(time.Minute))
	news, err := later.TakeGateNews(ctx, scope.Narrow(space), codex.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	var ended []string
	for _, g := range news.Ended {
		ended = append(ended, g.Ref+"="+string(g.Status))
	}
	want := []string{answered.Ref + "=answered", withdrawnByPerson.Ref + "=withdrawn", expired.Ref + "=expired"}
	if fmt.Sprint(ended) != fmt.Sprint(want) {
		t.Errorf("ended = %v, want %v", ended, want)
	}
	if len(news.Ended) > 0 && (news.Ended[0].Answer == nil || news.Ended[0].Answer.Label != "Railway" || news.Ended[0].Answer.Memory.Ref == "") {
		t.Errorf("the answer = %+v", news.Ended[0].Answer)
	}
	if len(news.Waiting) != 1 || news.Waiting[0].Ref != waiting.Ref {
		t.Errorf("waiting = %+v", news.Waiting)
	}
	again, err := later.TakeGateNews(ctx, scope.Narrow(space), codex.ID, false)
	if err != nil || len(again.Ended) != 0 || again.Waiting != nil {
		t.Errorf("told twice: %+v %v", again, err)
	}
	// Telling is a read: it wrote no receipt.
	if n := f.count(`SELECT count(*) FROM v2.receipts WHERE object_kind = 'gate'`); n != 7+3+1 {
		t.Errorf("%d gate receipts, want 11 (7 asked, 1+1 answered, 2 withdrawn)", n)
	}
}

// Gates are space-scoped records: invisible and untouchable from another
// space or tenant, through the ledger and through SQL as memax_v2.
func TestGatesAreIsolated(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	jy := f.user("jy")
	mine := f.space(zz, policy.SpaceProject, "memax-v2")
	side := f.space(zz, policy.SpaceProject, "side")
	theirs := f.space(jy, policy.SpaceProject, "elsewhere")
	g := f.asked(agentFor(policy.AutonomyPropose), f.scope(jy), theirs)
	f.asked(agentFor(policy.AutonomyPropose), f.scope(zz), mine)
	// Display IDs repeat across tenants: G-0001 exists in both.
	if g.Ref != "G-0001" {
		t.Fatalf("their gate is %s", g.Ref)
	}
	zzScope := f.scope(zz)
	if got, err := f.l.GetGate(ctx, zzScope, g.ID.String()); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another tenant's gate by id: %+v %v", got, err)
	}
	if got, err := f.l.GetGate(ctx, zzScope.Narrow(side), "G-0001"); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("a gate from another space of the tenant: %+v %v", got, err)
	}
	if _, err := f.l.ListGates(ctx, zzScope, ledger.GateQuery{SpaceID: theirs}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("listing another tenant's space: %v", err)
	}
	if _, err := f.l.Apply(ctx, answer(person(zz), zzScope, policy.ViaWeb, g.ID.String(), 1)); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("answering across tenants: %v", err)
	}
	if news, err := f.l.TakeGateNews(ctx, zzScope, g.AskedBy, true); err != nil || len(news.Waiting) != 0 {
		t.Errorf("news across tenants: %+v %v", news, err)
	}
	var mineTenant uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT tenant_id FROM hubs WHERE id = $1`, mine).Scan(&mineTenant); err != nil {
		t.Fatal(err)
	}
	err := f.rawAs(mine, mineTenant, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v2.decision_gates`).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("scope A sees %d gates, want its own 1", n)
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v2.decision_gates WHERE id = $1`, g.ID).Scan(&n); err != nil || n != 0 {
			return fmt.Errorf("scope A sees B's gate: %d %v", n, err)
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
	if err := f.asV2(nil, nil, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v2.decision_gates`).Scan(&n); err != nil || n != 0 {
			return fmt.Errorf("no scope sees %d gates: %v", n, err)
		}
		return nil
	}); err != nil {
		t.Error(err)
	}
}

// The database holds gates to rule 1 and to the status machine, whatever
// the caller: no write without a same-transaction receipt about the gate
// (telling the agent excepted), no second ending, no DELETE.
func TestGateGuaranteesInSQL(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	g := f.asked(agentFor(policy.AutonomyPropose), f.scope(zz), space)
	answeredGate := f.asked(agentFor(policy.AutonomyPropose), f.scope(zz), space)
	m := f.apply(answer(person(zz), f.scope(zz), policy.ViaWeb, answeredGate.Ref, 1)).Memory
	tenant := g.TenantID
	gateReceipt := func(tx pgx.Tx, object uuid.UUID, action string, version int) (uuid.UUID, error) {
		id := uuid.Must(uuid.NewV7())
		_, err := tx.Exec(ctx, `
			INSERT INTO v2.receipts (id, tenant_id, space_id, object_kind, object_id, object_ref, action, actor_kind, actor_id, via, occurred_at, stream_id, stream_version)
			VALUES ($1, $2, $3, 'gate', $4, 'G-0099', $5, 'person', $6, 'web', now(), $4, $7)`,
			id, tenant, space, object, action, zz, version)
		return id, err
	}
	cases := []struct {
		name string
		fn   func(tx pgx.Tx) error
		want string
	}{
		{"insert a gate with an old receipt", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO v2.decision_gates (id, tenant_id, space_id, seq, question, options, expires_at, asked_by, created_receipt_id, last_receipt_id)
				VALUES ($1, $2, $3, 99, 'Smuggled?', '[{"label":"a"},{"label":"b"}]', now() + interval '1 day', $4, $5, $5)`,
				uuid.New(), tenant, space, uuid.New(), g.CreatedReceiptID)
			return err
		}, "MXR01"},
		{"withdraw without a receipt", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE v2.decision_gates SET status = 'withdrawn', withdrawn_by_kind = 'person', withdrawn_by = $2, withdrawn_at = now() WHERE id = $1`, g.ID, zz)
			return err
		}, "MXR01"},
		{"a receipt about a memory doesn't cover a gate", func(tx pgx.Tx) error {
			rid, err := insertReceiptSQL(tx, m, g.ID, 2)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE v2.decision_gates SET status = 'withdrawn', withdrawn_by_kind = 'person', withdrawn_by = $2, withdrawn_at = now(),
			                       stream_version = 2, last_receipt_id = $3 WHERE id = $1`, g.ID, zz, rid)
			return err
		}, "MXR01"},
		{"telling the agent needs no receipt", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE v2.decision_gates SET delivered_at = now() WHERE id = $1`, answeredGate.ID)
			return err
		}, ""},
		{"a gate ends once", func(tx pgx.Tx) error {
			rid, err := gateReceipt(tx, answeredGate.ID, "withdrawn", 3)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE v2.decision_gates SET status = 'withdrawn', withdrawn_by_kind = 'person', withdrawn_by = $2, withdrawn_at = now(),
			                       stream_version = 3, last_receipt_id = $3 WHERE id = $1`, answeredGate.ID, zz, rid)
			return err
		}, "MXL03"},
		{"a gate starts waiting", func(tx pgx.Tx) error {
			id := uuid.New()
			rid, err := gateReceipt(tx, id, "asked", 1)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO v2.decision_gates (id, tenant_id, space_id, seq, question, options, status, expires_at, asked_by,
				                               withdrawn_by_kind, withdrawn_by, withdrawn_at, created_receipt_id, last_receipt_id)
				VALUES ($1, $2, $3, 98, 'Already over?', '[{"label":"a"},{"label":"b"}]', 'withdrawn', now() + interval '1 day', $4,
				        'person', $4, now(), $5, $5)`, id, tenant, space, zz, rid)
			return err
		}, "MXL03"},
		{"a waiting gate keeps its words", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE v2.decision_gates SET question = 'Rewritten?' WHERE id = $1`, g.ID)
			return err
		}, "MXL03"},
		{"a waiting gate's words aren't purged", func(tx pgx.Tx) error {
			rid, err := gateReceipt(tx, g.ID, "forgot", 2)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE v2.decision_gates SET question = NULL, context = NULL, options = NULL,
			                       stream_version = 2, last_receipt_id = $2 WHERE id = $1`, g.ID, rid)
			return err
		}, "MXL03"},
		{"withdrawing with its receipt commits", func(tx pgx.Tx) error {
			rid, err := gateReceipt(tx, g.ID, "withdrawn", 2)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE v2.decision_gates SET status = 'withdrawn', withdrawn_by_kind = 'person', withdrawn_by = $2, withdrawn_at = now(),
			                       stream_version = 2, last_receipt_id = $3 WHERE id = $1`, g.ID, zz, rid)
			return err
		}, ""},
		{"no deleting", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `DELETE FROM v2.decision_gates`)
			return err
		}, "42501"},
	}
	for _, c := range cases {
		err := f.rawAs(space, tenant, c.fn)
		if got := sqlstate(err); got != c.want {
			t.Errorf("%s: %v (SQLSTATE %q), want %q", c.name, err, got, c.want)
		}
	}
	// The trigger binds a superuser too.
	if _, err := f.pool.Exec(ctx, `UPDATE v2.decision_gates SET answer_option = 2 WHERE id = $1`, answeredGate.ID); sqlstate(err) != "MXL03" && sqlstate(err) != "MXR01" {
		t.Errorf("superuser rewrite of an answer: %v", err)
	}
}

// The status machine is defined twice, in Go and SQL; they agree.
func TestGateStatusMatchesSQL(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	from := []ledger.GateStatus{"", ledger.GateWaiting, ledger.GateAnswered, ledger.GateWithdrawn}
	for _, a := range from {
		for _, b := range []ledger.GateStatus{ledger.GateWaiting, ledger.GateAnswered, ledger.GateWithdrawn} {
			var sqlFrom any = string(a)
			if a == "" {
				sqlFrom = nil
			}
			var got bool
			if err := f.pool.QueryRow(context.Background(), `SELECT v2.gate_status_transition_allowed($1::text, $2)`, sqlFrom, string(b)).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if want := ledger.GateTransitionAllowed(a, b); got != want {
				t.Errorf("%q → %q: SQL %v, Go %v", a, b, got, want)
			}
		}
	}
}

// Every gate row carries its receipts, and receipts, idempotency records
// and logs never hold the question's words.
func TestGateReceiptsAndWords(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	words := []string{"heliotropequasar", "saffronmeridian", "obsidiankestrel", "lanternbasalt"}
	codex := agentFor(policy.AutonomyPropose)
	cmd := ask(codex, scope, space)
	cmd.Question = "Should the " + words[0] + " queue move?"
	cmd.Context = "The " + words[1] + " workers are idle."
	cmd.Options = []ledger.DecisionOption{{Label: "Move to " + words[2]}, {Label: "Keep " + words[3]}}
	g := f.apply(cmd).Gate
	f.apply(answer(person(zz), scope, policy.ViaWeb, g.Ref, 1))
	g2 := f.asked(codex, scope, space)
	f.apply(&ledger.WithdrawGate{Meta: meta(codex, scope, policy.ViaMCP), Gate: g2.Ref})
	f.asked(codex, scope, space)

	for _, table := range []string{"v2.receipts", "v2.command_keys", "v2.id_counters"} {
		rows, err := f.pool.Query(ctx, `SELECT t::text FROM `+table+` t`)
		if err != nil {
			t.Fatal(err)
		}
		texts, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range texts {
			for _, w := range words {
				if strings.Contains(strings.ToLower(row), w) {
					t.Errorf("%s holds the words %q: %s", table, w, row)
				}
			}
		}
	}
	for _, w := range words {
		if strings.Contains(strings.ToLower(f.logs.String()), w) {
			t.Errorf("logs hold the words %q", w)
		}
	}
	sweep := []struct {
		what, sql string
	}{
		{"gates without their asked receipt", `SELECT count(*) FROM v2.decision_gates g LEFT JOIN v2.receipts r
			ON r.id = g.created_receipt_id AND r.space_id = g.space_id AND r.object_id = g.id AND r.object_kind = 'gate' AND r.action = 'asked'
			AND r.actor_kind = 'agent' AND r.actor_id = g.asked_by WHERE r.id IS NULL`},
		{"gates whose latest receipt isn't about them", `SELECT count(*) FROM v2.decision_gates g LEFT JOIN v2.receipts r
			ON r.id = g.last_receipt_id AND r.space_id = g.space_id AND r.object_id = g.id AND r.stream_version = g.stream_version
			WHERE r.id IS NULL`},
		{"answered gates without an answered receipt", `SELECT count(*) FROM v2.decision_gates g LEFT JOIN v2.receipts r
			ON r.id = g.last_receipt_id AND r.action = 'answered' AND r.actor_kind = 'person' AND r.actor_id = g.answered_by
			AND r.assurance = g.assurance WHERE g.status = 'answered' AND r.id IS NULL`},
		{"answers not kept by the person who answered", `SELECT count(*) FROM v2.decision_gates g JOIN v2.memories m ON m.id = g.answer_memory_id
			LEFT JOIN v2.receipts r ON r.id = m.created_receipt_id AND r.action = 'kept' AND r.actor_kind = 'person' AND r.actor_id = g.answered_by
			WHERE r.id IS NULL OR m.lifecycle <> 'kept' OR m.kind <> 'decision'`},
		{"withdrawn gates without a withdrawn receipt", `SELECT count(*) FROM v2.decision_gates g LEFT JOIN v2.receipts r
			ON r.id = g.last_receipt_id AND r.action = 'withdrawn' AND r.actor_id = g.withdrawn_by WHERE g.status = 'withdrawn' AND r.id IS NULL`},
	}
	for _, c := range sweep {
		if n := f.count(c.sql); n != 0 {
			t.Errorf("%s: %d", c.what, n)
		}
	}
	if n := f.count(`SELECT count(*) FROM v2.decision_gates`); n != 3 {
		t.Fatalf("%d gates; the sweep checked nothing", n)
	}
}

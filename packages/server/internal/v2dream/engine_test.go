package v2dream_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/MemaxLabs/memax/packages/server/internal/email"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2dream"
	"github.com/MemaxLabs/memax/packages/server/internal/v2dream/dreamtest"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// world is a V2 space with what each phase acts on.
type world struct {
	t            *testing.T
	pool         *pgxpool.Pool
	l            *ledger.Ledger
	owner, space uuid.UUID
	m            map[string]*ledger.Memory
}

func newWorld(t *testing.T, opts ...ledger.Option) *world {
	t.Helper()
	_, pool := testdb.Acquire(t)
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, pool: pool, m: map[string]*ledger.Memory{}}
	w.l = ledger.New(pool, append([]ledger.Option{ledger.WithLogger(quiet), ledger.WithJobs(client)}, opts...)...)
	w.owner = w.user("zz")
	w.space = uuid.New()
	w.exec(`INSERT INTO hubs (id, name, slug, hub_type, owner_id, space_kind, v2_enabled_at) VALUES ($1, 'memax-v2', $2, 'team', $3, 'project', now())`,
		w.space, "memax-v2-"+w.space.String()[:8], w.owner)
	w.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'owner')`, w.space, w.owner)
	return w
}

// riverClient is the insert-only River client the sweep queues through.
func (w *world) riverClient() *river.Client[pgx.Tx] {
	w.t.Helper()
	c, err := river.NewClient(riverpgxv5.New(w.pool), &river.Config{Logger: quiet})
	if err != nil {
		w.t.Fatal(err)
	}
	return c
}

func (w *world) exec(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.pool.Exec(context.Background(), sql, args...); err != nil {
		w.t.Fatalf("exec: %v", err)
	}
}

func (w *world) count(sql string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		w.t.Fatalf("count: %v", err)
	}
	return n
}

func (w *world) user(name string) uuid.UUID {
	id := uuid.New()
	w.exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`, id, name+"-"+id.String()[:6]+"@example.com", name)
	return id
}

func (w *world) scope() ledger.Scope {
	w.t.Helper()
	s, err := w.l.UserScope(context.Background(), w.owner)
	if err != nil {
		w.t.Fatal(err)
	}
	return s
}

func (w *world) meta() ledger.Meta {
	return ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: w.owner, Name: "Ziyang"}, Scope: w.scope(),
		Via: policy.ViaWeb, IdempotencyKey: uuid.NewString()}
}

func (w *world) apply(cmd ledger.Command) ledger.Result {
	w.t.Helper()
	res, err := w.l.Apply(context.Background(), cmd)
	if err != nil {
		w.t.Fatalf("%s: %v", cmd.Name(), err)
	}
	return res
}

func (w *world) keep(key, statement string, mod func(*ledger.NewMemory)) *ledger.Memory {
	nm := ledger.NewMemory{SpaceID: w.space, Statement: statement, Section: ledger.SectionConventions}
	if mod != nil {
		mod(&nm)
	}
	m := w.apply(&ledger.Remember{Meta: w.meta(), NewMemory: nm}).Memory
	w.m[key] = m
	return m
}

func (w *world) propose(key, statement string) *ledger.Memory {
	m := w.apply(&ledger.Propose{Meta: w.meta(), NewMemory: ledger.NewMemory{SpaceID: w.space, Statement: statement,
		Section: ledger.SectionConventions}}).Memory
	w.m[key] = m
	return m
}

func (w *world) note(body, agent string) uuid.UUID {
	id := uuid.New()
	w.exec(`INSERT INTO memories (id, owner_id, hub_id, title, content, created_by_type, created_by_slug, source)
	        VALUES ($1, $2, $3, '', $4, 'agent', $5, 'mcp')`, id, w.owner, w.space, body, agent)
	return id
}

func (w *world) get(id uuid.UUID) *ledger.Memory {
	w.t.Helper()
	m, err := w.l.GetMemory(context.Background(), w.scope(), id.String())
	if err != nil {
		w.t.Fatal(err)
	}
	return m
}

// seed is the fixture every phase has something in.
func (w *world) seed() {
	past := time.Now().Add(-24 * time.Hour)
	w.keep("river", "Background jobs run on River, not Temporal.", nil)
	w.keep("railway", "Deploys go to Railway.", nil)
	w.keep("pnpm", "Use pnpm workspaces only.", func(nm *ledger.NewMemory) {
		nm.Kind, nm.Section, nm.Decision = ledger.KindDecision, ledger.SectionDecisions, &ledger.DecisionFields{Area: "package manager"}
	})
	w.keep("haiku", "Ask memax answers with the Haiku tier.", func(nm *ledger.NewMemory) { nm.StaleAfter = &past })
	w.keep("fly", "Deploys go to Fly.io in iad and ams.", nil)
	w.propose("cards", "Review cards show the diff against the memory they replace.")
	w.propose("cards2", "Review cards show the diff against the memory they replace!")
	w.propose("errors", "API errors are RFC 9457 problem+json.")
	w.propose("errors2", "The API answers errors as problem+json documents.")
	w.note("Talked it over again: background jobs run on River, it handles retries.", "claude-code")
	w.note("Decided with Jiahao: the CLI prints receipts in the same order as the web app.", "codex")
	w.note("lol, the coffee machine is broken again", "cursor")
	w.apply(&ledger.ReviseBrief{Meta: w.meta(), SpaceID: w.space, Title: "memax-v2", Sections: []ledger.BriefSection{
		{Key: "decisions", Heading: "Decisions", Items: []ledger.BriefItem{{Ref: w.m["pnpm"].Ref}}},
		{Key: "conventions", Heading: "Conventions", Items: []ledger.BriefItem{{Ref: w.m["river"].Ref}}},
	}})
}

func oracle() *dreamtest.Oracle {
	return &dreamtest.Oracle{
		Folds: []dreamtest.FoldRule{{Note: "background jobs run on River", Memory: "Background jobs run on River"}},
		Facts: []dreamtest.FactRule{{Notes: []string{"prints receipts"}, Statement: "The CLI prints receipts in the same order as the web app.",
			Section: "conventions"}},
		Pairs: []dreamtest.PairRule{
			{A: "problem+json documents", B: "RFC 9457 problem+json", Relation: "duplicate", Confidence: 0.95},
			{A: "Fly.io in iad", B: "Deploys go to Railway", Relation: "updates", Confidence: 0.92},
		},
		Brief: []dreamtest.BriefRule{
			{Op: "place", Item: "Deploys go to Railway", Section: "conventions", After: "Background jobs"},
			{Op: "add", Section: "conventions", Text: "Jobs and deploys are settled here.", Cites: []string{"Background jobs run on River"}},
			{Op: "add", Section: "conventions", Text: "An uncited claim the ledger must refuse."},
		},
	}
}

func engine(w *world, model v2dream.Model, cfg v2dream.Config, opts ...v2dream.Option) *v2dream.Engine {
	if cfg.Primary.Model == "" {
		cfg.Primary.Model = "fake/primary"
	}
	cfg.Strong.Model, cfg.Log, cfg.ZeroDataRetention = "fake/strong", quiet, true
	opts = append([]v2dream.Option{v2dream.WithSearcher(v2recall.New(w.l))}, opts...)
	return v2dream.New(w.l, model, cfg, opts...)
}

func run(t *testing.T, e *v2dream.Engine, w *world, slot time.Time) v2dream.Outcome {
	t.Helper()
	out, err := e.Run(context.Background(), ledger.DreamSpaceArgs{SpaceID: w.space, Slot: slot, Trigger: ledger.DreamScheduled})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return out
}

// One run over the fixture: every phase acts as labelled, the ledger
// applies it, and the cited Brief change goes in while the uncited one
// doesn't.
func TestRunPublishesAnEdition(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.seed()
	o := oracle()
	e := engine(w, o, v2dream.Config{})
	out := run(t, e, w, time.Now().Add(-time.Minute).Truncate(time.Second))
	if !out.Ran || out.Edition == nil {
		t.Fatalf("no edition: %+v", out)
	}
	ed := out.Edition
	want := map[ledger.DreamActionKind]int{ledger.DreamFold: 1, ledger.DreamPropose: 1, ledger.DreamDedupe: 2,
		ledger.DreamConflict: 1, ledger.DreamStale: 1, ledger.DreamBrief: 1}
	for k, n := range want {
		if ed.Counts[k] != n {
			t.Errorf("%s: %d, want %d (all %v; skipped %v; considered %v)", k, ed.Counts[k], n, ed.Counts, ed.Stats.Skipped, ed.Stats.Considered)
		}
	}
	if ed.NotesRead != 3 || len(ed.FactRefs) != 2 {
		t.Errorf("notes %d facts %v", ed.NotesRead, ed.FactRefs)
	}
	if w.get(w.m["cards2"].ID).Lifecycle != lifecycle.Merged || w.get(w.m["errors2"].ID).Lifecycle != lifecycle.Merged {
		t.Error("the repeats weren't folded before Review")
	}
	if fly := w.get(w.m["fly"].ID); !fly.Flags.Has(lifecycle.Conflict) {
		t.Errorf("the kept facts that disagree weren't flagged: %v", fly.Flags)
	}
	if h := w.get(w.m["haiku"].ID); !h.Flags.Has(lifecycle.Stale) {
		t.Error("the fact past its date wasn't flagged stale")
	}
	brief, err := w.l.GetBrief(context.Background(), w.scope(), w.space)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, s := range brief.Sections {
		for _, it := range s.Items {
			lines = append(lines, it.Ref+it.Text)
		}
	}
	got := strings.Join(lines, "|")
	if !strings.Contains(got, w.m["railway"].Ref) || !strings.Contains(got, "settled here") || strings.Contains(got, "uncited") {
		t.Errorf("the Brief reads %s", got)
	}
	if ed.Stats.Model == nil || ed.Stats.Model.Calls == 0 || !ed.Stats.Model.ZDR {
		t.Errorf("model usage %+v", ed.Stats.Model)
	}
	// Nothing in the edition's own rows holds words.
	if n := w.count(`SELECT count(*) FROM v2.dream_editions WHERE stats::text ILIKE '%receipts%' OR stats::text ILIKE '%River%'`); n != 0 {
		t.Error("the edition holds words")
	}

	// The cost guard: nothing new since, no run and no model call.
	calls := len(o.Calls())
	again := run(t, e, w, time.Now().Truncate(time.Second))
	if again.Ran || again.Reason != v2dream.ReasonNoInput || len(o.Calls()) != calls {
		t.Fatalf("a run with no new input: %+v, %d calls", again, len(o.Calls())-calls)
	}
	// A new note is new input.
	w.note("We agreed: background jobs run on River.", "codex")
	third := run(t, e, w, time.Now().Add(time.Second).Truncate(time.Second))
	if !third.Ran || third.Edition.NotesRead != 1 || third.Edition.Counts[ledger.DreamFold] != 1 {
		t.Errorf("the next note: %+v", third.Edition)
	}
}

// Fading: 60 days unread; never a decision in force, a memory the Brief
// places, a flagged one, or one in a file whose loads Memax can't observe.
func TestRunFades(t *testing.T) {
	t.Parallel()
	later := time.Now().Add(61 * 24 * time.Hour)
	w := newWorld(t, ledger.WithClock(func() time.Time { return later }))
	w.seed()
	e := engine(w, nil, v2dream.Config{}, v2dream.WithClock(func() time.Time { return later }))
	out := run(t, e, w, later.Truncate(time.Second))
	if !out.Ran {
		t.Fatalf("%+v", out)
	}
	faded := map[string]bool{}
	for k, m := range w.m {
		faded[k] = w.get(m.ID).Lifecycle == lifecycle.Faded
	}
	if !faded["railway"] || !faded["fly"] || faded["river"] || faded["pnpm"] || faded["haiku"] {
		t.Errorf("faded %v: want railway and fly only (river is in the Brief, pnpm a decision, haiku went stale)", faded)
	}
	// Without a model, nothing that needs one ran.
	if out.Edition.Counts[ledger.DreamFold]+out.Edition.Counts[ledger.DreamPropose]+out.Edition.Counts[ledger.DreamConflict] != 0 {
		t.Errorf("a modelless run folded or flagged: %v", out.Edition.Counts)
	}
	if out.Edition.Counts[ledger.DreamDedupe] != 1 {
		t.Errorf("the exact repeat wasn't folded without a model: %v", out.Edition.Counts)
	}
}

// The sweep queues a space once per night, catches up a missed one once,
// and follows its owner's zone.
func TestSweepIsIdempotentAndCatchesUp(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	ctx := context.Background()
	client := w.riverClient()
	e := engine(w, nil, v2dream.Config{})
	queued, err := e.Sweep(ctx, client)
	if err != nil || len(queued) != 0 {
		t.Fatalf("first sweep: %v %v", queued, err)
	}
	if n := w.count(`SELECT count(*) FROM v2.dream_schedules WHERE space_id = $1 AND time_zone = 'UTC'`, w.space); n != 1 {
		t.Fatalf("schedules %d", n)
	}
	// The owner's app reports Vancouver: the schedule moves to its night.
	if err := w.l.ObserveTimeZone(ctx, w.scope(), "America/Vancouver"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Sweep(ctx, client); err != nil {
		t.Fatal(err)
	}
	var due time.Time
	if err := w.pool.QueryRow(ctx, `SELECT due_at FROM v2.dream_schedules WHERE space_id = $1 AND time_zone = 'America/Vancouver'`, w.space).Scan(&due); err != nil {
		t.Fatalf("schedule after the zone: %v", err)
	}
	if local := due.In(mustZone("America/Vancouver")); local.Hour() != 3 {
		t.Errorf("due at %s, not 03:00 in Vancouver", local)
	}
	// Three nights missed: one run, for the latest; then nothing.
	w.exec(`UPDATE v2.dream_schedules SET due_at = now() - interval '3 days' WHERE space_id = $1`, w.space)
	for i := 0; i < 3; i++ {
		if _, err := e.Sweep(ctx, client); err != nil {
			t.Fatal(err)
		}
	}
	if n := w.count(`SELECT count(*) FROM river_job WHERE kind = 'dream_space' AND args->>'space_id' = $1`, w.space.String()); n != 1 {
		t.Errorf("%d runs queued, want 1", n)
	}
	// Concurrent sweeps queue once too.
	w.exec(`UPDATE v2.dream_schedules SET due_at = now() - interval '1 day', last_slot = NULL WHERE space_id = $1`, w.space)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() { _, _ = e.Sweep(ctx, client) })
	}
	wg.Wait()
	if n := w.count(`SELECT count(*) FROM river_job WHERE kind = 'dream_space' AND args->>'space_id' = $1`, w.space.String()); n > 2 {
		t.Errorf("%d runs queued after concurrent sweeps", n)
	}
}

func mustZone(z string) *time.Location {
	l, err := time.LoadLocation(z)
	if err != nil {
		panic(err)
	}
	return l
}

// sentMail collects what the mailer sends.
type sentMail struct {
	mu  sync.Mutex
	got []email.Message
}

func (s *sentMail) Send(_ context.Context, m email.Message) (email.SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, m)
	return email.SendResult{MessageID: "msg-" + uuid.NewString()[:8]}, nil
}

// The morning email goes once to each person who may keep, says only what
// they can read, and its one-click link turns it off.
func TestMorningEmail(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.seed()
	ctx := context.Background()
	viewer := w.user("sam")
	w.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'viewer')`, w.space, viewer)
	member := w.user("jy")
	w.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'contributor')`, w.space, member)
	out := run(t, engine(w, oracle(), v2dream.Config{}), w, time.Now().Add(-time.Minute).Truncate(time.Second))
	sent := &sentMail{}
	// Noon in UTC, everyone's zone here: outside the default quiet hours.
	noon := func() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }
	m := v2dream.NewMailer(w.l, sent, v2dream.MailerConfig{AppURL: "https://memax.app", APIURL: "https://api.memax.app",
		Log: quiet, Now: noon})
	for range 2 {
		if wait, err := m.Send(ctx, w.space, out.Edition.ID); err != nil || wait != 0 {
			t.Fatalf("send: wait %v, %v", wait, err)
		}
	}
	if len(sent.got) != 2 {
		t.Fatalf("sent %d emails, want the owner's and the member's, once", len(sent.got))
	}
	msg := sent.got[0]
	if !strings.Contains(msg.Subject, "3 notes became 2 facts") || !strings.Contains(msg.Subject, "need you") {
		t.Errorf("subject %q", msg.Subject)
	}
	for _, want := range []string{"Waiting on you", "Deploys go to Fly.io in iad and ams.", "Start review", "/memax-v2-", "Unsubscribe"} {
		if !strings.Contains(msg.HTML, want) || (want != "Start review" && !strings.Contains(msg.Text, strings.TrimSuffix(want, "."))) {
			t.Errorf("the email lacks %q", want)
		}
	}
	if !strings.HasPrefix(msg.Headers["List-Unsubscribe"], "<https://api.memax.app/v2/dream/email:unsubscribe?token=") ||
		msg.Headers["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" {
		t.Errorf("headers %v", msg.Headers)
	}
	// One click turns it off for that person: the next edition skips them.
	token := strings.TrimSuffix(strings.SplitN(msg.Headers["List-Unsubscribe"], "token=", 2)[1], ">")
	if err := w.l.UnsubscribeDreamEmail(ctx, token); err != nil {
		t.Fatal(err)
	}
	_, recipients, err := w.l.DreamRecipients(ctx, w.space, uuid.New())
	if err != nil || len(recipients) != 1 {
		t.Errorf("after unsubscribing, %d recipients (%v)", len(recipients), err)
	}
	// A forgotten memory's words leave the email too.
	fly := w.get(w.m["fly"].ID)
	w.apply(&ledger.Forget{Meta: w.meta(), Memory: fly.Ref, ExpectedVersion: fly.Version})
	sp, rs, err := w.l.DreamRecipients(ctx, w.space, out.Edition.ID)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := w.l.SpaceScope(ctx, w.space)
	ed, err := w.l.GetEdition(ctx, scope, w.space, out.Edition.Ref)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Render(ed, sp, rs[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(again.HTML, "Fly.io") || strings.Contains(again.Text, "Fly.io") {
		t.Error("a forgotten memory's words are in the email")
	}
}

// The morning email waits for the end of each person's quiet hours, in
// their own zone (notification settings, migration 049): the job snoozes
// until the first ends, then sends to whoever is out of them, once.
func TestMorningEmailWaitsForQuietHours(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.seed()
	ctx := context.Background()
	member := w.user("jy")
	w.exec(`INSERT INTO hub_members (hub_id, user_id, role) VALUES ($1, $2, 'contributor')`, w.space, member)
	out := run(t, engine(w, oracle(), v2dream.Config{}), w, time.Now().Add(-time.Minute).Truncate(time.Second))
	// The owner sleeps in Vancouver with the default quiet hours (20:00 to
	// 08:00); the member is in Shanghai, quiet from 22:00 to 07:30.
	zone := func(s string) *string { return &s }
	if _, err := w.l.UpdateDreamSettings(ctx, ledger.Scope{PersonID: w.owner}, zone("America/Vancouver"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.l.UpdateDreamSettings(ctx, ledger.Scope{PersonID: member}, zone("Asia/Shanghai"), nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.l.UpdateNotificationSettings(ctx, ledger.Scope{PersonID: member},
		ledger.NotificationChange{QuietFrom: zone("22:00"), QuietUntil: zone("07:30")}, 1, "quiet-1"); err != nil {
		t.Fatal(err)
	}
	// 03:12 in Vancouver is 18:12 in Shanghai: only the member is awake.
	clock := time.Date(2026, 10, 5, 10, 12, 0, 0, time.UTC)
	sent := &sentMail{}
	m := v2dream.NewMailer(w.l, sent, v2dream.MailerConfig{Log: quiet, Now: func() time.Time { return clock }})
	wait, err := m.Send(ctx, w.space, out.Edition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent.got) != 1 || wait != 4*time.Hour+48*time.Minute {
		t.Fatalf("sent %d, wait %v; want the member's now and the owner's at 08:00 Vancouver (4h48m)", len(sent.got), wait)
	}
	// At 08:00 in Vancouver it goes to the owner, and to nobody twice.
	clock = clock.Add(wait)
	if wait, err = m.Send(ctx, w.space, out.Edition.ID); err != nil || wait != 0 || len(sent.got) != 2 {
		t.Fatalf("at the end of quiet hours: sent %d, wait %v, %v", len(sent.got), wait, err)
	}
	// Quiet hours off: nothing waits, even at night.
	off := false
	if _, _, err := w.l.UpdateNotificationSettings(ctx, ledger.Scope{PersonID: w.owner},
		ledger.NotificationChange{QuietOn: &off}, 1, "quiet-off"); err != nil {
		t.Fatal(err)
	}
	_, rs, err := w.l.DreamRecipients(ctx, w.space, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.PersonID == w.owner && r.Quiet.Wait(clock.Add(-6*time.Hour), time.UTC) != 0 {
			t.Errorf("quiet hours off still hold the owner's email: %+v", r.Quiet)
		}
	}
}

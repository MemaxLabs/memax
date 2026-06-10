package approver_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	sdkapproval "github.com/MemaxLabs/memax-go-agent-sdk/toolkit/approvaltools"

	"github.com/MemaxLabs/memax/packages/server/internal/agent/approver"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// fakeStore implements the approver's package-private store
// surface (Create/Get/Expire). NOT a full store.Store — that's
// the point: codex's Phase 3.6b2 review flagged the embedded-
// interface trick as weakening compile-time coverage, so this
// fake matches the minimum the approver actually uses. If the
// approver later adds another store call, this fake won't
// satisfy the interface and the build fails — exactly the
// catch we want.
type fakeStore struct {
	mu     sync.Mutex
	rows   map[string]*model.ChatToolApproval // by approval id
	owners map[string]string                  // approval id → owner id

	createErr error
	fetchErr  error
	expireErr error

	// onCreate runs INSIDE CreateChatToolApproval after the row
	// is written but before the call returns. Tests use it to
	// simulate "user decides between INSERT and approver's first
	// read" — i.e., the initial-probe race.
	onCreate func(approvalID string)

	createCount int
	fetchCount  int
	expireCount int

	// fetchObservedCh fires once after the FIRST GetChatTool
	// Approval call returns. Tests use this for deterministic
	// "wait for the initial probe to complete" coordination —
	// avoids the time.Sleep micro-poll patterns codex flagged
	// as scheduler-timing dependent on stalled CI.
	fetchObservedOnce bool
	fetchObservedCh   chan struct{}
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		rows:   map[string]*model.ChatToolApproval{},
		owners: map[string]string{},
	}
}

func (f *fakeStore) CreateChatToolApproval(_ context.Context, ownerID string, ap *model.ChatToolApproval) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCount++
	if f.createErr != nil {
		return f.createErr
	}
	clone := *ap
	f.rows[ap.ID] = &clone
	f.owners[ap.ID] = ownerID
	if f.onCreate != nil {
		hook := f.onCreate
		f.mu.Unlock()
		hook(ap.ID)
		f.mu.Lock()
	}
	return nil
}

func (f *fakeStore) GetChatToolApproval(_ context.Context, approvalID, ownerID string) (*model.ChatToolApproval, error) {
	f.mu.Lock()
	f.fetchCount++
	// Fire fetchObservedCh exactly once, on the first fetch
	// (the approver's initial probe). Defer-unlock pattern
	// works because the channel send is unbuffered + non-
	// blocking via select — we never hold the lock during a
	// receiver-side wait.
	if !f.fetchObservedOnce && f.fetchObservedCh != nil {
		f.fetchObservedOnce = true
		select {
		case f.fetchObservedCh <- struct{}{}:
		default:
		}
	}
	defer f.mu.Unlock()
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	row, ok := f.rows[approvalID]
	if !ok || f.owners[approvalID] != ownerID {
		return nil, store.ErrChatApprovalNotFound
	}
	clone := *row
	return &clone, nil
}

// ExpireChatToolApproval mirrors PostgresStore semantics: CAS
// on status='pending' so a concurrent decide that committed
// first wins. Returns ErrChatApprovalAlreadyDecided if the row
// is non-pending; ErrChatApprovalNotFound if missing/wrong
// owner.
func (f *fakeStore) ExpireChatToolApproval(_ context.Context, approvalID, ownerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expireCount++
	if f.expireErr != nil {
		return f.expireErr
	}
	row, ok := f.rows[approvalID]
	if !ok || f.owners[approvalID] != ownerID {
		return store.ErrChatApprovalNotFound
	}
	if row.Status != model.ChatToolApprovalStatusPending {
		return store.ErrChatApprovalAlreadyDecided
	}
	row.Status = model.ChatToolApprovalStatusTimedOut
	return nil
}

// Decide mutates the row (test-side simulation of the user
// clicking approve/deny). The approver wakes via either the
// poll loop or the broker event — both end up reading via
// GetChatToolApproval and seeing this terminal status.
func (f *fakeStore) Decide(approvalID, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if row, ok := f.rows[approvalID]; ok {
		row.Status = status
	}
}

func (f *fakeStore) singleApprovalID(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rows) != 1 {
		t.Fatalf("singleApprovalID: rows = %d, want 1", len(f.rows))
	}
	for id := range f.rows {
		return id
	}
	return ""
}

// fakeSubscriber implements approver.EventSubscriber with a
// controllable channel. Tests Push events to drive the
// broker-path code in the approver's wait loop.
type fakeSubscriber struct {
	mu       sync.Mutex
	ch       chan events.Event
	canceled bool
	subUser  string // last userID Subscribe was called with
}

func newFakeSubscriber() *fakeSubscriber {
	return &fakeSubscriber{ch: make(chan events.Event, 8)}
}

func (s *fakeSubscriber) Subscribe(userID string) (<-chan events.Event, func()) {
	s.mu.Lock()
	s.subUser = userID
	s.mu.Unlock()
	return s.ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.canceled {
			s.canceled = true
			close(s.ch)
		}
	}
}

// Push delivers an event to the subscriber's channel.
func (s *fakeSubscriber) Push(ev events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.canceled {
		return
	}
	s.ch <- ev
}

// Close prematurely closes the channel without going through the
// cancel func — simulates a broker shutdown / connection drop.
// The approver's wait loop should detect the closed channel and
// fall through to poll-only.
func (s *fakeSubscriber) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.canceled {
		s.canceled = true
		close(s.ch)
	}
}

const (
	testOwner   = "11111111-1111-1111-1111-111111111111"
	testMessage = "33333333-3333-3333-3333-333333333333"
)

func fastConfig(s approver.Config) approver.Config {
	if s.PollInterval == 0 {
		s.PollInterval = 1 * time.Millisecond
	}
	if s.TTL == 0 {
		s.TTL = 90 * time.Second
	}
	return s
}

func sampleRequest() sdkapproval.Request {
	return sdkapproval.Request{
		Action:        "forget_memory",
		Reason:        "user asked to forget the canceled launch notes",
		ToolInput:     []byte(`{"id":"mem_xxx"}`),
		ToolInputHash: "deadbeef",
	}
}

// Happy path: user approves via the poll fallback. No broker —
// the approver hits the 1ms poll cadence, sees the terminal
// status, returns Approved=true with reason="user_approved".
func TestApprover_PollFallback_Approve(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	svc := approver.New(fastConfig(approver.Config{Store: fs}))
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		fs.Decide(fs.singleApprovalID(t), model.ChatToolApprovalStatusApproved)
	}()

	dec, err := app.RequestApproval(ctx, sampleRequest())
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if !dec.Approved {
		t.Errorf("Approved = false, want true")
	}
	if dec.Reason != approver.ReasonUserApproved {
		t.Errorf("Reason = %q, want %q", dec.Reason, approver.ReasonUserApproved)
	}
	if fs.createCount != 1 {
		t.Errorf("createCount = %d, want 1", fs.createCount)
	}
}

func TestApprover_PollFallback_Deny(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	svc := approver.New(fastConfig(approver.Config{Store: fs}))
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		fs.Decide(fs.singleApprovalID(t), model.ChatToolApprovalStatusDenied)
	}()

	dec, err := app.RequestApproval(ctx, sampleRequest())
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if dec.Approved {
		t.Errorf("Approved = true, want false")
	}
	if dec.Reason != approver.ReasonUserDenied {
		t.Errorf("Reason = %q, want %q", dec.Reason, approver.ReasonUserDenied)
	}
}

// Sweeper-flipped path: row turns timed_out before the local
// deadline (e.g., a sweeper from a prior test cycle ran).
func TestApprover_PollFallback_TimedOut(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	svc := approver.New(fastConfig(approver.Config{Store: fs}))
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		fs.Decide(fs.singleApprovalID(t), model.ChatToolApprovalStatusTimedOut)
	}()

	dec, err := app.RequestApproval(ctx, sampleRequest())
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if dec.Approved {
		t.Errorf("Approved = true, want false")
	}
	if dec.Reason != approver.ReasonTimedOut {
		t.Errorf("Reason = %q, want %q", dec.Reason, approver.ReasonTimedOut)
	}
}

// Initial-probe race: the user decides BEFORE the approver's
// first poll-tick fires.
func TestApprover_InitialProbeCatchesPreDecidedRow(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	fs.onCreate = func(approvalID string) {
		fs.Decide(approvalID, model.ChatToolApprovalStatusApproved)
	}

	svc := approver.New(approver.Config{
		Store:        fs,
		PollInterval: 1 * time.Hour, // basically never fires
		TTL:          90 * time.Second,
	})
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dec, err := app.RequestApproval(ctx, sampleRequest())
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if !dec.Approved {
		t.Errorf("Approved = false, want true")
	}
	if dec.Reason != approver.ReasonUserApproved {
		t.Errorf("Reason = %q, want %q", dec.Reason, approver.ReasonUserApproved)
	}
}

// ctx cancel: the SDK cancels the run mid-wait → RequestApproval
// returns ctx.Err(). The row stays pending; the timeout sweeper
// reaps it later. No spurious decision.
func TestApprover_CtxCancel(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	svc := approver.New(fastConfig(approver.Config{Store: fs}))
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	dec, err := app.RequestApproval(ctx, sampleRequest())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if dec.Approved {
		t.Errorf("Approved = true, want false")
	}
	row := fs.rows[fs.singleApprovalID(t)]
	if row.Status != model.ChatToolApprovalStatusPending {
		t.Errorf("row status = %q, want pending (ctx-cancel must not flip the row)", row.Status)
	}
}

// Create error propagates as a wrapped error.
func TestApprover_CreateErrorPropagates(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	fs.createErr = store.ErrChatMessageNotFound
	svc := approver.New(fastConfig(approver.Config{Store: fs}))
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	dec, err := app.RequestApproval(ctx, sampleRequest())
	if err == nil {
		t.Fatalf("err = nil, want create error")
	}
	if !errors.Is(err, store.ErrChatMessageNotFound) {
		t.Errorf("err = %v, want wrapped ErrChatMessageNotFound", err)
	}
	if dec.Approved {
		t.Errorf("Approved = true; create error must produce zero Decision")
	}
}

// Empty-input shape: SDK requests with no ToolInput / ToolInputHash
// still produce a valid row. The approver synthesizes "{}" args
// + computes its own sha256 hash.
func TestApprover_EmptyInputDefaults(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	svc := approver.New(fastConfig(approver.Config{Store: fs}))
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		fs.Decide(fs.singleApprovalID(t), model.ChatToolApprovalStatusApproved)
	}()

	req := sampleRequest()
	req.ToolInput = nil
	req.ToolInputHash = ""
	if _, err := app.RequestApproval(ctx, req); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	row := fs.rows[fs.singleApprovalID(t)]
	if string(row.Args) != `{}` {
		t.Errorf("Args = %q, want %q (jsonb empty default)", string(row.Args), `{}`)
	}
	if row.ArgsHash == "" {
		t.Errorf("ArgsHash empty; want a synthesized sha256")
	}
	if len(row.ArgsHash) != 64 {
		t.Errorf("ArgsHash length = %d, want 64 (sha256 hex)", len(row.ArgsHash))
	}
}

// ForTurn with empty owner or messageID is a wiring bug; the
// approver returns a non-nil error.
func TestApprover_ForTurn_RejectsEmptyIdentity(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	svc := approver.New(fastConfig(approver.Config{Store: fs}))

	ctx := context.Background()

	for name, args := range map[string][2]string{
		"empty owner":   {"", testMessage},
		"empty message": {testOwner, ""},
		"both empty":    {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			app := svc.ForTurn(approver.TurnConfig{OwnerID: args[0], MessageID: args[1]})
			_, err := app.RequestApproval(ctx, sampleRequest())
			if err == nil {
				t.Errorf("err = nil; want validation error for %q", name)
			}
		})
	}
}

// ttl is honored: the row's expires_at is RequestedAt + TTL.
func TestApprover_TTL_Honored(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	svc := approver.New(approver.Config{
		Store:        fs,
		PollInterval: 1 * time.Millisecond,
		TTL:          7 * time.Second, // unusual value to prove non-default
	})
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	// Use a ctx WITHOUT a deadline so the TTL clamp never
	// activates; this test is specifically about the
	// configured TTL being applied. The deadline-clamp
	// behavior is exercised by
	// TestApprover_TTL_ClampedToCtxDeadline.
	ctx := context.Background()
	go func() {
		time.Sleep(20 * time.Millisecond)
		fs.Decide(fs.singleApprovalID(t), model.ChatToolApprovalStatusApproved)
	}()
	if _, err := app.RequestApproval(ctx, sampleRequest()); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	row := fs.rows[fs.singleApprovalID(t)]
	gap := row.ExpiresAt.Sub(row.RequestedAt)
	if gap != 7*time.Second {
		t.Errorf("expires_at - requested_at = %s, want 7s", gap)
	}
}

// Phase 3.6b3c codex follow-up: when ctx has a deadline, the
// approver clamps expires_at to deadline - PostDecisionGrace
// so the row can never outlive the run. Without this, a 90s
// TTL inside a 120s run could leave a "valid"-looking
// approval prompt after the run already failed with
// ctx-deadline-exceeded.
func TestApprover_TTL_ClampedToCtxDeadline(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	svc := approver.New(approver.Config{
		Store:             fs,
		PollInterval:      1 * time.Millisecond,
		TTL:               90 * time.Second,
		PostDecisionGrace: 250 * time.Millisecond,
	})
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	// 5s ctx — far less than the 90s TTL.
	deadline := time.Now().Add(5 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	go func() {
		time.Sleep(20 * time.Millisecond)
		fs.Decide(fs.singleApprovalID(t), model.ChatToolApprovalStatusApproved)
	}()
	if _, err := app.RequestApproval(ctx, sampleRequest()); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	row := fs.rows[fs.singleApprovalID(t)]
	// Expected: ExpiresAt ≈ deadline - 250ms.
	wantUpper := deadline.Add(-250 * time.Millisecond)
	if row.ExpiresAt.After(wantUpper.Add(50 * time.Millisecond)) {
		t.Errorf("ExpiresAt = %v, want ≤ deadline-grace (%v) — clamp didn't fire", row.ExpiresAt, wantUpper)
	}
	if row.ExpiresAt.Before(wantUpper.Add(-50 * time.Millisecond)) {
		t.Errorf("ExpiresAt = %v, want ≈ deadline-grace (%v)", row.ExpiresAt, wantUpper)
	}
}

// When the ctx deadline is so close that even after grace
// there's no time left, RequestApproval fails fast rather than
// creating a row that's effectively pre-expired.
func TestApprover_RejectsApprovalWhenCtxNearDeadline(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	svc := approver.New(approver.Config{
		Store:             fs,
		PollInterval:      1 * time.Millisecond,
		TTL:               90 * time.Second,
		PostDecisionGrace: 250 * time.Millisecond,
	})
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	// 100ms ctx — less than the 250ms grace.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	dec, err := app.RequestApproval(ctx, sampleRequest())
	if err == nil {
		t.Fatal("err = nil; want failure for too-close ctx deadline")
	}
	if dec.Approved {
		t.Errorf("Approved = true, want false")
	}
	// Row was NEVER created — the fail-fast happens before
	// CreateChatToolApproval.
	if fs.createCount != 0 {
		t.Errorf("createCount = %d, want 0 (must not create a doomed row)", fs.createCount)
	}
}

func TestApprover_NewDefaults(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	svc := approver.New(approver.Config{Store: fs})
	if svc == nil {
		t.Fatal("New returned nil")
	}
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})
	if app == nil {
		t.Fatal("ForTurn returned nil")
	}
}

func TestApprover_NewPanicsWithoutStore(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Errorf("New(nil store) did not panic")
		}
	}()
	approver.New(approver.Config{})
}

// Fetch error propagates with the original wrapped via %w —
// codex v2 review noted the prior assertion just checked
// non-nil/non-empty, which would have passed even if the
// approver had silently swallowed the underlying error.
// errors.Is keeps the wrapping contract pinned.
func TestApprover_FetchErrorPropagates(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	sentinel := errors.New("synthetic fetch failure")
	fs.fetchErr = sentinel
	svc := approver.New(fastConfig(approver.Config{Store: fs}))
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, err := app.RequestApproval(ctx, sampleRequest())
	if err == nil {
		t.Fatalf("err = nil, want fetch failure")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want errors.Is = sentinel (proves %%w wrap)", err)
	}
}

// Bounded fetch count: codex flagged the original ≤6 bound as
// timing-sensitive under CI load. Loosened to ≤10 with the same
// "no busy-loop" guarantee — a regression that dropped the
// ticker would still explode well past 10.
func TestApprover_FetchCount_Bounded(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	svc := approver.New(approver.Config{
		Store:        fs,
		PollInterval: 50 * time.Millisecond,
		TTL:          90 * time.Second,
	})
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		time.Sleep(120 * time.Millisecond)
		fs.Decide(fs.singleApprovalID(t), model.ChatToolApprovalStatusApproved)
	}()
	if _, err := app.RequestApproval(ctx, sampleRequest()); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if fs.fetchCount > 10 {
		t.Errorf("fetchCount = %d, want ≤10 (initial probe + a few ticks; busy-loop regression catch)", fs.fetchCount)
	}
	if fs.fetchCount < 1 {
		t.Errorf("fetchCount = %d, want ≥1", fs.fetchCount)
	}
}

// --- Phase 3.6b2 v2 follow-up: broker-path tests ---

// Broker delivery: a matching approval.decided event wakes the
// approver fast (no need to wait for a poll tick). Pins the
// dual-signal contract — the broker is the fast path, polling
// is the safety net. Codex flagged this gap on v1 review.
func TestApprover_Broker_MatchingApprovalWakesImmediately(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	sub := newFakeSubscriber()
	svc := approver.New(approver.Config{
		Store:        fs,
		Subscriber:   sub,
		PollInterval: 1 * time.Hour, // proves the broker path is what woke us
		TTL:          90 * time.Second,
	})
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		// Wait for the row to land (so we have an approvalID),
		// flip the row, then push the broker event with the
		// matching EntityID.
		time.Sleep(20 * time.Millisecond)
		id := fs.singleApprovalID(t)
		fs.Decide(id, model.ChatToolApprovalStatusApproved)
		sub.Push(events.Event{
			Type:            events.EventTypeApprovalDecided,
			Audience:        "user",
			RecipientUserID: testOwner,
			EntityID:        id,
			State:           model.ChatToolApprovalStatusApproved,
		})
	}()

	dec, err := app.RequestApproval(ctx, sampleRequest())
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if !dec.Approved {
		t.Errorf("Approved = false, want true")
	}
	// Subscribe was called with the right userID.
	if sub.subUser != testOwner {
		t.Errorf("Subscribe userID = %q, want %q", sub.subUser, testOwner)
	}
}

// Broker delivery, unrelated event: an approval.decided for a
// DIFFERENT EntityID must not wake this approver. Pins the
// per-approval filtering inside the wait loop.
func TestApprover_Broker_UnrelatedEventIgnored(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	sub := newFakeSubscriber()
	svc := approver.New(approver.Config{
		Store:        fs,
		Subscriber:   sub,
		PollInterval: 5 * time.Millisecond,
		TTL:          90 * time.Second,
	})
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		time.Sleep(20 * time.Millisecond)
		// Push a noise event for a DIFFERENT approval.
		sub.Push(events.Event{
			Type:            events.EventTypeApprovalDecided,
			Audience:        "user",
			RecipientUserID: testOwner,
			EntityID:        "some-other-approval-id",
			State:           "approved",
		})
		// Push another noise event of a different type.
		sub.Push(events.Event{
			Type:            "agent.changed",
			Audience:        "user",
			RecipientUserID: testOwner,
		})
		// THEN flip our row — the poll loop should pick this up.
		time.Sleep(20 * time.Millisecond)
		fs.Decide(fs.singleApprovalID(t), model.ChatToolApprovalStatusApproved)
	}()

	dec, err := app.RequestApproval(ctx, sampleRequest())
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if !dec.Approved {
		t.Errorf("Approved = false, want true (poll fallback)")
	}
}

// Broker subscription closed mid-wait: the approver detects the
// closed channel and falls through to poll-only. The decide
// still resolves via the poll path.
func TestApprover_Broker_ClosedSubscriptionFallsBackToPolling(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	sub := newFakeSubscriber()
	svc := approver.New(approver.Config{
		Store:        fs,
		Subscriber:   sub,
		PollInterval: 5 * time.Millisecond,
		TTL:          90 * time.Second,
	})
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() {
		time.Sleep(20 * time.Millisecond)
		// Close the subscription before the decide commits.
		sub.Close()
		time.Sleep(20 * time.Millisecond)
		fs.Decide(fs.singleApprovalID(t), model.ChatToolApprovalStatusApproved)
	}()

	dec, err := app.RequestApproval(ctx, sampleRequest())
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if !dec.Approved {
		t.Errorf("Approved = false, want true (poll fallback)")
	}
}

// --- Phase 3.6b2 v2 follow-up: deadline tests ---

// Local deadline fires: the approver self-flips the row to
// timed_out and returns reason=timed_out. Codex flagged that
// without this, an unanswered 90s approval could linger
// pending until the 1m sweeper ran (up to 60s of dead time)
// while the agent run's 120s ctx cancelled first.
func TestApprover_Deadline_FlipsRowAndReturnsTimedOut(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	svc := approver.New(approver.Config{
		Store:        fs,
		PollInterval: 50 * time.Millisecond,
		TTL:          50 * time.Millisecond, // tiny TTL so the test deadline fires fast
	})
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dec, err := app.RequestApproval(ctx, sampleRequest())
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if dec.Approved {
		t.Errorf("Approved = true, want false")
	}
	if dec.Reason != approver.ReasonTimedOut {
		t.Errorf("Reason = %q, want %q", dec.Reason, approver.ReasonTimedOut)
	}
	row := fs.rows[fs.singleApprovalID(t)]
	if row.Status != model.ChatToolApprovalStatusTimedOut {
		t.Errorf("row.Status = %q, want timed_out (approver should self-flip)", row.Status)
	}
	if fs.expireCount != 1 {
		t.Errorf("expireCount = %d, want 1 (approver should hit ExpireChatToolApproval at the boundary)", fs.expireCount)
	}
}

// Deadline race: the user clicks approve AFTER the initial
// probe but BEFORE the approver's deadline fires. The store-side
// CAS in ExpireChatToolApproval rejects the approver's flip
// attempt with AlreadyDecided; the approver falls through to
// re-fetch the row and returns the user's decision.
//
// Codex's Phase 3.6b2 v2 review caught that the v1 shape (using
// onCreate) actually resolved via the initial probe, never
// hitting the deadline branch. v2 used a fetchCount poll loop
// which v3 codex flagged as scheduler-timing-dependent (a
// stalled CI scheduler could miss the 50ms TTL window). v3
// uses a channel hook (fetchObservedCh) that fires SYNCHRONOUSLY
// from inside fakeStore.GetChatToolApproval the first time it's
// called — fully deterministic regardless of scheduler load:
//
//  1. Row is created with status=pending. Approver's initial
//     probe calls GetChatToolApproval → fetchObservedCh fires.
//  2. The waiting goroutine receives the signal and
//     synchronously flips the row to approved.
//  3. Deadline timer fires at TTL=50ms.
//  4. Approver calls ExpireChatToolApproval → fakeStore sees
//     status != pending, returns ErrChatApprovalAlreadyDecided.
//  5. Approver re-fetches → status=approved → returns
//     Approved=true.
//
// The expireCount=1 assertion proves the deadline branch was
// hit (vs the initial-probe branch which doesn't call Expire).
func TestApprover_Deadline_UserWinsAtBoundary(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	// Buffer of 1 so the fakeStore's non-blocking send always
	// succeeds; the receiver goroutine processes it whenever
	// the runtime schedules it.
	fs.fetchObservedCh = make(chan struct{}, 1)
	svc := approver.New(approver.Config{
		Store:        fs,
		PollInterval: 1 * time.Hour,         // never fires
		TTL:          50 * time.Millisecond, // forces deadline path quickly
	})
	app := svc.ForTurn(approver.TurnConfig{OwnerID: testOwner, MessageID: testMessage})

	// Goroutine waits on the fetch hook (fires after the
	// initial probe returns), then flips the row. No sleeps,
	// no scheduler races.
	go func() {
		<-fs.fetchObservedCh
		fs.Decide(fs.singleApprovalID(t), model.ChatToolApprovalStatusApproved)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dec, err := app.RequestApproval(ctx, sampleRequest())
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if !dec.Approved {
		t.Errorf("Approved = false, want true (user wins the race)")
	}
	if dec.Reason != approver.ReasonUserApproved {
		t.Errorf("Reason = %q, want %q", dec.Reason, approver.ReasonUserApproved)
	}
	// Critical: the deadline branch ran. expireCount=1 proves
	// the approver called ExpireChatToolApproval (deadline
	// fired) AND the call returned AlreadyDecided. If this
	// assertion ever returns 0, the test silently regressed
	// to the initial-probe path again.
	if fs.expireCount != 1 {
		t.Errorf("expireCount = %d, want 1 (deadline branch must run; if 0, test resolved via initial probe like the v1 shape codex caught)", fs.expireCount)
	}
	row := fs.rows[fs.singleApprovalID(t)]
	if row.Status != model.ChatToolApprovalStatusApproved {
		t.Errorf("row.Status = %q, want approved (user's decision must stand)", row.Status)
	}
}

// fakeEmitter implements approver.ApprovalEventEmitter and
// records every call. Tests use it to assert that the per-turn
// emitter is fired with the expected payload (incl. the SDK
// Request fields propagated via Phase 3.6b3a v2).
type fakeEmitter struct {
	mu     sync.Mutex
	events []approver.ApprovalRequiredEvent
	err    error
}

func (e *fakeEmitter) EmitApprovalRequired(_ context.Context, evt approver.ApprovalRequiredEvent) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, evt)
	return e.err
}

// Phase 3.6b3a v2 — emitter is invoked once per RequestApproval
// with the row's identity AND the SDK Request's prompt fields
// (Reason, Details, Risk, Summary, Metadata). Codex flagged
// the v1 omission of these; without them the UI prompt would be
// just "agent wants to call X with Y args".
func TestApprover_Emitter_FiresWithRequestFields(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	em := &fakeEmitter{}
	svc := approver.New(fastConfig(approver.Config{Store: fs}))
	app := svc.ForTurn(approver.TurnConfig{
		OwnerID:   testOwner,
		MessageID: testMessage,
		Emitter:   em,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		fs.Decide(fs.singleApprovalID(t), model.ChatToolApprovalStatusApproved)
	}()

	req := sampleRequest()
	req.Details = "delete memory mem_xxx because user asked"
	req.Risk = "low"
	req.Summary = sdkapproval.Summary{
		Title:       "Forget canceled launch notes",
		Description: "Remove the agent-captured memory about the canceled launch.",
		Risk:        "low",
		Paths:       []string{"mem_xxx"},
	}
	req.Metadata = map[string]any{"source": "agent"}
	if _, err := app.RequestApproval(ctx, req); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}

	em.mu.Lock()
	defer em.mu.Unlock()
	if len(em.events) != 1 {
		t.Fatalf("emitter saw %d events, want 1", len(em.events))
	}
	got := em.events[0]
	if got.OwnerID != testOwner {
		t.Errorf("OwnerID = %q, want %q", got.OwnerID, testOwner)
	}
	if got.MessageID != testMessage {
		t.Errorf("MessageID = %q, want %q", got.MessageID, testMessage)
	}
	if got.ToolName != req.Action {
		t.Errorf("ToolName = %q, want %q", got.ToolName, req.Action)
	}
	if got.Reason != req.Reason {
		t.Errorf("Reason = %q, want %q", got.Reason, req.Reason)
	}
	if got.Details != req.Details {
		t.Errorf("Details = %q, want %q", got.Details, req.Details)
	}
	if got.Risk != req.Risk {
		t.Errorf("Risk = %q, want %q", got.Risk, req.Risk)
	}
	if got.Summary.Title != req.Summary.Title {
		t.Errorf("Summary.Title = %q, want %q", got.Summary.Title, req.Summary.Title)
	}
	if v, ok := got.Metadata["source"]; !ok || v != "agent" {
		t.Errorf("Metadata[source] = %v, want %q", v, "agent")
	}
}

// Phase 3.6b3a v2 — emitter failure → fail closed: the approver
// CAS-flips the row to timed_out, returns the emit error,
// agent.Run terminates with a clear failure rather than
// blocking 90s on an invisible prompt. Codex flagged this on
// v1 (warn-and-wait was an invisible-hang risk).
func TestApprover_Emitter_FailFailsClosed(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	emitErr := errors.New("synthetic emit failure")
	em := &fakeEmitter{err: emitErr}
	svc := approver.New(fastConfig(approver.Config{Store: fs}))
	app := svc.ForTurn(approver.TurnConfig{
		OwnerID:   testOwner,
		MessageID: testMessage,
		Emitter:   em,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dec, err := app.RequestApproval(ctx, sampleRequest())
	if err == nil {
		t.Fatalf("err = nil, want emit failure")
	}
	if !errors.Is(err, emitErr) {
		t.Errorf("err = %v, want errors.Is = emitErr (proves %%w wrap)", err)
	}
	if dec.Approved {
		t.Errorf("Approved = true, want false (fail-closed must produce zero Decision)")
	}
	// Row was rolled to timed_out (best-effort cleanup so
	// the sweeper doesn't have to reap it).
	row := fs.rows[fs.singleApprovalID(t)]
	if row.Status != model.ChatToolApprovalStatusTimedOut {
		t.Errorf("row.Status = %q, want timed_out (fail-closed must roll the row)", row.Status)
	}
	// expireCount = 1: the cleanup CAS ran exactly once.
	if fs.expireCount != 1 {
		t.Errorf("expireCount = %d, want 1", fs.expireCount)
	}
}

// Nil emitter is still allowed (unit tests + the wiring-only
// flow before mutating tools land). The row is created and
// the wait loop runs as before.
func TestApprover_NilEmitter_StillWaits(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	svc := approver.New(fastConfig(approver.Config{Store: fs}))
	app := svc.ForTurn(approver.TurnConfig{
		OwnerID:   testOwner,
		MessageID: testMessage,
		// Emitter intentionally nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		fs.Decide(fs.singleApprovalID(t), model.ChatToolApprovalStatusApproved)
	}()
	dec, err := app.RequestApproval(ctx, sampleRequest())
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if !dec.Approved {
		t.Errorf("Approved = false, want true (nil emitter must not block resolution)")
	}
}

// BrokerSubscriber wraps a real *events.Broker. Smoke that the
// helper handles a nil broker (returns nil) so wiring code can
// pass `BrokerSubscriber(broker)` unconditionally.
func TestApprover_BrokerSubscriber_NilBrokerIsNil(t *testing.T) {
	t.Parallel()
	if got := approver.BrokerSubscriber(nil); got != nil {
		t.Errorf("BrokerSubscriber(nil) = %v, want nil", got)
	}
}

// Smoke: the SDK Approver shape is satisfied. If anyone changes
// the SDK's interface (Decision shape, RequestApproval signature),
// this won't compile — early failure rather than runtime panic.
var _ sdkapproval.Approver = (*serviceWrapper)(nil)

type serviceWrapper struct{ a sdkapproval.Approver }

func (w *serviceWrapper) RequestApproval(ctx context.Context, req sdkapproval.Request) (sdkapproval.Decision, error) {
	if w.a == nil {
		return sdkapproval.Decision{}, fmt.Errorf("nil approver")
	}
	return w.a.RequestApproval(ctx, req)
}

func TestFakeStore_CrossOwnerReadsAsNotFound(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	id := uuid.NewString()
	if err := fs.CreateChatToolApproval(context.Background(), testOwner,
		&model.ChatToolApproval{ID: id, MessageID: testMessage, Status: model.ChatToolApprovalStatusPending}); err != nil {
		t.Fatalf("CreateChatToolApproval: %v", err)
	}
	if _, err := fs.GetChatToolApproval(context.Background(), id, "not-the-owner"); !errors.Is(err, store.ErrChatApprovalNotFound) {
		t.Errorf("cross-owner err = %v, want ErrChatApprovalNotFound", err)
	}
}

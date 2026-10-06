// Package ask answers a person's question from a space's kept memories:
// ⌘K Ask and `memax ask` on the V2 record (plan 25 §5.11, Phase 1 epic
// 1.10). It retrieves through internal/v2recall, the same hybrid search
// recall and memax_search use, and synthesises a short answer with the
// answer-tier model, streamed, every sentence cited to the memories it
// rests on.
//
// What it guarantees:
//
//   - Only kept memories in the one space are read, inside the person's
//     scope (ledger.Read, so RLS holds). Superseded decisions never come
//     back (v2recall leaves them out). Quarantined memories (external
//     trust) are left out too: a model can be steered by words in its
//     context, and a citation check can't tell a claim that rests on a
//     trusted memory from one an external memory planted; keeping such an
//     answer would launder external words into a person-trusted memory.
//   - Every citation names a memory the model was given; anything else is
//     removed before the person sees it (cite.go). An answer with no
//     citation left is unsupported and isn't shown as an answer.
//   - When the memories don't cover the question, the answer says so (the
//     NOT_COVERED sentinel, or no memory matched at all) instead of guessing.
//   - An Ask writes no record row and no receipt. It counts on the Ask
//     meter (ledger.CountAsk) only once the model is asked, and policy.Decide
//     holds the plan's monthly limit (D9).
//   - Cancelling the context (the person closed Ask) cancels the model call.
//
// Nil means disabled: New returns nil without a ledger or a searcher, and
// a nil Model turns synthesis off, so Ask answers with the matching
// memories alone ("sources_only").
package ask

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

// Searcher is the part of v2recall.Searcher Ask uses.
type Searcher interface {
	Search(ctx context.Context, scope ledger.Scope, q v2recall.Query) (v2recall.Result, error)
}

// Plans says a person's plan's monthly Ask limit in a space (0: none).
// Until V2 billing exists (Phase 3.4), the API passes a fixed limit
// (Config.MonthlyLimit), which is none during the free alpha.
type Plans interface {
	AskLimit(ctx context.Context, person uuid.UUID, space ledger.SpaceGrant) int
}

// FixedLimit is the same limit for everyone.
type FixedLimit int

// AskLimit implements Plans.
func (l FixedLimit) AskLimit(context.Context, uuid.UUID, ledger.SpaceGrant) int { return int(l) }

// Answered is what an Ask did, for an Observer: ids and counts, never the
// question's or the answer's words.
type Answered struct {
	SpaceID   uuid.UUID
	PersonID  uuid.UUID
	Outcome   Outcome
	Retrieved []uuid.UUID
	Cited     []uuid.UUID
	At        time.Time
}

// Observer hears every finished Ask, off the request path; it must not
// block. It is the seam for R- reads (plan §5.3), deliberately unwired: R-
// reads are what agents read (migration 038). They feed "read by", the
// north star (distinct agents a week), an agent's reads_7d and fading,
// and /v2 already leaves a person's own reads out (recordRead counts
// agents only). An Ask is a person reading, so counting it would blur
// those agent signals and keep a per-person trail of what someone asked
// about. Should people's asks come to count (to keep the memories they
// rely on from fading, say), an Observer that turns Cited into a
// ledger.ReadEvent (Reader person, a new ReadKind "ask", via web: both
// need adding to the spec, ReadKinds, ReadVias and the reads CHECKs)
// plugs in here with WithObserver.
type Observer interface {
	Answered(Answered)
}

// Outcome is how an Ask ended.
type Outcome string

// The outcomes.
const (
	// OutcomeAnswered: an answer with at least one citation.
	OutcomeAnswered Outcome = "answered"
	// OutcomeNotCovered: nothing kept matched, or the model said the
	// memories don't answer it.
	OutcomeNotCovered Outcome = "not_covered"
	// OutcomeUnsupported: the model answered, but none of its citations
	// named a memory it was given. Clients don't show it as an answer.
	OutcomeUnsupported Outcome = "unsupported"
	// OutcomeSourcesOnly: synthesis is off; the sources are the answer.
	OutcomeSourcesOnly Outcome = "sources_only"
)

// Outcomes lists every outcome.
var Outcomes = []Outcome{OutcomeAnswered, OutcomeNotCovered, OutcomeUnsupported, OutcomeSourcesOnly}

// MaxQuestionRunes bounds a question.
const MaxQuestionRunes = 2000

// Service answers questions.
type Service struct {
	ledger   *ledger.Ledger
	search   Searcher
	model    Model
	cfg      Config
	plans    Plans
	observer Observer
	now      func() time.Time
	log      *slog.Logger
}

// Option configures a Service.
type Option func(*Service)

// WithPlans replaces the fixed monthly limit with a plan lookup.
func WithPlans(p Plans) Option { return func(s *Service) { s.plans = p } }

// WithObserver sets the observer (nil: none).
func WithObserver(o Observer) Option { return func(s *Service) { s.observer = o } }

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// New returns the service, or nil without a ledger or a searcher. A nil
// model, or an empty cfg.Model, answers with memories only.
func New(l *ledger.Ledger, search Searcher, model Model, cfg Config, opts ...Option) *Service {
	if l == nil || search == nil {
		return nil
	}
	if v, ok := search.(*v2recall.Searcher); ok && v == nil {
		return nil
	}
	cfg = cfg.withDefaults()
	if cfg.Model == "" {
		model = nil
	}
	s := &Service{ledger: l, search: search, model: model, cfg: cfg, plans: FixedLimit(cfg.MonthlyLimit),
		now: time.Now, log: cfg.Log}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Synthesises reports whether answers are written by the model (false:
// sources only).
func (s *Service) Synthesises() bool { return s != nil && s.model != nil }

// Request is one question.
type Request struct {
	// Actor is who asks; only a signed-in person may (policy.Decide).
	Actor ledger.Actor
	Via   policy.Via
	// Scope is the asker's whole scope; the space must be in it.
	Scope     ledger.Scope
	Space     ledger.SpaceGrant
	SpaceName string
	Question  string
	// Uncounted leaves the meter alone: an operator's impersonated
	// session reads, but doesn't spend the person's asks.
	Uncounted bool
	// Started is when the request arrived (first-token latency counts
	// from here); zero means now.
	Started time.Time
}

// Source is one kept memory the answer may cite.
type Source struct {
	ID        uuid.UUID
	Ref       string
	Statement string
	Section   ledger.Section
	Kind      ledger.Kind
	State     string
	Trust     policy.Trust
	Version   int
	// Receipt is how it came to read as it does: who kept it, when.
	Receipt *ledger.Receipt
}

// Refusal is policy saying no (403 refused). Used and Limit are set for
// the plan limit.
type Refusal struct {
	Decision policy.Decision
	Limit    int
	ResetsAt time.Time
}

func (r *Refusal) Error() string { return r.Decision.Message }

// ErrSearchTimeout is the search running out of its budget.
var ErrSearchTimeout = errors.New("ask: the search took too long")

// Prepared is a question ready to answer: decided, counted and retrieved.
type Prepared struct {
	req         Request
	sources     []Source
	lexicalOnly bool
	counted     bool
	countedAt   time.Time
	retrievalMS int64
}

// Sources are the memories the answer may cite, best match first.
func (p *Prepared) Sources() []Source { return p.sources }

func (s *Service) policyActor(r Request) policy.Actor {
	return policy.Actor{Kind: r.Actor.Kind, Name: r.Actor.Name, Role: r.Space.Role, CanForget: r.Space.CanForget,
		Credential: r.Actor.Credential, Via: r.Via}
}

// Prepare decides the question (who may ask, the plan's limit), counts it
// when the model will answer, and retrieves its memories, all before any
// byte of the answer goes out: everything that can refuse or fail here
// becomes an ordinary error response. A *Refusal is policy's no.
func (s *Service) Prepare(ctx context.Context, r Request) (*Prepared, error) {
	if s == nil {
		return nil, ledger.ErrDisabled
	}
	if r.Started.IsZero() {
		r.Started = s.now()
	}
	scope := r.Scope.Narrow(r.Space.SpaceID)
	p := &Prepared{req: r}
	actor := s.policyActor(r)
	space := policy.Space{Name: r.SpaceName, Kind: r.Space.Kind}
	if d := policy.Decide(actor, policy.ActionAsk, policy.Object{}, space); d.Effect == policy.EffectRefuse {
		return nil, &Refusal{Decision: d}
	}
	if s.model != nil && !r.Uncounted {
		at := s.now()
		n, err := s.ledger.CountAsk(ctx, scope, at)
		if err != nil {
			return nil, err
		}
		p.counted, p.countedAt = true, at
		limit := s.plans.AskLimit(ctx, r.Scope.PersonID, r.Space)
		if d := policy.Decide(actor, policy.ActionAsk, policy.Object{AsksBefore: n - 1, AskLimit: limit}, space); d.Effect == policy.EffectRefuse {
			s.uncount(r, at)
			return nil, &Refusal{Decision: d, Limit: limit, ResetsAt: ledger.AskPeriod(at).AddDate(0, 1, 0)}
		}
	}

	start := time.Now()
	sctx, cancel := context.WithTimeout(ctx, s.cfg.RetrievalTimeout)
	found, err := s.search.Search(sctx, scope, v2recall.Query{
		Text: r.Question, Filter: v2recall.Filter{Spaces: []uuid.UUID{r.Space.SpaceID}},
		// Room for the quarantined ones left out below.
		Limit: s.cfg.Sources * 2,
	})
	timedOut := sctx.Err() != nil && ctx.Err() == nil
	cancel()
	p.retrievalMS = time.Since(start).Milliseconds()
	if err != nil {
		if p.counted {
			s.uncount(r, p.countedAt)
		}
		if timedOut || v2recall.IsTimeout(err) {
			return nil, ErrSearchTimeout
		}
		return nil, err
	}
	p.lexicalOnly = found.LexicalOnly
	ids := make([]uuid.UUID, 0, len(found.Hits))
	for _, h := range found.Hits {
		if h.SpaceID != r.Space.SpaceID || h.Lifecycle != "kept" || policy.Trust(h.Trust).External() {
			continue
		}
		p.sources = append(p.sources, Source{ID: h.ID, Ref: h.Ref, Statement: h.Statement, Section: h.Section,
			Kind: h.Kind, State: h.State, Trust: policy.Trust(h.Trust), Version: h.Version})
		ids = append(ids, h.ID)
		if len(p.sources) == s.cfg.Sources {
			break
		}
	}
	if len(ids) > 0 {
		rcs, err := s.ledger.LatestReceipts(ctx, scope, ids, ledger.SourceReceiptActions)
		if err != nil {
			// The receipt line is a courtesy; the answer doesn't need it.
			s.log.WarnContext(ctx, "ask: sources without receipts", "error", err)
		}
		for i := range p.sources {
			if rc, ok := rcs[p.sources[i].ID]; ok {
				p.sources[i].Receipt = &rc
			}
		}
	}
	return p, nil
}

func (s *Service) uncount(r Request, at time.Time) {
	// The ask is over either way; give it back even if the request is gone.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), 2*time.Second)
	defer cancel()
	if err := s.ledger.UncountAsk(ctx, r.Scope.Narrow(r.Space.SpaceID), at); err != nil {
		s.log.Warn("ask: could not give an ask back to the meter", "error", err)
	}
}

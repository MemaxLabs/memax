package ask

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// The events an answer streams, in order: sources once, then deltas and
// cites interleaved as the words arrive, then done; or error in place of
// done. They are the AskEvent schemas of openapi/v2.yaml.
const (
	EventSources = "sources"
	EventDelta   = "delta"
	EventCite    = "cite"
	EventDone    = "done"
	EventError   = "error"
)

// ErrorAnswerFailed is the error event's code: the model failed or ran
// out of time partway. What streamed so far stays on screen; the person
// can ask again.
const ErrorAnswerFailed = "answer_failed"

// SourcesEvent is the memories the answer may cite, best match first.
type SourcesEvent struct {
	Sources []SourceView `json:"sources"`
	// Answering is false when synthesis is off: done follows at once,
	// with outcome sources_only.
	Answering bool `json:"answering"`
	// LexicalOnly: the search matched words, not meaning.
	LexicalOnly bool `json:"lexical_only"`
}

// SourceView is one source on the wire.
type SourceView struct {
	ID        uuid.UUID       `json:"id"`
	Ref       string          `json:"ref"`
	Statement string          `json:"statement"`
	Section   ledger.Section  `json:"section"`
	Kind      ledger.Kind     `json:"kind"`
	State     string          `json:"state"`
	Trust     policy.Trust    `json:"trust"`
	Version   int             `json:"version"`
	Receipt   *ledger.Receipt `json:"receipt,omitempty"`
}

// DeltaEvent is the next words of the answer, citations removed.
type DeltaEvent struct {
	Text string `json:"text"`
}

// CiteEvent is a citation at this point of the answer. N numbers the
// cited memories in the order they're first cited, from 1.
type CiteEvent struct {
	N   int    `json:"n"`
	Ref string `json:"ref"`
}

// DoneEvent ends an answer.
type DoneEvent struct {
	Outcome Outcome `json:"outcome"`
	// Cited are the refs cited, in N order.
	Cited []string `json:"cited"`
	// Dropped counts citations removed because they named a memory the
	// model wasn't given.
	Dropped int       `json:"dropped"`
	Usage   UsageView `json:"usage"`
}

// UsageView is what the answer used and how long it took.
type UsageView struct {
	Model        string `json:"model,omitempty"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	RetrievalMS  int64  `json:"retrieval_ms"`
	// FirstTokenMS is from the request to the first words, when any came.
	FirstTokenMS *int64 `json:"first_token_ms,omitempty"`
	TotalMS      int64  `json:"total_ms"`
}

// ErrorEvent ends an answer that failed partway.
type ErrorEvent struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Emit sends one event. An error (the person went away) stops the answer
// and cancels the model call.
type Emit func(name string, data any) error

// Answer streams the answer to a prepared question through emit. It
// returns when the answer is done, failed, or the person went away
// (ctx cancelled, or emit failing); in the last case nothing more is sent.
func (s *Service) Answer(ctx context.Context, p *Prepared, emit Emit) Outcome {
	r := p.req
	views := make([]SourceView, len(p.sources))
	refs := make([]string, len(p.sources))
	byRef := map[string]uuid.UUID{}
	for i, src := range p.sources {
		views[i] = SourceView{ID: src.ID, Ref: src.Ref, Statement: src.Statement, Section: src.Section, Kind: src.Kind,
			State: src.State, Trust: src.Trust, Version: src.Version, Receipt: src.Receipt}
		refs[i] = src.Ref
		byRef[src.Ref] = src.ID
	}
	usage := UsageView{RetrievalMS: p.retrievalMS}
	answering := s.model != nil && len(p.sources) > 0
	if err := emit(EventSources, SourcesEvent{Sources: views, Answering: answering, LexicalOnly: p.lexicalOnly}); err != nil {
		s.giveBack(p)
		return ""
	}
	done := func(o Outcome, cited []string, dropped int) Outcome {
		usage.TotalMS = time.Since(r.Started).Milliseconds()
		if cited == nil {
			cited = []string{}
		}
		_ = emit(EventDone, DoneEvent{Outcome: o, Cited: cited, Dropped: dropped, Usage: usage})
		s.observe(p, o, cited, byRef)
		s.record(ctx, p, o, usage, len(cited), dropped)
		return o
	}
	if len(p.sources) == 0 {
		s.giveBack(p) // nothing reached the model
		return done(OutcomeNotCovered, nil, 0)
	}
	if s.model == nil {
		return done(OutcomeSourcesOnly, nil, 0)
	}

	mctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	usage.Model = s.cfg.Model
	c := newCiter(refs)
	var (
		emitErr    error
		numbered   = map[string]int{}
		cited      []string
		dropped    int
		spoke      bool
		firstToken time.Time
	)
	send := func(pieces []piece) {
		for _, pc := range pieces {
			if emitErr != nil {
				return
			}
			switch pc.kind {
			case pieceText:
				if firstToken.IsZero() {
					firstToken = time.Now()
				}
				emitErr = emit(EventDelta, DeltaEvent{Text: pc.text})
			case pieceCite:
				n, ok := numbered[pc.text]
				if !ok {
					n = len(numbered) + 1
					numbered[pc.text] = n
					cited = append(cited, pc.text)
				}
				emitErr = emit(EventCite, CiteEvent{N: n, Ref: pc.text})
			case pieceDropped:
				dropped++
			}
			if emitErr != nil {
				cancel() // the person went away: stop the model too
			}
		}
	}
	u, err := s.model.Stream(mctx, Call{Model: s.cfg.Model, System: systemPrompt(r.SpaceName),
		Prompt: userPrompt(r.Question, p.sources), MaxTokens: s.cfg.MaxTokens}, func(text string) {
		if text != "" {
			spoke = true
		}
		send(c.write(text))
	})
	usage.InputTokens, usage.OutputTokens = u.InputTokens, u.OutputTokens
	if !firstToken.IsZero() {
		ms := firstToken.Sub(r.Started).Milliseconds()
		usage.FirstTokenMS = &ms
	}
	if emitErr != nil || ctx.Err() != nil {
		if !spoke {
			s.giveBack(p)
		}
		s.log.Info("ask: the person left before the answer finished", "space_id", r.Space.SpaceID.String(),
			"first_token_ms", usage.FirstTokenMS, "spoke", spoke)
		cancelled.Add(context.WithoutCancel(ctx), 1)
		return ""
	}
	if err != nil {
		if !spoke {
			s.giveBack(p)
		}
		msg := "The answer didn't finish. Ask again in a moment."
		if errors.Is(mctx.Err(), context.DeadlineExceeded) {
			msg = "The answer took too long. Ask again, or ask something narrower."
		}
		s.log.WarnContext(ctx, "ask: the model failed", "model", s.cfg.Model, "spoke", spoke, "error", err)
		_ = emit(EventError, ErrorEvent{Code: ErrorAnswerFailed, Message: msg})
		usage.TotalMS = time.Since(r.Started).Milliseconds()
		s.record(ctx, p, "failed", usage, len(cited), dropped)
		return ""
	}
	send(c.close())
	if emitErr != nil {
		return ""
	}
	switch {
	case c.notCovered:
		return done(OutcomeNotCovered, nil, dropped)
	case len(cited) == 0:
		return done(OutcomeUnsupported, nil, dropped)
	}
	return done(OutcomeAnswered, cited, dropped)
}

// giveBack uncounts an ask that never reached the model.
func (s *Service) giveBack(p *Prepared) {
	if p.counted {
		p.counted = false
		s.uncount(p.req, p.countedAt)
	}
}

func (s *Service) observe(p *Prepared, o Outcome, cited []string, byRef map[string]uuid.UUID) {
	if s.observer == nil {
		return
	}
	a := Answered{SpaceID: p.req.Space.SpaceID, PersonID: p.req.Scope.PersonID, Outcome: o, At: s.now()}
	for _, src := range p.sources {
		a.Retrieved = append(a.Retrieved, src.ID)
	}
	for _, ref := range cited {
		a.Cited = append(a.Cited, byRef[ref])
	}
	s.observer.Answered(a)
}

var (
	metricsOnce sync.Once
	firstTokenH metric.Float64Histogram
	totalH      metric.Float64Histogram
	outcomes    metric.Int64Counter
	cancelled   metric.Int64Counter
)

func initMetrics() {
	metricsOnce.Do(func() {
		m := otel.Meter("memax.v2.ask")
		// Plan §11: Ask first token under 1.5 s.
		firstTokenH, _ = m.Float64Histogram("memax.v2.ask.first_token", metric.WithUnit("s"),
			metric.WithDescription("From the request to the answer's first words"))
		totalH, _ = m.Float64Histogram("memax.v2.ask.duration", metric.WithUnit("s"),
			metric.WithDescription("From the request to the answer's end"))
		outcomes, _ = m.Int64Counter("memax.v2.ask.outcomes", metric.WithDescription("Asks by outcome"))
		cancelled, _ = m.Int64Counter("memax.v2.ask.cancelled", metric.WithDescription("Asks the person left before the end"))
	})
}

func init() { initMetrics() }

// record logs and measures one finished Ask: counts and timings only,
// never the question's or the answer's words.
func (s *Service) record(ctx context.Context, p *Prepared, o Outcome, u UsageView, cited, dropped int) {
	attrs := metric.WithAttributes(attribute.String("outcome", string(o)), attribute.Bool("lexical_only", p.lexicalOnly))
	ctx = context.WithoutCancel(ctx)
	outcomes.Add(ctx, 1, attrs)
	totalH.Record(ctx, float64(u.TotalMS)/1000, attrs)
	if u.FirstTokenMS != nil {
		firstTokenH.Record(ctx, float64(*u.FirstTokenMS)/1000, attrs)
	}
	s.log.InfoContext(ctx, "ask: answered", "outcome", string(o), "space_id", p.req.Space.SpaceID.String(),
		"sources", len(p.sources), "cited", cited, "dropped_citations", dropped, "lexical_only", p.lexicalOnly,
		"model", u.Model, "input_tokens", u.InputTokens, "output_tokens", u.OutputTokens,
		"retrieval_ms", u.RetrievalMS, "first_token_ms", u.FirstTokenMS, "total_ms", u.TotalMS)
}

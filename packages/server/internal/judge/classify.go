package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// Proposal is the memory being judged, as the model sees it.
type Proposal struct {
	Ref       string
	Statement string
	Kind      string
	Section   string
	Area      string
	Sources   []SourceSummary
}

// SourceSummary is one of a proposal's sources.
type SourceSummary struct {
	Kind  string
	Ref   string
	URI   string
	Quote string
}

// Candidate is a memory the proposal is compared with.
type Candidate struct {
	Ref       string
	Statement string
	Kind      string
	Section   string
	Area      string
	// InForce: a decision in force.
	InForce bool
	// Keyed: in the keyed set (the same area key as the proposal, or the
	// proposal names its area), so an explicit change supersedes it.
	Keyed bool
}

// Pair is the verdict on one candidate.
type Pair struct {
	Ref             string
	Relation        ledger.Relation
	Confidence      float64
	Rationale       string
	MergedStatement string
	// ExplicitChange: the proposal itself says the earlier choice changed.
	ExplicitChange bool
	// Question, Labels and Suggested settle a contradiction: a short
	// question a person answers, one label per answer, and the answer the
	// sources support ("" for none). Empty for any other relation.
	Question  string
	Labels    ledger.ConflictLabels
	Suggested string
	// Tier is the tier whose answer this is.
	Tier string
	// Unconfirmed is set when the pair needed the strong tier and it gave
	// no answer: the pair is recorded but never acted on.
	Unconfirmed bool
}

// Condition is a "stays true while" condition the model proposed.
type Condition struct {
	Kind     string `json:"kind"`
	Manifest string `json:"manifest"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Repo     string `json:"repo"`
	Number   int    `json:"number"`
	State    string `json:"state"`
	Before   string `json:"before"`
}

// Classification is the model's answer for one proposal.
type Classification struct {
	// Pairs has one entry per candidate, in the candidates' order. A
	// candidate the model skipped is unrelated with confidence 0.
	Pairs      []Pair
	Conditions []Condition
	// Tier and Model answered the call (the strong tier's answers are on
	// the pairs it confirmed).
	Tier        string
	Model       string
	StrongModel string
	// Error is set when the strong tier failed (escalation_failed).
	Error string
	// LLM and Strong are the milliseconds spent in each phase.
	LLM, Strong int64
	// Calls counts model calls, by tier, for metrics and tests.
	Calls map[string]int
}

// ErrNoAnswer: no tier gave a valid answer.
var ErrNoAnswer = errors.New("judge: no valid answer from any model tier")

// Classifier makes the stage-1 call: prompt, validation, retry, fallback
// and escalation. It holds no state between calls.
type Classifier struct {
	model  Model
	cfg    Config
	schema *jsonschema.Schema
	raw    json.RawMessage
}

// NewClassifier returns a classifier, or nil when there is no model or
// no primary tier (stage 1 disabled).
func NewClassifier(m Model, cfg Config) *Classifier {
	cfg = cfg.withDefaults()
	if m == nil || !cfg.Primary.Enabled() {
		return nil
	}
	return &Classifier{model: m, cfg: cfg, schema: compiledSchema, raw: outputSchema}
}

// Classify judges a proposal against its candidates in one call (and, for
// pairs that touch a decision in force, one more on the strong tier).
func (c *Classifier) Classify(ctx context.Context, p Proposal, cands []Candidate) (Classification, error) {
	cls := Classification{Calls: map[string]int{}}
	if len(cands) == 0 {
		return cls, nil
	}
	start := time.Now()
	pairs, conds, tier, err := c.ask(ctx, &cls, []Tier{c.cfg.Primary, c.cfg.Fallback}, p, cands)
	cls.LLM = time.Since(start).Milliseconds()
	if err != nil {
		return cls, err
	}
	cls.Pairs, cls.Conditions, cls.Tier, cls.Model = pairs, conds, tier.Name, tier.Model

	// Escalate what touches a decision in force: those verdicts matter
	// most, and they are few.
	var idx []int
	for i, pr := range cls.Pairs {
		if cands[i].InForce && (pr.ExplicitChange || pr.Relation == ledger.RelationContradicts ||
			pr.Relation == ledger.RelationUpdates || pr.Relation == ledger.RelationDuplicate) {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 || !c.cfg.Strong.Enabled() || tier.Name == ledger.TierStrong {
		return cls, nil
	}
	sub := make([]Candidate, len(idx))
	for j, i := range idx {
		sub[j] = cands[i]
	}
	start = time.Now()
	strong, _, st, err := c.ask(ctx, &cls, []Tier{c.cfg.Strong}, p, sub)
	cls.Strong = time.Since(start).Milliseconds()
	if err != nil {
		// Never act on a verdict about a decision in force that the strong
		// tier didn't confirm: a false conflict costs a person.
		cls.Error = "escalation_failed"
		for _, i := range idx {
			cls.Pairs[i].Unconfirmed = true
		}
		return cls, nil
	}
	cls.StrongModel = st.Model
	for j, i := range idx {
		// The strong tier only confirms the relation (strongNote): the
		// words a person settles a conflict with stay the first reader's,
		// which wrote them for contradicts and updates alike, both flagged
		// on a decision in force. Writing them again cost the strong tier
		// a second or more per verdict (live eval, Oct 7). A conflict only
		// the strong tier found has none, and Review words it generically.
		sp := strong[j]
		if sp.Question == "" {
			sp.Question, sp.Labels, sp.Suggested = cls.Pairs[i].Question, cls.Pairs[i].Labels, cls.Pairs[i].Suggested
		}
		cls.Pairs[i] = sp
	}
	return cls, nil
}

// ask tries each tier in turn, each twice (the second time with what was
// wrong), and returns the first valid answer.
func (c *Classifier) ask(ctx context.Context, cls *Classification, tiers []Tier, p Proposal, cands []Candidate) ([]Pair, []Condition, Tier, error) {
	system := systemPrompt(c.cfg.Conditions)
	var last error
	for _, t := range tiers {
		if !t.Enabled() {
			continue
		}
		base := userPrompt(p, cands, t.Strict, c.raw)
		if t.Name == ledger.TierStrong {
			base += strongNote
		}
		prompt := base
		for attempt := 0; attempt < 2; attempt++ {
			cls.Calls[t.Name]++
			text, err := c.call(ctx, Call{Tier: t, System: system, Prompt: prompt, Schema: c.raw})
			if err == nil {
				var pairs []Pair
				var conds []Condition
				if pairs, conds, err = c.parse(text, cands, t.Name); err == nil {
					return pairs, conds, t, nil
				}
			}
			last = err
			if ctx.Err() != nil {
				return nil, nil, Tier{}, fmt.Errorf("%w: %v", ErrNoAnswer, ctx.Err())
			}
			// The retry says what was wrong, so the model can fix it.
			prompt = base + "\n\nYour previous answer was not usable (" +
				truncate(err.Error(), 200) + "). Answer again with only the JSON object."
		}
	}
	if last == nil {
		last = errors.New("no tier configured")
	}
	return nil, nil, Tier{}, fmt.Errorf("%w: %v", ErrNoAnswer, last)
}

func (c *Classifier) call(ctx context.Context, call Call) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
	defer cancel()
	ctx = anthropic.WithTracking(ctx, anthropic.Tracking{Metadata: map[string]any{"feature": "judge", "tier": call.Tier.Name}})
	text, err := c.model.Complete(ctx, call)
	if err == nil && strings.TrimSpace(text) == "" {
		err = ErrEmpty
	}
	return text, err
}

// answer is the JSON the model returns.
type answer struct {
	Pairs []struct {
		Candidate       string                `json:"candidate"`
		Relation        string                `json:"relation"`
		Confidence      float64               `json:"confidence"`
		ExplicitChange  bool                  `json:"explicit_change"`
		Rationale       string                `json:"rationale"`
		MergedStatement string                `json:"merged_statement"`
		Question        string                `json:"question"`
		Labels          ledger.ConflictLabels `json:"labels"`
		Suggested       string                `json:"suggested"`
	} `json:"pairs"`
	Conditions []Condition `json:"conditions"`
}

// parse validates an answer against the schema and the candidates.
func (c *Classifier) parse(text string, cands []Candidate, tier string) ([]Pair, []Condition, error) {
	raw := anthropic.ExtractJSONObject(anthropic.StripMarkdownFences(text))
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("not JSON: %w", err)
	}
	if err := c.schema.Validate(doc); err != nil {
		return nil, nil, fmt.Errorf("doesn't match the schema: %s", firstLine(err.Error()))
	}
	var a answer
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	if err := dec.Decode(&a); err != nil {
		return nil, nil, fmt.Errorf("not JSON: %w", err)
	}
	byRef := map[string]Pair{}
	for _, pr := range a.Pairs {
		ref := strings.ToUpper(strings.TrimSpace(pr.Candidate))
		if _, seen := byRef[ref]; seen || !slices.ContainsFunc(cands, func(x Candidate) bool { return x.Ref == ref }) {
			continue
		}
		if pr.Confidence < 0 || pr.Confidence > 1 {
			return nil, nil, fmt.Errorf("confidence %v for %s is outside 0..1", pr.Confidence, ref)
		}
		rel := ledger.Relation(pr.Relation)
		merged := strings.TrimSpace(pr.MergedStatement)
		if rel != ledger.RelationDuplicate && rel != ledger.RelationExtends {
			merged = ""
		}
		p := Pair{Ref: ref, Relation: rel, Confidence: pr.Confidence, ExplicitChange: pr.ExplicitChange,
			Rationale: truncate(strings.TrimSpace(pr.Rationale), 300), MergedStatement: truncate(merged, ledger.MaxStatementRunes),
			Tier: tier}
		// A person settles a contradiction (or an implicit update of a
		// decision in force, which is flagged too) by answering a question.
		if rel == ledger.RelationContradicts || rel == ledger.RelationUpdates {
			p.Question = oneLine(pr.Question, ledger.MaxConflictQuestion)
			p.Labels = ledger.ConflictLabels{
				Proposal: oneLine(pr.Labels.Proposal, ledger.MaxConflictLabel),
				Decision: oneLine(pr.Labels.Decision, ledger.MaxConflictLabel),
				Both:     oneLine(pr.Labels.Both, ledger.MaxConflictLabel),
				Open:     oneLine(pr.Labels.Open, ledger.MaxConflictLabel),
			}
			if slices.Contains(ledger.Suggestions, pr.Suggested) {
				p.Suggested = pr.Suggested
			}
		}
		byRef[ref] = p
	}
	if len(byRef) == 0 {
		return nil, nil, errors.New("it names none of the candidates")
	}
	out := make([]Pair, len(cands))
	for i, cd := range cands {
		if pr, ok := byRef[cd.Ref]; ok {
			out[i] = pr
		} else {
			out[i] = Pair{Ref: cd.Ref, Relation: ledger.RelationUnrelated, Tier: tier}
		}
	}
	if !c.cfg.Conditions {
		a.Conditions = nil
	}
	return out, a.Conditions, nil
}

// oneLine is one of the model's lines for a person: control characters
// become spaces, and it is trimmed and bounded.
func oneLine(s string, n int) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }), " ")
	return truncate(strings.TrimSpace(s), n)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// outputSchema is the answer's JSON Schema. It sticks to what strict
// structured outputs accept: closed objects, every property required,
// enums, no numeric bounds (confidence is checked in parse).
var outputSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["pairs", "conditions"],
  "properties": {
    "pairs": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["candidate", "relation", "confidence", "explicit_change", "rationale", "merged_statement", "question", "labels", "suggested"],
        "properties": {
          "candidate": {"type": "string"},
          "relation": {"type": "string", "enum": ["duplicate", "updates", "extends", "contradicts", "unrelated"]},
          "confidence": {"type": "number"},
          "explicit_change": {"type": "boolean"},
          "rationale": {"type": "string"},
          "merged_statement": {"type": "string"},
          "question": {"type": "string"},
          "labels": {
            "type": "object",
            "additionalProperties": false,
            "required": ["proposal", "decision", "both", "open"],
            "properties": {
              "proposal": {"type": "string"},
              "decision": {"type": "string"},
              "both": {"type": "string"},
              "open": {"type": "string"}
            }
          },
          "suggested": {"type": "string", "enum": ["proposal", "decision", "both", "open", "none"]}
        }
      }
    },
    "conditions": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["kind", "manifest", "name", "path", "repo", "number", "state", "before"],
        "properties": {
          "kind": {"type": "string", "enum": ["dep_present", "file_exists", "file_unchanged", "pr_state", "before"]},
          "manifest": {"type": "string"},
          "name": {"type": "string"},
          "path": {"type": "string"},
          "repo": {"type": "string"},
          "number": {"type": "integer"},
          "state": {"type": "string"},
          "before": {"type": "string"}
        }
      }
    }
  }
}`)

var compiledSchema = func() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(outputSchema))
	if err != nil {
		panic(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("judge.json", doc); err != nil {
		panic(err)
	}
	s, err := c.Compile("judge.json")
	if err != nil {
		panic(err)
	}
	return s
}()

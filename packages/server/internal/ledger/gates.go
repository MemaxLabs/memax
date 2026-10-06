package ledger

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Decision gates (G-; plan 25 §3, §5.12, E8; epic 1.11). An agent stops
// and asks a person to decide between two to four options
// (RequestDecision). A person answers (AnswerGate), and the answer is kept,
// in the same transaction, as a decision the person authored, linked back
// to the gate, so it compiles into every target. The question can be taken
// back before anyone answers (WithdrawGate), and it expires: a waiting gate
// past its expires_at reads as expired and can't be answered.
//
// Every gate command is receipted like the memory commands (migration 036),
// and its receipts never hold the question's words. How a gate ended
// reaches the agent that asked in its next recall, once (TakeGateNews).

// GateStatus is where a gate is.
type GateStatus string

// The statuses. Expired is never stored: it is a waiting gate whose
// expires_at has passed, by the ledger's clock.
const (
	GateWaiting   GateStatus = "waiting"
	GateAnswered  GateStatus = "answered"
	GateWithdrawn GateStatus = "withdrawn"
	GateExpired   GateStatus = "expired"
)

// GateStatuses lists every status a gate can read as.
var GateStatuses = []GateStatus{GateWaiting, GateAnswered, GateWithdrawn, GateExpired}

// Valid reports whether s is a known status.
func (s GateStatus) Valid() bool { return slices.Contains(GateStatuses, s) }

// GateTransitionAllowed is v2.gate_status_transition_allowed (migration
// 036): a gate starts waiting and ends once, answered or withdrawn. An
// empty from means asking it. Expired is never stored, so it is no
// transition.
func GateTransitionAllowed(from, to GateStatus) bool {
	if from == "" {
		return to == GateWaiting
	}
	return from == GateWaiting && (to == GateAnswered || to == GateWithdrawn)
}

// Limits on a gate. The question and its options are short on purpose: the
// answer becomes one kept statement, the question followed by the chosen
// option (gateStatement).
const (
	MaxGateQuestionRunes = 300
	MaxGateContextRunes  = 2000
	MinGateOptions       = 2
	MaxGateOptions       = 4
	MaxGateLabelRunes    = 200
	MaxGateDetailRunes   = 500
	// DefaultGateTTL is how long a gate waits when the agent doesn't say:
	// past a week, the session that asked has long moved on.
	DefaultGateTTL = 7 * 24 * time.Hour
	MinGateTTL     = 5 * time.Minute
	MaxGateTTL     = 30 * 24 * time.Hour
)

// The gate commands.
const (
	CommandRequestDecision CommandName = "request_decision"
	CommandAnswerGate      CommandName = "answer_gate"
	CommandWithdrawGate    CommandName = "withdraw_gate"
)

// The gate receipts (migration 036; answered was in 028's list).
const (
	ActionAsked     Action = "asked"     // an agent asked a gate
	ActionAnswered  Action = "answered"  // a person answered it; the decision is kept beside it
	ActionWithdrawn Action = "withdrawn" // the question was taken back
)

// ObjectGate is the receipts.object_kind of a gate.
const ObjectGate = "gate"

// RequestDecision asks a person to decide: a G- gate, waiting in the space
// for an answer. Only an agent that may propose there asks.
type RequestDecision struct {
	Meta
	SpaceID  uuid.UUID
	Question string
	// Context is why it matters: the tradeoffs, and a recommendation if the
	// agent has one.
	Context string
	// Options are the two to four answers, in order.
	Options []DecisionOption
	// ExpiresAt defaults to DefaultGateTTL from now, and must fall between
	// MinGateTTL and MaxGateTTL from now.
	ExpiresAt *time.Time
}

// Name implements Command.
func (*RequestDecision) Name() CommandName { return CommandRequestDecision }

// AnswerGate answers a waiting gate with one of its options. The answer is
// kept, in the same transaction, as a decision authored by the person who
// answered.
type AnswerGate struct {
	Meta
	// Gate is a display ID ("G-0012") or a uuid.
	Gate string
	// ExpectedVersion, when set, must match the gate's version (If-Match).
	ExpectedVersion int
	// Option is the chosen option, from 1.
	Option int
	// Delivered says the asking agent receives the answer in this same call
	// (an answer given inside the agent), so its next recall needn't repeat
	// it.
	Delivered bool
}

// Name implements Command.
func (*AnswerGate) Name() CommandName { return CommandAnswerGate }

// WithdrawGate takes a waiting gate's question back.
type WithdrawGate struct {
	Meta
	Gate            string
	ExpectedVersion int
}

// Name implements Command.
func (*WithdrawGate) Name() CommandName { return CommandWithdrawGate }

// Gate is the projection of one decision gate.
type Gate struct {
	ID       uuid.UUID `json:"id"`
	Ref      string    `json:"ref"`
	SpaceID  uuid.UUID `json:"space_id"`
	TenantID uuid.UUID `json:"tenant_id"`
	// Question, Context and Options are the asking agent's words.
	Question string           `json:"question"`
	Context  string           `json:"context,omitempty"`
	Options  []DecisionOption `json:"options"`
	// Status is what the gate reads as now: expired once a waiting gate's
	// expires_at has passed.
	Status    GateStatus `json:"status"`
	ExpiresAt time.Time  `json:"expires_at"`
	// AskedBy is the agent connection that asked, and Agent its slug.
	AskedBy    uuid.UUID `json:"asked_by"`
	Agent      string    `json:"agent,omitempty"`
	SessionRef string    `json:"session_ref,omitempty"`
	// NeedsWeb says an answer must be given by a person on the web: the
	// space's rule for decisions (D15).
	NeedsWeb  bool            `json:"needs_web"`
	Answer    *GateAnswer     `json:"answer,omitempty"`
	Withdrawn *GateWithdrawal `json:"withdrawn,omitempty"`
	// DeliveredAt is when the agent that asked was told how it ended.
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	// Version changes when the gate ends; send it back as If-Match.
	Version          int       `json:"version"`
	CreatedReceiptID uuid.UUID `json:"created_receipt_id"`
	LastReceiptID    uuid.UUID `json:"last_receipt_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	seq    int64
	stored GateStatus
}

// GateAnswer is how a person answered a gate, and the decision it became.
type GateAnswer struct {
	// Option is the chosen option, from 1, and Label its words.
	Option int    `json:"option"`
	Label  string `json:"label"`
	// Memory is the kept decision the answer became.
	Memory     MemoryPointer    `json:"memory"`
	AnsweredBy uuid.UUID        `json:"answered_by"`
	AnsweredAt time.Time        `json:"answered_at"`
	Assurance  policy.Assurance `json:"assurance"`
}

// GateWithdrawal is who took a gate's question back, and when.
type GateWithdrawal struct {
	// ByKind is agent (the one that asked) or person.
	ByKind policy.ActorKind `json:"by_kind"`
	By     uuid.UUID        `json:"by"`
	At     time.Time        `json:"at"`
}

// GateStateError is returned when a gate's status doesn't allow the
// command: it was answered or withdrawn already, or it expired.
type GateStateError struct {
	Ref     string
	Status  GateStatus
	Command CommandName
}

func (e *GateStateError) Error() string {
	switch {
	case e.Status == GateAnswered && e.Command == CommandAnswerGate:
		return e.Ref + " was already answered, and its answer is kept. Change that decision instead of answering again."
	case e.Status == GateAnswered:
		return e.Ref + " was already answered, so there is nothing to withdraw."
	case e.Status == GateWithdrawn:
		return e.Ref + " was withdrawn, so there is nothing to answer or withdraw."
	case e.Status == GateExpired:
		return e.Ref + " expired, so the agent that asked stopped waiting. Ask it again if it still matters."
	}
	return e.Ref + " is " + string(e.Status) + "."
}

// Is makes errors.Is(err, ErrInvalidTransition) match.
func (e *GateStateError) Is(target error) bool { return target == ErrInvalidTransition }

// waiting checks the gate is still waiting for an answer.
func (g *Gate) waiting(cmd CommandName) error {
	if g.Status != GateWaiting {
		return &GateStateError{Ref: g.Ref, Status: g.Status, Command: cmd}
	}
	return nil
}

func (c *RequestDecision) validate() error {
	if c.SpaceID == uuid.Nil {
		return invalid("space_id", "say which space to ask in")
	}
	c.Question = strings.TrimSpace(c.Question)
	if err := checkText("question", c.Question, MaxGateQuestionRunes, true); err != nil {
		return err
	}
	c.Context = strings.TrimSpace(c.Context)
	if err := checkText("context", c.Context, MaxGateContextRunes, false); err != nil {
		return err
	}
	if len(c.Options) < MinGateOptions || len(c.Options) > MaxGateOptions {
		return invalid("options", "give %d to %d options", MinGateOptions, MaxGateOptions)
	}
	seen := map[string]bool{}
	for i := range c.Options {
		o := &c.Options[i]
		o.Label, o.Detail = strings.TrimSpace(o.Label), strings.TrimSpace(o.Detail)
		if err := checkText("options.label", o.Label, MaxGateLabelRunes, true); err != nil {
			return err
		}
		if err := checkText("options.detail", o.Detail, MaxGateDetailRunes, false); err != nil {
			return err
		}
		key := strings.ToLower(o.Label)
		if seen[key] {
			return invalid("options.label", "%q is listed twice; make each option different", o.Label)
		}
		seen[key] = true
	}
	return nil
}

func validateGateTarget(ref string, expected int) error {
	if strings.TrimSpace(ref) == "" {
		return invalid("gate", "say which gate, by display ID (G-0012) or id")
	}
	if expected < 0 {
		return invalid("expected_version", "send the gate's version (If-Match), or none")
	}
	return nil
}

// gateStatement is the kept words of an answer: the question, then the
// chosen option ("Which deploy target should the v2 API use? Fly.io").
func gateStatement(question, label string) string {
	q := strings.TrimSpace(question)
	switch {
	case strings.HasSuffix(q, "?"), strings.HasSuffix(q, "."), strings.HasSuffix(q, "!"), strings.HasSuffix(q, ":"):
		return q + " " + label
	case strings.HasSuffix(q, "？"), strings.HasSuffix(q, "。"), strings.HasSuffix(q, "！"), strings.HasSuffix(q, "："):
		return q + label
	}
	return q + ": " + label
}

package ledger

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Limits on what one command may carry.
const (
	// MaxStatementRunes bounds one statement. A V2 memory is one fact,
	// not a document; long material belongs in a note.
	MaxStatementRunes = 2000
	MaxReasonRunes    = 2000
	MaxSources        = 20
	MaxQuoteRunes     = 4000
	MaxSourceRefRunes = 500
	MaxSourceURIRunes = 2048
	MaxKeyLength      = 255
	MaxSessionRef     = 255
	MaxAgentSlug      = 64
	// MaxClockSkew is how far in the future occurred_at may be.
	MaxClockSkew = 5 * time.Minute
)

// CommandName names a command in idempotency records and logs.
type CommandName string

// The commands this package applies. Later epics add merge, flag,
// verify, fade, restore, forget, move, undo and the rest of plan 25 §5.3.
const (
	CommandRemember CommandName = "remember"
	CommandPropose  CommandName = "propose"
	CommandKeep     CommandName = "keep"
	CommandEdit     CommandName = "edit"
	CommandReject   CommandName = "reject"
)

// Command is one change to the record. Pass a pointer to one of the
// command structs (*Remember, *Propose, *Keep, *Edit, *Reject, and the
// agent commands in agents.go).
type Command interface {
	envelope() *Meta
	Name() CommandName
}

// Meta is what every command carries besides its payload.
type Meta struct {
	Actor Actor
	// Scope is the spaces the actor may touch; see ResolveUserScope.
	Scope Scope
	Via   policy.Via
	// SessionRef is the agent session, if any ("cc-7f3a").
	SessionRef string
	// IdempotencyKey is required. Retrying a command with the same key
	// returns the original result without writing again.
	IdempotencyKey string
	// OccurredAt is when it happened on the client; an offline queue
	// keeps the original time. Zero means now.
	OccurredAt time.Time
	// Reason is optional ("duplicate of M-0156"). It is redacted when the
	// memory is forgotten, so it may mention the memory's content.
	Reason string
}

func (m *Meta) envelope() *Meta { return m }

// NewMemory is the payload of Remember and Propose.
type NewMemory struct {
	SpaceID   uuid.UUID
	Statement string
	Section   Section
	// Kind defaults to fact.
	Kind     Kind
	Decision *DecisionFields
	Sources  []SourceInput
	// StaleAfter is when the memory should be verified again.
	StaleAfter *time.Time
	// Conditions are "stays true while" predicates (a JSON array).
	Conditions json.RawMessage
	// Applies is where it applies, e.g. {"paths": ["packages/web/**"]}.
	Applies   json.RawMessage
	ValidFrom *time.Time
	ValidTo   *time.Time
	// ContradictsDecision says the statement contradicts a decision in
	// force, when the caller already knows (an import that found the
	// conflict). The judge finds it otherwise, after the write, and a
	// Write-level agent's write that touches a decision in force waits for
	// it (policy.CodeTouchesDecision).
	ContradictsDecision bool
}

// Remember writes a statement that its author means to keep: a member
// or owner keeps it at once; anyone else's goes to Review.
type Remember struct {
	Meta
	NewMemory
}

// Name implements Command.
func (*Remember) Name() CommandName { return CommandRemember }

// Propose writes a statement for Review. A person's proposal always
// waits for Review; an agent's is kept at once when its autonomy is
// Write and nothing downgrades it (plan 25 §5.6).
type Propose struct {
	Meta
	NewMemory
}

// Name implements Command.
func (*Propose) Name() CommandName { return CommandPropose }

// Keep keeps a proposal. Only a person can keep.
type Keep struct {
	Meta
	// Memory is a display ID ("M-0219") or a uuid.
	Memory string
	// ExpectedVersion, when set, must match the memory's version, so a
	// person never keeps words they haven't seen.
	ExpectedVersion int
}

// Name implements Command.
func (*Keep) Name() CommandName { return CommandKeep }

// Edit writes a new version of a memory's statement (and optionally
// moves it to another section).
type Edit struct {
	Meta
	Memory string
	// ExpectedVersion is required: the version the editor started from
	// (If-Match). A mismatch is an edit clash.
	ExpectedVersion int
	Statement       string
	// Section moves the memory; empty keeps it where it is.
	Section Section
	// Keep also keeps a proposal after editing it (Review's "edit and
	// keep"). It is ignored for kept memories.
	Keep bool
	// ContradictsDecision is the judge's verdict on the new statement.
	ContradictsDecision bool
}

// Name implements Command.
func (*Edit) Name() CommandName { return CommandEdit }

// Reject rejects a proposal. Only a person can reject.
type Reject struct {
	Meta
	Memory          string
	ExpectedVersion int
}

// Name implements Command.
func (*Reject) Name() CommandName { return CommandReject }

// validateMeta checks the envelope and fills defaults.
func validateMeta(m *Meta, now time.Time) error {
	a := m.Actor
	if !a.Kind.Valid() {
		return invalid("actor.kind", "use person, agent, dream, memax or repository")
	}
	if (a.Kind == policy.ActorPerson || a.Kind == policy.ActorAgent) && a.ID == uuid.Nil {
		return invalid("actor.id", "people and agents need an id")
	}
	if !a.Credential.Valid() {
		return invalid("actor.credential", "use session, oauth or api_key")
	}
	if a.Autonomy != "" && !a.Autonomy.Valid() {
		return invalid("actor.autonomy", "use read, propose or write")
	}
	if utf8.RuneCountInString(a.Agent) > MaxAgentSlug {
		return invalid("actor.agent", "use the agent's slug, at most %d characters", MaxAgentSlug)
	}
	if !m.Via.Valid() {
		return invalid("via", "say which surface the change came through (web, cli, mcp, review, …)")
	}
	if m.IdempotencyKey == "" || len(m.IdempotencyKey) > MaxKeyLength {
		return invalid("idempotency_key", "send an Idempotency-Key of 1 to %d characters with every command", MaxKeyLength)
	}
	if len(m.SessionRef) > MaxSessionRef {
		return invalid("session_ref", "at most %d characters", MaxSessionRef)
	}
	if err := checkText("reason", m.Reason, MaxReasonRunes, false); err != nil {
		return err
	}
	if m.OccurredAt.IsZero() {
		m.OccurredAt = now
	} else if m.OccurredAt.After(now.Add(MaxClockSkew)) {
		return invalid("occurred_at", "is in the future; check the device clock")
	}
	// Postgres keeps microseconds; truncate so the Result reports exactly
	// what the receipt stores (and what a replay returns).
	m.OccurredAt = m.OccurredAt.Truncate(time.Microsecond)
	return nil
}

// validate checks a new statement and normalises it in place.
func (n *NewMemory) validate() error {
	if n.SpaceID == uuid.Nil {
		return invalid("space_id", "say which space to write to")
	}
	n.Statement = strings.TrimSpace(n.Statement)
	if err := checkText("statement", n.Statement, MaxStatementRunes, true); err != nil {
		return err
	}
	if !n.Section.Valid() {
		return invalid("section", "use decisions, conventions, preferences or open_question")
	}
	if n.Kind == "" {
		n.Kind = KindFact
	}
	if !n.Kind.Valid() {
		return invalid("kind", "use fact or decision")
	}
	if n.Decision != nil && n.Kind != KindDecision {
		return invalid("decision", "only a decision has decision fields; set kind to decision")
	}
	if n.Decision != nil {
		if err := n.Decision.validate(); err != nil {
			return err
		}
	}
	if len(n.Sources) > MaxSources {
		return invalid("sources", "cite at most %d sources", MaxSources)
	}
	for i := range n.Sources {
		if err := n.Sources[i].validate(); err != nil {
			return err
		}
	}
	var err error
	if n.Conditions, err = jsonOr(n.Conditions, "conditions", "[]", '['); err != nil {
		return err
	}
	if n.Applies, err = jsonOr(n.Applies, "scope", "{}", '{'); err != nil {
		return err
	}
	if n.ValidFrom != nil && n.ValidTo != nil && n.ValidTo.Before(*n.ValidFrom) {
		return invalid("valid_to", "must not be before valid_from")
	}
	return nil
}

// validate bounds the decision fields like the other free text, so a NUL
// byte or a novel-length "why" is a clear 400 rather than a database
// error.
func (d *DecisionFields) validate() error {
	for _, f := range []struct {
		field, value string
		max          int
	}{
		{"decision.why", d.Why, MaxReasonRunes},
		{"decision.consequences", d.Consequences, MaxReasonRunes},
		{"decision.area", d.Area, MaxSourceRefRunes},
	} {
		if err := checkText(f.field, f.value, f.max, false); err != nil {
			return err
		}
	}
	switch d.Status {
	case "", DecisionInForce, DecisionSuperseded, DecisionOpen:
	default:
		return invalid("decision.status", "use in_force, superseded or open")
	}
	if len(d.Options) > MaxSources {
		return invalid("decision.options", "list at most %d options", MaxSources)
	}
	for _, o := range d.Options {
		if err := checkText("decision.options.label", strings.TrimSpace(o.Label), MaxSourceRefRunes, true); err != nil {
			return err
		}
		if err := checkText("decision.options.detail", o.Detail, MaxReasonRunes, false); err != nil {
			return err
		}
	}
	return nil
}

func (s *SourceInput) validate() error {
	if !s.Kind.Valid() {
		return invalid("sources.kind", "use session, pr, file, url, issue, email, note, import or memory")
	}
	s.Ref = strings.TrimSpace(s.Ref)
	if err := checkText("sources.ref", s.Ref, MaxSourceRefRunes, true); err != nil {
		return err
	}
	if err := checkText("sources.uri", s.URI, MaxSourceURIRunes, false); err != nil {
		return err
	}
	if err := checkText("sources.quote", s.Quote, MaxQuoteRunes, false); err != nil {
		return err
	}
	if s.Trust != "" && !s.Trust.Valid() {
		return invalid("sources.trust", "use person, agent_own_work, repository or external")
	}
	var err error
	s.Locator, err = jsonOr(s.Locator, "sources.locator", "{}", '{')
	return err
}

func validateTarget(ref string, expected int, required bool) error {
	if strings.TrimSpace(ref) == "" {
		return invalid("memory", "say which memory, by display ID (M-0219) or id")
	}
	if expected < 0 || (required && expected == 0) {
		return invalid("expected_version", "send the version you started from (If-Match)")
	}
	return nil
}

// checkText rejects NUL bytes and invalid UTF-8 (Postgres text can't
// hold them) and enforces a length in characters.
func checkText(field, s string, maxRunes int, required bool) error {
	switch {
	case required && s == "":
		return invalid(field, "can't be empty")
	case !utf8.ValidString(s) || strings.ContainsRune(s, 0):
		return invalid(field, "must be valid UTF-8 text")
	case utf8.RuneCountInString(s) > maxRunes:
		return invalid(field, "is too long (at most %d characters)", maxRunes)
	}
	return nil
}

// jsonOr returns raw compacted, or the default when empty; the value
// must be a JSON array or object (whichever open says).
func jsonOr(raw json.RawMessage, field, def string, open byte) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(def), nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil || buf.Len() == 0 || buf.Bytes()[0] != open {
		kind := "an object"
		if open == '[' {
			kind = "an array"
		}
		return nil, invalid(field, "must be %s", kind)
	}
	return buf.Bytes(), nil
}

// requestHash fingerprints what a command asks for, so a reused
// idempotency key with different content is refused. It covers the
// payload, the reason and the surface; not the key, the actor (the key
// is already per actor), the scope or the client time.
func requestHash(cmd Command) ([]byte, error) {
	m := cmd.envelope()
	strip := Meta{Via: m.Via, Reason: m.Reason, SessionRef: m.SessionRef}
	var v any
	switch c := cmd.(type) {
	case *Remember:
		cp := *c
		cp.Meta = strip
		v = cp
	case *Propose:
		cp := *c
		cp.Meta = strip
		v = cp
	case *Keep:
		cp := *c
		cp.Meta = strip
		v = cp
	case *Edit:
		cp := *c
		cp.Meta = strip
		v = cp
	case *Reject:
		cp := *c
		cp.Meta = strip
		v = cp
	case *ConnectAgent:
		cp := *c
		cp.Meta = strip
		v = cp
	case *SetAutonomy:
		cp := *c
		cp.Meta = strip
		v = cp
	case *PauseAgent:
		cp := *c
		cp.Meta = strip
		v = cp
	case *ResumeAgent:
		cp := *c
		cp.Meta = strip
		v = cp
	case *DisconnectAgent:
	case *ReviseBrief:
		cp := *c
		cp.Meta = strip
		v = cp
	case *ConfigureTarget:
		cp := *c
		cp.Meta = strip
		v = cp
	case *RequestCompile:
		cp := *c
		cp.Meta = strip
		v = cp
	case *RecordCompile:
		cp := *c
		cp.Meta = strip
		v = cp
	case *RecordDelivery:
		cp := *c
		cp.Meta = strip
		v = cp
	case *RecordObservation:
		cp := *c
		cp.Meta = strip
		v = cp
	case *ResolveDrift:
		cp := *c
		cp.Meta = strip
		v = cp
	case *RecordVerdict:
		cp := *c
		cp.Meta = strip
		v = cp
	case *ResolveConflict:
		cp := *c
		cp.Meta = strip
		v = cp
	case *Undo:
		cp := *c
		cp.Meta = strip
		v = cp
	case *RequestDecision:
		cp := *c
		cp.Meta = strip
		v = cp
	case *AnswerGate:
		cp := *c
		cp.Meta = strip
		v = cp
	case *WithdrawGate:
		cp := *c
		cp.Meta = strip
		v = cp
	case *Forget:
		cp := *c
		cp.Meta = strip
		v = cp
	case *ForgetSpace:
		cp := *c
		cp.Meta = strip
		v = cp
	case *RequestForget:
		cp := *c
		cp.Meta = strip
		v = cp
	case *DeclineForget:
		cp := *c
		cp.Meta = strip
		v = cp
	case *ReapplyForget:
		cp := *c
		cp.Meta = strip
		v = cp
	case *ImportStatement:
		cp := *c
		cp.Meta = strip
		v = cp
	case *RecordImportCheck:
		cp := *c
		cp.Meta = strip
		v = cp
	case *SettleImportConflict:
		cp := *c
		cp.Meta = strip
		v = cp
	case *PublishEdition:
		cp := *c
		cp.Meta = strip
		v = cp
	case *UndoDreamAction:
		cp := *c
		cp.Meta = strip
		v = cp
	case *Restore:
		cp := *c
		cp.Meta = strip
		v = cp
	case *Export:
		cp := *c
		cp.Meta = strip
		v = cp
	default:
		return nil, invalid("command", "unknown command")
	}
	b, err := json.Marshal(struct {
		Name CommandName
		Body any
	}{cmd.Name(), v})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	return sum[:], nil
}

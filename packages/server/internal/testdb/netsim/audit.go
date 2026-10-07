package netsim

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Audit checks, statement by statement in the order Postgres receives
// them, that nothing touches a v2 table outside a transaction that first
// switched to a V2 role and set its scope (app.space_ids). That is the
// rule row-level security rests on: the scope is SET LOCAL, so a statement
// that ran before it, or outside the transaction, would run unscoped, as
// whatever role the connection logged in as.
//
// It reads the wire (Proxy.Watch), not pgx's calls, so no client-side
// path (a pipeline, a batch, a raw connection) can slip past it. It
// understands the ledger's statements: BEGIN / COMMIT / ROLLBACK,
// set_config('role', $n, true) and set_config('app.space_ids', $m, true)
// with their bound values, and SET LOCAL ROLE.
type Audit struct {
	// Roles are the roles a v2 statement may run as.
	Roles []string

	mu         sync.Mutex
	conns      map[int64]*txState
	armed      bool
	checked    int
	violations []Violation
}

// Violation is a v2 statement that ran unscoped.
type Violation struct {
	Conn   int64
	SQL    string
	Reason string
}

func (v Violation) String() string {
	return fmt.Sprintf("conn %d: %s: %s", v.Conn, v.Reason, oneLine(v.SQL))
}

type txState struct {
	inTx   bool
	role   string
	scoped bool
}

// NewAudit returns an audit allowing the given roles (memax_v2 and the
// sweep roles, say).
func NewAudit(roles ...string) *Audit {
	return &Audit{Roles: roles, conns: map[int64]*txState{}}
}

// Arm starts recording violations; statements are tracked either way, so
// arming mid-run sees every connection's transaction state.
func (a *Audit) Arm() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.armed = true
}

// Disarm stops recording violations.
func (a *Audit) Disarm() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.armed = false
}

// Violations lists what ran unscoped while armed.
func (a *Audit) Violations() []Violation {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Violation(nil), a.violations...)
}

// Checked counts the v2 statements checked while armed.
func (a *Audit) Checked() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.checked
}

var (
	v2Table      = regexp.MustCompile(`(?i)\bv2\.[a-z_]+`)
	setRole      = regexp.MustCompile(`(?i)set_config\(\s*'role'\s*,\s*\$(\d+)`)
	setSpaces    = regexp.MustCompile(`(?i)set_config\(\s*'app\.space_ids'\s*,\s*\$(\d+)\s*,\s*true`)
	setLocalRole = regexp.MustCompile(`(?i)^set\s+local\s+role\s+"?([a-z0-9_]+)"?`)
)

// Observe takes one statement (Proxy.Watch).
func (a *Audit) Observe(s Statement) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.conns[s.Conn]
	if st == nil {
		st = &txState{}
		a.conns[s.Conn] = st
	}
	// A simple query may hold several statements; take them in order.
	for _, sql := range splitSimple(s) {
		a.step(s, st, sql)
	}
}

func (a *Audit) step(s Statement, st *txState, sql string) {
	head := strings.ToLower(strings.TrimSpace(stripComments(sql)))
	switch {
	case strings.HasPrefix(head, "begin"), strings.HasPrefix(head, "start transaction"):
		*st = txState{inTx: true}
		return
	case strings.HasPrefix(head, "rollback to"), strings.HasPrefix(head, "release"), strings.HasPrefix(head, "savepoint"):
		return
	case strings.HasPrefix(head, "commit"), strings.HasPrefix(head, "rollback"), strings.HasPrefix(head, "end"),
		strings.HasPrefix(head, "abort"):
		*st = txState{}
		return
	}
	// The statement's own settings apply before its v2 tables are read
	// (Postgres evaluates the target list's set_config calls first, which
	// is how the ledger's scope statement works), so record them first;
	// outside a transaction they last only for this statement.
	inTx := st.inTx
	role, scoped := st.role, st.scoped
	if m := setRole.FindStringSubmatch(sql); m != nil {
		role = param(s, m[1])
	}
	if m := setLocalRole.FindStringSubmatch(head); m != nil {
		role = m[1]
	}
	if setSpaces.MatchString(sql) {
		scoped = true
	}
	if inTx {
		st.role, st.scoped = role, scoped
	}
	if !v2Table.MatchString(sql) || !a.armed {
		return
	}
	a.checked++
	switch {
	case !inTx:
		a.violations = append(a.violations, Violation{Conn: s.Conn, SQL: sql, Reason: "outside a transaction"})
	case !a.allowed(role):
		a.violations = append(a.violations, Violation{Conn: s.Conn, SQL: sql, Reason: fmt.Sprintf("as role %q", role)})
	case !scoped:
		a.violations = append(a.violations, Violation{Conn: s.Conn, SQL: sql, Reason: "before the scope was set"})
	}
}

func (a *Audit) allowed(role string) bool {
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// param is the bound value of $n, or "" when it isn't bound.
func param(s Statement, n string) string {
	i, err := strconv.Atoi(n)
	if err != nil || i < 1 || i > len(s.Params) {
		return ""
	}
	return string(s.Params[i-1])
}

// splitSimple splits a simple query's text on semicolons outside quotes;
// an extended statement is always one.
func splitSimple(s Statement) []string {
	if s.Params != nil || !strings.Contains(s.SQL, ";") {
		return []string{s.SQL}
	}
	var out []string
	var b strings.Builder
	quote := rune(0)
	for _, r := range s.SQL {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ';':
			if strings.TrimSpace(b.String()) != "" {
				out = append(out, b.String())
			}
			b.Reset()
			continue
		}
		b.WriteRune(r)
	}
	if strings.TrimSpace(b.String()) != "" {
		out = append(out, b.String())
	}
	return out
}

// stripComments drops leading "-- …" lines.
func stripComments(sql string) string {
	s := strings.TrimSpace(sql)
	for strings.HasPrefix(s, "--") {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return ""
		}
		s = strings.TrimSpace(s[i+1:])
	}
	return s
}

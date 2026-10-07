// Package v2ui decides, per person, whether the web app shows them the V2
// Ledger UI or V1 (plan 25 E1: the V2 UI lives in packages/web "behind a
// per-user ui=v2 flag"). This is the one place that decides it: the API
// answers it on /v1/auth/me and with a web sign-in's tokens, and the web
// app's server keeps its memax_ui routing cookie in step with the answer.
// The cookie is only a hint for the web's proxy; this is the truth.
//
// The V2 UI is on for a person when any of these holds:
//
//   - they are a member of a space on the V2 record (hubs.v2_enabled_at
//     set: a space made by POST /v2/spaces, or one switched to V2), so
//     everyone who ran `memax init` gets V2 with nothing more to do;
//   - an operator turned it on for them;
//   - their account was created at or after V2_UI_SINCE (unset: never), for
//     when the founders want new signups on V2.
//
// An operator can also turn it off for a person, which wins over every
// rule, as an escape hatch. Only the operator's choice is stored
// (users.v2_ui, migration 055); the rest is read live, so switching a space
// to V2 turns the flag on, and switching back off (unless another space or
// rule keeps it on).
package v2ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UI is which web UI a person sees.
type UI string

const (
	V1 UI = "v1"
	V2 UI = "v2"
)

// Setting is an operator's choice for one person.
type Setting string

const (
	// Default lets the rules decide.
	Default Setting = "default"
	// On turns the V2 UI on.
	On Setting = "on"
	// Off turns it off, whatever the rules say.
	Off Setting = "off"
)

// ParseSetting reads "on", "off" or "default".
func ParseSetting(s string) (Setting, error) {
	switch Setting(strings.ToLower(strings.TrimSpace(s))) {
	case On:
		return On, nil
	case Off:
		return Off, nil
	case Default:
		return Default, nil
	}
	return "", fmt.Errorf("v2ui: %q is not on, off or default", s)
}

// Reason says which rule decided.
type Reason string

const (
	ReasonOperatorOff Reason = "operator_off"
	ReasonOperatorOn  Reason = "operator_on"
	ReasonV2Space     Reason = "v2_space"
	ReasonSignedUp    Reason = "signed_up_since"
	ReasonNone        Reason = "none"
)

// Facts are what the decision reads about a person.
type Facts struct {
	Setting Setting
	// V2Space: they own or are a member of a space on the V2 record.
	V2Space bool
	// CreatedAt is when their account was created.
	CreatedAt time.Time
}

// Decision is the answer, and why.
type Decision struct {
	UI      UI      `json:"ui"`
	Reason  Reason  `json:"reason"`
	Setting Setting `json:"setting"`
}

// Decide is the rule, given the facts and V2_UI_SINCE (zero: never).
func Decide(f Facts, since time.Time) Decision {
	setting := f.Setting
	if setting == "" {
		setting = Default
	}
	d := Decision{UI: V2, Setting: setting}
	switch {
	case setting == Off:
		d.UI, d.Reason = V1, ReasonOperatorOff
	case setting == On:
		d.Reason = ReasonOperatorOn
	case f.V2Space:
		d.Reason = ReasonV2Space
	case !since.IsZero() && !f.CreatedAt.IsZero() && !f.CreatedAt.Before(since):
		d.Reason = ReasonSignedUp
	default:
		d.UI, d.Reason = V1, ReasonNone
	}
	return d
}

// SinceFromEnv reads V2_UI_SINCE, an RFC 3339 time: accounts created at or
// after it get the V2 UI. Unset is the zero time (never); a value that
// doesn't parse is an error, and the caller should treat it as unset.
func SinceFromEnv(getenv func(string) string) (time.Time, error) {
	raw := strings.TrimSpace(getenv("V2_UI_SINCE"))
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("v2ui: V2_UI_SINCE %q is not an RFC 3339 time, like 2026-11-01T00:00:00Z", raw)
	}
	return t, nil
}

// FactsColumns reads a person's Facts from public.users aliased u, for a
// query that already reads their row (/v1/auth/me), so the flag costs no
// round trip of its own. Scan them into a Row's Dest. Membership is
// ownership or a hub_members row, as everywhere else that asks.
const FactsColumns = `u.v2_ui, u.created_at, EXISTS (
	SELECT 1 FROM public.hubs h
	 WHERE h.v2_enabled_at IS NOT NULL
	   AND (h.owner_id = u.id
	        OR EXISTS (SELECT 1 FROM public.hub_members m WHERE m.hub_id = h.id AND m.user_id = u.id)))`

// Row receives FactsColumns.
type Row struct {
	setting   *bool
	createdAt time.Time
	v2Space   bool
}

// Dest is where FactsColumns scan to, in order.
func (r *Row) Dest() []any { return []any{&r.setting, &r.createdAt, &r.v2Space} }

// Facts are the scanned facts.
func (r *Row) Facts() Facts {
	return Facts{Setting: settingOf(r.setting), V2Space: r.v2Space, CreatedAt: r.createdAt}
}

func settingOf(v *bool) Setting {
	switch {
	case v == nil:
		return Default
	case *v:
		return On
	default:
		return Off
	}
}

func columnOf(s Setting) *bool {
	switch s {
	case On:
		v := true
		return &v
	case Off:
		v := false
		return &v
	}
	return nil
}

// DB is what the resolver needs from a pgx pool (or a transaction).
type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ErrNoPerson: no account has that id or email.
var ErrNoPerson = errors.New("v2ui: no such person")

// Resolver reads the facts and applies the rule with V2_UI_SINCE. A nil
// *Resolver (no database) answers V1 for everyone.
type Resolver struct {
	db    DB
	since time.Time
}

// New returns a Resolver on db with V2_UI_SINCE (zero: never), or nil when
// db is nil.
func New(db DB, since time.Time) *Resolver {
	if db == nil {
		return nil
	}
	return &Resolver{db: db, since: since}
}

// Since is V2_UI_SINCE (zero: never).
func (r *Resolver) Since() time.Time {
	if r == nil {
		return time.Time{}
	}
	return r.since
}

// Decide applies the rule to facts already read (FactsColumns).
func (r *Resolver) Decide(f Facts) Decision {
	if r == nil {
		return Decision{UI: V1, Reason: ReasonNone, Setting: f.Setting}
	}
	return Decide(f, r.since)
}

// For decides for one person.
func (r *Resolver) For(ctx context.Context, person uuid.UUID) (Decision, error) {
	if r == nil {
		return Decision{UI: V1, Reason: ReasonNone, Setting: Default}, nil
	}
	var row Row
	err := r.db.QueryRow(ctx, `SELECT `+FactsColumns+` FROM public.users u WHERE u.id = $1`, person).Scan(row.Dest()...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Decision{}, ErrNoPerson
	}
	if err != nil {
		return Decision{}, fmt.Errorf("v2ui: read: %w", err)
	}
	return r.Decide(row.Facts()), nil
}

// Via says where an operator's change came from, for the audit row.
type Via string

const (
	ViaAdmin Via = "admin"   // the admin panel, /v1/admin/users/{id}/v2-ui
	ViaCmd   Via = "cmd"     // cmd/v2-ui, with the database's credentials
	ViaTest  Via = "testing" // tests
)

// Set records an operator's choice for a person, with an admin_audit row
// (resource_type user, action v2_ui) in the same statement, and answers
// the decision as it now stands. actor is the operator (uuid.Nil when
// unknown, as from cmd/v2-ui).
func (r *Resolver) Set(ctx context.Context, person uuid.UUID, s Setting, actor uuid.UUID, via Via) (Decision, error) {
	if r == nil {
		return Decision{}, errors.New("v2ui: no database")
	}
	if _, err := ParseSetting(string(s)); err != nil {
		return Decision{}, err
	}
	var actorID *uuid.UUID
	if actor != uuid.Nil {
		actorID = &actor
	}
	// Every part of one statement sees the row as it was, so `previous` is
	// the choice before this one; no row updated, no audit row.
	var changed bool
	err := r.db.QueryRow(ctx, `
		WITH prev AS (
			SELECT CASE WHEN v2_ui IS NULL THEN 'default' WHEN v2_ui THEN 'on' ELSE 'off' END AS setting
			  FROM public.users WHERE id = $1
		), upd AS (
			UPDATE public.users SET v2_ui = $2 WHERE id = $1 RETURNING id
		), audit AS (
			INSERT INTO public.admin_audit (resource_type, resource_id, action, actor_id, metadata)
			SELECT 'user', upd.id::text, 'v2_ui', $3,
			       jsonb_build_object('setting', $4::text, 'previous', (SELECT setting FROM prev), 'via', $5::text)
			  FROM upd
			RETURNING 1
		)
		SELECT EXISTS (SELECT 1 FROM audit)`,
		person, columnOf(s), actorID, string(s), string(via)).Scan(&changed)
	if err != nil {
		return Decision{}, fmt.Errorf("v2ui: set: %w", err)
	}
	if !changed {
		return Decision{}, ErrNoPerson
	}
	return r.For(ctx, person)
}

// PersonByEmail finds an account by its email (any case; emails are unique
// that way, idx_users_email_unique), for cmd/v2-ui.
func (r *Resolver) PersonByEmail(ctx context.Context, email string) (uuid.UUID, error) {
	if r == nil {
		return uuid.Nil, errors.New("v2ui: no database")
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return uuid.Nil, ErrNoPerson
	}
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT id FROM public.users WHERE lower(btrim(email)) = lower($1) AND btrim(email) <> ''`,
		email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNoPerson
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("v2ui: find %q: %w", email, err)
	}
	return id, nil
}

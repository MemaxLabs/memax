package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A person's notification preferences (migration 049; plan 25 §9, epic
// 2.6; the handoff's Notifications board): whether each event reaches
// them by email, and the quiet hours, in their own time zone, during which
// email waits. Like Dream's settings they are a person's preferences, not
// the record: no receipts, and RLS on app.person_id.
//
// One copy of each choice: the morning edition's email is
// v2.dream_settings.morning_email, which Dream's sweep reads and the
// email's one-click unsubscribe turns off, and the time zone is
// dream_settings' too. These functions read and write them there.

// NotificationEvent is something Memax can tell a person about.
type NotificationEvent string

// The events, in the board's order, then the two the board doesn't draw.
const (
	// NotifyDecisionGate: an agent asks the person to decide (a decision
	// gate, or a question in a handoff).
	NotifyDecisionGate NotificationEvent = "decision_gate"
	// NotifyMorningEdition: what Dream changed overnight. Its email is
	// v2.dream_settings.morning_email.
	NotifyMorningEdition NotificationEvent = "morning_edition"
	// NotifyReviewWaiting: proposals have waited ReviewAfterDays days; once
	// a day, never per proposal.
	NotifyReviewWaiting NotificationEvent = "review_waiting"
	// NotifyDrift: someone edited a file Memax writes.
	NotifyDrift NotificationEvent = "drift"
	// NotifyStale: something the person kept went stale (its source changed).
	NotifyStale NotificationEvent = "stale"
	// NotifyWriteHeld: a write was refused (no receipt) or held (content
	// from outside, quarantined).
	NotifyWriteHeld NotificationEvent = "write_held"
	// NotifyWeeklySummary: Mondays, what was kept, rejected, forgotten and
	// read. Email only: it has no place in the app.
	NotifyWeeklySummary NotificationEvent = "weekly_summary"
	// NotifyForgetDone: a Forget finished: every file rewritten, every agent
	// told.
	NotifyForgetDone NotificationEvent = "forget_done"
	// NotifyAgentChanged: an agent was connected to one of the person's
	// spaces, or its autonomy changed, or it was paused or disconnected.
	NotifyAgentChanged NotificationEvent = "agent_changed"
)

// notificationEvent is what an event does when the person hasn't chosen.
type notificationEvent struct {
	event NotificationEvent
	// inApp: it always shows in the app (Today, Review, Activity, the
	// memory's page); that isn't a choice.
	inApp bool
	// email is the default.
	email bool
}

var notificationEvents = []notificationEvent{
	{NotifyDecisionGate, true, true},
	{NotifyMorningEdition, true, true},
	{NotifyReviewWaiting, true, true},
	{NotifyDrift, true, false},
	{NotifyStale, true, false},
	{NotifyWriteHeld, true, true},
	{NotifyWeeklySummary, false, true},
	{NotifyForgetDone, true, false},
	{NotifyAgentChanged, true, true},
}

// NotificationEvents lists every event, in the order the page shows them.
var NotificationEvents = func() []NotificationEvent {
	out := make([]NotificationEvent, len(notificationEvents))
	for i, e := range notificationEvents {
		out[i] = e.event
	}
	return out
}()

// NotificationChoice is how one event reaches the person.
type NotificationChoice struct {
	Event NotificationEvent `json:"event"`
	// InApp: it always shows in the app; not a choice.
	InApp bool `json:"in_app"`
	// Email is the person's choice (or the default).
	Email bool `json:"email"`
	// EmailSent says whether Memax sends this email today. A choice for
	// one it doesn't send yet is kept and followed once it does. The API
	// sets it from what the deployment delivers (v2api.WithNotifications).
	EmailSent bool `json:"email_sent"`
}

// QuietHours are when email waits, in the person's time zone: From until
// Until, across midnight when From is later. Decision gates still come
// through when GatesThrough is set.
type QuietHours struct {
	On           bool   `json:"on"`
	From         string `json:"from"`
	Until        string `json:"until"`
	GatesThrough bool   `json:"gates_through"`
}

// NotificationSettings are a person's notification preferences.
type NotificationSettings struct {
	// Version is the ETag an edit sends as If-Match. A person who never
	// changed anything is version 1.
	Version int `json:"version"`
	// TimeZone and TimeZoneSource are Dream's (DreamSettings): the quiet
	// hours are read in it.
	TimeZone       string               `json:"time_zone"`
	TimeZoneSource string               `json:"time_zone_source"`
	Events         []NotificationChoice `json:"events"`
	QuietHours     QuietHours           `json:"quiet_hours"`
	// ReviewAfterDays is how long a proposal waits before the daily Review
	// reminder (NotifyReviewWaiting).
	ReviewAfterDays int        `json:"review_after_days"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

// Event returns the choice for one event.
func (s NotificationSettings) Event(e NotificationEvent) (NotificationChoice, bool) {
	for _, c := range s.Events {
		if c.Event == e {
			return c, true
		}
	}
	return NotificationChoice{}, false
}

// The defaults, equal to migration 049's column defaults
// (TestNotificationDefaultsMatchTheSchema).
const (
	DefaultQuietFrom       = "20:00"
	DefaultQuietUntil      = "08:00"
	DefaultReviewAfterDays = 1
	// MaxReviewAfterDays bounds the Review reminder's wait.
	MaxReviewAfterDays = 14
)

// DefaultNotificationSettings are a person's settings before they change
// any: version 1, UTC until Memax learns their zone.
func DefaultNotificationSettings() NotificationSettings {
	s := NotificationSettings{
		Version: 1, TimeZone: "UTC", TimeZoneSource: ZoneDefault,
		QuietHours:      QuietHours{On: true, From: DefaultQuietFrom, Until: DefaultQuietUntil, GatesThrough: true},
		ReviewAfterDays: DefaultReviewAfterDays,
	}
	for _, e := range notificationEvents {
		s.Events = append(s.Events, NotificationChoice{Event: e.event, InApp: e.inApp, Email: e.email})
	}
	return s
}

// NotificationChange is an edit to a person's settings; a nil field (or an
// event missing from Email) stays as it is.
type NotificationChange struct {
	Email           map[NotificationEvent]bool `json:"email,omitempty"`
	QuietOn         *bool                      `json:"quiet_on,omitempty"`
	QuietFrom       *string                    `json:"quiet_from,omitempty"`
	QuietUntil      *string                    `json:"quiet_until,omitempty"`
	GatesThrough    *bool                      `json:"gates_through,omitempty"`
	ReviewAfterDays *int                       `json:"review_after_days,omitempty"`
}

func (c NotificationChange) empty() bool {
	return len(c.Email) == 0 && c.QuietOn == nil && c.QuietFrom == nil && c.QuietUntil == nil &&
		c.GatesThrough == nil && c.ReviewAfterDays == nil
}

// clockRE is a 24-hour time of day, "08:00".
var clockRE = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func (c NotificationChange) validate() error {
	if c.empty() {
		return invalid("body", "change at least one setting")
	}
	for e := range c.Email {
		if !slices.Contains(NotificationEvents, e) {
			return invalid("events", "%q isn't an event Memax notifies about", e)
		}
	}
	for field, v := range map[string]*string{"quiet_hours.from": c.QuietFrom, "quiet_hours.until": c.QuietUntil} {
		if v != nil && !clockRE.MatchString(*v) {
			return invalid(field, "use a 24-hour time such as 08:00")
		}
	}
	if d := c.ReviewAfterDays; d != nil && (*d < 1 || *d > MaxReviewAfterDays) {
		return invalid("review_after_days", "use 1 to %d days", MaxReviewAfterDays)
	}
	return nil
}

// hash is the request's fingerprint for its Idempotency-Key.
func (c NotificationChange) hash() []byte {
	b, _ := json.Marshal(c) // map keys marshal sorted
	sum := sha256.Sum256(append([]byte("notification_settings\x00"), b...))
	return sum[:]
}

// notificationRow is the stored row, as read (nil: no row).
type notificationRow struct {
	version         int
	email           map[NotificationEvent]bool
	quietOn         bool
	quietFrom       string
	quietUntil      string
	gatesThrough    bool
	reviewAfterDays int
	updatedAt       time.Time
	lastKey         *string
	lastKeyHash     []byte
}

// readNotificationSettings reads the person's settings in tx, with Dream's
// zone and morning email.
func readNotificationSettings(ctx context.Context, tx pgx.Tx, person uuid.UUID) (NotificationSettings, *notificationRow, error) {
	const q = `
		SELECT n.person_id IS NOT NULL, n.version, n.email, n.quiet_on, left(n.quiet_from::text, 5),
		       left(n.quiet_until::text, 5), n.gates_through, n.review_after_days, n.updated_at, n.last_key,
		       n.last_key_hash, d.time_zone, d.time_zone_source, d.morning_email
		  FROM (SELECT $1::uuid AS id) p
		  LEFT JOIN v2.notification_settings n ON n.person_id = p.id
		  LEFT JOIN v2.dream_settings d ON d.person_id = p.id`
	var (
		found                         bool
		version, days                 *int
		emailJSON                     []byte
		quietOn, gates, morning       *bool
		from, until, zone, zoneSource *string
		updated                       *time.Time
		row                           notificationRow
	)
	if err := tx.QueryRow(ctx, q, person).Scan(&found, &version, &emailJSON, &quietOn, &from, &until, &gates, &days,
		&updated, &row.lastKey, &row.lastKeyHash, &zone, &zoneSource, &morning); err != nil {
		return NotificationSettings{}, nil, fmt.Errorf("ledger: notification settings: %w", err)
	}
	s := DefaultNotificationSettings()
	if zone != nil {
		s.TimeZone, s.TimeZoneSource = *zone, *zoneSource
	}
	morningEmail := morning == nil || *morning
	if !found {
		s.Events = applyEmail(s.Events, nil, morningEmail)
		return s, nil, nil
	}
	row.version, row.quietOn, row.quietFrom, row.quietUntil = *version, *quietOn, *from, *until
	row.gatesThrough, row.reviewAfterDays, row.updatedAt = *gates, *days, *updated
	if err := json.Unmarshal(emailJSON, &row.email); err != nil {
		return NotificationSettings{}, nil, fmt.Errorf("ledger: notification settings: email: %w", err)
	}
	s.Version = row.version
	s.Events = applyEmail(s.Events, row.email, morningEmail)
	s.QuietHours = QuietHours{On: row.quietOn, From: row.quietFrom, Until: row.quietUntil, GatesThrough: row.gatesThrough}
	s.ReviewAfterDays = row.reviewAfterDays
	at := row.updatedAt.UTC()
	s.UpdatedAt = &at
	return s, &row, nil
}

// applyEmail sets each event's email from the person's choices, the
// morning edition's from Dream's settings.
func applyEmail(events []NotificationChoice, chosen map[NotificationEvent]bool, morning bool) []NotificationChoice {
	for i := range events {
		if events[i].Event == NotifyMorningEdition {
			events[i].Email = morning
			continue
		}
		if v, ok := chosen[events[i].Event]; ok {
			events[i].Email = v
		}
	}
	return events
}

// GetNotificationSettings reads the scope's person's settings (the
// defaults when they have none).
func (l *Ledger) GetNotificationSettings(ctx context.Context, scope Scope) (NotificationSettings, error) {
	if l == nil {
		return NotificationSettings{}, ErrDisabled
	}
	if scope.PersonID == uuid.Nil {
		return NotificationSettings{}, ErrNotFound
	}
	var out NotificationSettings
	err := l.Read(ctx, Scope{PersonID: scope.PersonID}, func(tx pgx.Tx) error {
		s, _, err := readNotificationSettings(ctx, tx, scope.PersonID)
		out = s
		return err
	})
	return out, err
}

// NotificationSettingsRef names the settings in an edit clash.
const NotificationSettingsRef = "notification settings"

// UpdateNotificationSettings applies a person's edit when ifMatch is the
// version they read (else *EditClashError). key is the request's
// Idempotency-Key: the same key with the same change replays (replayed,
// the settings as they are now) instead of clashing with the version its
// first try wrote; with a different change it is ErrIdempotencyKeyReused.
// The morning edition's email goes to v2.dream_settings, whose trigger
// moves the version too.
func (l *Ledger) UpdateNotificationSettings(ctx context.Context, scope Scope, ch NotificationChange, ifMatch int,
	key string) (out NotificationSettings, replayed bool, err error) {
	if l == nil {
		return NotificationSettings{}, false, ErrDisabled
	}
	if scope.PersonID == uuid.Nil {
		return NotificationSettings{}, false, ErrNotFound
	}
	if err := ch.validate(); err != nil {
		return NotificationSettings{}, false, err
	}
	hash := ch.hash()
	person := scope.PersonID
	err = l.writePerson(ctx, scope, func(tx pgx.Tx) error {
		// Make the row (version 1 is what a person without one reads as) or
		// lock the one there, in one statement: a no-op upsert locks it.
		if _, err := tx.Exec(ctx, `
			INSERT INTO v2.notification_settings AS n (person_id) VALUES ($1)
			ON CONFLICT (person_id) DO UPDATE SET updated_at = n.updated_at`, person); err != nil {
			return err
		}
		cur, row, err := readNotificationSettings(ctx, tx, person)
		if err != nil {
			return err
		}
		if row == nil {
			return fmt.Errorf("ledger: notification settings: the row wasn't there after making it")
		}
		if row.lastKey != nil && *row.lastKey == key {
			if string(row.lastKeyHash) != string(hash) {
				return ErrIdempotencyKeyReused
			}
			out, replayed = cur, true
			return nil
		}
		if ifMatch != cur.Version {
			return &EditClashError{Ref: NotificationSettingsRef, Expected: ifMatch, Current: cur.Version}
		}
		next := *row
		email := make(map[NotificationEvent]bool, len(row.email))
		for e, v := range row.email {
			email[e] = v
		}
		var morning *bool
		for e, v := range ch.Email {
			if e == NotifyMorningEdition {
				morning = &v
				continue
			}
			email[e] = v
		}
		if ch.QuietOn != nil {
			next.quietOn = *ch.QuietOn
		}
		if ch.QuietFrom != nil {
			next.quietFrom = *ch.QuietFrom
		}
		if ch.QuietUntil != nil {
			next.quietUntil = *ch.QuietUntil
		}
		if next.quietFrom == next.quietUntil {
			return invalid("quiet_hours", "quiet hours need a start and an end that differ")
		}
		if ch.GatesThrough != nil {
			next.gatesThrough = *ch.GatesThrough
		}
		if ch.ReviewAfterDays != nil {
			next.reviewAfterDays = *ch.ReviewAfterDays
		}
		emailJSON, err := json.Marshal(email)
		if err != nil {
			return err
		}
		if morning != nil {
			// Dream's copy; its trigger moves the version when it changes.
			if _, err := tx.Exec(ctx, `
				INSERT INTO v2.dream_settings AS d (person_id, morning_email) VALUES ($1, $2)
				ON CONFLICT (person_id) DO UPDATE SET morning_email = $2, updated_at = now()
				 WHERE d.morning_email IS DISTINCT FROM $2`, person, *morning); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE v2.notification_settings
			   SET version = version + 1, email = $2, quiet_on = $3, quiet_from = ($4::text)::time,
			       quiet_until = ($5::text)::time,
			       gates_through = $6, review_after_days = $7, last_key = $8, last_key_hash = $9, updated_at = now()
			 WHERE person_id = $1`,
			person, emailJSON, next.quietOn, next.quietFrom, next.quietUntil, next.gatesThrough, next.reviewAfterDays,
			key, hash); err != nil {
			return err
		}
		out, _, err = readNotificationSettings(ctx, tx, person)
		return err
	})
	if err != nil {
		return NotificationSettings{}, false, err
	}
	return out, replayed, nil
}

// ---------------------------------------------------------------------
// Quiet hours, for delivery
// ---------------------------------------------------------------------

// clockMinutes reads "08:00" as minutes after midnight.
func clockMinutes(s string) (int, bool) {
	if !clockRE.MatchString(s) {
		return 0, false
	}
	h, _ := strconv.Atoi(s[:2])
	m, _ := strconv.Atoi(s[3:])
	return h*60 + m, true
}

// Wait is how long email to the person waits at now: until the quiet hours
// end, in loc, when now falls inside them; zero otherwise (or when they're
// off).
func (q QuietHours) Wait(now time.Time, loc *time.Location) time.Duration {
	from, ok1 := clockMinutes(q.From)
	until, ok2 := clockMinutes(q.Until)
	if !q.On || !ok1 || !ok2 || from == until {
		return 0
	}
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	at := local.Hour()*60 + local.Minute()
	inside := (from < until && at >= from && at < until) || (from > until && (at >= from || at < until))
	if !inside {
		return 0
	}
	// The next time the clock reads Until, in loc (a day later when Until
	// has passed today: the window crosses midnight).
	end := time.Date(local.Year(), local.Month(), local.Day(), until/60, until%60, 0, 0, loc)
	if !end.After(local) {
		end = time.Date(local.Year(), local.Month(), local.Day()+1, until/60, until%60, 0, 0, loc)
	}
	return end.Sub(now)
}

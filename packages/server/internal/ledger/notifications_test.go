package ledger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// A person without a row reads the defaults, and a row nobody has edited
// (one the morning email's trigger made) reads the same: migration 049's
// column defaults and DefaultNotificationSettings say one thing.
func TestNotificationDefaultsMatchTheSchema(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := f.user("zz")
	want := ledger.DefaultNotificationSettings()
	got, err := f.l.GetNotificationSettings(ctx, ledger.Scope{PersonID: zz})
	if err != nil {
		t.Fatal(err)
	}
	if !sameSettings(got, want) {
		t.Fatalf("without a row:\n got %+v\nwant %+v", got, want)
	}
	f.exec(`INSERT INTO v2.notification_settings (person_id) VALUES ($1)`, zz)
	got, err = f.l.GetNotificationSettings(ctx, ledger.Scope{PersonID: zz})
	if err != nil {
		t.Fatal(err)
	}
	if !sameSettings(got, want) || got.UpdatedAt == nil {
		t.Fatalf("a bare row:\n got %+v\nwant %+v", got, want)
	}
}

func sameSettings(a, b ledger.NotificationSettings) bool {
	if a.Version != b.Version || a.QuietHours != b.QuietHours || a.ReviewAfterDays != b.ReviewAfterDays ||
		a.TimeZone != b.TimeZone || len(a.Events) != len(b.Events) {
		return false
	}
	for i := range a.Events {
		if a.Events[i] != b.Events[i] {
			return false
		}
	}
	return true
}

// Edits merge, move the version, replay on their own key, clash on a stale
// version, and the morning edition's email is Dream's one setting.
func TestUpdateNotificationSettings(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz := ledger.Scope{PersonID: f.user("zz")}
	on, from, until, days := true, "21:30", "07:00", 3
	s, replayed, err := f.l.UpdateNotificationSettings(ctx, zz, ledger.NotificationChange{
		Email:     map[ledger.NotificationEvent]bool{ledger.NotifyDrift: on, ledger.NotifyMorningEdition: false},
		QuietFrom: &from, QuietUntil: &until, ReviewAfterDays: &days,
	}, 1, "k1")
	if err != nil || replayed {
		t.Fatalf("update: %v (replayed %v)", err, replayed)
	}
	drift, _ := s.Event(ledger.NotifyDrift)
	morning, _ := s.Event(ledger.NotifyMorningEdition)
	stale, _ := s.Event(ledger.NotifyStale)
	if !drift.Email || morning.Email || stale.Email || s.QuietHours.From != "21:30" || s.QuietHours.Until != "07:00" ||
		!s.QuietHours.On || s.ReviewAfterDays != 3 || s.Version < 2 {
		t.Fatalf("after the edit: %+v", s)
	}
	ds, err := f.l.GetDreamSettings(ctx, zz)
	if err != nil || ds.MorningEmail {
		t.Fatalf("Dream's morning email = %v (%v), want off: one setting", ds.MorningEmail, err)
	}
	// Its retry replays rather than clashing with the version it wrote.
	again, replayed, err := f.l.UpdateNotificationSettings(ctx, zz, ledger.NotificationChange{
		Email:     map[ledger.NotificationEvent]bool{ledger.NotifyDrift: on, ledger.NotifyMorningEdition: false},
		QuietFrom: &from, QuietUntil: &until, ReviewAfterDays: &days,
	}, 1, "k1")
	if err != nil || !replayed || again.Version != s.Version {
		t.Fatalf("retry: %v replayed=%v version %d, want %d", err, replayed, again.Version, s.Version)
	}
	// The same key for another change is refused.
	if _, _, err := f.l.UpdateNotificationSettings(ctx, zz, ledger.NotificationChange{ReviewAfterDays: &days}, 1, "k1"); !errors.Is(err, ledger.ErrIdempotencyKeyReused) {
		t.Fatalf("reused key: %v", err)
	}
	// A stale version clashes.
	var clash *ledger.EditClashError
	if _, _, err := f.l.UpdateNotificationSettings(ctx, zz, ledger.NotificationChange{ReviewAfterDays: &days}, 1, "k2"); !errors.As(err, &clash) ||
		clash.Expected != 1 || clash.Current != s.Version {
		t.Fatalf("stale version: %v", err)
	}
	// Turning the email back on through Dream's settings moves the version.
	yes := true
	if _, err := f.l.UpdateDreamSettings(ctx, zz, nil, &yes); err != nil {
		t.Fatal(err)
	}
	after, err := f.l.GetNotificationSettings(ctx, zz)
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := after.Event(ledger.NotifyMorningEdition); !m.Email || after.Version <= s.Version {
		t.Fatalf("after Dream's settings turned it on: email %v, version %d (was %d)", m.Email, after.Version, s.Version)
	}
	// Invalid changes say which field.
	same := "07:00"
	for name, ch := range map[string]ledger.NotificationChange{
		"nothing":       {},
		"unknown event": {Email: map[ledger.NotificationEvent]bool{"party": true}},
		"bad time":      {QuietFrom: ptrTo("7am")},
		"same times":    {QuietFrom: &same},
		"too many days": {ReviewAfterDays: ptrTo(30)},
	} {
		if _, _, err := f.l.UpdateNotificationSettings(ctx, zz, ch, after.Version, "bad-"+name); !errors.Is(err, ledger.ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
}

func ptrTo[T any](v T) *T { return &v }

// RLS on the person: memax_v2 scoped to one person reads, changes and
// makes no other person's row.
func TestNotificationSettingsAreThePersons(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	zz, jy := f.user("zz"), f.user("jy")
	days := 5
	if _, _, err := f.l.UpdateNotificationSettings(ctx, ledger.Scope{PersonID: zz},
		ledger.NotificationChange{ReviewAfterDays: &days}, 1, "zz-1"); err != nil {
		t.Fatal(err)
	}
	asJY := func(fn func(tx pgx.Tx) error) error {
		return f.asV2(nil, nil, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT set_config('app.person_id', $1, true)`, jy.String()); err != nil {
				return err
			}
			return fn(tx)
		})
	}
	if err := asJY(func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM v2.notification_settings`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("jy sees %d rows of notification settings, want none (zz's is there)", n)
		}
		tag, err := tx.Exec(ctx, `UPDATE v2.notification_settings SET review_after_days = 1 WHERE person_id = $1`, zz)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 0 {
			t.Errorf("jy changed zz's settings")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err := asJY(func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO v2.notification_settings (person_id) VALUES ($1)`, uuid.New())
		return err
	})
	if sqlstate(err) != "42501" {
		t.Errorf("jy made a row for someone else: %v, want an RLS refusal", err)
	}
	got, err := f.l.GetNotificationSettings(ctx, ledger.Scope{PersonID: jy})
	if err != nil || got.ReviewAfterDays != ledger.DefaultReviewAfterDays || got.Version != 1 {
		t.Errorf("jy's settings = %+v (%v), want the defaults", got, err)
	}
}

// Quiet hours hold email until they end, in the person's zone, across
// midnight and a change of clocks.
func TestQuietHoursWait(t *testing.T) {
	t.Parallel()
	van, err := time.LoadLocation("America/Vancouver")
	if err != nil {
		t.Fatal(err)
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	night := ledger.QuietHours{On: true, From: "20:00", Until: "08:00"}
	afternoon := ledger.QuietHours{On: true, From: "13:00", Until: "14:30"}
	for _, tc := range []struct {
		name  string
		q     ledger.QuietHours
		local time.Time
		want  time.Duration
	}{
		{"before midnight", night, time.Date(2026, 10, 5, 22, 15, 0, 0, van), 9*time.Hour + 45*time.Minute},
		{"after midnight", night, time.Date(2026, 10, 6, 3, 12, 0, 0, van), 4*time.Hour + 48*time.Minute},
		{"at the start", night, time.Date(2026, 10, 6, 20, 0, 0, 0, van), 12 * time.Hour},
		{"at the end", night, time.Date(2026, 10, 6, 8, 0, 0, 0, van), 0},
		{"daytime", night, time.Date(2026, 10, 6, 12, 0, 0, 0, van), 0},
		{"inside a daytime window", afternoon, time.Date(2026, 10, 6, 13, 30, 0, 0, van), time.Hour},
		{"outside a daytime window", afternoon, time.Date(2026, 10, 6, 15, 0, 0, 0, van), 0},
		{"off", ledger.QuietHours{From: "20:00", Until: "08:00"}, time.Date(2026, 10, 6, 3, 0, 0, 0, van), 0},
		// Berlin's clocks go back an hour on Oct 25, 2026: the night is an
		// hour longer, and the wait with it.
		{"the night the clocks go back", night, time.Date(2026, 10, 25, 0, 30, 0, 0, berlin), 8*time.Hour + 30*time.Minute},
	} {
		loc := tc.local.Location()
		if got := tc.q.Wait(tc.local.UTC(), loc); got != tc.want {
			t.Errorf("%s: wait %v, want %v", tc.name, got, tc.want)
		}
	}
}

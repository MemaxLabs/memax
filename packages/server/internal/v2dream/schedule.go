package v2dream

import (
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

// Dream runs in its space owner's local night, in their own time zone,
// whatever that is. Never assume Pacific: tzdata 2026c keeps British
// Columbia on UTC−7 from Nov 1, 2026 while the rest of Pacific Time goes
// back to UTC−8, so "Vancouver" and "Los Angeles" differ by an hour all
// winter. Slots are computed with time.Date in the owner's location, which
// carries every zone's own rules (DST included); an unknown zone means UTC
// until Memax learns theirs.

// location loads an IANA zone, or UTC.
func location(zone string) *time.Location {
	if zone == "" || zone == "Local" {
		return time.UTC
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// slotOn is the slot on the day `day` days after at's date in loc, or ok
// false when a weekly cadence doesn't run that day.
func slotOn(at time.Time, loc *time.Location, day int, c Cadence, hour int, weekday time.Weekday) (time.Time, bool) {
	local := at.In(loc)
	t := time.Date(local.Year(), local.Month(), local.Day()+day, hour, 0, 0, 0, loc)
	if c == Weekly && t.Weekday() != weekday {
		return time.Time{}, false
	}
	return t, true
}

// NextSlot is the first slot strictly after `after`: the next local night
// at `hour`, or for a weekly cadence the next such night on `weekday`.
func NextSlot(after time.Time, zone string, c Cadence, hour int, weekday time.Weekday) time.Time {
	loc := location(zone)
	for d := 0; d <= 8; d++ {
		if t, ok := slotOn(after, loc, d, c, hour, weekday); ok && t.After(after) {
			return t.UTC()
		}
	}
	return after.Add(24 * time.Hour).UTC() // unreachable: a week holds every weekday
}

// LatestSlot is the last slot at or before `at`.
func LatestSlot(at time.Time, zone string, c Cadence, hour int, weekday time.Weekday) time.Time {
	loc := location(zone)
	for d := 0; d >= -8; d-- {
		if t, ok := slotOn(at, loc, d, c, hour, weekday); ok && !t.After(at) {
			return t.UTC()
		}
	}
	return at.Add(-24 * time.Hour).UTC()
}

// cadenceFor is a space's cadence on its owner's plan: weekly on Free;
// nightly on Pro for the owner's busiest ProSpaces spaces, weekly for the
// rest (D9).
func (c Config) cadenceFor(plan Plan, busyRank int) Cadence {
	if plan == PlanFree {
		return c.FreeCadence
	}
	if busyRank >= 1 && busyRank <= c.ProSpaces {
		return c.ProCadence
	}
	return Weekly
}

// Decide is the sweep's decision for one space (ledger.DreamSweep): a new
// space is scheduled for its owner's next local night; a due one runs for
// the latest slot that came due (once, however many nights it missed) and
// is scheduled for the next; a space whose owner or zone changed is moved
// to the right night.
func (c Config) Decide(now time.Time, s ledger.SweepSpace) ledger.SweepDecision {
	zone := s.TimeZone
	if location(zone) == time.UTC && zone != "UTC" {
		zone = "UTC"
	}
	cadence := c.cadenceFor(c.Plan, s.BusyRank)
	d := ledger.SweepDecision{Cadence: string(cadence), TimeZone: zone,
		DueAt: NextSlot(now, zone, cadence, c.LocalHour, c.WeeklyDay)}
	if s.HasSchedule && !s.DueAt.After(now) {
		slot := LatestSlot(now, zone, cadence, c.LocalHour, c.WeeklyDay)
		// The slot that came due under the old zone or cadence, if the
		// latest one under the new is before it was even due.
		if slot.Before(s.DueAt) && !s.DueAt.After(now) {
			slot = s.DueAt.UTC()
		}
		if s.LastSlot == nil || slot.After(*s.LastSlot) {
			d.Slot = &slot
		}
	}
	return d
}

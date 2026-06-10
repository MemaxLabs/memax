package intent

import (
	"testing"
	"time"
)

func TestExtractTemporalBounds(t *testing.T) {
	// Fixed "now" for deterministic tests: Friday, April 11, 2026 14:30:00 UTC
	now := time.Date(2026, 4, 11, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name      string
		query     string
		wantStart time.Time
		wantEnd   time.Time
		wantOK    bool
	}{
		// Relative expressions
		{
			name:      "yesterday",
			query:     "what did I do yesterday",
			wantStart: time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "today",
			query:     "what happened today",
			wantStart: time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "this week",
			query:     "meetings this week",
			wantStart: time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC), // Monday
			wantEnd:   time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "last week",
			query:     "what happened last week",
			wantStart: time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC), // prev Monday
			wantEnd:   time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "this month",
			query:     "decisions this month",
			wantStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "last month",
			query:     "notes from last month",
			wantStart: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "last tuesday",
			query:     "what did I do last tuesday",
			wantStart: time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), // Tue Apr 7
			wantEnd:   time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "last friday (today is Saturday, last Fri = yesterday)",
			query:     "last friday notes",
			wantStart: time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC), // Sat -> most recent Fri = Apr 10
			wantEnd:   time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "last saturday (same weekday goes back a week)",
			query:     "last saturday party",
			wantStart: time.Date(2026, 4, 4, 0, 0, 0, 0, time.UTC), // today IS Sat, so last Sat = Apr 4
			wantEnd:   time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},

		// Absolute dates
		{
			name:      "ISO date",
			query:     "notes from 2026-04-04",
			wantStart: time.Date(2026, 4, 4, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "month day ordinal",
			query:     "what did I do on april 4th",
			wantStart: time.Date(2026, 4, 4, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "month day no ordinal",
			query:     "notes from march 28",
			wantStart: time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "month day with year",
			query:     "January 15, 2025 meeting",
			wantStart: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2025, 1, 16, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "future month defaults to last year",
			query:     "december 25 party notes",
			wantStart: time.Date(2025, 12, 25, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2025, 12, 26, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "day of month",
			query:     "the 4th of april notes",
			wantStart: time.Date(2026, 4, 4, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},
		{
			name:      "on weekday",
			query:     "meeting on monday",
			wantStart: time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC), // most recent Monday
			wantEnd:   time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC),
			wantOK:    true,
		},

		// No temporal expression
		{
			name:   "no temporal",
			query:  "what is kubernetes",
			wantOK: false,
		},
		{
			name:   "no temporal semantic",
			query:  "how to deploy to production",
			wantOK: false,
		},
		{
			name:   "month name in non-date context",
			query:  "the march of progress in software",
			wantOK: false, // "march" alone without a day number should not match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, ok := extractTemporalBounds(tt.query, now)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if !start.Equal(tt.wantStart) {
				t.Errorf("start = %v, want %v", start, tt.wantStart)
			}
			if !end.Equal(tt.wantEnd) {
				t.Errorf("end = %v, want %v", end, tt.wantEnd)
			}
		})
	}
}

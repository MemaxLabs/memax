package intent

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// extractTemporalBounds attempts to extract date/time bounds from a query string.
// Returns day-level bounds [start, end) where end is start + 1 day (or wider for ranges).
// Returns ok=false if no temporal expression is found.
func extractTemporalBounds(query string, now time.Time) (start, end *time.Time, ok bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	today := truncateToDay(now)

	// --- Relative expressions (most common, check first) ---

	if strings.Contains(q, "yesterday") {
		s := today.AddDate(0, 0, -1)
		e := today
		return &s, &e, true
	}
	if strings.Contains(q, "today") {
		e := today.AddDate(0, 0, 1)
		return &today, &e, true
	}
	if strings.Contains(q, "this week") {
		s := mostRecentWeekday(today, time.Monday)
		e := s.AddDate(0, 0, 7)
		return &s, &e, true
	}
	if strings.Contains(q, "last week") {
		s := mostRecentWeekday(today, time.Monday).AddDate(0, 0, -7)
		e := s.AddDate(0, 0, 7)
		return &s, &e, true
	}
	if strings.Contains(q, "this month") {
		s := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
		e := s.AddDate(0, 1, 0)
		return &s, &e, true
	}
	if strings.Contains(q, "last month") {
		s := time.Date(today.Year(), today.Month()-1, 1, 0, 0, 0, 0, today.Location())
		e := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
		return &s, &e, true
	}

	// "last Monday", "last Tuesday", etc.
	if m := lastWeekdayRe.FindStringSubmatch(q); len(m) == 2 {
		if wd, ok := weekdayMap[m[1]]; ok {
			s := mostRecentWeekday(today, wd)
			// If "last X" and today IS that weekday, go back a full week
			if s.Equal(today) {
				s = s.AddDate(0, 0, -7)
			}
			e := s.AddDate(0, 0, 1)
			return &s, &e, true
		}
	}

	// --- ISO date: 2026-04-04 ---
	if m := isoDateRe.FindStringSubmatch(q); len(m) == 4 {
		if s, ok := parseYMD(m[1], m[2], m[3], now); ok {
			e := s.AddDate(0, 0, 1)
			return &s, &e, true
		}
	}

	// --- "April 4th, 2026" or "April 4, 2026" or "April 4th" ---
	if m := monthDayYearRe.FindStringSubmatch(q); len(m) >= 3 {
		monthName := m[1]
		dayStr := stripOrdinal(m[2])
		yearStr := ""
		if len(m) >= 4 {
			yearStr = strings.TrimSpace(m[3])
		}
		if s, ok := parseMonthDayYear(monthName, dayStr, yearStr, now); ok {
			e := s.AddDate(0, 0, 1)
			return &s, &e, true
		}
	}

	// --- "4th of April" or "4 of April 2026" ---
	if m := dayOfMonthRe.FindStringSubmatch(q); len(m) >= 3 {
		dayStr := stripOrdinal(m[1])
		monthName := m[2]
		yearStr := ""
		if len(m) >= 4 {
			yearStr = strings.TrimSpace(m[3])
		}
		if s, ok := parseMonthDayYear(monthName, dayStr, yearStr, now); ok {
			e := s.AddDate(0, 0, 1)
			return &s, &e, true
		}
	}

	// --- "on Monday", "on Tuesday" (most recent occurrence, not "last") ---
	if m := onWeekdayRe.FindStringSubmatch(q); len(m) == 2 {
		if wd, ok := weekdayMap[m[1]]; ok {
			s := mostRecentWeekday(today, wd)
			e := s.AddDate(0, 0, 1)
			return &s, &e, true
		}
	}

	return nil, nil, false
}

// --- Compiled regexes ---

var (
	lastWeekdayRe  = regexp.MustCompile(`\blast\s+(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\b`)
	onWeekdayRe    = regexp.MustCompile(`\bon\s+(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\b`)
	isoDateRe      = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	monthDayYearRe = regexp.MustCompile(`\b(january|february|march|april|may|june|july|august|september|october|november|december)\s+(\d{1,2})(?:st|nd|rd|th)?(?:[,\s]+(\d{4}))?\b`)
	dayOfMonthRe   = regexp.MustCompile(`\b(\d{1,2})(?:st|nd|rd|th)?\s+(?:of\s+)(january|february|march|april|may|june|july|august|september|october|november|december)(?:\s+(\d{4}))?\b`)
	ordinalRe      = regexp.MustCompile(`(st|nd|rd|th)$`)
)

var weekdayMap = map[string]time.Weekday{
	"monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
	"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday, "sunday": time.Sunday,
}

var monthMap = map[string]time.Month{
	"january": time.January, "february": time.February, "march": time.March,
	"april": time.April, "may": time.May, "june": time.June,
	"july": time.July, "august": time.August, "september": time.September,
	"october": time.October, "november": time.November, "december": time.December,
}

// --- Helpers ---

func truncateToDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// mostRecentWeekday returns the most recent date (including today) that falls on the given weekday.
func mostRecentWeekday(from time.Time, wd time.Weekday) time.Time {
	diff := (7 + int(from.Weekday()) - int(wd)) % 7
	return from.AddDate(0, 0, -diff)
}

func stripOrdinal(s string) string {
	return ordinalRe.ReplaceAllString(strings.TrimSpace(s), "")
}

func parseYMD(yearStr, monthStr, dayStr string, _ time.Time) (time.Time, bool) {
	y, err := strconv.Atoi(yearStr)
	if err != nil {
		return time.Time{}, false
	}
	m, err := strconv.Atoi(monthStr)
	if err != nil || m < 1 || m > 12 {
		return time.Time{}, false
	}
	d, err := strconv.Atoi(dayStr)
	if err != nil || d < 1 || d > 31 {
		return time.Time{}, false
	}
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC), true
}

func parseMonthDayYear(monthName, dayStr, yearStr string, now time.Time) (time.Time, bool) {
	month, ok := monthMap[monthName]
	if !ok {
		return time.Time{}, false
	}
	day, err := strconv.Atoi(dayStr)
	if err != nil || day < 1 || day > 31 {
		return time.Time{}, false
	}

	year := now.Year()
	if yearStr != "" {
		y, err := strconv.Atoi(yearStr)
		if err != nil {
			return time.Time{}, false
		}
		year = y
	} else {
		// Assume most recent occurrence: if the date is in the future, use last year
		candidate := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
		if candidate.After(truncateToDay(now).AddDate(0, 0, 1)) {
			year--
		}
	}

	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC), true
}

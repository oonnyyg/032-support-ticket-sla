// Package calendar defines a Beijing working-hours calendar and counts
// working minutes between timestamps.
package calendar

import (
	"fmt"
	"time"
)

// DayStart is the start of a working day (09:00).
const DayStart = 9

// DayEnd is the end of a working day (18:00).
const DayEnd = 18

// Calendar is an immutable working-hours calendar for the Asia/Shanghai zone.
type Calendar struct {
	loc      *time.Location
	holidays map[string]bool
}

// Location returns the calendar time zone (Asia/Shanghai).
func (c *Calendar) Location() *time.Location { return c.loc }

// New creates a calendar. holidays are dates formatted "YYYY-MM-DD" in
// Beijing time; each listed date is treated as a full rest day.
func New(holidays []string) (*Calendar, error) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return nil, fmt.Errorf("load location: %w", err)
	}
	set := make(map[string]bool, len(holidays))
	for _, d := range holidays {
		if _, err := time.ParseInLocation("2006-01-02", d, loc); err != nil {
			return nil, fmt.Errorf("invalid holiday %q: want YYYY-MM-DD", d)
		}
		if set[d] {
			return nil, fmt.Errorf("duplicate holiday %q", d)
		}
		set[d] = true
	}
	return &Calendar{loc: loc, holidays: set}, nil
}

// Holidays returns the configured rest dates, sorted.
func (c *Calendar) Holidays() []string {
	out := make([]string, 0, len(c.holidays))
	for d := range c.holidays {
		out = append(out, d)
	}
	sortStrings(out)
	return out
}

// IsWorkday reports whether the calendar date of t is a Monday-to-Friday
// day that is not a configured holiday.
func (c *Calendar) IsWorkday(t time.Time) bool {
	t = t.In(c.loc)
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return false
	}
	return !c.holidays[t.Format("2006-01-02")]
}

// WorkMinutesBetween counts whole minutes of the half-open interval
// [start, end) that fall on working days between DayStart and DayEnd.
// Both timestamps are converted to Beijing time; a zero or negative
// interval yields 0.
func (c *Calendar) WorkMinutesBetween(start, end time.Time) int {
	s := start.In(c.loc)
	e := end.In(c.loc)
	if !e.After(s) {
		return 0
	}

	total := 0
	day := time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, c.loc)
	for !day.After(e) {
		if c.IsWorkday(day) {
			lo := time.Date(day.Year(), day.Month(), day.Day(), DayStart, 0, 0, 0, c.loc)
			hi := time.Date(day.Year(), day.Month(), day.Day(), DayEnd, 0, 0, 0, c.loc)
			if s.After(lo) {
				lo = s
			}
			if e.Before(hi) {
				hi = e
			}
			if hi.After(lo) {
				total += int(hi.Sub(lo) / time.Minute)
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return total
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

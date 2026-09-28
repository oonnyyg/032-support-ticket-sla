package calendar

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	loc, _ := time.LoadLocation("Asia/Shanghai")
	tm, err := time.ParseInLocation("2006-01-02T15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestWorkMinutesBetween(t *testing.T) {
	cal, err := New([]string{"2026-09-29"}) // Tuesday holiday
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		start string
		end   string
		want  int
	}{
		{"within day", "2026-09-28T10:00", "2026-09-28T12:00", 120},
		{"night before work", "2026-09-28T07:00", "2026-09-28T10:00", 60},
		{"night after work", "2026-09-28T17:30", "2026-09-28T20:00", 30},
		{"cross overnight", "2026-09-28T17:00", "2026-09-29T10:00", 60}, // holiday next day
		{"cross weekend", "2026-09-25T17:00", "2026-09-28T10:00", 120},  // Fri 60 + Mon 60
		{"holiday skipped", "2026-09-28T18:00", "2026-09-30T09:00", 0},  // Tue holiday
		{"cross holiday resuming", "2026-09-28T17:00", "2026-09-30T10:00", 120},
		{"empty interval", "2026-09-28T10:00", "2026-09-28T10:00", 0},
		{"reversed interval", "2026-09-28T11:00", "2026-09-28T10:00", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cal.WorkMinutesBetween(mustTime(t, tc.start), mustTime(t, tc.end))
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestNewInvalidHoliday(t *testing.T) {
	if _, err := New([]string{"2026/09/29"}); err == nil {
		t.Fatal("expected error for bad holiday format")
	}
	if _, err := New([]string{"2026-09-29", "2026-09-29"}); err == nil {
		t.Fatal("expected error for duplicate holiday")
	}
}

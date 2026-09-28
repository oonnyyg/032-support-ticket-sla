package desk

import (
	"time"
)

// MinuteCounter counts whole working minutes inside a time interval.
type MinuteCounter interface {
	WorkMinutesBetween(start, end time.Time) int
}

// Snapshot is the time-sensitive view of a ticket at a query time.
type Snapshot struct {
	ID            string    `json:"id"`
	Customer      string    `json:"customer"`
	Assignee      string    `json:"assignee"`
	Status        string    `json:"status"`
	LimitMins     int       `json:"limit_minutes"`
	ElapsedMins   int       `json:"elapsed_minutes"`
	RemainingMins int       `json:"remaining_minutes"`
	Overtime      bool      `json:"overtime"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ElapsedMinutes returns the processing minutes a ticket has accrued at time at.
//
// Only intervals spent in status open are counted, and only the portions
// inside working hours. After completion the value is frozen at the closing
// time regardless of at.
func ElapsedMinutes(t *Ticket, counter MinuteCounter, at time.Time) int {
	if t.Status == StatusDone {
		return t.FrozenMins
	}
	if !at.After(t.UpdatedAt) {
		at = t.UpdatedAt
	}

	total := 0
	inOpen := true
	segmentStart := t.CreatedAt
	for _, h := range t.History {
		if inOpen {
			total += counter.WorkMinutesBetween(segmentStart, h.At)
		}
		inOpen = h.To == StatusOpen
		segmentStart = h.At
	}
	if inOpen {
		total += counter.WorkMinutesBetween(segmentStart, at)
	}
	return total
}

// View builds a Snapshot of the ticket at time at. Remaining minutes are
// clamped at zero; overtime is true only when elapsed strictly exceeds the
// limit (exactly exhausting the limit is not overtime).
func View(t *Ticket, counter MinuteCounter, at time.Time) Snapshot {
	elapsed := ElapsedMinutes(t, counter, at)
	remaining := t.LimitMins - elapsed
	if remaining < 0 {
		remaining = 0
	}
	return Snapshot{
		ID:            t.ID,
		Customer:      t.Customer,
		Assignee:      t.Assignee,
		Status:        t.Status,
		LimitMins:     t.LimitMins,
		ElapsedMins:   elapsed,
		RemainingMins: remaining,
		Overtime:      elapsed > t.LimitMins,
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
	}
}

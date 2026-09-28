// Package desk contains the support-ticket domain model and SLA rules.
package desk

import (
	"encoding/json"
	"time"
)

// Ticket statuses.
const (
	StatusOpen    = "open"    // 处理中
	StatusWaiting = "waiting" // 等待客户
	StatusDone    = "done"    // 已完成
)

// HistoryEntry records one status change of a ticket.
type HistoryEntry struct {
	From   string    `json:"from"`
	To     string    `json:"to"`
	At     time.Time `json:"at"`
	Reason string    `json:"reason,omitempty"`
}

const minuteLayout = "2006-01-02T15:04"

type historyEntryJSON struct {
	From   string `json:"from"`
	To     string `json:"to"`
	At     string `json:"at"`
	Reason string `json:"reason,omitempty"`
}

// MarshalJSON renders At at minute precision ("YYYY-MM-DDTHH:MM").
func (h HistoryEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(historyEntryJSON{
		From: h.From, To: h.To, At: h.At.Format(minuteLayout), Reason: h.Reason,
	})
}

// UnmarshalJSON parses the minute-precision Beijing timestamp.
func (h *HistoryEntry) UnmarshalJSON(data []byte) error {
	var raw historyEntryJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return err
	}
	at, err := time.ParseInLocation(minuteLayout, raw.At, loc)
	if err != nil {
		return err
	}
	h.From, h.To, h.At, h.Reason = raw.From, raw.To, at, raw.Reason
	return nil
}

// Ticket is a single support ticket and its full history.
type Ticket struct {
	ID         string         `json:"id"`
	Customer   string         `json:"customer"`
	Assignee   string         `json:"assignee"`
	LimitMins  int            `json:"limit_minutes"`
	Status     string         `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	ClosedAt   *time.Time     `json:"closed_at,omitempty"`
	FrozenMins int            `json:"frozen_minutes,omitempty"`
	History    []HistoryEntry `json:"history"`
}

type ticketJSON struct {
	ID         string         `json:"id"`
	Customer   string         `json:"customer"`
	Assignee   string         `json:"assignee"`
	LimitMins  int            `json:"limit_minutes"`
	Status     string         `json:"status"`
	CreatedAt  string         `json:"created_at"`
	UpdatedAt  string         `json:"updated_at"`
	ClosedAt   string         `json:"closed_at,omitempty"`
	FrozenMins int            `json:"frozen_minutes,omitempty"`
	History    []HistoryEntry `json:"history"`
}

// MarshalJSON renders all timestamps at minute precision.
func (t Ticket) MarshalJSON() ([]byte, error) {
	j := ticketJSON{
		ID: t.ID, Customer: t.Customer, Assignee: t.Assignee,
		LimitMins: t.LimitMins, Status: t.Status,
		CreatedAt:  t.CreatedAt.Format(minuteLayout),
		UpdatedAt:  t.UpdatedAt.Format(minuteLayout),
		FrozenMins: t.FrozenMins, History: t.History,
	}
	if t.ClosedAt != nil {
		j.ClosedAt = t.ClosedAt.Format(minuteLayout)
	}
	return json.Marshal(j)
}

// UnmarshalJSON parses minute-precision Beijing timestamps.
func (t *Ticket) UnmarshalJSON(data []byte) error {
	var j ticketJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return err
	}
	parse := func(s string) (*time.Time, error) {
		if s == "" {
			return nil, nil
		}
		v, err := time.ParseInLocation(minuteLayout, s, loc)
		if err != nil {
			return nil, err
		}
		return &v, nil
	}
	created, err := parse(j.CreatedAt)
	if err != nil {
		return err
	}
	updated, err := parse(j.UpdatedAt)
	if err != nil {
		return err
	}
	closed, err := parse(j.ClosedAt)
	if err != nil {
		return err
	}
	t.ID, t.Customer, t.Assignee = j.ID, j.Customer, j.Assignee
	t.LimitMins, t.Status = j.LimitMins, j.Status
	t.CreatedAt, t.UpdatedAt = *created, *updated
	t.ClosedAt, t.FrozenMins, t.History = closed, j.FrozenMins, j.History
	return nil
}

// allowedTransitions maps a current status to the statuses it may move to.
var allowedTransitions = map[string]map[string]bool{
	StatusOpen: {
		StatusWaiting: true,
		StatusDone:    true,
	},
	StatusWaiting: {
		StatusOpen: true,
	},
	StatusDone: {},
}

// CanTransition reports whether moving to next is legal from the current
// status. open -> waiting|done, waiting -> open only, done is terminal.
func CanTransition(current, next string) bool {
	return allowedTransitions[current][next]
}

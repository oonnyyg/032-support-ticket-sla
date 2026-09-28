// Package proto implements the line-delimited JSON request/response protocol.
package proto

import (
	"encoding/json"
	"fmt"

	"example.com/support-ticket-sla/internal/desk"
)

// Request is one JSON object per line.
//
// op is one of: init, create, transition, get, list.
type Request struct {
	Op        string   `json:"op"`
	Holidays  []string `json:"holidays,omitempty"`
	ID        string   `json:"id,omitempty"`
	Customer  string   `json:"customer,omitempty"`
	Assignee  string   `json:"assignee,omitempty"`
	LimitMins int      `json:"limit_minutes,omitempty"`
	To        string   `json:"to,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	At        string   `json:"at,omitempty"`
	Status    string   `json:"status,omitempty"`
	Overtime  *bool    `json:"overtime,omitempty"`
}

// TicketDetail is the full ticket representation including history.
type TicketDetail struct {
	ID            string              `json:"id"`
	Customer      string              `json:"customer"`
	Assignee      string              `json:"assignee"`
	Status        string              `json:"status"`
	LimitMins     int                 `json:"limit_minutes"`
	ElapsedMins   int                 `json:"elapsed_minutes"`
	RemainingMins int                 `json:"remaining_minutes"`
	Overtime      bool                `json:"overtime"`
	CreatedAt     string              `json:"created_at"`
	UpdatedAt     string              `json:"updated_at"`
	ClosedAt      string              `json:"closed_at,omitempty"`
	History       []desk.HistoryEntry `json:"history"`
}

// Response is returned as one JSON object per processed request line.
type Response struct {
	OK      bool           `json:"ok"`
	Op      string         `json:"op,omitempty"`
	Error   string         `json:"error,omitempty"`
	Ticket  *TicketDetail  `json:"ticket,omitempty"`
	Tickets []TicketDetail `json:"tickets,omitempty"`
}

const tsLayout = "2006-01-02T15:04"

func toDetail(s desk.Snapshot, t *desk.Ticket) TicketDetail {
	d := TicketDetail{
		ID:            s.ID,
		Customer:      s.Customer,
		Assignee:      s.Assignee,
		Status:        s.Status,
		LimitMins:     s.LimitMins,
		ElapsedMins:   s.ElapsedMins,
		RemainingMins: s.RemainingMins,
		Overtime:      s.Overtime,
		CreatedAt:     s.CreatedAt.Format(tsLayout),
		UpdatedAt:     s.UpdatedAt.Format(tsLayout),
		History:       t.History,
	}
	if t.ClosedAt != nil {
		d.ClosedAt = t.ClosedAt.Format(tsLayout)
	}
	return d
}

// Handler processes parsed requests through a desk.Service.
type Handler struct {
	svc *desk.Service
}

// NewHandler creates a Handler.
func NewHandler(svc *desk.Service) *Handler {
	return &Handler{svc: svc}
}

// Handle executes one request and returns its response. All domain failures
// are reported as {ok:false} responses rather than Go errors.
func (h *Handler) Handle(req Request) Response {
	switch req.Op {
	case "init":
		if err := h.svc.Init(req.Holidays); err != nil {
			return fail(req.Op, err)
		}
		return Response{OK: true, Op: "init"}
	case "create":
		at, err := desk.ParseTime(req.At)
		if err != nil {
			return fail(req.Op, err)
		}
		t, err := h.svc.Create(desk.CreateInput{
			ID: req.ID, Customer: req.Customer, Assignee: req.Assignee,
			LimitMins: req.LimitMins, At: at,
		})
		if err != nil {
			return fail(req.Op, err)
		}
		snap, err := h.svc.SnapshotAt(t, at)
		if err != nil {
			return fail(req.Op, err)
		}
		d := toDetail(snap, t)
		return Response{OK: true, Op: "create", Ticket: &d}
	case "transition":
		at, err := desk.ParseTime(req.At)
		if err != nil {
			return fail(req.Op, err)
		}
		t, err := h.svc.Transition(req.ID, req.To, req.Reason, at)
		if err != nil {
			return fail(req.Op, err)
		}
		snap, _, err := h.svc.Get(t.ID, at)
		if err != nil {
			return fail(req.Op, err)
		}
		d := toDetail(snap, t)
		return Response{OK: true, Op: "transition", Ticket: &d}
	case "get":
		at, err := desk.ParseTime(req.At)
		if err != nil {
			return fail(req.Op, err)
		}
		snap, t, err := h.svc.Get(req.ID, at)
		if err != nil {
			return fail(req.Op, err)
		}
		d := toDetail(snap, t)
		return Response{OK: true, Op: "get", Ticket: &d}
	case "list":
		at, err := desk.ParseTime(req.At)
		if err != nil {
			return fail(req.Op, err)
		}
		views, err := h.svc.List(desk.ListFilter{
			Status: req.Status, Assignee: req.Assignee, Overtime: req.Overtime,
		}, at)
		if err != nil {
			return fail(req.Op, err)
		}
		details := make([]TicketDetail, len(views))
		for i, v := range views {
			details[i] = toDetail(v.View, v.Ticket)
		}
		return Response{OK: true, Op: "list", Tickets: details}
	default:
		return Response{OK: false, Op: req.Op, Error: fmt.Sprintf("unknown op %q", req.Op)}
	}
}

func fail(op string, err error) Response {
	return Response{OK: false, Op: op, Error: err.Error()}
}

// DecodeLine parses one request line.
func DecodeLine(line []byte) (Request, error) {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return Request{}, err
	}
	return req, nil
}

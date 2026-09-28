package desk

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"example.com/support-ticket-sla/internal/calendar"
)

// Errors returned by the service. They are wrapped with context; callers
// should use errors.Is.
var (
	ErrNotInitialized = errors.New("calendar not initialized")
	ErrAlreadyInit    = errors.New("calendar already initialized")
	ErrBadTime        = errors.New("invalid time: use Beijing time YYYY-MM-DDTHH:MM")
	ErrTimeReversed   = errors.New("operation time must not be earlier than the latest operation")
	ErrDuplicateID    = errors.New("ticket id already exists")
	ErrMissingField   = errors.New("missing required field")
	ErrInvalidLimit   = errors.New("limit_minutes must be a positive integer")
	ErrUnknownTicket  = errors.New("ticket not found")
	ErrInvalidStatus  = errors.New("invalid status")
	ErrIllegalChange  = errors.New("illegal status transition")
)

// Store persists tickets and the immutable calendar configuration.
type Store interface {
	Load() (*State, error)
	Save(*State) error
}

// Service applies ticket operations on top of a Store.
type Service struct {
	store Store
}

// NewService creates a Service backed by store.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// State is the full persisted payload.
type State struct {
	Initialized bool               `json:"initialized"`
	Holidays    []string           `json:"holidays,omitempty"`
	Tickets     map[string]*Ticket `json:"tickets"`
}

// ParseTime parses a minute-precision Beijing timestamp ("2026-09-28T14:30")
// and rejects second/sub-second precision.
func ParseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Time{}, err
	}
	if strings.Contains(s, "+") || strings.HasSuffix(s, "Z") {
		return time.Time{}, ErrBadTime
	}
	t, err := time.ParseInLocation("2006-01-02T15:04", s, loc)
	if err != nil {
		// Reject strings that parse only with seconds.
		if _, err2 := time.ParseInLocation("2006-01-02T15:04:05", s, loc); err2 == nil {
			return time.Time{}, ErrBadTime
		}
		return time.Time{}, ErrBadTime
	}
	return t, nil
}

// Init builds the immutable working calendar. It may run only once.
func (s *Service) Init(holidays []string) error {
	state, err := s.store.Load()
	if err != nil {
		return err
	}
	if state.Initialized {
		return ErrAlreadyInit
	}
	cal, err := calendar.New(holidays)
	if err != nil {
		return err
	}
	state.Initialized = true
	state.Holidays = cal.Holidays()
	return s.store.Save(state)
}

func (s *Service) calendar(state *State) (*calendar.Calendar, error) {
	if !state.Initialized {
		return nil, ErrNotInitialized
	}
	return calendar.New(state.Holidays)
}

// CreateInput carries the fields for creating a ticket.
type CreateInput struct {
	ID        string
	Customer  string
	Assignee  string
	LimitMins int
	At        time.Time
}

// Create opens a new ticket. Validation failures leave storage untouched.
func (s *Service) Create(in CreateInput) (*Ticket, error) {
	state, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	if _, err := s.calendar(state); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return nil, fmt.Errorf("%w: id", ErrMissingField)
	}
	if strings.TrimSpace(in.Customer) == "" {
		return nil, fmt.Errorf("%w: customer", ErrMissingField)
	}
	if strings.TrimSpace(in.Assignee) == "" {
		return nil, fmt.Errorf("%w: assignee", ErrMissingField)
	}
	if in.LimitMins <= 0 {
		return nil, ErrInvalidLimit
	}
	if in.At.IsZero() {
		return nil, ErrBadTime
	}
	if _, exists := state.Tickets[in.ID]; exists {
		return nil, fmt.Errorf("%w: %s", ErrDuplicateID, in.ID)
	}

	t := &Ticket{
		ID:        in.ID,
		Customer:  in.Customer,
		Assignee:  in.Assignee,
		LimitMins: in.LimitMins,
		Status:    StatusOpen,
		CreatedAt: in.At,
		UpdatedAt: in.At,
		History: []HistoryEntry{
			{From: "", To: StatusOpen, At: in.At},
		},
	}
	state.Tickets[in.ID] = t
	if err := s.store.Save(state); err != nil {
		return nil, err
	}
	return t, nil
}

// Transition changes a ticket's status at the given time. The time must not
// be earlier than the ticket's latest operation.
func (s *Service) Transition(id, to, reason string, at time.Time) (*Ticket, error) {
	state, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	if _, err := s.calendar(state); err != nil {
		return nil, err
	}
	if at.IsZero() {
		return nil, ErrBadTime
	}
	t, ok := state.Tickets[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTicket, id)
	}
	switch to {
	case StatusOpen, StatusWaiting, StatusDone:
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidStatus, to)
	}
	if at.Before(t.UpdatedAt) {
		return nil, fmt.Errorf("%w: %s before %s", ErrTimeReversed,
			at.Format("2006-01-02T15:04"), t.UpdatedAt.Format("2006-01-02T15:04"))
	}
	if !CanTransition(t.Status, to) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrIllegalChange, t.Status, to)
	}

	cal, err := s.calendar(state)
	if err != nil {
		return nil, err
	}
	from := t.Status
	if to == StatusDone {
		t.FrozenMins = ElapsedMinutes(t, cal, at)
		closed := at
		t.ClosedAt = &closed
	}
	t.History = append(t.History, HistoryEntry{From: from, To: to, At: at, Reason: reason})
	t.Status = to
	t.UpdatedAt = at
	if err := s.store.Save(state); err != nil {
		return nil, err
	}
	return t, nil
}

// Get returns a snapshot of one ticket at time at. Querying never writes.
func (s *Service) Get(id string, at time.Time) (Snapshot, *Ticket, error) {
	state, err := s.store.Load()
	if err != nil {
		return Snapshot{}, nil, err
	}
	cal, err := s.calendar(state)
	if err != nil {
		return Snapshot{}, nil, err
	}
	t, ok := state.Tickets[id]
	if !ok {
		return Snapshot{}, nil, fmt.Errorf("%w: %s", ErrUnknownTicket, id)
	}
	if at.IsZero() {
		return Snapshot{}, nil, ErrBadTime
	}
	if at.Before(t.UpdatedAt) {
		return Snapshot{}, nil, fmt.Errorf("%w: query %s before latest operation %s",
			ErrTimeReversed, at.Format("2006-01-02T15:04"),
			t.UpdatedAt.Format("2006-01-02T15:04"))
	}
	return View(t, cal, at), t, nil
}

// ListFilter narrows a List query. Empty fields mean "any".
type ListFilter struct {
	Status   string
	Assignee string
	Overtime *bool
}

// TicketView pairs a ticket with its snapshot at a query time.
type TicketView struct {
	Ticket *Ticket
	View   Snapshot
}

// List returns snapshots of matching tickets evaluated at time at, sorted by
// creation time and then by id. Nothing is persisted.
func (s *Service) List(f ListFilter, at time.Time) ([]TicketView, error) {
	state, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	cal, err := s.calendar(state)
	if err != nil {
		return nil, err
	}
	if at.IsZero() {
		return nil, ErrBadTime
	}
	if f.Status != "" && f.Status != StatusOpen && f.Status != StatusWaiting && f.Status != StatusDone {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStatus, f.Status)
	}

	tickets := make([]*Ticket, 0, len(state.Tickets))
	for _, t := range state.Tickets {
		if at.Before(t.UpdatedAt) {
			return nil, fmt.Errorf("%w: ticket %s query %s before latest operation %s",
				ErrTimeReversed, t.ID,
				at.Format("2006-01-02T15:04"),
				t.UpdatedAt.Format("2006-01-02T15:04"))
		}
		if f.Status != "" && t.Status != f.Status {
			continue
		}
		if f.Assignee != "" && t.Assignee != f.Assignee {
			continue
		}
		snap := View(t, cal, at)
		if f.Overtime != nil && snap.Overtime != *f.Overtime {
			continue
		}
		tickets = append(tickets, t)
	}
	sort.Slice(tickets, func(i, j int) bool {
		if !tickets[i].CreatedAt.Equal(tickets[j].CreatedAt) {
			return tickets[i].CreatedAt.Before(tickets[j].CreatedAt)
		}
		return tickets[i].ID < tickets[j].ID
	})
	out := make([]TicketView, len(tickets))
	for i, t := range tickets {
		out[i] = TicketView{Ticket: t, View: View(t, cal, at)}
	}
	return out, nil
}

// SnapshotAt returns the snapshot of an already-loaded ticket at time at.
func (s *Service) SnapshotAt(t *Ticket, at time.Time) (Snapshot, error) {
	state, err := s.store.Load()
	if err != nil {
		return Snapshot{}, err
	}
	cal, err := s.calendar(state)
	if err != nil {
		return Snapshot{}, err
	}
	return View(t, cal, at), nil
}

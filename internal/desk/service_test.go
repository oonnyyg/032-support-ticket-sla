package desk

import (
	"errors"
	"testing"
	"time"
)

type memStore struct{ state *State }

func (m *memStore) Load() (*State, error) {
	if m.state == nil {
		return &State{Tickets: map[string]*Ticket{}}, nil
	}
	return m.state, nil
}

func (m *memStore) Save(s *State) error { m.state = s; return nil }

func newTestService(t *testing.T, holidays ...string) *Service {
	t.Helper()
	s := NewService(&memStore{})
	if err := s.Init(holidays); err != nil {
		t.Fatal(err)
	}
	return s
}

func pt(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := ParseTime(s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func mkTicket(t *testing.T, s *Service, id, who string, limit int, at string) *Ticket {
	t.Helper()
	tk, err := s.Create(CreateInput{
		ID: id, Customer: "cust-" + id, Assignee: who, LimitMins: limit, At: pt(t, at),
	})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestFullLifecycle(t *testing.T) {
	s := newTestService(t)
	mkTicket(t, s, "T1", "alice", 120, "2026-09-28T09:00")

	// 60 processing minutes: exactly half the limit, not overtime.
	snap, _, err := s.Get("T1", pt(t, "2026-09-28T10:00"))
	if err != nil {
		t.Fatal(err)
	}
	if snap.ElapsedMins != 60 || snap.RemainingMins != 60 || snap.Overtime {
		t.Fatalf("unexpected snap: %+v", snap)
	}

	if _, err := s.Transition("T1", StatusWaiting, "", pt(t, "2026-09-28T10:00")); err != nil {
		t.Fatal(err)
	}
	// Waiting does not accrue, even across a full day.
	snap, _, _ = s.Get("T1", pt(t, "2026-09-29T17:00"))
	if snap.ElapsedMins != 60 || snap.RemainingMins != 60 {
		t.Fatalf("waiting should pause: %+v", snap)
	}

	if _, err := s.Transition("T1", StatusOpen, "", pt(t, "2026-09-29T17:00")); err != nil {
		t.Fatal(err)
	}
	// One hour later at 18:00 closes: 60 more minutes -> exactly at limit.
	tk, err := s.Transition("T1", StatusDone, "", pt(t, "2026-09-29T18:00"))
	if err != nil {
		t.Fatal(err)
	}
	if tk.FrozenMins != 120 {
		t.Fatalf("frozen = %d, want 120", tk.FrozenMins)
	}
	// Frozen regardless of query time; exactly at limit is not overtime.
	snap, _, _ = s.Get("T1", pt(t, "2026-12-31T18:00"))
	if snap.ElapsedMins != 120 || snap.RemainingMins != 0 || snap.Overtime {
		t.Fatalf("frozen snapshot wrong: %+v", snap)
	}
	if len(tk.History) != 4 {
		t.Fatalf("history len = %d, want 4", len(tk.History))
	}
}

func TestIllegalTransitions(t *testing.T) {
	s := newTestService(t)
	mkTicket(t, s, "T1", "alice", 60, "2026-09-28T09:00")

	if _, err := s.Transition("T1", StatusOpen, "", pt(t, "2026-09-28T09:30")); !errors.Is(err, ErrIllegalChange) {
		t.Fatalf("open->open should fail, got %v", err)
	}
	if _, err := s.Transition("T1", StatusDone, "", pt(t, "2026-09-28T09:30")); err != nil {
		t.Fatalf("open->done should work, got %v", err)
	}
	if _, err := s.Transition("T1", StatusOpen, "", pt(t, "2026-09-28T10:00")); !errors.Is(err, ErrIllegalChange) {
		t.Fatalf("done is terminal, got %v", err)
	}

	s2 := newTestService(t)
	mkTicket(t, s2, "T2", "bob", 60, "2026-09-28T09:00")
	if _, err := s2.Transition("T2", StatusWaiting, "", pt(t, "2026-09-28T10:00")); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Transition("T2", StatusDone, "", pt(t, "2026-09-28T10:30")); !errors.Is(err, ErrIllegalChange) {
		t.Fatalf("waiting->done must fail, got %v", err)
	}
	if _, err := s2.Transition("T2", "bogus", "", pt(t, "2026-09-28T10:30")); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("bogus status, got %v", err)
	}
}

func TestTimeMustNotReverse(t *testing.T) {
	s := newTestService(t)
	mkTicket(t, s, "T1", "alice", 60, "2026-09-28T09:00")
	if _, err := s.Transition("T1", StatusWaiting, "", pt(t, "2026-09-28T08:59")); !errors.Is(err, ErrTimeReversed) {
		t.Fatalf("transition reverse: %v", err)
	}
	if _, _, err := s.Get("T1", pt(t, "2026-09-28T08:59")); !errors.Is(err, ErrTimeReversed) {
		t.Fatalf("query reverse: %v", err)
	}
	// Failed transition must not mutate.
	got, _, _ := s.Get("T1", pt(t, "2026-09-28T09:00"))
	if got.Status != StatusOpen {
		t.Fatalf("status mutated on failure: %s", got.Status)
	}

	if _, err := s.Create(CreateInput{ID: "T2", Customer: "c", Assignee: "a", LimitMins: 60, At: pt(t, "2026-09-28T08:00")}); err != nil {
		t.Fatal(err)
	}
	// Per-ticket clock: T2 may operate at 08:00... but create at 08:00 and
	// then 07:59 on T2 must fail.
	if _, err := s.Transition("T2", StatusDone, "", pt(t, "2026-09-28T07:59")); !errors.Is(err, ErrTimeReversed) {
		t.Fatalf("per-ticket reverse: %v", err)
	}
}

func TestCreateValidation(t *testing.T) {
	s := newTestService(t)
	in := CreateInput{ID: "T1", Customer: "c", Assignee: "a", LimitMins: 30, At: pt(t, "2026-09-28T09:00")}
	if _, err := s.Create(in); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(in); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("dup id: %v", err)
	}
	bad := in
	bad.ID = "T2"
	bad.LimitMins = 0
	if _, err := s.Create(bad); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("zero limit: %v", err)
	}
	for _, mutate := range []func(*CreateInput){
		func(x *CreateInput) { x.ID = " " },
		func(x *CreateInput) { x.Customer = "" },
		func(x *CreateInput) { x.Assignee = "" },
	} {
		x := in
		x.ID = "T" + x.Assignee + x.Customer
		mutate(&x)
		if _, err := s.Create(x); !errors.Is(err, ErrMissingField) {
			t.Fatalf("missing field: %v", err)
		}
	}
}

func TestWeekendAndHolidaySkipped(t *testing.T) {
	// 2026-10-01 is Thursday; mark it a holiday.
	s := newTestService(t, "2026-10-01")
	mkTicket(t, s, "T1", "alice", 480, "2026-10-01T09:00") // holiday
	// Query after the long weekend: Fri 10-02 09:00-18:00 = 540, but limit 480.
	snap, _, err := s.Get("T1", pt(t, "2026-10-02T18:00"))
	if err != nil {
		t.Fatal(err)
	}
	if snap.ElapsedMins != 540 || !snap.Overtime || snap.RemainingMins != 0 {
		t.Fatalf("weekend/holiday calc wrong: %+v", snap)
	}

	// Created Friday evening; nothing accrues over the weekend until Monday.
	mkTicket(t, s, "T2", "bob", 60, "2026-10-02T17:00")
	snap2, _, _ := s.Get("T2", pt(t, "2026-10-05T09:30")) // Monday
	if snap2.ElapsedMins != 90 {
		t.Fatalf("cross-weekend: %+v", snap2)
	}
}

func TestOvertimeBoundary(t *testing.T) {
	s := newTestService(t)
	mkTicket(t, s, "T1", "alice", 30, "2026-09-28T09:00")
	snap, _, _ := s.Get("T1", pt(t, "2026-09-28T09:30"))
	if snap.Overtime || snap.RemainingMins != 0 {
		t.Fatalf("exactly at limit must not be overtime: %+v", snap)
	}
	snap, _, _ = s.Get("T1", pt(t, "2026-09-28T09:31"))
	if !snap.Overtime || snap.RemainingMins != 0 || snap.ElapsedMins != 31 {
		t.Fatalf("one minute over must be overtime: %+v", snap)
	}
}

func TestListFilterAndOrder(t *testing.T) {
	s := newTestService(t)
	mkTicket(t, s, "B", "alice", 10, "2026-09-28T09:00")
	mkTicket(t, s, "A", "alice", 5000, "2026-09-28T09:00")
	mkTicket(t, s, "C", "bob", 10, "2026-09-29T09:00")

	at := pt(t, "2026-09-29T18:00")
	over := true
	got, err := s.List(ListFilter{Overtime: &over}, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Ticket.ID != "B" || got[1].Ticket.ID != "C" {
		t.Fatalf("overtime filter/order wrong: %+v", got)
	}
	got, _ = s.List(ListFilter{Assignee: "alice"}, at)
	if len(got) != 2 || got[0].Ticket.ID != "A" {
		t.Fatalf("assignee filter wrong: %+v", got)
	}
	got, _ = s.List(ListFilter{Status: StatusOpen}, at)
	if len(got) != 3 {
		t.Fatalf("status filter wrong: %d", len(got))
	}
	if _, err := s.List(ListFilter{Status: "nope"}, at); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("want invalid status error, got %v", err)
	}
}

func TestInitOnceAndRequired(t *testing.T) {
	s := NewService(&memStore{})
	if _, err := s.Create(CreateInput{ID: "x", Customer: "c", Assignee: "a", LimitMins: 1, At: pt(t, "2026-09-28T09:00")}); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("create before init: %v", err)
	}
	if err := s.Init(nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(nil); !errors.Is(err, ErrAlreadyInit) {
		t.Fatalf("double init: %v", err)
	}
}

func TestParseTimeRejectsSeconds(t *testing.T) {
	for _, s := range []string{"2026-09-28T09:00:30", "2026-09-28T09:00Z", "2026-09-28 09:00", "nope"} {
		if _, err := ParseTime(s); !errors.Is(err, ErrBadTime) {
			t.Fatalf("ParseTime(%q) = %v", s, err)
		}
	}
}

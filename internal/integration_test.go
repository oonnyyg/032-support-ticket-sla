package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"example.com/support-ticket-sla/internal/desk"
	"example.com/support-ticket-sla/internal/proto"
	"example.com/support-ticket-sla/internal/store"
)

func run(t *testing.T, h *proto.Handler, line string) proto.Response {
	t.Helper()
	req, err := proto.DecodeLine([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	return h.Handle(req)
}

func TestPersistReopenAndProtocol(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")

	// Session 1: init, create, work, wait, reopen process.
	h := proto.NewHandler(desk.NewService(store.NewFileStore(dir)))
	if r := run(t, h, `{"op":"init","holidays":["2026-10-01"]}`); !r.OK {
		t.Fatalf("init: %s", r.Error)
	}
	r := run(t, h, `{"op":"create","id":"T-1","customer":"Acme","assignee":"alice","limit_minutes":200,"at":"2026-09-30T17:00"}`)
	if !r.OK {
		t.Fatalf("create: %s", r.Error)
	}
	// 60 minutes on Wed evening, then wait.
	if r := run(t, h, `{"op":"transition","id":"T-1","to":"waiting","at":"2026-10-02T10:00"}`); !r.OK {
		t.Fatalf("waiting: %s", r.Error)
	}
	if r := run(t, h, `{"op":"transition","id":"T-1","to":"open","at":"2026-10-05T09:00"}`); !r.OK {
		t.Fatalf("resume: %s", r.Error)
	}

	// Invalid requests must fail and leave history unchanged.
	bad := run(t, h, `{"op":"transition","id":"T-1","to":"done","at":"2026-10-04T09:00"}`)
	if bad.OK {
		t.Fatal("reversed transition accepted")
	}
	bad = run(t, h, `{"op":"transition","id":"T-1","to":"waiting","at":"2026-10-05T08:00"}`)
	if bad.OK {
		t.Fatal("reversed transition accepted")
	}
	dup := run(t, h, `{"op":"create","id":"T-1","customer":"x","assignee":"y","limit_minutes":10,"at":"2026-10-05T09:01"}`)
	if dup.OK {
		t.Fatal("duplicate id accepted")
	}

	// Session 2: reopen the same data directory.
	h2 := proto.NewHandler(desk.NewService(store.NewFileStore(dir)))
	// Calendar is immutable: re-init fails.
	if r := run(t, h2, `{"op":"init","holidays":[]}`); r.OK {
		t.Fatal("re-init after reopen should fail")
	}
	// Waiting Wed 10:00 -> Mon 09:00 accrued nothing; before that the open
	// segment was Wed 17:00-18:00 (60) + Thu 09:00-10:00 holiday skipped +
	// Fri 09:00-10:00 (60) = 120. Query Mon 10:00 adds 60 -> 180.
	r = run(t, h2, `{"op":"get","id":"T-1","at":"2026-10-05T10:00"}`)
	if !r.OK {
		t.Fatalf("get after reopen: %s", r.Error)
	}
	if r.Ticket.ElapsedMins != 180 || r.Ticket.RemainingMins != 20 || r.Ticket.Overtime {
		t.Fatalf("reopen SLA wrong: %+v", r.Ticket)
	}
	if len(r.Ticket.History) != 3 {
		t.Fatalf("history should have 3 entries after failed ops, got %d", len(r.Ticket.History))
	}

	// Complete freezes elapsed.
	if r := run(t, h2, `{"op":"transition","id":"T-1","to":"done","at":"2026-10-05T10:20"}`); !r.OK {
		t.Fatalf("done: %s", r.Error)
	}

	// Session 3: list filters against frozen data.
	h3 := proto.NewHandler(desk.NewService(store.NewFileStore(dir)))
	r = run(t, h3, `{"op":"list","at":"2026-12-31T18:00","status":"done","assignee":"alice"}`)
	if !r.OK || len(r.Tickets) != 1 || r.Tickets[0].ElapsedMins != 200 {
		t.Fatalf("list wrong: ok=%v n=%d %+v", r.OK, len(r.Tickets), r.Tickets)
	}
	if r.Tickets[0].Overtime {
		t.Fatal("exactly 200 of 200 must not be overtime")
	}
	r = run(t, h3, `{"op":"list","at":"2026-12-31T18:00","overtime":true}`)
	if !r.OK || len(r.Tickets) != 0 {
		t.Fatalf("no overtime tickets expected, got %+v", r.Tickets)
	}

	// State file must be valid JSON on disk.
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedLineAndUnknownOp(t *testing.T) {
	dir := t.TempDir()
	h := proto.NewHandler(desk.NewService(store.NewFileStore(dir)))
	if _, err := proto.DecodeLine([]byte("{not json")); err == nil {
		t.Fatal("expected decode error")
	}
	if r := run(t, h, `{"op":"frobnicate"}`); r.OK || r.Error == "" {
		t.Fatal("unknown op should fail with message")
	}
}

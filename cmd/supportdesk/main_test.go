package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func runLines(t *testing.T, dir string, lines ...string) []response {
	t.Helper()
	var out bytes.Buffer
	if err := run(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, dir); err != nil {
		t.Fatalf("run: %v", err)
	}
	var resps []response
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var r response
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("bad response %q: %v", line, err)
		}
		resps = append(resps, r)
	}
	return resps
}

func TestCLIProtocol(t *testing.T) {
	resps := runLines(t, t.TempDir(),
		`{"op":"create","id":"C1","customer":"ACME","assignee":"alice","limit_minutes":60,"at":"2026-09-28T09:00"}`,
		`{"op":"init"}`,
		`{"op":"create","id":"C1","customer":"ACME","assignee":"alice","limit_minutes":60,"at":"2026-09-28T09:00"}`,
		`{"op":"create","id":"C1","customer":"ACME","assignee":"alice","limit_minutes":60,"at":"2026-09-28T09:00"}`,
		`{"op":"create","id":"C2","customer":"ACME","assignee":"bob","limit_minutes":0,"at":"2026-09-28T09:00"}`,
		`{"op":"get","id":"C1","at":"2026-09-28T10:00"}`,
		`{"op":"list","at":"2026-09-28T10:00","assignee":"alice","overtime":false}`,
		`{"op":"bogus"}`,
		`not json at all`,
	)
	if resps[0].OK || !strings.Contains(resps[0].Error, "not initialized") {
		t.Fatalf("before init: %+v", resps[0])
	}
	if !resps[1].OK {
		t.Fatalf("init failed: %+v", resps[1])
	}
	if !resps[2].OK {
		t.Fatalf("create failed: %+v", resps[2])
	}
	if resps[3].OK || !strings.Contains(resps[3].Error, "duplicate") {
		t.Fatalf("duplicate should fail: %+v", resps[3])
	}
	if resps[4].OK || !strings.Contains(resps[4].Error, "positive integer") {
		t.Fatalf("bad limit should fail: %+v", resps[4])
	}

	var view map[string]any
	if err := json.Unmarshal(resps[5].Data, &view); err != nil {
		t.Fatal(err)
	}
	if view["used_minutes"].(float64) != 60 || view["overtime"].(bool) {
		t.Fatalf("get view wrong: %+v", view)
	}

	var listData struct {
		Tickets []map[string]any `json:"tickets"`
	}
	if err := json.Unmarshal(resps[6].Data, &listData); err != nil {
		t.Fatal(err)
	}
	if len(listData.Tickets) != 1 || listData.Tickets[0]["id"] != "C1" {
		t.Fatalf("filtered list wrong: %+v", listData)
	}
	if resps[7].OK || !strings.Contains(resps[7].Error, "unknown op") {
		t.Fatalf("unknown op: %+v", resps[7])
	}
	if resps[8].OK || !strings.Contains(resps[8].Error, "invalid JSON") {
		t.Fatalf("invalid json: %+v", resps[8])
	}
}

func TestCLIPersistenceAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	first := runLines(t, dir,
		`{"op":"init","rest_days":["2026-09-30"]}`,
		`{"op":"create","id":"P1","customer":"c","assignee":"a","limit_minutes":500,"at":"2026-09-28T09:00"}`,
		`{"op":"transition","id":"P1","status":"done","at":"2026-09-28T17:00"}`,
	)
	for i, r := range first {
		if !r.OK {
			t.Fatalf("first run line %d failed: %+v", i, r)
		}
	}

	second := runLines(t, dir,
		`{"op":"get","id":"P1","at":"2026-12-31T12:00"}`,
		`{"op":"transition","id":"P1","status":"processing","at":"2026-12-31T12:01"}`,
	)
	var view map[string]any
	json.Unmarshal(second[0].Data, &view)
	if view["status"] != "done" || view["used_minutes"].(float64) != 480 {
		t.Fatalf("reopened view wrong: %+v", view)
	}
	if second[1].OK {
		t.Fatalf("change after reopen+done must fail: %+v", second[1])
	}
}

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"example.com/support-ticket-sla/internal/desk"
)

// request 是所有请求类型的并集，由 op 字段区分。
type request struct {
	Op           string   `json:"op"`
	RestDays     []string `json:"rest_days,omitempty"`
	ID           string   `json:"id,omitempty"`
	Customer     string   `json:"customer,omitempty"`
	Assignee     string   `json:"assignee,omitempty"`
	LimitMinutes int      `json:"limit_minutes,omitempty"`
	Status       string   `json:"status,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	At           string   `json:"at,omitempty"`
	Overtime     *bool    `json:"overtime,omitempty"`
}

// response 是统一的返回结构。
type response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

func main() {
	stateDir := flag.String("state", "", "directory for persistent ticket data")
	flag.Parse()
	if *stateDir == "" {
		fmt.Fprintln(os.Stderr, "missing required --state directory")
		os.Exit(2)
	}
	if err := run(os.Stdin, os.Stdout, *stateDir); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

// run 逐行读取 JSON 请求并逐行输出 JSON 结果，直到输入结束。
func run(in io.Reader, out io.Writer, stateDir string) error {
	store, err := desk.Load(stateDir)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	reader := bufio.NewReader(in)
	writer := bufio.NewWriter(out)
	defer writer.Flush()

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			writeResponse(writer, handle(store, line))
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read input: %w", err)
		}
	}
}

func handle(store *desk.Store, line []byte) response {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return errResponse(fmt.Errorf("invalid JSON: %w", err))
	}

	switch req.Op {
	case "init":
		cal, err := store.Init(req.RestDays)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(map[string]any{
			"initialized": true,
			"rest_days":   cal.Holidays,
		})

	case "create":
		at, err := desk.ParseMinuteTime(req.At)
		if err != nil {
			return errResponse(err)
		}
		t, err := store.Create(req.ID, req.Customer, req.Assignee, req.LimitMinutes, at)
		if err != nil {
			return errResponse(err)
		}
		v, err := store.Get(t.ID, at)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(v)

	case "transition":
		at, err := desk.ParseMinuteTime(req.At)
		if err != nil {
			return errResponse(err)
		}
		t, err := store.Transition(req.ID, req.Status, at, req.Reason)
		if err != nil {
			return errResponse(err)
		}
		v, err := store.Get(t.ID, at)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(v)

	case "get":
		at, err := desk.ParseMinuteTime(req.At)
		if err != nil {
			return errResponse(err)
		}
		v, err := store.Get(req.ID, at)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(v)

	case "list":
		at, err := desk.ParseMinuteTime(req.At)
		if err != nil {
			return errResponse(err)
		}
		views, err := store.List(desk.ListFilter{
			Status:   req.Status,
			Assignee: req.Assignee,
			Overtime: req.Overtime,
		}, at)
		if err != nil {
			return errResponse(err)
		}
		return okResponse(map[string]any{"tickets": views})

	default:
		return errResponse(fmt.Errorf("unknown op %q", req.Op))
	}
}

func okResponse(data any) response {
	raw, err := json.Marshal(data)
	if err != nil {
		return errResponse(err)
	}
	return response{OK: true, Data: raw}
}

func errResponse(err error) response {
	return response{OK: false, Error: err.Error()}
}

func writeResponse(w io.Writer, r response) {
	raw, _ := json.Marshal(r)
	raw = append(raw, '\n')
	_, _ = w.Write(raw)
}

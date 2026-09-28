// Command supportdesk is a line-oriented local support-ticket manager.
//
// Usage:
//
//	supportdesk --state ./data
//
// Each input line is one JSON request; each processed line produces exactly
// one JSON response line on standard output. Malformed lines yield an
// {ok:false} response instead of stopping the process.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"example.com/support-ticket-sla/internal/desk"
	"example.com/support-ticket-sla/internal/proto"
	"example.com/support-ticket-sla/internal/store"
)

func main() {
	stateDir := flag.String("state", "", "data directory for ticket state")
	flag.Parse()
	if strings.TrimSpace(*stateDir) == "" {
		fmt.Fprintln(os.Stderr, "supportdesk: --state is required")
		os.Exit(2)
	}

	st := store.NewFileStore(*stateDir)
	handler := proto.NewHandler(desk.NewService(st))

	in := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	enc := json.NewEncoder(out)

	for {
		line, err := in.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := strings.TrimSpace(string(line))
			if trimmed != "" {
				resp := processLine(handler, trimmed)
				if encErr := enc.Encode(resp); encErr != nil {
					fmt.Fprintf(os.Stderr, "supportdesk: write response: %v\n", encErr)
					os.Exit(1)
				}
				out.Flush()
			}
		}
		if err != nil {
			break // EOF or read error: normal exit
		}
	}
}

func processLine(h *proto.Handler, line string) proto.Response {
	req, err := proto.DecodeLine([]byte(line))
	if err != nil {
		return proto.Response{OK: false, Error: "invalid JSON: " + err.Error()}
	}
	return h.Handle(req)
}

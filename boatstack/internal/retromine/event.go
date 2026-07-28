// Package retromine detects recurring operator instructions in coding-agent
// transcripts, offline and deterministically. It exists to serve one control
// law: a recurring operator instruction is steady-state error — evidence that
// the controller is missing a typed observation, verb, setpoint, or guard —
// and the remedy is promotion into a typed construct, never a saved prompt.
// This package is the SENSOR of that loop: it parses transcripts into neutral
// events, finds recurrence, and (in the classify layer) names the gap. It
// proposes; it never enforces, writes, or calls anything.
//
// Purity contract: no network, no subprocesses, no filesystem — inputs arrive
// as io.Readers, randomness and wall clocks are not used, and identical
// inputs in any order produce identical output.
// control-law: retro-derivation-is-offline-and-deterministic
package retromine

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Role classifies who produced an event. Only operator events feed the
// recurrence detector: the law is about what the OPERATOR keeps having to
// say, never about what an agent generates.
const (
	RoleOperator = "operator"
	RoleAgent    = "agent"
	RoleTool     = "tool"
)

// Event is the neutral transcript unit every adapter projects into.
// The schema is deliberately minimal: source (which adapter/file), a session
// identity (recurrence across sessions is the signal; within one session it
// is just conversation), an optional RFC3339 timestamp, a role, and the text.
type Event struct {
	Source    string `json:"source"`
	SessionID string `json:"session_id"`
	Timestamp string `json:"ts,omitempty"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	// Index is the event's position within its session, assigned by the
	// parser from transcript order. It is intrinsic to the event — never to
	// the order a caller happens to concatenate inputs in — which is what
	// keeps detection order-independent.
	Index int `json:"index"`
}

// EventRef points back into the parsed input so every proposal is traceable
// to its evidence without embedding whole transcripts anywhere.
type EventRef struct {
	SessionID string `json:"session_id"`
	Index     int    `json:"index"`
}

// ParseNeutralEvents reads the neutral JSONL format (one Event per line).
// A syntactically invalid line is a typed error naming its position — never
// a silent partial parse. Blank lines are permitted.
func ParseNeutralEvents(source string, r io.Reader) ([]Event, error) {
	scanner := newLineScanner(r)
	events := []Event{}
	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, fmt.Errorf("parse neutral events %s line %d: %w", source, line, err)
		}
		if event.Source == "" {
			event.Source = source
		}
		if err := validateEvent(source, line, event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read neutral events %s: %w", source, err)
	}
	return assignSessionIndexes(events), nil
}

// assignSessionIndexes stamps each event's intrinsic per-session position in
// transcript order, overwriting whatever the input carried so the value is
// always the parser's, never a caller's claim.
func assignSessionIndexes(events []Event) []Event {
	counters := map[string]int{}
	for i := range events {
		events[i].Index = counters[events[i].SessionID]
		counters[events[i].SessionID]++
	}
	return events
}

func validateEvent(source string, line int, event Event) error {
	switch event.Role {
	case RoleOperator, RoleAgent, RoleTool:
	default:
		return fmt.Errorf("parse neutral events %s line %d: unknown role %q", source, line, event.Role)
	}
	if strings.TrimSpace(event.SessionID) == "" {
		return fmt.Errorf("parse neutral events %s line %d: session_id is required", source, line)
	}
	return nil
}

// newLineScanner returns a scanner sized for long transcript lines (tool
// results and pasted logs routinely exceed bufio's default token size).
func newLineScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return scanner
}

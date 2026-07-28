package retromine

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Adapters perform the lossy projection from one host's transcript format
// into neutral events. Lossiness is one-directional by design: an adapter may
// SKIP entries it does not understand (host formats grow shapes constantly),
// but a line that fails to parse as the format at all is a typed error —
// mis-parsing must never silently become "no recurrence found".
// control-law: retro-derivation-is-offline-and-deterministic

// Format names for ParseTranscript.
const (
	FormatNeutral    = "events"
	FormatClaudeCode = "claudecode"
	FormatPlaintext  = "plaintext"
)

// ParseTranscript dispatches to the named adapter, or sniffs the format from
// content when format is empty: a JSON object line with a "role" field is the
// neutral format, one with "type"/"message" is a Claude Code session line,
// anything else is plain text.
func ParseTranscript(format, source string, content []byte) ([]Event, error) {
	if format == "" {
		format = sniffFormat(content)
	}
	switch format {
	case FormatNeutral:
		return ParseNeutralEvents(source, strings.NewReader(string(content)))
	case FormatClaudeCode:
		return ParseClaudeCodeSession(source, strings.NewReader(string(content)))
	case FormatPlaintext:
		return ParsePlaintextTranscript(source, strings.NewReader(string(content)))
	default:
		return nil, fmt.Errorf("unknown transcript format %q (supported: events, claudecode, plaintext)", format)
	}
}

func sniffFormat(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "{") {
			return FormatPlaintext
		}
		var probe map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			return FormatPlaintext
		}
		if _, ok := probe["role"]; ok {
			return FormatNeutral
		}
		return FormatClaudeCode
	}
	return FormatPlaintext
}

// claudeCodeLine is the subset of a Claude Code session JSONL entry the
// projection needs. Message content is either a plain string or an array of
// typed blocks; only text blocks carry conversational text, and tool_result
// blocks mark tool output.
type claudeCodeLine struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// ParseClaudeCodeSession projects a Claude Code session JSONL stream into
// neutral events. Entries whose type is not user/assistant (summaries,
// hooks, system reminders) are skipped — projection is lossy — but a line
// that is not valid JSON is a typed error.
func ParseClaudeCodeSession(source string, r io.Reader) ([]Event, error) {
	scanner := newLineScanner(r)
	events := []Event{}
	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var entry claudeCodeLine
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			return nil, fmt.Errorf("parse claudecode session %s line %d: %w", source, line, err)
		}
		role := ""
		switch entry.Type {
		case "user":
			role = RoleOperator
		case "assistant":
			role = RoleAgent
		default:
			continue
		}
		text, isToolPayload := claudeCodeText(entry.Message.Content)
		if isToolPayload {
			role = RoleTool
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		sessionID := entry.SessionID
		if sessionID == "" {
			sessionID = source
		}
		events = append(events, Event{
			Source: source, SessionID: sessionID, Timestamp: entry.Timestamp,
			Role: role, Text: text,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read claudecode session %s: %w", source, err)
	}
	return assignSessionIndexes(events), nil
}

// claudeCodeText extracts conversational text from a message content value.
// The bool reports that the content was ONLY tool payload (tool results),
// which projects as RoleTool so it never counts as an operator instruction.
func claudeCodeText(content json.RawMessage) (string, bool) {
	if len(content) == 0 {
		return "", false
	}
	var plain string
	if err := json.Unmarshal(content, &plain); err == nil {
		return plain, false
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &blocks); err != nil {
		return "", false
	}
	texts := []string{}
	sawTool := false
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) != "" {
				texts = append(texts, block.Text)
			}
		case "tool_result", "tool_use":
			sawTool = true
		}
	}
	if len(texts) == 0 {
		return "", sawTool
	}
	return strings.Join(texts, "\n"), false
}

// plaintextPrefixes maps a line prefix to a role for the plain-text adapter.
// Order matters: first match wins. Unprefixed text continues the current
// speaker's turn; before any prefix appears, text defaults to the operator —
// fail-open into the INPUT only (the worst a misclassified line can do is
// create one more proposal for a human to reject; it can never act).
var plaintextPrefixes = []struct {
	prefix string
	role   string
}{
	{"user:", RoleOperator},
	{"operator:", RoleOperator},
	{"h:", RoleOperator},
	{">", RoleOperator},
	{"assistant:", RoleAgent},
	{"agent:", RoleAgent},
	{"a:", RoleAgent},
	{"tool:", RoleTool},
}

// ParsePlaintextTranscript projects a prefix-annotated plain-text transcript
// (`User: …` / `Agent: …`) into neutral events. Consecutive lines of one
// speaker merge into one event; the whole file is one session identified by
// its source name.
func ParsePlaintextTranscript(source string, r io.Reader) ([]Event, error) {
	scanner := newLineScanner(r)
	events := []Event{}
	currentRole := RoleOperator
	var current []string
	flush := func() {
		text := strings.TrimSpace(strings.Join(current, "\n"))
		current = nil
		if text == "" {
			return
		}
		events = append(events, Event{Source: source, SessionID: source, Role: currentRole, Text: text})
	}
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		matched := false
		lower := strings.ToLower(trimmed)
		for _, candidate := range plaintextPrefixes {
			if strings.HasPrefix(lower, candidate.prefix) {
				flush()
				currentRole = candidate.role
				current = append(current, strings.TrimSpace(trimmed[len(candidate.prefix):]))
				matched = true
				break
			}
		}
		if !matched {
			current = append(current, line)
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read plaintext transcript %s: %w", source, err)
	}
	return assignSessionIndexes(events), nil
}

package deliverycontrol

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const (
	commandEventSchemaVersion = 1
	commandLogFile            = "commands.jsonl"
)

// CommandEvent is one secret-free helper dispatch. It deliberately excludes
// argv, stdin, stdout, stderr, and environment values; only bounded workflow
// identity and timing fields may enter the shadow log.
type CommandEvent struct {
	SchemaVersion        int          `json:"schema_version"`
	Verb                 string       `json:"verb"`
	Category             string       `json:"category"`
	Feature              string       `json:"feature,omitempty"`
	Slice                string       `json:"slice,omitempty"`
	Transition           TransitionID `json:"transition,omitempty"`
	StartedAt            string       `json:"started_at"`
	FinishedAt           string       `json:"finished_at"`
	DurationMS           int64        `json:"duration_ms"`
	ExitCode             int          `json:"exit_code"`
	Outcome              string       `json:"outcome"`
	AuthorityFingerprint string       `json:"authority_fingerprint,omitempty"`
	OperationFingerprint string       `json:"operation_fingerprint,omitempty"`
}

func NewCommandEvent(event CommandEvent) CommandEvent {
	event.SchemaVersion = commandEventSchemaVersion
	return event
}

func validateCommandEvent(event CommandEvent) error {
	if event.SchemaVersion != commandEventSchemaVersion || event.Verb == "" || event.Category == "" {
		return errors.New("command event identity is invalid")
	}
	started, err := time.Parse(time.RFC3339Nano, event.StartedAt)
	if err != nil {
		return errors.New("command event started_at is invalid")
	}
	finished, err := time.Parse(time.RFC3339Nano, event.FinishedAt)
	if err != nil || finished.Before(started) || event.DurationMS < 0 {
		return errors.New("command event timing is invalid")
	}
	if event.Outcome != "succeeded" && event.Outcome != "failed" && event.Outcome != "usage_error" {
		return errors.New("command event outcome is invalid")
	}
	return nil
}

func AppendCommandEvent(dir string, event CommandEvent) error {
	if dir == "" {
		return errors.New("command log directory is empty")
	}
	event = NewCommandEvent(event)
	if err := validateCommandEvent(event); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(event)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, commandLogFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(line, '\n'))
	return err
}

func ReadCommandEvents(dir string) ([]CommandEvent, error) {
	file, err := os.Open(filepath.Join(dir, commandLogFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []CommandEvent{}, nil
		}
		return nil, err
	}
	defer file.Close()

	events := []CommandEvent{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var event CommandEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		if err := validateCommandEvent(event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

package deliverycontrol

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// trajectoryLogFile is the append-only record of attempts within a flow-log
// directory. One JSON object per line (JSONL), oldest first.
const trajectoryLogFile = "trajectory.jsonl"

// AppendAttempt appends one attempt to the trajectory log under dir, creating
// the directory and file as needed. It is the write half of the shadow trace;
// it returns an error so tests can assert round-trips, but the live recorder
// treats every error as best-effort and swallows it — a trace must never change
// command behavior.
func AppendAttempt(dir string, attempt TransitionAttempt) error {
	if dir == "" {
		return errors.New("trajectory log directory is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(attempt)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, trajectoryLogFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// ReadTrajectory reads the append-only log under dir in write order. A missing
// log is an empty trajectory, not an error, so a first read before any write is
// well-defined.
func ReadTrajectory(dir string) (Trajectory, error) {
	file, err := os.Open(filepath.Join(dir, trajectoryLogFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Trajectory{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var trajectory Trajectory
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var attempt TransitionAttempt
		if err := json.Unmarshal(line, &attempt); err != nil {
			return nil, err
		}
		trajectory = append(trajectory, attempt)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return trajectory, nil
}

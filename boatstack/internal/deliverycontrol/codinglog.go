package deliverycontrol

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// codingLogFile is the append-only record of coding-effort signals within a
// flow-log directory, held separate from the trajectory log so J_coding and
// J_flow can never be conflated at the storage layer. One JSON object per line.
const codingLogFile = "coding.jsonl"

// AppendCodingSignal appends one coding-effort signal under dir, creating the
// directory and file as needed. Like the trajectory writer it returns an error so
// tests can assert round-trips, while the live recorder swallows every error as
// best-effort — telemetry must never change command behavior.
func AppendCodingSignal(dir string, signal CodingSignal) error {
	if dir == "" {
		return errors.New("coding log directory is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(signal)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, codingLogFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// ReadCodingSignals reads the append-only coding log under dir in write order. A
// missing log is an empty slice, not an error, so a first read before any write
// is well-defined.
func ReadCodingSignals(dir string) ([]CodingSignal, error) {
	file, err := os.Open(filepath.Join(dir, codingLogFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []CodingSignal{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var signals []CodingSignal
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var signal CodingSignal
		if err := json.Unmarshal(line, &signal); err != nil {
			return nil, err
		}
		signals = append(signals, signal)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return signals, nil
}

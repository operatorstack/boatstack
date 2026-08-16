package invocation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type RuntimeStore interface {
	EnsureDirectory(string, uint32) error
	WriteAtomic(string, []byte, uint32) error
}

type Store struct {
	Root   string
	Writer RuntimeStore
}

func (s Store) RequestPath(runID, transitionID, requestFingerprint string) (string, error) {
	if err := validSegment(runID); err != nil {
		return "", err
	}
	if err := validSegment(transitionID); err != nil {
		return "", err
	}
	if len(requestFingerprint) != 64 {
		return "", fmt.Errorf("input request fingerprint is invalid")
	}
	return filepath.Join(s.Root, "inputs", runID, transitionID, requestFingerprint+".request.json"), nil
}

func (s Store) ReceiptPath(runID, transitionID, requestFingerprint, parameterID string) (string, error) {
	if err := validSegment(runID); err != nil {
		return "", err
	}
	if err := validSegment(transitionID); err != nil {
		return "", err
	}
	if err := validSegment(parameterID); err != nil {
		return "", err
	}
	if len(requestFingerprint) != 64 {
		return "", fmt.Errorf("input request fingerprint is invalid")
	}
	return filepath.Join(s.Root, "inputs", runID, transitionID, requestFingerprint, parameterID+".receipt.json"), nil
}

func (s Store) SaveRequest(request InputRequest) error {
	path, err := s.RequestPath(request.RunID, request.TransitionID, request.Fingerprint)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return err
	}
	return s.writeIdempotent(path, append(raw, '\n'))
}

func (s Store) LoadRequest(runID, transitionID, requestFingerprint string) (InputRequest, error) {
	path, err := s.RequestPath(runID, transitionID, requestFingerprint)
	if err != nil {
		return InputRequest{}, err
	}
	var request InputRequest
	if err := decodeStrict(path, &request); err != nil {
		return InputRequest{}, err
	}
	if err := request.Validate(); err != nil {
		return InputRequest{}, err
	}
	return request, nil
}

func (s Store) FindRequest(runID, requestFingerprint string) (InputRequest, error) {
	if err := validSegment(runID); err != nil {
		return InputRequest{}, err
	}
	if len(requestFingerprint) != 64 {
		return InputRequest{}, fmt.Errorf("input request fingerprint is invalid")
	}
	root := filepath.Join(s.Root, "inputs", runID)
	var found *InputRequest
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".request.json") {
			return nil
		}
		var request InputRequest
		if err := decodeStrict(path, &request); err != nil {
			return err
		}
		if request.Fingerprint == requestFingerprint {
			if found != nil {
				return fmt.Errorf("input request fingerprint is ambiguous")
			}
			copy := request
			found = &copy
		}
		return nil
	})
	if err != nil {
		return InputRequest{}, err
	}
	if found == nil {
		return InputRequest{}, fmt.Errorf("input request %s was not found for run %s", requestFingerprint, runID)
	}
	if err := found.Validate(); err != nil {
		return InputRequest{}, err
	}
	return *found, nil
}

// LatestRequest returns the current immutable request generation for one exact
// invocation context and verifies the complete supersession chain.
func (s Store) LatestRequest(context Context) (InputRequest, bool, error) {
	if err := validSegment(context.RunID); err != nil {
		return InputRequest{}, false, err
	}
	if err := validSegment(context.TransitionID); err != nil {
		return InputRequest{}, false, err
	}
	root := filepath.Join(s.Root, "inputs", context.RunID, context.TransitionID)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return InputRequest{}, false, nil
	}
	if err != nil {
		return InputRequest{}, false, err
	}
	byGeneration := map[uint64]InputRequest{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".request.json") {
			continue
		}
		var request InputRequest
		if err := decodeStrict(filepath.Join(root, entry.Name()), &request); err != nil {
			return InputRequest{}, false, err
		}
		if err := request.Validate(); err != nil {
			return InputRequest{}, false, err
		}
		if !requestMatchesContext(request, context) {
			continue
		}
		generation := request.EffectiveGeneration()
		if prior, exists := byGeneration[generation]; exists && prior.Fingerprint != request.Fingerprint {
			return InputRequest{}, false, fmt.Errorf("input request generation %d is ambiguous", generation)
		}
		byGeneration[generation] = request
	}
	if len(byGeneration) == 0 {
		return InputRequest{}, false, nil
	}
	var latest InputRequest
	for generation := uint64(1); generation <= uint64(len(byGeneration)); generation++ {
		request, exists := byGeneration[generation]
		if !exists {
			return InputRequest{}, false, fmt.Errorf("input request supersession chain skips generation %d", generation)
		}
		if generation > 1 && (request.Supersession == nil || request.Supersession.PreviousRequestFingerprint != latest.Fingerprint) {
			return InputRequest{}, false, fmt.Errorf("input request supersession chain is invalid at generation %d", generation)
		}
		latest = request
	}
	return latest, true, nil
}

func requestMatchesContext(request InputRequest, context Context) bool {
	return request.RunID == context.RunID && request.ProgramFingerprint == context.ProgramFingerprint &&
		request.ExecutionProgramFingerprint == context.ExecutionProgramFingerprint && request.EntryID == context.EntryID &&
		request.TargetID == context.TargetID && request.TransitionID == context.TransitionID &&
		request.StateRevision == context.StateRevision && request.ContextFingerprint == context.ContextFingerprint &&
		request.ControlBundleFingerprint == context.ControlBundleFingerprint && request.ExecutionScopeFingerprint == context.ExecutionScopeFingerprint
}

func (s Store) SaveReceipt(receipt InputReceipt) error {
	path, err := s.ReceiptPath(receipt.RunID, receipt.TransitionID, receipt.RequestFingerprint, receipt.ParameterID)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return s.writeIdempotent(path, append(raw, '\n'))
}

func (s Store) LoadReceipts(runID, transitionID string) (map[string]InputReceipt, error) {
	if err := validSegment(runID); err != nil {
		return nil, err
	}
	if err := validSegment(transitionID); err != nil {
		return nil, err
	}
	base := filepath.Join(s.Root, "inputs", runID, transitionID)
	entries, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return map[string]InputReceipt{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := map[string]InputReceipt{}
	for _, requestDirectory := range entries {
		if !requestDirectory.IsDir() || len(requestDirectory.Name()) != 64 {
			continue
		}
		receipts, readErr := os.ReadDir(filepath.Join(base, requestDirectory.Name()))
		if readErr != nil {
			return nil, readErr
		}
		for _, entry := range receipts {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".receipt.json") {
				continue
			}
			var receipt InputReceipt
			if err := decodeStrict(filepath.Join(base, requestDirectory.Name(), entry.Name()), &receipt); err != nil {
				return nil, err
			}
			result[receipt.ParameterID+"@"+receipt.RequestFingerprint] = receipt
		}
	}
	return result, nil
}

func (s Store) List(runID string) ([]InputReceipt, error) {
	if err := validSegment(runID); err != nil {
		return nil, err
	}
	root := filepath.Join(s.Root, "inputs", runID)
	var result []InputReceipt
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".receipt.json") {
			return nil
		}
		var receipt InputReceipt
		if err := decodeStrict(path, &receipt); err != nil {
			return err
		}
		result = append(result, receipt)
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ParameterID < result[j].ParameterID })
	return result, err
}

func decodeStrict(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("record contains trailing JSON")
	}
	return nil
}

func (s Store) writeIdempotent(path string, raw []byte) error {
	if prior, err := os.ReadFile(path); err == nil {
		if bytes.Equal(prior, raw) {
			return nil
		}
		return fmt.Errorf("conflicting input record already exists at %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	if s.Writer == nil {
		return fmt.Errorf("input store requires an effects-owned runtime writer")
	}
	if err := s.Writer.EnsureDirectory(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return s.Writer.WriteAtomic(path, raw, 0o600)
}

func validSegment(value string) error {
	if value == "" || filepath.Base(value) != value || value == "." || value == ".." || strings.ContainsAny(value, `/\\`) {
		return fmt.Errorf("invalid input record identity %q", value)
	}
	return nil
}

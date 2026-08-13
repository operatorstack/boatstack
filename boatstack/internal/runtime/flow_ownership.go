package runtime

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const flowProjectionOwnershipSchema = 1

// FlowProjectionOwnership is kernel-owned provenance stored in Git worktree
// metadata. Repository artifacts describe outputs but do not authorize their
// replacement or retirement.
type FlowProjectionOwnership struct {
	SchemaVersion   int               `json:"schema_version"`
	SourcePath      string            `json:"source_path"`
	ArtifactPath    string            `json:"artifact_path"`
	ArtifactSHA256  string            `json:"artifact_sha256"`
	GeneratedSkills map[string]string `json:"generated_skills"`
}

type FlowProjectionOwnershipSnapshot struct {
	Record         FlowProjectionOwnership
	exists         bool
	expectedSHA256 string
	path           string
	sourcePath     string
}

type flowProjectionOwnershipChange struct {
	prior FlowProjectionOwnershipSnapshot
	next  FlowProjectionOwnership
}

func LoadFlowProjectionOwnership(repository, sourcePath string) (FlowProjectionOwnershipSnapshot, error) {
	path, err := flowProjectionOwnershipPath(repository, sourcePath)
	if err != nil {
		return FlowProjectionOwnershipSnapshot{}, err
	}
	snapshot := FlowProjectionOwnershipSnapshot{path: path, sourcePath: filepath.ToSlash(sourcePath)}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return FlowProjectionOwnershipSnapshot{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return FlowProjectionOwnershipSnapshot{}, fmt.Errorf("FLOW_PROJECTION_OWNERSHIP_INVALID: provenance is not a regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return FlowProjectionOwnershipSnapshot{}, err
	}
	record, err := decodeFlowProjectionOwnership(raw, sourcePath)
	if err != nil {
		return FlowProjectionOwnershipSnapshot{}, err
	}
	snapshot.Record, snapshot.exists, snapshot.expectedSHA256 = record, true, projectionDigest(raw)
	return snapshot, nil
}

func NewFlowProjectionOwnership(sourcePath, artifactPath string, artifact []byte, skills map[string][]byte) FlowProjectionOwnership {
	generated := make(map[string]string, len(skills))
	for path, content := range skills {
		generated[filepath.ToSlash(path)] = projectionDigest(content)
	}
	return FlowProjectionOwnership{
		SchemaVersion: flowProjectionOwnershipSchema, SourcePath: filepath.ToSlash(sourcePath),
		ArtifactPath: filepath.ToSlash(artifactPath), ArtifactSHA256: projectionDigest(artifact), GeneratedSkills: generated,
	}
}

func (s FlowProjectionOwnershipSnapshot) Exists() bool { return s.exists }

func ApplyOwnedFlowProjection(repository string, writes []ProjectionWrite, removals []ProjectionRemoval, expectations []ProjectionExpectation, prior FlowProjectionOwnershipSnapshot, next FlowProjectionOwnership) error {
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if _, err := decodeFlowProjectionOwnership(raw, prior.sourcePath); err != nil {
		return err
	}
	return applyFlowProjectionWithOwnership(repository, writes, removals, expectations, projectionHooks{}, &flowProjectionOwnershipChange{prior: prior, next: next})
}

// AcquireFlowProjectionLease serializes Flow effect execution with official
// projection publication for one Git worktree.
func AcquireFlowProjectionLease(repository string) (*FlowProjectionLease, error) {
	lock, err := acquireProjectionLock(repository)
	if err != nil {
		return nil, err
	}
	return &FlowProjectionLease{lock: lock}, nil
}

type FlowProjectionLease struct{ lock *projectionLock }

func (l *FlowProjectionLease) Release() {
	if l != nil && l.lock != nil {
		l.lock.release()
		l.lock = nil
	}
}

func flowProjectionOwnershipPath(repository, sourcePath string) (string, error) {
	if !safeProjectionRelative(sourcePath) {
		return "", fmt.Errorf("FLOW_PROJECTION_OWNERSHIP_INVALID: source path is not canonical")
	}
	gitDirectory, err := projectionGitDirectory(repository)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(filepath.ToSlash(sourcePath)))
	return filepath.Join(gitDirectory, "boatstack-flow-projections", hex.EncodeToString(digest[:])+".json"), nil
}

func decodeFlowProjectionOwnership(raw []byte, sourcePath string) (FlowProjectionOwnership, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var record FlowProjectionOwnership
	if err := decoder.Decode(&record); err != nil {
		return FlowProjectionOwnership{}, fmt.Errorf("FLOW_PROJECTION_OWNERSHIP_INVALID: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return FlowProjectionOwnership{}, fmt.Errorf("FLOW_PROJECTION_OWNERSHIP_INVALID: trailing data")
	}
	if record.SchemaVersion != flowProjectionOwnershipSchema || record.SourcePath != filepath.ToSlash(sourcePath) || !safeProjectionRelative(record.ArtifactPath) || len(record.ArtifactSHA256) != 64 || record.GeneratedSkills == nil {
		return FlowProjectionOwnership{}, fmt.Errorf("FLOW_PROJECTION_OWNERSHIP_INVALID: provenance envelope is incomplete")
	}
	for path, digest := range record.GeneratedSkills {
		if !safeProjectionRelative(path) || len(digest) != 64 {
			return FlowProjectionOwnership{}, fmt.Errorf("FLOW_PROJECTION_OWNERSHIP_INVALID: generated output binding is invalid")
		}
	}
	return record, nil
}

func safeProjectionRelative(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, `\`) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && filepath.ToSlash(clean) == value
}

func validateOwnershipSnapshot(snapshot FlowProjectionOwnershipSnapshot) error {
	info, err := os.Lstat(snapshot.path)
	if os.IsNotExist(err) {
		if snapshot.exists {
			return fmt.Errorf("FLOW_PROJECTION_OWNERSHIP_CHANGED: provenance disappeared before commit")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !snapshot.exists || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("FLOW_PROJECTION_OWNERSHIP_CHANGED: provenance changed before commit")
	}
	raw, err := os.ReadFile(snapshot.path)
	if err != nil || projectionDigest(raw) != snapshot.expectedSHA256 {
		return fmt.Errorf("FLOW_PROJECTION_OWNERSHIP_CHANGED: provenance changed before commit")
	}
	return nil
}

func stageOwnership(snapshot FlowProjectionOwnershipSnapshot, next FlowProjectionOwnership) (string, error) {
	if snapshot.path == "" || next.SourcePath == "" {
		return "", fmt.Errorf("FLOW_PROJECTION_OWNERSHIP_INVALID: provenance update is incomplete")
	}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	directory := filepath.Dir(snapshot.path)
	if info, statErr := os.Lstat(directory); statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
		return "", fmt.Errorf("FLOW_PROJECTION_OWNERSHIP_INVALID: provenance directory is unsafe")
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return "", statErr
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	for attempt := 0; attempt < 100; attempt++ {
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return "", err
		}
		temporary := filepath.Join(directory, fmt.Sprintf(".boatstack-ownership-%x", nonce))
		file, openErr := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(openErr) {
			continue
		}
		if openErr != nil {
			return "", openErr
		}
		_, writeErr := file.Write(raw)
		if closeErr := file.Close(); writeErr == nil {
			writeErr = closeErr
		}
		if writeErr != nil {
			_ = os.Remove(temporary)
			return "", writeErr
		}
		return temporary, nil
	}
	return "", fmt.Errorf("FLOW_PROJECTION_OWNERSHIP_INVALID: cannot stage provenance")
}

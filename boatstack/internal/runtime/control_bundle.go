package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const ControlBundleSchemaVersion = 1

// ControlBundleFile binds one repository-relative control file to exact bytes.
type ControlBundleFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
	Absent bool   `json:"absent,omitempty"`
}

// ControlBundleSnapshot is the canonical executable control projection at one
// repository root.
type ControlBundleSnapshot struct {
	Fingerprint string              `json:"fingerprint"`
	Files       []ControlBundleFile `json:"files"`
}

// ControlBundleContract binds the source projection and, for execution-context
// advances, the exact projection required at the target root and revision.
type ControlBundleContract struct {
	SchemaVersion    int                    `json:"schema_version"`
	Fingerprint      string                 `json:"fingerprint"`
	Source           ControlBundleSnapshot  `json:"source"`
	Target           *ControlBundleSnapshot `json:"target,omitempty"`
	TargetRevision   string                 `json:"target_revision,omitempty"`
	SourceRuntimePin *Pin                   `json:"source_runtime_pin,omitempty"`
	TargetRuntimePin *Pin                   `json:"target_runtime_pin,omitempty"`
}

func NewControlBundleSnapshot(files map[string][]byte) (ControlBundleSnapshot, error) {
	return NewControlBundleSnapshotWithAbsent(files, nil)
}

func NewControlBundleSnapshotWithAbsent(files map[string][]byte, absent []string) (ControlBundleSnapshot, error) {
	bindings := make([]ControlBundleFile, 0, len(files))
	for path, raw := range files {
		if !safeProjectionRelative(path) {
			return ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: unsafe path %q", path)
		}
		digest := sha256.Sum256(raw)
		bindings = append(bindings, ControlBundleFile{Path: path, SHA256: hex.EncodeToString(digest[:])})
	}
	for _, path := range absent {
		if !safeProjectionRelative(path) {
			return ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: unsafe path %q", path)
		}
		bindings = append(bindings, ControlBundleFile{Path: path, Absent: true})
	}
	if len(bindings) == 0 {
		return ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: bundle has no files")
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].Path < bindings[j].Path })
	for index := 1; index < len(bindings); index++ {
		if bindings[index-1].Path == bindings[index].Path {
			return ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: duplicate path %q", bindings[index].Path)
		}
	}
	fingerprint, err := controlBundleDigest(bindings)
	if err != nil {
		return ControlBundleSnapshot{}, err
	}
	return ControlBundleSnapshot{Fingerprint: fingerprint, Files: bindings}, nil
}

// ReplaceControlBundleFile derives a target snapshot without trusting a
// caller-supplied target fingerprint.
func ReplaceControlBundleFile(snapshot ControlBundleSnapshot, path string, raw []byte) (ControlBundleSnapshot, error) {
	if err := snapshot.validate(); err != nil {
		return ControlBundleSnapshot{}, err
	}
	if !safeProjectionRelative(path) {
		return ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: unsafe path %q", path)
	}
	digest := sha256.Sum256(raw)
	binding := ControlBundleFile{Path: path, SHA256: hex.EncodeToString(digest[:])}
	files := append([]ControlBundleFile(nil), snapshot.Files...)
	replaced := false
	for index := range files {
		if files[index].Path == path {
			files[index], replaced = binding, true
			break
		}
	}
	if !replaced {
		files = append(files, binding)
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	}
	fingerprint, err := controlBundleDigest(files)
	if err != nil {
		return ControlBundleSnapshot{}, err
	}
	return ControlBundleSnapshot{Fingerprint: fingerprint, Files: files}, nil
}

func NewControlBundleContract(source ControlBundleSnapshot, target *ControlBundleSnapshot, targetRevision string) (ControlBundleContract, error) {
	return NewControlBundleContractWithPins(source, target, targetRevision, nil, nil)
}

func NewControlBundleContractWithPins(source ControlBundleSnapshot, target *ControlBundleSnapshot, targetRevision string, sourcePin, targetPin *Pin) (ControlBundleContract, error) {
	contract := ControlBundleContract{SchemaVersion: ControlBundleSchemaVersion, Source: source, Target: target, TargetRevision: targetRevision, SourceRuntimePin: sourcePin, TargetRuntimePin: targetPin}
	if err := contract.validateFields(); err != nil {
		return ControlBundleContract{}, err
	}
	identity := contract
	identity.Fingerprint = ""
	fingerprint, err := controlBundleDigest(identity)
	if err != nil {
		return ControlBundleContract{}, err
	}
	contract.Fingerprint = fingerprint
	return contract, nil
}

func (c ControlBundleContract) Validate() error {
	if err := c.validateFields(); err != nil {
		return err
	}
	identity := c
	want := identity.Fingerprint
	identity.Fingerprint = ""
	got, err := controlBundleDigest(identity)
	if err != nil || want == "" || got != want {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: contract fingerprint mismatch")
	}
	return nil
}

func (c ControlBundleContract) validateFields() error {
	if c.SchemaVersion != ControlBundleSchemaVersion {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: unsupported schema")
	}
	if err := c.Source.validate(); err != nil {
		return err
	}
	if c.Target != nil {
		if err := c.Target.validate(); err != nil {
			return err
		}
	} else if c.TargetRevision != "" {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: target revision has no target bundle")
	}
	if err := validateBoundRuntimePin(c.Source, c.SourceRuntimePin); err != nil {
		return err
	}
	if c.Target != nil {
		if err := validateBoundRuntimePin(*c.Target, c.TargetRuntimePin); err != nil {
			return err
		}
	} else if c.TargetRuntimePin != nil {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: target runtime pin has no target bundle")
	}
	if c.TargetRevision != "" && !validObjectIdentity(c.TargetRevision) {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: target revision is not an exact object identity")
	}
	return nil
}

func validateBoundRuntimePin(snapshot ControlBundleSnapshot, pin *Pin) error {
	var binding *ControlBundleFile
	for index := range snapshot.Files {
		if snapshot.Files[index].Path == ".boatstack/runtime.json" {
			binding = &snapshot.Files[index]
			break
		}
	}
	if binding == nil || binding.Absent {
		if pin != nil {
			return fmt.Errorf("CONTROL_BUNDLE_INVALID: runtime-pin metadata has no runtime file")
		}
		return nil
	}
	if pin == nil {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: runtime file has no decoded pin metadata")
	}
	raw, err := EncodePin(*pin)
	if err != nil {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: %w", err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != binding.SHA256 {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: runtime-pin metadata does not match bundle bytes")
	}
	return nil
}

func (s ControlBundleSnapshot) validate() error {
	if !validControlDigest(s.Fingerprint) || len(s.Files) == 0 {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: snapshot is incomplete")
	}
	prior := ""
	for _, file := range s.Files {
		if !safeProjectionRelative(file.Path) || file.Path <= prior || (file.Absent && file.SHA256 != "") || (!file.Absent && !validControlDigest(file.SHA256)) {
			return fmt.Errorf("CONTROL_BUNDLE_INVALID: file bindings are not canonical")
		}
		prior = file.Path
	}
	fingerprint, err := controlBundleDigest(s.Files)
	if err != nil || fingerprint != s.Fingerprint {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: snapshot fingerprint mismatch")
	}
	return nil
}

func validControlDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validObjectIdentity(value string) bool {
	if (len(value) != 40 && len(value) != 64) || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func VerifyControlBundleRoot(repository string, snapshot ControlBundleSnapshot) error {
	if err := snapshot.validate(); err != nil {
		return err
	}
	repository, err := filepath.EvalSymlinks(repository)
	if err != nil || !filepath.IsAbs(repository) || filepath.Clean(repository) != repository {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: repository root is not exact")
	}
	for _, file := range snapshot.Files {
		if file.Absent {
			if _, statErr := os.Lstat(filepath.Join(repository, filepath.FromSlash(file.Path))); !os.IsNotExist(statErr) {
				return fmt.Errorf("CONTROL_BUNDLE_STALE: %s must be absent", file.Path)
			}
			continue
		}
		raw, readErr := readBundleFile(repository, file.Path)
		if readErr != nil {
			return fmt.Errorf("CONTROL_BUNDLE_STALE: %s: %w", file.Path, readErr)
		}
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != file.SHA256 {
			return fmt.Errorf("CONTROL_BUNDLE_STALE: %s does not match the admitted bundle", file.Path)
		}
	}
	return nil
}

func VerifyControlBundleRevision(ctx context.Context, repository, revision string, snapshot ControlBundleSnapshot) error {
	if err := snapshot.validate(); err != nil {
		return err
	}
	if repository == "" || !filepath.IsAbs(repository) || revision == "" {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: revision verification is incomplete")
	}
	for _, file := range snapshot.Files {
		if file.Absent {
			command := exec.CommandContext(ctx, "git", "cat-file", "-e", revision+":"+file.Path)
			command.Dir = repository
			if command.Run() == nil {
				return fmt.Errorf("CONTROL_BUNDLE_STALE: revision %s unexpectedly contains %s", revision, file.Path)
			}
			continue
		}
		command := exec.CommandContext(ctx, "git", "show", revision+":"+file.Path)
		command.Dir = repository
		raw, err := command.Output()
		if err != nil {
			return fmt.Errorf("CONTROL_BUNDLE_STALE: revision %s lacks %s", revision, file.Path)
		}
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != file.SHA256 {
			return fmt.Errorf("CONTROL_BUNDLE_STALE: revision %s has different %s", revision, file.Path)
		}
	}
	return nil
}

func ResolveCommitRevision(ctx context.Context, repository, reference string) (string, error) {
	command := exec.CommandContext(ctx, "git", "rev-parse", "--verify", reference+"^{commit}")
	command.Dir = repository
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("resolve Git reference %q: %w", reference, err)
	}
	revision := strings.TrimSpace(string(output))
	if (len(revision) != 40 && len(revision) != 64) || strings.ToLower(revision) != revision {
		return "", fmt.Errorf("Git reference %q did not resolve to an exact object identity", reference)
	}
	if _, err := hex.DecodeString(revision); err != nil {
		return "", fmt.Errorf("Git reference %q did not resolve to an exact object identity", reference)
	}
	return revision, nil
}

// VerifyControlBundleHead binds a newly created execution context to both the
// admitted control bytes and the exact commit resolved before the effect.
func VerifyControlBundleHead(ctx context.Context, repository, revision string, snapshot ControlBundleSnapshot) error {
	command := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "HEAD^{commit}")
	command.Dir = repository
	output, err := command.Output()
	if err != nil || strings.TrimSpace(string(output)) != revision {
		return fmt.Errorf("CONTROL_BUNDLE_STALE: target HEAD does not match admitted revision %s", revision)
	}
	return VerifyControlBundleRoot(repository, snapshot)
}

func EncodeControlBundle(contract ControlBundleContract) ([]byte, error) {
	if err := contract.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(contract)
}

func DecodeControlBundle(raw []byte) (ControlBundleContract, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var contract ControlBundleContract
	if err := decoder.Decode(&contract); err != nil {
		return ControlBundleContract{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ControlBundleContract{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: trailing data")
	}
	return contract, contract.Validate()
}

func readBundleFile(repository, relative string) ([]byte, error) {
	absolute := filepath.Join(repository, filepath.FromSlash(relative))
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("control file is not a regular file")
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil || (resolvedParent != repository && !strings.HasPrefix(resolvedParent, repository+string(filepath.Separator))) {
		return nil, fmt.Errorf("control file escapes repository")
	}
	return os.ReadFile(absolute)
}

func controlBundleDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

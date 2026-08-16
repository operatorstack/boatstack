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

// ControlBundleMemberSet binds the complete direct-child membership of one
// executable control directory, not only the files discovered at projection.
type ControlBundleMemberSet struct {
	Root   string   `json:"root"`
	Suffix string   `json:"suffix"`
	Paths  []string `json:"paths"`
}

// ControlBundleSnapshot is the canonical executable control projection at one
// repository root.
type ControlBundleSnapshot struct {
	Fingerprint string                   `json:"fingerprint"`
	Files       []ControlBundleFile      `json:"files"`
	MemberSets  []ControlBundleMemberSet `json:"member_sets,omitempty"`
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
	return NewControlBundleSnapshotWithMemberSets(files, absent, nil)
}

func NewControlBundleSnapshotWithMemberSets(files map[string][]byte, absent []string, memberSets []ControlBundleMemberSet) (ControlBundleSnapshot, error) {
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
	canonicalSets, err := canonicalControlBundleMemberSets(memberSets, bindings)
	if err != nil {
		return ControlBundleSnapshot{}, err
	}
	fingerprint, err := controlBundleSnapshotDigest(bindings, canonicalSets)
	if err != nil {
		return ControlBundleSnapshot{}, err
	}
	return ControlBundleSnapshot{Fingerprint: fingerprint, Files: bindings, MemberSets: canonicalSets}, nil
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
	memberSets, err := canonicalControlBundleMemberSets(snapshot.MemberSets, files)
	if err != nil {
		return ControlBundleSnapshot{}, err
	}
	fingerprint, err := controlBundleSnapshotDigest(files, memberSets)
	if err != nil {
		return ControlBundleSnapshot{}, err
	}
	return ControlBundleSnapshot{Fingerprint: fingerprint, Files: files, MemberSets: memberSets}, nil
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
	return c.validateFingerprint()
}

// ValidateCommittedHistory accepts the current contract encoding or the exact
// earlier schema-1 snapshot encoding. The earlier encoding hashed the canonical
// file array directly, before executable directory member sets were added.
// It is valid only through a committed-history admission validator.
func (c ControlBundleContract) ValidateCommittedHistory() error {
	if err := c.Validate(); err == nil {
		return nil
	}
	if err := c.validateHistoricalFields(); err != nil {
		return err
	}
	return c.validateFingerprint()
}

func (c ControlBundleContract) validateFingerprint() error {
	identity := c
	want := identity.Fingerprint
	identity.Fingerprint = ""
	got, err := controlBundleDigest(identity)
	if err != nil || want == "" || got != want {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: contract fingerprint mismatch")
	}
	return nil
}

func (c ControlBundleContract) validateHistoricalFields() error {
	if c.SchemaVersion != ControlBundleSchemaVersion {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: unsupported schema")
	}
	if err := c.Source.validateHistorical(); err != nil {
		return err
	}
	if c.Target != nil {
		if err := c.Target.validateHistorical(); err != nil {
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
	memberSets, err := canonicalControlBundleMemberSets(s.MemberSets, s.Files)
	if err != nil || !equalControlBundleMemberSets(memberSets, s.MemberSets) {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: member sets are not canonical")
	}
	fingerprint, err := controlBundleSnapshotDigest(s.Files, s.MemberSets)
	if err != nil || fingerprint != s.Fingerprint {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: snapshot fingerprint mismatch")
	}
	return nil
}

func (s ControlBundleSnapshot) validateHistorical() error {
	if !validControlDigest(s.Fingerprint) || len(s.Files) == 0 || len(s.MemberSets) != 0 {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: historical snapshot is incomplete")
	}
	prior := ""
	for _, file := range s.Files {
		if !safeProjectionRelative(file.Path) || file.Path <= prior || (file.Absent && file.SHA256 != "") || (!file.Absent && !validControlDigest(file.SHA256)) {
			return fmt.Errorf("CONTROL_BUNDLE_INVALID: historical file bindings are not canonical")
		}
		prior = file.Path
	}
	fingerprint, err := controlBundleDigest(s.Files)
	if err != nil || fingerprint != s.Fingerprint {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: historical snapshot fingerprint mismatch")
	}
	return nil
}

func canonicalControlBundleMemberSets(values []ControlBundleMemberSet, files []ControlBundleFile) ([]ControlBundleMemberSet, error) {
	sets := make([]ControlBundleMemberSet, len(values))
	copy(sets, values)
	bound := make(map[string]bool, len(files))
	for _, file := range files {
		if !file.Absent {
			bound[file.Path] = true
		}
	}
	for index := range sets {
		memberSet := &sets[index]
		if !safeProjectionRelative(memberSet.Root) || strings.Contains(memberSet.Suffix, "/") || strings.Contains(memberSet.Suffix, "\\") || memberSet.Suffix == "" {
			return nil, fmt.Errorf("CONTROL_BUNDLE_INVALID: unsafe member set")
		}
		memberSet.Root = strings.TrimSuffix(memberSet.Root, "/")
		memberSet.Paths = append([]string(nil), memberSet.Paths...)
		sort.Strings(memberSet.Paths)
		for pathIndex, memberPath := range memberSet.Paths {
			if !safeProjectionRelative(memberPath) || filepath.ToSlash(filepath.Dir(filepath.FromSlash(memberPath))) != memberSet.Root || !strings.HasSuffix(memberPath, memberSet.Suffix) || !bound[memberPath] {
				return nil, fmt.Errorf("CONTROL_BUNDLE_INVALID: member set path %q is not a bound direct child", memberPath)
			}
			if pathIndex > 0 && memberSet.Paths[pathIndex-1] == memberPath {
				return nil, fmt.Errorf("CONTROL_BUNDLE_INVALID: duplicate member set path %q", memberPath)
			}
		}
	}
	sort.Slice(sets, func(i, j int) bool {
		if sets[i].Root != sets[j].Root {
			return sets[i].Root < sets[j].Root
		}
		return sets[i].Suffix < sets[j].Suffix
	})
	for index := 1; index < len(sets); index++ {
		if sets[index-1].Root == sets[index].Root && sets[index-1].Suffix == sets[index].Suffix {
			return nil, fmt.Errorf("CONTROL_BUNDLE_INVALID: duplicate member set")
		}
	}
	return sets, nil
}

func controlBundleSnapshotDigest(files []ControlBundleFile, memberSets []ControlBundleMemberSet) (string, error) {
	return controlBundleDigest(struct {
		Files      []ControlBundleFile      `json:"files"`
		MemberSets []ControlBundleMemberSet `json:"member_sets,omitempty"`
	}{Files: files, MemberSets: memberSets})
}

func equalControlBundleMemberSets(left, right []ControlBundleMemberSet) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Root != right[index].Root || left[index].Suffix != right[index].Suffix || !equalStrings(left[index].Paths, right[index].Paths) {
			return false
		}
	}
	return true
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func rootMemberSet(repository string, memberSet ControlBundleMemberSet) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(repository, filepath.FromSlash(memberSet.Root)))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("CONTROL_BUNDLE_STALE: read member set %s: %w", memberSet.Root, err)
	}
	paths := []string{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), memberSet.Suffix) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || entry.Type()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("CONTROL_BUNDLE_STALE: member set path %s/%s is not a regular file", memberSet.Root, entry.Name())
		}
		paths = append(paths, memberSet.Root+"/"+entry.Name())
	}
	sort.Strings(paths)
	return paths, nil
}

func revisionMemberSet(ctx context.Context, repository, revision string, memberSet ControlBundleMemberSet) ([]string, error) {
	command := exec.CommandContext(ctx, "git", "ls-tree", "-r", "-z", revision, "--", memberSet.Root)
	command.Dir = repository
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("CONTROL_BUNDLE_STALE: inspect revision %s member set %s: %w", revision, memberSet.Root, err)
	}
	paths := []string{}
	for _, record := range bytes.Split(output, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		fields := bytes.SplitN(record, []byte{'\t'}, 2)
		metadata := strings.Fields(string(fields[0]))
		if len(fields) != 2 || len(metadata) < 1 {
			return nil, fmt.Errorf("CONTROL_BUNDLE_STALE: revision member set response is malformed")
		}
		memberPath := string(fields[1])
		if filepath.ToSlash(filepath.Dir(filepath.FromSlash(memberPath))) != memberSet.Root || !strings.HasSuffix(memberPath, memberSet.Suffix) {
			continue
		}
		if metadata[0] != "100644" && metadata[0] != "100755" {
			return nil, fmt.Errorf("CONTROL_BUNDLE_STALE: revision member set path %s is not a regular file", memberPath)
		}
		paths = append(paths, memberPath)
	}
	sort.Strings(paths)
	return paths, nil
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
	for _, memberSet := range snapshot.MemberSets {
		observed, observeErr := rootMemberSet(repository, memberSet)
		if observeErr != nil {
			return observeErr
		}
		if !equalStrings(observed, memberSet.Paths) {
			return fmt.Errorf("CONTROL_BUNDLE_STALE: member set %s/*%s does not match the admitted bundle", memberSet.Root, memberSet.Suffix)
		}
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
	for _, memberSet := range snapshot.MemberSets {
		observed, observeErr := revisionMemberSet(ctx, repository, revision, memberSet)
		if observeErr != nil {
			return observeErr
		}
		if !equalStrings(observed, memberSet.Paths) {
			return fmt.Errorf("CONTROL_BUNDLE_STALE: revision %s member set %s/*%s does not match the admitted bundle", revision, memberSet.Root, memberSet.Suffix)
		}
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

// VerifyCurrentControlBundleRevision binds the admitted bundle to the exact
// commit currently checked out by the repository. Matching working-tree bytes
// cannot substitute for the committed revision that supplied authority.
func VerifyCurrentControlBundleRevision(ctx context.Context, repository, expectedRevision string, snapshot ControlBundleSnapshot) error {
	currentRevision, err := ResolveCommitRevision(ctx, repository, "HEAD")
	if err != nil {
		return err
	}
	if currentRevision != expectedRevision {
		return fmt.Errorf("CONTROL_BUNDLE_REVISION_DRIFT: expected revision %s, observed %s", expectedRevision, currentRevision)
	}
	return VerifyControlBundleRevision(ctx, repository, currentRevision, snapshot)
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

// ResolveWorkspaceBaseRevision binds a configured branch name to an exact
// commit without requiring a local branch. A local ref remains authoritative;
// repositories cloned without local default branches may fall back to the
// corresponding origin remote-tracking ref.
func ResolveWorkspaceBaseRevision(ctx context.Context, repository, reference string) (string, error) {
	revision, localErr := ResolveCommitRevision(ctx, repository, reference)
	if localErr == nil {
		return revision, nil
	}
	check := exec.CommandContext(ctx, "git", "check-ref-format", "--branch", reference)
	check.Dir = repository
	if err := check.Run(); err != nil {
		return "", localErr
	}
	remoteReference := "refs/remotes/origin/" + reference
	revision, remoteErr := ResolveCommitRevision(ctx, repository, remoteReference)
	if remoteErr != nil {
		return "", fmt.Errorf("resolve workspace base %q locally or at origin: local: %v; origin: %w", reference, localErr, remoteErr)
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

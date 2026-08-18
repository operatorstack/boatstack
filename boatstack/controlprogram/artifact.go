package controlprogram

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/hostprojection"
)

const (
	ArtifactSchemaName     = "control-program-artifact"
	ArtifactSchemaRevision = 7
)

type Artifact struct {
	Schema                         string            `json:"schema"`
	SchemaRevision                 int               `json:"schema_revision"`
	CompilerVersion                string            `json:"compiler_version"`
	SourcePath                     string            `json:"source_path"`
	SourceSHA256                   string            `json:"source_sha256"`
	DependencyLockPath             string            `json:"dependency_lock_path"`
	DependencyLockSHA256           string            `json:"dependency_lock_sha256"`
	ProgramFingerprint             string            `json:"program_fingerprint"`
	Projections                    []string          `json:"projections"`
	ProjectionSelectionFingerprint string            `json:"projection_selection_fingerprint"`
	GeneratedProjections           map[string]string `json:"generated_projections"`
	Assets                         map[string]string `json:"assets"`
	Program                        Document          `json:"program"`
}

type ArtifactInput struct {
	CompilerVersion      string
	SourcePath           string
	Source               []byte
	DependencyLockPath   string
	DependencyLock       []byte
	Projections          []hostprojection.ID
	GeneratedProjections map[string][]byte
}

// ProjectionGenerator derives repository projections from compiled executable
// semantics. Artifact digests are evidence about this trusted derivation, not
// an alternative authority for projection contents.
type ProjectionGenerator func(Compiled, []hostprojection.ID) (map[string][]byte, error)

func NewArtifact(compiled Compiled, input ArtifactInput) (Artifact, []byte, error) {
	if input.CompilerVersion == "" || !safeRelative(input.SourcePath) || !safeRelative(input.DependencyLockPath) {
		return Artifact{}, nil, fmt.Errorf("CONTROL_PROGRAM_ARTIFACT_INVALID: compiler and relative source/lock paths are required")
	}
	projectionsIDs, err := hostprojection.ParseIDs(hostprojection.Strings(input.Projections))
	if err != nil {
		return Artifact{}, nil, fmt.Errorf("CONTROL_PROGRAM_ARTIFACT_INVALID: projection selection: %w", err)
	}
	selectionFingerprint, err := hostprojection.SelectionFingerprint(projectionsIDs)
	if err != nil {
		return Artifact{}, nil, fmt.Errorf("CONTROL_PROGRAM_ARTIFACT_INVALID: projection selection: %w", err)
	}
	projections := hostprojection.Strings(projectionsIDs)
	if projections == nil {
		projections = []string{}
	}
	generated := make(map[string]string, len(input.GeneratedProjections))
	for path, raw := range input.GeneratedProjections {
		if !hostprojection.ValidFlowPath(filepath.ToSlash(path)) {
			return Artifact{}, nil, fmt.Errorf("FLOW_PROJECTION_PATH_INVALID: invalid generated projection path %q", path)
		}
		generated[filepath.ToSlash(path)] = digest(raw)
	}
	artifact := Artifact{
		Schema: ArtifactSchemaName, SchemaRevision: ArtifactSchemaRevision, CompilerVersion: input.CompilerVersion,
		SourcePath: filepath.ToSlash(input.SourcePath), SourceSHA256: digest(input.Source),
		DependencyLockPath: filepath.ToSlash(input.DependencyLockPath), DependencyLockSHA256: digest(input.DependencyLock),
		ProgramFingerprint: compiled.Fingerprint, Projections: projections, ProjectionSelectionFingerprint: selectionFingerprint,
		GeneratedProjections: generated, Assets: workAssetBindings(compiled.Document), Program: compiled.Document,
	}
	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return Artifact{}, nil, err
	}
	return artifact, append(encoded, '\n'), nil
}

func LoadArtifact(source io.Reader) (Artifact, error) {
	raw, err := readLimited(source, 32<<20, "CONTROL_PROGRAM_ARTIFACT_INVALID: input exceeds 32 MiB")
	if err != nil {
		return Artifact{}, err
	}
	if err := rejectDuplicateKeys(raw); err != nil {
		return Artifact{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var artifact Artifact
	if err := decoder.Decode(&artifact); err != nil {
		return Artifact{}, fmt.Errorf("CONTROL_PROGRAM_ARTIFACT_INVALID: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return Artifact{}, err
	}
	if artifact.Schema != ArtifactSchemaName || artifact.SchemaRevision != ArtifactSchemaRevision || artifact.CompilerVersion == "" || !safeRelative(artifact.SourcePath) || !safeRelative(artifact.DependencyLockPath) || len(artifact.ProgramFingerprint) != 64 || artifact.Projections == nil || !hostprojection.ValidSHA256(artifact.ProjectionSelectionFingerprint) || artifact.GeneratedProjections == nil || artifact.Assets == nil {
		return Artifact{}, fmt.Errorf("CONTROL_PROGRAM_ARTIFACT_INVALID: artifact envelope is incomplete")
	}
	projections, err := hostprojection.ParseIDs(artifact.Projections)
	if err != nil || !sameProjectionStrings(artifact.Projections, hostprojection.Strings(projections)) {
		return Artifact{}, fmt.Errorf("CONTROL_PROGRAM_ARTIFACT_INVALID: projection selection is not canonical")
	}
	selectionFingerprint, err := hostprojection.SelectionFingerprint(projections)
	if err != nil || selectionFingerprint != artifact.ProjectionSelectionFingerprint {
		return Artifact{}, fmt.Errorf("CONTROL_PROGRAM_ARTIFACT_INVALID: projection selection fingerprint mismatch")
	}
	for path, fingerprint := range artifact.GeneratedProjections {
		if !hostprojection.ValidFlowPath(path) || !hostprojection.ValidSHA256(fingerprint) {
			return Artifact{}, fmt.Errorf("FLOW_PROJECTION_PATH_INVALID: invalid generated projection binding")
		}
	}
	for path, fingerprint := range artifact.Assets {
		if !safeRelative(path) || len(fingerprint) != 64 {
			return Artifact{}, fmt.Errorf("CONTROL_PROGRAM_ARTIFACT_INVALID: invalid asset binding")
		}
	}
	return artifact, nil
}

func CheckArtifact(repository string, artifact Artifact, compilerVersion string, resolver BindingResolver, expectedProjections []hostprojection.ID, generate ProjectionGenerator) (Compiled, error) {
	if artifact.CompilerVersion != compilerVersion {
		return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: compiler version changed")
	}
	repository, err := filepath.Abs(repository)
	if err != nil {
		return Compiled{}, err
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return Compiled{}, err
	}
	checks := []struct{ path, expected, label string }{
		{artifact.SourcePath, artifact.SourceSHA256, "source"},
		{artifact.DependencyLockPath, artifact.DependencyLockSHA256, "dependency lock"},
	}
	for _, check := range checks {
		raw, readErr := readRepositoryFile(repository, check.path)
		if readErr != nil || digest(raw) != check.expected {
			return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: %s does not match artifact", check.label)
		}
	}
	for path, expected := range artifact.Assets {
		raw, readErr := readRepositoryFile(repository, path)
		if readErr != nil || digest(raw) != expected {
			return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: work asset %s does not match artifact", path)
		}
	}
	compiled, err := Compile(artifact.Program, resolver)
	if err != nil {
		return Compiled{}, err
	}
	if compiled.Fingerprint != artifact.ProgramFingerprint {
		return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: program fingerprint does not match artifact")
	}
	expected := map[string][]byte{}
	canonicalExpected, err := hostprojection.ParseIDs(hostprojection.Strings(expectedProjections))
	if err != nil {
		return Compiled{}, fmt.Errorf("FLOW_PROJECTION_SELECTION_STALE: expected projection selection is invalid")
	}
	wantSelectionFingerprint, err := hostprojection.SelectionFingerprint(canonicalExpected)
	if err != nil || wantSelectionFingerprint != artifact.ProjectionSelectionFingerprint || !sameProjectionStrings(artifact.Projections, hostprojection.Strings(canonicalExpected)) {
		return Compiled{}, fmt.Errorf("FLOW_PROJECTION_SELECTION_STALE: artifact projection selection does not match project configuration")
	}
	if generate != nil {
		expected, err = generate(compiled, canonicalExpected)
		if err != nil {
			return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: regenerate projections: %w", err)
		}
	}
	if len(expected) != len(artifact.GeneratedProjections) {
		return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: generated projection set does not match compiled program")
	}
	paths := make([]string, 0, len(expected))
	for path := range expected {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if !hostprojection.ValidFlowPath(path) || artifact.GeneratedProjections[path] != digest(expected[path]) {
			return Compiled{}, fmt.Errorf("FLOW_PROJECTION_PATH_INVALID: generated projection %s is not derived from compiled program", path)
		}
		raw, readErr := readRepositoryFile(repository, path)
		if readErr != nil || !bytes.Equal(raw, expected[path]) {
			return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: generated projection %s does not match compiled program", path)
		}
	}
	return compiled, nil
}

func sameProjectionStrings(one, two []string) bool {
	if len(one) != len(two) {
		return false
	}
	for index := range one {
		if one[index] != two[index] {
			return false
		}
	}
	return true
}

func workAssetBindings(document Document) map[string]string {
	result := map[string]string{}
	for _, contract := range document.Work {
		result[contract.Instructions.Path] = contract.Instructions.SHA256
		for _, output := range contract.Outputs {
			if output.Guidance != nil {
				result[output.Guidance.Path] = output.Guidance.SHA256
			}
			if output.Schema != nil {
				result[output.Schema.Path] = output.Schema.SHA256
			}
		}
	}
	return result
}

// RepositoryAssetResolver resolves exact bounded regular files below one
// canonical repository root.
type RepositoryAssetResolver struct{ Repository string }

func (r RepositoryAssetResolver) ResolveAsset(path string, maxBytes int64) ([]byte, error) {
	if !safeRelative(path) || maxBytes <= 0 {
		return nil, fmt.Errorf("asset path or bound is invalid")
	}
	root, err := filepath.Abs(r.Repository)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	raw, err := readRepositoryFile(root, path)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("asset exceeds %d bytes", maxBytes)
	}
	return raw, nil
}

func digest(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func safeRelative(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, `\\`) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator)) && filepath.ToSlash(clean) == value
}

func readRepositoryFile(repository, relative string) ([]byte, error) {
	path := filepath.Join(repository, filepath.FromSlash(relative))
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("bound path is not a regular repository file")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(repository, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("bound path escapes repository")
	}
	return os.ReadFile(resolved)
}

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
)

const (
	ArtifactSchemaName     = "control-program-artifact"
	ArtifactSchemaRevision = 1
)

type Artifact struct {
	Schema               string            `json:"schema"`
	SchemaRevision       int               `json:"schema_revision"`
	CompilerVersion      string            `json:"compiler_version"`
	SourcePath           string            `json:"source_path"`
	SourceSHA256         string            `json:"source_sha256"`
	DependencyLockPath   string            `json:"dependency_lock_path"`
	DependencyLockSHA256 string            `json:"dependency_lock_sha256"`
	ProgramFingerprint   string            `json:"program_fingerprint"`
	GeneratedSkills      map[string]string `json:"generated_skills"`
	Program              Document          `json:"program"`
}

type ArtifactInput struct {
	CompilerVersion    string
	SourcePath         string
	Source             []byte
	DependencyLockPath string
	DependencyLock     []byte
	GeneratedSkills    map[string][]byte
}

// ProjectionGenerator derives repository projections from compiled executable
// semantics. Artifact digests are evidence about this trusted derivation, not
// an alternative authority for projection contents.
type ProjectionGenerator func(Compiled) (map[string][]byte, error)

func NewArtifact(compiled Compiled, input ArtifactInput) (Artifact, []byte, error) {
	if input.CompilerVersion == "" || !safeRelative(input.SourcePath) || !safeRelative(input.DependencyLockPath) {
		return Artifact{}, nil, fmt.Errorf("CONTROL_PROGRAM_ARTIFACT_INVALID: compiler and relative source/lock paths are required")
	}
	skills := make(map[string]string, len(input.GeneratedSkills))
	for path, raw := range input.GeneratedSkills {
		if !safeGeneratedSkillPath(filepath.ToSlash(path)) {
			return Artifact{}, nil, fmt.Errorf("CONTROL_PROGRAM_ARTIFACT_INVALID: invalid generated skill path %q", path)
		}
		skills[filepath.ToSlash(path)] = digest(raw)
	}
	artifact := Artifact{
		Schema: ArtifactSchemaName, SchemaRevision: ArtifactSchemaRevision, CompilerVersion: input.CompilerVersion,
		SourcePath: filepath.ToSlash(input.SourcePath), SourceSHA256: digest(input.Source),
		DependencyLockPath: filepath.ToSlash(input.DependencyLockPath), DependencyLockSHA256: digest(input.DependencyLock),
		ProgramFingerprint: compiled.Fingerprint, GeneratedSkills: skills, Program: compiled.Document,
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
	if artifact.Schema != ArtifactSchemaName || artifact.SchemaRevision != ArtifactSchemaRevision || artifact.CompilerVersion == "" || !safeRelative(artifact.SourcePath) || !safeRelative(artifact.DependencyLockPath) || len(artifact.ProgramFingerprint) != 64 || artifact.GeneratedSkills == nil {
		return Artifact{}, fmt.Errorf("CONTROL_PROGRAM_ARTIFACT_INVALID: artifact envelope is incomplete")
	}
	for path, fingerprint := range artifact.GeneratedSkills {
		if !safeGeneratedSkillPath(path) || len(fingerprint) != 64 {
			return Artifact{}, fmt.Errorf("CONTROL_PROGRAM_ARTIFACT_INVALID: invalid generated skill binding")
		}
	}
	return artifact, nil
}

func safeGeneratedSkillPath(value string) bool {
	if !safeRelative(value) {
		return false
	}
	parts := strings.Split(value, "/")
	if len(parts) == 4 && (parts[0] == ".agents" || parts[0] == ".claude") && parts[1] == "skills" && validID(parts[2]) && parts[3] == ".gitattributes" {
		return true
	}
	if len(parts) == 4 && parts[0] == ".agents" && parts[1] == "skills" && validID(parts[2]) && parts[3] == "SKILL.md" {
		return true
	}
	if len(parts) == 5 && parts[0] == ".agents" && parts[1] == "skills" && validID(parts[2]) && parts[3] == "agents" && parts[4] == "openai.yaml" {
		return true
	}
	return len(parts) == 4 && parts[0] == ".claude" && parts[1] == "skills" && validID(parts[2]) && parts[3] == "SKILL.md"
}

func CheckArtifact(repository string, artifact Artifact, compilerVersion string, resolver BindingResolver, generate ProjectionGenerator) (Compiled, error) {
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
	compiled, err := Compile(artifact.Program, resolver)
	if err != nil {
		return Compiled{}, err
	}
	if compiled.Fingerprint != artifact.ProgramFingerprint {
		return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: program fingerprint does not match artifact")
	}
	expected := map[string][]byte{}
	if generate != nil {
		expected, err = generate(compiled)
		if err != nil {
			return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: regenerate projections: %w", err)
		}
	}
	if len(expected) != len(artifact.GeneratedSkills) {
		return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: generated skill set does not match compiled program")
	}
	paths := make([]string, 0, len(expected))
	for path := range expected {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if !safeGeneratedSkillPath(path) || artifact.GeneratedSkills[path] != digest(expected[path]) {
			return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: generated skill %s is not derived from compiled program", path)
		}
		raw, readErr := readRepositoryFile(repository, path)
		if readErr != nil || !bytes.Equal(raw, expected[path]) {
			return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: generated skill %s does not match compiled program", path)
		}
	}
	return compiled, nil
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

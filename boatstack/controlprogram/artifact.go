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

const ArtifactSchemaVersion = 1

type Artifact struct {
	SchemaVersion        int               `json:"schema_version"`
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
		SchemaVersion: ArtifactSchemaVersion, CompilerVersion: input.CompilerVersion,
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
	if artifact.SchemaVersion != ArtifactSchemaVersion || artifact.CompilerVersion == "" || !safeRelative(artifact.SourcePath) || !safeRelative(artifact.DependencyLockPath) || len(artifact.ProgramFingerprint) != 64 || artifact.GeneratedSkills == nil {
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
	if len(parts) == 4 && parts[0] == ".agents" && parts[1] == "skills" && validID(parts[2]) && parts[3] == "SKILL.md" {
		return true
	}
	if len(parts) == 5 && parts[0] == ".agents" && parts[1] == "skills" && validID(parts[2]) && parts[3] == "agents" && parts[4] == "openai.yaml" {
		return true
	}
	return len(parts) == 4 && parts[0] == ".claude" && parts[1] == "skills" && validID(parts[2]) && parts[3] == "SKILL.md"
}

func CheckArtifact(repository string, artifact Artifact, compilerVersion string, resolver BindingResolver) (Compiled, error) {
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
	paths := make([]string, 0, len(artifact.GeneratedSkills))
	for path := range artifact.GeneratedSkills {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		raw, readErr := readRepositoryFile(repository, path)
		if readErr != nil || digest(raw) != artifact.GeneratedSkills[path] {
			return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: generated skill %s does not match artifact", path)
		}
	}
	compiled, err := Compile(artifact.Program, resolver)
	if err != nil {
		return Compiled{}, err
	}
	if compiled.Fingerprint != artifact.ProgramFingerprint {
		return Compiled{}, fmt.Errorf("CONTROL_PROGRAM_STALE: program fingerprint does not match artifact")
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

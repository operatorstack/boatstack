package runtime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
)

const (
	PinSchemaVersion = 1
	HomeEnvironment  = "BOATSTACK_HOME"
)

var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Identity struct {
	Version        string `json:"version"`
	SHA256         string `json:"sha256"`
	SourceRevision string `json:"source_revision"`
}

func (i Identity) Validate() error {
	if !versionPattern.MatchString(i.Version) {
		return fmt.Errorf("runtime version must be a non-empty filesystem-safe identity")
	}
	if len(i.SHA256) != 64 {
		return fmt.Errorf("runtime SHA-256 must contain 64 lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(i.SHA256); err != nil || strings.ToLower(i.SHA256) != i.SHA256 {
		return fmt.Errorf("runtime SHA-256 must contain 64 lowercase hexadecimal characters")
	}
	if strings.TrimSpace(i.SourceRevision) == "" {
		return fmt.Errorf("runtime source revision is required")
	}
	return nil
}

type Pin struct {
	SchemaVersion      int    `json:"schema_version"`
	Version            string `json:"version"`
	SHA256             string `json:"sha256"`
	SourceRevision     string `json:"source_revision"`
	ProgramFingerprint string `json:"program_fingerprint"`
	StateSchemaVersion int    `json:"state_schema_version"`
}

func NewPin(identity Identity, programFingerprint string, stateSchemaVersion int) Pin {
	return Pin{
		SchemaVersion: PinSchemaVersion, Version: identity.Version, SHA256: identity.SHA256,
		SourceRevision: identity.SourceRevision, ProgramFingerprint: programFingerprint,
		StateSchemaVersion: stateSchemaVersion,
	}
}

func (p Pin) Identity() Identity {
	return Identity{Version: p.Version, SHA256: p.SHA256, SourceRevision: p.SourceRevision}
}

func (p Pin) Validate() error {
	if p.SchemaVersion != PinSchemaVersion {
		return fmt.Errorf("runtime pin schema %d, want %d", p.SchemaVersion, PinSchemaVersion)
	}
	if err := p.Identity().Validate(); err != nil {
		return err
	}
	if len(p.ProgramFingerprint) != 64 {
		return fmt.Errorf("runtime pin requires an exact control-program fingerprint")
	}
	if _, err := hex.DecodeString(p.ProgramFingerprint); err != nil || strings.ToLower(p.ProgramFingerprint) != p.ProgramFingerprint {
		return fmt.Errorf("runtime pin control-program fingerprint must be lowercase hexadecimal")
	}
	if p.StateSchemaVersion < 1 {
		return fmt.Errorf("runtime pin requires a state schema identity")
	}
	return nil
}

func EncodePin(pin Pin) ([]byte, error) {
	if err := pin.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(pin, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func DecodePin(raw []byte) (Pin, error) {
	var pin Pin
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pin); err != nil {
		return Pin{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Pin{}, fmt.Errorf("runtime pin contains trailing JSON")
	}
	if err := pin.Validate(); err != nil {
		return Pin{}, err
	}
	return pin, nil
}

func PinPath(repository string) string {
	return filepath.Join(repository, ".boatstack", "runtime.json")
}

func Home(explicit string) (string, error) {
	if explicit == "" {
		explicit = strings.TrimSpace(os.Getenv(HomeEnvironment))
	}
	if explicit != "" {
		absolute, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		return filepath.Clean(absolute), nil
	}
	if goruntime.GOOS == "windows" {
		if base := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); base != "" {
			return filepath.Join(base, "Boatstack"), nil
		}
	}
	if base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); base != "" {
		return filepath.Join(base, "boatstack"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "boatstack"), nil
}

func ExecutablePath(home string, identity Identity) (string, error) {
	if err := identity.Validate(); err != nil {
		return "", err
	}
	if home == "" || !filepath.IsAbs(home) {
		return "", fmt.Errorf("runtime home must be absolute")
	}
	name := "boatstack-runtime"
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(home, "runtimes", identity.Version+"-"+identity.SHA256, name), nil
}

func VerifyExecutable(path string, identity Identity) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &runtimeVerificationError{
				code: CodeRuntimeNotInstalled, identity: identity,
				cause: fmt.Errorf("pinned Boatstack runtime is not installed: %s", identity.Version+"@"+identity.SHA256),
			}
		}
		return &runtimeVerificationError{
			code: CodeRuntimeInvalid, identity: identity,
			cause: fmt.Errorf("inspect pinned Boatstack runtime: %w", err),
		}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return &runtimeVerificationError{
			code: CodeRuntimeInvalid, identity: identity,
			cause: fmt.Errorf("pinned Boatstack runtime must be an immutable regular file"),
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return &runtimeVerificationError{
			code: CodeRuntimeInvalid, identity: identity,
			cause: fmt.Errorf("read pinned Boatstack runtime: %w", err),
		}
	}
	sum := sha256.Sum256(raw)
	if actual := hex.EncodeToString(sum[:]); actual != identity.SHA256 {
		return &runtimeVerificationError{
			code: CodeRuntimeChecksumMismatch, identity: identity, actual: actual,
			cause: fmt.Errorf("pinned Boatstack runtime checksum mismatch: got %s, want %s", actual, identity.SHA256),
		}
	}
	return nil
}

// InstallExecutable adds exact bytes to the host store without ever replacing
// an existing runtime identity. A digest collision fails closed.
func InstallExecutable(source, home string, identity Identity) (string, error) {
	target, err := ExecutablePath(home, identity)
	if err != nil {
		return "", err
	}
	if err := VerifyExecutable(source, identity); err != nil {
		return "", fmt.Errorf("verify runtime candidate: %w", err)
	}
	if err := VerifyExecutable(target, identity); err == nil {
		return target, nil
	} else if _, statErr := os.Lstat(target); statErr == nil {
		return "", fmt.Errorf("Boatstack immutable runtime store collision at %s", target)
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	sourceFile, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer sourceFile.Close()
	temporary, err := os.CreateTemp(filepath.Dir(target), ".boatstack-runtime-")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, sourceFile); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Chmod(0o755); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := VerifyExecutable(temporaryPath, identity); err != nil {
		return "", fmt.Errorf("verify staged runtime candidate: %w", err)
	}
	if err := os.Link(temporaryPath, target); err != nil {
		if verifyErr := VerifyExecutable(target, identity); verifyErr == nil {
			return target, nil
		}
		return "", fmt.Errorf("install immutable Boatstack runtime: %w", err)
	}
	return target, VerifyExecutable(target, identity)
}

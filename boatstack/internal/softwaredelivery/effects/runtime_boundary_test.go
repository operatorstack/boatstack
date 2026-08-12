package effects

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/buildinfo"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

func TestRuntimeAdmissionRequiresDurableImmutableStoreArtifact(t *testing.T) {
	// control-law: temporary-staging-path-can-never-become-runtime-identity
	home := t.TempDir()
	t.Setenv(boatstackruntime.HomeEnvironment, home)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	identity := boatstackruntime.Identity{Version: buildinfo.Version, SHA256: digest, SourceRevision: "candidate-source"}
	parameters := protocol.Parameters{
		{Name: "runtime_version", Value: identity.Version},
		{Name: "runtime_sha256", Value: identity.SHA256},
		{Name: "source_revision", Value: identity.SourceRevision},
	}
	transition, _ := testprogram.StandardRegistry().Lookup("installation.update")
	admission := protocol.Admission{
		Invocation: model.InvocationContext{RuntimeVersion: identity.Version, RuntimePath: executable, RuntimeFingerprint: identity.SHA256},
		Parameters: parameters,
	}
	if err := verifyRuntimeParameters(admission, transition); err == nil {
		t.Fatal("candidate absent from the durable host store was admitted")
	}
	if _, err := boatstackruntime.InstallExecutable(executable, home, identity); err != nil {
		t.Fatal(err)
	}
	if err := verifyRuntimeParameters(admission, transition); err != nil {
		t.Fatalf("durably installed exact candidate was rejected: %v", err)
	}
	withTemporaryPath := append(parameters, protocol.Parameter{Name: "runtime_path", Value: filepath.Join(t.TempDir(), "candidate")})
	if err := withTemporaryPath.Validate(transition); err == nil {
		t.Fatal("runtime transition still accepts a physical runtime path")
	}
}

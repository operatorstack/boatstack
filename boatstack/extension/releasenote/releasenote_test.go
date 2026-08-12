package releasenote

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

const testProgram = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestReferenceExtensionPlansVerifiesAndInvalidatesNamespacedEvidence(t *testing.T) {
	// control-law: release-note-obligation-is-evidenced-by-reversible-namespaced-bytes
	repository := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repository, "release-notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "release-notes", "change.md"), []byte("### Change\n\nUser-facing behavior changed.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime := Extension{}
	request := delivery.ExtensionRequest{
		ProtocolVersion: delivery.ExtensionProtocolVersion, ExtensionID: ID, ExtensionVersion: Version,
		ProgramFingerprint: testProgram, CorrelationID: "reference", RepositoryRoot: repository,
		Capabilities: []delivery.Capability{delivery.CapabilityRepositoryWrite, delivery.CapabilityCommandExecute},
	}
	request.Operation = delivery.ExtensionObserveOperation
	observed, err := runtime.Invoke(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.Facts) != 1 || observed.Facts[0].Status != model.FactKnown || observed.Facts[0].Value != "missing" {
		t.Fatalf("initial fact = %#v", observed.Facts)
	}
	request.Operation, request.TransitionID = delivery.ExtensionPlanLocalEffectOperation, Transition
	planned, err := runtime.Invoke(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := runtime.ExtensionManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	admission := protocol.Admission{EffectiveCapabilities: append([]delivery.Capability(nil), request.Capabilities...)}
	prepared, err := effects.NewExtensionLocalPrepared(repository, ID, planned.Writes, admission, manifest.Transitions[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	request.Operation = delivery.ExtensionVerifyOperation
	verified, err := runtime.Invoke(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Verified == nil || !*verified.Verified {
		t.Fatal("fresh reference evidence did not verify")
	}
	request.ProgramFingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	stale, err := runtime.Invoke(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Verified == nil || *stale.Verified {
		t.Fatal("evidence from another ControlProgram was accepted")
	}
}

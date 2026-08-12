package boatstack_test

import (
	"context"
	"strings"
	"testing"
	"time"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/distribution"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/surfaces"
)

func TestRecoverSurfaceConsumesCompiledRegistryInsteadOfFixedProgramIDs(t *testing.T) {
	// control-law: recover-is-classified-by-the-compiled-program-not-a-surface-shadow-list
	program, err := distribution.StandardProgram(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := boatstack.NewKernel(t.TempDir(), program)
	if err != nil {
		t.Fatal(err)
	}
	request := surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationRecover,
		Repository: "/repository-is-not-consulted", Host: "cli", CorrelationID: "compiled-recovery",
		FlowID: "flow", TransitionID: "plan.create",
	}
	prescriptionSnapshot := model.Snapshot{Observation: model.Observation{StateRevision: 1, ProgramFingerprint: program.Fingerprint()}, Fingerprint: strings.Repeat("a", 64)}
	request.Prescription, err = protocol.NewPrescription(prescriptionSnapshot, catalog.Transition{ID: request.TransitionID})
	if err != nil {
		t.Fatal(err)
	}
	response, err := kernel.Handle(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "compiled control program") || response.Error == "" {
		t.Fatalf("non-recovery transition crossed recover surface: response=%+v error=%v", response, err)
	}

	request.TransitionID = "example.extension.recover"
	request.Prescription, err = protocol.NewPrescription(prescriptionSnapshot, catalog.Transition{ID: request.TransitionID})
	if err != nil {
		t.Fatal(err)
	}
	if err := request.Validate(time.Now()); err != nil {
		t.Fatalf("surface schema rejected a recovery ID before compiled-registry validation: %v", err)
	}
}

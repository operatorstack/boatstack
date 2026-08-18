package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	planningpackage "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery/planningpackage"
)

func TestPlanningPackageVerifyCommandReportsSeparateStatuses(t *testing.T) {
	repository, deliveryID, packageFingerprint := installPlanningPackageCommandFixture(t)
	output, err := captureStdout(t, func() error {
		return runFlowPlanningPackage([]string{"verify", "--repo", repository, "--delivery", deliveryID, "--package", packageFingerprint, "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var result planningpackage.Result
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("single-package JSON is not an object: %v\n%s", err, output)
	}
	if result.Integrity != planningpackage.Valid || result.Contract != planningpackage.Valid || result.Approval != planningpackage.Missing || result.CurrentProgram != planningpackage.Unavailable || result.SemanticCorrectness != "not-evaluated" || result.OriginAuthenticity != "not-proven" {
		t.Fatalf("verification result = %#v", result)
	}
	if _, err := captureStdout(t, func() error {
		return runFlowPlanningPackage([]string{"verify", "--repo", repository, "--delivery", deliveryID, "--package", packageFingerprint, "--require-approval", "--format", "json"})
	}); err == nil {
		t.Fatal("missing approval satisfied --require-approval")
	}
}

func installPlanningPackageCommandFixture(t *testing.T) (string, string, string) {
	t.Helper()
	repository, deliveryID := t.TempDir(), "proof"
	plan := []byte("# plan\n")
	output := planningpackage.Output{ID: "implementation-plan", Path: "plan.md", MediaType: "text/markdown", Required: true, Size: int64(len(plan)), SHA256: planningpackage.Digest(plan)}
	work := planningpackage.WorkContract{ID: "planning", Instructions: planningpackage.Asset{Path: "package.md", SHA256: planningpackage.Digest([]byte("coordinate")), Content: "coordinate"}, Outputs: []planningpackage.WorkOutput{{ID: output.ID, Path: output.Path, MediaType: output.MediaType, Required: true, MaxBytes: 1024}}}
	var err error
	work.Fingerprint, err = planningpackage.RuntimeWorkFingerprint(work)
	if err != nil {
		t.Fatal(err)
	}
	_, contractRaw, err := planningpackage.SealContract(planningpackage.Contract{Work: work, PlanOutput: output.ID})
	if err != nil {
		t.Fatal(err)
	}
	programFingerprint := strings.Repeat("b", 64)
	_, receiptRaw, err := planningpackage.SealWorkReceipt(planningpackage.WorkReceipt{RequestID: "request", RequestFingerprint: strings.Repeat("c", 64), ResultFingerprint: strings.Repeat("d", 64), ContractID: work.ID, ContractFingerprint: work.Fingerprint, TransitionID: "planning.package.admit", ProgramFingerprint: programFingerprint, ContextFingerprint: strings.Repeat("e", 64), StateRevision: 2, RepositoryID: "repo", WorktreeID: "tree", Outputs: []planningpackage.Output{output}})
	if err != nil {
		t.Fatal(err)
	}
	manifest, manifestRaw, err := planningpackage.SealManifest(planningpackage.Manifest{DeliveryID: deliveryID, ProgramID: "program", ProgramFingerprint: programFingerprint, EntryID: "run", RunID: "run-proof", TransitionID: "planning.package.admit", WorkContractID: work.ID, WorkContractFingerprint: work.Fingerprint, WorkRequestFingerprint: strings.Repeat("c", 64), WorkResultFingerprint: strings.Repeat("d", 64), ContextFingerprint: strings.Repeat("e", 64), StateRevision: 2, PlanOutput: planningpackage.PlanOutput{ID: output.ID, Path: output.Path, MediaType: output.MediaType, SHA256: output.SHA256}, Contract: planningpackage.Reference{Path: "contract.json", SHA256: planningpackage.Digest(contractRaw)}, WorkReceipt: planningpackage.Reference{Path: "work-receipt.json", SHA256: planningpackage.Digest(receiptRaw)}, Outputs: []planningpackage.Output{output}})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repository, ".boatstack", "planning-packages", deliveryID, manifest.Fingerprint)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"manifest.json": manifestRaw, "contract.json": contractRaw, "work-receipt.json": receiptRaw, output.Path: plan} {
		if err := os.WriteFile(filepath.Join(root, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repository, deliveryID, manifest.Fingerprint
}

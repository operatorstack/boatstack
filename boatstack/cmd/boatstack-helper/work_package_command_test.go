package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workpackage "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery/workpackage"
)

func TestWorkPackageVerifyCommandReportsSeparateStatuses(t *testing.T) {
	repository, deliveryID, packageFingerprint := installWorkPackageCommandFixture(t)
	output, err := captureStdout(t, func() error {
		return runFlowWorkPackage([]string{"verify", "--repo", repository, "--delivery", deliveryID, "--package", packageFingerprint, "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var result workpackage.Result
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("single-package JSON is not an object: %v\n%s", err, output)
	}
	if result.ProgramID != "program" || result.Integrity != workpackage.Valid || result.Contract != workpackage.Valid || result.Approval != workpackage.Missing || result.CurrentProgram != workpackage.Unavailable || result.SemanticCorrectness != "not-evaluated" || result.OriginAuthenticity != "not-proven" {
		t.Fatalf("verification result = %#v", result)
	}
	if _, err := captureStdout(t, func() error {
		return runFlowWorkPackage([]string{"verify", "--repo", repository, "--delivery", deliveryID, "--package", packageFingerprint, "--require-approval", "--format", "json"})
	}); err == nil {
		t.Fatal("missing approval satisfied --require-approval")
	}
	manifestPath := filepath.Join(repository, ".boatstack", "work-packages", deliveryID, packageFingerprint, "manifest.json")
	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifestRaw = []byte(strings.Replace(string(manifestRaw), `"program_id": "program"`, `"program_id": "missing"`, 1))
	if err := os.WriteFile(manifestPath, manifestRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = captureStdout(t, func() error {
		return runFlowWorkPackage([]string{"verify", "--repo", repository, "--delivery", deliveryID, "--package", packageFingerprint, "--require-current-program", "--format", "json"})
	})
	if err == nil || !strings.Contains(err.Error(), "failed required verification") || strings.Contains(err.Error(), "missing.flow.ir.json") {
		t.Fatalf("invalid manifest selected a Flow artifact: %v", err)
	}
}

func installWorkPackageCommandFixture(t *testing.T) (string, string, string) {
	t.Helper()
	repository, deliveryID := t.TempDir(), "proof"
	plan := []byte("# plan\n")
	output := workpackage.Output{ID: "implementation-plan", Path: "plan.md", MediaType: "text/markdown", Required: true, Size: int64(len(plan)), SHA256: workpackage.Digest(plan)}
	work := workpackage.WorkContract{ID: "planning", Instructions: workpackage.Asset{Path: "package.md", SHA256: workpackage.Digest([]byte("coordinate")), Content: "coordinate"}, Outputs: []workpackage.WorkOutput{{ID: output.ID, Path: output.Path, MediaType: output.MediaType, Required: true, MaxBytes: 1024}}}
	var err error
	work.Fingerprint, err = workpackage.RuntimeWorkFingerprint(work)
	if err != nil {
		t.Fatal(err)
	}
	_, contractRaw, err := workpackage.SealContract(workpackage.Contract{Work: work})
	if err != nil {
		t.Fatal(err)
	}
	programFingerprint := strings.Repeat("b", 64)
	_, receiptRaw, err := workpackage.SealWorkReceipt(workpackage.WorkReceipt{RequestID: "request", RequestFingerprint: strings.Repeat("c", 64), ResultFingerprint: strings.Repeat("d", 64), ContractID: work.ID, ContractFingerprint: work.Fingerprint, TransitionID: "work.package.admit", ProgramFingerprint: programFingerprint, ContextFingerprint: strings.Repeat("e", 64), StateRevision: 2, RepositoryID: "repo", WorktreeID: "tree", Outputs: []workpackage.Output{output}})
	if err != nil {
		t.Fatal(err)
	}
	manifest, manifestRaw, err := workpackage.SealManifest(workpackage.Manifest{DeliveryID: deliveryID, ProgramID: "program", ProgramFingerprint: programFingerprint, EntryID: "run", RunID: "run-proof", TransitionID: "work.package.admit", WorkContractID: work.ID, WorkContractFingerprint: work.Fingerprint, WorkRequestFingerprint: strings.Repeat("c", 64), WorkResultFingerprint: strings.Repeat("d", 64), ContextFingerprint: strings.Repeat("e", 64), StateRevision: 2, Contract: workpackage.Reference{Path: "contract.json", SHA256: workpackage.Digest(contractRaw)}, WorkReceipt: workpackage.Reference{Path: "work-receipt.json", SHA256: workpackage.Digest(receiptRaw)}, Outputs: []workpackage.Output{output}})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repository, ".boatstack", "work-packages", deliveryID, manifest.Fingerprint)
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

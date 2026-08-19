package workpackage

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func installFixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	repository := t.TempDir()
	delivery := "proof"
	plan := []byte("# plan\n")
	architecture := []byte("# architecture\n")
	tasks := []byte("{\"items\":[]}\n")
	verification := []byte("# verification\n")
	journey := []byte("# journey\n")
	programFP := strings.Repeat("b", 64)
	outputs := []Output{
		{ID: "implementation-plan", Path: "plan.md", MediaType: "text/markdown", Required: true, Size: int64(len(plan)), SHA256: Digest(plan)},
		{ID: "architecture-plan", Path: "architecture.md", MediaType: "text/markdown", Required: true, Size: int64(len(architecture)), SHA256: Digest(architecture)},
		{ID: "tasks", Path: "tasks.json", MediaType: "application/json", Required: true, Size: int64(len(tasks)), SHA256: Digest(tasks)},
		{ID: "verification-contract", Path: "verification.md", MediaType: "text/markdown", Required: true, Size: int64(len(verification)), SHA256: Digest(verification)},
		{ID: "journey", Path: "journey.md", MediaType: "text/markdown", Required: true, Size: int64(len(journey)), SHA256: Digest(journey)},
	}
	work := WorkContract{ID: "planning", Instructions: Asset{Path: "package.md", SHA256: Digest([]byte("coordinate")), Content: "coordinate"}}
	for _, output := range outputs {
		work.Outputs = append(work.Outputs, WorkOutput{ID: output.ID, Path: output.Path, MediaType: output.MediaType, Required: true, MaxBytes: 1024})
	}
	var err error
	work.Fingerprint, err = RuntimeWorkFingerprint(work)
	if err != nil {
		t.Fatal(err)
	}
	workFP := work.Fingerprint
	_, contractRaw, err := SealContract(Contract{Work: work})
	if err != nil {
		t.Fatal(err)
	}
	_, receiptRaw, err := SealWorkReceipt(WorkReceipt{RequestID: "request", RequestFingerprint: strings.Repeat("c", 64), ResultFingerprint: strings.Repeat("d", 64), ContractID: "planning", ContractFingerprint: workFP, TransitionID: "work.package.admit", ProgramFingerprint: programFP, ContextFingerprint: strings.Repeat("e", 64), StateRevision: 2, RepositoryID: "repo", WorktreeID: "tree", Outputs: outputs})
	if err != nil {
		t.Fatal(err)
	}
	manifest, manifestRaw, err := SealManifest(Manifest{DeliveryID: delivery, ProgramID: "program", ProgramFingerprint: programFP, EntryID: "run", RunID: "run-proof", TransitionID: "work.package.admit", WorkContractID: "planning", WorkContractFingerprint: workFP, WorkRequestFingerprint: strings.Repeat("c", 64), WorkResultFingerprint: strings.Repeat("d", 64), ContextFingerprint: strings.Repeat("e", 64), StateRevision: 2, Contract: Reference{Path: "contract.json", SHA256: Digest(contractRaw)}, WorkReceipt: Reference{Path: "work-receipt.json", SHA256: Digest(receiptRaw)}, Outputs: outputs})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repository, ".boatstack", "work-packages", delivery, manifest.Fingerprint)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"manifest.json": manifestRaw, "contract.json": contractRaw, "work-receipt.json": receiptRaw, "plan.md": plan, "architecture.md": architecture, "tasks.json": tasks, "verification.md": verification, "journey.md": journey} {
		if err := os.WriteFile(filepath.Join(root, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repository, delivery, manifest.Fingerprint, workFP
}

func TestVerifyRejectsContradictoryEmbeddedWorkContractIdentity(t *testing.T) {
	repository, delivery, fingerprint, _ := installFixture(t)
	oldRoot := filepath.Join(repository, ".boatstack", "work-packages", delivery, fingerprint)
	contractRaw, _ := os.ReadFile(filepath.Join(oldRoot, "contract.json"))
	var contract Contract
	if err := StrictDecode(contractRaw, &contract); err != nil {
		t.Fatal(err)
	}
	contract.Work.ID = "embedded-planning"
	contract.Work.Fingerprint, _ = RuntimeWorkFingerprint(contract.Work)
	_, contractRaw, _ = SealContract(contract)
	receiptRaw, _ := os.ReadFile(filepath.Join(oldRoot, "work-receipt.json"))
	var receipt WorkReceipt
	if err := StrictDecode(receiptRaw, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt.ContractID = "manifest-planning"
	receipt.ContractFingerprint = contract.Work.Fingerprint
	_, receiptRaw, _ = SealWorkReceipt(receipt)
	manifestRaw, _ := os.ReadFile(filepath.Join(oldRoot, "manifest.json"))
	var manifest Manifest
	if err := StrictDecode(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.WorkContractID = receipt.ContractID
	manifest.WorkContractFingerprint = contract.Work.Fingerprint
	manifest.Contract.SHA256 = Digest(contractRaw)
	manifest.WorkReceipt.SHA256 = Digest(receiptRaw)
	manifest, manifestRaw, _ = SealManifest(manifest)
	newRoot := filepath.Join(filepath.Dir(oldRoot), manifest.Fingerprint)
	if err := os.Rename(oldRoot, newRoot); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"contract.json": contractRaw, "work-receipt.json": receiptRaw, "manifest.json": manifestRaw} {
		if err := os.WriteFile(filepath.Join(newRoot, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result := Verify(repository, delivery, manifest.Fingerprint, nil)
	if result.Integrity != Invalid || result.Contract != Invalid {
		t.Fatalf("contradictory identity result=%#v", result)
	}
}

func TestVerifyApprovalUsesAuthorityReceiptContract(t *testing.T) {
	repository, delivery, fingerprint, _ := installFixture(t)
	root := filepath.Join(repository, ".boatstack", "work-packages", delivery, fingerprint)
	approval := Approval{DeliveryID: delivery, PackageFingerprint: fingerprint, ManifestFingerprint: fingerprint, AdmissionID: "admission", AuthoritySources: []AuthoritySource{{ID: "human", Class: "human", Subject: "operator", Fingerprint: "human-proof"}}, Actor: "operator", IdentityRole: "developer", IdentityProviderFingerprint: strings.Repeat("9", 64), ApprovedAt: time.Unix(100, 0).UTC()}
	_, raw, _ := SealApproval(approval)
	if err := os.WriteFile(filepath.Join(root, "approval.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if result := Verify(repository, delivery, fingerprint, nil); result.Approval != Valid || result.Integrity != Valid {
		t.Fatalf("opaque authority fingerprint result=%#v", result)
	}
	approval.AuthoritySources[0].Class = "autonomy"
	_, raw, _ = SealApproval(approval)
	if err := os.WriteFile(filepath.Join(root, "approval.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if result := Verify(repository, delivery, fingerprint, nil); result.Approval != Valid || result.Integrity != Valid {
		t.Fatalf("delegated autonomy approval result=%#v", result)
	}
	approval.AuthoritySources[0].Class = "unknown"
	_, raw, _ = SealApproval(approval)
	if err := os.WriteFile(filepath.Join(root, "approval.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if result := Verify(repository, delivery, fingerprint, nil); result.Approval != Invalid {
		t.Fatalf("unknown authority class result=%#v", result)
	}
	approval.AuthoritySources = []AuthoritySource{{ID: "repository", Class: "repository-policy", Subject: "repository", Fingerprint: "policy-proof"}}
	_, raw, _ = SealApproval(approval)
	if err := os.WriteFile(filepath.Join(root, "approval.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if result := Verify(repository, delivery, fingerprint, nil); result.Approval != Invalid {
		t.Fatalf("repository-only approval result=%#v", result)
	}
}

func TestGenericContractManifestAndApprovalContainNoPlanFields(t *testing.T) {
	repository, delivery, fingerprint, _ := installFixture(t)
	root := filepath.Join(repository, ".boatstack", "work-packages", delivery, fingerprint)
	manifestRaw, _ := os.ReadFile(filepath.Join(root, "manifest.json"))
	approval := Approval{DeliveryID: delivery, PackageFingerprint: fingerprint, ManifestFingerprint: fingerprint, AdmissionID: "admission", AuthoritySources: []AuthoritySource{{ID: "human", Class: "human", Subject: "operator", Fingerprint: "human-proof"}}, Actor: "operator", IdentityRole: "developer", IdentityProviderFingerprint: strings.Repeat("9", 64), ApprovedAt: time.Unix(100, 0).UTC()}
	_, approvalRaw, err := SealApproval(approval)
	if err != nil {
		t.Fatal(err)
	}
	contractRaw, _ := os.ReadFile(filepath.Join(root, "contract.json"))
	for name, raw := range map[string][]byte{"contract": contractRaw, "manifest": manifestRaw, "approval": approvalRaw} {
		if strings.Contains(string(raw), "plan_output") || strings.Contains(string(raw), "plan_fingerprint") {
			t.Fatalf("%s contains privileged plan fields: %s", name, raw)
		}
	}
}

func TestVerifyRejectsContractOutsideRuntimeABI(t *testing.T) {
	repository, delivery, fingerprint, _ := installFixture(t)
	oldRoot := filepath.Join(repository, ".boatstack", "work-packages", delivery, fingerprint)
	contractRaw, _ := os.ReadFile(filepath.Join(oldRoot, "contract.json"))
	var contract Contract
	if err := StrictDecode(contractRaw, &contract); err != nil {
		t.Fatal(err)
	}
	contract.Work.Outputs[0].Schema = &Asset{Path: "schema.json", Content: `{}`, SHA256: Digest([]byte(`{}`))}
	contract.Work.Fingerprint, _ = RuntimeWorkFingerprint(contract.Work)
	_, contractRaw, _ = SealContract(contract)
	manifestRaw, _ := os.ReadFile(filepath.Join(oldRoot, "manifest.json"))
	var manifest Manifest
	if err := StrictDecode(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.WorkContractFingerprint = contract.Work.Fingerprint
	manifest.Contract.SHA256 = Digest(contractRaw)
	manifest, manifestRaw, _ = SealManifest(manifest)
	newRoot := filepath.Join(filepath.Dir(oldRoot), manifest.Fingerprint)
	if err := os.Rename(oldRoot, newRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newRoot, "contract.json"), contractRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newRoot, "manifest.json"), manifestRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	result := Verify(repository, delivery, manifest.Fingerprint, nil)
	if result.Contract != Invalid || result.Integrity != Invalid {
		t.Fatalf("non-JSON schema contract result=%#v", result)
	}
}

func TestEnumeratePropagatesDeliveryReadFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission mode is Unix-specific")
	}
	repository := t.TempDir()
	delivery := filepath.Join(repository, ".boatstack", "work-packages", "proof")
	if err := os.MkdirAll(delivery, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(delivery, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(delivery, 0o700) })
	if _, err := Enumerate(repository); err == nil {
		t.Fatal("unreadable delivery directory was silently skipped")
	}
}

func TestVerifySeparatesIntegrityApprovalAndCurrentProgram(t *testing.T) {
	repository, delivery, fingerprint, workFingerprint := installFixture(t)
	result := Verify(repository, delivery, fingerprint, &CurrentProgram{ProgramFingerprint: strings.Repeat("b", 64), WorkContractFingerprint: workFingerprint})
	if result.Integrity != Valid || result.Contract != Valid || result.Approval != Missing || result.CurrentProgram != Match || result.SemanticCorrectness != "not-evaluated" || result.OriginAuthenticity != "not-proven" {
		t.Fatalf("result=%#v", result)
	}
	result = Verify(repository, delivery, fingerprint, &CurrentProgram{ProgramFingerprint: strings.Repeat("f", 64)})
	if result.Integrity != Valid || result.CurrentProgram != Different {
		t.Fatalf("historical result=%#v", result)
	}
}

func TestSafeRelativeUsesPortableContractPathSemantics(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "compiled/evidence.md", want: true},
		{value: "/absolute", want: false},
		{value: "../escape", want: false},
		{value: "a/../escape", want: false},
		{value: `a\escape`, want: false},
		{value: "C:/escape", want: false},
		{value: "C:escape", want: false},
		{value: "a//escape", want: false},
		{value: "NUL", want: false},
		{value: "nested/con.txt", want: false},
		{value: "COM1.log", want: false},
		{value: "nested/LPT9", want: false},
		{value: "plan.", want: false},
		{value: "plan ", want: false},
		{value: "nested/name:stream", want: false},
		{value: "null.md", want: true},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			if got := safeRelative(test.value); got != test.want {
				t.Fatalf("safeRelative(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestReadRegularRejectsOversizedSparseMemberBeforeLoading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "member")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(1 << 30); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readRegular(path, 1024); err == nil || !strings.Contains(err.Error(), "byte bound") {
		t.Fatalf("oversized sparse member result = %v", err)
	}
}

func TestVerifyRejectsIndependentTampering(t *testing.T) {
	cases := []string{"manifest.json", "contract.json", "work-receipt.json", "plan.md"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			repository, delivery, fingerprint, _ := installFixture(t)
			path := filepath.Join(repository, ".boatstack", "work-packages", delivery, fingerprint, name)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(raw, 'x'), 0o644); err != nil {
				t.Fatal(err)
			}
			if result := Verify(repository, delivery, fingerprint, nil); result.Integrity != Invalid {
				t.Fatalf("tampered result=%#v", result)
			}
		})
	}
}

func TestVerifyRejectsUndeclaredAndSymlinkMembers(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		repository, delivery, fingerprint, _ := installFixture(t)
		path := filepath.Join(repository, ".boatstack", "work-packages", delivery, fingerprint, "extra")
		var err error
		if symlink {
			err = os.Symlink("plan.md", path)
		} else {
			err = os.WriteFile(path, []byte("extra"), 0o644)
		}
		if err != nil {
			t.Fatal(err)
		}
		if result := Verify(repository, delivery, fingerprint, nil); result.Integrity != Invalid {
			t.Fatalf("extra member result=%#v", result)
		}
	}
}

func TestValidateOutputPathsRejectsReservedCaseAndAncestors(t *testing.T) {
	for _, outputs := range [][]WorkOutput{{{ID: "plan", Path: "Manifest.JSON", MaxBytes: 1}}, {{ID: "plan", Path: "a", MaxBytes: 1}, {ID: "detail", Path: "a/detail.md", MaxBytes: 1}}, {{ID: "plan", Path: "../plan.md", MaxBytes: 1}}} {
		if err := ValidateOutputPaths(outputs); err == nil {
			t.Fatalf("accepted paths=%#v", outputs)
		}
	}
}

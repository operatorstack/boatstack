package planningpackage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installFixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	repository := t.TempDir()
	delivery := "proof"
	plan := []byte("# plan\n")
	programFP := strings.Repeat("b", 64)
	output := Output{ID: "implementation-plan", Path: "plan.md", MediaType: "text/markdown", Required: true, Size: int64(len(plan)), SHA256: Digest(plan)}
	work := WorkContract{ID: "planning", Instructions: Asset{Path: "package.md", SHA256: Digest([]byte("coordinate")), Content: "coordinate"}, Outputs: []WorkOutput{{ID: output.ID, Path: output.Path, MediaType: output.MediaType, Required: true, MaxBytes: 1024}}}
	var err error
	work.Fingerprint, err = RuntimeWorkFingerprint(work)
	if err != nil {
		t.Fatal(err)
	}
	workFP := work.Fingerprint
	_, contractRaw, err := SealContract(Contract{Work: work, PlanOutput: output.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, receiptRaw, err := SealWorkReceipt(WorkReceipt{RequestID: "request", RequestFingerprint: strings.Repeat("c", 64), ResultFingerprint: strings.Repeat("d", 64), ContractID: "planning", ContractFingerprint: workFP, TransitionID: "planning.package.admit", ProgramFingerprint: programFP, ContextFingerprint: strings.Repeat("e", 64), StateRevision: 2, RepositoryID: "repo", WorktreeID: "tree", Outputs: []Output{output}})
	if err != nil {
		t.Fatal(err)
	}
	manifest, manifestRaw, err := SealManifest(Manifest{DeliveryID: delivery, ProgramID: "program", ProgramFingerprint: programFP, EntryID: "run", RunID: "run-proof", TransitionID: "planning.package.admit", WorkContractID: "planning", WorkContractFingerprint: workFP, WorkRequestFingerprint: strings.Repeat("c", 64), WorkResultFingerprint: strings.Repeat("d", 64), ContextFingerprint: strings.Repeat("e", 64), StateRevision: 2, PlanOutput: PlanOutput{ID: output.ID, Path: output.Path, MediaType: output.MediaType, SHA256: output.SHA256}, Contract: Reference{Path: "contract.json", SHA256: Digest(contractRaw)}, WorkReceipt: Reference{Path: "work-receipt.json", SHA256: Digest(receiptRaw)}, Outputs: []Output{output}})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repository, ".boatstack", "planning-packages", delivery, manifest.Fingerprint)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"manifest.json": manifestRaw, "contract.json": contractRaw, "work-receipt.json": receiptRaw, "plan.md": plan} {
		if err := os.WriteFile(filepath.Join(root, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repository, delivery, manifest.Fingerprint, workFP
}

func TestVerifySeparatesIntegrityApprovalAndCurrentProgram(t *testing.T) {
	repository, delivery, fingerprint, workFingerprint := installFixture(t)
	result := Verify(repository, delivery, fingerprint, &CurrentProgram{ProgramFingerprint: strings.Repeat("b", 64), WorkContractFingerprint: workFingerprint, PlanOutput: "implementation-plan"})
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
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			if got := safeRelative(test.value); got != test.want {
				t.Fatalf("safeRelative(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestVerifyRejectsIndependentTampering(t *testing.T) {
	cases := []string{"manifest.json", "contract.json", "work-receipt.json", "plan.md"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			repository, delivery, fingerprint, _ := installFixture(t)
			path := filepath.Join(repository, ".boatstack", "planning-packages", delivery, fingerprint, name)
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
		path := filepath.Join(repository, ".boatstack", "planning-packages", delivery, fingerprint, "extra")
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

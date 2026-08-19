package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
)

func workEvidenceFixture() WorkEvidence {
	content := "accepted output\n"
	digest := sha256.Sum256([]byte(content))
	return WorkEvidence{
		SchemaVersion:       WorkEvidenceSchemaVersion,
		RequestID:           "work-request",
		RequestFingerprint:  strings.Repeat("1", 64),
		RunID:               "run-current",
		ProgramID:           "work-package-proof",
		EntryID:             "accept",
		ContractID:          "work-package-proof",
		ContractFingerprint: strings.Repeat("2", 64),
		TransitionID:        catalog.TransitionID("work.package.admit"),
		ProgramFingerprint:  strings.Repeat("3", 64),
		ContextFingerprint:  strings.Repeat("4", 64),
		StateRevision:       1,
		RepositoryID:        "repository",
		WorktreeID:          "worktree",
		Outputs: []WorkOutputEvidence{{
			ID: "implementation-plan", Path: "artifacts/implementation-plan.md", MediaType: "text/markdown",
			SHA256: hex.EncodeToString(digest[:]), Size: int64(len(content)), Content: content,
		}},
	}
}

func TestWorkEvidenceSchemaBindsRequiredExecutionIdentity(t *testing.T) {
	current, err := SealWorkEvidence(workEvidenceFixture())
	if err != nil {
		t.Fatal(err)
	}
	if err := current.Validate(); err != nil {
		t.Fatalf("current work evidence rejected: %v", err)
	}

	stale := workEvidenceFixture()
	stale.SchemaVersion = 2
	if _, err := SealWorkEvidence(stale); err == nil || !strings.Contains(err.Error(), "incomplete identity") {
		t.Fatalf("schema-2 work evidence validation = %v", err)
	}
}

func TestWorkEvidenceIdentityBindsProducerResultProvenance(t *testing.T) {
	base := workEvidenceFixture()
	base.Inputs = []WorkInputEvidence{{
		ID: "architecture", Fingerprint: strings.Repeat("a", 64),
		WorkOutput: &WorkOutputProvenance{
			ReceiptID: "trc-producer", TransitionID: "work-a", WorkID: "work-a", OutputID: "architecture",
			ResultFingerprint: strings.Repeat("b", 64), ContractFingerprint: strings.Repeat("c", 64), OutputSHA256: strings.Repeat("a", 64),
		},
	}}
	first, err := SealWorkEvidence(base)
	if err != nil {
		t.Fatal(err)
	}
	substituted := base
	substituted.Inputs = append([]WorkInputEvidence(nil), base.Inputs...)
	producer := *base.Inputs[0].WorkOutput
	producer.ResultFingerprint = strings.Repeat("d", 64)
	substituted.Inputs[0].WorkOutput = &producer
	second, err := SealWorkEvidence(substituted)
	if err != nil {
		t.Fatal(err)
	}
	if first.ResultFingerprint == second.ResultFingerprint {
		t.Fatal("same input bytes from a different producer result preserved downstream identity")
	}
}

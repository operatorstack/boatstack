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

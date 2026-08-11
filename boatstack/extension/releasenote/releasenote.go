// Package releasenote is a deterministic reference extension that adds a
// conservative release-note evidence obligation to PR and merged goals.
package releasenote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

const (
	ID         = "boatstack.release-note"
	Version    = "1.0.0"
	FactID     = "boatstack.release-note.present"
	Transition = "boatstack.release-note.verify"
	Resource   = "boatstack.release-note.evidence"
	Effect     = "boatstack.release-note.write-evidence"
	Verifier   = "boatstack.release-note.verify-evidence"
)

type Extension struct{}

func Definition() control.RuntimeExtension { return Extension{} }

func (Extension) Runtime() control.ExtensionRuntime { return Extension{} }

func (Extension) ExtensionManifest(context.Context) (control.ExtensionManifest, error) {
	transition := catalog.Transition{
		ID: Transition, Version: 1, Class: catalog.EventOwnedLocal, SelectionClass: catalog.SelectionGoalRequired,
		SourcePhases: []model.ProtocolPhase{model.PhaseActive, model.PhaseTerminal}, TargetPhases: []model.ProtocolPhase{model.PhaseActive, model.PhaseTerminal},
		GoalKinds: []model.GoalKind{model.GoalOpenPR, model.GoalMerged}, RequiredIdentity: []string{"repository-id", "git-common-id", "worktree-id", "controller-id", "topology", "host", "correlation-id"},
		Authority: []catalog.AuthorityClass{catalog.AuthorityRepository}, RequiredEvidence: []string{"snapshot-fingerprint", "goal", "facet:" + FactID},
		OwnedResources: []string{Resource}, Effect: Effect, LocalEffects: []catalog.EffectID{Effect}, Idempotent: true,
		Prescription:    catalog.Prescription{Operation: Transition, ExpectedPostcondition: "release-note evidence is verified"},
		SourcePredicate: "reference-release-note-missing", AdmissionPredicate: "exact-extension-admission", TargetPredicate: "reference-release-note-verified", Verifier: Verifier,
		SourceConditions: []catalog.FacetCondition{
			known(model.FacetProgram, string(model.ProgramUnbound), string(model.ProgramCurrent)),
			known(model.FacetPlan, string(model.PlanLocked)),
			known(model.FacetVerification, string(model.VerificationCurrent)),
			known(model.FacetName(FactID), "missing"),
		},
		TargetConditions: []catalog.FacetCondition{known(model.FacetName(FactID), "verified")},
		Interruption: catalog.InterruptionContract{
			Points: []string{"after-stage", "after-effect", "before-receipt"}, PartialState: []string{"namespaced evidence may be installed"},
			Detection: "fresh extension observation", ResumeContract: "re-observe exact namespaced evidence", RollbackContract: "restore prior namespaced bytes",
			CompensationContract: "not-required", Recovery: "recovery.escalate", RecoveryAuthority: "repository-policy", ResumptionPredicate: "program and extension evidence are current",
		},
		Reversibility: catalog.Reversible, TerminalEffect: "conjunctive-goal-obligation",
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt", CostClass: "local-verification",
	}
	constraint := func(goal model.GoalKind) control.GoalConstraint {
		return control.GoalConstraint{GoalKind: goal, Conditions: []catalog.FacetCondition{known(model.FacetName(FactID), "verified", "not-required")}}
	}
	return control.ExtensionManifest{
		ID: ID, Version: Version, ProtocolVersion: control.ExtensionProtocolVersion,
		SettingsSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
		Facts:          []string{FactID}, Transitions: []control.Transition{transition},
		GoalConstraints: []control.GoalConstraint{constraint(model.GoalOpenPR), constraint(model.GoalMerged)},
		OwnedResources:  []string{Resource}, Effects: []string{Effect}, Verifiers: []string{Verifier},
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt",
	}, nil
}

func (Extension) Invoke(_ context.Context, request control.ExtensionRequest) (control.ExtensionResponse, error) {
	response := control.ExtensionResponse{
		ProtocolVersion: control.ExtensionProtocolVersion, Operation: request.Operation,
		ExtensionID: ID, ExtensionVersion: Version, CorrelationID: request.CorrelationID,
	}
	switch request.Operation {
	case control.ExtensionObserveOperation:
		fact, err := observe(request.RepositoryRoot, request.ProgramFingerprint)
		if err != nil {
			return control.ExtensionResponse{}, err
		}
		response.Facts = []control.ExtensionFact{fact}
	case control.ExtensionPlanLocalEffectOperation:
		if request.TransitionID != Transition {
			return control.ExtensionResponse{}, fmt.Errorf("release-note extension received an unknown transition")
		}
		content, err := evidenceBytes(request.RepositoryRoot, request.ProgramFingerprint)
		if err != nil {
			return control.ExtensionResponse{}, err
		}
		response.Writes = []control.ResourceWrite{{
			Resource: Resource, Path: evidencePath(request.RepositoryRoot), Content: content, SHA256: digest(content), Mode: 0o600,
		}}
	case control.ExtensionVerifyOperation:
		fact, err := observe(request.RepositoryRoot, request.ProgramFingerprint)
		if err != nil {
			return control.ExtensionResponse{}, err
		}
		verified := fact.Status == model.FactKnown && fact.Value == "verified"
		response.Verified = &verified
	default:
		return control.ExtensionResponse{}, fmt.Errorf("release-note extension does not support %q", request.Operation)
	}
	return response, nil
}

type evidence struct {
	SchemaVersion      int    `json:"schema_version"`
	ReleaseNotesSHA256 string `json:"release_notes_sha256"`
	ProgramFingerprint string `json:"program_fingerprint"`
}

func observe(repository, programFingerprint string) (control.ExtensionFact, error) {
	releaseDigest, relevant, err := releaseNotesDigest(repository)
	if err != nil {
		return control.ExtensionFact{}, err
	}
	if !relevant {
		return control.ExtensionFact{ID: FactID, Status: model.FactKnown, Value: "not-required", Fingerprint: digest([]byte("not-required"))}, nil
	}
	raw, err := os.ReadFile(evidencePath(repository))
	if err != nil {
		if os.IsNotExist(err) {
			return control.ExtensionFact{ID: FactID, Status: model.FactKnown, Value: "missing", Fingerprint: releaseDigest}, nil
		}
		return control.ExtensionFact{}, err
	}
	var record evidence
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil || record.SchemaVersion != 1 || record.ReleaseNotesSHA256 != releaseDigest || record.ProgramFingerprint != programFingerprint {
		return control.ExtensionFact{ID: FactID, Status: model.FactStale, Detail: "release-note evidence is stale", Fingerprint: digest(raw)}, nil
	}
	return control.ExtensionFact{ID: FactID, Status: model.FactKnown, Value: "verified", Fingerprint: digest(raw)}, nil
}

func evidenceBytes(repository, programFingerprint string) ([]byte, error) {
	releaseDigest, relevant, err := releaseNotesDigest(repository)
	if err != nil {
		return nil, err
	}
	if !relevant || len(programFingerprint) != 64 {
		return nil, fmt.Errorf("release-note evidence requires relevant notes and exact program identity")
	}
	raw, err := json.MarshalIndent(evidence{SchemaVersion: 1, ReleaseNotesSHA256: releaseDigest, ProgramFingerprint: programFingerprint}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func releaseNotesDigest(repository string) (string, bool, error) {
	entries, err := os.ReadDir(filepath.Join(repository, "release-notes"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return "", false, nil
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(repository, "release-notes", name))
		if err != nil {
			return "", false, err
		}
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(raw)
	}
	return hex.EncodeToString(hash.Sum(nil)), true, nil
}

func evidencePath(repository string) string {
	return filepath.Join(repository, ".boatstack", "extensions", ID, "evidence.json")
}

func digest(raw []byte) string {
	value := sha256.Sum256(raw)
	return hex.EncodeToString(value[:])
}

func known(facet model.FacetName, values ...string) catalog.FacetCondition {
	return catalog.FacetCondition{Facet: facet, Statuses: []model.FactStatus{model.FactKnown}, Values: values}
}

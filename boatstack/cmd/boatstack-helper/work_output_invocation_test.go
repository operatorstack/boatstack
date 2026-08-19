package main

import (
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/foregroundwork"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestWorkOutputProducerRejectsStaleExecutionScope(t *testing.T) {
	// control-law: completed work is evidence only for its exact run, program,
	// entry, objective, source scope, contract, transition, and entry inputs.
	work := controlprogram.WorkContract{
		ID: "planning-package", Instructions: controlprogram.WorkAsset{Path: "instructions.md", SHA256: strings.Repeat("a", 64), Content: "Plan."},
		Inputs:  []controlprogram.WorkInput{{ID: "plan", Producer: controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceEntryInput, Input: "plan"}}},
		Outputs: []controlprogram.WorkOutput{{ID: "result", Path: "result.json", MediaType: "application/json", Required: true, MaxBytes: 1024}},
	}
	contract, err := softwareflow.RuntimeWorkContract(work)
	if err != nil {
		t.Fatal(err)
	}
	programFingerprint := strings.Repeat("b", 64)
	requestFingerprint := strings.Repeat("c", 64)
	contextFingerprint := strings.Repeat("d", 64)
	current := model.InvocationContext{RepositoryID: "repo", GitCommonID: "common", WorktreeID: "worktree", Ref: "refs/heads/main"}
	objective := model.Objective{ID: "objective", TargetID: "target", TrustedClass: "target", DeliveryID: "delivery"}
	result, err := protocol.SealWorkEvidence(protocol.WorkEvidence{
		SchemaVersion: protocol.WorkEvidenceSchemaVersion, RequestID: "work-request", RequestFingerprint: requestFingerprint,
		RunID: "run-1", ProgramID: "fixture", EntryID: "run",
		ContractID: contract.ID, ContractFingerprint: contract.Fingerprint, TransitionID: "planning.admit",
		ProgramFingerprint: programFingerprint, ContextFingerprint: contextFingerprint, StateRevision: 3,
		RepositoryID: current.RepositoryID, WorktreeID: current.WorktreeID,
		Outputs: []protocol.WorkOutputEvidence{{ID: "result", Path: "result.json", MediaType: "application/json", SHA256: "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a", Size: 2, Content: "{}"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	record := foregroundwork.Record{Status: foregroundwork.StatusCompleted, Request: foregroundwork.Request{
		ID: "work-request", Fingerprint: requestFingerprint, RunID: "run-1", ProgramID: "fixture", EntryID: "run", Objective: objective,
		TransitionID: "planning.admit", Contract: *contract, Inputs: []foregroundwork.InputBinding{{ID: "plan", Value: "plan.md", Fingerprint: strings.Repeat("e", 64)}},
		RepositoryID: current.RepositoryID, GitCommonID: current.GitCommonID, WorktreeID: current.WorktreeID, Ref: current.Ref,
		ProgramFingerprint: programFingerprint, ContextFingerprint: contextFingerprint, StateRevision: 3,
	}, Result: &result}
	compiled := controlprogram.Compiled{Fingerprint: programFingerprint, Document: controlprogram.Document{
		Program: controlprogram.Program{ID: "fixture"}, Work: []controlprogram.WorkContract{work},
		Transitions: []controlprogram.Transition{{ID: "planning.admit", Work: work.ID}},
	}}
	entry := controlprogram.Entry{ID: "run"}
	options := commandOptions{runID: "run-1", objectiveID: objective.ID, targetID: string(objective.TargetID), deliveryID: objective.DeliveryID, workInputs: map[string]protocol.WorkInputValue{"planning-package/plan": {Value: "plan.md", Fingerprint: strings.Repeat("e", 64)}}}
	if err := validateWorkOutputProducer(record, work, compiled, entry, options, current); err != nil {
		t.Fatal(err)
	}
	drifted := current
	drifted.WorktreeID = "other-worktree"
	if err := validateWorkOutputProducer(record, work, compiled, entry, options, drifted); err == nil || !strings.Contains(err.Error(), "FLOW_WORK_EVIDENCE_STALE") {
		t.Fatalf("stale work output result = %v", err)
	}
}

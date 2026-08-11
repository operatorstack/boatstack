package protocol

import (
	"fmt"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

const ReceiptSchemaVersion = 2

type Outcome string

const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeRecovered Outcome = "recovered"
	OutcomeRefused   Outcome = "refused"
	OutcomeUnknown   Outcome = "external-outcome-unknown"
)

type TransitionReceipt struct {
	SchemaVersion           int                  `json:"schema_version"`
	ID                      string               `json:"id"`
	FlowID                  string               `json:"flow_id"`
	Sequence                uint64               `json:"sequence"`
	TransitionID            catalog.TransitionID `json:"transition_id"`
	TransitionVersion       int                  `json:"transition_version"`
	ProgramFingerprint      string               `json:"program_fingerprint"`
	PriorProgramFingerprint string               `json:"prior_program_fingerprint,omitempty"`
	ProgramDeltaFingerprint string               `json:"program_delta_fingerprint,omitempty"`
	ProgramChangeAccepted   bool                 `json:"program_change_accepted,omitempty"`
	RuntimeFingerprint      string               `json:"runtime_fingerprint,omitempty"`
	RuntimeSourceRevision   string               `json:"runtime_source_revision,omitempty"`
	AdmissionID             string               `json:"admission_id"`
	GoalID                  string               `json:"goal_id"`
	GoalKind                model.GoalKind       `json:"goal_kind"`
	DeliveryID              string               `json:"delivery_id"`
	SourceFingerprint       string               `json:"source_fingerprint"`
	TargetFingerprint       string               `json:"target_fingerprint"`
	AuthorityClasses        []string             `json:"authority_classes"`
	IdempotencyKey          string               `json:"idempotency_key"`
	Verifier                string               `json:"verifier"`
	Outcome                 Outcome              `json:"outcome"`
	Recovery                catalog.TransitionID `json:"recovery,omitempty"`
	Terminal                model.TerminalStatus `json:"terminal"`
	StartedAt               time.Time            `json:"started_at"`
	CompletedAt             time.Time            `json:"completed_at"`
	DurationNanoseconds     int64                `json:"duration_nanoseconds"`
	FailureClass            string               `json:"failure_class,omitempty"`
}

func NewReceipt(flowID string, sequence uint64, admission Admission, transition catalog.Transition, target model.Snapshot, startedAt, completedAt time.Time, outcome Outcome, failureClass string) (TransitionReceipt, error) {
	if flowID == "" || sequence == 0 || admission.ID == "" || target.Fingerprint == "" {
		return TransitionReceipt{}, fmt.Errorf("receipt requires flow, sequence, admission, and target identity")
	}
	if completedAt.Before(startedAt) {
		return TransitionReceipt{}, fmt.Errorf("receipt completion precedes start")
	}
	classes := make([]string, 0, len(admission.Authority.Receipts))
	for _, authority := range admission.Authority.Receipts {
		classes = append(classes, string(authority.Class))
	}
	terminal := model.TerminalUnknown
	if target.Terminal.Status == model.FactKnown {
		terminal = target.Terminal.Value
	}
	receipt := TransitionReceipt{
		SchemaVersion: ReceiptSchemaVersion, FlowID: flowID, Sequence: sequence, TransitionID: transition.ID,
		TransitionVersion: transition.Version, ProgramFingerprint: admission.ProgramFingerprint, AdmissionID: admission.ID, GoalID: admission.Goal.ID, GoalKind: admission.Goal.Kind, DeliveryID: admission.Goal.DeliveryID,
		SourceFingerprint: admission.SnapshotFingerprint, TargetFingerprint: target.Fingerprint,
		AuthorityClasses: classes, IdempotencyKey: admission.IdempotencyKey, Verifier: transition.Verifier,
		Outcome: outcome, Recovery: transition.Interruption.Recovery, Terminal: terminal,
		StartedAt: startedAt.UTC(), CompletedAt: completedAt.UTC(), DurationNanoseconds: completedAt.Sub(startedAt).Nanoseconds(),
		FailureClass: failureClass,
	}
	receipt.PriorProgramFingerprint = admission.PriorProgramFingerprint
	receipt.ProgramDeltaFingerprint = admission.ProgramDeltaFingerprint
	if accepted, ok := admission.Parameters.Get("accept_obligation_change"); ok && accepted == "true" {
		receipt.ProgramChangeAccepted = true
	}
	receipt.RuntimeFingerprint, _ = admission.Parameters.Get("runtime_sha256")
	receipt.RuntimeSourceRevision, _ = admission.Parameters.Get("source_revision")
	if transition.Policy.ReconcilesProgram && receipt.RuntimeFingerprint == "" {
		receipt.RuntimeFingerprint = admission.Invocation.RuntimeFingerprint
	}
	identity := receipt
	identity.ID = ""
	var err error
	receipt.ID, err = contentID("trc-", identity)
	if err != nil {
		return TransitionReceipt{}, err
	}
	return receipt, nil
}

func (r TransitionReceipt) Validate() error {
	if r.SchemaVersion != ReceiptSchemaVersion || r.ID == "" || r.FlowID == "" || r.Sequence == 0 || r.TransitionID == "" || r.TransitionVersion < 1 || len(r.ProgramFingerprint) != 64 || r.AdmissionID == "" || r.GoalID == "" || !r.GoalKind.Valid() || r.DeliveryID == "" || r.SourceFingerprint == "" || r.TargetFingerprint == "" || r.IdempotencyKey == "" || r.Verifier == "" {
		return fmt.Errorf("receipt has incomplete identity or evidence")
	}
	if r.StartedAt.IsZero() || r.CompletedAt.Before(r.StartedAt) || r.DurationNanoseconds != r.CompletedAt.Sub(r.StartedAt).Nanoseconds() {
		return fmt.Errorf("receipt has invalid timing evidence")
	}
	switch r.Outcome {
	case OutcomeSucceeded, OutcomeRecovered, OutcomeRefused, OutcomeUnknown:
	default:
		return fmt.Errorf("receipt has invalid outcome %q", r.Outcome)
	}
	if (r.PriorProgramFingerprint == "") != (r.ProgramDeltaFingerprint == "") {
		return fmt.Errorf("receipt has incomplete program delta identity")
	}
	if r.PriorProgramFingerprint != "" {
		delta, err := ProgramDeltaFingerprint(r.PriorProgramFingerprint, r.ProgramFingerprint)
		if err != nil || delta != r.ProgramDeltaFingerprint {
			return fmt.Errorf("receipt has invalid program delta identity")
		}
	}
	if r.ProgramChangeAccepted && (r.PriorProgramFingerprint == "" || len(r.RuntimeFingerprint) != 64) {
		return fmt.Errorf("receipt accepts a program change without exact delta and runtime identity")
	}
	if r.TransitionID == "installation.reconcile-update" && (!r.ProgramChangeAccepted || r.PriorProgramFingerprint == "" || len(r.RuntimeFingerprint) != 64 || r.RuntimeSourceRevision == "") {
		return fmt.Errorf("reconciled installation receipt lacks exact program and runtime identity")
	}
	identity := r
	want := identity.ID
	identity.ID = ""
	got, err := contentID("trc-", identity)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("receipt %q failed content identity verification", r.ID)
	}
	return nil
}

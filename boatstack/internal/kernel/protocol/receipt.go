package protocol

import (
	"fmt"
	"sort"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

const ReceiptSchemaVersion = 5

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
	RuntimeVersion          string               `json:"runtime_version,omitempty"`
	RuntimeFingerprint      string               `json:"runtime_fingerprint,omitempty"`
	RuntimeSourceRevision   string               `json:"runtime_source_revision,omitempty"`
	PrescriptionID          string               `json:"prescription_id"`
	AdmissionID             string               `json:"admission_id"`
	PriorStateRevision      uint64               `json:"prior_state_revision"`
	ResultingStateRevision  uint64               `json:"resulting_state_revision"`
	GoalID                  string               `json:"goal_id"`
	GoalKind                model.GoalKind       `json:"goal_kind"`
	DeliveryID              string               `json:"delivery_id"`
	GoalScope               catalog.GoalScope    `json:"goal_scope,omitempty"`
	GoalStatus              model.FactStatus     `json:"goal_status,omitempty"`
	SourceFingerprint       string               `json:"source_fingerprint"`
	TargetFingerprint       string               `json:"target_fingerprint"`
	AuthorityClasses        []string             `json:"authority_classes"`
	AuthorityFingerprint    string               `json:"authority_fingerprint"`
	AuthoritySources        []AuthoritySource    `json:"authority_sources"`
	RequiredCapabilities    []catalog.Capability `json:"required_capabilities"`
	GrantedCapabilities     []catalog.Capability `json:"granted_capabilities"`
	ExercisedCapabilities   []catalog.Capability `json:"exercised_capabilities"`
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

type AuthoritySource struct {
	ID          string                 `json:"id"`
	Class       catalog.AuthorityClass `json:"class"`
	Subject     string                 `json:"subject"`
	Fingerprint string                 `json:"fingerprint"`
}

func NewReceipt(flowID string, sequence uint64, admission Admission, transition catalog.Transition, target model.Snapshot, startedAt, completedAt time.Time, outcome Outcome, failureClass string) (TransitionReceipt, error) {
	if flowID == "" || sequence == 0 || admission.ID == "" || target.Fingerprint == "" {
		return TransitionReceipt{}, fmt.Errorf("receipt requires flow, sequence, admission, and target identity")
	}
	if completedAt.Before(startedAt) {
		return TransitionReceipt{}, fmt.Errorf("receipt completion precedes start")
	}
	if admission.ExpectedStateRevision == ^uint64(0) || target.StateRevision != admission.ExpectedStateRevision+1 {
		return TransitionReceipt{}, fmt.Errorf("receipt target revision must advance exactly once from the prescribed revision")
	}
	classes := make([]string, 0, len(admission.Authority.Receipts))
	sources := make([]AuthoritySource, 0, len(admission.Authority.Receipts))
	for _, authority := range admission.Authority.Receipts {
		classes = append(classes, string(authority.Class))
		sources = append(sources, AuthoritySource{ID: authority.ID, Class: authority.Class, Subject: authority.Subject, Fingerprint: authority.Fingerprint})
	}
	terminal := model.TerminalUnknown
	if target.Terminal.Status == model.FactKnown {
		terminal = target.Terminal.Value
	}
	receipt := TransitionReceipt{
		SchemaVersion: ReceiptSchemaVersion, FlowID: flowID, Sequence: sequence, TransitionID: transition.ID,
		TransitionVersion: transition.Version, ProgramFingerprint: admission.ExpectedProgramFingerprint,
		PrescriptionID: admission.PrescriptionID, AdmissionID: admission.ID,
		PriorStateRevision: admission.ExpectedStateRevision, ResultingStateRevision: target.StateRevision,
		GoalID: admission.Goal.ID, GoalKind: admission.Goal.Kind, DeliveryID: admission.Goal.DeliveryID,
		GoalScope: admission.GoalScope, GoalStatus: admission.GoalStatus,
		SourceFingerprint: admission.ExpectedSnapshotFingerprint, TargetFingerprint: target.Fingerprint,
		AuthorityClasses: classes, IdempotencyKey: admission.IdempotencyKey, Verifier: transition.Verifier,
		AuthorityFingerprint: admission.AuthorityFingerprint, AuthoritySources: sources,
		RequiredCapabilities:  append([]catalog.Capability(nil), admission.RequiredCapabilities...),
		GrantedCapabilities:   append([]catalog.Capability(nil), admission.GrantedCapabilities...),
		ExercisedCapabilities: append([]catalog.Capability(nil), admission.EffectiveCapabilities...),
		Outcome:               outcome, Recovery: transition.Interruption.Recovery, Terminal: terminal,
		StartedAt: startedAt.UTC(), CompletedAt: completedAt.UTC(), DurationNanoseconds: completedAt.Sub(startedAt).Nanoseconds(),
		FailureClass: failureClass,
	}
	receipt.PriorProgramFingerprint = admission.PriorProgramFingerprint
	receipt.ProgramDeltaFingerprint = admission.ProgramDeltaFingerprint
	if accepted, ok := admission.Parameters.Get("accept_obligation_change"); ok && accepted == "true" {
		receipt.ProgramChangeAccepted = true
	}
	if receipt.RuntimeVersion, _ = admission.Parameters.Get("runtime_version"); receipt.RuntimeVersion != "" {
		receipt.RuntimeFingerprint, _ = admission.Parameters.Get("runtime_sha256")
		receipt.RuntimeSourceRevision, _ = admission.Parameters.Get("source_revision")
	}
	if transition.Policy.ReconcilesProgram && receipt.RuntimeFingerprint == "" {
		receipt.RuntimeVersion = admission.Invocation.RuntimeVersion
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
	if r.SchemaVersion != ReceiptSchemaVersion || r.ID == "" || r.FlowID == "" || r.Sequence == 0 || r.TransitionID == "" || r.TransitionVersion < 1 || len(r.ProgramFingerprint) != 64 || r.PrescriptionID == "" || r.AdmissionID == "" || r.PriorStateRevision == 0 || r.PriorStateRevision == ^uint64(0) || r.ResultingStateRevision == 0 || r.ResultingStateRevision != r.PriorStateRevision+1 || r.SourceFingerprint == "" || r.TargetFingerprint == "" || r.AuthorityFingerprint == "" || len(r.RequiredCapabilities) == 0 || len(r.ExercisedCapabilities) == 0 || r.IdempotencyKey == "" || r.Verifier == "" {
		return fmt.Errorf("receipt has incomplete identity or evidence")
	}
	if _, err := catalog.NormalizeCapabilities("receipt.required_capabilities", r.RequiredCapabilities); err != nil {
		return err
	}
	if _, err := catalog.NormalizeCapabilities("receipt.granted_capabilities", r.GrantedCapabilities); err != nil {
		return err
	}
	if _, err := catalog.NormalizeCapabilities("receipt.exercised_capabilities", r.ExercisedCapabilities); err != nil {
		return err
	}
	if missing := catalog.MissingCapability(r.ExercisedCapabilities, catalog.NewCapabilitySet(r.GrantedCapabilities...)); missing != "" {
		return fmt.Errorf("receipt exercised ungranted capability %q", missing)
	}
	if !sameCapabilities(r.RequiredCapabilities, r.ExercisedCapabilities) {
		return fmt.Errorf("receipt exercised capabilities differ from exact admitted requirements")
	}
	if len(r.AuthoritySources) != len(r.AuthorityClasses) {
		return fmt.Errorf("receipt authority provenance and class counts differ")
	}
	authoritySet := catalog.AuthoritySet{}
	sources := append([]AuthoritySource(nil), r.AuthoritySources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
	for index, source := range sources {
		if source.ID == "" || !source.Class.Valid() || source.Class == catalog.AuthorityNone || source.Subject == "" || source.Fingerprint == "" {
			return fmt.Errorf("receipt has invalid authority provenance")
		}
		if index > 0 && sources[index-1].ID == source.ID {
			return fmt.Errorf("receipt duplicates authority source %q", source.ID)
		}
		if source != r.AuthoritySources[index] || r.AuthorityClasses[index] != string(source.Class) {
			return fmt.Errorf("receipt authority provenance is not canonical")
		}
		authoritySet[source.Class] = true
	}
	fingerprint, err := contentID("auth-", sources)
	if err != nil || fingerprint != r.AuthorityFingerprint {
		return fmt.Errorf("receipt authority fingerprint does not match its provenance")
	}
	if !sameCapabilities(r.GrantedCapabilities, catalog.AuthorityCapabilities(authoritySet).Sorted()) {
		return fmt.Errorf("receipt granted capabilities do not match authority provenance")
	}
	if !r.GoalScope.Valid() {
		return fmt.Errorf("receipt has invalid product-goal scope %q", r.GoalScope)
	}
	if r.GoalScope == catalog.GoalScopeOptionalPreserve {
		switch r.GoalStatus {
		case model.FactKnown:
			if r.GoalID == "" || !r.GoalKind.Valid() || r.DeliveryID == "" {
				return fmt.Errorf("maintenance receipt has incomplete known product-goal binding")
			}
		case model.FactAbsent:
			if r.GoalID != "" || r.GoalKind != "" || r.DeliveryID != "" {
				return fmt.Errorf("maintenance receipt invents product intent from verified absence")
			}
		default:
			return fmt.Errorf("maintenance receipt requires known or verified-absent product-goal status")
		}
	} else if r.GoalID == "" || !r.GoalKind.Valid() || r.DeliveryID == "" {
		return fmt.Errorf("receipt has incomplete product-goal identity")
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
	if (r.RuntimeVersion == "") != (r.RuntimeFingerprint == "") || (r.RuntimeVersion == "") != (r.RuntimeSourceRevision == "") {
		return fmt.Errorf("receipt has incomplete runtime version, digest, or source identity")
	}
	if r.PriorProgramFingerprint != "" {
		delta, err := ProgramDeltaFingerprint(r.PriorProgramFingerprint, r.ProgramFingerprint)
		if err != nil || delta != r.ProgramDeltaFingerprint {
			return fmt.Errorf("receipt has invalid program delta identity")
		}
	}
	if r.ProgramChangeAccepted && (r.PriorProgramFingerprint == "" || r.RuntimeVersion == "" || len(r.RuntimeFingerprint) != 64) {
		return fmt.Errorf("receipt accepts a program change without exact delta and runtime identity")
	}
	if r.TransitionID == "installation.reconcile-update" && (!r.ProgramChangeAccepted || r.PriorProgramFingerprint == "" || r.RuntimeVersion == "" || len(r.RuntimeFingerprint) != 64 || r.RuntimeSourceRevision == "") {
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

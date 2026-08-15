package protocol

import (
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

const ReceiptSchemaVersion = 13

type TransitionFactKind string

const TransitionCommitted TransitionFactKind = "transition-committed"

type ProgramIdentity struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Fingerprint string `json:"fingerprint"`
}

func (p ProgramIdentity) Validate() error {
	if p.ID == "" || p.Version == "" || !validSHA256(p.Fingerprint) {
		return fmt.Errorf("receipt requires exact program id, version, and fingerprint")
	}
	return nil
}

type EffectFactKind string

const (
	EffectResourceMutation EffectFactKind = "resource-mutation"
	EffectBoundarySettled  EffectFactKind = "effect-boundary-settled"
)

type EffectFact struct {
	Kind                 EffectFactKind   `json:"kind"`
	EffectID             catalog.EffectID `json:"effect_id"`
	Owner                string           `json:"owner"`
	Resource             string           `json:"resource,omitempty"`
	Target               string           `json:"target"`
	Operation            string           `json:"operation"`
	PriorFingerprint     string           `json:"prior_fingerprint"`
	ResultingFingerprint string           `json:"resulting_fingerprint"`
}

func (f EffectFact) Validate() error {
	if f.EffectID == "" || f.Owner == "" || f.Target == "" || f.Operation == "" || !validSHA256(f.PriorFingerprint) || !validSHA256(f.ResultingFingerprint) {
		return fmt.Errorf("receipt effect fact has incomplete identity")
	}
	switch f.Kind {
	case EffectResourceMutation:
		if f.Resource == "" {
			return fmt.Errorf("resource mutation fact requires a governed resource")
		}
	case EffectBoundarySettled:
		if f.Resource != "" || f.Operation != "settled" {
			return fmt.Errorf("settled boundary fact has invalid resource or operation")
		}
	default:
		return fmt.Errorf("receipt effect fact has invalid kind %q", f.Kind)
	}
	return nil
}

type VerificationResult string

const VerificationSatisfied VerificationResult = "satisfied"

type VerificationFact struct {
	Verifier              string             `json:"verifier"`
	ExpectedPostcondition string             `json:"expected_postcondition"`
	Result                VerificationResult `json:"result"`
	EvidenceFingerprint   string             `json:"evidence_fingerprint"`
	VerifiedAt            time.Time          `json:"verified_at"`
}

func (v VerificationFact) Validate() error {
	if v.Verifier == "" || v.ExpectedPostcondition == "" || v.Result != VerificationSatisfied || !validSHA256(v.EvidenceFingerprint) || v.VerifiedAt.IsZero() {
		return fmt.Errorf("receipt verification fact is incomplete or not satisfied")
	}
	return nil
}

// TransitionReceipt is the immutable fact for one committed transition. It is
// not a request, prescription, admission, refusal, or recovery authorization.
type TransitionReceipt struct {
	SchemaVersion                  int                      `json:"schema_version"`
	Kind                           TransitionFactKind       `json:"kind"`
	ID                             string                   `json:"id"`
	FlowID                         string                   `json:"flow_id"`
	Sequence                       uint64                   `json:"sequence"`
	Program                        ProgramIdentity          `json:"program"`
	TransitionID                   catalog.TransitionID     `json:"transition_id"`
	TransitionVersion              int                      `json:"transition_version"`
	PriorProgramFingerprint        string                   `json:"prior_program_fingerprint,omitempty"`
	ProgramDeltaFingerprint        string                   `json:"program_delta_fingerprint,omitempty"`
	ProgramChangeAccepted          bool                     `json:"program_change_accepted,omitempty"`
	RuntimeVersion                 string                   `json:"runtime_version,omitempty"`
	RuntimeFingerprint             string                   `json:"runtime_fingerprint,omitempty"`
	RuntimeSourceRevision          string                   `json:"runtime_source_revision,omitempty"`
	PrescriptionID                 string                   `json:"prescription_id"`
	AdmissionID                    string                   `json:"admission_id"`
	PriorStateRevision             uint64                   `json:"prior_state_revision"`
	ResultingStateRevision         uint64                   `json:"resulting_state_revision"`
	ObjectiveID                    string                   `json:"objective_id"`
	TargetID                       model.TargetID           `json:"target_id"`
	TrustedClass                   model.TargetID           `json:"trusted_class,omitempty"`
	DeliveryID                     string                   `json:"delivery_id"`
	ObjectiveScope                 catalog.ObjectiveScope   `json:"objective_scope,omitempty"`
	ObjectiveStatus                model.FactStatus         `json:"objective_status,omitempty"`
	ObjectiveBindingFingerprint    string                   `json:"objective_binding_fingerprint"`
	SourceFingerprint              string                   `json:"source_fingerprint"`
	TargetFingerprint              string                   `json:"target_fingerprint"`
	AuthorityFingerprint           string                   `json:"authority_fingerprint"`
	AuthoritySources               []AuthoritySource        `json:"authority_sources"`
	RequiredCapabilities           []catalog.Capability     `json:"required_capabilities"`
	GrantedCapabilities            []catalog.Capability     `json:"granted_capabilities"`
	ExercisedCapabilities          []catalog.Capability     `json:"exercised_capabilities,omitempty"`
	CommittedEffects               []EffectFact             `json:"committed_effects"`
	EffectOutputs                  Parameters               `json:"effect_outputs,omitempty"`
	ChangedStateFacets             []model.StateFacet       `json:"changed_state_facets"`
	Verification                   VerificationFact         `json:"verification"`
	IdempotencyKey                 string                   `json:"idempotency_key"`
	Recovery                       catalog.TransitionID     `json:"recovery,omitempty"`
	Terminal                       model.TerminalStatus     `json:"terminal"`
	StartedAt                      time.Time                `json:"started_at"`
	CommittedAt                    time.Time                `json:"committed_at"`
	DurationNanoseconds            int64                    `json:"duration_nanoseconds"`
	ExecutionContext               string                   `json:"execution_context,omitempty"`
	PriorInvocation                *model.InvocationContext `json:"prior_invocation,omitempty"`
	ResultingInvocation            *model.InvocationContext `json:"resulting_invocation,omitempty"`
	WorkResultFingerprint          string                   `json:"work_result_fingerprint,omitempty"`
	ControlBundleSourceFingerprint string                   `json:"control_bundle_source_fingerprint,omitempty"`
	ControlBundleTargetFingerprint string                   `json:"control_bundle_target_fingerprint,omitempty"`
	InvocationFingerprint          string                   `json:"invocation_fingerprint,omitempty"`
}

type AuthoritySource struct {
	ID          string                 `json:"id"`
	Class       catalog.AuthorityClass `json:"class"`
	Subject     string                 `json:"subject"`
	Fingerprint string                 `json:"fingerprint"`
}

func NewReceipt(flowID string, sequence uint64, program ProgramIdentity, admission Admission, transition catalog.Transition, target model.Snapshot, changedStateFacets []model.StateFacet, effects []EffectFact, outputs Parameters, exercised []catalog.Capability, startedAt, committedAt time.Time) (TransitionReceipt, error) {
	if flowID == "" || sequence == 0 || admission.ID == "" || target.Fingerprint == "" {
		return TransitionReceipt{}, fmt.Errorf("receipt requires flow, sequence, admission, and target identity")
	}
	if err := program.Validate(); err != nil {
		return TransitionReceipt{}, err
	}
	if program.Fingerprint != admission.ExpectedProgramFingerprint {
		return TransitionReceipt{}, fmt.Errorf("receipt program identity differs from admitted program")
	}
	if committedAt.Before(startedAt) {
		return TransitionReceipt{}, fmt.Errorf("receipt commit precedes effect start")
	}
	if admission.ExpectedStateRevision == ^uint64(0) || target.StateRevision != admission.ExpectedStateRevision+1 {
		return TransitionReceipt{}, fmt.Errorf("receipt target revision must advance exactly once from the prescribed revision")
	}
	resultingObjectiveBindingFingerprint, err := ObjectiveBindingFingerprint(target)
	if err != nil {
		return TransitionReceipt{}, fmt.Errorf("receipt target objective binding: %w", err)
	}
	if len(effects) == 0 {
		return TransitionReceipt{}, fmt.Errorf("committed transition requires kernel-observed effect facts")
	}
	sources := make([]AuthoritySource, 0, len(admission.Authority.Receipts))
	for _, authority := range admission.Authority.Receipts {
		sources = append(sources, AuthoritySource{ID: authority.ID, Class: authority.Class, Subject: authority.Subject, Fingerprint: authority.Fingerprint})
	}
	terminal := model.TerminalUnknown
	if target.Terminal.Status == model.FactKnown {
		terminal = target.Terminal.Value
	}
	canonicalEffects := append([]EffectFact(nil), effects...)
	sortEffectFacts(canonicalEffects)
	canonicalFacets, err := model.NormalizeStateFacets("receipt.changed_state_facets", changedStateFacets)
	if err != nil || len(canonicalFacets) == 0 {
		return TransitionReceipt{}, fmt.Errorf("receipt requires canonical changed state facets: %v", err)
	}
	receipt := TransitionReceipt{
		SchemaVersion: ReceiptSchemaVersion, Kind: TransitionCommitted, FlowID: flowID, Sequence: sequence, Program: program,
		TransitionID: transition.ID, TransitionVersion: transition.Version,
		PrescriptionID: admission.PrescriptionID, AdmissionID: admission.ID,
		PriorStateRevision: admission.ExpectedStateRevision, ResultingStateRevision: target.StateRevision,
		ObjectiveID: admission.Objective.ID, TargetID: admission.Objective.TargetID, TrustedClass: admission.Objective.TrustedClass, DeliveryID: admission.Objective.DeliveryID,
		ObjectiveScope: admission.ObjectiveScope, ObjectiveStatus: admission.ObjectiveStatus,
		ObjectiveBindingFingerprint: resultingObjectiveBindingFingerprint,
		SourceFingerprint:           admission.ExpectedSnapshotFingerprint, TargetFingerprint: target.Fingerprint,
		AuthorityFingerprint: admission.AuthorityFingerprint, AuthoritySources: sources,
		RequiredCapabilities:  append([]catalog.Capability(nil), admission.RequiredCapabilities...),
		GrantedCapabilities:   append([]catalog.Capability(nil), admission.GrantedCapabilities...),
		ExercisedCapabilities: append([]catalog.Capability(nil), exercised...),
		CommittedEffects:      canonicalEffects,
		EffectOutputs:         outputs.Canonical(),
		ChangedStateFacets:    canonicalFacets,
		Verification:          VerificationFact{Verifier: transition.Verifier, ExpectedPostcondition: transition.TargetPredicate, Result: VerificationSatisfied, EvidenceFingerprint: target.Fingerprint, VerifiedAt: committedAt.UTC()},
		IdempotencyKey:        admission.IdempotencyKey, Recovery: transition.Interruption.Recovery, Terminal: terminal,
		StartedAt: startedAt.UTC(), CommittedAt: committedAt.UTC(), DurationNanoseconds: committedAt.Sub(startedAt).Nanoseconds(),
		InvocationFingerprint: admission.InvocationFingerprint,
	}
	if admission.Work != nil {
		receipt.WorkResultFingerprint = admission.Work.ResultFingerprint
	}
	if admission.ControlBundle != nil {
		receipt.ControlBundleSourceFingerprint = admission.ControlBundle.Source.Fingerprint
		if admission.ControlBundle.Target != nil {
			receipt.ControlBundleTargetFingerprint = admission.ControlBundle.Target.Fingerprint
		} else {
			receipt.ControlBundleTargetFingerprint = admission.ControlBundle.Source.Fingerprint
		}
	}
	if transition.ExecutionContext == "advance" {
		prior, resulting := admission.Invocation, target.Invocation
		if err := prior.Validate(true); err != nil {
			return TransitionReceipt{}, fmt.Errorf("receipt prior invocation: %w", err)
		}
		if err := resulting.Validate(true); err != nil {
			return TransitionReceipt{}, fmt.Errorf("receipt resulting invocation: %w", err)
		}
		receipt.ExecutionContext, receipt.PriorInvocation, receipt.ResultingInvocation = "advance", &prior, &resulting
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
	receipt.ID, err = contentID("trc-", identity)
	if err != nil {
		return TransitionReceipt{}, err
	}
	return receipt, nil
}

func (r TransitionReceipt) Validate() error {
	if r.SchemaVersion != ReceiptSchemaVersion || r.Kind != TransitionCommitted || r.ID == "" || r.FlowID == "" || r.Sequence == 0 || r.TransitionID == "" || r.TransitionVersion < 1 || r.PrescriptionID == "" || r.AdmissionID == "" || r.PriorStateRevision == 0 || r.PriorStateRevision == ^uint64(0) || r.ResultingStateRevision != r.PriorStateRevision+1 || !validSHA256(r.SourceFingerprint) || !validSHA256(r.TargetFingerprint) || !validSHA256(r.ObjectiveBindingFingerprint) || r.AuthorityFingerprint == "" || len(r.RequiredCapabilities) == 0 || r.IdempotencyKey == "" || len(r.CommittedEffects) == 0 || len(r.ChangedStateFacets) == 0 {
		return fmt.Errorf("receipt has incomplete committed-transition identity or evidence")
	}
	if (r.ControlBundleSourceFingerprint == "") != (r.ControlBundleTargetFingerprint == "") ||
		(r.ControlBundleSourceFingerprint != "" && (len(r.ControlBundleSourceFingerprint) != 64 || len(r.ControlBundleTargetFingerprint) != 64)) {
		return fmt.Errorf("receipt has incomplete repository control-bundle identity")
	}
	if r.InvocationFingerprint != "" && !validSHA256(r.InvocationFingerprint) {
		return fmt.Errorf("receipt has invalid invocation identity")
	}
	if err := validateEffectOutputs(r.EffectOutputs); err != nil {
		return err
	}
	if r.ExecutionContext != "" {
		if r.ExecutionContext != "advance" || r.PriorInvocation == nil || r.ResultingInvocation == nil {
			return fmt.Errorf("receipt has invalid execution context lineage")
		}
		if err := r.PriorInvocation.Validate(true); err != nil {
			return fmt.Errorf("receipt prior invocation: %w", err)
		}
		if err := r.ResultingInvocation.Validate(true); err != nil {
			return fmt.Errorf("receipt resulting invocation: %w", err)
		}
	} else if r.PriorInvocation != nil || r.ResultingInvocation != nil {
		return fmt.Errorf("receipt has invocation lineage without an execution context advance")
	}
	if r.WorkResultFingerprint != "" && !validSHA256(r.WorkResultFingerprint) {
		return fmt.Errorf("receipt has invalid foreground work identity")
	}
	canonicalFacets, err := model.NormalizeStateFacets("receipt.changed_state_facets", r.ChangedStateFacets)
	if err != nil || !slices.Equal(canonicalFacets, r.ChangedStateFacets) {
		return fmt.Errorf("receipt changed state facets are invalid or non-canonical: %v", err)
	}
	if err := r.Program.Validate(); err != nil {
		return err
	}
	if _, err := catalog.NormalizeCapabilities("receipt.required_capabilities", r.RequiredCapabilities); err != nil {
		return err
	}
	if _, err := catalog.NormalizeCapabilities("receipt.granted_capabilities", r.GrantedCapabilities); err != nil {
		return err
	}
	if missing := catalog.MissingCapability(r.RequiredCapabilities, catalog.NewCapabilitySet(r.GrantedCapabilities...)); missing != "" {
		return fmt.Errorf("receipt required capability %q was not admitted", missing)
	}
	if len(r.ExercisedCapabilities) > 0 {
		if _, err := catalog.NormalizeCapabilities("receipt.exercised_capabilities", r.ExercisedCapabilities); err != nil {
			return err
		}
		if missing := catalog.MissingCapability(r.ExercisedCapabilities, catalog.NewCapabilitySet(r.RequiredCapabilities...)); missing != "" {
			return fmt.Errorf("receipt exercised capability %q outside exact admission", missing)
		}
	}
	if err := r.Verification.Validate(); err != nil || r.Verification.EvidenceFingerprint != r.TargetFingerprint {
		return fmt.Errorf("receipt verification does not prove its target snapshot: %v", err)
	}
	effects := append([]EffectFact(nil), r.CommittedEffects...)
	sortEffectFacts(effects)
	for index, effect := range effects {
		if err := effect.Validate(); err != nil {
			return err
		}
		if effect != r.CommittedEffects[index] {
			return fmt.Errorf("receipt committed effects are not canonical")
		}
		if index > 0 && effect == effects[index-1] {
			return fmt.Errorf("receipt duplicates a committed effect fact")
		}
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
		if source != r.AuthoritySources[index] {
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
	if !r.ObjectiveScope.Valid() {
		return fmt.Errorf("receipt has invalid objective scope %q", r.ObjectiveScope)
	}
	if r.ObjectiveScope == catalog.ObjectiveScopeOptionalPreserve {
		switch r.ObjectiveStatus {
		case model.FactKnown:
			if r.ObjectiveID == "" || !r.TargetID.Valid() || (r.TrustedClass != "" && !r.TrustedClass.Valid()) || r.DeliveryID == "" {
				return fmt.Errorf("maintenance receipt has incomplete known objective binding")
			}
		case model.FactAbsent:
			if r.ObjectiveID != "" || r.TargetID != "" || r.DeliveryID != "" {
				return fmt.Errorf("maintenance receipt invents product intent from verified absence")
			}
		default:
			return fmt.Errorf("maintenance receipt requires known or verified-absent objective status")
		}
	} else if r.ObjectiveID == "" || !r.TargetID.Valid() || (r.TrustedClass != "" && !r.TrustedClass.Valid()) || r.DeliveryID == "" {
		return fmt.Errorf("receipt has incomplete objective identity")
	}
	if r.StartedAt.IsZero() || r.CommittedAt.Before(r.StartedAt) || r.DurationNanoseconds != r.CommittedAt.Sub(r.StartedAt).Nanoseconds() || r.Verification.VerifiedAt.After(r.CommittedAt) {
		return fmt.Errorf("receipt has invalid timing evidence")
	}
	if (r.PriorProgramFingerprint == "") != (r.ProgramDeltaFingerprint == "") {
		return fmt.Errorf("receipt has incomplete program delta identity")
	}
	if (r.RuntimeVersion == "") != (r.RuntimeFingerprint == "") || (r.RuntimeVersion == "") != (r.RuntimeSourceRevision == "") {
		return fmt.Errorf("receipt has incomplete runtime version, digest, or source identity")
	}
	if r.PriorProgramFingerprint != "" {
		delta, err := ProgramDeltaFingerprint(r.PriorProgramFingerprint, r.Program.Fingerprint)
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

func validateEffectOutputs(outputs Parameters) error {
	seen := map[string]bool{}
	canonical := outputs.Canonical()
	for index, output := range outputs {
		if output.Name == "" || output.Value == "" || seen[output.Name] {
			return fmt.Errorf("receipt effect outputs require unique non-empty names and values")
		}
		seen[output.Name] = true
		if canonical[index] != output {
			return fmt.Errorf("receipt effect outputs are not canonical")
		}
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func sortEffectFacts(facts []EffectFact) {
	sort.Slice(facts, func(i, j int) bool {
		left, right := facts[i], facts[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.EffectID != right.EffectID {
			return left.EffectID < right.EffectID
		}
		if left.Owner != right.Owner {
			return left.Owner < right.Owner
		}
		if left.Resource != right.Resource {
			return left.Resource < right.Resource
		}
		if left.Target != right.Target {
			return left.Target < right.Target
		}
		if left.Operation != right.Operation {
			return left.Operation < right.Operation
		}
		if left.PriorFingerprint != right.PriorFingerprint {
			return left.PriorFingerprint < right.PriorFingerprint
		}
		return left.ResultingFingerprint < right.ResultingFingerprint
	})
}

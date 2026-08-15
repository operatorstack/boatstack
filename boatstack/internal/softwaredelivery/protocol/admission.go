package protocol

import (
	"fmt"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

const AdmissionSchemaVersion = 7

type Admission struct {
	SchemaVersion                       int                     `json:"schema_version"`
	ID                                  string                  `json:"id"`
	PrescriptionID                      string                  `json:"prescription_id"`
	TransitionID                        catalog.TransitionID    `json:"transition_id"`
	TransitionVersion                   int                     `json:"transition_version"`
	ExpectedStateRevision               uint64                  `json:"expected_state_revision"`
	ExpectedProgramFingerprint          string                  `json:"expected_program_fingerprint"`
	PriorProgramFingerprint             string                  `json:"prior_program_fingerprint,omitempty"`
	ProgramDeltaFingerprint             string                  `json:"program_delta_fingerprint,omitempty"`
	ExpectedSnapshotFingerprint         string                  `json:"expected_snapshot_fingerprint"`
	ExpectedObjectiveBindingFingerprint string                  `json:"expected_objective_binding_fingerprint"`
	SourceRevision                      string                  `json:"source_revision,omitempty"`
	WorktreeFingerprint                 string                  `json:"worktree_fingerprint,omitempty"`
	SourcePhase                         model.ProtocolPhase     `json:"source_phase"`
	Invocation                          model.InvocationContext `json:"invocation"`
	Objective                           model.Objective         `json:"objective"`
	ObjectiveScope                      catalog.ObjectiveScope  `json:"objective_scope,omitempty"`
	ObjectiveStatus                     model.FactStatus        `json:"objective_status,omitempty"`
	Authority                           AuthorityBundle         `json:"authority"`
	AuthorityFingerprint                string                  `json:"authority_fingerprint"`
	RequiredCapabilities                []catalog.Capability    `json:"required_capabilities"`
	GrantedCapabilities                 []catalog.Capability    `json:"granted_capabilities"`
	EffectiveCapabilities               []catalog.Capability    `json:"effective_capabilities"`
	Parameters                          Parameters              `json:"parameters,omitempty"`
	Evidence                            []string                `json:"evidence"`
	IdempotencyKey                      string                  `json:"idempotency_key"`
	IssuedAt                            time.Time               `json:"issued_at"`
	ExpiresAt                           time.Time               `json:"expires_at"`
	Work                                *WorkEvidence           `json:"work,omitempty"`
}

func NewAdmission(snapshot model.Snapshot, objective model.Objective, transition catalog.Transition, prescription Prescription, authority AuthorityBundle, parameters Parameters, now time.Time, lifetime time.Duration) (Admission, error) {
	return NewAdmissionWithWork(snapshot, objective, transition, prescription, authority, parameters, nil, now, lifetime)
}

func NewAdmissionWithWork(snapshot model.Snapshot, objective model.Objective, transition catalog.Transition, prescription Prescription, authority AuthorityBundle, parameters Parameters, work *WorkEvidence, now time.Time, lifetime time.Duration) (Admission, error) {
	var err error
	objective, err = ObjectiveForTransition(snapshot, objective, transition)
	if err != nil {
		return Admission{}, err
	}
	if err := ValidateApplicability(snapshot, objective, transition, authority, parameters, now); err != nil {
		return Admission{}, err
	}
	capabilities, err := ProjectCapabilities(snapshot, transition, authority, now)
	if err != nil {
		return Admission{}, err
	}
	if lifetime <= 0 {
		return Admission{}, fmt.Errorf("admission lifetime must be positive")
	}
	if err := prescription.ValidateCurrent(snapshot, transition, capabilities); err != nil {
		return Admission{}, err
	}
	if err := prescription.ValidateWork(work); err != nil {
		return Admission{}, err
	}
	if transition.Work != nil {
		if work == nil {
			return Admission{}, fmt.Errorf("transition %q requires foreground work evidence", transition.ID)
		}
		if err := work.ValidateCurrent(snapshot, transition); err != nil {
			return Admission{}, err
		}
	} else if work != nil {
		return Admission{}, fmt.Errorf("transition %q does not accept foreground work evidence", transition.ID)
	}
	sourceRevision, worktreeFingerprint := gitBinding(snapshot)
	a := Admission{
		SchemaVersion: AdmissionSchemaVersion, PrescriptionID: prescription.ID, TransitionID: transition.ID, TransitionVersion: transition.Version,
		ExpectedStateRevision: prescription.ExpectedStateRevision, ExpectedProgramFingerprint: prescription.ExpectedProgramFingerprint,
		ExpectedSnapshotFingerprint: prescription.ExpectedSnapshotFingerprint, ExpectedObjectiveBindingFingerprint: prescription.ExpectedObjectiveBindingFingerprint,
		SourceRevision: sourceRevision, WorktreeFingerprint: worktreeFingerprint,
		SourcePhase: snapshot.Phase.Value, Invocation: snapshot.Invocation, Objective: objective, ObjectiveScope: transition.Policy.ObjectiveScope, Authority: authority.canonical(),
		AuthorityFingerprint: capabilities.AuthorityFingerprint, RequiredCapabilities: capabilities.Required,
		GrantedCapabilities: capabilities.Granted, EffectiveCapabilities: capabilities.Effective,
		Evidence: append([]string(nil), transition.RequiredEvidence...), Parameters: parameters.Canonical(), IssuedAt: now.UTC(), ExpiresAt: now.Add(lifetime).UTC(),
	}
	if work != nil {
		copy := *work
		copy.Outputs = append([]WorkOutputEvidence(nil), work.Outputs...)
		a.Work = &copy
	}
	if transition.Policy.ObjectiveScope == catalog.ObjectiveScopeOptionalPreserve {
		a.ObjectiveStatus = snapshot.Objective.Status
	}
	if snapshot.RecordedProgramFingerprint != "" && snapshot.RecordedProgramFingerprint != snapshot.ProgramFingerprint {
		a.PriorProgramFingerprint = snapshot.RecordedProgramFingerprint
		delta, err := ProgramDeltaFingerprint(snapshot.RecordedProgramFingerprint, prescription.ExpectedProgramFingerprint)
		if err != nil {
			return Admission{}, err
		}
		a.ProgramDeltaFingerprint = delta
	}
	key, err := contentID("idem-", struct {
		Transition catalog.TransitionID    `json:"transition"`
		Snapshot   string                  `json:"snapshot"`
		Invocation model.InvocationContext `json:"invocation"`
		Objective  model.Objective         `json:"objective"`
		Parameters Parameters              `json:"parameters"`
		Work       string                  `json:"work,omitempty"`
	}{transition.ID, snapshot.Fingerprint, snapshot.Invocation, objective, parameters.Canonical(), prescription.WorkResultFingerprint})
	if err != nil {
		return Admission{}, err
	}
	a.IdempotencyKey = key
	identity := a
	identity.ID = ""
	a.ID, err = contentID("adm-", identity)
	if err != nil {
		return Admission{}, err
	}
	return a, nil
}

// ObjectiveForTransition binds maintenance to verified durable objective state.
// Command-scoped product intent is deliberately irrelevant to maintenance.
func ObjectiveForTransition(snapshot model.Snapshot, requested model.Objective, transition catalog.Transition) (model.Objective, error) {
	if transition.Policy.ObjectiveScope != catalog.ObjectiveScopeOptionalPreserve {
		if err := requested.Validate(); err != nil {
			return model.Objective{}, err
		}
		return requested, nil
	}
	switch snapshot.Objective.Status {
	case model.FactKnown:
		if err := snapshot.Objective.Value.Validate(); err != nil {
			return model.Objective{}, fmt.Errorf("transition %q has invalid configured objective evidence: %w", transition.ID, err)
		}
		return snapshot.Objective.Value, nil
	case model.FactAbsent:
		return model.Objective{}, nil
	default:
		return model.Objective{}, fmt.Errorf("transition %q requires known or verified-absent product objective evidence", transition.ID)
	}
}

// ValidateApplicability is the deterministic transition law shared by
// resolution and admission. A transition that fails here must never be
// reported as prescribed for the same snapshot and context.
func ValidateApplicability(snapshot model.Snapshot, objective model.Objective, transition catalog.Transition, authority AuthorityBundle, parameters Parameters, now time.Time) error {
	if !transition.Controllable() {
		return fmt.Errorf("transition %q is uncontrollable and cannot be admitted", transition.ID)
	}
	if err := snapshot.Invocation.Validate(true); err != nil {
		return err
	}
	var err error
	objective, err = ObjectiveForTransition(snapshot, objective, transition)
	if err != nil {
		return err
	}
	if snapshot.Fingerprint == "" || len(snapshot.ProgramFingerprint) != 64 || !transition.SourceMatches(snapshot) || !transition.SupportsObjective(objective) {
		return fmt.Errorf("transition %q is not admissible from snapshot %q", transition.ID, snapshot.Fingerprint)
	}
	if snapshot.Objective.Status == model.FactKnown && snapshot.Objective.Value != objective && !transition.Policy.BindsRequestedObjective {
		return fmt.Errorf("transition %q cannot replace configured objective; objective.bind is required", transition.ID)
	}
	if err := authority.Validate(now); err != nil {
		return err
	}
	if err := validateAuthorityEvidence(snapshot, authority); err != nil {
		return err
	}
	if err := parameters.Validate(transition); err != nil {
		return err
	}
	if err := validateProviderAuthorityBinding(authority, transition, parameters); err != nil {
		return err
	}
	sourceRevision, worktreeFingerprint := gitBinding(snapshot)
	if transition.BindsSourceRevision {
		declared, _ := parameters.Get("source_revision")
		if sourceRevision == "" || worktreeFingerprint == "" || declared != sourceRevision {
			return fmt.Errorf("transition %q must bind the current Git revision and worktree fingerprint", transition.ID)
		}
	}
	if !authority.Set(now).Satisfies(transition.Authority, transition.AuthorityAll) {
		return fmt.Errorf("transition %q lacks required authority", transition.ID)
	}
	if err := validatePolicyAuthority(snapshot, transition, authority.Set(now)); err != nil {
		return err
	}
	if err := validateRecoveryPermission(snapshot, transition); err != nil {
		return err
	}
	return nil
}

func (a Admission) ValidateCurrent(snapshot model.Snapshot, objective model.Objective, transition catalog.Transition, now time.Time) error {
	if err := a.ValidateIdentity(); err != nil {
		return err
	}
	if !now.Before(a.ExpiresAt) {
		return fmt.Errorf("admission %q expired", a.ID)
	}
	if a.TransitionID != transition.ID || a.TransitionVersion != transition.Version {
		return fmt.Errorf("admission %q is bound to a different transition", a.ID)
	}
	if a.ObjectiveScope != transition.Policy.ObjectiveScope {
		return fmt.Errorf("admission %q is bound to a different objective scope", a.ID)
	}
	if a.ExpectedStateRevision != snapshot.StateRevision {
		return fmt.Errorf("admission %q is stale: state revision changed", a.ID)
	}
	if a.ExpectedSnapshotFingerprint != snapshot.Fingerprint {
		return fmt.Errorf("admission %q is stale: snapshot changed", a.ID)
	}
	objectiveBindingFingerprint, err := ObjectiveBindingFingerprint(snapshot)
	if err != nil {
		return err
	}
	if a.ExpectedObjectiveBindingFingerprint != objectiveBindingFingerprint {
		return fmt.Errorf("admission %q is stale: objective binding changed", a.ID)
	}
	if a.ExpectedProgramFingerprint != snapshot.ProgramFingerprint {
		return fmt.Errorf("admission %q is bound to a different control program", a.ID)
	}
	if transition.Work != nil {
		if a.Work == nil {
			return fmt.Errorf("admission %q is missing foreground work evidence", a.ID)
		}
		if err := a.Work.ValidateCurrent(snapshot, transition); err != nil {
			return fmt.Errorf("admission %q foreground work changed: %w", a.ID, err)
		}
	} else if a.Work != nil {
		return fmt.Errorf("admission %q carries unexpected foreground work evidence", a.ID)
	}
	expectedPrior := ""
	if snapshot.RecordedProgramFingerprint != "" && snapshot.RecordedProgramFingerprint != snapshot.ProgramFingerprint {
		expectedPrior = snapshot.RecordedProgramFingerprint
	}
	if a.PriorProgramFingerprint != expectedPrior {
		return fmt.Errorf("admission %q is bound to a different prior control program", a.ID)
	}
	if a.PriorProgramFingerprint != "" {
		delta, err := ProgramDeltaFingerprint(a.PriorProgramFingerprint, a.ExpectedProgramFingerprint)
		if err != nil || delta != a.ProgramDeltaFingerprint {
			return fmt.Errorf("admission %q has an invalid program delta binding", a.ID)
		}
	}
	if snapshot.Phase.Status != model.FactKnown || a.SourcePhase != snapshot.Phase.Value {
		return fmt.Errorf("admission %q is bound to a different source phase", a.ID)
	}
	if a.Invocation != snapshot.Invocation {
		return fmt.Errorf("admission %q is bound to a different invocation", a.ID)
	}
	if a.Objective != objective {
		return fmt.Errorf("admission %q is bound to a different objective", a.ID)
	}
	if a.ObjectiveScope == catalog.ObjectiveScopeOptionalPreserve && (a.ObjectiveStatus != snapshot.Objective.Status || (a.ObjectiveStatus == model.FactKnown && a.Objective != snapshot.Objective.Value)) {
		return fmt.Errorf("admission %q is stale: product objective changed", a.ID)
	}
	if err := a.Authority.Validate(now); err != nil {
		return err
	}
	capabilities, err := ProjectCapabilities(snapshot, transition, a.Authority, now)
	if err != nil {
		return err
	}
	if a.AuthorityFingerprint != capabilities.AuthorityFingerprint ||
		!sameCapabilities(a.RequiredCapabilities, capabilities.Required) ||
		!sameCapabilities(a.GrantedCapabilities, capabilities.Granted) ||
		!sameCapabilities(a.EffectiveCapabilities, capabilities.Effective) {
		return fmt.Errorf("admission %q authority or capability binding changed", a.ID)
	}
	if err := validateAuthorityEvidence(snapshot, a.Authority); err != nil {
		return err
	}
	if !a.Authority.Set(now).Satisfies(transition.Authority, transition.AuthorityAll) {
		return fmt.Errorf("admission %q no longer has required authority", a.ID)
	}
	if err := validatePolicyAuthority(snapshot, transition, a.Authority.Set(now)); err != nil {
		return err
	}
	if err := validateRecoveryPermission(snapshot, transition); err != nil {
		return err
	}
	sourceRevision, worktreeFingerprint := gitBinding(snapshot)
	if a.SourceRevision != sourceRevision || a.WorktreeFingerprint != worktreeFingerprint {
		return fmt.Errorf("admission %q Git binding changed", a.ID)
	}
	if transition.BindsSourceRevision {
		declared, _ := a.Parameters.Get("source_revision")
		if declared != sourceRevision || sourceRevision == "" || worktreeFingerprint == "" {
			return fmt.Errorf("admission %q is not bound to the current Git revision", a.ID)
		}
	}
	if err := a.Parameters.Validate(transition); err != nil {
		return err
	}
	if err := validateProviderAuthorityBinding(a.Authority, transition, a.Parameters); err != nil {
		return err
	}
	return nil
}

func gitBinding(snapshot model.Snapshot) (string, string) {
	for _, facts := range [][]model.Evidence{snapshot.Delivery.Evidence, snapshot.Verification.Evidence} {
		for _, evidence := range facts {
			if strings.HasPrefix(evidence.Source, "git:") && evidence.Revision != "" && evidence.Fingerprint != "" {
				return evidence.Revision, evidence.Fingerprint
			}
		}
	}
	return "", ""
}

func validateRecoveryPermission(snapshot model.Snapshot, transition catalog.Transition) error {
	if transition.Class != catalog.EventRecovery {
		return nil
	}
	if snapshot.RecoveryInfo.Status != model.FactKnown {
		return fmt.Errorf("transition %q requires exact recovery context", transition.ID)
	}
	for _, candidate := range snapshot.RecoveryInfo.Value.Permitted {
		if candidate == string(transition.ID) {
			return nil
		}
	}
	return fmt.Errorf("transition %q is not permitted for transaction %q", transition.ID, snapshot.RecoveryInfo.Value.TransactionID)
}

func validateProviderAuthorityBinding(authority AuthorityBundle, transition catalog.Transition, parameters Parameters) error {
	var expected string
	if transition.AuthorityFingerprintParameter != "" {
		expected, _ = parameters.Get(transition.AuthorityFingerprintParameter)
	}
	for _, receipt := range authority.Receipts {
		if receipt.Class != catalog.AuthorityProvider {
			continue
		}
		if expected == "" {
			return fmt.Errorf("transition %q does not accept provider authority", transition.ID)
		}
		if receipt.Fingerprint != expected {
			return fmt.Errorf("provider authority for transition %q does not bind the exact admitted request", transition.ID)
		}
	}
	return nil
}

func validatePolicyAuthority(snapshot model.Snapshot, transition catalog.Transition, authority catalog.AuthoritySet) error {
	if snapshot.ConfigurationPolicy.Status == model.FactKnown {
		enabled := false
		for _, host := range snapshot.ConfigurationPolicy.Value.Hosts {
			if host == snapshot.Invocation.Host {
				enabled = true
				break
			}
		}
		if !enabled {
			return fmt.Errorf("transition %q is unavailable to disabled host %q", transition.ID, snapshot.Invocation.Host)
		}
	}
	requiresPolicy := transition.Policy.RequiredWhen != "" || transition.Policy.AuthorityRule != "" || transition.Policy.AvailabilityRule != ""
	if !requiresPolicy {
		return nil
	}
	if snapshot.ConfigurationPolicy.Status != model.FactKnown {
		return fmt.Errorf("transition %q requires known configuration policy", transition.ID)
	}
	policy := snapshot.ConfigurationPolicy.Value
	if transition.Policy.AvailabilityRule == "visual-evidence-enabled" && policy.VisualEvidence == "off" {
		return fmt.Errorf("transition %q is disabled by repository policy", transition.ID)
	}
	if transition.Policy.AuthorityRule == "plan-approval" && policy.PlanApproval == "human" && !authority[catalog.AuthorityHuman] {
		return fmt.Errorf("transition %q requires human approval under repository policy", transition.ID)
	}
	if transition.Policy.AuthorityRule == "independent-high-risk-review" && policy.IndependentReviewForHighRisk && policy.HighRiskChange && !authority[catalog.AuthorityHuman] {
		return fmt.Errorf("transition %q requires independent human review for a high-risk change", transition.ID)
	}
	return nil
}

func validateAuthorityEvidence(snapshot model.Snapshot, authority AuthorityBundle) error {
	for _, receipt := range authority.Receipts {
		if receipt.Class != catalog.AuthorityRepository {
			continue
		}
		matched := false
		for _, evidence := range snapshot.Configuration.Evidence {
			if strings.HasPrefix(evidence.Source, "configuration:") && evidence.Fingerprint == receipt.Fingerprint {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("repository authority receipt %q is not bound to current configuration evidence", receipt.ID)
		}
	}
	return nil
}

func (a Admission) ValidateIdentity() error {
	if a.SchemaVersion != AdmissionSchemaVersion || a.ID == "" || a.PrescriptionID == "" || a.TransitionID == "" || a.TransitionVersion < 1 || a.ExpectedStateRevision == 0 || len(a.ExpectedProgramFingerprint) != 64 || len(a.ExpectedSnapshotFingerprint) != 64 || len(a.ExpectedObjectiveBindingFingerprint) != 64 || a.AuthorityFingerprint == "" || len(a.RequiredCapabilities) == 0 || len(a.EffectiveCapabilities) == 0 || !a.SourcePhase.Valid() || a.IdempotencyKey == "" || a.IssuedAt.IsZero() || a.ExpiresAt.Before(a.IssuedAt) {
		return fmt.Errorf("admission: invalid schema, identity, source, or lifetime")
	}
	fingerprint, err := a.Authority.Fingerprint()
	if err != nil || fingerprint != a.AuthorityFingerprint {
		return fmt.Errorf("admission has invalid authority identity")
	}
	for field, values := range map[string][]catalog.Capability{
		"required_capabilities":  a.RequiredCapabilities,
		"granted_capabilities":   a.GrantedCapabilities,
		"effective_capabilities": a.EffectiveCapabilities,
	} {
		if _, err := catalog.NormalizeCapabilities("admission."+field, values); err != nil {
			return err
		}
	}
	if !sameCapabilities(a.RequiredCapabilities, a.EffectiveCapabilities) {
		return fmt.Errorf("admission effective capabilities do not equal exact requirements")
	}
	if missing := catalog.MissingCapability(a.EffectiveCapabilities, catalog.NewCapabilitySet(a.GrantedCapabilities...)); missing != "" {
		return fmt.Errorf("admission effective capability %q was not granted", missing)
	}
	if (a.PriorProgramFingerprint == "") != (a.ProgramDeltaFingerprint == "") {
		return fmt.Errorf("admission has incomplete program delta identity")
	}
	if a.PriorProgramFingerprint != "" {
		delta, err := ProgramDeltaFingerprint(a.PriorProgramFingerprint, a.ExpectedProgramFingerprint)
		if err != nil || delta != a.ProgramDeltaFingerprint {
			return fmt.Errorf("admission has invalid program delta identity")
		}
	}
	if a.TransitionID == "installation.reconcile-update" {
		accepted, _ := a.Parameters.Get("accept_obligation_change")
		if a.PriorProgramFingerprint == "" || accepted != "true" {
			return fmt.Errorf("reconciled installation admission lacks exact program acceptance")
		}
	}
	if err := a.Invocation.Validate(true); err != nil {
		return err
	}
	if a.Work != nil {
		if err := a.Work.Validate(); err != nil {
			return err
		}
	}
	if !a.ObjectiveScope.Valid() {
		return fmt.Errorf("admission has invalid objective scope %q", a.ObjectiveScope)
	}
	if a.ObjectiveScope == catalog.ObjectiveScopeOptionalPreserve {
		switch a.ObjectiveStatus {
		case model.FactKnown:
			if err := a.Objective.Validate(); err != nil {
				return err
			}
		case model.FactAbsent:
			if a.Objective.Validate() == nil {
				return fmt.Errorf("maintenance admission cannot bind product intent to verified absence")
			}
		default:
			return fmt.Errorf("maintenance admission requires known or verified-absent product objective status")
		}
	} else if err := a.Objective.Validate(); err != nil {
		return err
	}
	identity := a
	want := identity.ID
	identity.ID = ""
	got, err := contentID("adm-", identity)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("admission %q failed content identity verification", a.ID)
	}
	return nil
}

func sameCapabilities(left, right []catalog.Capability) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

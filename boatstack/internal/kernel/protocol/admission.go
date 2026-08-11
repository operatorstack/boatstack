package protocol

import (
	"fmt"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

const AdmissionSchemaVersion = 2

type Admission struct {
	SchemaVersion       int                     `json:"schema_version"`
	ID                  string                  `json:"id"`
	TransitionID        catalog.TransitionID    `json:"transition_id"`
	TransitionVersion   int                     `json:"transition_version"`
	SnapshotFingerprint string                  `json:"snapshot_fingerprint"`
	SourceRevision      string                  `json:"source_revision,omitempty"`
	WorktreeFingerprint string                  `json:"worktree_fingerprint,omitempty"`
	SourcePhase         model.ProtocolPhase     `json:"source_phase"`
	Invocation          model.InvocationContext `json:"invocation"`
	Goal                model.Goal              `json:"goal"`
	Authority           AuthorityBundle         `json:"authority"`
	Parameters          Parameters              `json:"parameters,omitempty"`
	Evidence            []string                `json:"evidence"`
	IdempotencyKey      string                  `json:"idempotency_key"`
	IssuedAt            time.Time               `json:"issued_at"`
	ExpiresAt           time.Time               `json:"expires_at"`
}

func NewAdmission(snapshot model.Snapshot, goal model.Goal, transition catalog.Transition, authority AuthorityBundle, parameters Parameters, now time.Time, lifetime time.Duration) (Admission, error) {
	if !transition.Controllable() {
		return Admission{}, fmt.Errorf("transition %q is uncontrollable and cannot be admitted", transition.ID)
	}
	if err := snapshot.Invocation.Validate(true); err != nil {
		return Admission{}, err
	}
	if err := goal.Validate(); err != nil {
		return Admission{}, err
	}
	if snapshot.Fingerprint == "" || !transition.SourceMatches(snapshot) || !transition.SupportsGoal(goal) {
		return Admission{}, fmt.Errorf("transition %q is not admissible from snapshot %q", transition.ID, snapshot.Fingerprint)
	}
	if lifetime <= 0 {
		return Admission{}, fmt.Errorf("admission lifetime must be positive")
	}
	if err := authority.Validate(now); err != nil {
		return Admission{}, err
	}
	if err := validateAuthorityEvidence(snapshot, authority); err != nil {
		return Admission{}, err
	}
	if err := parameters.Validate(transition); err != nil {
		return Admission{}, err
	}
	if err := validateProviderAuthorityBinding(authority, transition, parameters); err != nil {
		return Admission{}, err
	}
	sourceRevision, worktreeFingerprint := gitBinding(snapshot)
	if transitionBindsSourceRevision(transition.ID) {
		declared, _ := parameters.Get("source_revision")
		if sourceRevision == "" || worktreeFingerprint == "" || declared != sourceRevision {
			return Admission{}, fmt.Errorf("transition %q must bind the current Git revision and worktree fingerprint", transition.ID)
		}
	}
	if !authority.Set(now).Satisfies(transition.Authority, transition.AuthorityAll) {
		return Admission{}, fmt.Errorf("transition %q lacks required authority", transition.ID)
	}
	if err := validatePolicyAuthority(snapshot, transition, authority.Set(now)); err != nil {
		return Admission{}, err
	}
	if err := validateRecoveryPermission(snapshot, transition); err != nil {
		return Admission{}, err
	}
	a := Admission{
		SchemaVersion: AdmissionSchemaVersion, TransitionID: transition.ID, TransitionVersion: transition.Version,
		SnapshotFingerprint: snapshot.Fingerprint, SourceRevision: sourceRevision, WorktreeFingerprint: worktreeFingerprint,
		SourcePhase: snapshot.Phase.Value, Invocation: snapshot.Invocation, Goal: goal, Authority: authority.canonical(),
		Evidence: append([]string(nil), transition.RequiredEvidence...), Parameters: parameters.Canonical(), IssuedAt: now.UTC(), ExpiresAt: now.Add(lifetime).UTC(),
	}
	key, err := contentID("idem-", struct {
		Transition catalog.TransitionID    `json:"transition"`
		Snapshot   string                  `json:"snapshot"`
		Invocation model.InvocationContext `json:"invocation"`
		Goal       model.Goal              `json:"goal"`
		Parameters Parameters              `json:"parameters"`
	}{transition.ID, snapshot.Fingerprint, snapshot.Invocation, goal, parameters.Canonical()})
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

func (a Admission) ValidateCurrent(snapshot model.Snapshot, goal model.Goal, transition catalog.Transition, now time.Time) error {
	if err := a.ValidateIdentity(); err != nil {
		return err
	}
	if !now.Before(a.ExpiresAt) {
		return fmt.Errorf("admission %q expired", a.ID)
	}
	if a.TransitionID != transition.ID || a.TransitionVersion != transition.Version {
		return fmt.Errorf("admission %q is bound to a different transition", a.ID)
	}
	if a.SnapshotFingerprint != snapshot.Fingerprint {
		return fmt.Errorf("admission %q is stale: snapshot changed", a.ID)
	}
	if snapshot.Phase.Status != model.FactKnown || a.SourcePhase != snapshot.Phase.Value {
		return fmt.Errorf("admission %q is bound to a different source phase", a.ID)
	}
	if a.Invocation != snapshot.Invocation {
		return fmt.Errorf("admission %q is bound to a different invocation", a.ID)
	}
	if a.Goal != goal {
		return fmt.Errorf("admission %q is bound to a different goal", a.ID)
	}
	if err := a.Authority.Validate(now); err != nil {
		return err
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
	if transitionBindsSourceRevision(transition.ID) {
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

func transitionBindsSourceRevision(id catalog.TransitionID) bool {
	value := string(id)
	return strings.HasPrefix(value, "gate.") || id == "evidence.visual.attach" || id == "delivery.slice.advance"
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
	switch transition.ID {
	case "publication.execute":
		expected, _ = parameters.Get("preview_fingerprint")
	case "publication.correct":
		expected, _ = parameters.Get("body_sha256")
	case "publication.reconcile":
		expected, _ = parameters.Get("publication_id")
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
	requiresPolicy := transition.ID == "plan.approve" || transition.ID == "plan.approve-amendment" ||
		transition.ID == "gate.review.record" || transition.ID == "evidence.visual.attach"
	if !requiresPolicy {
		return nil
	}
	if snapshot.ConfigurationPolicy.Status != model.FactKnown {
		return fmt.Errorf("transition %q requires known configuration policy", transition.ID)
	}
	policy := snapshot.ConfigurationPolicy.Value
	if transition.ID == "evidence.visual.attach" && policy.VisualEvidence == "off" {
		return fmt.Errorf("transition %q is disabled by repository policy", transition.ID)
	}
	if (transition.ID == "plan.approve" || transition.ID == "plan.approve-amendment") && policy.PlanApproval == "human" && !authority[catalog.AuthorityHuman] {
		return fmt.Errorf("transition %q requires human approval under repository policy", transition.ID)
	}
	if transition.ID == "gate.review.record" && policy.IndependentReviewForHighRisk && policy.HighRiskChange && !authority[catalog.AuthorityHuman] {
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
	if a.SchemaVersion != AdmissionSchemaVersion || a.ID == "" || a.TransitionID == "" || a.TransitionVersion < 1 || a.SnapshotFingerprint == "" || !a.SourcePhase.Valid() || a.IdempotencyKey == "" || a.IssuedAt.IsZero() || a.ExpiresAt.Before(a.IssuedAt) {
		return fmt.Errorf("admission: invalid schema, identity, source, or lifetime")
	}
	if err := a.Invocation.Validate(true); err != nil {
		return err
	}
	if err := a.Goal.Validate(); err != nil {
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

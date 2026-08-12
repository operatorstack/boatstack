package protocol

import (
	"fmt"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

type CapabilityProjection struct {
	AuthorityFingerprint string
	Required             []catalog.Capability
	Granted              []catalog.Capability
	Effective            []catalog.Capability
}

func ProjectCapabilities(snapshot model.Snapshot, transition catalog.Transition, authority AuthorityBundle, now time.Time) (CapabilityProjection, error) {
	if err := authority.Validate(now); err != nil {
		return CapabilityProjection{}, err
	}
	required := catalog.RequiredCapabilities(transition)
	if requiresHumanApprovalCapability(snapshot, transition) {
		required = catalog.UnionCapabilities(required, []catalog.Capability{catalog.CapabilityHumanApprove})
	}
	declared := catalog.NewCapabilitySet(transition.DeclaredCapabilities...)
	if missing := catalog.MissingCapability(required, declared); missing != "" {
		return CapabilityProjection{}, fmt.Errorf("CAPABILITY_NOT_DECLARED %q for transition %q", missing, transition.ID)
	}
	grantedSet := catalog.AuthorityCapabilities(authority.Set(now))
	if missing := catalog.MissingCapability(required, grantedSet); missing != "" {
		return CapabilityProjection{}, fmt.Errorf("CAPABILITY_DENIED %q for transition %q", missing, transition.ID)
	}
	fingerprint, err := authority.Fingerprint()
	if err != nil {
		return CapabilityProjection{}, err
	}
	return CapabilityProjection{
		AuthorityFingerprint: fingerprint,
		Required:             append([]catalog.Capability(nil), required...),
		Granted:              grantedSet.Sorted(),
		Effective:            append([]catalog.Capability(nil), required...),
	}, nil
}

func requiresHumanApprovalCapability(snapshot model.Snapshot, transition catalog.Transition) bool {
	if transition.Policy.AuthorityRule == "plan-approval" && snapshot.ConfigurationPolicy.Status == model.FactKnown {
		return snapshot.ConfigurationPolicy.Value.PlanApproval == "human"
	}
	if transition.Policy.AuthorityRule == "independent-high-risk-review" && snapshot.ConfigurationPolicy.Status == model.FactKnown {
		policy := snapshot.ConfigurationPolicy.Value
		return policy.IndependentReviewForHighRisk && policy.HighRiskChange
	}
	return false
}

func ValidateEffectCapabilities(admission Admission, transition catalog.Transition) error {
	required := catalog.RequiredCapabilities(transition)
	effective := catalog.NewCapabilitySet(admission.EffectiveCapabilities...)
	if missing := catalog.MissingCapability(required, effective); missing != "" {
		return fmt.Errorf("EFFECT_CAPABILITY_DENIED %q for transition %q", missing, transition.ID)
	}
	return nil
}

func RequireCapability(values []catalog.Capability, required catalog.Capability, component string) error {
	if !catalog.NewCapabilitySet(values...)[required] {
		return fmt.Errorf("EFFECT_CAPABILITY_DENIED %q for %s", required, component)
	}
	return nil
}

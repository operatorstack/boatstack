package model

import (
	"fmt"
	"regexp"
)

type TargetID string

var safeObjectiveIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

const (
	ObjectiveApprovedPlan TargetID = "approved-plan"
	ObjectiveVerified     TargetID = "verified-implementation"
	ObjectiveOpenPR       TargetID = "open-or-updated-pr"
	ObjectiveMerged       TargetID = "merged-delivery"
	ObjectiveAbandoned    TargetID = "safely-abandoned"
)

func (k TargetID) Valid() bool {
	return safeObjectiveIdentity.MatchString(string(k))
}

type Objective struct {
	ID                  string   `json:"id"`
	TargetID            TargetID `json:"target_id"`
	TrustedClass        TargetID `json:"trusted_class,omitempty"`
	DeliveryID          string   `json:"delivery_id"`
	EvidenceFingerprint string   `json:"evidence_fingerprint,omitempty"`
	FrontierIsStop      bool     `json:"frontier_is_stop,omitempty"`
}

func (g Objective) Validate() error {
	if !safeObjectiveIdentity.MatchString(g.ID) || !safeObjectiveIdentity.MatchString(g.DeliveryID) {
		return fmt.Errorf("objective: id and delivery identity must be safe semantic segments")
	}
	if !g.TargetID.Valid() || (g.TrustedClass != "" && !g.TrustedClass.Valid()) {
		return fmt.Errorf("objective: invalid target %q or trusted class %q", g.TargetID, g.TrustedClass)
	}
	return nil
}

func (g Objective) TrustedObjectiveClass() TargetID {
	if g.TrustedClass != "" {
		return g.TrustedClass
	}
	return g.TargetID
}

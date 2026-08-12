package model

import (
	"fmt"
	"regexp"
)

type ObjectiveKind string

const (
	ObjectiveApprovedPlan ObjectiveKind = "approved-plan"
	ObjectiveVerified     ObjectiveKind = "verified-implementation"
	ObjectiveOpenPR       ObjectiveKind = "open-or-updated-pr"
	ObjectiveMerged       ObjectiveKind = "merged-delivery"
	ObjectiveAbandoned    ObjectiveKind = "safely-abandoned"
)

func (k ObjectiveKind) Valid() bool {
	switch k {
	case ObjectiveApprovedPlan, ObjectiveVerified, ObjectiveOpenPR, ObjectiveMerged, ObjectiveAbandoned:
		return true
	default:
		return false
	}
}

type Objective struct {
	ID                  string        `json:"id"`
	Kind                ObjectiveKind `json:"kind"`
	DeliveryID          string        `json:"delivery_id"`
	EvidenceFingerprint string        `json:"evidence_fingerprint,omitempty"`
	FrontierIsStop      bool          `json:"frontier_is_stop,omitempty"`
}

var safeObjectiveIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (g Objective) Validate() error {
	if !safeObjectiveIdentity.MatchString(g.ID) || !safeObjectiveIdentity.MatchString(g.DeliveryID) {
		return fmt.Errorf("objective: id and delivery identity must be safe semantic segments")
	}
	if !g.Kind.Valid() {
		return fmt.Errorf("objective: invalid kind %q", g.Kind)
	}
	return nil
}

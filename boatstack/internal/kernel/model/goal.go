package model

import (
	"fmt"
	"regexp"
)

type GoalKind string

const (
	GoalApprovedPlan GoalKind = "approved-plan"
	GoalVerified     GoalKind = "verified-implementation"
	GoalOpenPR       GoalKind = "open-or-updated-pr"
	GoalMerged       GoalKind = "merged-delivery"
	GoalAbandoned    GoalKind = "safely-abandoned"
)

func (k GoalKind) Valid() bool {
	switch k {
	case GoalApprovedPlan, GoalVerified, GoalOpenPR, GoalMerged, GoalAbandoned:
		return true
	default:
		return false
	}
}

type Goal struct {
	ID                  string   `json:"id"`
	Kind                GoalKind `json:"kind"`
	DeliveryID          string   `json:"delivery_id"`
	EvidenceFingerprint string   `json:"evidence_fingerprint,omitempty"`
	FrontierIsStop      bool     `json:"frontier_is_stop,omitempty"`
}

var safeGoalIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (g Goal) Validate() error {
	if !safeGoalIdentity.MatchString(g.ID) || !safeGoalIdentity.MatchString(g.DeliveryID) {
		return fmt.Errorf("goal: id and delivery identity must be safe semantic segments")
	}
	if !g.Kind.Valid() {
		return fmt.Errorf("goal: invalid kind %q", g.Kind)
	}
	return nil
}

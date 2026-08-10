package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// LifecycleSnapshot is the canonical control projection for one managed
// delivery. It deliberately includes every durable dimension that can remove
// or grant an actuator. Callers may render or enforce this answer; they must not
// reconstruct authority from DeliverySlice.Status alone.
// control-law: lifecycle-authority-includes-every-controlling-dimension
type LifecycleSnapshot struct {
	SchemaVersion       int                     `json:"schema_version"`
	Feature             string                  `json:"feature"`
	State               deliverycontrol.StateID `json:"state"`
	Mode                string                  `json:"mode"`
	ResumeStage         string                  `json:"resume_stage,omitempty"`
	ActiveSlice         string                  `json:"active_slice,omitempty"`
	SliceStatus         string                  `json:"slice_status,omitempty"`
	ActiveIndex         int                     `json:"active_index"`
	TotalSlices         int                     `json:"total_slices"`
	PlanLockSHA256      string                  `json:"plan_lock_sha256"`
	LockedPlanSHA256    string                  `json:"locked_plan_sha256,omitempty"`
	PlanSHA256          string                  `json:"plan_sha256,omitempty"`
	ObservationID       string                  `json:"observation_id,omitempty"`
	Repository          string                  `json:"repository"`
	RepositoryID        string                  `json:"repository_id,omitempty"`
	WorktreeID          string                  `json:"worktree_id,omitempty"`
	Branch              string                  `json:"branch,omitempty"`
	ConfigurationSHA256 string                  `json:"configuration_sha256,omitempty"`
	HumanApproval       bool                    `json:"human_approval"`
	ApprovalCurrent     bool                    `json:"approval_current"`
	Fingerprint         string                  `json:"fingerprint"`
}

func lockPlanSHA256(path string) (string, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var lock map[string]any
	if err := DecodeJSON("inspect plan lock", path, value, &lock); err != nil {
		return "", err
	}
	sha := strings.TrimSpace(stringValue(lock["plan_sha256"]))
	if sha == "" {
		return "", fmt.Errorf("plan lock does not bind plan_sha256")
	}
	return sha, nil
}

func lifecycleStateForSlice(status string) (deliverycontrol.StateID, error) {
	switch status {
	case StatusPending:
		return deliverycontrol.StatePending, nil
	case StatusBuild:
		return deliverycontrol.StateBuild, nil
	case StatusTestPassed:
		return deliverycontrol.StateTestPassed, nil
	case StatusReviewPassed:
		return deliverycontrol.StateReviewPassed, nil
	case StatusPublished:
		return deliverycontrol.StatePublished, nil
	default:
		return deliverycontrol.StateUnresolved, fmt.Errorf("unsupported delivery slice status %q", status)
	}
}

func lifecycleFingerprint(snapshot LifecycleSnapshot) (string, error) {
	snapshot.Fingerprint = ""
	value, err := MarshalJSON(snapshot)
	if err != nil {
		return "", err
	}
	return SHA256Bytes(value), nil
}

// ResolveLifecycleSnapshot reads one verified delivery and returns the exact
// composite state consumed by next-status, flow, bootstrap, and safety
// admission. It is read-only.
func ResolveLifecycleSnapshot(repoPath, feature string) (LifecycleSnapshot, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return LifecycleSnapshot{}, err
	}
	workspace, err := ResolveWorkspaceContext(repo)
	if err != nil {
		return LifecycleSnapshot{}, err
	}
	state, err := CurrentDeliveryState(repo, feature)
	if err != nil {
		return LifecycleSnapshot{}, err
	}
	slice, err := activeDeliverySlice(state)
	if err != nil {
		return LifecycleSnapshot{}, err
	}
	branch, _ := gitCommand(repo, "branch", "--show-current")
	config, _, configErr := LoadConfig(workspace.ProjectConfigPath())
	if configErr != nil {
		return LifecycleSnapshot{}, fmt.Errorf("managed lifecycle requires a valid Boatstack configuration: %w", configErr)
	}
	configSHA, _ := SHA256File(workspace.ProjectConfigPath())
	planPath := filepath.Join(workspace.FeatureDir(feature), "plan.md")
	lockPath := filepath.Join(workspace.FeatureDir(feature), "plan.lock.json")
	planSHA, _ := SHA256File(planPath)
	lockedPlanSHA, err := lockPlanSHA256(lockPath)
	if err != nil {
		return LifecycleSnapshot{}, err
	}

	snapshot := LifecycleSnapshot{
		SchemaVersion: 1, Feature: feature, Mode: strings.TrimSpace(state.Mode),
		ResumeStage: strings.TrimSpace(state.ResumeStage), ActiveSlice: slice.ID,
		SliceStatus: slice.Status, ActiveIndex: state.ActiveIndex, TotalSlices: len(state.Slices),
		PlanLockSHA256: state.PlanLockHash, LockedPlanSHA256: lockedPlanSHA, PlanSHA256: planSHA,
		ObservationID: strings.TrimSpace(state.ActiveObservationID), Repository: repo,
		RepositoryID: workspace.RepoID, WorktreeID: workspace.WorktreeID,
		Branch: strings.TrimSpace(branch), ConfigurationSHA256: configSHA,
		HumanApproval: config.Workflow.HumanPlanApproval,
	}

	switch snapshot.Mode {
	case "AMENDMENT_REQUIRED", "PLAN_INVALID":
		if snapshot.Mode == "PLAN_INVALID" {
			snapshot.State = deliverycontrol.StatePlanInvalid
		} else {
			snapshot.State = deliverycontrol.StateAmendmentRequired
		}
		if snapshot.PlanSHA256 != "" && snapshot.PlanSHA256 != snapshot.LockedPlanSHA256 {
			snapshot.State = deliverycontrol.StateAmendmentDrafted
			if check, checkErr := CheckPlan(planPath); checkErr == nil {
				if !snapshot.HumanApproval {
					snapshot.ApprovalCurrent = true
					snapshot.State = deliverycontrol.StateAmendmentApproved
				} else if receipt, approvalErr := CheckApprovalReceipt(filepath.Join(filepath.Dir(planPath), "approval.md"), check); approvalErr == nil {
					preApprovalSHA, fingerprintErr := lifecycleFingerprint(snapshot)
					if fingerprintErr == nil && receipt.LifecycleSHA256 == preApprovalSHA &&
						receipt.PlanLockSHA256 == snapshot.PlanLockSHA256 && receipt.ObservationID == snapshot.ObservationID {
						snapshot.ApprovalCurrent = true
						snapshot.State = deliverycontrol.StateAmendmentApproved
					}
				}
			}
		}
	default:
		snapshot.State, err = lifecycleStateForSlice(slice.Status)
		if err != nil {
			return LifecycleSnapshot{}, err
		}
	}
	snapshot.Fingerprint, err = lifecycleFingerprint(snapshot)
	if err != nil {
		return LifecycleSnapshot{}, err
	}
	return snapshot, nil
}

func amendmentLifecycleState(state deliverycontrol.StateID) bool {
	return state == deliverycontrol.StateAmendmentRequired ||
		state == deliverycontrol.StateAmendmentDrafted ||
		state == deliverycontrol.StateAmendmentApproved ||
		state == deliverycontrol.StatePlanInvalid
}

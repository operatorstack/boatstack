package boatstack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ReadinessReceipt struct {
	Fingerprint           string
	PlanFingerprint       string
	BaseBranch            string
	HeadBranch            string
	BaseCommit            string
	HeadCommit            string
	Upstream              string
	Relation              string
	JourneyManifestSHA256 string
}

func readinessFingerprint(receipt ReadinessReceipt) (string, error) {
	canonical, err := MarshalJSON(map[string]any{
		"schema_version": 1, "plan_fingerprint": receipt.PlanFingerprint,
		"base_branch": receipt.BaseBranch, "head_branch": receipt.HeadBranch,
		"base_commit": receipt.BaseCommit, "head_commit": receipt.HeadCommit,
		"upstream": receipt.Upstream, "relation": receipt.Relation,
		"journey_manifest_sha256": receipt.JourneyManifestSHA256,
	})
	if err != nil {
		return "", err
	}
	return SHA256Bytes(canonical), nil
}

func CheckPlanReadiness(planPath string) (ReadinessReceipt, error) {
	check, err := CheckPlan(planPath)
	if err != nil {
		return ReadinessReceipt{}, err
	}
	repo, err := ResolveRepository(filepath.Dir(planPath))
	if err != nil {
		return ReadinessReceipt{}, err
	}
	config, _, err := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
	if err != nil {
		return ReadinessReceipt{}, fmt.Errorf("readiness requires valid Boatstack configuration: %w", err)
	}
	if err := guardManagedActivationWorktree(repo, config, stringValue(check.Plan["feature_id"])); err != nil {
		return ReadinessReceipt{}, err
	}
	preflight := CheckRunPreflight(repo, "")
	if preflight.VerificationStatus != "VERIFIED" {
		return ReadinessReceipt{}, fmt.Errorf("readiness blocked (%s): %s; recover the workspace and retry approval", preflight.Relation, preflight.Reason)
	}
	manifest, err := CompileJourneyManifest(check.Plan)
	if err != nil {
		return ReadinessReceipt{}, fmt.Errorf("journey capability is not ready: %w", err)
	}
	if err := checkJourneyCapabilities(repo, check.Plan); err != nil {
		return ReadinessReceipt{}, err
	}
	var manifestValue map[string]any
	if err := DecodeJSON("compiled journey oracle manifest", "journey-oracles.json", manifest, &manifestValue); err != nil {
		return ReadinessReceipt{}, err
	}
	baseCommit, err := runGitCommand(repo, "rev-parse", "refs/remotes/origin/"+preflight.BaseBranch+"^{commit}")
	if err != nil {
		return ReadinessReceipt{}, err
	}
	headCommit, err := runGitCommand(repo, "rev-parse", "HEAD^{commit}")
	if err != nil {
		return ReadinessReceipt{}, err
	}
	receipt := ReadinessReceipt{
		PlanFingerprint: check.Fingerprint, BaseBranch: preflight.BaseBranch,
		HeadBranch: preflight.HeadBranch, BaseCommit: strings.TrimSpace(baseCommit),
		HeadCommit: strings.TrimSpace(headCommit), Upstream: preflight.Upstream,
		Relation: preflight.Relation, JourneyManifestSHA256: stringValue(manifestValue["manifest_sha256"]),
	}
	fingerprint, err := readinessFingerprint(receipt)
	if err != nil {
		return ReadinessReceipt{}, err
	}
	receipt.Fingerprint = fingerprint
	return receipt, nil
}

func checkJourneyCapabilities(repo string, plan map[string]any) error {
	decision, _ := plan["journey_evidence"].(map[string]any)
	if strings.ToLower(stringValue(decision["relevance"])) != "relevant" {
		return nil
	}
	oracles, _ := objectSlice(decision["oracles"])
	for _, oracle := range oracles {
		fields := strings.Fields(stringValue(oracle["run"]))
		if len(fields) == 0 {
			return fmt.Errorf("journey capability %s has no runnable command", stringValue(oracle["id"]))
		}
		command := fields[0]
		if strings.Contains(command, "/") {
			if !filepath.IsAbs(command) {
				command = filepath.Join(repo, command)
			}
			info, err := os.Stat(command)
			if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
				return fmt.Errorf("journey capability %s is missing executable %s", stringValue(oracle["id"]), fields[0])
			}
		} else if _, err := exec.LookPath(command); err != nil {
			return fmt.Errorf("journey capability %s is missing command %s", stringValue(oracle["id"]), command)
		}
	}
	return nil
}

package boatstack

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const (
	autonomyMarkerStart = "<!-- boatstack-autonomy:v1 -->"
	autonomyMarkerEnd   = "<!-- /boatstack-autonomy -->"
)

type RunTarget string

const (
	RunTargetPlan     RunTarget = "plan"
	RunTargetVerified RunTarget = "verified"
	RunTargetPR       RunTarget = "pr"
)

type AutonomyImpact struct {
	PublicContract     bool `json:"public_contract"`
	AcceptanceCriteria bool `json:"acceptance_criteria"`
	Security           bool `json:"security"`
	Billing            bool `json:"billing"`
	Migration          bool `json:"migration"`
	HighRiskPath       bool `json:"high_risk_path"`
	Destructive        bool `json:"destructive"`
	ExternalTarget     bool `json:"external_target"`
}

type AutonomyOption struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	Recommended bool   `json:"recommended"`
}

type AutonomyVerification struct {
	Run    string `json:"run"`
	Oracle string `json:"oracle"`
}

type AutonomyDecision struct {
	ID             string               `json:"id"`
	Question       string               `json:"question"`
	Resolution     string               `json:"resolution"`
	SelectedOption string               `json:"selected_option"`
	Options        []AutonomyOption     `json:"options"`
	Material       bool                 `json:"material"`
	WithinSpec     bool                 `json:"within_spec"`
	Reversible     bool                 `json:"reversible"`
	Impact         AutonomyImpact       `json:"impact"`
	EvidenceIDs    []string             `json:"evidence_ids"`
	Verification   AutonomyVerification `json:"verification"`
	Rationale      string               `json:"rationale"`
}

type AutonomyReceipt struct {
	SchemaVersion   int                `json:"schema_version"`
	Feature         string             `json:"feature"`
	Target          RunTarget          `json:"target"`
	Repository      string             `json:"repository"`
	Branch          string             `json:"branch"`
	IssuingBranch   string             `json:"issuing_branch,omitempty"`
	PRAction        string             `json:"pr_action,omitempty"`
	PlanPath        string             `json:"plan_path"`
	PlanFingerprint string             `json:"plan_fingerprint"`
	Decisions       []AutonomyDecision `json:"decisions"`
	Evidence        []EvidenceRecord   `json:"evidence"`
	Fingerprint     string             `json:"fingerprint"`
}

type AutonomyRecordOptions struct {
	Repo       string
	PlanPath   string
	Target     RunTarget
	OutputPath string
}

func autonomyPathIf(enabled bool, path string) string {
	if enabled {
		return path
	}
	return ""
}

func selectedAutonomyEvidence(decisions []AutonomyDecision, ledger map[string]EvidenceRecord) ([]EvidenceRecord, error) {
	selected := map[string]EvidenceRecord{}
	for _, decision := range decisions {
		for _, id := range decision.EvidenceIDs {
			record, ok := ledger[id]
			if !ok || record.ID == "" || record.Operation == "" || record.RepositoryRevision == "" || record.CreatedBy == "" {
				return nil, fmt.Errorf("autonomy evidence %s is missing required repository provenance", id)
			}
			selected[id] = record
		}
	}
	ids := make([]string, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]EvidenceRecord, 0, len(ids))
	for _, id := range ids {
		result = append(result, selected[id])
	}
	return result, nil
}

func ParseRunTarget(value string) (RunTarget, error) {
	target := RunTarget(strings.ToLower(strings.TrimSpace(value)))
	switch target {
	case RunTargetPlan, RunTargetVerified, RunTargetPR:
		return target, nil
	default:
		return "", fmt.Errorf("run target must be plan, verified, or pr")
	}
}

func autonomyDecisions(plan map[string]any) ([]AutonomyDecision, error) {
	raw, present := plan["autonomy_decisions"]
	if !present {
		return []AutonomyDecision{}, nil
	}
	data, err := MarshalJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("autonomy_decisions must be a list: %w", err)
	}
	var decisions []AutonomyDecision
	if err := DecodeJSON("autonomy decisions", "plan.md", data, &decisions); err != nil {
		return nil, fmt.Errorf("autonomy_decisions must be a typed list: %w", err)
	}
	return decisions, nil
}

func validateAutonomyDecisions(plan map[string]any, evidence map[string]EvidenceRecord) ([]AutonomyDecision, error) {
	decisions, err := autonomyDecisions(plan)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, decision := range decisions {
		if decision.ID == "" || seen[decision.ID] {
			return nil, fmt.Errorf("autonomy decision ids must be present and unique")
		}
		seen[decision.ID] = true
		if decision.Resolution != "RESOLVED_BY_POLICY" {
			return nil, fmt.Errorf("autonomy decision %s resolution must be RESOLVED_BY_POLICY", decision.ID)
		}
		if decision.Material || !decision.WithinSpec || !decision.Reversible {
			return nil, fmt.Errorf("autonomy decision %s must be non-material, within-spec, and reversible", decision.ID)
		}
		impact := decision.Impact
		if impact.PublicContract || impact.AcceptanceCriteria || impact.Security || impact.Billing || impact.Migration || impact.HighRiskPath || impact.Destructive || impact.ExternalTarget {
			return nil, fmt.Errorf("autonomy decision %s has a protected impact", decision.ID)
		}
		recommended := ""
		for _, option := range decision.Options {
			if option.ID == "" || option.Text == "" {
				return nil, fmt.Errorf("autonomy decision %s has a malformed option", decision.ID)
			}
			if option.Recommended {
				if recommended != "" {
					return nil, fmt.Errorf("autonomy decision %s must have exactly one recommendation", decision.ID)
				}
				recommended = option.ID
			}
		}
		if recommended == "" || decision.SelectedOption != recommended {
			return nil, fmt.Errorf("autonomy decision %s must select its single recommended option", decision.ID)
		}
		if len(decision.EvidenceIDs) == 0 || decision.Verification.Run == "" || decision.Verification.Oracle == "" || decision.Rationale == "" {
			return nil, fmt.Errorf("autonomy decision %s requires evidence, rationale, and a runnable verification oracle", decision.ID)
		}
		for _, id := range decision.EvidenceIDs {
			if evidence != nil {
				if _, ok := evidence[id]; !ok {
					return nil, fmt.Errorf("autonomy decision %s references unknown evidence %s", decision.ID, id)
				}
			}
		}
	}
	return decisions, nil
}

func autonomyFingerprint(receipt AutonomyReceipt) (string, error) {
	receipt.Fingerprint = ""
	data, err := MarshalJSON(receipt)
	if err != nil {
		return "", err
	}
	return SHA256Bytes(data), nil
}

func RecordAutonomy(options AutonomyRecordOptions) (AutonomyReceipt, error) {
	target, err := ParseRunTarget(string(options.Target))
	if err != nil {
		return AutonomyReceipt{}, err
	}
	check, err := CheckPlan(options.PlanPath)
	if err != nil {
		return AutonomyReceipt{}, err
	}
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return AutonomyReceipt{}, err
	}
	evidence, err := LoadEvidenceLedger(filepath.Join(filepath.Dir(options.PlanPath), "evidence.json"))
	if err != nil {
		return AutonomyReceipt{}, err
	}
	decisions, err := validateAutonomyDecisions(check.Plan, evidence)
	if err != nil {
		return AutonomyReceipt{}, err
	}
	selectedEvidence, err := selectedAutonomyEvidence(decisions, evidence)
	if err != nil {
		return AutonomyReceipt{}, err
	}
	branch, err := gitCommand(repo, "rev-parse", "--abbrev-ref", "HEAD")
	branch = strings.TrimSpace(branch)
	if err != nil || branch == "" || branch == "HEAD" {
		return AutonomyReceipt{}, fmt.Errorf("autonomy requires an identifiable current branch")
	}
	issuingBranch := branch
	feature := stringValue(check.Plan["feature_id"])
	if workspaceEnabled(repo) && needsFreshCut(repo, feature) {
		branch = branchForFeature(feature)
	}
	repository, err := gitCommand(repo, "remote", "get-url", "origin")
	if err != nil {
		return AutonomyReceipt{}, fmt.Errorf("autonomy requires an origin repository identity")
	}
	action := ""
	if target == RunTargetPR {
		action, _, err = RecommendedPRAction(repo)
		if err != nil {
			return AutonomyReceipt{}, fmt.Errorf("PR target requires a stable open or update action: %w", err)
		}
		if action != "open" && action != "update" {
			return AutonomyReceipt{}, fmt.Errorf("PR target requires a stable open or update action")
		}
	}
	receipt := AutonomyReceipt{SchemaVersion: 1, Feature: feature, Target: target, Repository: strings.TrimSpace(repository), Branch: branch, IssuingBranch: issuingBranch, PRAction: action, PlanPath: filepath.ToSlash(options.PlanPath), PlanFingerprint: check.Fingerprint, Decisions: decisions, Evidence: selectedEvidence}
	receipt.Fingerprint, err = autonomyFingerprint(receipt)
	if err != nil {
		return AutonomyReceipt{}, err
	}
	if options.OutputPath == "" {
		options.OutputPath = filepath.Join(filepath.Dir(options.PlanPath), "autonomy.md")
	}
	expectedOutput := filepath.Join(filepath.Dir(options.PlanPath), "autonomy.md")
	actualOutput, _ := filepath.Abs(options.OutputPath)
	expectedOutput, _ = filepath.Abs(expectedOutput)
	if filepath.Clean(actualOutput) != filepath.Clean(expectedOutput) {
		return AutonomyReceipt{}, fmt.Errorf("autonomy receipt must be written beside plan.md as autonomy.md")
	}
	body, err := MarshalJSON(receipt)
	if err != nil {
		return AutonomyReceipt{}, err
	}
	content := []byte("# Boatstack autonomous run\n\nThis receipt records scoped policy authority. It is not human plan approval.\n\n" + autonomyMarkerStart + "\n```json\n" + string(body) + "```\n" + autonomyMarkerEnd + "\n")
	if err := writeFile(options.OutputPath, content, 0o644); err != nil {
		return AutonomyReceipt{}, err
	}
	return receipt, nil
}

func CheckAutonomyReceipt(path string, check PlanCheck, repo string, minimumTarget RunTarget, action string) (AutonomyReceipt, error) {
	return checkAutonomyReceipt(path, check, repo, minimumTarget, action, false)
}

// CheckAutonomyReceiptForPlanning permits the receipt's issuing branch only
// while resolving the pre-cut planning transition. Activation and publication
// always require the exact feature branch bound into Branch.
func CheckAutonomyReceiptForPlanning(path string, check PlanCheck, repo string, minimumTarget RunTarget) (AutonomyReceipt, error) {
	return checkAutonomyReceipt(path, check, repo, minimumTarget, "", true)
}

func checkAutonomyReceipt(path string, check PlanCheck, repo string, minimumTarget RunTarget, action string, allowIssuingBranch bool) (AutonomyReceipt, error) {
	value, err := loadJSONObject(path, "autonomy receipt", autonomyMarkerStart, autonomyMarkerEnd, true)
	if err != nil {
		return AutonomyReceipt{}, err
	}
	data, err := MarshalJSON(value)
	if err != nil {
		return AutonomyReceipt{}, err
	}
	var receipt AutonomyReceipt
	if err := DecodeJSON("autonomy receipt", path, data, &receipt); err != nil {
		return AutonomyReceipt{}, err
	}
	want, err := autonomyFingerprint(receipt)
	if err != nil || receipt.SchemaVersion != 1 || receipt.Fingerprint != want {
		return AutonomyReceipt{}, fmt.Errorf("autonomy receipt fingerprint is invalid")
	}
	if receipt.Feature != stringValue(check.Plan["feature_id"]) || receipt.PlanFingerprint != check.Fingerprint {
		return AutonomyReceipt{}, fmt.Errorf("autonomy receipt does not match the current plan")
	}
	currentRepo, err := gitCommand(repo, "remote", "get-url", "origin")
	if err != nil || strings.TrimSpace(currentRepo) != receipt.Repository {
		return AutonomyReceipt{}, fmt.Errorf("autonomy receipt repository identity changed")
	}
	branch, err := gitCommand(repo, "rev-parse", "--abbrev-ref", "HEAD")
	branch = strings.TrimSpace(branch)
	branchMatches := branch == receipt.Branch || (allowIssuingBranch && receipt.IssuingBranch != "" && branch == receipt.IssuingBranch)
	if err != nil || !branchMatches {
		return AutonomyReceipt{}, fmt.Errorf("autonomy receipt branch identity changed")
	}
	rank := map[RunTarget]int{RunTargetPlan: 1, RunTargetVerified: 2, RunTargetPR: 3}
	if rank[receipt.Target] < rank[minimumTarget] {
		return AutonomyReceipt{}, fmt.Errorf("autonomy target %s does not authorize %s", receipt.Target, minimumTarget)
	}
	if action != "" && receipt.PRAction != action {
		return AutonomyReceipt{}, fmt.Errorf("autonomy receipt authorizes PR action %s, not %s", receipt.PRAction, action)
	}
	evidence, err := LoadEvidenceLedger(filepath.Join(filepath.Dir(check.PlanPath), "evidence.json"))
	if err != nil {
		return AutonomyReceipt{}, err
	}
	decisions, err := validateAutonomyDecisions(check.Plan, evidence)
	if err != nil {
		return AutonomyReceipt{}, err
	}
	wantDecisions, _ := MarshalJSON(decisions)
	gotDecisions, _ := MarshalJSON(receipt.Decisions)
	if SHA256Bytes(wantDecisions) != SHA256Bytes(gotDecisions) {
		return AutonomyReceipt{}, fmt.Errorf("autonomy decision set changed")
	}
	selectedEvidence, err := selectedAutonomyEvidence(decisions, evidence)
	if err != nil {
		return AutonomyReceipt{}, err
	}
	wantEvidence, _ := MarshalJSON(selectedEvidence)
	gotEvidence, _ := MarshalJSON(receipt.Evidence)
	if SHA256Bytes(wantEvidence) != SHA256Bytes(gotEvidence) {
		return AutonomyReceipt{}, fmt.Errorf("autonomy evidence changed")
	}
	return receipt, nil
}

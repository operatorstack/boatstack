package boatstack

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// surfaceSlugPattern names a product surface (web, ops, admin-console):
// lowercase kebab, matching the project.commands["visual:<surface>"] key
// convention shared by plan scenarios and capability-register --surface.
var surfaceSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type ValidatePlanOptions struct {
	PlanPath     string
	RepoRoot     string
	RepoRevision string
}

// requireVisualSurfaceEvidence makes registered surface paths a shared,
// deterministic visual-impact input during planning. The approved decision is
// then reused by capture, review, and ship.
func requireVisualSurfaceEvidence(plan map[string]any, opts *ValidatePlanOptions) error {
	if opts == nil || opts.RepoRoot == "" || opts.PlanPath == "" {
		return nil
	}
	config, _, err := LoadConfig(WorkspaceFor(opts.RepoRoot).ProjectConfigPath())
	if err != nil || len(config.Project.VisualSurfaces) == 0 {
		return nil
	}
	baseline, err := productBaseline(opts.RepoRoot, opts.PlanPath)
	if err != nil {
		return err
	}
	matched := visualSurfaceChangedPaths(baseline.ChangedPaths, config.Project.VisualSurfaces)
	if len(matched) == 0 {
		return nil
	}
	decision, _ := plan["pr_visual_evidence"].(map[string]any)
	if stringValue(decision["relevance"]) != "relevant" {
		return fmt.Errorf("changes under registered visual surfaces require relevant pr_visual_evidence scenarios: %s", strings.Join(matched, ", "))
	}
	return nil
}

func visualSurfaceChangedPaths(changed []string, surfaces []VisualSurface) []string {
	seen := map[string]bool{}
	for _, changedPath := range changed {
		path := filepath.ToSlash(filepath.Clean(changedPath))
		for _, surface := range surfaces {
			for _, pattern := range surface.Paths {
				pattern = filepath.ToSlash(strings.TrimSpace(pattern))
				matched, _ := filepath.Match(filepath.FromSlash(pattern), filepath.FromSlash(path))
				prefix := strings.TrimSuffix(strings.TrimSuffix(pattern, "/**"), "/")
				if matched || (prefix != pattern && (path == prefix || strings.HasPrefix(path, prefix+"/"))) {
					seen[path] = true
				}
			}
		}
	}
	result := make([]string, 0, len(seen))
	for path := range seen {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func validateArchitectureGrounding(plan map[string]any, opts *ValidatePlanOptions) error {
	var ledger map[string]EvidenceRecord
	if opts != nil && opts.PlanPath != "" && opts.RepoRoot != "" {
		ledgerPath := filepath.Join(filepath.Dir(opts.PlanPath), "evidence.json")
		var err error
		ledger, err = LoadEvidenceLedger(ledgerPath)
		if err != nil {
			return err
		}
	} else {
		// In some tests or missing context cases, we initialize an empty ledger.
		ledger = make(map[string]EvidenceRecord)
	}

	facts, _ := objectSlice(plan["architecture_facts"])
	factIDs := make(map[string]bool)
	for _, fact := range facts {
		id := stringValue(fact["id"])
		if id == "" || factIDs[id] {
			return fmt.Errorf("architecture fact ID must be present and unique")
		}
		factIDs[id] = true

		kind := stringValue(fact["kind"])
		validKinds := map[string]bool{
			"route_exists": true, "route_absent": true, "symbol_exists": true,
			"component_exists": true, "test_target_exists": true, "data_access_pattern": true,
			"external_service_usage": true,
		}
		if !validKinds[kind] {
			return fmt.Errorf("unsupported architecture fact kind: %s", kind)
		}

		var evidence []EvidenceRecord
		evidenceLevel := EvidenceVerified
		premiseStatus := PremiseValid
		evidenceIDs, ok := stringSlice(fact["evidence_ids"])

		if !ok || len(evidenceIDs) == 0 {
			evidenceLevel = EvidenceAbsent
		} else {
			// Validation of evidence against ledger
			for _, evID := range evidenceIDs {
				record, exists := ledger[evID]
				if !exists {
					evidenceLevel = EvidenceAbsent
					break
				}
				evidence = append(evidence, record)
				if opts != nil && opts.RepoRevision != "" && record.RepositoryRevision != opts.RepoRevision {
					evidenceLevel = EvidenceSupported
				}

				// Basic operation check
				if kind == "route_absent" && record.Operation != "repository_search" && record.Operation != "route_lookup" {
					premiseStatus = PremiseInvalid
				}

				if opts != nil && opts.RepoRoot != "" && record.Path != "" {
					if err := ValidateEvidencePath(opts.RepoRoot, record.Path); err != nil {
						premiseStatus = PremiseInvalid
					}
					if len(record.Anchors) > 0 {
						targetPath := filepath.Join(opts.RepoRoot, filepath.Clean(record.Path))
						if err := CheckFileAnchors(targetPath, record.Anchors); err != nil {
							premiseStatus = PremiseInvalid
						}
					}
				}
			}
		}

		resolution := ResolvePlanDecision(PlanDecisionInput{
			DecisionKind:       "architecture_fact",
			IsMaterial:         false,
			RepositoryEvidence: evidence,
			EvidenceLevel:      evidenceLevel,
			PremiseStatus:      premiseStatus,
		})

		if resolution.Operator != OperatorInfer {
			return fmt.Errorf("architecture fact %s requires %s (%s): %s", id, resolution.Operator, resolution.RuleID, resolution.Reason)
		}
	}

	unknowns, _ := objectSlice(plan["architecture_unknowns"])
	unknownIDs := make(map[string]bool)
	for _, unk := range unknowns {
		id := stringValue(unk["id"])
		if id == "" || unknownIDs[id] {
			return fmt.Errorf("architecture unknown ID must be present and unique")
		}
		unknownIDs[id] = true
	}

	tasks, _ := objectSlice(plan["tasks"])
	for _, task := range tasks {
		id := stringValue(task["id"])
		reqFacts, _ := stringSlice(task["requires_facts"])
		for _, req := range reqFacts {
			if !factIDs[req] {
				return fmt.Errorf("task %s references unknown architecture fact %s", id, req)
			}
		}

		// check blocked by unknowns
		for _, unk := range unknowns {
			blocks, _ := stringSlice(unk["blocks"])
			for _, b := range blocks {
				if b == id {
					// unless it's a discovery task (no acceptance criteria? or named discovery)
					// Let's just say blocked by unknown means blocked.
					return fmt.Errorf("task %s is blocked by unresolved architecture unknown %s", id, stringValue(unk["id"]))
				}
			}
		}
	}

	return nil
}

func validatePlanAutonomy(plan map[string]any, opts *ValidatePlanOptions) error {
	if _, present := plan["autonomy_decisions"]; !present {
		return nil
	}
	var evidence map[string]EvidenceRecord
	if opts != nil && opts.PlanPath != "" {
		var err error
		evidence, err = LoadEvidenceLedger(filepath.Join(filepath.Dir(opts.PlanPath), "evidence.json"))
		if err != nil {
			return err
		}
	}
	_, err := validateAutonomyDecisions(plan, evidence)
	return err
}

func validateSystemicBoundaries(plan map[string]any) error {
	boundaries, ok := objectSlice(plan["systemic_boundaries"])
	if !ok || len(boundaries) == 0 {
		return nil
	}

	slices, ok := objectSlice(plan["delivery_slices"])
	if !ok || len(slices) < 2 {
		return fmt.Errorf("Programmatic enforcement requires a minimum of 2 delivery_slices (Boundary -> Feature)")
	}

	for _, boundary := range boundaries {
		id := stringValue(boundary["id"])
		if id == "" {
			return fmt.Errorf("systemic boundary requires an id")
		}
		if stringValue(boundary["verification_oracle"]) == "" {
			return fmt.Errorf("systemic boundary %s requires a verification_oracle", id)
		}
	}
	return nil
}

func validatePRVisualEvidence(plan map[string]any) error {
	value, present := plan["pr_visual_evidence"]
	if !present {
		return nil
	}
	decision, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("pr_visual_evidence must be an object")
	}
	relevance := stringValue(decision["relevance"])
	if relevance != "relevant" && relevance != "not_relevant" {
		return fmt.Errorf("pr_visual_evidence.relevance must be relevant or not_relevant")
	}
	scenarios, ok := objectSlice(decision["scenarios"])
	if !ok && decision["scenarios"] != nil {
		return fmt.Errorf("pr_visual_evidence.scenarios must be a list")
	}
	if relevance == "not_relevant" {
		if strings.TrimSpace(stringValue(decision["reason"])) == "" {
			return fmt.Errorf("not-relevant pr_visual_evidence requires a reason")
		}
		if len(scenarios) != 0 {
			return fmt.Errorf("not-relevant pr_visual_evidence must not define scenarios")
		}
		return nil
	}
	if len(scenarios) == 0 || len(scenarios) > 3 {
		return fmt.Errorf("relevant pr_visual_evidence requires one to three scenarios")
	}
	seen := map[string]bool{}
	for _, scenario := range scenarios {
		id := stringValue(scenario["id"])
		if id == "" || seen[id] {
			return fmt.Errorf("pr_visual_evidence scenario ids must be present and unique")
		}
		seen[id] = true
		for _, field := range []string{"entry", "state", "viewport"} {
			if stringValue(scenario[field]) == "" {
				return fmt.Errorf("pr_visual_evidence scenario %s requires %s", id, field)
			}
		}
		if surface := stringValue(scenario["surface"]); surface != "" && !surfaceSlugPattern.MatchString(surface) {
			return fmt.Errorf("pr_visual_evidence scenario %s surface must be a lowercase kebab slug", id)
		}
		expected, ok := stringSlice(scenario["expected"])
		if !ok || len(expected) == 0 {
			return fmt.Errorf("pr_visual_evidence scenario %s requires expected visible outcomes", id)
		}
	}
	return nil
}

func requireConfiguredPRVisualEvidenceDecision(plan map[string]any, opts *ValidatePlanOptions) error {
	if opts == nil || opts.RepoRoot == "" {
		return nil
	}
	config, _, err := LoadConfig(WorkspaceFor(opts.RepoRoot).ProjectConfigPath())
	if err != nil || normalizedPRVisualEvidencePolicy(config.Workflow.PRVisualEvidence) == "off" {
		return nil
	}
	if _, present := plan["pr_visual_evidence"]; !present {
		return fmt.Errorf("configured workflow.pr_visual_evidence requires a structural plan decision")
	}
	return nil
}

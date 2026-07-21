package boatstack

import (
	"fmt"
	"path/filepath"
)

type ValidatePlanOptions struct {
	PlanPath     string
	RepoRoot     string
	RepoRevision string
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
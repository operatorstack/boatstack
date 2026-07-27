package boatstack

import (
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// A denial that only states the law leaves a weaker model looping on the same
// blocked call. Each denial therefore carries the law's computed solution set:
// the admissible commands from exactly the position the finding describes,
// derived from the same declarations the guard enforces (the planning
// enumeration for phase findings, the registry's observe rows, the
// state-ownership map for protected paths) — never a hand-written list.
// The enumeration reads only the finding's own fields and the declared tables,
// so the deny path stays fast and cannot itself fail on unreadable state.
// control-law: solution-set-derives-from-guard-declarations

// denialMarker names a denial-prescribed helper verb outside the delivery
// model, mirroring the planning./recovery. marker convention: self-describing
// provenance, never a legal registry transition, never auto-driven.
func denialMarker(verb string) deliverycontrol.TransitionID {
	return deliverycontrol.TransitionID("denial." + strings.ReplaceAll(verb, "-", "_"))
}

// denialSolutionExceptions are the finding categories that deliberately carry
// no solution set, with the reason. The totality sweep fails a new category
// until it gains an enumeration rule or an entry here.
var denialSolutionExceptions = map[string]string{
	"malformed-tool-input":  "the tool event itself is unreadable; the Detail already names diagnose-hook with the exact host",
	"unsupported-host":      "an unknown host has no trusted verb surface to enumerate",
	"unresolved-repository": "without a repository identity no command can be assembled faithfully",
}

// enumerateDenialSolutions computes the solution set for a denial finding.
// host is the coding host the hook is serving (for diagnose-hook assembly).
func enumerateDenialSolutions(repo, host string, finding SafetyFinding) SolutionSet {
	set := SolutionSet{Basis: "denial", Stage: finding.WorkflowStage}
	if _, excepted := denialSolutionExceptions[finding.Category]; excepted {
		return set
	}
	switch {
	case finding.Category == "workflow-phase-bypass", finding.Category == "workflow-state-invalid":
		// The finding carries the exact planning position; re-run the planning
		// enumeration from it. Pure — no filesystem reads on the deny path.
		status := NextStatus{
			ObservedStage: finding.WorkflowStage,
			NextOperation: finding.NextOperation,
			Feature:       finding.BlockingFeature,
		}
		if finding.Category == "workflow-state-invalid" {
			status.ObservedStage = "INVALID_STATE"
			if finding.BlockingFeature != "" {
				status.BlockingAmbiguity = []string{finding.BlockingFeature}
			}
		}
		next := FlowNext{}
		if cmd, _ := prescribePlanning(repo, status); cmd != nil {
			next.Prescribed = cmd
		}
		planning := enumeratePlanningSolutions(repo, status, next)
		set.Options, set.Truncated = planning.Options, planning.Truncated
		return set

	case finding.Category == "workflow-state-tamper":
		// The state-ownership map already declares who may write the path; the
		// pick list is the position observers plus the hook diagnosis, and the
		// owning verbs surface separately (OwnerVerbs) — a verb whose full
		// arguments we cannot derive is named, never fabricated into a command.
		appendObserveOption(&set, repo, "", "delivery.next")
		appendDiagnoseHook(&set, repo, host)
		return set

	case finding.Category == "workflow-publication-bypass":
		if finding.BlockingFeature != "" {
			if cmd, ok := prescribeCommand(repo, finding.BlockingFeature, NextStatus{ActiveSlice: finding.BlockingSlice}, PublishTransition); ok {
				appendSolution(&set, *cmd)
			}
		}
		appendObserveOption(&set, repo, finding.BlockingFeature, "delivery.recovery_status")
		appendObserveOption(&set, repo, "", "delivery.next")
		return set

	case strings.HasPrefix(finding.Category, "operation-"):
		// Observation-only by design: inspect the durable operation state before
		// any retry (the observed-effect discipline).
		appendSolution(&set, PrescribedCommand{
			Verb: "operation-status", Args: repoFlagArgs(repo), AutoDerivable: true,
			Transition: denialMarker("operation-status"),
		})
		appendSolution(&set, PrescribedCommand{
			Verb: "mutation-status", Args: repoFlagArgs(repo), AutoDerivable: true,
			Transition: denialMarker("mutation-status"),
		})
		return set

	case finding.Category == "filesystem-destruction":
		// The sanctioned actuator for the one deletion Boatstack owns; the
		// operator confirmation is owed, never assumed.
		appendSolution(&set, PrescribedCommand{
			Verb: "workspace-reap", Args: repoFlagArgs(repo),
			RequiresHumanInput: []string{"--confirm"},
			Transition:         denialMarker("workspace-reap"),
		})
		appendDoctor(&set, repo)
		appendObserveOption(&set, repo, "", "delivery.next")
		return set
	}

	// Generic fallthrough (destruction families, sync bypass, anything new):
	// the position observers and the installation diagnosis are always legal.
	appendObserveOption(&set, repo, "", "delivery.next")
	appendDoctor(&set, repo)
	return set
}

// tamperOwnerVerbs derives the owning verbs of a protected path from the
// state-ownership map: the guard-protected entry whose boatstack subtree the
// attempted path names. Derived at runtime from StateRegistry — the same
// declaration the statemap conformance holds to the guard patterns.
// control-law: every-managed-path-has-a-declared-owner
func tamperOwnerVerbs(repo, attempted string) []string {
	if attempted == "" {
		return nil
	}
	normalized := filepath_ToSlashLower(attempted)
	w := WorkspaceFor(repo)
	for _, entry := range StateRegistry() {
		if !entry.GuardProtected {
			continue
		}
		sample, err := entry.Sample(w)
		if err != nil {
			continue
		}
		key := boatstackSubtreeKey(filepath_ToSlashLower(sample))
		if key != "" && strings.Contains(normalized, "boatstack/"+key) {
			return entry.OwnerVerbs
		}
	}
	return nil
}

// boatstackSubtreeKey extracts the first path segment after the last
// "boatstack/" in a sample path — the subtree a guard-protected entry owns.
func boatstackSubtreeKey(path string) string {
	marker := "boatstack/"
	index := strings.LastIndex(path, marker)
	if index < 0 {
		return ""
	}
	rest := path[index+len(marker):]
	if cut := strings.IndexByte(rest, '/'); cut >= 0 {
		return rest[:cut]
	}
	return rest
}

// filepath_ToSlashLower normalizes a path for fragment matching across
// platforms and case conventions.
func filepath_ToSlashLower(path string) string {
	return strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
}

func appendObserveOption(set *SolutionSet, repo, feature, transition string) {
	descriptor, ok := deliverycontrol.Transition(deliverycontrol.TransitionID(transition))
	if !ok {
		return
	}
	if cmd, ok := prescribeObserve(repo, feature, descriptor); ok {
		appendSolution(set, *cmd)
	}
}

func appendDoctor(set *SolutionSet, repo string) {
	appendSolution(set, PrescribedCommand{
		Verb: "doctor", Args: repoFlagArgs(repo), AutoDerivable: true,
		Transition: MarkerRecoveryDoctor,
	})
}

func appendDiagnoseHook(set *SolutionSet, repo, host string) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		appendDoctor(set, repo)
		return
	}
	appendSolution(set, PrescribedCommand{
		Verb:          "diagnose-hook",
		Args:          append([]string{"--host", host}, repoFlagArgs(repo)...),
		AutoDerivable: true,
		Transition:    denialMarker("diagnose-hook"),
	})
}

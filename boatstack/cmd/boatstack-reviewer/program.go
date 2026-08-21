package main

import (
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/kernel"
)

// Review control modes. The first mode is the initial mode; `converged` is
// the only marked mode, so an untargeted resolve on a converged instance
// answers MARKED instead of prescribing further work.
const (
	modeUnreviewed   = "unreviewed"
	modeFindingsOpen = "findings-open"
	modeConverged    = "converged"
	modeEscalated    = "escalated"
)

// Transition identities.
const (
	transitionConverge = "review.converge"
	transitionRecord   = "review.findings.record"
	transitionEscalate = "review.escalate"
	transitionReopen   = "review.reopen"
	transitionRecover  = "review.recover"
)

// Capabilities. The proposer (a coding agent or a human running the loop)
// submits candidates with `review.submit`; reopening a settled generation is
// a human decision (`review.human`); recovery is explicit (`review.recover`).
const (
	capabilitySubmit  = kernel.Capability("review.submit")
	capabilityHuman   = kernel.Capability("review.human")
	capabilityRecover = kernel.Capability("review.recover")
)

const facetRound = "review.round"

const (
	programID      = "boatstack-reviewer"
	programVersion = "1"
	programRuntime = "kernel-v1"
)

// compileReviewProgram binds the executable review control law and the exact
// admitted policy into one kernel Program identity. A policy change (prompt,
// schema, bounds, weights) or a law change (any transition edit) produces a
// different fingerprint, which stales every prior prescription and makes
// prior sealed receipts honestly unverifiable against the new program.
func compileReviewProgram(policy Policy) (kernel.Program, error) {
	if err := policy.validate(); err != nil {
		return kernel.Program{}, err
	}
	contract, err := policy.contractFingerprint()
	if err != nil {
		return kernel.Program{}, err
	}
	submitSources := []string{modeUnreviewed, modeFindingsOpen}
	transitions := []kernel.Transition{
		{
			ID:                   transitionConverge,
			SourceModes:          submitSources,
			TargetMode:           modeConverged,
			ObjectiveScope:       kernel.ObjectiveNone,
			ObjectiveMutation:    kernel.PreserveObjective,
			RequiredCapabilities: []kernel.Capability{capabilitySubmit},
			OwnedFacets:          []string{facetRound},
			Operation:            transitionConverge,
			Priority:             10,
		},
		{
			ID:                   transitionRecord,
			SourceModes:          submitSources,
			TargetMode:           modeFindingsOpen,
			ObjectiveScope:       kernel.ObjectiveNone,
			ObjectiveMutation:    kernel.PreserveObjective,
			RequiredCapabilities: []kernel.Capability{capabilitySubmit},
			OwnedFacets:          []string{facetRound},
			Operation:            transitionRecord,
			Priority:             20,
		},
		{
			ID:                   transitionEscalate,
			SourceModes:          submitSources,
			TargetMode:           modeEscalated,
			ObjectiveScope:       kernel.ObjectiveNone,
			ObjectiveMutation:    kernel.PreserveObjective,
			RequiredCapabilities: []kernel.Capability{capabilitySubmit},
			OwnedFacets:          []string{facetRound},
			Operation:            transitionEscalate,
			Priority:             30,
		},
		{
			ID:                   transitionReopen,
			SourceModes:          []string{modeConverged, modeEscalated},
			TargetMode:           modeUnreviewed,
			ObjectiveScope:       kernel.ObjectiveNone,
			ObjectiveMutation:    kernel.PreserveObjective,
			RequiredCapabilities: []kernel.Capability{capabilityHuman},
			OwnedFacets:          []string{facetRound},
			Operation:            transitionReopen,
			Priority:             40,
		},
		{
			ID:                   transitionRecover,
			SourceModes:          []string{modeUnreviewed, modeFindingsOpen, modeConverged, modeEscalated},
			TargetMode:           modeUnreviewed,
			ObjectiveScope:       kernel.ObjectiveNone,
			ObjectiveMutation:    kernel.PreserveObjective,
			RequiredCapabilities: []kernel.Capability{capabilityRecover},
			OwnedFacets:          []string{facetRound},
			Operation:            transitionRecover,
			Priority:             5,
			Recovers: []string{
				transitionConverge,
				transitionRecord,
				transitionEscalate,
				transitionReopen,
			},
		},
	}
	return kernel.CompileDomainProgram(
		programID, programVersion, programRuntime, contract,
		modeUnreviewed, []string{modeConverged}, transitions,
	)
}

// reviewCapabilities is the trusted capability classifier: the minimum
// capability for each concrete review operation is fixed mechanism
// configuration, never program or proposer data.
type reviewCapabilities struct{}

func (reviewCapabilities) RequiredCapabilities(transition kernel.Transition) ([]kernel.Capability, error) {
	switch transition.Operation {
	case transitionConverge, transitionRecord, transitionEscalate:
		return []kernel.Capability{capabilitySubmit}, nil
	case transitionReopen:
		return []kernel.Capability{capabilityHuman}, nil
	case transitionRecover:
		return []kernel.Capability{capabilityRecover}, nil
	default:
		return nil, fmt.Errorf("unclassified review operation %q", transition.Operation)
	}
}

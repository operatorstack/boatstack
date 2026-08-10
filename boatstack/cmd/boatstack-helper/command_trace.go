package main

import (
	"strings"
	"time"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

type commandTracePolicy struct {
	Category       string
	ExcludedReason string
}

// commandTracePolicies is the declared helper surface inventory. Safety hooks
// are intentionally excluded because telemetry must not add latency or writes
// to the enforcement path; every other dispatch is recorded once by run().
var commandTracePolicies = map[string]commandTracePolicy{
	"attach": {Category: "supervision"}, "detach": {Category: "supervision"},
	"detached-status": {Category: "supervision"}, "engagement-status": {Category: "supervision"}, "config-rebind": {Category: "supervision"}, "context": {Category: "supervision"},
	"activate": {Category: "supervision"}, "deactivate": {Category: "supervision"},
	"init": {Category: "installation"}, "update": {Category: "installation"},
	"check-update": {Category: "installation"}, "repair-status": {Category: "installation"},
	"prepare-update-pr": {Category: "update-publication"}, "publish-update-pr": {Category: "update-publication"},
	"release-classify": {Category: "release"}, "next-patch": {Category: "release"},
	"export": {Category: "installation"}, "migrate-config": {Category: "installation"},
	"hydrate-runtime": {Category: "installation"}, "activate-worktree-runtime": {Category: "installation"}, "doctor": {Category: "readiness"},
	"check-source-plan": {Category: "planning"}, "check-plan": {Category: "planning"},
	"planning-write": {Category: "planning"}, "record-approval": {Category: "planning"},
	"record-autonomy": {Category: "planning"}, "activate-plan": {Category: "delivery"},
	"delivery-status": {Category: "delivery"}, "next-status": {Category: "delivery"},
	"recovery-status": {Category: "recovery"}, "repair-state": {Category: "recovery"},
	"mutation-status": {Category: "recovery"}, "undo": {Category: "recovery"},
	"run-preflight": {Category: "readiness"}, "authority-context": {Category: "readiness"},
	"record-change": {Category: "recovery"}, "record-journey-results": {Category: "evidence"},
	"ignore-delivery": {Category: "delivery"}, "discard-delivery": {Category: "recovery"},
	"record-delivery-gate": {Category: "delivery"}, "record-pr-visual-evidence": {Category: "visual-evidence"},
	"review-pr-visual-evidence": {Category: "visual-evidence"}, "capture-evidence": {Category: "visual-evidence"},
	"provision-capability": {Category: "capability"}, "capability-register": {Category: "capability"},
	"record-pr-visual-publication": {Category: "visual-evidence"}, "attach-evidence": {Category: "visual-evidence"},
	"pr-context": {Category: "publication"}, "check-pr": {Category: "publication"},
	"publish-pr": {Category: "publication"}, "operation-status": {Category: "publication"},
	"diagnose-hook": {Category: "diagnostic"}, "render-denial": {Category: "diagnostic"},
	"check-safety": {Category: "readiness"}, "workspace-cut": {Category: "workspace"},
	"workspace-cleanup": {Category: "workspace"}, "workspace-reap": {Category: "workspace"},
	"workspace-status": {Category: "workspace"}, "workspace-sync": {Category: "workspace"},
	"flow": {Category: "flow"}, "retro": {Category: "analysis"},
	"insight": {Category: "insight"}, "version": {Category: "diagnostic"},
	"safety-hook":           {Category: "safety", ExcludedReason: "latency-sensitive enforcement path"},
	"engagement-probe":      {Category: "safety", ExcludedReason: "latency-sensitive engagement path"},
	"bootstrap-safety-hook": {Category: "safety", ExcludedReason: "latency-sensitive enforcement path"},
}

func traceFlag(arguments []string, name string) string {
	for index, argument := range arguments {
		if argument == name && index+1 < len(arguments) {
			return strings.TrimSpace(arguments[index+1])
		}
		if strings.HasPrefix(argument, name+"=") {
			return strings.TrimSpace(strings.TrimPrefix(argument, name+"="))
		}
	}
	return ""
}

func traceTransition(verb string, arguments []string) deliverycontrol.TransitionID {
	if verb == "record-delivery-gate" {
		switch strings.ToLower(traceFlag(arguments, "--gate")) {
		case "test":
			return "delivery.record_gate_test"
		case "review":
			return "delivery.record_gate_review"
		}
	}
	for _, transition := range deliverycontrol.Transitions() {
		if transition.CLIVerb == verb {
			return transition.ID
		}
	}
	return ""
}

func commandTraceCompletion(verb string, arguments []string) func(int) {
	policy, ok := commandTracePolicies[verb]
	if !ok || policy.ExcludedReason != "" {
		return nil
	}
	started := time.Now()
	recordedVerb := verb
	if (verb == "flow" || verb == "retro" || verb == "insight") && len(arguments) > 0 && !strings.HasPrefix(arguments[0], "-") {
		recordedVerb += "/" + arguments[0]
	}
	repo := traceFlag(arguments, "--repo")
	if repo == "" {
		repo = "."
	}
	feature := traceFlag(arguments, "--feature")
	slice := traceFlag(arguments, "--slice")
	transition := traceTransition(verb, arguments)
	return func(exitCode int) {
		boatstack.RecordCommandEvent(boatstack.CommandTraceInput{
			Repo: repo, Verb: recordedVerb, Category: policy.Category, Feature: feature, Slice: slice,
			Transition: transition, StartedAt: started, FinishedAt: time.Now(), ExitCode: exitCode,
		})
	}
}

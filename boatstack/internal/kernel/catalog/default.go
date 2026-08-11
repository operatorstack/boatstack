package catalog

import (
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

const DefaultTransitionCount = 61

type seed struct {
	id        TransitionID
	class     EventClass
	source    []model.ProtocolPhase
	target    []model.ProtocolPhase
	authority []AuthorityClass
	resource  string
	goals     []model.GoalKind
	priority  int
}

var activeSources = []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseFrontier, model.PhaseUnresolved}
var managedSources = []model.ProtocolPhase{model.PhaseDormant, model.PhaseObserved, model.PhaseActive, model.PhaseRecovery, model.PhaseFrontier, model.PhaseUnresolved}

func Default() Registry {
	registry, err := New(defaultTransitions())
	if err != nil {
		panic(err)
	}
	if registry.Len() != DefaultTransitionCount {
		panic("boatstack V2 default transition count drifted")
	}
	return registry
}

func defaultTransitions() []Transition {
	allGoals := []model.GoalKind{model.GoalApprovedPlan, model.GoalVerified, model.GoalOpenPR, model.GoalMerged, model.GoalAbandoned}
	seeds := []seed{
		{"engagement.begin", EventAuthority, []model.ProtocolPhase{model.PhaseDormant, model.PhaseObserved}, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive}, []AuthorityClass{AuthorityRepository}, "engagement", allGoals, 10},
		{"engagement.renew", EventAuthority, []model.ProtocolPhase{model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive}, []AuthorityClass{AuthorityRepository, AuthorityAutonomy}, "engagement", allGoals, 70},
		{"engagement.release", EventAuthority, []model.ProtocolPhase{model.PhaseActive, model.PhaseFrontier}, []model.ProtocolPhase{model.PhaseDormant}, []AuthorityClass{AuthorityRepository}, "engagement", allGoals, 95},
		{"invocation.rebind", EventOwnedLocal, []model.ProtocolPhase{model.PhaseObserved, model.PhaseUnresolved}, []model.ProtocolPhase{model.PhaseObserved}, []AuthorityClass{AuthorityRepository}, "identity-binding", allGoals, 15},
		{"repository.attach", EventOwnedLocal, []model.ProtocolPhase{model.PhaseDormant, model.PhaseObserved}, []model.ProtocolPhase{model.PhaseObserved}, []AuthorityClass{AuthorityHuman}, "repository-binding", allGoals, 12},
		{"repository.detach", EventOwnedLocal, []model.ProtocolPhase{model.PhaseDormant, model.PhaseObserved, model.PhaseFrontier}, []model.ProtocolPhase{model.PhaseDormant}, []AuthorityClass{AuthorityHuman}, "repository-binding", allGoals, 96},

		{"runtime.hydrate", EventOwnedLocal, []model.ProtocolPhase{model.PhaseObserved, model.PhaseRecovery, model.PhaseUnresolved}, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseTerminal}, []AuthorityClass{AuthorityRepository}, "runtime", allGoals, 20},
		{"runtime.replace", EventOwnedLocal, []model.ProtocolPhase{model.PhaseObserved, model.PhaseRecovery}, []model.ProtocolPhase{model.PhaseObserved, model.PhaseTerminal}, []AuthorityClass{AuthorityHuman, AuthorityRepository}, "runtime", allGoals, 25},
		{"runtime.reconcile", EventRecovery, []model.ProtocolPhase{model.PhaseRecovery, model.PhaseUnresolved}, []model.ProtocolPhase{model.PhaseObserved, model.PhaseFrontier, model.PhaseTerminal}, []AuthorityClass{AuthorityRepository}, "runtime", allGoals, 4},
		{"configuration.initialize", EventOwnedLocal, []model.ProtocolPhase{model.PhaseObserved}, []model.ProtocolPhase{model.PhaseObserved, model.PhaseTerminal}, []AuthorityClass{AuthorityHuman, AuthorityRepository}, "configuration", allGoals, 22},
		{"configuration.mutate", EventOwnedLocal, activeSources, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseTerminal}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "configuration", allGoals, 60},
		{"configuration.reconcile", EventRecovery, []model.ProtocolPhase{model.PhaseRecovery, model.PhaseUnresolved}, []model.ProtocolPhase{model.PhaseObserved, model.PhaseFrontier, model.PhaseTerminal}, []AuthorityClass{AuthorityHuman, AuthorityRepository}, "configuration", allGoals, 3},
		{"installation.initialize", EventOwnedLocal, []model.ProtocolPhase{model.PhaseDormant, model.PhaseObserved}, []model.ProtocolPhase{model.PhaseObserved}, []AuthorityClass{AuthorityHuman}, "installation", allGoals, 11},
		{"installation.update", EventOwnedLocal, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive}, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseTerminal}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "installation", allGoals, 65},

		{"goal.configure", EventAuthority, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseFrontier, model.PhaseTerminal, model.PhaseAbandoned}, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseFrontier}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "goal", allGoals, 30},
		{"plan.create", EventOwnedLocal, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "plan", allGoals, 35},
		{"plan.validate", EventOwnedLocal, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive, model.PhaseFrontier}, []AuthorityClass{AuthorityRepository}, "plan-evidence", allGoals, 40},
		{"plan.approve", EventAuthority, []model.ProtocolPhase{model.PhaseActive, model.PhaseFrontier}, []model.ProtocolPhase{model.PhaseActive, model.PhaseTerminal}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "approval", allGoals, 45},
		{"plan.activate", EventOwnedLocal, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "delivery-state", []model.GoalKind{model.GoalVerified, model.GoalOpenPR, model.GoalMerged}, 50},
		{"plan.amend", EventOwnedLocal, []model.ProtocolPhase{model.PhaseActive, model.PhaseFrontier}, []model.ProtocolPhase{model.PhaseActive}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "plan", allGoals, 42},
		{"plan.approve-amendment", EventAuthority, []model.ProtocolPhase{model.PhaseActive, model.PhaseFrontier}, []model.ProtocolPhase{model.PhaseActive}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "approval", allGoals, 46},
		{"plan.invalidate", EventOwnedLocal, []model.ProtocolPhase{model.PhaseActive, model.PhaseObserved}, []model.ProtocolPhase{model.PhaseFrontier}, []AuthorityClass{AuthorityRepository}, "plan-evidence", allGoals, 41},
		{"plan.abandon", EventAuthority, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseFrontier}, []model.ProtocolPhase{model.PhaseAbandoned}, []AuthorityClass{AuthorityHuman}, "plan", []model.GoalKind{model.GoalAbandoned}, 90},

		{"workspace.cut", EventOwnedLocal, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "workspace", allGoals, 52},
		{"workspace.sync", EventOwnedLocal, []model.ProtocolPhase{model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive, model.PhaseFrontier}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "workspace", allGoals, 58},
		{"workspace.activate", EventOwnedLocal, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive}, []AuthorityClass{AuthorityRepository}, "workspace", allGoals, 53},
		{"workspace.publish", EventOwnedLocal, []model.ProtocolPhase{model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive}, []AuthorityClass{AuthorityRepository}, "workspace-state", []model.GoalKind{model.GoalOpenPR, model.GoalMerged}, 75},
		{"workspace.cleanup", EventOwnedLocal, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseTerminal, model.PhaseAbandoned}, []model.ProtocolPhase{model.PhaseObserved, model.PhaseTerminal, model.PhaseAbandoned}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "workspace", allGoals, 92},
		{"workspace.reap", EventOwnedLocal, []model.ProtocolPhase{model.PhaseObserved, model.PhaseTerminal, model.PhaseAbandoned}, []model.ProtocolPhase{model.PhaseObserved, model.PhaseTerminal, model.PhaseAbandoned}, []AuthorityClass{AuthorityHuman}, "workspace", allGoals, 98},
		{"workspace.abandon", EventOwnedLocal, []model.ProtocolPhase{model.PhaseActive, model.PhaseFrontier}, []model.ProtocolPhase{model.PhaseAbandoned}, []AuthorityClass{AuthorityHuman}, "workspace", []model.GoalKind{model.GoalAbandoned}, 91},
		{"workspace.reconcile", EventRecovery, []model.ProtocolPhase{model.PhaseRecovery, model.PhaseUnresolved}, []model.ProtocolPhase{model.PhaseDormant, model.PhaseObserved, model.PhaseActive, model.PhaseFrontier, model.PhaseTerminal, model.PhaseAbandoned}, []AuthorityClass{AuthorityHuman, AuthorityRepository}, "workspace", allGoals, 2},

		{"gate.build.record", EventOwnedLocal, []model.ProtocolPhase{model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive}, []AuthorityClass{AuthorityRepository}, "gate-evidence", allGoals, 61},
		{"gate.test.record", EventOwnedLocal, []model.ProtocolPhase{model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive, model.PhaseTerminal}, []AuthorityClass{AuthorityRepository}, "gate-evidence", allGoals, 62},
		{"gate.review.record", EventOwnedLocal, []model.ProtocolPhase{model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive, model.PhaseTerminal}, []AuthorityClass{AuthorityHuman, AuthorityRepository}, "gate-evidence", allGoals, 63},
		{"gate.change.record", EventOwnedLocal, []model.ProtocolPhase{model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive}, []AuthorityClass{AuthorityRepository}, "gate-evidence", allGoals, 64},
		{"gate.journey.record", EventOwnedLocal, []model.ProtocolPhase{model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive}, []AuthorityClass{AuthorityRepository}, "gate-evidence", allGoals, 64},
		{"evidence.visual.attach", EventOwnedLocal, []model.ProtocolPhase{model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive, model.PhaseTerminal}, []AuthorityClass{AuthorityHuman, AuthorityRepository}, "evidence", allGoals, 66},
		{"evidence.approval.revoke", EventAuthority, []model.ProtocolPhase{model.PhaseActive, model.PhaseFrontier}, []model.ProtocolPhase{model.PhaseFrontier}, []AuthorityClass{AuthorityHuman}, "approval", allGoals, 44},
		{"delivery.slice.advance", EventOwnedLocal, []model.ProtocolPhase{model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive, model.PhaseTerminal}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "delivery-state", allGoals, 68},

		{"publication.preview", EventOwnedLocal, []model.ProtocolPhase{model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive}, []AuthorityClass{AuthorityRepository}, "publication-preview", []model.GoalKind{model.GoalOpenPR, model.GoalMerged}, 72},
		{"publication.execute", EventOwnedExternal, []model.ProtocolPhase{model.PhaseActive}, []model.ProtocolPhase{model.PhaseActive, model.PhaseRecovery}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "publication", []model.GoalKind{model.GoalOpenPR, model.GoalMerged}, 76},
		{"publication.observe", EventOwnedLocal, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseRecovery, model.PhaseUnresolved}, []model.ProtocolPhase{model.PhaseActive, model.PhaseTerminal, model.PhaseFrontier, model.PhaseUnresolved}, []AuthorityClass{AuthorityRepository}, "publication-evidence", []model.GoalKind{model.GoalOpenPR, model.GoalMerged}, 77},
		{"publication.reconcile", EventRecovery, []model.ProtocolPhase{model.PhaseRecovery, model.PhaseUnresolved}, []model.ProtocolPhase{model.PhaseActive, model.PhaseTerminal, model.PhaseFrontier, model.PhaseUnresolved}, []AuthorityClass{AuthorityHuman, AuthorityProvider}, "publication", []model.GoalKind{model.GoalOpenPR, model.GoalMerged}, 1},
		{"publication.correct", EventOwnedExternal, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseTerminal}, []model.ProtocolPhase{model.PhaseActive, model.PhaseRecovery}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy}, "publication", []model.GoalKind{model.GoalOpenPR, model.GoalMerged}, 80},
		{"publication.abandon", EventAuthority, []model.ProtocolPhase{model.PhaseActive, model.PhaseFrontier}, []model.ProtocolPhase{model.PhaseAbandoned}, []AuthorityClass{AuthorityHuman}, "publication", []model.GoalKind{model.GoalAbandoned}, 93},

		{"recovery.resume", EventRecovery, []model.ProtocolPhase{model.PhaseRecovery}, []model.ProtocolPhase{model.PhaseDormant, model.PhaseObserved, model.PhaseActive, model.PhaseFrontier, model.PhaseTerminal, model.PhaseAbandoned}, []AuthorityClass{AuthorityHuman, AuthorityAutonomy, AuthorityRepository}, "recovery-journal", allGoals, 2},
		{"recovery.rollback", EventRecovery, []model.ProtocolPhase{model.PhaseRecovery}, []model.ProtocolPhase{model.PhaseDormant, model.PhaseObserved, model.PhaseActive, model.PhaseFrontier, model.PhaseTerminal, model.PhaseAbandoned}, []AuthorityClass{AuthorityHuman, AuthorityRepository}, "recovery-journal", allGoals, 3},
		{"recovery.escalate", EventRecovery, []model.ProtocolPhase{model.PhaseRecovery, model.PhaseUnresolved}, []model.ProtocolPhase{model.PhaseFrontier}, []AuthorityClass{AuthorityRepository}, "recovery-journal", allGoals, 5},

		{"external.files-changed", EventObservedExternal, managedSources, []model.ProtocolPhase{model.PhaseObserved}, []AuthorityClass{AuthorityNone}, "", allGoals, 100},
		{"external.head-changed", EventObservedExternal, managedSources, []model.ProtocolPhase{model.PhaseObserved}, []AuthorityClass{AuthorityNone}, "", allGoals, 100},
		{"external.branch-changed", EventObservedExternal, managedSources, []model.ProtocolPhase{model.PhaseObserved}, []AuthorityClass{AuthorityNone}, "", allGoals, 100},
		{"external.runtime-disappeared", EventObservedExternal, managedSources, []model.ProtocolPhase{model.PhaseObserved, model.PhaseRecovery}, []AuthorityClass{AuthorityNone}, "", allGoals, 100},
		{"external.configuration-drifted", EventObservedExternal, managedSources, []model.ProtocolPhase{model.PhaseObserved, model.PhaseUnresolved}, []AuthorityClass{AuthorityNone}, "", allGoals, 100},
		{"external.lease-expired", EventObservedExternal, managedSources, []model.ProtocolPhase{model.PhaseDormant, model.PhaseFrontier}, []AuthorityClass{AuthorityNone}, "", allGoals, 100},
		{"external.host-interrupted", EventObservedExternal, managedSources, []model.ProtocolPhase{model.PhaseRecovery}, []AuthorityClass{AuthorityNone}, "", allGoals, 100},
		{"external.ci-completed", EventObservedExternal, managedSources, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseTerminal}, []AuthorityClass{AuthorityNone}, "", allGoals, 100},
		{"external.pr-opened", EventObservedExternal, managedSources, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseTerminal}, []AuthorityClass{AuthorityNone}, "", allGoals, 100},
		{"external.pr-updated", EventObservedExternal, managedSources, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseTerminal}, []AuthorityClass{AuthorityNone}, "", allGoals, 100},
		{"external.pr-closed", EventObservedExternal, managedSources, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseFrontier}, []AuthorityClass{AuthorityNone}, "", allGoals, 100},
		{"external.pr-merged", EventObservedExternal, managedSources, []model.ProtocolPhase{model.PhaseObserved, model.PhaseActive, model.PhaseTerminal}, []AuthorityClass{AuthorityNone}, "", allGoals, 100},
		{"external.provider-unavailable", EventObservedExternal, managedSources, []model.ProtocolPhase{model.PhaseUnresolved, model.PhaseRecovery}, []AuthorityClass{AuthorityNone}, "", allGoals, 100},
	}

	transitions := make([]Transition, 0, len(seeds))
	for _, item := range seeds {
		transitions = append(transitions, materialize(item))
	}
	return transitions
}

func materialize(item seed) Transition {
	controllable := item.class.Controllable()
	effect := EffectID("")
	var localEffects, externalEffects []EffectID
	resources := []string(nil)
	reversibility := ObservationOnly
	recovery := TransitionID("")
	points := []string{"after-observation"}
	if controllable {
		effect = EffectID(item.id)
		resources = []string{item.resource}
		reversibility = Reversible
		points = []string{"after-lock", "after-stage", "after-effect", "before-receipt"}
		if item.class == EventOwnedExternal {
			externalEffects = []EffectID{effect}
			reversibility = Compensatable
			points = []string{"before-request", "after-request", "before-settlement-observation", "before-receipt"}
		} else {
			localEffects = []EffectID{effect}
		}
		if item.class == EventRecovery {
			recovery = "recovery.escalate"
		} else {
			recovery = interruptionRecovery(item)
		}
	}
	transition := Transition{
		ID: item.id, Version: 1, Class: item.class, SourcePhases: item.source, TargetPhases: item.target,
		GoalKinds: item.goals, RequiredIdentity: invocationIdentityRequirements(), Authority: item.authority,
		RequiredEvidence: []string{"invocation-context", "snapshot-fingerprint", "goal"},
		OwnedResources:   resources, Effect: effect, Idempotent: controllable,
		LocalEffects: localEffects, ExternalEffects: externalEffects,
		Prescription:    Prescription{Operation: string(item.id), ExpectedPostcondition: "predicate:target-phase:" + string(item.id)},
		SourcePredicate: "predicate:source-phase:" + string(item.id), AdmissionPredicate: "predicate:exact-admission:" + string(item.id), TargetPredicate: "predicate:target-phase:" + string(item.id),
		Verifier:      "verifier:fresh-observation:" + string(item.id),
		Interruption:  interruptionContract(item, points, recovery),
		Reversibility: reversibility, TerminalEffect: terminalEffect(item.id), PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt",
		CostClass: "declared-neutral", Priority: item.priority,
	}
	transition.SourceConditions = sourceConditions(item.id)
	if item.class.Controllable() && item.class != EventRecovery && !isSafeAbandonment(item.id) {
		transition.SourceConditions = append(transition.SourceConditions,
			known(model.FacetRecovery, string(model.RecoveryNone)),
			known(model.FacetTransaction, string(model.TransactionNone)),
		)
	}
	if item.class.Controllable() && item.class != EventRecovery && item.id != "goal.configure" {
		terminalValues := []string{string(model.TerminalNonterminal)}
		if allowsStaleTerminalRepair(item.id) {
			terminalValues = append(terminalValues, string(model.TerminalStale))
		}
		if allowsPostTerminalMaintenance(item.id) {
			terminalValues = append(terminalValues, string(model.TerminalEstablished))
		}
		transition.SourceConditions = append(transition.SourceConditions, known(model.FacetTerminal, terminalValues...))
	}
	if requiresEngagement(item.id) {
		transition.SourceConditions = append(transition.SourceConditions, known(model.FacetEngagement, string(model.EngagementCommand), string(model.EngagementActive)))
		transition.SourceConditions = append(transition.SourceConditions, known(model.FacetGoal))
	}
	if requiresHealthyConfiguration(item.id) {
		transition.SourceConditions = append(transition.SourceConditions, known(model.FacetConfiguration, string(model.ConfigurationVerified)))
	}
	if requiresHealthyRuntime(item.id) {
		transition.SourceConditions = append(transition.SourceConditions, known(model.FacetRuntime, string(model.RuntimeVerified)))
	}
	if usesConfigurationPolicy(item.id) {
		transition.SourceConditions = append(transition.SourceConditions, known(model.FacetConfigurationPolicy))
	}
	transition.TargetConditions = targetConditions(item.id)
	transition.AuthorityAll = requiredAuthorities(item.id)
	for _, condition := range transition.SourceConditions {
		transition.RequiredEvidence = append(transition.RequiredEvidence, "facet:"+string(condition.Facet))
	}
	transition.Parameters = parameterSpecs(item.id)
	if item.id == "repository.attach" || item.id == "repository.detach" || item.id == "invocation.rebind" {
		transition.AllowsIdentityRebind = true
	}
	if item.id == "workspace.cut" || item.id == "workspace.cleanup" || item.id == "workspace.reap" || item.id == "workspace.reconcile" {
		transition.AllowsWorktreeTransfer = true
	}
	return transition
}

func isSafeAbandonment(id TransitionID) bool {
	return id == "goal.configure" || id == "plan.abandon" || id == "publication.abandon" || id == "workspace.abandon"
}

func invocationIdentityRequirements() []string {
	return []string{"repository-id", "git-common-id", "worktree-id", "ref", "controller-id", "invoking-path", "runtime-path", "runtime-fingerprint", "topology", "host", "correlation-id"}
}

func interruptionContract(item seed, points []string, recovery TransitionID) InterruptionContract {
	contract := InterruptionContract{
		Points: points, PartialState: []string{"no-owned-partial-state"}, Detection: "fresh-canonical-observation",
		ResumeContract: "not-applicable", RollbackContract: "not-applicable", CompensationContract: "not-applicable",
		Recovery: recovery, RecoveryAuthority: "not-applicable", ResumptionPredicate: "predicate:target-phase:" + string(item.id),
	}
	if !item.class.Controllable() {
		return contract
	}
	contract.PartialState = []string{"journal-begun", "effect-staged", "effect-possibly-installed", "postcondition-unreceipted"}
	contract.Detection = "pending-journal-plus-fresh-canonical-observation"
	contract.ResumeContract = "journal-target-replay-when-permitted"
	contract.RollbackContract = "exact-prior-byte-replay-when-permitted"
	contract.CompensationContract = "not-required-for-owned-local-effects"
	contract.RecoveryAuthority = "declared-by:" + string(recovery)
	contract.ResumptionPredicate = "recovery-contract-for:" + string(item.id)
	if item.class == EventOwnedExternal {
		contract.PartialState = []string{"request-not-sent", "request-possibly-accepted", "provider-settlement-unobserved"}
		contract.Detection = "pending-journal-plus-fresh-provider-observation"
		contract.ResumeContract = "forbidden-without-provider-observation"
		contract.RollbackContract = "not-provable-after-external-request"
		contract.CompensationContract = "provider-reconciliation-only"
	}
	if item.id == "workspace.cleanup" || item.id == "workspace.reap" {
		contract.ResumeContract = "forbidden-after-destructive-git-effect"
		contract.RollbackContract = "not-guaranteed-after-worktree-removal"
		contract.CompensationContract = "no-generic-compensation"
	}
	if item.class == EventRecovery {
		contract.ResumeContract = "never-blindly-retry-interrupted-recovery"
		contract.RollbackContract = "preserve-original-transaction-group"
		contract.CompensationContract = "escalation-only-after-nested-interruption"
	}
	return contract
}

func terminalEffect(id TransitionID) string {
	value := string(id)
	if strings.HasPrefix(value, "gate.") || id == "evidence.visual.attach" || id == "plan.approve" || id == "plan.approve-amendment" ||
		id == "runtime.hydrate" || id == "runtime.replace" || id == "runtime.reconcile" || id == "configuration.initialize" ||
		id == "configuration.mutate" || id == "configuration.reconcile" || id == "installation.update" ||
		id == "publication.observe" || id == "publication.reconcile" || id == "workspace.cleanup" || id == "workspace.reap" {
		return "may-establish-configured-goal-after-fresh-observation"
	}
	if id == "plan.abandon" || id == "publication.abandon" || id == "workspace.abandon" {
		return "may-establish-safe-abandonment"
	}
	return "none"
}

func interruptionRecovery(item seed) TransitionID {
	if item.class == EventOwnedExternal {
		return "publication.reconcile"
	}
	switch item.id {
	case "runtime.hydrate", "runtime.replace", "installation.initialize", "installation.update":
		return "runtime.reconcile"
	case "configuration.initialize", "configuration.mutate":
		return "configuration.reconcile"
	case "workspace.cut":
		return "workspace.reconcile"
	case "workspace.cleanup", "workspace.reap":
		return "recovery.escalate"
	default:
		return "recovery.resume"
	}
}

func allowsStaleTerminalRepair(id TransitionID) bool {
	value := string(id)
	return strings.HasPrefix(value, "gate.") || id == "evidence.visual.attach" ||
		id == "plan.create" || id == "plan.validate" || id == "plan.amend" || id == "plan.invalidate" ||
		id == "runtime.hydrate" || id == "runtime.replace" || id == "installation.update" ||
		id == "configuration.initialize" || id == "configuration.mutate" || id == "publication.correct"
}

func allowsPostTerminalMaintenance(id TransitionID) bool {
	switch id {
	case "workspace.cleanup", "workspace.reap", "publication.correct":
		return true
	default:
		return false
	}
}

func parameterSpecs(id TransitionID) []ParameterSpec {
	required := func(names ...string) []ParameterSpec {
		result := make([]ParameterSpec, 0, len(names))
		for _, name := range names {
			result = append(result, ParameterSpec{Name: name, Required: true})
		}
		return result
	}
	switch id {
	case "repository.attach":
		return required("topology", "config_authority")
	case "runtime.hydrate", "runtime.replace", "installation.update":
		return required("source_revision", "runtime_path", "runtime_sha256")
	case "runtime.reconcile":
		return required("source_revision", "runtime_path", "runtime_sha256", "transaction_id")
	case "installation.initialize":
		return required("source_revision", "runtime_path", "runtime_sha256", "config_path", "config_sha256")
	case "configuration.initialize", "configuration.mutate":
		return required("config_path", "config_sha256")
	case "configuration.reconcile", "workspace.reconcile":
		return required("transaction_id")
	case "goal.configure":
		return required("goal_kind", "delivery_id")
	case "plan.create", "plan.amend":
		return required("source_path", "delivery_id")
	case "plan.approve", "plan.approve-amendment":
		return required("plan_fingerprint", "actor")
	case "workspace.cut":
		return required("branch", "base_ref", "destination")
	case "workspace.sync", "workspace.activate", "workspace.publish", "workspace.cleanup", "workspace.reap", "workspace.abandon":
		return required("branch")
	case "gate.build.record", "gate.test.record", "gate.review.record", "gate.change.record", "gate.journey.record":
		return required("source_revision", "evidence_path", "evidence_fingerprint")
	case "evidence.visual.attach":
		return required("manifest_path", "privacy_receipt", "source_revision")
	case "delivery.slice.advance":
		return required("slice_id", "source_revision")
	case "publication.preview":
		return required("base_ref", "head_ref", "body_path")
	case "publication.execute":
		return required("preview_fingerprint")
	case "publication.observe":
		return required("publication_id")
	case "publication.reconcile":
		return required("publication_id", "transaction_id")
	case "publication.correct":
		return required("publication_id", "body_path", "body_sha256")
	case "recovery.resume", "recovery.rollback", "recovery.escalate":
		return required("transaction_id")
	default:
		return nil
	}
}

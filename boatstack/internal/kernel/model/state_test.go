package model

import (
	"path/filepath"
	"testing"
	"time"
)

func testAbsolutePath(parts ...string) string {
	path, err := filepath.Abs(filepath.Join(parts...))
	if err != nil {
		panic(err)
	}
	return path
}

func testEvidence() Evidence {
	return Evidence{Source: "fixture", Fingerprint: "sha256:fixture", Revision: "deadbeef", ObservedAt: time.Unix(100, 0).UTC()}
}

func testObservation(phase ProtocolPhase) Observation {
	evidence := testEvidence()
	return Observation{
		SchemaVersion: SnapshotSchemaVersion,
		Invocation: InvocationContext{
			RepositoryID: "repo-1", GitCommonID: "git-1", WorktreeID: "worktree-1", Ref: "refs/heads/feature",
			ControllerID: "controller-1", InvokingPath: testAbsolutePath("test-fixture", "repo"), RuntimeVersion: "runtime-version", RuntimePath: testAbsolutePath("test-fixture", "runtime", "boatstack"), RuntimeFingerprint: "runtime-fingerprint",
			Topology: TopologyEmbedded, Host: "cli", Correlation: "corr-1",
		},
		Phase: Known(phase, evidence), Engagement: Known(EngagementActive, evidence), Delivery: Known(DeliveryActive, evidence),
		Workspace: Known(WorkspaceActive, evidence), Plan: Known(PlanApproved, evidence),
		Configuration: Known(ConfigurationVerified, evidence), Runtime: Known(RuntimeVerified, evidence),
		ConfigurationPolicy: Known(ConfigurationPolicy{PlanApproval: "human", VisualEvidence: "optional", ExternalEffectAuthority: "human-or-autonomy-plus-provider", Hosts: []string{"cli"}}, evidence),
		Publication:         Known(PublicationNone, evidence), Verification: Known(VerificationUnverified, evidence),
		Recovery: Known(RecoveryNone, evidence), Transaction: Known(TransactionNone, evidence),
		RecoveryInfo: Absent[RecoveryContext]("none", evidence), TransactionInfo: Absent[TransactionContext]("none", evidence),
		Terminal: Known(TerminalNonterminal, evidence), Goal: Absent[Goal]("not configured", evidence), ObservedAt: time.Unix(100, 0).UTC(),
	}
}

func TestCanonicalizeIsDeterministicAndControlSensitive(t *testing.T) {
	// control-law: canonical-snapshot-is-the-only-state-authority
	one, err := Canonicalize(testObservation(PhaseObserved))
	if err != nil {
		t.Fatal(err)
	}
	two, err := Canonicalize(testObservation(PhaseObserved))
	if err != nil {
		t.Fatal(err)
	}
	if one.Fingerprint != two.Fingerprint {
		t.Fatalf("same observation produced %q and %q", one.Fingerprint, two.Fingerprint)
	}
	changed := testObservation(PhaseActive)
	three, err := Canonicalize(changed)
	if err != nil {
		t.Fatal(err)
	}
	if one.Fingerprint == three.Fingerprint {
		t.Fatal("controlling phase change did not change snapshot fingerprint")
	}
}

func TestCanonicalizeRejectsKnownFactWithoutEvidence(t *testing.T) {
	// control-law: controlling-facts-carry-evidence
	observation := testObservation(PhaseObserved)
	observation.Publication.Evidence = nil
	if _, err := Canonicalize(observation); err == nil {
		t.Fatal("known publication without evidence was accepted")
	}
}

func TestInvocationContextRejectsPathOnlyEffectIdentity(t *testing.T) {
	// control-law: effectful-identity-is-explicit-and-injective
	context := testObservation(PhaseObserved).Invocation
	context.WorktreeID = ""
	if err := context.Validate(true); err == nil {
		t.Fatal("effectful invocation without worktree identity was accepted")
	}
}

func TestInvocationContextRejectsEffectWithoutExactRuntimeIdentity(t *testing.T) {
	context := testObservation(PhaseObserved).Invocation
	context.RuntimeFingerprint = ""
	if err := context.Validate(true); err == nil {
		t.Fatal("effectful invocation without exact executing runtime was accepted")
	}
}

func TestCanonicalizeRejectsEstablishedTerminalInActivePhase(t *testing.T) {
	// control-law: terminal-is-an-evidence-backed-goal-state
	observation := testObservation(PhaseActive)
	observation.Terminal.Value = TerminalEstablished
	if _, err := Canonicalize(observation); err == nil {
		t.Fatal("established terminal evidence in active phase was accepted")
	}
}

func TestEveryControllingFacetChangesCanonicalIdentity(t *testing.T) {
	// control-law: snapshot-fingerprint-covers-every-admissibility-input
	baseObservation := testObservation(PhaseObserved)
	base, err := Canonicalize(baseObservation)
	if err != nil {
		t.Fatal(err)
	}
	evidence := testEvidence()
	mutations := map[FacetName]func(*Observation){
		FacetPhase:         func(o *Observation) { o.Phase = Known(PhaseActive, evidence) },
		FacetProgram:       func(o *Observation) { o.Program = Known(ProgramCurrent, evidence) },
		FacetTopology:      func(o *Observation) { o.Invocation.Topology = TopologyDetached },
		FacetEngagement:    func(o *Observation) { o.Engagement = Known(EngagementCommand, evidence) },
		FacetDelivery:      func(o *Observation) { o.Delivery = Known(DeliveryApproved, evidence) },
		FacetWorkspace:     func(o *Observation) { o.Workspace = Known(WorkspaceCut, evidence) },
		FacetPlan:          func(o *Observation) { o.Plan = Known(PlanValid, evidence) },
		FacetConfiguration: func(o *Observation) { o.Configuration = Known(ConfigurationStale, evidence) },
		FacetConfigurationPolicy: func(o *Observation) {
			o.ConfigurationPolicy = Known(ConfigurationPolicy{PlanApproval: "human-or-autonomy", VisualEvidence: "required", ExternalEffectAuthority: "human-or-autonomy-plus-provider", Hosts: []string{"cli"}}, evidence)
		},
		FacetRuntime:      func(o *Observation) { o.Runtime = Known(RuntimeStale, evidence) },
		FacetPublication:  func(o *Observation) { o.Publication = Known(PublicationOpen, evidence) },
		FacetVerification: func(o *Observation) { o.Verification = Known(VerificationCurrent, evidence) },
		FacetRecovery: func(o *Observation) {
			o.Phase = Known(PhaseRecovery, evidence)
			o.Recovery = Known(RecoveryReconcile, evidence)
			o.Transaction = Known(TransactionExternalUncertain, evidence)
			o.RecoveryInfo = Known(RecoveryContext{TransactionID: "adm-1", Cause: "interrupted", SourcePhase: PhaseActive, Permitted: []string{"recovery.rollback"}, BudgetRemaining: 1, Resumption: PhaseActive}, evidence)
			o.TransactionInfo = Known(TransactionContext{ID: "adm-1", TransitionID: "plan.create", Status: "recovery-required"}, evidence)
		},
		FacetTransaction: func(o *Observation) {
			o.Transaction = Known(TransactionStaged, evidence)
			o.TransactionInfo = Known(TransactionContext{ID: "adm-2", TransitionID: "plan.create", Status: "staged"}, evidence)
		},
		FacetRecoveryInfo: func(o *Observation) { o.RecoveryInfo = Unknown[RecoveryContext](FactStale, "stale context", evidence) },
		FacetTransactionInfo: func(o *Observation) {
			o.TransactionInfo = Unknown[TransactionContext](FactStale, "stale context", evidence)
		},
		FacetTerminal: func(o *Observation) { o.Terminal = Known(TerminalStale, evidence) },
		FacetGoal: func(o *Observation) {
			o.Goal = Known(Goal{ID: "goal", Kind: GoalVerified, DeliveryID: "delivery"}, evidence)
		},
	}
	for _, facet := range ControllingFacets() {
		mutate, ok := mutations[facet]
		if !ok {
			t.Errorf("facet %s has no fingerprint fixture", facet)
			continue
		}
		changedObservation := baseObservation
		mutate(&changedObservation)
		changed, err := Canonicalize(changedObservation)
		if err != nil {
			t.Fatalf("facet %s: %v", facet, err)
		}
		if changed.Fingerprint == base.Fingerprint {
			t.Errorf("facet %s did not change canonical identity", facet)
		}
	}
}

func TestProtocolPhaseProjectionIsOrderedAndDefensive(t *testing.T) {
	phases := ProtocolPhases()
	if len(phases) != 13 || phases[0] != PhaseDormant || phases[len(phases)-1] != PhaseAbandoned {
		t.Fatalf("protocol phases = %v", phases)
	}
	phases[0] = PhaseTerminal
	if ProtocolPhases()[0] != PhaseDormant {
		t.Fatal("ProtocolPhases exposed mutable kernel state")
	}
	for _, phase := range ProtocolPhases() {
		wantMarked := phase == PhaseFrontier || phase == PhaseTerminal || phase == PhaseAbandoned
		if phase.IsCompletionTarget() != wantMarked {
			t.Errorf("completion target for %s = %v, want %v", phase, phase.IsCompletionTarget(), wantMarked)
		}
	}
}

package engine

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
)

type fixedClock struct{ now time.Time }

const syntheticProgramFingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

var syntheticProgram = protocol.ProgramIdentity{ID: "test.synthetic", Version: "1.0.0", Fingerprint: syntheticProgramFingerprint}

func syntheticObjectiveContracts(t *testing.T) catalog.ObjectiveContracts {
	t.Helper()
	contracts, err := catalog.NewObjectiveContracts([]catalog.ObjectiveContract{{
		TargetID:   model.ObjectiveVerified,
		Conditions: []catalog.FacetCondition{{Facet: model.FacetName("test.synthetic.stage"), Statuses: []model.FactStatus{model.FactKnown}, Values: []string{"terminal"}}},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return contracts
}

func (c fixedClock) Now() time.Time { return c.now }

func fixtureAbsolutePath(parts ...string) string {
	path, err := filepath.Abs(filepath.Join(parts...))
	if err != nil {
		panic(err)
	}
	return path
}

type sequenceObserver struct {
	mu    sync.Mutex
	items []model.Observation
}

func (o *sequenceObserver) Observe(context.Context, ports.ObservationRequest) (model.Observation, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.items) == 0 {
		return model.Observation{}, errors.New("unexpected observation")
	}
	item := o.items[0]
	o.items = o.items[1:]
	return item, nil
}

type failingObserver struct{ err error }

func (o failingObserver) Observe(context.Context, ports.ObservationRequest) (model.Observation, error) {
	return model.Observation{}, o.err
}

type fakeLock struct{ released bool }

func (l *fakeLock) Release() error { l.released = true; return nil }

type fakeLocker struct{ lock *fakeLock }

func (l fakeLocker) Acquire(context.Context, model.InvocationContext, []string) (ports.Lock, error) {
	return l.lock, nil
}

type fakeJournal struct {
	begun, committed, aborted, recovery int
	failMark                            string
	commitErr                           error
}

func (j *fakeJournal) Begin(context.Context, protocol.Admission, catalog.Transition) error {
	j.begun++
	return nil
}
func (j *fakeJournal) Stage(context.Context, string, []ports.ResourceMutation) error { return nil }
func (j *fakeJournal) Mark(_ context.Context, _ string, status string) error {
	if status == j.failMark {
		return errors.New("injected journal mark failure")
	}
	return nil
}
func (j *fakeJournal) Commit(context.Context, protocol.TransitionReceipt) error {
	if j.commitErr != nil {
		return j.commitErr
	}
	j.committed++
	return nil
}
func (j *fakeJournal) Abort(context.Context, string, string) error { j.aborted++; return nil }
func (j *fakeJournal) RequireRecovery(context.Context, string, string) error {
	j.recovery++
	return nil
}

type fakeEffects struct {
	executions, rollbacks int
	result                ports.EffectResult
	err                   error
	prepareErr            error
	transition            catalog.Transition
}

func (e *fakeEffects) Prepare(_ context.Context, _ protocol.Admission, transition catalog.Transition) (ports.PreparedEffect, error) {
	if e.prepareErr != nil {
		return nil, e.prepareErr
	}
	e.transition = transition
	return e, nil
}
func (e *fakeEffects) Manifest() []ports.ResourceMutation { return nil }
func (e *fakeEffects) ChangedStateFacets() []model.StateFacet {
	return []model.StateFacet{model.StateFacetControl}
}
func (e *fakeEffects) CommittedEffects() []protocol.EffectFact {
	return []protocol.EffectFact{{
		Kind: protocol.EffectResourceMutation, EffectID: e.transition.Effect, Owner: e.transition.Owner, Resource: e.transition.OwnedResources[0],
		Target: "/test/state.json", Operation: "update", PriorFingerprint: strings.Repeat("1", 64), ResultingFingerprint: strings.Repeat("2", 64),
	}}
}
func (e *fakeEffects) VerificationInvocation() (model.InvocationContext, bool) {
	return model.InvocationContext{}, false
}
func (e *fakeEffects) Execute(context.Context) (ports.EffectResult, error) {
	e.executions++
	return e.result, e.err
}
func (e *fakeEffects) Rollback(context.Context) error {
	e.rollbacks++
	return nil
}

type memoryReceipts struct {
	next       uint64
	values     []protocol.TransitionReceipt
	projectErr error
}

func (s *memoryReceipts) Bind(context.Context, string, protocol.Admission) error { return nil }
func (s *memoryReceipts) Unbind(string)                                          {}

func (s *memoryReceipts) NextSequence(context.Context, string) (uint64, error) {
	s.next++
	return s.next, nil
}
func (s *memoryReceipts) FindByIdempotency(_ context.Context, _ model.InvocationContext, key string) (protocol.TransitionReceipt, bool, error) {
	for _, receipt := range s.values {
		if receipt.IdempotencyKey == key {
			return receipt, true, nil
		}
	}
	return protocol.TransitionReceipt{}, false, nil
}
func (s *memoryReceipts) Project(_ context.Context, receipt protocol.TransitionReceipt) error {
	if s.projectErr != nil {
		return s.projectErr
	}
	s.values = append(s.values, receipt)
	return nil
}

func observation(phase model.ProtocolPhase, fingerprint string) model.Observation {
	e := model.Evidence{Source: "fixture", Fingerprint: fingerprint, ObservedAt: time.Unix(20, 0).UTC()}
	configurationEvidence := model.Evidence{Source: "configuration:/repo/.boatstack/project.json", Fingerprint: "config-fingerprint", ObservedAt: time.Unix(20, 0).UTC()}
	stage := "start"
	revision := uint64(1)
	if phase == model.PhaseActive {
		stage = "terminal"
		revision = 2
	} else if phase == model.PhaseRecovery {
		stage = "verify"
	}
	return model.Observation{
		SchemaVersion: model.SnapshotSchemaVersion, StateRevision: revision,
		Invocation: model.InvocationContext{RepositoryID: "repo", GitCommonID: "git", WorktreeID: "wt", Ref: "refs/heads/f", ControllerID: "ctl", InvokingPath: fixtureAbsolutePath("test-fixture", "repo"), RuntimeVersion: "runtime-version", RuntimePath: fixtureAbsolutePath("test-fixture", "runtime"), RuntimeFingerprint: "runtime", Topology: model.TopologyEmbedded, Host: "cli", Correlation: "corr"},
		Phase:      model.Known(phase, e), Engagement: model.Known(model.EngagementActive, e), Delivery: model.Known(model.DeliveryActive, e), Workspace: model.Known(model.WorkspaceActive, e),
		Plan: model.Known(model.PlanApproved, e), Configuration: model.Known(model.ConfigurationVerified, configurationEvidence), Runtime: model.Known(model.RuntimeVerified, e),
		ConfigurationPolicy: model.Known(model.ConfigurationPolicy{PlanApproval: "human", VisualEvidence: "optional", ExternalEffectAuthority: "human-or-autonomy-plus-provider", Hosts: []string{"cli"}}, configurationEvidence),
		Publication:         model.Known(model.PublicationNone, e), Verification: model.Known(model.VerificationUnverified, e), Recovery: model.Known(model.RecoveryNone, e),
		Transaction: model.Known(model.TransactionNone, e), RecoveryInfo: model.Absent[model.RecoveryContext]("none", e), TransactionInfo: model.Absent[model.TransactionContext]("none", e),
		Terminal: model.Known(model.TerminalNonterminal, e), Objective: model.Known(model.Objective{ID: "objective", TargetID: model.ObjectiveVerified, DeliveryID: "delivery"}, e), ObservedAt: time.Unix(20, 0).UTC(),
		ProgramFacts: map[string]model.Fact[string]{"test.synthetic.stage": model.Known(stage, e)},
	}
}

func recoveryObservation(fingerprint string) model.Observation {
	value := observation(model.PhaseRecovery, fingerprint)
	value.StateRevision = 2
	evidence := value.Phase.Evidence[0]
	value.Recovery = model.Known(model.RecoveryReconcile, evidence)
	value.Transaction = model.Known(model.TransactionLocalApplied, evidence)
	value.RecoveryInfo = model.Known(model.RecoveryContext{
		TransactionID: "adm-pending", Cause: "receipt commit interrupted", SourcePhase: model.PhaseObserved,
		Permitted: []string{"recovery.escalate"}, BudgetRemaining: 1, Resumption: model.PhaseObserved,
	}, evidence)
	value.TransactionInfo = model.Known(model.TransactionContext{ID: "adm-pending", TransitionID: "test.advance", Status: "recovery-required"}, evidence)
	value.Terminal = model.Known(model.TerminalStale, evidence)
	return value
}

func testRegistry(t *testing.T) catalog.Registry {
	return testRegistryWithAdvanceClass(t, catalog.EventOwnedLocal)
}

func testRegistryWithAdvanceClass(t *testing.T, class catalog.EventClass) catalog.Registry {
	t.Helper()
	identity := []string{"repository-id", "git-common-id", "worktree-id"}
	interruption := func(recovery catalog.TransitionID) catalog.InterruptionContract {
		return catalog.InterruptionContract{
			Points: []string{"after-effect"}, PartialState: []string{"effect-possibly-installed"}, Detection: "test-observation",
			ResumeContract: "test-resume", RollbackContract: "test-rollback", CompensationContract: "not-required",
			Recovery: recovery, RecoveryAuthority: "test-authority", ResumptionPredicate: "test-resumption",
		}
	}
	authority := []catalog.AuthorityClass{catalog.AuthorityRepository}
	localEffects := []catalog.EffectID{"test.advance"}
	var externalEffects []catalog.EffectID
	if class == catalog.EventOwnedExternal {
		authority = []catalog.AuthorityClass{catalog.AuthorityHuman}
		localEffects = nil
		externalEffects = []catalog.EffectID{"test.advance"}
	}
	activePhase := string(model.PhaseActive)
	frontierPhase := string(model.PhaseFrontier)
	escalatedRecovery := string(model.RecoveryEscalated)
	r, err := catalog.New([]catalog.Transition{{
		ID: "test.advance", Version: 1, Class: class,
		Origin: catalog.TransitionOrigin{Kind: catalog.OriginControlProgram, ID: "test.synthetic", Version: "1.0.0", ManifestFingerprint: syntheticProgramFingerprint}, Owner: "test.synthetic", SelectionClass: catalog.SelectionProgramProgress,
		SourcePhases: []model.ProtocolPhase{model.PhaseObserved}, TargetPhases: []model.ProtocolPhase{model.PhaseActive},
		RequiredIdentity: identity, Authority: authority, RequiredCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite}, DeclaredCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite, catalog.CapabilityCommandExecute}, RequiredEvidence: []string{"snapshot"}, OwnedResources: []string{"state"}, OwnedFacets: []model.StateFacet{model.StateFacetControl}, StateEffect: catalog.StateEffect{Kind: catalog.StateEffectAssignments, Assignments: []catalog.StateAssignment{{Facet: "phase", Value: &activePhase}}}, Effect: "test.advance", LocalEffects: localEffects, ExternalEffects: externalEffects, Idempotent: true,
		Prescription: catalog.Prescription{Operation: "test.advance", ExpectedPostcondition: "active"}, SourcePredicate: "observed", AdmissionPredicate: "exact-admission", TargetPredicate: "active", Verifier: "fresh-active",
		SourceConditions: []catalog.FacetCondition{{Facet: model.FacetName("test.synthetic.stage"), Statuses: []model.FactStatus{model.FactKnown}, Values: []string{"start"}}},
		TargetConditions: []catalog.FacetCondition{{Facet: model.FacetName("test.synthetic.stage"), Statuses: []model.FactStatus{model.FactKnown}, Values: []string{"terminal"}}},
		Interruption:     interruption("test.recover"), Reversibility: catalog.Reversible, TerminalEffect: "none",
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt", CostClass: "test", Policy: catalog.PolicyContract{ObjectiveScope: catalog.ObjectiveScopeBoundExact}, Priority: 1,
	}, {
		ID: "test.recover", Version: 1, Class: catalog.EventRecovery,
		Origin: catalog.TransitionOrigin{Kind: catalog.OriginControlProgram, ID: "test.synthetic", Version: "1.0.0", ManifestFingerprint: syntheticProgramFingerprint}, Owner: "test.synthetic", SelectionClass: catalog.SelectionProgramRecovery,
		SourcePhases: []model.ProtocolPhase{model.PhaseRecovery}, TargetPhases: []model.ProtocolPhase{model.PhaseFrontier},
		RequiredIdentity: identity, Authority: []catalog.AuthorityClass{catalog.AuthorityRepository}, RequiredCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite}, DeclaredCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite, catalog.CapabilityCommandExecute}, RequiredEvidence: []string{"snapshot"}, OwnedResources: []string{"state"}, OwnedFacets: []model.StateFacet{model.StateFacetControl}, StateEffect: catalog.StateEffect{Kind: catalog.StateEffectAssignments, Assignments: []catalog.StateAssignment{{Facet: "phase", Value: &frontierPhase}, {Facet: "recovery", Value: &escalatedRecovery}}}, Effect: "test.recover", LocalEffects: []catalog.EffectID{"test.recover"}, Idempotent: true,
		Prescription: catalog.Prescription{Operation: "test.recover", ExpectedPostcondition: "frontier"}, SourcePredicate: "recovery", AdmissionPredicate: "exact-recovery-admission", TargetPredicate: "frontier", Verifier: "fresh-frontier",
		SourceConditions: []catalog.FacetCondition{{Facet: model.FacetRecovery, Statuses: []model.FactStatus{model.FactKnown}, Values: []string{string(model.RecoveryReconcile)}}},
		TargetConditions: []catalog.FacetCondition{{Facet: model.FacetRecovery, Statuses: []model.FactStatus{model.FactKnown}, Values: []string{string(model.RecoveryEscalated)}}},
		Interruption:     interruption("test.recover"), Reversibility: catalog.Reversible, TerminalEffect: "none",
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt", CostClass: "test", Policy: catalog.PolicyContract{ObjectiveScope: catalog.ObjectiveScopeBoundExact}, Priority: 2,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func request(t *testing.T, now time.Time) ApplyRequest {
	t.Helper()
	invocation := observation(model.PhaseObserved, "source").Invocation
	snapshot, err := model.CanonicalizeForProgram(observation(model.PhaseObserved, "source"), syntheticProgramFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	transition, _ := testRegistry(t).Lookup("test.advance")
	authorityBundle := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{ID: "auth", Class: catalog.AuthorityRepository, Subject: "repo", Fingerprint: "config-fingerprint", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}}}
	capabilities, err := protocol.ProjectCapabilities(snapshot, transition, authorityBundle, now)
	if err != nil {
		t.Fatal(err)
	}
	prescription, err := protocol.NewPrescription(snapshot, transition, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	return ApplyRequest{ResolveRequest: ResolveRequest{
		Invocation: invocation, Objective: model.Objective{ID: "objective", TargetID: model.ObjectiveVerified, DeliveryID: "delivery"}, Requested: "test.advance",
		Authority: authorityBundle,
	}, FlowID: "flow", Prescription: prescription, AdmissionLifetime: time.Minute}
}

func TestRequiredObserverFailureReturnsTypedUnresolvedDecision(t *testing.T) {
	// control-law: required-observation-failure-is-a-typed-fail-closed-decision
	now := time.Unix(30, 0).UTC()
	journal, effects, receipts, lock := &fakeJournal{}, &fakeEffects{}, &memoryReceipts{}, &fakeLock{}
	kernel, err := New(
		testRegistry(t), syntheticObjectiveContracts(t), syntheticProgram,
		failingObserver{err: errors.New("observer unavailable")}, fixedClock{now},
		fakeLocker{lock}, journal, effects, receipts,
	)
	if err != nil {
		t.Fatal(err)
	}

	resolved, resolveErr := kernel.Resolve(context.Background(), request(t, now).ResolveRequest)
	if resolveErr == nil || !strings.Contains(resolveErr.Error(), "observer unavailable") {
		t.Fatalf("resolve error = %v, want observer failure", resolveErr)
	}
	if resolved.Decision.Kind != supervisor.DecisionUnresolved || resolved.Decision.Reason != "required observation failed" {
		t.Fatalf("resolve decision = %+v, want typed UNRESOLVED", resolved.Decision)
	}

	applied, applyErr := kernel.Apply(context.Background(), request(t, now))
	if applyErr == nil || !strings.Contains(applyErr.Error(), "observer unavailable") {
		t.Fatalf("apply error = %v, want observer failure", applyErr)
	}
	if applied.Decision.Kind != supervisor.DecisionUnresolved || applied.Decision.Reason != "required observation failed" {
		t.Fatalf("apply decision = %+v, want typed UNRESOLVED", applied.Decision)
	}
	if effects.executions != 0 || journal.begun != 0 || len(receipts.values) != 0 {
		t.Fatalf("observer failure crossed mutation boundary: effects=%d journal=%d receipts=%d", effects.executions, journal.begun, len(receipts.values))
	}
}

func TestResolutionDoesNotPrescribeBeforeRequiredParametersAreBound(t *testing.T) {
	// control-law: a selected transition is only a candidate until deterministic admission inputs are complete
	now := time.Unix(30, 0).UTC()
	transitions := testRegistry(t).All()
	for index := range transitions {
		if transitions[index].ID == "test.advance" {
			transitions[index].Parameters = []catalog.ParameterSpec{{Name: "value", Required: true}}
		}
	}
	registry, err := catalog.New(transitions)
	if err != nil {
		t.Fatal(err)
	}
	observer := &sequenceObserver{items: []model.Observation{observation(model.PhaseObserved, "source"), observation(model.PhaseObserved, "source")}}
	kernel, err := New(registry, syntheticObjectiveContracts(t), syntheticProgram, observer, fixedClock{now}, fakeLocker{&fakeLock{}}, &fakeJournal{}, &fakeEffects{}, &memoryReceipts{})
	if err != nil {
		t.Fatal(err)
	}
	req := request(t, now).ResolveRequest
	req.Requested = ""
	candidate, err := kernel.Resolve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Decision.Kind != supervisor.DecisionCandidate || candidate.Decision.Transition == nil || candidate.Decision.Transition.ID != "test.advance" {
		t.Fatalf("incomplete resolution = %+v, want CANDIDATE", candidate.Decision)
	}
	req.Requested = "test.advance"
	req.Parameters = protocol.Parameters{{Name: "value", Value: "bound"}}
	prescribed, err := kernel.Resolve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if prescribed.Decision.Kind != supervisor.DecisionPrescribed || prescribed.Decision.Transition == nil || prescribed.Decision.Transition.ID != "test.advance" {
		t.Fatalf("complete resolution = %+v, want PRESCRIBED", prescribed.Decision)
	}
}

func TestResolutionDoesNotPrescribeAnEffectThatDeterministicPreflightRejects(t *testing.T) {
	// control-law: effect preparation cannot introduce a deterministic apply-only refusal
	now := time.Unix(30, 0).UTC()
	effects := &fakeEffects{prepareErr: errors.New("malformed artifact")}
	observer := &sequenceObserver{items: []model.Observation{observation(model.PhaseObserved, "source")}}
	journal := &fakeJournal{}
	kernel, err := New(testRegistry(t), syntheticObjectiveContracts(t), syntheticProgram, observer, fixedClock{now}, fakeLocker{&fakeLock{}}, journal, effects, &memoryReceipts{})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := kernel.Resolve(context.Background(), request(t, now).ResolveRequest)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Decision.Kind != supervisor.DecisionUnresolved || resolved.Decision.Transition != nil || !strings.Contains(resolved.Decision.Reason, "malformed artifact") {
		t.Fatalf("preflight decision = %+v, want typed UNRESOLVED without prescription", resolved.Decision)
	}
	if effects.executions != 0 || journal.begun != 0 {
		t.Fatalf("preflight crossed mutation boundary: effects=%d journals=%d", effects.executions, journal.begun)
	}
}

func TestApplyCrossesAdmissionEffectVerificationAndReceiptBoundary(t *testing.T) {
	// control-law: synthetic-flow-crosses-exact-admission-and-postcondition-without-standard-flow
	now := time.Unix(30, 0).UTC()
	observer := &sequenceObserver{items: []model.Observation{observation(model.PhaseObserved, "source"), observation(model.PhaseObserved, "source"), observation(model.PhaseActive, "target"), observation(model.PhaseActive, "target")}}
	journal, effects, receipts, lock := &fakeJournal{}, &fakeEffects{result: ports.EffectResult{Settlement: ports.EffectSettled}}, &memoryReceipts{}, &fakeLock{}
	kernel, err := New(testRegistry(t), syntheticObjectiveContracts(t), syntheticProgram, observer, fixedClock{now}, fakeLocker{lock}, journal, effects, receipts)
	if err != nil {
		t.Fatal(err)
	}
	result, err := kernel.Apply(context.Background(), request(t, now))
	if err != nil {
		t.Fatal(err)
	}
	if effects.executions != 1 || effects.rollbacks != 0 || journal.committed != 1 || journal.aborted != 0 || len(receipts.values) != 1 || result.Receipt.ID == "" || !lock.released {
		t.Fatalf("unexpected boundary evidence: effects=%+v journal=%+v receipts=%d receipt=%q released=%v", effects, journal, len(receipts.values), result.Receipt.ID, lock.released)
	}
	retry := request(t, now)
	retry.IdempotencyKey = result.Admission.IdempotencyKey
	replayed, err := kernel.Apply(context.Background(), retry)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || replayed.Receipt.ID != result.Receipt.ID || effects.executions != 1 || journal.begun != 1 {
		t.Fatalf("idempotent replay crossed effect boundary: replay=%+v effects=%d journals=%d", replayed, effects.executions, journal.begun)
	}
}

func TestCommitFailureCannotProjectSuccessfulTransitionFact(t *testing.T) {
	// control-law: canonical commit precedes every passive success projection
	now := time.Unix(30, 0).UTC()
	observer := &sequenceObserver{items: []model.Observation{observation(model.PhaseObserved, "source"), observation(model.PhaseObserved, "source"), observation(model.PhaseActive, "target")}}
	journal := &fakeJournal{commitErr: errors.New("injected canonical commit failure")}
	effects, receipts := &fakeEffects{result: ports.EffectResult{Settlement: ports.EffectSettled}}, &memoryReceipts{}
	kernel, _ := New(testRegistry(t), syntheticObjectiveContracts(t), syntheticProgram, observer, fixedClock{now}, fakeLocker{&fakeLock{}}, journal, effects, receipts)
	result, err := kernel.Apply(context.Background(), request(t, now))
	if err == nil || !strings.Contains(err.Error(), "canonical commit failure") {
		t.Fatalf("error=%v, want canonical commit failure", err)
	}
	if result.Receipt.ID != "" || len(receipts.values) != 0 || journal.recovery != 1 {
		t.Fatalf("failed commit escaped as success: result=%#v projections=%d journal=%+v", result.Receipt, len(receipts.values), journal)
	}
}

func TestProjectionFailureCannotUndoCanonicalTransitionFact(t *testing.T) {
	// control-law: receipt JSONL and telemetry are projections, not commit authority
	now := time.Unix(30, 0).UTC()
	observer := &sequenceObserver{items: []model.Observation{observation(model.PhaseObserved, "source"), observation(model.PhaseObserved, "source"), observation(model.PhaseActive, "target")}}
	journal := &fakeJournal{}
	effects := &fakeEffects{result: ports.EffectResult{Settlement: ports.EffectSettled}}
	receipts := &memoryReceipts{projectErr: errors.New("projection unavailable")}
	kernel, _ := New(testRegistry(t), syntheticObjectiveContracts(t), syntheticProgram, observer, fixedClock{now}, fakeLocker{&fakeLock{}}, journal, effects, receipts)
	result, err := kernel.Apply(context.Background(), request(t, now))
	if err != nil || result.Receipt.ID == "" || journal.committed != 1 || journal.recovery != 0 || len(receipts.values) != 0 {
		t.Fatalf("passive projection changed commit result: result=%#v err=%v journal=%+v", result.Receipt, err, journal)
	}
}

func TestSyntheticStartVerifyTerminalContractNeedsNoStandardFlowFacet(t *testing.T) {
	// control-law: kernel-terminal-is-defined-only-by-the-compiled-control-program-contract
	objective := model.Objective{ID: "objective", TargetID: model.ObjectiveVerified, DeliveryID: "delivery"}
	source, err := model.CanonicalizeForProgram(observation(model.PhaseObserved, "source"), syntheticProgramFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	syntheticSupervisor := supervisor.New(testRegistry(t), syntheticObjectiveContracts(t))
	one := syntheticSupervisor.Resolve(source, objective, catalog.AuthoritySet{catalog.AuthorityRepository: true}, "")
	two := syntheticSupervisor.Resolve(source, objective, catalog.AuthoritySet{catalog.AuthorityRepository: true}, "")
	if !reflect.DeepEqual(one, two) || one.Kind != supervisor.DecisionPrescribed || one.Transition == nil || one.Transition.ID != "test.advance" {
		t.Fatalf("synthetic resolution is not deterministic: one=%+v two=%+v", one, two)
	}
	outside := syntheticSupervisor.Resolve(source, objective, catalog.AuthoritySet{catalog.AuthorityRepository: true}, "not.compiled")
	if outside.Kind != supervisor.DecisionRefused || outside.Transition != nil {
		t.Fatalf("transition outside compiled program was not refused: %+v", outside)
	}

	observed := observation(model.PhaseActive, "target")
	evidence := observed.Phase.Evidence[0]
	observed.Plan = model.Known(model.PlanAbsent, evidence)
	observed.Workspace = model.Known(model.WorkspaceAbsent, evidence)
	observed.Delivery = model.Known(model.DeliveryPlanning, evidence)
	target, err := model.CanonicalizeForProgram(observed, syntheticProgramFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	decision := syntheticSupervisor.Resolve(target, objective, nil, "")
	if decision.Kind != supervisor.DecisionTerminal {
		t.Fatalf("synthetic terminal decision = %+v", decision)
	}
	withoutContract := supervisor.New(testRegistry(t), catalog.ObjectiveContracts{}).Resolve(target, objective, nil, "")
	if withoutContract.Kind == supervisor.DecisionTerminal {
		t.Fatalf("synthetic state terminated without a compiled flow objective contract: %+v", withoutContract)
	}
	if target.Terminal.Value != model.TerminalNonterminal || target.Plan.Value != model.PlanAbsent || target.Publication.Value != model.PublicationNone {
		t.Fatalf("fixture unexpectedly relied on StandardFlow terminal state: %+v", target)
	}
}

func TestExactPermittedRecoveryRemainsReachableAcrossProgramDrift(t *testing.T) {
	// control-law: interrupted-program-update-can-rollback-under-either-program-epoch
	observed := recoveryObservation("program-change-pending")
	observed.RecordedProgramFingerprint = strings.Repeat("a", 64)
	evidence := observed.Phase.Evidence[0]
	recovery := observed.RecoveryInfo.Value
	recovery.Permitted = []string{"test.recover"}
	observed.RecoveryInfo = model.Known(recovery, evidence)
	snapshot, err := model.CanonicalizeForProgram(observed, syntheticProgramFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Program.Value != model.ProgramDrift {
		t.Fatalf("program state = %s, want drift", snapshot.Program.Value)
	}
	control := supervisor.New(testRegistry(t), syntheticObjectiveContracts(t))
	decision := control.Resolve(snapshot, snapshot.Objective.Value, catalog.AuthoritySet{catalog.AuthorityRepository: true}, "test.recover")
	if decision.Kind != supervisor.DecisionPrescribed || decision.Transition == nil || decision.Transition.ID != "test.recover" {
		t.Fatalf("permitted recovery across drift = %+v", decision)
	}
	recovery.Permitted = []string{"recovery.escalate"}
	observed.RecoveryInfo = model.Known(recovery, evidence)
	snapshot, err = model.CanonicalizeForProgram(observed, syntheticProgramFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	blocked := control.Resolve(snapshot, snapshot.Objective.Value, catalog.AuthoritySet{catalog.AuthorityRepository: true}, "test.recover")
	if blocked.Kind != supervisor.DecisionUnresolved {
		t.Fatalf("unpermitted recovery crossed program drift: %+v", blocked)
	}
}

func TestIdempotencyReceiptCannotHideUncommittedRecoveryJournal(t *testing.T) {
	// control-law: receipt-before-journal-commit-is-not-a-clean-replay
	now := time.Unix(30, 0).UTC()
	observer := &sequenceObserver{items: []model.Observation{
		observation(model.PhaseObserved, "source"), observation(model.PhaseObserved, "source"), observation(model.PhaseActive, "target"),
		recoveryObservation("recovery"),
	}}
	journal, effects, receipts, lock := &fakeJournal{}, &fakeEffects{result: ports.EffectResult{Settlement: ports.EffectSettled}}, &memoryReceipts{}, &fakeLock{}
	kernel, err := New(testRegistry(t), syntheticObjectiveContracts(t), syntheticProgram, observer, fixedClock{now}, fakeLocker{lock}, journal, effects, receipts)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := kernel.Apply(context.Background(), request(t, now))
	if err != nil {
		t.Fatal(err)
	}
	retry := request(t, now)
	retry.IdempotencyKey = completed.Admission.IdempotencyKey
	_, err = kernel.Apply(context.Background(), retry)
	var recovery ReplayRecoveryError
	if !errors.As(err, &recovery) {
		t.Fatalf("replay error=%v, want ReplayRecoveryError", err)
	}
	if effects.executions != 1 {
		t.Fatalf("recovery replay executed effect %d times", effects.executions)
	}
}

func TestApplyRejectsSnapshotDriftBeforeEffect(t *testing.T) {
	// control-law: stale-prescription-fails-before-mutation
	now := time.Unix(30, 0).UTC()
	observer := &sequenceObserver{items: []model.Observation{observation(model.PhaseObserved, "source"), observation(model.PhaseObserved, "drifted")}}
	journal, effects, receipts, lock := &fakeJournal{}, &fakeEffects{}, &memoryReceipts{}, &fakeLock{}
	kernel, _ := New(testRegistry(t), syntheticObjectiveContracts(t), syntheticProgram, observer, fixedClock{now}, fakeLocker{lock}, journal, effects, receipts)
	_, err := kernel.Apply(context.Background(), request(t, now))
	var stale StalePrescriptionError
	if !errors.As(err, &stale) {
		t.Fatalf("error = %v, want StaleAdmissionError", err)
	}
	if effects.executions != 0 || journal.aborted != 0 || journal.begun != 0 {
		t.Fatalf("effect executions=%d aborted=%d", effects.executions, journal.aborted)
	}
}

func TestApplyRejectsHumanRevisionAdvanceBeforeEffect(t *testing.T) {
	// control-law: a later human commit invalidates an older agent prescription
	now := time.Unix(30, 0).UTC()
	advanced := observation(model.PhaseObserved, "source")
	advanced.StateRevision = 2
	observer := &sequenceObserver{items: []model.Observation{observation(model.PhaseObserved, "source"), advanced}}
	journal, effects, receipts, lock := &fakeJournal{}, &fakeEffects{}, &memoryReceipts{}, &fakeLock{}
	kernel, _ := New(testRegistry(t), syntheticObjectiveContracts(t), syntheticProgram, observer, fixedClock{now}, fakeLocker{lock}, journal, effects, receipts)
	_, err := kernel.Apply(context.Background(), request(t, now))
	var stale StalePrescriptionError
	if !errors.As(err, &stale) || stale.ExpectedStateRevision != 1 || stale.ObservedStateRevision != 2 {
		t.Fatalf("error = %v, want state-revision stale prescription", err)
	}
	if effects.executions != 0 || journal.begun != 0 || len(receipts.values) != 0 {
		t.Fatalf("stale revision crossed effect boundary: effects=%d journals=%d receipts=%d", effects.executions, journal.begun, len(receipts.values))
	}
}

func TestApplyRollsBackFailedPostconditionAndDoesNotReceipt(t *testing.T) {
	// control-law: successful-effect-call-is-not-transition-success
	now := time.Unix(30, 0).UTC()
	observer := &sequenceObserver{items: []model.Observation{observation(model.PhaseObserved, "source"), observation(model.PhaseObserved, "source"), observation(model.PhaseObserved, "unchanged")}}
	journal, effects, receipts, lock := &fakeJournal{}, &fakeEffects{result: ports.EffectResult{Settlement: ports.EffectSettled}}, &memoryReceipts{}, &fakeLock{}
	kernel, _ := New(testRegistry(t), syntheticObjectiveContracts(t), syntheticProgram, observer, fixedClock{now}, fakeLocker{lock}, journal, effects, receipts)
	_, err := kernel.Apply(context.Background(), request(t, now))
	var postcondition PostconditionError
	if !errors.As(err, &postcondition) {
		t.Fatalf("error = %v, want PostconditionError", err)
	}
	if effects.executions != 1 || effects.rollbacks != 1 || len(receipts.values) != 0 || journal.aborted != 1 {
		t.Fatalf("effects=%+v receipts=%d journal=%+v", effects, len(receipts.values), journal)
	}
}

func TestApplyRequiresRecoveryWhenJournalFailsAfterEffect(t *testing.T) {
	// control-law: post-effect-journal-failure-cannot-be-collapsed-to-abort
	now := time.Unix(30, 0).UTC()
	observer := &sequenceObserver{items: []model.Observation{observation(model.PhaseObserved, "source"), observation(model.PhaseObserved, "source")}}
	journal := &fakeJournal{failMark: "verifying"}
	effects, receipts, lock := &fakeEffects{result: ports.EffectResult{Settlement: ports.EffectSettled}}, &memoryReceipts{}, &fakeLock{}
	kernel, _ := New(testRegistry(t), syntheticObjectiveContracts(t), syntheticProgram, observer, fixedClock{now}, fakeLocker{lock}, journal, effects, receipts)
	_, err := kernel.Apply(context.Background(), request(t, now))
	if err == nil || !strings.Contains(err.Error(), "injected journal mark failure") {
		t.Fatalf("error=%v, want injected post-effect journal failure", err)
	}
	if effects.executions != 1 || effects.rollbacks != 0 || journal.recovery != 1 || journal.aborted != 0 || len(receipts.values) != 0 {
		t.Fatalf("post-effect journal failure was not preserved: effects=%+v journal=%+v receipts=%d", effects, journal, len(receipts.values))
	}
}

func TestApplyPreservesUnknownExternalOutcomeForReconciliation(t *testing.T) {
	// control-law: unknown-external-outcome-is-never-retried-or-collapsed-to-false
	now := time.Unix(30, 0).UTC()
	observer := &sequenceObserver{items: []model.Observation{observation(model.PhaseObserved, "source"), observation(model.PhaseObserved, "source"), observation(model.PhaseObserved, "unchanged")}}
	journal, effects, receipts, lock := &fakeJournal{}, &fakeEffects{result: ports.EffectResult{Settlement: ports.EffectUnknown}}, &memoryReceipts{}, &fakeLock{}
	kernel, _ := New(testRegistry(t), syntheticObjectiveContracts(t), syntheticProgram, observer, fixedClock{now}, fakeLocker{lock}, journal, effects, receipts)
	_, err := kernel.Apply(context.Background(), request(t, now))
	var unknown ExternalOutcomeUnknownError
	if !errors.As(err, &unknown) {
		t.Fatalf("error=%v, want ExternalOutcomeUnknownError", err)
	}
	if effects.executions != 1 || effects.rollbacks != 0 || journal.recovery != 1 || len(receipts.values) != 0 {
		t.Fatalf("external uncertainty was not preserved: effects=%+v journal=%+v receipts=%d", effects, journal, len(receipts.values))
	}
}

func TestOwnedExternalExecutionErrorRequiresRecoveryWithoutRollback(t *testing.T) {
	// control-law: a returned transport error cannot prove an external effect did not settle
	now := time.Unix(30, 0).UTC()
	registry := testRegistryWithAdvanceClass(t, catalog.EventOwnedExternal)
	observer := &sequenceObserver{items: []model.Observation{observation(model.PhaseObserved, "source"), observation(model.PhaseObserved, "source")}}
	journal, effects, receipts, lock := &fakeJournal{}, &fakeEffects{err: context.DeadlineExceeded}, &memoryReceipts{}, &fakeLock{}
	kernel, err := New(registry, syntheticObjectiveContracts(t), syntheticProgram, observer, fixedClock{now}, fakeLocker{lock}, journal, effects, receipts)
	if err != nil {
		t.Fatal(err)
	}
	apply := request(t, now)
	apply.Authority.Receipts[0].Class = catalog.AuthorityHuman
	snapshot, snapshotErr := model.CanonicalizeForProgram(observation(model.PhaseObserved, "source"), syntheticProgramFingerprint)
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	transition, _ := registry.Lookup("test.advance")
	capabilities, capabilityErr := protocol.ProjectCapabilities(snapshot, transition, apply.Authority, now)
	if capabilityErr != nil {
		t.Fatal(capabilityErr)
	}
	apply.Prescription, err = protocol.NewPrescription(snapshot, transition, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	_, err = kernel.Apply(context.Background(), apply)
	var unknown ExternalOutcomeUnknownError
	if !errors.As(err, &unknown) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v, want unknown external outcome joined with deadline", err)
	}
	if effects.executions != 1 || effects.rollbacks != 0 || journal.recovery != 1 || journal.aborted != 0 || len(receipts.values) != 0 {
		t.Fatalf("ambiguous external error was collapsed: effects=%+v journal=%+v receipts=%d", effects, journal, len(receipts.values))
	}
}

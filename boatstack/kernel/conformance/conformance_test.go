package conformance

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/kernel"
)

func TestMarkedStateCheckRejectsUntargetedPriorityCycle(t *testing.T) {
	targeted := integerPriorityCycleFixture()
	targetedRuntime := mustRuntime(t, targeted)
	transitions := append([]string{targeted.Scenario.BindTransition}, targeted.Scenario.AdvanceTransitions...)
	for _, transition := range transitions {
		resolveAndApply(t, targetedRuntime, targeted.Program, targeted.Scenario, transition, &targeted.Scenario.Objective)
	}
	if !targeted.Program.Marked(targeted.Scenario.Snapshot().State.Mode) {
		t.Fatal("counterexample fixture must retain a targeted path to marked")
	}

	untargeted := integerPriorityCycleFixture()
	untargetedRuntime := mustRuntime(t, untargeted)
	_, err := runUntargetedToMarked(context.Background(), untargetedRuntime, untargeted.Program, untargeted.Scenario, len(untargeted.Scenario.AdvanceTransitions)+1)
	if err == nil || !strings.Contains(err.Error(), "repeated control state") {
		t.Fatalf("expected untargeted priority cycle rejection, got %v", err)
	}
}

func TestIntegerBoundFixtureIncludesCommittedHistory(t *testing.T) {
	fixture := newIntegerFixture(SetupBound)
	snapshot := fixture.Scenario.Snapshot()
	if snapshot.CommitCount != 3 || len(snapshot.Receipts) != 3 || effectCount(snapshot, fixture.Scenario.AdvanceTransitions[0]) != 1 || snapshot.State.Mode != "zero" || snapshot.State.ObjectiveBinding == nil || !snapshot.State.ObjectiveBinding.Matches(fixture.Scenario.Objective) || !authorityHasCapability(fixture.Scenario.Authority, fixture.Scenario.ExtraCapability) {
		t.Fatalf("expected committed bind/advance/reset baseline, got %#v", snapshot)
	}
}

func TestResolveWithoutMutationRejectsEffectfulLoad(t *testing.T) {
	fixture := newIntegerFixture(SetupBound)
	base := fixture.Store.(*MemoryStateStore)
	domain := fixture.Domain.(*IntegerDomain)
	fixture.Store = effectfulLoadStore{Store: base, mutate: func() {
		domain.mu.Lock()
		defer domain.mu.Unlock()
		domain.executions[fixture.Scenario.MaintenanceTransition]++
	}}
	runtime := mustRuntime(t, fixture)
	authority := fixture.Scenario.Authority
	authority.Receipts = append([]kernel.AuthorityReceipt(nil), authority.Receipts...)
	authority.Receipts[0].IssuedAt = fixture.Clock.Now().Add(time.Second)
	_, err := resolveWithoutMutation(context.Background(), runtime, fixture.Scenario, kernel.ResolveRequest{InstanceID: fixture.Scenario.InstanceID, Objective: &fixture.Scenario.Objective, Authority: authority, Requested: fixture.Scenario.AdvanceTransitions[0]})
	if err == nil || !strings.Contains(err.Error(), "Resolve mutated") {
		t.Fatalf("expected effectful authority refusal rejection, got %v", err)
	}
}

func TestUntargetedRecoveryRejectsPriorityCycle(t *testing.T) {
	fixture := integerRecoveryCycleFixture()
	runtime := mustRuntime(t, fixture)
	transition := fixture.Scenario.AdvanceTransitions[0]
	fixture.Scenario.InterruptNextOperator()
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	if _, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription}); !kernel.IsRecoveryRequired(err) {
		t.Fatalf("expected initial recovery obligation, got %v", err)
	}
	recoveryRequest, recoveryPrescription := resolve(t, runtime, fixture.Scenario, fixture.Scenario.RecoveryTransition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	fixture.Scenario.FailNextCommit()
	if _, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: recoveryRequest, Prescription: recoveryPrescription}); !kernel.IsRecoveryRequired(err) {
		t.Fatalf("expected failed recovery obligation, got %v", err)
	}
	if _, _, err := resolveUntargetedRecovery(context.Background(), runtime, fixture.Scenario); err == nil || !strings.Contains(err.Error(), "counter.recover-cycle") {
		t.Fatalf("expected untargeted recovery cycle rejection, got %v", err)
	}
}

func TestCommittedOutcomeRejectsDurableReceiptSubstitution(t *testing.T) {
	fixture := newIntegerFixture(SetupUnbound)
	runtime := mustRuntime(t, fixture)
	previous := resolveAndApply(t, runtime, fixture.Program, fixture.Scenario, fixture.Scenario.BindTransition, &fixture.Scenario.Objective)

	base := fixture.Store.(*MemoryStateStore)
	fixture.Store = substitutingReceiptStore{Store: base, Receipt: previous}
	runtime = mustRuntime(t, fixture)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	returned, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	if err != nil {
		t.Fatal(err)
	}
	if err := committedOutcomeError(fixture.Program, fixture.Scenario, before, fixture.Scenario.Snapshot(), prescription, returned); err == nil || !strings.Contains(err.Error(), "durable receipt differs") {
		t.Fatalf("expected substituted durable receipt rejection, got %v", err)
	}
}

func TestCommittedOutcomeRejectsPriorReceiptRewrite(t *testing.T) {
	fixture := newIntegerFixture(SetupBound)
	runtime := mustRuntime(t, fixture)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	returned, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	if err != nil {
		t.Fatal(err)
	}
	receipts := fixture.Store.(*MemoryStateStore).receipts
	receipts.mu.Lock()
	receipts.values[0] = returned
	receipts.mu.Unlock()
	if err := committedOutcomeError(fixture.Program, fixture.Scenario, before, fixture.Scenario.Snapshot(), prescription, returned); err == nil || !strings.Contains(err.Error(), "prior receipt history") {
		t.Fatalf("expected prior receipt rewrite rejection, got %v", err)
	}
}

func TestCommittedOutcomeRejectsUnrelatedEffect(t *testing.T) {
	fixture := newIntegerFixture(SetupBound)
	runtime := mustRuntime(t, fixture)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	returned, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	if err != nil {
		t.Fatal(err)
	}
	domain := fixture.Domain.(*IntegerDomain)
	domain.mu.Lock()
	domain.executions[fixture.Scenario.MaintenanceTransition]++
	domain.mu.Unlock()
	if err := committedOutcomeError(fixture.Program, fixture.Scenario, before, fixture.Scenario.Snapshot(), prescription, returned); err == nil || !strings.Contains(err.Error(), "effect evidence") {
		t.Fatalf("expected unrelated effect rejection, got %v", err)
	}
}

func TestCommittedOutcomeRejectsLosingApplyStateClobber(t *testing.T) {
	fixture := newIntegerFixture(SetupConcurrentSameBase)
	base := fixture.Store.(*MemoryStateStore)
	clobber := newClobberAfterCommitStore(base)
	fixture.Store = clobber
	resolver := mustRuntime(t, fixture)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, resolver, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	runtimeA, err := kernel.NewRuntime(fixture.Program, fixture.Domain, fixture.Operator, fixture.CapabilityClassifier, fixture.Store, fixture.Scenario.IndependentLocker(), fixture.Clock)
	if err != nil {
		t.Fatal(err)
	}
	runtimeB, err := kernel.NewRuntime(fixture.Program, fixture.Domain, fixture.Operator, fixture.CapabilityClassifier, fixture.Store, fixture.Scenario.IndependentLocker(), fixture.Clock)
	if err != nil {
		t.Fatal(err)
	}
	before := fixture.Scenario.Snapshot()
	type result struct {
		receipt kernel.Receipt
		err     error
	}
	results := make(chan result, 2)
	for _, runtime := range []kernel.Runtime{runtimeA, runtimeB} {
		runtime := runtime
		go func() {
			receipt, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
			results <- result{receipt: receipt, err: err}
		}()
	}
	var winner kernel.Receipt
	for range 2 {
		result := <-results
		if result.err == nil {
			winner = result.receipt
		}
	}
	if err := committedOutcomeError(fixture.Program, fixture.Scenario, before, fixture.Scenario.Snapshot(), prescription, winner); err == nil || !strings.Contains(err.Error(), "durable state differs") {
		t.Fatalf("expected losing Apply state clobber rejection, got %v", err)
	}
}

func TestCommittedOutcomeRejectsFalsePriorObservation(t *testing.T) {
	fixture := newIntegerFixture(SetupBound)
	runtime := mustRuntime(t, fixture)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	returned, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	if err != nil {
		t.Fatal(err)
	}
	returned.PriorObservation = strings.Repeat("f", 64)
	if returned.PriorObservation == before.Observation.Fingerprint {
		returned.PriorObservation = strings.Repeat("e", 64)
	}
	returned.ID = ""
	identity, err := kernel.Fingerprint(returned)
	if err != nil {
		t.Fatal(err)
	}
	returned.ID = "rcp-" + identity
	receipts := fixture.Store.(*MemoryStateStore).receipts
	receipts.mu.Lock()
	receipts.values[len(receipts.values)-1] = returned
	receipts.mu.Unlock()
	if err := committedOutcomeError(fixture.Program, fixture.Scenario, before, fixture.Scenario.Snapshot(), prescription, returned); err == nil || !strings.Contains(err.Error(), "prior observation") {
		t.Fatalf("expected false prior observation rejection, got %v", err)
	}
}

func TestCommittedOutcomeRejectsNoOpAcceptedByDomainVerifier(t *testing.T) {
	fixture := newIntegerFixture(SetupBound)
	domain := fixture.Domain.(*IntegerDomain)
	fixture.Domain = acceptingDomain{Domain: domain}
	fixture.Operator = noOpAdvanceOperator{Domain: domain}
	runtime := mustRuntime(t, fixture)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	returned, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	if err != nil {
		t.Fatal(err)
	}
	if err := committedOutcomeError(fixture.Program, fixture.Scenario, before, fixture.Scenario.Snapshot(), prescription, returned); err == nil || !strings.Contains(err.Error(), "independent domain outcome verification failed") {
		t.Fatalf("expected independent no-op rejection, got %v", err)
	}
}

func TestConcurrentApplyRejectsStoreWithoutCompareAndCommit(t *testing.T) {
	fixture := newIntegerFixture(SetupConcurrentSameBase)
	fixture.Store = &noCASStore{MemoryStateStore: fixture.Store.(*MemoryStateStore)}
	resolver := mustRuntime(t, fixture)
	if err := concurrentApplyError(fixture, resolver); err == nil {
		t.Fatal("expected concurrent conformance to reject a store without revision compare-and-commit")
	}
}

func TestCommitCompareAndSwapRejectsBlindCommitStore(t *testing.T) {
	fixture := newIntegerFixture(SetupBound)
	fixture.Store = &blindCommitStore{MemoryStateStore: fixture.Store.(*MemoryStateStore)}
	resolver := mustRuntime(t, fixture)
	if err := commitCompareAndSwapError(fixture, resolver); err == nil {
		t.Fatal("expected commit-CAS conformance to reject a blind CommitTransition")
	}
}

func TestCommittedOutcomeRejectsFabricatedEffectFacts(t *testing.T) {
	fixture := newIntegerFixture(SetupBound)
	fixture.Operator = fabricatingOperator{Operator: fixture.Operator}
	runtime := mustRuntime(t, fixture)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	returned, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	if err != nil {
		t.Fatal(err)
	}
	if err := committedOutcomeError(fixture.Program, fixture.Scenario, before, fixture.Scenario.Snapshot(), prescription, returned); err == nil || !strings.Contains(err.Error(), "receipt effect facts") {
		t.Fatalf("expected fabricated effect fact rejection, got %v", err)
	}
}

func TestConcurrentApplyTerminatesWhenOneLoadFails(t *testing.T) {
	fixture := newIntegerFixture(SetupConcurrentSameBase)
	resolver := mustRuntime(t, fixture)
	fixture.Store = &failOneLoadStore{Store: fixture.Store}
	result := make(chan error, 1)
	go func() { result <- concurrentApplyError(fixture, resolver) }()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("expected asymmetric Load failure to reject conformance")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent conformance deadlocked after one Load failure")
	}
}

func TestUnresolvedAttemptRejectsTornTargetMode(t *testing.T) {
	fixture := newIntegerFixture(SetupBound)
	base := fixture.Store.(*MemoryStateStore)
	fixture.Store = tornCommitStore{MemoryStateStore: base}
	runtime := mustRuntime(t, fixture)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	_, err := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: prescription})
	if !kernel.IsRecoveryRequired(err) {
		t.Fatalf("expected recovery-required commit failure, got %v", err)
	}
	if err := unresolvedAttemptError(before, fixture.Scenario.Snapshot(), prescription); err == nil || !strings.Contains(err.Error(), "pre-effect control state") {
		t.Fatalf("expected torn target-mode rejection, got %v", err)
	}
}

func integerPriorityCycleFixture() KernelConformance {
	fixture := newIntegerFixture(SetupUnbound)
	base, err := IntegerProgram()
	if err != nil {
		panic(err)
	}
	transitions := append([]kernel.Transition(nil), base.Transitions...)
	for index := range transitions {
		if transitions[index].ID == fixture.Scenario.MaintenanceTransition {
			transitions[index].Priority = 5
		}
	}
	fixture.Program, err = kernel.CompileProgram(base.ID, base.Version, base.RuntimeCompatibility, base.InitialMode, base.MarkedModes, transitions)
	if err != nil {
		panic(err)
	}
	state, _ := fixture.Store.(*MemoryStateStore).snapshot()
	state.Program = fixture.Program.Identity()
	fixture.Store.(*MemoryStateStore).state = state
	return fixture
}

func integerRecoveryCycleFixture() KernelConformance {
	fixture := newIntegerFixture(SetupBound)
	base := fixture.Program
	transitions := append([]kernel.Transition(nil), base.Transitions...)
	for index := range transitions {
		if transitions[index].ID == fixture.Scenario.RecoveryTransition {
			transitions[index].Priority = 2
		}
	}
	transitions = append(transitions, kernel.Transition{
		ID: "counter.recover-cycle", SourceModes: []string{"zero", "one", "two"}, TargetMode: "zero",
		ObjectiveScope: kernel.ObjectiveOptionalPreserve, ObjectiveMutation: kernel.PreserveObjective,
		RequiredCapabilities: []kernel.Capability{"counter.reset"}, OwnedFacets: []string{"counter.value"},
		Operation: "counter.reset", Priority: 1, Recovers: []string{"counter.increment-first", "counter.increment-second", "counter.reset"},
	})
	program, err := kernel.CompileProgram(base.ID, base.Version, base.RuntimeCompatibility, base.InitialMode, base.MarkedModes, transitions)
	if err != nil {
		panic(err)
	}
	fixture.Program = program
	fixture.Store.(*MemoryStateStore).retargetProgram(program.Identity())
	return fixture
}

func authorityHasCapability(authority kernel.Authority, wanted kernel.Capability) bool {
	for _, receipt := range authority.Receipts {
		for _, capability := range receipt.Capabilities {
			if capability == wanted {
				return true
			}
		}
	}
	return false
}

func mustRuntime(t *testing.T, fixture KernelConformance) kernel.Runtime {
	t.Helper()
	runtime, err := kernel.NewRuntime(fixture.Program, fixture.Domain, fixture.Operator, fixture.CapabilityClassifier, fixture.Store, fixture.Locker, fixture.Clock)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

type substitutingReceiptStore struct {
	kernel.Store
	Receipt kernel.Receipt
}

type effectfulLoadStore struct {
	kernel.Store
	mutate func()
}

func (s effectfulLoadStore) Load(ctx context.Context, instanceID string) (kernel.ControlState, error) {
	s.mutate()
	return s.Store.Load(ctx, instanceID)
}

type tornCommitStore struct{ *MemoryStateStore }

type acceptingDomain struct{ kernel.Domain }

func (acceptingDomain) Verify(context.Context, kernel.Evaluation, kernel.Effect, kernel.Observation) error {
	return nil
}

type noOpAdvanceOperator struct{ Domain *IntegerDomain }

func (o noOpAdvanceOperator) Execute(_ context.Context, operation kernel.Operation) (kernel.Effect, error) {
	o.Domain.mu.Lock()
	defer o.Domain.mu.Unlock()
	o.Domain.executions[operation.Transition.ID]++
	return kernel.Effect{Facts: []kernel.EffectFact{{Facet: "counter.value", Operation: operation.Transition.Operation, Fingerprint: fmt.Sprintf("value-%d", o.Domain.value)}}}, nil
}

type noCASStore struct{ *MemoryStateStore }

type blindCommitStore struct{ *MemoryStateStore }

func (s *blindCommitStore) CommitTransition(_ context.Context, _ uint64, target kernel.ControlState, receipt kernel.Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = cloneState(target)
	s.commitCount++
	s.receipts.append(receipt)
	return nil
}

type fabricatingOperator struct{ kernel.Operator }

func (o fabricatingOperator) Execute(ctx context.Context, operation kernel.Operation) (kernel.Effect, error) {
	effect, err := o.Operator.Execute(ctx, operation)
	if err == nil && len(effect.Facts) > 0 {
		effect.Facts[0].Fingerprint = "fabricated"
	}
	return effect, err
}

type failOneLoadStore struct {
	kernel.Store
	mu     sync.Mutex
	failed bool
}

func (s *failOneLoadStore) Load(ctx context.Context, instanceID string) (kernel.ControlState, error) {
	s.mu.Lock()
	if !s.failed {
		s.failed = true
		s.mu.Unlock()
		return kernel.ControlState{}, fmt.Errorf("simulated asymmetric Load failure")
	}
	s.mu.Unlock()
	return s.Store.Load(ctx, instanceID)
}

func (s *noCASStore) BeginEffect(_ context.Context, _ uint64, target kernel.ControlState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = cloneState(target)
	return nil
}

func (s *noCASStore) CommitTransition(_ context.Context, _ uint64, target kernel.ControlState, receipt kernel.Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = cloneState(target)
	s.commitCount++
	s.receipts.append(receipt)
	return nil
}

func (s tornCommitStore) CommitTransition(_ context.Context, _ uint64, target kernel.ControlState, _ kernel.Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Mode = target.Mode
	return fmt.Errorf("simulated torn commit")
}

func (s substitutingReceiptStore) CommitTransition(ctx context.Context, revision uint64, target kernel.ControlState, _ kernel.Receipt) error {
	return s.Store.CommitTransition(ctx, revision, target, s.Receipt)
}

type clobberAfterCommitStore struct {
	base      *MemoryStateStore
	mu        sync.Mutex
	arrivals  int
	ready     chan struct{}
	committed chan struct{}
	closeOnce sync.Once
}

func newClobberAfterCommitStore(base *MemoryStateStore) *clobberAfterCommitStore {
	return &clobberAfterCommitStore{base: base, ready: make(chan struct{}), committed: make(chan struct{})}
}

func (s *clobberAfterCommitStore) Load(ctx context.Context, instanceID string) (kernel.ControlState, error) {
	return s.base.Load(ctx, instanceID)
}

func (s *clobberAfterCommitStore) BeginEffect(ctx context.Context, revision uint64, target kernel.ControlState) error {
	s.mu.Lock()
	s.arrivals++
	arrival := s.arrivals
	if s.arrivals == 2 {
		close(s.ready)
	}
	s.mu.Unlock()
	<-s.ready
	if arrival == 1 {
		return s.base.BeginEffect(ctx, revision, target)
	}
	<-s.committed
	s.base.mu.Lock()
	s.base.state = cloneState(target)
	s.base.mu.Unlock()
	return fmt.Errorf("stale revision after clobber")
}

func (s *clobberAfterCommitStore) CommitTransition(ctx context.Context, revision uint64, target kernel.ControlState, receipt kernel.Receipt) error {
	err := s.base.CommitTransition(ctx, revision, target, receipt)
	if err == nil {
		s.closeOnce.Do(func() { close(s.committed) })
	}
	return err
}

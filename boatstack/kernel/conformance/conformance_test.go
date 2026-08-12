package conformance

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

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
	if snapshot.CommitCount != 1 || len(snapshot.Receipts) != 1 || snapshot.State.ObjectiveBinding == nil || !snapshot.State.ObjectiveBinding.Matches(fixture.Scenario.Objective) {
		t.Fatalf("expected one committed objective-binding baseline, got %#v", snapshot)
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
	if err := committedOutcomeError(fixture.Program, before, fixture.Scenario.Snapshot(), returned); err == nil || !strings.Contains(err.Error(), "durable receipt differs") {
		t.Fatalf("expected substituted durable receipt rejection, got %v", err)
	}
}

func TestCommittedOutcomeRejectsLosingApplyStateClobber(t *testing.T) {
	fixture := newIntegerFixture(SetupConcurrentSameBase)
	base := fixture.Store.(*MemoryStateStore)
	base.beginBarrier = nil
	clobber := newClobberAfterCommitStore(base)
	fixture.Store = clobber
	runtime := mustRuntime(t, fixture)
	transition := fixture.Scenario.AdvanceTransitions[0]
	request, prescription := resolve(t, runtime, fixture.Scenario, transition, &fixture.Scenario.Objective, fixture.Scenario.Authority)
	before := fixture.Scenario.Snapshot()
	type result struct {
		receipt kernel.Receipt
		err     error
	}
	results := make(chan result, 2)
	for range 2 {
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
	if err := committedOutcomeError(fixture.Program, before, fixture.Scenario.Snapshot(), winner); err == nil || !strings.Contains(err.Error(), "durable state differs") {
		t.Fatalf("expected losing Apply state clobber rejection, got %v", err)
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

package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/kernel"
)

// IntegerDomain is the reference non-software domain.
type IntegerDomain struct {
	mu                     sync.Mutex
	value                  int
	executions             map[string]int
	interruptNextIncrement bool
	panicNextIncrement     bool
}

func (d *IntegerDomain) Observe(context.Context, string) (kernel.Observation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return kernel.NewObservation(struct {
		Value int `json:"value"`
	}{d.value})
}

func (d *IntegerDomain) Admissible(_ context.Context, evaluation kernel.Evaluation) (bool, string, error) {
	var observed struct {
		Value int `json:"value"`
	}
	if err := json.Unmarshal(evaluation.Observation.Value, &observed); err != nil {
		return false, "", err
	}
	switch evaluation.Transition.Operation {
	case "objective.bind":
		return evaluation.State.ObjectiveBinding == nil && evaluation.Objective != nil, "objective is not yet bound", nil
	case "counter.increment":
		return evaluation.Objective != nil && observed.Value < 2, "exact objective is present and value is below target", nil
	case "counter.reset":
		return observed.Value > 0 || evaluation.State.Recovery != nil, "value is nonzero or recovery remains active", nil
	default:
		return false, "unknown transition", nil
	}
}

func (d *IntegerDomain) Verify(_ context.Context, evaluation kernel.Evaluation, _ kernel.Effect, target kernel.Observation) error {
	var before, after struct {
		Value int `json:"value"`
	}
	if err := json.Unmarshal(evaluation.Observation.Value, &before); err != nil {
		return err
	}
	if err := json.Unmarshal(target.Value, &after); err != nil {
		return err
	}
	switch evaluation.Transition.Operation {
	case "objective.bind":
		if before.Value != after.Value {
			return fmt.Errorf("objective binding changed domain state")
		}
	case "counter.increment":
		if after.Value != before.Value+1 {
			return fmt.Errorf("increment postcondition failed")
		}
	case "counter.reset":
		if after.Value != 0 {
			return fmt.Errorf("reset postcondition failed")
		}
	}
	return nil
}

func (d *IntegerDomain) changeObservation() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.value++
}

func (d *IntegerDomain) interruptNext() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.interruptNextIncrement = true
}

func (d *IntegerDomain) panicNext() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.panicNextIncrement = true
}

func (d *IntegerDomain) effectCounts() map[string]int {
	d.mu.Lock()
	defer d.mu.Unlock()
	copy := make(map[string]int, len(d.executions))
	for transition, count := range d.executions {
		copy[transition] = count
	}
	return copy
}

// IntegerOperator applies the reference domain operations.
type IntegerOperator struct{ Domain *IntegerDomain }

func (o IntegerOperator) Execute(_ context.Context, operation kernel.Operation) (kernel.Effect, error) {
	o.Domain.mu.Lock()
	defer o.Domain.mu.Unlock()
	o.Domain.executions[operation.Transition.ID]++
	switch operation.Transition.Operation {
	case "objective.bind":
	case "counter.increment":
		o.Domain.value++
		if o.Domain.interruptNextIncrement {
			o.Domain.interruptNextIncrement = false
			return kernel.Effect{}, fmt.Errorf("simulated interrupted operator")
		}
		if o.Domain.panicNextIncrement {
			o.Domain.panicNextIncrement = false
			panic("simulated process panic")
		}
	case "counter.reset":
		o.Domain.value = 0
	default:
		return kernel.Effect{}, fmt.Errorf("unknown operation")
	}
	facet := "counter.value"
	if operation.Transition.Operation == "objective.bind" {
		facet = "supervisor.objective"
	}
	return kernel.Effect{Facts: []kernel.EffectFact{{Facet: facet, Operation: operation.Transition.Operation, Fingerprint: fmt.Sprintf("value-%d", o.Domain.value)}}}, nil
}

// IntegerCapabilities classifies the reference operations.
type IntegerCapabilities struct{}

func (IntegerCapabilities) RequiredCapabilities(transition kernel.Transition) ([]kernel.Capability, error) {
	switch transition.Operation {
	case "objective.bind":
		return []kernel.Capability{"objective.bind"}, nil
	case "counter.increment":
		return []kernel.Capability{"counter.increment"}, nil
	case "counter.reset":
		return []kernel.Capability{"counter.reset"}, nil
	default:
		return nil, fmt.Errorf("unclassified operation %q", transition.Operation)
	}
}

// MemoryReceipts records committed receipts for the reference store.
type MemoryReceipts struct {
	mu     sync.Mutex
	values []kernel.Receipt
}

func (r *MemoryReceipts) append(receipt kernel.Receipt) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = append(r.values, receipt)
}

func (r *MemoryReceipts) snapshot() []kernel.Receipt {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]kernel.Receipt(nil), r.values...)
}

// MemoryStateStore is a revision-CAS reference Store.
type MemoryStateStore struct {
	mu             sync.Mutex
	state          kernel.ControlState
	receipts       *MemoryReceipts
	commitFailures int
	commitCount    int
}

func (s *MemoryStateStore) Load(context.Context, string) (kernel.ControlState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneState(s.state), nil
}

func (s *MemoryStateStore) BeginEffect(_ context.Context, revision uint64, target kernel.ControlState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Revision != revision {
		return fmt.Errorf("stale revision")
	}
	s.state = cloneState(target)
	return nil
}

func (s *MemoryStateStore) CommitTransition(_ context.Context, revision uint64, target kernel.ControlState, receipt kernel.Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.commitFailures > 0 {
		s.commitFailures--
		return fmt.Errorf("simulated atomic transaction failure")
	}
	if s.state.Revision != revision {
		return fmt.Errorf("stale revision")
	}
	s.state = cloneState(target)
	s.commitCount++
	s.receipts.append(receipt)
	return nil
}

func (s *MemoryStateStore) snapshot() (kernel.ControlState, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneState(s.state), s.commitCount
}

func (s *MemoryStateStore) failNextCommit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commitFailures++
}

func (s *MemoryStateStore) retarget(instanceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.InstanceID = instanceID
}

func (s *MemoryStateStore) bumpRevision() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Revision++
}

func (s *MemoryStateStore) retargetProgram(program kernel.ProgramIdentity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Program = program
}

func (s *MemoryStateStore) rebind(objective kernel.Objective) {
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, err := kernel.BindObjective(objective)
	if err != nil {
		panic(err)
	}
	s.state.ObjectiveBinding = &binding
}

// MemoryLocker serializes one control instance.
type MemoryLocker struct{ mu sync.Mutex }

func (l *MemoryLocker) Acquire(context.Context, string) (kernel.Lock, error) {
	l.mu.Lock()
	return memoryLock{mu: &l.mu}, nil
}

type memoryLock struct{ mu *sync.Mutex }

func (l memoryLock) Unlock() error {
	l.mu.Unlock()
	return nil
}

// FixedClock returns one deterministic, test-controlled time.
type FixedClock struct {
	mu   sync.Mutex
	Time time.Time
}

func (c *FixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Time
}

func (c *FixedClock) advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Time = c.Time.Add(duration)
}

// IntegerProgram compiles the reference control program.
func IntegerProgram() (kernel.Program, error) {
	return kernel.CompileProgram("integer-control", "1.0.0", "kernel-v1", "unbound", []string{"two"}, []kernel.Transition{
		{ID: "objective.bind", SourceModes: []string{"unbound"}, TargetMode: "zero", ObjectiveScope: kernel.ObjectiveNone, ObjectiveMutation: kernel.BindObjectiveMutation, RequiredCapabilities: []kernel.Capability{"objective.bind"}, OwnedFacets: []string{"supervisor.objective"}, Operation: "objective.bind", Priority: 5},
		{ID: "counter.increment-first", SourceModes: []string{"zero"}, TargetMode: "one", ObjectiveScope: kernel.ObjectiveBoundExact, ObjectiveMutation: kernel.PreserveObjective, RequiredCapabilities: []kernel.Capability{"counter.increment"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.increment", Priority: 10},
		{ID: "counter.increment-second", SourceModes: []string{"one"}, TargetMode: "two", ObjectiveScope: kernel.ObjectiveBoundExact, ObjectiveMutation: kernel.PreserveObjective, RequiredCapabilities: []kernel.Capability{"counter.increment"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.increment", Priority: 10},
		{ID: "counter.reset", SourceModes: []string{"one", "two"}, TargetMode: "zero", ObjectiveScope: kernel.ObjectiveOptionalPreserve, ObjectiveMutation: kernel.PreserveObjective, RequiredCapabilities: []kernel.Capability{"counter.reset"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.reset", Priority: 20},
		{ID: "objective.recover", SourceModes: []string{"unbound"}, TargetMode: "unbound", ObjectiveScope: kernel.ObjectiveOptionalPreserve, ObjectiveMutation: kernel.PreserveObjective, RequiredCapabilities: []kernel.Capability{"counter.reset"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.reset", Priority: 1, Recovers: []string{"objective.bind"}},
		{ID: "counter.recover", SourceModes: []string{"zero", "one", "two"}, TargetMode: "zero", ObjectiveScope: kernel.ObjectiveOptionalPreserve, ObjectiveMutation: kernel.PreserveObjective, RequiredCapabilities: []kernel.Capability{"counter.reset"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.reset", Priority: 1, Recovers: []string{"counter.increment-first", "counter.increment-second", "counter.reset"}},
	})
}

// IntegerFixture returns the reusable reference conformance suite.
func IntegerFixture() KernelConformance {
	return newIntegerFixture(SetupUnbound)
}

func newIntegerFixture(setup Setup) KernelConformance {
	if !setup.valid() {
		panic(invalidSetup(setup))
	}
	program, err := IntegerProgram()
	if err != nil {
		panic(err)
	}
	objective, err := kernel.NewObjective("reach-two", 1, map[string]int{"value": 2})
	if err != nil {
		panic(err)
	}
	revised, err := kernel.NewObjective("reach-two", 2, map[string]int{"value": 3})
	if err != nil {
		panic(err)
	}
	conflicting, err := kernel.NewObjective("other", 1, map[string]int{"value": 0})
	if err != nil {
		panic(err)
	}
	alternateTransitions := append([]kernel.Transition(nil), program.Transitions...)
	for index := range alternateTransitions {
		if alternateTransitions[index].ID == "counter.increment-first" {
			alternateTransitions[index].TargetMode = "two"
		}
	}
	alternateProgram, err := kernel.CompileProgram(program.ID, program.Version, program.RuntimeCompatibility, program.InitialMode, program.MarkedModes, alternateTransitions)
	if err != nil {
		panic(err)
	}
	state := kernel.ControlState{InstanceID: "counter-fixture", Program: program.Identity(), Mode: "unbound", Revision: 1}
	value := 0
	switch setup {
	case SetupConcurrentSameBase:
		binding, bindErr := kernel.BindObjective(objective)
		if bindErr != nil {
			panic(bindErr)
		}
		state.Mode, state.ObjectiveBinding = "zero", &binding
	case SetupMaintenanceAbsent:
		state.Mode, value = "one", 1
	case SetupMaintenanceBound:
		binding, bindErr := kernel.BindObjective(objective)
		if bindErr != nil {
			panic(bindErr)
		}
		state.Mode, state.ObjectiveBinding, value = "one", &binding, 1
	}
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	authority := kernel.Authority{Receipts: []kernel.AuthorityReceipt{{ID: "human-counter", Subject: "fixture", Fingerprint: "fixture-authority", Capabilities: []kernel.Capability{"counter.audit", "counter.increment", "counter.reset", "objective.bind"}, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(24 * time.Hour)}}}
	domain := &IntegerDomain{value: value, executions: map[string]int{}}
	receipts := &MemoryReceipts{}
	store := &MemoryStateStore{state: state, receipts: receipts}
	clock := &FixedClock{Time: now}
	fixture := KernelConformance{
		Domain:               domain,
		Operator:             IntegerOperator{Domain: domain},
		CapabilityClassifier: IntegerCapabilities{},
		Store:                store,
		Locker:               &MemoryLocker{},
		Clock:                clock,
		Program:              program,
	}
	fixture.Scenario = Scenario{
		InstanceID:            state.InstanceID,
		Objective:             objective,
		RevisedObjective:      revised,
		ConflictingObjective:  conflicting,
		AlternateProgram:      alternateProgram,
		Authority:             authority,
		BindTransition:        "objective.bind",
		AdvanceTransitions:    []string{"counter.increment-first", "counter.increment-second"},
		MaintenanceTransition: "counter.reset",
		RecoveryTransition:    "counter.recover",
		RecoveryCapability:    "counter.reset",
		ExtraCapability:       "counter.audit",
		ChangeObservation:     domain.changeObservation,
		RebindObjective:       store.rebind,
		BumpStateRevision:     store.bumpRevision,
		RetargetProgram:       store.retargetProgram,
		AdvanceClock:          clock.advance,
		IndependentLocker:     func() kernel.Locker { return &MemoryLocker{} },
		VerifyCommitted: func(before, after Snapshot, receipt kernel.Receipt) error {
			return verifyIntegerCommitted(program, before, after, receipt)
		},
		InterruptNextOperator: domain.interruptNext,
		PanicNextOperator:     domain.panicNext,
		FailNextCommit:        store.failNextCommit,
		RetargetInstance:      store.retarget,
		Snapshot: func() Snapshot {
			current, commits := store.snapshot()
			observation, observeErr := domain.Observe(context.Background(), current.InstanceID)
			if observeErr != nil {
				panic(observeErr)
			}
			return Snapshot{State: current, Observation: observation, Effects: domain.effectCounts(), Receipts: receipts.snapshot(), CommitCount: commits}
		},
	}
	fixture.New = func(_ testing.TB, requested Setup) KernelConformance {
		return newIntegerFixture(requested)
	}
	if setup == SetupBound {
		runtime, runtimeErr := kernel.NewRuntime(program, fixture.Domain, fixture.Operator, fixture.CapabilityClassifier, fixture.Store, fixture.Locker, fixture.Clock)
		if runtimeErr != nil {
			panic(runtimeErr)
		}
		for _, transition := range []string{fixture.Scenario.BindTransition, fixture.Scenario.AdvanceTransitions[0], fixture.Scenario.MaintenanceTransition} {
			request := kernel.ResolveRequest{InstanceID: state.InstanceID, Objective: &objective, Authority: authority, Requested: transition}
			resolution, resolveErr := runtime.Resolve(context.Background(), request)
			if resolveErr != nil || resolution.Prescription == nil {
				panic(fmt.Sprintf("seed bound fixture %s: decision=%#v error=%v", transition, resolution.Decision, resolveErr))
			}
			if _, applyErr := runtime.Apply(context.Background(), kernel.ApplyRequest{ResolveRequest: request, Prescription: *resolution.Prescription}); applyErr != nil {
				panic(applyErr)
			}
		}
	}
	return fixture
}

func verifyIntegerCommitted(program kernel.Program, before, after Snapshot, receipt kernel.Receipt) error {
	var prior, result struct {
		Value int `json:"value"`
	}
	if err := json.Unmarshal(before.Observation.Value, &prior); err != nil {
		return err
	}
	if err := json.Unmarshal(after.Observation.Value, &result); err != nil {
		return err
	}
	transition, ok := program.Transition(receipt.TransitionID)
	if !ok {
		return fmt.Errorf("unknown transition %q", receipt.TransitionID)
	}
	facet := "counter.value"
	if transition.Operation == "objective.bind" {
		facet = "supervisor.objective"
	}
	expectedFact := kernel.EffectFact{Facet: facet, Operation: transition.Operation, Fingerprint: fmt.Sprintf("value-%d", result.Value)}
	if len(receipt.Effects) != 1 || receipt.Effects[0] != expectedFact {
		return fmt.Errorf("receipt effect facts %#v differ from independent evidence %#v", receipt.Effects, expectedFact)
	}
	switch transition.Operation {
	case "objective.bind":
		if result.Value != prior.Value {
			return fmt.Errorf("objective binding changed value from %d to %d", prior.Value, result.Value)
		}
	case "counter.increment":
		if result.Value != prior.Value+1 {
			return fmt.Errorf("increment changed value from %d to %d", prior.Value, result.Value)
		}
	case "counter.reset":
		if result.Value != 0 {
			return fmt.Errorf("reset left value at %d", result.Value)
		}
	default:
		return fmt.Errorf("unsupported operation %q", transition.Operation)
	}
	return nil
}

func cloneState(state kernel.ControlState) kernel.ControlState {
	copy := state
	if state.ObjectiveBinding != nil {
		binding := *state.ObjectiveBinding
		copy.ObjectiveBinding = &binding
	}
	if state.Recovery != nil {
		recovery := *state.Recovery
		copy.Recovery = &recovery
	}
	return copy
}

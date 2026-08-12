package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

type integerDomain struct {
	mu                  sync.Mutex
	value               int
	incrementExecutions int
	failAfterIncrement  bool
	panicAfterIncrement bool
}

func (d *integerDomain) Observe(context.Context, string) (Observation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return NewObservation(struct {
		Value int `json:"value"`
	}{d.value})
}

func (d *integerDomain) Admissible(_ context.Context, evaluation Evaluation) (bool, string, error) {
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
		if evaluation.Objective == nil {
			return false, "exact objective required", nil
		}
		return observed.Value < 2, "value is below objective", nil
	case "counter.inspect":
		return true, "inspection is always available", nil
	case "counter.reset":
		return observed.Value > 0 || evaluation.State.Recovery != nil, "value is nonzero or recovery remains active", nil
	default:
		return false, "unknown transition", nil
	}
}

func (d *integerDomain) Verify(_ context.Context, evaluation Evaluation, effect Effect, target Observation) error {
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

type integerOperator struct{ domain *integerDomain }

func (o integerOperator) Execute(_ context.Context, operation Operation) (Effect, error) {
	o.domain.mu.Lock()
	defer o.domain.mu.Unlock()
	switch operation.Transition.Operation {
	case "objective.bind":
	case "counter.increment":
		o.domain.incrementExecutions++
		o.domain.value++
		if o.domain.panicAfterIncrement {
			o.domain.panicAfterIncrement = false
			panic("simulated process panic")
		}
		if o.domain.failAfterIncrement {
			o.domain.failAfterIncrement = false
			return Effect{}, fmt.Errorf("simulated interrupted operator")
		}
	case "counter.reset":
		o.domain.value = 0
	case "counter.inspect":
	default:
		return Effect{}, fmt.Errorf("unknown operation")
	}
	facet := "counter.value"
	if operation.Transition.Operation == "objective.bind" {
		facet = "supervisor.objective"
	}
	return Effect{Facts: []EffectFact{{Facet: facet, Operation: operation.Transition.Operation, Fingerprint: fmt.Sprintf("value-%d", o.domain.value)}}}, nil
}

type memoryStateStore struct {
	mu             sync.Mutex
	state          ControlState
	receipts       *memoryReceipts
	commitFailures int
}

func (s *memoryStateStore) Load(context.Context, string) (ControlState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, nil
}
func (s *memoryStateStore) BeginEffect(_ context.Context, revision uint64, target ControlState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Revision != revision {
		return fmt.Errorf("stale revision")
	}
	s.state = target
	return nil
}
func (s *memoryStateStore) CommitTransition(_ context.Context, revision uint64, target ControlState, receipt Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.commitFailures > 0 {
		s.commitFailures--
		return fmt.Errorf("simulated atomic transaction failure")
	}
	if s.state.Revision != revision {
		return fmt.Errorf("stale revision")
	}
	s.state = target
	s.receipts.values = append(s.receipts.values, receipt)
	return nil
}

type memoryReceipts struct{ values []Receipt }

type memoryLock struct{ mu *sync.Mutex }

func (l memoryLock) Unlock() error { l.mu.Unlock(); return nil }

type memoryLocker struct{ mu sync.Mutex }

func (l *memoryLocker) Acquire(context.Context, string) (Lock, error) {
	l.mu.Lock()
	return memoryLock{&l.mu}, nil
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type integerCapabilities struct{}

func (integerCapabilities) RequiredCapabilities(transition Transition) ([]Capability, error) {
	switch transition.Operation {
	case "objective.bind":
		return []Capability{"objective.bind"}, nil
	case "counter.increment":
		return []Capability{"counter.increment"}, nil
	case "counter.reset":
		return []Capability{"counter.reset"}, nil
	default:
		return nil, fmt.Errorf("unclassified operation %q", transition.Operation)
	}
}

func integerProgram(t *testing.T) Program {
	t.Helper()
	program, err := CompileProgram("integer-control", "1.0.0", "kernel-v1", "unbound", []string{"two"}, []Transition{
		{ID: "objective.bind", SourceModes: []string{"unbound"}, TargetMode: "zero", ObjectiveScope: ObjectiveNone, ObjectiveMutation: BindObjectiveMutation, RequiredCapabilities: []Capability{"objective.bind"}, OwnedFacets: []string{"supervisor.objective"}, Operation: "objective.bind", Priority: 5},
		{ID: "counter.increment-first", SourceModes: []string{"zero"}, TargetMode: "one", ObjectiveScope: ObjectiveBoundExact, ObjectiveMutation: PreserveObjective, RequiredCapabilities: []Capability{"counter.increment"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.increment", Priority: 10},
		{ID: "counter.increment-second", SourceModes: []string{"one"}, TargetMode: "two", ObjectiveScope: ObjectiveBoundExact, ObjectiveMutation: PreserveObjective, RequiredCapabilities: []Capability{"counter.increment"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.increment", Priority: 10},
		{ID: "counter.reset", SourceModes: []string{"one", "two"}, TargetMode: "zero", ObjectiveScope: ObjectiveOptionalPreserve, ObjectiveMutation: PreserveObjective, RequiredCapabilities: []Capability{"counter.reset"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.reset", Priority: 20},
		{ID: "objective.recover", SourceModes: []string{"unbound"}, TargetMode: "unbound", ObjectiveScope: ObjectiveOptionalPreserve, ObjectiveMutation: PreserveObjective, RequiredCapabilities: []Capability{"counter.reset"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.reset", Priority: 1, Recovers: []string{"objective.bind"}},
		{ID: "counter.recover", SourceModes: []string{"zero", "one", "two"}, TargetMode: "zero", ObjectiveScope: ObjectiveOptionalPreserve, ObjectiveMutation: PreserveObjective, RequiredCapabilities: []Capability{"counter.reset"}, OwnedFacets: []string{"counter.value"}, Operation: "counter.reset", Priority: 1, Recovers: []string{"counter.increment-first", "counter.increment-second", "counter.reset"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func newIntegerRuntime(t *testing.T, bound bool) (Runtime, *memoryStateStore, *memoryReceipts, *integerDomain, Objective, Authority) {
	t.Helper()
	program := integerProgram(t)
	objective, err := NewObjective("reach-two", 1, map[string]int{"value": 2})
	if err != nil {
		t.Fatal(err)
	}
	state := ControlState{InstanceID: "counter-fixture", Program: program.Identity(), Mode: "unbound", Revision: 1}
	if bound {
		binding, _ := BindObjective(objective)
		state.ObjectiveBinding = &binding
		state.Mode = "zero"
	}
	receipts, domain := &memoryReceipts{}, &integerDomain{}
	states := &memoryStateStore{state: state, receipts: receipts}
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	authority := Authority{Receipts: []AuthorityReceipt{{ID: "human-counter", Subject: "fixture", Fingerprint: "fixture-authority", Capabilities: []Capability{"objective.bind", "counter.increment", "counter.reset"}, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}}}
	runtime, err := NewRuntime(program, domain, integerOperator{domain}, integerCapabilities{}, states, &memoryLocker{}, fixedClock{now})
	if err != nil {
		t.Fatal(err)
	}
	return runtime, states, receipts, domain, objective, authority
}

func TestDeterministicNonSoftwareProgramReachesMarkedState(t *testing.T) {
	runtime, states, receipts, _, objective, authority := newIntegerRuntime(t, false)
	ctx := context.Background()
	for _, transition := range []string{"objective.bind", "counter.increment-first", "counter.increment-second"} {
		resolution, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: transition})
		if err != nil || resolution.Decision.Kind != Prescribed {
			t.Fatalf("resolve %s: %#v %v", transition, resolution.Decision, err)
		}
		receipt, err := runtime.Apply(ctx, ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: transition}, Prescription: *resolution.Prescription})
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Verification != "satisfied" || receipt.Program.Fingerprint == "" || receipt.AttemptStateRevision != receipt.PriorStateRevision+1 || receipt.ResultStateRevision != receipt.AttemptStateRevision+1 {
			t.Fatalf("incomplete receipt: %#v", receipt)
		}
	}
	resolution, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority})
	if err != nil || resolution.Decision.Kind != Marked {
		t.Fatalf("marked resolve: %#v %v", resolution.Decision, err)
	}
	if states.state.Revision != 7 || len(receipts.values) != 3 {
		t.Fatalf("state/receipts = %d/%d", states.state.Revision, len(receipts.values))
	}
}

func TestObjectiveRevisionInvalidatesPrescriptionBeforeEffects(t *testing.T) {
	runtime, states, _, domain, objective, authority := newIntegerRuntime(t, true)
	ctx := context.Background()
	resolution, _ := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"})
	revised, _ := NewObjective("reach-two", 2, map[string]int{"value": 3})
	binding, _ := BindObjective(revised)
	states.state.ObjectiveBinding = &binding
	_, err := runtime.Apply(ctx, ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-fixture", Objective: &revised, Authority: authority, Requested: "counter.increment-first"}, Prescription: *resolution.Prescription})
	if !IsStale(err) || domain.value != 0 {
		t.Fatalf("err/value = %v/%d", err, domain.value)
	}
}

func TestAuthorityDenialAndObjectiveAbsenceFailClosed(t *testing.T) {
	runtime, _, _, domain, objective, _ := newIntegerRuntime(t, true)
	resolution, err := runtime.Resolve(context.Background(), ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Requested: "counter.increment-first"})
	if err != nil || resolution.Decision.Kind != Frontier {
		t.Fatalf("decision/error = %#v/%v", resolution.Decision, err)
	}
	if domain.value != 0 {
		t.Fatal("refused resolution mutated domain")
	}
}

func TestFutureAuthorityReceiptFailsClosedBeforeEffects(t *testing.T) {
	runtime, _, _, domain, objective, authority := newIntegerRuntime(t, true)
	authority.Receipts[0].IssuedAt = time.Date(2026, 8, 12, 10, 0, 1, 0, time.UTC)
	resolution, err := runtime.Resolve(context.Background(), ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"})
	if err != nil || resolution.Decision.Kind != Refused || domain.value != 0 || domain.incrementExecutions != 0 {
		t.Fatalf("decision/value/executions/error = %#v/%d/%d/%v", resolution.Decision, domain.value, domain.incrementExecutions, err)
	}
}

func TestUntargetedAndTargetedResolutionShareOneRelation(t *testing.T) {
	runtime, _, _, _, objective, authority := newIntegerRuntime(t, true)
	ctx := context.Background()
	untargeted, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority})
	if err != nil || untargeted.Decision.Kind != Prescribed {
		t.Fatalf("untargeted: %#v %v", untargeted.Decision, err)
	}
	targeted, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: untargeted.Decision.Transition})
	if err != nil || targeted.Decision.Kind != Prescribed || targeted.Prescription.ID != untargeted.Prescription.ID {
		t.Fatalf("targeted: %#v %v", targeted.Decision, err)
	}
	if _, err := runtime.Apply(ctx, ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: targeted.Decision.Transition}, Prescription: *targeted.Prescription}); err != nil {
		t.Fatalf("prescribed transition was rejected by apply: %v", err)
	}
}

func TestObservationChangeMakesPrescriptionStaleBeforeOperator(t *testing.T) {
	runtime, _, _, domain, objective, authority := newIntegerRuntime(t, true)
	ctx := context.Background()
	resolution, _ := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"})
	domain.value = 1
	_, err := runtime.Apply(ctx, ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"}, Prescription: *resolution.Prescription})
	if !IsStale(err) || domain.value != 1 {
		t.Fatalf("err/value = %v/%d", err, domain.value)
	}
}

type strictCapabilities struct{ integerCapabilities }

func (strictCapabilities) RequiredCapabilities(transition Transition) ([]Capability, error) {
	base, err := integerCapabilities{}.RequiredCapabilities(transition)
	if transition.Operation == "counter.increment" {
		base = append(base, "counter.audit")
	}
	return base, err
}

func TestTrustedCapabilityClassifierCannotBeWeakenedByProgram(t *testing.T) {
	program := integerProgram(t)
	objective, _ := NewObjective("reach-two", 1, map[string]int{"value": 2})
	binding, _ := BindObjective(objective)
	receipts, domain := &memoryReceipts{}, &integerDomain{}
	states := &memoryStateStore{state: ControlState{InstanceID: "counter-fixture", Program: program.Identity(), ObjectiveBinding: &binding, Mode: "zero", Revision: 1}, receipts: receipts}
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	authority := Authority{Receipts: []AuthorityReceipt{{ID: "program-declared-only", Subject: "fixture", Fingerprint: "authority", Capabilities: []Capability{"counter.increment"}, IssuedAt: now.Add(-time.Minute)}}}
	runtime, err := NewRuntime(program, domain, integerOperator{domain}, strictCapabilities{}, states, &memoryLocker{}, fixedClock{now})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := runtime.Resolve(context.Background(), ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"})
	if err != nil || resolution.Decision.Kind != Frontier || domain.value != 0 {
		t.Fatalf("decision/value/error = %#v/%d/%v", resolution.Decision, domain.value, err)
	}
}

func TestOptionalMaintenancePreservesObjectiveAbsence(t *testing.T) {
	runtime, states, _, domain, _, authority := newIntegerRuntime(t, false)
	states.state.Mode, domain.value = "one", 1
	commandObjective, _ := NewObjective("unbound-command", 1, map[string]int{"value": 0})
	resolution, err := runtime.Resolve(context.Background(), ResolveRequest{InstanceID: "counter-fixture", Objective: &commandObjective, Authority: authority, Requested: "counter.reset"})
	if err != nil || resolution.Decision.Kind != Prescribed {
		t.Fatalf("decision/error = %#v/%v", resolution.Decision, err)
	}
	_, err = runtime.Apply(context.Background(), ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-fixture", Objective: &commandObjective, Authority: authority, Requested: "counter.reset"}, Prescription: *resolution.Prescription})
	if err != nil || states.state.ObjectiveBinding != nil {
		t.Fatalf("maintenance synthesized objective: %#v %v", states.state.ObjectiveBinding, err)
	}
}

func TestOptionalMaintenancePreservesExactBinding(t *testing.T) {
	runtime, states, _, domain, objective, authority := newIntegerRuntime(t, true)
	domain.value = 1
	states.state.Mode = "one"
	conflicting, _ := NewObjective("other", 1, map[string]int{"value": 0})
	before := *states.state.ObjectiveBinding
	resolution, err := runtime.Resolve(context.Background(), ResolveRequest{InstanceID: "counter-fixture", Objective: &conflicting, Authority: authority, Requested: "counter.reset"})
	if err != nil || resolution.Decision.Kind != Prescribed {
		t.Fatalf("decision/error = %#v/%v", resolution.Decision, err)
	}
	_, err = runtime.Apply(context.Background(), ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-fixture", Objective: &conflicting, Authority: authority, Requested: "counter.reset"}, Prescription: *resolution.Prescription})
	if err != nil {
		t.Fatal(err)
	}
	if states.state.ObjectiveBinding == nil || *states.state.ObjectiveBinding != before {
		t.Fatal("maintenance changed objective binding")
	}
	_ = objective
}

func TestInterruptedOperatorRequiresAndCompletesExplicitRecovery(t *testing.T) {
	runtime, states, _, domain, objective, authority := newIntegerRuntime(t, true)
	domain.failAfterIncrement = true
	ctx := context.Background()
	resolution, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"})
	if err != nil || resolution.Decision.Kind != Prescribed {
		t.Fatalf("resolve: %#v %v", resolution.Decision, err)
	}
	_, err = runtime.Apply(ctx, ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"}, Prescription: *resolution.Prescription})
	if !IsRecoveryRequired(err) || states.state.Recovery == nil || states.state.Revision != 2 || domain.value != 1 {
		t.Fatalf("recovery state: %#v value=%d err=%v", states.state, domain.value, err)
	}
	recovery, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority})
	if err != nil || recovery.Decision.Kind != Prescribed || recovery.Decision.Transition != "counter.recover" {
		t.Fatalf("recovery resolve: %#v %v", recovery.Decision, err)
	}
	_, err = runtime.Apply(ctx, ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.recover"}, Prescription: *recovery.Prescription})
	if err != nil || states.state.Recovery != nil || states.state.Revision != 4 || domain.value != 0 {
		t.Fatalf("recovery result: %#v value=%d err=%v", states.state, domain.value, err)
	}
}

func TestAtomicTransactionFailureEntersRecoveryWithoutDuplicateEffect(t *testing.T) {
	runtime, store, receipts, domain, objective, authority := newIntegerRuntime(t, true)
	store.commitFailures = 1
	ctx := context.Background()
	resolution, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"})
	if err != nil || resolution.Decision.Kind != Prescribed {
		t.Fatalf("resolve: %#v %v", resolution.Decision, err)
	}
	_, err = runtime.Apply(ctx, ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"}, Prescription: *resolution.Prescription})
	if !IsRecoveryRequired(err) {
		t.Fatalf("apply error = %v", err)
	}
	if store.state.Mode != "zero" || store.state.Recovery == nil || len(receipts.values) != 0 {
		t.Fatalf("non-atomic store result: state=%#v receipts=%d", store.state, len(receipts.values))
	}
	if domain.value != 1 || domain.incrementExecutions != 1 {
		t.Fatalf("operator value/executions = %d/%d", domain.value, domain.incrementExecutions)
	}
	recovery, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority})
	if err != nil || recovery.Decision.Kind != Prescribed || recovery.Decision.Transition != "counter.recover" {
		t.Fatalf("recovery resolve: %#v %v", recovery.Decision, err)
	}
	if domain.incrementExecutions != 1 {
		t.Fatalf("original operator ran %d times", domain.incrementExecutions)
	}
}

func TestProcessPanicLeavesDurableRecoveryBeforeEffectReplay(t *testing.T) {
	runtime, store, _, domain, objective, authority := newIntegerRuntime(t, true)
	domain.panicAfterIncrement = true
	ctx := context.Background()
	resolution, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"})
	if err != nil || resolution.Decision.Kind != Prescribed {
		t.Fatalf("resolve: %#v %v", resolution.Decision, err)
	}
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = runtime.Apply(ctx, ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"}, Prescription: *resolution.Prescription})
	}()
	if recovered == nil || store.state.Recovery == nil || store.state.Recovery.TransitionID != "counter.increment-first" || store.state.Revision != 2 || domain.value != 1 || domain.incrementExecutions != 1 {
		t.Fatalf("panic/recovery state: panic=%v state=%#v value=%d executions=%d", recovered, store.state, domain.value, domain.incrementExecutions)
	}
	next, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority})
	if err != nil || next.Decision.Kind != Prescribed || next.Decision.Transition != "counter.recover" || domain.incrementExecutions != 1 {
		t.Fatalf("post-panic resolution: %#v executions=%d error=%v", next.Decision, domain.incrementExecutions, err)
	}
}

func TestFailedRecoveryAttemptPreservesOriginalRecoveryObligation(t *testing.T) {
	runtime, store, _, domain, objective, authority := newIntegerRuntime(t, true)
	domain.failAfterIncrement = true
	ctx := context.Background()

	resolution, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"})
	if err != nil || resolution.Decision.Kind != Prescribed {
		t.Fatalf("resolve: %#v %v", resolution.Decision, err)
	}
	_, err = runtime.Apply(ctx, ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"}, Prescription: *resolution.Prescription})
	if !IsRecoveryRequired(err) || store.state.Recovery == nil {
		t.Fatalf("initial recovery: state=%#v err=%v", store.state, err)
	}
	original := *store.state.Recovery

	recovery, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority})
	if err != nil || recovery.Decision.Kind != Prescribed || recovery.Decision.Transition != "counter.recover" {
		t.Fatalf("first recovery resolve: %#v %v", recovery.Decision, err)
	}
	store.commitFailures = 1
	_, err = runtime.Apply(ctx, ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.recover"}, Prescription: *recovery.Prescription})
	if !IsRecoveryRequired(err) || store.state.Recovery == nil || *store.state.Recovery != original || domain.value != 0 {
		t.Fatalf("failed recovery attempt: state=%#v value=%d err=%v", store.state, domain.value, err)
	}

	retry, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority})
	if err != nil || retry.Decision.Kind != Prescribed || retry.Decision.Transition != "counter.recover" {
		t.Fatalf("recovery retry resolve: %#v %v", retry.Decision, err)
	}
	_, err = runtime.Apply(ctx, ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.recover"}, Prescription: *retry.Prescription})
	if err != nil || store.state.Recovery != nil || store.state.Mode != "zero" || store.state.Revision != 5 {
		t.Fatalf("recovery retry result: state=%#v err=%v", store.state, err)
	}
}

func TestPrescriptionCannotReplayAcrossControlInstances(t *testing.T) {
	runtime, store, _, domain, objective, authority := newIntegerRuntime(t, true)
	ctx := context.Background()
	resolution, err := runtime.Resolve(ctx, ResolveRequest{InstanceID: "counter-fixture", Objective: &objective, Authority: authority, Requested: "counter.increment-first"})
	if err != nil || resolution.Decision.Kind != Prescribed {
		t.Fatalf("resolve: %#v %v", resolution.Decision, err)
	}
	store.state.InstanceID = "counter-other"
	_, err = runtime.Apply(ctx, ApplyRequest{ResolveRequest: ResolveRequest{InstanceID: "counter-other", Objective: &objective, Authority: authority, Requested: "counter.increment-first"}, Prescription: *resolution.Prescription})
	if !IsStale(err) || domain.value != 0 || domain.incrementExecutions != 0 {
		t.Fatalf("cross-instance apply: value=%d executions=%d err=%v", domain.value, domain.incrementExecutions, err)
	}
}

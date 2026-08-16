package invocation

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
)

type testRuntimeStore struct{}

func (testRuntimeStore) EnsureDirectory(path string, mode uint32) error {
	return os.MkdirAll(path, os.FileMode(mode))
}

func (testRuntimeStore) WriteAtomic(path string, raw []byte, mode uint32) error {
	return os.WriteFile(path, raw, os.FileMode(mode))
}

func testContext() Context {
	return Context{RunID: "run-one", ProgramFingerprint: strings.Repeat("a", 64), ExecutionProgramFingerprint: strings.Repeat("e", 64), EntryID: "run", TargetID: "mitigated", TransitionID: "respond", StateRevision: 12, ContextFingerprint: strings.Repeat("b", 64), ControlBundleFingerprint: strings.Repeat("c", 64), ExecutionScopeFingerprint: strings.Repeat("d", 64), InputReceipts: map[string]InputReceipt{}}
}

func testContract() controlprogram.OperatorParameter {
	return controlprogram.OperatorParameter{ID: "channel", Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Required: true, AllowedSources: []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceHostInput}, Authority: controlprogram.AuthorityRequirement{AnyOf: []string{"human"}}}
}

func testProducer() controlprogram.ParameterProducer {
	return controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceHostInput, Request: &controlprogram.HostInputRequest{ID: "channel", Description: "Select the response channel.", Authorities: []string{"human"}, Scope: "transition"}}
}

func TestHostInputSuspendsAndSameRunReceiptResumes(t *testing.T) {
	// control-law: declared-missing-host-input-suspends-without-guessing-and-resumes-only-the-bound-run
	context := testContext()
	bindings := []controlprogram.TransitionParameterBinding{{Parameter: "channel", Producer: testProducer()}}
	result, err := Materialize([]controlprogram.OperatorParameter{testContract()}, bindings, context, nil)
	if err != nil || result.Request == nil || result.Request.Code != "TRANSITION_INPUT_REQUIRED" || result.Ready != nil {
		t.Fatalf("suspension = %#v, %v", result, err)
	}
	receipt, err := SealReceipt(InputReceipt{RunID: context.RunID, ProgramFingerprint: context.ProgramFingerprint, ExecutionProgramFingerprint: context.ExecutionProgramFingerprint, EntryID: context.EntryID, TargetID: context.TargetID, TransitionID: context.TransitionID, ParameterID: "channel", Type: testContract().Type, Value: "pager", ProducerFingerprint: fingerprintProducer(testProducer()), RequestFingerprint: result.Request.Fingerprint, StateRevision: context.StateRevision, ContextFingerprint: context.ContextFingerprint, ControlBundleFingerprint: context.ControlBundleFingerprint, ExecutionScopeFingerprint: context.ExecutionScopeFingerprint, Actor: "operator", Host: "codex", AuthorityReceipts: []string{"human:operator"}, CreatedAt: time.Now().UTC(), Scope: "transition"})
	if err != nil {
		t.Fatal(err)
	}
	context.InputReceipts["channel@"+result.Request.Fingerprint] = receipt
	resumed, err := Materialize([]controlprogram.OperatorParameter{testContract()}, bindings, context, nil)
	if err != nil || resumed.Ready == nil || resumed.Ready.Parameters[0].Value != "pager" || resumed.Request != nil {
		t.Fatalf("resumed = %#v, %v", resumed, err)
	}
	changed := context
	changed.RunID = "run-other"
	replayed, err := Materialize([]controlprogram.OperatorParameter{testContract()}, bindings, changed, nil)
	if err != nil || replayed.Request == nil || replayed.Ready != nil {
		t.Fatalf("cross-run replay = %#v, %v", replayed, err)
	}
}

func TestInvocationFingerprintChangesWithProducerOrContext(t *testing.T) {
	contract := testContract()
	contract.Authority = controlprogram.AuthorityRequirement{}
	contract.AllowedSources = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceEntryInput}
	binding := controlprogram.TransitionParameterBinding{Parameter: "channel", Producer: controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceEntryInput, Input: "channel"}}
	context := testContext()
	context.EntryInputs = map[string]Value{"channel": {Type: contract.Type, Canonical: "pager", Provenance: "entry"}}
	one, _ := Materialize([]controlprogram.OperatorParameter{contract}, []controlprogram.TransitionParameterBinding{binding}, context, nil)
	context.ContextFingerprint = strings.Repeat("d", 64)
	two, _ := Materialize([]controlprogram.OperatorParameter{contract}, []controlprogram.TransitionParameterBinding{binding}, context, nil)
	if one.Ready == nil || two.Ready == nil || one.Ready.InvocationFingerprint == two.Ready.InvocationFingerprint {
		t.Fatal("context drift preserved invocation fingerprint")
	}
	context.ContextFingerprint = strings.Repeat("b", 64)
	context.ExecutionProgramFingerprint = strings.Repeat("f", 64)
	three, _ := Materialize([]controlprogram.OperatorParameter{contract}, []controlprogram.TransitionParameterBinding{binding}, context, nil)
	if three.Ready == nil || one.Ready.InvocationFingerprint == three.Ready.InvocationFingerprint {
		t.Fatal("executable program drift preserved invocation fingerprint")
	}
}

func TestZeroParameterTransitionStillProducesInvocationEvidence(t *testing.T) {
	// control-law: zero-runtime-parameters-do-not-mean-zero-transition-invocation
	result, err := Materialize(nil, nil, testContext(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Ready == nil || result.Ready.InvocationFingerprint == "" || result.Request != nil || result.Blocker != nil {
		t.Fatalf("zero-parameter invocation = %#v", result)
	}
	if result.Ready.Parameters != nil {
		t.Fatalf("zero-parameter invocation exposed parameters: %#v", result.Ready.Parameters)
	}
}

func TestStateOrReceiptMaterializesEitherExactAlternative(t *testing.T) {
	// control-law: one canonical producer can explicitly cover normal receipt
	// output and recovery-established durable state without inferring either.
	contract := testContract()
	contract.Authority = controlprogram.AuthorityRequirement{}
	contract.AllowedSources = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceStateOrReceipt}
	producer := controlprogram.ParameterProducer{
		Kind: controlprogram.ParameterSourceStateOrReceipt, Facet: "channel",
		AvailableWhen: &controlprogram.Predicate{Fact: &controlprogram.FactPredicate{Facet: "channel", Statuses: []string{"known"}}},
		Transition:    "observe-channel", Field: "channel",
	}
	binding := []controlprogram.TransitionParameterBinding{{Parameter: "channel", Producer: producer}}

	receiptContext := testContext()
	receiptContext.Receipts = map[string]Value{"observe-channel/channel": {Type: contract.Type, Canonical: "pager", Provenance: "transition-receipt:receipt-channel"}}
	receiptResult, err := Materialize([]controlprogram.OperatorParameter{contract}, binding, receiptContext, nil)
	if err != nil || receiptResult.Ready == nil || receiptResult.Ready.Parameters[0].Value != "pager" {
		t.Fatalf("receipt alternative = %#v, %v", receiptResult, err)
	}

	stateContext := testContext()
	stateContext.State = map[string]Value{"channel": {Type: contract.Type, Canonical: "chat", Provenance: "durable-state:channel"}}
	stateResult, err := Materialize([]controlprogram.OperatorParameter{contract}, binding, stateContext, nil)
	if err != nil || stateResult.Ready == nil || stateResult.Ready.Parameters[0].Value != "chat" {
		t.Fatalf("state alternative = %#v, %v", stateResult, err)
	}

	missing, err := Materialize([]controlprogram.OperatorParameter{contract}, binding, testContext(), nil)
	if err != nil || missing.Blocker == nil || missing.Blocker.Code != "TRANSITION_INPUT_UNAVAILABLE" {
		t.Fatalf("missing alternatives = %#v, %v", missing, err)
	}
}

func TestInputReceiptRejectsCrossScopeExpiryAndControlDrift(t *testing.T) {
	context := testContext()
	binding := []controlprogram.TransitionParameterBinding{{Parameter: "channel", Producer: testProducer()}}
	suspended, err := Materialize([]controlprogram.OperatorParameter{testContract()}, binding, context, nil)
	if err != nil || suspended.Request == nil {
		t.Fatalf("suspension = %#v, %v", suspended, err)
	}
	base, err := SealReceipt(InputReceipt{
		RunID: context.RunID, ProgramFingerprint: context.ProgramFingerprint, ExecutionProgramFingerprint: context.ExecutionProgramFingerprint, EntryID: context.EntryID, TargetID: context.TargetID,
		TransitionID: context.TransitionID, ParameterID: "channel", Type: testContract().Type, Value: "pager",
		ProducerFingerprint: fingerprintProducer(testProducer()), RequestFingerprint: suspended.Request.Fingerprint,
		StateRevision: context.StateRevision, ContextFingerprint: context.ContextFingerprint, ControlBundleFingerprint: context.ControlBundleFingerprint,
		ExecutionScopeFingerprint: context.ExecutionScopeFingerprint,
		Actor:                     "operator", Host: "codex", AuthorityReceipts: []string{"human:operator"}, Scope: "transition",
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Context){
		"entry":             func(value *Context) { value.EntryID = "alternate" },
		"execution-program": func(value *Context) { value.ExecutionProgramFingerprint = strings.Repeat("f", 64) },
		"target":            func(value *Context) { value.TargetID = "alternate" },
		"transition":        func(value *Context) { value.TransitionID = "alternate" },
		"state":             func(value *Context) { value.StateRevision++ },
		"context":           func(value *Context) { value.ContextFingerprint = strings.Repeat("d", 64) },
		"control-bundle":    func(value *Context) { value.ControlBundleFingerprint = strings.Repeat("e", 64) },
		"execution-scope":   func(value *Context) { value.ExecutionScopeFingerprint = strings.Repeat("f", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := context
			changed.InputReceipts = map[string]InputReceipt{"channel@" + suspended.Request.Fingerprint: base}
			mutate(&changed)
			result, materializeErr := Materialize([]controlprogram.OperatorParameter{testContract()}, binding, changed, nil)
			if materializeErr != nil || result.Request == nil || result.Ready != nil {
				t.Fatalf("cross-scope result = %#v, %v", result, materializeErr)
			}
		})
	}
	expired := base
	expired.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	expired.Fingerprint = ""
	expired, _ = SealReceipt(expired)
	context.InputReceipts = map[string]InputReceipt{"channel@" + suspended.Request.Fingerprint: expired}
	result, err := Materialize([]controlprogram.OperatorParameter{testContract()}, binding, context, nil)
	if err != nil || result.Blocker == nil || !strings.Contains(result.Blocker.Detail, "expired") {
		t.Fatalf("expired result = %#v, %v", result, err)
	}
}

func TestInputReceiptCannotClaimUnrecordedParameterAuthority(t *testing.T) {
	context := testContext()
	binding := []controlprogram.TransitionParameterBinding{{Parameter: "channel", Producer: testProducer()}}
	suspended, err := Materialize([]controlprogram.OperatorParameter{testContract()}, binding, context, nil)
	if err != nil || suspended.Request == nil {
		t.Fatalf("suspension = %#v, %v", suspended, err)
	}
	receipt, err := SealReceipt(InputReceipt{
		RunID: context.RunID, ProgramFingerprint: context.ProgramFingerprint, ExecutionProgramFingerprint: context.ExecutionProgramFingerprint,
		EntryID: context.EntryID, TargetID: context.TargetID, TransitionID: context.TransitionID, ParameterID: "channel",
		Type: testContract().Type, Value: "pager", ProducerFingerprint: fingerprintProducer(testProducer()), RequestFingerprint: suspended.Request.Fingerprint,
		StateRevision: context.StateRevision, ContextFingerprint: context.ContextFingerprint, ControlBundleFingerprint: context.ControlBundleFingerprint,
		ExecutionScopeFingerprint: context.ExecutionScopeFingerprint, Actor: "automation", Host: "codex",
		AuthorityReceipts: []string{"autonomy:automation"}, Scope: "transition",
	})
	if err != nil {
		t.Fatal(err)
	}
	context.InputReceipts["channel@"+suspended.Request.Fingerprint] = receipt
	result, err := Materialize([]controlprogram.OperatorParameter{testContract()}, binding, context, nil)
	if err != nil || result.Blocker == nil || !strings.Contains(result.Blocker.Detail, "parameter authority") {
		t.Fatalf("wrong-authority input receipt = %#v, %v", result, err)
	}
}

func TestEvidenceValidationRejectsNonCanonicalParameters(t *testing.T) {
	context := testContext()
	contract := testContract()
	contract.Authority = controlprogram.AuthorityRequirement{}
	contract.AllowedSources = []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceEntryInput}
	context.EntryInputs = map[string]Value{"channel": {Type: contract.Type, Canonical: "pager", Provenance: "entry"}}
	result, err := Materialize([]controlprogram.OperatorParameter{contract}, []controlprogram.TransitionParameterBinding{{Parameter: "channel", Producer: controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceEntryInput, Input: "channel"}}}, context, nil)
	if err != nil || result.Ready == nil || result.Ready.Validate() != nil {
		t.Fatalf("valid evidence = %#v, %v", result, err)
	}
	changed := *result.Ready
	changed.Parameters[0].ValueFingerprint = strings.Repeat("f", 64)
	changed.InvocationFingerprint = fingerprintWithoutField(changed, "InvocationFingerprint")
	if err := changed.Validate(); err == nil || !strings.Contains(err.Error(), "value identity") {
		t.Fatalf("noncanonical parameter result = %v", err)
	}
}

func TestStoreIsIdempotentAndRejectsConflictingAnswers(t *testing.T) {
	store := Store{Root: t.TempDir(), Writer: testRuntimeStore{}}
	receipt, _ := SealReceipt(InputReceipt{RunID: "run", ProgramFingerprint: strings.Repeat("a", 64), ExecutionProgramFingerprint: strings.Repeat("f", 64), EntryID: "run", TargetID: "target", TransitionID: "respond", ParameterID: "channel", Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Value: "pager", ValueFingerprint: digest("pager\x00"), ProducerFingerprint: strings.Repeat("b", 64), RequestFingerprint: strings.Repeat("c", 64), StateRevision: 1, ContextFingerprint: strings.Repeat("d", 64), ExecutionScopeFingerprint: strings.Repeat("e", 64), Actor: "operator", Host: "codex", Scope: "transition"})
	if err := store.SaveReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	conflict := receipt
	conflict.Value = "chat"
	conflict, _ = SealReceipt(conflict)
	if err := store.SaveReceipt(conflict); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("conflicting answer result = %v", err)
	}
}

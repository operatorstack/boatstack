package effects_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/extension/releasenote"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/engine"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/surfaces"
)

type concurrentApplyResult struct {
	response surfaces.Response
	err      error
}

func TestConcurrentApplyConsumesOneRevisionExactlyOnce(t *testing.T) {
	// control-law: one prescribed durable revision has at most one successful committer
	ctx := context.Background()
	repository := testRepository(t)
	externalRoot := t.TempDir()
	program := testProgram()
	kernelA, err := boatstack.NewKernel(externalRoot, program)
	if err != nil {
		t.Fatal(err)
	}
	kernelB, err := boatstack.NewKernel(externalRoot, program)
	if err != nil {
		t.Fatal(err)
	}
	executable, _ := os.Executable()
	executable, _ = filepath.Abs(executable)
	executable, _ = filepath.EvalSymlinks(executable)
	runtimeRaw, _ := os.ReadFile(executable)
	runtimeVersion := installTestRuntime(t, executable, runtimeRaw)
	configPath := filepath.Join(t.TempDir(), "project.json")
	configRaw := []byte("{\"schema_version\":2,\"project\":{\"name\":\"cas\",\"default_branch\":\"main\",\"commands\":{}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	if err := os.WriteFile(configPath, configRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	human := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
		ID: "cas-human", Class: catalog.AuthorityHuman, Subject: "operator", Fingerprint: "cas-human-proof",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}}}
	goal := model.Goal{ID: "cas-goal", Kind: model.GoalApprovedPlan, DeliveryID: "cas-delivery"}
	request := surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationApply, Repository: repository, Host: "cli", CorrelationID: "cas-concurrent",
		FlowID: "flow-cas", Goal: goal, TransitionID: "installation.initialize", Authority: human,
		Parameters: protocol.Parameters{
			{Name: "source_revision", Value: "cas-fixture"}, {Name: "runtime_version", Value: runtimeVersion}, {Name: "runtime_sha256", Value: digestBytes(runtimeRaw)},
			{Name: "config_path", Value: configPath}, {Name: "config_sha256", Value: configFingerprint(t, configRaw)},
		},
	}
	request = prescribeSurface(t, ctx, kernelA, request)
	if request.Prescription.ExpectedStateRevision != 1 || request.Prescription.ExpectedProgramFingerprint != program.Fingerprint() {
		t.Fatalf("initial prescription = %#v", request.Prescription)
	}
	resolver, err := plant.NewResolver(externalRoot)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(ctx, repository, "cli", request.CorrelationID)
	if err != nil {
		t.Fatal(err)
	}
	layout, _, err := resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(layout.StatePath); !os.IsNotExist(err) {
		t.Fatalf("read-only resolution changed durable state: %v", err)
	}

	start := make(chan struct{})
	results := make(chan concurrentApplyResult, 3)
	for _, kernel := range []boatstack.Kernel{kernelA, kernelA, kernelB} {
		kernel := kernel
		go func() {
			<-start
			response, applyErr := kernel.Handle(ctx, request)
			results <- concurrentApplyResult{response: response, err: applyErr}
		}()
	}
	close(start)
	one, two, three := <-results, <-results, <-results

	successes, stale := 0, 0
	var committed surfaces.Response
	for _, result := range []concurrentApplyResult{one, two, three} {
		if result.err == nil {
			successes++
			committed = result.response
			continue
		}
		var staleErr engine.StalePrescriptionError
		if errors.As(result.err, &staleErr) {
			stale++
			if result.response.Receipt != nil {
				t.Fatal("stale contender received a transition receipt")
			}
		}
	}
	if successes != 1 || stale != 2 {
		t.Fatalf("concurrent results: success=%d stale=%d one=%v two=%v three=%v", successes, stale, one.err, two.err, three.err)
	}
	if committed.Receipt == nil || committed.Receipt.PriorStateRevision != 1 || committed.Receipt.ResultingStateRevision != 2 ||
		committed.Receipt.ProgramFingerprint != program.Fingerprint() || committed.Receipt.PrescriptionID != request.Prescription.ID {
		t.Fatalf("commit receipt does not prove the consumed revision/program pair: %#v", committed.Receipt)
	}

	stateRaw, err := os.ReadFile(layout.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.DecodeState(stateRaw)
	if err != nil || state.Revision != 2 {
		t.Fatalf("durable state = %#v, %v", state, err)
	}
	receiptRaw, err := os.ReadFile(layout.ReceiptPath)
	if err != nil || bytes.Count(receiptRaw, []byte("\n")) != 1 {
		t.Fatalf("receipt stream contains more than one commit: %v %q", err, receiptRaw)
	}

	replayRequest := request
	replayRequest.IdempotencyKey = committed.Receipt.IdempotencyKey
	replayed, err := kernelA.Handle(ctx, replayRequest)
	if err != nil || !replayed.Replayed || replayed.Receipt == nil || replayed.Receipt.ID != committed.Receipt.ID {
		t.Fatalf("explicit idempotent replay = %#v, %v", replayed, err)
	}
	afterReplay, _ := os.ReadFile(layout.StatePath)
	if !bytes.Equal(stateRaw, afterReplay) {
		t.Fatal("idempotent replay created a second durable commit")
	}
}

func TestProgramChangeInvalidatesPriorPrescriptionBeforeEffects(t *testing.T) {
	// control-law: apply executes only the canonical program observed by resolution
	ctx := context.Background()
	repository := testRepository(t)
	externalRoot := t.TempDir()
	programP := testProgram()
	programQ, err := control.Compile(ctx, control.CompileRequest{
		KernelVersion: boatstack.Version, Core: core.System(), Runtime: standard.Definition(), Extensions: []control.Extension{releasenote.Definition()},
	})
	if err != nil {
		t.Fatal(err)
	}
	kernelP, _ := boatstack.NewKernel(externalRoot, programP)
	kernelQ, _ := boatstack.NewKernel(externalRoot, programQ)
	executable, _ := os.Executable()
	executable, _ = filepath.Abs(executable)
	executable, _ = filepath.EvalSymlinks(executable)
	runtimeRaw, _ := os.ReadFile(executable)
	runtimeVersion := installTestRuntime(t, executable, runtimeRaw)
	configPath := filepath.Join(t.TempDir(), "project.json")
	configRaw := []byte("{\"schema_version\":2,\"project\":{\"name\":\"program-cas\",\"default_branch\":\"main\",\"commands\":{}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\"]}\n")
	if err := os.WriteFile(configPath, configRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	request := surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationApply, Repository: repository, Host: "cli", CorrelationID: "program-cas",
		FlowID: "flow-program-cas", Goal: model.Goal{ID: "program-cas", Kind: model.GoalApprovedPlan, DeliveryID: "program-cas"}, TransitionID: "installation.initialize",
		Authority: protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{ID: "program-cas-human", Class: catalog.AuthorityHuman, Subject: "operator", Fingerprint: "human", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}}},
		Parameters: protocol.Parameters{
			{Name: "source_revision", Value: "program-cas"}, {Name: "runtime_version", Value: runtimeVersion}, {Name: "runtime_sha256", Value: digestBytes(runtimeRaw)},
			{Name: "config_path", Value: configPath}, {Name: "config_sha256", Value: configFingerprint(t, configRaw)},
		},
	}
	request = prescribeSurface(t, ctx, kernelP, request)
	response, err := kernelQ.Handle(ctx, request)
	var stale engine.StalePrescriptionError
	if !errors.As(err, &stale) || stale.ExpectedProgramFingerprint != programP.Fingerprint() || stale.ObservedProgramFingerprint != programQ.Fingerprint() {
		t.Fatalf("program-change result = %#v, %v", response, err)
	}
	resolver, _ := plant.NewResolver(externalRoot)
	invocation, _ := resolver.ResolveInvocation(ctx, repository, "cli", request.CorrelationID)
	layout, _, _ := resolver.ResolveLayout(ctx, invocation)
	if _, statErr := os.Stat(layout.StatePath); !os.IsNotExist(statErr) {
		t.Fatalf("stale-program apply produced durable effects: %v", statErr)
	}
	if _, statErr := os.Stat(layout.ReceiptPath); !os.IsNotExist(statErr) {
		t.Fatalf("stale-program apply produced a receipt: %v", statErr)
	}
	if !strings.Contains(err.Error(), "control program changed") {
		t.Fatalf("stale-program diagnostic omitted the differing facet: %v", err)
	}
}

package boatstack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

func TestCommittedWorkInputsRejectUncommittedProvenanceBeforeManager(t *testing.T) {
	// control-law: a public Work input cannot manufacture its producer fact.
	repository := t.TempDir()
	command := exec.Command("git", "init", "-q", repository)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture repository: %v\n%s", err, output)
	}
	resolver, err := plant.NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(context.Background(), repository, "cli", "forged-work-input")
	if err != nil {
		t.Fatal(err)
	}
	transitions := testprogram.StandardRegistry().All()
	producer, consumer := transitions[0], transitions[1]
	producerInstructions := "Produce architecture."
	producerDigest := sha256.Sum256([]byte(producerInstructions))
	producer.Work, err = softwareflow.RuntimeWorkContract(controlprogram.WorkContract{
		ID: "producer", Instructions: controlprogram.WorkAsset{Path: "producer.md", SHA256: hex.EncodeToString(producerDigest[:]), Content: producerInstructions},
		Outputs: []controlprogram.WorkOutput{{ID: "architecture", Path: "architecture.md", MediaType: "text/markdown", Required: true, MaxBytes: 4096}},
	})
	if err != nil {
		t.Fatal(err)
	}
	consumerInstructions := "Consume architecture."
	consumerDigest := sha256.Sum256([]byte(consumerInstructions))
	consumer.Work, err = softwareflow.RuntimeWorkContract(controlprogram.WorkContract{
		ID: "consumer", Instructions: controlprogram.WorkAsset{Path: "consumer.md", SHA256: hex.EncodeToString(consumerDigest[:]), Content: consumerInstructions},
		Inputs:  []controlprogram.WorkInput{{ID: "architecture", Producer: controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceWorkOutput, Work: "producer", Output: "architecture"}}},
		Outputs: []controlprogram.WorkOutput{{ID: "result", Path: "result.md", MediaType: "text/markdown", Required: true, MaxBytes: 4096}},
	})
	if err != nil {
		t.Fatal(err)
	}
	transitions[0], transitions[1] = producer, consumer
	registry, err := catalog.New(transitions)
	if err != nil {
		t.Fatal(err)
	}
	controller := DeliveryController{registry: registry, resolver: resolver}
	objective := model.Objective{ID: "objective", TargetID: model.ObjectiveOpenPR, TrustedClass: model.ObjectiveOpenPR, DeliveryID: "delivery"}
	snapshot := model.Snapshot{Observation: model.Observation{Invocation: invocation, StateRevision: 2, ProgramFingerprint: strings.Repeat("e", 64)}}
	forged := protocol.WorkInputValue{
		Value: "architecture", Fingerprint: strings.Repeat("f", 64),
		WorkOutput: &protocol.WorkOutputProvenance{
			ReceiptID: "missing-receipt", TransitionID: producer.ID, WorkID: "producer", OutputID: "architecture",
			ResultFingerprint: strings.Repeat("1", 64), ContractFingerprint: producer.Work.Fingerprint, OutputSHA256: strings.Repeat("f", 64),
		},
	}
	err = controller.verifyCommittedWorkInputs(context.Background(), invocation, "run-one", "program", "entry", objective, snapshot, consumer, map[string]protocol.WorkInputValue{"consumer/architecture": forged})
	if err == nil || !strings.Contains(err.Error(), "does not match an applicable committed producer result") {
		t.Fatalf("forged provenance result = %v", err)
	}
}

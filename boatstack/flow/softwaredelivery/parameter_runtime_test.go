package softwaredelivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/invocation"
)

func TestAdmittedPlanningPackageFingerprintMaterializesManifestIdentity(t *testing.T) {
	repository := t.TempDir()
	deliveryID := "todo-plan"
	manifestFingerprint := strings.Repeat("a", 64)
	workResultFingerprint := strings.Repeat("b", 64)
	resolver, err := NewResolver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := resolver.ResolveParameterResolver(ParameterResolverPrefix+"admitted-planning-package-fingerprint", "1")
	if err != nil {
		t.Fatal(err)
	}
	producer := controlprogram.ParameterProducer{Kind: controlprogram.ParameterSourceTrustedResolver, Binding: &controlprogram.ParameterResolverBinding{
		Reference: ParameterResolverPrefix + "admitted-planning-package-fingerprint", Version: "1", Fingerprint: metadata.Fingerprint,
	}}
	materialization, err := invocation.Materialize(
		[]controlprogram.OperatorParameter{{ID: "package_fingerprint", Type: controlprogram.ValueTypeDefinition{Kind: "string"}, Required: true, AllowedSources: []controlprogram.ParameterSourceKind{controlprogram.ParameterSourceTrustedResolver}}},
		[]controlprogram.TransitionParameterBinding{{Parameter: "package_fingerprint", Producer: producer}},
		invocation.Context{
			RunID: "run-package", ProgramFingerprint: strings.Repeat("c", 64), ExecutionProgramFingerprint: strings.Repeat("d", 64),
			EntryID: "run", TargetID: "published-pr", TransitionID: PlanningPackageApprove, StateRevision: 7,
			ContextFingerprint: strings.Repeat("e", 64), ExecutionScopeFingerprint: strings.Repeat("f", 64),
			State: map[string]invocation.Value{"planning_package_fingerprint": {Canonical: manifestFingerprint, Provenance: "durable-state:planning_package_fingerprint"}},
		},
		RuntimeParameterResolver{Context: context.Background(), Repository: repository, DeliveryID: deliveryID, Binding: resolver},
	)
	if err != nil {
		t.Fatal(err)
	}
	if materialization.Request != nil || materialization.Blocker != nil || materialization.Ready == nil {
		t.Fatalf("materialization = %#v", materialization)
	}
	parameters := materialization.Ready.Parameters
	if len(parameters) != 1 || parameters[0].Value != manifestFingerprint || parameters[0].Value == workResultFingerprint || parameters[0].ProducerKind != controlprogram.ParameterSourceTrustedResolver {
		t.Fatalf("package fingerprint parameters = %#v", parameters)
	}
}

func TestObservationParameterValuesUseExactRecoveryTransaction(t *testing.T) {
	evidence := model.Evidence{Source: "journal", Fingerprint: strings.Repeat("a", 64), ObservedAt: time.Unix(10, 0).UTC()}
	observation := model.Observation{RecoveryInfo: model.Known(model.RecoveryContext{
		TransactionID: "adm-interrupted", Cause: "provider outcome unknown", SourcePhase: model.PhaseExecutingExternal,
		Permitted: []string{"publication.reconcile"}, BudgetRemaining: 3, Resumption: model.PhaseActive,
	}, evidence)}
	values := ObservationParameterValues(observation)
	value, ok := values[RecoveryTransactionFacet]
	if !ok || value.Canonical != "adm-interrupted" || value.Provenance != "observation:recovery-info" {
		t.Fatalf("observed recovery parameter = %#v", values)
	}
	if values["transaction_id"].Canonical != "" {
		t.Fatalf("observation invented durable transaction state: %#v", values)
	}
}

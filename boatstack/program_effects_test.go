package boatstack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/engine"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
)

type protocolStateRuntime struct{}

func (protocolStateRuntime) ProgramRuntime() delivery.ProgramRuntime { return protocolStateRuntime{} }

func (protocolStateRuntime) InvokeProgram(_ context.Context, request delivery.ProgramRuntimeRequest) (delivery.ProgramRuntimeResponse, error) {
	response := delivery.ProgramRuntimeResponse{
		ProtocolVersion: delivery.ProgramRuntimeProtocolVersion,
		Operation:       request.Operation,
		ProgramID:       request.ProgramID,
		ProgramVersion:  request.ProgramVersion,
		CorrelationID:   request.CorrelationID,
	}
	if request.Operation == delivery.ProgramPlanLocalEffectOperation || request.Operation == delivery.ProgramRecoverOperation {
		content := []byte("protocol-state\n")
		digest := sha256.Sum256(content)
		response.Writes = []delivery.ResourceWrite{{
			Resource: "fixture.state.resource",
			Path:     filepath.Join(request.RepositoryRoot, ".boatstack", "flows", request.ProgramID, "state.json"),
			Content:  content,
			SHA256:   hex.EncodeToString(digest[:]),
		}}
	}
	return response, nil
}

func (protocolStateRuntime) RuntimeManifest(context.Context) (delivery.ProgramRuntimeManifest, error) {
	const (
		programID = "fixture.state"
		resource  = "fixture.state.resource"
	)
	published := string(model.DeliveryPublished)
	noneRecovery, noneTransaction, active := string(model.RecoveryNone), string(model.TransactionNone), string(model.PhaseActive)
	interruption := func(recovery delivery.TransitionID) delivery.InterruptionContract {
		return delivery.InterruptionContract{
			Points: []string{"after-effect"}, PartialState: []string{"declared-state"}, Detection: "fresh-observation",
			ResumeContract: "resume", RollbackContract: "rollback", CompensationContract: "not-required",
			Recovery: recovery, RecoveryAuthority: "repository-policy", ResumptionPredicate: "fresh-state",
		}
	}
	recoverID := delivery.TransitionID(programID + ".recover")
	publishID := delivery.TransitionID(programID + ".publish")
	recoverEffect, publishEffect := delivery.EffectID(recoverID), delivery.EffectID(publishID)
	recover := delivery.Transition{
		ID: recoverID, Version: 1, SelectionClass: delivery.SelectionProgramRecovery, Class: delivery.EventRecovery,
		SourcePhases: []delivery.ProtocolPhase{delivery.PhaseRecovery}, TargetPhases: []delivery.ProtocolPhase{delivery.PhaseActive},
		RequiredIdentity: []string{"repository-id", "git-common-id", "worktree-id"}, Authority: []delivery.AuthorityClass{delivery.AuthorityRepository},
		RequiredCapabilities: []delivery.Capability{delivery.CapabilityRepositoryWrite, delivery.CapabilityCommandExecute}, RequiredEvidence: []string{"snapshot"},
		OwnedResources: []string{resource}, OwnedFacets: []delivery.StateFacet{delivery.StateFacetControl},
		StateEffect: delivery.StateEffect{Kind: delivery.StateEffectAssignments, Assignments: []delivery.StateAssignment{
			{Facet: "phase", Value: &active}, {Facet: "recovery", Value: &noneRecovery}, {Facet: "transaction", Value: &noneTransaction},
		}},
		Effect: recoverEffect, LocalEffects: []delivery.EffectID{recoverEffect}, Idempotent: true,
		Prescription:    delivery.Prescription{Operation: string(recoverID), ExpectedPostcondition: "active"},
		SourcePredicate: "recovery-required", SourceConditions: []delivery.FacetCondition{delivery.KnownCondition(delivery.FacetRecovery, string(model.RecoveryReconcile))},
		AdmissionPredicate: "exact-admission", TargetPredicate: "active", TargetConditions: []delivery.FacetCondition{
			delivery.KnownCondition(delivery.FacetRecovery, string(model.RecoveryNone)), delivery.KnownCondition(delivery.FacetTransaction, string(model.TransactionNone)),
		},
		Verifier: "fixture.state.recovered", Interruption: interruption(recoverID), Reversibility: delivery.Reversible, TerminalEffect: "none",
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt", CostClass: "local",
		Policy: delivery.PolicyContract{ObjectiveScope: delivery.ObjectiveScopeOptionalPreserve}, Priority: 1,
	}
	publish := delivery.Transition{
		ID: publishID, Version: 1, SelectionClass: delivery.SelectionProgramProgress, Class: delivery.EventOwnedLocal,
		SourcePhases: []delivery.ProtocolPhase{delivery.PhaseActive}, TargetPhases: []delivery.ProtocolPhase{delivery.PhaseActive},
		TargetIDs: []delivery.TargetID{delivery.ObjectiveVerified}, RequiredIdentity: []string{"repository-id", "git-common-id", "worktree-id"},
		Authority:            []delivery.AuthorityClass{delivery.AuthorityHuman, delivery.AuthorityRepository},
		RequiredCapabilities: []delivery.Capability{delivery.CapabilityRepositoryWrite, delivery.CapabilityCommandExecute, delivery.CapabilityProductMutate}, RequiredEvidence: []string{"snapshot", "objective"},
		OwnedResources: []string{resource}, OwnedFacets: []delivery.StateFacet{delivery.StateFacetControl, delivery.StateFacetProduct},
		StateEffect: delivery.StateEffect{Kind: delivery.StateEffectAssignments, Assignments: []delivery.StateAssignment{{Facet: "delivery", Value: &published}}},
		Effect:      publishEffect, LocalEffects: []delivery.EffectID{publishEffect}, Idempotent: true,
		Prescription:    delivery.Prescription{Operation: string(publishID), ExpectedPostcondition: "published"},
		SourcePredicate: "active", SourceConditions: []delivery.FacetCondition{
			delivery.KnownCondition(delivery.FacetProgram, string(model.ProgramCurrent)), delivery.KnownCondition(delivery.FacetDelivery, string(model.DeliveryActive)),
		},
		AdmissionPredicate: "exact-admission", TargetPredicate: "published", TargetConditions: []delivery.FacetCondition{delivery.KnownCondition(delivery.FacetDelivery, string(model.DeliveryPublished))},
		Verifier: "fixture.state.published", Interruption: interruption(recoverID), Reversibility: delivery.Reversible, TerminalEffect: "none",
		PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt", CostClass: "local",
		Policy: delivery.PolicyContract{ObjectiveScope: delivery.ObjectiveScopeBoundExact}, Priority: 2,
	}
	return delivery.ProgramRuntimeManifest{
		ID: programID, Version: "1.0.0", ProtocolVersion: delivery.ProgramRuntimeProtocolVersion, RuntimeMode: delivery.ProgramRuntimeProtocol,
		SupportedTargets:   []delivery.TargetID{delivery.ObjectiveVerified},
		ObjectiveContracts: []delivery.ObjectiveContract{{TargetID: delivery.ObjectiveVerified, Conditions: []delivery.FacetCondition{delivery.KnownCondition(delivery.FacetDelivery, string(model.DeliveryPublished))}}},
		Transitions:        []delivery.Transition{publish, recover}, OwnedResources: []string{resource},
		Effects: []string{string(publishEffect), string(recoverEffect)}, Verifiers: []string{"fixture.state.published", "fixture.state.recovered"},
		Capabilities:        []delivery.Capability{delivery.CapabilityRepositoryWrite, delivery.CapabilityCommandExecute, delivery.CapabilityProductMutate},
		ConfigurationSchema: json.RawMessage(`{"type":"object"}`), PrivacyClassification: "metadata-only", TelemetryClassification: "transition-receipt",
	}, nil
}

type protocolStateClock struct{ now time.Time }

func (c protocolStateClock) Now() time.Time { return c.now }

type protocolStateObserver struct {
	path        string
	repository  string
	invocation  model.InvocationContext
	program     string
	now         time.Time
	configProof model.Evidence
}

func (o protocolStateObserver) Observe(context.Context, ports.ObservationRequest) (model.Observation, error) {
	raw, err := os.ReadFile(o.path)
	if err != nil {
		return model.Observation{}, err
	}
	state, err := durable.DecodeState(raw)
	if err != nil {
		return model.Observation{}, err
	}
	evidence := model.Evidence{Source: "fixture", Fingerprint: "fixture-state", ObservedAt: o.now}
	gitEvidence := evidence
	if o.repository != "" {
		command := exec.Command("git", "rev-parse", "--verify", "HEAD^{commit}")
		command.Dir = o.repository
		head, headErr := command.Output()
		if headErr != nil {
			return model.Observation{}, headErr
		}
		revision := strings.TrimSpace(string(head))
		digest := sha256.Sum256([]byte(revision))
		gitEvidence = model.Evidence{Source: "git:" + o.repository, Fingerprint: hex.EncodeToString(digest[:]), Revision: revision, ObservedAt: o.now}
	}
	return model.Observation{
		SchemaVersion: model.SnapshotSchemaVersion, StateRevision: state.Revision, RecordedProgramFingerprint: state.ProgramFingerprint,
		Invocation: o.invocation, Phase: model.Known(state.Phase, evidence), Engagement: model.Known(state.Engagement, evidence),
		Delivery: model.Known(state.Delivery, gitEvidence), Workspace: model.Known(state.Workspace, evidence), Plan: model.Known(state.Plan, evidence),
		Configuration: model.Known(state.Configuration, o.configProof), ConfigurationPolicy: model.Known(state.ConfigurationPolicy(), o.configProof),
		Runtime: model.Known(state.Runtime, evidence), Publication: model.Known(state.Publication, evidence), Verification: model.Known(state.Verification, gitEvidence),
		Recovery: model.Known(state.Recovery, evidence), Transaction: model.Known(state.Transaction, evidence),
		RecoveryInfo: model.Absent[model.RecoveryContext]("none", evidence), TransactionInfo: model.Absent[model.TransactionContext]("none", evidence),
		Terminal: model.Known(state.Terminal, evidence), Objective: model.Known(state.Objective, evidence), ObservedAt: o.now,
	}, nil
}

func TestProgramRuntimeProtocolCommitsDeclaredStateEffectBeforeReceipt(t *testing.T) {
	// control-law: protocol-runtime-effects-use-the-same-declared-state-reducer-as-native-effects
	ctx := context.Background()
	program, err := delivery.Compile(ctx, delivery.CompileRequest{KernelVersion: Version, Core: core.System(), Runtime: protocolStateRuntime{}})
	if err != nil {
		t.Fatal(err)
	}
	repository := t.TempDir()
	runGit := func(arguments ...string) {
		command := exec.Command("git", arguments...)
		command.Dir = repository
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	runGit("init", "-q")
	runGit("config", "user.email", "boatstack@example.invalid")
	runGit("config", "user.name", "Boatstack Test")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "README.md")
	runGit("commit", "-q", "-m", "fixture")
	baseRevisionCommand := exec.Command("git", "rev-parse", "HEAD")
	baseRevisionCommand.Dir = repository
	baseRevisionRaw, err := baseRevisionCommand.Output()
	if err != nil {
		t.Fatal(err)
	}
	baseRevision := strings.TrimSpace(string(baseRevisionRaw))
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("protocol candidate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "README.md")
	runGit("commit", "-q", "-m", "protocol candidate")
	acceptedRevisionCommand := exec.Command("git", "rev-parse", "HEAD")
	acceptedRevisionCommand.Dir = repository
	acceptedRevisionRaw, err := acceptedRevisionCommand.Output()
	if err != nil {
		t.Fatal(err)
	}
	acceptedRevision := strings.TrimSpace(string(acceptedRevisionRaw))

	now := time.Unix(1200, 0).UTC()
	clock := protocolStateClock{now: now}
	resolver, err := plant.NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(ctx, repository, "cli", "protocol-state-effect")
	if err != nil {
		t.Fatal(err)
	}
	layout, _, err := resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		t.Fatal(err)
	}
	state := durable.Default(invocation, now)
	state.ProgramFingerprint = program.Fingerprint()
	state.Phase, state.Engagement, state.Delivery = model.PhaseActive, model.EngagementActive, model.DeliveryActive
	state.Configuration, state.ConfigFingerprint = model.ConfigurationVerified, "configuration-fingerprint"
	state.PlanApprovalPolicy, state.VisualEvidencePolicy, state.ExternalEffectPolicy, state.EnabledHosts = "human", "optional", "human-or-autonomy-plus-provider", []string{"cli"}
	state.Objective = model.Objective{ID: "protocol-objective", TargetID: model.ObjectiveVerified, DeliveryID: "protocol-delivery"}
	encoded, err := durable.EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(layout.StatePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.StatePath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	configurationEvidence := model.Evidence{Source: "configuration:fixture", Fingerprint: state.ConfigFingerprint, ObservedAt: now}
	observer := protocolStateObserver{path: layout.StatePath, repository: repository, invocation: invocation, program: program.Fingerprint(), now: now, configProof: configurationEvidence}
	locker, _ := effects.NewLocker(resolver)
	journal, _ := effects.NewJournal(resolver, clock)
	receipts, _ := effects.NewReceiptStore(resolver, clock)
	base, err := effects.NewProgramDriver(resolver, clock, effects.NewNativeBoundary(), program.ResourceOwnership(), "")
	if err != nil {
		t.Fatal(err)
	}
	driver := programEffectDriver{base: base, program: program, resolver: resolver, clock: clock}
	summary := program.Summary()
	kernel, err := engine.New(program.RuntimeRegistry(), program.RuntimeObjectiveContracts(), protocol.ProgramIdentity{ID: summary.ProgramID, Version: summary.ProgramVersion, Fingerprint: summary.ProgramFingerprint}, observer, clock, locker, journal, driver, receipts)
	if err != nil {
		t.Fatal(err)
	}
	authority := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{
		{ID: "human", Class: catalog.AuthorityHuman, Subject: invocation.RepositoryID, Fingerprint: "human", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)},
		{ID: "repository", Class: catalog.AuthorityRepository, Subject: configurationEvidence.Source, Fingerprint: configurationEvidence.Fingerprint, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)},
	}}
	readme, err := os.ReadFile(filepath.Join(repository, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	bundleSnapshot, err := boatstackruntime.NewControlBundleSnapshot(map[string][]byte{"README.md": readme})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := boatstackruntime.NewControlBundleContract(bundleSnapshot, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	request := engine.ApplyRequest{
		ResolveRequest: engine.ResolveRequest{Invocation: invocation, Objective: state.Objective, Authority: authority, Requested: "fixture.state.publish", ControlBundle: &bundle, ControlBundleRevision: acceptedRevision},
		FlowID:         "protocol-state-flow", AdmissionLifetime: time.Minute,
	}
	resolution, err := kernel.Resolve(ctx, request.ResolveRequest)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Decision.Kind != supervisor.DecisionPrescribed {
		t.Fatalf("resolution = %#v", resolution.Decision)
	}
	runGit("reset", "--mixed", baseRevision)
	if _, prepareErr := driver.Prepare(ctx, resolution.Admission, *resolution.Decision.Transition); prepareErr == nil || !strings.Contains(prepareErr.Error(), "CONTROL_BUNDLE_REVISION_DRIFT") {
		t.Fatalf("protocol runtime accepted stale commit-bound bundle: %v", prepareErr)
	}
	runGit("reset", "--mixed", acceptedRevision)
	request.Prescription = resolution.Prescription
	result, err := kernel.Apply(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.ID == "" || result.Target.Delivery.Value != model.DeliveryPublished {
		t.Fatalf("protocol result did not bind declared state and receipt: %#v", result)
	}
	afterRaw, err := os.ReadFile(layout.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	after, err := durable.DecodeState(afterRaw)
	if err != nil {
		t.Fatal(err)
	}
	if after.Delivery != model.DeliveryPublished || after.LastTransition != "fixture.state.publish" || after.Revision != state.Revision+1 {
		t.Fatalf("durable protocol state = %#v", after)
	}
}

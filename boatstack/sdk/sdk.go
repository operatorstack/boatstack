// Package sdk exposes the versioned Boatstack surface protocol without
// exposing or duplicating the internal controller implementation.
package sdk

import (
	"context"
	"fmt"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/distribution"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

const SchemaVersion = surfaces.SchemaVersion

type Operation = surfaces.Operation

const (
	OperationResolve = surfaces.OperationResolve
	OperationApply   = surfaces.OperationApply
	OperationRecover = surfaces.OperationRecover
	OperationDoctor  = surfaces.OperationDoctor
	OperationCatalog = surfaces.OperationCatalog
	OperationEvents  = surfaces.OperationEvents
	OperationGuard   = surfaces.OperationGuard
)

type Request = surfaces.Request
type Response = surfaces.Response
type DoctorReport = surfaces.DoctorReport
type ProgramChange = surfaces.ProgramChange
type Objective = model.Objective
type ObjectiveKind = model.ObjectiveKind
type StateFacet = model.StateFacet

const (
	ObjectiveApprovedPlan = model.ObjectiveApprovedPlan
	ObjectiveVerified     = model.ObjectiveVerified
	ObjectiveOpenPR       = model.ObjectiveOpenPR
	ObjectiveMerged       = model.ObjectiveMerged
	ObjectiveAbandoned    = model.ObjectiveAbandoned

	StateFacetInstallation = model.StateFacetInstallation
	StateFacetProgram      = model.StateFacetProgram
	StateFacetControl      = model.StateFacetControl
	StateFacetProduct      = model.StateFacetProduct
)

type TransitionID = catalog.TransitionID
type Transition = catalog.Transition
type ObjectiveScope = catalog.ObjectiveScope
type AuthorityClass = catalog.AuthorityClass
type Capability = catalog.Capability

const (
	ObjectiveScopeNone             = catalog.ObjectiveScopeNone
	ObjectiveScopeOptionalPreserve = catalog.ObjectiveScopeOptionalPreserve
	ObjectiveScopeBoundExact       = catalog.ObjectiveScopeBoundExact

	AuthorityRepository = catalog.AuthorityRepository
	AuthorityHuman      = catalog.AuthorityHuman
	AuthorityAutonomy   = catalog.AuthorityAutonomy
	AuthorityProvider   = catalog.AuthorityProvider

	CapabilityRepositoryWrite    = catalog.CapabilityRepositoryWrite
	CapabilityCommandExecute     = catalog.CapabilityCommandExecute
	CapabilityProductMutate      = catalog.CapabilityProductMutate
	CapabilityPublicationPrepare = catalog.CapabilityPublicationPrepare
	CapabilityPublicationPublish = catalog.CapabilityPublicationPublish
	CapabilityHumanApprove       = catalog.CapabilityHumanApprove
)

type AuthorityReceipt = protocol.AuthorityReceipt
type AuthorityBundle = protocol.AuthorityBundle
type Parameter = protocol.Parameter
type Parameters = protocol.Parameters
type Admission = protocol.Admission
type TransitionReceipt = protocol.TransitionReceipt
type TransitionFactKind = protocol.TransitionFactKind
type ProgramIdentity = protocol.ProgramIdentity
type EffectFactKind = protocol.EffectFactKind
type EffectFact = protocol.EffectFact
type VerificationResult = protocol.VerificationResult
type VerificationFact = protocol.VerificationFact

const (
	TransitionCommitted    = protocol.TransitionCommitted
	EffectResourceMutation = protocol.EffectResourceMutation
	EffectBoundarySettled  = protocol.EffectBoundarySettled
	VerificationSatisfied  = protocol.VerificationSatisfied
)

type Decision = supervisor.Decision
type DecisionKind = supervisor.DecisionKind
type GuardDecision = supervisor.GuardDecision

const (
	DecisionPrescribed = supervisor.DecisionPrescribed
	DecisionTerminal   = supervisor.DecisionTerminal
	DecisionFrontier   = supervisor.DecisionFrontier
	DecisionBlocked    = supervisor.DecisionBlocked
	DecisionRefused    = supervisor.DecisionRefused
	DecisionUnresolved = supervisor.DecisionUnresolved
)

const HostIdentity = "sdk"

type options struct {
	runtime    delivery.ProgramRuntimeDefinition
	extensions []delivery.Extension
}

type Option func(*options) error

func WithProgramRuntime(runtime delivery.ProgramRuntimeDefinition) Option {
	return func(configuration *options) error {
		if runtime == nil {
			return fmt.Errorf("SDK program runtime cannot be nil")
		}
		if configuration.runtime != nil {
			return fmt.Errorf("SDK accepts exactly one ProgramRuntime")
		}
		configuration.runtime = runtime
		return nil
	}
}

func WithExtension(extension delivery.Extension) Option {
	return func(configuration *options) error {
		if extension == nil {
			return fmt.Errorf("SDK extension cannot be nil")
		}
		configuration.extensions = append(configuration.extensions, extension)
		return nil
	}
}

// Client is an immutable program factory plus the canonical request boundary.
// It compiles one repository-scoped ControlProgram per request, allowing a
// single Client to serve concurrent repositories with different extensions.
type Client struct {
	externalStateRoot string
	standard          bool
	runtime           delivery.ProgramRuntimeDefinition
	extensions        []delivery.Extension
}

// New assembles the standard Boatstack distribution. Options may add
// extensions but cannot replace StandardFlow.
func New(externalStateRoot string, supplied ...Option) (Client, error) {
	configuration, err := applyOptions(supplied)
	if err != nil {
		return Client{}, err
	}
	if configuration.runtime != nil {
		return Client{}, fmt.Errorf("sdk.New always uses StandardFlow; use sdk.NewProgramClient for an explicit flow")
	}
	if _, err := distribution.StandardProgram(context.Background(), configuration.extensions...); err != nil {
		return Client{}, err
	}
	return Client{externalStateRoot: externalStateRoot, standard: true, extensions: append([]delivery.Extension(nil), configuration.extensions...)}, nil
}

// NewProgramClient is the low-level composition API. It never inserts StandardFlow;
// callers must supply exactly one WithProgramRuntime option.
func NewProgramClient(externalStateRoot string, supplied ...Option) (Client, error) {
	configuration, err := applyOptions(supplied)
	if err != nil {
		return Client{}, err
	}
	if configuration.runtime == nil {
		return Client{}, fmt.Errorf("sdk.NewProgramClient requires an explicit ProgramRuntime")
	}
	if _, err := delivery.Compile(context.Background(), delivery.CompileRequest{
		KernelVersion: boatstack.Version, Core: core.System(), Runtime: configuration.runtime,
		Extensions: configuration.extensions,
	}); err != nil {
		return Client{}, err
	}
	return Client{externalStateRoot: externalStateRoot, runtime: configuration.runtime, extensions: append([]delivery.Extension(nil), configuration.extensions...)}, nil
}

func applyOptions(supplied []Option) (options, error) {
	var configuration options
	for _, option := range supplied {
		if option == nil {
			return options{}, fmt.Errorf("SDK option cannot be nil")
		}
		if err := option(&configuration); err != nil {
			return options{}, err
		}
	}
	return configuration, nil
}

func (c Client) Do(ctx context.Context, request Request) (Response, error) {
	if request.Host == "" {
		request.Host = HostIdentity
	}
	repositoryRequest := distribution.RepositoryProgramRequest{
		Repository: request.Repository, ExternalStateRoot: c.externalStateRoot, Host: request.Host,
		CorrelationID: request.CorrelationID, Extensions: c.extensions,
	}
	if request.TransitionID == "installation.initialize" || request.TransitionID == "configuration.initialize" {
		repositoryRequest.ConfigurationPath, _ = request.Parameters.Get("config_path")
		repositoryRequest.ConfigurationFingerprint, _ = request.Parameters.Get("config_sha256")
	}
	var program delivery.ControlProgram
	var err error
	if c.standard {
		program, err = distribution.StandardProgramForRepository(ctx, repositoryRequest)
	} else {
		var configured []delivery.Extension
		var settings any
		configured, settings, err = distribution.ConfiguredExtensions(ctx, repositoryRequest)
		if err == nil {
			extensions := append([]delivery.Extension(nil), c.extensions...)
			extensions = append(extensions, configured...)
			program, err = delivery.Compile(ctx, delivery.CompileRequest{
				KernelVersion: boatstack.Version, Core: core.System(), Runtime: c.runtime,
				Extensions: extensions, Settings: settings,
			})
		}
	}
	if err != nil {
		return Response{}, err
	}
	kernel, err := boatstack.NewDeliveryController(c.externalStateRoot, program)
	if err != nil {
		return Response{}, err
	}
	return kernel.Handle(ctx, request)
}

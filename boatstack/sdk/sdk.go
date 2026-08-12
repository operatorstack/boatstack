// Package sdk exposes the versioned Boatstack surface protocol without
// exposing or duplicating the internal controller implementation.
package sdk

import (
	"context"
	"fmt"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/distribution"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/supervisor"
	"github.com/operatorstack/boatstack/boatstack/internal/surfaces"
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
type Goal = model.Goal
type GoalKind = model.GoalKind

const (
	GoalApprovedPlan = model.GoalApprovedPlan
	GoalVerified     = model.GoalVerified
	GoalOpenPR       = model.GoalOpenPR
	GoalMerged       = model.GoalMerged
	GoalAbandoned    = model.GoalAbandoned
)

type TransitionID = catalog.TransitionID
type Transition = catalog.Transition
type GoalScope = catalog.GoalScope
type AuthorityClass = catalog.AuthorityClass
type Capability = catalog.Capability

const (
	GoalScopeOptionalPreserve = catalog.GoalScopeOptionalPreserve

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
	runtime    control.ProgramRuntimeDefinition
	extensions []control.Extension
}

type Option func(*options) error

func WithProgramRuntime(runtime control.ProgramRuntimeDefinition) Option {
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

func WithExtension(extension control.Extension) Option {
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
	runtime           control.ProgramRuntimeDefinition
	extensions        []control.Extension
}

// New assembles the standard Boatstack distribution. Options may add
// extensions but cannot replace StandardFlow.
func New(externalStateRoot string, supplied ...Option) (Client, error) {
	configuration, err := applyOptions(supplied)
	if err != nil {
		return Client{}, err
	}
	if configuration.runtime != nil {
		return Client{}, fmt.Errorf("sdk.New always uses StandardFlow; use sdk.NewKernel for an explicit flow")
	}
	if _, err := distribution.StandardProgram(context.Background(), configuration.extensions...); err != nil {
		return Client{}, err
	}
	return Client{externalStateRoot: externalStateRoot, standard: true, extensions: append([]control.Extension(nil), configuration.extensions...)}, nil
}

// NewKernel is the low-level composition API. It never inserts StandardFlow;
// callers must supply exactly one WithProgramRuntime option.
func NewKernel(externalStateRoot string, supplied ...Option) (Client, error) {
	configuration, err := applyOptions(supplied)
	if err != nil {
		return Client{}, err
	}
	if configuration.runtime == nil {
		return Client{}, fmt.Errorf("sdk.NewKernel requires an explicit ProgramRuntime")
	}
	if _, err := control.Compile(context.Background(), control.CompileRequest{
		KernelVersion: boatstack.Version, Core: core.System(), Runtime: configuration.runtime,
		Extensions: configuration.extensions,
	}); err != nil {
		return Client{}, err
	}
	return Client{externalStateRoot: externalStateRoot, runtime: configuration.runtime, extensions: append([]control.Extension(nil), configuration.extensions...)}, nil
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
	var program control.ControlProgram
	var err error
	if c.standard {
		program, err = distribution.StandardProgramForRepository(ctx, repositoryRequest)
	} else {
		var configured []control.Extension
		var settings any
		configured, settings, err = distribution.ConfiguredExtensions(ctx, repositoryRequest)
		if err == nil {
			extensions := append([]control.Extension(nil), c.extensions...)
			extensions = append(extensions, configured...)
			program, err = control.Compile(ctx, control.CompileRequest{
				KernelVersion: boatstack.Version, Core: core.System(), Runtime: c.runtime,
				Extensions: extensions, Settings: settings,
			})
		}
	}
	if err != nil {
		return Response{}, err
	}
	kernel, err := boatstack.NewKernel(c.externalStateRoot, program)
	if err != nil {
		return Response{}, err
	}
	return kernel.Handle(ctx, request)
}

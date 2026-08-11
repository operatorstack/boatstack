// Package sdk exposes the versioned Boatstack V2 surface protocol without
// exposing or duplicating the internal controller implementation.
package sdk

import (
	"context"

	boatstack "github.com/operatorstack/boatstack/boatstack"
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
type AuthorityClass = catalog.AuthorityClass

const (
	AuthorityRepository = catalog.AuthorityRepository
	AuthorityHuman      = catalog.AuthorityHuman
	AuthorityAutonomy   = catalog.AuthorityAutonomy
	AuthorityProvider   = catalog.AuthorityProvider
)

type AuthorityReceipt = protocol.AuthorityReceipt
type AuthorityBundle = protocol.AuthorityBundle
type Parameter = protocol.Parameter
type Parameters = protocol.Parameters
type Admission = protocol.Admission
type TransitionReceipt = protocol.TransitionReceipt
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

// Client is the only supported in-process V2 entry point. It delegates every
// decision and effect to the same kernel used by the CLI.
type Client struct{ kernel boatstack.V2Kernel }

func New(externalStateRoot string) (Client, error) {
	kernel, err := boatstack.NewV2Kernel(externalStateRoot)
	if err != nil {
		return Client{}, err
	}
	return Client{kernel: kernel}, nil
}

func (c Client) Do(ctx context.Context, request Request) (Response, error) {
	return c.kernel.Handle(ctx, request)
}

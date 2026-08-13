package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/delegation"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

func prepareDelegation(ctx context.Context, request *surfaces.Request) (ports.Lock, *surfaces.Response, error) {
	if request.ProgramID == "" || len(request.DelegatedAuthorities) == 0 {
		return nil, nil, nil
	}
	resolver, err := plant.NewResolver("")
	if err != nil {
		return nil, nil, err
	}
	invocation, err := resolver.ResolveInvocation(ctx, request.Repository, request.Host, request.CorrelationID)
	if err != nil {
		return nil, nil, err
	}
	layout, _, err := resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		return nil, nil, err
	}
	var lock ports.Lock
	if request.Operation == surfaces.OperationApply || request.Operation == surfaces.OperationRecover {
		lockPath, lockErr := delegation.LockPath(layout.LockRoot, request.FlowID)
		if lockErr != nil {
			return nil, nil, lockErr
		}
		lock, err = effects.AcquireExclusivePath(ctx, lockPath)
		if err != nil {
			return nil, nil, err
		}
	}
	releaseOnError := func() {
		if lock != nil {
			_ = lock.Release()
		}
	}
	recordPath, err := delegation.Path(layout.FlowRoot, request.FlowID)
	if err != nil {
		releaseOnError()
		return nil, nil, err
	}
	record, err := delegation.Load(recordPath)
	if os.IsNotExist(err) {
		releaseOnError()
		return nil, &surfaces.Response{
			SchemaVersion: surfaces.SchemaVersion, Operation: request.Operation, ProgramID: request.ProgramID, EntryID: request.EntryID, RunID: request.FlowID, Objective: request.Objective,
			Delegation: &surfaces.DelegationRequired{Code: "DELEGATION_REQUIRED", RunID: request.FlowID, RequestFingerprint: request.DelegationRequestFingerprint, Authorities: append([]catalog.AuthorityClass(nil), request.DelegatedAuthorities...), Description: "Explicitly authorize " + request.ProgramID + "/" + request.EntryID + " for this exact run"},
		}, nil
	}
	if err != nil {
		releaseOnError()
		return nil, nil, err
	}
	if record.RequestFingerprint != request.DelegationRequestFingerprint || record.Request.RunID != request.FlowID || record.Request.ProgramID != request.ProgramID || record.Request.ProgramFingerprint != request.ProgramFingerprint || record.Request.EntryID != request.EntryID || record.Request.TargetID != string(request.Objective.TargetID) || record.Request.ObjectiveID != request.Objective.ID || record.Request.DeliveryID != request.Objective.DeliveryID || record.Request.RepositoryID != invocation.RepositoryID || record.Request.GitCommonID != invocation.GitCommonID || record.Request.BindingFingerprint != request.DelegationBindingFingerprint {
		releaseOnError()
		return nil, nil, fmt.Errorf("DELEGATION_DRIFT: authorization does not match the current run context")
	}
	initial := invocation
	initial.WorktreeID, initial.Ref = record.Request.InitialWorktreeID, record.Request.InitialRef
	authorizedContext, lineageErr := effects.InvocationAuthorizedByFlow(layout, request.FlowID, initial, invocation)
	if lineageErr != nil {
		releaseOnError()
		return nil, nil, fmt.Errorf("DELEGATION_LINEAGE_INVALID: %w", lineageErr)
	}
	if !authorizedContext {
		releaseOnError()
		return nil, nil, fmt.Errorf("DELEGATION_CONTEXT_UNAUTHORIZED: current worktree is not in the verified run lineage")
	}
	filtered := request.Authority.Receipts[:0]
	for _, receipt := range request.Authority.Receipts {
		if len(receipt.ID) < len("delegation-") || receipt.ID[:len("delegation-")] != "delegation-" {
			filtered = append(filtered, receipt)
		}
	}
	request.Authority.Receipts = filtered
	if record.Status == "completed" && request.Operation == surfaces.OperationResolve {
		// A completed delegation carries no authority, but resolving the exact
		// bound run remains safe and lets restarts replay its terminal state.
		return nil, nil, nil
	}
	if record.Status != "active" {
		releaseOnError()
		return nil, nil, fmt.Errorf("DELEGATION_REVOKED: run authorization is %s", record.Status)
	}
	if !record.ExpiresAt.IsZero() && !time.Now().UTC().Before(record.ExpiresAt) {
		releaseOnError()
		return nil, nil, fmt.Errorf("DELEGATION_EXPIRED: run authorization expired")
	}
	for _, authority := range request.DelegatedAuthorities {
		receiptDigest := sha256.Sum256([]byte(record.ReceiptID + "\x00" + string(authority)))
		request.Authority.Receipts = append(request.Authority.Receipts, protocol.AuthorityReceipt{
			ID: "delegation-" + hex.EncodeToString(receiptDigest[:8]), Class: authority,
			Subject: record.Actor, Fingerprint: record.RequestFingerprint, IssuedAt: record.AuthorizedAt, ExpiresAt: record.ExpiresAt,
		})
	}
	return lock, nil, nil
}

func settleDelegationAtTarget(ctx context.Context, request surfaces.Request, response surfaces.Response, targetSatisfied, lockHeld bool) error {
	terminalDecision := response.Decision != nil && response.Decision.Kind == supervisor.DecisionTerminal
	committedTarget := response.Receipt != nil && targetSatisfied
	if len(request.DelegatedAuthorities) == 0 || (!terminalDecision && !committedTarget) {
		return nil
	}
	resolver, err := plant.NewResolver("")
	if err != nil {
		return err
	}
	repository := request.Repository
	if committedTarget && response.Snapshot != nil {
		repository = response.Snapshot.Invocation.InvokingPath
	}
	invocation, err := resolver.ResolveInvocation(ctx, repository, request.Host, request.CorrelationID)
	if err != nil {
		return err
	}
	layout, _, err := resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		return err
	}
	if !lockHeld {
		lockPath, err := delegation.LockPath(layout.LockRoot, request.FlowID)
		if err != nil {
			return err
		}
		lock, err := effects.AcquireExclusivePath(ctx, lockPath)
		if err != nil {
			return err
		}
		defer lock.Release()
	}
	recordPath, err := delegation.Path(layout.FlowRoot, request.FlowID)
	if err != nil {
		return err
	}
	record, err := delegation.Load(recordPath)
	if err != nil {
		return err
	}
	if record.Status != "active" {
		return nil
	}
	record.Status, record.Revision = "completed", record.Revision+1
	record.EndedAt, record.EndReason = time.Now().UTC(), "target-met"
	return effects.StoreDelegationRecord(recordPath, record)
}

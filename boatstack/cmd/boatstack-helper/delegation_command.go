package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/delegation"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

func runFlowAuthorize(arguments []string) error {
	flags := flag.NewFlagSet("flow authorize", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := commandOptions{repository: ".", host: "cli"}
	requestFingerprint := ""
	expiresIn := time.Duration(0)
	flags.StringVar(&options.repository, "repo", options.repository, "repository or worktree")
	flags.StringVar(&options.programID, "flow", "", "repository Control Program identity")
	flags.StringVar(&options.entryID, "entry", "", "named Flow entry")
	flags.StringVar(&options.runID, "run-id", "", "exact run identity")
	flags.StringVar(&requestFingerprint, "request-fingerprint", "", "exact delegation request fingerprint")
	flags.StringVar(&options.humanActor, "human", "", "authorizing human actor")
	flags.StringVar(&options.host, "host", options.host, "trusted host identity")
	flags.DurationVar(&expiresIn, "expires-in", 0, "optional delegation lifetime")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || options.runID == "" || requestFingerprint == "" || options.humanActor == "" {
		return fmt.Errorf("flow authorize requires --flow, --entry, --run-id, --request-fingerprint, and --human")
	}
	if expiresIn < 0 {
		return fmt.Errorf("flow authorize --expires-in cannot be negative")
	}
	bound, err := bindFlowEntry(context.Background(), options)
	if err != nil {
		return err
	}
	if bound.delegationRequestFingerprint == "" || requestFingerprint != bound.delegationRequestFingerprint || bound.runID != options.runID {
		return fmt.Errorf("DELEGATION_REQUEST_MISMATCH: authorization does not match the exact current request")
	}
	resolver, err := plant.NewResolver("")
	if err != nil {
		return err
	}
	invocation, err := resolver.ResolveInvocation(context.Background(), bound.repository, bound.host, "flow-authorize")
	if err != nil {
		return err
	}
	layout, _, err := resolver.ResolveLayout(context.Background(), invocation)
	if err != nil {
		return err
	}
	lockPath, err := delegation.LockPath(layout.LockRoot, bound.runID)
	if err != nil {
		return err
	}
	lock, err := effects.AcquireExclusivePath(context.Background(), lockPath)
	if err != nil {
		return err
	}
	defer lock.Release()
	recordPath, err := delegation.Path(layout.FlowRoot, bound.runID)
	if err != nil {
		return err
	}
	var existing *delegation.Record
	if loaded, loadErr := delegation.Load(recordPath); loadErr == nil {
		existing = &loaded
	} else if !os.IsNotExist(loadErr) {
		return loadErr
	}
	now := time.Now().UTC()
	record, changed, err := authorizeDelegation(existing, bound.delegationRequest, requestFingerprint, options.humanActor, expiresIn, now)
	if err != nil {
		return err
	}
	if changed {
		if err := effects.StoreDelegationRecord(recordPath, record); err != nil {
			return err
		}
	}
	return printDelegationRecord(record)
}

func authorizeDelegation(existing *delegation.Record, request delegation.Request, requestFingerprint, actor string, expiresIn time.Duration, now time.Time) (delegation.Record, bool, error) {
	if expiresIn < 0 {
		return delegation.Record{}, false, fmt.Errorf("flow authorize --expires-in cannot be negative")
	}
	if existing != nil {
		if existing.RequestFingerprint != requestFingerprint || existing.Actor != actor || existing.Status != "active" {
			return delegation.Record{}, false, fmt.Errorf("DELEGATION_CONFLICT: run already has a different authorization, actor, or status")
		}
		if existing.ExpiresAt.IsZero() || now.Before(existing.ExpiresAt) {
			return *existing, false, nil
		}
		record := *existing
		record.Revision++
		record.AuthorizedAt = now
		record.ExpiresAt = time.Time{}
		if expiresIn > 0 {
			record.ExpiresAt = now.Add(expiresIn)
		}
		record.ReceiptID = authorizationReceiptID(requestFingerprint, actor, record.Revision, now)
		record.RevokedAt, record.EndedAt, record.EndReason = time.Time{}, time.Time{}, ""
		return record, true, nil
	}
	record := delegation.Record{
		Schema: delegation.Schema, SchemaRevision: delegation.SchemaRevision,
		Request: request, RequestFingerprint: requestFingerprint,
		ReceiptID: authorizationReceiptID(requestFingerprint, actor, 1, now), Actor: actor,
		AuthorizedAt: now, Revision: 1, Status: "active",
	}
	if expiresIn > 0 {
		record.ExpiresAt = now.Add(expiresIn)
	}
	return record, true, nil
}

func authorizationReceiptID(requestFingerprint, actor string, revision uint64, authorizedAt time.Time) string {
	receiptDigest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s", requestFingerprint, actor, revision, authorizedAt.UTC().Format(time.RFC3339Nano))))
	return "authorization-" + hex.EncodeToString(receiptDigest[:12])
}

func runFlowRevoke(arguments []string) error {
	flags := flag.NewFlagSet("flow revoke", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	repository, runID, actor, host := ".", "", "", "cli"
	flags.StringVar(&repository, "repo", repository, "repository or worktree")
	flags.StringVar(&runID, "run-id", "", "exact run identity")
	flags.StringVar(&actor, "human", "", "revoking human actor")
	flags.StringVar(&host, "host", host, "trusted host identity")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || runID == "" || actor == "" {
		return fmt.Errorf("flow revoke requires --run-id and --human")
	}
	resolver, err := plant.NewResolver("")
	if err != nil {
		return err
	}
	invocation, err := resolver.ResolveInvocation(context.Background(), repository, host, "flow-revoke")
	if err != nil {
		return err
	}
	layout, _, err := resolver.ResolveLayout(context.Background(), invocation)
	if err != nil {
		return err
	}
	lockPath, err := delegation.LockPath(layout.LockRoot, runID)
	if err != nil {
		return err
	}
	lock, err := effects.AcquireExclusivePath(context.Background(), lockPath)
	if err != nil {
		return err
	}
	defer lock.Release()
	recordPath, err := delegation.Path(layout.FlowRoot, runID)
	if err != nil {
		return err
	}
	record, err := delegation.Load(recordPath)
	if err != nil {
		return err
	}
	if record.Actor != actor {
		return fmt.Errorf("DELEGATION_CONFLICT: revocation actor does not match the authorizing actor")
	}
	if record.Status == "revoked" {
		return printDelegationRecord(record)
	}
	if record.Status != "active" {
		return fmt.Errorf("DELEGATION_CONFLICT: delegation is %s", record.Status)
	}
	record.Status, record.Revision, record.RevokedAt = "revoked", record.Revision+1, time.Now().UTC()
	if err := effects.StoreDelegationRecord(recordPath, record); err != nil {
		return err
	}
	return printDelegationRecord(record)
}

func printDelegationRecord(record delegation.Record) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(record)
}

func runFlowContinuation(arguments []string) error {
	options, err := parseOptions("flow run", arguments, "", nil)
	if err != nil {
		return err
	}
	if options.programID == "" || options.entryID == "" {
		return fmt.Errorf("flow run requires --flow and --entry")
	}
	var response surfaces.Response
	for step := 0; step < 256; step++ {
		response, err = executeContinuationStep(context.Background(), options)
		if err != nil {
			return err
		}
		if response.RunID != "" {
			options.runID = response.RunID
		}
		if response.Objective.ID != "" {
			options.objectiveID = response.Objective.ID
			options.targetID = string(response.Objective.TargetID)
			options.trustedObjectiveClass = string(response.Objective.TrustedObjectiveClass())
			options.deliveryID = response.Objective.DeliveryID
		}
		if err := advanceContinuation(&options, response); err != nil {
			return err
		}
		if response.Delegation != nil || response.Prescription == nil || response.Receipt == nil {
			return renderResponse(response, options.format)
		}
	}
	return fmt.Errorf("FLOW_RUN_SUSPENDED: continuation step limit reached")
}

func executeContinuationStep(ctx context.Context, options commandOptions) (surfaces.Response, error) {
	bound, err := bindFlowEntry(ctx, options)
	if err != nil {
		return surfaces.Response{}, err
	}
	resolveRequest, err := buildRequest(surfaces.OperationResolve, bound)
	if err != nil {
		return surfaces.Response{}, err
	}
	_, delegationResponse, err := prepareDelegation(ctx, &resolveRequest)
	if err != nil {
		return surfaces.Response{}, err
	}
	if delegationResponse != nil {
		return *delegationResponse, nil
	}
	kernel, err := standardKernel(ctx, resolveRequest)
	if err != nil {
		return surfaces.Response{}, err
	}
	resolved, err := kernel.Handle(ctx, resolveRequest)
	if settleErr := settleDelegationAtTarget(ctx, resolveRequest, resolved, kernel.TargetSatisfied(resolved.Snapshot, resolveRequest.Objective), false); settleErr != nil && err == nil {
		err = settleErr
	}
	if err != nil || resolved.Prescription == nil {
		return resolved, err
	}
	applyRequest := resolveRequest
	applyRequest.Operation = surfaces.OperationApply
	applyRequest.TransitionID = resolved.Prescription.TransitionID
	applyRequest.Prescription = *resolved.Prescription
	if resolved.Admission != nil {
		applyRequest.IdempotencyKey = resolved.Admission.IdempotencyKey
	}
	delegationLock, delegationResponse, err := prepareDelegation(ctx, &applyRequest)
	if err != nil {
		return surfaces.Response{}, err
	}
	if delegationLock != nil {
		defer delegationLock.Release()
	}
	if delegationResponse != nil {
		return *delegationResponse, nil
	}
	lease, err := acquireFlowExecutionLease(applyRequest)
	if err != nil {
		return surfaces.Response{}, err
	}
	defer lease.Release()
	applied, err := kernel.Handle(ctx, applyRequest)
	if settleErr := settleDelegationAtTarget(ctx, applyRequest, applied, kernel.TargetSatisfied(applied.Snapshot, applyRequest.Objective), delegationLock != nil); settleErr != nil && err == nil {
		err = settleErr
	}
	if err != nil {
		return applied, err
	}
	// Mark the response as a completed internal continuation step. The next
	// iteration resolves again from the committed receipt and durable state.
	if applied.Prescription == nil {
		applied.Prescription = &protocol.Prescription{SchemaVersion: protocol.PrescriptionSchemaVersion, ID: "continued", TransitionID: catalog.TransitionID(applyRequest.TransitionID)}
	}
	return applied, nil
}

func advanceContinuation(options *commandOptions, response surfaces.Response) error {
	if response.Receipt == nil {
		return nil
	}
	if response.Receipt.ExecutionContext == "advance" {
		if response.Receipt.ResultingInvocation == nil {
			return fmt.Errorf("FLOW_RUN_SUSPENDED: advancing receipt has no resulting invocation")
		}
		resulting := *response.Receipt.ResultingInvocation
		if err := resulting.Validate(true); err != nil {
			return fmt.Errorf("FLOW_RUN_SUSPENDED: invalid resulting invocation: %w", err)
		}
		options.repository = resulting.InvokingPath
	}
	options.transitionID = ""
	options.parameters = nil
	options.prescriptionID = ""
	options.expectedInstanceID = ""
	options.expectedStateRevision = 0
	options.expectedProgramFingerprint = ""
	options.expectedSnapshotFingerprint = ""
	options.expectedObjectiveBindingFingerprint = ""
	options.authorityFingerprint = ""
	options.requiredCapabilities = nil
	options.effectiveCapabilities = nil
	options.idempotencyKey = ""
	return nil
}

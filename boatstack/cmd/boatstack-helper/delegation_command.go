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

	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/delegation"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentity"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

var resolveGitHubProviderAuthority = func(ctx context.Context, repository, previewFingerprint string, now time.Time) (protocol.AuthorityReceipt, error) {
	return effects.NewNativeBoundary().ResolveGitHubProviderAuthority(ctx, repository, previewFingerprint, now)
}

func trustedProviderAuthorityParameter(ctx context.Context, transitionID string) (string, error) {
	if transitionID == "" {
		return "", nil
	}
	manifest, err := standard.Definition().RuntimeManifest(ctx)
	if err != nil {
		return "", err
	}
	for _, transition := range manifest.Transitions {
		if string(transition.ID) == transitionID {
			return transition.AuthorityFingerprintParameter, nil
		}
	}
	return "", nil
}

func runFlowAuthorize(arguments []string) error {
	flags := flag.NewFlagSet("flow authorize", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := commandOptions{repository: ".", host: "cli"}
	requestFingerprint := ""
	identityProviderFingerprint := ""
	expiresIn := time.Duration(0)
	flags.StringVar(&options.repository, "repo", options.repository, "repository or worktree")
	flags.StringVar(&options.programID, "flow", "", "repository Control Program identity")
	flags.StringVar(&options.entryID, "entry", "", "named Flow entry")
	flags.StringVar(&options.runID, "run-id", "", "exact run identity")
	flags.StringVar(&requestFingerprint, "request-fingerprint", "", "exact delegation request fingerprint")
	flags.StringVar(&identityProviderFingerprint, "human-identity-provider-fingerprint", "", "exact human identity provider fingerprint from the delegation request")
	flags.StringVar(&options.humanActor, "human", "", "authorizing human actor")
	flags.StringVar(&options.host, "host", options.host, "trusted host identity")
	flags.DurationVar(&expiresIn, "expires-in", 0, "optional delegation lifetime")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || options.runID == "" || requestFingerprint == "" || identityProviderFingerprint == "" || options.humanActor == "" {
		return fmt.Errorf("flow authorize requires --flow, --entry, --run-id, --request-fingerprint, --human-identity-provider-fingerprint, and --human")
	}
	if err := humanidentity.ValidateActor(options.humanActor); err != nil {
		return err
	}
	if expiresIn < 0 {
		return fmt.Errorf("flow authorize --expires-in cannot be negative")
	}
	// Authorization reconstructs the exact request surfaced after candidate
	// selection. Ordinary unbound resolution must not create product delegation
	// before installation has established the control bundle.
	options.delegationRequestProjection = true
	bound, err := bindFlowEntry(context.Background(), options)
	if err != nil {
		if suspended, ok := flowCommitRequiredResponse(err, surfaces.OperationResolve); ok {
			return renderResponse(suspended, "json")
		}
		return err
	}
	resolveRequest, err := buildRequest(surfaces.OperationResolve, bound)
	if err != nil {
		return err
	}
	programChange, err := preflightFlowAuthorizationProgramChange(context.Background(), resolveRequest)
	if err != nil {
		return err
	}
	if programChange != nil {
		return fmt.Errorf("DELEGATION_PROGRAM_UNADMITTED: reconcile the exact candidate program before authorizing product delegation")
	}
	if bound.delegationRequestFingerprint == "" || requestFingerprint != bound.delegationRequestFingerprint || bound.runID != options.runID {
		return fmt.Errorf("DELEGATION_REQUEST_MISMATCH: authorization does not match the exact current request")
	}
	if identityProviderFingerprint != bound.delegationRequest.HumanIdentityProviderFingerprint {
		return fmt.Errorf("HUMAN_IDENTITY_DRIFT: authorization does not match the current identity provider")
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
	record, changed, err := authorizeDelegation(existing, bound.delegationRequest, requestFingerprint, identityProviderFingerprint, options.humanActor, expiresIn, now, bound.delegationReprojection)
	if err != nil {
		return err
	}
	if changed {
		if existing != nil && existing.RequestFingerprint != record.RequestFingerprint {
			archivePath, archivePathErr := delegation.SupersededPath(layout.FlowRoot, bound.runID, existing.RequestFingerprint)
			if archivePathErr != nil {
				return archivePathErr
			}
			if archiveErr := effects.ArchiveDelegationRecord(archivePath, *existing); archiveErr != nil {
				return archiveErr
			}
		}
		if err := effects.StoreDelegationRecord(recordPath, record); err != nil {
			return err
		}
	}
	return printDelegationRecord(record)
}

func authorizeDelegation(existing *delegation.Record, request delegation.Request, requestFingerprint, identityProviderFingerprint, actor string, expiresIn time.Duration, now time.Time, allowReprojection bool) (delegation.Record, bool, error) {
	if expiresIn < 0 {
		return delegation.Record{}, false, fmt.Errorf("flow authorize --expires-in cannot be negative")
	}
	computedFingerprint, err := request.Fingerprint()
	if err != nil || computedFingerprint != requestFingerprint {
		return delegation.Record{}, false, fmt.Errorf("DELEGATION_REQUEST_MISMATCH: authorization does not match the exact current request")
	}
	if identityProviderFingerprint != request.HumanIdentityProviderFingerprint {
		return delegation.Record{}, false, fmt.Errorf("HUMAN_IDENTITY_DRIFT: authorization does not match the current identity provider")
	}
	if err := humanidentity.ValidateActor(actor); err != nil {
		return delegation.Record{}, false, err
	}
	if existing != nil {
		if allowReprojection && existing.RequestFingerprint != requestFingerprint {
			if existing.Status != "active" && existing.Status != "revoked" {
				return delegation.Record{}, false, fmt.Errorf("DELEGATION_CONFLICT: reconciled run authorization is %s", existing.Status)
			}
			record := delegation.Record{
				Schema: delegation.Schema, SchemaRevision: delegation.SchemaRevision,
				Request: request, RequestFingerprint: requestFingerprint,
				ReceiptID: authorizationReceiptID(requestFingerprint, actor, request.HumanIdentityRole, identityProviderFingerprint, existing.Revision+1, now), Actor: actor,
				ActorIdentityRole:                request.HumanIdentityRole,
				ActorIdentityProviderFingerprint: identityProviderFingerprint,
				AuthorizedAt:                     now, Revision: existing.Revision + 1, Status: "active",
			}
			if expiresIn > 0 {
				record.ExpiresAt = now.Add(expiresIn)
			}
			return record, true, nil
		}
		if existing.RequestFingerprint != requestFingerprint || existing.Actor != actor || existing.ActorIdentityRole != request.HumanIdentityRole || existing.ActorIdentityProviderFingerprint != identityProviderFingerprint || existing.Status != "active" {
			if existing.RequestFingerprint == requestFingerprint && existing.Actor == actor && existing.ActorIdentityRole == request.HumanIdentityRole && existing.ActorIdentityProviderFingerprint == identityProviderFingerprint && existing.Status == "revoked" {
				reauthorized := *existing
				reauthorized.Revision++
				reauthorized.AuthorizedAt = now
				reauthorized.ExpiresAt = time.Time{}
				if expiresIn > 0 {
					reauthorized.ExpiresAt = now.Add(expiresIn)
				}
				reauthorized.ReceiptID = authorizationReceiptID(requestFingerprint, actor, request.HumanIdentityRole, identityProviderFingerprint, reauthorized.Revision, now)
				reauthorized.Status = "active"
				reauthorized.RevokedAt, reauthorized.EndedAt, reauthorized.EndReason = time.Time{}, time.Time{}, ""
				return reauthorized, true, nil
			}
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
		record.ReceiptID = authorizationReceiptID(requestFingerprint, actor, request.HumanIdentityRole, identityProviderFingerprint, record.Revision, now)
		record.RevokedAt, record.EndedAt, record.EndReason = time.Time{}, time.Time{}, ""
		return record, true, nil
	}
	record := delegation.Record{
		Schema: delegation.Schema, SchemaRevision: delegation.SchemaRevision,
		Request: request, RequestFingerprint: requestFingerprint,
		ReceiptID: authorizationReceiptID(requestFingerprint, actor, request.HumanIdentityRole, identityProviderFingerprint, 1, now), Actor: actor,
		ActorIdentityRole:                request.HumanIdentityRole,
		ActorIdentityProviderFingerprint: identityProviderFingerprint,
		AuthorizedAt:                     now, Revision: 1, Status: "active",
	}
	if expiresIn > 0 {
		record.ExpiresAt = now.Add(expiresIn)
	}
	return record, true, nil
}

func authorizationReceiptID(requestFingerprint, actor, identityRole, identityProviderFingerprint string, revision uint64, authorizedAt time.Time) string {
	receiptDigest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d\x00%s", requestFingerprint, actor, identityRole, identityProviderFingerprint, revision, authorizedAt.UTC().Format(time.RFC3339Nano))))
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
	if err := humanidentity.ValidateActor(actor); err != nil {
		return err
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
	return runFlowContinuationOptions(options)
}

func runFlowContinuationOptions(options commandOptions) error {
	if options.programID == "" || options.entryID == "" {
		return fmt.Errorf("flow run requires --flow and --entry")
	}
	if handled, declarativeErr := tryRunDeclarativeFlow(context.Background(), options); handled || declarativeErr != nil {
		return declarativeErr
	}
	if len(options.entryInputs) != 0 {
		return fmt.Errorf("FLOW_INPUT_INVALID: --input is available only to declarative Flow entries")
	}
	var response surfaces.Response
	var err error
	for step := 0; step < 256; step++ {
		response, err = executeContinuationStep(context.Background(), options)
		if err != nil {
			if suspended, ok := flowCommitRequiredResponse(err, surfaces.OperationResolve); ok {
				return renderResponse(suspended, options.format)
			}
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
		if response.Authorization != nil || response.CommitRequired != nil || response.Prescription == nil || response.Receipt == nil || (response.Decision != nil && response.Decision.Kind == supervisor.DecisionTerminal) {
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
	programChangeResponse, err := preflightFlowAuthorizationProgramChange(ctx, resolveRequest)
	if err != nil {
		return surfaces.Response{}, err
	}
	if programChangeResponse != nil {
		return *programChangeResponse, nil
	}
	_, delegationResponse, err := prepareFlowAuthorization(ctx, &resolveRequest)
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
	resolveLease, err := acquireFlowExecutionLease(resolveRequest)
	if err != nil {
		return surfaces.Response{}, err
	}
	if err := verifyTrustedRequestControlBundle(ctx, resolveRequest); err != nil {
		resolveLease.Release()
		return surfaces.Response{}, err
	}
	resolved, err := handleWithHumanIdentity(ctx, kernel, resolveRequest)
	resolveLease.Release()
	if settleErr := settleFlowAuthorizationAtTarget(ctx, resolveRequest, resolved, kernel.TargetSatisfied(resolved.Snapshot, resolveRequest.Objective), false); settleErr != nil && err == nil {
		err = settleErr
	}
	if err == nil {
		resolveRequest, resolved, _, err = stabilizeRepositoryPrescription(ctx, resolveRequest, resolved)
	}
	if err != nil || resolved.Prescription == nil {
		if err != nil {
			return resolved, err
		}
		rebound, changed, rebindErr := bindTrustedProviderCandidate(ctx, bound, resolved)
		if rebindErr != nil {
			return surfaces.Response{}, rebindErr
		}
		if !changed {
			rebound, changed, rebindErr = bindContinuationCandidate(ctx, bound, resolved)
		}
		if rebindErr != nil {
			return surfaces.Response{}, rebindErr
		}
		if !changed {
			return resolved, nil
		}
		resolveRequest, err = buildRequest(surfaces.OperationResolve, rebound)
		if err != nil {
			return surfaces.Response{}, err
		}
		programChangeResponse, err = preflightFlowAuthorizationProgramChange(ctx, resolveRequest)
		if err != nil {
			return surfaces.Response{}, err
		}
		if programChangeResponse != nil {
			return *programChangeResponse, nil
		}
		_, delegationResponse, err = prepareFlowAuthorization(ctx, &resolveRequest)
		if err != nil {
			return surfaces.Response{}, err
		}
		if delegationResponse != nil {
			return *delegationResponse, nil
		}
		kernel, err = standardKernel(ctx, resolveRequest)
		if err != nil {
			return surfaces.Response{}, err
		}
		resolveLease, err = acquireFlowExecutionLease(resolveRequest)
		if err != nil {
			return surfaces.Response{}, err
		}
		if err := verifyTrustedRequestControlBundle(ctx, resolveRequest); err != nil {
			resolveLease.Release()
			return surfaces.Response{}, err
		}
		resolved, err = handleWithHumanIdentity(ctx, kernel, resolveRequest)
		resolveLease.Release()
		if settleErr := settleFlowAuthorizationAtTarget(ctx, resolveRequest, resolved, kernel.TargetSatisfied(resolved.Snapshot, resolveRequest.Objective), false); settleErr != nil && err == nil {
			err = settleErr
		}
		if err != nil || resolved.Prescription == nil {
			return resolved, err
		}
	}
	applyRequest := resolveRequest
	applyRequest.Operation = surfaces.OperationApply
	applyRequest.TransitionID = resolved.Prescription.TransitionID
	applyRequest.Prescription = *resolved.Prescription
	if resolved.Admission != nil {
		applyRequest.IdempotencyKey = resolved.Admission.IdempotencyKey
	}
	lease, err := acquireFlowExecutionLease(applyRequest)
	if err != nil {
		return surfaces.Response{}, err
	}
	defer lease.Release()
	applyRequest.Parameters, applyRequest.InvocationEvidence, applyRequest.InputRequest = nil, nil, nil
	applyRequest, err = bindRPCFlowEntry(ctx, applyRequest)
	if err != nil {
		return surfaces.Response{}, err
	}
	if applyRequest.InputRequest != nil {
		return surfaces.Response{SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationApply, ProgramID: applyRequest.ProgramID, EntryID: applyRequest.EntryID, RunID: applyRequest.FlowID, InputRequest: applyRequest.InputRequest}, nil
	}
	delegationLock, delegationResponse, err := prepareFlowAuthorization(ctx, &applyRequest)
	if err != nil {
		return surfaces.Response{}, err
	}
	if delegationLock != nil {
		defer delegationLock.Release()
	}
	if delegationResponse != nil {
		return *delegationResponse, nil
	}
	if err := verifyTrustedRequestControlBundle(ctx, applyRequest); err != nil {
		return surfaces.Response{}, err
	}
	applied, err := handleWithHumanIdentity(ctx, kernel, applyRequest)
	targetSatisfied := kernel.TargetSatisfied(applied.Snapshot, applyRequest.Objective)
	if settleErr := settleFlowAuthorizationAtTarget(ctx, applyRequest, applied, targetSatisfied, delegationLock != nil); settleErr != nil && err == nil {
		err = settleErr
	}
	if err != nil {
		return applied, err
	}
	if targetSatisfied {
		snapshotFingerprint := ""
		if applied.Snapshot != nil {
			snapshotFingerprint = applied.Snapshot.Fingerprint
		}
		applied.Decision = &supervisor.Decision{Kind: supervisor.DecisionTerminal, SnapshotFingerprint: snapshotFingerprint, Reason: "marked Flow target is established"}
		applied.Prescription = nil
		return applied, nil
	}
	// Mark the response as a completed internal continuation step. The next
	// iteration resolves again from the committed receipt and durable state.
	if applied.Prescription == nil {
		applied.Prescription = &protocol.Prescription{SchemaVersion: protocol.PrescriptionSchemaVersion, ID: "continued", TransitionID: catalog.TransitionID(applyRequest.TransitionID)}
	}
	return applied, nil
}

// stabilizeRepositoryPrescription ensures that selection and parameter
// materialization are one resolved identity before a prescription can leave
// the command boundary. It is shared by next, RPC, and Flow continuation.
func stabilizeRepositoryPrescription(ctx context.Context, request surfaces.Request, response surfaces.Response) (surfaces.Request, surfaces.Response, bool, error) {
	rebound, changed, err := bindPrescribedRepositoryInvocation(ctx, request, response)
	if err != nil || !changed {
		return request, response, changed, err
	}
	if rebound.InputRequest != nil {
		return rebound, surfaces.Response{
			SchemaVersion: surfaces.SchemaVersion, Operation: surfaces.OperationResolve,
			ProgramID: rebound.ProgramID, EntryID: rebound.EntryID, RunID: rebound.FlowID,
			InputRequest: rebound.InputRequest,
		}, true, nil
	}
	lease, err := acquireFlowExecutionLease(rebound)
	if err != nil {
		return surfaces.Request{}, surfaces.Response{}, true, err
	}
	defer lease.Release()
	if err := verifyTrustedRequestControlBundle(ctx, rebound); err != nil {
		return surfaces.Request{}, surfaces.Response{}, true, err
	}
	kernel, err := standardKernel(ctx, rebound)
	if err != nil {
		return surfaces.Request{}, surfaces.Response{}, true, err
	}
	stabilized, err := handleWithHumanIdentity(ctx, kernel, rebound)
	if err != nil {
		return rebound, stabilized, true, err
	}
	if stabilized.Prescription != nil {
		if rebound.InvocationEvidence == nil {
			return surfaces.Request{}, surfaces.Response{}, true, fmt.Errorf("FLOW_INVOCATION_INCOMPLETE: stabilized repository prescription has no invocation evidence")
		}
		if err := stabilized.Prescription.ValidateInvocation(rebound.InvocationEvidence.InvocationFingerprint); err != nil {
			return surfaces.Request{}, surfaces.Response{}, true, err
		}
	}
	return rebound, stabilized, true, nil
}

func bindTrustedProviderCandidate(ctx context.Context, bound commandOptions, response surfaces.Response) (commandOptions, bool, error) {
	if bound.transitionID != "" || response.Prescription != nil || response.Decision == nil ||
		(response.Decision.Kind != supervisor.DecisionFrontier && response.Decision.Kind != supervisor.DecisionCandidate) ||
		len(response.Decision.Candidates) != 1 {
		return bound, false, nil
	}
	candidate := string(response.Decision.Candidates[0])
	authorityParameter, err := trustedProviderAuthorityParameter(ctx, candidate)
	if err != nil {
		return commandOptions{}, false, err
	}
	if authorityParameter == "" {
		return bound, false, nil
	}
	rebound := bound
	rebound.transitionID = candidate
	rebound, err = bindFlowEntry(ctx, rebound)
	if err != nil {
		return commandOptions{}, false, err
	}
	parameters, err := parseParameters(rebound.parameters)
	if err != nil {
		return commandOptions{}, false, err
	}
	if fingerprint, ok := parameters.Get(authorityParameter); !ok || fingerprint == "" {
		return bound, false, nil
	}
	return rebound, true, nil
}

func bindContinuationCandidate(ctx context.Context, bound commandOptions, response surfaces.Response) (commandOptions, bool, error) {
	if bound.transitionID != "" || response.Prescription != nil || response.Decision == nil || response.Decision.Kind != supervisor.DecisionCandidate || response.Decision.Transition == nil || len(response.Decision.Candidates) != 1 {
		return bound, false, nil
	}
	candidate := response.Decision.Transition.ID
	if response.Decision.Candidates[0] != candidate {
		return bound, false, nil
	}
	rebound := bound
	rebound.transitionID = string(candidate)
	rebound, err := bindFlowEntry(ctx, rebound)
	if err != nil {
		return commandOptions{}, false, err
	}
	if rebound.inputRequest == nil && len(rebound.parameters) <= len(bound.parameters) && rebound.delegationRequestFingerprint == "" {
		return bound, false, nil
	}
	return rebound, true, nil
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
	options.trustedAuthorityReceipts = nil
	options.invocationEvidence = nil
	options.inputRequest = nil
	options.controlBundleRevision = ""
	return nil
}

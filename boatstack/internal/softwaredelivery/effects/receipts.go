package effects

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

type ReceiptStore struct {
	resolver ports.InvocationResolver
	clock    ports.Clock
	// The engine serializes a worktree through the kernel lock. currentInvocation
	// binds receipt operations to that exact Apply call without reverse lookup.
	mu                lockedMutex
	currentInvocation map[string]receiptBinding
}

type receiptBinding struct {
	admission protocol.Admission
	layout    ports.ControllerLayout
}

func NewReceiptStore(resolver ports.InvocationResolver, clock ports.Clock) (*ReceiptStore, error) {
	if resolver == nil || clock == nil {
		return nil, fmt.Errorf("receipt store requires resolver and clock")
	}
	return &ReceiptStore{resolver: resolver, clock: clock, currentInvocation: map[string]receiptBinding{}}, nil
}

func (s *ReceiptStore) Bind(ctx context.Context, flowID string, admission protocol.Admission) error {
	layout, _, err := s.resolver.ResolveLayout(ctx, admission.Invocation)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, exists := s.currentInvocation[flowID]; exists && current.admission.ID != admission.ID {
		return fmt.Errorf("receipt flow %q is already bound to a different admission", flowID)
	}
	s.currentInvocation[flowID] = receiptBinding{admission: admission, layout: layout}
	return nil
}

func (s *ReceiptStore) Unbind(flowID string) {
	s.mu.Lock()
	delete(s.currentInvocation, flowID)
	s.mu.Unlock()
}

func (s *ReceiptStore) bindingForFlow(flowID string) (receiptBinding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.currentInvocation[flowID]
	return binding, ok
}

func (s *ReceiptStore) layoutForFlow(_ context.Context, flowID string) (ports.ControllerLayout, error) {
	binding, ok := s.bindingForFlow(flowID)
	if !ok {
		return ports.ControllerLayout{}, fmt.Errorf("receipt flow %q is not bound to an admission", flowID)
	}
	return binding.layout, nil
}

func scanCommittedReceipts(layout ports.ControllerLayout, visit func(journalRecord) error) error {
	entries, err := os.ReadDir(layout.JournalRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".committed") {
			continue
		}
		record, err := readJournal(filepath.Join(layout.JournalRoot, entry.Name()))
		if err != nil {
			return err
		}
		if record.Receipt == nil {
			return fmt.Errorf("committed transaction %s lacks a transition fact", entry.Name())
		}
		if err := visit(record); err != nil {
			return err
		}
	}
	return nil
}

// FindLatestCommittedFlowForObjective returns the authoritative committed flow
// identity for the current objective. Projected receipt files are deliberately
// not used because projection is best effort.
func FindLatestCommittedFlowForObjective(layout ports.ControllerLayout, invocation model.InvocationContext, objective model.Objective, maximumRevision uint64) (protocol.TransitionReceipt, bool, error) {
	records := []journalRecord{}
	if err := scanCommittedReceipts(layout, func(record journalRecord) error {
		records = append(records, record)
		return nil
	}); err != nil {
		return protocol.TransitionReceipt{}, false, err
	}
	return findLatestCommittedFlowForObjective(records, invocation, objective, maximumRevision)
}

func findLatestCommittedFlowForObjective(records []journalRecord, invocation model.InvocationContext, objective model.Objective, maximumRevision uint64) (protocol.TransitionReceipt, bool, error) {
	var found protocol.TransitionReceipt
	for _, record := range records {
		receipt := *record.Receipt
		if !matchesObjectiveBinding(receipt, objective, maximumRevision) {
			continue
		}
		bindingInvocation := record.Admission.Invocation
		authorized := sameStateLineage(bindingInvocation, invocation)
		if !authorized && bindingInvocation.ControllerID == invocation.ControllerID {
			var err error
			authorized, err = invocationAuthorizedByRecords(records, receipt.FlowID, bindingInvocation, invocation)
			if err != nil {
				return protocol.TransitionReceipt{}, false, err
			}
		}
		if authorized && (found.ID == "" || receipt.ResultingStateRevision > found.ResultingStateRevision) {
			found = receipt
		}
	}
	return found, found.ID != "", nil
}

// FindLatestCommittedTransitionOutput returns one effect output only from a
// committed receipt in the current Flow lineage. The sole historical adapter
// projects the exact admitted branch from a schema-12 publication commit as an
// observation locator; provider observation must still establish the PR ID.
func FindLatestCommittedTransitionOutput(layout ports.ControllerLayout, flowID string, invocation model.InvocationContext, transitionID catalog.TransitionID, field string, maximumRevision uint64) (string, string, bool, error) {
	records := []journalRecord{}
	if err := scanCommittedReceipts(layout, func(record journalRecord) error {
		records = append(records, record)
		return nil
	}); err != nil {
		return "", "", false, err
	}
	return findLatestCommittedTransitionOutput(records, flowID, invocation, transitionID, field, maximumRevision)
}

func findLatestCommittedTransitionOutput(records []journalRecord, flowID string, invocation model.InvocationContext, transitionID catalog.TransitionID, field string, maximumRevision uint64) (string, string, bool, error) {
	var found protocol.TransitionReceipt
	var value string
	for _, record := range records {
		receipt := *record.Receipt
		if receipt.FlowID != flowID || receipt.TransitionID != transitionID || receipt.ResultingStateRevision > maximumRevision {
			continue
		}
		output, exists := committedTransitionOutput(record, transitionID, field)
		if !exists {
			continue
		}
		authorized := sameStateLineage(record.Admission.Invocation, invocation)
		if !authorized && record.Admission.Invocation.ControllerID == invocation.ControllerID {
			var err error
			authorized, err = invocationAuthorizedByRecords(records, flowID, record.Admission.Invocation, invocation)
			if err != nil {
				return "", "", false, err
			}
		}
		if !authorized {
			continue
		}
		if found.ID == "" || receipt.Sequence > found.Sequence {
			found, value = receipt, output
		}
	}
	return value, found.ID, found.ID != "", nil
}

func committedTransitionOutput(record journalRecord, transitionID catalog.TransitionID, field string) (string, bool) {
	if output, exists := record.Receipt.EffectOutputs.Get(field); exists {
		return output, true
	}
	if record.Admission.SchemaVersion != protocol.PreviousAdmissionSchemaVersion || record.Receipt.SchemaVersion != protocol.PreviousReceiptSchemaVersion ||
		transitionID != "publication.execute" || field != "publication_id" || record.Receipt.TransitionID != transitionID {
		return "", false
	}
	const branchPrefix = "refs/heads/"
	if !strings.HasPrefix(record.Admission.Invocation.Ref, branchPrefix) {
		return "", false
	}
	branch := strings.TrimPrefix(record.Admission.Invocation.Ref, branchPrefix)
	if err := protocol.ValidateGitBranch(branch); err != nil {
		return "", false
	}
	return branch, true
}

// InstallationReprojectionAdmits reports whether the exact current control
// bundle was committed by an installation transition in this Flow lineage.
// The bundle binds the Flow artifact and runtime program together. This permits
// a fresh delegation request; it never carries prior delegation authority.
func InstallationReprojectionAdmits(layout ports.ControllerLayout, flowID string, invocation model.InvocationContext, objective model.Objective, controlBundleFingerprint string) (bool, error) {
	records := []journalRecord{}
	if err := scanCommittedReceipts(layout, func(record journalRecord) error {
		records = append(records, record)
		return nil
	}); err != nil {
		return false, err
	}
	return installationReprojectionAdmits(records, flowID, invocation, objective, controlBundleFingerprint)
}

func installationReprojectionAdmits(records []journalRecord, flowID string, invocation model.InvocationContext, objective model.Objective, controlBundleFingerprint string) (bool, error) {
	for _, record := range records {
		receipt := *record.Receipt
		installationTransition := receipt.TransitionID == "installation.update" || (receipt.TransitionID == "installation.reconcile-update" && receipt.ProgramChangeAccepted)
		if !installationTransition || receipt.FlowID != flowID || receipt.ControlBundleTargetFingerprint != controlBundleFingerprint ||
			receipt.ObjectiveID != objective.ID || receipt.TargetID != objective.TargetID || receipt.TrustedClass != objective.TrustedObjectiveClass() || receipt.DeliveryID != objective.DeliveryID {
			continue
		}
		authorized := sameStateLineage(record.Admission.Invocation, invocation)
		if !authorized && record.Admission.Invocation.ControllerID == invocation.ControllerID {
			var err error
			authorized, err = invocationAuthorizedByRecords(records, flowID, record.Admission.Invocation, invocation)
			if err != nil {
				return false, err
			}
		}
		if authorized {
			return true, nil
		}
	}
	return false, nil
}

// InvocationAuthorizedByFlow reconstructs worktree lineage only from valid,
// committed transition receipts. Mutable delegation records cannot invent a
// context transfer.
func InvocationAuthorizedByFlow(layout ports.ControllerLayout, flowID string, initial, current model.InvocationContext) (bool, error) {
	records := []journalRecord{}
	if err := scanCommittedReceipts(layout, func(record journalRecord) error {
		records = append(records, record)
		return nil
	}); err != nil {
		return false, err
	}
	return invocationAuthorizedByRecords(records, flowID, initial, current)
}

func invocationAuthorizedByRecords(records []journalRecord, flowID string, initial, current model.InvocationContext) (bool, error) {
	receipts := []protocol.TransitionReceipt{}
	for _, record := range records {
		if record.Receipt != nil && record.Receipt.FlowID == flowID {
			receipts = append(receipts, *record.Receipt)
		}
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].Sequence < receipts[j].Sequence })
	contextKey := func(invocation model.InvocationContext) string {
		return invocation.WorktreeID + "\x00" + invocation.Ref
	}
	authorized := map[string]bool{contextKey(initial): true}
	for _, receipt := range receipts {
		if receipt.ExecutionContext != "advance" {
			continue
		}
		prior, resulting := receipt.PriorInvocation, receipt.ResultingInvocation
		if prior == nil || resulting == nil || prior.RepositoryID != initial.RepositoryID || prior.GitCommonID != initial.GitCommonID || resulting.RepositoryID != initial.RepositoryID || resulting.GitCommonID != initial.GitCommonID || !authorized[contextKey(*prior)] {
			return false, fmt.Errorf("delegation receipt lineage is invalid at %s", receipt.ID)
		}
		authorized[contextKey(*resulting)] = true
	}
	return current.RepositoryID == initial.RepositoryID && current.GitCommonID == initial.GitCommonID && authorized[contextKey(current)], nil
}

func sameStateLineage(left, right model.InvocationContext) bool {
	return left.RepositoryID == right.RepositoryID && left.GitCommonID == right.GitCommonID &&
		left.WorktreeID == right.WorktreeID && left.ControllerID == right.ControllerID
}

func matchesObjectiveBinding(receipt protocol.TransitionReceipt, objective model.Objective, maximumRevision uint64) bool {
	return receipt.TransitionID == "objective.bind" && strings.HasPrefix(receipt.FlowID, "run-") &&
		receipt.ObjectiveID == objective.ID && receipt.TargetID == objective.TargetID && receipt.TrustedClass == objective.TrustedClass && receipt.DeliveryID == objective.DeliveryID &&
		receipt.ResultingStateRevision <= maximumRevision
}

func (s *ReceiptStore) NextSequence(ctx context.Context, flowID string) (uint64, error) {
	layout, err := s.layoutForFlow(ctx, flowID)
	if err != nil {
		return 0, err
	}
	var maximum uint64
	err = scanCommittedReceipts(layout, func(record journalRecord) error {
		receipt := *record.Receipt
		if receipt.FlowID == flowID && receipt.Sequence > maximum {
			maximum = receipt.Sequence
		}
		return nil
	})
	return maximum + 1, err
}

func (s *ReceiptStore) FindByIdempotency(ctx context.Context, invocation model.InvocationContext, key string) (protocol.TransitionReceipt, bool, error) {
	if key == "" {
		return protocol.TransitionReceipt{}, false, nil
	}
	layout, _, err := s.resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		return protocol.TransitionReceipt{}, false, err
	}
	var found protocol.TransitionReceipt
	err = scanCommittedReceipts(layout, func(record journalRecord) error {
		receipt := *record.Receipt
		if receipt.IdempotencyKey == key {
			if found.ID != "" && found.ID != receipt.ID {
				return fmt.Errorf("idempotency key %q identifies multiple committed transition facts", key)
			}
			found = receipt
		}
		return nil
	})
	return found, found.ID != "", err
}

type processEvent struct {
	SchemaVersion          int                       `json:"schema_version"`
	FlowID                 string                    `json:"flow_id"`
	Sequence               uint64                    `json:"sequence"`
	Timestamp              time.Time                 `json:"timestamp"`
	ObjectiveID            string                    `json:"objective_id"`
	ObjectiveScope         string                    `json:"objective_scope,omitempty"`
	ObjectiveStatus        string                    `json:"objective_status,omitempty"`
	TransitionID           string                    `json:"transition_id"`
	ProgramID              string                    `json:"program_id"`
	ProgramVersion         string                    `json:"program_version"`
	ProgramFingerprint     string                    `json:"program_fingerprint"`
	PrescriptionID         string                    `json:"prescription_id"`
	PriorStateRevision     uint64                    `json:"prior_state_revision"`
	ResultingStateRevision uint64                    `json:"resulting_state_revision"`
	SourceFingerprint      string                    `json:"source_fingerprint"`
	TargetFingerprint      string                    `json:"target_fingerprint"`
	FactKind               string                    `json:"fact_kind"`
	DurationNanoseconds    int64                     `json:"duration_nanoseconds"`
	AuthorityClasses       []string                  `json:"authority_classes,omitempty"`
	AuthorityFingerprint   string                    `json:"authority_fingerprint"`
	RequiredCapabilities   []catalog.Capability      `json:"required_capabilities"`
	GrantedCapabilities    []catalog.Capability      `json:"granted_capabilities"`
	CommittedEffects       []protocol.EffectFact     `json:"committed_effects"`
	EffectOutputs          protocol.Parameters       `json:"effect_outputs,omitempty"`
	ChangedStateFacets     []model.StateFacet        `json:"changed_state_facets"`
	Verification           protocol.VerificationFact `json:"verification"`
	Recovery               string                    `json:"recovery,omitempty"`
	Terminal               string                    `json:"terminal"`
}

func appendLine(path string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func (s *ReceiptStore) Project(ctx context.Context, receipt protocol.TransitionReceipt) error {
	if err := receipt.Validate(); err != nil {
		return err
	}
	layout, err := s.layoutForFlow(ctx, receipt.FlowID)
	if err != nil {
		return err
	}
	if err := appendLine(layout.ReceiptPath, receipt, 0o600); err != nil {
		return err
	}
	event := processEvent{
		SchemaVersion: 5, FlowID: receipt.FlowID, Sequence: receipt.Sequence, Timestamp: s.clock.Now().UTC(), ObjectiveID: receipt.ObjectiveID,
		ObjectiveScope: string(receipt.ObjectiveScope), ObjectiveStatus: string(receipt.ObjectiveStatus),
		TransitionID: string(receipt.TransitionID), ProgramID: receipt.Program.ID, ProgramVersion: receipt.Program.Version, ProgramFingerprint: receipt.Program.Fingerprint, PrescriptionID: receipt.PrescriptionID,
		PriorStateRevision: receipt.PriorStateRevision, ResultingStateRevision: receipt.ResultingStateRevision,
		SourceFingerprint: receipt.SourceFingerprint, TargetFingerprint: receipt.TargetFingerprint,
		FactKind: string(receipt.Kind), DurationNanoseconds: receipt.DurationNanoseconds,
		Recovery: string(receipt.Recovery), Terminal: string(receipt.Terminal),
		AuthorityFingerprint: receipt.AuthorityFingerprint,
		RequiredCapabilities: append([]catalog.Capability(nil), receipt.RequiredCapabilities...),
		GrantedCapabilities:  append([]catalog.Capability(nil), receipt.GrantedCapabilities...),
		CommittedEffects:     append([]protocol.EffectFact(nil), receipt.CommittedEffects...),
		EffectOutputs:        append(protocol.Parameters(nil), receipt.EffectOutputs...),
		ChangedStateFacets:   append([]model.StateFacet(nil), receipt.ChangedStateFacets...),
		Verification:         receipt.Verification,
	}
	for _, source := range receipt.AuthoritySources {
		event.AuthorityClasses = append(event.AuthorityClasses, string(source.Class))
	}
	// Telemetry is passive and never changes transition success.
	_ = appendLine(layout.EventPath, event, 0o600)
	s.Unbind(receipt.FlowID)
	return nil
}

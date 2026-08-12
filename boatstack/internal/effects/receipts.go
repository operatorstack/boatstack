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

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
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

func scanCommittedReceipts(layout ports.ControllerLayout, visit func(protocol.TransitionReceipt) error) error {
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
		if err := visit(*record.Receipt); err != nil {
			return err
		}
	}
	return nil
}

func (s *ReceiptStore) NextSequence(ctx context.Context, flowID string) (uint64, error) {
	layout, err := s.layoutForFlow(ctx, flowID)
	if err != nil {
		return 0, err
	}
	var maximum uint64
	err = scanCommittedReceipts(layout, func(receipt protocol.TransitionReceipt) error {
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
	err = scanCommittedReceipts(layout, func(receipt protocol.TransitionReceipt) error {
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
	GoalID                 string                    `json:"goal_id"`
	GoalScope              string                    `json:"goal_scope,omitempty"`
	GoalStatus             string                    `json:"goal_status,omitempty"`
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
		SchemaVersion: 4, FlowID: receipt.FlowID, Sequence: receipt.Sequence, Timestamp: s.clock.Now().UTC(), GoalID: receipt.GoalID,
		GoalScope: string(receipt.GoalScope), GoalStatus: string(receipt.GoalStatus),
		TransitionID: string(receipt.TransitionID), ProgramID: receipt.Program.ID, ProgramVersion: receipt.Program.Version, ProgramFingerprint: receipt.Program.Fingerprint, PrescriptionID: receipt.PrescriptionID,
		PriorStateRevision: receipt.PriorStateRevision, ResultingStateRevision: receipt.ResultingStateRevision,
		SourceFingerprint: receipt.SourceFingerprint, TargetFingerprint: receipt.TargetFingerprint,
		FactKind: string(receipt.Kind), DurationNanoseconds: receipt.DurationNanoseconds,
		Recovery: string(receipt.Recovery), Terminal: string(receipt.Terminal),
		AuthorityFingerprint: receipt.AuthorityFingerprint,
		RequiredCapabilities: append([]catalog.Capability(nil), receipt.RequiredCapabilities...),
		GrantedCapabilities:  append([]catalog.Capability(nil), receipt.GrantedCapabilities...),
		CommittedEffects:     append([]protocol.EffectFact(nil), receipt.CommittedEffects...),
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

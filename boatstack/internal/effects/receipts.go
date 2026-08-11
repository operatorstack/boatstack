package effects

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

func scanReceipts(path string, visit func(protocol.TransitionReceipt) error) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 4*1024*1024)
	for scanner.Scan() {
		var receipt protocol.TransitionReceipt
		if err := json.Unmarshal(scanner.Bytes(), &receipt); err != nil {
			return fmt.Errorf("decode receipt stream: %w", err)
		}
		if err := receipt.Validate(); err != nil {
			return fmt.Errorf("verify receipt stream: %w", err)
		}
		if err := visit(receipt); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *ReceiptStore) NextSequence(ctx context.Context, flowID string) (uint64, error) {
	layout, err := s.layoutForFlow(ctx, flowID)
	if err != nil {
		return 0, err
	}
	var maximum uint64
	err = scanReceipts(layout.ReceiptPath, func(receipt protocol.TransitionReceipt) error {
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
	err = scanReceipts(layout.ReceiptPath, func(receipt protocol.TransitionReceipt) error {
		if receipt.IdempotencyKey == key {
			found = receipt
		}
		return nil
	})
	return found, found.ID != "", err
}

type processEvent struct {
	SchemaVersion       int       `json:"schema_version"`
	FlowID              string    `json:"flow_id"`
	Sequence            uint64    `json:"sequence"`
	Timestamp           time.Time `json:"timestamp"`
	GoalID              string    `json:"goal_id"`
	TransitionID        string    `json:"transition_id"`
	SourceFingerprint   string    `json:"source_fingerprint"`
	TargetFingerprint   string    `json:"target_fingerprint"`
	Outcome             string    `json:"outcome"`
	DurationNanoseconds int64     `json:"duration_nanoseconds"`
	AuthorityClasses    []string  `json:"authority_classes,omitempty"`
	Recovery            string    `json:"recovery,omitempty"`
	Terminal            string    `json:"terminal"`
	FailureClass        string    `json:"failure_class,omitempty"`
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

func (s *ReceiptStore) Append(ctx context.Context, receipt protocol.TransitionReceipt) error {
	layout, err := s.layoutForFlow(ctx, receipt.FlowID)
	if err != nil {
		return err
	}
	if err := appendLine(layout.ReceiptPath, receipt, 0o600); err != nil {
		return err
	}
	event := processEvent{
		SchemaVersion: 1, FlowID: receipt.FlowID, Sequence: receipt.Sequence, Timestamp: s.clock.Now().UTC(), GoalID: receipt.GoalID,
		TransitionID: string(receipt.TransitionID), SourceFingerprint: receipt.SourceFingerprint, TargetFingerprint: receipt.TargetFingerprint,
		Outcome: string(receipt.Outcome), DurationNanoseconds: receipt.DurationNanoseconds,
		AuthorityClasses: append([]string(nil), receipt.AuthorityClasses...), Recovery: string(receipt.Recovery), Terminal: string(receipt.Terminal), FailureClass: receipt.FailureClass,
	}
	// Telemetry is passive and never changes transition success.
	_ = appendLine(layout.EventPath, event, 0o600)
	s.Unbind(receipt.FlowID)
	return nil
}

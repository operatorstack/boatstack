package effects

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
)

type Journal struct {
	resolver ports.InvocationResolver
	clock    ports.Clock
	mu       lockedMutex
	active   map[string]string
}

func NewJournal(resolver ports.InvocationResolver, clock ports.Clock) (*Journal, error) {
	if resolver == nil || clock == nil {
		return nil, fmt.Errorf("transaction journal requires resolver and clock")
	}
	return &Journal{resolver: resolver, clock: clock, active: map[string]string{}}, nil
}

type journalRecord struct {
	SchemaVersion     int                      `json:"schema_version"`
	Admission         protocol.Admission       `json:"admission"`
	TransitionID      catalog.TransitionID     `json:"transition_id"`
	TransitionClass   catalog.EventClass       `json:"transition_class"`
	ReconcilesProgram bool                     `json:"reconciles_program,omitempty"`
	Status            string                   `json:"status"`
	Mutations         []ports.ResourceMutation `json:"mutations,omitempty"`
	Reason            string                   `json:"reason,omitempty"`
	ReceiptID         string                   `json:"receipt_id,omitempty"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
}

func journalName(id, suffix string) (string, error) {
	if !strings.HasPrefix(id, "adm-") || strings.ContainsAny(id, `/\\`) {
		return "", fmt.Errorf("invalid admission identity %q", id)
	}
	return id + suffix, nil
}

func (j *Journal) pendingPath(ctx context.Context, admission protocol.Admission) (string, error) {
	layout, _, err := j.resolver.ResolveLayout(ctx, admission.Invocation)
	if err != nil {
		return "", err
	}
	name, err := journalName(admission.ID, ".pending")
	if err != nil {
		return "", err
	}
	return filepath.Join(layout.JournalRoot, name), nil
}

func (j *Journal) Begin(ctx context.Context, admission protocol.Admission, transition catalog.Transition) error {
	path, err := j.pendingPath(ctx, admission)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr == nil {
		return fmt.Errorf("transaction journal already exists for admission %s", admission.ID)
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	now := j.clock.Now().UTC()
	record := journalRecord{SchemaVersion: 2, Admission: admission, TransitionID: transition.ID, TransitionClass: transition.Class, ReconcilesProgram: transition.Policy.ReconcilesProgram, Status: "begun", CreatedAt: now, UpdatedAt: now}
	raw, err := encodeJSON(record)
	if err != nil {
		return err
	}
	if err := atomicWrite(path, raw, 0o600); err != nil {
		return err
	}
	name, err := journalName(admission.ID, ".pending")
	if err != nil {
		return err
	}
	j.bind(name, path)
	return nil
}

func readJournal(path string) (journalRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return journalRecord{}, err
	}
	var record journalRecord
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return journalRecord{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return journalRecord{}, fmt.Errorf("transaction journal %s contains trailing JSON", path)
	}
	if record.SchemaVersion != 2 || record.Admission.ID == "" || record.TransitionID == "" || !record.TransitionClass.Valid() || !record.TransitionClass.Controllable() || record.Status == "" {
		return journalRecord{}, fmt.Errorf("invalid transaction journal %s", path)
	}
	if err := record.Admission.ValidateIdentity(); err != nil || record.Admission.TransitionID != record.TransitionID {
		return journalRecord{}, fmt.Errorf("invalid transaction admission in %s: %v", path, err)
	}
	return record, nil
}

func (j *Journal) update(ctx context.Context, admissionID string, update func(*journalRecord)) error {
	name, err := journalName(admissionID, ".pending")
	if err != nil {
		return err
	}
	// The admission is inside the journal, so locate it only under the explicit
	// invocation layout supplied by the active engine through the in-memory index.
	// A process restart uses the recovery scanner rather than this method.
	return j.updateKnownPath(ctx, name, update)
}

func (j *Journal) updateKnownPath(ctx context.Context, name string, update func(*journalRecord)) error {
	// Journal operations occur under the per-worktree kernel lock. Find the one
	// pending record by its content identity across the current process's known
	// layouts is intentionally not allowed; callers bind the path at Begin.
	journalPath, ok := j.activePath(name)
	if !ok {
		return fmt.Errorf("transaction journal path is not bound for %s", name)
	}
	record, err := readJournal(journalPath)
	if err != nil {
		return err
	}
	update(&record)
	record.UpdatedAt = j.clock.Now().UTC()
	raw, err := encodeJSON(record)
	if err != nil {
		return err
	}
	return atomicWrite(journalPath, raw, 0o600)
}

func (j *Journal) activePath(name string) (string, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	value, ok := j.active[name]
	return value, ok
}

func (j *Journal) bind(name, path string) {
	j.mu.Lock()
	j.active[name] = path
	j.mu.Unlock()
}

func (j *Journal) unbind(name string) {
	j.mu.Lock()
	delete(j.active, name)
	j.mu.Unlock()
}

func (j *Journal) Stage(ctx context.Context, admissionID string, mutations []ports.ResourceMutation) error {
	return j.update(ctx, admissionID, func(record *journalRecord) {
		record.Status = "staged"
		record.Mutations = append([]ports.ResourceMutation(nil), mutations...)
	})
}

func (j *Journal) Mark(ctx context.Context, admissionID, status string) error {
	if strings.TrimSpace(status) == "" {
		return fmt.Errorf("journal status is required")
	}
	return j.update(ctx, admissionID, func(record *journalRecord) { record.Status = status })
}

func (j *Journal) finalize(ctx context.Context, admissionID, suffix, status, reason, receiptID string) error {
	name, err := journalName(admissionID, ".pending")
	if err != nil {
		return err
	}
	path, ok := j.activePath(name)
	if !ok {
		return fmt.Errorf("transaction journal path is not bound for %s", admissionID)
	}
	record, err := readJournal(path)
	if err != nil {
		return err
	}
	record.Status, record.Reason, record.ReceiptID, record.UpdatedAt = status, reason, receiptID, j.clock.Now().UTC()
	raw, err := encodeJSON(record)
	if err != nil {
		return err
	}
	finalPath := strings.TrimSuffix(path, ".pending") + suffix
	if err := atomicWrite(finalPath, raw, 0o600); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	j.unbind(name)
	return nil
}

func (j *Journal) Commit(ctx context.Context, receipt protocol.TransitionReceipt) error {
	return j.finalize(ctx, receipt.AdmissionID, ".committed", "committed", "", receipt.ID)
}

func (j *Journal) Abort(ctx context.Context, admissionID, reason string) error {
	return j.finalize(ctx, admissionID, ".aborted", "aborted", reason, "")
}

func (j *Journal) RequireRecovery(ctx context.Context, admissionID, reason string) error {
	return j.update(ctx, admissionID, func(record *journalRecord) {
		record.Status, record.Reason = "recovery-required", reason
	})
}

// scanJournal is used by recovery and tests. It never mutates a record.
func scanJournal(path string) (journalRecord, error) { return readJournal(path) }

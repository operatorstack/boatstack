package effects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
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
	SchemaVersion   int                  `json:"schema_version"`
	Admission       protocol.Admission   `json:"admission"`
	TransitionID    catalog.TransitionID `json:"transition_id"`
	TransitionClass catalog.EventClass   `json:"transition_class"`
	// AllowedStateFacets preserves the declared record shape. Recovery
	// authority is reconstructed from Admission.RequiredCapabilities instead.
	AllowedStateFacets []model.StateFacet          `json:"allowed_state_facets"`
	ReconcilesProgram  bool                        `json:"reconciles_program,omitempty"`
	Status             string                      `json:"status"`
	Mutations          []ports.ResourceMutation    `json:"mutations,omitempty"`
	Reason             string                      `json:"reason,omitempty"`
	ReceiptID          string                      `json:"receipt_id,omitempty"`
	Receipt            *protocol.TransitionReceipt `json:"receipt,omitempty"`
	CreatedAt          time.Time                   `json:"created_at"`
	UpdatedAt          time.Time                   `json:"updated_at"`
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
	policy, err := catalog.DurableStateFacetPolicy(transition)
	if err != nil {
		return err
	}
	record := journalRecord{SchemaVersion: protocol.JournalSchemaVersion, Admission: admission, TransitionID: transition.ID, TransitionClass: transition.Class, AllowedStateFacets: policy.Writes, ReconcilesProgram: transition.Policy.ReconcilesProgram, Status: "begun", CreatedAt: now, UpdatedAt: now}
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
	if record.SchemaVersion != protocol.JournalSchemaVersion || record.Admission.ID == "" || record.TransitionID == "" || !record.TransitionClass.Valid() || !record.TransitionClass.Controllable() || record.Status == "" {
		return journalRecord{}, fmt.Errorf("invalid transaction journal %s", path)
	}
	if err := record.Admission.ValidateIdentity(); err != nil || record.Admission.TransitionID != record.TransitionID {
		return journalRecord{}, fmt.Errorf("invalid transaction admission in %s: %v", path, err)
	}
	allowed, err := model.NormalizeStateFacets("journal allowed state facets", record.AllowedStateFacets)
	if err != nil || len(allowed) == 0 || !slices.Equal(allowed, record.AllowedStateFacets) {
		return journalRecord{}, fmt.Errorf("invalid allowed state facets in %s: %v", path, err)
	}
	for _, mutation := range record.Mutations {
		facets, err := model.NormalizeStateFacets("journal mutation state facets", mutation.StateFacets)
		if err != nil || !slices.Equal(facets, mutation.StateFacets) {
			return journalRecord{}, fmt.Errorf("invalid transaction state facets in %s: %v", path, err)
		}
	}
	if record.Receipt != nil {
		if err := record.Receipt.Validate(); err != nil || record.Receipt.ID != record.ReceiptID || record.Receipt.AdmissionID != record.Admission.ID || record.Receipt.TransitionID != record.TransitionID {
			return journalRecord{}, fmt.Errorf("invalid committed transition fact in %s: %v", path, err)
		}
		receipt := record.Receipt
		admission := record.Admission
		if receipt.PrescriptionID != admission.PrescriptionID || receipt.TransitionVersion != admission.TransitionVersion || receipt.Program.Fingerprint != admission.ExpectedProgramFingerprint ||
			receipt.PriorStateRevision != admission.ExpectedStateRevision || receipt.SourceFingerprint != admission.ExpectedSnapshotFingerprint ||
			receipt.AuthorityFingerprint != admission.AuthorityFingerprint || !slices.Equal(receipt.RequiredCapabilities, admission.RequiredCapabilities) ||
			!slices.Equal(receipt.GrantedCapabilities, admission.GrantedCapabilities) || receipt.ObjectiveID != admission.Objective.ID || receipt.TargetID != admission.Objective.TargetID || receipt.TrustedClass != admission.Objective.TrustedClass ||
			receipt.DeliveryID != admission.Objective.DeliveryID || receipt.ObjectiveScope != admission.ObjectiveScope || receipt.ObjectiveStatus != admission.ObjectiveStatus {
			return journalRecord{}, fmt.Errorf("committed transition fact in %s does not match its exact admission", path)
		}
		if err := validateReceiptWorkRelation(admission, *receipt); err != nil {
			return journalRecord{}, fmt.Errorf("committed transition fact in %s: %w", path, err)
		}
		if admission.ControlBundle != nil {
			targetFingerprint := admission.ControlBundle.Source.Fingerprint
			if admission.ControlBundle.Target != nil {
				targetFingerprint = admission.ControlBundle.Target.Fingerprint
			}
			if receipt.ControlBundleSourceFingerprint != admission.ControlBundle.Source.Fingerprint || receipt.ControlBundleTargetFingerprint != targetFingerprint {
				return journalRecord{}, fmt.Errorf("committed transition fact in %s does not match its admitted control bundle", path)
			}
		} else if receipt.ControlBundleSourceFingerprint != "" || receipt.ControlBundleTargetFingerprint != "" {
			return journalRecord{}, fmt.Errorf("committed transition fact in %s invents a control bundle", path)
		}
		if err := validateCommittedMutationFacts(record.TransitionClass, record.Mutations, receipt.ChangedStateFacets, receipt.CommittedEffects); err != nil {
			return journalRecord{}, fmt.Errorf("committed transition fact in %s: %w", path, err)
		}
	}
	if strings.HasSuffix(path, ".committed") && (record.Status != "committed" || record.Receipt == nil) {
		return journalRecord{}, fmt.Errorf("committed transaction journal %s lacks its canonical transition fact", path)
	}
	return record, nil
}

func validateReceiptWorkRelation(admission protocol.Admission, receipt protocol.TransitionReceipt) error {
	admittedFingerprint := ""
	if admission.Work != nil {
		admittedFingerprint = admission.Work.ResultFingerprint
	}
	if receipt.WorkResultFingerprint != admittedFingerprint {
		return fmt.Errorf("foreground-work identity does not match its exact admission")
	}
	return nil
}

func validateCommittedMutationFacts(class catalog.EventClass, mutations []ports.ResourceMutation, receiptFacets []model.StateFacet, facts []protocol.EffectFact) error {
	var mutationFacets []model.StateFacet
	for _, mutation := range mutations {
		mutationFacets = model.UnionStateFacets(mutationFacets, mutation.StateFacets)
	}
	if !slices.Equal(mutationFacets, receiptFacets) {
		return fmt.Errorf("receipt changed state facets %v do not match staged mutation facets %v", receiptFacets, mutationFacets)
	}
	resourceFacts := make([]protocol.EffectFact, 0, len(facts))
	boundarySettled := false
	for _, fact := range facts {
		if fact.Kind == protocol.EffectResourceMutation {
			resourceFacts = append(resourceFacts, fact)
		} else if fact.Kind == protocol.EffectBoundarySettled {
			boundarySettled = true
		}
	}
	if class == catalog.EventOwnedExternal && !boundarySettled {
		return fmt.Errorf("owned external transaction lacks a settled boundary fact")
	}
	if len(resourceFacts) != len(mutations) {
		return fmt.Errorf("resource fact count %d does not match staged mutation count %d", len(resourceFacts), len(mutations))
	}
	matched := make([]bool, len(resourceFacts))
	for _, mutation := range mutations {
		operation := "update"
		switch {
		case mutation.Delete:
			operation = "delete"
		case mutation.TargetLink != "":
			operation = "symlink"
		case !mutation.PriorExists:
			operation = "create"
		}
		prior := mutationStateFingerprint(mutation.PriorExists, mutation.Prior, mutation.PriorLink, mutation.Mode)
		result := mutationStateFingerprint(!mutation.Delete, mutation.Target, mutation.TargetLink, mutation.Mode)
		found := false
		for index, fact := range resourceFacts {
			if !matched[index] && fact.Target == mutation.Path && fact.Operation == operation && fact.PriorFingerprint == prior && fact.ResultingFingerprint == result {
				matched[index], found = true, true
				break
			}
		}
		if !found {
			return fmt.Errorf("staged mutation %s has no exact committed effect fact", mutation.Path)
		}
	}
	return nil
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
	if status != "committed" {
		record.ReceiptID = ""
		record.Receipt = nil
	}
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
	if err := receipt.Validate(); err != nil {
		return err
	}
	name, err := journalName(receipt.AdmissionID, ".pending")
	if err != nil {
		return err
	}
	path, ok := j.activePath(name)
	if !ok {
		return fmt.Errorf("transaction journal path is not bound for %s", receipt.AdmissionID)
	}
	record, err := readJournal(path)
	if err != nil {
		return err
	}
	if record.Admission.ID != receipt.AdmissionID || record.TransitionID != receipt.TransitionID {
		return fmt.Errorf("transition fact does not match its transaction journal")
	}
	record.Status, record.Reason, record.ReceiptID, record.UpdatedAt = "committed", "", receipt.ID, j.clock.Now().UTC()
	receiptCopy := receipt
	record.Receipt = &receiptCopy
	raw, err := encodeJSON(record)
	if err != nil {
		return err
	}
	// Persist the complete fact into the pending record, then atomically rename
	// that same record. A crash exposes either recovery-required pending work or
	// one canonical committed fact, never a separate success that outruns it.
	if err := atomicWrite(path, raw, 0o600); err != nil {
		return err
	}
	finalPath := strings.TrimSuffix(path, ".pending") + ".committed"
	if _, statErr := os.Stat(finalPath); statErr == nil {
		return fmt.Errorf("committed transaction journal already exists for %s", receipt.AdmissionID)
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if err := replaceFile(path, finalPath); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		if rollbackErr := replaceFile(finalPath, path); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("restore pending journal after directory sync failure: %w", rollbackErr))
		}
		return err
	}
	j.unbind(name)
	return nil
}

func (j *Journal) Abort(ctx context.Context, admissionID, reason string) error {
	return j.finalize(ctx, admissionID, ".aborted", "aborted", reason, "")
}

func (j *Journal) RequireRecovery(ctx context.Context, admissionID, reason string) error {
	return j.update(ctx, admissionID, func(record *journalRecord) {
		record.Status, record.Reason = "recovery-required", reason
		record.ReceiptID, record.Receipt = "", nil
	})
}

// scanJournal is used by recovery and tests. It never mutates a record.
func scanJournal(path string) (journalRecord, error) { return readJournal(path) }

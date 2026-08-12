package effects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
)

func (d Driver) prepareRecoveryReplay(ctx context.Context, layout ports.ControllerLayout, admission protocol.Admission, transition catalog.Transition) (ports.PreparedEffect, error) {
	transactionID, _ := admission.Parameters.Get("transaction_id")
	record, pendingPath, err := loadInterruptedJournal(layout, transactionID)
	if err != nil {
		return nil, err
	}
	if record.TransitionClass == catalog.EventOwnedExternal {
		return nil, fmt.Errorf("transaction %s may have an external effect; reconcile or escalate instead of replaying it", transactionID)
	}
	if transition.ID == "workspace.reconcile" {
		return d.prepareWorkspaceCutReconciliation(ctx, layout, admission, record, pendingPath)
	}
	resume := transition.ID == "recovery.resume"
	if resume && len(record.Mutations) == 0 {
		return nil, fmt.Errorf("transaction %s has no staged mutation manifest to resume", transactionID)
	}
	mutations := make([]ports.ResourceMutation, 0, len(record.Mutations)+2)
	for _, original := range record.Mutations {
		if err := validateRecoveryPath(layout, record.Admission, original.Path); err != nil {
			return nil, err
		}
		var target []byte
		var targetLink string
		deleteResource := false
		if resume {
			target = original.Target
			targetLink = original.TargetLink
			deleteResource = original.Delete
		} else if original.PriorLink != "" {
			targetLink = original.PriorLink
		} else if original.PriorExists {
			target = original.Prior
		} else {
			deleteResource = true
		}
		mutation, mutationErr := mutationForExactResource(original.Path, target, targetLink, os.FileMode(original.Mode), original.InstallLast, deleteResource)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, mutation)
	}
	mutations, err = d.advanceRecoveredState(layout, admission, transition.ID, mutations)
	if err != nil {
		return nil, err
	}
	closure, err := prepareJournalClosureFromRecord(pendingPath, record, string(transition.ID), d.clock.Now())
	if err != nil {
		return nil, err
	}
	mutations = append(mutations, closure...)
	changed, err := recoveryStateFacets(record, transition.ID, admission.Invocation, mutations)
	if err != nil {
		return nil, err
	}
	mutations = annotateStateFacetMutations(mutations, changed)
	return &preparedEffect{mutations: mutations, changedStateFacets: changed}, nil
}

func (d Driver) prepareWorkspaceCutReconciliation(ctx context.Context, layout ports.ControllerLayout, admission protocol.Admission, record journalRecord, pendingPath string) (ports.PreparedEffect, error) {
	if record.TransitionID != "workspace.cut" {
		return nil, fmt.Errorf("workspace reconciliation has no safe contract for interrupted transition %q", record.TransitionID)
	}
	destination, err := canonicalWorkspaceDestination(record.Admission)
	if err != nil {
		return nil, err
	}
	resume := false
	var verificationInvocation *model.InvocationContext
	if _, statErr := os.Stat(destination); statErr == nil {
		current, resolveErr := d.resolver.ResolveInvocation(ctx, destination, admission.Invocation.Host, admission.Invocation.Correlation)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve partially created workspace: %w", resolveErr)
		}
		expectedID, deriveErr := model.DeriveWorktreeID(admission.Invocation.GitCommonID, destination)
		if deriveErr != nil {
			return nil, deriveErr
		}
		branch, _ := record.Admission.Parameters.Get("branch")
		if current.RepositoryID != admission.Invocation.RepositoryID || current.GitCommonID != admission.Invocation.GitCommonID ||
			current.WorktreeID != expectedID || current.Ref != "refs/heads/"+branch ||
			current.ControllerID != admission.Invocation.ControllerID || current.Topology != admission.Invocation.Topology {
			return nil, fmt.Errorf("partially created workspace identity conflicts with interrupted admission")
		}
		resume = true
		verificationInvocation = &current
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}

	mutations := make([]ports.ResourceMutation, 0, len(record.Mutations)+2)
	for _, original := range record.Mutations {
		if err := validateRecoveryPath(layout, record.Admission, original.Path); err != nil {
			return nil, err
		}
		var target []byte
		deleteResource := false
		if resume {
			target, deleteResource = original.Target, original.Delete
		} else if original.PriorExists {
			target = original.Prior
		} else {
			deleteResource = true
		}
		mutation, mutationErr := mutationFor(original.Path, target, os.FileMode(original.Mode), original.InstallLast, deleteResource)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, mutation)
	}
	mutations, err = d.advanceRecoveredState(layout, admission, "workspace.reconcile", mutations)
	if err != nil {
		return nil, err
	}
	closure, err := prepareJournalClosureFromRecord(pendingPath, record, string("workspace.reconcile"), d.clock.Now())
	if err != nil {
		return nil, err
	}
	mutations = append(mutations, closure...)
	changed, err := recoveryStateFacets(record, "workspace.reconcile", admission.Invocation, mutations)
	if err != nil {
		return nil, err
	}
	mutations = annotateStateFacetMutations(mutations, changed)
	return &preparedEffect{mutations: mutations, verifyInvocation: verificationInvocation, changedStateFacets: changed}, nil
}

func recoveryStateFacets(record journalRecord, recovery catalog.TransitionID, invocation model.InvocationContext, mutations []ports.ResourceMutation) ([]model.StateFacet, error) {
	staged, err := journalStateFacets(record.Mutations)
	if err != nil {
		return nil, err
	}
	allowed := model.UnionStateFacets(catalog.DurableStateWritesForRecovery(record.TransitionID), []model.StateFacet{model.StateFacetControl})
	if _, err := validateAllowedStateFacets(recovery, staged, allowed); err != nil {
		return nil, err
	}
	changed, err := mutationStateFacets(invocation, mutations)
	if err != nil {
		return nil, err
	}
	return validateAllowedStateFacets(recovery, changed, allowed)
}

func (d Driver) advanceRecoveredState(layout ports.ControllerLayout, admission protocol.Admission, transition catalog.TransitionID, mutations []ports.ResourceMutation) ([]ports.ResourceMutation, error) {
	resultingRevision, err := durable.NextRevision(admission.ExpectedStateRevision)
	if err != nil {
		return nil, err
	}
	advanced := false
	for index := range mutations {
		mutation := &mutations[index]
		if mutation.Delete && mutation.TargetLink == "" && filepath.Clean(mutation.Path) == filepath.Clean(layout.StatePath) {
			state := durable.Default(admission.Invocation, d.clock.Now())
			state.ProgramFingerprint = admission.ExpectedProgramFingerprint
			state.Revision = resultingRevision
			state.LastTransition = transition
			state.UpdatedAt = d.clock.Now().UTC()
			mutation.Target, err = durable.EncodeState(state)
			if err != nil {
				return nil, err
			}
			mutation.Delete = false
			mutation.InstallLast = true
			advanced = true
			continue
		}
		if mutation.Delete || mutation.TargetLink != "" || filepath.Base(mutation.Path) != "state.json" || len(mutation.Target) == 0 {
			continue
		}
		state, err := durable.DecodeState(mutation.Target)
		if err != nil {
			continue
		}
		state.Revision = resultingRevision
		state.LastTransition = transition
		state.UpdatedAt = d.clock.Now().UTC()
		mutation.Target, err = durable.EncodeState(state)
		if err != nil {
			return nil, err
		}
		advanced = true
	}
	if advanced {
		return mutations, nil
	}
	state, err := loadDurableState(layout.StatePath, admission.Invocation, d.clock.Now())
	if err != nil {
		return nil, err
	}
	if state.Revision != admission.ExpectedStateRevision {
		return nil, fmt.Errorf("recovery state revision changed after admission")
	}
	state.Revision = resultingRevision
	state.LastTransition = transition
	state.UpdatedAt = d.clock.Now().UTC()
	raw, err := durable.EncodeState(state)
	if err != nil {
		return nil, err
	}
	mutation, err := mutationFor(layout.StatePath, raw, 0o600, true, false)
	if err != nil {
		return nil, err
	}
	return append(mutations, mutation), nil
}

func loadInterruptedJournal(layout ports.ControllerLayout, transactionID string) (journalRecord, string, error) {
	name, err := journalName(transactionID, ".pending")
	if err != nil {
		return journalRecord{}, "", err
	}
	path := filepath.Join(layout.JournalRoot, name)
	record, err := readJournal(path)
	if err != nil {
		return journalRecord{}, "", fmt.Errorf("read interrupted transaction %s: %w", transactionID, err)
	}
	if record.Admission.ID != transactionID {
		return journalRecord{}, "", fmt.Errorf("interrupted transaction identity mismatch")
	}
	return record, path, nil
}

func prepareJournalClosure(layout ports.ControllerLayout, transactionID, outcome string, now time.Time, excludeAdmissionID string) ([]ports.ResourceMutation, error) {
	var journals []struct {
		path   string
		record journalRecord
	}
	if record, path, err := loadInterruptedJournal(layout, transactionID); err == nil {
		journals = append(journals, struct {
			path   string
			record journalRecord
		}{path: path, record: record})
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	entries, err := os.ReadDir(layout.JournalRoot)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".pending" || entry.Name() == excludeAdmissionID+".pending" || entry.Name() == transactionID+".pending" {
			continue
		}
		path := filepath.Join(layout.JournalRoot, entry.Name())
		record, readErr := readJournal(path)
		if readErr != nil {
			return nil, readErr
		}
		parent, ok := record.Admission.Parameters.Get("transaction_id")
		if record.TransitionClass == catalog.EventRecovery && ok && parent == transactionID {
			journals = append(journals, struct {
				path   string
				record journalRecord
			}{path: path, record: record})
		}
	}
	if len(journals) == 0 {
		return nil, fmt.Errorf("interrupted transaction group %s has no pending journal", transactionID)
	}
	var mutations []ports.ResourceMutation
	for _, journal := range journals {
		closure, closureErr := prepareJournalClosureFromRecord(journal.path, journal.record, outcome, now)
		if closureErr != nil {
			return nil, closureErr
		}
		mutations = append(mutations, closure...)
	}
	return mutations, nil
}

func prepareJournalClosureFromRecord(pendingPath string, record journalRecord, outcome string, now time.Time) ([]ports.ResourceMutation, error) {
	record.Status = "recovered"
	record.Reason = strings.TrimSpace(outcome)
	record.ReceiptID, record.Receipt = "", nil
	record.UpdatedAt = now.UTC()
	raw, err := encodeJSON(record)
	if err != nil {
		return nil, err
	}
	archivePath := strings.TrimSuffix(pendingPath, ".pending") + ".recovered"
	archive, err := mutationFor(archivePath, raw, 0o600, false, false)
	if err != nil {
		return nil, err
	}
	removePending, err := mutationFor(pendingPath, nil, 0o600, true, true)
	if err != nil {
		return nil, err
	}
	return []ports.ResourceMutation{archive, removePending}, nil
}

func validateRecoveryPath(layout ports.ControllerLayout, admission protocol.Admission, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("interrupted transaction contains a non-absolute resource path")
	}
	for _, root := range []string{layout.RepositoryRoot, layout.StateRoot, layout.SharedRoot, layout.EmbeddedStateRoot, layout.ExternalStateRoot} {
		if root == "" {
			continue
		}
		relative, err := filepath.Rel(root, path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	if destination, ok := admission.Parameters.Get("destination"); ok {
		relative, err := filepath.Rel(destination, path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("interrupted transaction resource escapes managed roots: %s", path)
}

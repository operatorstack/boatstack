package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// featurePathPattern extracts the feature slug from a managed-artifact path such
// as ".product-loop/features/<feature>/plan.lock.json". The mutation boundary is
// domain-agnostic, but the undo verb needs the owning feature to decide whether a
// reversal would strand delivery state.
var featurePathPattern = regexp.MustCompile(`(?:^|/)\.product-loop/features/([a-z0-9]+(?:-[a-z0-9]+)*)/`)

// ListMutationReceipts returns every durable mutation receipt in the repository,
// most recent first. It backs the read-only `mutation-status` verb so an agent can
// discover the receipt to reverse (and, for redo, the undo receipt to reverse in
// turn — the boundary is closed under inversion).
func ListMutationReceipts(repoPath string) ([]MutationReceipt, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return nil, err
	}
	directory, err := mutationDirectory(repo)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []MutationReceipt{}, nil
	}
	if err != nil {
		return nil, err
	}
	receipts := make([]MutationReceipt, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		receipt, ok, loadErr := loadMutationReceipt(repo, id)
		if loadErr != nil {
			return nil, loadErr
		}
		if ok {
			receipts = append(receipts, receipt)
		}
	}
	sort.SliceStable(receipts, func(i, j int) bool {
		return receipts[i].RecordedAt > receipts[j].RecordedAt
	})
	return receipts, nil
}

// GetMutationReceipt loads a single receipt by id for `mutation-status --mutation`.
func GetMutationReceipt(repoPath, mutationID string) (MutationReceipt, bool, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return MutationReceipt{}, false, err
	}
	return loadMutationReceipt(repo, strings.TrimSpace(mutationID))
}

// UndoManagedMutation is the agent-facing, state-aware undo verb. It reverses a
// Boatstack-generated managed artifact by re-applying the receipt's inverse through
// the same transactional boundary (UndoMutation), but first refuses any reversal
// that would strand delivery state: undoing a plan activation (or its compiled
// artifacts) once a delivery gate receipt exists would remove the plan lock the
// delivery state depends on, deadlocking the workflow at INVALID_STATE. The
// primitive stays domain-agnostic; this thin wrapper carries the domain guard,
// mirroring how RepairState self-guards.
func UndoManagedMutation(repoPath, mutationID string) (MutationReceipt, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return MutationReceipt{}, err
	}
	id := strings.TrimSpace(mutationID)
	receipt, ok, err := loadMutationReceipt(repo, id)
	if err != nil {
		return MutationReceipt{}, err
	}
	if !ok {
		return MutationReceipt{}, fmt.Errorf("no mutation receipt for %s", id)
	}
	if governsPlanActivation(receipt.Kind) {
		feature := featureForMutation(receipt)
		if feature != "" {
			progressed, gate, guardErr := deliveryProgressed(repo, feature)
			if guardErr != nil {
				return MutationReceipt{}, guardErr
			}
			if progressed {
				return MutationReceipt{}, fmt.Errorf("refusing to undo %s (%s): delivery has progressed for feature %s (%s); undo would strand delivery state without its plan lock", id, receipt.Kind, feature, gate)
			}
		}
	}
	return UndoMutation(repo, id)
}

// governsPlanActivation reports whether a mutation kind promotes the managed plan
// artifacts whose removal a live delivery state depends on.
func governsPlanActivation(kind string) bool {
	kind = strings.TrimSpace(kind)
	return kind == "plan-activation" || kind == "compiled-plan"
}

// featureForMutation extracts the owning feature slug from a receipt's changed
// paths, or "" when the receipt does not touch a feature-scoped artifact.
func featureForMutation(receipt MutationReceipt) string {
	for _, change := range receipt.Changes {
		if match := featurePathPattern.FindStringSubmatch(filepath.ToSlash(change.Path)); match != nil {
			return match[1]
		}
	}
	for _, path := range receipt.Scope {
		if match := featurePathPattern.FindStringSubmatch(filepath.ToSlash(path)); match != nil {
			return match[1]
		}
	}
	return ""
}

// deliveryProgressed reports whether any delivery gate receipt has been recorded
// for the feature, which is the point past which undoing the plan activation would
// strand delivery state. It returns a short human-readable reason on true.
func deliveryProgressed(repo, feature string) (bool, string, error) {
	statePath, err := deliveryStatePath(repo, feature)
	if err != nil {
		return false, "", err
	}
	receiptsDir := filepath.Join(filepath.Dir(statePath), "receipts")
	sliceDirs, err := os.ReadDir(receiptsDir)
	if os.IsNotExist(err) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	for _, sliceDir := range sliceDirs {
		if !sliceDir.IsDir() {
			continue
		}
		gates, gatesErr := os.ReadDir(filepath.Join(receiptsDir, sliceDir.Name()))
		if gatesErr != nil {
			return false, "", gatesErr
		}
		for _, gate := range gates {
			if strings.HasSuffix(gate.Name(), ".json") {
				return true, fmt.Sprintf("gate receipt %s/%s", sliceDir.Name(), strings.TrimSuffix(gate.Name(), ".json")), nil
			}
		}
	}
	return false, "", nil
}

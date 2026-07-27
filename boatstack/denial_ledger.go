package boatstack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// The sibling harness's paid canary recorded the trajectory this law exists to
// stop: an agent under repair pressure repeated the same denied move sixteen
// times and then escalated into a protected-boundary write. Repetition of one
// denial is the signal that the stated law is not reaching the model — so a
// repeat denial escalates its corrective information (the full solution set
// plus a fresh diagnostic probe), never its severity. The law is pure
// optimization: it changes WHAT a denial says, never what is denied or
// allowed, and a broken ledger degrades to the unescalated rendering — a
// denial must never turn into a crash or an allow because bookkeeping failed.
// control-law: repeated-denials-escalate-to-solutions

const (
	// denialEscalationThreshold is the identical-denial count at which the
	// rendering escalates (matches the fresh-probe discipline: two repeats of
	// the same failure without a new probe is thrash).
	denialEscalationThreshold = 3
	// denialLedgerMaxKeys bounds the ledger; the oldest keys are pruned.
	denialLedgerMaxKeys = 32
)

type denialLedgerEntry struct {
	Count    int   `json:"count"`
	LastUnix int64 `json:"last_unix"`
}

type denialLedger struct {
	Counts map[string]denialLedgerEntry `json:"counts"`
}

// denialLedgerKey identifies "the same denial": the category at the workflow
// stage it fired. Two different categories never cross-escalate.
func denialLedgerKey(finding SafetyFinding) string {
	return finding.Category + "\x00" + finding.WorkflowStage
}

func denialLedgerPath(repo string) (string, bool) {
	w, err := ResolveWorkspaceContext(repo)
	if err != nil {
		return "", false
	}
	base, err := w.GuardDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(base, "denials.json"), true
}

func loadDenialLedger(path string) denialLedger {
	ledger := denialLedger{Counts: map[string]denialLedgerEntry{}}
	value, err := os.ReadFile(path)
	if err != nil {
		return ledger
	}
	// An unreadable or corrupt ledger starts fresh — fail-calm, never fail-open
	// or crash.
	var parsed denialLedger
	if json.Unmarshal(value, &parsed) == nil && parsed.Counts != nil {
		ledger = parsed
	}
	if ledger.Counts == nil {
		ledger.Counts = map[string]denialLedgerEntry{}
	}
	return ledger
}

func saveDenialLedger(path string, ledger denialLedger) {
	// Prune to the bound, dropping the oldest keys first.
	if len(ledger.Counts) > denialLedgerMaxKeys {
		type aged struct {
			key  string
			last int64
		}
		entries := make([]aged, 0, len(ledger.Counts))
		for key, entry := range ledger.Counts {
			entries = append(entries, aged{key, entry.LastUnix})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].last > entries[j].last })
		for _, stale := range entries[denialLedgerMaxKeys:] {
			delete(ledger.Counts, stale.key)
		}
	}
	value, err := json.Marshal(ledger)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return // fail-calm: persistence is best-effort
	}
	_ = os.WriteFile(path, value, 0o644)
}

// recordDenial bumps the finding's repeat count in the per-worktree ledger and
// returns the new count. Every failure mode returns a usable count of at least
// 1 — bookkeeping can degrade the escalation, never the denial itself.
func recordDenial(repo string, finding SafetyFinding) int {
	path, ok := denialLedgerPath(repo)
	if !ok {
		return 1
	}
	ledger := loadDenialLedger(path)
	key := denialLedgerKey(finding)
	entry := ledger.Counts[key]
	entry.Count++
	entry.LastUnix = time.Now().Unix()
	ledger.Counts[key] = entry
	saveDenialLedger(path, ledger)
	return entry.Count
}

// resetDenialLedger clears the ledger. Called when the guard ALLOWS a
// mutation-capable call: forward progress means the agent is no longer stuck,
// so stale history must not escalate the next unrelated denial.
func resetDenialLedger(repo string) {
	path, ok := denialLedgerPath(repo)
	if !ok {
		return
	}
	_ = os.Remove(path)
}

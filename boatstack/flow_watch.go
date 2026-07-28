package boatstack

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// `flow watch` is the bounded waiting primitive for the asynchronous world a
// published PR lives in (CI runs, reviews land, merges happen). Each tick it
// re-runs the same read-only frontier observation and compares a stable
// signature of every row; it EXITS on the first change, on an all-terminal
// frontier, or at the deadline — it never acts on what it sees. Boatstack
// stays a synchronous oracle: the loop here only decides when to ask the
// oracle again, and hands control back the moment the answer differs. No
// daemon, no writes, no transition execution path is reachable from it.
// control-law: watch-observes-and-exits-never-acts
const flowWatchSchemaVersion = 1

const (
	WatchOutcomeChanged  = "changed"
	WatchOutcomeTerminal = "terminal"
	WatchOutcomeTimeout  = "timeout"
)

// Seams for tests: the watcher must be provable without real waiting.
var (
	flowWatchNow   = time.Now
	flowWatchSleep = time.Sleep
)

type FlowWatchOptions struct {
	Repo     string
	Interval time.Duration
	Timeout  time.Duration
}

// FlowWatchResult reports why the watch loop returned and what it saw. Final
// always carries the last observed frontier so the caller re-orients without
// another resolution.
type FlowWatchResult struct {
	SchemaVersion int          `json:"schema_version"`
	Outcome       string       `json:"outcome"`
	Ticks         int          `json:"ticks"`
	ChangedRows   []string     `json:"changed_rows,omitempty"`
	Final         FlowFrontier `json:"final"`
}

const (
	defaultWatchInterval = 30 * time.Second
	defaultWatchTimeout  = 30 * time.Minute
	// minimumWatchInterval keeps a mistyped interval from hammering GitHub.
	minimumWatchInterval = 5 * time.Second
)

// WatchFrontier runs the bounded observe-compare loop. It returns an error
// only for the faults ResolveFrontier itself refuses (unreadable store,
// invalid config); a failing gh observation degrades each row to an Unknown
// phase — a signature like any other — and the loop stays bounded.
func WatchFrontier(options FlowWatchOptions) (FlowWatchResult, error) {
	interval := options.Interval
	if interval <= 0 {
		interval = defaultWatchInterval
	}
	if interval < minimumWatchInterval {
		interval = minimumWatchInterval
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultWatchTimeout
	}

	result := FlowWatchResult{SchemaVersion: flowWatchSchemaVersion}
	initial, err := ResolveFrontier(options.Repo)
	if err != nil {
		return result, err
	}
	result.Final = initial
	if frontierAllTerminal(initial) {
		result.Outcome = WatchOutcomeTerminal
		return result, nil
	}
	baseline := frontierSignatures(initial)
	deadline := flowWatchNow().Add(timeout)
	for {
		if !flowWatchNow().Before(deadline) {
			result.Outcome = WatchOutcomeTimeout
			return result, nil
		}
		flowWatchSleep(interval)
		result.Ticks++
		current, err := ResolveFrontier(options.Repo)
		if err != nil {
			return result, err
		}
		result.Final = current
		signatures := frontierSignatures(current)
		if changed := signatureDiff(baseline, signatures); len(changed) > 0 {
			result.Outcome = WatchOutcomeChanged
			result.ChangedRows = changed
			return result, nil
		}
	}
}

// frontierAllTerminal reports whether nothing on the frontier can move: no
// rows at all, or every row terminal. Blocked and operator rows are NOT
// terminal — external state (a review, a merge, a fix landing elsewhere) can
// change them, which is exactly what a watcher waits for.
func frontierAllTerminal(frontier FlowFrontier) bool {
	if !frontier.Initialized || len(frontier.Rows) == 0 {
		return true
	}
	for _, row := range frontier.Rows {
		if row.Actor != string(NextActorNone) {
			return false
		}
	}
	return true
}

// frontierSignatures reduces each row to the stable facts a caller would act
// on: position, actor, lifecycle, and the failing-check set. Reasons and
// prescribed command text are deliberately excluded — wording changes are not
// frontier changes.
func frontierSignatures(frontier FlowFrontier) map[string]string {
	signatures := map[string]string{}
	for _, row := range frontier.Rows {
		key := row.Feature + "/" + row.Slice
		signatures[key] = strings.Join([]string{
			row.Stage, row.Lifecycle, row.PRPhase, row.Actor, row.NextOperation,
			fmt.Sprintf("blocked=%t", row.Blocked),
			strings.Join(row.PRFailingChecks, "|"),
		}, "·")
	}
	return signatures
}

func signatureDiff(before, after map[string]string) []string {
	changed := []string{}
	for key, value := range after {
		if previous, ok := before[key]; !ok || previous != value {
			changed = append(changed, key)
		}
	}
	for key := range before {
		if _, ok := after[key]; !ok {
			changed = append(changed, key+" (gone)")
		}
	}
	sort.Strings(changed)
	return changed
}

// FormatFlowWatch renders the watch outcome and the final frontier.
func FormatFlowWatch(result FlowWatchResult) string {
	var b strings.Builder
	switch result.Outcome {
	case WatchOutcomeChanged:
		fmt.Fprintf(&b, "Watch: frontier changed after %d tick(s): %s\n", result.Ticks, strings.Join(result.ChangedRows, ", "))
	case WatchOutcomeTerminal:
		b.WriteString("Watch: nothing on the frontier can move; not waiting.\n")
	case WatchOutcomeTimeout:
		fmt.Fprintf(&b, "Watch: no frontier change within the timeout (%d tick(s)).\n", result.Ticks)
	}
	b.WriteString(FormatFlowFrontier(result.Final))
	return b.String()
}

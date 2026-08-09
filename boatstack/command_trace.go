package boatstack

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// CommandTraceInput is the bounded, secret-free observation supplied by the
// helper dispatcher. Raw arguments and process I/O are intentionally absent.
type CommandTraceInput struct {
	Repo       string
	Verb       string
	Category   string
	Feature    string
	Slice      string
	Transition deliverycontrol.TransitionID
	StartedAt  time.Time
	FinishedAt time.Time
	ExitCode   int
}

func commandAuthorityFingerprint(repo, feature string) string {
	if strings.TrimSpace(feature) == "" {
		return ""
	}
	path := filepath.Join(WorkspaceFor(repo).FeatureDir(feature), "autonomy.md")
	value, err := loadJSONObject(path, "autonomy receipt", autonomyMarkerStart, autonomyMarkerEnd, true)
	if err != nil {
		return ""
	}
	return stringValue(value["fingerprint"])
}

func commandOperationFingerprint(repo, feature, slice string) string {
	receipts, err := operationReceipts(repo)
	if err != nil {
		return ""
	}
	selected := OperationReceipt{}
	for _, receipt := range receipts {
		if receipt.State == OperationSucceeded || receipt.State == OperationFailedFinal {
			continue
		}
		if feature != "" && receipt.Scope.Feature != "" && receipt.Scope.Feature != feature {
			continue
		}
		if slice != "" && receipt.Scope.Slice != "" && receipt.Scope.Slice != slice {
			continue
		}
		if selected.UpdatedAt == "" || receipt.UpdatedAt > selected.UpdatedAt {
			selected = receipt
		}
	}
	return selected.PackageFingerprint
}

func resolveCommandFeature(repo, feature, slice string) (string, string) {
	if feature != "" {
		return feature, slice
	}
	active, err := ActiveManagedDeliveries(repo)
	if err != nil || len(active) != 1 {
		return "", slice
	}
	feature = active[0]
	if slice == "" {
		if state, stateErr := LoadDeliveryState(repo, feature); stateErr == nil {
			if activeSlice, sliceErr := activeDeliverySlice(state); sliceErr == nil {
				slice = activeSlice.ID
			}
		}
	}
	return feature, slice
}

// RecordCommandEvent appends one best-effort shadow event. Telemetry is never a
// control point: all failures and panics are swallowed after the command result
// has already been decided.
func RecordCommandEvent(input CommandTraceInput) {
	defer func() { _ = recover() }()
	if strings.TrimSpace(input.Verb) == "" || strings.TrimSpace(input.Category) == "" || input.StartedAt.IsZero() || input.FinishedAt.IsZero() {
		return
	}
	if strings.TrimSpace(os.Getenv(flowTraceKillSwitch)) == "0" {
		return
	}
	repo, err := ResolveRepository(input.Repo)
	if err != nil {
		return
	}
	feature, slice := resolveCommandFeature(repo, strings.TrimSpace(input.Feature), strings.TrimSpace(input.Slice))
	outcome := "succeeded"
	if input.ExitCode == 2 {
		outcome = "usage_error"
	} else if input.ExitCode != 0 {
		outcome = "failed"
	}
	duration := input.FinishedAt.Sub(input.StartedAt)
	if duration < 0 {
		return
	}
	directory, err := flowLogDirectory(repo)
	if err != nil {
		return
	}
	_ = deliverycontrol.AppendCommandEvent(directory, deliverycontrol.CommandEvent{
		Verb: input.Verb, Category: input.Category, Feature: feature, Slice: slice,
		Transition: input.Transition, StartedAt: input.StartedAt.UTC().Format(time.RFC3339Nano),
		FinishedAt: input.FinishedAt.UTC().Format(time.RFC3339Nano), DurationMS: duration.Milliseconds(),
		ExitCode: input.ExitCode, Outcome: outcome,
		AuthorityFingerprint: commandAuthorityFingerprint(repo, feature),
		OperationFingerprint: commandOperationFingerprint(repo, feature, slice),
	})
}

package boatstack

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

// control-law: registry-mirrors-real-transitions
// The deliverycontrol registry is a second projection of the real delivery
// state machine in this package. These tests are the boundary that keeps the two
// from drifting: every HandlerRef must name a real exported function here, and
// the registry's slice-status states must equal the DeliverySlice.Status
// literals the real machine uses.

// realDeliveryHandlers maps the exported functions the registry may reference to
// their values. Referencing the values makes this fail to COMPILE if any handler
// is renamed or removed, so a registry HandlerRef can never point at a function
// that no longer exists.
var realDeliveryHandlers = map[string]any{
	"ActivatePlan":              ActivatePlan,
	"RecordDeliveryGate":        RecordDeliveryGate,
	"RecordChangeObservation":   RecordChangeObservation,
	"RecordJourneyResults":      RecordJourneyResults,
	"PublishPR":                 PublishPR,
	"UndoManagedMutation":       UndoManagedMutation,
	"DiscardDelivery":           DiscardDelivery,
	"RepairState":               RepairState,
	"IgnoreDelivery":            IgnoreDelivery,
	"CurrentDeliveryState":      CurrentDeliveryState,
	"CheckDeliveryReadyForShip": CheckDeliveryReadyForShip,
	"ResolveNext":               ResolveNext,
	"ResolveRecovery":           ResolveRecovery,
	"WritePlanningArtifact":     WritePlanningArtifact,
	"RecordApproval":            RecordApproval,
}

func TestRegistryHandlerRefsAreRealFunctions(t *testing.T) {
	for _, tr := range deliverycontrol.Transitions() {
		if _, ok := realDeliveryHandlers[tr.HandlerRef]; !ok {
			t.Errorf("transition %s references handler %q which is not a known real delivery function", tr.ID, tr.HandlerRef)
		}
	}
}

func TestRegistrySliceStatusMatchesRealLiterals(t *testing.T) {
	// The real slice lifecycle literals derive from a single source —
	// DeliverySliceStatuses() in delivery.go, which the real DeliverySlice.Status
	// assignments and the nextForDelivery switch use as named constants. A new
	// slice status therefore cannot be introduced as a raw literal that this
	// hand-maintained test would miss: it must appear in DeliverySliceStatuses(),
	// and the registry's SliceStatusStates() must then match it.
	realLiterals := map[string]bool{}
	for _, s := range DeliverySliceStatuses() {
		realLiterals[s] = true
	}
	registryStatus := map[string]bool{}
	for _, s := range deliverycontrol.SliceStatusStates() {
		registryStatus[string(s)] = true
	}
	for lit := range realLiterals {
		if !registryStatus[lit] {
			t.Errorf("registry SliceStatusStates is missing real literal %q", lit)
		}
	}
	for s := range registryStatus {
		if !realLiterals[s] {
			t.Errorf("registry declares slice-status %q that the real machine does not use", s)
		}
	}
}

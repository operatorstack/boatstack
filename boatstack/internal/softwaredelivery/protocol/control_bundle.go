package protocol

import (
	"fmt"

	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func RequiresControlBundle(transition catalog.Transition) bool {
	if transition.ExecutionContext == "advance" {
		return true
	}
	switch transition.ID {
	case "runtime.hydrate", "runtime.replace", "runtime.reconcile",
		"installation.initialize", "installation.update", "installation.reconcile-update", "catalog.reconcile":
		return true
	default:
		return false
	}
}

func ProjectControlBundle(snapshot model.Snapshot, transition catalog.Transition, parameters Parameters, bundle *boatstackruntime.ControlBundleContract) (*boatstackruntime.ControlBundleContract, error) {
	if bundle == nil || !RequiresControlBundle(transition) || transition.ExecutionContext == "advance" {
		return bundle, nil
	}
	identity := boatstackruntime.Identity{}
	if transition.ID == "catalog.reconcile" {
		if bundle.SourceRuntimePin == nil {
			return nil, fmt.Errorf("CONTROL_BUNDLE_TARGET_INVALID: catalog reconciliation has no trusted runtime pin")
		}
		identity = bundle.SourceRuntimePin.Identity()
	} else {
		version, versionOK := parameters.Get("runtime_version")
		sha256, shaOK := parameters.Get("runtime_sha256")
		source, sourceOK := parameters.Get("source_revision")
		if !versionOK || !shaOK || !sourceOK {
			return nil, fmt.Errorf("CONTROL_BUNDLE_TARGET_INVALID: runtime mutation lacks exact candidate identity")
		}
		identity = boatstackruntime.Identity{Version: version, SHA256: sha256, SourceRevision: source}
	}
	targetPin := boatstackruntime.NewPin(
		identity,
		snapshot.ProgramFingerprint,
		durable.StateSchemaVersion,
	)
	pinRaw, err := boatstackruntime.EncodePin(targetPin)
	if err != nil {
		return nil, fmt.Errorf("CONTROL_BUNDLE_TARGET_INVALID: %w", err)
	}
	base := bundle.Source
	if bundle.Target != nil {
		base = *bundle.Target
	}
	target, err := boatstackruntime.ReplaceControlBundleFile(base, ".boatstack/runtime.json", pinRaw)
	if err != nil {
		return nil, err
	}
	projected, err := boatstackruntime.NewControlBundleContractWithPins(bundle.Source, &target, bundle.TargetRevision, bundle.SourceRuntimePin, &targetPin)
	if err != nil {
		return nil, err
	}
	return &projected, nil
}

func ValidateControlBundleForTransition(bundle *boatstackruntime.ControlBundleContract, transition catalog.Transition) error {
	if bundle == nil {
		if RequiresControlBundle(transition) {
			return fmt.Errorf("CONTROL_BUNDLE_REQUIRED: transition %q requires an exact repository control bundle", transition.ID)
		}
		return nil
	}
	if err := bundle.Validate(); err != nil {
		return err
	}
	if RequiresControlBundle(transition) && bundle.Target == nil {
		return fmt.Errorf("CONTROL_BUNDLE_REQUIRED: transition %q requires a target bundle", transition.ID)
	}
	return nil
}

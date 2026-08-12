package effects

import (
	"context"
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

const (
	kernelStateResource = "boatstack.kernel.state"
	kernelStateOwner    = "boatstack.kernel"
)

// BindStateRevision appends the kernel-owned logical state commit to a
// protocol-backed program or extension effect. Native effects already include
// this mutation in Driver.Prepare.
func BindStateRevision(ctx context.Context, prepared ports.PreparedEffect, resolver ports.InvocationResolver, clock ports.Clock, admission protocol.Admission, transition catalog.Transition) (ports.PreparedEffect, error) {
	effect, ok := prepared.(*preparedEffect)
	if !ok {
		return nil, fmt.Errorf("state revision binding requires a Boatstack prepared effect")
	}
	if err := bindPreparedCapabilities(effect, admission, transition); err != nil {
		return nil, err
	}
	layout, invocation, err := resolver.ResolveLayout(ctx, admission.Invocation)
	if err != nil {
		return nil, err
	}
	if invocation.RepositoryID != admission.Invocation.RepositoryID || invocation.GitCommonID != admission.Invocation.GitCommonID || invocation.WorktreeID != admission.Invocation.WorktreeID {
		return nil, fmt.Errorf("effect invocation identity changed before revision binding")
	}
	state, err := loadDurableState(layout.StatePath, admission.Invocation, clock.Now())
	if err != nil {
		return nil, err
	}
	if state.Revision != admission.ExpectedStateRevision {
		return nil, fmt.Errorf("durable state revision changed before revision binding")
	}
	if state.ProgramFingerprint != "" && state.ProgramFingerprint != admission.ExpectedProgramFingerprint {
		return nil, fmt.Errorf("compiled control program changed before revision binding")
	}
	before := state
	state.Revision, err = durable.NextRevision(state.Revision)
	if err != nil {
		return nil, err
	}
	if state.ProgramFingerprint == "" {
		state.ProgramFingerprint = admission.ExpectedProgramFingerprint
	}
	state.LastTransition = transition.ID
	state.UpdatedAt = clock.Now().UTC()
	raw, err := durable.EncodeState(state)
	if err != nil {
		return nil, err
	}
	mutation, err := mutationFor(layout.StatePath, raw, 0o600, true, false)
	if err != nil {
		return nil, err
	}
	mutation.Resource, mutation.Owner = kernelStateResource, kernelStateOwner
	effect.mutations = append(effect.mutations, mutation)
	changed, err := changedStateFacets([2]durable.State{before, state})
	if err != nil {
		return nil, err
	}
	changed, err = validateTransitionStateFacets(transition, changed)
	if err != nil {
		return nil, err
	}
	effect.mutations = annotateStateFacetMutations(effect.mutations, changed)
	effect.changedStateFacets = model.UnionStateFacets(effect.changedStateFacets, changed)
	return effect, nil
}

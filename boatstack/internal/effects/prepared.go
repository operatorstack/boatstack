package effects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
)

type boundaryCall func(context.Context) (ports.EffectResult, error)

type preparedEffect struct {
	mutations        []ports.ResourceMutation
	boundary         boundaryCall
	verifyInvocation *model.InvocationContext
	applied          []ports.ResourceMutation
	externalSettled  bool
}

func (p *preparedEffect) Manifest() []ports.ResourceMutation {
	result := make([]ports.ResourceMutation, len(p.mutations))
	copy(result, p.mutations)
	return result
}

func (p *preparedEffect) VerificationInvocation() (model.InvocationContext, bool) {
	if p.verifyInvocation == nil {
		return model.InvocationContext{}, false
	}
	return *p.verifyInvocation, true
}

func (p *preparedEffect) Execute(ctx context.Context) (ports.EffectResult, error) {
	result := ports.EffectResult{Settlement: ports.EffectSettled}
	if p.boundary != nil {
		var err error
		result, err = p.boundary(ctx)
		if err != nil || result.Settlement == ports.EffectUnknown {
			return result, err
		}
		p.externalSettled = true
	}
	ordered := append([]ports.ResourceMutation(nil), p.mutations...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].InstallLast != ordered[j].InstallLast {
			return !ordered[i].InstallLast
		}
		return ordered[i].Path < ordered[j].Path
	})
	for _, mutation := range ordered {
		var err error
		if mutation.Delete {
			err = os.Remove(mutation.Path)
			if os.IsNotExist(err) {
				err = nil
			}
		} else if mutation.TargetLink != "" {
			err = atomicSymlink(mutation.Path, mutation.TargetLink)
		} else {
			err = atomicWrite(mutation.Path, mutation.Target, os.FileMode(mutation.Mode))
		}
		if err != nil {
			return result, fmt.Errorf("install managed resource %s: %w", mutation.Path, err)
		}
		p.applied = append(p.applied, mutation)
	}
	return result, nil
}

func (p *preparedEffect) Rollback(context.Context) error {
	var rollbackErrors []error
	for index := len(p.applied) - 1; index >= 0; index-- {
		mutation := p.applied[index]
		if mutation.PriorLink != "" {
			if err := atomicSymlink(mutation.Path, mutation.PriorLink); err != nil {
				rollbackErrors = append(rollbackErrors, err)
			}
		} else if mutation.PriorExists {
			if err := atomicWrite(mutation.Path, mutation.Prior, os.FileMode(mutation.Mode)); err != nil {
				rollbackErrors = append(rollbackErrors, err)
			}
		} else if err := os.Remove(mutation.Path); err != nil && !os.IsNotExist(err) {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	p.applied = nil
	if p.externalSettled {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("external effect settled and requires reconciliation or compensation"))
	}
	return errors.Join(rollbackErrors...)
}

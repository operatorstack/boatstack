// Package extension contains public helpers for trusted in-process Boatstack
// extensions. Trust is assigned by the application compiling the program.
package extension

import (
	"context"
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/control"
)

type InProcess struct {
	manifest control.ExtensionManifest
	runtime  control.ExtensionRuntime
}

func NewInProcess(manifest control.ExtensionManifest, runtime control.ExtensionRuntime) (*InProcess, error) {
	if runtime == nil {
		return nil, fmt.Errorf("in-process extension requires a runtime")
	}
	return &InProcess{manifest: manifest, runtime: runtime}, nil
}

func (e *InProcess) ExtensionManifest(context.Context) (control.ExtensionManifest, error) {
	return e.manifest, nil
}

func (e *InProcess) Runtime() control.ExtensionRuntime { return e.runtime }

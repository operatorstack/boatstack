// Package extension contains public helpers for trusted in-process Boatstack
// extensions. Trust is assigned by the application compiling the program.
package extension

import (
	"context"
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/delivery"
)

type InProcess struct {
	manifest delivery.ExtensionManifest
	runtime  delivery.ExtensionRuntime
}

func NewInProcess(manifest delivery.ExtensionManifest, runtime delivery.ExtensionRuntime) (*InProcess, error) {
	if runtime == nil {
		return nil, fmt.Errorf("in-process extension requires a runtime")
	}
	return &InProcess{manifest: manifest, runtime: runtime}, nil
}

func (e *InProcess) ExtensionManifest(context.Context) (delivery.ExtensionManifest, error) {
	return e.manifest, nil
}

func (e *InProcess) Runtime() delivery.ExtensionRuntime { return e.runtime }

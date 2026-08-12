// Package testprogram assembles first-party definitions for compatibility and
// parity tests. Product code must use distribution or an explicit compiler.
package testprogram

import (
	"context"

	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
)

// StandardRegistry compiles the real CoreSystem and StandardFlow declaration
// bytes. It panics only in tests when a checked first-party manifest is invalid.
func StandardRegistry() catalog.Registry {
	program, err := delivery.Compile(context.Background(), delivery.CompileRequest{
		KernelVersion: "test-kernel",
		Core:          core.System(),
		Runtime:       standard.Definition(),
	})
	if err != nil {
		panic(err)
	}
	return program.RuntimeRegistry()
}

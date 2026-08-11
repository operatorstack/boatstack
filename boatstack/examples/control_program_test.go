package examples_test

import (
	"context"
	"fmt"

	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/extension/releasenote"
	"github.com/operatorstack/boatstack/boatstack/flow/standard"
	"github.com/operatorstack/boatstack/boatstack/sdk"
)

func Example_standardFlowWithReleaseNoteExtension() {
	program, err := control.Compile(context.Background(), control.CompileRequest{
		KernelVersion: "example-kernel",
		Core:          core.System(),
		Runtime:       standard.Definition(),
		Extensions:    []control.Extension{releasenote.Definition()},
	})
	if err != nil {
		panic(err)
	}
	summary := program.Summary()
	fmt.Printf("%s + %s + %s: %d transitions\n", summary.Core.ID, summary.Runtime.ID, summary.Extensions[0].ID, summary.TotalTransitionCount)
	// Output:
	// boatstack.core + boatstack.standard + boatstack.release-note: 64 transitions
}

func Example_sdkCustomKernel() {
	_, err := sdk.NewKernel("",
		sdk.WithProgramRuntime(standard.Definition()),
		sdk.WithExtension(releasenote.Definition()),
	)
	fmt.Println(err == nil)
	// Output:
	// true
}

package kernel_test

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/kernel/conformance"
)

func TestRuntimeConformance(t *testing.T) {
	conformance.IntegerFixture().Run(t)
}

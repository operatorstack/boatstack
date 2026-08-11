package sdk_test

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/sdk"
)

func TestPublicProtocolCanBeConstructedWithoutInternalPackages(t *testing.T) {
	request := sdk.Request{
		SchemaVersion: sdk.SchemaVersion,
		Operation:     sdk.OperationResolve,
		Repository:    t.TempDir(),
		Host:          "mcp",
		CorrelationID: "correlation",
		Goal:          sdk.Goal{ID: "goal", Kind: sdk.GoalVerified, DeliveryID: "delivery"},
	}
	if request.Goal.Kind != sdk.GoalVerified || request.Operation != sdk.OperationResolve {
		t.Fatalf("public V2 aliases lost protocol identity: %#v", request)
	}
}

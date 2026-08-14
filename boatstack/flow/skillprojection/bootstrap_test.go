package skillprojection

import (
	"strings"
	"testing"
)

func TestBootstrapContractFailsClosedBeforeFlowExecution(t *testing.T) {
	// control-law: generated-skills-report-bootstrap-recovery-without-executing-it
	contract := BootstrapContract("v9.9.10")
	for _, expected := range []string{
		"command -v boatstack", "Get-Command boatstack", ".boatstack/runtime.json",
		"BOATSTACK_LAUNCHER_NOT_FOUND", "BOATSTACK_RUNTIME_PIN_MISSING",
		"BOATSTACK_RUNTIME_PIN_INVALID", "explicit approval", "Never run it",
		"creates no\nFlow run ID", "resume this same requested entry",
		"BOATSTACK_MODE=hydrate", "BOATSTACK_VERSION=<exact-version>",
		"BOATSTACK_EXPECTED_RUNTIME_SHA256=<exact-sha256>", "/v9.9.10/install.sh",
	} {
		if !strings.Contains(contract, expected) {
			t.Fatalf("bootstrap contract lacks %q", expected)
		}
	}
	if strings.Contains(contract, "BOATSTACK_VERSION=latest") {
		t.Fatal("bootstrap contract permits mutable latest selection")
	}
	if strings.Contains(contract, "BOATSTACK_MODE=update") {
		t.Fatal("bootstrap contract permits repository mutation during runtime recovery")
	}
}

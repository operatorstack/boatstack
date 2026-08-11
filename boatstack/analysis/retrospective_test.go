package analysis_test

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/analysis"
)

func TestRetrospectiveIsAReadOnlyPublicProjection(t *testing.T) {
	report, err := analysis.DeriveRetrospective(analysis.FormatPlaintext, "fixture", []byte("User: always run the exact test\nUser: always run the exact test\nUser: always run the exact test\n"))
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion == 0 {
		t.Fatalf("invalid retrospective report: %#v", report)
	}
}

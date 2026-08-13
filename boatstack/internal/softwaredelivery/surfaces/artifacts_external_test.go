package surfaces_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/distribution"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

func TestCheckedArchitectureArtifactsMatchCompiledStandardProgram(t *testing.T) {
	program, err := distribution.StandardProgram(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	transitions := program.Transitions()
	checks := map[string]string{
		"boatstack-transition-catalog.md":  surfaces.RenderCatalogMarkdown(transitions),
		"boatstack-transition-catalog.mmd": surfaces.RenderCatalogMermaid(transitions),
		"boatstack-standard-flow.mmd":      surfaces.RenderStandardFlowMermaid(transitions),
	}
	safety, err := surfaces.RenderCatalogLocusSafety(transitions)
	if err != nil {
		t.Fatal(err)
	}
	liveness, err := surfaces.RenderCatalogLocusLiveness(transitions)
	if err != nil {
		t.Fatal(err)
	}
	checks["boatstack-locus-safety.json"] = safety
	checks["boatstack-locus-liveness.json"] = liveness

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate checked architecture artifacts")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	for name, expected := range checks {
		actual, err := os.ReadFile(filepath.Join(repositoryRoot, "docs", "architecture", name))
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != expected {
			t.Errorf("%s drifted; regenerate it with boatstack catalog", name)
		}
	}
}

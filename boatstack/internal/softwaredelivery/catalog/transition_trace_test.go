package catalog

import (
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func TestFacetConditionReasonDoesNotExposeNamespacedValues(t *testing.T) {
	evidence := model.Evidence{Source: "fixture", Fingerprint: "fixture", ObservedAt: time.Unix(1, 0).UTC()}
	snapshot := model.Snapshot{Observation: model.Observation{ProgramFacts: map[string]model.Fact[string]{
		"test.synthetic.secret": model.Known("private-actual-value", evidence),
	}}}
	condition := FacetCondition{
		Facet: "test.synthetic.secret", Statuses: []model.FactStatus{model.FactKnown}, Values: []string{"private-allowed-value"},
	}
	allowed, reason := condition.Evaluate(snapshot)
	if allowed || strings.Contains(reason, "private-actual-value") || strings.Contains(reason, "private-allowed-value") {
		t.Fatalf("rejected namespaced value leaked through reason %q", reason)
	}
	condition.Values = []string{"private-actual-value"}
	allowed, reason = condition.Evaluate(snapshot)
	if !allowed || strings.Contains(reason, "private-actual-value") {
		t.Fatalf("accepted namespaced value leaked through reason %q", reason)
	}
}

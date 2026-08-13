package softwaredelivery_test

import (
	"encoding/json"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
)

func TestPlanInboxForEntryMatchesProductionResolverContract(t *testing.T) {
	config := json.RawMessage(`{"path":".boatstack/plans/inbox","cardinality":"exactly-one"}`)
	valid := controlprogram.Entry{ID: "run", Inputs: []controlprogram.EntryInput{{
		ID: "plan", Type: "markdown-file", Required: true, Resolver: softwareflow.PlanInboxResolver, Config: config,
	}}}
	if inbox, err := softwareflow.PlanInboxForEntry(valid); err != nil || inbox.Path != ".boatstack/plans/inbox" {
		t.Fatalf("production plan inbox = %+v, %v", inbox, err)
	}
	clone := func() controlprogram.Entry {
		entry := valid
		entry.Inputs = append([]controlprogram.EntryInput(nil), valid.Inputs...)
		return entry
	}
	invalid := map[string]func(*controlprogram.Entry){
		"missing":        func(entry *controlprogram.Entry) { entry.Inputs = nil },
		"extra":          func(entry *controlprogram.Entry) { entry.Inputs = append(entry.Inputs, entry.Inputs[0]) },
		"optional":       func(entry *controlprogram.Entry) { entry.Inputs[0].Required = false },
		"wrong-id":       func(entry *controlprogram.Entry) { entry.Inputs[0].ID = "source" },
		"wrong-type":     func(entry *controlprogram.Entry) { entry.Inputs[0].Type = "text" },
		"wrong-resolver": func(entry *controlprogram.Entry) { entry.Inputs[0].Resolver = "repository.file" },
		"unknown-config": func(entry *controlprogram.Entry) {
			entry.Inputs[0].Config = json.RawMessage(`{"path":".boatstack/plans/inbox","cardinality":"exactly-one","extra":true}`)
		},
		"malformed-config": func(entry *controlprogram.Entry) { entry.Inputs[0].Config = json.RawMessage(`{"path":`) },
		"escaping-path": func(entry *controlprogram.Entry) {
			entry.Inputs[0].Config = json.RawMessage(`{"path":"../inbox","cardinality":"exactly-one"}`)
		},
	}
	for name, mutate := range invalid {
		t.Run(name, func(t *testing.T) {
			entry := clone()
			mutate(&entry)
			if _, err := softwareflow.PlanInboxForEntry(entry); err == nil {
				t.Fatal("invalid production entry input was accepted")
			}
		})
	}
}

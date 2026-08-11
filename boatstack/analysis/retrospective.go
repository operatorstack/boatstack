// Package analysis exposes passive, deterministic evidence analysis that never
// participates in lifecycle authority or managed writes.
package analysis

import "github.com/operatorstack/boatstack/boatstack/internal/retromine"

const (
	FormatNeutral    = retromine.FormatNeutral
	FormatClaudeCode = retromine.FormatClaudeCode
	FormatPlaintext  = retromine.FormatPlaintext
)

type Event = retromine.Event
type EventRef = retromine.EventRef
type Cluster = retromine.Cluster
type Proposal = retromine.Proposal
type Report = retromine.Report

// DeriveRetrospective performs a read-only transcript projection and returns
// proposals for human review. It cannot admit or execute a kernel transition.
func DeriveRetrospective(format, source string, content []byte) (Report, error) {
	events, err := retromine.ParseTranscript(format, source, content)
	if err != nil {
		return Report{}, err
	}
	return retromine.BuildReport(events), nil
}

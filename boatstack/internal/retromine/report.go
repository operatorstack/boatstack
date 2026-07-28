package retromine

// The report is the miner's entire output surface: typed proposals for the
// classified recurrences, and the unclassified recurrences named so nothing
// is silently dropped. It is data for a human to review — the derivation
// proposes, and promotion into a real state, verb, setpoint, or guard is
// always a reviewed change made by hand.
// control-law: retro-proposes-never-enforces
const ReportSchemaVersion = 1

// Proposal is one recurring instruction promoted to a typed suggestion.
type Proposal struct {
	GapType        string     `json:"gap_type"`
	Occurrences    int        `json:"occurrences"`
	Sessions       []string   `json:"sessions"`
	Exemplar       string     `json:"exemplar"`
	SuggestedShape string     `json:"suggested_shape"`
	Evidence       []EventRef `json:"evidence"`
}

// Report is the full derivation result over one set of transcripts.
type Report struct {
	SchemaVersion  int        `json:"schema_version"`
	EventsScanned  int        `json:"events_scanned"`
	OperatorEvents int        `json:"operator_events"`
	Proposals      []Proposal `json:"proposals"`
	// Unclassified recurrences are surfaced — a recurrence the lexicon cannot
	// place is still steady-state error worth a human look — but they never
	// become proposals (fail-closed).
	Unclassified []Cluster `json:"unclassified,omitempty"`
}

// BuildReport mines the events and classifies every recurrence.
func BuildReport(events []Event) Report {
	report := Report{SchemaVersion: ReportSchemaVersion, EventsScanned: len(events), Proposals: []Proposal{}}
	for _, event := range events {
		if event.Role == RoleOperator {
			report.OperatorEvents++
		}
	}
	for _, cluster := range DetectRecurrence(events) {
		gapType := ClassifyGap(cluster.Normalized)
		if gapType == GapUnclassified {
			report.Unclassified = append(report.Unclassified, cluster)
			continue
		}
		report.Proposals = append(report.Proposals, Proposal{
			GapType:        gapType,
			Occurrences:    cluster.Occurrences,
			Sessions:       cluster.Sessions,
			Exemplar:       cluster.Exemplar,
			SuggestedShape: SuggestedShape(gapType),
			Evidence:       cluster.Evidence,
		})
	}
	return report
}

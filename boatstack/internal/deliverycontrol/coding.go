package deliverycontrol

// CodingSignal is one recorded unit of coding effort — the work of writing a fix,
// distinct from navigating the delivery flow. It is telemetry only: coding effort
// is never modeled as a graph, never optimized, and never gated. Keeping it in its
// own record type is what guarantees J_coding can never leak into J_flow or
// regret, per the J = J_flow + J_coding decomposition.
type CodingSignal struct {
	Sequence int    `json:"sequence"`
	Units    int    `json:"units"`
	Note     string `json:"note,omitempty"`
}

// CodingEffort is the tally of coding signals in a session: the summed units
// (J_coding) and how many signals produced it. It stands beside J_flow in a
// report; the two figures are never added together.
type CodingEffort struct {
	JCoding int `json:"j_coding"`
	Signals int `json:"signals"`
}

// TallyCoding sums coding signals into J_coding. A signal with non-positive units
// counts as a single unit, so a bare "coding work happened" marker still
// registers exactly one unit of effort.
func TallyCoding(signals []CodingSignal) CodingEffort {
	effort := CodingEffort{}
	for _, s := range signals {
		units := s.Units
		if units <= 0 {
			units = 1
		}
		effort.JCoding += units
		effort.Signals++
	}
	return effort
}

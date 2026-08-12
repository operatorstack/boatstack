package retromine

import "strings"

// Gap classification names WHICH typed construct a recurring instruction is
// compensating for. The four gap types are the four ways a controller can be
// missing a term:
//
//	missing_observation — the operator keeps asking what the system could show
//	missing_verb        — the operator keeps describing an action to take
//	missing_setpoint    — the operator keeps restating a objective or condition to
//	                      pursue ("until", "every time", "at least")
//	missing_guard       — the operator keeps warning what must not happen
//
// The classifier is a deterministic keyword lexicon over the normalized
// instruction, with fixed precedence guard > setpoint > observation > verb:
// a guard misclassified as a verb could become an action proposal, so the
// constraining readings win. Anything the lexicon cannot place lands in
// unclassified, which is REPORTED but never generates a proposal.
// control-law: retro-proposes-never-enforces
const (
	GapObservation  = "missing_observation"
	GapVerb         = "missing_verb"
	GapSetpoint     = "missing_setpoint"
	GapGuard        = "missing_guard"
	GapUnclassified = "unclassified"
)

// The lexicons match either whole tokens or normalized phrases. Normalization
// has already lowered the text and stripped punctuation ("don't" → "don t").
var (
	guardPhrases = []string{"don t", "do not", "make sure not", "must not", "never", "only if", "unless", "be careful", "avoid", "without asking", "instead of"}
	guardTokens  = []string{"dont", "stop"}

	setpointPhrases = []string{"until", "at least", "at most", "within", "every time", "each time", "whenever", "keep doing", "always", "from now on", "before you finish", "when green", "when it passes"}

	observationPhrases = []string{"check the", "check whether", "check if", "what is the", "what s the", "show me", "look at", "status of", "is it", "did it", "how is", "where is", "monitor"}

	verbTokens = []string{"run", "merge", "publish", "push", "rerun", "retry", "open", "record", "fix", "update", "deploy", "rebase", "commit", "create", "install", "sync", "clean", "make"}
)

// ClassifyGap places one normalized instruction into a gap type.
func ClassifyGap(normalized string) string {
	padded := " " + normalized + " "
	containsPhrase := func(phrases []string) bool {
		for _, phrase := range phrases {
			if strings.Contains(padded, " "+phrase+" ") {
				return true
			}
		}
		return false
	}
	tokens := map[string]bool{}
	for _, token := range strings.Fields(normalized) {
		tokens[token] = true
	}
	containsToken := func(list []string) bool {
		for _, token := range list {
			if tokens[token] {
				return true
			}
		}
		return false
	}
	switch {
	case containsPhrase(guardPhrases) || containsToken(guardTokens):
		return GapGuard
	case containsPhrase(setpointPhrases):
		return GapSetpoint
	case containsPhrase(observationPhrases):
		return GapObservation
	case containsToken(verbTokens):
		return GapVerb
	default:
		return GapUnclassified
	}
}

// SuggestedShape names the typed construct to add for a gap type — prose
// pointing a human at the right kind of promotion, never a diff.
func SuggestedShape(gapType string) string {
	switch gapType {
	case GapObservation:
		return "Add a typed observation: a read-only status or frontier field that answers this without being asked."
	case GapVerb:
		return "Add or prescribe a typed verb: a deterministic command the flow names at the right state."
	case GapSetpoint:
		return "Add a typed setpoint: a persisted objective or condition (like delivery.terminal) the flow pursues so this stops being restated."
	case GapGuard:
		return "Add a typed guard: an enforced precondition or denial (a gate or policy) instead of a remembered warning."
	default:
		return ""
	}
}

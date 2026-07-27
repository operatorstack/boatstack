package boatstack

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// RenderNextStatusResponse renders the canonical user-facing response contract
// for the current workflow position: the branded banner, one friendly outcome
// line, and exactly one "### Next step" block carrying the exact runnable
// command when one is prescribable. Adapters present this output verbatim
// instead of re-deriving the state table from prose — the decision was always
// deterministic (ResolveNext + the prescription layer); this makes the
// rendering deterministic too.
//
// It obeys the banner law (banner-hides-internal-machinery): machine stage
// names and operation codes never appear as status prose. The prescribed
// command line is the one legitimate place helper verbs appear — it is the
// runnable next step, exactly as `flow next` prints it.
// control-law: response-contract-is-helper-rendered
func RenderNextStatusResponse(repo string, status NextStatus) (string, error) {
	next, err := nextControlFromStatus(repo, status)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(RenderNextStatusBanner(status))
	b.WriteString("\n" + sentenceCase(friendlyPhrase(status)) + ".\n")
	b.WriteString("\n### Next step\n\n")

	switch {
	case next.Prescribed != nil:
		writePrescribed(&b, next.Prescribed)
		if next.FollowUp != "" {
			fmt.Fprintf(&b, "Then: %s\n", next.FollowUp)
		}
		if next.SubAction != nil {
			title := next.SubAction.Title
			if title != "" {
				title = " — " + title
			}
			fmt.Fprintf(&b, "Next sub-action: %s%s (from the plan task DAG; see `flow tasks`)\n", next.SubAction.ID, title)
		}
		// The solution set as one short secondary sentence — the contract allows
		// exactly one, and the verbs appear only in command position.
		// control-law: solution-set-derives-from-guard-declarations
		if len(next.Alternatives) > 0 {
			verbs := make([]string, 0, solutionSetTextCap)
			for _, alt := range next.Alternatives {
				if len(verbs) == solutionSetTextCap {
					break
				}
				verbs = append(verbs, "`"+alt.Verb+"`")
			}
			fmt.Fprintf(&b, "Other legal moves: %s.\n", strings.Join(verbs, ", "))
		}
	case status.ObservedStage == "FEATURE_COMPLETE",
		status.ObservedStage == "PUBLISHED" && status.Lifecycle == "PUBLISHED_MERGED":
		b.WriteString("No action required.\n")
	case status.ObservedStage == "PUBLISHED":
		if status.PRURL != "" {
			fmt.Fprintf(&b, "Review the pull request: %s\n", status.PRURL)
		} else {
			b.WriteString("Review the pull request.\n")
		}
	case len(status.BlockingAmbiguity) > 0:
		b.WriteString(sentenceCase(friendlyBlockReason(status)) + ":\n")
		for _, candidate := range status.BlockingAmbiguity {
			fmt.Fprintf(&b, "- %s\n", candidate)
		}
	default:
		b.WriteString(sentenceCase(friendlyBlockReason(status)) + ".\n")
	}
	return b.String(), nil
}

// sentenceCase upper-cases the first rune of a friendly phrase so it can open
// a sentence without changing the phrase vocabulary.
func sentenceCase(phrase string) string {
	if phrase == "" {
		return phrase
	}
	first, size := utf8.DecodeRuneInString(phrase)
	return string(unicode.ToUpper(first)) + phrase[size:]
}

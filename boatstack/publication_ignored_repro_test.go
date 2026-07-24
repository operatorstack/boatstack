package boatstack

import "testing"

// TestPublicationBypassHonorsIgnoredDeliveries reproduces the external report on
// PR #322: `next`/`run` scope ambiguity through workflow.ignored_deliveries
// (see TestResolveNextIgnoredActiveDeliveryClearsAmbiguity), but the
// publication-authority path — publicationBypassFinding, which emits
// relation=ambiguous on a denied push — iterates ActiveManagedDeliveries
// directly and never filters ignored slugs. A single ignored, stale-but-active
// delivery (agentic-l3-full: APPROVED lock, never published) therefore poisons
// publication authority for every other delivery.
//
// This mirrors the `next` test: two active deliveries, one ignored, neither on
// the current branch. With the ignore list honored, only one active delivery
// remains and the finding must not be ambiguous.
func TestPublicationBypassHonorsIgnoredDeliveries(t *testing.T) {
	repo := nextTestRepo(t)
	writeNextDelivery(t, repo, "agentic-l3-full", "BUILD", 0) // stale, ignored blocker
	writeNextDelivery(t, repo, "roles-access-policies", "BUILD", 0)
	setIgnoredDeliveries(t, repo, "agentic-l3-full")

	finding, blocked := publicationBypassFinding(repo, "denied push", "tool-input")
	if !blocked {
		t.Fatalf("expected a publication finding for the remaining active delivery")
	}
	if finding.BranchRelation == "ambiguous" {
		t.Fatalf("ignored active delivery poisoned publication authority: BlockingFeature=%q relation=%q",
			finding.BlockingFeature, finding.BranchRelation)
	}
	if finding.BlockingFeature != "roles-access-policies" {
		t.Fatalf("expected the un-ignored delivery to be blocking, got %q", finding.BlockingFeature)
	}
}

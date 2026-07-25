package boatstack

import (
	"strings"
	"testing"
)

// Boundary: the human-facing status banner (RenderNextStatusBanner).
// Control law: banner-hides-internal-machinery — the banner is pure presentation
// of the read-only NextStatus. It must never surface an internal stage name or
// machine code, must never carry a logo/badge for Boatstack, must render exactly
// one "current" marker on a fixed four-node rail, and a blocked status must show
// the needs-you marker rather than a clean in-progress marker.

// The exhaustive enum value sets the banner must tolerate as inputs.
var (
	allObservedStages = []string{
		"BUILD", "TEST_PASSED", "REVIEW_PASSED", "PR_PREVIEW", "PUBLISHED",
		"FEATURE_COMPLETE", "NOT_INITIALIZED", "INVALID_STATE", "AMBIGUOUS",
		"POLICY_READY", "APPROVED", "DRAFT_PLAN", "NOT_STARTED",
	}
	allVerificationStatuses = []string{"BLOCKED", "VERIFIED", "UNVERIFIED"}
	allNextOperations       = []string{
		"build", "review-gate", "ship-gate", "none", "repair-state",
		"discard-delivery", "init", "resolve-ambiguity", "workspace-cut",
		"plan-gate", "workspace-cleanup", "auto-plan",
	}
	allLifecycles = []string{
		"", "PUBLISHED_UNKNOWN", "PUBLISHED_OPEN", "PUBLISHED_MERGED", "PUBLISHED_CLOSED",
	}

	// Tokens that would leak the internal machine into a user-facing surface.
	forbiddenBannerTokens = []string{
		"BUILD", "TEST_PASSED", "REVIEW_PASSED", "PR_PREVIEW", "PUBLISHED",
		"FEATURE_COMPLETE", "NOT_INITIALIZED", "NOT_STARTED", "INVALID_STATE",
		"AMBIGUOUS", "POLICY_READY", "APPROVED", "DRAFT_PLAN",
		"PUBLISHED_UNKNOWN", "PUBLISHED_OPEN", "PUBLISHED_MERGED", "PUBLISHED_CLOSED",
		"discard-delivery", "repair-state", "ship-gate", "review-gate", "plan-gate",
		"resolve-ambiguity", "workspace-cut", "workspace-cleanup", "auto-plan",
		"⚓", // no logo/badge for Boatstack itself
	}
)

// negative / bypass: no combination of inputs ever leaks internal machinery.
// control-law: banner-hides-internal-machinery
func TestBannerNeverLeaksInternalMachinery(t *testing.T) {
	for _, stage := range allObservedStages {
		for _, verification := range allVerificationStatuses {
			for _, op := range allNextOperations {
				for _, lifecycle := range allLifecycles {
					status := NextStatus{
						VerificationStatus: verification,
						ObservedStage:      stage,
						NextOperation:      op,
						Lifecycle:          lifecycle,
						Feature:            "roles-access",
						ActiveSlice:        "slice-2",
						SliceIndex:         2,
						TotalSlices:        4,
					}
					banner := RenderNextStatusBanner(status)
					for _, token := range forbiddenBannerTokens {
						if strings.Contains(banner, token) {
							t.Fatalf("banner leaked internal token %q for stage=%s verification=%s op=%s lifecycle=%s:\n%s",
								token, stage, verification, op, lifecycle, banner)
						}
					}
				}
			}
		}
	}
}

// positive: representative states render the expected rail, phrase, and wordmark.
// control-law: banner-hides-internal-machinery
func TestBannerRendersExpectedFriendlyStates(t *testing.T) {
	cases := []struct {
		name       string
		status     NextStatus
		wantRail   string
		wantPhrase string
	}{
		{
			name:       "building",
			status:     NextStatus{VerificationStatus: "VERIFIED", ObservedStage: "BUILD", NextOperation: "build", Feature: "roles-access", SliceIndex: 2, TotalSlices: 4},
			wantRail:   "✓──▸──·──·",
			wantPhrase: "building your changes",
		},
		{
			name:       "checking",
			status:     NextStatus{VerificationStatus: "VERIFIED", ObservedStage: "TEST_PASSED", NextOperation: "review-gate", Feature: "roles-access", SliceIndex: 2, TotalSlices: 4},
			wantRail:   "✓──✓──▸──·",
			wantPhrase: "checking your changes",
		},
		{
			name:       "ready to ship",
			status:     NextStatus{VerificationStatus: "VERIFIED", ObservedStage: "REVIEW_PASSED", NextOperation: "ship-gate", Feature: "roles-access", SliceIndex: 2, TotalSlices: 4},
			wantRail:   "✓──✓──✓──▸",
			wantPhrase: "ready to ship",
		},
		{
			name:       "complete",
			status:     NextStatus{VerificationStatus: "VERIFIED", ObservedStage: "FEATURE_COMPLETE", NextOperation: "none", Feature: "roles-access"},
			wantRail:   "✓──✓──✓──✱",
			wantPhrase: "complete",
		},
		{
			name:       "blocked needs you",
			status:     NextStatus{VerificationStatus: "BLOCKED", ObservedStage: "INVALID_STATE", NextOperation: "discard-delivery", Feature: "roles-access", SliceIndex: 2, TotalSlices: 4},
			wantRail:   "✓──▲──·──·",
			wantPhrase: "needs you: an old draft needs clearing before we continue",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			banner := RenderNextStatusBanner(tc.status)
			if !strings.Contains(banner, bannerWordmark) {
				t.Errorf("banner missing wordmark:\n%s", banner)
			}
			if !strings.Contains(banner, tc.wantRail) {
				t.Errorf("banner missing rail %q:\n%s", tc.wantRail, banner)
			}
			if !strings.Contains(banner, tc.wantPhrase) {
				t.Errorf("banner missing phrase %q:\n%s", tc.wantPhrase, banner)
			}
		})
	}
}

// relation: the rail is always exactly four nodes.
// control-law: banner-hides-internal-machinery
func TestBannerRailIsAlwaysFourNodes(t *testing.T) {
	for _, stage := range allObservedStages {
		nodes := journeyNodes(NextStatus{ObservedStage: stage, VerificationStatus: "VERIFIED"})
		if len(nodes) != 4 {
			t.Fatalf("stage %s produced %d nodes, want 4: %v", stage, len(nodes), nodes)
		}
	}
}

// relation: a non-terminal, non-blocked state has exactly one in-progress marker
// and no needs-you marker.
// control-law: banner-hides-internal-machinery
func TestBannerSingleCurrentMarkerWhenMidFlight(t *testing.T) {
	midFlight := []string{"NOT_STARTED", "DRAFT_PLAN", "POLICY_READY", "APPROVED", "BUILD", "TEST_PASSED", "REVIEW_PASSED", "PR_PREVIEW"}
	for _, stage := range midFlight {
		banner := RenderNextStatusBanner(NextStatus{ObservedStage: stage, VerificationStatus: "VERIFIED", Feature: "roles-access"})
		if got := strings.Count(banner, bannerGlyphNow); got != 1 {
			t.Errorf("stage %s: want exactly one %q, got %d:\n%s", stage, bannerGlyphNow, got, banner)
		}
		if strings.Contains(banner, bannerGlyphBlocked) {
			t.Errorf("stage %s: mid-flight banner must not show the needs-you marker %q:\n%s", stage, bannerGlyphBlocked, banner)
		}
	}
}

// bypass / failure-state: a BLOCKED status always shows the needs-you marker and
// never a clean in-progress marker — a stall can never masquerade as progress.
// control-law: banner-hides-internal-machinery
func TestBannerBlockedShowsNeedsYouNotProgress(t *testing.T) {
	for _, stage := range allObservedStages {
		if stage == "FEATURE_COMPLETE" || stage == "PUBLISHED" {
			continue // terminal states are not blockable on the rail
		}
		banner := RenderNextStatusBanner(NextStatus{ObservedStage: stage, VerificationStatus: "BLOCKED", NextOperation: "repair-state", Feature: "roles-access"})
		if !strings.Contains(banner, bannerGlyphBlocked) {
			t.Errorf("blocked stage %s must show the needs-you marker %q:\n%s", stage, bannerGlyphBlocked, banner)
		}
		if strings.Contains(banner, bannerGlyphNow) {
			t.Errorf("blocked stage %s must not show a clean in-progress marker %q:\n%s", stage, bannerGlyphNow, banner)
		}
		if !strings.Contains(banner, "needs you:") {
			t.Errorf("blocked stage %s must lead with a needs-you phrase:\n%s", stage, banner)
		}
	}
}

// relation: the no-project (UNVERIFIED) banner shows no rail at all.
// control-law: banner-hides-internal-machinery
func TestBannerUnverifiedShowsNoRail(t *testing.T) {
	banner := RenderNextStatusBanner(NextStatus{VerificationStatus: "UNVERIFIED", ObservedStage: "NOT_INITIALIZED", NextOperation: "init"})
	for _, glyph := range []string{bannerGlyphNow, bannerGlyphDone, bannerGlyphTodo, bannerGlyphBlocked} {
		if strings.Contains(banner, glyph) {
			t.Errorf("UNVERIFIED banner must not render a rail glyph %q:\n%s", glyph, banner)
		}
	}
	if !strings.Contains(banner, "not tracking a feature here yet") {
		t.Errorf("UNVERIFIED banner should explain nothing is tracked:\n%s", banner)
	}
}

// relation: subtitle uses the non-coder word "part" and is omitted for single-slice.
// control-law: banner-hides-internal-machinery
func TestBannerSubtitleUsesPartAndOmitsSingleSlice(t *testing.T) {
	multi := RenderNextStatusBanner(NextStatus{VerificationStatus: "VERIFIED", ObservedStage: "BUILD", Feature: "roles-access", SliceIndex: 2, TotalSlices: 4})
	if !strings.Contains(multi, "roles-access · part 2 of 4") {
		t.Errorf("multi-slice subtitle wrong:\n%s", multi)
	}
	single := RenderNextStatusBanner(NextStatus{VerificationStatus: "VERIFIED", ObservedStage: "BUILD", Feature: "roles-access", SliceIndex: 1, TotalSlices: 1})
	if strings.Contains(single, "part") {
		t.Errorf("single-slice subtitle should not mention parts:\n%s", single)
	}
}

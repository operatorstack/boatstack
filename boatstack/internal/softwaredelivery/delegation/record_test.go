package delegation_test

import (
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/delegation"
)

func request() delegation.Request {
	return delegation.Request{
		RunID: "run-example", ProgramID: "program", ProgramFingerprint: strings.Repeat("a", 64), ControlBundleFingerprint: strings.Repeat("c", 64), EntryID: "run",
		TargetID: "done", ObjectiveID: "objective", DeliveryID: "delivery", InputFingerprints: []string{"b", "a"},
		RepositoryID: "repository", GitCommonID: "common", InitialWorktreeID: "worktree", InitialRef: "refs/heads/main",
		BindingFingerprint: strings.Repeat("b", 64), RequestedAuthorities: []string{"human", "autonomy"}, Description: "Run the program",
	}
}

func TestRequestFingerprintCanonicalizesSetsAndBindsSemantics(t *testing.T) {
	left := request()
	right := request()
	right.InputFingerprints[0], right.InputFingerprints[1] = right.InputFingerprints[1], right.InputFingerprints[0]
	right.RequestedAuthorities[0], right.RequestedAuthorities[1] = right.RequestedAuthorities[1], right.RequestedAuthorities[0]
	leftFingerprint, err := left.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	rightFingerprint, err := right.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if leftFingerprint != rightFingerprint {
		t.Fatalf("equivalent request fingerprints differ: %s != %s", leftFingerprint, rightFingerprint)
	}
	right.TargetID = "other"
	changed, err := right.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if changed == leftFingerprint {
		t.Fatal("semantic request change preserved fingerprint")
	}
}

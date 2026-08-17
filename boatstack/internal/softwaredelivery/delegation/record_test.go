package delegation_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/delegation"
)

func request() delegation.Request {
	return delegation.Request{
		RunID: "run-example", ProgramID: "program", ProgramFingerprint: strings.Repeat("a", 64), ControlBundleFingerprint: strings.Repeat("c", 64), EntryID: "run",
		TargetID: "done", ObjectiveID: "objective", DeliveryID: "delivery", InputFingerprints: []string{"b", "a"},
		RepositoryID: "repository", GitCommonID: "common", InitialWorktreeID: "worktree", InitialRef: "refs/heads/main",
		BindingFingerprint: strings.Repeat("b", 64), HumanIdentityProviderFingerprint: strings.Repeat("d", 64), RequestedAuthorities: []string{"human", "autonomy"}, Description: "Run the program",
	}
}

func TestSupersededPathBindsRunAndExactRequest(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	path, err := delegation.SupersededPath("/controller", "run-example", fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join("/controller", "delegations", "run-example.superseded-aaaaaaaaaaaa.json") {
		t.Fatalf("superseded path = %q", path)
	}
	if _, err := delegation.SupersededPath("/controller", "../run", fingerprint); err == nil {
		t.Fatal("unsafe run identity produced a supersession path")
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

func TestRecordRejectsPriorSchemaAndIdentityProvenanceMismatch(t *testing.T) {
	value := request()
	requestFingerprint, err := value.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	record := delegation.Record{
		Schema: delegation.Schema, SchemaRevision: delegation.SchemaRevision,
		Request: value, RequestFingerprint: requestFingerprint, ReceiptID: "authorization-example",
		Actor: "operator", ActorIdentityProviderFingerprint: value.HumanIdentityProviderFingerprint,
		AuthorizedAt: time.Unix(1_700_000_000, 0).UTC(), Revision: 1, Status: "active",
	}
	write := func(value delegation.Record) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "record.json")
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if _, err := delegation.Load(write(record)); err != nil {
		t.Fatal(err)
	}
	prior := record
	prior.SchemaRevision--
	if _, err := delegation.Load(write(prior)); err == nil {
		t.Fatal("prior delegation schema was accepted")
	}
	drifted := record
	drifted.ActorIdentityProviderFingerprint = strings.Repeat("e", 64)
	if _, err := delegation.Load(write(drifted)); err == nil {
		t.Fatal("identity provenance mismatch was accepted")
	}
}

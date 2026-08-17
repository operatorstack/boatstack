package effects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/delegation"
)

func TestDelegationSupersessionArchiveIsImmutableAndIdempotent(t *testing.T) {
	request := delegation.Request{
		RunID: "run-example", ProgramID: "program", ProgramFingerprint: strings.Repeat("a", 64), ControlBundleFingerprint: strings.Repeat("b", 64),
		EntryID: "run", TargetID: "done", ObjectiveID: "objective", DeliveryID: "delivery", InputFingerprints: []string{"input"},
		RepositoryID: "repository", GitCommonID: "common", InitialWorktreeID: "worktree", InitialRef: "refs/heads/main",
		BindingFingerprint: strings.Repeat("c", 64), HumanIdentityRole: "developer", HumanIdentityProviderFingerprint: strings.Repeat("d", 64), RequestedAuthorities: []string{"autonomy"}, Description: "Run the program",
	}
	fingerprint, err := request.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	record := delegation.Record{
		Schema: delegation.Schema, SchemaRevision: delegation.SchemaRevision, Request: request, RequestFingerprint: fingerprint,
		ReceiptID: "authorization-one", Actor: "operator", ActorIdentityRole: request.HumanIdentityRole, ActorIdentityProviderFingerprint: request.HumanIdentityProviderFingerprint, AuthorizedAt: time.Unix(1_700_000_000, 0).UTC(), Revision: 1, Status: "revoked",
	}
	path := filepath.Join(t.TempDir(), "prior.json")
	if err := ArchiveDelegationRecord(path, record); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ArchiveDelegationRecord(path, record); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil || string(first) != string(second) {
		t.Fatalf("idempotent archive changed: %v", err)
	}
	record.Actor = "other"
	if err := ArchiveDelegationRecord(path, record); err == nil {
		t.Fatal("conflicting archive overwrote prior authority evidence")
	}
}

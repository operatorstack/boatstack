package protocol

import (
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

func capabilityAuthority(now time.Time, class catalog.AuthorityClass, id string) AuthorityBundle {
	return AuthorityBundle{Receipts: []AuthorityReceipt{{
		ID: id, Class: class, Subject: "test-source", Fingerprint: "test-fingerprint",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}}}
}

func capabilityTransition(required, declared []catalog.Capability) catalog.Transition {
	return catalog.Transition{
		ID: "program/write", Class: catalog.EventOwnedLocal, Effect: "program.write",
		RequiredCapabilities: required, DeclaredCapabilities: declared,
	}
}

func TestCapabilityProjectionRequiresDeclarationAndExternalGrant(t *testing.T) {
	// control-law: declaration narrows authority but never creates it
	now := time.Unix(100, 0).UTC()
	snapshot := model.Snapshot{}
	write := capabilityTransition(
		[]catalog.Capability{catalog.CapabilityRepositoryWrite},
		[]catalog.Capability{catalog.CapabilityRepositoryWrite},
	)
	if _, err := ProjectCapabilities(snapshot, write, AuthorityBundle{}, now); err == nil || !strings.Contains(err.Error(), "CAPABILITY_DENIED") {
		t.Fatalf("declaration without grant = %v", err)
	}
	projection, err := ProjectCapabilities(snapshot, write, capabilityAuthority(now, catalog.AuthorityRepository, "repository"), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Effective) != 1 || projection.Effective[0] != catalog.CapabilityRepositoryWrite {
		t.Fatalf("effective capabilities = %v", projection.Effective)
	}

	undeclared := write
	undeclared.DeclaredCapabilities = []catalog.Capability{catalog.CapabilityCommandExecute}
	if _, err := ProjectCapabilities(snapshot, undeclared, capabilityAuthority(now, catalog.AuthorityHuman, "human"), now); err == nil || !strings.Contains(err.Error(), "CAPABILITY_NOT_DECLARED") {
		t.Fatalf("broad host grant escaped program surface: %v", err)
	}
}

func TestCapabilityProjectionRejectsPartialGrant(t *testing.T) {
	// control-law: intersection never silently drops a required capability
	now := time.Unix(100, 0).UTC()
	transition := capabilityTransition(
		[]catalog.Capability{catalog.CapabilityRepositoryWrite, catalog.CapabilityPublicationPublish},
		[]catalog.Capability{catalog.CapabilityRepositoryWrite, catalog.CapabilityPublicationPublish},
	)
	_, err := ProjectCapabilities(model.Snapshot{}, transition, capabilityAuthority(now, catalog.AuthorityRepository, "repository"), now)
	if err == nil || !strings.Contains(err.Error(), `CAPABILITY_DENIED "publication.publish"`) {
		t.Fatalf("partial grant = %v", err)
	}
}

func TestHumanApprovalCannotBeSynthesizedByProgramDeclaration(t *testing.T) {
	// control-law: an approval declaration is not human authority
	now := time.Unix(100, 0).UTC()
	transition := capabilityTransition(
		[]catalog.Capability{catalog.CapabilityRepositoryWrite, catalog.CapabilityHumanApprove},
		[]catalog.Capability{catalog.CapabilityRepositoryWrite, catalog.CapabilityHumanApprove},
	)
	if _, err := ProjectCapabilities(model.Snapshot{}, transition, capabilityAuthority(now, catalog.AuthorityRepository, "repository"), now); err == nil || !strings.Contains(err.Error(), `CAPABILITY_DENIED "human.approve"`) {
		t.Fatalf("repository source synthesized approval: %v", err)
	}
	if _, err := ProjectCapabilities(model.Snapshot{}, transition, capabilityAuthority(now, catalog.AuthorityHuman, "human"), now); err != nil {
		t.Fatalf("human approval authority was rejected: %v", err)
	}
}

func TestRecoveryRequiresFreshMatchingAuthority(t *testing.T) {
	// control-law: a recovery transition cannot amplify the failed admission
	now := time.Unix(100, 0).UTC()
	recovery := capabilityTransition(
		[]catalog.Capability{catalog.CapabilityRepositoryWrite, catalog.CapabilityPublicationPublish},
		[]catalog.Capability{catalog.CapabilityRepositoryWrite, catalog.CapabilityPublicationPublish},
	)
	recovery.ID = "program/recover"
	recovery.Class = catalog.EventRecovery
	if _, err := ProjectCapabilities(model.Snapshot{}, recovery, capabilityAuthority(now, catalog.AuthorityRepository, "original"), now); err == nil || !strings.Contains(err.Error(), `CAPABILITY_DENIED "publication.publish"`) {
		t.Fatalf("recovery amplified authority: %v", err)
	}
}

func TestPublicationPrepareCannotCrossPublishEffectBoundary(t *testing.T) {
	transition := catalog.Transition{ID: "program/publish", Class: catalog.EventOwnedExternal, Effect: "publication.execute"}
	admission := Admission{EffectiveCapabilities: []catalog.Capability{
		catalog.CapabilityRepositoryWrite,
		catalog.CapabilityCommandExecute,
		catalog.CapabilityProductMutate,
		catalog.CapabilityPublicationPrepare,
	}}
	err := ValidateEffectCapabilities(admission, transition)
	if err == nil || !strings.Contains(err.Error(), `EFFECT_CAPABILITY_DENIED "publication.publish"`) {
		t.Fatalf("publication.prepare crossed publish boundary: %v", err)
	}
}

func TestAuthorityFingerprintBindsSourceIdentityNotRematerializationTime(t *testing.T) {
	// control-law: one stable authority source survives bounded re-materialization
	now := time.Unix(100, 0).UTC()
	one := capabilityAuthority(now, catalog.AuthorityRepository, "policy")
	two := one
	two.Receipts = append([]AuthorityReceipt(nil), one.Receipts...)
	two.Receipts[0].IssuedAt = now
	two.Receipts[0].ExpiresAt = now.Add(2 * time.Hour)
	oneFingerprint, err := one.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	twoFingerprint, err := two.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if oneFingerprint != twoFingerprint {
		t.Fatal("re-materializing the same authority source changed its identity")
	}
	two.Receipts[0].Fingerprint = "different-source"
	changed, err := two.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if changed == oneFingerprint {
		t.Fatal("authority source substitution preserved identity")
	}
}

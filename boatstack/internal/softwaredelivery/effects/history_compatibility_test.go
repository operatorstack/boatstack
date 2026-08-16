package effects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

func legacyContentID(t *testing.T, prefix string, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return prefix + hex.EncodeToString(digest[:])
}

func legacyCommittedRecord(t *testing.T, transitionID catalog.TransitionID, class catalog.EventClass, sequence, priorRevision uint64, objective model.Objective, invocation model.InvocationContext) journalRecord {
	t.Helper()
	now := time.Unix(1_700_000_000+int64(sequence), 0).UTC()
	authority := protocol.AuthorityBundle{Receipts: []protocol.AuthorityReceipt{{
		ID: "policy", Class: catalog.AuthorityRepository, Subject: "configuration:/repo/.boatstack/project.json", Fingerprint: "configuration-fingerprint",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}}}
	authorityFingerprint, err := authority.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	granted := authority.GrantedCapabilities(now)
	transition := catalog.Transition{
		ID: transitionID, Version: 1, Owner: "product-delivery", Effect: catalog.EffectID(transitionID), Class: class,
		TargetPredicate: "legacy target", Verifier: "legacy.verifier",
		Policy: catalog.PolicyContract{ObjectiveScope: catalog.ObjectiveScopeBoundExact},
	}
	admission := protocol.Admission{
		SchemaVersion:  protocol.PreviousAdmissionSchemaVersion,
		PrescriptionID: "prx-legacy-" + string(transitionID), TransitionID: transitionID, TransitionVersion: transition.Version,
		ExpectedStateRevision: priorRevision, ExpectedProgramFingerprint: strings.Repeat("a", 64),
		ExpectedSnapshotFingerprint: strings.Repeat("b", 64), ExpectedObjectiveBindingFingerprint: strings.Repeat("c", 64),
		SourceRevision: "legacy-head", WorktreeFingerprint: "legacy-worktree", SourcePhase: model.PhaseActive, Invocation: invocation,
		Objective: objective, ObjectiveScope: catalog.ObjectiveScopeBoundExact, Authority: authority, AuthorityFingerprint: authorityFingerprint,
		RequiredCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite}, GrantedCapabilities: granted,
		EffectiveCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite}, IdempotencyKey: "idem-legacy-" + string(transitionID),
		IssuedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	admission.ID = legacyContentID(t, "adm-", admission)
	if err := admission.ValidateCommittedHistoryIdentity(); err != nil {
		t.Fatalf("legacy admission fixture: %v", err)
	}

	mutation := ports.ResourceMutation{
		Resource: "software-delivery.state", Owner: transition.Owner, Path: "/repo/.boatstack/state.json",
		Prior: []byte("prior"), Target: []byte("target"), PriorExists: true, Mode: 0o600,
		StateFacets: []model.StateFacet{model.StateFacetControl, model.StateFacetProduct},
	}
	effects := []protocol.EffectFact{{
		Kind: protocol.EffectResourceMutation, EffectID: transition.Effect, Owner: transition.Owner, Resource: mutation.Resource,
		Target: mutation.Path, Operation: "update",
		PriorFingerprint: mutationStateFingerprint(true, mutation.Prior, "", mutation.Mode), ResultingFingerprint: mutationStateFingerprint(true, mutation.Target, "", mutation.Mode),
	}}
	if class == catalog.EventOwnedExternal {
		effects = append(effects, protocol.EffectFact{
			Kind: protocol.EffectBoundarySettled, EffectID: transition.Effect, Owner: transition.Owner,
			Target: string(transition.ID), Operation: "settled", PriorFingerprint: strings.Repeat("d", 64), ResultingFingerprint: strings.Repeat("e", 64),
		})
	}
	target := model.Snapshot{
		Observation: model.Observation{StateRevision: priorRevision + 1, Objective: model.Known(objective, model.Evidence{Source: "legacy", Fingerprint: "objective", ObservedAt: now})},
		Fingerprint: strings.Repeat("f", 64),
	}
	receipt, err := protocol.NewReceipt(
		"run-legacy", sequence, protocol.ProgramIdentity{ID: "product-delivery", Version: "1.0.0", Fingerprint: admission.ExpectedProgramFingerprint},
		admission, transition, target, mutation.StateFacets, effects, nil, nil, now, now.Add(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	receipt.SchemaVersion = protocol.PreviousReceiptSchemaVersion
	receipt.ID = ""
	receipt.ID = legacyContentID(t, "trc-", receipt)
	if err := receipt.ValidateCommittedHistory(); err != nil {
		t.Fatalf("legacy receipt fixture: %v", err)
	}
	return journalRecord{
		SchemaVersion: protocol.JournalSchemaVersion, Admission: admission, TransitionID: transitionID, TransitionClass: class,
		AllowedStateFacets: mutation.StateFacets, Status: "committed", Mutations: []ports.ResourceMutation{mutation}, ReceiptID: receipt.ID, Receipt: &receipt,
		CreatedAt: now, UpdatedAt: now.Add(time.Second),
	}
}

func writeLegacyJournal(t *testing.T, root string, record journalRecord, suffix string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, record.Admission.ID+suffix)
	raw, err := encodeJSON(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPreviousCommittedHistoryRemainsReadableAndResumable(t *testing.T) {
	// This fixture uses the exact admission-8/receipt-12 wire shape written by
	// the immediately preceding release, including a publication commit with no
	// effect_outputs field.
	root := t.TempDir()
	invocation := model.InvocationContext{
		RepositoryID: "repo", GitCommonID: "common", WorktreeID: "worktree", Ref: "refs/heads/feature/legacy",
		ControllerID: "controller", InvokingPath: "/repo", RuntimeVersion: "1.0.0", RuntimePath: "/runtime", RuntimeFingerprint: "runtime",
		Topology: model.TopologyEmbedded, Host: "cursor", Correlation: "legacy-run",
	}
	objective := model.Objective{ID: "objective-legacy", TargetID: model.ObjectiveOpenPR, TrustedClass: model.ObjectiveOpenPR, DeliveryID: "legacy"}
	binding := legacyCommittedRecord(t, "objective.bind", catalog.EventOwnedLocal, 1, 41, objective, invocation)
	published := legacyCommittedRecord(t, "publication.execute", catalog.EventOwnedExternal, 2, 56, objective, invocation)
	writeLegacyJournal(t, root, binding, ".committed")
	publishedPath := writeLegacyJournal(t, root, published, ".committed")
	legacyRaw, err := os.ReadFile(publishedPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(legacyRaw, []byte(`"effect_outputs"`)) || bytes.Contains(legacyRaw, []byte(`"invocation_fingerprint"`)) {
		t.Fatalf("legacy wire fixture contains fields absent from the base encoding: %s", legacyRaw)
	}

	if _, err := readJournal(publishedPath); err != nil {
		t.Fatalf("read base-version publication history: %v", err)
	}
	layout := ports.ControllerLayout{JournalRoot: root}
	active, found, err := FindLatestCommittedFlowForObjective(layout, invocation, objective, 57)
	if err != nil || !found || active.FlowID != "run-legacy" {
		t.Fatalf("active legacy run = %#v found=%t err=%v", active, found, err)
	}
	store := ReceiptStore{currentInvocation: map[string]receiptBinding{"run-legacy": {admission: published.Admission, layout: layout}}}
	sequence, err := store.NextSequence(context.Background(), "run-legacy")
	if err != nil || sequence != 3 {
		t.Fatalf("next legacy sequence = %d err=%v", sequence, err)
	}
	locator, receiptID, found, err := FindLatestCommittedTransitionOutput(layout, "run-legacy", invocation, "publication.execute", "publication_id", 57)
	if err != nil || !found || locator != "feature/legacy" || receiptID != published.ReceiptID {
		t.Fatalf("legacy publication locator=%q receipt=%q found=%t err=%v", locator, receiptID, found, err)
	}
	runner := &boundaryRunner{output: []byte(`{"state":"OPEN","url":"https://example.invalid/pull/222","number":222,"mergedAt":null,"baseRefName":"main","headRefName":"feature/legacy","headRefOid":"legacy-head","isCrossRepository":false}`)}
	boundary, err := NewNativeBoundaryWithRunner(runner)
	if err != nil {
		t.Fatal(err)
	}
	observe, _ := testprogram.StandardRegistry().Lookup("publication.observe")
	observationAdmission := protocol.Admission{
		Invocation: invocation, SourceRevision: published.Admission.SourceRevision,
		Parameters:           protocol.Parameters{{Name: "publication_id", Value: locator}},
		RequiredCapabilities: catalog.RequiredCapabilities(observe),
	}
	observationAdmission.EffectiveCapabilities = observationAdmission.RequiredCapabilities
	state := durable.State{Publication: model.PublicationPublishedNotLanded}
	if err := boundary.PrepareObservation(context.Background(), observationAdmission, observe, writeBoundaryConfig(t, "go test ./..."), &state); err != nil {
		t.Fatal(err)
	}
	if state.Publication != model.PublicationOpen || state.PublicationID != "222" || runner.arguments[len(runner.arguments)-1] != locator {
		t.Fatalf("legacy publication observation state=%s id=%q selector=%q", state.Publication, state.PublicationID, runner.arguments[len(runner.arguments)-1])
	}
}

func TestPreviousHistoryCompatibilityIsCommittedOnlyAndExact(t *testing.T) {
	invocation := model.InvocationContext{
		RepositoryID: "repo", GitCommonID: "common", WorktreeID: "worktree", Ref: "refs/heads/feature/legacy",
		ControllerID: "controller", InvokingPath: "/repo", RuntimeVersion: "1.0.0", RuntimePath: "/runtime", RuntimeFingerprint: "runtime",
		Topology: model.TopologyEmbedded, Host: "cursor", Correlation: "legacy-run",
	}
	objective := model.Objective{ID: "objective-legacy", TargetID: model.ObjectiveOpenPR, TrustedClass: model.ObjectiveOpenPR, DeliveryID: "legacy"}
	record := legacyCommittedRecord(t, "publication.execute", catalog.EventOwnedExternal, 2, 56, objective, invocation)

	t.Run("pending", func(t *testing.T) {
		path := writeLegacyJournal(t, t.TempDir(), record, ".pending")
		if _, err := readJournal(path); err == nil {
			t.Fatal("legacy pending journal was accepted")
		}
	})
	t.Run("mixed schema pair", func(t *testing.T) {
		mixed := record
		receipt := *mixed.Receipt
		receipt.SchemaVersion = protocol.ReceiptSchemaVersion
		receipt.ID = ""
		receipt.ID = legacyContentID(t, "trc-", receipt)
		mixed.Receipt, mixed.ReceiptID = &receipt, receipt.ID
		path := writeLegacyJournal(t, t.TempDir(), mixed, ".committed")
		if _, err := readJournal(path); err == nil {
			t.Fatal("mixed legacy/current schema pair was accepted")
		}
	})
	t.Run("tampered content", func(t *testing.T) {
		tampered := record
		tampered.Admission.SourceRevision = "substituted"
		path := writeLegacyJournal(t, t.TempDir(), tampered, ".committed")
		if _, err := readJournal(path); err == nil {
			t.Fatal("tampered legacy content identity was accepted")
		}
	})
}

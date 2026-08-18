package effects

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	workpackage "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery/workpackage"
	"github.com/operatorstack/boatstack/boatstack/internal/hostprojection"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func prepareAttachBinding(layout ports.ControllerLayout, admission protocol.Admission, now time.Time) (ports.ResourceMutation, error) {
	topologyValue, _ := admission.Parameters.Get("topology")
	configAuthority, _ := admission.Parameters.Get("config_authority")
	topology := model.Topology(topologyValue)
	if topology != model.TopologyDetached && topology != model.TopologyHybrid {
		return ports.ResourceMutation{}, fmt.Errorf("repository.attach topology must be detached or hybrid")
	}
	controllerID := sha256Bytes([]byte("controller:" + admission.Invocation.RepositoryID + ":" + admission.Invocation.GitCommonID + ":" + topologyValue + ":" + configAuthority))[:20]
	binding := durable.Binding{
		SchemaVersion: durable.BindingSchemaVersion, RepositoryID: admission.Invocation.RepositoryID, GitCommonID: admission.Invocation.GitCommonID,
		Topology: topology, ControllerID: controllerID, ConfigAuthority: configAuthority, CreatedAt: admission.IssuedAt.UTC(),
	}
	raw, err := durable.EncodeBinding(binding)
	if err != nil {
		return ports.ResourceMutation{}, err
	}
	return mutationFor(layout.BindingPath, raw, 0o600, true, false)
}

type approvalArtifact struct {
	SchemaVersion      int       `json:"schema_version"`
	DeliveryID         string    `json:"delivery_id"`
	PlanFingerprint    string    `json:"plan_fingerprint"`
	PackageFingerprint string    `json:"package_fingerprint,omitempty"`
	Actor              string    `json:"actor"`
	AdmissionID        string    `json:"admission_id"`
	ApprovedAt         time.Time `json:"approved_at"`
}

type planPromotionReceipt struct {
	SchemaVersion                  int       `json:"schema_version"`
	DeliveryID                     string    `json:"delivery_id"`
	PlanFingerprint                string    `json:"plan_fingerprint"`
	WorkPackageFingerprint         string    `json:"work_package_fingerprint"`
	WorkPackageApprovalFingerprint string    `json:"work_package_approval_fingerprint"`
	PlanOutputID                   string    `json:"plan_output_id"`
	AdmissionID                    string    `json:"admission_id"`
	PromotedAt                     time.Time `json:"promoted_at"`
	Fingerprint                    string    `json:"fingerprint"`
}

func sealPlanPromotionReceipt(value planPromotionReceipt) (planPromotionReceipt, []byte, error) {
	value.SchemaVersion = 2
	value.Fingerprint = ""
	identity, err := encodeJSON(value)
	if err != nil {
		return planPromotionReceipt{}, nil, err
	}
	value.Fingerprint = sha256Bytes(identity)
	raw, err := encodeJSON(value)
	return value, raw, err
}

type gateArtifact struct {
	SchemaVersion int                  `json:"schema_version"`
	DeliveryID    string               `json:"delivery_id"`
	TransitionID  catalog.TransitionID `json:"transition_id"`
	Revision      string               `json:"revision"`
	Fingerprint   string               `json:"fingerprint"`
	AdmissionID   string               `json:"admission_id"`
	RecordedAt    time.Time            `json:"recorded_at"`
}

type gateEvidenceInput struct {
	SchemaVersion  int       `json:"schema_version"`
	Gate           string    `json:"gate"`
	SourceRevision string    `json:"source_revision"`
	Outcome        string    `json:"outcome"`
	Producer       string    `json:"producer"`
	CompletedAt    time.Time `json:"completed_at"`
}

func decodeStrictArtifact(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("artifact contains trailing JSON")
	}
	return nil
}

type publicationPreview struct {
	SchemaVersion       int       `json:"schema_version"`
	DeliveryID          string    `json:"delivery_id"`
	BaseRef             string    `json:"base_ref"`
	HeadRef             string    `json:"head_ref"`
	SourceRevision      string    `json:"source_revision"`
	WorktreeFingerprint string    `json:"worktree_fingerprint"`
	BodyPath            string    `json:"body_path"`
	BodySHA256          string    `json:"body_sha256"`
	Fingerprint         string    `json:"fingerprint"`
	CreatedAt           time.Time `json:"created_at"`
}

func prepareArtifacts(layout ports.ControllerLayout, admission protocol.Admission, transition catalog.Transition, state *durable.State, humanIdentityRole ...string) ([]ports.ResourceMutation, error) {
	var mutations []ports.ResourceMutation
	var selectedProjections []hostprojection.ID
	var deliveryID string
	if transitionUsesDeliveryArtifacts(transition.ID) {
		var err error
		deliveryID, err = safeSegment(admission.Objective.DeliveryID, "delivery identity")
		if err != nil {
			return nil, err
		}
	}
	artifactRoot := filepath.Join(layout.RepositoryRoot, ".boatstack")
	switch transition.ID {
	case "configuration.initialize", "configuration.mutate", "installation.initialize":
		source, _ := admission.Parameters.Get("config_path")
		expected, _ := admission.Parameters.Get("config_sha256")
		raw, readErr := os.ReadFile(source)
		if readErr != nil {
			return nil, fmt.Errorf("read configuration source: %w", readErr)
		}
		config, actual, decodeErr := protocol.ProjectConfigFingerprint(raw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if actual != expected {
			return nil, fmt.Errorf("configuration fingerprint mismatch: got %s", actual)
		}
		if transition.ID == "configuration.mutate" {
			// Role preservation is checked against the same hash-bound bytes used
			// to construct the mutation below. No second source read can race this
			// decision and installation.
			if err := verifyCandidateConfigurationPreservesAdmittedRole(*state, config); err != nil {
				return nil, err
			}
		}
		mutation, mutationErr := mutationFor(layout.ConfigPath, raw, 0o644, false, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, mutation)
		state.ConfigFingerprint = expected
		policy := config.ControlPolicy()
		state.PlanApprovalPolicy = policy.PlanApproval
		state.VisualEvidencePolicy = policy.VisualEvidence
		state.ExternalEffectPolicy = policy.ExternalEffectAuthority
		state.IndependentReview = policy.IndependentReviewForHighRisk
		state.EnabledHosts = append([]string(nil), policy.Hosts...)
		selectedProjections, decodeErr = config.ProjectionIDs()
		if decodeErr != nil {
			return nil, decodeErr
		}
	case "plan.create", "plan.amend":
		source, _ := admission.Parameters.Get("source_path")
		expected, _ := admission.Parameters.Get("source_fingerprint")
		raw, readErr := os.ReadFile(source)
		if readErr != nil {
			return nil, fmt.Errorf("read source plan: %w", readErr)
		}
		if len(raw) == 0 {
			return nil, fmt.Errorf("source plan is empty")
		}
		fingerprint := sha256Bytes(raw)
		if expected != "" && fingerprint != expected {
			return nil, fmt.Errorf("source plan fingerprint changed after entry binding")
		}
		path := filepath.Join(artifactRoot, "plans", deliveryID+".source")
		mutation, mutationErr := mutationFor(path, raw, 0o644, false, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, mutation)
		approvalMutation, mutationErr := mutationFor(filepath.Join(artifactRoot, "approvals", deliveryID+".json"), nil, 0o644, false, true)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, approvalMutation)
		state.PlanFingerprint = fingerprint
		state.WorkPackage, state.WorkPackageFingerprint, state.WorkPackageApprovalFingerprint = model.WorkPackageAbsent, "", ""
		state.ApprovalFingerprint = ""
	case "work.package.admit":
		if admission.Work == nil || len(admission.Work.Outputs) == 0 {
			return nil, fmt.Errorf("work package admission requires exact foreground work evidence")
		}
		manifest, files, buildErr := buildWorkPackage(admission, transition, deliveryID)
		if buildErr != nil {
			return nil, buildErr
		}
		packageRoot := filepath.Join(artifactRoot, "work-packages", deliveryID, manifest.Fingerprint)
		rootExists := false
		if info, statErr := os.Lstat(packageRoot); statErr == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("immutable work package root is unsafe")
			}
			rootExists = true
			verified := workpackage.Verify(layout.RepositoryRoot, deliveryID, manifest.Fingerprint, nil)
			if verified.Integrity != workpackage.Valid || verified.Contract != workpackage.Valid || verified.Approval == workpackage.Invalid {
				return nil, fmt.Errorf("existing immutable work package conflicts: %s", strings.Join(verified.Diagnostics, "; "))
			}
		} else if !os.IsNotExist(statErr) {
			return nil, statErr
		}
		for relative, raw := range files {
			mutation, mutationErr := immutableWorkPackageMutation(filepath.Join(packageRoot, filepath.FromSlash(relative)), raw)
			if mutationErr != nil {
				return nil, mutationErr
			}
			if relative == "manifest.json" {
				mutation.InstallLast = true
			}
			if !rootExists {
				mutation.AtomicTreeRoot = packageRoot
			}
			mutations = append(mutations, mutation)
		}
		state.WorkPackageFingerprint, state.WorkPackageApprovalFingerprint = manifest.Fingerprint, ""
	case "work.package.approve":
		expected, _ := admission.Parameters.Get("package_fingerprint")
		manifest, loadErr := loadWorkPackageManifest(artifactRoot, deliveryID, expected)
		if loadErr != nil {
			return nil, loadErr
		}
		if expected == "" || expected != manifest.Fingerprint {
			return nil, fmt.Errorf("work package approval fingerprint is stale")
		}
		if state.WorkPackageFingerprint != manifest.Fingerprint {
			return nil, fmt.Errorf("work package state fingerprint is stale")
		}
		approval := workpackage.Approval{DeliveryID: deliveryID, PackageFingerprint: manifest.Fingerprint, ManifestFingerprint: manifest.Fingerprint, AdmissionID: admission.ID, ApprovedAt: admission.IssuedAt.UTC()}
		for _, receipt := range admission.Authority.Receipts {
			approval.AuthoritySources = append(approval.AuthoritySources, workpackage.AuthoritySource{ID: receipt.ID, Class: string(receipt.Class), Subject: receipt.Subject, Fingerprint: receipt.Fingerprint})
		}
		expectedRole := ""
		if len(humanIdentityRole) > 0 {
			expectedRole = humanIdentityRole[0]
		}
		for _, authorityClass := range []catalog.AuthorityClass{catalog.AuthorityHuman, catalog.AuthorityAutonomy} {
			for _, receipt := range admission.Authority.Receipts {
				if receipt.Subject == "" || receipt.IdentityRole == "" || receipt.IdentityProviderFingerprint == "" || receipt.Class != authorityClass {
					continue
				}
				if expectedRole != "" && receipt.IdentityRole != expectedRole {
					return nil, fmt.Errorf("work package approval identity role %q does not match admitted program role %q", receipt.IdentityRole, expectedRole)
				}
				if approval.Actor != "" && (receipt.Subject != approval.Actor || receipt.IdentityRole != approval.IdentityRole || receipt.IdentityProviderFingerprint != approval.IdentityProviderFingerprint) {
					return nil, fmt.Errorf("work package approval identity provenance is ambiguous")
				}
				approval.Actor, approval.IdentityRole, approval.IdentityProviderFingerprint = receipt.Subject, receipt.IdentityRole, receipt.IdentityProviderFingerprint
			}
			if approval.Actor != "" {
				break
			}
		}
		if approval.IdentityRole == "" || approval.IdentityProviderFingerprint == "" {
			return nil, fmt.Errorf("work package approval requires admitted identity provenance matching actor %q", approval.Actor)
		}
		approval, raw, encodeErr := workpackage.SealApproval(approval)
		if encodeErr != nil {
			return nil, encodeErr
		}
		approvalPath := filepath.Join(artifactRoot, "work-packages", deliveryID, manifest.Fingerprint, "approval.json")
		mutation, mutationErr := immutableWorkPackageMutation(approvalPath, raw)
		if mutationErr != nil {
			return nil, mutationErr
		}
		if !mutation.PriorExists {
			mutations = append(mutations, mutation)
		}
		state.WorkPackageFingerprint, state.WorkPackageApprovalFingerprint = manifest.Fingerprint, approval.Fingerprint
	case "planning.package.promote":
		planOutputID, _ := admission.Parameters.Get("plan_output")
		if planOutputID == "" {
			return nil, fmt.Errorf("planning promotion requires a compiler-bound plan output")
		}
		packagePath := filepath.Join(artifactRoot, "work-packages", deliveryID, state.WorkPackageFingerprint)
		packageRoot, openErr := os.OpenRoot(packagePath)
		if openErr != nil {
			return nil, fmt.Errorf("open immutable work package: %w", openErr)
		}
		defer packageRoot.Close()
		verified := verifyWorkPackage(layout.RepositoryRoot, deliveryID, state.WorkPackageFingerprint, nil)
		if verified.Integrity != workpackage.Valid || verified.Contract != workpackage.Valid || verified.Approval != workpackage.Valid {
			return nil, fmt.Errorf("work package verification failed: %s", strings.Join(verified.Diagnostics, "; "))
		}
		pinnedInfo, pinnedErr := packageRoot.Stat(".")
		currentInfo, currentErr := os.Stat(packagePath)
		if pinnedErr != nil || currentErr != nil || !os.SameFile(pinnedInfo, currentInfo) {
			return nil, fmt.Errorf("immutable work package changed during verification")
		}
		snapshotRepository, cleanupSnapshot, snapshotErr := capturePinnedWorkPackage(packageRoot, deliveryID, state.WorkPackageFingerprint)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		defer cleanupSnapshot()
		currentInfo, currentErr = os.Stat(packagePath)
		if currentErr != nil || !os.SameFile(pinnedInfo, currentInfo) {
			return nil, fmt.Errorf("immutable work package changed during verification")
		}
		verified = verifyWorkPackage(snapshotRepository, deliveryID, state.WorkPackageFingerprint, nil)
		if verified.Integrity != workpackage.Valid || verified.Contract != workpackage.Valid || verified.Approval != workpackage.Valid {
			return nil, fmt.Errorf("work package verification failed: %s", strings.Join(verified.Diagnostics, "; "))
		}
		snapshotRoot := filepath.Join(snapshotRepository, ".boatstack", "work-packages", deliveryID, state.WorkPackageFingerprint)
		manifestRaw, loadErr := os.ReadFile(filepath.Join(snapshotRoot, "manifest.json"))
		if loadErr != nil {
			return nil, loadErr
		}
		var manifest workpackage.Manifest
		if decodeErr := workpackage.StrictDecode(manifestRaw, &manifest); decodeErr != nil {
			return nil, fmt.Errorf("decode pinned work package manifest: %w", decodeErr)
		}
		canonicalManifestRaw, manifestEncodeErr := workpackage.Encode(manifest)
		manifestIdentity := manifest
		manifestIdentity.Fingerprint = ""
		manifestIdentityRaw, manifestIdentityEncodeErr := workpackage.Encode(manifestIdentity)
		if manifestEncodeErr != nil || manifestIdentityEncodeErr != nil || !bytes.Equal(manifestRaw, canonicalManifestRaw) || manifest.Fingerprint != state.WorkPackageFingerprint || sha256Bytes(manifestIdentityRaw) != manifest.Fingerprint {
			return nil, fmt.Errorf("pinned work package manifest does not bind durable state")
		}
		approvalRaw, readErr := os.ReadFile(filepath.Join(snapshotRoot, "approval.json"))
		if readErr != nil {
			return nil, fmt.Errorf("read work package approval: %w", readErr)
		}
		var approval workpackage.Approval
		if decodeErr := workpackage.StrictDecode(approvalRaw, &approval); decodeErr != nil {
			return nil, fmt.Errorf("decode work package approval: %w", decodeErr)
		}
		if approvalErr := workpackage.ValidateApproval(approvalRaw, approval, manifest, deliveryID, state.WorkPackageFingerprint); approvalErr != nil || approval.Fingerprint != state.WorkPackageApprovalFingerprint {
			return nil, fmt.Errorf("work package approval does not bind the exact package")
		}
		var selected workpackage.Output
		for _, output := range manifest.Outputs {
			if output.ID == planOutputID {
				selected = output
				break
			}
		}
		if selected.ID == "" || !selected.Required {
			return nil, fmt.Errorf("compiler-bound plan output %q is absent or optional", planOutputID)
		}
		planRaw, readErr := os.ReadFile(filepath.Join(snapshotRoot, filepath.FromSlash(selected.Path)))
		if readErr != nil || sha256Bytes(planRaw) != selected.SHA256 {
			return nil, fmt.Errorf("work package plan changed after approval")
		}
		promotion, promotionRaw, promotionErr := sealPlanPromotionReceipt(planPromotionReceipt{
			DeliveryID: deliveryID, PlanFingerprint: selected.SHA256,
			WorkPackageFingerprint: manifest.Fingerprint, WorkPackageApprovalFingerprint: approval.Fingerprint,
			PlanOutputID: selected.ID, AdmissionID: admission.ID, PromotedAt: admission.IssuedAt.UTC(),
		})
		if promotionErr != nil {
			return nil, promotionErr
		}
		planMutation, mutationErr := mutationFor(filepath.Join(artifactRoot, "plans", deliveryID+".source"), planRaw, 0o644, false, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		approvalMutation, mutationErr := mutationFor(filepath.Join(artifactRoot, "approvals", deliveryID+".json"), promotionRaw, 0o644, false, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, planMutation, approvalMutation)
		state.PlanFingerprint, state.ApprovalFingerprint = selected.SHA256, sha256Bytes(promotionRaw)
		_ = promotion
	case "plan.validate":
		path := filepath.Join(artifactRoot, "plans", deliveryID+".source")
		raw, readErr := os.ReadFile(path)
		if readErr != nil || len(raw) == 0 {
			return nil, fmt.Errorf("validate source plan %s: %w", path, readErr)
		}
		state.PlanFingerprint = sha256Bytes(raw)
		state.WorkPackage, state.WorkPackageFingerprint, state.WorkPackageApprovalFingerprint = model.WorkPackageAbsent, "", ""
	case "plan.approve", "plan.approve-amendment":
		fingerprint, _ := admission.Parameters.Get("plan_fingerprint")
		actor, _ := admission.Parameters.Get("actor")
		if state.PlanFingerprint == "" || fingerprint != state.PlanFingerprint {
			return nil, fmt.Errorf("approval plan fingerprint is stale")
		}
		artifact := approvalArtifact{SchemaVersion: 1, DeliveryID: deliveryID, PlanFingerprint: fingerprint, Actor: actor, AdmissionID: admission.ID, ApprovedAt: admission.IssuedAt.UTC()}
		raw, encodeErr := encodeJSON(artifact)
		if encodeErr != nil {
			return nil, encodeErr
		}
		mutation, mutationErr := mutationFor(filepath.Join(artifactRoot, "approvals", deliveryID+".json"), raw, 0o644, false, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, mutation)
		state.ApprovalFingerprint = sha256Bytes(raw)
	case "evidence.approval.revoke":
		mutation, mutationErr := mutationFor(filepath.Join(artifactRoot, "approvals", deliveryID+".json"), nil, 0o644, false, true)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, mutation)
		state.ApprovalFingerprint = ""
	case "gate.build.record", "gate.test.record", "gate.review.record", "gate.change.record", "gate.journey.record":
		revision, _ := admission.Parameters.Get("source_revision")
		evidencePath, _ := admission.Parameters.Get("evidence_path")
		fingerprint, _ := admission.Parameters.Get("evidence_fingerprint")
		evidenceRaw, readErr := os.ReadFile(evidencePath)
		if readErr != nil {
			return nil, fmt.Errorf("read gate evidence: %w", readErr)
		}
		if actual := sha256Bytes(evidenceRaw); actual != fingerprint {
			return nil, fmt.Errorf("gate evidence fingerprint mismatch: got %s", actual)
		}
		gate, _ := standardGateName(transition.ID)
		var input gateEvidenceInput
		if decodeErr := decodeStrictArtifact(evidenceRaw, &input); decodeErr != nil || input.SchemaVersion != 1 ||
			input.Gate != gate || input.SourceRevision != revision || input.Outcome != "passed" || input.Producer == "" || input.CompletedAt.IsZero() {
			return nil, fmt.Errorf("gate evidence must be a strict passed schema-1 %s receipt for revision %s", gate, revision)
		}
		artifact := gateArtifact{SchemaVersion: 1, DeliveryID: deliveryID, TransitionID: transition.ID, Revision: revision, Fingerprint: fingerprint, AdmissionID: admission.ID, RecordedAt: admission.IssuedAt.UTC()}
		raw, encodeErr := encodeJSON(artifact)
		if encodeErr != nil {
			return nil, encodeErr
		}
		payloadMutation, mutationErr := mutationFor(filepath.Join(layout.EvidenceRoot, deliveryID, gate+".evidence.json"), evidenceRaw, 0o644, false, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, payloadMutation)
		mutation, mutationErr := mutationFor(filepath.Join(layout.EvidenceRoot, deliveryID, gate+".json"), raw, 0o644, false, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, mutation)
	case "evidence.visual.attach":
		manifest, _ := admission.Parameters.Get("manifest_path")
		privacyReceipt, _ := admission.Parameters.Get("privacy_receipt")
		raw, readErr := os.ReadFile(manifest)
		if readErr != nil {
			return nil, readErr
		}
		if sha256Bytes(raw) != privacyReceipt {
			return nil, fmt.Errorf("visual evidence privacy receipt does not bind manifest bytes")
		}
		mutation, mutationErr := mutationFor(filepath.Join(layout.EvidenceRoot, deliveryID, "visual-manifest.json"), raw, 0o644, false, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, mutation)
	case "publication.preview":
		baseRef, _ := admission.Parameters.Get("base_ref")
		headRef, _ := admission.Parameters.Get("head_ref")
		bodyPath, _ := admission.Parameters.Get("body_path")
		if err := protocol.ValidateGitReference(baseRef); err != nil {
			return nil, err
		}
		if err := protocol.ValidateGitBranch(headRef); err != nil {
			return nil, err
		}
		if admission.Invocation.Ref != "refs/heads/"+headRef {
			return nil, fmt.Errorf("publication head does not match the exact invoking branch")
		}
		configRaw, readConfigErr := os.ReadFile(layout.ConfigPath)
		if readConfigErr != nil {
			return nil, readConfigErr
		}
		config, decodeConfigErr := protocol.DecodeProjectConfig(configRaw)
		if decodeConfigErr != nil {
			return nil, decodeConfigErr
		}
		if baseRef != config.Project.DefaultBranch {
			return nil, fmt.Errorf("publication base %q does not match configured default branch %q", baseRef, config.Project.DefaultBranch)
		}
		body, readErr := os.ReadFile(bodyPath)
		if readErr != nil {
			return nil, readErr
		}
		if admission.SourceRevision == "" || admission.WorktreeFingerprint == "" {
			return nil, fmt.Errorf("publication preview requires an exact committed source and worktree identity")
		}
		preview := publicationPreview{
			SchemaVersion: 2, DeliveryID: deliveryID, BaseRef: baseRef, HeadRef: headRef,
			SourceRevision: admission.SourceRevision, WorktreeFingerprint: admission.WorktreeFingerprint,
			BodyPath: bodyPath, BodySHA256: sha256Bytes(body), CreatedAt: admission.IssuedAt.UTC(),
		}
		identity := preview
		identity.Fingerprint, identity.CreatedAt = "", time.Time{}
		identityRaw, encodeErr := json.Marshal(identity)
		if encodeErr != nil {
			return nil, encodeErr
		}
		preview.Fingerprint = sha256Bytes(identityRaw)
		raw, encodeErr := encodeJSON(preview)
		if encodeErr != nil {
			return nil, encodeErr
		}
		mutation, mutationErr := mutationFor(filepath.Join(artifactRoot, "publication", deliveryID+".preview.json"), raw, 0o644, false, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, mutation)
		state.PreviewFingerprint = preview.Fingerprint
	case "publication.execute":
		preview, readErr := loadPublicationPreview(filepath.Join(artifactRoot, "publication", deliveryID+".preview.json"))
		if readErr != nil {
			return nil, readErr
		}
		if err := validatePublicationPreviewForAdmission(layout, admission, preview); err != nil {
			return nil, err
		}
		if preview.Fingerprint != state.PreviewFingerprint {
			return nil, fmt.Errorf("publication preview fingerprint is stale")
		}
	case "publication.correct":
		if err := validateCorrectionBody(admission); err != nil {
			return nil, err
		}
	}
	if transition.ID == "configuration.initialize" || transition.ID == "configuration.mutate" || transition.ID == "installation.initialize" || transition.ID == "installation.update" || transition.ID == "installation.reconcile-update" {
		if selectedProjections == nil {
			raw, readErr := os.ReadFile(layout.ConfigPath)
			if readErr != nil {
				return nil, fmt.Errorf("read current project configuration for host projections: %w", readErr)
			}
			config, decodeErr := protocol.DecodeProjectConfig(raw)
			if decodeErr != nil {
				return nil, decodeErr
			}
			selectedProjections, decodeErr = config.ProjectionIDs()
			if decodeErr != nil {
				return nil, decodeErr
			}
		}
		hostMutations, hostErr := prepareHostProjectionMutations(layout.RepositoryRoot, selectedProjections)
		if hostErr != nil {
			return nil, hostErr
		}
		mutations = append(mutations, hostMutations...)
	}
	return mutations, nil
}

var verifyWorkPackage = workpackage.Verify

func capturePinnedWorkPackage(root *os.Root, deliveryID, fingerprint string) (string, func(), error) {
	repository, err := os.MkdirTemp("", "boatstack-work-package-snapshot-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(repository) }
	destination := filepath.Join(repository, ".boatstack", "work-packages", deliveryID, fingerprint)
	if err := os.MkdirAll(destination, 0o700); err != nil {
		cleanup()
		return "", nil, err
	}
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("work package member is a symlink: %s", name)
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if entry.IsDir() {
			return os.Mkdir(target, 0o700)
		}
		before, err := root.Lstat(filepath.FromSlash(name))
		if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || runtime.GOOS != "windows" && before.Mode().Perm() != 0o644 {
			return fmt.Errorf("work package member is not canonical: %s", name)
		}
		raw, err := root.ReadFile(filepath.FromSlash(name))
		if err != nil {
			return err
		}
		after, err := root.Lstat(filepath.FromSlash(name))
		if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) || after.Size() != int64(len(raw)) || after.Mode().Perm() != before.Mode().Perm() {
			return fmt.Errorf("work package member changed while captured: %s", name)
		}
		return os.WriteFile(target, raw, 0o644)
	})
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return repository, cleanup, nil
}

// prepareWorkspacePlanTransfer carries runtime-owned plan artifacts into a
// newly cut worktree. A run binds the plan bytes before the cut, so the target
// worktree must observe those exact bytes rather than fall back to the inbox
// and accidentally select new intent.
func prepareWorkspacePlanTransfer(repositoryRoot, workspacePath, deliveryID, expectedPlanFingerprint, expectedApprovalFingerprint string) ([]ports.ResourceMutation, error) {
	if expectedPlanFingerprint == "" || deliveryID == "" {
		return nil, nil
	}
	if workspacePath == "" || expectedApprovalFingerprint == "" {
		return nil, fmt.Errorf("workspace plan transfer requires destination and exact approval for a bound plan")
	}
	deliveryID, err := safeSegment(deliveryID, "delivery identity")
	if err != nil {
		return nil, err
	}
	sourceRoot := filepath.Join(repositoryRoot, ".boatstack")
	destinationRoot := filepath.Join(workspacePath, ".boatstack")
	planPath := filepath.Join(sourceRoot, "plans", deliveryID+".source")
	planRaw, err := readRegularWorkspacePlanArtifact(planPath)
	if err != nil {
		return nil, err
	}
	if actual := sha256Bytes(planRaw); actual != expectedPlanFingerprint {
		return nil, fmt.Errorf("workspace plan artifact fingerprint changed: got %s", actual)
	}
	approvalPath := filepath.Join(sourceRoot, "approvals", deliveryID+".json")
	approvalRaw, err := readRegularWorkspacePlanArtifact(approvalPath)
	if err != nil {
		return nil, err
	}
	if actual := sha256Bytes(approvalRaw); actual != expectedApprovalFingerprint {
		return nil, fmt.Errorf("workspace approval artifact fingerprint changed: got %s", actual)
	}
	if err := validateWorkspaceApproval(repositoryRoot, deliveryID, expectedPlanFingerprint, approvalRaw); err != nil {
		return nil, fmt.Errorf("workspace approval artifact does not bind the admitted plan: %w", err)
	}
	planMutation, err := mutationFor(filepath.Join(destinationRoot, "plans", deliveryID+".source"), planRaw, 0o644, false, false)
	if err != nil {
		return nil, err
	}
	approvalMutation, err := mutationFor(filepath.Join(destinationRoot, "approvals", deliveryID+".json"), approvalRaw, 0o644, false, false)
	if err != nil {
		return nil, err
	}
	mutations := []ports.ResourceMutation{planMutation, approvalMutation}
	var promotion planPromotionReceipt
	if decodeStrictArtifact(approvalRaw, &promotion) == nil && promotion.SchemaVersion == 2 {
		packageMutations, packageErr := prepareWorkspaceWorkPackageTransfer(repositoryRoot, workspacePath, deliveryID, promotion.WorkPackageFingerprint)
		if packageErr != nil {
			return nil, packageErr
		}
		mutations = append(mutations, packageMutations...)
	}
	return mutations, nil
}

func prepareWorkspaceWorkPackageTransfer(repositoryRoot, workspacePath, deliveryID, packageFingerprint string) ([]ports.ResourceMutation, error) {
	sourcePath := filepath.Join(repositoryRoot, ".boatstack", "work-packages", deliveryID, packageFingerprint)
	root, err := os.OpenRoot(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("open immutable work package for workspace transfer: %w", err)
	}
	defer root.Close()
	pinnedInfo, err := root.Stat(".")
	if err != nil {
		return nil, err
	}
	snapshotRepository, cleanup, err := capturePinnedWorkPackage(root, deliveryID, packageFingerprint)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	currentInfo, err := os.Stat(sourcePath)
	if err != nil || !os.SameFile(pinnedInfo, currentInfo) {
		return nil, fmt.Errorf("immutable work package changed during workspace transfer")
	}
	verified := workpackage.Verify(snapshotRepository, deliveryID, packageFingerprint, nil)
	if verified.Integrity != workpackage.Valid || verified.Contract != workpackage.Valid || verified.Approval != workpackage.Valid {
		return nil, fmt.Errorf("workspace work package verification failed: %s", strings.Join(verified.Diagnostics, "; "))
	}
	snapshotRoot := filepath.Join(snapshotRepository, ".boatstack", "work-packages", deliveryID, packageFingerprint)
	destinationRoot := filepath.Join(workspacePath, ".boatstack", "work-packages", deliveryID, packageFingerprint)
	destinationExists := false
	if info, statErr := os.Lstat(destinationRoot); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("workspace work package destination is unsafe")
		}
		destinationExists = true
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	var mutations []ports.ResourceMutation
	err = filepath.WalkDir(snapshotRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(snapshotRoot, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mutation, err := immutableWorkPackageMutation(filepath.Join(destinationRoot, relative), raw)
		if err != nil {
			return err
		}
		if filepath.ToSlash(relative) == "manifest.json" {
			mutation.InstallLast = true
		}
		if !destinationExists {
			mutation.AtomicTreeRoot = destinationRoot
		}
		mutations = append(mutations, mutation)
		return nil
	})
	return mutations, err
}

func validateWorkspaceApproval(repositoryRoot, deliveryID, expectedPlanFingerprint string, raw []byte) error {
	var envelope struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	switch envelope.SchemaVersion {
	case 1:
		var approval approvalArtifact
		if err := decodeStrictArtifact(raw, &approval); err != nil || approval.DeliveryID != deliveryID || approval.PlanFingerprint != expectedPlanFingerprint || approval.Actor == "" || approval.AdmissionID == "" || approval.ApprovedAt.IsZero() {
			return fmt.Errorf("schema-1 approval identity is invalid")
		}
		return nil
	case 2:
		var promotion planPromotionReceipt
		if err := decodeStrictArtifact(raw, &promotion); err != nil || promotion.DeliveryID != deliveryID || promotion.PlanFingerprint != expectedPlanFingerprint ||
			!workpackage.ValidFingerprint(promotion.WorkPackageFingerprint) || !workpackage.ValidFingerprint(promotion.WorkPackageApprovalFingerprint) ||
			promotion.PlanOutputID == "" || promotion.AdmissionID == "" || promotion.PromotedAt.IsZero() {
			return fmt.Errorf("schema-2 plan promotion identity is invalid")
		}
		identity := promotion
		identity.Fingerprint = ""
		identityRaw, err := encodeJSON(identity)
		if err != nil || promotion.Fingerprint != sha256Bytes(identityRaw) {
			return fmt.Errorf("schema-2 plan promotion fingerprint is invalid")
		}
		verified := workpackage.Verify(repositoryRoot, deliveryID, promotion.WorkPackageFingerprint, nil)
		if verified.Integrity != workpackage.Valid || verified.Contract != workpackage.Valid || verified.Approval != workpackage.Valid {
			return fmt.Errorf("schema-2 promotion package is invalid")
		}
		packageRoot := filepath.Join(repositoryRoot, ".boatstack", "work-packages", deliveryID, promotion.WorkPackageFingerprint)
		approvalRaw, err := os.ReadFile(filepath.Join(packageRoot, "approval.json"))
		var approval workpackage.Approval
		if err != nil || workpackage.StrictDecode(approvalRaw, &approval) != nil || approval.Fingerprint != promotion.WorkPackageApprovalFingerprint {
			return fmt.Errorf("schema-2 promotion approval lineage is invalid")
		}
		manifestRaw, err := os.ReadFile(filepath.Join(packageRoot, "manifest.json"))
		var manifest workpackage.Manifest
		if err != nil || workpackage.StrictDecode(manifestRaw, &manifest) != nil {
			return fmt.Errorf("schema-2 promotion manifest is invalid")
		}
		for _, output := range manifest.Outputs {
			if output.ID == promotion.PlanOutputID && output.Required && output.SHA256 == expectedPlanFingerprint {
				return nil
			}
		}
		return fmt.Errorf("schema-2 promotion output is not bound")
	default:
		return fmt.Errorf("unsupported approval schema %d", envelope.SchemaVersion)
	}
}

func readRegularWorkspacePlanArtifact(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect workspace plan artifact %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("workspace plan artifact is not a regular file: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workspace plan artifact %s: %w", path, err)
	}
	return raw, nil
}

func transitionUsesDeliveryArtifacts(id catalog.TransitionID) bool {
	switch id {
	case "plan.create", "plan.amend", "plan.validate", "plan.approve", "plan.approve-amendment",
		"work.package.admit", "work.package.approve", "planning.package.promote",
		"evidence.approval.revoke", "gate.build.record", "gate.test.record", "gate.review.record",
		"gate.change.record", "gate.journey.record", "evidence.visual.attach", "publication.preview",
		"publication.execute", "publication.correct":
		return true
	default:
		return false
	}
}

func buildWorkPackage(admission protocol.Admission, transition catalog.Transition, deliveryID string) (workpackage.Manifest, map[string][]byte, error) {
	if admission.Work == nil || transition.Work == nil {
		return workpackage.Manifest{}, nil, fmt.Errorf("work package requires work evidence and contract")
	}
	work := workpackage.WorkContract{ID: transition.Work.ID, Fingerprint: transition.Work.Fingerprint, Instructions: workpackage.Asset{Path: transition.Work.InstructionPath, SHA256: transition.Work.InstructionSHA256, Content: transition.Work.InstructionContent}}
	for _, input := range transition.Work.Inputs {
		work.Inputs = append(work.Inputs, workpackage.WorkInput{ID: input.ID, EntryInput: input.EntryInput})
	}
	declarations := map[string]catalog.WorkOutput{}
	for _, output := range transition.Work.Outputs {
		declarations[output.ID] = output
		item := workpackage.WorkOutput{ID: output.ID, Path: output.Path, MediaType: output.MediaType, Required: output.Required, MaxBytes: output.MaxBytes}
		if output.GuidancePath != "" {
			item.Guidance = &workpackage.Asset{Path: output.GuidancePath, SHA256: output.GuidanceSHA256, Content: output.GuidanceContent}
		}
		if output.SchemaPath != "" {
			item.Schema = &workpackage.Asset{Path: output.SchemaPath, SHA256: output.SchemaSHA256, Content: output.SchemaContent}
		}
		work.Outputs = append(work.Outputs, item)
	}
	if err := workpackage.ValidateOutputPaths(work.Outputs); err != nil {
		return workpackage.Manifest{}, nil, err
	}
	contract, contractRaw, err := workpackage.SealContract(workpackage.Contract{Work: work})
	if err != nil {
		return workpackage.Manifest{}, nil, err
	}
	receipt := workpackage.WorkReceipt{RequestID: admission.Work.RequestID, RequestFingerprint: admission.Work.RequestFingerprint, ResultFingerprint: admission.Work.ResultFingerprint, ContractID: admission.Work.ContractID, ContractFingerprint: admission.Work.ContractFingerprint, TransitionID: string(admission.Work.TransitionID), ProgramFingerprint: admission.Work.ProgramFingerprint, ContextFingerprint: admission.Work.ContextFingerprint, StateRevision: admission.Work.StateRevision, RepositoryID: admission.Work.RepositoryID, WorktreeID: admission.Work.WorktreeID}
	files := map[string][]byte{"contract.json": contractRaw}
	for _, evidence := range admission.Work.Outputs {
		declaration, ok := declarations[evidence.ID]
		if !ok {
			return workpackage.Manifest{}, nil, fmt.Errorf("work package output %q is undeclared", evidence.ID)
		}
		output := workpackage.Output{ID: evidence.ID, Path: evidence.Path, MediaType: evidence.MediaType, Required: declaration.Required, Size: evidence.Size, SHA256: evidence.SHA256, GuidanceSHA256: declaration.GuidanceSHA256, SchemaSHA256: declaration.SchemaSHA256}
		receipt.Outputs = append(receipt.Outputs, output)
		files[evidence.Path] = []byte(evidence.Content)
	}
	receipt, receiptRaw, err := workpackage.SealWorkReceipt(receipt)
	if err != nil {
		return workpackage.Manifest{}, nil, err
	}
	files["work-receipt.json"] = receiptRaw
	manifest := workpackage.Manifest{DeliveryID: deliveryID, ProgramID: admission.Work.ProgramID, ProgramFingerprint: admission.Work.ProgramFingerprint, EntryID: admission.Work.EntryID, RunID: admission.Work.RunID, TransitionID: string(transition.ID), WorkContractID: transition.Work.ID, WorkContractFingerprint: transition.Work.Fingerprint, WorkRequestFingerprint: admission.Work.RequestFingerprint, WorkResultFingerprint: admission.Work.ResultFingerprint, ContextFingerprint: admission.Work.ContextFingerprint, StateRevision: admission.Work.StateRevision, Contract: workpackage.Reference{Path: "contract.json", SHA256: workpackage.Digest(contractRaw)}, WorkReceipt: workpackage.Reference{Path: "work-receipt.json", SHA256: workpackage.Digest(receiptRaw)}, Outputs: receipt.Outputs}
	if manifest.ProgramID == "" || manifest.EntryID == "" || manifest.RunID == "" {
		return workpackage.Manifest{}, nil, fmt.Errorf("work package work evidence lacks Flow identity")
	}
	manifest, manifestRaw, err := workpackage.SealManifest(manifest)
	if err != nil {
		return workpackage.Manifest{}, nil, err
	}
	files["manifest.json"] = manifestRaw
	_ = contract
	_ = receipt
	return manifest, files, nil
}

func immutableWorkPackageMutation(path string, target []byte) (ports.ResourceMutation, error) {
	mutation, err := mutationFor(path, target, 0o644, false, false)
	if err != nil {
		return ports.ResourceMutation{}, err
	}
	if mutation.PriorExists && !bytes.Equal(mutation.Prior, target) {
		return ports.ResourceMutation{}, fmt.Errorf("immutable work package member conflicts: %s", path)
	}
	return mutation, nil
}

func loadWorkPackageManifest(artifactRoot, deliveryID, packageFingerprint string) (workpackage.Manifest, error) {
	result := workpackage.Verify(filepath.Dir(artifactRoot), deliveryID, packageFingerprint, nil)
	if result.Integrity != workpackage.Valid || result.Contract != workpackage.Valid {
		return workpackage.Manifest{}, fmt.Errorf("work package verification failed: %s", strings.Join(result.Diagnostics, "; "))
	}
	raw, err := os.ReadFile(filepath.Join(artifactRoot, "work-packages", deliveryID, packageFingerprint, "manifest.json"))
	if err != nil {
		return workpackage.Manifest{}, err
	}
	var manifest workpackage.Manifest
	if err := workpackage.StrictDecode(raw, &manifest); err != nil {
		return workpackage.Manifest{}, err
	}
	return manifest, nil
}

func authorityActor(admission protocol.Admission) string {
	for _, receipt := range admission.Authority.Receipts {
		if receipt.Subject != "" {
			return receipt.Subject
		}
	}
	return "authorized-actor"
}

func loadPublicationPreview(path string) (publicationPreview, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return publicationPreview{}, err
	}
	var preview publicationPreview
	if err := decodeStrictArtifact(raw, &preview); err != nil {
		return publicationPreview{}, err
	}
	if preview.SchemaVersion != 2 || preview.DeliveryID == "" || preview.BaseRef == "" || preview.HeadRef == "" || preview.SourceRevision == "" || preview.WorktreeFingerprint == "" || preview.BodyPath == "" || preview.BodySHA256 == "" || preview.Fingerprint == "" || preview.CreatedAt.IsZero() {
		return publicationPreview{}, fmt.Errorf("invalid publication preview")
	}
	if err := protocol.ValidateGitReference(preview.BaseRef); err != nil {
		return publicationPreview{}, err
	}
	if err := protocol.ValidateGitBranch(preview.HeadRef); err != nil {
		return publicationPreview{}, err
	}
	if !filepath.IsAbs(preview.BodyPath) {
		return publicationPreview{}, fmt.Errorf("publication body path must be absolute")
	}
	body, err := os.ReadFile(preview.BodyPath)
	if err != nil {
		return publicationPreview{}, err
	}
	if sha256Bytes(body) != preview.BodySHA256 {
		return publicationPreview{}, fmt.Errorf("publication body changed after preview")
	}
	identity := preview
	identity.Fingerprint, identity.CreatedAt = "", time.Time{}
	identityRaw, err := json.Marshal(identity)
	if err != nil {
		return publicationPreview{}, err
	}
	if sha256Bytes(identityRaw) != preview.Fingerprint {
		return publicationPreview{}, fmt.Errorf("publication preview failed content identity verification")
	}
	return preview, nil
}

func validatePublicationPreviewForAdmission(layout ports.ControllerLayout, admission protocol.Admission, preview publicationPreview) error {
	deliveryID, err := safeSegment(admission.Objective.DeliveryID, "delivery identity")
	if err != nil {
		return err
	}
	expected, _ := admission.Parameters.Get("preview_fingerprint")
	if expected == "" || preview.Fingerprint != expected || preview.DeliveryID != deliveryID {
		return fmt.Errorf("publication preview does not match the exact admitted delivery")
	}
	if admission.Invocation.Ref != "refs/heads/"+preview.HeadRef {
		return fmt.Errorf("publication preview head does not match the exact invoking branch")
	}
	if admission.SourceRevision != preview.SourceRevision || admission.WorktreeFingerprint != preview.WorktreeFingerprint {
		return fmt.Errorf("publication preview does not match the exact committed HEAD and worktree")
	}
	configRaw, err := os.ReadFile(layout.ConfigPath)
	if err != nil {
		return err
	}
	config, err := protocol.DecodeProjectConfig(configRaw)
	if err != nil {
		return err
	}
	if preview.BaseRef != config.Project.DefaultBranch {
		return fmt.Errorf("publication preview base does not match current configuration authority")
	}
	return nil
}

func validateCorrectionBody(admission protocol.Admission) error {
	bodyPath, _ := admission.Parameters.Get("body_path")
	expected, _ := admission.Parameters.Get("body_sha256")
	raw, err := os.ReadFile(bodyPath)
	if err != nil {
		return err
	}
	if expected == "" || sha256Bytes(raw) != expected {
		return fmt.Errorf("publication correction body does not match its admitted fingerprint")
	}
	return nil
}

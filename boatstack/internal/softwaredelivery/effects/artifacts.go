package effects

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	planningpackage "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery/planningpackage"
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
		state.PlanningPackageFingerprint = ""
		state.ApprovalFingerprint = ""
	case "planning.package.admit":
		if admission.Work == nil || len(admission.Work.Outputs) == 0 {
			return nil, fmt.Errorf("planning package admission requires exact foreground work evidence")
		}
		planOutput, _ := admission.Parameters.Get("plan_output")
		manifest, files, buildErr := buildPlanningPackage(admission, transition, deliveryID, planOutput)
		if buildErr != nil {
			return nil, buildErr
		}
		packageRoot := filepath.Join(artifactRoot, "planning-packages", deliveryID, manifest.Fingerprint)
		rootExists := false
		if info, statErr := os.Lstat(packageRoot); statErr == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("immutable planning package root is unsafe")
			}
			rootExists = true
			verified := planningpackage.Verify(layout.RepositoryRoot, deliveryID, manifest.Fingerprint, nil)
			if verified.Integrity != planningpackage.Valid || verified.Contract != planningpackage.Valid || verified.Approval == planningpackage.Invalid {
				return nil, fmt.Errorf("existing immutable planning package conflicts: %s", strings.Join(verified.Diagnostics, "; "))
			}
		} else if !os.IsNotExist(statErr) {
			return nil, statErr
		}
		for relative, raw := range files {
			mutation, mutationErr := immutablePlanningMutation(filepath.Join(packageRoot, filepath.FromSlash(relative)), raw)
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
		state.PlanFingerprint, state.PlanningPackageFingerprint, state.ApprovalFingerprint = manifest.PlanOutput.SHA256, manifest.Fingerprint, ""
	case "planning.package.approve":
		expected, _ := admission.Parameters.Get("package_fingerprint")
		manifest, loadErr := loadPlanningPackageManifestV2(artifactRoot, deliveryID, expected)
		if loadErr != nil {
			return nil, loadErr
		}
		if expected == "" || expected != manifest.Fingerprint {
			return nil, fmt.Errorf("planning package approval fingerprint is stale")
		}
		if state.PlanningPackageFingerprint != manifest.Fingerprint {
			return nil, fmt.Errorf("planning package state fingerprint is stale")
		}
		approval := planningpackage.Approval{DeliveryID: deliveryID, PackageFingerprint: manifest.Fingerprint, ManifestFingerprint: manifest.Fingerprint, PlanOutputID: manifest.PlanOutput.ID, PlanFingerprint: manifest.PlanOutput.SHA256, AdmissionID: admission.ID, Actor: authorityActor(admission), ApprovedAt: admission.IssuedAt.UTC()}
		for _, receipt := range admission.Authority.Receipts {
			approval.AuthoritySources = append(approval.AuthoritySources, planningpackage.AuthoritySource{ID: receipt.ID, Class: string(receipt.Class), Subject: receipt.Subject, Fingerprint: receipt.Fingerprint})
		}
		if len(admission.Authority.Receipts) > 0 {
			approval.IdentityRole = string(admission.Authority.Receipts[0].Class)
			if len(humanIdentityRole) > 0 && humanIdentityRole[0] != "" {
				approval.IdentityRole = humanIdentityRole[0]
			}
			approval.IdentityProviderFingerprint = admission.Authority.Receipts[0].Fingerprint
		}
		approval, raw, encodeErr := planningpackage.SealApproval(approval)
		if encodeErr != nil {
			return nil, encodeErr
		}
		approvalPath := filepath.Join(artifactRoot, "planning-packages", deliveryID, manifest.Fingerprint, "approval.json")
		mutation, mutationErr := immutablePlanningMutation(approvalPath, raw)
		if mutationErr != nil {
			return nil, mutationErr
		}
		if !mutation.PriorExists {
			mutations = append(mutations, mutation)
		}
		state.PlanFingerprint, state.PlanningPackageFingerprint, state.ApprovalFingerprint = manifest.PlanOutput.SHA256, manifest.Fingerprint, approval.Fingerprint
	case "planning.package.promote":
		manifest, loadErr := loadPlanningPackageManifestV2(artifactRoot, deliveryID, state.PlanningPackageFingerprint)
		if loadErr != nil {
			return nil, loadErr
		}
		packageRoot := filepath.Join(artifactRoot, "planning-packages", deliveryID, manifest.Fingerprint)
		approvalPath := filepath.Join(packageRoot, "approval.json")
		approvalRaw, readErr := os.ReadFile(approvalPath)
		if readErr != nil {
			return nil, fmt.Errorf("read planning package approval: %w", readErr)
		}
		var approval planningpackage.Approval
		if decodeErr := planningpackage.StrictDecode(approvalRaw, &approval); decodeErr != nil || approval.SchemaVersion != planningpackage.ApprovalSchemaVersion || approval.DeliveryID != deliveryID || approval.PackageFingerprint != manifest.Fingerprint || approval.PlanFingerprint != manifest.PlanOutput.SHA256 || approval.Actor == "" || approval.AdmissionID == "" || approval.ApprovedAt.IsZero() {
			return nil, fmt.Errorf("planning package approval does not bind the exact package")
		}
		planArtifact, pathErr := planningPackageOutputPath(packageRoot, manifest.PlanOutput.Path)
		if pathErr != nil {
			return nil, pathErr
		}
		planRaw, readErr := readRegularWorkspacePlanArtifact(planArtifact)
		if readErr != nil || sha256Bytes(planRaw) != manifest.PlanOutput.SHA256 {
			return nil, fmt.Errorf("planning package plan changed after approval")
		}
		planMutation, mutationErr := mutationFor(filepath.Join(artifactRoot, "plans", deliveryID+".source"), planRaw, 0o644, false, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		approvalMutation, mutationErr := mutationFor(filepath.Join(artifactRoot, "approvals", deliveryID+".json"), approvalRaw, 0o644, false, false)
		if mutationErr != nil {
			return nil, mutationErr
		}
		mutations = append(mutations, planMutation, approvalMutation)
		state.PlanFingerprint, state.PlanningPackageFingerprint, state.ApprovalFingerprint = manifest.PlanOutput.SHA256, manifest.Fingerprint, sha256Bytes(approvalRaw)
	case "plan.validate":
		path := filepath.Join(artifactRoot, "plans", deliveryID+".source")
		raw, readErr := os.ReadFile(path)
		if readErr != nil || len(raw) == 0 {
			return nil, fmt.Errorf("validate source plan %s: %w", path, readErr)
		}
		state.PlanFingerprint = sha256Bytes(raw)
		state.PlanningPackageFingerprint = ""
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
	return []ports.ResourceMutation{planMutation, approvalMutation}, nil
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
	case planningpackage.ApprovalSchemaVersion:
		var approval planningpackage.Approval
		if err := planningpackage.StrictDecode(raw, &approval); err != nil || approval.DeliveryID != deliveryID || approval.PlanFingerprint != expectedPlanFingerprint || !planningpackage.ValidFingerprint(approval.PackageFingerprint) {
			return fmt.Errorf("schema-2 approval identity is invalid")
		}
		verified := planningpackage.Verify(repositoryRoot, deliveryID, approval.PackageFingerprint, nil)
		if verified.Integrity != planningpackage.Valid || verified.Contract != planningpackage.Valid || verified.Approval != planningpackage.Valid {
			return fmt.Errorf("schema-2 approval package is invalid")
		}
		snapshotRaw, err := os.ReadFile(filepath.Join(repositoryRoot, ".boatstack", "planning-packages", deliveryID, approval.PackageFingerprint, "approval.json"))
		if err != nil || !bytes.Equal(snapshotRaw, raw) {
			return fmt.Errorf("schema-2 approval projection differs from immutable snapshot")
		}
		return nil
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
		"planning.package.admit", "planning.package.approve", "planning.package.promote",
		"evidence.approval.revoke", "gate.build.record", "gate.test.record", "gate.review.record",
		"gate.change.record", "gate.journey.record", "evidence.visual.attach", "publication.preview",
		"publication.execute", "publication.correct":
		return true
	default:
		return false
	}
}

func planningPackageOutputPath(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("planning package output path is unsafe")
	}
	return filepath.Join(root, relative), nil
}

func buildPlanningPackage(admission protocol.Admission, transition catalog.Transition, deliveryID, planOutputID string) (planningpackage.Manifest, map[string][]byte, error) {
	if admission.Work == nil || transition.Work == nil || planOutputID == "" {
		return planningpackage.Manifest{}, nil, fmt.Errorf("planning package requires work evidence, contract, and explicit plan output")
	}
	work := planningpackage.WorkContract{ID: transition.Work.ID, Fingerprint: transition.Work.Fingerprint, Instructions: planningpackage.Asset{Path: transition.Work.InstructionPath, SHA256: transition.Work.InstructionSHA256, Content: transition.Work.InstructionContent}}
	for _, input := range transition.Work.Inputs {
		work.Inputs = append(work.Inputs, planningpackage.WorkInput{ID: input.ID, EntryInput: input.EntryInput})
	}
	declarations := map[string]catalog.WorkOutput{}
	for _, output := range transition.Work.Outputs {
		declarations[output.ID] = output
		item := planningpackage.WorkOutput{ID: output.ID, Path: output.Path, MediaType: output.MediaType, Required: output.Required, MaxBytes: output.MaxBytes}
		if output.GuidancePath != "" {
			item.Guidance = &planningpackage.Asset{Path: output.GuidancePath, SHA256: output.GuidanceSHA256, Content: output.GuidanceContent}
		}
		if output.SchemaPath != "" {
			item.Schema = &planningpackage.Asset{Path: output.SchemaPath, SHA256: output.SchemaSHA256, Content: output.SchemaContent}
		}
		work.Outputs = append(work.Outputs, item)
	}
	if err := planningpackage.ValidateOutputPaths(work.Outputs); err != nil {
		return planningpackage.Manifest{}, nil, err
	}
	contract, contractRaw, err := planningpackage.SealContract(planningpackage.Contract{Work: work, PlanOutput: planOutputID})
	if err != nil {
		return planningpackage.Manifest{}, nil, err
	}
	receipt := planningpackage.WorkReceipt{RequestID: admission.Work.RequestID, RequestFingerprint: admission.Work.RequestFingerprint, ResultFingerprint: admission.Work.ResultFingerprint, ContractID: admission.Work.ContractID, ContractFingerprint: admission.Work.ContractFingerprint, TransitionID: string(admission.Work.TransitionID), ProgramFingerprint: admission.Work.ProgramFingerprint, ContextFingerprint: admission.Work.ContextFingerprint, StateRevision: admission.Work.StateRevision, RepositoryID: admission.Work.RepositoryID, WorktreeID: admission.Work.WorktreeID}
	files := map[string][]byte{"contract.json": contractRaw}
	var plan planningpackage.PlanOutput
	for _, evidence := range admission.Work.Outputs {
		declaration, ok := declarations[evidence.ID]
		if !ok {
			return planningpackage.Manifest{}, nil, fmt.Errorf("planning package output %q is undeclared", evidence.ID)
		}
		output := planningpackage.Output{ID: evidence.ID, Path: evidence.Path, MediaType: evidence.MediaType, Required: declaration.Required, Size: evidence.Size, SHA256: evidence.SHA256, GuidanceSHA256: declaration.GuidanceSHA256, SchemaSHA256: declaration.SchemaSHA256}
		receipt.Outputs = append(receipt.Outputs, output)
		files[evidence.Path] = []byte(evidence.Content)
		if evidence.ID == planOutputID {
			plan = planningpackage.PlanOutput{ID: evidence.ID, Path: evidence.Path, MediaType: evidence.MediaType, SHA256: evidence.SHA256}
		}
	}
	if plan.ID == "" || !declarations[planOutputID].Required {
		return planningpackage.Manifest{}, nil, fmt.Errorf("designated plan output %q is missing or optional", planOutputID)
	}
	receipt, receiptRaw, err := planningpackage.SealWorkReceipt(receipt)
	if err != nil {
		return planningpackage.Manifest{}, nil, err
	}
	files["work-receipt.json"] = receiptRaw
	manifest := planningpackage.Manifest{DeliveryID: deliveryID, ProgramID: admission.Work.ProgramID, ProgramFingerprint: admission.Work.ProgramFingerprint, EntryID: admission.Work.EntryID, RunID: admission.Work.RunID, TransitionID: string(transition.ID), WorkContractID: transition.Work.ID, WorkContractFingerprint: transition.Work.Fingerprint, WorkRequestFingerprint: admission.Work.RequestFingerprint, WorkResultFingerprint: admission.Work.ResultFingerprint, ContextFingerprint: admission.Work.ContextFingerprint, StateRevision: admission.Work.StateRevision, PlanOutput: plan, Contract: planningpackage.Reference{Path: "contract.json", SHA256: planningpackage.Digest(contractRaw)}, WorkReceipt: planningpackage.Reference{Path: "work-receipt.json", SHA256: planningpackage.Digest(receiptRaw)}, Outputs: receipt.Outputs}
	if manifest.ProgramID == "" || manifest.EntryID == "" || manifest.RunID == "" {
		return planningpackage.Manifest{}, nil, fmt.Errorf("planning package work evidence lacks Flow identity")
	}
	manifest, manifestRaw, err := planningpackage.SealManifest(manifest)
	if err != nil {
		return planningpackage.Manifest{}, nil, err
	}
	files["manifest.json"] = manifestRaw
	_ = contract
	_ = receipt
	return manifest, files, nil
}

func immutablePlanningMutation(path string, target []byte) (ports.ResourceMutation, error) {
	mutation, err := mutationFor(path, target, 0o644, false, false)
	if err != nil {
		return ports.ResourceMutation{}, err
	}
	if mutation.PriorExists && !bytes.Equal(mutation.Prior, target) {
		return ports.ResourceMutation{}, fmt.Errorf("immutable planning package member conflicts: %s", path)
	}
	return mutation, nil
}

func loadPlanningPackageManifestV2(artifactRoot, deliveryID, packageFingerprint string) (planningpackage.Manifest, error) {
	result := planningpackage.Verify(filepath.Dir(artifactRoot), deliveryID, packageFingerprint, nil)
	if result.Integrity != planningpackage.Valid || result.Contract != planningpackage.Valid {
		return planningpackage.Manifest{}, fmt.Errorf("planning package verification failed: %s", strings.Join(result.Diagnostics, "; "))
	}
	raw, err := os.ReadFile(filepath.Join(artifactRoot, "planning-packages", deliveryID, packageFingerprint, "manifest.json"))
	if err != nil {
		return planningpackage.Manifest{}, err
	}
	var manifest planningpackage.Manifest
	if err := planningpackage.StrictDecode(raw, &manifest); err != nil {
		return planningpackage.Manifest{}, err
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

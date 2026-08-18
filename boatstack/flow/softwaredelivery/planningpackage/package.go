// Package planningpackage owns the portable, content-addressed planning-package
// format. It is deliberately independent of live controller state and authority.
package planningpackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	general "github.com/operatorstack/boatstack/boatstack/kernel"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	ManifestSchemaVersion    = 2
	ContractSchemaVersion    = 1
	WorkReceiptSchemaVersion = 1
	ApprovalSchemaVersion    = 2
	maxPackageMetadataBytes  = 16 << 20
)

var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var fingerprint = regexp.MustCompile(`^[a-f0-9]{64}$`)
var identityRole = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

var Reserved = []string{"approval.json", "contract.json", "manifest.json", "work-receipt.json"}

type Asset struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Content string `json:"content"`
}

type WorkOutput struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
	Required  bool   `json:"required"`
	MaxBytes  int64  `json:"max_bytes"`
	Guidance  *Asset `json:"guidance,omitempty"`
	Schema    *Asset `json:"schema,omitempty"`
}

type WorkInput struct {
	ID         string `json:"id"`
	EntryInput string `json:"entry_input"`
}

type WorkContract struct {
	ID           string       `json:"id"`
	Fingerprint  string       `json:"fingerprint"`
	Instructions Asset        `json:"instructions"`
	Inputs       []WorkInput  `json:"inputs,omitempty"`
	Outputs      []WorkOutput `json:"outputs"`
}

type Contract struct {
	SchemaVersion int          `json:"schema_version"`
	Work          WorkContract `json:"work"`
	PlanOutput    string       `json:"plan_output"`
	Fingerprint   string       `json:"fingerprint"`
}

type Output struct {
	ID             string `json:"id"`
	Path           string `json:"path"`
	MediaType      string `json:"media_type"`
	Required       bool   `json:"required"`
	Size           int64  `json:"size"`
	SHA256         string `json:"sha256"`
	GuidanceSHA256 string `json:"guidance_sha256,omitempty"`
	SchemaSHA256   string `json:"schema_sha256,omitempty"`
}

type Reference struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type PlanOutput struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
	SHA256    string `json:"sha256"`
}

type Manifest struct {
	SchemaVersion           int        `json:"schema_version"`
	DeliveryID              string     `json:"delivery_id"`
	ProgramID               string     `json:"program_id"`
	ProgramFingerprint      string     `json:"program_fingerprint"`
	EntryID                 string     `json:"entry_id"`
	RunID                   string     `json:"run_id"`
	TransitionID            string     `json:"transition_id"`
	WorkContractID          string     `json:"work_contract_id"`
	WorkContractFingerprint string     `json:"work_contract_fingerprint"`
	WorkRequestFingerprint  string     `json:"work_request_fingerprint"`
	WorkResultFingerprint   string     `json:"work_result_fingerprint"`
	ContextFingerprint      string     `json:"context_fingerprint"`
	StateRevision           uint64     `json:"state_revision"`
	PlanOutput              PlanOutput `json:"plan_output"`
	Contract                Reference  `json:"contract"`
	WorkReceipt             Reference  `json:"work_receipt"`
	Outputs                 []Output   `json:"outputs"`
	Fingerprint             string     `json:"fingerprint"`
}

type WorkReceipt struct {
	SchemaVersion       int      `json:"schema_version"`
	RequestID           string   `json:"request_id"`
	RequestFingerprint  string   `json:"request_fingerprint"`
	ResultFingerprint   string   `json:"result_fingerprint"`
	ContractID          string   `json:"contract_id"`
	ContractFingerprint string   `json:"contract_fingerprint"`
	TransitionID        string   `json:"transition_id"`
	ProgramFingerprint  string   `json:"program_fingerprint"`
	ContextFingerprint  string   `json:"context_fingerprint"`
	StateRevision       uint64   `json:"state_revision"`
	RepositoryID        string   `json:"repository_id"`
	WorktreeID          string   `json:"worktree_id"`
	Outputs             []Output `json:"outputs"`
	Fingerprint         string   `json:"fingerprint"`
}

type AuthoritySource struct {
	ID          string `json:"id"`
	Class       string `json:"class"`
	Subject     string `json:"subject"`
	Fingerprint string `json:"fingerprint"`
}

type Approval struct {
	SchemaVersion               int               `json:"schema_version"`
	DeliveryID                  string            `json:"delivery_id"`
	PackageFingerprint          string            `json:"package_fingerprint"`
	ManifestFingerprint         string            `json:"manifest_fingerprint"`
	PlanOutputID                string            `json:"plan_output_id"`
	PlanFingerprint             string            `json:"plan_fingerprint"`
	AdmissionID                 string            `json:"admission_id"`
	AuthoritySources            []AuthoritySource `json:"authority_sources"`
	Actor                       string            `json:"actor"`
	IdentityRole                string            `json:"identity_role"`
	IdentityProviderFingerprint string            `json:"identity_provider_fingerprint,omitempty"`
	ApprovedAt                  time.Time         `json:"approved_at"`
	Fingerprint                 string            `json:"fingerprint"`
}

type Status string

const (
	Valid       Status = "valid"
	Invalid     Status = "invalid"
	Missing     Status = "missing"
	Match       Status = "match"
	Different   Status = "different"
	Unavailable Status = "unavailable"
)

type Result struct {
	DeliveryID          string   `json:"delivery_id"`
	PackageFingerprint  string   `json:"package_fingerprint"`
	Integrity           Status   `json:"integrity"`
	Contract            Status   `json:"contract"`
	Approval            Status   `json:"approval"`
	CurrentProgram      Status   `json:"current_program"`
	SemanticCorrectness string   `json:"semantic_correctness"`
	OriginAuthenticity  string   `json:"origin_authenticity"`
	Diagnostics         []string `json:"diagnostics,omitempty"`
}

type CurrentProgram struct{ ProgramFingerprint, WorkContractFingerprint, PlanOutput string }

func Encode(value any) ([]byte, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func Digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func StrictDecode(raw []byte, value any) error {
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

func fingerprintValue(value any, clear func()) (string, error) {
	clear()
	raw, err := Encode(value)
	if err != nil {
		return "", err
	}
	return Digest(raw), nil
}

func SealContract(value Contract) (Contract, []byte, error) {
	value.SchemaVersion = ContractSchemaVersion
	value.Fingerprint = ""
	fp, err := fingerprintValue(value, func() { value.Fingerprint = "" })
	if err != nil {
		return Contract{}, nil, err
	}
	value.Fingerprint = fp
	raw, err := Encode(value)
	if err == nil && len(raw) > maxPackageMetadataBytes {
		return Contract{}, nil, fmt.Errorf("planning package contract exceeds %d bytes", maxPackageMetadataBytes)
	}
	return value, raw, err
}

func ValidateContractMetadata(work WorkContract, planOutput string) error {
	_, _, err := SealContract(Contract{Work: work, PlanOutput: planOutput})
	return err
}

func SealWorkReceipt(value WorkReceipt) (WorkReceipt, []byte, error) {
	value.SchemaVersion = WorkReceiptSchemaVersion
	sort.Slice(value.Outputs, func(i, j int) bool { return value.Outputs[i].ID < value.Outputs[j].ID })
	value.Fingerprint = ""
	raw, err := Encode(value)
	if err != nil {
		return WorkReceipt{}, nil, err
	}
	value.Fingerprint = Digest(raw)
	raw, err = Encode(value)
	return value, raw, err
}

func SealManifest(value Manifest) (Manifest, []byte, error) {
	value.SchemaVersion = ManifestSchemaVersion
	sort.Slice(value.Outputs, func(i, j int) bool { return value.Outputs[i].ID < value.Outputs[j].ID })
	value.Fingerprint = ""
	raw, err := Encode(value)
	if err != nil {
		return Manifest{}, nil, err
	}
	value.Fingerprint = Digest(raw)
	raw, err = Encode(value)
	return value, raw, err
}

func SealApproval(value Approval) (Approval, []byte, error) {
	value.SchemaVersion = ApprovalSchemaVersion
	sort.Slice(value.AuthoritySources, func(i, j int) bool { return value.AuthoritySources[i].ID < value.AuthoritySources[j].ID })
	value.Fingerprint = ""
	raw, err := Encode(value)
	if err != nil {
		return Approval{}, nil, err
	}
	value.Fingerprint = Digest(raw)
	raw, err = Encode(value)
	return value, raw, err
}

func ValidSegment(value string) bool {
	return segment.MatchString(value) && value != "." && value != ".."
}
func ValidFingerprint(value string) bool { return fingerprint.MatchString(value) }

func ValidateOutputPaths(outputs []WorkOutput) error {
	paths := map[string]string{}
	ids := map[string]bool{}
	for _, output := range outputs {
		if !ValidSegment(output.ID) || !safeRelative(output.Path) || output.MaxBytes < 1 {
			return fmt.Errorf("output %q has invalid identity, path, or bound", output.ID)
		}
		if ids[output.ID] {
			return fmt.Errorf("output %q is duplicated", output.ID)
		}
		ids[output.ID] = true
		folded := strings.ToLower(filepath.ToSlash(output.Path))
		for _, reserved := range Reserved {
			if folded == reserved || strings.HasPrefix(folded, reserved+"/") || strings.HasPrefix(reserved, folded+"/") {
				return fmt.Errorf("output %q conflicts with reserved path %q", output.ID, reserved)
			}
		}
		for prior, id := range paths {
			if folded == prior || strings.HasPrefix(folded, prior+"/") || strings.HasPrefix(prior, folded+"/") {
				return fmt.Errorf("outputs %q and %q have colliding paths", id, output.ID)
			}
		}
		paths[folded] = output.ID
	}
	return nil
}

func safeRelative(value string) bool {
	if value == "" || strings.Contains(value, `\`) || pathpkg.IsAbs(value) || hasWindowsVolumePrefix(value) {
		return false
	}
	clean := pathpkg.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != value {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if !portableWindowsComponent(component) {
			return false
		}
	}
	return true
}

func hasWindowsVolumePrefix(value string) bool {
	return len(value) >= 2 && value[1] == ':' && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z'))
}

func portableWindowsComponent(value string) bool {
	if value == "" || strings.HasSuffix(value, ".") || strings.HasSuffix(value, " ") || strings.ContainsAny(value, `<>:"|?*`) {
		return false
	}
	for _, character := range value {
		if character < 32 {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(value, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return false
	}
	return !(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9')
}

func Verify(repository, deliveryID, packageFingerprint string, current *CurrentProgram) Result {
	result := Result{DeliveryID: deliveryID, PackageFingerprint: packageFingerprint, Integrity: Invalid, Contract: Invalid, Approval: Missing, CurrentProgram: Unavailable, SemanticCorrectness: "not-evaluated", OriginAuthenticity: "not-proven"}
	fail := func(message string) Result { result.Diagnostics = append(result.Diagnostics, message); return result }
	if !ValidSegment(deliveryID) || !ValidFingerprint(packageFingerprint) {
		return fail("package path identity is invalid")
	}
	root, err := filepath.Abs(repository)
	if err != nil {
		return fail(err.Error())
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return fail("repository root is unavailable")
	}
	packageRoot := filepath.Join(root, ".boatstack", "planning-packages", deliveryID, packageFingerprint)
	if info, err = os.Lstat(packageRoot); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fail("package directory is unavailable or unsafe")
	}
	manifestRaw, err := readRegular(filepath.Join(packageRoot, "manifest.json"), maxPackageMetadataBytes)
	if err != nil {
		return fail(err.Error())
	}
	var manifest Manifest
	if err = StrictDecode(manifestRaw, &manifest); err != nil {
		return fail("manifest: " + err.Error())
	}
	if !canonicalEncoding(manifestRaw, manifest) || !outputsSorted(manifest.Outputs) {
		return fail("manifest encoding or output order is non-canonical")
	}
	manifestIdentity := manifest
	manifestIdentity.Fingerprint = ""
	identityRaw, _ := Encode(manifestIdentity)
	if identityErr := validateManifestIdentity(manifest, deliveryID, packageFingerprint, Digest(identityRaw)); identityErr != nil {
		return fail("manifest identity is invalid: " + identityErr.Error())
	}
	contractRaw, err := readRegular(filepath.Join(packageRoot, "contract.json"), maxPackageMetadataBytes)
	if err != nil || Digest(contractRaw) != manifest.Contract.SHA256 || manifest.Contract.Path != "contract.json" {
		return fail("contract reference is invalid")
	}
	var contract Contract
	if err = StrictDecode(contractRaw, &contract); err != nil {
		return fail("contract: " + err.Error())
	}
	if !canonicalEncoding(contractRaw, contract) {
		return fail("contract encoding is non-canonical")
	}
	contractIdentity := contract
	contractIdentity.Fingerprint = ""
	identityRaw, _ = Encode(contractIdentity)
	if contract.SchemaVersion != ContractSchemaVersion || contract.Fingerprint != Digest(identityRaw) || contract.Work.ID != manifest.WorkContractID || contract.Work.Fingerprint != manifest.WorkContractFingerprint || contract.PlanOutput != manifest.PlanOutput.ID {
		return fail("contract identity is invalid")
	}
	if err = validateContractAssets(contract.Work); err != nil {
		return fail(err.Error())
	}
	result.Contract = Valid
	receiptRaw, err := readRegular(filepath.Join(packageRoot, "work-receipt.json"), maxPackageMetadataBytes)
	if err != nil || Digest(receiptRaw) != manifest.WorkReceipt.SHA256 || manifest.WorkReceipt.Path != "work-receipt.json" {
		return fail("work receipt reference is invalid")
	}
	var receipt WorkReceipt
	if err = StrictDecode(receiptRaw, &receipt); err != nil {
		return fail("work receipt: " + err.Error())
	}
	if !canonicalEncoding(receiptRaw, receipt) || !outputsSorted(receipt.Outputs) {
		return fail("work receipt encoding or output order is non-canonical")
	}
	receiptIdentity := receipt
	receiptIdentity.Fingerprint = ""
	identityRaw, _ = Encode(receiptIdentity)
	if receipt.SchemaVersion != WorkReceiptSchemaVersion || receipt.Fingerprint != Digest(identityRaw) || receipt.RequestID == "" || receipt.ContractID != manifest.WorkContractID || receipt.TransitionID != manifest.TransitionID || receipt.RepositoryID == "" || receipt.WorktreeID == "" ||
		receipt.RequestFingerprint != manifest.WorkRequestFingerprint || receipt.ResultFingerprint != manifest.WorkResultFingerprint || receipt.ContractFingerprint != manifest.WorkContractFingerprint || receipt.ProgramFingerprint != manifest.ProgramFingerprint || receipt.ContextFingerprint != manifest.ContextFingerprint || receipt.StateRevision != manifest.StateRevision {
		return fail("work receipt identity is invalid")
	}
	if err = verifyOutputs(packageRoot, manifest, contract, receipt); err != nil {
		return fail(err.Error())
	}
	approvalPath := filepath.Join(packageRoot, "approval.json")
	if _, statErr := os.Lstat(approvalPath); statErr == nil {
		approvalRaw, readErr := readRegular(approvalPath, maxPackageMetadataBytes)
		if readErr != nil {
			result.Approval = Invalid
			return fail(readErr.Error())
		}
		var approval Approval
		if StrictDecode(approvalRaw, &approval) != nil || !canonicalEncoding(approvalRaw, approval) || !authoritySourcesSorted(approval.AuthoritySources) {
			result.Approval = Invalid
			return fail("approval is invalid")
		}
		if err := ValidateApproval(approvalRaw, approval, manifest, deliveryID, packageFingerprint); err != nil {
			result.Approval = Invalid
			return fail("approval identity is invalid: " + err.Error())
		}
		result.Approval = Valid
	} else if !os.IsNotExist(statErr) {
		result.Approval = Invalid
		return fail("approval path is unsafe")
	}
	if err = rejectExtraFiles(packageRoot, manifest, result.Approval == Valid); err != nil {
		return fail(err.Error())
	}
	result.Integrity = Valid
	if current != nil {
		if current.ProgramFingerprint == manifest.ProgramFingerprint && current.WorkContractFingerprint == manifest.WorkContractFingerprint && current.PlanOutput == manifest.PlanOutput.ID {
			result.CurrentProgram = Match
		} else {
			result.CurrentProgram = Different
		}
	}
	return result
}

func validateManifestIdentity(manifest Manifest, deliveryID, packageFingerprint, identityFingerprint string) error {
	switch {
	case manifest.SchemaVersion != ManifestSchemaVersion:
		return fmt.Errorf("schema version")
	case manifest.Fingerprint != identityFingerprint || manifest.Fingerprint != packageFingerprint:
		return fmt.Errorf("package fingerprint")
	case manifest.DeliveryID != deliveryID:
		return fmt.Errorf("delivery identity")
	case !ValidSegment(manifest.ProgramID) || !ValidSegment(manifest.EntryID) || !ValidSegment(manifest.RunID) || !ValidSegment(manifest.WorkContractID):
		return fmt.Errorf("semantic identity")
	case !ValidFingerprint(manifest.ProgramFingerprint) || !ValidFingerprint(manifest.WorkContractFingerprint) || !ValidFingerprint(manifest.WorkRequestFingerprint) || !ValidFingerprint(manifest.WorkResultFingerprint) || !ValidFingerprint(manifest.ContextFingerprint):
		return fmt.Errorf("lineage fingerprint")
	case manifest.TransitionID != "planning.package.admit":
		return fmt.Errorf("transition identity")
	case !ValidSegment(manifest.PlanOutput.ID) || !safeRelative(manifest.PlanOutput.Path) || manifest.PlanOutput.MediaType == "" || !ValidFingerprint(manifest.PlanOutput.SHA256):
		return fmt.Errorf("plan output identity")
	default:
		return nil
	}
}

func canonicalEncoding(raw []byte, value any) bool {
	encoded, err := Encode(value)
	return err == nil && bytes.Equal(raw, encoded)
}

func outputsSorted(outputs []Output) bool {
	for index := 1; index < len(outputs); index++ {
		if outputs[index-1].ID >= outputs[index].ID {
			return false
		}
	}
	return true
}

func authoritySourcesSorted(sources []AuthoritySource) bool {
	for index, source := range sources {
		if source.ID == "" || !validAuthorityClass(source.Class) || source.Subject == "" || source.Fingerprint == "" || index > 0 && sources[index-1].ID >= source.ID {
			return false
		}
	}
	return true
}

// ValidateApproval checks the complete portable approval admission contract
// against the exact manifest it authorizes.
func ValidateApproval(raw []byte, approval Approval, manifest Manifest, deliveryID, packageFingerprint string) error {
	if !canonicalEncoding(raw, approval) || !authoritySourcesSorted(approval.AuthoritySources) {
		return fmt.Errorf("encoding or authority sources")
	}
	identity := approval
	identity.Fingerprint = ""
	identityRaw, err := Encode(identity)
	if err != nil {
		return err
	}
	if approval.SchemaVersion != ApprovalSchemaVersion || approval.Fingerprint != Digest(identityRaw) || approval.DeliveryID != deliveryID || approval.PackageFingerprint != packageFingerprint || approval.ManifestFingerprint != manifest.Fingerprint || approval.PlanOutputID != manifest.PlanOutput.ID || approval.PlanFingerprint != manifest.PlanOutput.SHA256 || approval.Actor == "" || approval.AdmissionID == "" || approval.ApprovedAt.IsZero() {
		return fmt.Errorf("lineage")
	}
	if len(approval.IdentityRole) == 0 || len(approval.IdentityRole) > 128 || !identityRole.MatchString(approval.IdentityRole) || !ValidFingerprint(approval.IdentityProviderFingerprint) {
		return fmt.Errorf("human identity provenance")
	}
	matchingActor := false
	for _, source := range approval.AuthoritySources {
		if source.Subject == approval.Actor {
			matchingActor = true
		}
	}
	if !matchingActor {
		return fmt.Errorf("authority does not match actor")
	}
	return nil
}

func validAuthorityClass(value string) bool {
	switch value {
	case "repository-policy", "human", "autonomy", "external-provider":
		return true
	default:
		return false
	}
}

func readRegular(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("package member is not regular: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		return nil, fmt.Errorf("package member has non-canonical mode: %s", path)
	}
	if maxBytes < 0 || info.Size() > maxBytes {
		return nil, fmt.Errorf("package member exceeds its byte bound: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(info, after) || after.Size() != int64(len(raw)) || int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("package member changed or exceeded its byte bound: %s", path)
	}
	return raw, nil
}

func validateContractAssets(work WorkContract) error {
	if !ValidSegment(work.ID) || !ValidFingerprint(work.Fingerprint) || !safeRelative(work.Instructions.Path) || strings.TrimSpace(work.Instructions.Content) == "" || Digest([]byte(work.Instructions.Content)) != work.Instructions.SHA256 || !utf8.ValidString(work.Instructions.Content) || len(work.Outputs) == 0 {
		return fmt.Errorf("embedded work contract is invalid")
	}
	inputIDs := map[string]bool{}
	for _, input := range work.Inputs {
		if !ValidSegment(input.ID) || !ValidSegment(input.EntryInput) || inputIDs[input.ID] {
			return fmt.Errorf("embedded work input %q is invalid", input.ID)
		}
		inputIDs[input.ID] = true
	}
	if err := ValidateOutputPaths(work.Outputs); err != nil {
		return err
	}
	computed, err := RuntimeWorkFingerprint(work)
	if err != nil || computed != work.Fingerprint {
		return fmt.Errorf("embedded work contract fingerprint is invalid")
	}
	for _, o := range work.Outputs {
		if o.MaxBytes < 1 || o.MaxBytes > 16<<20 {
			return fmt.Errorf("output %q has an invalid byte limit", o.ID)
		}
		if o.MediaType != "text/markdown" && o.MediaType != "text/plain" && o.MediaType != "application/json" {
			return fmt.Errorf("output %q has unsupported media type", o.ID)
		}
		for _, a := range []*Asset{o.Guidance, o.Schema} {
			if a != nil && (!safeRelative(a.Path) || strings.TrimSpace(a.Content) == "" || !utf8.ValidString(a.Content) || Digest([]byte(a.Content)) != a.SHA256) {
				return fmt.Errorf("output %q has invalid embedded asset", o.ID)
			}
		}
		if o.Schema != nil {
			if o.MediaType != "application/json" {
				return fmt.Errorf("output %q has a schema for a non-JSON media type", o.ID)
			}
			var schema any
			if json.Unmarshal([]byte(o.Schema.Content), &schema) != nil {
				return fmt.Errorf("output %q has invalid embedded schema JSON", o.ID)
			}
			compiler := jsonschema.NewCompiler()
			compiler.DefaultDraft(jsonschema.Draft2020)
			if compiler.AddResource(o.Schema.Path, schema) != nil {
				return fmt.Errorf("output %q has invalid embedded schema", o.ID)
			}
			if _, err := compiler.Compile(o.Schema.Path); err != nil {
				return fmt.Errorf("output %q has invalid embedded schema: %w", o.ID, err)
			}
		}
	}
	return nil
}

func RuntimeWorkFingerprint(work WorkContract) (string, error) {
	type runtimeOutput struct {
		ID              string `json:"id"`
		Path            string `json:"path"`
		MediaType       string `json:"media_type"`
		Required        bool   `json:"required"`
		MaxBytes        int64  `json:"max_bytes"`
		GuidancePath    string `json:"guidance_path,omitempty"`
		GuidanceSHA256  string `json:"guidance_sha256,omitempty"`
		GuidanceContent string `json:"guidance_content,omitempty"`
		SchemaPath      string `json:"schema_path,omitempty"`
		SchemaSHA256    string `json:"schema_sha256,omitempty"`
		SchemaContent   string `json:"schema_content,omitempty"`
	}
	outputs := make([]runtimeOutput, 0, len(work.Outputs))
	for _, output := range work.Outputs {
		item := runtimeOutput{ID: output.ID, Path: output.Path, MediaType: output.MediaType, Required: output.Required, MaxBytes: output.MaxBytes}
		if output.Guidance != nil {
			item.GuidancePath, item.GuidanceSHA256, item.GuidanceContent = output.Guidance.Path, output.Guidance.SHA256, output.Guidance.Content
		}
		if output.Schema != nil {
			item.SchemaPath, item.SchemaSHA256, item.SchemaContent = output.Schema.Path, output.Schema.SHA256, output.Schema.Content
		}
		outputs = append(outputs, item)
	}
	return general.Fingerprint(struct {
		ID                 string          `json:"id"`
		InstructionPath    string          `json:"instruction_path"`
		InstructionSHA256  string          `json:"instruction_sha256"`
		InstructionContent string          `json:"instruction_content"`
		Inputs             []WorkInput     `json:"inputs,omitempty"`
		Outputs            []runtimeOutput `json:"outputs"`
	}{work.ID, work.Instructions.Path, work.Instructions.SHA256, work.Instructions.Content, work.Inputs, outputs})
}

func verifyOutputs(root string, manifest Manifest, contract Contract, receipt WorkReceipt) error {
	declared := map[string]WorkOutput{}
	for _, o := range contract.Work.Outputs {
		declared[o.ID] = o
	}
	receipts := map[string]Output{}
	for _, o := range receipt.Outputs {
		receipts[o.ID] = o
	}
	seen := map[string]bool{}
	for _, o := range manifest.Outputs {
		if seen[o.ID] {
			return fmt.Errorf("manifest output %q is duplicated", o.ID)
		}
		seen[o.ID] = true
		decl, ok := declared[o.ID]
		rec, rok := receipts[o.ID]
		if !ok || !rok || decl.Path != o.Path || decl.MediaType != o.MediaType || decl.Required != o.Required || o.Size < 0 || o.Size > decl.MaxBytes || rec != o {
			return fmt.Errorf("output %q does not match contract and receipt", o.ID)
		}
		if (decl.Guidance == nil) != (o.GuidanceSHA256 == "") || decl.Guidance != nil && decl.Guidance.SHA256 != o.GuidanceSHA256 || (decl.Schema == nil) != (o.SchemaSHA256 == "") || decl.Schema != nil && decl.Schema.SHA256 != o.SchemaSHA256 {
			return fmt.Errorf("output %q asset binding is invalid", o.ID)
		}
		raw, err := readRegular(filepath.Join(root, filepath.FromSlash(o.Path)), decl.MaxBytes)
		if err != nil || int64(len(raw)) != o.Size || Digest(raw) != o.SHA256 {
			return fmt.Errorf("output %q content is invalid", o.ID)
		}
		if strings.HasPrefix(o.MediaType, "text/") && !utf8.Valid(raw) {
			return fmt.Errorf("output %q is not UTF-8", o.ID)
		}
		if o.MediaType == "application/json" {
			var value any
			if json.Unmarshal(raw, &value) != nil {
				return fmt.Errorf("output %q is invalid JSON", o.ID)
			}
			if decl.Schema != nil {
				compiler := jsonschema.NewCompiler()
				compiler.DefaultDraft(jsonschema.Draft2020)
				var schema any
				if json.Unmarshal([]byte(decl.Schema.Content), &schema) != nil || compiler.AddResource(decl.Schema.Path, schema) != nil {
					return fmt.Errorf("output %q schema is invalid", o.ID)
				}
				compiled, err := compiler.Compile(decl.Schema.Path)
				if err != nil || compiled.Validate(value) != nil {
					return fmt.Errorf("output %q fails embedded schema", o.ID)
				}
			}
		}
	}
	if len(seen) != len(receipts) {
		return fmt.Errorf("package output set is incomplete")
	}
	for id, declaration := range declared {
		if declaration.Required && !seen[id] {
			return fmt.Errorf("package is missing required output %q", id)
		}
	}
	plan, ok := seen[manifest.PlanOutput.ID]
	if !ok || !plan {
		return fmt.Errorf("designated plan output is absent")
	}
	for _, o := range manifest.Outputs {
		if o.ID == manifest.PlanOutput.ID && (o.Path != manifest.PlanOutput.Path || o.MediaType != manifest.PlanOutput.MediaType || o.SHA256 != manifest.PlanOutput.SHA256 || !o.Required) {
			return fmt.Errorf("designated plan output is invalid")
		}
	}
	return nil
}

func rejectExtraFiles(root string, manifest Manifest, approval bool) error {
	allowed := map[string]bool{"manifest.json": true, "contract.json": true, "work-receipt.json": true}
	allowedDirectories := map[string]bool{".": true}
	if approval {
		allowed["approval.json"] = true
	}
	for _, o := range manifest.Outputs {
		path := filepath.ToSlash(o.Path)
		allowed[path] = true
		for directory := filepath.ToSlash(filepath.Dir(path)); directory != "."; directory = filepath.ToSlash(filepath.Dir(directory)) {
			allowedDirectories[directory] = true
		}
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("package contains symlink %q", rel)
		}
		if d.IsDir() {
			if !allowedDirectories[rel] {
				return fmt.Errorf("package contains undeclared directory %q", rel)
			}
			return nil
		}
		if !d.Type().IsRegular() || !allowed[rel] {
			return fmt.Errorf("package contains undeclared member %q", rel)
		}
		return nil
	})
}

func Enumerate(repository string) ([][2]string, error) {
	root := filepath.Join(repository, ".boatstack", "planning-packages")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result [][2]string
	for _, delivery := range entries {
		if !delivery.IsDir() || !ValidSegment(delivery.Name()) {
			continue
		}
		packages, err := os.ReadDir(filepath.Join(root, delivery.Name()))
		if err != nil {
			return nil, fmt.Errorf("read planning-package delivery %q: %w", delivery.Name(), err)
		}
		for _, pkg := range packages {
			if pkg.IsDir() && ValidFingerprint(pkg.Name()) {
				result = append(result, [2]string{delivery.Name(), pkg.Name()})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i][0] == result[j][0] {
			return result[i][1] < result[j][1]
		}
		return result[i][0] < result[j][0]
	})
	return result, nil
}

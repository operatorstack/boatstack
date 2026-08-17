package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/operatorstack/boatstack/boatstack/internal/hostprojection"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentity"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

const ConfigSchemaVersion = 5

type ProjectSettings struct {
	Name          string            `json:"name"`
	DefaultBranch string            `json:"default_branch"`
	Context       []string          `json:"context,omitempty"`
	Commands      map[string]string `json:"commands"`
	HighRiskPaths []string          `json:"high_risk_paths,omitempty"`
}

type PolicySettings struct {
	PlanApproval                 string `json:"plan_approval"`
	IndependentReviewForHighRisk bool   `json:"independent_review_for_high_risk,omitempty"`
	VisualEvidence               string `json:"visual_evidence"`
	ExternalEffectAuthority      string `json:"external_effect_authority,omitempty"`
}

// SubprocessExtensionSettings is a repository-selected, checksum-bound
// additive capability. It cannot select or replace the program runtime.
type SubprocessExtensionSettings struct {
	ID             string          `json:"id"`
	Version        string          `json:"version"`
	Executable     string          `json:"executable"`
	SHA256         string          `json:"sha256"`
	Manifest       json.RawMessage `json:"manifest"`
	Settings       json.RawMessage `json:"settings,omitempty"`
	DeadlineMillis int             `json:"deadline_millis,omitempty"`
	StdoutBytes    int64           `json:"stdout_bytes,omitempty"`
	StderrBytes    int64           `json:"stderr_bytes,omitempty"`
}

type IdentitySettings struct {
	Default string                              `json:"default"`
	Roles   map[string]humanidentity.Descriptor `json:"roles"`
}

type ProjectConfig struct {
	SchemaVersion int                           `json:"schema_version"`
	Identity      IdentitySettings              `json:"identity"`
	Project       ProjectSettings               `json:"project"`
	Policy        PolicySettings                `json:"policy"`
	Hosts         []string                      `json:"hosts"`
	Projections   []string                      `json:"projections"`
	Extensions    []SubprocessExtensionSettings `json:"extensions,omitempty"`
}

var canonicalHosts = []string{"claude", "cli", "codex", "cursor", "gemini", "mcp", "sdk"}
var extensionID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)+$`)

func CanonicalHosts() []string { return append([]string(nil), canonicalHosts...) }

func CanonicalProjections() []string { return hostprojection.CanonicalStrings() }

func (c ProjectConfig) ProjectionIDs() ([]hostprojection.ID, error) {
	return hostprojection.Parse(c.Projections, c.Hosts)
}

func (c ProjectConfig) ProjectionSelectionFingerprint() (string, error) {
	projections, err := c.ProjectionIDs()
	if err != nil {
		return "", err
	}
	return hostprojection.SelectionFingerprint(projections)
}

func (c ProjectConfig) ControlPolicy() model.ConfigurationPolicy {
	external := c.Policy.ExternalEffectAuthority
	if external == "" {
		external = "human-or-autonomy-plus-provider"
	}
	return model.ConfigurationPolicy{
		PlanApproval: c.Policy.PlanApproval, IndependentReviewForHighRisk: c.Policy.IndependentReviewForHighRisk,
		VisualEvidence: c.Policy.VisualEvidence, ExternalEffectAuthority: external, Hosts: append([]string(nil), c.Hosts...),
	}.Canonical()
}

func DecodeProjectConfig(value []byte) (ProjectConfig, error) {
	var config ProjectConfig
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return ProjectConfig{}, fmt.Errorf("decode Boatstack project configuration: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ProjectConfig{}, fmt.Errorf("Boatstack project configuration contains trailing JSON")
	}
	if err := config.Validate(); err != nil {
		return ProjectConfig{}, err
	}
	return config, nil
}

// ProjectConfigFingerprint binds configuration authority to strict schema-5
// semantics rather than checkout-specific JSON bytes. Formatting, object-key
// order, line endings, the defaulted external-effect policy, and host ordering
// therefore cannot make an otherwise identical configuration stale.
func ProjectConfigFingerprint(value []byte) (ProjectConfig, string, error) {
	config, err := DecodeProjectConfig(value)
	if err != nil {
		return ProjectConfig{}, "", err
	}
	canonical := config
	canonical.Hosts = append([]string(nil), config.Hosts...)
	sort.Strings(canonical.Hosts)
	canonical.Projections = append([]string(nil), config.Projections...)
	sort.Strings(canonical.Projections)
	canonical.Extensions = append([]SubprocessExtensionSettings(nil), config.Extensions...)
	for index := range canonical.Extensions {
		values := []struct {
			name  string
			value *json.RawMessage
		}{{"manifest", &canonical.Extensions[index].Manifest}, {"settings", &canonical.Extensions[index].Settings}}
		for _, item := range values {
			name, value := item.name, item.value
			if len(*value) == 0 {
				continue
			}
			var decoded any
			if err := json.Unmarshal(*value, &decoded); err != nil {
				return ProjectConfig{}, "", fmt.Errorf("canonicalize extension %q %s: %w", canonical.Extensions[index].ID, name, err)
			}
			*value, err = json.Marshal(decoded)
			if err != nil {
				return ProjectConfig{}, "", fmt.Errorf("canonicalize extension %q %s: %w", canonical.Extensions[index].ID, name, err)
			}
		}
	}
	sort.Slice(canonical.Extensions, func(i, j int) bool { return canonical.Extensions[i].ID < canonical.Extensions[j].ID })
	if canonical.Policy.ExternalEffectAuthority == "" {
		canonical.Policy.ExternalEffectAuthority = "human-or-autonomy-plus-provider"
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return ProjectConfig{}, "", fmt.Errorf("encode canonical Boatstack project configuration: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return config, hex.EncodeToString(digest[:]), nil
}

func (c ProjectConfig) Validate() error {
	if c.SchemaVersion != ConfigSchemaVersion || c.Project.Name == "" || c.Project.DefaultBranch == "" || c.Project.Commands == nil {
		return fmt.Errorf("Boatstack project configuration requires schema 5, project name, default branch, commands, and named human identities")
	}
	if len(c.Identity.Roles) == 0 {
		return fmt.Errorf("Boatstack project configuration requires at least one human identity role")
	}
	if err := humanidentity.ValidateRole(c.Identity.Default); err != nil {
		return err
	}
	if _, ok := c.Identity.Roles[c.Identity.Default]; !ok {
		return fmt.Errorf("HUMAN_IDENTITY_ROLE_UNBOUND: default role %q is not defined", c.Identity.Default)
	}
	for role, descriptor := range c.Identity.Roles {
		if err := humanidentity.ValidateRole(role); err != nil {
			return err
		}
		if err := descriptor.Validate(); err != nil {
			return fmt.Errorf("human identity role %q: %w", role, err)
		}
	}
	if err := ValidateGitBranch(c.Project.DefaultBranch); err != nil {
		return fmt.Errorf("invalid default branch: %w", err)
	}
	switch c.Policy.PlanApproval {
	case "human", "human-or-autonomy":
	default:
		return fmt.Errorf("unsupported plan approval policy %q", c.Policy.PlanApproval)
	}
	switch c.Policy.VisualEvidence {
	case "off", "optional", "required":
	default:
		return fmt.Errorf("unsupported visual evidence policy %q", c.Policy.VisualEvidence)
	}
	if c.Policy.ExternalEffectAuthority != "" && c.Policy.ExternalEffectAuthority != "human-or-autonomy-plus-provider" {
		return fmt.Errorf("unsupported external effect authority policy %q", c.Policy.ExternalEffectAuthority)
	}
	allowed := map[string]bool{}
	for _, host := range canonicalHosts {
		allowed[host] = true
	}
	seen := map[string]bool{}
	for _, host := range c.Hosts {
		if !allowed[host] || seen[host] {
			return fmt.Errorf("unsupported or duplicated host %q", host)
		}
		seen[host] = true
	}
	if !seen["cli"] {
		return fmt.Errorf("Boatstack project configuration must enable the canonical CLI surface")
	}
	if _, err := hostprojection.Parse(c.Projections, c.Hosts); err != nil {
		return err
	}
	seenExtensions := map[string]bool{}
	for _, extension := range c.Extensions {
		if !extensionID.MatchString(extension.ID) || extension.Version == "" || !filepath.IsAbs(extension.Executable) || filepath.Clean(extension.Executable) != extension.Executable || len(extension.SHA256) != 64 || len(extension.Manifest) == 0 {
			return fmt.Errorf("subprocess extension requires semantic id, version, exact absolute executable, SHA-256, and declarative manifest")
		}
		if _, err := hex.DecodeString(extension.SHA256); err != nil {
			return fmt.Errorf("subprocess extension %q has invalid SHA-256", extension.ID)
		}
		if seenExtensions[extension.ID] {
			return fmt.Errorf("duplicated subprocess extension %q", extension.ID)
		}
		seenExtensions[extension.ID] = true
		if extension.DeadlineMillis < 0 || extension.StdoutBytes < 0 || extension.StderrBytes < 0 {
			return fmt.Errorf("subprocess extension %q has negative limits", extension.ID)
		}
		values := []struct {
			name  string
			value json.RawMessage
		}{{"manifest", extension.Manifest}, {"settings", extension.Settings}}
		for _, item := range values {
			name, value := item.name, item.value
			if len(value) == 0 {
				continue
			}
			var settings any
			decoder := json.NewDecoder(bytes.NewReader(value))
			decoder.UseNumber()
			if err := decoder.Decode(&settings); err != nil {
				return fmt.Errorf("subprocess extension %q %s is invalid JSON", extension.ID, name)
			}
			var trailing any
			if err := decoder.Decode(&trailing); err != io.EOF {
				return fmt.Errorf("subprocess extension %q %s contains trailing JSON", extension.ID, name)
			}
			if name == "manifest" {
				if _, ok := settings.(map[string]any); !ok {
					return fmt.Errorf("subprocess extension %q manifest must be a JSON object", extension.ID)
				}
			}
		}
	}
	return nil
}

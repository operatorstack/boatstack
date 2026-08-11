package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
)

const ConfigSchemaVersion = 2

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

type ProjectConfig struct {
	SchemaVersion int             `json:"schema_version"`
	Project       ProjectSettings `json:"project"`
	Policy        PolicySettings  `json:"policy"`
	Hosts         []string        `json:"hosts"`
}

var canonicalHosts = []string{"claude", "cli", "codex", "cursor", "gemini", "mcp"}

func CanonicalHosts() []string { return append([]string(nil), canonicalHosts...) }

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
		return ProjectConfig{}, fmt.Errorf("decode V2 project configuration: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ProjectConfig{}, fmt.Errorf("V2 project configuration contains trailing JSON")
	}
	if err := config.Validate(); err != nil {
		return ProjectConfig{}, err
	}
	return config, nil
}

func (c ProjectConfig) Validate() error {
	if c.SchemaVersion != ConfigSchemaVersion || c.Project.Name == "" || c.Project.DefaultBranch == "" || c.Project.Commands == nil {
		return fmt.Errorf("V2 project configuration requires schema 2, project name, default branch, and commands")
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
		return fmt.Errorf("V2 project configuration must enable the canonical CLI surface")
	}
	return nil
}

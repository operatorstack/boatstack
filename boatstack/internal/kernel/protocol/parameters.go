package protocol

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
)

type Parameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Parameters []Parameter

type MissingParameterError struct {
	Transition catalog.TransitionID
	Parameter  string
}

func (e MissingParameterError) Error() string {
	return fmt.Sprintf("transition %q requires parameter %q", e.Transition, e.Parameter)
}

func IsMissingParameter(err error) bool {
	var missing MissingParameterError
	return errors.As(err, &missing)
}

func (p Parameters) Canonical() Parameters {
	result := append(Parameters(nil), p...)
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (p Parameters) Validate(transition catalog.Transition) error {
	allowed := map[string]catalog.ParameterSpec{}
	for _, spec := range transition.Parameters {
		allowed[spec.Name] = spec
	}
	seen := map[string]bool{}
	for _, parameter := range p {
		if parameter.Name == "" || parameter.Value == "" {
			return fmt.Errorf("transition %q parameters require non-empty names and values", transition.ID)
		}
		if seen[parameter.Name] {
			return fmt.Errorf("transition %q parameter %q is duplicated", transition.ID, parameter.Name)
		}
		if _, ok := allowed[parameter.Name]; !ok {
			return fmt.Errorf("transition %q does not accept parameter %q", transition.ID, parameter.Name)
		}
		seen[parameter.Name] = true
		switch parameter.Name {
		case "source_path", "config_path", "destination", "evidence_path", "manifest_path", "body_path":
			if !filepath.IsAbs(parameter.Value) {
				return fmt.Errorf("transition %q parameter %q must be an absolute path", transition.ID, parameter.Name)
			}
		case "branch", "head_ref":
			if err := ValidateGitBranch(parameter.Value); err != nil {
				return fmt.Errorf("transition %q parameter %q: %w", transition.ID, parameter.Name, err)
			}
		case "base_ref":
			if err := ValidateGitReference(parameter.Value); err != nil {
				return fmt.Errorf("transition %q parameter %q: %w", transition.ID, parameter.Name, err)
			}
		case "topology":
			if parameter.Value != "detached" && parameter.Value != "hybrid" {
				return fmt.Errorf("transition %q requires detached or hybrid topology", transition.ID)
			}
		case "config_authority":
			if parameter.Value != "repository" && parameter.Value != "external" {
				return fmt.Errorf("transition %q requires repository or external configuration authority", transition.ID)
			}
		}
	}
	for _, spec := range transition.Parameters {
		if spec.Required && !seen[spec.Name] {
			return MissingParameterError{Transition: transition.ID, Parameter: spec.Name}
		}
	}
	return nil
}

func (p Parameters) Get(name string) (string, bool) {
	for _, parameter := range p {
		if parameter.Name == name {
			return parameter.Value, true
		}
	}
	return "", false
}

package boatstack

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

var codexSkillFields = map[string]string{
	"name":        "!!str",
	"description": "!!str",
}

var claudeSkillFields = map[string]string{
	"name":                     "!!str",
	"description":              "!!str",
	"argument-hint":            "!!str",
	"user-invocable":           "!!bool",
	"disable-model-invocation": "!!bool",
}

func validateGeneratedSkills(files map[string][]byte) error {
	for _, path := range sortedKeys(files) {
		if !strings.HasSuffix(path, "/SKILL.md") {
			continue
		}
		if err := validateSkillFrontmatter(path, files[path]); err != nil {
			return fmt.Errorf("validate generated skill %s: %w", path, err)
		}
	}
	return nil
}

func validateSkillFrontmatter(path string, raw []byte) error {
	const opening = "---\n"
	if !strings.HasPrefix(string(raw), opening) {
		return fmt.Errorf("must start with YAML frontmatter delimiter ---")
	}
	closing := strings.Index(string(raw[len(opening):]), "\n---\n")
	if closing < 0 {
		return fmt.Errorf("frontmatter is missing its closing ---")
	}
	frontmatter := raw[len(opening) : len(opening)+closing]
	if strings.ContainsRune(string(frontmatter), '\t') {
		return fmt.Errorf("frontmatter must not contain tabs")
	}

	var document yaml.Node
	decoder := yaml.NewDecoder(strings.NewReader(string(frontmatter)))
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("parse YAML frontmatter: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("frontmatter must contain exactly one YAML document")
		}
		return fmt.Errorf("parse YAML frontmatter: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("frontmatter must be one YAML mapping")
	}

	allowed, err := skillFieldsForPath(path)
	if err != nil {
		return err
	}
	root := document.Content[0]
	fields := map[string]*yaml.Node{}
	for index := 0; index < len(root.Content); index += 2 {
		key := root.Content[index]
		value := root.Content[index+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return fmt.Errorf("frontmatter keys must be strings")
		}
		if key.Column != 1 {
			return fmt.Errorf("top-level key %q must start at column 1", key.Value)
		}
		expectedTag, ok := allowed[key.Value]
		if !ok {
			return fmt.Errorf("unsupported frontmatter field %q", key.Value)
		}
		if _, duplicate := fields[key.Value]; duplicate {
			return fmt.Errorf("duplicate frontmatter field %q", key.Value)
		}
		if value.Kind != yaml.ScalarNode || value.Tag != expectedTag {
			return fmt.Errorf("frontmatter field %q must have type %s", key.Value, yamlTypeName(expectedTag))
		}
		fields[key.Value] = value
	}

	for _, required := range []string{"name", "description"} {
		value, ok := fields[required]
		if !ok || strings.TrimSpace(value.Value) == "" {
			return fmt.Errorf("frontmatter field %q is required and must not be empty", required)
		}
	}
	expectedName := filepath.Base(filepath.Dir(filepath.FromSlash(path)))
	if fields["name"].Value != expectedName {
		return fmt.Errorf("frontmatter name %q must match skill directory %q", fields["name"].Value, expectedName)
	}
	return nil
}

func skillFieldsForPath(path string) (map[string]string, error) {
	switch {
	case strings.HasPrefix(path, ".agents/skills/"):
		return codexSkillFields, nil
	case strings.HasPrefix(path, ".claude/skills/"):
		return claudeSkillFields, nil
	default:
		return nil, fmt.Errorf("unsupported generated skill path")
	}
}

func yamlTypeName(tag string) string {
	if tag == "!!bool" {
		return "boolean"
	}
	return "string"
}

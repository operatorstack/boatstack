package boatstack

import (
	"strings"
	"testing"
)

func TestGeneratedSkillFrontmatterIsValidYAML(t *testing.T) {
	config := testConfig()
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildExportBundle(".boatstack-project.json", config, raw, "boatstack")
	if err != nil {
		t.Fatal(err)
	}

	skillCount := 0
	for path, value := range bundle.Files {
		if !strings.HasSuffix(path, "/SKILL.md") {
			continue
		}
		skillCount++
		if err := validateSkillFrontmatter(path, value); err != nil {
			t.Errorf("%s has invalid frontmatter: %v", path, err)
		}
	}
	if expected := len(claudeVisibleSkills) + 2; skillCount != expected {
		t.Fatalf("validated %d generated skills, want %d", skillCount, expected)
	}
}

func TestBoatstackRoutersHaveUnindentedTopLevelKeys(t *testing.T) {
	config := testConfig()
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildExportBundle(".boatstack-project.json", config, raw, "boatstack")
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		".agents/skills/boatstack/SKILL.md",
		".claude/skills/boatstack/SKILL.md",
	} {
		frontmatter := skillFrontmatterForTest(t, bundle.Files[path])
		if !strings.Contains(frontmatter, "\ndescription: Use when") {
			t.Errorf("%s description is not at column 1:\n%s", path, frontmatter)
		}
		for _, line := range strings.Split(frontmatter, "\n") {
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				t.Errorf("%s has an indented top-level key %q", path, line)
			}
		}
	}
}

func TestValidateSkillFrontmatterRejectsMalformedMetadata(t *testing.T) {
	validCodex := "---\nname: boatstack\ndescription: Use when testing.\n---\n\n# Boatstack\n"
	validClaude := "---\nname: boatstack\ndescription: Use when testing.\nuser-invocable: false\n---\n\n# Boatstack\n"
	tests := []struct {
		name string
		path string
		raw  string
		want string
	}{
		{"tab indentation", ".agents/skills/boatstack/SKILL.md", "---\nname: boatstack\n\tdescription: broken\n---\n", "must not contain tabs"},
		{"space indentation", ".agents/skills/boatstack/SKILL.md", "---\n name: boatstack\n description: broken\n---\n", "column 1"},
		{"invalid YAML", ".agents/skills/boatstack/SKILL.md", "---\nname: [\ndescription: broken\n---\n", "parse YAML"},
		{"missing opening delimiter", ".agents/skills/boatstack/SKILL.md", "name: boatstack\n", "must start"},
		{"missing closing delimiter", ".agents/skills/boatstack/SKILL.md", "---\nname: boatstack\n", "missing its closing"},
		{"duplicate field", ".agents/skills/boatstack/SKILL.md", "---\nname: boatstack\nname: boatstack\ndescription: duplicate\n---\n", "duplicate"},
		{"missing description", ".agents/skills/boatstack/SKILL.md", "---\nname: boatstack\n---\n", "description"},
		{"empty description", ".agents/skills/boatstack/SKILL.md", "---\nname: boatstack\ndescription: '  '\n---\n", "must not be empty"},
		{"unsupported Codex field", ".agents/skills/boatstack/SKILL.md", strings.Replace(validCodex, "description:", "user-invocable: false\ndescription:", 1), "unsupported"},
		{"wrong Claude type", ".claude/skills/boatstack/SKILL.md", strings.Replace(validClaude, "user-invocable: false", "user-invocable: no", 1), "boolean"},
		{"directory mismatch", ".agents/skills/other/SKILL.md", validCodex, "must match skill directory"},
		{"unsupported host", ".other/skills/boatstack/SKILL.md", validCodex, "unsupported generated skill path"},
		{"non-mapping", ".agents/skills/boatstack/SKILL.md", "---\n- name\n- description\n---\n", "one YAML mapping"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSkillFrontmatter(test.path, []byte(test.raw))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestGeneratedSkillHarnessRejectsInvalidBundle(t *testing.T) {
	files := map[string][]byte{
		".agents/skills/boatstack/SKILL.md": []byte("---\nname: boatstack\n\tdescription: broken\n---\n"),
	}
	err := validateGeneratedSkills(files)
	if err == nil || !strings.Contains(err.Error(), ".agents/skills/boatstack/SKILL.md") {
		t.Fatalf("generated-skill boundary did not name and reject invalid output: %v", err)
	}
}

func skillFrontmatterForTest(t *testing.T, raw []byte) string {
	t.Helper()
	value := string(raw)
	closing := strings.Index(value[4:], "\n---\n")
	if !strings.HasPrefix(value, "---\n") || closing < 0 {
		t.Fatalf("test fixture lacks frontmatter delimiters: %q", value)
	}
	return value[:4+closing]
}

package softwaredelivery_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
)

func TestGeneratedSkillsProjectOnlyDeclaredEntriesWithHostParity(t *testing.T) {
	// control-law: hosts-receive-the-same-entry-contract-without-kernel-inference
	truth := true
	compiled := controlprogram.Compiled{Fingerprint: strings.Repeat("a", 64), Document: controlprogram.Document{
		Program: controlprogram.Program{ID: "product-delivery"},
		Targets: []controlprogram.Target{{ID: "published-pr", Predicate: controlprogram.Predicate{True: &truth}}},
		Entries: []controlprogram.Entry{{ID: "run", Target: "published-pr", Description: "Publish the reviewed change"}},
	}}
	files, err := softwareflow.GenerateSkills(compiled, []string{"codex", "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("generated file count = %d, want 3", len(files))
	}
	codex := files[".agents/skills/product-delivery-run/SKILL.md"]
	claude := files[".claude/skills/product-delivery-run/SKILL.md"]
	if strings.ReplaceAll(string(codex), "--host codex", "--host HOST") != strings.ReplaceAll(string(claude), "--host claude", "--host HOST") {
		t.Fatal("Codex and Claude entry contracts differ")
	}
	value := string(codex)
	for _, contract := range []string{"--flow product-delivery --entry run", "same run ID", "Nothing continues in the\nbackground", "no merge or deploy"} {
		if !strings.Contains(value, contract) {
			t.Fatalf("generated skill lacks %q", contract)
		}
	}
	for path := range files {
		if strings.Contains(path, "autoplan") || strings.Contains(path, "boatstack-run") {
			t.Fatalf("undeclared entry generated: %s", path)
		}
	}
}

func TestGeneratedSkillDescriptionIsQuotedYAML(t *testing.T) {
	description := "Implement: parser\n# heading\n---\nnext"
	compiled := controlprogram.Compiled{Document: controlprogram.Document{
		Program: controlprogram.Program{ID: "product-delivery"},
		Entries: []controlprogram.Entry{{ID: "run", Target: "published-pr", Description: description}},
	}}
	files, err := softwareflow.GenerateSkills(compiled, []string{"codex", "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for path, raw := range files {
		if !strings.HasSuffix(path, "SKILL.md") {
			continue
		}
		value := string(raw)
		if strings.Count(value, "\n---\n") != 1 {
			t.Fatalf("%s contains an injected frontmatter delimiter", path)
		}
		var rendered string
		for _, line := range strings.Split(value, "\n") {
			if strings.HasPrefix(line, "description: ") {
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "description: ")), &rendered); err != nil {
					t.Fatalf("%s description is not a quoted YAML/JSON scalar: %v", path, err)
				}
			}
		}
		expected := description + " Use only when the user explicitly selects this repository Flow entry."
		if rendered != expected {
			t.Fatalf("%s description = %q, want %q", path, rendered, expected)
		}
	}
}

func TestGeneratedRunSkillRequiresExplicitAbandonmentBeforeReplacement(t *testing.T) {
	compiled := controlprogram.Compiled{Document: controlprogram.Document{
		Program: controlprogram.Program{ID: "product-delivery"},
		Entries: []controlprogram.Entry{
			{ID: "run", Target: "published-pr"},
			{ID: "abandon", Target: "safely-abandoned"},
		},
	}}
	files, err := softwareflow.GenerateSkills(compiled, []string{"codex", "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 6 {
		t.Fatalf("generated file count = %d, want 6", len(files))
	}
	run := string(files[".agents/skills/product-delivery-run/SKILL.md"])
	for _, contract := range []string{"never retarget this run", "$product-delivery-abandon", "abandonment receipt", "starting a new run"} {
		if !strings.Contains(run, contract) {
			t.Fatalf("generated run skill lacks %q", contract)
		}
	}
	if _, ok := files[".agents/skills/product-delivery-abandon/SKILL.md"]; !ok {
		t.Fatal("abandonment entry skill was not generated")
	}
}

func TestGeneratedSkillsRejectKernelMaintenanceIdentity(t *testing.T) {
	compiled := controlprogram.Compiled{Document: controlprogram.Document{
		Program: controlprogram.Program{ID: "boatstack"},
		Entries: []controlprogram.Entry{{ID: "update", Target: "done"}},
	}}
	if _, err := softwareflow.GenerateSkills(compiled, []string{"codex"}); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("maintenance collision result = %v", err)
	}
}

func TestGeneratedSkillIdentityIsInjectiveAcrossProgramEntryPairs(t *testing.T) {
	generate := func(program, entry string) map[string][]byte {
		t.Helper()
		compiled := controlprogram.Compiled{Document: controlprogram.Document{
			Program: controlprogram.Program{ID: program},
			Entries: []controlprogram.Entry{{ID: entry, Target: "done"}},
		}}
		files, err := softwareflow.GenerateSkills(compiled, []string{"codex"})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	first := generate("a-b", "c")
	second := generate("a", "b-c")
	for path := range first {
		if _, collision := second[path]; collision {
			t.Fatalf("distinct program/entry pairs collide at %s", path)
		}
	}
	encoded := generate("product-delivery", "run-now")
	for path := range encoded {
		if strings.Contains(path, "--") {
			t.Fatalf("hyphenated entry produced an invalid path: %s", path)
		}
	}
	reservedPrefix := generate("product-delivery", "x072756e2d6e6f77")
	for path := range encoded {
		if _, collision := reservedPrefix[path]; collision {
			t.Fatalf("encoded and literal entry identities collide at %s", path)
		}
	}
}

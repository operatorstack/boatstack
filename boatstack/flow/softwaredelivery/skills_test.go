package softwaredelivery_test

import (
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

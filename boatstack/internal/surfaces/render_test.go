package surfaces

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/supervisor"
)

func TestShellRenderersConsumeOneCommandAST(t *testing.T) {
	// control-law: shell-rendering-never-changes-transition-semantics
	transition, ok := catalog.Default().Lookup("plan.create")
	if !ok {
		t.Fatal("missing plan.create")
	}
	goal := model.Goal{ID: "goal", Kind: model.GoalVerified, DeliveryID: "delivery"}
	parameters := protocol.Parameters{{Name: "source_path", Value: "/tmp/O'Brien plan.md"}, {Name: "delivery_id", Value: "delivery"}}
	command := PrescriptionCommand(transition, "/repo with space", goal, "flow", parameters)
	before := append([]string(nil), command.Arguments...)
	posix, err := RenderCommand(command, ShellPOSIX)
	if err != nil {
		t.Fatal(err)
	}
	powerShell, err := RenderCommand(command, ShellPowerShell)
	if err != nil {
		t.Fatal(err)
	}
	if posix == powerShell || posix == "" || powerShell == "" {
		t.Fatalf("renderings should be non-empty shell projections: %q / %q", posix, powerShell)
	}
	if !reflect.DeepEqual(command.Arguments, before) {
		t.Fatal("renderer mutated semantic command AST")
	}
	gitBash, err := RenderCommand(command, ShellGitBash)
	if err != nil {
		t.Fatal(err)
	}
	if gitBash != posix {
		t.Fatalf("Git Bash projection diverged from POSIX semantics: %q != %q", gitBash, posix)
	}
}

func TestCatalogArtifactsAreGeneratedFromEveryRuntimeTransition(t *testing.T) {
	registry := catalog.Default()
	markdown := RenderCatalogMarkdown(registry.All())
	mermaid := RenderCatalogMermaid(registry.All())
	for _, transition := range registry.All() {
		rowPrefix := "\n| `" + string(transition.ID) + "` | " + string(transition.Class) + " |"
		if strings.Count(markdown, rowPrefix) != 1 {
			t.Errorf("markdown does not contain transition %s exactly once", transition.ID)
		}
		if strings.Count(mermaid, string(transition.ID)+"<br/>") != 1 {
			t.Errorf("Mermaid does not contain transition %s exactly once", transition.ID)
		}
	}
	if markdown != RenderCatalogMarkdown(registry.All()) || mermaid != RenderCatalogMermaid(registry.All()) {
		t.Fatal("catalog artifact rendering is not deterministic")
	}
}

func TestLocusModelsAreGeneratedFromEveryRuntimeTransition(t *testing.T) {
	// control-law: formal-model-alphabet-is-the-runtime-catalog
	registry := catalog.Default()
	for _, render := range []func([]catalog.Transition) (string, error){RenderCatalogLocusSafety, RenderCatalogLocusLiveness} {
		one, err := render(registry.All())
		if err != nil {
			t.Fatal(err)
		}
		two, err := render(registry.All())
		if err != nil || one != two {
			t.Fatal("Locus catalog model is not deterministic")
		}
		var decoded struct {
			Events []struct {
				ID string `json:"id"`
			} `json:"events"`
		}
		if err := json.Unmarshal([]byte(one), &decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded.Events) != registry.Len() {
			t.Fatalf("Locus event count=%d, want %d", len(decoded.Events), registry.Len())
		}
		seen := map[string]bool{}
		for _, event := range decoded.Events {
			seen[event.ID] = true
		}
		for _, transition := range registry.All() {
			if !seen[string(transition.ID)] {
				t.Errorf("Locus model omitted transition %s", transition.ID)
			}
		}
	}
}

func TestCheckedArchitectureArtifactsMatchExecutableCatalog(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate checked architecture artifacts")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	checks := map[string]string{
		"boatstack-v2-transition-catalog.md":  RenderCatalogMarkdown(catalog.Default().All()),
		"boatstack-v2-transition-catalog.mmd": RenderCatalogMermaid(catalog.Default().All()),
	}
	safety, err := RenderCatalogLocusSafety(catalog.Default().All())
	if err != nil {
		t.Fatal(err)
	}
	liveness, err := RenderCatalogLocusLiveness(catalog.Default().All())
	if err != nil {
		t.Fatal(err)
	}
	checks["boatstack-v2-locus-safety.json"] = safety
	checks["boatstack-v2-locus-liveness.json"] = liveness
	for name, expected := range checks {
		path := filepath.Join(repositoryRoot, "docs", "architecture", name)
		actual, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != expected {
			t.Errorf("%s drifted; regenerate it with boatstack catalog", name)
		}
	}
}

func TestEveryHostConsumesOneSemanticPrescription(t *testing.T) {
	goal := model.Goal{ID: "goal", Kind: model.GoalVerified, DeliveryID: "delivery"}
	for _, transition := range catalog.Default().All() {
		if !transition.Controllable() {
			continue
		}
		parameters := make(protocol.Parameters, 0, len(transition.Parameters))
		for _, parameter := range transition.Parameters {
			parameters = append(parameters, protocol.Parameter{Name: parameter.Name, Value: "fixture-" + parameter.Name})
		}
		var canonical HostPrescription
		for index, host := range CanonicalHostNames() {
			projection, err := ProjectHostPrescription(host, transition, "/repo", goal, "flow", parameters)
			if err != nil {
				t.Fatal(err)
			}
			if index == 0 {
				canonical = projection
				continue
			}
			projection.Host = canonical.Host
			if !reflect.DeepEqual(projection, canonical) {
				t.Fatalf("transition %s host %s changed control semantics: %#v != %#v", transition.ID, host, projection, canonical)
			}
		}
	}
}

func TestCanonicalHostsAreDataNotControllers(t *testing.T) {
	want := []string{"claude", "cli", "codex", "cursor", "gemini", "mcp"}
	if got := CanonicalHostNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts = %v, want %v", got, want)
	}
}

func TestGuardClassifierIsShellNeutralAndPrivacyBounded(t *testing.T) {
	commands := []string{"git reset --hard HEAD~1", "GIT RESET --HARD HEAD~1", "git   reset   --hard   HEAD~1"}
	for _, command := range commands {
		intent := ClassifyCommandIntent(command)
		if intent.Class != supervisor.IntentDestructive || intent.Operation != "git.reset-hard" || intent.Fingerprint == "" {
			t.Fatalf("intent for %q = %#v", command, intent)
		}
	}
	managed := ClassifyCommandIntent("gh pr create --base main --head feature")
	if managed.Class != supervisor.IntentManagedBypass || managed.Transition != "publication.execute" {
		t.Fatalf("managed intent = %#v", managed)
	}
	ordinary := ClassifyCommandIntent("go test ./...")
	if ordinary.Class != supervisor.IntentOrdinary || ordinary.Transition != "" {
		t.Fatalf("ordinary intent = %#v", ordinary)
	}
}

func TestGuardPreservesConstitutionalDestructionFloor(t *testing.T) {
	destructive := []string{
		`psql -c "DROP SCHEMA public CASCADE"`,
		`psql -c "TRUNCATE TABLE accounts"`,
		`psql -c "DELETE FROM accounts"`,
		`psql -f migrate.sql`,
		`supabase db reset`,
		`prisma migrate reset --force`,
		`terraform destroy -auto-approve`,
		`rm -rf /`,
		`rm -rf build/*`,
		`Remove-Item -Recurse -Force $HOME`,
		`git reset --hard HEAD~1`,
		`git clean -fdx`,
		`git push --force-with-lease origin main`,
		`kubectl delete namespace production`,
		`docker volume rm data-volume`,
		`gcloud sql instances delete primary`,
		`aws ec2 terminate-instances --instance-ids i-1`,
		`gh pr merge 123 --squash`,
		`printf '{}' > .git/boatstack/v2/worktrees/x/state.json`,
		`Remove-Item -Recurse -Force .boatstack/evidence`,
	}
	for _, command := range destructive {
		intent := ClassifyCommandIntent(command)
		if intent.Class != supervisor.IntentDestructive || intent.Operation == "" || intent.Fingerprint == "" {
			t.Errorf("constitutional destruction was not denied: %q -> %#v", command, intent)
		}
	}
}

func TestGuardKeepsRoutineDataOperationsOrdinary(t *testing.T) {
	routine := []string{
		`git add migrate.sql`,
		`git commit -m "document DROP TABLE recovery"`,
		`git diff --stat migrate.sql`,
		`cat migrate.sql`,
		`psql -c "SELECT count(*) FROM accounts"`,
		`rm -rf build`,
		`go test ./...`,
		`rg -n "git reset --hard" docs`,
	}
	for _, command := range routine {
		intent := ClassifyCommandIntent(command)
		if intent.Class != supervisor.IntentOrdinary {
			t.Errorf("routine data operation was not ordinary: %q -> %#v", command, intent)
		}
	}
}

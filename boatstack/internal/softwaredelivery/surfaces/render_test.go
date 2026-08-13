package surfaces

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
)

func TestShellRenderersConsumeOneCommandAST(t *testing.T) {
	// control-law: shell-rendering-never-changes-transition-semantics
	transition, ok := testprogram.StandardRegistry().Lookup("plan.create")
	if !ok {
		t.Fatal("missing plan.create")
	}
	objective := model.Objective{ID: "objective", Kind: model.ObjectiveVerified, DeliveryID: "delivery"}
	parameters := protocol.Parameters{{Name: "source_path", Value: "/tmp/O'Brien plan.md"}, {Name: "delivery_id", Value: "delivery"}}
	prescription := protocol.Prescription{
		ID: "prx-fixture", Freshness: general.Freshness{ExpectedInstanceID: "repo-fixture", ExpectedStateRevision: 41, ExpectedProgramFingerprint: strings.Repeat("a", 64), ExpectedSnapshotFingerprint: strings.Repeat("b", 64), ExpectedObjectiveBindingFingerprint: strings.Repeat("c", 64), AuthorityFingerprint: "auth-fixture"},
		RequiredCapabilities:  []catalog.Capability{catalog.CapabilityRepositoryWrite, catalog.CapabilityCommandExecute},
		EffectiveCapabilities: []catalog.Capability{catalog.CapabilityRepositoryWrite, catalog.CapabilityCommandExecute},
	}
	command := PrescriptionCommand(transition, prescription, "corr-1", "/repo with space", objective, "run-1", "product-delivery", "run", parameters)
	joined := strings.Join(command.Arguments, " ")
	for _, binding := range []string{"--correlation corr-1", "--prescription-id prx-fixture", "--expected-instance-id repo-fixture", "--expected-state-revision 41", "--expected-program-fingerprint", "--expected-snapshot-fingerprint", "--expected-objective-binding-fingerprint", "--authority-fingerprint auth-fixture", "--required-capability repository.write", "--required-capability command.execute", "--effective-capability repository.write", "--effective-capability command.execute"} {
		if !strings.Contains(joined, binding) {
			t.Fatalf("prescription command omitted CAS binding %q: %s", binding, joined)
		}
	}
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
	registry := testprogram.StandardRegistry()
	markdown := RenderCatalogMarkdown(registry.All())
	if !strings.Contains(markdown, "| Required capabilities |") || !strings.Contains(markdown, "`repository.write`") {
		t.Fatal("catalog markdown omitted kernel-classified capability requirements")
	}
	mermaid := RenderCatalogMermaid(registry.All())
	for _, transition := range registry.All() {
		rowPrefix := "\n| `" + string(transition.ID) + "` | " + string(transition.Origin.Kind) + ":"
		if strings.Count(markdown, rowPrefix) != 1 {
			t.Errorf("markdown does not contain transition %s exactly once", transition.ID)
		}
		if strings.Count(mermaid, `["`+string(transition.ID)+`<br/>`) != 1 {
			t.Errorf("Mermaid does not contain transition %s exactly once", transition.ID)
		}
	}
	if !strings.Contains(mermaid, " --> ") {
		t.Fatal("Mermaid transition inventory is not connected to protocol phases")
	}
	if markdown != RenderCatalogMarkdown(registry.All()) || mermaid != RenderCatalogMermaid(registry.All()) {
		t.Fatal("catalog artifact rendering is not deterministic")
	}
}

func TestLocusModelsAreGeneratedFromEveryRuntimeTransition(t *testing.T) {
	// control-law: formal-model-alphabet-is-the-runtime-catalog
	registry := testprogram.StandardRegistry()
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

func TestEveryHostConsumesOneSemanticPrescription(t *testing.T) {
	objective := model.Objective{ID: "objective", Kind: model.ObjectiveVerified, DeliveryID: "delivery"}
	prescription := protocol.Prescription{ID: "prx-fixture", Freshness: general.Freshness{ExpectedInstanceID: "repo-fixture", ExpectedStateRevision: 41, ExpectedProgramFingerprint: strings.Repeat("a", 64), ExpectedSnapshotFingerprint: strings.Repeat("b", 64), ExpectedObjectiveBindingFingerprint: strings.Repeat("c", 64)}}
	for _, transition := range testprogram.StandardRegistry().All() {
		if !transition.Controllable() {
			continue
		}
		parameters := make(protocol.Parameters, 0, len(transition.Parameters))
		for _, parameter := range transition.Parameters {
			parameters = append(parameters, protocol.Parameter{Name: parameter.Name, Value: "fixture-" + parameter.Name})
		}
		var canonical HostPrescription
		for index, host := range CanonicalHostNames() {
			projection, err := ProjectHostPrescription(host, transition, prescription, "corr-1", "/repo", objective, "run-1", "product-delivery", "run", parameters)
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
	want := []string{"claude", "cli", "codex", "cursor", "gemini", "mcp", "sdk"}
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
	if managed.Class != supervisor.IntentManagedBypass || managed.Operation != "publication.create" || managed.Transition != "" {
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

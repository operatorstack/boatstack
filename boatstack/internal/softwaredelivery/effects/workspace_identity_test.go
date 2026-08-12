package effects

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/testprogram"
)

type workspaceIdentityBoundary struct{ calls int }

func (b *workspaceIdentityBoundary) PrepareObservation(context.Context, protocol.Admission, catalog.Transition, ports.ControllerLayout, *durable.State) error {
	b.calls++
	return nil
}

func (b *workspaceIdentityBoundary) Execute(context.Context, protocol.Admission, catalog.Transition, ports.ControllerLayout, durable.State) (ports.EffectResult, error) {
	b.calls++
	return ports.EffectResult{Settlement: ports.EffectSettled}, nil
}

func TestWorkspaceRemovalTransitionsRefuseForgedDurableDestinationBeforeEffect(t *testing.T) {
	// control-law: destructive workspace effects rebind durable paths to the admitted worktree identity
	ctx := context.Background()
	repository := recoveryRepository(t)
	other := filepath.Join(t.TempDir(), "other-worktree")
	command := exec.Command("git", "worktree", "add", "-q", "-b", "other-worktree", other)
	command.Dir = repository
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create other worktree: %v: %s", err, output)
	}

	clock := recoveryClock{value: time.Unix(4000, 0).UTC()}
	resolver, err := plant.NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := resolver.ResolveInvocation(ctx, repository, "cli", "forged-workspace-destination")
	if err != nil {
		t.Fatal(err)
	}
	layout, _, err := resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		t.Fatal(err)
	}
	branch := strings.TrimPrefix(invocation.Ref, "refs/heads/")
	state := durable.Default(invocation, clock.Now())
	state.ProgramFingerprint = testProgramFingerprint
	state.Phase, state.Engagement, state.Delivery = model.PhaseAbandoned, model.EngagementActive, model.DeliveryDiscarded
	state.Workspace, state.Terminal = model.WorkspaceAbandoned, model.TerminalEstablished
	state.WorkspacePath, state.WorkspaceBranch = other, branch
	state.WorkspaceSourcePath, state.WorkspaceSourceID, state.WorkspaceSourceRef = repository, invocation.WorktreeID, invocation.Ref
	raw, err := durable.EncodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(layout.StatePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.StatePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, transitionID := range []catalog.TransitionID{"workspace.cleanup", "workspace.reap"} {
		t.Run(string(transitionID), func(t *testing.T) {
			transition, ok := testprogram.StandardRegistry().Lookup(transitionID)
			if !ok {
				t.Fatalf("%s is absent", transitionID)
			}
			boundary := &workspaceIdentityBoundary{}
			driver, err := NewDriver(resolver, clock, boundary)
			if err != nil {
				t.Fatal(err)
			}
			admission := protocol.Admission{
				TransitionID: transition.ID, ExpectedStateRevision: state.Revision, ExpectedProgramFingerprint: testProgramFingerprint,
				Invocation: invocation, Parameters: protocol.Parameters{{Name: "branch", Value: branch}}, EffectiveCapabilities: catalog.RequiredCapabilities(transition),
			}
			if _, err := driver.Prepare(ctx, admission, transition); err == nil || !strings.Contains(err.Error(), "workspace destination identity changed") {
				t.Fatalf("forged workspace destination error = %v", err)
			}
			if boundary.calls != 0 {
				t.Fatalf("forged workspace destination reached effect boundary %d times", boundary.calls)
			}
		})
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("other worktree was removed: %v", err)
	}
}

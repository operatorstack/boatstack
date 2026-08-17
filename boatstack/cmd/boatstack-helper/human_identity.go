package main

import (
	"context"
	"fmt"

	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentity"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentitybinding"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

type humanIdentityResponseHandler interface {
	Handle(context.Context, surfaces.Request) (surfaces.Response, error)
}

// handleWithHumanIdentity is the single user-facing Kernel response boundary.
// A response cannot leave the command layer before every human authority
// surface is bound to the verified repository identity descriptor.
func handleWithHumanIdentity(ctx context.Context, handler humanIdentityResponseHandler, request surfaces.Request) (surfaces.Response, error) {
	return humanidentitybinding.Handle(ctx, "", handler, request)
}

func humanIdentityPresentationForRequest(request surfaces.Request) (humanidentity.Presentation, error) {
	return humanidentitybinding.PresentationForRequest(context.Background(), "", request, nil)
}

func humanIdentityPresentationForRepositoryBound(ctx context.Context, repository, host, correlation, role string, bundle boatstackruntime.ControlBundleSnapshot, observed *model.Snapshot) (humanidentity.Presentation, error) {
	return humanidentitybinding.PresentationForRepository(ctx, "", repository, host, correlation, role, &bundle, observed)
}

func humanIdentityPresentationForRepositoryDefault(ctx context.Context, repository, host, correlation string, bundle boatstackruntime.ControlBundleSnapshot, observed *model.Snapshot) (humanidentity.Presentation, error) {
	return humanidentitybinding.PresentationForRepositoryDefault(ctx, "", repository, host, correlation, &bundle, observed)
}

func humanIdentityPresentationForCurrentRepositoryDefault(ctx context.Context, repository, host, correlation string) (humanidentity.Presentation, error) {
	return humanidentitybinding.PresentationForRepositoryDefault(ctx, "", repository, host, correlation, nil, nil)
}

func humanIdentityPresentationForProgramChange(ctx context.Context, repository, host, correlation, programID, transitionID string, bundle boatstackruntime.ControlBundleSnapshot) (humanidentity.Presentation, error) {
	return humanidentitybinding.PresentationForProgramChange(ctx, "", surfaces.Request{
		Repository: repository, Host: host, CorrelationID: correlation, ProgramID: programID, TransitionID: catalog.TransitionID(transitionID),
		ControlBundle: &boatstackruntime.ControlBundleContract{Source: bundle},
	}, nil)
}

func humanIdentityPresentationForCurrentProgramChange(ctx context.Context, repository, host, correlation, transitionID string) (humanidentity.Presentation, error) {
	return humanidentitybinding.PresentationForProgramChange(ctx, "", surfaces.Request{
		Repository: repository, Host: host, CorrelationID: correlation, TransitionID: catalog.TransitionID(transitionID),
	}, nil)
}

func attachHumanIdentity(request surfaces.Request, response *surfaces.Response) error {
	return humanidentitybinding.Attach(context.Background(), "", request, response)
}

func renderHumanIdentity(presentation humanidentity.Presentation) {
	fmt.Printf("human_identity_role=%s human_identity_provider=%s kind=%s\n", presentation.Role, presentation.ProviderFingerprint, presentation.Descriptor.Kind)
	if presentation.Descriptor.Kind == humanidentity.KindLiteral {
		fmt.Printf("suggested_human_actor=%s\n", presentation.Descriptor.Value)
		return
	}
	fmt.Printf("human_identity_command=%q", presentation.Descriptor.Command)
	for _, argument := range presentation.Descriptor.Args {
		fmt.Printf(" %q", argument)
	}
	fmt.Println()
}

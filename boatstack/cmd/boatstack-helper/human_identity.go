package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentity"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

func humanIdentityPresentationForRequest(request surfaces.Request) (humanidentity.Presentation, error) {
	if request.ControlBundle == nil {
		return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_UNBOUND: request has no verified control bundle")
	}
	snapshot := request.ControlBundle.Source
	configPath := filepath.Join(request.Repository, ".boatstack", "project.json")
	if request.TransitionID == "installation.initialize" {
		if request.ControlBundle.Target == nil {
			return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_UNBOUND: initialization has no target control bundle")
		}
		snapshot = *request.ControlBundle.Target
		value, ok := request.Parameters.Get("config_path")
		if !ok {
			return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_UNBOUND: initialization has no configuration path")
		}
		configPath = value
	}
	return humanIdentityPresentationFromBoundConfig(configPath, snapshot)
}

func humanIdentityPresentationFromBoundConfig(configPath string, snapshot boatstackruntime.ControlBundleSnapshot) (humanidentity.Presentation, error) {
	var binding *boatstackruntime.ControlBundleFile
	for index := range snapshot.Files {
		if snapshot.Files[index].Path == ".boatstack/project.json" {
			binding = &snapshot.Files[index]
			break
		}
	}
	if binding == nil || binding.Absent {
		return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_UNBOUND: verified project configuration is absent")
	}
	info, err := os.Lstat(configPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_UNBOUND: project configuration is not a regular file")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return humanidentity.Presentation{}, err
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != binding.SHA256 {
		return humanidentity.Presentation{}, fmt.Errorf("HUMAN_IDENTITY_DRIFT: project configuration does not match the verified control bundle")
	}
	config, err := protocol.DecodeProjectConfig(raw)
	if err != nil {
		return humanidentity.Presentation{}, err
	}
	return humanidentity.NewPresentation(config.Identity.Human)
}

func humanIdentityPresentationForRepository(repository string) (humanidentity.Presentation, error) {
	presentation, _, err := humanIdentityPresentationAndFingerprintForRepository(repository)
	return presentation, err
}

func humanIdentityPresentationAndFingerprintForRepository(repository string) (humanidentity.Presentation, string, error) {
	raw, err := os.ReadFile(filepath.Join(repository, ".boatstack", "project.json"))
	if err != nil {
		return humanidentity.Presentation{}, "", err
	}
	config, fingerprint, err := protocol.ProjectConfigFingerprint(raw)
	if err != nil {
		return humanidentity.Presentation{}, "", err
	}
	presentation, err := humanidentity.NewPresentation(config.Identity.Human)
	return presentation, fingerprint, err
}

func attachHumanIdentity(request surfaces.Request, response *surfaces.Response) error {
	if response == nil || response.Question == nil || !questionRequiresHuman(*response.Question) {
		return nil
	}
	// Before installation there is no repository-selected identity descriptor
	// to bind. The bootstrap caller must supply an explicit actor; once a
	// candidate or installed configuration exists, every human question below
	// is required to carry its verified descriptor.
	if request.ControlBundle == nil && response.Question.TransitionID == "installation.initialize" {
		return nil
	}
	var presentation humanidentity.Presentation
	var err error
	if request.ControlBundle != nil {
		presentation, err = humanIdentityPresentationForRequest(request)
	} else if request.ProgramID == "" && response.Snapshot != nil {
		var fingerprint string
		presentation, fingerprint, err = humanIdentityPresentationAndFingerprintForRepository(request.Repository)
		if err == nil {
			bound := false
			for _, evidence := range response.Snapshot.Configuration.Evidence {
				if evidence.Fingerprint == fingerprint {
					bound = true
					break
				}
			}
			if !bound {
				return fmt.Errorf("HUMAN_IDENTITY_DRIFT: project configuration does not match the observed configuration")
			}
		}
	} else {
		err = fmt.Errorf("HUMAN_IDENTITY_UNBOUND: human authority question has no verified project configuration")
	}
	if err != nil {
		return err
	}
	response.Question.HumanIdentity = &presentation
	return nil
}

func questionRequiresHuman(question surfaces.Question) bool {
	for _, authority := range append(append([]catalog.AuthorityClass(nil), question.Authority...), question.AuthorityAll...) {
		if authority == catalog.AuthorityHuman {
			return true
		}
	}
	return false
}

func renderHumanIdentity(presentation humanidentity.Presentation) {
	fmt.Printf("human_identity_provider=%s kind=%s\n", presentation.ProviderFingerprint, presentation.Descriptor.Kind)
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

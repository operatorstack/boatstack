package surfaces

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
)

type Shell string

const (
	ShellPOSIX      Shell = "posix"
	ShellPowerShell Shell = "powershell"
	ShellGitBash    Shell = "git-bash"
)

// CommandAST is the sole semantic prescription consumed by every shell and
// host renderer.
type CommandAST struct {
	Executable string
	Arguments  []string
}

func PrescriptionCommand(transition catalog.Transition, prescription protocol.Prescription, correlation, repository string, goal model.Goal, flowID string, parameters protocol.Parameters) CommandAST {
	arguments := []string{"apply", "--repo", repository, "--transition", string(transition.ID), "--flow", flowID,
		"--correlation", correlation, "--prescription-id", prescription.ID,
		"--expected-state-revision", strconv.FormatUint(prescription.ExpectedStateRevision, 10),
		"--expected-program-fingerprint", prescription.ExpectedProgramFingerprint,
		"--expected-snapshot-fingerprint", prescription.ExpectedSnapshotFingerprint}
	if goal.Validate() == nil {
		arguments = append(arguments, "--goal-kind", string(goal.Kind), "--delivery", goal.DeliveryID, "--goal-id", goal.ID)
	}
	canonical := parameters.Canonical()
	for _, parameter := range canonical {
		arguments = append(arguments, "--param", parameter.Name+"="+parameter.Value)
	}
	return CommandAST{Executable: "boatstack", Arguments: arguments}
}

func RenderCommand(command CommandAST, shell Shell) (string, error) {
	if command.Executable == "" {
		return "", fmt.Errorf("command executable is required")
	}
	quote := quotePOSIX
	if shell == ShellPowerShell {
		quote = quotePowerShell
	} else if shell != ShellPOSIX && shell != ShellGitBash {
		return "", fmt.Errorf("unsupported shell %q", shell)
	}
	parts := []string{quote(command.Executable)}
	for _, argument := range command.Arguments {
		parts = append(parts, quote(argument))
	}
	return strings.Join(parts, " "), nil
}

type HostPrescription struct {
	Host                  string     `json:"host"`
	TransitionID          string     `json:"transition_id"`
	Command               CommandAST `json:"command"`
	AuthorityPrompt       string     `json:"authority_prompt,omitempty"`
	ExpectedPostcondition string     `json:"expected_postcondition"`
}

// ProjectHostPrescription changes host capability metadata only. Every host
// consumes the same semantic command, authority prompt, and postcondition.
func ProjectHostPrescription(host string, transition catalog.Transition, prescription protocol.Prescription, correlation, repository string, goal model.Goal, flowID string, parameters protocol.Parameters) (HostPrescription, error) {
	known := false
	for _, candidate := range CanonicalHostNames() {
		if host == candidate {
			known = true
			break
		}
	}
	if !known {
		return HostPrescription{}, fmt.Errorf("unsupported host %q", host)
	}
	return HostPrescription{
		Host: host, TransitionID: string(transition.ID), Command: PrescriptionCommand(transition, prescription, correlation, repository, goal, flowID, parameters),
		AuthorityPrompt: transition.Prescription.AuthorityPrompt, ExpectedPostcondition: transition.Prescription.ExpectedPostcondition,
	}, nil
}

func quotePOSIX(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\r\n'\"\\$`;&|<>()[]{}*!?") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func quotePowerShell(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\r\n'\"`$;&|<>()[]{}") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func CanonicalHostNames() []string {
	return protocol.CanonicalHosts()
}

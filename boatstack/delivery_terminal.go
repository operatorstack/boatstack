package boatstack

import "strings"

// The delivery terminal is the standing goal of the flow — the state past
// which nothing more is owed. It resolves in a fixed order: the goal the
// delivery was ACTIVATED under (state.Goal — hysteresis, so a mid-flight
// config change never silently changes an in-progress delivery's goal), then
// the repository config (delivery.terminal), then the published default.
// Every unreadable or invalid input resolves to the narrower published goal:
// a goal is widened only by an explicit, verifiable operator choice.
// control-law: terminal-goal-defaults-to-published-and-hydrates-from-state-then-config
type DeliveryTerminal string

const (
	// TerminalPublished — the flow is done when the slice's PR is open.
	TerminalPublished DeliveryTerminal = "published"
	// TerminalMerged — the flow keeps naming read-only post-publish steps
	// until the PR is observed merged.
	TerminalMerged DeliveryTerminal = "merged"
)

func normalizeDeliveryTerminal(value string) (DeliveryTerminal, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(TerminalPublished):
		return TerminalPublished, true
	case string(TerminalMerged):
		return TerminalMerged, true
	default:
		return "", false
	}
}

// configuredDeliveryTerminal reads the repository's standing terminal from
// the project config. Absent, invalid, or unreadable configuration resolves
// to published — never an error, because the terminal is consulted from
// read-only paths that must not gain a new failure mode.
func configuredDeliveryTerminal(repo string) DeliveryTerminal {
	config, _, err := LoadConfig(WorkspaceFor(repo).ProjectConfigPath())
	if err != nil || config.Delivery == nil {
		return TerminalPublished
	}
	if terminal, ok := normalizeDeliveryTerminal(config.Delivery.Terminal); ok {
		return terminal
	}
	return TerminalPublished
}

// resolveDeliveryTerminal resolves the terminal for one feature: the
// activation snapshot first, then config, then the default.
func resolveDeliveryTerminal(repo, feature string) DeliveryTerminal {
	if strings.TrimSpace(feature) != "" {
		if state, err := LoadDeliveryState(repo, feature); err == nil {
			if terminal, ok := normalizeDeliveryTerminal(state.Goal); ok {
				return terminal
			}
		}
	}
	return configuredDeliveryTerminal(repo)
}

// deliveryGoalSnapshot is what activation records on the new delivery state.
// Only the non-default goal is snapshotted: a default-config delivery keeps
// an empty Goal, so its persisted state is byte-identical to before this
// field existed.
func deliveryGoalSnapshot(repo string) string {
	if configuredDeliveryTerminal(repo) == TerminalMerged {
		return string(TerminalMerged)
	}
	return ""
}

package boatstack

// Context projection for Detached Supervision. In embedded mode the coding agent
// reads Boatstack's generated references and project.json from the repository; in
// detached mode those files are external, so the agent needs a bounded, read-only
// projection of the current supervisory position for the operation it is running.
// This reuses the authoritative resolver (ResolveNext) and the deterministic
// next-move oracle (NextControl) rather than reconstructing the state machine.

// OperatorContext is the bounded, read-only view a host needs to run one operation
// under Boatstack supervision, in either ownership mode.
type OperatorContext struct {
	SchemaVersion      int    `json:"schema_version"`
	Mode               string `json:"mode"`
	Attached           bool   `json:"attached"`
	RepoRoot           string `json:"repo_root"`
	ControlRoot        string `json:"control_root,omitempty"`
	Operation          string `json:"operation,omitempty"`
	Host               string `json:"host,omitempty"`
	Feature            string `json:"feature,omitempty"`
	VerificationStatus string `json:"verification_status"`
	ObservedStage      string `json:"observed_stage,omitempty"`
	ActiveSlice        string `json:"active_slice,omitempty"`
	NextOperation      string `json:"next_operation,omitempty"`
	RecommendedCommand string `json:"recommended_command,omitempty"`
	RemainingFlowCost  int    `json:"remaining_flow_cost,omitempty"`
	Reason             string `json:"reason"`
}

// ProjectOperatorContext returns the bounded supervisory context for a repository.
// It is read-only. For an attached-but-unverifiable detached repository it reports
// BLOCKED with a bounded recovery action rather than a normal position.
func ProjectOperatorContext(repoPath, operation, host string) (OperatorContext, error) {
	root, err := ResolveRepository(repoPath)
	if err != nil {
		return OperatorContext{}, err
	}
	out := OperatorContext{
		SchemaVersion: detachedSchemaVersion, Mode: string(SupervisionEmbedded),
		RepoRoot: root, ControlRoot: root, Operation: operation, Host: host,
	}

	if ctx, ok, verifyErr := detachedContextFor(root); verifyErr != nil {
		out.Mode = string(SupervisionDetached)
		out.Attached = true
		out.VerificationStatus = "BLOCKED"
		out.Reason = verifyErr.Error() + " Run `boatstack-helper detached-status --repo .` and reattach."
		return out, nil
	} else if ok {
		out.Mode = string(SupervisionDetached)
		out.Attached = true
		out.ControlRoot = ctx.controlRoot
	}

	next, err := NextControl(root, "")
	if err != nil {
		return out, nil
	}
	status, statusErr := ResolveNext(root, "")
	if statusErr == nil {
		out.Feature = status.Feature
		out.VerificationStatus = status.VerificationStatus
		out.ObservedStage = status.ObservedStage
		out.ActiveSlice = status.ActiveSlice
	}
	out.NextOperation = next.RecommendedOp
	out.RemainingFlowCost = next.RemainingCost
	if out.Reason == "" {
		out.Reason = next.Reason
	}
	if next.Prescribed != nil {
		out.RecommendedCommand = next.Prescribed.CommandLine()
	}
	return out, nil
}

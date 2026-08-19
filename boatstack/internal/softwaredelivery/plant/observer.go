package plant

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	workpackage "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery/workpackage"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

type TimeSource interface{ Now() time.Time }

type Observer struct {
	resolver Resolver
	clock    TimeSource
}

func NewObserver(resolver Resolver, clock TimeSource) (Observer, error) {
	if clock == nil {
		return Observer{}, fmt.Errorf("plant observer requires a clock")
	}
	return Observer{resolver: resolver, clock: clock}, nil
}

func hashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func (o Observer) Observe(ctx context.Context, request ports.ObservationRequest) (model.Observation, error) {
	invocation := request.Invocation
	layout, current, err := o.resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		return model.Observation{}, err
	}
	now := o.clock.Now().UTC()
	state, stateEvidence, stateExists, err := o.readState(layout.StatePath, current, now)
	if err != nil {
		return model.Observation{}, err
	}
	if state.RepositoryID != current.RepositoryID || state.GitCommonID != current.GitCommonID || state.WorktreeID != current.WorktreeID {
		return model.Observation{}, fmt.Errorf("durable state identity does not match invocation")
	}
	gitEvidence, head, worktreeFingerprint, err := o.gitEvidence(ctx, layout.RepositoryRoot, now)
	if err != nil {
		return model.Observation{}, err
	}
	configEvidence, _, configExists, err := fileEvidence(layout.ConfigPath, "configuration", now)
	if err != nil {
		return model.Observation{}, err
	}
	configuration := state.Configuration
	configurationPolicy := model.Absent[model.ConfigurationPolicy]("no valid Boatstack configuration policy", configEvidence)
	if !configExists {
		configuration = model.ConfigurationUnsupported
	} else if configRaw, readErr := os.ReadFile(layout.ConfigPath); readErr != nil {
		return model.Observation{}, readErr
	} else if config, configFingerprint, decodeErr := protocol.ProjectConfigFingerprint(configRaw); decodeErr != nil {
		configuration = model.ConfigurationConflicting
		configurationPolicy = model.Unknown[model.ConfigurationPolicy](model.FactConflicting, decodeErr.Error(), configEvidence)
	} else {
		configEvidence.Fingerprint = configFingerprint
		policy := config.ControlPolicy()
		highRisk, highRiskErr := o.highRiskChange(ctx, layout.RepositoryRoot, config.Project.DefaultBranch, config.Project.HighRiskPaths)
		if highRiskErr != nil && policy.IndependentReviewForHighRisk && len(config.Project.HighRiskPaths) > 0 {
			highRisk = true
		}
		policy.HighRiskChange = highRisk
		policyEvidence := append([]model.Evidence{configEvidence}, gitEvidence...)
		configurationPolicy = model.Fact[model.ConfigurationPolicy]{Status: model.FactKnown, Value: policy, Evidence: policyEvidence}
		if state.ConfigFingerprint == "" || state.ConfigFingerprint != configFingerprint {
			configuration = model.ConfigurationStale
		} else if state.Configuration == model.ConfigurationVerified && !sameConfigurationPolicy(state.ConfigurationPolicy(), policy) {
			configuration = model.ConfigurationConflicting
		}
	}
	runtimeState := state.Runtime
	runtimeEvidence := append([]model.Evidence(nil), stateEvidence...)
	pinPath := boatstackruntime.PinPath(layout.RepositoryRoot)
	pinEvidence, _, pinExists, pinEvidenceErr := fileEvidence(pinPath, "runtime-pin", now)
	if pinEvidenceErr != nil {
		return model.Observation{}, pinEvidenceErr
	}
	runtimeEvidence = append(runtimeEvidence, pinEvidence)
	if state.RuntimeVersion == "" || state.RuntimeFingerprint == "" || state.RuntimeSource == "" {
		if runtimeState == model.RuntimeVerified {
			runtimeState = model.RuntimeInvalid
		}
		if pinExists {
			runtimeState = model.RuntimeConflicting
			if !stateExists && state.Runtime == model.RuntimeAbsent {
				pinRaw, readPinErr := os.ReadFile(pinPath)
				if readPinErr != nil {
					return model.Observation{}, readPinErr
				}
				pin, decodePinErr := boatstackruntime.DecodePin(pinRaw)
				if decodePinErr == nil && durable.CanReadStateSchema(pin.StateSchemaVersion) &&
					pin.Version == current.RuntimeVersion && pin.SHA256 == current.RuntimeFingerprint {
					home, homeErr := boatstackruntime.Home("")
					if homeErr != nil {
						return model.Observation{}, homeErr
					}
					runtimePath, pathErr := boatstackruntime.ExecutablePath(home, pin.Identity())
					if pathErr == nil {
						evidence, fingerprint, exists, runtimeErr := fileEvidence(runtimePath, "runtime", now)
						if runtimeErr != nil {
							return model.Observation{}, runtimeErr
						}
						runtimeEvidence = append(runtimeEvidence, evidence)
						switch {
						case !exists:
							runtimeState = model.RuntimeStale
						case boatstackruntime.VerifyExecutable(runtimePath, pin.Identity()) != nil || fingerprint != pin.SHA256:
							runtimeState = model.RuntimeStale
						default:
							runtimeState = model.RuntimeAbsent
						}
					}
				}
			}
		}
	} else if !pinExists {
		runtimeState = model.RuntimeAbsent
	} else {
		pinRaw, readPinErr := os.ReadFile(pinPath)
		if readPinErr != nil {
			return model.Observation{}, readPinErr
		}
		pin, decodePinErr := boatstackruntime.DecodePin(pinRaw)
		identity := boatstackruntime.Identity{Version: state.RuntimeVersion, SHA256: state.RuntimeFingerprint, SourceRevision: state.RuntimeSource}
		if decodePinErr != nil || pin.Identity() != identity || pin.ProgramFingerprint != state.ProgramFingerprint || !durable.CanReadStateSchema(pin.StateSchemaVersion) {
			runtimeState = model.RuntimeConflicting
		} else {
			home, homeErr := boatstackruntime.Home("")
			if homeErr != nil {
				return model.Observation{}, homeErr
			}
			runtimePath, pathErr := boatstackruntime.ExecutablePath(home, identity)
			if pathErr != nil {
				runtimeState = model.RuntimeInvalid
			} else {
				evidence, fingerprint, exists, runtimeErr := fileEvidence(runtimePath, "runtime", now)
				if runtimeErr != nil {
					return model.Observation{}, runtimeErr
				}
				runtimeEvidence = append(runtimeEvidence, evidence)
				if !exists {
					runtimeState = model.RuntimeAbsent
				} else if verifyErr := boatstackruntime.VerifyExecutable(runtimePath, identity); verifyErr != nil || fingerprint != state.RuntimeFingerprint {
					runtimeState = model.RuntimeStale
				} else if current.RuntimeVersion != state.RuntimeVersion || current.RuntimeFingerprint != state.RuntimeFingerprint {
					runtimeState = model.RuntimeWrongSource
				}
			}
		}
	}
	verification := state.Verification
	if state.SourceRevision != "" && head != "" && state.SourceRevision != head {
		verification = model.VerificationStale
	}
	if state.WorktreeFingerprint != "" && state.WorktreeFingerprint != worktreeFingerprint {
		verification = model.VerificationStale
	}
	plan, artifactVerification, artifactTerminal, planEvidence, artifactEvidence, artifactErr := observeRepositoryArtifacts(layout, state, now)
	if artifactErr != nil {
		return model.Observation{}, artifactErr
	}
	if artifactVerification != state.Verification {
		verification = artifactVerification
	}
	workPackageEvidence := append([]model.Evidence(nil), stateEvidence...)
	workPackageFact := model.Fact[model.WorkPackageState]{Status: model.FactKnown, Value: state.WorkPackage, Evidence: workPackageEvidence}
	if state.WorkPackage != model.WorkPackageAbsent {
		evidence, valid, workPackageErr := observeWorkPackage(layout, state, now)
		if workPackageErr != nil {
			return model.Observation{}, workPackageErr
		}
		workPackageEvidence = append(workPackageEvidence, evidence...)
		workPackageFact.Evidence = workPackageEvidence
		if !valid {
			workPackageFact.Status = model.FactStale
			// The durable value records what was last admitted. Once its
			// immutable evidence is invalid, the resolver must not treat that
			// value as a current valid or approved package. Project absence with
			// explicit staleness so the already-selected admission transition is
			// the only package-domain recovery path.
			workPackageFact.Value = model.WorkPackageAbsent
			artifactTerminal = model.TerminalStale
		}
	}
	phase := state.Phase
	delivery := state.Delivery
	recoveryFact := model.Fact[model.RecoveryState]{Status: model.FactKnown, Value: state.Recovery, Evidence: stateEvidence}
	transactionFact := model.Fact[model.TransactionState]{Status: model.FactKnown, Value: state.Transaction, Evidence: stateEvidence}
	recoveryInfoFact := model.Absent[model.RecoveryContext]("no recovery context", stateEvidence...)
	transactionInfoFact := model.Absent[model.TransactionContext]("no active transaction", stateEvidence...)
	terminal := artifactTerminal
	class := state.Objective.TrustedObjectiveClass()
	requiresCurrentImplementation := class == model.ObjectiveVerified || class == model.ObjectiveOpenPR
	currentDeliveryInvalid := verification != model.VerificationCurrent || configuration != model.ConfigurationVerified || runtimeState != model.RuntimeVerified
	if requiresCurrentImplementation && (terminal == model.TerminalStale || (terminal == model.TerminalEstablished && currentDeliveryInvalid)) {
		terminal, phase, delivery = model.TerminalStale, model.PhaseActive, model.DeliveryActive
		if runtimeState == model.RuntimeAbsent {
			phase = model.PhaseObserved
		}
	} else if (class == model.ObjectiveApprovedPlan || class == model.ObjectiveApprovedWorkPackage) && terminal == model.TerminalStale {
		phase, delivery = model.PhaseActive, model.DeliveryPlanning
	}
	if class == model.ObjectiveMerged && state.Publication == model.PublicationMerged && state.Delivery == model.DeliveryTerminal &&
		(state.Workspace == model.WorkspaceLanded || state.Workspace == model.WorkspaceAbsent) {
		terminal, phase = model.TerminalEstablished, model.PhaseTerminal
	}
	if class == model.ObjectiveAbandoned && state.Delivery == model.DeliveryDiscarded &&
		(state.Workspace == model.WorkspaceAbandoned || state.Workspace == model.WorkspaceAbsent) {
		terminal, phase = model.TerminalEstablished, model.PhaseAbandoned
	}
	if state.Recovery != model.RecoveryNone && state.TransactionID != "" {
		recoveryInfoFact = model.Known(model.RecoveryContext{
			TransactionID: state.TransactionID, Cause: state.RecoveryCause, SourcePhase: state.RecoverySourcePhase,
			Permitted: []string{"recovery.escalate"}, BudgetRemaining: state.RecoveryBudget, Resumption: state.RecoveryResumption,
		}, stateEvidence[0])
	}
	if state.Transaction != model.TransactionNone && state.TransactionID != "" {
		transactionInfoFact = model.Known(model.TransactionContext{ID: state.TransactionID, TransitionID: state.TransactionTransition, Status: string(state.Transaction)}, stateEvidence[0])
	}
	pending, pendingErr := pendingJournalEvidence(layout.JournalRoot, request.IgnoreAdmissionID, now)
	if pendingErr != nil {
		return model.Observation{}, pendingErr
	}
	recordedProgramFingerprint := state.ProgramFingerprint
	if pending.ProgramFingerprint != "" {
		if recordedProgramFingerprint != "" && recordedProgramFingerprint != pending.ProgramFingerprint && !pending.ReconcilesProgram {
			return model.Observation{}, fmt.Errorf("durable state and pending transaction bind different control programs")
		}
		recordedProgramFingerprint = pending.ProgramFingerprint
	}
	if pending.Conflicting {
		evidence := append(append([]model.Evidence(nil), stateEvidence...), pending.Evidence...)
		phase = model.PhaseUnresolved
		recoveryFact = model.Unknown[model.RecoveryState](model.FactConflicting, "several interrupted transactions require explicit selection", evidence...)
		transactionFact = model.Unknown[model.TransactionState](model.FactConflicting, "several interrupted transactions require explicit selection", evidence...)
		recoveryInfoFact = model.Unknown[model.RecoveryContext](model.FactConflicting, "transaction identity is ambiguous", evidence...)
		transactionInfoFact = model.Unknown[model.TransactionContext](model.FactConflicting, "transaction identity is ambiguous", evidence...)
		terminal = model.TerminalStale
	} else if pending.Found {
		evidence := append(append([]model.Evidence(nil), stateEvidence...), pending.Evidence...)
		phase = model.PhaseRecovery
		recoveryFact = model.Fact[model.RecoveryState]{Status: model.FactKnown, Value: model.RecoveryReconcile, Evidence: evidence}
		transactionFact = model.Fact[model.TransactionState]{Status: model.FactKnown, Value: pending.TransactionState, Evidence: evidence}
		recoveryInfoFact = model.Fact[model.RecoveryContext]{Status: model.FactKnown, Value: pending.Recovery, Evidence: pending.Evidence}
		transactionInfoFact = model.Fact[model.TransactionContext]{Status: model.FactKnown, Value: pending.Transaction, Evidence: pending.Evidence}
		terminal = model.TerminalStale
	}
	deliveryEvidence := append(append([]model.Evidence(nil), stateEvidence...), gitEvidence...)
	verificationEvidence := append(append([]model.Evidence(nil), deliveryEvidence...), artifactEvidence...)
	planEvidence = append(append([]model.Evidence(nil), stateEvidence...), planEvidence...)
	terminalEvidence := append(append([]model.Evidence(nil), stateEvidence...), artifactEvidence...)
	publication := currentPublicationState(layout, state, head, worktreeFingerprint)
	configurationEvidence := stateEvidence
	if configEvidence.Source != "" {
		configurationEvidence = append(append([]model.Evidence(nil), stateEvidence...), configEvidence)
	}
	objectiveFact := model.Absent[model.Objective]("no configured Boatstack objective", stateEvidence...)
	if state.Objective.Validate() == nil {
		objectiveFact = model.Fact[model.Objective]{Status: model.FactKnown, Value: state.Objective, Evidence: stateEvidence}
	}
	return model.Observation{
		SchemaVersion: model.SnapshotSchemaVersion, StateRevision: state.Revision, RecordedProgramFingerprint: recordedProgramFingerprint, Invocation: current,
		Phase:               model.Fact[model.ProtocolPhase]{Status: model.FactKnown, Value: phase, Evidence: stateEvidence},
		Engagement:          model.Fact[model.EngagementState]{Status: model.FactKnown, Value: state.Engagement, Evidence: stateEvidence},
		Delivery:            model.Fact[model.DeliveryState]{Status: model.FactKnown, Value: delivery, Evidence: deliveryEvidence},
		Workspace:           model.Fact[model.WorkspaceState]{Status: model.FactKnown, Value: state.Workspace, Evidence: deliveryEvidence},
		WorkPackage:         workPackageFact,
		Plan:                model.Fact[model.PlanState]{Status: model.FactKnown, Value: plan, Evidence: planEvidence},
		Configuration:       model.Fact[model.ConfigurationState]{Status: model.FactKnown, Value: configuration, Evidence: configurationEvidence},
		ConfigurationPolicy: configurationPolicy,
		Runtime:             model.Fact[model.RuntimeState]{Status: model.FactKnown, Value: runtimeState, Evidence: runtimeEvidence},
		Publication:         model.Fact[model.PublicationState]{Status: model.FactKnown, Value: publication, Evidence: stateEvidence},
		Verification:        model.Fact[model.VerificationState]{Status: model.FactKnown, Value: verification, Evidence: verificationEvidence},
		Recovery:            recoveryFact,
		Transaction:         transactionFact,
		RecoveryInfo:        recoveryInfoFact,
		TransactionInfo:     transactionInfoFact,
		Terminal:            model.Fact[model.TerminalStatus]{Status: model.FactKnown, Value: terminal, Evidence: terminalEvidence},
		Objective:           objectiveFact,
		ObservedAt:          now,
	}, nil
}

type observedPublicationPreview struct {
	SchemaVersion       int    `json:"schema_version"`
	DeliveryID          string `json:"delivery_id"`
	SourceRevision      string `json:"source_revision"`
	WorktreeFingerprint string `json:"worktree_fingerprint"`
}

func currentPublicationState(layout ports.ControllerLayout, state durable.State, head, worktreeFingerprint string) model.PublicationState {
	if state.Publication != model.PublicationCandidate {
		return state.Publication
	}
	deliveryID := state.Objective.DeliveryID
	if deliveryID == "" || filepath.Base(deliveryID) != deliveryID || deliveryID == "." || deliveryID == ".." {
		return model.PublicationNone
	}
	raw, err := os.ReadFile(filepath.Join(layout.RepositoryRoot, ".boatstack", "publication", deliveryID+".preview.json"))
	if err != nil {
		return model.PublicationNone
	}
	var preview observedPublicationPreview
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(&preview) != nil || preview.SchemaVersion != 2 || preview.DeliveryID != deliveryID ||
		preview.SourceRevision != head || preview.WorktreeFingerprint != worktreeFingerprint {
		return model.PublicationNone
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return model.PublicationNone
	}
	return state.Publication
}

func (o Observer) highRiskChange(ctx context.Context, repository, defaultBranch string, patterns []string) (bool, error) {
	if len(patterns) == 0 {
		return false, nil
	}
	changed := map[string]bool{}
	base, err := o.resolver.git(ctx, repository, "rev-parse", "--verify", "--end-of-options", "refs/heads/"+defaultBranch+"^{commit}")
	if err != nil {
		return false, fmt.Errorf("resolve configured default branch: %w", err)
	}
	committed, err := o.resolver.git(ctx, repository, "diff", "--name-only", "--relative", "-z", base+"...HEAD")
	if err != nil {
		return false, err
	}
	for _, name := range strings.Split(committed, "\x00") {
		if name != "" {
			changed[filepath.ToSlash(name)] = true
		}
	}
	status, err := o.resolver.git(ctx, repository, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return false, err
	}
	records := strings.Split(status, "\x00")
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) < 4 {
			continue
		}
		code, name := record[:2], record[3:]
		changed[filepath.ToSlash(name)] = true
		if (code[0] == 'R' || code[0] == 'C' || code[1] == 'R' || code[1] == 'C') && index+1 < len(records) {
			index++
			if prior := records[index]; prior != "" {
				changed[filepath.ToSlash(prior)] = true
			}
		}
	}
	for name := range changed {
		for _, pattern := range patterns {
			matched, matchErr := doublestarMatch(filepath.ToSlash(pattern), name)
			if matchErr != nil {
				return false, matchErr
			}
			if matched {
				return true, nil
			}
		}
	}
	return false, nil
}

func doublestarMatch(pattern, name string) (bool, error) {
	if pattern == "" || strings.HasPrefix(pattern, "/") || strings.Contains(pattern, "..") {
		return false, fmt.Errorf("invalid high-risk path pattern %q", pattern)
	}
	runes := []rune(filepath.ToSlash(pattern))
	var expression strings.Builder
	expression.WriteString("^(?:")
	for index := 0; index < len(runes); index++ {
		switch runes[index] {
		case '*':
			if index+1 < len(runes) && runes[index+1] == '*' {
				index++
				if index+1 < len(runes) && runes[index+1] == '/' {
					index++
					expression.WriteString(`(?:.*/)?`)
				} else {
					expression.WriteString(`.*`)
				}
			} else {
				expression.WriteString(`[^/]*`)
			}
		case '?':
			expression.WriteString(`[^/]`)
		default:
			expression.WriteString(regexp.QuoteMeta(string(runes[index])))
		}
	}
	expression.WriteString(")$")
	compiled, err := regexp.Compile(expression.String())
	if err != nil {
		return false, err
	}
	return compiled.MatchString(filepath.ToSlash(name)), nil
}

func (o Observer) readState(path string, invocation model.InvocationContext, now time.Time) (durable.State, []model.Evidence, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fingerprint := hashBytes([]byte("absent:" + path + ":" + invocation.RepositoryID + ":" + invocation.WorktreeID))
			evidence := []model.Evidence{{Source: path, Fingerprint: fingerprint, ObservedAt: now}}
			return durable.Default(invocation, now), evidence, false, nil
		}
		return durable.State{}, nil, false, fmt.Errorf("read durable state: %w", err)
	}
	state, err := durable.DecodeState(raw)
	if err != nil {
		return durable.State{}, nil, false, fmt.Errorf("decode durable state: %w", err)
	}
	return state, []model.Evidence{{Source: path, Fingerprint: hashBytes(raw), ObservedAt: now}}, true, nil
}

func (o Observer) gitEvidence(ctx context.Context, repository string, now time.Time) ([]model.Evidence, string, string, error) {
	head, err := o.resolver.git(ctx, repository, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return nil, "", "", fmt.Errorf("observe exact Git HEAD: %w", err)
	}
	if head == "" {
		return nil, "", "", fmt.Errorf("observe exact Git HEAD: empty object identity")
	}
	status, err := o.resolver.git(ctx, repository, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, "", "", fmt.Errorf("observe Git worktree status: %w", err)
	}
	status = canonicalProductStatus(status)
	fingerprint := hashBytes([]byte(head + "\x00" + status))
	return []model.Evidence{{Source: "git:" + repository, Fingerprint: fingerprint, Revision: head, ObservedAt: now}}, head, fingerprint, nil
}

func canonicalProductStatus(status string) string {
	records := strings.Split(status, "\x00")
	kept := make([]string, 0, len(records))
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) < 4 {
			continue
		}
		code, name := record[:2], filepath.ToSlash(record[3:])
		prior := ""
		if (code[0] == 'R' || code[0] == 'C' || code[1] == 'R' || code[1] == 'C') && index+1 < len(records) {
			index++
			prior = filepath.ToSlash(records[index])
		}
		if generatedBoatstackPath(name) && (prior == "" || generatedBoatstackPath(prior)) {
			continue
		}
		kept = append(kept, code+" "+name)
		if prior != "" {
			kept = append(kept, "prior "+prior)
		}
	}
	sort.Strings(kept)
	return strings.Join(kept, "\x00")
}

func generatedBoatstackPath(name string) bool {
	name = strings.TrimPrefix(filepath.ToSlash(name), "./")
	for _, prefix := range []string{".boatstack/approvals/", ".boatstack/evidence/", ".boatstack/work-packages/", ".boatstack/plans/", ".boatstack/publication/"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func fileEvidence(path, source string, now time.Time) (model.Evidence, string, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fingerprint := hashBytes([]byte("absent:" + path))
			return model.Evidence{Source: source + ":" + path, Fingerprint: fingerprint, ObservedAt: now}, fingerprint, false, nil
		}
		return model.Evidence{}, "", false, err
	}
	fingerprint := hashBytes(raw)
	return model.Evidence{Source: source + ":" + path, Fingerprint: fingerprint, ObservedAt: now}, fingerprint, true, nil
}

func sameConfigurationPolicy(one, two model.ConfigurationPolicy) bool {
	one, two = one.Canonical(), two.Canonical()
	return one.PlanApproval == two.PlanApproval &&
		one.IndependentReviewForHighRisk == two.IndependentReviewForHighRisk &&
		one.VisualEvidence == two.VisualEvidence &&
		one.ExternalEffectAuthority == two.ExternalEffectAuthority &&
		slices.Equal(one.Hosts, two.Hosts)
}

type observedApproval struct {
	SchemaVersion      int       `json:"schema_version"`
	DeliveryID         string    `json:"delivery_id"`
	PlanFingerprint    string    `json:"plan_fingerprint"`
	PackageFingerprint string    `json:"package_fingerprint,omitempty"`
	Actor              string    `json:"actor"`
	AdmissionID        string    `json:"admission_id"`
	ApprovedAt         time.Time `json:"approved_at"`
}

type observedPlanPromotion struct {
	SchemaVersion                  int       `json:"schema_version"`
	DeliveryID                     string    `json:"delivery_id"`
	PlanFingerprint                string    `json:"plan_fingerprint"`
	WorkPackageFingerprint         string    `json:"work_package_fingerprint"`
	WorkPackageApprovalFingerprint string    `json:"work_package_approval_fingerprint"`
	PlanOutputID                   string    `json:"plan_output_id"`
	AdmissionID                    string    `json:"admission_id"`
	PromotedAt                     time.Time `json:"promoted_at"`
	Fingerprint                    string    `json:"fingerprint"`
}

type observedGate struct {
	SchemaVersion int       `json:"schema_version"`
	DeliveryID    string    `json:"delivery_id"`
	TransitionID  string    `json:"transition_id"`
	Revision      string    `json:"revision"`
	Fingerprint   string    `json:"fingerprint"`
	AdmissionID   string    `json:"admission_id"`
	RecordedAt    time.Time `json:"recorded_at"`
}

type observedGateEvidence struct {
	SchemaVersion  int       `json:"schema_version"`
	Gate           string    `json:"gate"`
	SourceRevision string    `json:"source_revision"`
	Outcome        string    `json:"outcome"`
	Producer       string    `json:"producer"`
	CompletedAt    time.Time `json:"completed_at"`
}

func decodeStrictJSON[T any](raw []byte, value *T) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("artifact contains trailing JSON")
	}
	return nil
}

func observeRepositoryArtifacts(layout ports.ControllerLayout, state durable.State, now time.Time) (model.PlanState, model.VerificationState, model.TerminalStatus, []model.Evidence, []model.Evidence, error) {
	plan, verification, terminal := state.Plan, state.Verification, state.Terminal
	var planEvidence, verificationEvidence []model.Evidence
	if state.Objective.Validate() != nil {
		return plan, verification, terminal, planEvidence, verificationEvidence, nil
	}
	deliveryID := state.Objective.DeliveryID
	if state.Plan != model.PlanAbsent {
		path := filepath.Join(layout.RepositoryRoot, ".boatstack", "plans", deliveryID+".source")
		evidence, fingerprint, exists, err := fileEvidence(path, "plan", now)
		if err != nil {
			return plan, verification, terminal, nil, nil, err
		}
		planEvidence = append(planEvidence, evidence)
		if !exists || state.PlanFingerprint == "" || fingerprint != state.PlanFingerprint {
			plan, terminal = model.PlanStale, model.TerminalStale
		}
	}
	if state.Plan == model.PlanApproved || state.Plan == model.PlanLocked {
		path := filepath.Join(layout.RepositoryRoot, ".boatstack", "approvals", deliveryID+".json")
		evidence, fingerprint, exists, err := fileEvidence(path, "approval", now)
		if err != nil {
			return plan, verification, terminal, nil, nil, err
		}
		planEvidence = append(planEvidence, evidence)
		valid := exists && state.ApprovalFingerprint != "" && fingerprint == state.ApprovalFingerprint
		if valid {
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return plan, verification, terminal, nil, nil, readErr
			}
			if state.WorkPackageFingerprint != "" {
				var promotion observedPlanPromotion
				valid = valid && decodeStrictJSON(raw, &promotion) == nil && promotion.SchemaVersion == 2 &&
					promotion.DeliveryID == deliveryID && promotion.PlanFingerprint == state.PlanFingerprint &&
					promotion.WorkPackageFingerprint == state.WorkPackageFingerprint &&
					promotion.WorkPackageApprovalFingerprint == state.WorkPackageApprovalFingerprint &&
					promotion.PlanOutputID != "" && promotion.AdmissionID != "" && !promotion.PromotedAt.IsZero()
				if valid {
					identity := promotion
					identity.Fingerprint = ""
					identityRaw, encodeErr := json.MarshalIndent(identity, "", "  ")
					identityRaw = append(identityRaw, '\n')
					valid = encodeErr == nil && promotion.Fingerprint == hashBytes(identityRaw)
				}
			} else {
				var approval observedApproval
				valid = valid && decodeStrictJSON(raw, &approval) == nil && approval.SchemaVersion == 1 &&
					approval.DeliveryID == deliveryID && approval.PlanFingerprint == state.PlanFingerprint && approval.PackageFingerprint == "" &&
					approval.Actor != "" && approval.AdmissionID != "" && !approval.ApprovedAt.IsZero()
			}
		}
		if !valid {
			plan, terminal = model.PlanStale, model.TerminalStale
		}
	}

	gateNames := map[string]string{
		"build": "gate.build.record", "test": "gate.test.record", "review": "gate.review.record",
		"change": "gate.change.record", "journey": "gate.journey.record",
	}
	hasVisual := false
	for _, gate := range state.Gates {
		if gate.Gate == "visual" {
			hasVisual = true
			path := filepath.Join(layout.EvidenceRoot, deliveryID, "visual-manifest.json")
			evidence, fingerprint, exists, err := fileEvidence(path, "visual-evidence", now)
			if err != nil {
				return plan, verification, terminal, nil, nil, err
			}
			if exists && fingerprint == gate.Fingerprint && gate.Revision == state.SourceRevision {
				evidence.Revision = gate.Revision
			}
			verificationEvidence = append(verificationEvidence, evidence)
			if !exists || fingerprint != gate.Fingerprint || gate.Revision == "" {
				verification, terminal = model.VerificationStale, model.TerminalStale
			}
			continue
		}
		transitionID, known := gateNames[gate.Gate]
		path := filepath.Join(layout.EvidenceRoot, deliveryID, gate.Gate+".json")
		evidence, _, exists, err := fileEvidence(path, "gate-evidence:"+gate.Gate, now)
		if err != nil {
			return plan, verification, terminal, nil, nil, err
		}
		valid := known && exists
		if valid {
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return plan, verification, terminal, nil, nil, readErr
			}
			var artifact observedGate
			valid = decodeStrictJSON(raw, &artifact) == nil && artifact.SchemaVersion == 1 &&
				artifact.DeliveryID == deliveryID && artifact.TransitionID == transitionID &&
				artifact.Revision == gate.Revision && artifact.Fingerprint == gate.Fingerprint &&
				artifact.AdmissionID != "" && !artifact.RecordedAt.IsZero()
		}
		payloadPath := filepath.Join(layout.EvidenceRoot, deliveryID, gate.Gate+".evidence.json")
		payloadEvidence, payloadFingerprint, payloadExists, payloadErr := fileEvidence(payloadPath, "gate-payload", now)
		if payloadErr != nil {
			return plan, verification, terminal, nil, nil, payloadErr
		}
		verificationEvidence = append(verificationEvidence, payloadEvidence)
		if payloadExists {
			payloadRaw, readErr := os.ReadFile(payloadPath)
			if readErr != nil {
				return plan, verification, terminal, nil, nil, readErr
			}
			var payload observedGateEvidence
			valid = valid && payloadFingerprint == gate.Fingerprint && decodeStrictJSON(payloadRaw, &payload) == nil &&
				payload.SchemaVersion == 1 && payload.Gate == gate.Gate && payload.SourceRevision == gate.Revision &&
				payload.Outcome == "passed" && payload.Producer != "" && !payload.CompletedAt.IsZero()
		} else {
			valid = false
		}
		if valid && gate.Revision == state.SourceRevision {
			evidence.Revision = gate.Revision
		}
		verificationEvidence = append(verificationEvidence, evidence)
		if !valid {
			verification, terminal = model.VerificationStale, model.TerminalStale
		}
	}
	if state.Verification == model.VerificationCurrent && !state.RequiredGateEvidenceCurrent() {
		verification, terminal = model.VerificationStale, model.TerminalStale
	}
	if terminal == model.TerminalEstablished && state.Objective.TrustedObjectiveClass() == model.ObjectiveVerified && state.VisualEvidencePolicy == "required" && !hasVisual {
		verification, terminal = model.VerificationUnresolved, model.TerminalStale
	}
	return plan, verification, terminal, planEvidence, verificationEvidence, nil
}

func observeWorkPackage(layout ports.ControllerLayout, state durable.State, now time.Time) ([]model.Evidence, bool, error) {
	root := filepath.Join(layout.RepositoryRoot, ".boatstack", "work-packages", state.Objective.DeliveryID, state.WorkPackageFingerprint)
	manifestPath := filepath.Join(root, "manifest.json")
	manifestEvidence, _, exists, err := fileEvidence(manifestPath, "work-package", now)
	evidence := []model.Evidence{manifestEvidence}
	if err != nil || !exists {
		return evidence, false, err
	}
	result := verifyObservedWorkPackage(layout.RepositoryRoot, state.Objective.DeliveryID, state.WorkPackageFingerprint, nil)
	valid := result.Integrity == workpackage.Valid && result.Contract == workpackage.Valid
	if state.WorkPackage == model.WorkPackageApproved {
		valid = valid && result.Approval == workpackage.Valid
		if valid {
			verified, approvalErr := workpackage.ReadVerifiedApproval(layout.RepositoryRoot, state.Objective.DeliveryID, state.WorkPackageFingerprint)
			valid = approvalErr == nil && verified.Approval.Fingerprint == state.WorkPackageApprovalFingerprint
		}
	}
	return evidence, valid, nil
}

var verifyObservedWorkPackage = workpackage.Verify

type pendingJournalHeader struct {
	SchemaVersion     int    `json:"schema_version"`
	TransitionID      string `json:"transition_id"`
	TransitionClass   string `json:"transition_class"`
	ReconcilesProgram bool   `json:"reconciles_program"`
	Status            string `json:"status"`
	Reason            string `json:"reason"`
	Admission         struct {
		ID                         string                  `json:"id"`
		ExpectedProgramFingerprint string                  `json:"expected_program_fingerprint"`
		SourcePhase                model.ProtocolPhase     `json:"source_phase"`
		Invocation                 model.InvocationContext `json:"invocation"`
		Parameters                 protocol.Parameters     `json:"parameters"`
	} `json:"admission"`
	Mutations []struct {
		Path   string `json:"path"`
		Prior  []byte `json:"prior"`
		Target []byte `json:"target"`
	} `json:"mutations"`
}

type pendingJournalSet struct {
	Found              bool
	Conflicting        bool
	Evidence           []model.Evidence
	Recovery           model.RecoveryContext
	Transaction        model.TransactionContext
	TransactionState   model.TransactionState
	ProgramFingerprint string
	ReconcilesProgram  bool
}

type pendingJournalRecord struct {
	header pendingJournalHeader
	set    pendingJournalSet
}

func pendingJournalEvidence(root, ignoreAdmissionID string, now time.Time) (pendingJournalSet, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return pendingJournalSet{}, nil
		}
		return pendingJournalSet{}, err
	}
	recoveryAttempts := map[string]int{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".aborted" {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(root, entry.Name()))
		if readErr != nil {
			return pendingJournalSet{}, readErr
		}
		var header pendingJournalHeader
		if json.Unmarshal(raw, &header) != nil || header.SchemaVersion != protocol.JournalSchemaVersion || header.TransitionClass != string(catalog.EventRecovery) {
			continue
		}
		if transactionID, ok := header.Admission.Parameters.Get("transaction_id"); ok {
			recoveryAttempts[transactionID]++
		}
	}
	groups := map[string][]pendingJournalRecord{}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".pending" {
			path := filepath.Join(root, entry.Name())
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return pendingJournalSet{}, readErr
			}
			evidence := model.Evidence{Source: path, Fingerprint: hashBytes(raw), ObservedAt: now}
			var header pendingJournalHeader
			if err := json.Unmarshal(raw, &header); err != nil {
				return pendingJournalSet{}, err
			}
			class := catalog.EventClass(header.TransitionClass)
			if header.SchemaVersion != protocol.JournalSchemaVersion || header.Admission.ID == "" || len(header.Admission.ExpectedProgramFingerprint) != 64 || entry.Name() != header.Admission.ID+".pending" || header.TransitionID == "" || header.Status == "" || !class.Valid() || !class.Controllable() {
				return pendingJournalSet{}, fmt.Errorf("invalid pending transaction journal %s", path)
			}
			if header.Admission.ID == ignoreAdmissionID {
				continue
			}
			resourceDigests := make([]string, 0, len(header.Mutations))
			for _, mutation := range header.Mutations {
				resourceDigests = append(resourceDigests, hashBytes([]byte(mutation.Path+"\x00"+hashBytes(mutation.Prior)+"\x00"+hashBytes(mutation.Target))))
			}
			sort.Strings(resourceDigests)
			external := header.TransitionClass == "owned-external"
			transactionState := model.TransactionStaged
			switch header.Status {
			case "verifying":
				transactionState = model.TransactionVerifying
			case "recovery-required":
				if external {
					transactionState = model.TransactionExternalUncertain
				} else {
					transactionState = model.TransactionLocalApplied
				}
			}
			budget := 3 - recoveryAttempts[header.Admission.ID]
			if budget < 0 {
				budget = 0
			}
			permitted := recoveryContract(header.TransitionID, external, len(header.Mutations) > 0, budget)
			cause := header.Reason
			if cause == "" {
				cause = "process ended before transition receipt"
			}
			set := pendingJournalSet{
				Found: true, Evidence: []model.Evidence{evidence}, TransactionState: transactionState,
				ProgramFingerprint: header.Admission.ExpectedProgramFingerprint,
				ReconcilesProgram:  header.ReconcilesProgram,
				Recovery:           model.RecoveryContext{TransactionID: header.Admission.ID, Cause: cause, SourcePhase: header.Admission.SourcePhase, Permitted: permitted, BudgetRemaining: budget, Resumption: header.Admission.SourcePhase},
				Transaction:        model.TransactionContext{ID: header.Admission.ID, TransitionID: header.TransitionID, Status: header.Status, ResourceDigests: resourceDigests, ExternalPossible: external},
			}
			rootID := header.Admission.ID
			if class == catalog.EventRecovery {
				parent, ok := header.Admission.Parameters.Get("transaction_id")
				if !ok {
					return pendingJournalSet{}, fmt.Errorf("recovery journal %s has no interrupted transaction identity", path)
				}
				rootID = parent
			}
			groups[rootID] = append(groups[rootID], pendingJournalRecord{header: header, set: set})
		}
	}
	if len(groups) == 0 {
		return pendingJournalSet{}, nil
	}
	if len(groups) == 1 {
		for rootID, records := range groups {
			var root *pendingJournalRecord
			var attempts []pendingJournalRecord
			for index := range records {
				if catalog.EventClass(records[index].header.TransitionClass) == catalog.EventRecovery {
					attempts = append(attempts, records[index])
				} else if root != nil {
					return conflictingPending(records), nil
				} else {
					root = &records[index]
				}
			}
			if len(attempts) == 0 && root != nil {
				return root.set, nil
			}
			// An interrupted recovery attempt may have partially changed the
			// original resources. Preserve the group and permit only escalation;
			// a fresh recovery attempt closes every older journal in the group.
			base := attempts[0].set
			if root != nil {
				base = root.set
			}
			base.Evidence = nil
			base.Transaction.ResourceDigests = nil
			for _, record := range records {
				if record.set.ProgramFingerprint != base.ProgramFingerprint {
					return conflictingPending(records), nil
				}
				base.Evidence = append(base.Evidence, record.set.Evidence...)
				base.Transaction.ResourceDigests = append(base.Transaction.ResourceDigests, record.set.Transaction.ResourceDigests...)
			}
			sort.Strings(base.Transaction.ResourceDigests)
			budget := 3 - recoveryAttempts[rootID] - len(attempts)
			if budget < 0 {
				budget = 0
			}
			base.Found, base.Conflicting = true, false
			base.Recovery.TransactionID = rootID
			base.Recovery.Cause = "a recovery attempt was interrupted; the transaction group requires escalation"
			base.Recovery.Permitted = []string{"recovery.escalate"}
			base.Recovery.BudgetRemaining = budget
			base.Transaction.ID = rootID
			base.Transaction.Status = "nested-recovery-interrupted"
			base.TransactionState = model.TransactionLocalApplied
			if root != nil && root.set.Transaction.ExternalPossible {
				base.TransactionState = model.TransactionExternalUncertain
			}
			return base, nil
		}
	}
	result := pendingJournalSet{Conflicting: true}
	for _, records := range groups {
		for _, record := range records {
			result.Evidence = append(result.Evidence, record.set.Evidence...)
		}
	}
	return result, nil
}

func conflictingPending(records []pendingJournalRecord) pendingJournalSet {
	result := pendingJournalSet{Conflicting: true}
	for _, record := range records {
		result.Evidence = append(result.Evidence, record.set.Evidence...)
	}
	return result
}

func recoveryContract(transitionID string, external, staged bool, budget int) []string {
	if budget == 0 {
		return []string{"recovery.escalate"}
	}
	if external {
		return []string{"publication.reconcile", "recovery.escalate"}
	}
	switch transitionID {
	case "installation.reconcile-update":
		return []string{"recovery.rollback", "recovery.escalate"}
	case "runtime.hydrate", "runtime.replace", "installation.update", "installation.initialize":
		return []string{"runtime.reconcile", "recovery.rollback", "recovery.escalate"}
	case "configuration.initialize", "configuration.mutate":
		return []string{"configuration.reconcile", "recovery.rollback", "recovery.escalate"}
	case "workspace.cut":
		return []string{"workspace.reconcile", "recovery.escalate"}
	case "workspace.cleanup", "workspace.reap":
		return []string{"recovery.escalate"}
	default:
		if !staged {
			return []string{"recovery.rollback", "recovery.escalate"}
		}
		return []string{"recovery.resume", "recovery.rollback", "recovery.escalate"}
	}
}

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

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/durable"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
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
	state, stateEvidence, err := o.readState(layout.StatePath, current, now)
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
	configEvidence, configFingerprint, configExists, err := fileEvidence(layout.ConfigPath, "configuration", now)
	if err != nil {
		return model.Observation{}, err
	}
	configuration := state.Configuration
	configurationPolicy := model.Absent[model.ConfigurationPolicy]("no valid V2 configuration policy", configEvidence)
	if !configExists {
		configuration = model.ConfigurationUnsupported
	} else if configRaw, readErr := os.ReadFile(layout.ConfigPath); readErr != nil {
		return model.Observation{}, readErr
	} else if config, decodeErr := protocol.DecodeProjectConfig(configRaw); decodeErr != nil {
		configuration = model.ConfigurationConflicting
		configurationPolicy = model.Unknown[model.ConfigurationPolicy](model.FactConflicting, decodeErr.Error(), configEvidence)
	} else {
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
	if state.RuntimePath == "" {
		if runtimeState == model.RuntimeVerified {
			runtimeState = model.RuntimeInvalid
		}
	} else {
		evidence, fingerprint, exists, runtimeErr := fileEvidence(state.RuntimePath, "runtime", now)
		if runtimeErr != nil {
			return model.Observation{}, runtimeErr
		}
		runtimeEvidence = append(runtimeEvidence, evidence)
		if !exists {
			runtimeState = model.RuntimeAbsent
		} else if state.RuntimeFingerprint == "" || state.RuntimeFingerprint != fingerprint {
			runtimeState = model.RuntimeStale
		} else if state.RuntimePath != current.RuntimePath || state.RuntimeFingerprint != current.RuntimeFingerprint {
			runtimeState = model.RuntimeWrongSource
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
	phase := state.Phase
	delivery := state.Delivery
	recoveryFact := model.Fact[model.RecoveryState]{Status: model.FactKnown, Value: state.Recovery, Evidence: stateEvidence}
	transactionFact := model.Fact[model.TransactionState]{Status: model.FactKnown, Value: state.Transaction, Evidence: stateEvidence}
	recoveryInfoFact := model.Absent[model.RecoveryContext]("no recovery context", stateEvidence...)
	transactionInfoFact := model.Absent[model.TransactionContext]("no active transaction", stateEvidence...)
	terminal := artifactTerminal
	requiresCurrentImplementation := state.Goal.Kind == model.GoalVerified || state.Goal.Kind == model.GoalOpenPR
	currentDeliveryInvalid := verification != model.VerificationCurrent || configuration != model.ConfigurationVerified || runtimeState != model.RuntimeVerified
	if requiresCurrentImplementation && (terminal == model.TerminalStale || (terminal == model.TerminalEstablished && currentDeliveryInvalid)) {
		terminal, phase, delivery = model.TerminalStale, model.PhaseActive, model.DeliveryActive
		if runtimeState == model.RuntimeAbsent {
			phase = model.PhaseObserved
		}
	} else if state.Goal.Kind == model.GoalApprovedPlan && terminal == model.TerminalStale {
		phase, delivery = model.PhaseActive, model.DeliveryPlanning
	}
	if state.Goal.Kind == model.GoalMerged && state.Publication == model.PublicationMerged && state.Delivery == model.DeliveryTerminal &&
		(state.Workspace == model.WorkspaceLanded || state.Workspace == model.WorkspaceAbsent) {
		terminal, phase = model.TerminalEstablished, model.PhaseTerminal
	}
	if state.Goal.Kind == model.GoalAbandoned && state.Delivery == model.DeliveryDiscarded &&
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
	configurationEvidence := stateEvidence
	if configEvidence.Source != "" {
		configurationEvidence = append(append([]model.Evidence(nil), stateEvidence...), configEvidence)
	}
	goalFact := model.Absent[model.Goal]("no configured V2 goal", stateEvidence...)
	if state.Goal.Validate() == nil {
		goalFact = model.Fact[model.Goal]{Status: model.FactKnown, Value: state.Goal, Evidence: stateEvidence}
	}
	return model.Observation{
		SchemaVersion: model.SnapshotSchemaVersion, Invocation: current,
		Phase:               model.Fact[model.ProtocolPhase]{Status: model.FactKnown, Value: phase, Evidence: stateEvidence},
		Engagement:          model.Fact[model.EngagementState]{Status: model.FactKnown, Value: state.Engagement, Evidence: stateEvidence},
		Delivery:            model.Fact[model.DeliveryState]{Status: model.FactKnown, Value: delivery, Evidence: deliveryEvidence},
		Workspace:           model.Fact[model.WorkspaceState]{Status: model.FactKnown, Value: state.Workspace, Evidence: deliveryEvidence},
		Plan:                model.Fact[model.PlanState]{Status: model.FactKnown, Value: plan, Evidence: planEvidence},
		Configuration:       model.Fact[model.ConfigurationState]{Status: model.FactKnown, Value: configuration, Evidence: configurationEvidence},
		ConfigurationPolicy: configurationPolicy,
		Runtime:             model.Fact[model.RuntimeState]{Status: model.FactKnown, Value: runtimeState, Evidence: runtimeEvidence},
		Publication:         model.Fact[model.PublicationState]{Status: model.FactKnown, Value: state.Publication, Evidence: stateEvidence},
		Verification:        model.Fact[model.VerificationState]{Status: model.FactKnown, Value: verification, Evidence: verificationEvidence},
		Recovery:            recoveryFact,
		Transaction:         transactionFact,
		RecoveryInfo:        recoveryInfoFact,
		TransactionInfo:     transactionInfoFact,
		Terminal:            model.Fact[model.TerminalStatus]{Status: model.FactKnown, Value: terminal, Evidence: terminalEvidence},
		Goal:                goalFact,
		ObservedAt:          now,
	}, nil
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

func (o Observer) readState(path string, invocation model.InvocationContext, now time.Time) (durable.State, []model.Evidence, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			fingerprint := hashBytes([]byte("absent:" + path + ":" + invocation.RepositoryID + ":" + invocation.WorktreeID))
			evidence := []model.Evidence{{Source: path, Fingerprint: fingerprint, ObservedAt: now}}
			return durable.Default(invocation, now), evidence, nil
		}
		return durable.State{}, nil, fmt.Errorf("read durable state: %w", err)
	}
	state, err := durable.DecodeState(raw)
	if err != nil {
		return durable.State{}, nil, fmt.Errorf("decode durable state: %w", err)
	}
	return state, []model.Evidence{{Source: path, Fingerprint: hashBytes(raw), ObservedAt: now}}, nil
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
	for _, prefix := range []string{".boatstack/approvals/", ".boatstack/evidence/", ".boatstack/plans/", ".boatstack/publication/"} {
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
	SchemaVersion   int       `json:"schema_version"`
	DeliveryID      string    `json:"delivery_id"`
	PlanFingerprint string    `json:"plan_fingerprint"`
	Actor           string    `json:"actor"`
	AdmissionID     string    `json:"admission_id"`
	ApprovedAt      time.Time `json:"approved_at"`
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
	if state.Goal.Validate() != nil {
		return plan, verification, terminal, planEvidence, verificationEvidence, nil
	}
	deliveryID := state.Goal.DeliveryID
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
		evidence, _, exists, err := fileEvidence(path, "approval", now)
		if err != nil {
			return plan, verification, terminal, nil, nil, err
		}
		planEvidence = append(planEvidence, evidence)
		valid := exists
		if exists {
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return plan, verification, terminal, nil, nil, readErr
			}
			var approval observedApproval
			valid = decodeStrictJSON(raw, &approval) == nil && approval.SchemaVersion == 1 &&
				approval.DeliveryID == deliveryID && approval.PlanFingerprint == state.PlanFingerprint &&
				approval.Actor != "" && approval.AdmissionID != "" && !approval.ApprovedAt.IsZero()
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
			verificationEvidence = append(verificationEvidence, evidence)
			if !exists || fingerprint != gate.Fingerprint || gate.Revision == "" {
				verification, terminal = model.VerificationStale, model.TerminalStale
			}
			continue
		}
		transitionID, known := gateNames[gate.Gate]
		path := filepath.Join(layout.EvidenceRoot, deliveryID, gate.Gate+".json")
		evidence, _, exists, err := fileEvidence(path, "gate-evidence", now)
		if err != nil {
			return plan, verification, terminal, nil, nil, err
		}
		verificationEvidence = append(verificationEvidence, evidence)
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
		if !valid {
			verification, terminal = model.VerificationStale, model.TerminalStale
		}
	}
	if terminal == model.TerminalEstablished && state.Goal.Kind == model.GoalVerified && state.VisualEvidencePolicy == "required" && !hasVisual {
		verification, terminal = model.VerificationUnresolved, model.TerminalStale
	}
	return plan, verification, terminal, planEvidence, verificationEvidence, nil
}

type pendingJournalHeader struct {
	SchemaVersion   int    `json:"schema_version"`
	TransitionID    string `json:"transition_id"`
	TransitionClass string `json:"transition_class"`
	Status          string `json:"status"`
	Reason          string `json:"reason"`
	Admission       struct {
		ID          string                  `json:"id"`
		SourcePhase model.ProtocolPhase     `json:"source_phase"`
		Invocation  model.InvocationContext `json:"invocation"`
		Parameters  protocol.Parameters     `json:"parameters"`
	} `json:"admission"`
	Mutations []struct {
		Path   string `json:"path"`
		Prior  []byte `json:"prior"`
		Target []byte `json:"target"`
	} `json:"mutations"`
}

type pendingJournalSet struct {
	Found            bool
	Conflicting      bool
	Evidence         []model.Evidence
	Recovery         model.RecoveryContext
	Transaction      model.TransactionContext
	TransactionState model.TransactionState
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
		if json.Unmarshal(raw, &header) != nil || header.SchemaVersion != 2 || header.TransitionClass != string(catalog.EventRecovery) {
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
			if header.SchemaVersion != 2 || header.Admission.ID == "" || entry.Name() != header.Admission.ID+".pending" || header.TransitionID == "" || header.Status == "" || !class.Valid() || !class.Controllable() {
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
			permitted := recoveryContract(header.TransitionID, external, budget)
			cause := header.Reason
			if cause == "" {
				cause = "process ended before transition receipt"
			}
			set := pendingJournalSet{
				Found: true, Evidence: []model.Evidence{evidence}, TransactionState: transactionState,
				Recovery:    model.RecoveryContext{TransactionID: header.Admission.ID, Cause: cause, SourcePhase: header.Admission.SourcePhase, Permitted: permitted, BudgetRemaining: budget, Resumption: header.Admission.SourcePhase},
				Transaction: model.TransactionContext{ID: header.Admission.ID, TransitionID: header.TransitionID, Status: header.Status, ResourceDigests: resourceDigests, ExternalPossible: external},
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

func recoveryContract(transitionID string, external bool, budget int) []string {
	if budget == 0 {
		return []string{"recovery.escalate"}
	}
	if external {
		return []string{"publication.reconcile", "recovery.escalate"}
	}
	switch transitionID {
	case "runtime.hydrate", "runtime.replace", "installation.update", "installation.initialize":
		return []string{"runtime.reconcile", "recovery.rollback", "recovery.escalate"}
	case "configuration.initialize", "configuration.mutate":
		return []string{"configuration.reconcile", "recovery.rollback", "recovery.escalate"}
	case "workspace.cut":
		return []string{"workspace.reconcile", "recovery.escalate"}
	case "workspace.cleanup", "workspace.reap":
		return []string{"recovery.escalate"}
	default:
		return []string{"recovery.resume", "recovery.rollback", "recovery.escalate"}
	}
}

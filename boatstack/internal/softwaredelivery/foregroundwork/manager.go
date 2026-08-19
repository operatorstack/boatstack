package foregroundwork

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
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const RecordSchemaVersion = 4

type Status string

const (
	StatusRequested     Status = "requested"
	StatusInputRequired Status = "input-required"
	StatusCompleted     Status = "completed"
	StatusBlocked       Status = "blocked"
	StatusInvalidated   Status = "invalidated"
)

type InputBinding struct {
	ID          string                         `json:"id"`
	Value       string                         `json:"value"`
	Fingerprint string                         `json:"fingerprint"`
	WorkOutput  *protocol.WorkOutputProvenance `json:"work_output,omitempty"`
}

type Question struct {
	ID        string          `json:"id"`
	Prompt    string          `json:"prompt"`
	Schema    json.RawMessage `json:"schema,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type Answer struct {
	QuestionID  string          `json:"question_id"`
	Value       json.RawMessage `json:"value"`
	Fingerprint string          `json:"fingerprint"`
	AnsweredAt  time.Time       `json:"answered_at"`
}

type Event struct {
	Kind        string    `json:"kind"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	At          time.Time `json:"at"`
}

type Request struct {
	ID                 string               `json:"id"`
	Fingerprint        string               `json:"fingerprint"`
	RunID              string               `json:"run_id"`
	ProgramID          string               `json:"program_id"`
	EntryID            string               `json:"entry_id"`
	Objective          model.Objective      `json:"objective"`
	TransitionID       catalog.TransitionID `json:"transition_id"`
	Contract           catalog.WorkContract `json:"contract"`
	Inputs             []InputBinding       `json:"inputs,omitempty"`
	RepositoryID       string               `json:"repository_id"`
	GitCommonID        string               `json:"git_common_id"`
	WorktreeID         string               `json:"worktree_id"`
	Ref                string               `json:"ref"`
	ProgramFingerprint string               `json:"program_fingerprint"`
	ContextFingerprint string               `json:"context_fingerprint"`
	StateRevision      uint64               `json:"state_revision"`
	InstructionContent string               `json:"instruction_content"`
	StagingRoot        string               `json:"staging_root"`
	CreatedAt          time.Time            `json:"created_at"`
}

type Record struct {
	SchemaVersion int                    `json:"schema_version"`
	Revision      uint64                 `json:"revision"`
	Status        Status                 `json:"status"`
	Request       Request                `json:"request"`
	Question      *Question              `json:"question,omitempty"`
	Answers       []Answer               `json:"answers,omitempty"`
	Result        *protocol.WorkEvidence `json:"result,omitempty"`
	BlockReason   string                 `json:"block_reason,omitempty"`
	Events        []Event                `json:"events"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

type Manager struct {
	resolver ports.InvocationResolver
	locker   ports.Locker
	clock    ports.Clock
	store    ports.RuntimeStore
}

// LoadRecord reads one exact runtime-owned foreground-work record for generic
// invocation materialization. It does not mutate or advance the work state.
func LoadRecord(layout ports.ControllerLayout, runID, workID string) (Record, error) {
	if !segment(runID) || !segment(workID) {
		return Record{}, fmt.Errorf("foreground work requires semantic run and work identities")
	}
	return load(recordPath(layout, runID, workID))
}

func NewManager(resolver ports.InvocationResolver, locker ports.Locker, clock ports.Clock, store ports.RuntimeStore) (Manager, error) {
	if resolver == nil || locker == nil || clock == nil || store == nil {
		return Manager{}, fmt.Errorf("foreground work manager requires resolver, locker, clock, and runtime store")
	}
	return Manager{resolver: resolver, locker: locker, clock: clock, store: store}, nil
}

func (m Manager) Ensure(ctx context.Context, invocation model.InvocationContext, runID, programID, entryID string, objective model.Objective, snapshot model.Snapshot, transition catalog.Transition, values map[string]protocol.WorkInputValue) (Record, error) {
	if transition.Work == nil || runID == "" || programID == "" || entryID == "" {
		return Record{}, fmt.Errorf("foreground work request requires run, program, entry, and contract")
	}
	return m.mutate(ctx, invocation, runID, transition.Work.ID, func(current *Record, layout ports.ControllerLayout) (Record, error) {
		request, err := newRequest(m.store, layout, m.clock.Now(), runID, programID, entryID, objective, snapshot, transition, values)
		if err != nil {
			return Record{}, err
		}
		if current != nil && current.Request.Fingerprint == request.Fingerprint {
			return *current, nil
		}
		now := m.clock.Now().UTC()
		events := []Event{{Kind: "work.requested", Fingerprint: request.Fingerprint, At: now}}
		if current != nil {
			events = append(append([]Event(nil), current.Events...), Event{Kind: "work.invalidated", Fingerprint: current.Request.Fingerprint, At: now}, events[0])
		}
		return Record{SchemaVersion: RecordSchemaVersion, Revision: nextRevision(current), Status: StatusRequested, Request: request, Events: events, UpdatedAt: now}, nil
	})
}

func (m Manager) Show(ctx context.Context, invocation model.InvocationContext, runID, workID string) (Record, error) {
	layout, _, err := m.resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		return Record{}, err
	}
	return load(recordPath(layout, runID, workID))
}

func (m Manager) InputRequired(ctx context.Context, invocation model.InvocationContext, runID, workID, prompt string, schema json.RawMessage) (Record, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || len(prompt) > 64<<10 {
		return Record{}, fmt.Errorf("foreground work question requires a bounded prompt")
	}
	if len(schema) != 0 {
		if err := validateJSONSchema(schema); err != nil {
			return Record{}, fmt.Errorf("foreground work question schema: %w", err)
		}
	}
	return m.update(ctx, invocation, runID, workID, func(record *Record) error {
		if record.Status != StatusRequested && record.Status != StatusInputRequired {
			return fmt.Errorf("foreground work question is not admissible from status %q", record.Status)
		}
		identity, err := general.Fingerprint(struct{ Request, Prompt string }{record.Request.Fingerprint, prompt})
		if err != nil {
			return err
		}
		now := m.clock.Now().UTC()
		record.Status, record.Question = StatusInputRequired, &Question{ID: "question-" + identity[:24], Prompt: prompt, Schema: append(json.RawMessage(nil), schema...), CreatedAt: now}
		record.Events = append(record.Events, Event{Kind: "work.input-required", Fingerprint: identity, At: now})
		return nil
	})
}

func (m Manager) Answer(ctx context.Context, invocation model.InvocationContext, runID, workID, questionID string, value json.RawMessage) (Record, error) {
	if len(value) == 0 || len(value) > 1<<20 || !json.Valid(value) {
		return Record{}, fmt.Errorf("foreground work answer must be bounded JSON")
	}
	canonical, err := normalizedJSON(value)
	if err != nil {
		return Record{}, fmt.Errorf("foreground work answer must be bounded JSON: %w", err)
	}
	return m.update(ctx, invocation, runID, workID, func(record *Record) error {
		if record.Status != StatusInputRequired || record.Question == nil || record.Question.ID != questionID {
			return fmt.Errorf("foreground work answer does not match the current question")
		}
		if len(record.Question.Schema) != 0 {
			if err := validateJSON(record.Question.Schema, canonical); err != nil {
				return fmt.Errorf("foreground work answer: %w", err)
			}
		}
		fingerprint := digest(canonical)
		now := m.clock.Now().UTC()
		record.Answers = append(record.Answers, Answer{QuestionID: questionID, Value: canonical, Fingerprint: fingerprint, AnsweredAt: now})
		record.Status, record.Question = StatusRequested, nil
		record.Events = append(record.Events, Event{Kind: "work.answered", Fingerprint: fingerprint, At: now})
		return nil
	})
}

func (m Manager) Complete(ctx context.Context, invocation model.InvocationContext, runID, workID string) (Record, error) {
	return m.update(ctx, invocation, runID, workID, func(record *Record) error {
		if record.Status != StatusRequested {
			return fmt.Errorf("foreground work completion is not admissible from status %q", record.Status)
		}
		outputs, err := verifyOutputs(record.Request)
		if err != nil {
			return err
		}
		inputs := make([]protocol.WorkInputEvidence, 0, len(record.Request.Inputs))
		for _, input := range record.Request.Inputs {
			inputs = append(inputs, protocol.WorkInputEvidence{ID: input.ID, Fingerprint: input.Fingerprint, WorkOutput: input.WorkOutput})
		}
		evidence, err := protocol.SealWorkEvidence(protocol.WorkEvidence{
			SchemaVersion: protocol.WorkEvidenceSchemaVersion, RequestID: record.Request.ID, RequestFingerprint: record.Request.Fingerprint,
			RunID: record.Request.RunID, ProgramID: record.Request.ProgramID, EntryID: record.Request.EntryID,
			ContractID: record.Request.Contract.ID, ContractFingerprint: record.Request.Contract.Fingerprint, TransitionID: record.Request.TransitionID,
			ProgramFingerprint: record.Request.ProgramFingerprint, ContextFingerprint: record.Request.ContextFingerprint, StateRevision: record.Request.StateRevision,
			RepositoryID: record.Request.RepositoryID, WorktreeID: record.Request.WorktreeID, Inputs: inputs, Outputs: outputs,
		})
		if err != nil {
			return err
		}
		now := m.clock.Now().UTC()
		record.Status, record.Result = StatusCompleted, &evidence
		record.Events = append(record.Events, Event{Kind: "work.completed", Fingerprint: evidence.ResultFingerprint, At: now})
		return nil
	})
}

func (m Manager) Block(ctx context.Context, invocation model.InvocationContext, runID, workID, reason string) (Record, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 64<<10 {
		return Record{}, fmt.Errorf("foreground work blocker requires a bounded reason")
	}
	return m.update(ctx, invocation, runID, workID, func(record *Record) error {
		if record.Status == StatusCompleted || record.Status == StatusInvalidated {
			return fmt.Errorf("foreground work blocker is not admissible from status %q", record.Status)
		}
		now := m.clock.Now().UTC()
		record.Status, record.BlockReason, record.Question = StatusBlocked, reason, nil
		record.Events = append(record.Events, Event{Kind: "work.blocked", Fingerprint: digest([]byte(reason)), At: now})
		return nil
	})
}

func (m Manager) update(ctx context.Context, invocation model.InvocationContext, runID, workID string, change func(*Record) error) (Record, error) {
	return m.mutate(ctx, invocation, runID, workID, func(current *Record, _ ports.ControllerLayout) (Record, error) {
		if current == nil {
			return Record{}, fmt.Errorf("foreground work request does not exist")
		}
		result := *current
		result.Answers, result.Events = append([]Answer(nil), current.Answers...), append([]Event(nil), current.Events...)
		if current.Question != nil {
			question := *current.Question
			question.Schema = append(json.RawMessage(nil), current.Question.Schema...)
			result.Question = &question
		}
		if err := change(&result); err != nil {
			return Record{}, err
		}
		result.Revision, result.UpdatedAt = current.Revision+1, m.clock.Now().UTC()
		return result, nil
	})
}

func (m Manager) mutate(ctx context.Context, invocation model.InvocationContext, runID, workID string, change func(*Record, ports.ControllerLayout) (Record, error)) (Record, error) {
	if !segment(runID) || !segment(workID) {
		return Record{}, fmt.Errorf("foreground work requires semantic run and work identities")
	}
	lockName := "foreground-work-" + workID
	lock, err := m.locker.Acquire(ctx, invocation, []string{lockName})
	if err != nil {
		return Record{}, err
	}
	defer lock.Release()
	layout, _, err := m.resolver.ResolveLayout(ctx, invocation)
	if err != nil {
		return Record{}, err
	}
	path := recordPath(layout, runID, workID)
	current, err := load(path)
	if err != nil && !os.IsNotExist(err) {
		return Record{}, err
	}
	var prior *Record
	if err == nil {
		prior = &current
	}
	next, err := change(prior, layout)
	if err != nil {
		return Record{}, err
	}
	if err := save(m.store, path, next); err != nil {
		return Record{}, err
	}
	return next, nil
}

func newRequest(store ports.RuntimeStore, layout ports.ControllerLayout, now time.Time, runID, programID, entryID string, objective model.Objective, snapshot model.Snapshot, transition catalog.Transition, values map[string]protocol.WorkInputValue) (Request, error) {
	work := *transition.Work
	bindings := make([]InputBinding, 0, len(work.Inputs))
	for _, input := range work.Inputs {
		value, ok := values[work.ID+"/"+input.ID]
		if !ok || value.Validate() != nil {
			return Request{}, fmt.Errorf("foreground work input %q is not bound", input.ID)
		}
		switch input.Producer.Kind {
		case controlprogram.ParameterSourceEntryInput:
			if value.WorkOutput != nil {
				return Request{}, fmt.Errorf("foreground work entry input %q invents work-output provenance", input.ID)
			}
		case controlprogram.ParameterSourceWorkOutput:
			if value.WorkOutput == nil || value.WorkOutput.WorkID != input.Producer.Work || value.WorkOutput.OutputID != input.Producer.Output {
				return Request{}, fmt.Errorf("foreground work input %q does not match its declared producer", input.ID)
			}
		default:
			return Request{}, fmt.Errorf("foreground work input %q has an unsupported producer", input.ID)
		}
		bindings = append(bindings, InputBinding{ID: input.ID, Value: value.Value, Fingerprint: value.Fingerprint, WorkOutput: value.WorkOutput})
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].ID < bindings[j].ID })
	contextFingerprint, err := model.ForegroundWorkContextFingerprint(snapshot)
	if err != nil {
		return Request{}, fmt.Errorf("foreground work context fingerprint: %w", err)
	}
	request := Request{
		RunID: runID, ProgramID: programID, EntryID: entryID, Objective: objective, TransitionID: transition.ID, Contract: work, Inputs: bindings,
		RepositoryID: snapshot.Invocation.RepositoryID, GitCommonID: snapshot.Invocation.GitCommonID, WorktreeID: snapshot.Invocation.WorktreeID, Ref: snapshot.Invocation.Ref,
		ProgramFingerprint: snapshot.ProgramFingerprint, ContextFingerprint: contextFingerprint, StateRevision: snapshot.StateRevision,
		InstructionContent: work.InstructionContent, CreatedAt: now.UTC(),
	}
	identity := request
	identity.ID, identity.Fingerprint, identity.StagingRoot, identity.CreatedAt = "", "", "", time.Time{}
	fingerprint, err := general.Fingerprint(identity)
	if err != nil {
		return Request{}, err
	}
	request.Fingerprint, request.ID = fingerprint, "work-"+fingerprint[:24]
	staging := filepath.Join(layout.FlowRoot, "work", runID, work.ID, "requests", fingerprint, "staging")
	request.StagingRoot = staging
	if err := store.EnsureDirectory(staging, 0o700); err != nil {
		return Request{}, err
	}
	return request, nil
}

func verifyOutputs(request Request) ([]protocol.WorkOutputEvidence, error) {
	var result []protocol.WorkOutputEvidence
	for _, output := range request.Contract.Outputs {
		path, err := safeStagingFile(request.StagingRoot, output.Path)
		if os.IsNotExist(err) && !output.Required {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("foreground work output %q: %w", output.ID, err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("foreground work output %q: %w", output.ID, err)
		}
		if int64(len(raw)) > output.MaxBytes || !utf8.Valid(raw) {
			return nil, fmt.Errorf("foreground work output %q exceeds its bound or is not UTF-8", output.ID)
		}
		if output.MediaType == "application/json" {
			if !json.Valid(raw) {
				return nil, fmt.Errorf("foreground work output %q is not JSON", output.ID)
			}
			if output.SchemaContent != "" {
				if err := validateJSON([]byte(output.SchemaContent), raw); err != nil {
					return nil, fmt.Errorf("foreground work output %q: %w", output.ID, err)
				}
			}
		}
		result = append(result, protocol.WorkOutputEvidence{ID: output.ID, Path: output.Path, MediaType: output.MediaType, SHA256: digest(raw), Size: int64(len(raw)), Content: string(raw)})
	}
	return protocol.CanonicalWorkOutputs(result), nil
}

func validateJSONSchema(raw []byte) error {
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("schema.json", document); err != nil {
		return err
	}
	_, err := compiler.Compile("schema.json")
	return err
}

func validateJSON(schemaRaw, valueRaw []byte) error {
	var document any
	if err := json.Unmarshal(schemaRaw, &document); err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("schema.json", document); err != nil {
		return err
	}
	schema, err := compiler.Compile("schema.json")
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(valueRaw, &value); err != nil {
		return err
	}
	return schema.Validate(value)
}

func safeStagingFile(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe output path")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, relative)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("output is not a regular file")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, filepath.Join(parent, filepath.Base(path)))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output escapes staging root")
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

func recordPath(layout ports.ControllerLayout, runID, workID string) string {
	return filepath.Join(layout.FlowRoot, "work", runID, workID, "record.json")
}

func load(path string) (Record, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	var record Record
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return Record{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Record{}, fmt.Errorf("foreground work record has trailing input")
	}
	if record.SchemaVersion != RecordSchemaVersion || record.Revision == 0 || record.Request.Fingerprint == "" || record.UpdatedAt.IsZero() {
		return Record{}, fmt.Errorf("foreground work record has invalid identity")
	}
	if !validStatus(record) {
		return Record{}, fmt.Errorf("foreground work record has invalid status state")
	}
	identity := record.Request
	wantID, wantFingerprint := identity.ID, identity.Fingerprint
	identity.ID, identity.Fingerprint, identity.StagingRoot, identity.CreatedAt = "", "", "", time.Time{}
	fingerprint, err := general.Fingerprint(identity)
	expectedStaging := filepath.Join(filepath.Dir(path), "requests", fingerprint, "staging")
	if err != nil || fingerprint != wantFingerprint || wantID != "work-"+fingerprint[:24] || record.Request.StagingRoot != expectedStaging {
		return Record{}, fmt.Errorf("foreground work request fingerprint is invalid")
	}
	if record.Result != nil {
		if err := record.Result.Validate(); err != nil || record.Result.RequestID != record.Request.ID || record.Result.RequestFingerprint != record.Request.Fingerprint ||
			record.Result.RunID != record.Request.RunID || record.Result.ProgramID != record.Request.ProgramID || record.Result.EntryID != record.Request.EntryID {
			return Record{}, fmt.Errorf("foreground work result is invalid: %v", err)
		}
	}
	for _, answer := range record.Answers {
		canonical, err := normalizedJSON(answer.Value)
		if !segment(answer.QuestionID) || err != nil || answer.Fingerprint != digest(canonical) || answer.AnsweredAt.IsZero() {
			return Record{}, fmt.Errorf("foreground work answer evidence is invalid")
		}
	}
	for _, event := range record.Events {
		if !validEventKind(event.Kind) || event.At.IsZero() {
			return Record{}, fmt.Errorf("foreground work event is invalid")
		}
	}
	return record, nil
}

func normalizedJSON(raw []byte) (json.RawMessage, error) {
	var normalized bytes.Buffer
	if err := json.Compact(&normalized, raw); err != nil {
		return nil, err
	}
	return json.RawMessage(append([]byte(nil), normalized.Bytes()...)), nil
}

func validStatus(record Record) bool {
	switch record.Status {
	case StatusRequested, StatusInvalidated:
		return record.Question == nil && record.Result == nil && record.BlockReason == ""
	case StatusInputRequired:
		return record.Question != nil && segment(record.Question.ID) && strings.TrimSpace(record.Question.Prompt) != "" && record.Result == nil && record.BlockReason == "" && record.Question.CreatedAt.IsZero() == false
	case StatusCompleted:
		return record.Question == nil && record.Result != nil && record.BlockReason == ""
	case StatusBlocked:
		return record.Question == nil && record.Result == nil && strings.TrimSpace(record.BlockReason) != ""
	default:
		return false
	}
}

func validEventKind(kind string) bool {
	switch kind {
	case "work.requested", "work.invalidated", "work.input-required", "work.answered", "work.completed", "work.blocked":
		return true
	default:
		return false
	}
}

func save(store ports.RuntimeStore, path string, record Record) error {
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return store.WriteAtomic(path, raw, 0o600)
}

func nextRevision(current *Record) uint64 {
	if current == nil {
		return 1
	}
	return current.Revision + 1
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func segment(value string) bool {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\\`) {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' && char != '.' {
			return false
		}
	}
	return true
}

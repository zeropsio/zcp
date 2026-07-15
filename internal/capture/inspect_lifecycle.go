package capture

import (
	"errors"
	"fmt"
	"sort"
)

type lifecycleInspectionData struct {
	status      string
	complete    bool
	recordCount int
	evalRuns    []EvalRunInspection
	warnings    []string
}

type lifecycleRunBuilder struct {
	status        string
	closed        bool
	scenarios     map[string]*lifecycleScenarioBuilder
	scenarioOrder []string
}

type lifecycleScenarioBuilder struct {
	status          string
	closed          bool
	artifacts       []string
	invocations     map[string]*EvalInvocationInspection
	invocationOrder []string
}

type lifecycleHierarchy struct {
	runs     map[string]*lifecycleRunBuilder
	runOrder []string
}

func inspectLifecycleFile(path, relative, expectedCaptureID string, providerExchanges []inspectedProviderExchange) (*lifecycleInspectionData, error) {
	records, err := ReadLifecycleRecords(path)
	if err != nil {
		return nil, err
	}
	terminal, err := validateLifecycleRecordEnvelope(records, expectedCaptureID)
	if err != nil {
		return nil, err
	}
	hierarchy := lifecycleHierarchy{runs: make(map[string]*lifecycleRunBuilder)}
	for _, record := range records[1 : len(records)-1] {
		evidence := RawEvidence{File: relative, SeqStart: record.Seq, SeqEnd: record.Seq, ObservedAt: record.Time}
		if err := hierarchy.apply(record, evidence); err != nil {
			return nil, err
		}
	}
	return hierarchy.project(terminal.Status, len(records), providerExchanges), nil
}

func validateLifecycleRecordEnvelope(records []LifecycleRecord, expectedCaptureID string) (LifecycleRecord, error) {
	if len(records) == 0 {
		return LifecycleRecord{}, errors.New("lifecycle record file is empty")
	}
	captureID := expectedCaptureID
	if captureID == "" {
		captureID = records[0].CaptureID
	}
	if captureID == "" {
		return LifecycleRecord{}, fmt.Errorf("%w: lifecycle capture ID is empty", errInspectionIdentityMismatch)
	}
	for index, record := range records {
		wantSeq := uint64(index + 1)
		if record.Seq != wantSeq {
			return LifecycleRecord{}, fmt.Errorf("lifecycle sequence discontinuity: got %d, want %d", record.Seq, wantSeq)
		}
		if record.CaptureID != captureID {
			return LifecycleRecord{}, fmt.Errorf("%w: lifecycle seq %d capture ID %q differs from %q", errInspectionIdentityMismatch, record.Seq, record.CaptureID, captureID)
		}
		if err := validateLifecycleMarker(record.LifecycleMarker); err != nil {
			return LifecycleRecord{}, fmt.Errorf("lifecycle seq %d: %w", record.Seq, err)
		}
	}
	if records[0].Kind != LifecycleStreamStart {
		return LifecycleRecord{}, fmt.Errorf("lifecycle first record = %q, want %q", records[0].Kind, LifecycleStreamStart)
	}
	terminal := records[len(records)-1]
	if terminal.Kind != LifecycleStreamEnd {
		return LifecycleRecord{}, fmt.Errorf("lifecycle terminal record = %q, want %q", terminal.Kind, LifecycleStreamEnd)
	}
	return terminal, nil
}

func (hierarchy *lifecycleHierarchy) apply(record LifecycleRecord, evidence RawEvidence) error {
	switch record.Kind {
	case LifecycleEvalRunStart:
		return hierarchy.startRun(record)
	case LifecycleEvalRunEnd:
		return hierarchy.endRun(record)
	case LifecycleScenarioStart:
		return hierarchy.startScenario(record)
	case LifecycleScenarioEnd:
		return hierarchy.endScenario(record)
	case LifecycleInvocationStart:
		return hierarchy.startInvocation(record, evidence)
	case LifecycleInvocationBind:
		return hierarchy.bindInvocation(record, evidence)
	case LifecycleInvocationEnd:
		return hierarchy.endInvocation(record, evidence)
	case LifecycleArtifact:
		return hierarchy.addArtifact(record)
	default:
		return fmt.Errorf("unexpected lifecycle marker %q", record.Kind)
	}
}

func (hierarchy *lifecycleHierarchy) startRun(record LifecycleRecord) error {
	if _, exists := hierarchy.runs[record.EvalRunID]; exists {
		return fmt.Errorf("duplicate eval run start %q", record.EvalRunID)
	}
	hierarchy.runs[record.EvalRunID] = &lifecycleRunBuilder{scenarios: make(map[string]*lifecycleScenarioBuilder)}
	hierarchy.runOrder = append(hierarchy.runOrder, record.EvalRunID)
	return nil
}

func (hierarchy *lifecycleHierarchy) endRun(record LifecycleRecord) error {
	run, ok := hierarchy.runs[record.EvalRunID]
	if !ok {
		return fmt.Errorf("eval run end %q has no start", record.EvalRunID)
	}
	if run.closed {
		return fmt.Errorf("duplicate eval run end %q", record.EvalRunID)
	}
	for _, scenarioRunID := range run.scenarioOrder {
		if !run.scenarios[scenarioRunID].closed {
			return fmt.Errorf("eval run %q ended before scenario %q", record.EvalRunID, scenarioRunID)
		}
	}
	run.status = record.Status
	run.closed = true
	return nil
}

func (hierarchy *lifecycleHierarchy) startScenario(record LifecycleRecord) error {
	run, ok := hierarchy.runs[record.EvalRunID]
	if !ok {
		return fmt.Errorf("scenario %q references unknown eval run %q", record.ScenarioRunID, record.EvalRunID)
	}
	if run.closed {
		return fmt.Errorf("scenario %q starts after eval run %q ended", record.ScenarioRunID, record.EvalRunID)
	}
	if _, exists := run.scenarios[record.ScenarioRunID]; exists {
		return fmt.Errorf("duplicate scenario start %q", record.ScenarioRunID)
	}
	run.scenarios[record.ScenarioRunID] = &lifecycleScenarioBuilder{invocations: make(map[string]*EvalInvocationInspection)}
	run.scenarioOrder = append(run.scenarioOrder, record.ScenarioRunID)
	return nil
}

func (hierarchy *lifecycleHierarchy) endScenario(record LifecycleRecord) error {
	scenario, err := hierarchy.openScenario(record.EvalRunID, record.ScenarioRunID)
	if err != nil {
		return err
	}
	for _, invocationID := range scenario.invocationOrder {
		if scenario.invocations[invocationID].EndSource.SeqStart == 0 {
			return fmt.Errorf("scenario %q ended before invocation %q", record.ScenarioRunID, invocationID)
		}
	}
	scenario.status = record.Status
	scenario.closed = true
	return nil
}

func (hierarchy *lifecycleHierarchy) startInvocation(record LifecycleRecord, evidence RawEvidence) error {
	scenario, err := hierarchy.openScenario(record.EvalRunID, record.ScenarioRunID)
	if err != nil {
		return err
	}
	if _, exists := scenario.invocations[record.InvocationID]; exists {
		return fmt.Errorf("duplicate invocation start %q", record.InvocationID)
	}
	scenario.invocations[record.InvocationID] = &EvalInvocationInspection{
		InvocationID: record.InvocationID,
		Phase:        record.Phase,
		StartedAt:    record.Time,
		StartSource:  evidence,
	}
	scenario.invocationOrder = append(scenario.invocationOrder, record.InvocationID)
	return nil
}

func (hierarchy *lifecycleHierarchy) bindInvocation(record LifecycleRecord, evidence RawEvidence) error {
	invocation, err := hierarchy.openInvocation(record.EvalRunID, record.ScenarioRunID, record.InvocationID)
	if err != nil {
		return err
	}
	if invocation.Phase != record.Phase {
		return fmt.Errorf("invocation %q bind phase %q differs from start %q", record.InvocationID, record.Phase, invocation.Phase)
	}
	if invocation.BindSource.SeqStart != 0 {
		return fmt.Errorf("duplicate invocation bind %q", record.InvocationID)
	}
	invocation.ClaudeSessionID = record.ClaudeSessionID
	invocation.BindSource = evidence
	return nil
}

func (hierarchy *lifecycleHierarchy) endInvocation(record LifecycleRecord, evidence RawEvidence) error {
	invocation, err := hierarchy.openInvocation(record.EvalRunID, record.ScenarioRunID, record.InvocationID)
	if err != nil {
		return err
	}
	if invocation.Phase != record.Phase {
		return fmt.Errorf("invocation %q end phase %q differs from start %q", record.InvocationID, record.Phase, invocation.Phase)
	}
	invocation.Status = record.Status
	invocation.EndedAt = record.Time
	invocation.EndSource = evidence
	return nil
}

func (hierarchy *lifecycleHierarchy) addArtifact(record LifecycleRecord) error {
	scenario, err := hierarchy.openScenario(record.EvalRunID, record.ScenarioRunID)
	if err != nil {
		return err
	}
	scenario.artifacts = append(scenario.artifacts, record.ArtifactPath)
	return nil
}

func (hierarchy *lifecycleHierarchy) openScenario(evalRunID, scenarioRunID string) (*lifecycleScenarioBuilder, error) {
	run, ok := hierarchy.runs[evalRunID]
	if !ok {
		return nil, fmt.Errorf("unknown eval run %q", evalRunID)
	}
	if run.closed {
		return nil, fmt.Errorf("eval run %q is already closed", evalRunID)
	}
	scenario, ok := run.scenarios[scenarioRunID]
	if !ok {
		return nil, fmt.Errorf("unknown eval scenario %q in run %q", scenarioRunID, evalRunID)
	}
	if scenario.closed {
		return nil, fmt.Errorf("eval scenario %q in run %q is already closed", scenarioRunID, evalRunID)
	}
	return scenario, nil
}

func (hierarchy *lifecycleHierarchy) openInvocation(evalRunID, scenarioRunID, invocationID string) (*EvalInvocationInspection, error) {
	scenario, err := hierarchy.openScenario(evalRunID, scenarioRunID)
	if err != nil {
		return nil, err
	}
	invocation, ok := scenario.invocations[invocationID]
	if !ok {
		return nil, fmt.Errorf("unknown eval invocation %q", invocationID)
	}
	if invocation.EndSource.SeqStart != 0 {
		return nil, fmt.Errorf("eval invocation %q is already closed", invocationID)
	}
	return invocation, nil
}

func (hierarchy *lifecycleHierarchy) project(status string, recordCount int, providerExchanges []inspectedProviderExchange) *lifecycleInspectionData {
	data := &lifecycleInspectionData{status: status, complete: true, recordCount: recordCount}
	for _, evalRunID := range hierarchy.runOrder {
		runBuilder := hierarchy.runs[evalRunID]
		run := EvalRunInspection{EvalRunID: evalRunID, Status: runBuilder.status}
		if run.Status == "" {
			data.complete = false
			data.warnings = append(data.warnings, fmt.Sprintf("eval run %s has no terminal marker", evalRunID))
		}
		for _, scenarioRunID := range runBuilder.scenarioOrder {
			scenarioBuilder := runBuilder.scenarios[scenarioRunID]
			scenario := EvalScenarioInspection{ScenarioRunID: scenarioRunID, Status: scenarioBuilder.status, Artifacts: append([]string(nil), scenarioBuilder.artifacts...)}
			sort.Strings(scenario.Artifacts)
			if scenario.Status == "" {
				data.complete = false
				data.warnings = append(data.warnings, fmt.Sprintf("eval scenario %s/%s has no terminal marker", evalRunID, scenarioRunID))
			}
			for _, invocationID := range scenarioBuilder.invocationOrder {
				invocation := *scenarioBuilder.invocations[invocationID]
				if invocation.Status == "" {
					data.complete = false
					data.warnings = append(data.warnings, fmt.Sprintf("eval invocation %s has no terminal marker", invocationID))
				}
				if invocation.ClaudeSessionID == "" {
					data.complete = false
					data.warnings = append(data.warnings, fmt.Sprintf("eval invocation %s has no Claude session binding", invocationID))
				}
				joinInvocationProviderExchanges(&invocation, providerExchanges)
				scenario.Invocations = append(scenario.Invocations, invocation)
			}
			run.Scenarios = append(run.Scenarios, scenario)
		}
		data.evalRuns = append(data.evalRuns, run)
	}
	return data
}

func joinInvocationProviderExchanges(invocation *EvalInvocationInspection, exchanges []inspectedProviderExchange) {
	if invocation.ClaudeSessionID == "" || invocation.StartedAt.IsZero() {
		return
	}
	end := invocation.EndedAt
	for _, exchange := range exchanges {
		if exchange.claudeSessionID != invocation.ClaudeSessionID || exchange.startedAt.Before(invocation.StartedAt) || (!end.IsZero() && exchange.startedAt.After(end)) {
			continue
		}
		invocation.ProviderExchanges++
		invocation.ExchangeIDs = append(invocation.ExchangeIDs, exchange.id)
	}
}

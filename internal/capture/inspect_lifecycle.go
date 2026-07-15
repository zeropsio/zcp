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
	scenarios     map[string]*lifecycleScenarioBuilder
	scenarioOrder []string
}

type lifecycleScenarioBuilder struct {
	status          string
	artifacts       []string
	invocations     map[string]*EvalInvocationInspection
	invocationOrder []string
}

func inspectLifecycleFile(path, relative, expectedCaptureID string, providerExchanges []inspectedProviderExchange) (*lifecycleInspectionData, error) {
	records, err := ReadLifecycleRecords(path)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, errors.New("lifecycle record file is empty")
	}
	captureID := expectedCaptureID
	if captureID == "" {
		captureID = records[0].CaptureID
	}
	if captureID == "" {
		return nil, fmt.Errorf("%w: lifecycle capture ID is empty", errInspectionIdentityMismatch)
	}
	for index, record := range records {
		wantSeq := uint64(index + 1)
		if record.Seq != wantSeq {
			return nil, fmt.Errorf("lifecycle sequence discontinuity: got %d, want %d", record.Seq, wantSeq)
		}
		if record.CaptureID != captureID {
			return nil, fmt.Errorf("%w: lifecycle seq %d capture ID %q differs from %q", errInspectionIdentityMismatch, record.Seq, record.CaptureID, captureID)
		}
		if err := validateLifecycleMarker(record.LifecycleMarker); err != nil {
			return nil, fmt.Errorf("lifecycle seq %d: %w", record.Seq, err)
		}
	}
	if records[0].Kind != LifecycleStreamStart {
		return nil, fmt.Errorf("lifecycle first record = %q, want %q", records[0].Kind, LifecycleStreamStart)
	}
	terminal := records[len(records)-1]
	if terminal.Kind != LifecycleStreamEnd {
		return nil, fmt.Errorf("lifecycle terminal record = %q, want %q", terminal.Kind, LifecycleStreamEnd)
	}

	runs := make(map[string]*lifecycleRunBuilder)
	var runOrder []string
	for _, record := range records[1 : len(records)-1] {
		evidence := RawEvidence{File: relative, SeqStart: record.Seq, SeqEnd: record.Seq, ObservedAt: record.Time}
		switch record.Kind {
		case LifecycleEvalRunStart:
			if _, exists := runs[record.EvalRunID]; exists {
				return nil, fmt.Errorf("duplicate eval run start %q", record.EvalRunID)
			}
			runs[record.EvalRunID] = &lifecycleRunBuilder{scenarios: make(map[string]*lifecycleScenarioBuilder)}
			runOrder = append(runOrder, record.EvalRunID)
		case LifecycleEvalRunEnd:
			run, ok := runs[record.EvalRunID]
			if !ok {
				return nil, fmt.Errorf("eval run end %q has no start", record.EvalRunID)
			}
			run.status = record.Status
		case LifecycleScenarioStart:
			run, ok := runs[record.EvalRunID]
			if !ok {
				return nil, fmt.Errorf("scenario %q references unknown eval run %q", record.ScenarioRunID, record.EvalRunID)
			}
			if _, exists := run.scenarios[record.ScenarioRunID]; exists {
				return nil, fmt.Errorf("duplicate scenario start %q", record.ScenarioRunID)
			}
			run.scenarios[record.ScenarioRunID] = &lifecycleScenarioBuilder{invocations: make(map[string]*EvalInvocationInspection)}
			run.scenarioOrder = append(run.scenarioOrder, record.ScenarioRunID)
		case LifecycleScenarioEnd:
			scenario, err := lifecycleScenario(runs, record.EvalRunID, record.ScenarioRunID)
			if err != nil {
				return nil, err
			}
			scenario.status = record.Status
		case LifecycleInvocationStart:
			scenario, err := lifecycleScenario(runs, record.EvalRunID, record.ScenarioRunID)
			if err != nil {
				return nil, err
			}
			if _, exists := scenario.invocations[record.InvocationID]; exists {
				return nil, fmt.Errorf("duplicate invocation start %q", record.InvocationID)
			}
			scenario.invocations[record.InvocationID] = &EvalInvocationInspection{
				InvocationID: record.InvocationID,
				Phase:        record.Phase,
				StartedAt:    record.Time,
				StartSource:  evidence,
			}
			scenario.invocationOrder = append(scenario.invocationOrder, record.InvocationID)
		case LifecycleInvocationBind:
			invocation, err := lifecycleInvocation(runs, record.EvalRunID, record.ScenarioRunID, record.InvocationID)
			if err != nil {
				return nil, err
			}
			if invocation.Phase != record.Phase {
				return nil, fmt.Errorf("invocation %q bind phase %q differs from start %q", record.InvocationID, record.Phase, invocation.Phase)
			}
			if invocation.ClaudeSessionID != "" && invocation.ClaudeSessionID != record.ClaudeSessionID {
				return nil, fmt.Errorf("invocation %q has conflicting Claude session bindings", record.InvocationID)
			}
			invocation.ClaudeSessionID = record.ClaudeSessionID
			invocation.BindSource = evidence
		case LifecycleInvocationEnd:
			invocation, err := lifecycleInvocation(runs, record.EvalRunID, record.ScenarioRunID, record.InvocationID)
			if err != nil {
				return nil, err
			}
			if invocation.Phase != record.Phase {
				return nil, fmt.Errorf("invocation %q end phase %q differs from start %q", record.InvocationID, record.Phase, invocation.Phase)
			}
			invocation.Status = record.Status
			invocation.EndedAt = record.Time
			invocation.EndSource = evidence
		case LifecycleArtifact:
			scenario, err := lifecycleScenario(runs, record.EvalRunID, record.ScenarioRunID)
			if err != nil {
				return nil, err
			}
			scenario.artifacts = append(scenario.artifacts, record.ArtifactPath)
		default:
			return nil, fmt.Errorf("unexpected lifecycle marker %q", record.Kind)
		}
	}

	data := &lifecycleInspectionData{status: terminal.Status, complete: true, recordCount: len(records)}
	for _, evalRunID := range runOrder {
		runBuilder := runs[evalRunID]
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
	return data, nil
}

func lifecycleScenario(runs map[string]*lifecycleRunBuilder, evalRunID, scenarioRunID string) (*lifecycleScenarioBuilder, error) {
	run, ok := runs[evalRunID]
	if !ok {
		return nil, fmt.Errorf("unknown eval run %q", evalRunID)
	}
	scenario, ok := run.scenarios[scenarioRunID]
	if !ok {
		return nil, fmt.Errorf("unknown eval scenario %q in run %q", scenarioRunID, evalRunID)
	}
	return scenario, nil
}

func lifecycleInvocation(runs map[string]*lifecycleRunBuilder, evalRunID, scenarioRunID, invocationID string) (*EvalInvocationInspection, error) {
	scenario, err := lifecycleScenario(runs, evalRunID, scenarioRunID)
	if err != nil {
		return nil, err
	}
	invocation, ok := scenario.invocations[invocationID]
	if !ok {
		return nil, fmt.Errorf("unknown eval invocation %q", invocationID)
	}
	return invocation, nil
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

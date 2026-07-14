package projection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/capture"
)

func Build(ctx context.Context, sessionDir string) (*View, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	manifest, err := capture.ReadSessionManifest(filepath.Join(sessionDir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	if manifest.Status == capture.CaptureRunning {
		return buildRunningView(ctx, sessionDir, manifest)
	}
	report, err := capture.InspectSession(sessionDir)
	if err != nil {
		return buildInvalidView(manifest, err)
	}
	view := newView(manifest)
	view.Integrity = IntegritySummary{
		State:                 integrityState(report.Integrity.Valid, report.Integrity.Complete),
		Valid:                 report.Integrity.Valid,
		Complete:              report.Integrity.Complete,
		ManifestPresent:       report.ManifestPresent,
		ManifestFilesVerified: report.Integrity.ManifestFilesVerified,
		ProviderRecords:       report.Integrity.ProviderRecords,
		LifecycleRecords:      report.Integrity.LifecycleRecords,
		MCPRecords:            report.Integrity.MCPRecords,
		ProvenanceRecords:     report.Integrity.ProvenanceRecords,
		WarningCount:          len(report.Warnings),
	}
	for _, warning := range report.Warnings {
		view.Diagnostics = append(view.Diagnostics, StructuralDiagnostic{Code: "inspection.warning", Severity: "warning", Summary: warning, Basis: "derived-exact"})
	}
	if err := addManifestFiles(view, manifest); err != nil {
		return nil, err
	}
	if err := addRawEvidence(ctx, view, sessionDir, manifest); err != nil {
		return nil, err
	}
	view.RawRecordTotalKnown = true
	addClientArtifacts(view, sessionDir, manifest)
	addReportHierarchy(view, report)
	addReportSessions(view, report)
	addReportContexts(view, report)
	addReportMCP(view, report)
	addReportTools(view, report)
	finalizeOverview(view, report)
	buildEdges(view)
	buildMetrics(view)
	sortTimeline(view.Timeline)
	return view, nil
}

func newView(manifest *capture.SessionManifestDocument) *View {
	view := &View{
		FormatVersion: FormatVersion1,
		Capture: CaptureSummary{
			ID: manifest.SessionID, Label: manifest.Label, Status: manifest.Status, Plaintext: manifest.Plaintext,
			StartedAt: manifest.StartedAt, FormatVersion: manifest.FormatVersion,
			BuildVersion: manifest.Build.Version, BuildCommit: manifest.Build.Commit, BuildTime: manifest.Build.Built,
			ProviderOrigin: manifest.Provider.Origin, ChildExitCode: manifest.ChildExitCode,
		},
		Overview:       Overview{BytesByKind: make(map[string]int64)},
		EvalRuns:       []EvalRun{},
		Sessions:       []ClientSession{},
		ClientRuns:     []ClientRun{},
		Conversation:   []ConversationEvent{},
		Exchanges:      []ProviderExchange{},
		ProviderEvents: []ProviderEvent{},
		ProviderBlocks: []ProviderBlock{},
		Contexts:       []ContextSnapshot{},
		Tools:          []ToolExecution{},
		MCPProcesses:   []MCPProcess{},
		MCPCalls:       []MCPCall{},
		Sources:        []SourceOwner{},
		Timeline:       []TimelineEvent{},
		RawFiles:       []RawFile{},
		RawRecords:     []RawRecordSummary{},
		Artifacts:      []Artifact{},
		Diagnostics:    []StructuralDiagnostic{},
		Metrics:        []Metric{},
		Edges:          []Edge{},
	}
	if manifest.EndedAt != nil {
		view.Capture.EndedAt = *manifest.EndedAt
		view.Capture.DurationMs = manifest.EndedAt.Sub(manifest.StartedAt).Milliseconds()
	}
	return view
}

func buildInvalidView(manifest *capture.SessionManifestDocument, inspectionErr error) (*View, error) {
	view := newView(manifest)
	view.Integrity = IntegritySummary{State: "invalid", ManifestPresent: true}
	view.Diagnostics = append(view.Diagnostics, StructuralDiagnostic{
		Code: "inspection.invalid", Severity: statusError, Summary: inspectionErr.Error(), Basis: "manifest-validation",
	})
	if err := addManifestFiles(view, manifest); err != nil {
		return nil, err
	}
	buildMetrics(view)
	for index := range view.Metrics {
		metric := &view.Metrics[index]
		if metric.ID == "integrity.valid" || metric.ID == "integrity.complete" {
			continue
		}
		if metric.ID == "capture.bundle.bytes" || metric.ID == "capture.files" || metric.ID == "capture.artifacts" || metric.ID == "capture.artifact_bytes" || metric.ID == "capture.duration" {
			metric.EvidenceBasis = "unverified-manifest-declaration"
			continue
		}
		metric.Value = nil
		metric.SampleCount = 0
		metric.MissingCount = 1
	}
	return view, nil
}

func addManifestFiles(view *View, manifest *capture.SessionManifestDocument) error {
	for _, file := range manifest.Files {
		if file.Path == "" {
			return errors.New("capture manifest contains an empty file path")
		}
		view.RawFiles = append(view.RawFiles, RawFile{Kind: file.Kind, Path: file.Path, SizeBytes: file.SizeBytes, SHA256: file.SHA256})
		view.Overview.BundleBytes += file.SizeBytes
		view.Overview.BytesByKind[file.Kind] += file.SizeBytes
		if file.Kind == capture.ManifestFileEval {
			view.Artifacts = append(view.Artifacts, Artifact{Path: file.Path, SizeBytes: file.SizeBytes, SHA256: file.SHA256, Type: artifactType(file.Path)})
		}
	}
	return nil
}

func addRawEvidence(ctx context.Context, view *View, sessionDir string, manifest *capture.SessionManifestDocument) error {
	for _, file := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		path, err := resolveFile(sessionDir, file.Path)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", file.Path, err)
		}
		switch file.Kind {
		case capture.ManifestFileProvider, capture.ManifestFileMCP:
			records, err := capture.ReadRecords(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", file.Path, err)
			}
			addRecordSummaries(view, file.Path, records)
			if file.Kind == capture.ManifestFileProvider {
				addProviderEvents(view, file.Path, records)
			} else {
				addMCPRecordEvents(view, file.Path, records)
			}
		case capture.ManifestFileLifecycle:
			records, err := capture.ReadLifecycleRecords(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", file.Path, err)
			}
			addLifecycleEvents(view, file.Path, records)
		case capture.ManifestFileProvenance:
			records, err := capture.ReadCompositionRecords(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", file.Path, err)
			}
			addProvenanceEvents(view, file.Path, records)
		}
	}
	return nil
}

const maxProjectedRawRecords = 5_000

func addRecordSummaries(view *View, file string, records []capture.Record) {
	for _, record := range records {
		appendRawRecordSummary(view, RawRecordSummary{
			ID: rawRecordID(file, record.Seq), File: file, Seq: record.Seq, Time: record.Time, Kind: record.Kind,
			ExchangeID: record.ExchangeID, ProcessID: record.ProcessID, EvalRunID: record.EvalRunID,
			ScenarioRunID: record.ScenarioRunID, InvocationID: record.InvocationID, Phase: record.Phase,
			Direction: record.Direction, BodyBytes: record.BodyBytes, StreamOffset: record.StreamOffset,
			StatusCode: record.StatusCode, CaptureStatus: record.CaptureStatus,
			HasBody: record.BodyBase64 != "", HasError: record.Error != "",
		})
		if record.Kind == capture.RecordSessionStart || record.Kind == capture.RecordSessionEnd || record.Kind == capture.RecordCaptureGap {
			view.Timeline = append(view.Timeline, recordTimelineEvent(file, record, "capture"))
		}
	}
}

func appendRawRecordSummary(view *View, record RawRecordSummary) {
	view.RawRecordTotal++
	if len(view.RawRecords) < maxProjectedRawRecords {
		view.RawRecords = append(view.RawRecords, record)
		return
	}
	if view.RawRecordsTruncated {
		return
	}
	view.RawRecordsTruncated = true
	view.Diagnostics = append(view.Diagnostics, StructuralDiagnostic{
		Code: "projection.raw_records.truncated", Severity: "info",
		Summary: fmt.Sprintf("initial raw record projection is limited to %d records; canonical files remain available through bounded detail queries", maxProjectedRawRecords),
		Basis:   "derived-exact",
	})
}

type exchangeBuilder struct {
	value    ProviderExchange
	seqStart uint64
	seqEnd   uint64
}

func addProviderEvents(view *View, file string, records []capture.Record) {
	builders := make(map[string]*exchangeBuilder)
	var order []string
	for _, record := range records {
		if record.ExchangeID == "" {
			continue
		}
		builder := builders[record.ExchangeID]
		if builder == nil {
			builder = &exchangeBuilder{value: ProviderExchange{ID: record.ExchangeID, Status: "incomplete"}}
			builders[record.ExchangeID] = builder
			order = append(order, record.ExchangeID)
		}
		if builder.seqStart == 0 {
			builder.seqStart = record.Seq
		}
		builder.seqEnd = record.Seq
		switch record.Kind {
		case capture.RecordProviderRequestStart:
			builder.value.Method = record.Method
			builder.value.Path = record.Path
			builder.value.StartedAt = record.Time
		case capture.RecordProviderRequestEnd:
			builder.value.RequestEndedAt = record.Time
			builder.value.RequestBytes = record.BodyBytes
		case capture.RecordProviderResponseStart:
			builder.value.ResponseAt = record.Time
			builder.value.StatusCode = record.StatusCode
		case capture.RecordProviderResponseEnd:
			builder.value.EndedAt = record.Time
			builder.value.ResponseBytes = record.BodyBytes
			builder.value.ErrorPresent = record.Error != ""
			if record.Error == "" {
				builder.value.Status = "complete"
			} else {
				builder.value.Status = statusError
			}
		case capture.RecordProviderExchangeError:
			builder.value.EndedAt = record.Time
			builder.value.ErrorPresent = true
			builder.value.Status = statusError
		}
	}
	for _, exchangeID := range order {
		builder := builders[exchangeID]
		exchange := builder.value
		if !exchange.StartedAt.IsZero() && !exchange.EndedAt.IsZero() {
			exchange.DurationMs = exchange.EndedAt.Sub(exchange.StartedAt).Milliseconds()
			exchange.TimingObserved = exchange.DurationMs >= 0
		}
		if !exchange.RequestEndedAt.IsZero() && !exchange.ResponseAt.IsZero() {
			exchange.ProviderWaitMs = exchange.ResponseAt.Sub(exchange.RequestEndedAt).Milliseconds()
			exchange.ProviderWaitObserved = exchange.ProviderWaitMs >= 0
		}
		evidence := EvidenceRef{
			ID: rawRangeID(file, builder.seqStart, builder.seqEnd), File: file, SeqStart: builder.seqStart, SeqEnd: builder.seqEnd,
			ExchangeID: exchange.ID, ObservedAt: exchange.StartedAt,
		}
		exchange.Evidence = []EvidenceRef{evidence}
		view.Exchanges = append(view.Exchanges, exchange)
		view.Timeline = append(view.Timeline, TimelineEvent{
			ID: "provider:" + exchange.ID, Kind: "provider.exchange", Lane: "provider", Title: exchange.Method + " " + exchange.Path,
			Status: exchange.Status, Basis: "raw", StartedAt: exchange.StartedAt, EndedAt: exchange.EndedAt,
			DurationMs: exchange.DurationMs, ExchangeID: exchange.ID, Evidence: []EvidenceRef{evidence},
		})
	}
	projectProviderResponses(view, file, records)
}

func addMCPRecordEvents(view *View, file string, records []capture.Record) {
	for _, record := range records {
		switch record.Kind {
		case capture.RecordMCPStreamStart, capture.RecordMCPStreamEnd, capture.RecordMCPStdinError, capture.RecordMCPStdoutError, capture.RecordCaptureGap:
			event := recordTimelineEvent(file, record, "mcp:"+file)
			event.EvalRunID = record.EvalRunID
			event.ScenarioRunID = record.ScenarioRunID
			event.InvocationID = record.InvocationID
			event.Phase = record.Phase
			view.Timeline = append(view.Timeline, event)
		}
	}
	projectMCPMessages(view, file, records)
}

func addLifecycleEvents(view *View, file string, records []capture.LifecycleRecord) {
	for _, record := range records {
		evidence := EvidenceRef{ID: rawRecordID(file, record.Seq), File: file, SeqStart: record.Seq, SeqEnd: record.Seq, ObservedAt: record.Time}
		appendRawRecordSummary(view, RawRecordSummary{
			ID: evidence.ID, File: file, Seq: record.Seq, Time: record.Time, Kind: record.Kind,
			EvalRunID: record.EvalRunID, ScenarioRunID: record.ScenarioRunID, InvocationID: record.InvocationID,
			Phase: record.Phase, CaptureStatus: record.Status, HasError: record.Error != "",
		})
		view.Timeline = append(view.Timeline, TimelineEvent{
			ID: "lifecycle:" + fmt.Sprint(record.Seq), Kind: record.Kind, Lane: "lifecycle", Title: lifecycleTitle(record),
			Status: record.Status, Basis: "raw", StartedAt: record.Time, EvalRunID: record.EvalRunID,
			ScenarioRunID: record.ScenarioRunID, InvocationID: record.InvocationID, Phase: record.Phase,
			ClientSessionID: record.ClaudeSessionID, Evidence: []EvidenceRef{evidence},
		})
	}
}

func addProvenanceEvents(view *View, file string, records []capture.CompositionRecord) {
	for index, record := range records {
		seq := uint64(index + 1)
		evidence := EvidenceRef{ID: rawRecordID(file, seq), File: file, SeqStart: seq, SeqEnd: seq, ObservedAt: record.Time, ByteLength: int64(record.OutputBytes)}
		appendRawRecordSummary(view, RawRecordSummary{ID: evidence.ID, File: file, Seq: seq, Time: record.Time, Kind: "composition", ProcessID: record.ProcessID, BodyBytes: int64(record.OutputBytes)})
		view.Timeline = append(view.Timeline, TimelineEvent{
			ID: fmt.Sprintf("provenance:%s:%d", file, index+1), Kind: "composition", Lane: "provenance", Title: record.Surface,
			Basis: "raw", StartedAt: record.Time, Evidence: []EvidenceRef{evidence},
		})
	}
}

func addReportHierarchy(view *View, report *capture.InspectionReport) {
	for _, reportRun := range report.EvalRuns {
		run := EvalRun{ID: reportRun.EvalRunID, Status: reportRun.Status}
		for _, reportScenario := range reportRun.Scenarios {
			scenario := Scenario{ID: reportScenario.ScenarioRunID, Status: reportScenario.Status, Artifacts: append([]string(nil), reportScenario.Artifacts...)}
			view.Overview.Scenarios++
			for _, reportInvocation := range reportScenario.Invocations {
				invocation := Invocation{
					ID: reportInvocation.InvocationID, Phase: reportInvocation.Phase, ClientSessionID: reportInvocation.ClaudeSessionID,
					Status: reportInvocation.Status, StartedAt: reportInvocation.StartedAt, EndedAt: reportInvocation.EndedAt,
					ProviderExchanges: reportInvocation.ProviderExchanges, ExchangeIDs: append([]string(nil), reportInvocation.ExchangeIDs...),
					MCPProcesses: reportInvocation.MCPStreams, MCPFiles: append([]string(nil), reportInvocation.MCPFiles...),
				}
				if !invocation.StartedAt.IsZero() && !invocation.EndedAt.IsZero() {
					invocation.DurationMs = invocation.EndedAt.Sub(invocation.StartedAt).Milliseconds()
					invocation.TimingObserved = invocation.DurationMs >= 0
				}
				for _, source := range []capture.RawEvidence{reportInvocation.StartSource, reportInvocation.BindSource, reportInvocation.EndSource} {
					if source.File != "" {
						invocation.Evidence = append(invocation.Evidence, evidenceFromRaw(source))
					}
				}
				scenario.Invocations = append(scenario.Invocations, invocation)
				view.Overview.Invocations++
			}
			run.Scenarios = append(run.Scenarios, scenario)
		}
		view.EvalRuns = append(view.EvalRuns, run)
	}
}

func addReportSessions(view *View, report *capture.InspectionReport) {
	for _, session := range report.ClaudeSessions {
		value := ClientSession{
			ID: session.SessionID, ProviderExchanges: session.ProviderExchanges, Models: append([]string(nil), session.Models...),
			FirstObservedAt: session.FirstObservedAt, LastObservedAt: session.LastObservedAt,
			Evidence: []EvidenceRef{evidenceFromRaw(session.FirstSource)},
		}
		if !value.FirstObservedAt.IsZero() && !value.LastObservedAt.IsZero() {
			value.DurationMs = value.LastObservedAt.Sub(value.FirstObservedAt).Milliseconds()
			value.TimingObserved = value.DurationMs >= 0
		}
		view.Sessions = append(view.Sessions, value)
	}
}

func addReportContexts(view *View, report *capture.InspectionReport) {
	exchanges := make(map[string]*ProviderExchange, len(view.Exchanges))
	for index := range view.Exchanges {
		exchanges[view.Exchanges[index].ID] = &view.Exchanges[index]
	}
	for _, item := range report.ModelContexts {
		other := max(0, item.RequestBytes-item.SystemBytes-item.ToolBytes-item.MessageBytes)
		value := ContextSnapshot{
			ExchangeID: item.ExchangeID, ClientSessionID: item.ClaudeSessionID, Model: item.Model, ProviderMessageID: item.ProviderMessageID,
			RequestBytes: item.RequestBytes, SystemBlocks: item.SystemBlocks, SystemBytes: item.SystemBytes,
			ToolCount: item.ToolCount, MCPToolCount: item.MCPToolCount, BuiltInToolCount: item.BuiltInToolCount, ToolBytes: item.ToolBytes,
			MessageCount: item.MessageCount, MessageBytes: item.MessageBytes, OtherBytes: other,
			CommonPrefixMessages: item.CommonPrefixMessages, AddedMessages: item.AddedMessages, AddedMessageBytes: item.AddedMessageBytes,
			RemovedMessages: item.RemovedMessages, RewrittenMessages: item.RewrittenMessages,
			ContextReset: item.ContextReset, HistoryRewritten: item.HistoryRewritten,
			SystemChanged: item.SystemChanged, ToolsChanged: item.ToolsChanged,
			InputTokens: item.InputTokens, InputTokensObserved: item.InputTokensObserved,
			CacheCreationInputTokens: item.CacheCreationInputTokens, CacheCreationInputTokensObserved: item.CacheCreationInputTokensObserved,
			CacheReadInputTokens: item.CacheReadInputTokens, CacheReadInputTokensObserved: item.CacheReadInputTokensObserved,
			OutputTokens: item.OutputTokens, OutputTokensObserved: item.OutputTokensObserved,
			Evidence: evidenceFromRaw(item.Source),
		}
		view.Contexts = append(view.Contexts, value)
		if exchange := exchanges[item.ExchangeID]; exchange != nil {
			exchange.ClientSessionID = item.ClaudeSessionID
			exchange.Model = item.Model
		}
	}
}

func addReportMCP(view *View, report *capture.InspectionReport) {
	for _, stream := range report.MCPStreams {
		view.MCPProcesses = append(view.MCPProcesses, MCPProcess{
			File: stream.File, Status: stream.Status, EvalRunID: stream.EvalRunID, ScenarioRunID: stream.ScenarioRunID,
			InvocationID: stream.InvocationID, Phase: stream.Phase, InputBytes: stream.InputBytes, OutputBytes: stream.OutputBytes,
			ToolCalls: stream.ToolCalls, ProgressNotifications: stream.ProgressNotifications,
		})
	}
}

func addReportTools(view *View, report *capture.InspectionReport) {
	ownerByKey := make(map[string]*SourceOwner)
	for index, correlation := range report.Correlations {
		id := fmt.Sprintf("tool:%06d", index+1)
		propagation := correlation.ProviderResultStatus
		if propagation == "" {
			propagation = propagationMissing
		}
		basis := "name-order"
		if correlation.ArgumentsEqual {
			basis = "exact-arguments"
		}
		value := ToolExecution{
			ID: id, InvocationID: correlation.InvocationID, ClientSessionID: correlation.ClaudeSessionID,
			Category: toolCategoryMCP, ToolName: correlation.MCPToolName, ToolUseID: correlation.ProviderToolUseID,
			ProposalExchangeID: correlation.ProviderSource.ExchangeID, ResultExchangeID: correlation.ProviderResultSource.ExchangeID, MCPFile: correlation.MCPCallSource.File,
			ProviderToolName: correlation.ProviderToolName, MCPToolName: correlation.MCPToolName, MCPRequestID: correlation.MCPRequestID,
			ArgumentsBytes: len([]byte(correlation.ArgumentsJSON)), ArgumentsEqual: correlation.ArgumentsEqual,
			ResultBytes: correlation.MCPResultBytes, ProviderResultBytes: len([]byte(correlation.ProviderResultText)),
			IsError: correlation.MCPIsError, Propagation: propagation,
			CorrelationBasis: basis, StartedAt: correlation.MCPCallSource.ObservedAt, CompletedAt: correlation.MCPResultSource.ObservedAt,
		}
		if !value.StartedAt.IsZero() && !value.CompletedAt.IsZero() {
			value.DurationMs = value.CompletedAt.Sub(value.StartedAt).Milliseconds()
			value.TimingObserved = value.DurationMs >= 0
		}
		for _, evidence := range []capture.RawEvidence{correlation.ProviderSource, correlation.MCPCallSource, correlation.MCPResultSource, correlation.ProviderResultSource} {
			if evidence.File != "" {
				value.Evidence = append(value.Evidence, evidenceFromRaw(evidence))
			}
		}
		for _, source := range correlation.SourceMatches {
			value.SourceOwners = append(value.SourceOwners, source.AtomID)
			key := "current:" + source.AtomID
			addSourceOwner(ownerByKey, key, SourceOwner{Kind: "current-corpus", Owner: source.AtomID, File: source.File}, source.MatchedBytes, id)
		}
		for _, composition := range correlation.CompositionMatches {
			for _, component := range composition.Components {
				value.CompositionOwners = append(value.CompositionOwners, component.Owner)
				key := "capture:" + component.Kind + ":" + component.Owner
				addSourceOwner(ownerByKey, key, SourceOwner{Kind: component.Kind, Owner: component.Owner, File: composition.File}, component.End-component.Start, id)
			}
		}
		if propagation != "exact" {
			view.Diagnostics = append(view.Diagnostics, StructuralDiagnostic{
				Code: "tool.result.propagation_not_exact", Severity: "warning",
				Summary: propagationDiagnosticSummary(propagation), Basis: "derived-exact", ScopeID: id,
				Evidence: append([]EvidenceRef(nil), value.Evidence...),
			})
		}
		view.Tools = append(view.Tools, value)
		view.Timeline = append(view.Timeline, TimelineEvent{
			ID: id, Kind: "tool.execution", Lane: "tools", Title: correlation.MCPToolName,
			Status: toolStatus(correlation.MCPIsError), Basis: "joined-order", StartedAt: value.StartedAt,
			EndedAt: value.CompletedAt, DurationMs: value.DurationMs, InvocationID: correlation.InvocationID,
			ClientSessionID: correlation.ClaudeSessionID, ExchangeID: correlation.ProviderSource.ExchangeID,
			Evidence: append([]EvidenceRef(nil), value.Evidence...),
		})
	}
	keys := make([]string, 0, len(ownerByKey))
	for key := range ownerByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		view.Sources = append(view.Sources, *ownerByKey[key])
	}
}

func propagationDiagnosticSummary(status string) string {
	switch status {
	case propagationDifferent:
		return "Provider tool_result exists but differs from the exact MCP result"
	case propagationMissing:
		return "Completed MCP result has no observed provider tool_result"
	case propagationAmbiguous:
		return "MCP result evidence is incomplete, so provider propagation is ambiguous"
	default:
		return "MCP result was not proven byte-identical in a later provider request"
	}
}

func addSourceOwner(values map[string]*SourceOwner, key string, initial SourceOwner, bytes int, toolID string) {
	value := values[key]
	if value == nil {
		initialCopy := initial
		value = &initialCopy
		values[key] = value
	}
	value.Occurrences++
	value.MatchedBytes += bytes
	if !containsString(value.ToolIDs, toolID) {
		value.ToolIDs = append(value.ToolIDs, toolID)
	}
}

func finalizeOverview(view *View, report *capture.InspectionReport) {
	view.Overview.ProviderExchanges = report.ProviderExchanges
	view.Overview.UnattributedExchanges = report.UnattributedProviderExchanges
	view.Overview.ClientSessions = len(report.ClaudeSessions)
	view.Overview.EvalRuns = len(report.EvalRuns)
	view.Overview.MCPProcesses = len(report.MCPStreams)
	view.Overview.ToolExecutions = len(view.Tools)
	for _, contextView := range view.Contexts {
		view.Overview.TotalRequestBytes += int64(contextView.RequestBytes)
		view.Overview.TotalSystemBytes += int64(contextView.SystemBytes)
		view.Overview.TotalToolSchemaBytes += int64(contextView.ToolBytes)
		view.Overview.TotalMessageBytes += int64(contextView.MessageBytes)
		view.Overview.InputTokens += contextView.InputTokens
		view.Overview.CacheCreationInputTokens += contextView.CacheCreationInputTokens
		view.Overview.CacheReadInputTokens += contextView.CacheReadInputTokens
		view.Overview.OutputTokens += contextView.OutputTokens
		if contextView.RequestBytes > view.Overview.LargestRequestBytes {
			view.Overview.LargestRequestBytes = contextView.RequestBytes
		}
	}
	for _, tool := range view.Tools {
		if tool.IsError {
			view.Overview.ToolErrors++
		}
		if tool.Category == toolCategoryMCP {
			if tool.Propagation == "exact" {
				view.Overview.PropagationExact++
			} else {
				view.Overview.PropagationMissing++
			}
		}
	}
	for _, process := range view.MCPProcesses {
		view.Overview.MCPInputBytes += process.InputBytes
		view.Overview.MCPOutputBytes += process.OutputBytes
	}
}

func buildRunningView(ctx context.Context, sessionDir string, manifest *capture.SessionManifestDocument) (*View, error) {
	view := newView(manifest)
	view.Integrity = IntegritySummary{State: "running", ManifestPresent: true}
	view.Diagnostics = append(view.Diagnostics, StructuralDiagnostic{Code: "capture.running", Severity: "info", Summary: "Capture is running; only the durably visible prefix is shown and integrity is not final", Basis: "raw"})
	mcp, _ := filepath.Glob(filepath.Join(sessionDir, "mcp", "*.jsonl"))
	provenance, _ := filepath.Glob(filepath.Join(sessionDir, "provenance", "*.jsonl"))
	paths := make([]struct{ kind, rel string }, 0, 2+len(mcp)+len(provenance))
	paths = append(paths,
		struct{ kind, rel string }{capture.ManifestFileProvider, "provider.jsonl"},
		struct{ kind, rel string }{capture.ManifestFileLifecycle, "lifecycle.jsonl"},
	)
	for _, path := range mcp {
		rel, _ := filepath.Rel(sessionDir, path)
		paths = append(paths, struct{ kind, rel string }{capture.ManifestFileMCP, filepath.ToSlash(rel)})
	}
	for _, path := range provenance {
		rel, _ := filepath.Rel(sessionDir, path)
		paths = append(paths, struct{ kind, rel string }{capture.ManifestFileProvenance, filepath.ToSlash(rel)})
	}
	for _, item := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := resolveFile(sessionDir, item.rel)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		view.RawFiles = append(view.RawFiles, RawFile{Kind: item.kind, Path: item.rel, SizeBytes: info.Size()})
		view.Overview.BundleBytes += info.Size()
		view.Overview.BytesByKind[item.kind] += info.Size()
		var partialErr error
		switch item.kind {
		case capture.ManifestFileProvider, capture.ManifestFileMCP:
			var records []capture.Record
			records, partialErr = readRecordPrefix(path)
			addRecordSummaries(view, item.rel, records)
			if item.kind == capture.ManifestFileProvider {
				addProviderEvents(view, item.rel, records)
			} else {
				addMCPRecordEvents(view, item.rel, records)
			}
		case capture.ManifestFileLifecycle:
			var records []capture.LifecycleRecord
			records, partialErr = readLifecyclePrefix(path)
			addLifecycleEvents(view, item.rel, records)
		case capture.ManifestFileProvenance:
			var records []capture.CompositionRecord
			records, partialErr = readCompositionPrefix(path)
			addProvenanceEvents(view, item.rel, records)
		}
		if partialErr != nil {
			view.Diagnostics = append(view.Diagnostics, StructuralDiagnostic{Code: "capture.prefix.partial_line", Severity: "info", Summary: partialErr.Error(), Basis: "raw"})
		}
	}
	view.RawRecordTotalKnown = true
	view.Overview.ProviderExchanges = len(view.Exchanges)
	view.Overview.MCPProcesses = len(view.MCPProcesses)
	buildEdges(view)
	buildMetrics(view)
	sortTimeline(view.Timeline)
	return view, nil
}

func readLifecyclePrefix(path string) ([]capture.LifecycleRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var records []capture.LifecycleRecord
	for {
		var record capture.LifecycleRecord
		if err := decoder.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				return records, nil
			}
			return records, fmt.Errorf("durable lifecycle prefix stopped after %d records: %w", len(records), err)
		}
		records = append(records, record)
	}
}

func readCompositionPrefix(path string) ([]capture.CompositionRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var records []capture.CompositionRecord
	for {
		var record capture.CompositionRecord
		if err := decoder.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				return records, nil
			}
			return records, fmt.Errorf("durable provenance prefix stopped after %d records: %w", len(records), err)
		}
		records = append(records, record)
	}
}

func readRecordPrefix(path string) ([]capture.Record, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var records []capture.Record
	for {
		var record capture.Record
		if err := decoder.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				return records, nil
			}
			return records, fmt.Errorf("durable prefix stopped after %d records: %w", len(records), err)
		}
		records = append(records, record)
	}
}

func integrityState(valid, complete bool) string {
	switch {
	case valid && complete:
		return "ok"
	case valid:
		return "incomplete"
	default:
		return "invalid"
	}
}

func evidenceFromRaw(source capture.RawEvidence) EvidenceRef {
	return EvidenceRef{
		ID: rawRangeID(source.File, source.SeqStart, source.SeqEnd), File: source.File,
		SeqStart: source.SeqStart, SeqEnd: source.SeqEnd, StreamOffset: source.StreamOffset,
		DecodedOffset: source.DecodedOffset, ByteLength: source.ByteLength,
		ExchangeID: source.ExchangeID, ObservedAt: source.ObservedAt,
	}
}

func recordTimelineEvent(file string, record capture.Record, lane string) TimelineEvent {
	evidence := EvidenceRef{ID: rawRecordID(file, record.Seq), File: file, SeqStart: record.Seq, SeqEnd: record.Seq, ExchangeID: record.ExchangeID, ObservedAt: record.Time, ByteLength: record.BodyBytes, StreamOffset: record.StreamOffset}
	return TimelineEvent{
		ID: "record:" + evidence.ID, Kind: record.Kind, Lane: lane, Title: record.Kind,
		Status: record.CaptureStatus, Basis: "raw", StartedAt: record.Time, ExchangeID: record.ExchangeID,
		Evidence: []EvidenceRef{evidence},
	}
}

func lifecycleTitle(record capture.LifecycleRecord) string {
	for _, value := range []string{record.InvocationID, record.ScenarioRunID, record.EvalRunID, record.Kind} {
		if value != "" {
			return value
		}
	}
	return record.Kind
}

func toolStatus(isError bool) string {
	if isError {
		return statusError
	}
	return "ok"
}

func sortTimeline(events []TimelineEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].StartedAt.Equal(events[j].StartedAt) {
			return events[i].ID < events[j].ID
		}
		if events[i].StartedAt.IsZero() {
			return false
		}
		if events[j].StartedAt.IsZero() {
			return true
		}
		return events[i].StartedAt.Before(events[j].StartedAt)
	})
}

func artifactType(path string) string {
	base := filepath.Base(path)
	switch base {
	case "scenario.md":
		return "scenario"
	case "task-prompt.txt":
		return "task-prompt"
	case "retrospective-prompt.txt":
		return "retrospective-prompt"
	case "transcript.jsonl":
		return "transcript"
	case "retrospective.jsonl":
		return "retrospective"
	case "self-review.md":
		return "self-review"
	case "meta.json":
		return "metadata"
	default:
		return strings.TrimPrefix(filepath.Ext(base), ".")
	}
}

func rawRecordID(file string, seq uint64) string { return fmt.Sprintf("raw:%s:%d", file, seq) }
func rawRangeID(file string, start, end uint64) string {
	if end == 0 {
		end = start
	}
	return fmt.Sprintf("raw:%s:%d-%d", file, start, end)
}

func containsString(values []string, wanted string) bool {
	return slices.Contains(values, wanted)
}

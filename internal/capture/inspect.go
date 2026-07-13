package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	inspectionArgumentPreviewBytes = 1200
	inspectionResultPreviewBytes   = 8192
)

// RawEvidence links one derived inspection fact back to canonical raw records.
type RawEvidence struct {
	File          string
	SeqStart      uint64
	SeqEnd        uint64
	StreamOffset  int64
	ByteLength    int64
	ExchangeID    string
	ObservedAt    time.Time
	DecodedOffset int64
}

// InspectionIntegrity summarizes checks performed before protocol parsing.
type InspectionIntegrity struct {
	// Valid means every available byte/record/hash check succeeded. Complete is
	// separate: a partial or unclean capture can be structurally valid without
	// containing the full observed protocol lifetime.
	Valid                 bool
	Complete              bool
	ManifestFilesVerified int
	ProviderRecords       int
	LifecycleRecords      int
	MCPRecords            int
	ProvenanceRecords     int
}

// MCPStreamInspection summarizes one captured ZCP stdio process.
// ClaudeSessionInspection groups provider exchanges by the adapter-specific
// session identity observed inside the exact Messages request body.
type ClaudeSessionInspection struct {
	SessionID         string
	ProviderExchanges int
	Models            []string
	FirstObservedAt   time.Time
	LastObservedAt    time.Time
	FirstSource       RawEvidence
}

// ModelContextInspection summarizes one exact provider request without printing
// model-visible content. Byte counts refer to JSON wire fragments.
type ModelContextInspection struct {
	ExchangeID               string
	ClaudeSessionID          string
	Model                    string
	ProviderMessageID        string
	RequestBytes             int
	SystemBlocks             int
	SystemBytes              int
	ToolCount                int
	MCPToolCount             int
	BuiltInToolCount         int
	ToolBytes                int
	MessageCount             int
	MessageBytes             int
	AddedMessages            int
	AddedMessageBytes        int
	SystemChanged            bool
	ToolsChanged             bool
	InputTokens              int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
	OutputTokens             int64
	Source                   RawEvidence
}

// EvalInvocationInspection is one explicitly bounded Claude CLI invocation.
// A resumed Claude session can appear in several invocation phases.
type EvalInvocationInspection struct {
	InvocationID      string
	Phase             string
	ClaudeSessionID   string
	Status            string
	StartedAt         time.Time
	EndedAt           time.Time
	ProviderExchanges int
	ExchangeIDs       []string
	MCPStreams        int
	MCPFiles          []string
	StartSource       RawEvidence
	BindSource        RawEvidence
	EndSource         RawEvidence
}

type EvalScenarioInspection struct {
	ScenarioRunID string
	Status        string
	Invocations   []EvalInvocationInspection
	Artifacts     []string
}

type EvalRunInspection struct {
	EvalRunID string
	Status    string
	Scenarios []EvalScenarioInspection
}

type MCPStreamInspection struct {
	File                  string
	Status                string
	EvalRunID             string
	ScenarioRunID         string
	InvocationID          string
	Phase                 string
	InputBytes            int64
	OutputBytes           int64
	ToolCalls             int
	ProgressNotifications int
}

// ToolCorrelation is one evidence-backed model → MCP → model-context chain.
type ToolCorrelation struct {
	InvocationID           string
	ClaudeSessionID        string
	ProviderToolName       string
	MCPToolName            string
	ArgumentsJSON          string
	ArgumentsEqual         bool
	MCPRequestID           string
	MCPResultText          string
	MCPResultBytes         int
	MCPIsError             bool
	ProviderResultObserved bool
	ProviderSource         RawEvidence
	MCPCallSource          RawEvidence
	MCPResultSource        RawEvidence
	ProviderResultSource   RawEvidence
	SourceMatches          []ContentSourceMatch
	CompositionMatches     []CompositionSourceMatch
}

// InspectionReport is an ephemeral view over canonical raw capture files. Its
// shape is explicitly not a stable storage or API contract.
type InspectionReport struct {
	SessionDir                    string
	SessionID                     string
	Label                         string
	Status                        string
	ManifestPresent               bool
	CaptureBuild                  CaptureBuildInfo
	Integrity                     InspectionIntegrity
	ProviderExchanges             int
	ClaudeSessions                []ClaudeSessionInspection
	UnattributedProviderExchanges int
	EvalRuns                      []EvalRunInspection
	ModelContexts                 []ModelContextInspection
	MCPStreams                    []MCPStreamInspection
	Correlations                  []ToolCorrelation
	Warnings                      []string
}

type inspectionFile struct {
	kind string
	path string
	rel  string
}

// InspectSession validates raw files first, then derives a causal protocol
// view. Legacy pre-manifest captures remain readable but are explicitly marked.
func InspectSession(sessionDir string) (*InspectionReport, error) {
	info, err := os.Stat(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("stat capture session: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("capture session %q is not a directory", sessionDir)
	}
	report := &InspectionReport{SessionDir: sessionDir}
	files, err := inspectionFiles(sessionDir, report)
	if err != nil {
		return nil, err
	}
	if err := validateInspectionManifestFiles(sessionDir, report, files); err != nil {
		return nil, err
	}

	var providerFile inspectionFile
	var lifecycleFile inspectionFile
	var mcpFiles []inspectionFile
	var provenanceFiles []inspectionFile
	for _, file := range files {
		switch file.kind {
		case ManifestFileProvider:
			if providerFile.path != "" {
				return nil, errors.New("capture contains multiple provider files")
			}
			providerFile = file
		case ManifestFileLifecycle:
			if lifecycleFile.path != "" {
				return nil, errors.New("capture contains multiple lifecycle files")
			}
			lifecycleFile = file
		case ManifestFileMCP:
			mcpFiles = append(mcpFiles, file)
		case ManifestFileProvenance:
			provenanceFiles = append(provenanceFiles, file)
		}
	}
	if providerFile.path == "" {
		return nil, errors.New("capture provider.jsonl is missing")
	}

	provider, err := inspectProviderFile(providerFile.path, providerFile.rel)
	if err != nil {
		if report.Status != CaptureUnclean {
			return nil, fmt.Errorf("inspect provider capture: %w", err)
		}
		report.Warnings = append(report.Warnings, "unclean provider prefix is not protocol-complete: "+err.Error())
		provider = &providerInspectionData{sessionID: report.SessionID, status: CaptureUnclean, toolResults: make(map[string]inspectedProviderToolResult)}
	}
	report.ProviderExchanges = provider.exchangeCount
	report.ClaudeSessions = provider.claudeSessions
	report.ModelContexts = provider.contexts
	report.UnattributedProviderExchanges = provider.unattributedExchanges
	report.Warnings = append(report.Warnings, provider.warnings...)
	report.Integrity.ProviderRecords = provider.recordCount
	if report.SessionID == "" {
		report.SessionID = provider.sessionID
	}
	if report.Status == "" {
		report.Status = provider.status
	} else if report.Status != provider.status {
		return nil, fmt.Errorf("manifest status %q differs from provider terminal status %q", report.Status, provider.status)
	}

	lifecycleStatus := CaptureComplete
	if lifecycleFile.path != "" {
		lifecycle, err := inspectLifecycleFile(lifecycleFile.path, lifecycleFile.rel, provider.exchanges)
		if err != nil {
			if report.Status != CaptureUnclean {
				return nil, fmt.Errorf("inspect lifecycle capture: %w", err)
			}
			report.Warnings = append(report.Warnings, "unclean lifecycle prefix is not protocol-complete: "+err.Error())
			lifecycle = &lifecycleInspectionData{status: CaptureUnclean}
		}
		report.EvalRuns = lifecycle.evalRuns
		report.Warnings = append(report.Warnings, lifecycle.warnings...)
		report.Integrity.LifecycleRecords = lifecycle.recordCount
		lifecycleStatus = lifecycle.status
		if report.Status != lifecycle.status {
			return nil, fmt.Errorf("manifest status %q differs from lifecycle terminal status %q", report.Status, lifecycle.status)
		}
	}

	var calls []inspectedMCPCall
	for _, file := range mcpFiles {
		stream, err := inspectMCPFile(file.path, file.rel)
		if err != nil {
			if report.Status != CaptureUnclean {
				return nil, fmt.Errorf("inspect MCP capture %s: %w", file.rel, err)
			}
			report.Warnings = append(report.Warnings, fmt.Sprintf("unclean MCP prefix %s is not protocol-complete: %v", file.rel, err))
			continue
		}
		report.MCPStreams = append(report.MCPStreams, stream.summary)
		report.Integrity.MCPRecords += stream.recordCount
		calls = append(calls, stream.calls...)
	}
	sort.Slice(calls, func(i, j int) bool {
		if calls[i].source.ObservedAt.Equal(calls[j].source.ObservedAt) {
			if calls[i].source.File == calls[j].source.File {
				return calls[i].source.StreamOffset < calls[j].source.StreamOffset
			}
			return calls[i].source.File < calls[j].source.File
		}
		return calls[i].source.ObservedAt.Before(calls[j].source.ObservedAt)
	})
	var compositionRecords []inspectedCompositionRecord
	for _, file := range provenanceFiles {
		records, err := inspectCompositionFile(file.path, file.rel, report.SessionID)
		if err != nil {
			return nil, fmt.Errorf("inspect composition provenance %s: %w", file.rel, err)
		}
		report.Integrity.ProvenanceRecords += len(records)
		compositionRecords = append(compositionRecords, records...)
	}
	report.Warnings = append(report.Warnings, joinEvalMCPStreams(report.EvalRuns, report.MCPStreams)...)
	assignProviderToolInvocations(provider.toolUses, report.EvalRuns)
	report.Correlations = correlateToolEvidence(provider.toolUses, provider.toolResults, calls)
	sourceDocuments, sourceErr := loadInspectionSourceDocuments()
	if sourceErr != nil {
		report.Warnings = append(report.Warnings, "load current embedded atom corpus for source matching: "+sourceErr.Error())
	} else {
		for index := range report.Correlations {
			if report.Correlations[index].MCPResultText == "" {
				continue
			}
			matches, matchErr := matchInspectionSources(report.Correlations[index].MCPResultText, sourceDocuments)
			if matchErr != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("source-match tool correlation %d: %v", index+1, matchErr))
				continue
			}
			report.Correlations[index].SourceMatches = matches
		}
	}
	for index := range report.Correlations {
		if report.Correlations[index].MCPResultText == "" {
			continue
		}
		matches, matchErr := matchCompositionSources(report.Correlations[index].MCPResultText, compositionRecords)
		if matchErr != nil {
			return nil, fmt.Errorf("match composition provenance for tool correlation %d: %w", index+1, matchErr)
		}
		report.Correlations[index].CompositionMatches = matches
	}
	report.Integrity.Valid = true
	report.Integrity.Complete = report.Status == CaptureComplete && provider.status == CaptureComplete && lifecycleStatus == CaptureComplete
	for _, stream := range report.MCPStreams {
		if stream.Status != CaptureComplete {
			report.Integrity.Complete = false
		}
	}
	if !report.Integrity.Complete {
		report.Warnings = append(report.Warnings, "capture evidence is partial or unclean; hashes validate only the recorded prefix")
	}
	return report, nil
}

func inspectionFiles(sessionDir string, report *InspectionReport) ([]inspectionFile, error) {
	manifestPath := filepath.Join(sessionDir, manifestFilename)
	manifest, err := ReadSessionManifest(manifestPath)
	if err == nil {
		if manifest.FormatVersion != ManifestFormat1 && manifest.FormatVersion != ManifestFormatPrototype1 {
			return nil, fmt.Errorf("unsupported capture manifest format %q", manifest.FormatVersion)
		}
		report.ManifestPresent = true
		report.SessionID = manifest.SessionID
		report.Label = manifest.Label
		report.Status = manifest.Status
		report.CaptureBuild = manifest.Build
		files := make([]inspectionFile, 0, len(manifest.Files))
		for _, manifestFile := range manifest.Files {
			path, err := resolveInspectionPath(sessionDir, manifestFile.Path)
			if err != nil {
				return nil, fmt.Errorf("manifest file %q: %w", manifestFile.Path, err)
			}
			files = append(files, inspectionFile{kind: manifestFile.Kind, path: path, rel: filepath.ToSlash(manifestFile.Path)})
		}
		return files, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	report.Warnings = append(report.Warnings, "manifest.json missing; inspecting legacy capture by filename discovery")
	files := []inspectionFile{{kind: ManifestFileProvider, path: filepath.Join(sessionDir, recordsFilename), rel: recordsFilename}}
	lifecyclePath := filepath.Join(sessionDir, lifecycleFilename)
	if _, lifecycleErr := os.Lstat(lifecyclePath); lifecycleErr == nil {
		files = append(files, inspectionFile{kind: ManifestFileLifecycle, path: lifecyclePath, rel: lifecycleFilename})
	} else if !errors.Is(lifecycleErr, os.ErrNotExist) {
		return nil, fmt.Errorf("stat legacy lifecycle capture: %w", lifecycleErr)
	}
	mcpPaths, globErr := filepath.Glob(filepath.Join(sessionDir, "mcp", "*.jsonl"))
	if globErr != nil {
		return nil, fmt.Errorf("find legacy MCP captures: %w", globErr)
	}
	sort.Strings(mcpPaths)
	for _, path := range mcpPaths {
		relative, relErr := filepath.Rel(sessionDir, path)
		if relErr != nil {
			return nil, fmt.Errorf("resolve legacy MCP capture: %w", relErr)
		}
		files = append(files, inspectionFile{kind: ManifestFileMCP, path: path, rel: filepath.ToSlash(relative)})
	}
	provenancePaths, globErr := filepath.Glob(filepath.Join(sessionDir, "provenance", "*.jsonl"))
	if globErr != nil {
		return nil, fmt.Errorf("find legacy composition provenance: %w", globErr)
	}
	sort.Strings(provenancePaths)
	for _, path := range provenancePaths {
		relative, relErr := filepath.Rel(sessionDir, path)
		if relErr != nil {
			return nil, fmt.Errorf("resolve legacy composition provenance: %w", relErr)
		}
		files = append(files, inspectionFile{kind: ManifestFileProvenance, path: path, rel: filepath.ToSlash(relative)})
	}
	return files, nil
}

func resolveInspectionPath(sessionDir, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", errors.New("path must be non-empty and relative")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes capture session")
	}
	return filepath.Join(sessionDir, clean), nil
}

func validateInspectionManifestFiles(sessionDir string, report *InspectionReport, files []inspectionFile) error {
	if !report.ManifestPresent {
		return nil
	}
	manifest, err := ReadSessionManifest(filepath.Join(sessionDir, manifestFilename))
	if err != nil {
		return err
	}
	byPath := make(map[string]ManifestFile, len(manifest.Files))
	for _, file := range manifest.Files {
		normalized := filepath.ToSlash(file.Path)
		if _, duplicate := byPath[normalized]; duplicate {
			return fmt.Errorf("manifest contains duplicate file path %q", normalized)
		}
		switch file.Kind {
		case ManifestFileProvider, ManifestFileLifecycle, ManifestFileMCP, ManifestFileProvenance, ManifestFileEval:
		default:
			return fmt.Errorf("manifest file %q has unsupported kind %q", normalized, file.Kind)
		}
		byPath[normalized] = file
	}
	for _, file := range files {
		expected, ok := byPath[file.rel]
		if !ok {
			return fmt.Errorf("manifest inventory missing %s", file.rel)
		}
		info, err := os.Lstat(file.path)
		if err != nil {
			return fmt.Errorf("stat manifest file %s: %w", file.rel, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("manifest file %s is not a regular file", file.rel)
		}
		if info.Size() != expected.SizeBytes {
			return fmt.Errorf("manifest size mismatch for %s: got %d, want %d", file.rel, info.Size(), expected.SizeBytes)
		}
		opened, err := os.Open(file.path)
		if err != nil {
			return fmt.Errorf("open manifest file %s: %w", file.rel, err)
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, opened)
		closeErr := opened.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return fmt.Errorf("hash manifest file %s: %w", file.rel, err)
		}
		actual := hex.EncodeToString(hash.Sum(nil))
		if actual != expected.SHA256 {
			return fmt.Errorf("manifest hash mismatch for %s: got %s, want %s", file.rel, actual, expected.SHA256)
		}
		report.Integrity.ManifestFilesVerified++
	}
	return nil
}

func correlateToolEvidence(providerUses []inspectedProviderToolUse, providerResults map[string]inspectedProviderToolResult, calls []inspectedMCPCall) []ToolCorrelation {
	correlations := make([]ToolCorrelation, 0)
	usedCalls := make(map[int]bool)
	for _, providerUse := range providerUses {
		if !strings.HasPrefix(providerUse.name, "mcp__") {
			continue
		}
		match := -1
		argumentsEqual := false
		for index := range calls {
			if usedCalls[index] {
				continue
			}
			if providerUse.invocationID != "" && calls[index].invocationID != providerUse.invocationID {
				continue
			}
			if !providerAndMCPToolNamesMatch(providerUse.name, calls[index].name) {
				continue
			}
			if providerUse.argumentsJSON == calls[index].argumentsJSON {
				match = index
				argumentsEqual = true
				break
			}
			if match == -1 {
				match = index
			}
		}
		if match == -1 {
			continue
		}
		call := calls[match]
		usedCalls[match] = true
		correlation := ToolCorrelation{
			InvocationID:     providerUse.invocationID,
			ClaudeSessionID:  providerUse.claudeSessionID,
			ProviderToolName: providerUse.name,
			MCPToolName:      call.name,
			ArgumentsJSON:    providerUse.argumentsJSON,
			ArgumentsEqual:   argumentsEqual,
			MCPRequestID:     call.requestID,
			ProviderSource:   providerUse.source,
			MCPCallSource:    call.source,
		}
		if call.result != nil {
			correlation.MCPResultText = call.result.text
			correlation.MCPResultBytes = len([]byte(call.result.text))
			correlation.MCPIsError = call.result.isError
			correlation.MCPResultSource = call.result.source
			if providerResult, ok := providerResults[providerUse.toolUseID]; ok {
				correlation.ProviderResultObserved = providerResult.text == call.result.text && providerResult.isError == call.result.isError
				if correlation.ProviderResultObserved {
					correlation.ProviderResultSource = providerResult.source
				}
			}
		}
		correlations = append(correlations, correlation)
	}
	return correlations
}

func providerAndMCPToolNamesMatch(providerName, mcpName string) bool {
	return strings.HasSuffix(providerName, "__"+mcpName)
}

// RenderInspection writes a compact human view. Arguments and result previews
// are bounded; byte counts and raw evidence links remain explicit.
func RenderInspection(writer io.Writer, report *InspectionReport) error {
	if err := renderFullInspectionHeader(writer, report); err != nil {
		return err
	}
	if err := renderFullClaudeSessions(writer, report); err != nil {
		return err
	}
	if err := renderFullContexts(writer, report); err != nil {
		return err
	}
	if err := renderFullEvalRuns(writer, report); err != nil {
		return err
	}
	if err := renderFullMCPStreams(writer, report); err != nil {
		return err
	}
	return renderFullCorrelations(writer, report.Correlations)
}

func renderEvidence(writer io.Writer, label string, evidence RawEvidence) error {
	sequence := fmt.Sprintf("seq %d", evidence.SeqStart)
	if evidence.SeqEnd > evidence.SeqStart {
		sequence = fmt.Sprintf("seq %d-%d", evidence.SeqStart, evidence.SeqEnd)
	}
	extra := ""
	if evidence.ExchangeID != "" {
		extra = " exchange=" + evidence.ExchangeID
	}
	if evidence.StreamOffset > 0 {
		extra += fmt.Sprintf(" offset=%d", evidence.StreamOffset)
	}
	if evidence.DecodedOffset > 0 {
		extra += fmt.Sprintf(" decodedOffset=%d", evidence.DecodedOffset)
	}
	if _, err := fmt.Fprintf(writer, "   source[%s]: %s %s%s\n", label, evidence.File, sequence, extra); err != nil {
		return fmt.Errorf("render %s evidence: %w", label, err)
	}
	return nil
}

func equalityLabel(equal bool) string {
	if equal {
		return "exact"
	}
	return "DIFFERS"
}

func inspectionPreview(value string, limit int) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\r", "\\r")
	value = strings.ReplaceAll(value, "\n", "\\n")
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + fmt.Sprintf("… [%d bytes total]", len([]byte(value)))
}

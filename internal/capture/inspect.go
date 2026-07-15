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
	inspectionStatusExact          = "exact"
)

var errInspectionIdentityMismatch = errors.New("capture identity mismatch")

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
	ExchangeID                       string
	ClaudeSessionID                  string
	Model                            string
	ProviderMessageID                string
	RequestBytes                     int
	SystemBlocks                     int
	SystemBytes                      int
	ToolCount                        int
	MCPToolCount                     int
	BuiltInToolCount                 int
	ToolBytes                        int
	MessageCount                     int
	MessageBytes                     int
	CommonPrefixMessages             int
	AddedMessages                    int
	AddedMessageBytes                int
	RemovedMessages                  int
	RewrittenMessages                int
	ContextReset                     bool
	HistoryRewritten                 bool
	SystemChanged                    bool
	ToolsChanged                     bool
	InputTokens                      int64
	InputTokensObserved              bool
	CacheCreationInputTokens         int64
	CacheCreationInputTokensObserved bool
	CacheReadInputTokens             int64
	CacheReadInputTokensObserved     bool
	OutputTokens                     int64
	OutputTokensObserved             bool
	Source                           RawEvidence
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
	ProviderToolUseID      string
	ProviderToolName       string
	MCPToolName            string
	ArgumentsJSON          string
	ArgumentsEqual         bool
	MCPRequestID           string
	MCPResultText          string
	MCPResultBytes         int
	MCPIsError             bool
	ProviderResultObserved bool
	ProviderResultStatus   string
	ProviderResultText     string
	ProviderResultIsError  bool
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
	entry, err := os.Lstat(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("stat capture session: %w", err)
	}
	if entry.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("capture session %q is a symlink", sessionDir)
	}
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
	if err := validateInspectionFiles(sessionDir, report, files); err != nil {
		return nil, err
	}

	fileSet, err := classifyInspectionFiles(files)
	if err != nil {
		return nil, err
	}
	provider, err := inspectSessionProvider(report, fileSet.provider)
	if err != nil {
		return nil, err
	}
	lifecycleStatus, lifecycleComplete, err := inspectSessionLifecycle(report, fileSet.lifecycle, provider.exchanges)
	if err != nil {
		return nil, err
	}
	calls, err := inspectSessionMCP(report, fileSet.mcp)
	if err != nil {
		return nil, err
	}
	compositionRecords, err := inspectSessionProvenance(report, fileSet.provenance)
	if err != nil {
		return nil, err
	}
	if err := addInspectionCorrelations(report, provider, calls, compositionRecords); err != nil {
		return nil, err
	}
	finalizeInspectionIntegrity(report, provider.status, provider.complete, lifecycleStatus, lifecycleComplete)
	return report, nil
}

type inspectionFileSet struct {
	provider   inspectionFile
	lifecycle  inspectionFile
	mcp        []inspectionFile
	provenance []inspectionFile
}

func classifyInspectionFiles(files []inspectionFile) (inspectionFileSet, error) {
	var set inspectionFileSet
	for _, file := range files {
		switch file.kind {
		case ManifestFileProvider:
			if set.provider.path != "" {
				return inspectionFileSet{}, errors.New("capture contains multiple provider files")
			}
			set.provider = file
		case ManifestFileLifecycle:
			if set.lifecycle.path != "" {
				return inspectionFileSet{}, errors.New("capture contains multiple lifecycle files")
			}
			set.lifecycle = file
		case ManifestFileMCP:
			set.mcp = append(set.mcp, file)
		case ManifestFileProvenance:
			set.provenance = append(set.provenance, file)
		}
	}
	if set.provider.path == "" {
		return inspectionFileSet{}, errors.New("capture provider.jsonl is missing")
	}
	return set, nil
}

func inspectSessionProvider(report *InspectionReport, file inspectionFile) (*providerInspectionData, error) {
	provider, err := inspectProviderFile(file.path, file.rel, report.SessionID)
	if err != nil {
		if report.Status != CaptureUnclean || errors.Is(err, errInspectionIdentityMismatch) {
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
	return provider, nil
}

func inspectSessionLifecycle(report *InspectionReport, file inspectionFile, exchanges []inspectedProviderExchange) (string, bool, error) {
	if file.path == "" {
		return CaptureComplete, true, nil
	}
	lifecycle, err := inspectLifecycleFile(file.path, file.rel, report.SessionID, exchanges)
	if err != nil {
		if report.Status != CaptureUnclean || errors.Is(err, errInspectionIdentityMismatch) {
			return "", false, fmt.Errorf("inspect lifecycle capture: %w", err)
		}
		report.Warnings = append(report.Warnings, "unclean lifecycle prefix is not protocol-complete: "+err.Error())
		lifecycle = &lifecycleInspectionData{status: CaptureUnclean}
	}
	report.EvalRuns = lifecycle.evalRuns
	report.Warnings = append(report.Warnings, lifecycle.warnings...)
	report.Integrity.LifecycleRecords = lifecycle.recordCount
	if report.Status != lifecycle.status {
		return "", false, fmt.Errorf("manifest status %q differs from lifecycle terminal status %q", report.Status, lifecycle.status)
	}
	return lifecycle.status, lifecycle.complete, nil
}

func inspectSessionMCP(report *InspectionReport, files []inspectionFile) ([]inspectedMCPCall, error) {
	var calls []inspectedMCPCall
	for _, file := range files {
		stream, err := inspectMCPFile(file.path, file.rel, report.SessionID)
		if err != nil {
			if report.Status != CaptureUnclean || errors.Is(err, errInspectionIdentityMismatch) {
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
	return calls, nil
}

func inspectSessionProvenance(report *InspectionReport, files []inspectionFile) ([]inspectedCompositionRecord, error) {
	var compositionRecords []inspectedCompositionRecord
	for _, file := range files {
		records, err := inspectCompositionFile(file.path, file.rel, report.SessionID)
		if err != nil {
			return nil, fmt.Errorf("inspect composition provenance %s: %w", file.rel, err)
		}
		report.Integrity.ProvenanceRecords += len(records)
		compositionRecords = append(compositionRecords, records...)
	}
	return compositionRecords, nil
}

func addInspectionCorrelations(report *InspectionReport, provider *providerInspectionData, calls []inspectedMCPCall, compositionRecords []inspectedCompositionRecord) error {
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
			return fmt.Errorf("match composition provenance for tool correlation %d: %w", index+1, matchErr)
		}
		report.Correlations[index].CompositionMatches = matches
	}
	return nil
}

func finalizeInspectionIntegrity(report *InspectionReport, providerStatus string, providerComplete bool, lifecycleStatus string, lifecycleComplete bool) {
	report.Integrity.Valid = true
	report.Integrity.Complete = report.Status == CaptureComplete && providerStatus == CaptureComplete && providerComplete && lifecycleStatus == CaptureComplete && lifecycleComplete
	for _, stream := range report.MCPStreams {
		if stream.Status != CaptureComplete {
			report.Integrity.Complete = false
		}
	}
	if !report.Integrity.Complete {
		report.Warnings = append(report.Warnings, "capture evidence is partial or unclean; hashes validate only the recorded prefix")
	}
}

func inspectionFiles(sessionDir string, report *InspectionReport) ([]inspectionFile, error) {
	manifestPath := filepath.Join(sessionDir, manifestFilename)
	manifestInfo, statErr := os.Lstat(manifestPath)
	if statErr == nil && manifestInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("manifest.json is a symlink")
	}
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("stat capture manifest: %w", statErr)
	}
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
	providerPath, err := resolveInspectionPath(sessionDir, recordsFilename)
	if err != nil {
		return nil, fmt.Errorf("resolve legacy provider capture: %w", err)
	}
	files := []inspectionFile{{kind: ManifestFileProvider, path: providerPath, rel: recordsFilename}}
	lifecyclePath, err := resolveInspectionPath(sessionDir, lifecycleFilename)
	if err != nil {
		return nil, fmt.Errorf("resolve legacy lifecycle capture: %w", err)
	}
	if _, lifecycleErr := os.Lstat(lifecyclePath); lifecycleErr == nil {
		files = append(files, inspectionFile{kind: ManifestFileLifecycle, path: lifecyclePath, rel: lifecycleFilename})
	} else if !errors.Is(lifecycleErr, os.ErrNotExist) {
		return nil, fmt.Errorf("stat legacy lifecycle capture: %w", lifecycleErr)
	}
	mcpFiles, err := legacyInspectionFilesInDir(sessionDir, "mcp", ManifestFileMCP)
	if err != nil {
		return nil, fmt.Errorf("find legacy MCP captures: %w", err)
	}
	files = append(files, mcpFiles...)
	provenanceFiles, err := legacyInspectionFilesInDir(sessionDir, "provenance", ManifestFileProvenance)
	if err != nil {
		return nil, fmt.Errorf("find legacy composition provenance: %w", err)
	}
	files = append(files, provenanceFiles...)
	return files, nil
}

func legacyInspectionFilesInDir(sessionDir, relativeDir, kind string) ([]inspectionFile, error) {
	directory, err := resolveInspectionPath(sessionDir, relativeDir)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("legacy evidence path %q is not a directory", relativeDir)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	files := make([]inspectionFile, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		relative := filepath.Join(relativeDir, entry.Name())
		path, err := resolveInspectionPath(sessionDir, relative)
		if err != nil {
			return nil, err
		}
		fileInfo, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !fileInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("legacy evidence file %q is not a regular file", filepath.ToSlash(relative))
		}
		files = append(files, inspectionFile{kind: kind, path: path, rel: filepath.ToSlash(relative)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
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
	root, err := filepath.Abs(sessionDir)
	if err != nil {
		return "", fmt.Errorf("resolve capture session: %w", err)
	}
	current := root
	parts := strings.Split(clean, string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) && index == len(parts)-1 {
			return current, nil
		}
		if statErr != nil {
			return "", fmt.Errorf("inspect path component %s: %w", current, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path component %s is a symlink", current)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("path component %s is not a directory", current)
		}
	}
	return current, nil
}

func validateInspectionFiles(sessionDir string, report *InspectionReport, files []inspectionFile) error {
	for _, file := range files {
		info, err := os.Lstat(file.path)
		if err != nil {
			return fmt.Errorf("stat capture file %s: %w", file.rel, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("capture file %s is not a regular file", file.rel)
		}
	}
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
		info, err := os.Stat(file.path)
		if err != nil {
			return fmt.Errorf("stat manifest file %s: %w", file.rel, err)
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
		for index := range calls {
			if usedCalls[index] {
				continue
			}
			if providerUse.invocationID != "" && calls[index].invocationID != providerUse.invocationID {
				continue
			}
			if !providerAndMCPToolNamesMatch(providerUse.name, calls[index].name) || providerUse.argumentsJSON != calls[index].argumentsJSON {
				continue
			}
			if match != -1 {
				// More than one unused call has the same name and arguments. The
				// available evidence does not identify which execution belongs to
				// this provider proposal, so leave it unmatched rather than pairing
				// by incidental order.
				match = -1
				break
			}
			match = index
		}
		if match < 0 || match >= len(calls) {
			continue
		}
		call := calls[match]
		usedCalls[match] = true
		correlation := ToolCorrelation{
			InvocationID:         providerUse.invocationID,
			ClaudeSessionID:      providerUse.claudeSessionID,
			ProviderToolUseID:    providerUse.toolUseID,
			ProviderToolName:     providerUse.name,
			ProviderResultStatus: "ambiguous",
			MCPToolName:          call.name,
			ArgumentsJSON:        providerUse.argumentsJSON,
			ArgumentsEqual:       true,
			MCPRequestID:         call.requestID,
			ProviderSource:       providerUse.source,
			MCPCallSource:        call.source,
		}
		if call.result != nil {
			correlation.ProviderResultStatus = "missing"
			correlation.MCPResultText = call.result.text
			correlation.MCPResultBytes = len([]byte(call.result.text))
			correlation.MCPIsError = call.result.isError
			correlation.MCPResultSource = call.result.source
			if providerResult, ok := providerResults[providerUse.toolUseID]; ok {
				correlation.ProviderResultStatus = "different"
				correlation.ProviderResultText = providerResult.text
				correlation.ProviderResultIsError = providerResult.isError
				correlation.ProviderResultObserved = providerResult.canonical != "" && providerResult.canonical == call.result.canonical
				correlation.ProviderResultSource = providerResult.source
				if correlation.ProviderResultObserved {
					correlation.ProviderResultStatus = inspectionStatusExact
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
		return inspectionStatusExact
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

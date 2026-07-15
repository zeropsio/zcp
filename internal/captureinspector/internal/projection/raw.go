package projection

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/capture"
)

const (
	defaultArtifactLimit = 1 << 20
	maxRawRecordPage     = 1_000
	maxRawDetailBody     = 1 << 20
	maxToolDetailText    = 1 << 20
	maxScanDepth         = 6
	maxScanDirectories   = 10_000
	maxScanCaptures      = 1_000
)

func ScanRoot(ctx context.Context, root string) ([]CaptureIndexEntry, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve capture root: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	} else if !errors.Is(resolveErr, os.ErrNotExist) {
		return nil, fmt.Errorf("resolve capture root symlinks: %w", resolveErr)
	}
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return []CaptureIndexEntry{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("read capture root: %w", err)
	}
	views := make([]CaptureIndexEntry, 0)
	directories := 0
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return filepath.SkipDir
		}
		directories++
		if directories > maxScanDirectories {
			return fmt.Errorf("capture root exceeds %d-directory scan limit", maxScanDirectories)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		depth := 0
		if relative != "." {
			depth = strings.Count(relative, string(filepath.Separator)) + 1
		}
		if depth > maxScanDepth {
			return filepath.SkipDir
		}
		manifestPath := filepath.Join(path, "manifest.json")
		manifestInfo, err := os.Lstat(manifestPath)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if manifestInfo.Mode()&os.ModeSymlink != 0 {
			return filepath.SkipDir
		}
		if !manifestInfo.Mode().IsRegular() {
			return fmt.Errorf("capture manifest %s is not a regular file", manifestPath)
		}
		manifest, err := capture.ReadSessionManifest(manifestPath)
		if err != nil {
			return fmt.Errorf("read capture manifest %s: %w", manifestPath, err)
		}
		views = append(views, captureIndexEntry(path, manifest))
		if len(views) > maxScanCaptures {
			return fmt.Errorf("capture root exceeds %d-capture scan limit", maxScanCaptures)
		}
		return filepath.SkipDir
	})
	if err != nil {
		return nil, fmt.Errorf("scan capture root: %w", err)
	}
	seenIDs := make(map[string]string, len(views))
	for _, view := range views {
		if previous := seenIDs[view.ID]; previous != "" {
			return nil, fmt.Errorf("duplicate capture ID %q in %s and %s", view.ID, previous, view.SessionPath)
		}
		seenIDs[view.ID] = view.SessionPath
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].StartedAt.Equal(views[j].StartedAt) {
			return views[i].ID < views[j].ID
		}
		return views[i].StartedAt.After(views[j].StartedAt)
	})
	return views, nil
}

func captureIndexEntry(sessionDir string, manifest *capture.SessionManifestDocument) CaptureIndexEntry {
	value := CaptureIndexEntry{
		ID: manifest.SessionID, Label: manifest.Label, Status: manifest.Status, Integrity: "not-verified",
		StartedAt: manifest.StartedAt, BuildVersion: manifest.Build.Version, BuildCommit: manifest.Build.Commit,
		Plaintext: manifest.Plaintext, SessionPath: sessionDir,
	}
	if manifest.EndedAt != nil {
		value.EndedAt = *manifest.EndedAt
		value.DurationMs = manifest.EndedAt.Sub(manifest.StartedAt).Milliseconds()
	}
	for _, file := range manifest.Files {
		value.SizeBytes += file.SizeBytes
	}
	if manifest.Status == capture.CaptureRunning {
		value.Integrity = "running"
	}
	return value
}

func FindSession(ctx context.Context, root, id string) (string, error) {
	entries, err := ScanRoot(ctx, root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.ID == id {
			return entry.SessionPath, nil
		}
	}
	return "", fmt.Errorf("capture %q not found", id)
}

func ReadRawRecordPage(sessionDir, relative string, after uint64, limit int) (*RawRecordPage, error) {
	if limit <= 0 {
		limit = 250
	}
	if limit > maxRawRecordPage {
		limit = maxRawRecordPage
	}
	kind, path, err := inventoriedFile(sessionDir, relative)
	if err != nil {
		return nil, err
	}
	manifest, err := capture.ReadSessionManifest(filepath.Join(sessionDir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	page := &RawRecordPage{FormatVersion: FormatVersion1, CaptureID: manifest.SessionID, File: relative, After: after, Limit: limit, Items: []RawRecordSummary{}}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	switch kind {
	case capture.ManifestFileProvider, capture.ManifestFileMCP:
		for {
			var record capture.Record
			if err := decoder.Decode(&record); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return nil, err
			}
			if record.Seq <= after {
				continue
			}
			page.Items = append(page.Items, rawSummaryFromRecord(relative, record))
			if len(page.Items) > limit {
				break
			}
		}
	case capture.ManifestFileLifecycle:
		for {
			var record capture.LifecycleRecord
			if err := decoder.Decode(&record); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return nil, err
			}
			if record.Seq <= after {
				continue
			}
			page.Items = append(page.Items, RawRecordSummary{
				ID: rawRecordID(relative, record.Seq), File: relative, Seq: record.Seq, Time: record.Time, Kind: record.Kind,
				EvalRunID: record.EvalRunID, ScenarioRunID: record.ScenarioRunID, InvocationID: record.InvocationID,
				Phase: record.Phase, CaptureStatus: record.Status, HasError: record.Error != "",
			})
			if len(page.Items) > limit {
				break
			}
		}
	case capture.ManifestFileProvenance:
		var seq uint64
		for {
			var record capture.CompositionRecord
			if err := decoder.Decode(&record); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return nil, err
			}
			seq++
			if seq <= after {
				continue
			}
			page.Items = append(page.Items, RawRecordSummary{ID: rawRecordID(relative, seq), File: relative, Seq: seq, Time: record.Time, Kind: "composition", ProcessID: record.ProcessID, BodyBytes: int64(record.OutputBytes)})
			if len(page.Items) > limit {
				break
			}
		}
	default:
		return nil, fmt.Errorf("file %q is not a raw record stream", relative)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.HasMore = true
	}
	if len(page.Items) > 0 {
		page.NextAfter = page.Items[len(page.Items)-1].Seq
	} else {
		page.NextAfter = after
	}
	return page, nil
}

func rawSummaryFromRecord(file string, record capture.Record) RawRecordSummary {
	return RawRecordSummary{
		ID: rawRecordID(file, record.Seq), File: file, Seq: record.Seq, Time: record.Time, Kind: record.Kind,
		ExchangeID: record.ExchangeID, ProcessID: record.ProcessID, EvalRunID: record.EvalRunID,
		ScenarioRunID: record.ScenarioRunID, InvocationID: record.InvocationID, Phase: record.Phase,
		Direction: record.Direction, BodyBytes: record.BodyBytes, StreamOffset: record.StreamOffset,
		StatusCode: record.StatusCode, CaptureStatus: record.CaptureStatus,
		HasBody: record.BodyBase64 != "", HasError: record.Error != "",
	}
}

func ReadRawDetail(sessionDir, relative string, seq uint64) (*RawDetail, error) {
	kind, path, err := inventoriedFile(sessionDir, relative)
	if err != nil {
		return nil, err
	}
	detail := &RawDetail{File: relative}
	switch kind {
	case capture.ManifestFileProvider, capture.ManifestFileMCP:
		records, err := capture.ReadRecords(path)
		if err != nil {
			return nil, err
		}
		for index := range records {
			if records[index].Seq != seq {
				continue
			}
			recordCopy := records[index]
			recordCopy.BodyBase64 = ""
			detail.Record = &recordCopy
			if records[index].BodyBase64 != "" {
				decoded, decodeErr := base64.StdEncoding.DecodeString(records[index].BodyBase64)
				if decodeErr != nil {
					return nil, fmt.Errorf("decode body at seq %d: %w", seq, decodeErr)
				}
				preview := decoded
				if len(preview) > maxRawDetailBody {
					preview = preview[:maxRawDetailBody]
					detail.BodyTruncated = true
				}
				if utf8.Valid(decoded) {
					for len(preview) > 0 && !utf8.Valid(preview) {
						preview = preview[:len(preview)-1]
					}
					detail.BodyText = string(preview)
				} else {
					detail.BodyBase64 = base64.StdEncoding.EncodeToString(preview)
				}
			}
			return detail, nil
		}
	case capture.ManifestFileLifecycle:
		records, err := capture.ReadLifecycleRecords(path)
		if err != nil {
			return nil, err
		}
		for index := range records {
			if records[index].Seq == seq {
				detail.Lifecycle = &records[index]
				return detail, nil
			}
		}
	case capture.ManifestFileProvenance:
		records, err := capture.ReadCompositionRecords(path)
		if err != nil {
			return nil, err
		}
		if seq > 0 && seq <= uint64(len(records)) {
			detail.Composition = &records[seq-1]
			return detail, nil
		}
	default:
		return nil, fmt.Errorf("file %q is not a raw record stream", relative)
	}
	return nil, fmt.Errorf("record %d not found in %s", seq, relative)
}

func ReadArtifactLine(sessionDir, relative string, lineNumber uint64, limit int64) (*ArtifactLineDetail, error) {
	if lineNumber == 0 {
		return nil, errors.New("artifact line must be positive")
	}
	if limit <= 0 || limit > defaultArtifactLimit {
		limit = defaultArtifactLimit
	}
	kind, path, err := inventoriedFile(sessionDir, relative)
	if err != nil {
		return nil, err
	}
	if kind != capture.ManifestFileEval {
		return nil, fmt.Errorf("file %q is not an eval artifact", relative)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	var current uint64
	for {
		current++
		var content bytes.Buffer
		truncated := false
		for {
			part, isPrefix, readErr := reader.ReadLine()
			if current == lineNumber {
				remaining := int(limit) - content.Len()
				if remaining > 0 {
					if len(part) > remaining {
						_, _ = content.Write(part[:remaining])
						truncated = true
					} else {
						_, _ = content.Write(part)
					}
				} else if len(part) > 0 {
					truncated = true
				}
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					if current == lineNumber && content.Len() > 0 {
						return &ArtifactLineDetail{Path: relative, Line: lineNumber, Content: content.String(), Truncated: truncated}, nil
					}
					return nil, fmt.Errorf("line %d not found in %s", lineNumber, relative)
				}
				return nil, readErr
			}
			if !isPrefix {
				break
			}
		}
		if current == lineNumber {
			return &ArtifactLineDetail{Path: relative, Line: lineNumber, Content: content.String(), Truncated: truncated}, nil
		}
	}
}

func ReadArtifact(sessionDir, relative string, limit int64) (*ArtifactDetail, error) {
	kind, path, err := inventoriedFile(sessionDir, relative)
	if err != nil {
		return nil, err
	}
	if kind != capture.ManifestFileEval {
		return nil, fmt.Errorf("file %q is not an eval artifact", relative)
	}
	if limit <= 0 || limit > defaultArtifactLimit {
		limit = defaultArtifactLimit
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat artifact: %w", err)
	}
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	truncated := int64(len(content)) > limit
	if truncated {
		content = content[:limit]
	}
	return &ArtifactDetail{Path: relative, Type: artifactType(relative), SizeBytes: info.Size(), Content: string(bytes.ToValidUTF8(content, []byte("�"))), Truncated: truncated}, nil
}

func ReadToolDetail(sessionDir string, index int) (*ToolDetail, error) {
	report, err := capture.InspectSession(sessionDir)
	if err != nil {
		return nil, err
	}
	if index < 1 || index > len(report.Correlations) {
		return nil, fmt.Errorf("tool execution %d not found", index)
	}
	correlation := report.Correlations[index-1]
	propagation := correlation.ProviderResultStatus
	if propagation == "" {
		propagation = propagationMissing
	}
	detail := &ToolDetail{
		ID: fmt.Sprintf("tool:%06d", index), Category: toolCategoryMCP, ToolName: correlation.MCPToolName, ArgumentsJSON: correlation.ArgumentsJSON,
		ResultText: correlation.MCPResultText, IsError: correlation.MCPIsError,
		MCPResultText: correlation.MCPResultText, MCPIsError: correlation.MCPIsError,
		ProviderResultText: correlation.ProviderResultText, ProviderResultError: correlation.ProviderResultIsError,
		Propagation:   propagation,
		SourceMatches: append([]capture.ContentSourceMatch(nil), correlation.SourceMatches...),
		Composition:   append([]capture.CompositionSourceMatch(nil), correlation.CompositionMatches...),
	}
	detail.ArgumentsJSON, detail.ArgumentsTruncated = boundedDetailText(detail.ArgumentsJSON)
	detail.ResultText, detail.ResultTruncated = boundedDetailText(detail.ResultText)
	detail.MCPResultText = detail.ResultText
	detail.ProviderResultText, detail.ProviderResultTruncated = boundedDetailText(detail.ProviderResultText)
	for _, source := range []capture.RawEvidence{correlation.ProviderSource, correlation.MCPCallSource, correlation.MCPResultSource, correlation.ProviderResultSource} {
		if source.File != "" {
			detail.Evidence = append(detail.Evidence, evidenceFromRaw(source))
		}
	}
	return detail, nil
}

func ReadToolExecutionDetail(sessionDir string, execution ToolExecution) (*ToolDetail, error) {
	switch execution.Category {
	case toolCategoryMCP:
		var index int
		if _, err := fmt.Sscanf(execution.ID, "tool:%d", &index); err != nil || index < 1 {
			return nil, fmt.Errorf("invalid MCP tool execution ID %q", execution.ID)
		}
		return ReadToolDetail(sessionDir, index)
	case "builtin":
		return readClientToolDetail(sessionDir, execution)
	default:
		return nil, fmt.Errorf("unsupported tool category %q", execution.Category)
	}
}

func readClientToolDetail(sessionDir string, execution ToolExecution) (*ToolDetail, error) {
	if execution.ClientArtifact == "" || execution.ToolUseID == "" {
		return nil, errors.New("built-in tool execution lacks artifact coordinates")
	}
	kind, path, err := inventoriedFile(sessionDir, execution.ClientArtifact)
	if err != nil {
		return nil, err
	}
	if kind != capture.ManifestFileEval {
		return nil, fmt.Errorf("built-in tool artifact %q is not eval evidence", execution.ClientArtifact)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	detail := &ToolDetail{ID: execution.ID, Category: "builtin", ToolName: execution.ToolName, Propagation: "pending-client-result", Evidence: []EvidenceRef{}}
	reader := bufio.NewReader(file)
	lineNumber := uint64(0)
	foundUse := false
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			lineNumber++
			var event clientStreamEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return nil, fmt.Errorf("decode %s line %d: %w", execution.ClientArtifact, lineNumber, err)
			}
			blocks, err := decodeClientBlocks(event.Message.Content)
			if err != nil {
				return nil, fmt.Errorf("decode %s line %d content: %w", execution.ClientArtifact, lineNumber, err)
			}
			for _, block := range blocks {
				evidence := EvidenceRef{ID: rawRecordID(execution.ClientArtifact, lineNumber), File: execution.ClientArtifact, SeqStart: lineNumber, SeqEnd: lineNumber}
				if block.Type == blockTypeToolUse && block.ID == execution.ToolUseID {
					foundUse = true
					detail.ToolName = block.Name
					detail.ArgumentsJSON = compactRawJSON(block.Input)
					detail.Evidence = append(detail.Evidence, evidence)
				}
				if block.Type == blockTypeToolResult && block.ToolUseID == execution.ToolUseID {
					detail.ResultText = rawContentText(block.Content)
					detail.IsError = block.IsError
					detail.Propagation = "client-result"
					detail.Evidence = append(detail.Evidence, evidence)
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, readErr
		}
	}
	if !foundUse {
		return nil, fmt.Errorf("built-in tool use %q not found in %s", execution.ToolUseID, execution.ClientArtifact)
	}
	detail.ArgumentsJSON, detail.ArgumentsTruncated = boundedDetailText(detail.ArgumentsJSON)
	detail.ResultText, detail.ResultTruncated = boundedDetailText(detail.ResultText)
	return detail, nil
}

func boundedDetailText(value string) (string, bool) {
	if len(value) <= maxToolDetailText {
		return value, false
	}
	preview := value[:maxToolDetailText]
	for len(preview) > 0 && !utf8.ValidString(preview) {
		preview = preview[:len(preview)-1]
	}
	return preview, true
}

func compactRawJSON(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "{}"
	}
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil {
		return string(raw)
	}
	return compact.String()
}

func rawContentText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return compactRawJSON(raw)
}

func inventoriedFile(sessionDir, relative string) (string, string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", "", errors.New("capture file path must be non-empty and relative")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", "", errors.New("capture file path escapes session")
	}
	manifest, err := capture.ReadSessionManifest(filepath.Join(sessionDir, "manifest.json"))
	if err != nil {
		return "", "", err
	}
	var expected *capture.ManifestFile
	for index := range manifest.Files {
		if filepath.ToSlash(manifest.Files[index].Path) == clean {
			expected = &manifest.Files[index]
			break
		}
	}
	if expected == nil {
		return "", "", fmt.Errorf("file %q is not inventoried by the capture manifest", relative)
	}
	path, err := resolveFile(sessionDir, clean)
	if err != nil {
		return "", "", err
	}
	if err := verifyInventoriedFile(path, clean, *expected); err != nil {
		return "", "", err
	}
	return expected.Kind, path, nil
}

// Detail queries re-open canonical files after the projected view may have been
// cached. Re-check the selected manifest entry at that boundary so a valid
// cached view can never authorize bytes modified after projection.
func verifyInventoriedFile(path, relative string, expected capture.ManifestFile) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open manifest file %s: %w", relative, err)
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return fmt.Errorf("stat manifest file %s: %w", relative, statErr)
	}
	if info.Size() != expected.SizeBytes {
		_ = file.Close()
		return fmt.Errorf("manifest size mismatch for %s: got %d, want %d", relative, info.Size(), expected.SizeBytes)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return fmt.Errorf("hash manifest file %s: %w", relative, err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected.SHA256 {
		return fmt.Errorf("manifest hash mismatch for %s: got %s, want %s", relative, actual, expected.SHA256)
	}
	return nil
}

func resolveFile(sessionDir, relative string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes capture session")
	}
	root, err := filepath.Abs(sessionDir)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, clean)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if filepath.Clean(resolved) != filepath.Clean(path) {
		return "", fmt.Errorf("capture path %q traverses a symlink", relative)
	}
	relativeResolved, err := filepath.Rel(root, resolved)
	if err != nil || relativeResolved == ".." || strings.HasPrefix(relativeResolved, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes capture session")
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("capture path %q is not a regular file", relative)
	}
	return resolved, nil
}

package capture

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	lifecycleFilename = "lifecycle.jsonl"

	LifecycleStreamStart     = "lifecycle.stream.start"
	LifecycleStreamEnd       = "lifecycle.stream.end"
	LifecycleEvalRunStart    = "eval.run.start"
	LifecycleEvalRunEnd      = "eval.run.end"
	LifecycleScenarioStart   = "eval.scenario.start"
	LifecycleScenarioEnd     = "eval.scenario.end"
	LifecycleInvocationStart = "eval.invocation.start"
	LifecycleInvocationBind  = "eval.invocation.bind"
	LifecycleInvocationEnd   = "eval.invocation.end"
	LifecycleArtifact        = "eval.artifact"
)

// LifecycleMarker is a capture-only declaration emitted by an orchestrator.
// It never crosses the provider or MCP protocol boundaries.
type LifecycleMarker struct {
	Kind            string `json:"kind"`
	EvalRunID       string `json:"evalRunId,omitempty"`
	ScenarioRunID   string `json:"scenarioRunId,omitempty"`
	InvocationID    string `json:"invocationId,omitempty"`
	Phase           string `json:"phase,omitempty"`
	ClaudeSessionID string `json:"claudeSessionId,omitempty"`
	Status          string `json:"status,omitempty"`
	Error           string `json:"error,omitempty"`
	ArtifactPath    string `json:"artifactPath,omitempty"`
}

// LifecycleRecord adds recorder-owned ordering and observation time to one
// lifecycle declaration.
type LifecycleRecord struct {
	Seq       uint64    `json:"seq"`
	Time      time.Time `json:"time"`
	CaptureID string    `json:"captureId"`
	LifecycleMarker
}

// LifecycleRecorder is a low-volume, synchronously durable side-channel
// recorder. It is deliberately independent of the provider's non-blocking hot
// path: an eval marker may wait for disk, provider forwarding never does.
type LifecycleRecorder struct {
	mu        sync.Mutex
	captureID string
	path      string
	file      *os.File
	encoder   *json.Encoder
	nextSeq   uint64
	closed    bool
	closeErr  error
}

func NewLifecycleRecorder(sessionDir, captureID string) (*LifecycleRecorder, error) {
	if sessionDir == "" {
		return nil, errors.New("lifecycle session directory is required")
	}
	if captureID == "" {
		return nil, errors.New("lifecycle capture ID is required")
	}
	info, err := os.Stat(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("stat lifecycle session directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("lifecycle session path %q is not a directory", sessionDir)
	}
	path := filepath.Join(sessionDir, lifecycleFilename)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create lifecycle record file: %w", err)
	}
	recorder := &LifecycleRecorder{
		captureID: captureID,
		path:      path,
		file:      file,
		encoder:   json.NewEncoder(file),
	}
	if _, err := recorder.markLocked(LifecycleMarker{Kind: LifecycleStreamStart, Status: CaptureRunning}); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write lifecycle stream start: %w", err)
	}
	return recorder, nil
}

// Mark validates, timestamps, appends, and fsyncs one orchestration marker.
func (r *LifecycleRecorder) Mark(marker LifecycleMarker) (LifecycleRecord, error) {
	if err := validateLifecycleMarker(marker); err != nil {
		return LifecycleRecord{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return LifecycleRecord{}, errRecorderClosed
	}
	return r.markLocked(marker)
}

func (r *LifecycleRecorder) markLocked(marker LifecycleMarker) (LifecycleRecord, error) {
	r.nextSeq++
	record := LifecycleRecord{
		Seq:             r.nextSeq,
		Time:            time.Now().UTC(),
		CaptureID:       r.captureID,
		LifecycleMarker: marker,
	}
	if err := r.encoder.Encode(record); err != nil {
		return LifecycleRecord{}, fmt.Errorf("encode lifecycle seq %d: %w", record.Seq, err)
	}
	if err := r.file.Sync(); err != nil {
		return LifecycleRecord{}, fmt.Errorf("sync lifecycle seq %d: %w", record.Seq, err)
	}
	return record, nil
}

// Close writes the terminal lifecycle status and durably closes the stream.
func (r *LifecycleRecorder) Close(status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.closeErr
	}
	r.closed = true
	_, markErr := r.markLocked(LifecycleMarker{Kind: LifecycleStreamEnd, Status: status})
	syncErr := r.file.Sync()
	closeErr := r.file.Close()
	r.closeErr = errors.Join(markErr, syncErr, closeErr)
	if r.closeErr != nil {
		return fmt.Errorf("close lifecycle record file: %w", r.closeErr)
	}
	return nil
}

func validateLifecycleMarker(marker LifecycleMarker) error {
	if len(marker.Kind) == 0 || len(marker.Kind) > 128 {
		return errors.New("lifecycle marker kind is required and must be at most 128 bytes")
	}
	for name, value := range map[string]string{
		"evalRunId":       marker.EvalRunID,
		"scenarioRunId":   marker.ScenarioRunID,
		"invocationId":    marker.InvocationID,
		"phase":           marker.Phase,
		"claudeSessionId": marker.ClaudeSessionID,
	} {
		if len(value) > 512 {
			return fmt.Errorf("lifecycle %s exceeds 512 bytes", name)
		}
	}
	require := func(name, value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("lifecycle %s requires %s", marker.Kind, name)
		}
		return nil
	}
	switch marker.Kind {
	case LifecycleStreamStart, LifecycleStreamEnd:
		return nil
	case LifecycleEvalRunStart, LifecycleEvalRunEnd:
		return require("evalRunId", marker.EvalRunID)
	case LifecycleScenarioStart, LifecycleScenarioEnd:
		if err := require("evalRunId", marker.EvalRunID); err != nil {
			return err
		}
		return require("scenarioRunId", marker.ScenarioRunID)
	case LifecycleInvocationStart, LifecycleInvocationEnd:
		for _, field := range []struct{ name, value string }{
			{"evalRunId", marker.EvalRunID},
			{"scenarioRunId", marker.ScenarioRunID},
			{"invocationId", marker.InvocationID},
			{"phase", marker.Phase},
		} {
			if err := require(field.name, field.value); err != nil {
				return err
			}
		}
		return nil
	case LifecycleInvocationBind:
		for _, field := range []struct{ name, value string }{
			{"evalRunId", marker.EvalRunID},
			{"scenarioRunId", marker.ScenarioRunID},
			{"invocationId", marker.InvocationID},
			{"phase", marker.Phase},
			{"claudeSessionId", marker.ClaudeSessionID},
		} {
			if err := require(field.name, field.value); err != nil {
				return err
			}
		}
		return nil
	case LifecycleArtifact:
		if err := require("evalRunId", marker.EvalRunID); err != nil {
			return err
		}
		if err := require("scenarioRunId", marker.ScenarioRunID); err != nil {
			return err
		}
		return require("artifactPath", marker.ArtifactPath)
	default:
		return fmt.Errorf("unsupported lifecycle marker kind %q", marker.Kind)
	}
}

func (r *LifecycleRecorder) Path() string { return r.path }

func ReadLifecycleRecords(path string) ([]LifecycleRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open lifecycle records: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var records []LifecycleRecord
	for {
		var record LifecycleRecord
		if err := decoder.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode lifecycle record %d: %w", len(records)+1, err)
		}
		records = append(records, record)
	}
	return records, nil
}

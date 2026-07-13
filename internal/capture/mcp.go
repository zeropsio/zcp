package capture

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// MCPRecorderConfig identifies one opt-in ZCP MCP stdio stream inside a parent
// provider-capture session.
type MCPRecorderConfig struct {
	SessionDir    string
	SessionID     string
	ProcessID     int
	EvalRunID     string
	ScenarioRunID string
	InvocationID  string
	Phase         string
}

// MCPRecorder records exact bytes observed on one ZCP server's stdin/stdout.
type MCPRecorder struct {
	recorder      *Recorder
	processID     int
	evalRunID     string
	scenarioRunID string
	invocationID  string
	phase         string
	input         streamDigest
	output        streamDigest
	errMu         sync.Mutex
	err           error
}

type streamDigest struct {
	mu    sync.Mutex
	bytes int64
	hash  hash.Hash
}

func newStreamDigest() streamDigest { return streamDigest{hash: sha256.New()} }

func (d *streamDigest) add(chunk []byte) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	offset := d.bytes
	d.bytes += int64(len(chunk))
	_, _ = d.hash.Write(chunk)
	return offset
}

func (d *streamDigest) snapshot() (int64, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.bytes, hex.EncodeToString(d.hash.Sum(nil))
}

// NewMCPRecorder creates mcp/zcp-<pid>.jsonl in an existing private capture
// session. Existing files are never appended or overwritten.
func NewMCPRecorder(cfg MCPRecorderConfig) (*MCPRecorder, error) {
	if cfg.SessionDir == "" {
		return nil, errors.New("MCP capture session directory is required")
	}
	if cfg.SessionID == "" {
		return nil, errors.New("MCP capture session ID is required")
	}
	if cfg.ProcessID <= 0 {
		cfg.ProcessID = os.Getpid()
	}
	info, err := os.Stat(cfg.SessionDir)
	if err != nil {
		return nil, fmt.Errorf("stat MCP capture session directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("MCP capture session path %q is not a directory", cfg.SessionDir)
	}
	mcpDir := filepath.Join(cfg.SessionDir, "mcp")
	if err := os.Mkdir(mcpDir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create MCP capture directory: %w", err)
	}
	if err := os.Chmod(mcpDir, 0o700); err != nil {
		return nil, fmt.Errorf("set MCP capture directory permissions: %w", err)
	}
	path := filepath.Join(mcpDir, fmt.Sprintf("zcp-%d.jsonl", cfg.ProcessID))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create MCP capture file: %w", err)
	}

	m := &MCPRecorder{
		recorder:      newRecorder(file, cfg.SessionDir, path, cfg.SessionID, "", defaultQueueCapacity, 0),
		processID:     cfg.ProcessID,
		evalRunID:     cfg.EvalRunID,
		scenarioRunID: cfg.ScenarioRunID,
		invocationID:  cfg.InvocationID,
		phase:         cfg.Phase,
		input:         newStreamDigest(),
		output:        newStreamDigest(),
	}
	if err := m.record(Record{Kind: RecordMCPStreamStart}); err != nil {
		_ = m.recorder.closeWriter()
		return nil, fmt.Errorf("queue MCP stream start: %w", err)
	}
	return m, nil
}

// NewMCPRecorderFromEnvironment enables capture only when the parent wrapper
// propagated both required values. A half-configured opt-in is a visible error,
// never a silent uncaptured MCP session.
func NewMCPRecorderFromEnvironment() (*MCPRecorder, bool, error) {
	sessionID := os.Getenv(EnvSessionID)
	sessionDir := os.Getenv(EnvSessionDir)
	if sessionID == "" && sessionDir == "" {
		return nil, false, nil
	}
	if sessionID == "" || sessionDir == "" {
		return nil, true, fmt.Errorf("MCP capture requires both %s and %s", EnvSessionID, EnvSessionDir)
	}
	evalRunID := os.Getenv(EnvEvalRunID)
	scenarioRunID := os.Getenv(EnvScenarioRunID)
	invocationID := os.Getenv(EnvInvocationID)
	phase := os.Getenv(EnvInvocationPhase)
	metadataValues := []string{evalRunID, scenarioRunID, invocationID, phase}
	metadataPresent := 0
	for _, value := range metadataValues {
		if value != "" {
			metadataPresent++
		}
	}
	if metadataPresent != 0 && metadataPresent != len(metadataValues) {
		return nil, true, fmt.Errorf("MCP eval capture requires %s, %s, %s, and %s together", EnvEvalRunID, EnvScenarioRunID, EnvInvocationID, EnvInvocationPhase)
	}
	recorder, err := NewMCPRecorder(MCPRecorderConfig{
		SessionDir:    sessionDir,
		SessionID:     sessionID,
		ProcessID:     os.Getpid(),
		EvalRunID:     evalRunID,
		ScenarioRunID: scenarioRunID,
		InvocationID:  invocationID,
		Phase:         phase,
	})
	if err != nil {
		return nil, true, err
	}
	return recorder, true, nil
}

func (m *MCPRecorder) record(record Record) error {
	if record.ProcessID == 0 {
		record.ProcessID = m.processID
	}
	if record.EvalRunID == "" {
		record.EvalRunID = m.evalRunID
	}
	if record.ScenarioRunID == "" {
		record.ScenarioRunID = m.scenarioRunID
	}
	if record.InvocationID == "" {
		record.InvocationID = m.invocationID
	}
	if record.Phase == "" {
		record.Phase = m.phase
	}
	err := m.recorder.Record(record)
	m.rememberError(err)
	return err
}

func (m *MCPRecorder) recordInput(chunk []byte) {
	offset := m.input.add(chunk)
	sum := sha256.Sum256(chunk)
	_ = m.record(Record{
		Kind:         RecordMCPStdinChunk,
		Direction:    "client_to_zcp",
		StreamOffset: offset,
		BodyBase64:   base64.StdEncoding.EncodeToString(chunk),
		BodyBytes:    int64(len(chunk)),
		SHA256:       hex.EncodeToString(sum[:]),
	})
}

func (m *MCPRecorder) recordOutput(chunk []byte) {
	offset := m.output.add(chunk)
	sum := sha256.Sum256(chunk)
	_ = m.record(Record{
		Kind:         RecordMCPStdoutChunk,
		Direction:    "zcp_to_client",
		StreamOffset: offset,
		BodyBase64:   base64.StdEncoding.EncodeToString(chunk),
		BodyBytes:    int64(len(chunk)),
		SHA256:       hex.EncodeToString(sum[:]),
	})
}

func (m *MCPRecorder) recordIOError(kind, direction string, err error) {
	if err == nil || errors.Is(err, io.EOF) {
		return
	}
	_ = m.record(Record{Kind: kind, Direction: direction, Error: err.Error()})
}

func (m *MCPRecorder) rememberError(err error) {
	if err == nil {
		return
	}
	m.errMu.Lock()
	if m.err == nil {
		m.err = err
	}
	m.errMu.Unlock()
}

// Error returns the first capture-only failure without changing stream I/O.
func (m *MCPRecorder) Error() error {
	m.errMu.Lock()
	defer m.errMu.Unlock()
	return m.err
}

// Close records whole-stream byte counts/hashes and durably closes the file.
func (m *MCPRecorder) Close(status string) error {
	if m.Error() != nil && status == CaptureComplete {
		status = CapturePartial
	}
	inputBytes, inputHash := m.input.snapshot()
	outputBytes, outputHash := m.output.snapshot()
	closeErr := m.recorder.closeWith(Record{
		Kind:          RecordMCPStreamEnd,
		ProcessID:     m.processID,
		EvalRunID:     m.evalRunID,
		ScenarioRunID: m.scenarioRunID,
		InvocationID:  m.invocationID,
		Phase:         m.phase,
		InputBytes:    inputBytes,
		OutputBytes:   outputBytes,
		InputSHA256:   inputHash,
		OutputSHA256:  outputHash,
		CaptureStatus: status,
	})
	priorErr := m.Error()
	m.rememberError(closeErr)
	return errors.Join(priorErr, closeErr)
}

// Path returns this process's private MCP JSONL path.
func (m *MCPRecorder) Path() string { return m.recorder.Path() }

// WrapMCPReader observes bytes after the underlying stdin read succeeds while
// returning the delegate's exact (n, err) pair unchanged.
func WrapMCPReader(reader io.ReadCloser, recorder *MCPRecorder) io.ReadCloser {
	if recorder == nil {
		return reader
	}
	return &mcpCaptureReader{delegate: reader, recorder: recorder}
}

type mcpCaptureReader struct {
	delegate io.ReadCloser
	recorder *MCPRecorder
}

func (r *mcpCaptureReader) Read(buffer []byte) (int, error) {
	count, err := r.delegate.Read(buffer)
	if count > 0 {
		r.recorder.recordInput(buffer[:count])
	}
	r.recorder.recordIOError(RecordMCPStdinError, "client_to_zcp", err)
	return count, err
}

func (r *mcpCaptureReader) Close() error {
	err := r.delegate.Close()
	r.recorder.recordIOError(RecordMCPStdinError, "client_to_zcp", err)
	return err
}

// WrapMCPWriter observes exactly the prefix accepted by the real stdout writer
// and returns the delegate's exact (n, err) pair unchanged.
func WrapMCPWriter(writer io.WriteCloser, recorder *MCPRecorder) io.WriteCloser {
	if recorder == nil {
		return writer
	}
	return &mcpCaptureWriter{delegate: writer, recorder: recorder}
}

type mcpCaptureWriter struct {
	delegate io.WriteCloser
	recorder *MCPRecorder
}

func (w *mcpCaptureWriter) Write(buffer []byte) (int, error) {
	count, err := w.delegate.Write(buffer)
	if count > 0 {
		w.recorder.recordOutput(buffer[:count])
	}
	w.recorder.recordIOError(RecordMCPStdoutError, "zcp_to_client", err)
	return count, err
}

func (w *mcpCaptureWriter) Close() error {
	err := w.delegate.Close()
	w.recorder.recordIOError(RecordMCPStdoutError, "zcp_to_client", err)
	return err
}

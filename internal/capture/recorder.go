// Package capture records raw developer-observed protocol boundaries without
// interpreting them. The JSONL record stream is append-only and plaintext.
package capture

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// EnvSessionID and EnvSessionDir propagate one opt-in capture through the
	// wrapped process tree. A ZCP MCP subprocess uses them in the next slice.
	EnvSessionID       = "ZCP_CAPTURE_SESSION_ID"
	EnvSessionDir      = "ZCP_CAPTURE_SESSION_DIR"
	EnvControlSocket   = "ZCP_CAPTURE_CONTROL_SOCKET"
	EnvControlToken    = "ZCP_CAPTURE_CONTROL_TOKEN"
	EnvEvalRunID       = "ZCP_CAPTURE_EVAL_RUN_ID"
	EnvScenarioRunID   = "ZCP_CAPTURE_SCENARIO_RUN_ID"
	EnvInvocationID    = "ZCP_CAPTURE_INVOCATION_ID"
	EnvInvocationPhase = "ZCP_CAPTURE_INVOCATION_PHASE"

	RecordSessionStart          = "session.start"
	RecordSessionEnd            = "session.end"
	RecordCaptureGap            = "capture.gap"
	RecordProviderRequestStart  = "provider.request.start"
	RecordProviderRequestBody   = "provider.request.body"
	RecordProviderRequestEnd    = "provider.request.end"
	RecordProviderResponseStart = "provider.response.start"
	RecordProviderResponseBody  = "provider.response.body"
	RecordProviderResponseEnd   = "provider.response.end"
	RecordProviderExchangeError = "provider.exchange.error"
	RecordMCPStreamStart        = "mcp.stream.start"
	RecordMCPStdinChunk         = "mcp.stdin.chunk"
	RecordMCPStdoutChunk        = "mcp.stdout.chunk"
	RecordMCPStdinError         = "mcp.stdin.error"
	RecordMCPStdoutError        = "mcp.stdout.error"
	RecordMCPStreamEnd          = "mcp.stream.end"
	CaptureRunning              = "running"
	CaptureComplete             = "complete"
	CapturePartial              = "partial"
	CaptureUnclean              = "unclean"
	defaultQueueCapacity        = 512
	recordsFilename             = "provider.jsonl"
)

var (
	errRecorderClosed = errors.New("capture recorder is closed")
	// ErrCaptureGap reports that an observation was deliberately not queued
	// rather than applying backpressure to protocol traffic. A capture.gap
	// record with the lost sequence range is emitted as soon as capacity exists.
	ErrCaptureGap = errors.New("capture queue overflow")
)

// Record is one ordered observation at a declared local boundary. Body chunks
// are base64 encoded so arbitrary entity bytes survive JSONL round trips.
type Record struct {
	Seq            uint64      `json:"seq"`
	Time           time.Time   `json:"time"`
	SessionID      string      `json:"sessionId"`
	Kind           string      `json:"kind"`
	Label          string      `json:"label,omitempty"`
	ExchangeID     string      `json:"exchangeId,omitempty"`
	Direction      string      `json:"direction,omitempty"`
	Method         string      `json:"method,omitempty"`
	Path           string      `json:"path,omitempty"`
	StatusCode     int         `json:"statusCode,omitempty"`
	Headers        http.Header `json:"headers,omitempty"`
	BodyBase64     string      `json:"bodyBase64,omitempty"`
	BodyBytes      int64       `json:"bodyBytes,omitempty"`
	SHA256         string      `json:"sha256,omitempty"`
	Error          string      `json:"error,omitempty"`
	CaptureStatus  string      `json:"captureStatus,omitempty"`
	ChildExitCode  int         `json:"childExitCode,omitempty"`
	ProcessID      int         `json:"processId,omitempty"`
	EvalRunID      string      `json:"evalRunId,omitempty"`
	ScenarioRunID  string      `json:"scenarioRunId,omitempty"`
	InvocationID   string      `json:"invocationId,omitempty"`
	Phase          string      `json:"phase,omitempty"`
	StreamOffset   int64       `json:"streamOffset,omitempty"`
	InputBytes     int64       `json:"inputBytes,omitempty"`
	OutputBytes    int64       `json:"outputBytes,omitempty"`
	InputSHA256    string      `json:"inputSha256,omitempty"`
	OutputSHA256   string      `json:"outputSha256,omitempty"`
	GapStartSeq    uint64      `json:"gapStartSeq,omitempty"`
	GapEndSeq      uint64      `json:"gapEndSeq,omitempty"`
	DroppedRecords uint64      `json:"droppedRecords,omitempty"`
	DroppedBytes   int64       `json:"droppedBytes,omitempty"`
}

// RecorderConfig identifies one capture session and its local storage root.
type RecorderConfig struct {
	RootDir       string
	SessionID     string
	Label         string
	QueueCapacity int

	// writerDelay exists only to make overflow behavior deterministic in tests.
	writerDelay time.Duration
}

type recordFile interface {
	io.Writer
	Sync() error
	Close() error
}

type writeRequest struct {
	record *Record
	close  bool
	ack    chan error
}

type pendingGap struct {
	startSeq       uint64
	endSeq         uint64
	droppedRecords uint64
	droppedBytes   int64
}

// Recorder owns one JSONL writer goroutine. Protocol-facing calls only attempt
// a bounded in-memory enqueue; file I/O happens in the writer goroutine and
// never while queueMu is held.
type Recorder struct {
	sessionID  string
	label      string
	sessionDir string
	path       string
	queue      chan writeRequest
	queueMu    sync.Mutex
	nextSeq    uint64
	gap        pendingGap
	hadGap     bool
	closed     bool
	closeOnce  sync.Once
	closeErr   error
}

// NewRecorder creates a private, unique session directory and queues the first
// session.start record before returning.
func NewRecorder(cfg RecorderConfig) (*Recorder, error) {
	if cfg.RootDir == "" {
		return nil, errors.New("capture root directory is required")
	}
	if cfg.SessionID == "" {
		return nil, errors.New("capture session ID is required")
	}
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = defaultQueueCapacity
	}
	if err := os.MkdirAll(cfg.RootDir, 0o700); err != nil {
		return nil, fmt.Errorf("create capture root: %w", err)
	}
	if err := os.Chmod(cfg.RootDir, 0o700); err != nil {
		return nil, fmt.Errorf("set capture root permissions: %w", err)
	}
	sessionDir := filepath.Join(cfg.RootDir, cfg.SessionID)
	if err := os.Mkdir(sessionDir, 0o700); err != nil {
		return nil, fmt.Errorf("create capture session directory: %w", err)
	}
	path := filepath.Join(sessionDir, recordsFilename)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create capture record file: %w", err)
	}

	r := newRecorder(file, sessionDir, path, cfg.SessionID, cfg.Label, cfg.QueueCapacity, cfg.writerDelay)
	if err := r.Record(Record{Kind: RecordSessionStart, Label: cfg.Label}); err != nil {
		_ = r.closeWriter()
		return nil, fmt.Errorf("queue capture session start: %w", err)
	}
	return r, nil
}

func newRecorder(file recordFile, sessionDir, path, sessionID, label string, queueCapacity int, writerDelay time.Duration) *Recorder {
	r := &Recorder{
		sessionID:  sessionID,
		label:      label,
		sessionDir: sessionDir,
		path:       path,
		queue:      make(chan writeRequest, queueCapacity),
	}
	go runRecordWriter(file, r.queue, writerDelay)
	return r
}

func runRecordWriter(file recordFile, queue <-chan writeRequest, writerDelay time.Duration) {
	writer := bufio.NewWriterSize(file, 64*1024)
	encoder := json.NewEncoder(writer)
	var writeErr error
	for request := range queue {
		if request.close {
			flushErr := writer.Flush()
			syncErr := file.Sync()
			closeErr := file.Close()
			request.ack <- errors.Join(writeErr, flushErr, syncErr, closeErr)
			close(request.ack)
			return
		}
		if writerDelay > 0 {
			time.Sleep(writerDelay)
		}
		if err := encoder.Encode(request.record); err != nil {
			writeErr = errors.Join(writeErr, fmt.Errorf("encode seq %d: %w", request.record.Seq, err))
		}
	}
}

// Record attempts a bounded, non-blocking append. On overflow it returns
// ErrCaptureGap and remembers the exact lost sequence range for a later
// capture.gap record; it never backpressures the observed protocol.
func (r *Recorder) Record(record Record) error {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()
	if r.closed {
		return errRecorderClosed
	}

	r.flushGapIfCapacityLocked()
	request := r.prepareRecord(record)
	select {
	case r.queue <- request:
		return nil
	default:
		r.noteDroppedLocked(*request.record)
		return fmt.Errorf("%w: dropped seq %d", ErrCaptureGap, request.record.Seq)
	}
}

func (r *Recorder) prepareRecord(record Record) writeRequest {
	r.nextSeq++
	record.Seq = r.nextSeq
	if record.Time.IsZero() {
		record.Time = time.Now().UTC()
	}
	if record.SessionID == "" {
		record.SessionID = r.sessionID
	}
	return writeRequest{record: &record}
}

func (r *Recorder) noteDroppedLocked(record Record) {
	if r.gap.droppedRecords == 0 {
		r.gap.startSeq = record.Seq
	}
	r.gap.endSeq = record.Seq
	r.gap.droppedRecords++
	if record.BodyBase64 != "" {
		r.gap.droppedBytes += record.BodyBytes
	}
	r.hadGap = true
}

func (r *Recorder) flushGapIfCapacityLocked() {
	if r.gap.droppedRecords == 0 || len(r.queue) >= cap(r.queue) {
		return
	}
	request := r.prepareRecord(r.gapRecord())
	r.queue <- request
	r.gap = pendingGap{}
}

func (r *Recorder) gapRecord() Record {
	return Record{
		Kind:           RecordCaptureGap,
		GapStartSeq:    r.gap.startSeq,
		GapEndSeq:      r.gap.endSeq,
		DroppedRecords: r.gap.droppedRecords,
		DroppedBytes:   r.gap.droppedBytes,
	}
}

// Close queues the terminal session record, flushes, fsyncs, and closes the
// record file. A prior queue gap forces the terminal status to partial. The
// first call owns status/exit-code values; later calls return the same result.
func (r *Recorder) Close(status string, childExitCode int) error {
	return r.closeWith(Record{
		Kind:          RecordSessionEnd,
		Label:         r.label,
		CaptureStatus: status,
		ChildExitCode: childExitCode,
	})
}

// CloseDaemon closes a capture window that is not owned by one wrapped child.
// ChildExitCode remains omitted from the terminal JSON record.
func (r *Recorder) CloseDaemon(status string) error {
	return r.closeWith(Record{
		Kind:          RecordSessionEnd,
		Label:         r.label,
		CaptureStatus: status,
	})
}

func (r *Recorder) closeWith(end Record) error {
	r.closeOnce.Do(func() {
		r.queueMu.Lock()
		r.closed = true
		if r.gap.droppedRecords > 0 {
			gapRequest := r.prepareRecord(r.gapRecord())
			r.queue <- gapRequest
			r.gap = pendingGap{}
		}
		if r.hadGap && end.CaptureStatus == CaptureComplete {
			end.CaptureStatus = CapturePartial
		}
		endRequest := r.prepareRecord(end)
		closeRequest := writeRequest{close: true, ack: make(chan error, 1)}
		r.queue <- endRequest
		r.queue <- closeRequest
		r.queueMu.Unlock()

		if closeErr := <-closeRequest.ack; closeErr != nil {
			r.closeErr = fmt.Errorf("close capture record file: %w", closeErr)
		}
	})
	return r.closeErr
}

func (r *Recorder) closeWriter() error {
	r.queueMu.Lock()
	if r.closed {
		r.queueMu.Unlock()
		return nil
	}
	r.closed = true
	request := writeRequest{close: true, ack: make(chan error, 1)}
	r.queue <- request
	r.queueMu.Unlock()
	return <-request.ack
}

// SessionDir returns the private directory containing this capture.
func (r *Recorder) SessionDir() string { return r.sessionDir }

// Path returns the provider JSONL path.
func (r *Recorder) Path() string { return r.path }

// ReadRecords decodes an append-only JSONL capture without a scanner line-size
// cap; model request bodies may produce multi-megabyte records.
func ReadRecords(path string) ([]Record, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open capture records: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	var records []Record
	for {
		var record Record
		if err := decoder.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode capture record %d: %w", len(records)+1, err)
		}
		records = append(records, record)
	}
	return records, nil
}

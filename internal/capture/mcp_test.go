package capture

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMCPStreamCapture_PreservesStdioAndRecordsExactBytes(t *testing.T) {
	t.Parallel()

	sessionDir := filepath.Join(t.TempDir(), "session-mcp")
	if err := os.Mkdir(sessionDir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	recorder, err := NewMCPRecorder(MCPRecorderConfig{
		SessionDir:    sessionDir,
		SessionID:     "session-mcp",
		ProcessID:     4242,
		EvalRunID:     "suite-1",
		ScenarioRunID: "weather",
		InvocationID:  "weather/agent.initial",
		Phase:         "agent.initial",
	})
	if err != nil {
		t.Fatalf("NewMCPRecorder() error = %v", err)
	}

	incoming := []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n")
	outgoingPart1 := []byte("{\"jsonrpc\":\"2.0\",\"id\":1,")
	outgoingPart2 := []byte("\"result\":{}}\n")
	reader := WrapMCPReader(io.NopCloser(bytes.NewReader(incoming)), recorder)
	var stdout bytes.Buffer
	writer := WrapMCPWriter(nopBufferWriteCloser{Writer: &stdout}, recorder)

	gotIncoming, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(gotIncoming, incoming) {
		t.Fatalf("wrapped stdin changed:\nwant %q\n got %q", incoming, gotIncoming)
	}
	if _, err := writer.Write(outgoingPart1); err != nil {
		t.Fatalf("first stdout write: %v", err)
	}
	if _, err := writer.Write(outgoingPart2); err != nil {
		t.Fatalf("second stdout write: %v", err)
	}
	wantOutgoing := append(bytes.Clone(outgoingPart1), outgoingPart2...)
	if !bytes.Equal(stdout.Bytes(), wantOutgoing) {
		t.Fatalf("wrapped stdout changed:\nwant %q\n got %q", wantOutgoing, stdout.Bytes())
	}
	if err := recorder.Close(CaptureComplete); err != nil {
		t.Fatalf("MCPRecorder.Close() error = %v", err)
	}

	info, err := os.Stat(recorder.Path())
	if err != nil {
		t.Fatalf("stat MCP capture: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("MCP capture mode = %#o, want 0600", got)
	}
	records, err := ReadRecords(recorder.Path())
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	if got := joinedMCPBytes(t, records, RecordMCPStdinChunk); !bytes.Equal(got, incoming) {
		t.Fatalf("captured stdin changed:\nwant %q\n got %q", incoming, got)
	}
	if got := joinedMCPBytes(t, records, RecordMCPStdoutChunk); !bytes.Equal(got, wantOutgoing) {
		t.Fatalf("captured stdout changed:\nwant %q\n got %q", wantOutgoing, got)
	}
	start := firstRecord(t, records, RecordMCPStreamStart)
	if start.ProcessID != 4242 || start.EvalRunID != "suite-1" || start.ScenarioRunID != "weather" || start.InvocationID != "weather/agent.initial" || start.Phase != "agent.initial" {
		t.Fatalf("stream start identity = %+v", start)
	}
	end := firstRecord(t, records, RecordMCPStreamEnd)
	incomingHash := sha256.Sum256(incoming)
	outgoingHash := sha256.Sum256(wantOutgoing)
	if end.InputBytes != int64(len(incoming)) || end.OutputBytes != int64(len(wantOutgoing)) ||
		end.InputSHA256 != hex.EncodeToString(incomingHash[:]) || end.OutputSHA256 != hex.EncodeToString(outgoingHash[:]) ||
		end.CaptureStatus != CaptureComplete {
		t.Fatalf("stream end = %+v", end)
	}
}

func TestNewMCPRecorderFromEnvironment_DisabledByDefault(t *testing.T) {
	t.Setenv(EnvSessionID, "")
	t.Setenv(EnvSessionDir, "")

	recorder, enabled, err := NewMCPRecorderFromEnvironment()
	if err != nil || recorder != nil || enabled {
		t.Fatalf("NewMCPRecorderFromEnvironment() = (%v, %v, %v), want disabled without error", recorder, enabled, err)
	}
}

func TestNewMCPRecorderFromEnvironment_RequiresCompleteOptIn(t *testing.T) {
	t.Setenv(EnvSessionID, "session-only")
	t.Setenv(EnvSessionDir, "")

	recorder, enabled, err := NewMCPRecorderFromEnvironment()
	if err == nil || recorder != nil || !enabled {
		t.Fatalf("NewMCPRecorderFromEnvironment() = (%v, %v, %v), want enabled configuration error", recorder, enabled, err)
	}
}

func joinedMCPBytes(t *testing.T, records []Record, kind string) []byte {
	t.Helper()
	var out []byte
	for _, record := range records {
		if record.Kind != kind {
			continue
		}
		chunk, err := base64.StdEncoding.DecodeString(record.BodyBase64)
		if err != nil {
			t.Fatalf("decode %s: %v", kind, err)
		}
		out = append(out, chunk...)
	}
	return out
}

type nopBufferWriteCloser struct{ io.Writer }

func (nopBufferWriteCloser) Close() error { return nil }

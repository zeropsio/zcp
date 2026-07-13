package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/zeropsio/zcp/internal/capture"
)

// TestMcpTransport_UsesSuppliedWriter pins the P0-1 fix: the MCP stdio
// transport must frame JSON-RPC over the writer captured before the serve
// path repointed os.Stdout at stderr — NOT the live os.Stdout (which now
// points at stderr). Without this the JSON-RPC stream would go to stderr
// and the protocol would be dead.
func TestMcpTransport_UsesSuppliedWriter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tr := mcpTransport(&buf)

	if tr.Reader != os.Stdin {
		t.Errorf("transport.Reader = %v, want os.Stdin", tr.Reader)
	}

	const payload = `{"jsonrpc":"2.0"}`
	if _, err := tr.Writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := buf.String(); got != payload {
		t.Errorf("payload reached %q, want %q — transport must write to the supplied writer", got, payload)
	}
}

// TestMcpTransport_CloseIsNoOp pins the Codex correction: the transport
// writer must be wrapped so the SDK's connection teardown (ioConn.Close →
// rwc.Close → Writer.Close) does NOT close the underlying real stdout (fd 1).
// A bare *os.File would be closed by the SDK on shutdown.
func TestMCPCaptureStatus_CleanTransportTermination(t *testing.T) {
	t.Parallel()

	for _, runErr := range []error{nil, context.Canceled, io.EOF, fmt.Errorf("server is closing: %w", io.EOF), errors.New("server is closing: EOF")} {
		if got := mcpCaptureStatus(runErr); got != capture.CaptureComplete {
			t.Fatalf("mcpCaptureStatus(%v) = %s, want complete", runErr, got)
		}
	}
	if got := mcpCaptureStatus(errors.New("protocol failure")); got != capture.CapturePartial {
		t.Fatalf("protocol failure status = %s, want partial", got)
	}
}

func TestMcpTransport_CloseIsNoOp(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tr := mcpTransport(&buf)

	if err := tr.Writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The wrapped writer must remain usable after Close (proves Close was a
	// no-op and did not propagate to the underlying writer).
	if _, err := tr.Writer.Write([]byte("still-open")); err != nil {
		t.Fatalf("write after Close: %v", err)
	}
	if buf.String() != "still-open" {
		t.Errorf("underlying writer unusable after Close — nopWriteCloser must not propagate Close")
	}
}

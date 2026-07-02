package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

func TestWireRuntimeEnv(t *testing.T) {
	if got := wireRuntimeEnv(runtime.Info{InContainer: true}); got != wire.RuntimeContainer {
		t.Errorf("InContainer=true → %q, want %q", got, wire.RuntimeContainer)
	}
	if got := wireRuntimeEnv(runtime.Info{InContainer: false}); got != wire.RuntimeLocal {
		t.Errorf("InContainer=false → %q, want %q", got, wire.RuntimeLocal)
	}
}

func TestShapeOrUnknownCLI(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"sync", "sync"},
		{"cache-clear", wire.UnknownIdentifier}, // hyphen fails identifierPattern
		{"/etc/passwd", wire.UnknownIdentifier},
		{"secret-token-123", wire.UnknownIdentifier},
	}
	for _, tt := range tests {
		if got := shapeOrUnknownCLI(tt.in); got != tt.want {
			t.Errorf("shapeOrUnknownCLI(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEmitCLICommand_CommandOnly_NoActionForNonVerbCommands(t *testing.T) {
	rec := &recordingEmitter{}
	emitCLICommand(rec, []string{"eval", "some-scenario-id"}, time.Now(), true)

	if len(rec.events) != 1 {
		t.Fatalf("got %d events, want 1", len(rec.events))
	}
	e := rec.events[0]
	if e.EventType != wire.EventCLICommand {
		t.Errorf("EventType = %q, want %q", e.EventType, wire.EventCLICommand)
	}
	if e.Command != "eval" {
		t.Errorf("Command = %q, want %q", e.Command, "eval")
	}
	if e.Action != "" {
		t.Errorf("Action = %q, want empty — eval's second arg is a free-form scenario id (spec B2), never sent", e.Action)
	}
	if !e.Success {
		t.Error("Success = false, want true")
	}
}

func TestEmitCLICommand_ActionFilledForVerbCommands(t *testing.T) {
	tests := []struct {
		args       []string
		wantAction string
	}{
		{[]string{"sync", "pull"}, "pull"},
		{[]string{"schema", "sync"}, "sync"},
		{[]string{"catalog", "sync"}, "sync"},
		{[]string{"service", "start"}, "start"},
		{[]string{"telemetry", "status"}, "status"},
	}
	for _, tt := range tests {
		rec := &recordingEmitter{}
		emitCLICommand(rec, tt.args, time.Now(), true)
		if len(rec.events) != 1 {
			t.Fatalf("args=%v: got %d events, want 1", tt.args, len(rec.events))
		}
		if rec.events[0].Action != tt.wantAction {
			t.Errorf("args=%v: Action = %q, want %q", tt.args, rec.events[0].Action, tt.wantAction)
		}
	}
}

func TestEmitCLICommand_ShapeInvalidActionBecomesUnknown(t *testing.T) {
	rec := &recordingEmitter{}
	emitCLICommand(rec, []string{"sync", "cache-clear"}, time.Now(), false)

	if len(rec.events) != 1 {
		t.Fatalf("got %d events, want 1", len(rec.events))
	}
	e := rec.events[0]
	if e.Action != wire.UnknownIdentifier {
		t.Errorf("Action = %q, want %q (cache-clear fails identifier shape)", e.Action, wire.UnknownIdentifier)
	}
	if e.Success {
		t.Error("Success = true, want false")
	}
}

func TestEmitCLICommand_NoSecondArg_NoAction(t *testing.T) {
	rec := &recordingEmitter{}
	emitCLICommand(rec, []string{"version"}, time.Now(), true)
	if len(rec.events) != 1 {
		t.Fatalf("got %d events, want 1", len(rec.events))
	}
	if rec.events[0].Action != "" {
		t.Errorf("Action = %q, want empty", rec.events[0].Action)
	}
}

func TestRunServiceCmd_UsageError_DoesNotCallServiceStart(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", nil},
		{"missing service name", []string{"start"}},
		{"wrong verb", []string{"stop", "nginx"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if code := runServiceCmd(tt.args); code != 1 {
				t.Errorf("runServiceCmd(%v) = %d, want 1", tt.args, code)
			}
		})
	}
}

// TestMainGo_SingleExitPoint pins the whole-tree single-exit-point
// invariant: every helper behind every CLI verb (init, service, update,
// eval, catalog, schema, sync, analyze, telemetry) RETURNS a code/error up
// to the dispatcher instead of exiting inline, so telemetry always gets a
// chance to flush before the process terminates (spec-telemetry.md §5.5
// "CLI one-shots ... flushed before the single exit point returns"). Scans
// every non-test .go file under cmd/zcp/ (including cmd/zcp/analyze/):
// exactly one os.Exit call site total — main.go's main(), the sole place
// the process may terminate directly — and zero log.Fatal/log.Fatalf/
// log.Fatalln calls anywhere in the tree.
func TestMainGo_SingleExitPoint(t *testing.T) {
	var exitSites, fatalSites []string

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for lineNo, line := range strings.Split(string(src), "\n") {
			code, _, _ := strings.Cut(line, "//") // ignore comments — this scan checks live code, not prose mentioning the symbol
			if strings.Contains(code, "os.Exit(") {
				exitSites = append(exitSites, fmt.Sprintf("%s:%d: %s", path, lineNo+1, strings.TrimSpace(code)))
			}
			if strings.Contains(code, "log.Fatal") {
				fatalSites = append(fatalSites, fmt.Sprintf("%s:%d: %s", path, lineNo+1, strings.TrimSpace(code)))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk cmd/zcp: %v", err)
	}

	if len(fatalSites) != 0 {
		t.Errorf("log.Fatal/log.Fatalf/log.Fatalln bypasses the telemetry flush — must return a code up to the dispatcher instead; found:\n%s", strings.Join(fatalSites, "\n"))
	}

	if len(exitSites) != 1 {
		t.Fatalf("os.Exit must appear exactly once in the whole cmd/zcp/ tree (main.go's main(), the single exit point); found %d:\n%s", len(exitSites), strings.Join(exitSites, "\n"))
	}
	if !strings.HasPrefix(exitSites[0], "main.go:") {
		t.Errorf("the sole os.Exit call must be in main.go, got: %s", exitSites[0])
	}
}

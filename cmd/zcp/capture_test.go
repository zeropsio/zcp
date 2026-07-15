package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/capture"
)

func TestCaptureManifestProviderOrigin_OmitsCredentialsAndQuery(t *testing.T) {
	t.Parallel()

	got, err := captureManifestProviderOrigin("https://user:secret@provider.example/base?token=secret#fragment")
	if err != nil {
		t.Fatalf("captureManifestProviderOrigin() error = %v", err)
	}
	if got != "https://provider.example/base" {
		t.Fatalf("captureManifestProviderOrigin() = %q, want credential-free origin", got)
	}
}

func TestCaptureChildEnv_OverridesProviderAndAddsSession(t *testing.T) {
	t.Parallel()

	got := captureChildEnv([]string{
		"PATH=/bin",
		"ANTHROPIC_BASE_URL=https://old.example",
		capture.EnvSessionID + "=old-session",
		capture.EnvSessionDir + "=/old/session",
	}, "http://127.0.0.1:43210", "session-new", "/tmp/session-new", "/tmp/control.sock", "control-token")

	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"PATH=/bin",
		"ANTHROPIC_BASE_URL=http://127.0.0.1:43210",
		capture.EnvSessionID + "=session-new",
		capture.EnvSessionDir + "=/tmp/session-new",
		capture.EnvControlSocket + "=/tmp/control.sock",
		capture.EnvControlToken + "=control-token",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("captureChildEnv() missing %q in %v", want, got)
		}
	}
	if strings.Contains(joined, "old.example") || strings.Contains(joined, "old-session") || strings.Contains(joined, "/old/session") {
		t.Fatalf("captureChildEnv() retained overridden values: %v", got)
	}
}

func TestParseCaptureUIArgs_AcceptsRootSessionAndSecurityFlags(t *testing.T) {
	t.Parallel()

	options, err := parseCaptureUIArgs([]string{"/tmp/capture", "--root", "/tmp/root", "--listen", "127.0.0.1:43210", "--no-open"})
	if err != nil {
		t.Fatalf("parseCaptureUIArgs() error = %v", err)
	}
	if options.SessionDir != "/tmp/capture" || options.CaptureRoot != "/tmp/root" || options.ListenAddr != "127.0.0.1:43210" || !options.NoOpen || options.Active {
		t.Fatalf("options = %+v", options)
	}
	if _, err := parseCaptureUIArgs([]string{"/tmp/one", "/tmp/two"}); err == nil {
		t.Fatal("parseCaptureUIArgs() accepted two session directories")
	}
}

func TestParseCaptureUIArgs_Help(t *testing.T) {
	t.Parallel()
	options, err := parseCaptureUIArgs([]string{"--help"})
	if err != nil || !options.Help {
		t.Fatalf("parseCaptureUIArgs(--help) = %+v, %v", options, err)
	}
}

func TestParseCaptureUIArgs_ActiveConflictsWithSession(t *testing.T) {
	t.Parallel()

	if _, err := parseCaptureUIArgs([]string{"/tmp/capture", "--active"}); err == nil {
		t.Fatal("parseCaptureUIArgs() accepted --active with a session directory")
	}
}

func TestParseCaptureInspectArgs_AcceptsFlagsAfterSessionPath(t *testing.T) {
	t.Parallel()

	options, err := parseCaptureInspectArgs([]string{"/tmp/capture", "--view", "context", "--format=json", "--eval", "suite-1", "--scenario=weather", "--invocation", "weather/agent.initial"})
	if err != nil {
		t.Fatalf("parseCaptureInspectArgs() error = %v", err)
	}
	if options.SessionDir != "/tmp/capture" || options.View != "context" || options.Format != "json" || options.Filter.EvalRunID != "suite-1" || options.Filter.ScenarioRunID != "weather" || options.Filter.InvocationID != "weather/agent.initial" {
		t.Fatalf("options = %+v", options)
	}
}

func TestParseEvalCaptureArgs_StripsRawModeWithoutChangingOtherArguments(t *testing.T) {
	t.Parallel()

	clean, requested, err := parseEvalCaptureArgs([]string{"behavioral", "run", "--id", "weather", "--capture", "raw", "--cleanup-workdir"})
	if err != nil {
		t.Fatalf("parseEvalCaptureArgs() error = %v", err)
	}
	if !requested {
		t.Fatal("parseEvalCaptureArgs() requested = false, want true")
	}
	want := []string{"behavioral", "run", "--id", "weather", "--cleanup-workdir"}
	if !slices.Equal(clean, want) {
		t.Fatalf("parseEvalCaptureArgs() args = %v, want %v", clean, want)
	}
}

func TestParseEvalCaptureArgs_RejectsUnsupportedOrMissingMode(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"behavioral", "run", "--capture"}, {"behavioral", "run", "--capture", "semantic"}, {"behavioral", "run", "--capture=semantic"}} {
		if _, _, err := parseEvalCaptureArgs(args); err == nil {
			t.Errorf("parseEvalCaptureArgs(%v) error = nil", args)
		}
	}
}

func TestRunCaptureChild_SignaledExitUsesShellConvention(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		signal string
		want   int
	}{
		{name: "term", signal: "TERM", want: 143},
		{name: "kill", signal: "KILL", want: 137},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			exitCode, err := runCaptureChild([]string{"sh", "-c", "kill -" + test.signal + " $$"}, os.Environ())
			if err != nil {
				t.Fatalf("runCaptureChild() error = %v", err)
			}
			if exitCode != test.want {
				t.Fatalf("runCaptureChild() exit = %d, want %d", exitCode, test.want)
			}
		})
	}
}

func TestRunCaptureRaw_ChildExitPreservedAndSessionClosed(t *testing.T) {
	root := t.TempDir()
	code := runCaptureRaw([]string{
		"--label", "exit-seven",
		"--output-dir", root,
		"--",
		"sh", "-c", "exit 7",
	})
	if code != 7 {
		t.Fatalf("runCaptureRaw() exit = %d, want child exit 7", code)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("capture root entries = %v, want one session directory", entries)
	}
	records, err := capture.ReadRecords(filepath.Join(root, entries[0].Name(), "provider.jsonl"))
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("record count = %d, want start + end", len(records))
	}
	end := records[len(records)-1]
	if end.Kind != capture.RecordSessionEnd || end.CaptureStatus != capture.CaptureComplete || end.ChildExitCode != 7 {
		t.Fatalf("session end = %+v", end)
	}

	manifest, err := capture.ReadSessionManifest(filepath.Join(root, entries[0].Name(), "manifest.json"))
	if err != nil {
		t.Fatalf("ReadSessionManifest() error = %v", err)
	}
	wantCommand := []string{"sh", "-c", "exit 7"}
	if manifest.Status != capture.CaptureComplete || manifest.ChildExitCode == nil || *manifest.ChildExitCode != 7 || !slices.Equal(manifest.Command, wantCommand) {
		t.Fatalf("manifest lifecycle = %+v", manifest)
	}
	if len(manifest.Files) != 2 || manifest.Files[0].Kind != capture.ManifestFileLifecycle || manifest.Files[0].Path != "lifecycle.jsonl" || manifest.Files[1].Kind != capture.ManifestFileProvider || manifest.Files[1].Path != "provider.jsonl" {
		t.Fatalf("manifest files = %+v", manifest.Files)
	}

	var inspectOut bytes.Buffer
	var inspectErr bytes.Buffer
	if code := runCaptureInspectTo([]string{filepath.Join(root, entries[0].Name())}, &inspectOut, &inspectErr); code != 0 {
		t.Fatalf("runCaptureInspectTo() exit = %d, stderr = %s", code, inspectErr.String())
	}
	if !strings.Contains(inspectOut.String(), "Integrity: OK") || !strings.Contains(inspectOut.String(), "Status: complete") {
		t.Fatalf("inspection output = %s", inspectOut.String())
	}
}

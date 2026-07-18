package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/telemetry"
	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

// captureOutput redirects the process-global os.Stdout/os.Stderr for the
// duration of f and returns what was written. Not safe to run with
// t.Parallel() (os.Stdout/os.Stderr are process-global) — none of these
// tests opt in.
func captureOutput(t *testing.T, f func()) (stdout, stderr string) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout, os.Stderr = outW, errW

	var outBuf, errBuf bytes.Buffer
	outDone := make(chan struct{})
	errDone := make(chan struct{})
	go func() { _, _ = io.Copy(&outBuf, outR); close(outDone) }()
	go func() { _, _ = io.Copy(&errBuf, errR); close(errDone) }()

	f()

	_ = outW.Close()
	_ = errW.Close()
	<-outDone
	<-errDone
	os.Stdout, os.Stderr = origOut, origErr
	return outBuf.String(), errBuf.String()
}

func TestRunTelemetryCmd_NoArgs_UsageError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var code int
	_, stderr := captureOutput(t, func() {
		code = runTelemetryCmd(nil, telemetry.Config{})
	})
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr = %q, want it to contain Usage:", stderr)
	}
}

func TestRunTelemetryCmd_UnknownSubcommand_ReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var code int
	captureOutput(t, func() {
		code = runTelemetryCmd([]string{"bogus"}, telemetry.Config{})
	})
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
}

func TestRunTelemetryCmd_Status_PrintsStateChannelReason(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := telemetry.Config{Enabled: true, Channel: wire.ChannelExternal, Reason: telemetry.ReasonEnabled}

	var code int
	stdout, _ := captureOutput(t, func() {
		code = runTelemetryCmd([]string{"status"}, cfg)
	})
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "enabled") {
		t.Errorf("stdout = %q, want it to mention enabled state", stdout)
	}
	if !strings.Contains(stdout, wire.ChannelExternal) {
		t.Errorf("stdout = %q, want it to mention channel %q", stdout, wire.ChannelExternal)
	}
	if !strings.Contains(stdout, telemetry.ReasonEnabled) {
		t.Errorf("stdout = %q, want it to mention reason %q", stdout, telemetry.ReasonEnabled)
	}
}

func TestRunTelemetryCmd_Status_DisabledState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := telemetry.Config{Enabled: false, Channel: wire.ChannelExternal, Reason: telemetry.ReasonOptedOutEnvTelemetry}

	var code int
	stdout, _ := captureOutput(t, func() {
		code = runTelemetryCmd([]string{"status"}, cfg)
	})
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "disabled") {
		t.Errorf("stdout = %q, want it to mention disabled state", stdout)
	}
}

func TestRunTelemetryCmd_Enable_WritesInstallFileAndPrintsNotice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := telemetry.Config{Channel: wire.ChannelExternal}

	var code int
	stdout, _ := captureOutput(t, func() {
		code = runTelemetryCmd([]string{"enable"}, cfg)
	})
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "Telemetry enabled.") {
		t.Errorf("stdout = %q, want confirmation", stdout)
	}
	if !strings.Contains(stdout, "ZCP_TELEMETRY=0") {
		t.Errorf("stdout = %q, want the disclosure notice (opt-out mention)", stdout)
	}

	id, err := telemetry.InstallIDOf(home, false)
	if err != nil {
		t.Fatalf("InstallIDOf after enable: %v", err)
	}
	if id == "" {
		t.Error("InstallID empty after enable")
	}
}

func TestRunTelemetryCmd_Enable_InternalChannel_WritesInternalFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := telemetry.Config{Channel: wire.ChannelInternalDev}

	captureOutput(t, func() {
		runTelemetryCmd([]string{"enable"}, cfg)
	})

	if _, err := telemetry.InstallIDOf(home, true); err != nil {
		t.Errorf("InstallIDOf(internal) after enable: %v", err)
	}
	if _, err := telemetry.InstallIDOf(home, false); err == nil {
		t.Error("external install file must not be written for an internal-channel enable")
	}
}

func TestRunTelemetryCmd_Disable_SetsDisabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := telemetry.Config{Channel: wire.ChannelExternal}

	// Seed an enabled install first.
	if err := telemetry.Enable(home, false, time.Now()); err != nil {
		t.Fatalf("seed Enable: %v", err)
	}

	var code int
	stdout, _ := captureOutput(t, func() {
		code = runTelemetryCmd([]string{"disable"}, cfg)
	})
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "Telemetry disabled.") {
		t.Errorf("stdout = %q, want confirmation", stdout)
	}

	// Re-resolving would now see disabled=true (rule 3) — verify indirectly
	// via a fresh Resolve.
	var buf bytes.Buffer
	got := telemetry.Resolve(os.Getenv, home, "v1.0.0", wire.RuntimeLocal, &buf)
	if got.Enabled {
		t.Error("Resolve after disable: Enabled = true, want false")
	}
}

func TestRunTelemetryCmd_ID_NoInstallFile_ReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := telemetry.Config{Channel: wire.ChannelExternal}

	var code int
	_, stderr := captureOutput(t, func() {
		code = runTelemetryCmd([]string{"id"}, cfg)
	})
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if stderr == "" {
		t.Error("stderr empty, want an error message")
	}
}

func TestRunTelemetryCmd_ID_PrintsInstallID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := telemetry.Config{Channel: wire.ChannelExternal}

	if err := telemetry.Enable(home, false, time.Now()); err != nil {
		t.Fatalf("seed Enable: %v", err)
	}
	wantID, err := telemetry.InstallIDOf(home, false)
	if err != nil {
		t.Fatalf("InstallIDOf: %v", err)
	}

	var code int
	stdout, _ := captureOutput(t, func() {
		code = runTelemetryCmd([]string{"id"}, cfg)
	})
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != wantID {
		t.Errorf("stdout = %q, want %q", strings.TrimSpace(stdout), wantID)
	}
}

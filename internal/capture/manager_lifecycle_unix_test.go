//go:build darwin || linux

package capture

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestManager_ReadinessFailureDoesNotOrphanDaemon(t *testing.T) {
	root := t.TempDir()
	process := exec.CommandContext(t.Context(), "/bin/sleep", "30")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = process.Process.Kill()
		_, _ = process.Process.Wait()
	})

	terminated := false
	manager, err := NewManager(ManagerConfig{
		StateDir:           filepath.Join(root, "state"),
		CaptureRoot:        filepath.Join(root, "captures"),
		ClaudeSettingsPath: filepath.Join(root, "settings.json"),
		ControlSocket:      filepath.Join(root, "missing-control.sock"),
		DefaultUpstreamURL: "https://api.anthropic.com",
		StartDaemon: func(context.Context, DaemonStartConfig) (DaemonReady, error) {
			return DaemonReady{
				ProcessID: process.Process.Pid, ProxyURL: "http://127.0.0.1:43210",
				SessionDir: filepath.Join(root, "captures", "audit"), ControlSocket: filepath.Join(root, "missing-control.sock"),
			}, nil
		},
		ProcessAlive: processAlive,
		TerminateProcess: func(pid int, _ string) error {
			terminated = true
			found, findErr := os.FindProcess(pid)
			if findErr != nil {
				return findErr
			}
			return found.Kill()
		},
		KillProcess: func(pid int, _ string) error {
			found, findErr := os.FindProcess(pid)
			if findErr != nil {
				return findErr
			}
			return found.Kill()
		},
		NewCaptureID:    func() (string, error) { return "audit-orphan", nil },
		NewControlToken: func() (string, error) { return "audit-token", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.On(context.Background(), ManagerOnOptions{}); err == nil {
		t.Fatal("On unexpectedly succeeded")
	}
	if processAlive(process.Process.Pid) {
		t.Fatalf("readiness rollback left daemon pid %d alive (TerminateProcess called=%v)", process.Process.Pid, terminated)
	}
	if !terminated {
		t.Fatal("readiness rollback did not use the identity-checked process fallback")
	}
	if _, err := os.Stat(filepath.Join(root, "state", managerStateFilename)); !os.IsNotExist(err) {
		t.Fatalf("manager journal remains after process exit was proven: %v", err)
	}
}

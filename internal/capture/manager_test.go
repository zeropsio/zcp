package capture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestManager_OnStatusOffIsTransactionalAndIdempotent(t *testing.T) {
	// non-parallel: the fake daemon uses a deterministic process identity and
	// exercises lifecycle polling; keeping it isolated makes timing failures
	// actionable.
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".claude", "settings.json")
	writeSettingsObject(t, settingsPath, map[string]any{
		"env": map[string]any{"ANTHROPIC_BASE_URL": "https://gateway.example"},
	}, 0o600)
	var starts atomic.Int32
	var alive atomic.Bool
	var runtime *Runtime
	starter := func(ctx context.Context, cfg DaemonStartConfig) (DaemonReady, error) {
		starts.Add(1)
		started, err := StartRuntime(ctx, RuntimeConfig{
			RootDir:       cfg.CaptureRoot,
			CaptureID:     cfg.CaptureID,
			Label:         cfg.Label,
			ListenAddr:    cfg.ListenAddr,
			UpstreamURL:   cfg.UpstreamURL,
			ControlSocket: cfg.ControlSocket,
			ControlToken:  cfg.ControlToken,
			Command:       []string{"fake-daemon"},
		})
		if err != nil {
			return DaemonReady{}, err
		}
		runtime = started
		alive.Store(true)
		go func() {
			<-started.ShutdownRequested()
			_, _ = started.Close(CaptureComplete)
			alive.Store(false)
		}()
		return DaemonReady{
			ProcessID:     os.Getpid(),
			ProxyURL:      started.ProxyURL(),
			SessionDir:    started.SessionDir(),
			ControlSocket: started.ControlSocket(),
		}, nil
	}
	manager, err := NewManager(ManagerConfig{
		StateDir:           filepath.Join(root, "state"),
		CaptureRoot:        filepath.Join(root, "captures"),
		ClaudeSettingsPath: settingsPath,
		ControlSocket:      filepath.Join(shortSocketDir(t), "manager.sock"),
		DefaultUpstreamURL: "https://api.anthropic.com",
		StartDaemon:        starter,
		ProcessAlive:       func(pid int) bool { return pid == os.Getpid() && alive.Load() },
		NewCaptureID:       func() (string, error) { return "capture-manager-test", nil },
		NewControlToken:    func() (string, error) { return "manager-secret", nil },
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	on, err := manager.On(context.Background(), ManagerOnOptions{Label: "eval-window"})
	if err != nil {
		t.Fatalf("On() error = %v", err)
	}
	if on.State != ManagerStateOn || on.CaptureID != "capture-manager-test" || on.ProxyURL == "" || !alive.Load() {
		t.Fatalf("On() status = %+v, alive=%v", on, alive.Load())
	}
	if starts.Load() != 1 {
		t.Fatalf("daemon starts = %d, want 1", starts.Load())
	}
	installed := readSettingsObject(t, settingsPath)["env"].(map[string]any)
	if installed["ANTHROPIC_BASE_URL"] != on.ProxyURL || installed[EnvSessionID] != on.CaptureID || installed[EnvSessionDir] != on.SessionDir {
		t.Fatalf("installed settings = %+v, status = %+v", installed, on)
	}

	again, err := manager.On(context.Background(), ManagerOnOptions{})
	if err != nil {
		t.Fatalf("second On() error = %v", err)
	}
	if again.State != ManagerStateOn || starts.Load() != 1 {
		t.Fatalf("second On() = %+v, starts=%d", again, starts.Load())
	}
	current, err := manager.Status(context.Background())
	if err != nil || current.State != ManagerStateOn {
		t.Fatalf("Status() = %+v, %v", current, err)
	}

	off, err := manager.Off(context.Background())
	if err != nil {
		t.Fatalf("Off() error = %v", err)
	}
	if off.State != ManagerStateOff || alive.Load() {
		t.Fatalf("Off() = %+v, alive=%v", off, alive.Load())
	}
	restored := readSettingsObject(t, settingsPath)["env"].(map[string]any)
	if restored["ANTHROPIC_BASE_URL"] != "https://gateway.example" {
		t.Fatalf("base URL was not restored: %+v", restored)
	}
	if _, ok := restored[EnvSessionID]; ok {
		t.Fatalf("capture settings remain after off: %+v", restored)
	}
	if _, err := os.Stat(filepath.Join(root, "state", managerStateFilename)); !os.IsNotExist(err) {
		t.Fatalf("manager state remains after off: %v", err)
	}
	if runtime == nil {
		t.Fatal("fake runtime was never started")
	}

	offAgain, err := manager.Off(context.Background())
	if err != nil || offAgain.State != ManagerStateOff {
		t.Fatalf("second Off() = %+v, %v", offAgain, err)
	}
}

func TestManager_OffRecoversCrashBeforeDaemonReadinessWasPersisted(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manager, err := NewManager(ManagerConfig{
		StateDir: filepath.Join(root, "state"), CaptureRoot: filepath.Join(root, "captures"), ClaudeSettingsPath: filepath.Join(root, "settings.json"),
		ControlSocket: filepath.Join(shortSocketDir(t), "manager.sock"), DefaultUpstreamURL: "https://api.anthropic.com",
		StartDaemon: func(context.Context, DaemonStartConfig) (DaemonReady, error) {
			return DaemonReady{}, errors.New("unused")
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if err := manager.writeState(managerStateDocument{
		FormatVersion: managerStateFormat, Phase: managerPhaseEnabling, CaptureID: "capture-before-ready", ControlSocket: manager.config.ControlSocket, ControlToken: "pending-token",
		ClaudePatch: ClaudeSettingsPatch{Entries: map[string]ClaudeSettingRestore{"ANTHROPIC_BASE_URL": {Installed: "http://127.0.0.1:43210"}}},
	}); err != nil {
		t.Fatalf("write pending state: %v", err)
	}
	status, err := manager.Off(context.Background())
	if err != nil || status.State != ManagerStateOff {
		t.Fatalf("Off() = %+v, %v", status, err)
	}
	if _, exists, err := manager.readState(); err != nil || exists {
		t.Fatalf("manager state after Off() exists=%v err=%v", exists, err)
	}
}

func TestManager_OnRequiresExplicitUpstreamForExistingLoopbackGateway(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	settingsPath := filepath.Join(root, "settings.json")
	writeSettingsObject(t, settingsPath, map[string]any{"env": map[string]any{"ANTHROPIC_BASE_URL": "http://127.0.0.1:9999"}}, 0o600)
	var started atomic.Bool
	manager, err := NewManager(ManagerConfig{
		StateDir: filepath.Join(root, "state"), CaptureRoot: filepath.Join(root, "captures"), ClaudeSettingsPath: settingsPath,
		ControlSocket: filepath.Join(shortSocketDir(t), "manager.sock"), DefaultUpstreamURL: "https://api.anthropic.com",
		StartDaemon: func(context.Context, DaemonStartConfig) (DaemonReady, error) {
			started.Store(true)
			return DaemonReady{}, nil
		},
		NewCaptureID: func() (string, error) { return "capture-loopback", nil }, NewControlToken: func() (string, error) { return "secret", nil },
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	_, err = manager.On(context.Background(), ManagerOnOptions{})
	if err == nil || !strings.Contains(err.Error(), "--upstream") {
		t.Fatalf("On() error = %v, want explicit upstream requirement", err)
	}
	if started.Load() {
		t.Fatal("daemon started after silently replacing existing loopback gateway")
	}
}

func TestManager_OnRefusesUnownedExistingControlSocket(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	controlDir := shortSocketDir(t)
	controlPath := filepath.Join(controlDir, "manager.sock")
	if err := os.WriteFile(controlPath, []byte("unowned"), 0o600); err != nil {
		t.Fatalf("write control collision: %v", err)
	}
	var started atomic.Bool
	manager, err := NewManager(ManagerConfig{
		StateDir: filepath.Join(root, "state"), CaptureRoot: filepath.Join(root, "captures"), ClaudeSettingsPath: filepath.Join(root, "settings.json"),
		ControlSocket: controlPath, DefaultUpstreamURL: "https://api.anthropic.com",
		StartDaemon: func(context.Context, DaemonStartConfig) (DaemonReady, error) {
			started.Store(true)
			return DaemonReady{}, nil
		},
		NewCaptureID: func() (string, error) { return "capture-collision", nil }, NewControlToken: func() (string, error) { return "secret", nil },
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	_, err = manager.On(context.Background(), ManagerOnOptions{})
	if err == nil || !strings.Contains(err.Error(), "control socket") {
		t.Fatalf("On() error = %v, want control collision", err)
	}
	if started.Load() {
		t.Fatal("daemon started despite unowned control socket")
	}
	off, err := manager.Off(context.Background())
	if err != nil || off.State != ManagerStateOff {
		t.Fatalf("Off() stale socket reconciliation = %+v, %v", off, err)
	}
	if _, err := os.Stat(controlPath); !os.IsNotExist(err) {
		t.Fatalf("stale control socket path remains: %v", err)
	}
}

func TestManager_OnFailureDoesNotInstallClaudeProxy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	settingsPath := filepath.Join(root, "settings.json")
	writeSettingsObject(t, settingsPath, map[string]any{"env": map[string]any{"KEEP": "yes"}}, 0o600)
	manager, err := NewManager(ManagerConfig{
		StateDir:           filepath.Join(root, "state"),
		CaptureRoot:        filepath.Join(root, "captures"),
		ClaudeSettingsPath: settingsPath,
		ControlSocket:      filepath.Join(shortSocketDir(t), "manager.sock"),
		DefaultUpstreamURL: "https://api.anthropic.com",
		StartDaemon: func(context.Context, DaemonStartConfig) (DaemonReady, error) {
			return DaemonReady{}, errors.New("listener unavailable")
		},
		NewCaptureID:    func() (string, error) { return "capture-failure", nil },
		NewControlToken: func() (string, error) { return "secret", nil },
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	_, err = manager.On(context.Background(), ManagerOnOptions{})
	if err == nil || !strings.Contains(err.Error(), "listener unavailable") {
		t.Fatalf("On() error = %v", err)
	}
	env := readSettingsObject(t, settingsPath)["env"].(map[string]any)
	if len(env) != 1 || env["KEEP"] != "yes" {
		t.Fatalf("settings changed after failed on: %+v", env)
	}
	if _, err := os.Stat(filepath.Join(root, "state", managerStateFilename)); !os.IsNotExist(err) {
		t.Fatalf("manager state remains after failed on: %v", err)
	}
}

func TestManager_StatusReportsBrokenWhenConfiguredDaemonDied(t *testing.T) {
	// non-parallel: deterministic fake process lifecycle.
	root := t.TempDir()
	var alive atomic.Bool
	var started *Runtime
	manager, err := NewManager(ManagerConfig{
		StateDir:           filepath.Join(root, "state"),
		CaptureRoot:        filepath.Join(root, "captures"),
		ClaudeSettingsPath: filepath.Join(root, "settings.json"),
		ControlSocket:      filepath.Join(shortSocketDir(t), "manager.sock"),
		DefaultUpstreamURL: "https://api.anthropic.com",
		StartDaemon: func(ctx context.Context, cfg DaemonStartConfig) (DaemonReady, error) {
			var err error
			started, err = StartRuntime(ctx, RuntimeConfig{RootDir: cfg.CaptureRoot, CaptureID: cfg.CaptureID, UpstreamURL: cfg.UpstreamURL, ListenAddr: cfg.ListenAddr, ControlSocket: cfg.ControlSocket, ControlToken: cfg.ControlToken})
			if err != nil {
				return DaemonReady{}, err
			}
			alive.Store(true)
			return DaemonReady{ProcessID: os.Getpid(), ProxyURL: started.ProxyURL(), SessionDir: started.SessionDir(), ControlSocket: started.ControlSocket()}, nil
		},
		ProcessAlive:    func(pid int) bool { return pid == os.Getpid() && alive.Load() },
		NewCaptureID:    func() (string, error) { return "capture-broken", nil },
		NewControlToken: func() (string, error) { return "secret", nil },
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if _, err := manager.On(context.Background(), ManagerOnOptions{}); err != nil {
		t.Fatalf("On() error = %v", err)
	}
	_, _ = started.Close(CaptureUnclean)
	alive.Store(false)

	status, err := manager.Status(context.Background())
	if err == nil || status.State != ManagerStateBroken {
		t.Fatalf("Status() = %+v, %v; want BROKEN", status, err)
	}
	if len(status.Problems) == 0 {
		t.Fatalf("broken status has no problems: %+v", status)
	}
}

func waitAtomicFalse(value *atomic.Bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !value.Load() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return !value.Load()
}

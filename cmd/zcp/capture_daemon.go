package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
	"github.com/zeropsio/zcp/internal/server"
)

const captureDaemonTokenEnv = "ZCP_CAPTURE_DAEMON_TOKEN"

func newDefaultCaptureManager() (*capture.Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve zcp executable: %w", err)
	}
	stateDir := filepath.Join(home, ".local", "state", "zcp", "capture-runtime")
	captureRoot := filepath.Join(home, ".local", "state", "zcp", "captures")
	claudeConfigDir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if claudeConfigDir == "" {
		claudeConfigDir = filepath.Join(home, ".claude")
	}
	controlDir := captureControlDir(stateDir)
	starter := func(ctx context.Context, cfg capture.DaemonStartConfig) (capture.DaemonReady, error) {
		if err := os.MkdirAll(controlDir, 0o700); err != nil {
			return capture.DaemonReady{}, fmt.Errorf("create capture control directory: %w", err)
		}
		if err := os.Chmod(controlDir, 0o700); err != nil {
			return capture.DaemonReady{}, fmt.Errorf("set capture control directory permissions: %w", err)
		}
		return startCaptureDaemonProcess(ctx, executable, stateDir, cfg)
	}
	return capture.NewManager(capture.ManagerConfig{
		StateDir:           stateDir,
		CaptureRoot:        captureRoot,
		ClaudeSettingsPath: filepath.Join(claudeConfigDir, "settings.json"),
		ControlSocket:      filepath.Join(controlDir, "control.sock"),
		DefaultUpstreamURL: captureUpstreamFromEnv(),
		ListenAddr:         "127.0.0.1:0",
		StartDaemon:        starter,
		TerminateProcess: func(pid int, captureID string) error {
			return terminateCaptureDaemon(executable, pid, captureID, false)
		},
		KillProcess: func(pid int, captureID string) error {
			return terminateCaptureDaemon(executable, pid, captureID, true)
		},
	})
}

func startCaptureDaemonProcess(ctx context.Context, executable, stateDir string, cfg capture.DaemonStartConfig) (capture.DaemonReady, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return capture.DaemonReady{}, fmt.Errorf("create daemon state directory: %w", err)
	}
	readyPath := filepath.Join(stateDir, "ready-"+cfg.CaptureID+".json")
	_ = os.Remove(readyPath)
	logPath := filepath.Join(stateDir, "daemon-"+cfg.CaptureID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return capture.DaemonReady{}, fmt.Errorf("create capture daemon log: %w", err)
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		_ = logFile.Close()
		return capture.DaemonReady{}, fmt.Errorf("open null input for capture daemon: %w", err)
	}
	args := []string{
		"capture", "daemon",
		"--capture-id", cfg.CaptureID,
		"--root", cfg.CaptureRoot,
		"--label", cfg.Label,
		"--listen", cfg.ListenAddr,
		"--upstream", cfg.UpstreamURL,
		"--control-socket", cfg.ControlSocket,
		"--ready-file", readyPath,
	}
	cmd := exec.CommandContext(context.WithoutCancel(ctx), executable, args...)
	cmd.Stdin = devNull
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = appendEnvironmentOverride(os.Environ(), captureDaemonTokenEnv, cfg.ControlToken)
	configureCaptureDaemonCommand(cmd)
	if err := cmd.Start(); err != nil {
		_ = devNull.Close()
		_ = logFile.Close()
		return capture.DaemonReady{}, fmt.Errorf("start capture daemon: %w", err)
	}
	_ = devNull.Close()
	_ = logFile.Close()

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case waitErr := <-waited:
			return capture.DaemonReady{}, fmt.Errorf("capture daemon exited before readiness (log %s): %w", logPath, waitErr)
		case <-ctx.Done():
			cleanupErr := stopUnreadyCaptureDaemon(cmd, waited)
			return capture.DaemonReady{}, errors.Join(fmt.Errorf("wait for capture daemon readiness: %w", ctx.Err()), cleanupErr)
		case <-timeout.C:
			cleanupErr := stopUnreadyCaptureDaemon(cmd, waited)
			return capture.DaemonReady{}, errors.Join(fmt.Errorf("capture daemon readiness timed out (log %s)", logPath), cleanupErr)
		case <-ticker.C:
			data, readErr := os.ReadFile(readyPath)
			if errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			if readErr != nil {
				cleanupErr := stopUnreadyCaptureDaemon(cmd, waited)
				return capture.DaemonReady{}, errors.Join(fmt.Errorf("read capture daemon readiness: %w", readErr), cleanupErr)
			}
			var ready capture.DaemonReady
			if err := json.Unmarshal(data, &ready); err != nil {
				cleanupErr := stopUnreadyCaptureDaemon(cmd, waited)
				return capture.DaemonReady{}, errors.Join(fmt.Errorf("decode capture daemon readiness: %w", err), cleanupErr)
			}
			_ = os.Remove(readyPath)
			if ready.ProcessID != cmd.Process.Pid {
				cleanupErr := stopUnreadyCaptureDaemon(cmd, waited)
				return capture.DaemonReady{}, errors.Join(fmt.Errorf("capture daemon readiness pid = %d, started pid %d", ready.ProcessID, cmd.Process.Pid), cleanupErr)
			}
			return ready, nil
		}
	}
}

func stopUnreadyCaptureDaemon(cmd *exec.Cmd, waited <-chan error) error {
	stopErr := stopStartingCaptureDaemon(cmd)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-waited:
		return nil
	case <-timer.C:
	}
	killErr := cmd.Process.Kill()
	killTimer := time.NewTimer(2 * time.Second)
	defer killTimer.Stop()
	select {
	case <-waited:
		return nil
	case <-killTimer.C:
		return errors.Join(stopErr, killErr, errors.New("capture daemon remains unreaped after forced startup rollback"))
	}
}

func runCaptureDaemon(args []string) int {
	flags := flag.NewFlagSet("capture daemon", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	captureID := flags.String("capture-id", "", "capture window ID")
	root := flags.String("root", "", "capture storage root")
	label := flags.String("label", "capture-window", "capture label")
	listen := flags.String("listen", "127.0.0.1:0", "provider listener")
	upstream := flags.String("upstream", capture.DefaultUpstreamBaseURL, "provider upstream")
	controlSocket := flags.String("control-socket", "", "private control socket")
	readyFile := flags.String("ready-file", "", "manager readiness file")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	token := os.Getenv(captureDaemonTokenEnv)
	if *captureID == "" || *root == "" || *controlSocket == "" || *readyFile == "" || token == "" {
		fmt.Fprintln(os.Stderr, "capture daemon: incomplete manager contract")
		return 2
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), captureShutdownSignals()...)
	defer stopSignals()
	runtime, err := capture.StartRuntime(signalCtx, capture.RuntimeConfig{
		RootDir:       *root,
		CaptureID:     *captureID,
		Label:         *label,
		ListenAddr:    *listen,
		UpstreamURL:   *upstream,
		ControlSocket: *controlSocket,
		ControlToken:  token,
		Command:       []string{"zcp", "capture", "on"},
		Build: capture.CaptureBuildInfo{
			Version: server.Version,
			Commit:  server.Commit,
			Built:   server.Built,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture daemon: %v\n", err)
		return 1
	}
	ready := capture.DaemonReady{
		ProcessID:     os.Getpid(),
		ProxyURL:      runtime.ProxyURL(),
		SessionDir:    runtime.SessionDir(),
		ControlSocket: runtime.ControlSocket(),
	}
	if err := writeDaemonReady(*readyFile, ready); err != nil {
		_, _ = runtime.Close(signalCtx, capture.CapturePartial)
		fmt.Fprintf(os.Stderr, "capture daemon: publish readiness: %v\n", err)
		return 1
	}

	select {
	case <-runtime.ShutdownRequested():
	case <-signalCtx.Done():
	}
	status, closeErr := runtime.Close(signalCtx, capture.CaptureComplete)
	if closeErr != nil {
		fmt.Fprintf(os.Stderr, "capture daemon close (%s): %v\n", status, closeErr)
		return 1
	}
	return 0
}

func writeDaemonReady(path string, ready capture.DaemonReady) error {
	data, err := json.Marshal(ready)
	if err != nil {
		return fmt.Errorf("encode readiness: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create readiness directory: %w", err)
	}
	temp, err := os.CreateTemp(directory, ".ready-*.tmp")
	if err != nil {
		return fmt.Errorf("create readiness temp file: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set readiness permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write readiness: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync readiness: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close readiness: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace readiness: %w", err)
	}
	removeTemp = false
	return nil
}

func appendEnvironmentOverride(environment []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, prefix+value)
}

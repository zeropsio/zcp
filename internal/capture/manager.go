package capture

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const (
	managerStateFilename = "active.json"
	managerLockFilename  = "lifecycle.lock"
	managerStateFormat   = "zcp-capture-manager-1"

	ManagerStateOff      = "OFF"
	ManagerStateOn       = "ON"
	ManagerStateBroken   = "BROKEN"
	managerPhaseEnabling = "enabling"
	managerPhaseOn       = "on"
)

// DaemonStartConfig is the manager-to-daemon startup contract.
type DaemonStartConfig struct {
	CaptureRoot   string
	CaptureID     string
	Label         string
	ListenAddr    string
	UpstreamURL   string
	ControlSocket string
	ControlToken  string
}

// DaemonReady is accepted only after the daemon has bound and health-checked
// both provider and control listeners.
type DaemonReady struct {
	ProcessID     int    `json:"processId"`
	ProxyURL      string `json:"proxyUrl"`
	SessionDir    string `json:"sessionDir"`
	ControlSocket string `json:"controlSocket"`
}

type DaemonStarter func(context.Context, DaemonStartConfig) (DaemonReady, error)

// ManagerConfig separates filesystem/process policy from the state machine so
// lifecycle failure paths can be exercised without spawning test binaries.
type ManagerConfig struct {
	StateDir           string
	CaptureRoot        string
	ClaudeSettingsPath string
	ControlSocket      string
	DefaultUpstreamURL string
	ListenAddr         string
	StartDaemon        DaemonStarter
	ProcessAlive       func(int) bool
	TerminateProcess   func(int, string) error
	KillProcess        func(int, string) error
	NewCaptureID       func() (string, error)
	NewControlToken    func() (string, error)
}

type ManagerOnOptions struct {
	Label       string
	UpstreamURL string
	ListenAddr  string
}

// ManagerStatus is reconciled state, not a copy of the manager journal.
type ManagerStatus struct {
	State      string
	CaptureID  string
	ProxyURL   string
	SessionDir string
	ProcessID  int
	StartedAt  time.Time
	Problems   []string
	Warnings   []string
}

// Connection is the minimal active-window contract consumed by eval runners.
// It carries no client-specific configuration mutation capability.
type Connection struct {
	CaptureID  string
	ProxyURL   string
	SessionDir string
	Control    *ControlClient
}

func (c *Connection) Close() {
	if c != nil && c.Control != nil {
		c.Control.Close()
	}
}

func (c *Connection) Mark(ctx context.Context, marker LifecycleMarker) (LifecycleRecord, error) {
	if c == nil || c.Control == nil {
		return LifecycleRecord{}, errors.New("capture connection is not active")
	}
	return c.Control.Mark(ctx, marker)
}

type managerStateDocument struct {
	FormatVersion string              `json:"formatVersion"`
	Phase         string              `json:"phase"`
	CaptureID     string              `json:"captureId"`
	ProxyURL      string              `json:"proxyUrl"`
	SessionDir    string              `json:"sessionDir"`
	ProcessID     int                 `json:"processId"`
	ControlSocket string              `json:"controlSocket"`
	ControlToken  string              `json:"controlToken"`
	StartedAt     time.Time           `json:"startedAt"`
	ClaudePatch   ClaudeSettingsPatch `json:"claudePatch"`
}

type Manager struct{ config ManagerConfig }

func NewManager(cfg ManagerConfig) (*Manager, error) {
	for name, value := range map[string]string{
		"state directory":      cfg.StateDir,
		"capture root":         cfg.CaptureRoot,
		"Claude settings path": cfg.ClaudeSettingsPath,
		"control socket":       cfg.ControlSocket,
		"default upstream URL": cfg.DefaultUpstreamURL,
	} {
		if value == "" {
			return nil, fmt.Errorf("capture manager %s is required", name)
		}
	}
	if cfg.StartDaemon == nil {
		return nil, errors.New("capture manager daemon starter is required")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:0"
	}
	if cfg.ProcessAlive == nil {
		cfg.ProcessAlive = processAlive
	}
	if cfg.TerminateProcess == nil {
		cfg.TerminateProcess = func(_ int, _ string) error { return errors.New("capture daemon termination is not configured") }
	}
	if cfg.KillProcess == nil {
		cfg.KillProcess = func(_ int, _ string) error { return errors.New("capture daemon forced termination is not configured") }
	}
	if cfg.NewCaptureID == nil {
		cfg.NewCaptureID = newManagerCaptureID
	}
	if cfg.NewControlToken == nil {
		cfg.NewControlToken = newManagerControlToken
	}
	return &Manager{config: cfg}, nil
}

func (m *Manager) On(ctx context.Context, options ManagerOnOptions) (ManagerStatus, error) {
	unlock, err := m.lock()
	if err != nil {
		return ManagerStatus{}, err
	}
	defer unlock()

	state, exists, err := m.readState()
	if err != nil {
		return ManagerStatus{State: ManagerStateBroken, Problems: []string{err.Error()}}, err
	}
	if exists {
		status, statusErr := m.statusLocked(ctx, state)
		if statusErr == nil && status.State == ManagerStateOn {
			return status, nil
		}
		return status, fmt.Errorf("capture lifecycle is broken; run `zcp capture off` before enabling again: %w", statusErr)
	}
	if _, socketErr := os.Lstat(m.config.ControlSocket); socketErr == nil {
		return ManagerStatus{State: ManagerStateBroken, Problems: []string{"capture control socket exists without manager state"}}, errors.New("capture control socket exists without manager state; run `zcp capture off` to reconcile it")
	} else if !errors.Is(socketErr, os.ErrNotExist) {
		return ManagerStatus{State: ManagerStateBroken, Problems: []string{socketErr.Error()}}, fmt.Errorf("stat capture control socket: %w", socketErr)
	}

	captureID, err := m.config.NewCaptureID()
	if err != nil {
		return ManagerStatus{}, fmt.Errorf("create capture ID: %w", err)
	}
	token, err := m.config.NewControlToken()
	if err != nil {
		return ManagerStatus{}, fmt.Errorf("create capture control token: %w", err)
	}
	upstream := options.UpstreamURL
	if upstream == "" {
		prior, priorExists, readErr := ClaudeSettingString(m.config.ClaudeSettingsPath, "ANTHROPIC_BASE_URL")
		if readErr != nil {
			return ManagerStatus{}, readErr
		}
		if priorExists {
			if managerLoopbackURL(prior) {
				return ManagerStatus{}, fmt.Errorf("existing ANTHROPIC_BASE_URL %q is loopback; pass --upstream explicitly so capture cannot silently recurse or bypass the current gateway", prior)
			}
			upstream = prior
		} else {
			upstream = m.config.DefaultUpstreamURL
		}
	}
	listenAddr := options.ListenAddr
	if listenAddr == "" {
		listenAddr = m.config.ListenAddr
	}
	if options.Label == "" {
		options.Label = "capture-window"
	}
	if err := os.MkdirAll(m.config.StateDir, 0o700); err != nil {
		return ManagerStatus{}, fmt.Errorf("create capture manager state directory: %w", err)
	}
	if err := os.Chmod(m.config.StateDir, 0o700); err != nil {
		return ManagerStatus{}, fmt.Errorf("set capture manager state permissions: %w", err)
	}
	pending := managerStateDocument{
		FormatVersion: managerStateFormat,
		Phase:         managerPhaseEnabling,
		CaptureID:     captureID,
		ControlSocket: m.config.ControlSocket,
		ControlToken:  token,
		StartedAt:     time.Now().UTC(),
	}
	if err := m.writeState(pending); err != nil {
		return ManagerStatus{}, err
	}
	rollbackState := true
	defer func() {
		if rollbackState {
			_ = m.removeState()
		}
	}()

	ready, err := m.config.StartDaemon(ctx, DaemonStartConfig{
		CaptureRoot:   m.config.CaptureRoot,
		CaptureID:     captureID,
		Label:         options.Label,
		ListenAddr:    listenAddr,
		UpstreamURL:   upstream,
		ControlSocket: m.config.ControlSocket,
		ControlToken:  token,
	})
	if err != nil {
		return ManagerStatus{}, fmt.Errorf("start capture daemon: %w", err)
	}
	// Persist every ownership coordinate returned by a started daemon before
	// any later transaction step can fail. If rollback cannot prove process
	// exit, this journal remains the recovery capability for `capture off`.
	pending.ProcessID = ready.ProcessID
	pending.ProxyURL = ready.ProxyURL
	pending.SessionDir = ready.SessionDir
	pending.ControlSocket = ready.ControlSocket
	failAfterReady := func(cause error) (ManagerStatus, error) {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 12*time.Second)
		cleanupErr := m.stopReadyDaemon(cleanupCtx, ready, token, captureID)
		cancelCleanup()
		if cleanupErr == nil {
			return ManagerStatus{}, cause
		}
		rollbackState = false
		pending.Phase = managerPhaseEnabling
		persistErr := m.writeState(pending)
		status := managerStatusFromState(ManagerStateBroken, pending)
		status.Problems = append(status.Problems, cleanupErr.Error())
		if persistErr != nil {
			status.Problems = append(status.Problems, "retain capture ownership journal: "+persistErr.Error())
		}
		return status, errors.Join(cause, fmt.Errorf("rollback started capture daemon: %w", cleanupErr), persistErr)
	}
	if err := m.writeState(pending); err != nil {
		return failAfterReady(fmt.Errorf("journal capture daemon readiness: %w", err))
	}
	if ready.ProxyURL == "" || ready.SessionDir == "" || ready.ProcessID <= 0 || ready.ControlSocket != m.config.ControlSocket {
		return failAfterReady(fmt.Errorf("capture daemon returned incomplete readiness: %+v", ready))
	}
	client := NewControlClient(ready.ControlSocket, token)
	healthCtx, cancelHealth := context.WithTimeout(ctx, 3*time.Second)
	health, healthErr := client.Status(healthCtx)
	cancelHealth()
	client.Close()
	if healthErr != nil {
		return failAfterReady(fmt.Errorf("verify capture daemon readiness: %w", healthErr))
	}
	if health.CaptureID != captureID || health.ProxyURL != ready.ProxyURL || health.ProcessID != ready.ProcessID {
		return failAfterReady(fmt.Errorf("capture daemon readiness identity mismatch: ready=%+v status=%+v", ready, health))
	}
	values := map[string]string{
		"ANTHROPIC_BASE_URL": ready.ProxyURL,
		EnvSessionID:         captureID,
		EnvSessionDir:        ready.SessionDir,
	}
	patch, err := PrepareClaudeCaptureSettings(m.config.ClaudeSettingsPath, values)
	if err != nil {
		return failAfterReady(fmt.Errorf("prepare Claude capture settings: %w", err))
	}
	// Journal the exact restore patch before the atomic settings rename. A
	// process crash can therefore leave either old or ZCP-owned values, both of
	// which a later `off` reconciles safely.
	pending.ClaudePatch = patch
	if err := m.writeState(pending); err != nil {
		return failAfterReady(err)
	}
	if err := ApplyClaudeCaptureSettings(m.config.ClaudeSettingsPath, patch); err != nil {
		return failAfterReady(fmt.Errorf("install Claude capture settings: %w", err))
	}
	pending.Phase = managerPhaseOn
	if err := m.writeState(pending); err != nil {
		pending.Phase = managerPhaseEnabling
		_, restoreErr := RestoreClaudeCaptureSettings(m.config.ClaudeSettingsPath, patch)
		return failAfterReady(errors.Join(err, restoreErr))
	}
	rollbackState = false
	return managerStatusFromState(ManagerStateOn, pending), nil
}

func (m *Manager) Off(ctx context.Context) (ManagerStatus, error) {
	unlock, err := m.lock()
	if err != nil {
		return ManagerStatus{}, err
	}
	defer unlock()
	state, exists, err := m.readState()
	if err != nil {
		return ManagerStatus{State: ManagerStateBroken, Problems: []string{err.Error()}}, err
	}
	if !exists {
		if err := reconcileStaleControlSocket(m.config.ControlSocket); err != nil {
			return ManagerStatus{State: ManagerStateBroken, Problems: []string{err.Error()}}, err
		}
		return ManagerStatus{State: ManagerStateOff}, nil
	}
	warnings, err := RestoreClaudeCaptureSettings(m.config.ClaudeSettingsPath, state.ClaudePatch)
	if err != nil {
		status := managerStatusFromState(ManagerStateBroken, state)
		status.Problems = append(status.Problems, err.Error())
		return status, fmt.Errorf("restore Claude capture settings: %w", err)
	}

	client := NewControlClient(state.ControlSocket, state.ControlToken)
	shutdownCtx, cancelShutdown := context.WithTimeout(ctx, 3*time.Second)
	shutdownErr := client.Shutdown(shutdownCtx)
	cancelShutdown()
	client.Close()
	if shutdownErr != nil {
		warnings = append(warnings, "capture control shutdown failed; falling back to an identity-checked process signal: "+shutdownErr.Error())
	}
	if err := m.waitForProcessExit(ctx, state.ProcessID, 3*time.Second); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return managerStatusFromState(ManagerStateBroken, state), err
	}
	if m.config.ProcessAlive(state.ProcessID) {
		if err := m.config.TerminateProcess(state.ProcessID, state.CaptureID); err != nil {
			status := managerStatusFromState(ManagerStateBroken, state)
			status.Warnings = warnings
			status.Problems = append(status.Problems, err.Error())
			return status, fmt.Errorf("terminate capture daemon: %w", err)
		}
		if err := m.waitForProcessExit(ctx, state.ProcessID, 5*time.Second); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return managerStatusFromState(ManagerStateBroken, state), err
		}
	}
	if m.config.ProcessAlive(state.ProcessID) {
		if err := m.config.KillProcess(state.ProcessID, state.CaptureID); err != nil {
			status := managerStatusFromState(ManagerStateBroken, state)
			status.Warnings = warnings
			status.Problems = append(status.Problems, err.Error())
			return status, fmt.Errorf("kill capture daemon: %w", err)
		}
		if err := m.waitForProcessExit(ctx, state.ProcessID, 2*time.Second); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return managerStatusFromState(ManagerStateBroken, state), err
		}
	}
	if m.config.ProcessAlive(state.ProcessID) {
		status := managerStatusFromState(ManagerStateBroken, state)
		status.Warnings = warnings
		status.Problems = append(status.Problems, "capture daemon remains alive after forced termination")
		return status, errors.New("capture daemon remains alive after forced termination")
	}
	sessionDir := state.SessionDir
	if sessionDir == "" && state.CaptureID != "" {
		sessionDir = filepath.Join(m.config.CaptureRoot, state.CaptureID)
	}
	if _, statErr := os.Stat(filepath.Join(sessionDir, manifestFilename)); statErr == nil {
		if recovered, recoverErr := RecoverUncleanSessionManifest(sessionDir); recoverErr != nil {
			status := managerStatusFromState(ManagerStateBroken, state)
			status.Warnings = warnings
			status.Problems = append(status.Problems, recoverErr.Error())
			return status, fmt.Errorf("recover stopped capture manifest: %w", recoverErr)
		} else if recovered {
			warnings = append(warnings, "capture daemon ended without finalization; durable raw prefix was inventoried as unclean")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		status := managerStatusFromState(ManagerStateBroken, state)
		status.Warnings = warnings
		status.Problems = append(status.Problems, statErr.Error())
		return status, fmt.Errorf("stat stopped capture manifest: %w", statErr)
	}
	if err := os.Remove(state.ControlSocket); err != nil && !errors.Is(err, os.ErrNotExist) {
		status := managerStatusFromState(ManagerStateBroken, state)
		status.Warnings = warnings
		status.Problems = append(status.Problems, err.Error())
		return status, fmt.Errorf("remove stopped capture control socket: %w", err)
	}
	if err := m.removeState(); err != nil {
		return ManagerStatus{State: ManagerStateBroken, Warnings: warnings, Problems: []string{err.Error()}}, err
	}
	return ManagerStatus{State: ManagerStateOff, Warnings: warnings}, nil
}

func (m *Manager) Status(ctx context.Context) (ManagerStatus, error) {
	unlock, err := m.lock()
	if err != nil {
		return ManagerStatus{}, err
	}
	defer unlock()
	state, exists, err := m.readState()
	if err != nil {
		return ManagerStatus{State: ManagerStateBroken, Problems: []string{err.Error()}}, err
	}
	if !exists {
		if _, socketErr := os.Lstat(m.config.ControlSocket); socketErr == nil {
			problem := "capture control socket exists without manager state"
			return ManagerStatus{State: ManagerStateBroken, Problems: []string{problem}}, errors.New(problem)
		} else if !errors.Is(socketErr, os.ErrNotExist) {
			return ManagerStatus{State: ManagerStateBroken, Problems: []string{socketErr.Error()}}, socketErr
		}
		return ManagerStatus{State: ManagerStateOff}, nil
	}
	return m.statusLocked(ctx, state)
}

func (m *Manager) statusLocked(ctx context.Context, state managerStateDocument) (ManagerStatus, error) {
	status := managerStatusFromState(ManagerStateOn, state)
	if state.FormatVersion != managerStateFormat {
		status.Problems = append(status.Problems, fmt.Sprintf("unsupported manager state format %q", state.FormatVersion))
	}
	if state.Phase != managerPhaseOn {
		status.Problems = append(status.Problems, fmt.Sprintf("manager transaction stopped in phase %q", state.Phase))
	}
	if !m.config.ProcessAlive(state.ProcessID) {
		status.Problems = append(status.Problems, fmt.Sprintf("capture daemon pid %d is not running", state.ProcessID))
	}
	values := map[string]string{"ANTHROPIC_BASE_URL": state.ProxyURL, EnvSessionID: state.CaptureID, EnvSessionDir: state.SessionDir}
	installed, settingsErr := ClaudeCaptureSettingsInstalled(m.config.ClaudeSettingsPath, values)
	if settingsErr != nil {
		status.Problems = append(status.Problems, settingsErr.Error())
	} else if !installed {
		status.Problems = append(status.Problems, "Claude capture settings do not match the active daemon")
	}
	client := NewControlClient(state.ControlSocket, state.ControlToken)
	controlCtx, cancel := context.WithTimeout(ctx, time.Second)
	controlStatus, controlErr := client.Status(controlCtx)
	cancel()
	client.Close()
	if controlErr != nil {
		status.Problems = append(status.Problems, controlErr.Error())
	} else {
		if controlStatus.CaptureID != state.CaptureID || controlStatus.ProxyURL != state.ProxyURL || controlStatus.ProcessID != state.ProcessID {
			status.Problems = append(status.Problems, "capture daemon identity differs from manager state")
		}
	}
	if len(status.Problems) > 0 {
		status.State = ManagerStateBroken
		return status, errors.New("capture lifecycle is inconsistent")
	}
	return status, nil
}

// ActiveClient returns the health-checked active control client. Callers own
// the returned client's Close method.
func (m *Manager) ActiveConnection(ctx context.Context) (*Connection, ManagerStatus, error) {
	// OFF discovery is a read-only hot path for every ordinary eval. Avoid
	// creating the manager directory and lifecycle lock when no journal exists.
	if _, err := os.Lstat(filepath.Join(m.config.StateDir, managerStateFilename)); errors.Is(err, os.ErrNotExist) {
		return nil, ManagerStatus{State: ManagerStateOff}, nil
	} else if err != nil {
		return nil, ManagerStatus{State: ManagerStateBroken}, fmt.Errorf("inspect capture manager state: %w", err)
	}

	unlock, err := m.lock()
	if err != nil {
		return nil, ManagerStatus{}, err
	}
	defer unlock()
	state, exists, err := m.readState()
	if err != nil {
		return nil, ManagerStatus{State: ManagerStateBroken}, err
	}
	if !exists {
		return nil, ManagerStatus{State: ManagerStateOff}, nil
	}
	status, err := m.statusLocked(ctx, state)
	if err != nil {
		return nil, status, err
	}
	return &Connection{
		CaptureID:  state.CaptureID,
		ProxyURL:   state.ProxyURL,
		SessionDir: state.SessionDir,
		Control:    NewControlClient(state.ControlSocket, state.ControlToken),
	}, status, nil
}

func (m *Manager) waitForProcessExit(ctx context.Context, pid int, timeout time.Duration) error {
	if !m.config.ProcessAlive(pid) {
		return nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for capture daemon: %w", ctx.Err())
		case <-timer.C:
			return context.DeadlineExceeded
		case <-ticker.C:
			if !m.config.ProcessAlive(pid) {
				return nil
			}
		}
	}
}

func (m *Manager) stopReadyDaemon(ctx context.Context, ready DaemonReady, token, captureID string) error {
	var shutdownErr error
	if ready.ControlSocket != "" {
		client := NewControlClient(ready.ControlSocket, token)
		shutdownCtx, cancel := context.WithTimeout(ctx, time.Second)
		shutdownErr = client.Shutdown(shutdownCtx)
		cancel()
		client.Close()
	}
	if ready.ProcessID <= 0 {
		// No process identity was declared, so there is no process capability to
		// retain or signal. A control error is still useful diagnostic context,
		// but it cannot prove that an owned process exists.
		return nil
	}
	if shutdownErr == nil {
		_ = m.waitForProcessExit(ctx, ready.ProcessID, time.Second)
	}
	if !m.config.ProcessAlive(ready.ProcessID) {
		return nil
	}
	terminateErr := m.config.TerminateProcess(ready.ProcessID, captureID)
	if terminateErr == nil {
		_ = m.waitForProcessExit(ctx, ready.ProcessID, 5*time.Second)
	}
	if !m.config.ProcessAlive(ready.ProcessID) {
		return nil
	}
	killErr := m.config.KillProcess(ready.ProcessID, captureID)
	if killErr == nil {
		_ = m.waitForProcessExit(ctx, ready.ProcessID, 2*time.Second)
	}
	if !m.config.ProcessAlive(ready.ProcessID) {
		return nil
	}
	return errors.Join(
		shutdownErr,
		terminateErr,
		killErr,
		fmt.Errorf("capture daemon pid %d remains alive after rollback", ready.ProcessID),
	)
}

func (m *Manager) lock() (func(), error) {
	if err := os.MkdirAll(m.config.StateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create capture manager state directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(m.config.StateDir, managerLockFilename), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open capture lifecycle lock: %w", err)
	}
	if err := lockManagerFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock capture lifecycle: %w", err)
	}
	return func() {
		_ = unlockManagerFile(file)
		_ = file.Close()
	}, nil
}

func (m *Manager) readState() (managerStateDocument, bool, error) {
	path := filepath.Join(m.config.StateDir, managerStateFilename)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return managerStateDocument{}, false, nil
	}
	if err != nil {
		return managerStateDocument{}, false, fmt.Errorf("read capture manager state: %w", err)
	}
	var state managerStateDocument
	if err := json.Unmarshal(data, &state); err != nil {
		return managerStateDocument{}, true, fmt.Errorf("decode capture manager state: %w", err)
	}
	return state, true, nil
}

func (m *Manager) writeState(state managerStateDocument) error {
	if err := os.MkdirAll(m.config.StateDir, 0o700); err != nil {
		return fmt.Errorf("create capture manager state directory: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode capture manager state: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(m.config.StateDir, managerStateFilename)
	temp, err := os.CreateTemp(m.config.StateDir, ".active-*.tmp")
	if err != nil {
		return fmt.Errorf("create capture manager state temp file: %w", err)
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
		return fmt.Errorf("set capture manager state permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write capture manager state: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync capture manager state: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close capture manager state: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace capture manager state: %w", err)
	}
	removeTemp = false
	return syncDirectory(m.config.StateDir)
}

func (m *Manager) removeState() error {
	path := filepath.Join(m.config.StateDir, managerStateFilename)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove capture manager state: %w", err)
	}
	return syncDirectory(m.config.StateDir)
}

func managerStatusFromState(state string, document managerStateDocument) ManagerStatus {
	return ManagerStatus{
		State:      state,
		CaptureID:  document.CaptureID,
		ProxyURL:   document.ProxyURL,
		SessionDir: document.SessionDir,
		ProcessID:  document.ProcessID,
		StartedAt:  document.StartedAt,
	}
}

func reconcileStaleControlSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat stale capture control socket: %w", err)
	}
	if info.Mode()&os.ModeSocket != 0 {
		connection, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			return errors.New("capture control socket is live without manager state; refusing to stop an unowned daemon")
		}
		if !managerConnectionRefused(dialErr) && !errors.Is(dialErr, os.ErrNotExist) {
			return fmt.Errorf("probe stale capture control socket: %w", dialErr)
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale capture control socket: %w", err)
	}
	return nil
}

func managerLoopbackURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	return host == "localhost" || host == "::1" || len(host) >= 4 && host[:4] == "127."
}

func newManagerCaptureID() (string, error) {
	random, err := randomHex(6)
	if err != nil {
		return "", err
	}
	return "capture-" + time.Now().UTC().Format("20060102T150405Z") + "-" + random, nil
}

func newManagerControlToken() (string, error) { return randomHex(32) }

func randomHex(bytesCount int) (string, error) {
	buffer := make([]byte, bytesCount)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("read crypto randomness: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

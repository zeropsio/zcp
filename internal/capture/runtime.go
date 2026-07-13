package capture

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"
)

// RuntimeConfig starts one provider/lifecycle capture window. Client
// configuration and background-process ownership remain the manager's job.
type RuntimeConfig struct {
	RootDir       string
	CaptureID     string
	Label         string
	ListenAddr    string
	UpstreamURL   string
	ControlSocket string
	ControlToken  string
	Command       []string
	Build         CaptureBuildInfo
}

// Runtime owns all live writers/listeners for one capture window.
type Runtime struct {
	captureID  string
	sessionDir string
	proxy      *ProxyServer
	recorder   *Recorder
	lifecycle  *LifecycleRecorder
	manifest   *SessionManifest
	control    *ControlServer
	cancel     context.CancelFunc

	shutdownOnce sync.Once
	shutdownCh   chan struct{}
	closeOnce    sync.Once
	closeStatus  string
	closeErr     error
}

func StartRuntime(parent context.Context, cfg RuntimeConfig) (*Runtime, error) {
	if cfg.CaptureID == "" {
		return nil, errors.New("capture runtime ID is required")
	}
	if cfg.ControlSocket == "" || cfg.ControlToken == "" {
		return nil, errors.New("capture runtime control socket and token are required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &Runtime{captureID: cfg.CaptureID, cancel: cancel, shutdownCh: make(chan struct{})}
	cleanupFailure := func(err error) (*Runtime, error) {
		cancel()
		if runtime.control != nil {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = runtime.control.Close(closeCtx)
			closeCancel()
		}
		if runtime.proxy != nil {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = runtime.proxy.Shutdown(closeCtx)
			closeCancel()
		}
		if runtime.lifecycle != nil {
			_ = runtime.lifecycle.Close(CapturePartial)
		}
		if runtime.recorder != nil {
			_ = runtime.recorder.CloseDaemon(CapturePartial)
		}
		if runtime.manifest != nil {
			_ = runtime.manifest.FinalizeDaemon(CapturePartial)
		}
		return nil, err
	}

	recorder, err := NewRecorder(RecorderConfig{RootDir: cfg.RootDir, SessionID: cfg.CaptureID, Label: cfg.Label})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("initialize provider recorder: %w", err)
	}
	runtime.recorder = recorder
	runtime.sessionDir = recorder.SessionDir()

	proxy, err := StartProxy(ctx, ProxyConfig{ListenAddr: cfg.ListenAddr, UpstreamBaseURL: cfg.UpstreamURL, Recorder: recorder})
	if err != nil {
		return cleanupFailure(fmt.Errorf("start provider proxy: %w", err))
	}
	runtime.proxy = proxy
	origin, err := runtimeProviderOrigin(cfg.UpstreamURL)
	if err != nil {
		return cleanupFailure(err)
	}
	manifest, err := NewSessionManifest(SessionManifestConfig{
		SessionDir: recorder.SessionDir(),
		SessionID:  cfg.CaptureID,
		Label:      cfg.Label,
		Command:    cfg.Command,
		Build:      cfg.Build,
		Provider:   ProviderManifestInfo{Origin: origin, ProxyURL: proxy.URL()},
	})
	if err != nil {
		return cleanupFailure(fmt.Errorf("start capture manifest: %w", err))
	}
	runtime.manifest = manifest
	lifecycle, err := NewLifecycleRecorder(recorder.SessionDir(), cfg.CaptureID)
	if err != nil {
		return cleanupFailure(fmt.Errorf("start lifecycle recorder: %w", err))
	}
	runtime.lifecycle = lifecycle
	control, err := StartControlServer(ctx, ControlServerConfig{
		SocketPath: cfg.ControlSocket,
		Token:      cfg.ControlToken,
		Recorder:   lifecycle,
		StatusFunc: func() ControlStatus {
			return ControlStatus{
				CaptureID:  cfg.CaptureID,
				Status:     CaptureRunning,
				ProxyURL:   proxy.URL(),
				ProcessID:  os.Getpid(),
				SessionDir: recorder.SessionDir(),
			}
		},
		Shutdown: runtime.requestShutdown,
	})
	if err != nil {
		return cleanupFailure(fmt.Errorf("start capture control server: %w", err))
	}
	runtime.control = control
	go func() {
		select {
		case <-parent.Done():
			runtime.requestShutdown()
		case <-runtime.shutdownCh:
		}
	}()
	return runtime, nil
}

func runtimeProviderOrigin(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse provider origin: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("provider origin %q must include scheme and host", rawURL)
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String(), nil
}

func (r *Runtime) requestShutdown() {
	r.shutdownOnce.Do(func() { close(r.shutdownCh) })
}

func (r *Runtime) ShutdownRequested() <-chan struct{} { return r.shutdownCh }
func (r *Runtime) ProxyURL() string                   { return r.proxy.URL() }
func (r *Runtime) ControlSocket() string              { return r.control.SocketPath() }
func (r *Runtime) SessionDir() string                 { return r.sessionDir }
func (r *Runtime) CaptureID() string                  { return r.captureID }

// Close bounded-drains listeners, closes all raw streams, and writes the
// terminal daemon manifest. Returned status is downgraded to partial on any
// capture component failure.
func (r *Runtime) Close(requestedStatus string) (string, error) {
	return r.close(requestedStatus, nil)
}

// CloseChild finalizes an explicit wrapper window with the observed child's
// exit code while sharing the same runtime/control implementation as `on`.
func (r *Runtime) CloseChild(requestedStatus string, childExitCode int) (string, error) {
	return r.close(requestedStatus, &childExitCode)
}

func (r *Runtime) close(requestedStatus string, childExitCode *int) (string, error) {
	r.closeOnce.Do(func() {
		status := requestedStatus
		if status == "" || status == CaptureRunning {
			status = CaptureComplete
		}
		var errs []error
		if proxyErr := r.proxy.CaptureError(); proxyErr != nil {
			status = CapturePartial
			errs = append(errs, proxyErr)
		}
		if controlErr := r.control.Error(); controlErr != nil {
			status = CapturePartial
			errs = append(errs, controlErr)
		}

		controlCtx, controlCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := r.control.Close(controlCtx); err != nil {
			status = CapturePartial
			errs = append(errs, err)
		}
		controlCancel()
		proxyCtx, proxyCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := r.proxy.Shutdown(proxyCtx); err != nil {
			status = CapturePartial
			errs = append(errs, err)
		}
		proxyCancel()
		r.cancel()
		if err := r.lifecycle.Close(status); err != nil {
			status = CapturePartial
			errs = append(errs, err)
		}
		var recorderErr error
		if childExitCode == nil {
			recorderErr = r.recorder.CloseDaemon(status)
		} else {
			recorderErr = r.recorder.Close(status, *childExitCode)
		}
		if recorderErr != nil {
			status = CapturePartial
			errs = append(errs, recorderErr)
		}
		var manifestErr error
		if childExitCode == nil {
			manifestErr = r.manifest.FinalizeDaemon(status)
		} else {
			manifestErr = r.manifest.Finalize(status, *childExitCode)
		}
		if manifestErr != nil {
			status = CapturePartial
			errs = append(errs, manifestErr)
		}
		r.closeStatus = status
		r.closeErr = errors.Join(errs...)
	})
	return r.closeStatus, r.closeErr
}

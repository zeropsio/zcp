package capture

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	controlTokenHeader = "X-ZCP-Capture-Token"
	controlBodyLimit   = 64 * 1024
)

var ErrControlUnauthorized = errors.New("capture control unauthorized")

// ControlStatus is the daemon truth returned over its private Unix socket.
type ControlStatus struct {
	CaptureID  string `json:"captureId"`
	Status     string `json:"status"`
	ProxyURL   string `json:"proxyUrl"`
	ProcessID  int    `json:"processId"`
	SessionDir string `json:"sessionDir,omitempty"`
}

// ControlServerConfig exposes low-volume lifecycle/control operations without
// sharing the provider listener or its credential-bearing HTTP surface.
type ControlServerConfig struct {
	SocketPath string
	Token      string
	Recorder   *LifecycleRecorder
	Status     ControlStatus
	StatusFunc func() ControlStatus
	Shutdown   func()
}

type ControlServer struct {
	socketPath string
	listener   net.Listener
	server     *http.Server
	done       chan struct{}
	closeOnce  sync.Once
	closeErr   error
	errMu      sync.Mutex
	serveErr   error
}

func StartControlServer(ctx context.Context, cfg ControlServerConfig) (*ControlServer, error) {
	if cfg.SocketPath == "" {
		return nil, errors.New("capture control socket path is required")
	}
	if cfg.Token == "" {
		return nil, errors.New("capture control token is required")
	}
	if cfg.Recorder == nil {
		return nil, errors.New("capture lifecycle recorder is required")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SocketPath), 0o700); err != nil {
		return nil, fmt.Errorf("create capture control directory: %w", err)
	}
	if err := os.Remove(cfg.SocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale capture control socket: %w", err)
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "unix", cfg.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on capture control socket: %w", err)
	}
	if err := os.Chmod(cfg.SocketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(cfg.SocketPath)
		return nil, fmt.Errorf("set capture control socket permissions: %w", err)
	}

	mux := http.NewServeMux()
	server := &ControlServer{socketPath: cfg.SocketPath, listener: listener, done: make(chan struct{})}
	authorized := func(r *http.Request) bool {
		provided := r.Header.Get(controlTokenHeader)
		return len(provided) == len(cfg.Token) && subtle.ConstantTimeCompare([]byte(provided), []byte(cfg.Token)) == 1
	}
	writeJSON := func(w http.ResponseWriter, status int, value any) {
		data, err := json.Marshal(value)
		if err != nil {
			http.Error(w, "encode control response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(append(data, '\n'))
	}
	guard := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !authorized(r) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /v1/status", guard(func(w http.ResponseWriter, _ *http.Request) {
		status := cfg.Status
		if cfg.StatusFunc != nil {
			status = cfg.StatusFunc()
		}
		writeJSON(w, http.StatusOK, status)
	}))
	mux.HandleFunc("POST /v1/markers", guard(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, controlBodyLimit))
		decoder.DisallowUnknownFields()
		var marker LifecycleMarker
		if err := decoder.Decode(&marker); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		record, err := cfg.Recorder.Mark(marker)
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, record)
	}))
	mux.HandleFunc("POST /v1/shutdown", guard(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
		if cfg.Shutdown != nil {
			go cfg.Shutdown()
		}
	}))
	server.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		serveErr := server.server.Serve(listener)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			server.errMu.Lock()
			server.serveErr = fmt.Errorf("serve capture control socket: %w", serveErr)
			server.errMu.Unlock()
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = server.Close(closeCtx)
		case <-server.done:
		}
	}()
	return server, nil
}

func (s *ControlServer) SocketPath() string { return s.socketPath }

func (s *ControlServer) Error() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.serveErr
}

func (s *ControlServer) Close(ctx context.Context) error {
	s.closeOnce.Do(func() {
		close(s.done)
		shutdownErr := s.server.Shutdown(ctx)
		if errors.Is(shutdownErr, http.ErrServerClosed) {
			shutdownErr = nil
		}
		removeErr := os.Remove(s.socketPath)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		s.closeErr = errors.Join(shutdownErr, removeErr, s.Error())
	})
	if s.closeErr != nil {
		return fmt.Errorf("close capture control server: %w", s.closeErr)
	}
	return nil
}

// ControlClient talks to one private daemon control socket.
type ControlClient struct {
	socketPath string
	token      string
	client     *http.Client
	transport  *http.Transport
}

func NewControlClient(socketPath, token string) *ControlClient {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		DisableCompression: true,
	}
	return &ControlClient{
		socketPath: socketPath,
		token:      token,
		client:     &http.Client{Transport: transport},
		transport:  transport,
	}
}

func (c *ControlClient) Status(ctx context.Context) (ControlStatus, error) {
	var status ControlStatus
	if err := c.doJSON(ctx, http.MethodGet, "/v1/status", nil, &status); err != nil {
		return ControlStatus{}, err
	}
	return status, nil
}

func (c *ControlClient) Mark(ctx context.Context, marker LifecycleMarker) (LifecycleRecord, error) {
	var record LifecycleRecord
	if err := c.doJSON(ctx, http.MethodPost, "/v1/markers", marker, &record); err != nil {
		return LifecycleRecord{}, err
	}
	return record, nil
}

func (c *ControlClient) Shutdown(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodPost, "/v1/shutdown", struct{}{}, nil)
}

func (c *ControlClient) Close() { c.transport.CloseIdleConnections() }

func (c *ControlClient) doJSON(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("marshal capture control request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://capture.local"+path, body)
	if err != nil {
		return fmt.Errorf("build capture control request: %w", err)
	}
	request.Header.Set(controlTokenHeader, c.token)
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("call capture control %s: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, response.Body)
		return ErrControlUnauthorized
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("capture control %s returned %s: %s", path, response.Status, string(limited))
	}
	if output == nil {
		_, err := io.Copy(io.Discard, response.Body)
		return err
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return fmt.Errorf("decode capture control %s response: %w", path, err)
	}
	return nil
}

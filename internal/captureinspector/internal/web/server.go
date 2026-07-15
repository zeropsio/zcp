// Package web serves the local, read-only browser over immutable capture
// evidence. It is compiler-private to the captureinspector domain and separate
// from provider and MCP hot paths.
package web

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
	"github.com/zeropsio/zcp/internal/captureinspector/internal/projection"
)

type inspectorError string

func (err inspectorError) Error() string { return string(err) }

const errCaptureIntegrityInvalid inspectorError = "capture integrity is invalid; plaintext and raw detail queries are disabled"

const (
	capabilityCookie = "zcp_capture_ui"
	revealCookie     = "zcp_capture_reveal"
	requestBodyLimit = 64 * 1024
)

//go:embed assets/*
var assets embed.FS

type Config struct {
	ListenAddr      string
	CaptureRoot     string
	SessionDir      string
	CapabilityToken string
	RevealToken     string
}

type Server struct {
	config     Config
	listener   net.Listener
	httpServer *http.Server
	done       chan struct{}
	closeOnce  sync.Once
	closeErr   error
	launchMu   sync.Mutex
	launchUsed bool
	cacheMu    sync.Mutex
	cache      map[string]*projection.View
}

func Start(ctx context.Context, cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:0"
	}
	if err := validateLoopback(cfg.ListenAddr); err != nil {
		return nil, err
	}
	if cfg.CaptureRoot == "" {
		return nil, errors.New("capture UI root is required")
	}
	if cfg.CapabilityToken == "" || cfg.RevealToken == "" {
		return nil, errors.New("capture UI capability and reveal tokens are required")
	}
	rootInfo, err := os.Stat(cfg.CaptureRoot)
	if err != nil {
		return nil, fmt.Errorf("stat capture UI root: %w", err)
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("capture UI root %q is not a directory", cfg.CaptureRoot)
	}
	if cfg.SessionDir != "" {
		manifest, err := capture.ReadSessionManifest(filepath.Join(cfg.SessionDir, "manifest.json"))
		if err != nil {
			return nil, fmt.Errorf("open capture UI session: %w", err)
		}
		if manifest.SessionID == "" {
			return nil, errors.New("capture UI session manifest has no ID")
		}
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen for capture UI: %w", err)
	}
	server := &Server{config: cfg, listener: listener, done: make(chan struct{}), cache: make(map[string]*projection.View)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /launch/{token}", server.handleLaunch)
	mux.HandleFunc("GET /", server.authorized(server.handleIndex))
	mux.HandleFunc("GET /assets/{name}", server.authorized(server.handleAsset))
	mux.HandleFunc("GET /api/v1/captures", server.authorized(server.handleCaptures))
	mux.HandleFunc("POST /api/v1/reveal", server.authorized(server.handleReveal))
	mux.HandleFunc("GET /api/v1/compare", server.authorized(server.handleCompare))
	mux.HandleFunc("GET /api/v1/captures/{id}/view", server.authorized(server.handleView))
	mux.HandleFunc("GET /api/v1/captures/{id}/session-trace", server.authorized(server.handleSessionTrace))
	mux.HandleFunc("GET /api/v1/captures/{id}/trace-content", server.authorized(server.revealed(server.handleTraceContent)))
	mux.HandleFunc("GET /api/v1/captures/{id}/provider-events", server.authorized(server.handleProviderEvents))
	mux.HandleFunc("GET /api/v1/captures/{id}/provider-event", server.authorized(server.revealed(server.handleProviderEvent)))
	mux.HandleFunc("GET /api/v1/captures/{id}/records", server.authorized(server.handleRawRecords))
	mux.HandleFunc("GET /api/v1/captures/{id}/raw", server.authorized(server.revealed(server.handleRaw)))
	mux.HandleFunc("GET /api/v1/captures/{id}/context", server.authorized(server.revealed(server.handleContext)))
	mux.HandleFunc("GET /api/v1/captures/{id}/artifact", server.authorized(server.revealed(server.handleArtifact)))
	mux.HandleFunc("GET /api/v1/captures/{id}/artifact-line", server.authorized(server.revealed(server.handleArtifactLine)))
	mux.HandleFunc("GET /api/v1/captures/{id}/tool", server.authorized(server.revealed(server.handleTool)))
	server.httpServer = &http.Server{
		Handler: server.secure(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 2 * time.Minute, IdleTimeout: time.Minute, MaxHeaderBytes: 32 << 10,
	}
	go func() {
		_ = server.httpServer.Serve(listener)
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

func (s *Server) URL() string { return "http://" + s.listener.Addr().String() }
func (s *Server) LaunchURL() string {
	return s.URL() + "/launch/" + url.PathEscape(s.config.CapabilityToken)
}

func (s *Server) Close(ctx context.Context) error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.closeErr = s.httpServer.Shutdown(ctx)
		if errors.Is(s.closeErr, http.ErrServerClosed) {
			s.closeErr = nil
		}
	})
	if s.closeErr != nil {
		return fmt.Errorf("close capture UI: %w", s.closeErr)
	}
	return nil
}

func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if !requestHostIsLoopback(r.Host) {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authorized(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(capabilityCookie)
		if err != nil || !secureEqual(cookie.Value, s.config.CapabilityToken) {
			writeAPIError(w, http.StatusUnauthorized, "capture UI capability required")
			return
		}
		next(w, r)
	}
}

func (s *Server) revealed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(revealCookie)
		if err != nil || !secureEqual(cookie.Value, s.config.RevealToken) {
			writeAPIError(w, http.StatusForbidden, "plaintext reveal confirmation required")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	if !secureEqual(r.PathValue("token"), s.config.CapabilityToken) {
		http.NotFound(w, r)
		return
	}
	s.launchMu.Lock()
	if s.launchUsed {
		s.launchMu.Unlock()
		http.Error(w, "launch capability already used", http.StatusGone)
		return
	}
	s.launchUsed = true
	s.launchMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: capabilityCookie, Value: s.config.CapabilityToken, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := assets.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "UI asset missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	if name != r.PathValue("name") || name == "." {
		http.NotFound(w, r)
		return
	}
	data, err := assets.ReadFile("assets/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch filepath.Ext(name) {
	case ".js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	_, _ = w.Write(data)
}

func (s *Server) handleCaptures(w http.ResponseWriter, r *http.Request) {
	entries, err := s.captureEntries(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	view, err := s.view(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleProviderEvent(w http.ResponseWriter, r *http.Request) {
	ordinal, err := strconv.Atoi(r.URL.Query().Get("ordinal"))
	if err != nil || ordinal < 1 {
		writeAPIError(w, http.StatusBadRequest, "provider event ordinal must be positive")
		return
	}
	if _, err := s.inspectableView(r.Context(), r.PathValue("id")); err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	sessionDir, err := s.sessionPath(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	detail, err := projection.ReadProviderEventDetail(sessionDir, r.URL.Query().Get("exchange"), ordinal)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleRawRecords(w http.ResponseWriter, r *http.Request) {
	after := uint64(0)
	if value := r.URL.Query().Get("after"); value != "" {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "raw record after must be a non-negative integer")
			return
		}
		after = parsed
	}
	limit, err := optionalNonNegativeInt(r.URL.Query().Get("limit"), 250)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "raw record limit must be a non-negative integer")
		return
	}
	if _, err := s.inspectableView(r.Context(), r.PathValue("id")); err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	sessionDir, err := s.sessionPath(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	page, err := projection.ReadRawRecordPage(sessionDir, r.URL.Query().Get("file"), after, limit)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleProviderEvents(w http.ResponseWriter, r *http.Request) {
	offset, err := optionalNonNegativeInt(r.URL.Query().Get("offset"), 0)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "provider event offset must be a non-negative integer")
		return
	}
	limit, err := optionalNonNegativeInt(r.URL.Query().Get("limit"), 500)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "provider event limit must be a non-negative integer")
		return
	}
	view, err := s.view(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	page, err := projection.PageProviderEvents(view, offset, limit)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleSessionTrace(w http.ResponseWriter, r *http.Request) {
	view, err := s.inspectableView(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	sessionDir, err := s.sessionPath(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	trace, err := projection.BuildSessionTrace(sessionDir, view, projection.TraceFilter{
		SessionID: r.URL.Query().Get("session"), InvocationID: r.URL.Query().Get("invocation"),
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, trace)
}

func (s *Server) handleTraceContent(w http.ResponseWriter, r *http.Request) {
	view, err := s.inspectableView(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	sessionDir, err := s.sessionPath(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	detail, err := projection.ReadTraceContent(sessionDir, view, r.URL.Query().Get("ref"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	left, err := s.view(r.Context(), r.URL.Query().Get("left"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	right, err := s.view(r.Context(), r.URL.Query().Get("right"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, projection.Compare(left, right))
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	if !s.validOrigin(r) {
		writeAPIError(w, http.StatusForbidden, "invalid capture UI origin")
		return
	}
	defer r.Body.Close()
	var request struct {
		Confirm string `json:"confirm"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, requestBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.Confirm != "REVEAL" {
		writeAPIError(w, http.StatusBadRequest, "confirm must equal REVEAL")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "reveal request must contain exactly one JSON object")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: revealCookie, Value: s.config.RevealToken, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	if _, err := s.inspectableView(r.Context(), r.PathValue("id")); err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	seq, err := strconv.ParseUint(r.URL.Query().Get("seq"), 10, 64)
	if err != nil || seq == 0 {
		writeAPIError(w, http.StatusBadRequest, "raw seq must be a positive integer")
		return
	}
	sessionDir, err := s.sessionPath(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	detail, err := projection.ReadRawDetail(sessionDir, r.URL.Query().Get("file"), seq)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
	if _, err := s.inspectableView(r.Context(), r.PathValue("id")); err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	sessionDir, err := s.sessionPath(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	detail, err := projection.ReadContextDetail(sessionDir, r.URL.Query().Get("exchange"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleArtifactLine(w http.ResponseWriter, r *http.Request) {
	if _, err := s.inspectableView(r.Context(), r.PathValue("id")); err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	line, err := strconv.ParseUint(r.URL.Query().Get("line"), 10, 64)
	if err != nil || line == 0 {
		writeAPIError(w, http.StatusBadRequest, "artifact line must be a positive integer")
		return
	}
	sessionDir, err := s.sessionPath(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	detail, err := projection.ReadArtifactLine(sessionDir, r.URL.Query().Get("path"), line, 1<<20)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	if _, err := s.inspectableView(r.Context(), r.PathValue("id")); err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	sessionDir, err := s.sessionPath(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	detail, err := projection.ReadArtifact(sessionDir, r.URL.Query().Get("path"), 1<<20)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleTool(w http.ResponseWriter, r *http.Request) {
	validatedView, err := s.inspectableView(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	sessionDir, err := s.sessionPath(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, statusForLookupError(err), err.Error())
		return
	}
	toolID := r.URL.Query().Get("id")
	if toolID != "" {
		for _, execution := range validatedView.Tools {
			if execution.ID != toolID {
				continue
			}
			detail, err := projection.ReadToolExecutionDetail(sessionDir, execution)
			if err != nil {
				writeAPIError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, detail)
			return
		}
		writeAPIError(w, http.StatusNotFound, "tool execution not found")
		return
	}
	index, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil || index < 1 {
		writeAPIError(w, http.StatusBadRequest, "tool id or positive index is required")
		return
	}
	detail, err := projection.ReadToolDetail(sessionDir, index)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) captureEntries(ctx context.Context) ([]projection.CaptureIndexEntry, error) {
	entries, err := projection.ScanRoot(ctx, s.config.CaptureRoot)
	if err != nil {
		return nil, err
	}
	if s.config.SessionDir == "" {
		return entries, nil
	}
	manifest, err := capture.ReadSessionManifest(filepath.Join(s.config.SessionDir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.ID == manifest.SessionID {
			return entries, nil
		}
	}
	entry := indexEntryFromSession(s.config.SessionDir, manifest)
	return append([]projection.CaptureIndexEntry{entry}, entries...), nil
}

func (s *Server) sessionPath(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", errors.New("capture ID is required")
	}
	if s.config.SessionDir != "" {
		manifest, err := capture.ReadSessionManifest(filepath.Join(s.config.SessionDir, "manifest.json"))
		if err != nil {
			return "", err
		}
		if manifest.SessionID == id {
			return s.config.SessionDir, nil
		}
	}
	return projection.FindSession(ctx, s.config.CaptureRoot, id)
}

func (s *Server) inspectableView(ctx context.Context, id string) (*projection.View, error) {
	view, err := s.view(ctx, id)
	if err != nil {
		return nil, err
	}
	if view.Capture.Status != capture.CaptureRunning && !view.Integrity.Valid {
		return nil, errCaptureIntegrityInvalid
	}
	return view, nil
}

func (s *Server) view(ctx context.Context, id string) (*projection.View, error) {
	sessionDir, err := s.sessionPath(ctx, id)
	if err != nil {
		return nil, err
	}
	cacheKey, err := captureViewCacheKey(sessionDir)
	if err != nil {
		return nil, err
	}
	s.cacheMu.Lock()
	cached := s.cache[cacheKey]
	s.cacheMu.Unlock()
	if cached != nil && cached.Capture.Status != capture.CaptureRunning {
		return cached, nil
	}
	view, err := projection.Build(ctx, sessionDir)
	if err != nil {
		return nil, err
	}
	if view.Capture.Status != capture.CaptureRunning {
		s.cacheMu.Lock()
		s.cache[cacheKey] = view
		s.cacheMu.Unlock()
	}
	return view, nil
}

func captureViewCacheKey(sessionDir string) (string, error) {
	manifestData, err := os.ReadFile(filepath.Join(sessionDir, "manifest.json"))
	if err != nil {
		return "", err
	}
	var manifest capture.SessionManifestDocument
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return "", fmt.Errorf("decode capture manifest for view cache: %w", err)
	}
	identity := append([]byte(nil), manifestData...)
	for _, file := range manifest.Files {
		identity = append(identity, 0)
		identity = append(identity, file.Path...)
		identity = append(identity, 0)
		clean := filepath.Clean(filepath.FromSlash(file.Path))
		if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			identity = append(identity, "\x00invalid-path"...)
			continue
		}
		info, statErr := os.Lstat(filepath.Join(sessionDir, clean))
		if statErr != nil {
			identity = append(identity, "\x00stat-error:"...)
			identity = append(identity, statErr.Error()...)
			continue
		}
		identity = strconv.AppendInt(identity, info.Size(), 10)
		identity = append(identity, 0)
		identity = strconv.AppendInt(identity, info.ModTime().UnixNano(), 10)
		identity = append(identity, 0)
		identity = append(identity, info.Mode().String()...)
	}
	sum := sha256.Sum256(identity)
	return fmt.Sprintf("%s\x00%s\x00%x", sessionDir, projection.FormatVersion1, sum), nil
}

func (s *Server) validOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin == s.URL()
}

func indexEntryFromSession(path string, manifest *capture.SessionManifestDocument) projection.CaptureIndexEntry {
	entry := projection.CaptureIndexEntry{
		ID: manifest.SessionID, Label: manifest.Label, Status: manifest.Status, Integrity: "not-verified",
		StartedAt: manifest.StartedAt, BuildVersion: manifest.Build.Version, BuildCommit: manifest.Build.Commit,
		Plaintext: manifest.Plaintext, SessionPath: path,
	}
	if manifest.EndedAt != nil {
		entry.EndedAt = *manifest.EndedAt
		entry.DurationMs = manifest.EndedAt.Sub(manifest.StartedAt).Milliseconds()
	}
	for _, file := range manifest.Files {
		entry.SizeBytes += file.SizeBytes
	}
	if manifest.Status == capture.CaptureRunning {
		entry.Integrity = "running"
	}
	return entry
}

func validateLoopback(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse capture UI listen address: %w", err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("capture UI listen address %q must be loopback", address)
	}
	return nil
}

func optionalNonNegativeInt(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, errors.New("value must be a non-negative integer")
	}
	return parsed, nil
}

func requestHostIsLoopback(hostport string) bool {
	host := hostport
	if parsedHost, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsedHost
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func secureEqual(left, right string) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func statusForLookupError(err error) int {
	if errors.Is(err, errCaptureIntegrityInvalid) {
		return http.StatusConflict
	}
	if strings.Contains(err.Error(), "not found") {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

type apiError struct {
	Error string `json:"error"`
}

type jsonResponse interface {
	[]projection.CaptureIndexEntry |
		*projection.View |
		*projection.SessionTrace |
		*projection.TraceContentDetail |
		*projection.ProviderEventDetail |
		*projection.RawRecordPage |
		*projection.ProviderEventPage |
		projection.Comparison |
		*projection.RawDetail |
		*projection.ContextDetail |
		*projection.ArtifactLineDetail |
		*projection.ArtifactDetail |
		*projection.ToolDetail |
		apiError
}

func writeJSON[T jsonResponse](w http.ResponseWriter, status int, value T) {
	data, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "encode JSON response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, apiError{Error: message})
}

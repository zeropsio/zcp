package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultUpstreamBaseURL is the fixed provider origin used when no
	// developer override is supplied.
	DefaultUpstreamBaseURL = "https://api.anthropic.com"
	bodyRecordChunkSize    = 64 * 1024
)

// ProxyConfig configures one loopback-only, fixed-upstream provider observer.
type ProxyConfig struct {
	ListenAddr      string
	UpstreamBaseURL string
	Recorder        *Recorder
}

// ProxyServer forwards provider traffic while recording raw entity bytes.
type ProxyServer struct {
	httpServer *http.Server
	listener   net.Listener
	upstream   *url.URL
	recorder   *Recorder
	client     *http.Client
	exchanges  atomic.Uint64
	errMu      sync.Mutex
	captureErr error
}

// StartProxy starts a fixed-origin proxy. It rejects non-loopback listeners so
// provider credentials cannot accidentally be exposed through a public proxy.
func StartProxy(ctx context.Context, cfg ProxyConfig) (*ProxyServer, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:0"
	}
	if cfg.UpstreamBaseURL == "" {
		cfg.UpstreamBaseURL = DefaultUpstreamBaseURL
	}
	if cfg.Recorder == nil {
		return nil, errors.New("capture recorder is required")
	}
	if err := validateLoopbackAddress(cfg.ListenAddr); err != nil {
		return nil, err
	}
	upstream, err := url.Parse(cfg.UpstreamBaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse provider upstream: %w", err)
	}
	if upstream.Scheme != "http" && upstream.Scheme != "https" {
		return nil, fmt.Errorf("provider upstream scheme %q must be http or https", upstream.Scheme)
	}
	if upstream.Host == "" {
		return nil, errors.New("provider upstream must include a host")
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen for provider capture: %w", err)
	}

	server := &ProxyServer{
		listener: listener,
		upstream: upstream,
		recorder: cfg.Recorder,
		client: &http.Client{
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				DisableCompression:  true,
				ForceAttemptHTTP2:   true,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
	server.httpServer = &http.Server{
		Handler:           server,
		ReadHeaderTimeout: 30 * time.Second,
	}
	go func() {
		if serveErr := server.httpServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			server.setCaptureError(fmt.Errorf("serve provider capture proxy: %w", serveErr))
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
			server.setCaptureError(shutdownErr)
		}
	}()
	return server, nil
}

func validateLoopbackAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse provider listen address %q: %w", address, err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("provider listen address %q must use a loopback host", address)
	}
	return nil
}

// URL returns the loopback base URL assigned to the proxy listener.
func (s *ProxyServer) URL() string { return "http://" + s.listener.Addr().String() }

// Shutdown drains active HTTP exchanges and closes the listener.
func (s *ProxyServer) Shutdown(ctx context.Context) error {
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown provider capture proxy: %w", err)
	}
	return nil
}

// CaptureError returns the first recorder or serving failure. Provider and
// client protocol errors are recorded as exchange events rather than capture
// failures.
func (s *ProxyServer) CaptureError() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.captureErr
}

func (s *ProxyServer) setCaptureError(err error) {
	if err == nil {
		return
	}
	s.errMu.Lock()
	if s.captureErr == nil {
		s.captureErr = err
	}
	s.errMu.Unlock()
}

func (s *ProxyServer) record(record Record) {
	if err := s.recorder.Record(record); err != nil {
		s.setCaptureError(err)
	}
}

func (s *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	exchangeID := fmt.Sprintf("exchange-%06d", s.exchanges.Add(1))
	s.record(Record{
		Kind:       RecordProviderRequestStart,
		ExchangeID: exchangeID,
		Direction:  "client_to_provider",
		Method:     r.Method,
		Path:       r.URL.Path,
		Headers:    capturedRequestHeaders(r.Header),
	})

	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		s.recordExchangeError(exchangeID, r.Method, r.URL.Path, fmt.Errorf("read client request body: %w", err))
		http.Error(w, "read request body", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()
	s.recordBody(exchangeID, RecordProviderRequestBody, "client_to_provider", requestBody)
	requestHash := sha256.Sum256(requestBody)
	s.record(Record{
		Kind:       RecordProviderRequestEnd,
		ExchangeID: exchangeID,
		Direction:  "client_to_provider",
		BodyBytes:  int64(len(requestBody)),
		SHA256:     hex.EncodeToString(requestHash[:]),
	})

	upstreamRequest, err := s.buildUpstreamRequest(r, requestBody)
	if err != nil {
		s.recordExchangeError(exchangeID, r.Method, r.URL.Path, err)
		http.Error(w, "build upstream request", http.StatusBadGateway)
		return
	}
	response, err := s.client.Do(upstreamRequest)
	if err != nil {
		s.recordExchangeError(exchangeID, r.Method, r.URL.Path, fmt.Errorf("call provider upstream: %w", err))
		http.Error(w, "provider upstream error", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()

	copyResponseHeaders(w.Header(), response.Header)
	s.record(Record{
		Kind:       RecordProviderResponseStart,
		ExchangeID: exchangeID,
		Direction:  "provider_to_client",
		StatusCode: response.StatusCode,
		Headers:    capturedResponseHeaders(response.Header),
	})
	w.WriteHeader(response.StatusCode)

	hash := sha256.New()
	var responseBytes int64
	buffer := make([]byte, 32*1024)
	streaming := strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream")
	flusher, _ := w.(http.Flusher)
	for {
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			chunk := bytes.Clone(buffer[:count])
			responseBytes += int64(count)
			_, _ = hash.Write(chunk)
			s.recordBody(exchangeID, RecordProviderResponseBody, "provider_to_client", chunk)
			if _, writeErr := w.Write(chunk); writeErr != nil {
				s.recordResponseEnd(exchangeID, responseBytes, hash.Sum(nil), fmt.Errorf("write provider response to client: %w", writeErr))
				return
			}
			if streaming && flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				s.recordResponseEnd(exchangeID, responseBytes, hash.Sum(nil), nil)
				return
			}
			s.recordResponseEnd(exchangeID, responseBytes, hash.Sum(nil), fmt.Errorf("read provider response: %w", readErr))
			return
		}
	}
}

func (s *ProxyServer) buildUpstreamRequest(original *http.Request, body []byte) (*http.Request, error) {
	upstreamURL := *s.upstream
	upstreamURL.Path = joinURLPath(s.upstream.Path, original.URL.Path)
	upstreamURL.RawPath = ""
	upstreamURL.RawQuery = original.URL.RawQuery
	request, err := http.NewRequestWithContext(original.Context(), original.Method, upstreamURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build provider request: %w", err)
	}
	request.Header = cloneHeader(original.Header)
	removeHopByHopHeaders(request.Header)
	request.Host = s.upstream.Host
	request.ContentLength = int64(len(body))
	return request, nil
}

func (s *ProxyServer) recordBody(exchangeID, kind, direction string, body []byte) {
	for offset := 0; offset < len(body); offset += bodyRecordChunkSize {
		end := offset + bodyRecordChunkSize
		end = min(end, len(body))
		chunk := body[offset:end]
		s.record(Record{
			Kind:       kind,
			ExchangeID: exchangeID,
			Direction:  direction,
			BodyBase64: base64.StdEncoding.EncodeToString(chunk),
			BodyBytes:  int64(len(chunk)),
		})
	}
}

func (s *ProxyServer) recordResponseEnd(exchangeID string, bodyBytes int64, sum []byte, err error) {
	record := Record{
		Kind:       RecordProviderResponseEnd,
		ExchangeID: exchangeID,
		Direction:  "provider_to_client",
		BodyBytes:  bodyBytes,
		SHA256:     hex.EncodeToString(sum),
	}
	if err != nil {
		record.Error = err.Error()
	}
	s.record(record)
}

func (s *ProxyServer) recordExchangeError(exchangeID, method, path string, err error) {
	s.record(Record{
		Kind:       RecordProviderExchangeError,
		ExchangeID: exchangeID,
		Method:     method,
		Path:       path,
		Error:      err.Error(),
	})
}

func capturedRequestHeaders(headers http.Header) http.Header {
	return captureHeaderAllowlist(headers, map[string]bool{
		"accept":            true,
		"accept-encoding":   true,
		"anthropic-beta":    true,
		"anthropic-version": true,
		"content-type":      true,
		"user-agent":        true,
		"x-app":             true,
	}, []string{"x-stainless-"})
}

func capturedResponseHeaders(headers http.Header) http.Header {
	return captureHeaderAllowlist(headers, map[string]bool{
		"content-encoding": true,
		"content-type":     true,
		"date":             true,
		"request-id":       true,
		"x-request-id":     true,
	}, []string{"anthropic-ratelimit-", "x-ratelimit-"})
}

func captureHeaderAllowlist(headers http.Header, exact map[string]bool, prefixes []string) http.Header {
	captured := make(http.Header)
	for key, values := range headers {
		lower := strings.ToLower(key)
		allowed := exact[lower]
		if !allowed {
			for _, prefix := range prefixes {
				if strings.HasPrefix(lower, prefix) {
					allowed = true
					break
				}
			}
		}
		if allowed {
			captured[key] = append([]string(nil), values...)
		}
	}
	return captured
}

func joinURLPath(basePath, requestPath string) string {
	if basePath == "" || basePath == "/" {
		if requestPath == "" {
			return "/"
		}
		return requestPath
	}
	return strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(requestPath, "/")
}

func cloneHeader(headers http.Header) http.Header {
	clone := make(http.Header, len(headers))
	for key, values := range headers {
		clone[key] = append([]string(nil), values...)
	}
	return clone
}

func copyResponseHeaders(destination, source http.Header) {
	connectionHeaders := connectionNominatedHeaders(source)
	for key, values := range source {
		if isHopByHopHeader(key) {
			continue
		}
		if _, nominated := connectionHeaders[strings.ToLower(key)]; nominated {
			continue
		}
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func removeHopByHopHeaders(headers http.Header) {
	for key := range connectionNominatedHeaders(headers) {
		headers.Del(key)
	}
	for key := range headers {
		if isHopByHopHeader(key) {
			headers.Del(key)
		}
	}
}

func connectionNominatedHeaders(headers http.Header) map[string]struct{} {
	connectionHeaders := make(map[string]struct{})
	for _, value := range headers.Values("Connection") {
		for key := range strings.SplitSeq(value, ",") {
			if trimmed := strings.TrimSpace(key); trimmed != "" {
				connectionHeaders[strings.ToLower(trimmed)] = struct{}{}
			}
		}
	}
	return connectionHeaders
}

func isHopByHopHeader(key string) bool {
	switch strings.ToLower(key) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

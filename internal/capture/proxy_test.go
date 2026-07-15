package capture

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxy_ExactBodiesRecordedAndAuthorizationExcluded(t *testing.T) {
	t.Parallel()

	requestBody := []byte("{\n  \"model\": \"claude-test\",\n  \"messages\": []\n}\n")
	responseBody := []byte("{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}\n")
	var upstreamBody []byte
	var upstreamAuthorization string
	var upstreamHost string
	var upstreamQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		upstreamBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream request: %v", err)
			return
		}
		upstreamAuthorization = r.Header.Get("Authorization")
		upstreamHost = r.Host
		upstreamQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "identity")
		w.Header().Set("Request-Id", "req_test")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(responseBody)
	}))
	defer upstream.Close()

	recorder, err := NewRecorder(RecorderConfig{
		RootDir:   t.TempDir(),
		SessionID: "session-test",
		Label:     "exact",
	})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	proxy, err := StartProxy(ctx, ProxyConfig{
		ListenAddr:      "127.0.0.1:0",
		UpstreamBaseURL: upstream.URL,
		Recorder:        recorder,
	})
	if err != nil {
		t.Fatalf("StartProxy() error = %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, proxy.URL()+"/v1/messages?beta=1", bytes.NewReader(requestBody))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Host = "attacker.example"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer provider-secret")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	gotResponse, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if err := proxy.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := recorder.Close(CaptureComplete, 0); err != nil {
		t.Fatalf("Recorder.Close() error = %v", err)
	}

	if !bytes.Equal(upstreamBody, requestBody) {
		t.Fatalf("upstream request body changed:\nwant %q\n got %q", requestBody, upstreamBody)
	}
	if upstreamAuthorization != "Bearer provider-secret" {
		t.Fatalf("upstream Authorization = %q, want forwarded value", upstreamAuthorization)
	}
	if upstreamHost != strings.TrimPrefix(upstream.URL, "http://") {
		t.Fatalf("upstream Host = %q, want fixed destination %q", upstreamHost, upstream.URL)
	}
	if upstreamQuery != "beta=1" {
		t.Fatalf("upstream query = %q, want forwarded query", upstreamQuery)
	}
	if !bytes.Equal(gotResponse, responseBody) {
		t.Fatalf("client response body changed:\nwant %q\n got %q", responseBody, gotResponse)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	records, err := ReadRecords(recorder.Path())
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	exchangeID := firstExchangeID(t, records)
	if got := joinedBody(t, records, RecordProviderRequestBody, exchangeID); !bytes.Equal(got, requestBody) {
		t.Fatalf("recorded request body changed:\nwant %q\n got %q", requestBody, got)
	}
	if got := joinedBody(t, records, RecordProviderResponseBody, exchangeID); !bytes.Equal(got, responseBody) {
		t.Fatalf("recorded response body changed:\nwant %q\n got %q", responseBody, got)
	}

	start := firstRecord(t, records, RecordProviderRequestStart)
	if start.Path != "/v1/messages" {
		t.Fatalf("recorded path = %q, want query-free path", start.Path)
	}
	if got := start.Headers.Get("Authorization"); got != "" {
		t.Fatalf("recorded Authorization = %q, want structurally omitted", got)
	}
	if got := start.Headers.Get("Anthropic-Version"); got != "2023-06-01" {
		t.Fatalf("recorded Anthropic-Version = %q", got)
	}
	responseStart := firstRecord(t, records, RecordProviderResponseStart)
	if got := responseStart.Headers.Get("Content-Encoding"); got != "identity" {
		t.Fatalf("recorded Content-Encoding = %q, want identity", got)
	}
	end := firstRecord(t, records, RecordProviderResponseEnd)
	if end.BodyBytes != int64(len(responseBody)) || end.SHA256 == "" {
		t.Fatalf("response end = %+v, want byte count and hash", end)
	}
}

func TestProxy_SSEForwardedBeforeUpstreamEOFAndRecordedExactly(t *testing.T) {
	t.Parallel()

	first := "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"
	second := "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, first)
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, second)
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()

	recorder, err := NewRecorder(RecorderConfig{RootDir: t.TempDir(), SessionID: "session-stream", Label: "stream"})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	proxy, err := StartProxy(ctx, ProxyConfig{ListenAddr: "127.0.0.1:0", UpstreamBaseURL: upstream.URL, Recorder: recorder})
	if err != nil {
		t.Fatalf("StartProxy() error = %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, proxy.URL()+"/v1/messages", strings.NewReader(`{"stream":true}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	reader := bufio.NewReader(resp.Body)
	firstRead := make(chan string, 1)
	go func() {
		var b strings.Builder
		for b.Len() < len(first) {
			part, readErr := reader.ReadString('\n')
			b.WriteString(part)
			if readErr != nil {
				break
			}
		}
		firstRead <- b.String()
	}()

	select {
	case got := <-firstRead:
		if got != first {
			t.Fatalf("first streamed bytes = %q, want %q", got, first)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first SSE event did not reach client before upstream EOF")
	}
	close(release)
	rest, err := io.ReadAll(reader)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read remaining response: %v", err)
	}
	if string(rest) != second {
		t.Fatalf("remaining bytes = %q, want %q", rest, second)
	}
	if err := proxy.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := recorder.Close(CaptureComplete, 0); err != nil {
		t.Fatalf("Recorder.Close() error = %v", err)
	}

	records, err := ReadRecords(recorder.Path())
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	exchangeID := firstExchangeID(t, records)
	got := joinedBody(t, records, RecordProviderResponseBody, exchangeID)
	if string(got) != first+second {
		t.Fatalf("recorded stream changed:\nwant %q\n got %q", first+second, got)
	}
}

func TestProxy_RepresentativeEvalPayloadHasNoCaptureGap(t *testing.T) {
	if testing.Short() {
		t.Skip("representative multi-megabyte capture payload")
	}
	t.Parallel()

	responseBody := bytes.Repeat([]byte("data: {\"type\":\"content_block_delta\"}\n\n"), 200000)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(responseBody)
	}))
	defer upstream.Close()
	recorder, err := NewRecorder(RecorderConfig{RootDir: t.TempDir(), SessionID: "session-representative", Label: "representative"})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	proxy, err := StartProxy(ctx, ProxyConfig{ListenAddr: "127.0.0.1:0", UpstreamBaseURL: upstream.URL, Recorder: recorder})
	if err != nil {
		t.Fatalf("StartProxy() error = %v", err)
	}
	response, err := http.Get(proxy.URL() + "/v1/messages") //nolint:noctx // local bounded test servers
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	got, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if !bytes.Equal(got, responseBody) {
		t.Fatalf("representative response changed: got %d bytes, want %d", len(got), len(responseBody))
	}
	if err := proxy.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := recorder.Close(CaptureComplete, 0); err != nil {
		t.Fatalf("Recorder.Close() error = %v", err)
	}
	records, err := ReadRecords(recorder.Path())
	if err != nil {
		t.Fatalf("ReadRecords() error = %v", err)
	}
	for _, record := range records {
		if record.Kind == RecordCaptureGap {
			t.Fatalf("representative payload produced capture gap: %+v", record)
		}
	}
}

func TestProxy_ContextCancellationClosesListener(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer upstream.Close()
	recorder, err := NewRecorder(RecorderConfig{RootDir: t.TempDir(), SessionID: "session-cancel", Label: "cancel"})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	proxy, err := StartProxy(ctx, ProxyConfig{ListenAddr: "127.0.0.1:0", UpstreamBaseURL: upstream.URL, Recorder: recorder})
	if err != nil {
		t.Fatalf("StartProxy() error = %v", err)
	}
	proxyURL := proxy.URL()
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		requestCtx, requestCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		request, requestErr := http.NewRequestWithContext(requestCtx, http.MethodGet, proxyURL, nil)
		if requestErr != nil {
			requestCancel()
			t.Fatalf("NewRequestWithContext() error = %v", requestErr)
		}
		response, requestErr := http.DefaultClient.Do(request)
		requestCancel()
		if requestErr != nil {
			if err := recorder.Close(CaptureComplete, 0); err != nil {
				t.Fatalf("Recorder.Close() error = %v", err)
			}
			return
		}
		_ = response.Body.Close()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("proxy listener still accepted connections after context cancellation")
}

func TestProxy_RemovesConnectionNominatedResponseHeaders(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Connection", "X-Upstream-Hop")
		w.Header().Set("X-Upstream-Hop", "must-not-reach-client")
		w.Header().Set("X-End-To-End", "keep")
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	recorder, err := NewRecorder(RecorderConfig{RootDir: t.TempDir(), SessionID: "session-hop-header", Label: "hop-header"})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	proxy, err := StartProxy(ctx, ProxyConfig{ListenAddr: "127.0.0.1:0", UpstreamBaseURL: upstream.URL, Recorder: recorder})
	if err != nil {
		t.Fatalf("StartProxy() error = %v", err)
	}

	response, err := http.Get(proxy.URL() + "/v1/messages") //nolint:noctx // test request is bounded by local test servers
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if err := proxy.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := recorder.Close(CaptureComplete, 0); err != nil {
		t.Fatalf("Recorder.Close() error = %v", err)
	}

	if got := response.Header.Get("X-Upstream-Hop"); got != "" {
		t.Fatalf("connection-nominated header reached client: %q", got)
	}
	if got := response.Header.Get("X-End-To-End"); got != "keep" {
		t.Fatalf("end-to-end header = %q, want keep", got)
	}
}

func TestProxy_RelaysRedirectWithoutFollowingIt(t *testing.T) {
	t.Parallel()

	var redirectedRequests atomic.Int32
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectedRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer redirectTarget.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", redirectTarget.URL+"/unexpected")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()

	recorder, err := NewRecorder(RecorderConfig{RootDir: t.TempDir(), SessionID: "session-redirect", Label: "redirect"})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	proxy, err := StartProxy(ctx, ProxyConfig{ListenAddr: "127.0.0.1:0", UpstreamBaseURL: upstream.URL, Recorder: recorder})
	if err != nil {
		t.Fatalf("StartProxy() error = %v", err)
	}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, proxy.URL()+"/v1/messages", strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	_ = response.Body.Close()
	if err := proxy.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := recorder.Close(CaptureComplete, 0); err != nil {
		t.Fatalf("Recorder.Close() error = %v", err)
	}

	if response.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want relayed %d", response.StatusCode, http.StatusTemporaryRedirect)
	}
	if got := response.Header.Get("Location"); got != redirectTarget.URL+"/unexpected" {
		t.Fatalf("Location = %q, want upstream redirect", got)
	}
	if got := redirectedRequests.Load(); got != 0 {
		t.Fatalf("redirect target received %d requests; fixed-upstream proxy followed redirect", got)
	}
}

func firstExchangeID(t *testing.T, records []Record) string {
	t.Helper()
	for _, record := range records {
		if record.ExchangeID != "" {
			return record.ExchangeID
		}
	}
	t.Fatal("no exchange ID in records")
	return ""
}

func firstRecord(t *testing.T, records []Record, kind string) Record {
	t.Helper()
	for _, record := range records {
		if record.Kind == kind {
			return record
		}
	}
	t.Fatalf("record kind %q not found", kind)
	return Record{}
}

func joinedBody(t *testing.T, records []Record, kind, exchangeID string) []byte {
	t.Helper()
	var out []byte
	for _, record := range records {
		if record.Kind != kind || record.ExchangeID != exchangeID {
			continue
		}
		chunk, err := base64.StdEncoding.DecodeString(record.BodyBase64)
		if err != nil {
			t.Fatalf("decode %s body: %v", kind, err)
		}
		out = append(out, chunk...)
	}
	return out
}

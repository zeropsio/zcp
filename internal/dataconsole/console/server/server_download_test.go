package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zeropsio/zcp/internal/dataconsole/console"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/safety"
)

type downloadProvider struct {
	*fakeObject
	download func(context.Context, provider.Path) (io.ReadCloser, provider.BlobDownloadMeta, error)
}

func (p *downloadProvider) DownloadBlob(ctx context.Context, path provider.Path) (io.ReadCloser, provider.BlobDownloadMeta, error) {
	return p.download(ctx, path)
}

func newDownloadTestServer(t *testing.T, p provider.Provider) (*testServer, string) {
	t.Helper()
	return newDownloadTestServerWithDiagnostics(t, p, io.Discard)
}

func newDownloadTestServerWithDiagnostics(t *testing.T, p provider.Provider, diagnostics io.Writer) (*testServer, string) {
	t.Helper()
	factories := map[provider.Family]console.Factory{
		provider.FamilyObject: func(console.ConnectionInfo, *safety.Policy) (provider.Provider, error) { return p, nil },
	}
	eng := console.NewEngine(fakeHost{}, writePolicy(false), factories)
	if err := eng.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	srv := NewWithDiagnostics(eng, "secret", fstest.MapFS{"index.html": {Data: []byte("ok")}}, diagnostics)
	return &testServer{handler: srv.Handler()}, "secret"
}

func responseBytes(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return b
}

func TestHandleDownload_FullContent_StreamsBeyondPreviewCap(t *testing.T) {
	t.Parallel()
	full := []byte("complete value beyond preview cap")
	preview := append([]byte(nil), full[:8]...)
	base := &fakeObject{
		blobs:         map[string][]byte{"large.bin": preview},
		truncatedSize: map[string]int64{"large.bin": int64(len(full))},
		readOnly:      true,
	}
	p := &downloadProvider{fakeObject: base, download: func(_ context.Context, path provider.Path) (io.ReadCloser, provider.BlobDownloadMeta, error) {
		return io.NopCloser(bytes.NewReader(full)), provider.BlobDownloadMeta{
			ContentType: "text/plain", Size: int64(len(full)), Filename: path.Segments[len(path.Segments)-1],
		}, nil
	}}
	ts, token := newDownloadTestServer(t, p)

	previewReq := newRequest(t, http.MethodGet, `/api/blob?service=store&segs=%5B%22large.bin%22%5D`, nil)
	previewResp := doReq(t, ts, previewReq, token, false)
	defer func() { _ = previewResp.Body.Close() }()
	previewBody := responseBytes(t, previewResp)
	if !bytes.Equal(previewBody, preview) {
		t.Fatalf("preview bytes = %q, want capped %q", previewBody, preview)
	}
	downloadReq := newRequest(t, http.MethodGet, `/api/download?service=store&segs=%5B%22large.bin%22%5D`, nil)
	downloadResp := doReq(t, ts, downloadReq, token, false)
	defer func() { _ = downloadResp.Body.Close() }()
	downloadBody := responseBytes(t, downloadResp)
	if !bytes.Equal(downloadBody, full) {
		t.Fatalf("download bytes = %q, want full %q", downloadBody, full)
	}
}

func TestHandleDownload_MissingBearer_RejectsRequest(t *testing.T) {
	t.Parallel()
	p := &downloadProvider{fakeObject: &fakeObject{blobs: map[string][]byte{}, readOnly: true}, download: func(context.Context, provider.Path) (io.ReadCloser, provider.BlobDownloadMeta, error) {
		t.Fatal("DownloadBlob reached without bearer")
		return nil, provider.BlobDownloadMeta{}, nil
	}}
	ts, _ := newDownloadTestServer(t, p)
	req := newRequest(t, http.MethodGet, `/api/download?service=store&segs=%5B%22x%22%5D`, nil)
	resp := doReq(t, ts, req, "", false)
	defer func() { _ = resp.Body.Close() }()
	body := responseBytes(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("download without bearer = %d, want 401; body=%s", resp.StatusCode, body)
	}
}

func TestHandleDownload_AttachmentHeaders_SanitizeFilename(t *testing.T) {
	t.Parallel()
	p := &downloadProvider{fakeObject: &fakeObject{blobs: map[string][]byte{}, readOnly: true}, download: func(context.Context, provider.Path) (io.ReadCloser, provider.BlobDownloadMeta, error) {
		return io.NopCloser(strings.NewReader("data")), provider.BlobDownloadMeta{
			ContentType: "text/plain\r\nX-Evil: yes", Size: 4, Filename: "..\\..\\folder\\report\r\n.txt",
		}, nil
	}}
	ts, token := newDownloadTestServer(t, p)
	req := newRequest(t, http.MethodGet, `/api/download?service=store&segs=%5B%22x%22%5D`, nil)
	resp := doReq(t, ts, req, token, false)
	defer func() { _ = resp.Body.Close() }()
	body := responseBytes(t, resp)
	if resp.StatusCode != http.StatusOK || string(body) != "data" {
		t.Fatalf("download = %d %q, want 200 data", resp.StatusCode, body)
	}
	mediaType, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	if err != nil {
		t.Fatalf("parse Content-Disposition: %v", err)
	}
	if mediaType != "attachment" || params["filename"] != "report.txt" {
		t.Fatalf("Content-Disposition = %q, want attachment filename=report.txt", resp.Header.Get("Content-Disposition"))
	}
	wantHeaders := map[string]string{
		"Content-Type":              "application/octet-stream",
		"Content-Length":            "4",
		"X-Content-Type-Options":    "nosniff",
		"Cache-Control":             "no-store",
		"Referrer-Policy":           "no-referrer",
		"X-DataConsole-ContentType": "text/plainX-Evil: yes",
		"X-DataConsole-Size":        "4",
	}
	for name, want := range wantHeaders {
		if got := resp.Header.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

type failingDownloadReader struct {
	closed bool
}

func (*failingDownloadReader) Read([]byte) (int, error) {
	return 0, errors.New("secret-driver-cause")
}

func (r *failingDownloadReader) Close() error {
	r.closed = true
	return nil
}

func TestHandleDownload_FirstReadFailure_ReturnsEnvelope(t *testing.T) {
	t.Parallel()
	reader := &failingDownloadReader{}
	diagnostics := &bytes.Buffer{}
	p := &downloadProvider{fakeObject: &fakeObject{blobs: map[string][]byte{}, readOnly: true}, download: func(context.Context, provider.Path) (io.ReadCloser, provider.BlobDownloadMeta, error) {
		return reader, provider.BlobDownloadMeta{ContentType: "application/x-secret", Size: 20, Filename: "secret.bin"}, nil
	}}
	ts, token := newDownloadTestServerWithDiagnostics(t, p, diagnostics)
	req := newRequest(t, http.MethodGet, `/api/download?service=store&segs=%5B%22secret.bin%22%5D`, nil)
	resp := doReq(t, ts, req, token, false)
	defer func() { _ = resp.Body.Close() }()
	body := responseBytes(t, resp)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("first read failure = %d, want 502; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"code":"upstream"`) || strings.Contains(string(body), "secret-driver-cause") {
		t.Fatalf("error envelope = %s, want sanitized upstream code without raw cause", body)
	}
	if !strings.Contains(diagnostics.String(), "secret-driver-cause") {
		t.Fatalf("diagnostics = %q, want raw cause", diagnostics.String())
	}
	if !reader.closed {
		t.Fatal("download reader was not closed after first-read failure")
	}
	if got := resp.Header.Get("Content-Disposition"); got != "" {
		t.Fatalf("error Content-Disposition = %q, want cleared download header", got)
	}
}

type cancelDownloadReader struct {
	done     <-chan struct{}
	started  chan struct{}
	canceled chan struct{}
	closed   chan struct{}
	once     sync.Once
	first    bool
}

func (r *cancelDownloadReader) Read(p []byte) (int, error) {
	if !r.first {
		r.first = true
		for i := range p {
			p[i] = 'x'
		}
		close(r.started)
		return len(p), nil
	}
	<-r.done
	close(r.canceled)
	return 0, errors.New("backend stream aborted after client disconnect")
}

func (r *cancelDownloadReader) Close() error {
	r.once.Do(func() { close(r.closed) })
	return nil
}

func TestHandleDownload_ClientCancel_PropagatesAndCloses(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	canceled := make(chan struct{})
	closed := make(chan struct{})
	diagnostics := &bytes.Buffer{}
	p := &downloadProvider{fakeObject: &fakeObject{blobs: map[string][]byte{}, readOnly: true}, download: func(ctx context.Context, _ provider.Path) (io.ReadCloser, provider.BlobDownloadMeta, error) {
		return &cancelDownloadReader{done: ctx.Done(), started: started, canceled: canceled, closed: closed}, provider.BlobDownloadMeta{
			ContentType: "application/octet-stream", Size: 1 << 20, Filename: "large.bin",
		}, nil
	}}
	ts, token := newDownloadTestServerWithDiagnostics(t, p, diagnostics)
	httpServer := httptest.NewServer(ts.handler)
	t.Cleanup(httpServer.Close)
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+`/api/download?service=store&segs=%5B%22large.bin%22%5D`, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("download request: %v", err)
	}
	<-started
	buf := make([]byte, 1)
	if _, err := resp.Body.Read(buf); err != nil {
		t.Fatalf("read first byte: %v", err)
	}
	cancel()
	_ = resp.Body.Close()
	for name, ch := range map[string]<-chan struct{}{"provider context cancellation": canceled, "reader close": closed} {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %s", name)
		}
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("client cancellation produced noisy diagnostics: %s", diagnostics.String())
	}
}

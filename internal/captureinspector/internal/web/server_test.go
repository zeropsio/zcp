package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
	"github.com/zeropsio/zcp/internal/captureinspector/internal/projection"
)

func TestServer_RequiresCapabilityAndPlaintextReveal(t *testing.T) {
	t.Parallel()

	server, client, _ := newTestServer(t)
	unauthorized := testGET(t, http.DefaultClient, server.URL()+"/api/v1/captures")
	_ = unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.StatusCode)
	}
	authorizeTestClient(t, server, client)
	rawURL := server.URL() + "/api/v1/captures/capture-ui-fixture/raw?file=provider.jsonl&seq=1"
	assertGETStatus(t, client, rawURL, http.StatusForbidden)
	assertGETStatus(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/provider-event?exchange=exchange-1&ordinal=1", http.StatusForbidden)
	assertGETStatus(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/trace-content?ref=request:x:0:-1", http.StatusForbidden)

	revealRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL()+"/api/v1/reveal", strings.NewReader(`{"confirm":"REVEAL"}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	revealRequest.Header.Set("Content-Type", "application/json")
	revealRequest.Header.Set("Origin", server.URL())
	reveal, err := client.Do(revealRequest)
	if err != nil {
		t.Fatalf("reveal request error = %v", err)
	}
	_ = reveal.Body.Close()
	if reveal.StatusCode != http.StatusNoContent {
		t.Fatalf("reveal status = %d", reveal.StatusCode)
	}
	assertGETStatus(t, client, rawURL, http.StatusOK)
	assertGETStatus(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/context?exchange=missing", http.StatusBadRequest)
}

func TestServer_SummaryAndPagedQueriesExcludePlaintext(t *testing.T) {
	t.Parallel()

	server, client, _ := newTestServer(t)
	authorizeTestClient(t, server, client)
	response := testGET(t, client, server.URL()+"/api/v1/captures")
	var entries []projection.CaptureIndexEntry
	if err := json.NewDecoder(response.Body).Decode(&entries); err != nil {
		t.Fatalf("decode captures: %v", err)
	}
	_ = response.Body.Close()
	if len(entries) != 1 || entries[0].ID != "capture-ui-fixture" {
		t.Fatalf("entries = %+v", entries)
	}
	viewResponse := testGET(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/view")
	viewBody, err := io.ReadAll(viewResponse.Body)
	_ = viewResponse.Body.Close()
	if err != nil {
		t.Fatalf("read view response: %v", err)
	}
	if strings.Contains(string(viewBody), "TOP-SECRET-PROMPT") {
		t.Fatal("summary view exposed plaintext artifact content before reveal")
	}
	traceResponse := testGET(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/session-trace?session=client-session")
	traceBody, err := io.ReadAll(traceResponse.Body)
	_ = traceResponse.Body.Close()
	if err != nil {
		t.Fatalf("read trace response: %v", err)
	}
	if strings.Contains(string(traceBody), "TOP-SECRET-PROMPT") || strings.Contains(string(traceBody), "SECRET-MODEL-RESPONSE") {
		t.Fatalf("session trace exposed plaintext before reveal: %s", traceBody)
	}
	var trace projection.SessionTrace
	if err := json.Unmarshal(traceBody, &trace); err != nil {
		t.Fatalf("decode session trace: %v", err)
	}
	if len(trace.Flow.Turns) != 1 || len(trace.Flow.Nodes) < 3 || len(trace.Flow.Edges) < 2 {
		t.Fatalf("session flow = %+v", trace.Flow)
	}
	assertProviderEventPageExcludesPayload(t, server, client)
	assertRawRecordPage(t, server, client)
}

func TestServer_InvalidCaptureDisablesDetailQueries(t *testing.T) {
	t.Parallel()
	server, _, sessionDir := newTestServer(t)
	providerPath := filepath.Join(sessionDir, "provider.jsonl")
	file, err := os.OpenFile(providerPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open provider for tamper: %v", err)
	}
	if _, err := file.WriteString("tamper\n"); err != nil {
		_ = file.Close()
		t.Fatalf("tamper provider: %v", err)
	}
	_ = file.Close()
	view, err := server.view(t.Context(), "capture-ui-fixture")
	if err != nil || view.Integrity.State != "invalid" {
		t.Fatalf("invalid view = %+v, %v", view, err)
	}
	if _, err := server.inspectableView(t.Context(), "capture-ui-fixture"); !errors.Is(err, errCaptureIntegrityInvalid) {
		t.Fatalf("inspectableView(invalid) error = %v", err)
	}
}

func TestServer_CachedValidViewDoesNotServePostVerificationTampering(t *testing.T) {
	t.Parallel()

	server, client, sessionDir := newTestServer(t)
	authorizeTestClient(t, server, client)
	revealTestClient(t, server, client)
	assertGETStatus(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/view", http.StatusOK)

	providerPath := filepath.Join(sessionDir, "provider.jsonl")
	providerInfo, err := os.Stat(providerPath)
	if err != nil {
		t.Fatalf("stat provider for post-verification tamper: %v", err)
	}
	providerData, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatalf("read provider for post-verification tamper: %v", err)
	}
	tampered := bytes.Replace(providerData, []byte(`"exchange-ui"`), []byte(`"exchange-vi"`), 1)
	if bytes.Equal(providerData, tampered) || len(providerData) != len(tampered) {
		t.Fatal("tamper fixture must alter bytes without changing file size")
	}
	if err := os.WriteFile(providerPath, tampered, 0o600); err != nil {
		t.Fatalf("rewrite provider after view cache: %v", err)
	}
	if err := os.Chtimes(providerPath, providerInfo.ModTime(), providerInfo.ModTime()); err != nil {
		t.Fatalf("restore provider timestamp for cache-bypass regression: %v", err)
	}

	response := testGET(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/raw?file=provider.jsonl&seq=1")
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatalf("read tampered detail response: %v", readErr)
	}
	if response.StatusCode != http.StatusBadRequest || !bytes.Contains(body, []byte("manifest hash mismatch")) {
		t.Fatalf("tampered detail status=%d body=%s", response.StatusCode, body)
	}
}

func TestServer_CacheIdentityIncludesManifestAndEvidenceState(t *testing.T) {
	t.Parallel()

	server, client, sessionDir := newTestServer(t)
	authorizeTestClient(t, server, client)
	assertGETStatus(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/view", http.StatusOK)
	manifestPath := filepath.Join(sessionDir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest for cache identity test: %v", err)
	}
	manifestData = bytes.Replace(manifestData, []byte(`"label": "ui-fixture"`), []byte(`"label": "cache-refreshed"`), 1)
	if err := os.WriteFile(manifestPath, manifestData, 0o600); err != nil {
		t.Fatalf("rewrite manifest for cache identity test: %v", err)
	}
	response := testGET(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/view")
	var refreshed projection.View
	if err := json.NewDecoder(response.Body).Decode(&refreshed); err != nil {
		t.Fatalf("decode refreshed view: %v", err)
	}
	_ = response.Body.Close()
	if refreshed.Capture.Label != "cache-refreshed" {
		t.Fatalf("cache ignored manifest identity change: label=%q", refreshed.Capture.Label)
	}

	provider, err := os.OpenFile(filepath.Join(sessionDir, "provider.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open provider for cache identity test: %v", err)
	}
	_, writeErr := provider.WriteString("tamper\n")
	closeErr := provider.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		t.Fatalf("tamper provider for cache identity test: %v", err)
	}
	response = testGET(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/view")
	var invalidated projection.View
	if err := json.NewDecoder(response.Body).Decode(&invalidated); err != nil {
		t.Fatalf("decode evidence-invalidated view: %v", err)
	}
	_ = response.Body.Close()
	if invalidated.Integrity.State != "invalid" {
		t.Fatalf("cache ignored evidence identity change: integrity=%+v", invalidated.Integrity)
	}
}

func TestEmbeddedApplication_DoesNotRequireInlineCSPExceptions(t *testing.T) {
	t.Parallel()
	application, err := assets.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read embedded application: %v", err)
	}
	for _, forbidden := range [][]byte{[]byte(`style="`), []byte(`onclick=`), []byte(`onload=`), []byte(`onerror=`)} {
		if bytes.Contains(application, forbidden) {
			t.Fatalf("embedded application contains CSP-blocked inline markup %q", forbidden)
		}
	}
}

func TestServer_RejectsHostOriginAndEvidencePathAbuse(t *testing.T) {
	t.Parallel()

	server, client, _ := newTestServer(t)
	hostRequest, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.LaunchURL(), nil)
	if err != nil {
		t.Fatalf("build hostile Host request: %v", err)
	}
	hostRequest.Host = "attacker.example"
	hostResponse, err := client.Do(hostRequest)
	if err != nil {
		t.Fatalf("hostile Host request: %v", err)
	}
	_ = hostResponse.Body.Close()
	if hostResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("hostile Host status = %d", hostResponse.StatusCode)
	}

	authorizeTestClient(t, server, client)
	originRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL()+"/api/v1/reveal", strings.NewReader(`{"confirm":"REVEAL"}`))
	if err != nil {
		t.Fatalf("build hostile Origin request: %v", err)
	}
	originRequest.Header.Set("Content-Type", "application/json")
	originRequest.Header.Set("Origin", "https://attacker.example")
	originResponse, err := client.Do(originRequest)
	if err != nil {
		t.Fatalf("hostile Origin request: %v", err)
	}
	_ = originResponse.Body.Close()
	if originResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("hostile Origin status = %d", originResponse.StatusCode)
	}

	revealTestClient(t, server, client)
	traversalURL := server.URL() + "/api/v1/captures/capture-ui-fixture/raw?file=%2e%2e%2fprovider.jsonl&seq=1"
	assertGETStatus(t, client, traversalURL, http.StatusBadRequest)
}

func TestServer_RejectsNonLoopbackListener(t *testing.T) {
	t.Parallel()

	_, err := Start(context.Background(), Config{ListenAddr: "0.0.0.0:0", CaptureRoot: t.TempDir(), CapabilityToken: "cap", RevealToken: "reveal"})
	if err == nil {
		t.Fatal("Start() accepted non-loopback listener")
	}
}

func newTestServer(t *testing.T) (*Server, *http.Client, string) {
	t.Helper()
	sessionDir := completeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	server, err := Start(ctx, Config{
		ListenAddr: "127.0.0.1:0", CaptureRoot: filepath.Dir(sessionDir), SessionDir: sessionDir,
		CapabilityToken: "capability-secret", RevealToken: "reveal-secret",
	})
	if err != nil {
		cancel()
		t.Fatalf("Start() error = %v", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		cancel()
		t.Fatalf("cookiejar.New() error = %v", err)
	}
	t.Cleanup(func() {
		cancel()
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		if err := server.Close(closeCtx); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return server, &http.Client{Jar: jar}, sessionDir
}

func authorizeTestClient(t *testing.T, server *Server, client *http.Client) {
	t.Helper()
	launch := testGET(t, client, server.LaunchURL())
	_ = launch.Body.Close()
	if launch.StatusCode != http.StatusOK || launch.Header.Get("Content-Security-Policy") == "" {
		t.Fatalf("launch response = %d CSP=%q", launch.StatusCode, launch.Header.Get("Content-Security-Policy"))
	}
	secondLaunch := testGET(t, client, server.LaunchURL())
	_ = secondLaunch.Body.Close()
	if secondLaunch.StatusCode != http.StatusGone {
		t.Fatalf("second launch status = %d, want one-time capability", secondLaunch.StatusCode)
	}
}

func revealTestClient(t *testing.T, server *Server, client *http.Client) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL()+"/api/v1/reveal", strings.NewReader(`{"confirm":"REVEAL"}`))
	if err != nil {
		t.Fatalf("build reveal request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", server.URL())
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("reveal request: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("reveal status = %d", response.StatusCode)
	}
}

func testGET(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", url, err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	return response
}

func assertGETStatus(t *testing.T, client *http.Client, url string, wanted int) {
	t.Helper()
	response := testGET(t, client, url)
	_ = response.Body.Close()
	if response.StatusCode != wanted {
		t.Fatalf("GET %s status = %d, want %d", url, response.StatusCode, wanted)
	}
}

func assertProviderEventPageExcludesPayload(t *testing.T, server *Server, client *http.Client) {
	t.Helper()
	response := testGET(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/provider-events?offset=0&limit=25")
	var page projection.ProviderEventPage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode provider events: %v", err)
	}
	_ = response.Body.Close()
	if page.Total == 0 || len(page.Items) == 0 {
		t.Fatalf("provider events page = %+v", page)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal provider events: %v", err)
	}
	if bytes.Contains(encoded, []byte("SECRET-MODEL-RESPONSE")) {
		t.Fatalf("provider event index exposed payload: %s", encoded)
	}
}

func assertRawRecordPage(t *testing.T, server *Server, client *http.Client) {
	t.Helper()
	response := testGET(t, client, server.URL()+"/api/v1/captures/capture-ui-fixture/records?file=provider.jsonl&limit=1")
	var page projection.RawRecordPage
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatalf("decode raw record page: %v", err)
	}
	_ = response.Body.Close()
	if len(page.Items) != 1 || !page.HasMore {
		t.Fatalf("raw record page = %+v", page)
	}
}

func completeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	recorder, err := capture.NewRecorder(capture.RecorderConfig{RootDir: root, SessionID: "capture-ui-fixture", Label: "ui-fixture"})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	lifecycle, err := capture.NewLifecycleRecorder(recorder.SessionDir(), "capture-ui-fixture")
	if err != nil {
		t.Fatalf("NewLifecycleRecorder() error = %v", err)
	}
	manifest, err := capture.NewSessionManifest(capture.SessionManifestConfig{SessionDir: recorder.SessionDir(), SessionID: "capture-ui-fixture", Label: "ui-fixture", Provider: capture.ProviderManifestInfo{Origin: "https://api.anthropic.com"}})
	if err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}
	requestBody := []byte(`{"model":"claude-test","metadata":{"user_id":"{\"session_id\":\"client-session\"}"},"messages":[{"role":"user","content":"TOP-SECRET-PROMPT</script><img src=x onerror=\"window.__zcpXSS=1\">"}]}`)
	requestHash := sha256.Sum256(requestBody)
	responseBody := []byte("" +
		`data: {"type":"message_start","message":{"id":"msg_ui_fixture"}}` + "\n\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"SECRET-MODEL-RESPONSE"}}` + "\n\n" +
		`data: {"type":"content_block_stop","index":0}` + "\n\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}` + "\n\n" +
		`data: {"type":"message_stop"}` + "\n\n")
	responseHash := sha256.Sum256(responseBody)
	for _, record := range []capture.Record{
		{Kind: capture.RecordProviderRequestStart, ExchangeID: "exchange-ui", Method: http.MethodPost, Path: "/v1/messages"},
		{Kind: capture.RecordProviderRequestBody, ExchangeID: "exchange-ui", BodyBase64: base64.StdEncoding.EncodeToString(requestBody), BodyBytes: int64(len(requestBody))},
		{Kind: capture.RecordProviderRequestEnd, ExchangeID: "exchange-ui", BodyBytes: int64(len(requestBody)), SHA256: hex.EncodeToString(requestHash[:])},
		{Kind: capture.RecordProviderResponseStart, ExchangeID: "exchange-ui", StatusCode: http.StatusOK, Headers: http.Header{"Content-Type": []string{"text/event-stream"}}},
		{Kind: capture.RecordProviderResponseBody, ExchangeID: "exchange-ui", BodyBase64: base64.StdEncoding.EncodeToString(responseBody), BodyBytes: int64(len(responseBody))},
		{Kind: capture.RecordProviderResponseEnd, ExchangeID: "exchange-ui", BodyBytes: int64(len(responseBody)), SHA256: hex.EncodeToString(responseHash[:])},
	} {
		if err := recorder.Record(record); err != nil {
			t.Fatalf("record provider fixture %s: %v", record.Kind, err)
		}
	}
	evalDir := filepath.Join(recorder.SessionDir(), "eval", "suite", "scenario")
	if err := os.MkdirAll(evalDir, 0o700); err != nil {
		t.Fatalf("mkdir eval artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(evalDir, "task-prompt.txt"), []byte("TOP-SECRET-PROMPT"), 0o600); err != nil {
		t.Fatalf("write eval artifact: %v", err)
	}
	if err := recorder.CloseDaemon(capture.CaptureComplete); err != nil {
		t.Fatalf("close provider: %v", err)
	}
	if err := lifecycle.Close(capture.CaptureComplete); err != nil {
		t.Fatalf("close lifecycle: %v", err)
	}
	if err := manifest.FinalizeDaemon(capture.CaptureComplete); err != nil {
		t.Fatalf("finalize manifest: %v", err)
	}
	return recorder.SessionDir()
}

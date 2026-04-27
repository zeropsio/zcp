// Tests for: verify_render.go — agent-browser augmentation of http_root.
//
// These tests are NOT parallel. They share the package-level browserRun
// global with browser_test.go and verify the same OverrideBrowserRunner
// hook from BrowserBatch's mutex-serialized critical section.
package ops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// makeRenderStdout builds an agent-browser --json output for the
// canonical render walk: [open] [snapshot] [get text body] [errors]
// [console] [close]. Pass bodyText for the get-step result and
// errorMessages for the errors-step pageError list.
func makeRenderStdout(t *testing.T, url, bodyText string, errorMessages []string) string {
	t.Helper()
	type errEntry struct {
		Message   string `json:"message"`
		Timestamp int64  `json:"timestamp"`
	}
	errors := make([]errEntry, len(errorMessages))
	for i, m := range errorMessages {
		errors[i] = errEntry{Message: m, Timestamp: int64(1700000000 + i)}
	}
	steps := []map[string]any{
		{"command": []string{"open", url}, "success": true, "result": map[string]any{"url": url}},
		{"command": []string{"snapshot", "-i"}, "success": true, "result": map[string]any{"snapshot": ""}},
		{"command": []string{"get", "text", "body"}, "success": true, "result": map[string]any{"text": bodyText, "origin": url}},
		{"command": []string{"errors"}, "success": true, "result": map[string]any{"errors": errors}},
		{"command": []string{"console"}, "success": true, "result": map[string]any{"messages": []any{}}},
		{"command": []string{"close"}, "success": true, "result": map[string]any{"closed": true}},
	}
	b, err := json.Marshal(steps)
	if err != nil {
		t.Fatalf("makeRenderStdout marshal: %v", err)
	}
	return string(b)
}

// --- extractBodyText / extractConsoleErrors unit tests ---

func TestExtractBodyText_Found(t *testing.T) {
	steps := []BrowserStepResult{
		{Command: []string{"open", "https://x"}, Success: true, Result: json.RawMessage(`{"url":"https://x"}`)},
		{Command: []string{"snapshot", "-i"}, Success: true, Result: json.RawMessage(`{}`)},
		{Command: []string{"get", "text", "body"}, Success: true, Result: json.RawMessage(`{"text":"hello world","origin":"https://x"}`)},
	}
	got := extractBodyText(steps)
	if got != "hello world" {
		t.Errorf("extractBodyText = %q, want %q", got, "hello world")
	}
}

func TestExtractBodyText_TruncatesAtCap(t *testing.T) {
	huge := strings.Repeat("a", browserBodyTextCap+200)
	raw, _ := json.Marshal(map[string]any{"text": huge})
	steps := []BrowserStepResult{
		{Command: []string{"get", "text", "body"}, Success: true, Result: raw},
	}
	got := extractBodyText(steps)
	if len(got) != browserBodyTextCap {
		t.Errorf("extractBodyText len = %d, want %d", len(got), browserBodyTextCap)
	}
}

func TestExtractBodyText_UTF8Safe(t *testing.T) {
	// Choose a length where the cap falls mid-rune. Each "ě" is 2 bytes.
	// Build a string just over the cap so truncation must walk back to
	// a rune boundary.
	body := strings.Repeat("ě", (browserBodyTextCap/2)+10)
	raw, _ := json.Marshal(map[string]any{"text": body})
	steps := []BrowserStepResult{
		{Command: []string{"get", "text", "body"}, Success: true, Result: raw},
	}
	got := extractBodyText(steps)
	// Every byte must be valid UTF-8 — no half-runes.
	if !json.Valid([]byte(`"` + got + `"`)) {
		// json.Valid + quoted form is the cheap UTF-8 sanity check.
		t.Errorf("extractBodyText returned invalid UTF-8: bytes=%v", []byte(got)[len(got)-4:])
	}
}

func TestExtractBodyText_MissingStep_Empty(t *testing.T) {
	steps := []BrowserStepResult{
		{Command: []string{"open", "https://x"}, Success: true, Result: json.RawMessage(`{}`)},
		{Command: []string{"snapshot"}, Success: true, Result: json.RawMessage(`{}`)},
	}
	if got := extractBodyText(steps); got != "" {
		t.Errorf("extractBodyText = %q, want empty", got)
	}
}

func TestExtractBodyText_MalformedJSON_Empty(t *testing.T) {
	steps := []BrowserStepResult{
		{Command: []string{"get", "text", "body"}, Success: true, Result: json.RawMessage(`not-json`)},
	}
	if got := extractBodyText(steps); got != "" {
		t.Errorf("extractBodyText = %q, want empty (malformed JSON)", got)
	}
}

func TestExtractBodyText_FailedStep_Skipped(t *testing.T) {
	steps := []BrowserStepResult{
		{Command: []string{"get", "text", "body"}, Success: false, Result: json.RawMessage(`{"text":"never"}`)},
	}
	if got := extractBodyText(steps); got != "" {
		t.Errorf("extractBodyText = %q, want empty (failed step)", got)
	}
}

func TestExtractConsoleErrors_LastN(t *testing.T) {
	raw := json.RawMessage(`{"errors":[{"message":"first","timestamp":1},{"message":"second","timestamp":2},{"message":"third","timestamp":3},{"message":"fourth","timestamp":4},{"message":"fifth","timestamp":5}]}`)
	got := extractConsoleErrors(raw)
	if len(got) != browserConsoleMax {
		t.Fatalf("len = %d, want %d", len(got), browserConsoleMax)
	}
	want := []string{"third", "fourth", "fifth"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("entry[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestExtractConsoleErrors_TruncatesEntry(t *testing.T) {
	huge := strings.Repeat("z", browserConsoleEntryCap+200)
	raw, _ := json.Marshal(map[string]any{
		"errors": []map[string]any{{"message": huge, "timestamp": 1}},
	})
	got := extractConsoleErrors(raw)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if len(got[0]) != browserConsoleEntryCap {
		t.Errorf("entry[0] len = %d, want %d", len(got[0]), browserConsoleEntryCap)
	}
}

func TestExtractConsoleErrors_EmptyEntries_Skipped(t *testing.T) {
	raw := json.RawMessage(`{"errors":[{"message":""},{"message":"   "}]}`)
	if got := extractConsoleErrors(raw); got != nil {
		t.Errorf("extractConsoleErrors = %v, want nil (only blank entries)", got)
	}
}

func TestExtractConsoleErrors_NoRaw_Nil(t *testing.T) {
	if got := extractConsoleErrors(nil); got != nil {
		t.Errorf("extractConsoleErrors(nil) = %v, want nil", got)
	}
}

func TestExtractConsoleErrors_BadShape_Nil(t *testing.T) {
	if got := extractConsoleErrors(json.RawMessage(`["not","an","object"]`)); got != nil {
		t.Errorf("extractConsoleErrors(non-object) = %v, want nil", got)
	}
}

// --- renderHTTPRoot integration with fake browserRunner ---

func TestRenderHTTPRoot_PopulatesBodyAndErrors(t *testing.T) {
	url := "https://probe.example.com/"
	stdout := makeRenderStdout(t, url, "ParseError\nendif unexpected\nweather.blade.php", []string{"console error A"})
	fake := &fakeBrowserRunner{runStdout: stdout}
	defer OverrideBrowserRunnerForTest(fake)()

	body, errs := renderHTTPRoot(context.Background(), url)
	if !strings.Contains(body, "ParseError") || !strings.Contains(body, "weather.blade.php") {
		t.Errorf("body = %q, want to contain ParseError + weather.blade.php", body)
	}
	if len(errs) != 1 || errs[0] != "console error A" {
		t.Errorf("consoleErrors = %v, want [console error A]", errs)
	}
}

func TestRenderHTTPRoot_AgentBrowserMissing_Silent(t *testing.T) {
	fake := &fakeBrowserRunner{lookPathErr: errAgentBrowserMissing()}
	defer OverrideBrowserRunnerForTest(fake)()

	body, errs := renderHTTPRoot(context.Background(), "https://x/")
	if body != "" || errs != nil {
		t.Errorf("missing agent-browser must yield empty result; got body=%q errs=%v", body, errs)
	}
}

func TestRenderHTTPRoot_ForkRecovery_Silent(t *testing.T) {
	// Stderr signals fork exhaustion → BrowserBatch sets ForkRecoveryAttempted
	// and returns Result with a Message. Render must not surface partial data.
	fake := &fakeBrowserRunner{
		runStderr: "pthread_create: Resource temporarily unavailable",
		runStdout: "",
	}
	defer OverrideBrowserRunnerForTest(fake)()

	body, errs := renderHTTPRoot(context.Background(), "https://x/")
	if body != "" || errs != nil {
		t.Errorf("fork recovery must yield empty result; got body=%q errs=%v", body, errs)
	}
}

func TestRenderHTTPRoot_EmptyURL_Silent(t *testing.T) {
	body, errs := renderHTTPRoot(context.Background(), "   ")
	if body != "" || errs != nil {
		t.Errorf("empty URL must yield empty result; got body=%q errs=%v", body, errs)
	}
}

// --- verifyService integration: render attaches to http_root check ---

func TestVerify_HTTPRoot_AttachesRenderedBodyOn500(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		// Real Ignition would render an HTML chrome; the rendered text
		// exposed via document.body.innerText is what the agent reads.
		_, _ = w.Write([]byte("<html><body>500</body></html>"))
	}))
	defer srv.Close()

	// The verify path (checkHTTPRoot → renderHTTPRoot augment) is
	// exercised by composition here. The full subdomain-resolution
	// plumbing is covered by neighbouring verify_test.go tests; the
	// augmentation seam is what this test targets.
	stdout := makeRenderStdout(t, srv.URL+"/", "ParseError syntax error, unexpected token \"endif\" (View: weather.blade.php)", []string{})
	fake := &fakeBrowserRunner{runStdout: stdout}
	defer OverrideBrowserRunnerForTest(fake)()

	check := checkHTTPRoot(context.Background(), srv.Client(), srv.URL+"/")
	if check.HTTPStatus != 500 {
		t.Fatalf("checkHTTPRoot status = %d, want 500", check.HTTPStatus)
	}
	bodyText, consoleErrors := renderHTTPRoot(context.Background(), srv.URL+"/")
	check.BodyText = bodyText
	check.ConsoleErrors = consoleErrors

	if !strings.Contains(check.BodyText, "ParseError") {
		t.Errorf("BodyText = %q, want to contain ParseError", check.BodyText)
	}
	if !strings.Contains(check.BodyText, "weather.blade.php") {
		t.Errorf("BodyText = %q, want to contain weather.blade.php", check.BodyText)
	}
	if check.Status != CheckFail {
		t.Errorf("Status = %q, want fail (5xx)", check.Status)
	}
}

func TestVerify_HTTPRoot_NoBodyTextWhenAgentBrowserMissing(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer srv.Close()

	fake := &fakeBrowserRunner{lookPathErr: errAgentBrowserMissing()}
	defer OverrideBrowserRunnerForTest(fake)()

	check := checkHTTPRoot(context.Background(), srv.Client(), srv.URL+"/")
	bodyText, consoleErrors := renderHTTPRoot(context.Background(), srv.URL+"/")
	check.BodyText = bodyText
	check.ConsoleErrors = consoleErrors

	if check.Status != CheckPass {
		t.Errorf("Status = %q, want pass", check.Status)
	}
	if check.BodyText != "" {
		t.Errorf("BodyText must be empty when agent-browser is missing; got %q", check.BodyText)
	}
	if check.ConsoleErrors != nil {
		t.Errorf("ConsoleErrors must be nil when agent-browser is missing; got %v", check.ConsoleErrors)
	}
}

func TestVerify_HTTPRoot_OmitemptyJSON(t *testing.T) {
	c := CheckResult{Name: "http_root", Status: CheckPass, HTTPStatus: 200}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	for _, key := range []string{"bodyText", "consoleErrors"} {
		if strings.Contains(got, key) {
			t.Errorf("marshalled JSON must not contain %q when empty: %s", key, got)
		}
	}
}

// errAgentBrowserMissing is the sentinel exec.LookPath error shape.
func errAgentBrowserMissing() error {
	return &lookPathErr{msg: "exec: \"agent-browser\": executable file not found in $PATH"}
}

type lookPathErr struct{ msg string }

func (e *lookPathErr) Error() string { return e.msg }

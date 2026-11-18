// Tests for: browser_env.go — `{{env:KEY}}` in a zerops_browser command: the
// page gets the value the container's live env store holds, a refused
// placeholder opens no browser, and the result carries the placeholder where
// the value came back (docs/spec-mate.md §5.8a, MF-19).
//
// Not parallel: BrowserBatch runs on the package's browser runner, overridden
// per test (see browser_test.go).

package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The test sign-in Fen's container holds: an e-mail, a password that JSON
// and a URL both escape, and a PIN too short to look for in a result.
const (
	fenEmail    = "fen@example.test"
	fenPassword = `pw"of<the>test & account`
	fenPIN      = "4271"
	fenZcpKey   = "zk-of-zcp-itself"
)

// fenEnvStore writes the live env store of Fen's container and returns its
// path.
func fenEnvStore(t *testing.T) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"TEST_EMAIL": fenEmail, "TEST_PASSWORD": fenPassword, "TEST_PIN": fenPIN,
		"ZCP_API_KEY": fenZcpKey, "VSCODE_PASSWORD": "vp-of-the-editor",
	})
	if err != nil {
		t.Fatalf("encode the store: %v", err)
	}
	path := filepath.Join(t.TempDir(), "env.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write the store: %v", err)
	}
	return path
}

// pageRunner answers each step of the batch as answer says, and as
// agent-browser writes it: the command as it ran, JSON that leaves `<` and
// `&` as they are.
type pageRunner struct {
	fakeBrowserRunner
	answer func(cmd []string) (result any, errText string)
}

func (p *pageRunner) Run(_ context.Context, stdin string, _ time.Duration) (string, string, bool, error) {
	p.lastStdin = stdin
	var batch [][]string
	if err := json.Unmarshal([]byte(stdin), &batch); err != nil {
		return "", "", false, err
	}
	steps := make([]map[string]any, 0, len(batch))
	for _, cmd := range batch {
		step := map[string]any{"command": cmd, "success": true, "result": map[string]any{"ok": true}}
		if p.answer != nil {
			if result, errText := p.answer(cmd); errText != "" {
				step["success"], step["error"] = false, errText
				delete(step, "result")
			} else if result != nil {
				step["result"] = result
			}
		}
		steps = append(steps, step)
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(steps); err != nil {
		return "", "", false, err
	}
	return out.String(), "", false, nil
}

// A placeholder reaches the page as the value the store holds now: a whole
// argument (a fill's text) and inside one (an eval's source), every
// occurrence — so a sign-in form can be filled without the value ever being
// in the call.
// witness: internal/ops/browser_env.go | return values[browserEnvRef.FindStringSubmatch(ref)[1]] → return ref
func TestBrowserBatch_EnvPlaceholder_ReachesThePageAsTheValue(t *testing.T) {
	runner := &pageRunner{}
	defer OverrideBrowserRunnerForTest(runner)()
	defer OverrideBrowserScaleForTest(func(context.Context) error { return nil })()

	_, err := BrowserBatch(t.Context(), BrowserBatchInput{
		URL:          "https://appdev.example.invalid/sign-in",
		EnvStorePath: fenEnvStore(t),
		Commands: [][]string{
			{"fill", "#email", "{{env:TEST_EMAIL}}"},
			{"find", "label", "Password", "fill", "{{env:TEST_PASSWORD}}"},
			{"eval", "window.who = '{{env:TEST_EMAIL}}'; window.again = '{{env:TEST_EMAIL}}'"},
		},
	})
	if err != nil {
		t.Fatalf("BrowserBatch: %v", err)
	}
	batch := parseStdinBatch(t, runner.lastStdin)
	for _, want := range [][]string{
		{"fill", "#email", fenEmail},
		{"find", "label", "Password", "fill", fenPassword},
		{"eval", "window.who = '" + fenEmail + "'; window.again = '" + fenEmail + "'"},
	} {
		if !slices.ContainsFunc(batch, func(cmd []string) bool { return slices.Equal(cmd, want) }) {
			t.Errorf("the page was not sent %q; the batch is %q", want, batch)
		}
	}
}

// A placeholder the store's reader refuses — a key it does not hold, a name
// Mate and zcp keep for themselves, something that is no key's name — fails
// the whole call before the browser is asked for, naming the key and never a
// value: a half-filled sign-in form is never submitted.
// witness: internal/ops/browser_env.go | if refused != nil { → if false {
func TestBrowserBatch_EnvPlaceholder_RefusedBeforeTheBrowserOpens(t *testing.T) {
	const typed = "typed-where-the-name-belongs"
	tests := []struct {
		name    string
		arg     string
		store   string
		wantKey string
	}{
		{name: "a key the store does not hold", arg: "{{env:SIGNING_KEY}}", wantKey: "SIGNING_KEY"},
		{name: "a name Mate and zcp keep for themselves", arg: "Bearer {{env:ZCP_API_KEY}}", wantKey: "ZCP_API_KEY"},
		{name: "the container's own machinery", arg: "{{env:VSCODE_PASSWORD}}", wantKey: "VSCODE_PASSWORD"},
		{name: "a value where the name belongs", arg: "{{env:TEST_PASSWORD=" + typed + "}}"},
		{name: "no store", arg: "{{env:TEST_PASSWORD}}", store: filepath.Join(t.TempDir(), "absent.json")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &pageRunner{}
			defer OverrideBrowserRunnerForTest(runner)()
			defer OverrideBrowserScaleForTest(func(context.Context) error { return nil })()
			store := tt.store
			if store == "" {
				store = fenEnvStore(t)
			}
			result, err := BrowserBatch(t.Context(), BrowserBatchInput{
				URL:          "https://appdev.example.invalid/sign-in",
				EnvStorePath: store,
				Commands:     [][]string{{"fill", "#email", "{{env:TEST_EMAIL}}"}, {"fill", "#password", tt.arg}},
			})
			if err == nil || result != nil {
				t.Fatalf("the call was answered: %+v", result)
			}
			if runner.lastIsolation != "" || runner.lastStdin != "" {
				t.Errorf("the browser was asked for (isolation %q) or ran %q", runner.lastIsolation, runner.lastStdin)
			}
			if !strings.Contains(err.Error(), tt.wantKey) {
				t.Errorf("the refusal does not name %s: %v", tt.wantKey, err)
			}
			for _, value := range []string{fenEmail, fenPassword, fenZcpKey, "vp-of-the-editor", typed} {
				if strings.Contains(err.Error(), value) {
					t.Errorf("the refusal carries a value: %v", err)
				}
			}
		})
	}
}

// The result never holds a substituted value: each command comes back as it
// was written, and a value that came back from the page — in a step's
// result, an eval's value, an error, the console, a failed request's URL —
// is its placeholder again, however JSON or the URL escaped it. A value too
// short to look for stays in ordinary output, which is left whole.
// witness: internal/ops/browser_env.go | replacer := strings.NewReplacer(pairs...) → replacer := strings.NewReplacer()
func TestBrowserBatch_EnvPlaceholder_TheResultKeepsThePlaceholder(t *testing.T) {
	runner := &pageRunner{answer: func(cmd []string) (any, string) {
		switch cmd[0] {
		case "get":
			return map[string]any{"value": fenPassword}, ""
		case "eval":
			return map[string]any{"origin": "https://appdev.example.invalid", "result": fenEmail + " signed in, " + fenPIN + " items"}, ""
		case "check":
			return nil, "no element matches " + fenPassword
		case "console":
			return map[string]any{"logs": []any{"posting " + fenPassword}}, ""
		case "network":
			return []any{map[string]any{"url": "https://appdev.example.invalid/login?password=" + url.QueryEscape(fenPassword), "method": "GET", "status": 401}}, ""
		}
		return nil, ""
	}}
	defer OverrideBrowserRunnerForTest(runner)()
	defer OverrideBrowserScaleForTest(func(context.Context) error { return nil })()

	written := [][]string{
		{"fill", "#email", "{{env:TEST_EMAIL}}"},
		{"fill", "#password", "{{env:TEST_PASSWORD}}"},
		{"fill", "#pin", "{{env:TEST_PIN}}"},
		{"get", "value", "#password"},
		{"eval", "document.title"},
		{"check", "{{env:TEST_PASSWORD}}"},
	}
	result, err := BrowserBatch(t.Context(), BrowserBatchInput{
		URL: "https://appdev.example.invalid/sign-in", EnvStorePath: fenEnvStore(t), Commands: written,
	})
	if err != nil {
		t.Fatalf("BrowserBatch: %v", err)
	}
	for _, want := range written {
		if !slices.ContainsFunc(result.Steps, func(step BrowserStepResult) bool { return slices.Equal(step.Command, want) }) {
			t.Errorf("no step names %q as it was written", want)
		}
	}
	answered, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("the result does not encode: %v", err)
	}
	var decoded struct {
		Steps []struct {
			Command []string `json:"command"`
			Result  any      `json:"result"`
			Error   string   `json:"error"`
		} `json:"steps"`
		ConsoleOutput any              `json:"consoleOutput"`
		NetworkOutput []NetworkRequest `json:"networkOutput"`
	}
	if err := json.Unmarshal(answered, &decoded); err != nil {
		t.Fatalf("the result does not decode: %v\n%s", err, answered)
	}
	read, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("encode what was read: %v", err)
	}
	var text bytes.Buffer
	if err := json.Indent(&text, read, "", " "); err != nil {
		t.Fatalf("indent: %v", err)
	}
	for _, form := range valueForms(fenPassword) {
		if strings.Contains(text.String(), form) || strings.Contains(string(answered), form) {
			t.Errorf("the result holds the password as %q:\n%s", form, answered)
		}
	}
	if strings.Contains(string(answered), fenEmail) {
		t.Errorf("the result holds the e-mail:\n%s", answered)
	}
	for _, want := range []string{
		"{{env:TEST_EMAIL}} signed in, " + fenPIN + " items",
		"no element matches {{env:TEST_PASSWORD}}",
		"posting {{env:TEST_PASSWORD}}",
		"/login?password={{env:TEST_PASSWORD}}",
	} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("the result lacks %q:\n%s", want, text.String())
		}
	}
}

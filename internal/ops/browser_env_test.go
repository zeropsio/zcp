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
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
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
	return envStoreOf(t, map[string]string{
		"TEST_EMAIL": fenEmail, "TEST_PASSWORD": fenPassword, "TEST_PIN": fenPIN,
		"ZCP_API_KEY": fenZcpKey, "VSCODE_PASSWORD": "vp-of-the-editor",
	})
}

// envStoreOf writes a live env store holding values and returns its path.
func envStoreOf(t *testing.T, values map[string]string) string {
	t.Helper()
	body, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("encode the store: %v", err)
	}
	path := filepath.Join(t.TempDir(), "env.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write the store: %v", err)
	}
	return path
}

// walkRunner runs the walk itself (runBrowserSteps) over a scripted
// agent-browser, so a step's fate is the walk's own.
type walkRunner struct {
	fakeBrowserRunner
	exec browserStepExec
}

func (w *walkRunner) Run(ctx context.Context, stdin string, _ time.Duration) (string, string, bool, error) {
	var steps [][]string
	if err := json.Unmarshal([]byte(stdin), &steps); err != nil {
		return "", "", false, err
	}
	return runBrowserSteps(ctx, steps, w.exec)
}

// signInWith opens a sign-in page whose commands are written and whose
// container holds values, on runner.
func signInWith(t *testing.T, runner browserRunner, values map[string]string, written [][]string) *BrowserBatchResult {
	t.Helper()
	defer OverrideBrowserRunnerForTest(runner)()
	defer OverrideBrowserScaleForTest(func(context.Context) error { return nil })()
	result, err := BrowserBatch(t.Context(), BrowserBatchInput{
		URL: "https://appdev.example.invalid/sign-in", EnvStorePath: envStoreOf(t, values), InMate: true, Commands: written,
	})
	if err != nil {
		t.Fatalf("BrowserBatch: %v", err)
	}
	return result
}

// stepOf is the step of result that ran command name, as the agent reads it.
func stepOf(t *testing.T, result *BrowserBatchResult, name string) BrowserStepResult {
	t.Helper()
	for _, step := range result.Steps {
		if len(step.Command) > 0 && step.Command[0] == name {
			return step
		}
	}
	t.Fatalf("the result has no %s step: %+v", name, result.Steps)
	return BrowserStepResult{}
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
// witness: internal/ops/browser_env.go | value := values[browserEnvRef.FindStringSubmatch(ref)[1]] → value := ref
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
// witness: internal/ops/browser_env.go | step.Result = u.redactor.json(step.Result) →
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
	for _, form := range []string{fenPassword, `pw\"of`, "of<the>test", url.QueryEscape(fenPassword), url.PathEscape(fenPassword)} {
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

// A percent-encoded value is found whichever characters the encoder chose to
// escape: a failed sign-in comes back with the password in the request's
// address and form body as the browser wrote it — `*`, `!`, `~` and a space
// are escaped by one encoder and left by another — and it is the placeholder
// there, never the password.
// witness: internal/ops/browser_match.go | if c == '%' && token.rawLen > 0 { → if false {
func TestBrowserBatch_EnvPlaceholder_APercentEncodedValueIsFoundWhicheverCharactersWereEscaped(t *testing.T) {
	tests := []struct {
		name, value, inURL, inBody string
	}{
		{name: "a form body leaves the star", value: "Tr0ub4dor&3*", inURL: "Tr0ub4dor%263*", inBody: "email=fen%40example.test&password=Tr0ub4dor%263*"},
		{name: "encodeURIComponent leaves the bang", value: "P@ssw0rd!", inURL: "P%40ssw0rd!", inBody: "password=P%40ssw0rd%21"},
		{name: "a form writes a space as a plus", value: "p@ss w0rd*~", inURL: "p%40ss+w0rd*%7E", inBody: "password=p%40ss%20w0rd%2a%7e"},
		{name: "every byte escaped", value: "Pa$$w0rd", inURL: "%50%61%24%24%77%30%72%64", inBody: "password=Pa%24%24w0rd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &pageRunner{answer: func(cmd []string) (any, string) {
				if cmd[0] != "network" {
					return nil, ""
				}
				return map[string]any{"requests": []any{map[string]any{
					"url": "https://appdev.example.invalid/login?password=" + tt.inURL, "method": "POST", "status": 401, "postData": tt.inBody,
				}}}, ""
			}}
			result := signInWith(t, runner, map[string]string{"TEST_PASSWORD": tt.value}, [][]string{{"fill", "#password", "{{env:TEST_PASSWORD}}"}})
			if len(result.NetworkOutput) != 1 || result.NetworkOutput[0].URL != "https://appdev.example.invalid/login?password={{env:TEST_PASSWORD}}" {
				t.Errorf("the failed request is listed as %+v, want its password as the placeholder", result.NetworkOutput)
			}
			tail := string(stepOf(t, result, "network").Result)
			if strings.Contains(tail, tt.inURL) || strings.Contains(tail, strings.TrimPrefix(tt.inBody[strings.LastIndex(tt.inBody, "=")+1:], "")) {
				t.Errorf("the network step holds the password as the browser encoded it: %s", tail)
			}
			if strings.Count(tail, "{{env:TEST_PASSWORD}}") != 2 {
				t.Errorf("the network step does not carry the placeholder in the address and in the body: %s", tail)
			}
		})
	}
}

// A value is found inside JSON that is itself inside a JSON string — a
// request body in the network lines, a page that logs JSON.stringify of what
// it sent, a server's error echoing that again — and as HTML writes it, in a
// snapshot or a `get html` of the form it was typed into.
// witness: internal/ops/browser_match.go | for level := 1; level <= browserRedactJSONDepth; level++ { → for level := 1; level <= 1; level++ {
func TestBrowserBatch_EnvPlaceholder_AValueInNestedJSONOrHTMLIsFound(t *testing.T) {
	const password = `pw"x\long<1>&'end`
	nested := func(levels int) string {
		text := password
		for range levels {
			encoded, err := json.Marshal(map[string]string{"password": text})
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			text = string(encoded)
		}
		return text
	}
	runner := &pageRunner{answer: func(cmd []string) (any, string) {
		switch cmd[0] {
		case "network":
			return map[string]any{"requests": []any{map[string]any{"url": "https://appdev.example.invalid/login", "status": 401, "postData": nested(1)}}}, ""
		case "console":
			return map[string]any{"messages": []any{
				map[string]any{"type": "log", "text": "sent " + nested(2)},
				map[string]any{"type": "error", "text": "the server said " + nested(3)},
			}}, ""
		case "get":
			return map[string]any{"html": `<input id="password" value="pw&quot;x\long&lt;1&gt;&amp;&#39;end">`}, ""
		case "snapshot":
			return map[string]any{"snapshot": `- textbox "Password": pw"x\long&lt;1&gt;&amp;'end`}, ""
		}
		return nil, ""
	}}
	result := signInWith(t, runner, map[string]string{"TEST_PASSWORD": password}, [][]string{
		{"fill", "#password", "{{env:TEST_PASSWORD}}"}, {"get", "html", "form"}, {"snapshot", "-i"},
	})
	answered, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("the result does not encode: %v", err)
	}
	// Read as the agent reads it: every string of the result, decoded, and
	// each JSON text found in one decoded again.
	var texts []string
	var collect func(any)
	collect = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				texts = append(texts, key)
				collect(child)
			}
		case []any:
			for _, child := range value {
				collect(child)
			}
		case string:
			texts = append(texts, value)
			if from := strings.Index(value, "{"); from >= 0 {
				var inner any
				if json.Unmarshal([]byte(value[from:]), &inner) == nil {
					collect(inner)
				}
			}
		}
	}
	var decoded any
	if err := json.Unmarshal(answered, &decoded); err != nil {
		t.Fatalf("the result does not decode: %v", err)
	}
	collect(decoded)
	for _, text := range texts {
		if strings.Contains(text, password) || strings.Contains(text, "x\\long") {
			t.Errorf("the result holds the password in %q", text)
		}
	}
	for _, want := range []string{`value="{{env:TEST_PASSWORD}}"`, `- textbox "Password": {{env:TEST_PASSWORD}}`, `{"password":"{{env:TEST_PASSWORD}}"}`} {
		if !slices.ContainsFunc(texts, func(text string) bool { return strings.Contains(text, want) }) {
			t.Errorf("the result lacks %q:\n%s", want, answered)
		}
	}
}

// A step that got no answer is named, in the message zcp writes about it, by
// the command as it was written: the value it ran with is not there, however
// short it is and whatever bytes it holds.
// witness: internal/ops/browser_env.go | unanswered.command = u.written[unanswered.step] →
func TestBrowserBatch_EnvPlaceholder_AStepWithNoAnswerIsNamedAsWritten(t *testing.T) {
	tests := []struct {
		name, value string
		spellings   []string
	}{
		{name: "a PIN too short to search for", value: "4271", spellings: []string{"4271"}},
		{name: "a control byte", value: "pass\x01word-long", spellings: []string{"pass\x01word-long", `pass\x01word-long`, `pass\u0001word-long`}},
		{name: "a delete byte", value: "pass\x7fword-long", spellings: []string{"pass\x7fword-long", `pass\x7fword-long`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &walkRunner{exec: func(_ context.Context, args []string) (string, string, error) {
				if args[0] == "fill" {
					return "", "agent-browser: daemon connection lost", errors.New("exit status 1")
				}
				return `{"success":true,"data":{"ok":true}}`, "", nil
			}}
			result := signInWith(t, runner, map[string]string{"TEST_SECRET": tt.value}, [][]string{{"fill", "#field", "{{env:TEST_SECRET}}"}})
			if !strings.Contains(result.Message, `fill #field {{env:TEST_SECRET}}`) {
				t.Errorf("the message does not name the step as written: %q", result.Message)
			}
			for _, spelling := range tt.spellings {
				if strings.Contains(result.Message, spelling) {
					t.Errorf("the message holds the value as %q: %q", spelling, result.Message)
				}
			}
		})
	}
}

// Each step names its command exactly as it was written — never searched
// for values: a password that is the word "password", a value equal to its
// key's name and a value that is itself placeholder text leave the selector,
// the waited-for text and the placeholders as the agent wrote them.
// witness: internal/ops/browser_env.go | step.Command = u.written[i] →
func TestBrowserBatch_EnvPlaceholder_TheEchoIsTheCommandAsWritten(t *testing.T) {
	written := [][]string{
		{"fill", "#password", "{{env:TEST_PASSWORD}}"},
		{"fill", "#user", "{{env:TEST_USER}}"},
		{"fill", "#note", "x{{env:TEST_NOTE}}y"},
		{"wait", "--text", "Wrong password"},
	}
	runner := &pageRunner{}
	result := signInWith(t, runner, map[string]string{
		"TEST_PASSWORD": "password", "TEST_USER": "TEST_USER", "TEST_NOTE": "{{env:TEST_USER}}",
	}, written)
	var caller [][]string
	for _, step := range result.Steps {
		if step.Command[0] == "fill" || (step.Command[0] == "wait" && step.Command[1] == "--text") {
			caller = append(caller, step.Command)
		}
	}
	if !slices.EqualFunc(caller, written, slices.Equal[[]string]) {
		t.Errorf("the steps name %q, want the commands as written %q", caller, written)
	}
	ran := parseStdinBatch(t, runner.lastStdin)
	if want := []string{"fill", "#note", "x{{env:TEST_USER}}y"}; !slices.ContainsFunc(ran, func(cmd []string) bool { return slices.Equal(cmd, want) }) {
		t.Errorf("a value that is placeholder text was read again: %q", ran)
	}
}

// In JavaScript the placeholder stands inside a quoted string and the page
// gets exactly the value: a password with a quote, a backslash, a backtick,
// `${` or a line end neither breaks the script nor sets another string —
// which would fail the sign-in as "wrong password" with nothing to see. An
// error that quotes the source back is the placeholder too.
// witness: internal/ops/browser_env.go | return jsStringEscape(value) → return value
func TestBrowserBatch_EnvPlaceholder_InAnEvalTheQuotedStringIsExactlyTheValue(t *testing.T) {
	const password = "it's \"a\" \\ `tick` ${x}\nnext"
	// The password as a JavaScript string's body, by the language's own
	// escapes: valid between ', " or `.
	const literal = `it\'s \"a\" \\ \` + "`" + `tick\` + "`" + ` \${x}\nnext`
	runner := &pageRunner{answer: func(cmd []string) (any, string) {
		if cmd[0] == "eval" {
			return nil, "SyntaxError: Unexpected identifier in " + cmd[1]
		}
		return nil, ""
	}}
	result := signInWith(t, runner, map[string]string{"TEST_PASSWORD": password}, [][]string{
		{"eval", "document.querySelector('#password').value = '{{env:TEST_PASSWORD}}'"},
		{"wait", "--fn", "document.title === `{{env:TEST_PASSWORD}}`"},
		{"fill", "#password", "{{env:TEST_PASSWORD}}"},
	})
	ran := parseStdinBatch(t, runner.lastStdin)
	for _, want := range [][]string{
		{"eval", "document.querySelector('#password').value = '" + literal + "'"},
		{"wait", "--fn", "document.title === `" + literal + "`"},
		{"fill", "#password", password},
	} {
		if !slices.ContainsFunc(ran, func(cmd []string) bool { return slices.Equal(cmd, want) }) {
			t.Errorf("the page was not sent %q; the batch is %q", want, ran)
		}
	}
	failed := stepOf(t, result, "eval")
	if failed.Error == nil || !strings.HasSuffix(*failed.Error, "value = '{{env:TEST_PASSWORD}}'") {
		t.Errorf("the error that quotes the source does not carry the placeholder: %v", failed.Error)
	}
}

// Two values that overlap in a text are taken out together: nothing of one
// is left standing beside the other's placeholder.
// witness: internal/ops/browser_match.go | end: max(last.end, span.end) → end: last.end
func TestBrowserBatch_EnvPlaceholder_OverlappingValuesAreReplacedTogether(t *testing.T) {
	runner := &pageRunner{answer: func(cmd []string) (any, string) {
		if cmd[0] == "get" {
			return map[string]any{"text": "abcdefghi"}, ""
		}
		return nil, ""
	}}
	result := signInWith(t, runner, map[string]string{"TEST_FIRST": "abcdef", "TEST_SECOND": "defghi"}, [][]string{
		{"fill", "#first", "{{env:TEST_FIRST}}"}, {"fill", "#second", "{{env:TEST_SECOND}}"}, {"get", "text", "#both"},
	})
	if got := string(stepOf(t, result, "get").Result); got != `{"text":"{{env:TEST_FIRST}}{{env:TEST_SECOND}}"}` {
		t.Errorf("the overlapping values came back as %s", got)
	}
}

// An eval's value is the page's own, keys and numbers included, while the
// names agent-browser wraps it in are not: a value that happens to be one of
// those names is taken out of the page's value and the step still reads as
// an eval's value.
// witness: internal/ops/browser_redact.go | if depth >= 1 { → if depth >= 0 {
func TestBrowserBatch_EnvPlaceholder_AnEvalsValueIsSearchedKeysAndNumbersToo(t *testing.T) {
	runner := &pageRunner{answer: func(cmd []string) (any, string) {
		if cmd[0] == "eval" {
			return map[string]any{"origin": "https://appdev.example.invalid", "result": map[string]any{"result": "signed in", "pin-830142": 830142}}, ""
		}
		return nil, ""
	}}
	result := signInWith(t, runner, map[string]string{"TEST_USER": "result", "TEST_PIN": "830142"}, [][]string{
		{"fill", "#user", "{{env:TEST_USER}}"}, {"fill", "#pin", "{{env:TEST_PIN}}"}, {"eval", "state()"},
	})
	var text string
	if err := json.Unmarshal(stepOf(t, result, "eval").Result, &text); err != nil {
		t.Fatalf("the eval's value is not text: %v", err)
	}
	if want := `{"pin-{{env:TEST_PIN}}":"{{env:TEST_PIN}}","{{env:TEST_USER}}":"signed in"}`; text != want {
		t.Errorf("the eval's value came back as %s, want %s", text, want)
	}
}

// A key the store does not hold is worded by where zcp runs: a Mate is sent
// to the ask that fills its own vault; a container that is no Mate, which
// has neither that ask nor mate=true, to this service's variables — never to
// a parameter its zerops_env refuses.
// witness: internal/ops/browser_env.go | LiveEnvNotHeldWords(refused.Key, inMate, "call again") → LiveEnvNotHeldWords(refused.Key, true, "call again")
func TestBrowserBatch_EnvPlaceholder_AMissingKeyIsWordedByWhereZcpRuns(t *testing.T) {
	for _, inMate := range []bool{true, false} {
		runner := &pageRunner{}
		restoreRunner := OverrideBrowserRunnerForTest(runner)
		restoreScale := OverrideBrowserScaleForTest(func(context.Context) error { return nil })
		_, err := BrowserBatch(t.Context(), BrowserBatchInput{
			URL: "https://appdev.example.invalid/sign-in", EnvStorePath: fenEnvStore(t), InMate: inMate,
			Commands: [][]string{{"fill", "#key", "{{env:SIGNING_KEY}}"}},
		})
		restoreScale()
		restoreRunner()
		var refusal *platform.PlatformError
		if !errors.As(err, &refusal) {
			t.Fatalf("in a Mate = %v: the call was not refused: %v", inMate, err)
		}
		if strings.Contains(refusal.Suggestion, "mate=true") != inMate {
			t.Errorf("in a Mate = %v, the refusal says: %s", inMate, refusal.Suggestion)
		}
	}
}

// A value comes back as the browser made it, not as it was stored: an
// e-mail field trims it, a one-line field drops its line breaks, a textarea
// gives LF for CRLF, a submitted form CRLF for LF, and a snapshot one text
// per line. Each of those is the placeholder too.
// witness: internal/ops/browser_redact.go | for _, form := range browserReturnForms(values[key]) { → for _, form := range []string{values[key]} {
func TestBrowserBatch_EnvPlaceholder_AValueTheBrowserNormalizedIsFound(t *testing.T) {
	values := map[string]string{
		"TEST_EMAIL":    " fen+test@Example.test ",
		"TEST_PASSWORD": "hunter2secret\n",
		"TEST_NOTES":    "line1-secret\r\nline2-secret",
		"TEST_ADDRESS":  "first-line-of-it\nsecond-line-of-it",
	}
	runner := &pageRunner{answer: func(cmd []string) (any, string) {
		switch cmd[0] {
		case "get":
			return map[string]any{"values": []any{"fen+test@Example.test", "hunter2secret", "line1-secret\nline2-secret"}}, ""
		case "snapshot":
			return map[string]any{"snapshot": "- textbox: first-line-of-it\n- text: second-line-of-it"}, ""
		case "network":
			return []any{map[string]any{"status": 401, "url": "https://appdev.example.invalid/login?email=fen%2Btest%40Example.test&notes=line1-secret%0D%0Aline2-secret&a=first-line-of-it%0D%0Asecond-line-of-it"}}, ""
		}
		return nil, ""
	}}
	result := signInWith(t, runner, values, [][]string{
		{"fill", "#email", "{{env:TEST_EMAIL}}"}, {"fill", "#password", "{{env:TEST_PASSWORD}}"},
		{"fill", "#notes", "{{env:TEST_NOTES}}"}, {"fill", "#address", "{{env:TEST_ADDRESS}}"},
		{"get", "value", "input"}, {"snapshot", "-i"},
	})
	if got := string(stepOf(t, result, "get").Result); got != `{"values":["{{env:TEST_EMAIL}}","{{env:TEST_PASSWORD}}","{{env:TEST_NOTES}}"]}` {
		t.Errorf("the fields' values came back as %s", got)
	}
	if got := string(stepOf(t, result, "snapshot").Result); got != `{"snapshot":"- textbox: {{env:TEST_ADDRESS}}\n- text: {{env:TEST_ADDRESS}}"}` {
		t.Errorf("the snapshot came back as %s", got)
	}
	if want := "https://appdev.example.invalid/login?email={{env:TEST_EMAIL}}&notes={{env:TEST_NOTES}}&a={{env:TEST_ADDRESS}}"; len(result.NetworkOutput) != 1 || result.NetworkOutput[0].URL != want {
		t.Errorf("the submitted form is listed as %+v, want %s", result.NetworkOutput, want)
	}
}

// In JavaScript a placeholder that is not plain content of a quoted string
// fails the call before the browser opens: left in code, the page's own
// syntax error would quote a piece of the value back. Where the scan cannot
// tell, it refuses too.
// witness: internal/ops/browser_script.go | return false → return true
func TestBrowserBatch_EnvPlaceholder_InJavaScriptOutsideAQuotedStringIsRefused(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		refused bool
	}{
		{name: "in code", command: []string{"eval", "x = {{env:TEST_PASSWORD}}"}, refused: true},
		{name: "in a selector built in code", command: []string{"eval", "document.querySelector({{env:TEST_PASSWORD}})"}, refused: true},
		{name: "after a backslash", command: []string{"eval", `x = '\{{env:TEST_PASSWORD}}'`}, refused: true},
		{name: "inside a template's expression", command: []string{"eval", "x = `${ {{env:TEST_PASSWORD}} }`"}, refused: true},
		{name: "right after a template's dollar", command: []string{"eval", "x = `${{env:TEST_PASSWORD}}`"}, refused: true},
		{name: "in a comment", command: []string{"eval", "x = 1 // {{env:TEST_PASSWORD}}"}, refused: true},
		{name: "after a string that ended", command: []string{"eval", "x = 'a' + {{env:TEST_PASSWORD}} + 'b'"}, refused: true},
		{name: "after what may be a regular expression", command: []string{"eval", "x = /'/.test(y) ? '{{env:TEST_PASSWORD}}' : ''"}, refused: true},
		{name: "in a string that runs into a line end", command: []string{"eval", "x = 'a\n{{env:TEST_PASSWORD}}'"}, refused: true},
		{name: "in a wait's function, in code", command: []string{"wait", "--fn", "document.title === {{env:TEST_PASSWORD}}"}, refused: true},
		{name: "in single quotes", command: []string{"eval", "x = '{{env:TEST_PASSWORD}}'"}},
		{name: "in double quotes", command: []string{"eval", `x = "user:{{env:TEST_PASSWORD}}"`}},
		{name: "in a template, beside an expression", command: []string{"eval", "x = `${a}{{env:TEST_PASSWORD}}${b}`"}},
		{name: "in a string after a comment and a path in a string", command: []string{"eval", "/* sign in */ fetch('/login', {body: '{{env:TEST_PASSWORD}}'})"}},
		{name: "in a wait's function, quoted", command: []string{"wait", "--fn", "document.title === '{{env:TEST_PASSWORD}}'"}},
		{name: "outside JavaScript, as it is", command: []string{"fill", "#password", "{{env:TEST_PASSWORD}}"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &pageRunner{}
			defer OverrideBrowserRunnerForTest(runner)()
			defer OverrideBrowserScaleForTest(func(context.Context) error { return nil })()
			_, err := BrowserBatch(t.Context(), BrowserBatchInput{
				URL: "https://appdev.example.invalid/sign-in", EnvStorePath: fenEnvStore(t), InMate: true, Commands: [][]string{tt.command},
			})
			if (err != nil) != tt.refused {
				t.Fatalf("refused = %v (%v), want %v", err != nil, err, tt.refused)
			}
			if tt.refused && (runner.lastIsolation != "" || runner.lastStdin != "") {
				t.Errorf("the browser was asked for (isolation %q) or ran %q", runner.lastIsolation, runner.lastStdin)
			}
		})
	}
}

// What the page names is searched in any step, not only an eval's value: a
// storage's keys are the page's own, and one that holds the e-mail or a
// token comes back with the placeholder in it.
// witness: internal/ops/browser_redact.go | key = leaf(key) →
func TestBrowserBatch_EnvPlaceholder_PageOwnedKeysOfAnyStepAreSearched(t *testing.T) {
	const email, token = "fen@example.test", "tok-of-the-session-1"
	runner := &pageRunner{answer: func(cmd []string) (any, string) {
		if cmd[0] == "storage" {
			return map[string]any{"data": map[string]any{"auth." + email + ".idToken": "x", token: "1"}}, ""
		}
		return nil, ""
	}}
	result := signInWith(t, runner, map[string]string{"TEST_EMAIL": email, "TEST_TOKEN": token}, [][]string{
		{"fill", "#email", "{{env:TEST_EMAIL}}"}, {"fill", "#token", "{{env:TEST_TOKEN}}"}, {"storage", "local"},
	})
	if got := string(stepOf(t, result, "storage").Result); got != `{"data":{"auth.{{env:TEST_EMAIL}}.idToken":"x","{{env:TEST_TOKEN}}":"1"}}` {
		t.Errorf("the storage came back as %s", got)
	}
}

// Any character of a value may come back escaped: as \uXXXX in any mix of
// hex case — an encoder that keeps `+`, `'` or `=` out of its output writes
// them so — as a surrogate pair, as a numeric HTML entity in decimal or
// hex, and the no-break space by its name.
// witness: internal/ops/browser_match.go | escapes = append(escapes, unicode) → if r == '<' || r == '>' || r == '&' || r >= utf8.RuneSelf { escapes = append(escapes, unicode) }
func TestBrowserBatch_EnvPlaceholder_AnyCharacterAsAUnicodeEscapeOrANumericEntityIsFound(t *testing.T) {
	u := func(hex string) string { return `\` + "u" + hex }
	tests := []struct{ name, value, comesBack string }{
		{name: "a plus as an escape", value: "fen+test@example.test", comesBack: "fen" + u("002B") + "test@example.test"},
		{name: "an apostrophe as an escape", value: "it's-secret", comesBack: "it" + u("0027") + "s-secret"},
		{name: "an equals sign as an escape", value: "base64==pad", comesBack: "base64" + u("003d") + u("003D") + "pad"},
		{name: "a letter as an escape", value: "abcdef", comesBack: u("0061") + "bcdef"},
		{name: "a pair, each half in its own case", value: "pw\U0001F600ok", comesBack: "pw" + u("D83d") + u("dE00") + "ok"},
		{name: "one escape in mixed case", value: "pwïok-1", comesBack: "pw" + u("00eF") + "ok-1"},
		{name: "a decimal entity", value: "tom&jerry", comesBack: "tom&#38;jerry"},
		{name: "a hex entity with leading zeros", value: "fen+test@example.test", comesBack: "fen&#X002b;test@example.test"},
		{name: "an entity past ASCII", value: "héslo-1", comesBack: "h&#233;slo-1"},
		{name: "a no-break space by name", value: "ab cdef", comesBack: "ab&nbsp;cdef"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &pageRunner{answer: func(cmd []string) (any, string) {
				if cmd[0] == "get" {
					return map[string]any{"text": "before " + tt.comesBack + " after"}, ""
				}
				return nil, ""
			}}
			result := signInWith(t, runner, map[string]string{"TEST_VALUE": tt.value}, [][]string{{"fill", "#field", "{{env:TEST_VALUE}}"}, {"get", "text", "#out"}})
			if got := string(stepOf(t, result, "get").Result); got != `{"text":"before {{env:TEST_VALUE}} after"}` {
				t.Errorf("%q came back as %s", tt.comesBack, got)
			}
		})
	}
}

// A value longer than the search is bounded for is not substituted at all:
// the call is refused before the browser opens, naming the key and not the
// value.
// witness: internal/ops/browser_env.go | if len(values[key]) > browserValueMaxLen { → if false {
func TestBrowserBatch_EnvPlaceholder_AValueOverTheLengthLimitIsRefused(t *testing.T) {
	for _, tt := range []struct {
		length  int
		refused bool
	}{{length: 4096}, {length: 4097, refused: true}} {
		runner := &pageRunner{}
		restoreRunner := OverrideBrowserRunnerForTest(runner)
		restoreScale := OverrideBrowserScaleForTest(func(context.Context) error { return nil })
		value := strings.Repeat("k3y-", tt.length/4+1)[:tt.length]
		_, err := BrowserBatch(t.Context(), BrowserBatchInput{
			URL: "https://appdev.example.invalid/sign-in", EnvStorePath: envStoreOf(t, map[string]string{"TEST_KEY": value}), InMate: true,
			Commands: [][]string{{"fill", "#key", "{{env:TEST_KEY}}"}},
		})
		restoreScale()
		restoreRunner()
		if (err != nil) != tt.refused || (tt.refused && runner.lastStdin != "") {
			t.Errorf("a value of %d bytes: refused = %v (ran %v), want %v before the browser", tt.length, err != nil, runner.lastStdin != "", tt.refused)
		}
		if err != nil && (!strings.Contains(err.Error(), "TEST_KEY") || strings.Contains(err.Error(), "k3y-k3y-")) {
			t.Errorf("the refusal does not name the key, or carries the value: %.200s", err.Error())
		}
	}
}

// When taking the values out cannot finish within its bound, nothing of the
// browser side is returned: text that was not searched is withheld, never
// handed back, and the result says so. The steps still say what ran.
// witness: internal/ops/browser_env.go | if u.redactor.spent { → if false {
func TestBrowserBatch_EnvPlaceholder_ASearchThatCannotFinishWithholdsTheText(t *testing.T) {
	if testing.Short() {
		t.Skip("spends the whole search bound, a second or two")
	}
	// A value of one repeated character against megabytes of that character
	// keeps a reading alive for every place it could have begun.
	value := strings.Repeat("a", 4000)
	page := strings.Repeat("a", 1<<20)
	runner := &pageRunner{answer: func(cmd []string) (any, string) {
		switch cmd[0] {
		case "get":
			return map[string]any{"text": page}, ""
		case "check":
			return nil, "no element matches " + value
		case "console":
			return map[string]any{"messages": []any{map[string]any{"text": "typed " + value}}}, ""
		}
		return nil, ""
	}}
	written := [][]string{{"fill", "#field", "{{env:TEST_VALUE}}"}, {"get", "text", "body"}, {"check", "#missing"}}
	result := signInWith(t, runner, map[string]string{"TEST_VALUE": value}, written)
	if result.Message != browserWithheld {
		t.Errorf("the result does not say its text was withheld: %.200q", result.Message)
	}
	answered, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("the result does not encode: %v", err)
	}
	if strings.Contains(string(answered), "aaaaaa") || len(result.ConsoleOutput) != 0 || len(result.ErrorsOutput) != 0 {
		t.Errorf("the withheld result carries text of the browser side: %.300s", answered)
	}
	for _, want := range written {
		if !slices.ContainsFunc(result.Steps, func(step BrowserStepResult) bool { return slices.Equal(step.Command, want) && step.Result == nil }) {
			t.Errorf("no step names %q as written, without a result", want)
		}
	}
	if failed := stepOf(t, result, "check"); failed.Success || failed.Error == nil || strings.Contains(*failed.Error, "aaa") {
		t.Errorf("the failed step does not say so without its text: %+v", failed)
	}
}

// A capture cut at its cap keeps no part of a value: where agent-browser's
// stderr was cut in the middle of one, its end goes from where the value
// began, so the first bytes of a password do not stand at the end of the
// message.
// witness: internal/ops/browser_env.go | stderr = u.redactor.cutText(stderr) → stderr = u.redactor.text(stderr)
func TestBrowserBatch_EnvPlaceholder_ACaptureCutAtItsCapKeepsNoPartOfAValue(t *testing.T) {
	const value = "correct-horse-battery-staple"
	// The cap falls ten bytes into the value.
	filler := strings.Repeat("x", browserOutputCap-len("debug: typing ")-10)
	message := typedWithStderr(t, value, filler+"debug: typing "+value+"\n")
	if !strings.HasSuffix(message, "debug: typing") {
		t.Errorf("the message ends %q, want it to end where the value began", message[max(0, len(message)-40):])
	}
	if strings.Contains(message, value[:6]) {
		t.Errorf("the message holds the beginning of the value: %q", message[max(0, len(message)-40):])
	}
}

// A step's own capture keeps as many bytes of its stderr as the walk's, so
// a step whose stderr filled it was cut there too: the value it went
// through keeps no part either, though the walk's capture took it whole.
// witness: internal/ops/browser_warm.go | if len(serr) >= browserOutputCap { → if false {
func TestBrowserBatch_EnvPlaceholder_ACaptureCutByTheStepsOwnCapKeepsNoPartOfAValue(t *testing.T) {
	const value = "correct-horse-battery-staple"
	// The step's capture ended ten bytes into the value.
	filler := strings.Repeat("x", browserOutputCap-len("debug: typing ")-10)
	message := typedWithStderr(t, value, filler+"debug: typing "+value[:10])
	if !strings.HasSuffix(message, "debug: typing") {
		t.Errorf("the message ends %q, want it to end where the value began", message[max(0, len(message)-40):])
	}
	if strings.Contains(message, value[:6]) {
		t.Errorf("the message holds the beginning of the value: %q", message[max(0, len(message)-40):])
	}
}

// typedWithStderr is the message of a call that types value into a field
// whose step fails with stderr.
func typedWithStderr(t *testing.T, value, stderr string) string {
	t.Helper()
	runner := &walkRunner{exec: func(_ context.Context, args []string) (string, string, error) {
		if args[0] == "fill" {
			return "", stderr, errors.New("exit status 1")
		}
		return `{"success":true,"data":{"ok":true}}`, "", nil
	}}
	return signInWith(t, runner, map[string]string{"TEST_PASSWORD": value}, [][]string{{"fill", "#password", "{{env:TEST_PASSWORD}}"}}).Message
}

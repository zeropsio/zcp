package ops

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Browser-render augmentation caps. Bound the BodyText / ConsoleErrors
// fields so a runaway page (megabytes of generated text, infinite
// console-error loop) cannot blow up the verify envelope. Detail-level
// truncation lives next to the data it bounds — agents downstream rely
// on these limits when sizing their own context budgets.
const (
	// browserBodyTextCap caps document.body.innerText. 8 KiB is enough
	// to capture a Laravel Ignition / Symfony / Rails error page with
	// stack frames, plus framework chrome — going larger blows the
	// envelope budget without adding diagnostic value.
	browserBodyTextCap = 8 << 10
	// browserConsoleMax caps how many console-error / pageerror entries
	// surface. Production pages routinely log dozens of warnings; only
	// the most recent are useful for triage.
	browserConsoleMax = 3
	// browserConsoleEntryCap caps each surfaced error message. Stack
	// traces from minified frontends can be tens of KiB on their own.
	browserConsoleEntryCap = 500
	// browserRenderTimeoutSeconds bounds the agent-browser walk that
	// the verify path spawns as best-effort enhancement. Short enough
	// that a wedged renderer does not block the verify response;
	// BrowserBatch's own deadline-recovery path kicks in past this.
	browserRenderTimeoutSeconds = 30
)

// renderHTTPRoot drives a bounded agent-browser walk against url and
// returns (bodyText, consoleErrors). Both return values are best-effort:
// on any failure (agent-browser missing, fork recovery, CDP wedge, JSON
// shape mismatch, ctx cancel) the function returns ("", nil) without
// surfacing an error. The verify path treats absence as the normal
// fallback — never as a degraded signal.
//
// The walk is the canonical batch [open, snapshot -i, get text body,
// errors, console, close]. snapshot -i forces the page to settle (the
// accessibility-tree dump waits for layout) so SPA frameworks reach a
// real DOM before we read innerText. We use the `get text body`
// command (textContent semantics) — the spec calls for innerText
// semantics, but any framework error page (Laravel Ignition, Symfony,
// Rails) renders content as actual DOM text, and `get text` is what
// agent-browser exposes. Style/script tags in the response body are
// invisible to textContent on rendered DOM (the browser itself
// suppresses them) which is what the spec wanted "innerText" to
// guarantee.
func renderHTTPRoot(ctx context.Context, url string) (string, []string) {
	if strings.TrimSpace(url) == "" {
		return "", nil
	}

	result, err := BrowserBatch(ctx, BrowserBatchInput{
		URL:            url,
		Commands:       [][]string{{"snapshot", "-i"}, {"get", "text", "body"}},
		TimeoutSeconds: browserRenderTimeoutSeconds,
	})
	if err != nil {
		// agent-browser missing / lock not acquired / unmarshal of input.
		// Best-effort path: silent no-op.
		return "", nil
	}
	// Fork-recovery, CDP wedge, deadline timeout, or parse failure all
	// leave Message set and the canonical [errors] step un-populated.
	// Treat any of those as "no rendered text available" — never claim
	// a partial walk produced data.
	if result.ForkRecoveryAttempted || result.Message != "" {
		return "", nil
	}

	bodyText := extractBodyText(result.Steps)
	consoleErrors := extractConsoleErrors(result.ErrorsOutput)
	return bodyText, consoleErrors
}

// extractBodyText pulls the `get text body` step's text field out of
// the canonical batch result. Returns "" when the step is missing,
// the JSON shape doesn't match, or the resulting text is empty.
// Result text is capped at browserBodyTextCap (UTF-8-safe truncation).
func extractBodyText(steps []BrowserStepResult) string {
	for i := range steps {
		s := steps[i]
		if !s.Success || len(s.Result) == 0 {
			continue
		}
		if !isCommand(s.Command, "get") {
			continue
		}
		// The canonical sub-shape is ["get", "text", "body"]; we accept
		// any "get text <selector>" so the tolerance survives caller
		// drift (e.g. "@e1" passed instead of "body").
		if len(s.Command) < 2 || s.Command[1] != "text" {
			continue
		}
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(s.Result, &payload); err != nil {
			continue
		}
		return capUTF8(strings.TrimSpace(payload.Text), browserBodyTextCap)
	}
	return ""
}

// extractConsoleErrors pulls the most recent browserConsoleMax messages
// from the canonical [errors] step. Returns nil when ErrorsOutput is
// absent or its JSON shape doesn't match. Each message is capped at
// browserConsoleEntryCap chars (UTF-8-safe).
func extractConsoleErrors(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var payload struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	if len(payload.Errors) == 0 {
		return nil
	}
	// The collector appends in chronological order — the spec asks for
	// the most recent N, so walk from the tail.
	start := max(0, len(payload.Errors)-browserConsoleMax)
	out := make([]string, 0, len(payload.Errors)-start)
	for _, e := range payload.Errors[start:] {
		msg := strings.TrimSpace(e.Message)
		if msg == "" {
			continue
		}
		out = append(out, capUTF8(msg, browserConsoleEntryCap))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// capUTF8 truncates s at limit bytes without splitting a multi-byte
// rune. UTF-8 truncation is necessary because the rendered text comes
// from arbitrary user-controlled framework output — splitting mid-rune
// would surface invalid UTF-8 in the verify envelope and break any
// downstream JSON consumer.
func capUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

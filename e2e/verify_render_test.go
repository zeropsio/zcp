//go:build e2e

// Tests for: e2e — zerops_verify rendered-text augmentation against the
// live agent-browser daemon in the ZCP container.
//
// These tests pin the contract that http_root carries body_text from a
// real browser walk so framework error pages (Laravel Ignition,
// Symfony, Rails) surface as searchable text — not just "HTTP 500".
//
// Run on zcp (eval-zcp project, agent-browser pre-installed):
//
//	/tmp/e2e-test -test.run TestVerifyRenderedText -test.v -test.timeout 300s
//
// The "broken Blade" case is opt-in via env var: it requires a service
// pre-staged with a deliberately broken Blade template (e.g. a
// `@if(...)@endif@if(...)@endif` mash). Stage once, point the env var
// at the service hostname, and the test pins that http_root surfaces
// the Ignition stack frame's identifying tokens.
//
//	ZCP_E2E_VERIFY_BROKEN_BLADE_SERVICE=appdev /tmp/e2e-test -test.run TestVerifyRenderedText_BrokenBlade -test.v
//
// The "happy path" case piggy-backs on whatever Laravel deploy already
// landed in the project (TestLaravelFullStack stages it in the same
// run). It asserts body_text is non-empty for a 2xx — the rendered
// state contract.

package e2e_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// verifyResultJSON mirrors ops.VerifyResult / CheckResult JSON for the
// fields this test reads. Kept inline so the e2e package does not pull
// in internal/ops directly.
type verifyResultJSON struct {
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
	Checks   []struct {
		Name          string   `json:"name"`
		Status        string   `json:"status"`
		Detail        string   `json:"detail,omitempty"`
		HTTPStatus    int      `json:"httpStatus,omitempty"`
		BodyText      string   `json:"bodyText,omitempty"`
		ConsoleErrors []string `json:"consoleErrors,omitempty"`
	} `json:"checks"`
}

func parseVerify(t *testing.T, text string) verifyResultJSON {
	t.Helper()
	var v verifyResultJSON
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("parse verify: %v\nraw: %s", err, text)
	}
	return v
}

func findHTTPRootCheck(t *testing.T, v verifyResultJSON) (httpStatus int, bodyText string, consoleErrors []string) {
	t.Helper()
	for _, c := range v.Checks {
		if c.Name == "http_root" {
			return c.HTTPStatus, c.BodyText, c.ConsoleErrors
		}
	}
	t.Fatalf("http_root check not found in verify result: %+v", v)
	return 0, "", nil
}

// TestVerifyRenderedText_BrokenBlade pins that a 5xx response from a
// Laravel app with a deliberately broken Blade template surfaces the
// Ignition error page's identifying tokens via http_root.body_text.
//
// Skipped when ZCP_E2E_VERIFY_BROKEN_BLADE_SERVICE is unset — staging
// requires a hand-broken weather.blade.php (or equivalent) in the app
// fixture. The test asserts substrings, not full text, because the
// exact Ignition chrome varies between Laravel versions.
func TestVerifyRenderedText_BrokenBlade(t *testing.T) {
	hostname := os.Getenv("ZCP_E2E_VERIFY_BROKEN_BLADE_SERVICE")
	if hostname == "" {
		t.Skip("ZCP_E2E_VERIFY_BROKEN_BLADE_SERVICE unset — pre-stage a Laravel app with a broken Blade template and point this env at it")
	}

	h := newHarness(t)
	s := newSession(t, h.srv)

	verifyText := s.mustCallSuccess("zerops_verify", map[string]any{
		"serviceHostname": hostname,
	})
	v := parseVerify(t, verifyText)

	httpStatus, bodyText, _ := findHTTPRootCheck(t, v)
	if httpStatus < 500 || httpStatus >= 600 {
		t.Fatalf("http_root.httpStatus = %d, want 5xx (broken Blade should render Ignition page); raw: %s", httpStatus, verifyText)
	}
	if bodyText == "" {
		t.Fatalf("http_root.bodyText is empty — agent-browser walk should surface the Ignition page; raw: %s", verifyText)
	}

	wantSubstrings := []string{"ParseError", "endif", "blade.php"}
	for _, want := range wantSubstrings {
		if !strings.Contains(bodyText, want) {
			t.Errorf("http_root.bodyText missing %q\nbodyText: %s", want, bodyText)
		}
	}
}

// TestVerifyRenderedText_HappyPath pins that for a 2xx response the
// rendered body_text is non-empty — the contract that "rendered state"
// is documented even when the page is fine.
//
// Skipped when ZCP_E2E_VERIFY_HEALTHY_WEB_SERVICE is unset. Point at
// any healthy web-facing service in the project (the Laravel app
// staged by TestLaravelFullStack works after subdomain enable).
func TestVerifyRenderedText_HappyPath(t *testing.T) {
	hostname := os.Getenv("ZCP_E2E_VERIFY_HEALTHY_WEB_SERVICE")
	if hostname == "" {
		t.Skip("ZCP_E2E_VERIFY_HEALTHY_WEB_SERVICE unset — point at any healthy web-facing service with subdomain enabled")
	}

	h := newHarness(t)
	s := newSession(t, h.srv)

	verifyText := s.mustCallSuccess("zerops_verify", map[string]any{
		"serviceHostname": hostname,
	})
	v := parseVerify(t, verifyText)

	httpStatus, bodyText, _ := findHTTPRootCheck(t, v)
	if httpStatus == 0 {
		t.Fatalf("http_root did not connect (httpStatus=0); raw: %s", verifyText)
	}
	if httpStatus >= 500 {
		t.Fatalf("expected non-5xx for healthy service, got %d; raw: %s", httpStatus, verifyText)
	}
	if bodyText == "" {
		t.Fatalf("http_root.bodyText must be populated for a 2xx-3xx-4xx response (rendered-state contract); raw: %s", verifyText)
	}
}

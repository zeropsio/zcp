package webui

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// jsGateAction decides whether an absent JS/DOM test dependency should fail
// the harness (fatal) or skip it, per spec-dataconsole-testing.md §7:
// ZCP_JS_REQUIRED=1 (required=true, set in CI) turns a missing-node/jsdom
// skip into a FAILURE in all three JS/DOM harnesses; locally (required=false)
// absence still skips. missing names exactly what is absent, embedded in the
// returned message so the caller's t.Fatal/t.Skip names it too.
func jsGateAction(required bool, missing string) (fatal bool, message string) {
	if required {
		return true, fmt.Sprintf("ZCP_JS_REQUIRED=1: %s missing; failing instead of skipping", missing)
	}
	return false, fmt.Sprintf("%s missing; skipping", missing)
}

// jsGateRequired reports whether ZCP_JS_REQUIRED=1 is set, the CI-only
// override that makes jsGateAction fail instead of skip.
func jsGateRequired() bool {
	return os.Getenv("ZCP_JS_REQUIRED") == "1"
}

// TestJSGate_RequiredMode_FailsInsteadOfSkip pins the seam that lets CI turn
// a missing node/jsdom dependency into a hard failure instead of a silent
// skip (spec-dataconsole-testing.md §7): "ZCP_JS_REQUIRED=1 (set in CI) turns
// a missing-node/jsdom skip into a FAILURE in all three harnesses ...
// locally, absence still skips." Independent oracle: the four (required,
// missing) -> fatal outcomes below are copied from that sentence, not
// derived from the implementation.
func TestJSGate_RequiredMode_FailsInsteadOfSkip(t *testing.T) {
	tests := []struct {
		name      string
		required  bool
		missing   string
		wantFatal bool
	}{
		{name: "not required, node missing", required: false, missing: "node", wantFatal: false},
		{name: "required, node missing", required: true, missing: "node", wantFatal: true},
		{name: "required, jsdom missing", required: true, missing: "node_modules/jsdom", wantFatal: true},
		{name: "not required, jsdom missing", required: false, missing: "node_modules/jsdom", wantFatal: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fatal, msg := jsGateAction(tt.required, tt.missing)
			if fatal != tt.wantFatal {
				t.Errorf("jsGateAction(%v, %q) fatal = %v, want %v", tt.required, tt.missing, fatal, tt.wantFatal)
			}
			if !strings.Contains(msg, tt.missing) {
				t.Errorf("jsGateAction(%v, %q) message %q does not name what is missing", tt.required, tt.missing, msg)
			}
		})
	}
}

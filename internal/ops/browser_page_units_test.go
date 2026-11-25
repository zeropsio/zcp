package ops

import (
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// witness: internal/ops/browser.go | if mark == devsup.PageMarkHold { → if mark == devsup.PageMarkFinal {
func TestBrowserPage_BPDevsup_TheSupervisorsPageFailsTheCall(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		mark, says    string
		failure, wait bool
	}{
		{"hold", "web is starting.", true, true},
		{"final", "web exited with code 1.", true, false},
		{"", "Internal Server Error", false, false},
	} {
		got, ok := devsupPageFailure(tt.mark, tt.says)
		if ok != tt.failure {
			t.Errorf("mark %q: failure = %v, want %v", tt.mark, ok, tt.failure)
			continue
		}
		if !ok {
			continue
		}
		if got.Code != platform.ErrPrerequisiteMissing || !strings.Contains(got.Message, strconv.Quote(tt.says)) {
			t.Errorf("mark %q: code/message = %q/%q, want prerequisite and quoted page text", tt.mark, got.Code, got.Message)
		}
		if !strings.Contains(got.Suggestion, "zerops_dev_server action=status") || strings.Contains(got.Suggestion, "waitSeconds") != tt.wait {
			t.Errorf("mark %q: suggestion = %q, want status and waitSeconds = %v", tt.mark, got.Suggestion, tt.wait)
		}
		if !tt.wait && (!strings.Contains(got.Suggestion, "exit") || !strings.Contains(got.Suggestion, "log")) {
			t.Errorf("mark %q: suggestion = %q, want exit and log", tt.mark, got.Suggestion)
		}
	}
}

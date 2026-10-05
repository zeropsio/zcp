// Tests for: tools/hq_describe_retry.go — a describe that meets HQ's Core
// rolling deploy waits it out instead of leaving the change a draft, run
// against a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
)

// TestDescribeChange_WaitsOutARollingDeploy: HQ's Core rolls for about 20 s,
// and a describe that met it failed once and left the change a draft. The
// description's calls are tried again while HQ is not reached or answers
// 502, 503 or 504, for about a minute; HQ's own refusal is never tried again.
// When the minute is spent, the Mate is told plainly that the description
// did not land and the change is still a draft.
func TestDescribeChange_WaitsOutARollingDeploy(t *testing.T) {
	isPatch := func(r *http.Request) bool { return r.Method == http.MethodPatch }
	isAttach := func(r *http.Request) bool { return strings.HasSuffix(r.URL.Path, "/attachments") }
	tests := []struct {
		name string
		// hq makes the fake HQ meet the describe as the case says.
		hq func(lab *hqLab)
		// picture: the words show a screenshot, so a picture is attached
		// first.
		picture       bool
		wantDescribed bool
		wantTries     int
		wantText      []string
	}{
		{
			name: "a 502 while Core rolls", hq: func(lab *hqLab) { lab.hq.answerWith(http.StatusBadGateway, "", 3, isPatch) },
			wantDescribed: true, wantTries: 4, wantText: []string{`"described":true`},
		},
		{
			name: "a standby's 503", hq: func(lab *hqLab) { lab.hq.standby(2, isPatch) },
			wantDescribed: true, wantTries: 3, wantText: []string{`"described":true`},
		},
		{
			name: "a 504", hq: func(lab *hqLab) { lab.hq.answerWith(http.StatusGatewayTimeout, "", 1, isPatch) },
			wantDescribed: true, wantTries: 2, wantText: []string{`"described":true`},
		},
		{
			name: "a picture's upload meets the roll", picture: true,
			hq:            func(lab *hqLab) { lab.hq.answerWith(http.StatusBadGateway, "", 2, isAttach) },
			wantDescribed: true, wantTries: 3, wantText: []string{`"described":true`},
		},
		{
			name: "HQ drops every connection for the whole minute", hq: func(lab *hqLab) { lab.hq.setDown(true) },
			wantTries: 8,
			wantText:  []string{`"described":false`, `"kept":true`, "did not land", "still a draft", "not asked to review", "8 tries"},
		},
		{
			name: "HQ's own refusal is not tried again", hq: func(lab *hqLab) { lab.hq.answerWith(http.StatusUnprocessableEntity, "invalid", 1, isPatch) },
			wantTries: 1, wantText: []string{`"described":false`, "did not take", "422"},
		},
		{
			name: "a 500 is HQ's own failure, not a roll", hq: func(lab *hqLab) { lab.hq.answerWith(http.StatusInternalServerError, "", 1, isPatch) },
			wantTries: 1, wantText: []string{`"described":false`, "did not take"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := deliveredLab(t)
			words := describedWords
			if tt.picture {
				words += "\n\n![The count](" + keptScreenshot(t, lab.stateDir, []byte("\x89PNG\r\n\x1a\n-a-screenshot"), 1280, 720) + ")"
			}
			tt.hq(lab)
			text, isError := lab.describe("", words)
			if isError {
				t.Fatalf("want an answer, got an error:\n%s", text)
			}
			for _, want := range tt.wantText {
				if !strings.Contains(text, want) {
					t.Errorf("the answer misses %q:\n%s", want, text)
				}
			}
			body := lab.hq.change(1).Body
			if (body != "") != tt.wantDescribed {
				t.Errorf("change #1's body = %q, want described=%v", body, tt.wantDescribed)
			}
			if got := len(lab.describeWaits) + 1; got != tt.wantTries {
				t.Errorf("tried %d times (waits %v), want %d", got, lab.describeWaits, tt.wantTries)
			}
			var waited time.Duration
			for _, d := range lab.describeWaits {
				waited += d
			}
			if waited > time.Minute {
				t.Errorf("waited %s in all, want about a minute at most", waited)
			}
		})
	}
}

// TestDescribeTransient: which answers of HQ a describe waits out.
func TestDescribeTransient(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"none", nil, false},
		{"connection refused", &hq.UnavailableError{Err: fmt.Errorf("hq /x: %w", syscall.ECONNREFUSED)}, true},
		{"connection reset", &hq.UnavailableError{Err: fmt.Errorf("hq /x: %w", syscall.ECONNRESET)}, true},
		{"a standby's 503", &hq.UnavailableError{Code: "not_active"}, true},
		{"no connection within the bound", &hq.UnavailableError{Err: &hq.NoAnswerError{Path: "/x", Within: time.Second}}, true},
		{"connected, no answer within the bound", &hq.UnavailableError{Err: &hq.NoAnswerError{Path: "/x", Connected: true, Within: time.Second}}, false},
		{"502", &hq.RefusedError{Status: http.StatusBadGateway}, true},
		{"504", &hq.RefusedError{Status: http.StatusGatewayTimeout}, true},
		{"500", &hq.RefusedError{Status: http.StatusInternalServerError}, false},
		{"409 change_not_open", &hq.RefusedError{Status: http.StatusConflict, Code: "conflict", Reason: "change_not_open"}, false},
		{"a picture HQ will not keep", fmt.Errorf("%w: HQ refused shot-1 (not_png)", errCannotAttach), false},
		{"anything else", errors.New("shot-1 is no longer kept"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := describeTransient(tt.err); got != tt.want {
				t.Errorf("describeTransient(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

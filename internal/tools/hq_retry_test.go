// Tests for: tools/hq_retry.go — a delivery step HQ cannot serve is tried at
// most three times, a short wait apart, and only while HQ could not serve it
// (spec-mate §10.10).
package tools

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
)

// TestDeliveryRetry_TriesOnlyWhileHQCannotServe: three tries at most, one
// wait between each two, and none after a try HQ answered — served or
// refused — or after one HQ left unanswered past its bound, which a wait
// would only spend again; a context that ends during a wait ends the tries
// there.
func TestDeliveryRetry_TriesOnlyWhileHQCannotServe(t *testing.T) {
	t.Parallel()
	const (
		answered   = hqAnswered
		notServing = hqNotServing
		silent     = hqSilent
	)
	tests := []struct {
		name      string
		answers   []hqAnswer // what each try meets; past the end, answered
		cancelled bool       // the context ends at the first wait
		wantTries int
		wantWaits []time.Duration
	}{
		{"HQ not serving throughout", []hqAnswer{notServing, notServing, notServing, notServing}, false, 3, []time.Duration{time.Second, 3 * time.Second}},
		{"served at once", nil, false, 1, nil},
		{"refused at once", []hqAnswer{answered}, false, 1, nil},
		{"served on the second try", []hqAnswer{notServing, answered}, false, 2, []time.Duration{time.Second}},
		{"served on the third try", []hqAnswer{notServing, notServing, answered}, false, 3, []time.Duration{time.Second, 3 * time.Second}},
		{"silent past its bound at once", []hqAnswer{silent, notServing}, false, 1, nil},
		{"silent on the second try", []hqAnswer{notServing, silent, notServing}, false, 2, []time.Duration{time.Second}},
		{"the call is cancelled while it waits", []hqAnswer{notServing, notServing, notServing}, true, 1, []time.Duration{time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var waits []time.Duration
			retry := hqRetry{
				waits: []time.Duration{time.Second, 3 * time.Second},
				pause: func(_ context.Context, d time.Duration) error {
					waits = append(waits, d)
					if tt.cancelled {
						return context.Canceled
					}
					return nil
				},
			}
			tried := 0
			tries := retry.run(t.Context(), func() hqAnswer {
				tried++
				if tried > len(tt.answers) {
					return answered
				}
				return tt.answers[tried-1]
			})
			if tries != tt.wantTries || tried != tt.wantTries {
				t.Errorf("tries = %d (ran %d), want %d", tries, tried, tt.wantTries)
			}
			if !reflect.DeepEqual(waits, tt.wantWaits) {
				t.Errorf("waits = %v, want %v", waits, tt.wantWaits)
			}
		})
	}
}

// TestHQUnavailable_OnlyUnreachableOr5xx: HQ not reached, or answering any
// 5xx — the balancer's 502 for an HQ that is down included — is HQ unable to
// serve; a 4xx, or an answer zcp could not read, is HQ's own word.
func TestHQUnavailable_OnlyUnreachableOr5xx(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"served", nil, false},
		{"not reached", &hq.UnavailableError{Err: errors.New("connection refused")}, true},
		{"a standby's 503", &hq.UnavailableError{Code: "not_active"}, true},
		{"the balancer's 502", &hq.RefusedError{Status: 502}, true},
		{"a 500", &hq.RefusedError{Status: 500, Code: "internal"}, true},
		{"a 504", fmt.Errorf("open: %w", &hq.RefusedError{Status: 504}), true},
		{"a 403", &hq.RefusedError{Status: 403, Code: "forbidden"}, false},
		{"a 404", &hq.RefusedError{Status: 404, Code: "repo_not_found"}, false},
		{"an answer zcp could not read", errors.New("hq /api/mate/changes: malformed answer"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hqUnavailable(tt.err); got != tt.want {
				t.Errorf("hqUnavailable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestHQNotServingWords: the last answer of a step HQ could not serve is said
// as HQ not serving — a 5xx as "HQ answered 502 (not serving)", over the API
// or through git — and never as HQ refusing.
func TestHQNotServingWords(t *testing.T) {
	t.Parallel()
	gitFailed := errors.New("exit status 128")
	tests := []struct {
		name   string
		err    error
		output string // git's, when the step ran git
		want   string
	}{
		{"the balancer's 502", &hq.RefusedError{Status: 502}, "", "HQ answered 502 (not serving)"},
		{"a 504 wrapped", fmt.Errorf("open: %w", &hq.RefusedError{Status: 504, Code: "timeout"}), "", "HQ answered 504 (not serving)"},
		{"a standby's 503", &hq.UnavailableError{Code: "not_active"}, "", "HQ answered 503 (not serving)"},
		{"not reached", &hq.UnavailableError{Err: errors.New("hq /api/mate/changes: dial tcp: connection refused")}, "", "no answer (hq /api/mate/changes: dial tcp: connection refused)"},
		{"git meets a 502", gitFailed, "fatal: unable to access 'https://hq.example/git/a/appdev.git/': The requested URL returned error: 502\n", "HQ answered 502 (not serving)"},
		{"git meets a 503", gitFailed, "error: RPC failed; HTTP 503 curl 22 The requested URL returned error: 503\n", "HQ answered 503 (not serving)"},
		{"no connection within the bound", &hq.UnavailableError{Err: &hq.NoAnswerError{Path: "/api/mate/changes", Within: 5 * time.Second}}, "", "no connection within 5s"},
		{"no answer within the bound", &hq.UnavailableError{Err: &hq.NoAnswerError{Path: "/api/mate/changes", Connected: true, Within: 10 * time.Second}}, "", "no answer within 10s"},
		{"git ended by its bound", gitFailed, "ZCP_HQ_NO_ANSWER: no answer within 15s\n", "no answer within 15s"},
		{"git reaches nothing", gitFailed, "fatal: unable to access 'https://hq.example/git/a/appdev.git/': Could not resolve host: hq.example\n", "fatal: unable to access 'https://hq.example/git/a/appdev.git/': Could not resolve host: hq.example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hqNotServingWords(tt.err)
			if tt.output != "" {
				got = gitNotServingWords(tt.err, []byte(tt.output))
			}
			if got != tt.want {
				t.Errorf("words = %q, want %q", got, tt.want)
			}
			if strings.Contains(got, "hq refused") {
				t.Errorf("an HQ that does not serve never reads as refusing: %q", got)
			}
		})
	}
}

// TestHQAnswerOf: a try of a delivery step met HQ answering — served, or
// refused, both final — HQ not serving it, which is tried again, or HQ silent
// past the try's bound, which ends the step.
func TestHQAnswerOf(t *testing.T) {
	t.Parallel()
	gitFailed := errors.New("exit status 128")
	tests := []struct {
		name   string
		err    error
		output string // git's, when the try ran git
		git    bool
		want   hqAnswer
	}{
		{"an API call served", nil, "", false, hqAnswered},
		{"an API call refused", &hq.RefusedError{Status: 403, Code: "forbidden"}, "", false, hqAnswered},
		{"an API call meets a 502", &hq.RefusedError{Status: 502}, "", false, hqNotServing},
		{"an API call meets a standby", &hq.UnavailableError{Code: "not_active"}, "", false, hqNotServing},
		{"an API call reaches nothing", &hq.UnavailableError{Err: errors.New("connection refused")}, "", false, hqNotServing},
		{"an API call silent past its bound", &hq.UnavailableError{Err: &hq.NoAnswerError{Within: 5 * time.Second}}, "", false, hqSilent},
		{"git served", nil, "", true, hqAnswered},
		{"git refused", gitFailed, "fatal: unable to access 'https://hq/x.git/': The requested URL returned error: 403", true, hqAnswered},
		{"git meets a 502", gitFailed, "fatal: unable to access 'https://hq/x.git/': The requested URL returned error: 502", true, hqNotServing},
		{"git reaches nothing", gitFailed, "fatal: unable to access 'https://hq/x.git/': Failed to connect to hq port 443", true, hqNotServing},
		{"git silent past its bound", gitFailed, "ZCP_HQ_NO_ANSWER: no answer within 15s", true, hqSilent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hqCallAnswer(tt.err)
			if tt.git {
				got = hqGitAnswer(tt.err, []byte(tt.output))
			}
			if got != tt.want {
				t.Errorf("answer = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDeliveryBounds_AStepEndsWellUnderAMinute pins the bounds a delivery
// step runs under: an HQ dropping every packet costs a step one bound — 5 s
// for an API call, HQGitBound for git — and even an HQ answering 5xx at the
// last moment of every try ends a step within a minute.
func TestDeliveryBounds_AStepEndsWellUnderAMinute(t *testing.T) {
	t.Parallel()
	var waits time.Duration
	for _, wait := range deliveryRetry.waits {
		waits += wait
	}
	tries := len(deliveryRetry.waits) + 1
	if deliveryBounds.connect > 5*time.Second || ops.HQGitBound > 15*time.Second {
		t.Errorf("an HQ that drops every packet costs an API step %s and a git step %s, want at most 5s and 15s",
			deliveryBounds.connect, ops.HQGitBound)
	}
	for name, bound := range map[string]time.Duration{"an API call": deliveryBounds.answer, "git": ops.HQGitBound} {
		if worst := bound*time.Duration(tries) + waits; worst >= time.Minute {
			t.Errorf("a step of %s ends within %s at worst, want under a minute", name, worst)
		}
	}
}

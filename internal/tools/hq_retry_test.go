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
)

// TestDeliveryRetry_TriesOnlyWhileHQCannotServe: three tries at most, one
// wait between each two, and none after a try HQ answered — served or
// refused; a context that ends during a wait ends the tries there.
func TestDeliveryRetry_TriesOnlyWhileHQCannotServe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		unavailable []bool // what each try answers; past the end, false
		cancelled   bool   // the context ends at the first wait
		wantTries   int
		wantWaits   []time.Duration
	}{
		{"HQ away throughout", []bool{true, true, true, true}, false, 3, []time.Duration{time.Second, 3 * time.Second}},
		{"served at once", nil, false, 1, nil},
		{"refused at once", []bool{false}, false, 1, nil},
		{"served on the second try", []bool{true, false}, false, 2, []time.Duration{time.Second}},
		{"served on the third try", []bool{true, true, false}, false, 3, []time.Duration{time.Second, 3 * time.Second}},
		{"the call is cancelled while it waits", []bool{true, true, true}, true, 1, []time.Duration{time.Second}},
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
			tries := retry.run(t.Context(), func() bool {
				tried++
				return tried <= len(tt.unavailable) && tt.unavailable[tried-1]
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

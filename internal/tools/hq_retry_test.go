// Tests for: tools/hq_retry.go — a call HQ cannot serve is tried again, a
// wait apart, and only while HQ could not serve it.
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

// TestHQRetry_TriesOnlyWhileHQCannotServe: one try more than the waits at
// most, one wait between each two, and none after a try HQ answered — served
// or refused; a context that ends during a wait ends the tries there.
func TestHQRetry_TriesOnlyWhileHQCannotServe(t *testing.T) {
	t.Parallel()
	const (
		answered   = hqAnswered
		notServing = hqNotServing
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

// TestHQNotServingWords: the last answer of a call HQ could not serve is said
// as HQ not serving — a 5xx as "HQ answered 502 (not serving)" — and never as
// HQ refusing.
func TestHQNotServingWords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"the balancer's 502", &hq.RefusedError{Status: 502}, "HQ answered 502 (not serving)"},
		{"a 504 wrapped", fmt.Errorf("open: %w", &hq.RefusedError{Status: 504, Code: "timeout"}), "HQ answered 504 (not serving)"},
		{"a standby's 503", &hq.UnavailableError{Code: "not_active"}, "HQ answered 503 (not serving)"},
		{"not reached", &hq.UnavailableError{Err: errors.New("hq /api/mate/changes: dial tcp: connection refused")}, "no answer (hq /api/mate/changes: dial tcp: connection refused)"},
		{"no connection within the bound", &hq.UnavailableError{Err: &hq.NoAnswerError{Path: "/api/mate/changes", Within: 5 * time.Second}}, "no connection within 5s"},
		{"no answer within the bound", &hq.UnavailableError{Err: &hq.NoAnswerError{Path: "/api/mate/changes", Connected: true, Within: 10 * time.Second}}, "no answer within 10s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hqNotServingWords(tt.err)
			if got != tt.want {
				t.Errorf("words = %q, want %q", got, tt.want)
			}
			if strings.Contains(got, "hq refused") {
				t.Errorf("an HQ that does not serve never reads as refusing: %q", got)
			}
		})
	}
}

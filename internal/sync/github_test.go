package sync

import (
	"errors"
	"testing"
)

// Tests for: transient-GitHub-read retry classification (github.go).
// Origin: Release CI v9.125.0 + v9.125.1 each lost ONE random guide file to
// a transient `gh api` failure (different file each run), which the pull
// then masked with exit 0 until an embed test failed two steps later
// (plans/backlog/sync-pull-per-item-error-masks-ci.md).

func TestRetryableGHRead_TableDriven(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error is not retryable", nil, false},
		{"transient exit status without HTTP status", errors.New("exit status 1\nstderr: "), true},
		{"server error is retryable", errors.New("exit status 1\nstderr: gh: Internal Server Error (HTTP 500)"), true},
		{"secondary rate limit is retryable", errors.New("exit status 1\nstderr: gh: You have exceeded a secondary rate limit (HTTP 403)"), true},
		{"genuine 404 fails fast", errors.New("exit status 1\nstderr: gh: Not Found (HTTP 404)"), false},
		{"404 status without phrase fails fast", errors.New("exit status 1\nstderr: gh: (HTTP 404)"), false},
		{"bad credentials fail fast", errors.New("exit status 1\nstderr: gh: Bad credentials (HTTP 401)"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := retryableGHRead(tc.err); got != tc.want {
				t.Errorf("retryableGHRead(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

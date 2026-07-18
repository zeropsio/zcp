package provider

import (
	"errors"
	"fmt"
	"net"
	"testing"
)

func TestHealthErr_ClassifiesReachabilityVsAuth(t *testing.T) {
	t.Parallel()
	// A socket-layer dial failure -> ErrUnreachable (VPN gate, 503).
	dial := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
	if err := HealthErr("kv: ping", dial); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("dial error: want ErrUnreachable, got %v", err)
	}
	// A wrapped "connection refused" string (driver-flattened) -> ErrUnreachable.
	if err := HealthErr("tabular: ping", fmt.Errorf("pq: %s", "dial tcp 10.0.0.1:5432: connect: connection refused")); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("string dial error: want ErrUnreachable, got %v", err)
	}
	// An auth/protocol error -> ErrUpstream (502, plain error, NOT a VPN hint).
	if err := HealthErr("tabular: ping", errors.New("password authentication failed for user")); !errors.Is(err, ErrUpstream) {
		t.Fatalf("auth error: want ErrUpstream, got %v", err)
	}
	if errors.Is(HealthErr("x", errors.New("password authentication failed")), ErrUnreachable) {
		t.Fatal("auth error must NOT classify as unreachable (would mislabel as VPN)")
	}
	// Status mapping.
	if HTTPStatus(ErrUnreachable) != 503 {
		t.Fatalf("ErrUnreachable status = %d, want 503", HTTPStatus(ErrUnreachable))
	}
}

// TestIsNetUnreachable_TextPatterns pins the full driver-flattened-to-string
// fallback list directly (TestHealthErr_ClassifiesReachabilityVsAuth above
// only spot-checks two of these through HealthErr). "connection reset by
// peer" and "broken pipe" are EXT-3: a mid-session connection drop a driver
// flattens to one of these bare phrases is exactly the reachability class
// this fallback exists for — the codebase's own tabular/query_test.go
// outageDriver fixture already uses "connection reset by peer" as its
// canonical "genuine outage" example, so the pattern list must recognize it.
func TestIsNetUnreachable_TextPatterns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		msg  string
		want bool
	}{
		{"connection refused", "dial tcp 10.0.0.1:5432: connect: connection refused", true},
		{"no such host", "dial tcp: lookup db.internal: no such host", true},
		{"i/o timeout", "read tcp 10.0.0.1:5432: i/o timeout", true},
		{"no route to host", "dial tcp 10.0.0.1:5432: connect: no route to host", true},
		{"network unreachable", "dial tcp 10.0.0.1:5432: connect: network is unreachable", true},
		{"operation timed out", "dial tcp 10.0.0.1:5432: connect: operation timed out", true},
		{"connection reset by peer", "read tcp 10.0.0.1:5432: read: connection reset by peer", true},
		{"broken pipe", "write tcp 10.0.0.1:5432: write: broken pipe", true},
		{"auth failure is not reachability", `password authentication failed for user "appuser"`, false},
		{"engine rejection is not reachability", `syntax error at or near "SELEKT"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsNetUnreachable(errors.New(tc.msg)); got != tc.want {
				t.Errorf("IsNetUnreachable(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

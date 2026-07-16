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

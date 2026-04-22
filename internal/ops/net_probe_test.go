package ops

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// TestProbeManagedReachable covers the happy and failure branches of the
// TCP dial probe. A fake dialer stands in for the real network so tests
// don't need a listener — they just confirm the probe honors the dialer's
// verdict and closes connections on success.
func TestProbeManagedReachable(t *testing.T) {
	tests := []struct {
		name      string
		host      string
		port      int
		dialerErr error
		want      bool
	}{
		{"reachable host → true", "db", 5432, nil, true},
		{"unreachable host → false", "db", 5432, errors.New("no route to host"), false},
		{"empty host short-circuits false", "", 5432, errors.New("should not dial"), false},
		{"zero port short-circuits false", "db", 0, errors.New("should not dial"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialed := false
			restore := OverrideVPNProbeDialerForTest(func(_ context.Context, address string, _ time.Duration) (net.Conn, error) {
				dialed = true
				if tt.dialerErr != nil {
					return nil, tt.dialerErr
				}
				// Return a self-closed fake conn; the real probe only needs
				// successful dial → we don't exercise reads/writes.
				client, server := net.Pipe()
				_ = server.Close()
				return client, nil
			})
			defer restore()

			got := ProbeManagedReachable(context.Background(), tt.host, tt.port)
			if got != tt.want {
				t.Errorf("ProbeManagedReachable(%q, %d) = %v, want %v", tt.host, tt.port, got, tt.want)
			}
			// Short-circuit cases must NOT touch the dialer.
			if (tt.host == "" || tt.port == 0) && dialed {
				t.Errorf("short-circuit path should not call the dialer; got dialed=%v", dialed)
			}
		})
	}
}

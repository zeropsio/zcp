package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

type sequencingSSH struct {
	failCount int
	called    int
}

func (s *sequencingSSH) ExecSSH(_ context.Context, _ string, _ string) ([]byte, error) {
	s.called++
	if s.called <= s.failCount {
		return nil, fmt.Errorf("connection refused")
	}
	return nil, nil
}

func (s *sequencingSSH) ExecSSHStdin(ctx context.Context, hostname, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

// ExecSSHBackground — waitSSHReady never uses the background variant but
// the interface requires both methods, so stub it with the same pattern.
func (s *sequencingSSH) ExecSSHBackground(_ context.Context, _ string, _ string, _ time.Duration) ([]byte, error) {
	return nil, nil
}

func TestWaitSSHReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		deployer  SSHDeployer
		ctx       func() (context.Context, context.CancelFunc)
		cfg       sshReadyConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "ImmediateSuccess",
			deployer: &sequencingSSH{failCount: 0},
			ctx:      func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			cfg:      sshReadyConfig{interval: time.Millisecond, timeout: 100 * time.Millisecond, command: "true"},
		},
		{
			name:     "FailThenSuccess",
			deployer: &sequencingSSH{failCount: 3},
			ctx:      func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			cfg:      sshReadyConfig{interval: time.Millisecond, timeout: 100 * time.Millisecond, command: "true"},
		},
		{
			name:      "Timeout",
			deployer:  &sequencingSSH{failCount: 9999},
			ctx:       func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			cfg:       sshReadyConfig{interval: time.Millisecond, timeout: 10 * time.Millisecond, command: "true"},
			wantErr:   true,
			errSubstr: "did not answer within",
		},
		{
			name:     "ContextCanceled",
			deployer: &sequencingSSH{failCount: 9999},
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			cfg:       sshReadyConfig{interval: time.Millisecond, timeout: 100 * time.Millisecond, command: "true"},
			wantErr:   true,
			errSubstr: "context canceled",
		},
		{
			name:      "NilDeployer",
			deployer:  nil,
			ctx:       func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			cfg:       sshReadyConfig{interval: time.Millisecond, timeout: 100 * time.Millisecond, command: "true"},
			wantErr:   true,
			errSubstr: "no SSH deployer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := tt.ctx()
				defer cancel()

				err := waitSSHReady(ctx, tt.deployer, "testhost", tt.cfg)
				if tt.wantErr {
					if err == nil {
						t.Fatal("expected error, got nil")
					}
					if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
						t.Errorf("error %q does not contain %q", err.Error(), tt.errSubstr)
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			})
		})
	}
}

// answersSSH answers the readiness probe with first, then with rest on every
// later call.
type answersSSH struct {
	first, rest error
	called      bool
}

func (s *answersSSH) ExecSSH(_ context.Context, _, _ string) ([]byte, error) {
	if !s.called {
		s.called = true
		return nil, s.first
	}
	return nil, s.rest
}

func (s *answersSSH) ExecSSHStdin(ctx context.Context, hostname, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

func (s *answersSSH) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return nil, nil
}

// witness: internal/platform/errors.go | if SSHExitCode(err) > 0 { → if SSHExitCode(err) == 1 {
func TestErrorClass_E3_ProbeAdmissionRefusalIsRefusedAtOnceWithPlatformWords(t *testing.T) {
	t.Parallel()
	refusal := &platform.SSHExecError{Hostname: "webstage", Output: "Access denied. Current SSH isolation: vpn\n", Err: platform.ExitStatusError(2)}
	synctest.Test(t, func(t *testing.T) {
		err := waitSSHReady(t.Context(), &answersSSH{first: refusal, rest: refusal}, "webstage",
			sshReadyConfig{interval: time.Second, timeout: 30 * time.Second, command: "true"})
		var probe *platform.SSHProbeError
		if !errors.As(err, &probe) {
			t.Fatalf("err = %v, want an SSHProbeError", err)
		}
		if c := platform.ClassOf(err); c != platform.ClassRefused || probe.Waited != 0 {
			t.Fatalf("ClassOf = %q, waited %s; want refused, not waited on", c, probe.Waited)
		}
		if !strings.Contains(err.Error(), "Access denied. Current SSH isolation: vpn") {
			t.Fatalf("err = %q, want the platform's words", err.Error())
		}
	})
}

// witness: internal/ops/ssh_ready.go | probe.Waited = cfg.timeout → probe.Waited = 0
func TestErrorClass_E7_ProbeBoundRunOutReportsBoundAndLastCause(t *testing.T) {
	t.Parallel()
	first := &platform.SSHExecError{Hostname: "webstage", Output: "ssh: connect to host webstage port 22: Connection refused\n", Err: platform.ExitStatusError(255)}
	last := &platform.SSHExecError{Hostname: "webstage", Output: "kex_exchange_identification: Connection closed by remote host\n", Err: platform.ExitStatusError(255)}
	synctest.Test(t, func(t *testing.T) {
		err := waitSSHReady(t.Context(), &answersSSH{first: first, rest: last}, "webstage",
			sshReadyConfig{interval: time.Second, timeout: 30 * time.Second, command: "true"})
		var probe *platform.SSHProbeError
		if !errors.As(err, &probe) {
			t.Fatalf("err = %v, want an SSHProbeError", err)
		}
		if probe.Waited != 30*time.Second || platform.ClassOf(err) != platform.ClassUnreachable {
			t.Fatalf("waited %s, class %q; want 30s, unreachable", probe.Waited, platform.ClassOf(err))
		}
		if msg := err.Error(); !strings.Contains(msg, "Connection closed by remote host") || !strings.Contains(msg, "within 30s") {
			t.Fatalf("err = %q, want the bound and the last cause", msg)
		}
	})
}

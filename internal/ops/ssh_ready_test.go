package ops

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
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
			errSubstr: "ssh not ready",
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
	}
}

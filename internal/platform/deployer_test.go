// Tests for: platform/deployer.go — SystemLocalDeployer and SystemSSHDeployer exec wrappers.
package platform

import (
	"context"
	"strings"
	"testing"
)

func TestSystemLocalDeployer_ExecZcli_NotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "zcli not installed",
			args:    []string{"push", "--serviceId", "svc-123"},
			wantErr: "zcli",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := NewSystemLocalDeployer()
			_, err := d.ExecZcli(context.Background(), tt.args...)
			if err == nil {
				t.Fatal("expected error when zcli is not installed")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestSystemSSHDeployer_ExecSSH_NotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		command  string
		wantErr  string
	}{
		{
			name:     "ssh executes with hostname and command",
			hostname: "appdev",
			command:  "cd /var/www && zcli push",
			wantErr:  "ssh appdev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := NewSystemSSHDeployer()
			_, err := d.ExecSSH(context.Background(), tt.hostname, tt.command)
			if err == nil {
				t.Fatal("expected error when ssh is not available or host unreachable")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

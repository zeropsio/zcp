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

func TestSSHArgs_HostKeyOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		command  string
		want     []string
	}{
		{
			name:     "includes StrictHostKeyChecking and UserKnownHostsFile",
			hostname: "appstage",
			command:  "cd /var/www && zcli push",
			want: []string{
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				"appstage", "cd /var/www && zcli push",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := sshArgs(tt.hostname, tt.command)
			if len(got) != len(tt.want) {
				t.Fatalf("sshArgs() = %v (len %d), want %v (len %d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("sshArgs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

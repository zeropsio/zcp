// Tests for: platform/deployer.go — SystemLocalDeployer exec wrapper.
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

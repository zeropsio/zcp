// Tests for: ops/deploy.go — Deploy mode detection and error cases.
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestDeploy_NoParams(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	ssh := &mockSSHDeployer{}
	authInfo := testAuthInfo()

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
		"", "", "", false)
	if err == nil {
		t.Fatal("expected error for no params")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrInvalidParameter)
	}
}

func TestDeploy_ModeDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sourceService string
		targetService string
		wantMode      string
		wantSource    string
	}{
		{
			name:          "source + target = SSH cross-deploy",
			sourceService: "builder",
			targetService: "app",
			wantMode:      "ssh",
			wantSource:    "builder",
		},
		{
			name:          "only target = SSH self-deploy (auto-inferred)",
			sourceService: "",
			targetService: "app",
			wantMode:      "ssh",
			wantSource:    "app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "builder"},
					{ID: "svc-2", Name: "app"},
				})
			ssh := &mockSSHDeployer{output: []byte("ok")}
			authInfo := testAuthInfo()

			result, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
				tt.sourceService, tt.targetService, "", false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != "BUILD_TRIGGERED" {
				t.Errorf("status = %s, want BUILD_TRIGGERED", result.Status)
			}
			if result.MonitorHint == "" {
				t.Error("monitorHint should not be empty")
			}
			if result.Mode != tt.wantMode {
				t.Errorf("mode = %s, want %s", result.Mode, tt.wantMode)
			}
			if result.SourceService != tt.wantSource {
				t.Errorf("sourceService = %s, want %s", result.SourceService, tt.wantSource)
			}
		})
	}
}

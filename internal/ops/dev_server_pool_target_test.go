package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// A pool's or Box's own service hostname is never a dev-server target: an SSH
// to a pool's service hostname reaches an arbitrary container (PS-3); a dev
// server in a pool runs in a lease, addressed by its alias.
func TestVerifyDevServerTarget_PoolOrBoxHostname_Refused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, host, typeVersion, want string
	}{
		{name: "swarm pool", host: "webpool", typeVersion: "swarm@1", want: "zerops_lease"},
		{name: "swarm-vm pool", host: "vmpool", typeVersion: "swarm-vm@1", want: "zerops_lease"},
		{name: "box", host: "box", typeVersion: "ubuntu/zbox@1", want: "Zerops Box"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServices([]platform.ServiceStack{{
				ID: "svc-" + tt.host, Name: tt.host, Status: platform.ServiceStatusActive,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: tt.typeVersion},
			}})
			err := verifyDevServerTarget(context.Background(), mock, "proj-1", tt.host)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("verifyDevServerTarget(%q) = %v, want a PlatformError refusing a %s", tt.host, err, tt.typeVersion)
			}
			if pe.Code != platform.ErrInvalidParameter {
				t.Errorf("code = %s, want %s", pe.Code, platform.ErrInvalidParameter)
			}
			if !strings.Contains(pe.Message+" "+pe.Suggestion, tt.want) {
				t.Errorf("refusal %q / %q should name %q", pe.Message, pe.Suggestion, tt.want)
			}
		})
	}
}

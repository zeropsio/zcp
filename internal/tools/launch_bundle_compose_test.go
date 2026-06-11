package tools

import (
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestServiceSecretsToBundleEnvs_DropsInfrastructure pins the F0 bundle-leak
// fix: infrastructure-classified keys (GIT_TOKEN, ZCP_API_KEY, ...) read from
// the push-source service's SECRET env layer must NEVER be copied into the
// export/launch bundle envSecrets — the destination project re-emits its own
// equivalents (GIT_TOKEN at git-push-setup, ZCP_* at container init), and the
// import YAML is agent-visible, so carrying them forward leaks the source's
// live credential verbatim.
func TestServiceSecretsToBundleEnvs_DropsInfrastructure(t *testing.T) {
	t.Parallel()

	in := []platform.ServiceEnvVar{
		{ID: "1", Key: "GIT_TOKEN", Content: "github_pat_LIVE_SECRET"},
		{ID: "2", Key: "ZCP_API_KEY", Content: "zerops-standing-key"},
		{ID: "3", Key: "APP_KEY", Content: "laravel-app-key"},
		{ID: "4", Key: "DATABASE_POOL", Content: "10"},
		{ID: "5", Key: "ZCP_LAUNCH_TOKEN", Content: "staged-launch-token"},
	}

	out := serviceSecretsToBundleEnvs(in)

	got := map[string]string{}
	for _, e := range out {
		got[e.Key] = e.Value
	}
	if _, leaked := got["GIT_TOKEN"]; leaked {
		t.Error("GIT_TOKEN leaked into bundle envSecrets — infrastructure keys must be filtered")
	}
	if _, leaked := got["ZCP_API_KEY"]; leaked {
		t.Error("ZCP_API_KEY leaked into bundle envSecrets — infrastructure keys must be filtered")
	}
	if _, leaked := got["ZCP_LAUNCH_TOKEN"]; leaked {
		t.Error("ZCP_LAUNCH_TOKEN (staged launch token) leaked into bundle envSecrets — infrastructure keys must be filtered")
	}
	// GAP0-1 regression: genuine app secrets + plain config still carry.
	if got["APP_KEY"] != "laravel-app-key" {
		t.Errorf("APP_KEY must still carry (GAP0-1); got %q", got["APP_KEY"])
	}
	if got["DATABASE_POOL"] != "10" {
		t.Errorf("DATABASE_POOL must still carry; got %q", got["DATABASE_POOL"])
	}
}

// TestServiceSecretsToBundleEnvs_AllInfrastructureYieldsNil pins the empty
// result shape: when every SECRET env is infrastructure-classified the
// helper returns nil (not an empty slice), matching the no-secrets case so
// the composer's `len(svcSecrets) > 0` gate skips the envSecrets block.
func TestServiceSecretsToBundleEnvs_AllInfrastructureYieldsNil(t *testing.T) {
	t.Parallel()

	out := serviceSecretsToBundleEnvs([]platform.ServiceEnvVar{
		{ID: "1", Key: "GIT_TOKEN", Content: "github_pat_LIVE_SECRET"},
	})
	if out != nil {
		t.Errorf("all-infrastructure input must yield nil, got %v", out)
	}
}

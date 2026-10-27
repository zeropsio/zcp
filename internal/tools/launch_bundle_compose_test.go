package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// TestServiceUserEnvsToBundleSecrets_DropsInfrastructure pins the F0 bundle-leak
// fix: infrastructure-classified keys (GIT_TOKEN, ZCP_API_KEY, ...) read from
// the push-source service's SECRET env layer must NEVER be copied into the
// export/launch bundle's service vault — the destination project re-emits its own
// equivalents (GIT_TOKEN at git-push-setup, ZCP_* at container init), and the
// import YAML is agent-visible, so carrying them forward leaks the source's
// live credential verbatim.
func TestServiceUserEnvsToBundleSecrets_DropsInfrastructure(t *testing.T) {
	t.Parallel()

	in := []platform.ServiceEnvVar{
		{ID: "1", Key: "GIT_TOKEN", Content: "github_pat_LIVE_SECRET"},
		{ID: "2", Key: "ZCP_API_KEY", Content: "zerops-standing-key"},
		{ID: "3", Key: "APP_KEY", Content: "laravel-app-key"},
		{ID: "4", Key: "DATABASE_POOL", Content: "10"},
		{ID: "5", Key: "ZCP_LAUNCH_TOKEN", Content: "staged-launch-token"},
	}

	out := serviceUserEnvsToBundleSecrets(in)

	got := map[string]string{}
	for _, e := range out {
		got[e.Key] = e.Value
	}
	if _, leaked := got["GIT_TOKEN"]; leaked {
		t.Error("GIT_TOKEN leaked into the bundle's service vault — infrastructure keys must be filtered")
	}
	if _, leaked := got["ZCP_API_KEY"]; leaked {
		t.Error("ZCP_API_KEY leaked into the bundle's service vault — infrastructure keys must be filtered")
	}
	if _, leaked := got["ZCP_LAUNCH_TOKEN"]; leaked {
		t.Error("ZCP_LAUNCH_TOKEN (staged launch token) leaked into the bundle's service vault — infrastructure keys must be filtered")
	}
	// GAP0-1 regression: genuine app secrets + plain config still carry.
	if got["APP_KEY"] != "laravel-app-key" {
		t.Errorf("APP_KEY must still carry (GAP0-1); got %q", got["APP_KEY"])
	}
	if got["DATABASE_POOL"] != "10" {
		t.Errorf("DATABASE_POOL must still carry; got %q", got["DATABASE_POOL"])
	}
}

// TestServiceUserEnvsToBundleSecrets_AllInfrastructureYieldsNil pins the empty
// result shape: when every SECRET env is infrastructure-classified the
// helper returns nil (not an empty slice), matching the no-secrets case so
// the composer's `len(svcVault) > 0` gate skips the service vault block.
func TestServiceUserEnvsToBundleSecrets_AllInfrastructureYieldsNil(t *testing.T) {
	t.Parallel()

	out := serviceUserEnvsToBundleSecrets([]platform.ServiceEnvVar{
		{ID: "1", Key: "GIT_TOKEN", Content: "github_pat_LIVE_SECRET"},
	})
	if out != nil {
		t.Errorf("all-infrastructure input must yield nil, got %v", out)
	}
}

// TestBuildLaunchBundle_PoolInProject_NamedNotCarried pins spec-workflows.md
// §8 PS-3 for launch-production: a Swarm pool or Zerops Box the source
// project holds is never emitted into the production import (not as the app
// runtime, not as a managed dependency), and the bundle names each one in a
// warning that tells the agent to add it to the production import itself —
// rather than dropping it silently. A pool is not "infrastructure" by the
// platform category, so without this it would have been taken for a runtime.
func TestBuildLaunchBundle_PoolInProject_NamedNotCarried(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj1", Name: "demo", Status: "ACTIVE"}).
		WithServices([]platform.ServiceStack{
			runtimeService("appdev", "php-apache@8.4", false),
			managedService(),
			runtimeService("fleet", "swarm@1", false),
			runtimeService("fleetvm", "swarm-vm@1", false),
			runtimeService("lab", "ubuntu/zbox@1", false),
		})
	ssh := &routedSSH{responses: map[string]string{
		"cat /var/www/zerops.yaml": exportTestZeropsYAML,
		"git rev-parse HEAD":       "deadbeef",
	}}
	runtimes := []resolvedLaunchRuntime{{
		ChoiceHostname:  "appdev",
		PushHostname:    "appdev",
		ProdHostname:    "app",
		SetupName:       "appdev",
		SetupProvenance: "dev-setup-promoted",
	}}
	gateChecks := []*LaunchSourceControlCheck{{
		ChoiceHostname: "appdev",
		PushHostname:   "appdev",
		MetaRemoteURL:  "https://github.com/example/demo.git",
	}}

	inputs, _, err := composeLaunchBundleInputs(
		context.Background(), mock, ssh, runtime.Info{InContainer: true},
		"proj1", "myapp-prod", runtimes, gateChecks,
		nil, nil, nil, nil, bundle.VariantLaunchNew,
	)
	if err != nil {
		t.Fatalf("composeLaunchBundleInputs: %v", err)
	}
	b, err := ops.BuildLaunchBundle(inputs, nil)
	if err != nil {
		t.Fatalf("BuildLaunchBundle: %v", err)
	}

	for _, host := range []string{"fleet", "fleetvm", "lab"} {
		if strings.Contains(b.ImportYAML, "hostname: "+host) {
			t.Errorf("%s must not be emitted into the production import:\n%s", host, b.ImportYAML)
		}
		var named string
		for _, w := range b.Warnings {
			if strings.Contains(w, `"`+host+`"`) {
				named = w
			}
		}
		if named == "" {
			t.Errorf("warnings must name %s as not carried, got: %v", host, b.Warnings)
			continue
		}
		if !strings.Contains(named, "not carried") || !strings.Contains(named, "add it to the production import yourself") {
			t.Errorf("warning for %s must say it is not carried and to add it yourself, got: %q", host, named)
		}
	}
	// The app runtime and the managed dependency still ride.
	for _, host := range []string{"app", "db"} {
		if !strings.Contains(b.ImportYAML, "hostname: "+host) {
			t.Errorf("%s must still be in the production import:\n%s", host, b.ImportYAML)
		}
	}
}

// Tests for: ops/pool_migrate_retire.go — the reads the retire's guards make
// of what still points at the dev service: env references and domains.
package ops

import (
	"context"
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestPoolMigrateEnvReferences_NamesEveryReferenceButTheDevsOwn(t *testing.T) {
	t.Parallel()
	services := []platform.ServiceStack{
		{ID: "svc-dev", Name: "appdev"},
		{ID: "svc-stage", Name: "appstage"},
		{ID: "svc-worker", Name: "worker"},
		{ID: "svc-pool", Name: "apppool", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}},
	}
	mock := platform.NewMock().
		WithProjectEnv([]platform.ProjectEnvVar{
			{Key: "DEV_URL", Content: "https://${appdev_zeropsSubdomain}"},
			{Key: "OTHER", Content: "${appdevx_hostname}"},
		}).
		WithServiceEnv("svc-dev", []platform.ServiceEnvVar{{Key: "SELF", Content: "${appdev_hostname}"}}).
		WithServiceEnv("svc-stage", []platform.ServiceEnvVar{{Key: "API", Content: "http://${appdev_hostname}:3000"}, {Key: "PLAIN", Content: "x"}}).
		WithServiceEnv("svc-worker", []platform.ServiceEnvVar{{Key: "SECRET", Content: platform.MaskedEnvValue, Sensitive: true}}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "APP", Content: "${appstage_hostname}"}, {Key: "API_TOKEN", Content: platform.MaskedEnvValue, Sensitive: true}})

	refs, unread, err := PoolMigrateEnvReferences(context.Background(), mock, "proj-1", "appdev", services)
	if err != nil {
		t.Fatalf("PoolMigrateEnvReferences: %v", err)
	}
	if want := []string{"appstage API", "project DEV_URL"}; !slices.Equal(refs, want) {
		t.Errorf("references = %q, want %q (the dev's own env and a longer hostname are not references to it)", refs, want)
	}
	if want := []string{"worker SECRET"}; !slices.Equal(unread, want) {
		t.Errorf("unread = %q, want %q (a pool's own API_TOKEN is a token, never a reference)", unread, want)
	}
}

func TestPoolMigrateDevDomains_RoutedToTheDevByID(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithPublicHTTPRoutings(
		platform.PublicHTTPRouting{
			Domains:   []platform.PublicHTTPDomain{{Name: "dev.example.com"}, {Name: "www.dev.example.com"}},
			Locations: []platform.PublicHTTPLocation{{Path: "/", Port: 3000, ServiceID: "svc-dev"}},
		},
		platform.PublicHTTPRouting{
			Domains:   []platform.PublicHTTPDomain{{Name: "example.com"}},
			Locations: []platform.PublicHTTPLocation{{Path: "/", Port: 3000, ServiceID: "svc-stage"}},
		},
	)
	got, err := PoolMigrateDevDomains(context.Background(), mock, "proj-1", "svc-dev")
	if err != nil {
		t.Fatalf("PoolMigrateDevDomains: %v", err)
	}
	if want := []string{"dev.example.com", "www.dev.example.com"}; !slices.Equal(got, want) {
		t.Errorf("domains = %q, want %q", got, want)
	}
}

// TestPoolMigrateDevDomains_OwnPlatformSubdomain_NotACustomDomain pins that
// the dev's own zerops.app subdomains in the routing list are no domain the
// retire waits on: they die with the service, so the retire names them under
// what goes with it, never as a custom domain to move first.
func TestPoolMigrateDevDomains_OwnPlatformSubdomain_NotACustomDomain(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithPublicHTTPRoutings(
		platform.PublicHTTPRouting{
			Domains:   []platform.PublicHTTPDomain{{Name: "appdev-38ff-3000.prg1.zerops.app"}, {Name: "appdev-38ff.prg1.zerops.app"}},
			Locations: []platform.PublicHTTPLocation{{Path: "/", Port: 3000, ServiceID: "svc-dev"}},
		},
	)
	got, err := PoolMigrateDevDomains(context.Background(), mock, "proj-1", "svc-dev")
	if err != nil {
		t.Fatalf("PoolMigrateDevDomains: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("domains = %q, want none: a service's own zerops.app subdomain is no custom domain", got)
	}
}

package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestResolveSubdomainURL_DirectBuild — the happy path: project's
// SubdomainHost is a full "{prefix}.{rest}" string, BuildSubdomainURL
// returns a valid URL, no env fetch happens.
func TestResolveSubdomainURL_DirectBuild(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", SubdomainHost: "1df2.prg1.zerops.app"})

	svc := &platform.ServiceStack{
		ID: "svc-1", Name: "api",
		SubdomainAccess: true,
		Ports:           []platform.Port{{Port: 3000}},
	}

	got := ResolveSubdomainURL(context.Background(), mock, "p1", svc)
	want := "https://api-1df2-3000.prg1.zerops.app"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestResolveSubdomainURL_BarePrefixFallback — when the project's
// SubdomainHost is just a prefix without a dot (so BuildSubdomainURL
// returns ""), the helper falls back to ExtractDomainFromEnv, parses
// the domain off the service's zeropsSubdomain env, and stitches a URL.
// This branch (verify_checks.go:166-174) had no direct test before.
func TestResolveSubdomainURL_BarePrefixFallback(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", SubdomainHost: "1df2"}).
		WithServiceEnv("svc-1", []platform.EnvVar{
			{Key: "zeropsSubdomain", Content: "https://api-1df2-3000.prg1.zerops.app"},
		})

	svc := &platform.ServiceStack{
		ID: "svc-1", Name: "api",
		SubdomainAccess: true,
		Ports:           []platform.Port{{Port: 3000}},
	}

	got := ResolveSubdomainURL(context.Background(), mock, "p1", svc)
	want := "https://api-1df2-3000.prg1.zerops.app"
	if got != want {
		t.Errorf("got %q, want %q (bare-prefix fallback)", got, want)
	}
}

// TestResolveSubdomainURL_BarePrefixPort80 — port 80 case omits the
// port suffix in both the direct-build path AND the bare-prefix
// fallback. Pinned separately so a future refactor can't drop one.
func TestResolveSubdomainURL_BarePrefixPort80(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", SubdomainHost: "1df2"}).
		WithServiceEnv("svc-1", []platform.EnvVar{
			{Key: "zeropsSubdomain", Content: "https://api-1df2.prg1.zerops.app"},
		})

	svc := &platform.ServiceStack{
		ID: "svc-1", Name: "api",
		SubdomainAccess: true,
		Ports:           []platform.Port{{Port: 80}},
	}

	got := ResolveSubdomainURL(context.Background(), mock, "p1", svc)
	want := "https://api-1df2.prg1.zerops.app"
	if got != want {
		t.Errorf("got %q, want %q (bare-prefix port 80)", got, want)
	}
}

// TestResolveSubdomainURL_NoSubdomain — short-circuits to "" when
// SubdomainAccess is false, no API calls.
func TestResolveSubdomainURL_NoSubdomain(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	svc := &platform.ServiceStack{
		ID: "svc-1", Name: "api",
		SubdomainAccess: false,
		Ports:           []platform.Port{{Port: 3000}},
	}
	if got := ResolveSubdomainURL(context.Background(), mock, "p1", svc); got != "" {
		t.Errorf("got %q, want empty when SubdomainAccess=false", got)
	}
}

// TestResolveSubdomainURL_NoPorts — short-circuits to "" when the
// service exposes no ports. Subdomain URL needs a port.
func TestResolveSubdomainURL_NoPorts(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	svc := &platform.ServiceStack{
		ID: "svc-1", Name: "api",
		SubdomainAccess: true,
		Ports:           nil,
	}
	if got := ResolveSubdomainURL(context.Background(), mock, "p1", svc); got != "" {
		t.Errorf("got %q, want empty when no ports", got)
	}
}

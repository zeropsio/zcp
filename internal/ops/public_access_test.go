package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestObservePublicAccess_SubdomainState_Table pins the three-state subdomain
// read (§8 O3): svc.SubdomainAccess (REST-authoritative) ⇒ on; else a live
// stack.enableSubdomainAccess process referencing THIS service ⇒ enabling;
// else off — including the negative where the live enable process references
// a DIFFERENT service (must not leak enabling onto svc).
func TestObservePublicAccess_SubdomainState_Table(t *testing.T) {
	const projectID = "proj-1"
	svc := &platform.ServiceStack{ID: "svc-1", Name: "api"}

	tests := []struct {
		name  string
		svc   platform.ServiceStack
		procs []platform.Process
		want  topology.SubdomainState
	}{
		{
			name: "on",
			svc:  platform.ServiceStack{ID: "svc-1", Name: "api", SubdomainAccess: true},
			want: topology.SubdomainOn,
		},
		{
			name: "off_no_processes",
			svc:  *svc,
			want: topology.SubdomainOff,
		},
		{
			name: "enabling_live_process_this_service",
			svc:  *svc,
			procs: []platform.Process{{
				ID: "proc-1", ActionName: "stack.enableSubdomainAccess", Status: platform.ProcessStatusPending,
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-1", Name: "api"}},
				Created:       "2026-09-13T10:00:00Z",
			}},
			want: topology.SubdomainEnabling,
		},
		{
			name: "off_live_enable_process_other_service",
			svc:  *svc,
			procs: []platform.Process{{
				ID: "proc-1", ActionName: "stack.enableSubdomainAccess", Status: platform.ProcessStatusPending,
				ServiceStacks: []platform.ServiceStackRef{{ID: "svc-2", Name: "other"}},
				Created:       "2026-09-13T10:00:00Z",
			}},
			want: topology.SubdomainOff,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := platform.NewMock().WithProjectProcesses(tt.procs)
			svcCopy := tt.svc
			got, err := ObservePublicAccess(context.Background(), mock, projectID, &svcCopy)
			if err != nil {
				t.Fatalf("ObservePublicAccess: %v", err)
			}
			if got.Observed.Subdomain != tt.want {
				t.Errorf("Subdomain = %q, want %q", got.Observed.Subdomain, tt.want)
			}
		})
	}
}

// TestObservePublicAccess_Domains_FilteredByServiceID pins that only the
// routing whose location references svc.ID is returned — a routing for
// another service must not leak in. Observed.Domains carries just the
// de-duplicated names; Domains carries the per-route detail.
func TestObservePublicAccess_Domains_FilteredByServiceID(t *testing.T) {
	const projectID = "proj-1"
	svc := &platform.ServiceStack{ID: "svc-1", Name: "api"}

	mock := platform.NewMock().WithPublicHTTPRoutings(
		platform.PublicHTTPRouting{
			ID:         "routing-1",
			SSLEnabled: true,
			IsSynced:   false,
			Domains: []platform.PublicHTTPDomain{
				{Name: "probe.example.com", DNSCheckStatus: "PENDING", SSLStatus: "PENDING"},
			},
			Locations: []platform.PublicHTTPLocation{
				{Path: "/", Port: 3000, ServiceID: "svc-1"},
			},
		},
		platform.PublicHTTPRouting{
			ID: "routing-2",
			Domains: []platform.PublicHTTPDomain{
				{Name: "other.example.com"},
			},
			Locations: []platform.PublicHTTPLocation{
				{Path: "/", Port: 8080, ServiceID: "svc-2"},
			},
		},
	)

	got, err := ObservePublicAccess(context.Background(), mock, projectID, svc)
	if err != nil {
		t.Fatalf("ObservePublicAccess: %v", err)
	}

	if len(got.Observed.Domains) != 1 || got.Observed.Domains[0] != "probe.example.com" {
		t.Fatalf("Observed.Domains = %v, want [probe.example.com]", got.Observed.Domains)
	}
	if len(got.Domains) != 1 {
		t.Fatalf("Domains = %+v, want exactly one route", got.Domains)
	}
	route := got.Domains[0]
	if route.Name != "probe.example.com" || route.Port != 3000 || route.Path != "/" ||
		route.DNSCheckStatus != "PENDING" || !route.SSLEnabled {
		t.Errorf("route = %+v, unexpected", route)
	}
}

// TestObservePublicAccess_URLOnlyWhenOn pins that URL is populated only when
// Subdomain == on (via ResolveSubdomainURL, the same resolver used
// elsewhere), and "" otherwise — never guessed independently.
func TestObservePublicAccess_URLOnlyWhenOn(t *testing.T) {
	const projectID = "p1"
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: projectID, SubdomainHost: "1df2.prg1.zerops.app"})

	on := &platform.ServiceStack{
		ID: "svc-1", Name: "api",
		SubdomainAccess: true,
		Ports:           []platform.Port{{Port: 3000}},
	}
	got, err := ObservePublicAccess(context.Background(), mock, projectID, on)
	if err != nil {
		t.Fatalf("ObservePublicAccess: %v", err)
	}
	want := ResolveSubdomainURL(context.Background(), mock, projectID, on)
	if want == "" {
		t.Fatal("test setup: ResolveSubdomainURL returned empty for the ON fixture")
	}
	if got.URL != want {
		t.Errorf("URL = %q, want %q (ResolveSubdomainURL)", got.URL, want)
	}

	off := &platform.ServiceStack{
		ID: "svc-2", Name: "worker",
		SubdomainAccess: false,
		Ports:           []platform.Port{{Port: 3000}},
	}
	got, err = ObservePublicAccess(context.Background(), mock, projectID, off)
	if err != nil {
		t.Fatalf("ObservePublicAccess: %v", err)
	}
	if got.URL != "" {
		t.Errorf("URL = %q, want empty when off", got.URL)
	}
}

// TestObservePublicAccess_RoutingListError_Returned pins that a
// ListPublicHTTPRoutings failure is wrapped and returned, never swallowed —
// the caller decides how to degrade.
func TestObservePublicAccess_RoutingListError_Returned(t *testing.T) {
	wantErr := errors.New("routing list boom")
	mock := platform.NewMock().WithError("ListPublicHTTPRoutings", wantErr)
	svc := &platform.ServiceStack{ID: "svc-1", Name: "api"}

	_, err := ObservePublicAccess(context.Background(), mock, "p1", svc)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want wrapping %v", err, wantErr)
	}
}

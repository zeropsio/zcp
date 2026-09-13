// Tests for: internal/platform/public_routing.go — the project's
// custom-domain routing list (docs/spec-workflows.md §8 O3). The service
// DTO carries no domain field, so ListPublicHTTPRoutings is the only read
// that surfaces a service's public domain(s).
package platform

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListPublicHTTPRoutings_MapsListAndIgnoresTotalCount_ReturnsRoutings
// pins the wire shape against a live-verified literal (brief S1,
// 2026-09-13): a body with totalCount: 0 and one routing still yields one
// PublicHTTPRouting — TotalCount is unreliable and must never be trusted.
func TestListPublicHTTPRoutings_MapsListAndIgnoresTotalCount_ReturnsRoutings(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"list": [
				{
					"id": "routing-1",
					"sslEnabled": true,
					"isSynced": false,
					"domains": [
						{"domainName": "probe.example.com", "dnsCheckStatus": "PENDING", "sslStatus": "NONE"}
					],
					"locations": [
						{"path": "/", "port": 3000, "serviceStackId": "svc-1"}
					]
				}
			],
			"totalCount": 0
		}`))
	}))
	t.Cleanup(srv.Close)

	z, err := NewZeropsClient("fake-token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}

	routings, err := z.ListPublicHTTPRoutings(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("ListPublicHTTPRoutings: %v", err)
	}
	if len(routings) != 1 {
		t.Fatalf("expected 1 routing (totalCount must not be trusted), got %d", len(routings))
	}

	got := routings[0]
	if got.ID != "routing-1" {
		t.Errorf("ID = %q, want %q", got.ID, "routing-1")
	}
	if !got.SSLEnabled {
		t.Errorf("SSLEnabled = false, want true")
	}
	if got.IsSynced {
		t.Errorf("IsSynced = true, want false")
	}
	if len(got.Domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(got.Domains))
	}
	if got.Domains[0].Name != "probe.example.com" {
		t.Errorf("Domains[0].Name = %q, want %q", got.Domains[0].Name, "probe.example.com")
	}
	if got.Domains[0].DNSCheckStatus != "PENDING" {
		t.Errorf("Domains[0].DNSCheckStatus = %q, want %q", got.Domains[0].DNSCheckStatus, "PENDING")
	}
	if len(got.Locations) != 1 {
		t.Fatalf("expected 1 location, got %d", len(got.Locations))
	}
	if got.Locations[0].Path != "/" {
		t.Errorf("Locations[0].Path = %q, want %q", got.Locations[0].Path, "/")
	}
	if got.Locations[0].Port != 3000 {
		t.Errorf("Locations[0].Port = %d, want %d", got.Locations[0].Port, 3000)
	}
	if got.Locations[0].ServiceID != "svc-1" {
		t.Errorf("Locations[0].ServiceID = %q, want %q", got.Locations[0].ServiceID, "svc-1")
	}
}

// TestListPublicHTTPRoutings_APIError_WrapsPlatformError verifies a 4xx
// response maps through the same PlatformError conversion the sibling SDK
// methods use, with the op name in the wrap.
func TestListPublicHTTPRoutings_APIError_WrapsPlatformError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"projectNotFound","message":"project not found"}}`))
	}))
	t.Cleanup(srv.Close)

	z, err := NewZeropsClient("fake-token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}

	_, err = z.ListPublicHTTPRoutings(context.Background(), "missing-project")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pe *PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.APICode != "projectNotFound" {
		t.Errorf("APICode = %q, want %q", pe.APICode, "projectNotFound")
	}
	const wantOpFragment = "list public http routings"
	if got := err.Error(); !strings.Contains(got, wantOpFragment) {
		t.Errorf("expected op fragment %q in error, got %q", wantOpFragment, got)
	}
}

// TestMock_ListPublicHTTPRoutings_ReturnsConfigured is the mock contract:
// WithPublicHTTPRoutings seeds what ListPublicHTTPRoutings returns.
func TestMock_ListPublicHTTPRoutings_ReturnsConfigured(t *testing.T) {
	t.Parallel()

	want := []PublicHTTPRouting{
		{
			ID:         "routing-1",
			SSLEnabled: true,
			IsSynced:   false,
			Domains:    []PublicHTTPDomain{{Name: "probe.example.com", DNSCheckStatus: "PENDING", SSLStatus: "NONE"}},
			Locations:  []PublicHTTPLocation{{Path: "/", Port: 3000, ServiceID: "svc-1"}},
		},
	}
	m := NewMock().WithPublicHTTPRoutings(want...)

	got, err := m.ListPublicHTTPRoutings(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "routing-1" {
		t.Fatalf("ListPublicHTTPRoutings = %+v, want %+v", got, want)
	}
}

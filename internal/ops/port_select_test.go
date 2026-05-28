package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestPreferredHTTPPort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		ports  []platform.Port
		want   int
		wantOK bool
	}{
		{"empty", nil, 0, false},
		{"single port", []platform.Port{{Port: 3000}}, 3000, true},
		{"scheme http beats earlier tcp", []platform.Port{{Port: 1025, Scheme: "tcp"}, {Port: 8025, Scheme: "http"}}, 8025, true},
		{"scheme https chosen over db scheme", []platform.Port{{Port: 5432, Scheme: "postgresql"}, {Port: 443, Scheme: "https"}}, 443, true},
		{"httpSupport when no scheme", []platform.Port{{Port: 1025}, {Port: 8025, HTTPSupport: true}}, 8025, true},
		{"port 80 when no scheme/httpSupport", []platform.Port{{Port: 1025}, {Port: 80}}, 80, true},
		{"first when nothing matches", []platform.Port{{Port: 1025}, {Port: 9000}}, 1025, true},
		{"scheme beats httpSupport and 80", []platform.Port{{Port: 80}, {Port: 8025, Scheme: "http"}}, 8025, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := PreferredHTTPPort(tt.ports)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got.Port != tt.want {
				t.Errorf("port = %d, want %d", got.Port, tt.want)
			}
		})
	}
}

// TestResolveSubdomainURL_MultiPort_PicksHTTPScheme — the resolver picks the
// http-scheme port, not Ports[0]. This is the mailpit bug (SMTP 1025 sorts
// before HTTP 8025) fixed by Port.Scheme, which is populated at deploy time
// (verified live: scheme "http" on 8025, "tcp" on 1025, before subdomain enable).
func TestResolveSubdomainURL_MultiPort_PicksHTTPScheme(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", SubdomainHost: "1df2.prg1.zerops.app"})
	svc := &platform.ServiceStack{
		ID: "svc-1", Name: "mailpit",
		SubdomainAccess: true,
		Ports:           []platform.Port{{Port: 1025, Scheme: "tcp"}, {Port: 8025, Scheme: "http"}},
	}
	got := ResolveSubdomainURL(context.Background(), mock, "p1", svc)
	want := "https://mailpit-1df2-8025.prg1.zerops.app"
	if got != want {
		t.Errorf("got %q, want %q (must pick HTTP port 8025, not SMTP 1025)", got, want)
	}
}

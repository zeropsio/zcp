package ops

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
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

func TestOrderedHTTPCandidatePorts_HTTPSchemeFirstNeverDrops(t *testing.T) {
	t.Parallel()
	// mailpit shape, SMTP (tcp) declared first.
	ports := []platform.Port{{Port: 1025, Scheme: "tcp"}, {Port: 8025, Scheme: "http"}}
	got := OrderedHTTPCandidatePorts(ports)
	if len(got) != len(ports) {
		t.Fatalf("dropped a port: got %d want %d", len(got), len(ports))
	}
	if got[0].Port != 8025 || got[1].Port != 1025 {
		t.Errorf("order = [%d, %d], want [8025, 1025]", got[0].Port, got[1].Port)
	}
}

// TestResolveSubdomainURL_MultiPort_PicksHTTPScheme — the pure resolver picks
// the HTTP-scheme port, not Ports[0]. This is the mailpit bug (SMTP 1025 sorts
// before HTTP 8025) fixed without a probe.
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

// routingDoer answers 200 only for URLs containing okSubstr; anything else is a
// transport error (models a non-HTTP port like SMTP refusing the connection).
type routingDoer struct{ okSubstr string }

func (d routingDoer) Do(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.String(), d.okSubstr) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	return nil, fmt.Errorf("connection refused")
}

// TestResolveHTTPSubdomainURL_ProbesToAnsweringPort — even with NO scheme/
// httpSupport hints (the post-deploy window), the probe fallback finds the port
// that actually answers HTTP and skips the one that refuses.
func TestResolveHTTPSubdomainURL_ProbesToAnsweringPort(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", SubdomainHost: "1df2.prg1.zerops.app"})
	svc := &platform.ServiceStack{
		ID: "svc-1", Name: "mailpit",
		SubdomainAccess: true,
		Ports:           []platform.Port{{Port: 1025}, {Port: 8025}}, // no hints — force probe
	}
	got := ResolveHTTPSubdomainURL(context.Background(), mock, routingDoer{okSubstr: "-8025."}, "p1", svc)
	want := "https://mailpit-1df2-8025.prg1.zerops.app"
	if got != want {
		t.Errorf("got %q, want %q (probe must find the answering HTTP port)", got, want)
	}
}

// TestResolveHTTPSubdomainURL_NilDoerFallsBackToPreferred — without a doer it
// returns the preferred (scheme-picked) URL, never empty.
func TestResolveHTTPSubdomainURL_NilDoerFallsBackToPreferred(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", SubdomainHost: "1df2.prg1.zerops.app"})
	svc := &platform.ServiceStack{
		ID: "svc-1", Name: "mailpit",
		SubdomainAccess: true,
		Ports:           []platform.Port{{Port: 1025, Scheme: "tcp"}, {Port: 8025, Scheme: "http"}},
	}
	got := ResolveHTTPSubdomainURL(context.Background(), mock, nil, "p1", svc)
	want := "https://mailpit-1df2-8025.prg1.zerops.app"
	if got != want {
		t.Errorf("got %q, want %q (nil doer → preferred scheme port)", got, want)
	}
}

package ops

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// Decision: one owning projection resolves a selected HTTP port to exactly one valid URI; verify, discover and envelope readers consume it.
// Independent expectations from mono-krls-probe evidence/22-public-ports.json
// and evidence/66-public-address-list.json, not the URL builder under test.
func TestPublicAddress_TwoHTTPPorts_SelectsOneURI(t *testing.T) {
	t.Parallel()
	const first = "https://portprobe-31e1-5733.prg1.zerops.app"
	const second = "https://portprobe-31e1-5734.prg1.zerops.app"
	for order, content := range []string{first + "\n" + second, second + "\n" + first} {
		for _, selected := range []int{5733, 5734} {
			t.Run(fmt.Sprintf("port=%d/order=%d", selected, order), func(t *testing.T) {
				t.Parallel()
				other := 5733
				want := second
				if selected == 5733 {
					other = 5734
					want = first
				}
				svc := platform.ServiceStack{
					ID: "svc-1", Name: "portprobe", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
					SubdomainAccess:      true,
					Ports:                []platform.Port{{Port: selected, Scheme: "http", HTTPSupport: true}, {Port: other, Scheme: "http", HTTPSupport: true}},
				}
				mock := platform.NewMock().WithServices([]platform.ServiceStack{svc}).WithService(&svc).
					WithProject(&platform.Project{ID: "proj-1", SubdomainHost: "31e1"}).
					WithServiceEnv(svc.ID, []platform.ServiceEnvVar{{Key: "zeropsSubdomain", Content: content}})
				ctx := context.Background()
				t.Run("projection", func(t *testing.T) {
					got := subdomainURLForPort(ctx, mock, "31e1", &svc, selected, nil)
					if got != want {
						t.Errorf("ASSERTION: selected port %d address = %q, want %q", selected, got, want)
					}
					if u, err := url.ParseRequestURI(got); err != nil || u.Host == "" {
						t.Errorf("ASSERTION: selected address must be one valid URI: %q", got)
					}
				})
				t.Run("verify", func(t *testing.T) {
					doer := &recordingHTTPDoer{}
					result, err := Verify(ctx, mock, platform.NewMockLogFetcher(), doer, "proj-1", svc.Name)
					if err != nil {
						t.Fatal(err)
					}
					if !slices.Contains(doer.urls, want+"/") {
						t.Errorf("ASSERTION: verify requests = %q, want selected public URI %q", doer.urls, want+"/")
					}
					findCheck(t, result, "http_public", CheckPass)
				})
				for _, includeEnvs := range []bool{false, true} {
					t.Run(fmt.Sprintf("discover/envs=%t", includeEnvs), func(t *testing.T) {
						result, err := Discover(ctx, mock, "proj-1", svc.Name, includeEnvs, false, false)
						if err != nil {
							t.Fatal(err)
						}
						got := result.Services[0]
						if got.SubdomainURL != want {
							t.Errorf("ASSERTION: discover address = %q, want %q", got.SubdomainURL, want)
						}
						if got.PublicAccess == nil || got.PublicAccess.URL != want {
							t.Errorf("ASSERTION: discover public access = %+v, want URI %q", got.PublicAccess, want)
						}
					})
				}
				t.Run("cockpit", func(t *testing.T) {
					result, err := Discover(ctx, mock, "proj-1", "", false, false, false)
					if err != nil {
						t.Fatal(err)
					}
					FillSubdomainURLs(ctx, mock, result)
					if got := result.Services[0].SubdomainURL; got != want {
						t.Errorf("ASSERTION: cockpit address = %q, want %q", got, want)
					}
				})
				t.Run("subdomain-list", func(t *testing.T) {
					result := &SubdomainResult{Hostname: svc.Name}
					attachSubdomainUrlsToResult(ctx, mock, result, "proj-1", svc.ID)
					if !slices.Equal(result.SubdomainUrls, []string{want, map[int]string{5733: first, 5734: second}[other]}) {
						t.Errorf("ASSERTION: subdomain addresses = %q, want exact URI for each declared HTTP port", result.SubdomainUrls)
					}
				})
			})
		}
	}
}

func TestExtractSubdomainURL_AdvertisedAddresses_ProjectsSelectedPort(t *testing.T) {
	t.Parallel()
	const first = "https://portprobe-31e1-5733.prg1.zerops.app"
	const second = "https://portprobe-31e1-5734.prg1.zerops.app"
	for _, tc := range []struct {
		name     string
		hostname string
		ports    []platform.Port
		content  string
		want     string
	}{
		{"HTTP+TCP", "portprobe", []platform.Port{{Port: 1025, Scheme: "tcp"}, {Port: 5734, Scheme: "http"}}, first + "\n" + second, second},
		{"port80/numeric-prefix", "web", []platform.Port{{Port: 80, Scheme: "http"}, {Port: 5734, Scheme: "http"}}, "https://web-1234-5734.prg1.zerops.app\nhttps://web-1234.prg1.zerops.app", "https://web-1234.prg1.zerops.app"},
		{"valid-single", "portprobe", []platform.Port{{Port: 5733}}, first, first},
		{"empty", "portprobe", []platform.Port{{Port: 5733}}, "", ""},
		{"invalid-URI", "portprobe", []platform.Port{{Port: 5733}}, "https://portprobe-31e1-5733.prg1.zerops.app\x00", ""},
		{"valid-after-invalid", "portprobe", []platform.Port{{Port: 5733}}, "https://broken\x00\n" + first, first},
		{"unadvertised-port", "portprobe", []platform.Port{{Port: 5735}}, first + "\n" + second, ""},
	} {
		for _, cached := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/cached=%t", tc.name, cached), func(t *testing.T) {
				t.Parallel()
				envs := []platform.ServiceEnvVar{{Key: "zeropsSubdomain", Content: tc.content}}
				mock := platform.NewMock().WithServiceEnv("svc-1", envs)
				raw := envs
				if cached {
					mock.WithError("GetServiceEnv", fmt.Errorf("cached key must avoid fetch"))
				} else {
					// The cached list predates asynchronous platform injection; the direct
					// refetch now contains the advertised addresses for the selected port.
					raw = []platform.ServiceEnvVar{{Key: "OTHER_VAR", Content: "ignored"}}
				}
				svc := &platform.ServiceStack{ID: "svc-1", Name: tc.hostname, Ports: tc.ports}
				got := ExtractSubdomainURL(context.Background(), mock, svc, raw)
				if got != tc.want {
					t.Errorf("selected public URI = %q, want %q", got, tc.want)
				}
			})
		}
	}
}

package tools

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/workflow"
)

// orgClient is an HQ client of an identity that names orgID (or none, ""):
// the organisation travels on the client, never on the process.
func orgClient(t *testing.T, orgID string) hq.Client {
	t.Helper()
	client, err := hq.Open(http.DefaultClient, hq.Identity{Origin: "https://hq.example", Key: "k", ProjectID: "p", OrgID: orgID})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func orgPair(mode string) *workflow.ServiceMeta {
	return &workflow.ServiceMeta{Hostname: "appdev", HQ: &workflow.HQRepoRef{AppID: "a1", Repo: "appdev", Origin: &workflow.HQOrigin{Mode: mode, URL: "https://github.com/o/r"}}}
}

// A container whose organisation is unknown is told so wherever HQ's Git
// address is built; it is never read as nothing to move.
func TestHQRepositoryAddress_WithoutOrganisation_IsReportedNotSkipped(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		orgID   string
		mode    string
		want    string
		wantErr error
	}{
		{"kept in HQ, organisation known", "org-1", workflow.HQOriginNone, "https://hq.example/git/org-1/a1/appdev.git", nil},
		{"kept in HQ, organisation unknown", "", workflow.HQOriginNone, "", hq.ErrNoOrganisation},
		{"followed, organisation unknown", "", hq.OriginFollowed, "", hq.ErrNoOrganisation},
		{"kept on GitHub needs no organisation", "", hq.OriginOnGitHub, "https://github.com/o/r", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hqc := orgClient(t, tt.orgID)
			got, err := hqPairOriginTarget(hqc.Address(), hqc.OrgID(), orgPair(tt.mode))
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Fatalf("hqPairOriginTarget = %q, %v; want %q, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestHQPairLines_WithoutOrganisation_NameTheCause(t *testing.T) {
	t.Parallel()
	hqc := orgClient(t, "")
	m := orgPair(workflow.HQOriginNone)
	// No ssh deployer: a pass that reaches for the checkout has gone past the
	// address it could not build.
	for name, line := range map[string]string{
		"origin": hqPairOrigin(t.Context(), nil, hqc, m),
		"main":   hqPairMain(t.Context(), nil, hqc, m),
	} {
		if !strings.Contains(line, hq.ErrNoOrganisation.Error()) {
			t.Errorf("%s line = %q; want it to name %q", name, line, hq.ErrNoOrganisation)
		}
	}
}

package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zeropsio/zcp/internal/hq/hqtest"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// non-parallel: HOME locates the Mate's HQ enrollment (hq.EnrollmentPath).

const rebuildMateTier = `services:
  - hostname: appdev
    zeropsSetup: dev
    buildFromGit: HQ/git/app-1/appdev
  - hostname: appstage
    zeropsSetup: prod
    buildFromGit: HQ/git/app-1/appdev
  - hostname: api
    zeropsSetup: api
    buildFromGit: HQ/git/app-1/appdev
`

// rebuildMate is a Mate whose zcp was an older one for a while: appdev's meta
// carries only the stageHostname mirror, its checkout is on the mount, and
// HQ answers the Mate tier, counting the reads.
func rebuildMate(t *testing.T, zeropsYml string) (stateDir, mountRoot string, httpClient standupHTTP, tierReads *atomic.Int32) {
	t.Helper()
	tierReads = &atomic.Int32{}
	var tier string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mate/recipe/"+hq.RecipeTierMate || r.Header.Get("Authorization") != "Project "+standupKey {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		tierReads.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"state": hq.RecipePresent, "importYaml": tier, "mainHead": standupHead})
	}))
	t.Cleanup(srv.Close)
	tier = strings.ReplaceAll(rebuildMateTier, "HQ", srv.URL)
	t.Setenv("HOME", t.TempDir())
	hqtest.Link(t, srv.URL, "p1", standupKey)
	hqtest.GitCredential(t, standupCredential)
	stateDir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(stateDir, "services"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"hostname":"appdev","mode":"standard","stageHostname":"appstage","primarySetupName":"dev","stageSetupName":"prod","bootstrapSession":"s","bootstrappedAt":"t"}`
	if err := os.WriteFile(filepath.Join(stateDir, "services", "appdev.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	mountRoot = t.TempDir()
	if err := os.MkdirAll(filepath.Join(mountRoot, "appdev"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mountRoot, "appdev", "zerops.yml"), []byte(zeropsYml), 0o600); err != nil {
		t.Fatal(err)
	}
	return stateDir, mountRoot, standupHTTP{hq: srv.Client(), host: strings.TrimPrefix(srv.URL, "https://")}, tierReads
}

func TestRebuildMateStages_OldMetaOfMultiPartApp_StagesWrittenFromTier(t *testing.T) {
	mate := runtime.Info{InContainer: true, MateEnabled: true}
	tests := []struct {
		name          string
		yml           string
		rt            runtime.Info
		wantStages    []string
		wantTierReads int32
	}{
		{
			name:          "a two-part app's old meta records both stages, the tier read once",
			yml:           "zerops:\n  - setup: dev\n  - setup: prod\n  - setup: api\n",
			rt:            mate,
			wantStages:    []string{"appstage", "api"},
			wantTierReads: 1,
		},
		{
			name:          "a single-part app's old meta stays as it is and HQ is not asked",
			yml:           "zerops:\n  - setup: dev\n  - setup: prod\n",
			rt:            mate,
			wantStages:    []string{"appstage"},
			wantTierReads: 0,
		},
		{
			name:          "outside a Mate nothing is rebuilt",
			yml:           "zerops:\n  - setup: dev\n  - setup: prod\n  - setup: api\n",
			rt:            runtime.Info{InContainer: true},
			wantStages:    []string{"appstage"},
			wantTierReads: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir, mountRoot, httpClient, tierReads := rebuildMate(t, tt.yml)
			rebuildMateStages(t.Context(), httpClient, tt.rt, stateDir, mountRoot)
			meta, err := workflow.ReadServiceMeta(stateDir, "appdev")
			if err != nil {
				t.Fatal(err)
			}
			if got := meta.StageHostnames(); !slices.Equal(got, tt.wantStages) {
				t.Errorf("stages = %v, want %v", got, tt.wantStages)
			}
			if got := tierReads.Load(); got != tt.wantTierReads {
				t.Errorf("tier reads = %d, want %d", got, tt.wantTierReads)
			}
			// A second pass finds nothing left to rebuild.
			rebuildMateStages(t.Context(), httpClient, tt.rt, stateDir, mountRoot)
			if got := tierReads.Load(); got != tt.wantTierReads {
				t.Errorf("tier reads after a second pass = %d, want %d", got, tt.wantTierReads)
			}
		})
	}
}

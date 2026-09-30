// Tests for: tools/gitea_recipe_inputs.go — what the recipe reconcile reads
// of the Mate's live project: the group's name, the project's core package
// and variables, each pair's own variables, each managed service as it runs,
// and the runtimes built from a public repository.
package tools

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// medusaLikeProject is a Mate's live project: a wired pair, the data it uses,
// a mail catcher built from a public repository, a runtime nothing wired yet,
// and zcp itself.
func medusaLikeProject() *platform.Mock {
	explicit := false
	runtime := func(id, name, typ string) platform.ServiceStack {
		return platform.ServiceStack{ID: id, Name: name, Status: "ACTIVE", SubdomainAccess: true,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: typ, ServiceStackTypeCategoryName: "USER"},
			CurrentAutoscaling:   &platform.CustomAutoscaling{HorizontalMinCount: 1, HorizontalMaxCount: 1, CPUMode: "SHARED", MinRAM: 0.5}}
	}
	mailpit := runtime("svc-mailpit", "mailpit", "alpine@3.21")
	mailpit.ActiveAppVersion = &platform.ActiveAppVersionDigest{
		ID: "av-mailpit", Source: "GIT", Built: true,
		PublicGitSource:            &platform.AppVersionGitSource{GitURL: "https://github.com/zerops-recipe-apps/mailpit-app", BranchName: "main"},
		PublicGitSourceExplicitSet: &explicit,
	}
	db := platform.ServiceStack{ID: "svc-db", Name: "db", Status: "ACTIVE", Profile: "oltp-hobby",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@17", ServiceStackTypeCategoryName: "USER"},
		CurrentAutoscaling:   &platform.CustomAutoscaling{CPUMode: "SHARED", MinRAM: 0.25, MaxRAM: 4}}
	storage := platform.ServiceStack{ID: "svc-storage", Name: "storage", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "object-storage", ServiceStackTypeCategoryName: "USER"}}
	search := platform.ServiceStack{ID: "svc-search", Name: "search", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "meilisearch@1.20", ServiceStackTypeCategoryName: "USER"}}
	services := []platform.ServiceStack{
		runtime("svc-appdev", "appdev", "nodejs@22"),
		runtime("svc-appstage", "appstage", "nodejs@22"),
		runtime("svc-orphan", "orphan", "nodejs@22"),
		runtime("svc-zcp", "zcp", "zcp@1"),
		mailpit, db, storage, search,
	}
	return platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "acme-mate-1", Status: "ACTIVE", Mode: "SERIOUS"}).
		WithServicesDirect(services).
		WithServices(services).
		WithProjectEnv([]platform.ProjectEnvVar{
			{Key: "API_URL", Content: "https://appstage-${zeropsSubdomainHost}-3000.prg1.zerops.app", Type: platform.ProjectEnvUser},
			{Key: "JWT_SECRET", Content: "live-jwt-value-0123456789", Type: platform.ProjectEnvUser, Sensitive: true},
			{Key: "envIsolation", Content: "none", Type: platform.ProjectEnvSystem},
			{Key: "ZCP_API_KEY", Content: "live-zcp-key", Type: platform.ProjectEnvUser},
			{Key: "GITEA_TOKEN", Content: "live-gitea-token", Type: platform.ProjectEnvUser, Sensitive: true},
		}).
		WithServiceEnv("svc-appdev", []platform.ServiceEnvVar{
			{Key: "NODE_ENV", Content: "development", Type: platform.ServiceEnvUser},
			{Key: "hostname", Content: "appdev", Type: platform.ServiceEnvSystem},
		}).
		WithServiceEnv("svc-appstage", []platform.ServiceEnvVar{
			{Key: "NODE_ENV", Content: "production", Type: platform.ServiceEnvUser},
			{Key: "GIT_TOKEN", Content: "live-git-token", Type: platform.ServiceEnvUser, Sensitive: true},
			{Key: "STRIPE_API_KEY", Content: "REDACTED", Type: platform.ServiceEnvUser, Sensitive: true},
		}).
		WithServiceEnv("svc-mailpit", []platform.ServiceEnvVar{{Key: "MP_UI_AUTH", Content: "admin:pw", Type: platform.ServiceEnvUser}}).
		WithServiceEnv("svc-storage", []platform.ServiceEnvVar{
			{Key: "quotaGBytes", Content: "5", Type: platform.ServiceEnvSystem},
			{Key: "bucketName", Content: "acme-bucket-1x2y", Type: platform.ServiceEnvSystem},
		}).
		WithServiceExportYAML("services:\n  - hostname: storage\n    type: object-storage\n    objectStorageSize: 5\n    objectStoragePolicy: public-read\n")
}

func TestComposeGroupRecipeInputs_ReadsTheLiveProject(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaWiredPairMeta(t, stateDir)
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		t.Fatalf("ListServiceMetas: %v", err)
	}
	inputs, warnings, err := composeGroupRecipeInputs(context.Background(), medusaLikeProject(), "p1", "acme", t.TempDir(), metas, metas)
	if err != nil {
		t.Fatalf("composeGroupRecipeInputs: %v", err)
	}
	if inputs.Name != "acme" || inputs.MateProjectName != "acme-mate-1" {
		t.Errorf("Name = %q, MateProjectName = %q; want the group's slug and the Mate's own name", inputs.Name, inputs.MateProjectName)
	}
	if inputs.CorePackage != "SERIOUS" {
		t.Errorf("CorePackage = %q, want the project's", inputs.CorePackage)
	}
	gotProject := envMap(inputs.ProjectEnvs)
	wantProject := map[string]string{
		"API_URL":    "https://appstage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		"JWT_SECRET": "live-jwt-value-0123456789 (sensitive)",
	}
	if !maps.Equal(gotProject, wantProject) {
		t.Errorf("project variables = %v, want %v — the platform's own and the control plane's left out", gotProject, wantProject)
	}

	if len(inputs.Runtimes) != 1 {
		t.Fatalf("runtimes = %d, want the one wired pair", len(inputs.Runtimes))
	}
	pair := inputs.Runtimes[0]
	if got := envMap(pair.ServiceEnvs); !maps.Equal(got, map[string]string{"NODE_ENV": "development"}) {
		t.Errorf("dev half's variables = %v", got)
	}
	if got := envMap(pair.StageServiceEnvs); !maps.Equal(got, map[string]string{"NODE_ENV": "production", "STRIPE_API_KEY": "REDACTED (sensitive)"}) {
		t.Errorf("stage half's variables = %v — GIT_TOKEN must stay out", got)
	}
	if pair.Scaling == nil || pair.Scaling.MinRAM != 0.5 {
		t.Errorf("pair scaling = %+v, want the live shape", pair.Scaling)
	}

	managed := map[string]bundle.ManagedServiceEntry{}
	for _, m := range inputs.ManagedServices {
		managed[m.Hostname] = m
	}
	if db := managed["db"]; db.Profile != "oltp-hobby" || db.Scaling == nil || db.Scaling.MaxRAM != 4 {
		t.Errorf("db = %+v, want its profile and its scale", db)
	}
	if st := managed["storage"]; st.QuotaGBytes != 5 || st.ObjectStoragePolicy != "public-read" {
		t.Errorf("storage = %+v, want 5 GB and public-read", st)
	}
	if !slices.Equal(inputs.HAIncapable, []string{"search"}) {
		t.Errorf("HAIncapable = %v, want [search]", inputs.HAIncapable)
	}

	if len(inputs.Utilities) != 1 {
		t.Fatalf("utilities = %+v, want mailpit alone", inputs.Utilities)
	}
	mailpit := inputs.Utilities[0]
	if mailpit.Hostname != "mailpit" || mailpit.BuildFromGit != "https://github.com/zerops-recipe-apps/mailpit-app" ||
		mailpit.ServiceType != "alpine@3.21" || !mailpit.SubdomainEnabled || mailpit.SetupName != "" || mailpit.Scaling == nil {
		t.Errorf("mailpit = %+v", mailpit)
	}
	if !warningsHave(warnings, `"orphan"`) {
		t.Errorf("warnings %v do not say the runtime nothing wired is left out", warnings)
	}
	if warningsHave(warnings, `"zcp"`) {
		t.Errorf("warnings %v talk about zcp itself", warnings)
	}
}

// A read that decides what a tier carries fails the pass instead of
// composing without it: a tier on the group repo stays there, so a tier
// composed from a failed read would carry the gap for good. A read that only
// refines is a warning.
func TestComposeGroupRecipeInputs_ReadFailures(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		wantErr     string
		wantWarning string
	}{
		{name: "the project's variables", method: "GetProjectEnv", wantErr: "project's variables"},
		{name: "a pair's own variables", method: "GetServiceEnv", wantErr: "variables"},
		{name: "the object storage's policy", method: "GetServiceStackExport", wantWarning: "access policy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()
			writeGiteaWiredPairMeta(t, stateDir)
			metas, _ := workflow.ListServiceMetas(stateDir)
			client := medusaLikeProject().WithError(tt.method, errors.New("upstream timeout"))
			_, warnings, err := composeGroupRecipeInputs(context.Background(), client, "p1", "acme", t.TempDir(), metas, metas)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one naming %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("composeGroupRecipeInputs: %v", err)
			}
			if !warningsHave(warnings, tt.wantWarning) {
				t.Errorf("warnings %v are missing %q", warnings, tt.wantWarning)
			}
		})
	}
}

// envMap reads composer variables as KEY → value, a sensitive one marked.
func envMap(envs []bundle.ProjectEnvVar) map[string]string {
	out := map[string]string{}
	for _, env := range envs {
		value := env.Value
		if env.Sensitive {
			value += " (sensitive)"
		}
		out[env.Key] = value
	}
	return out
}

func warningsHave(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

// Through the reconcile itself: the proposal it writes names the group's
// environments after the group, carries the project's variables with every
// secret generated, and brings the public-build mail catcher along.
func TestReconcileGiteaGroupRecipe_ProposesTheLiveProject(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaWiredPairMeta(t, stateDir)
	fake := newFakeGroupGitea()
	srv := fake.start(t)
	env := map[string]string{"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken}

	outcome := giteaGroupRecipeOutcome(context.Background(), medusaLikeProject(), srv.Client(),
		runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir, writeLiveEnvFile(t, env))
	if outcome.PullNumber == 0 {
		t.Fatalf("nothing proposed: %+v", outcome)
	}
	_, files := fake.proposal(t)
	stage := files["3 — Stage/import.yaml"]
	for _, want := range []string{
		"name: acme stage",
		"API_URL: https://app-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		"JWT_SECRET: <@generateRandomString(<25>)>",
		"hostname: mailpit",
		"objectStoragePolicy: public-read",
		"corePackage: SERIOUS",
	} {
		if !strings.Contains(stage, want) {
			t.Errorf("the Stage tier is missing %q:\n%s", want, stage)
		}
	}
	for path, body := range files {
		for _, secret := range []string{"live-jwt-value-0123456789", "live-zcp-key", "live-gitea-token", "live-git-token", "admin:pw"} {
			if strings.Contains(body, secret) {
				t.Errorf("%s carries %q", path, secret)
			}
		}
	}
	if !warningsHave(outcome.Warnings, `"orphan"`) {
		t.Errorf("the outcome's warnings %v do not name the runtime left out", outcome.Warnings)
	}
}

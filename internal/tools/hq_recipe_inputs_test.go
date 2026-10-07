// Tests for: tools/hq_recipe_inputs.go — what the recipe reconcile reads
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

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// liveRuntime is a running runtime service of a Mate's project.
func liveRuntime(id, name, typ string) platform.ServiceStack {
	return platform.ServiceStack{ID: id, Name: name, Status: "ACTIVE", SubdomainAccess: true,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: typ, ServiceStackTypeCategoryName: "USER"},
		CurrentAutoscaling:   &platform.CustomAutoscaling{HorizontalMinCount: 1, HorizontalMaxCount: 1, CPUMode: "SHARED", MinRAM: 0.5}}
}

// publicBuildRuntime is a runtime whose active version an import built from a
// public repository.
func publicBuildRuntime(id, name, typ, gitURL string) platform.ServiceStack {
	explicit := false
	svc := liveRuntime(id, name, typ)
	svc.ActiveAppVersion = &platform.ActiveAppVersionDigest{
		ID: "av-" + name, Source: "GIT", Built: true,
		PublicGitSource:            &platform.AppVersionGitSource{GitURL: gitURL, BranchName: "main"},
		PublicGitSourceExplicitSet: &explicit,
	}
	return svc
}

// medusaLikeProject is a Mate's live project: a wired pair, the data it uses,
// a mail catcher built from a public repository, a runtime nothing wired yet,
// and zcp itself — and whatever else a test runs beside them.
func medusaLikeProject(extra ...platform.ServiceStack) *platform.Mock {
	runtime := liveRuntime
	mailpit := publicBuildRuntime("svc-mailpit", "mailpit", "alpine@3.21", "https://github.com/zerops-recipe-apps/mailpit-app")
	db := platform.ServiceStack{ID: "svc-db", Name: "db", Status: "ACTIVE", Profile: "oltp-hobby",
		ProfileOverrides:     map[string]any{"max_connections": "200"},
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@17", ServiceStackTypeCategoryName: "USER"},
		CurrentAutoscaling:   &platform.CustomAutoscaling{CPUMode: "SHARED", MinRAM: 0.25, MaxRAM: 4}}
	storage := platform.ServiceStack{ID: "svc-storage", Name: "storage", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "object-storage", ServiceStackTypeCategoryName: "USER"}}
	search := platform.ServiceStack{ID: "svc-search", Name: "search", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "meilisearch@1.20", ServiceStackTypeCategoryName: "USER"}}
	services := append([]platform.ServiceStack{
		runtime("svc-appdev", "appdev", "nodejs@22"),
		runtime("svc-appstage", "appstage", "nodejs@22"),
		runtime("svc-orphan", "orphan", "nodejs@22"),
		runtime("svc-zcp", "zcp", "zcp@1"),
		mailpit, db, storage, search,
	}, extra...)
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
	writeHQWiredPairMeta(t, stateDir)
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		t.Fatalf("ListServiceMetas: %v", err)
	}
	inputs, warnings, err := composeGroupRecipeInputs(context.Background(), medusaLikeProject(), "p1", "acme", t.TempDir(), testHQAddress, labApp, metas, metas)
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
	if db := managed["db"]; db.Profile != "oltp-hobby" || db.Scaling == nil || db.Scaling.MaxRAM != 4 ||
		db.ProfileOverrides["max_connections"] != "200" {
		t.Errorf("db = %+v, want its profile, its overrides and its scale", db)
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
			writeHQWiredPairMeta(t, stateDir)
			metas, _ := workflow.ListServiceMetas(stateDir)
			client := medusaLikeProject().WithError(tt.method, errors.New("upstream timeout"))
			_, warnings, err := composeGroupRecipeInputs(context.Background(), client, "p1", "acme", t.TempDir(), testHQAddress, labApp, metas, metas)
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

// testHQAddress is the Mate's HQ in these tests: the wired pair's
// repository is there.
const testHQAddress = "https://hq.example"

// writeHQWiredPairMeta seeds a pair the repository pass has already given its
// repository in HQ, both halves deployed: the state the recipe starts from.
func writeHQWiredPairMeta(t *testing.T, stateDir string) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-09-16",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        testHQAddress + "/git/" + labApp + "/appdev.git",
		PrimarySetupName: "api",
		// Both halves deployed: the group tiers build the stage half's setup,
		// and a stage setup nothing records is withheld rather than guessed.
		StageSetupName: "prod",
		HQ:             &workflow.HQRepoRef{AppID: labApp, Repo: "appdev", Branch: "mate/" + labMate},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// The recipe waits only for what a later pass brings: a finished pair the
// repository pass will still wire, or a dev/stage pair zcp has not adopted.
// Written without either, a tier on main would stay without it; written as
// two utilities, a pair's halves landed in Small Production with no setup.
// What no pass will ever wire must not hold the group's first recipe back
// for good: a pair pushing to its own repository, or one whose bootstrap
// never finished, is left out and said. A runtime is a utility only when it
// is standalone — no pair records it, and no dev/stage sibling runs beside
// it: `app` beside a wired appdev/appstage is one, and so are `back` and
// `backstage`, which are no pair.
func TestComposeGroupRecipeInputs_WaitsOnlyForWhatALaterPassBrings(t *testing.T) {
	public := func(name string) platform.ServiceStack {
		return publicBuildRuntime("svc-"+name, name, "nodejs@22", "https://github.com/zerops-recipe-apps/"+name)
	}
	workers := []platform.ServiceStack{public("workerdev"), public("workerstage")}
	workerPair := func(bootstrapped, remote string) *workflow.ServiceMeta {
		m := &workflow.ServiceMeta{Hostname: "workerdev", StageHostname: "workerstage", Mode: topology.PlanModeStandard,
			BootstrapSession: "test", BootstrappedAt: bootstrapped, RemoteURL: remote}
		if remote != "" {
			m.GitPushState = topology.GitPushConfigured
		}
		return m
	}
	tests := []struct {
		name  string
		meta  *workflow.ServiceMeta
		extra []platform.ServiceStack
		// wantWait are what the warnings and the error name while the recipe
		// waits; none when it composes.
		wantWait []string
		// wantLeftOut is what a warning must say when it composes.
		wantLeftOut []string
		// wantUtilities are the utilities it composes with.
		wantUtilities []string
	}{
		{name: "a finished pair the repository pass will wire", meta: workerPair("2026-09-30", ""), extra: workers,
			wantWait: []string{`"workerdev"`, "repository pass"}},
		{name: "a pair wired in another application", meta: func() *workflow.ServiceMeta {
			m := workerPair("2026-09-30", testHQAddress+"/git/app-0/workerdev.git")
			m.HQ = &workflow.HQRepoRef{AppID: "app-0", Repo: "workerdev", Branch: "mate/" + labMate}
			return m
		}(), extra: workers, wantWait: []string{`"workerdev"`, "another application"}},
		{name: "a pair named after the recipe repository is left out", meta: &workflow.ServiceMeta{Hostname: "group", StageHostname: "groupstage",
			Mode: topology.PlanModeStandard, BootstrapSession: "test", BootstrappedAt: "2026-09-30"},
			extra:       []platform.ServiceStack{public("group"), public("groupstage")},
			wantLeftOut: []string{`"group"`, "Rename the service"}, wantUtilities: []string{"mailpit"}},
		{name: "a dev/stage pair zcp has not adopted", extra: workers,
			wantWait: []string{`"workerdev"`, `"workerstage"`, "adopt"}},
		{name: "a pair pushing to its own repository is left out", meta: workerPair("2026-09-30", "https://github.com/acme/worker.git"), extra: workers,
			wantLeftOut: []string{`"workerdev"`, "its own"}, wantUtilities: []string{"mailpit"}},
		{name: "a pair whose bootstrap never finished is left out", meta: workerPair("", ""), extra: workers,
			wantLeftOut: []string{`"workerdev"`, "bootstrap"}, wantUtilities: []string{"mailpit"}},
		{name: "a runtime named like a wired pair's group runtime is standalone", extra: []platform.ServiceStack{public("app")},
			wantUtilities: []string{"app", "mailpit"}},
		{name: "back and backstage are no pair", extra: []platform.ServiceStack{public("back"), public("backstage")},
			wantUtilities: []string{"back", "backstage", "mailpit"}},
		{name: "a pair whose services are gone holds nothing up", meta: workerPair("2026-09-30", ""),
			wantUtilities: []string{"mailpit"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()
			writeHQWiredPairMeta(t, stateDir)
			wired, err := workflow.ListServiceMetas(stateDir)
			if err != nil {
				t.Fatalf("ListServiceMetas: %v", err)
			}
			metas := wired
			if tt.meta != nil {
				metas = append(slices.Clone(wired), tt.meta)
			}
			inputs, warnings, err := composeGroupRecipeInputs(context.Background(), medusaLikeProject(tt.extra...), "p1", "acme", t.TempDir(), testHQAddress, labApp, metas, wired)
			if len(tt.wantWait) > 0 {
				if err == nil {
					t.Fatalf("composed while a pair waits: utilities %+v", inputs.Utilities)
				}
				if len(inputs.Utilities) > 0 || len(inputs.Runtimes) > 0 {
					t.Errorf("inputs = %+v, want nothing composed", inputs)
				}
				for _, want := range tt.wantWait {
					if !warningsHave(warnings, want) {
						t.Errorf("warnings %v do not say %s", warnings, want)
					}
				}
				if !strings.Contains(err.Error(), "workerdev") {
					t.Errorf("error %q does not name the pair", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("composeGroupRecipeInputs: %v", err)
			}
			var utilities []string
			for _, u := range inputs.Utilities {
				utilities = append(utilities, u.Hostname)
			}
			slices.Sort(utilities)
			if !slices.Equal(utilities, tt.wantUtilities) {
				t.Errorf("utilities = %v, want %v", utilities, tt.wantUtilities)
			}
			for _, want := range tt.wantLeftOut {
				if !warningsHave(warnings, want) {
					t.Errorf("warnings %v do not say %s", warnings, want)
				}
			}
			if warningsHave(warnings, "waits") {
				t.Errorf("warnings %v say the recipe waits, yet it composed", warnings)
			}
		})
	}
}

// Through the reconcile: while a pair waits for its repository nothing is
// proposed, the outcome's warnings say which pair, and its line says the
// recipe is proposed on a later pass.
func TestGroupRecipe_WaitsForAnUnwiredPair(t *testing.T) {
	lab := newHQLab(t)
	lab.mock = medusaLikeProject(
		publicBuildRuntime("svc-workerdev", "workerdev", "nodejs@22", "https://github.com/zerops-recipe-apps/medusa-worker"),
		publicBuildRuntime("svc-workerstage", "workerstage", "nodejs@22", "https://github.com/zerops-recipe-apps/medusa-worker"),
	)
	lab.ssh.mock = lab.mock
	lab.wire()
	if err := workflow.WriteServiceMeta(lab.stateDir, &workflow.ServiceMeta{Hostname: "workerdev", StageHostname: "workerstage",
		Mode: topology.PlanModeStandard, BootstrapSession: "test", BootstrappedAt: "2026-09-30"}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	outcome := lab.proposeRecipe(lab.mock)

	if outcome.Change != 0 || lab.hq.changeIn(hq.RecipeRepo, 1) != nil {
		t.Errorf("outcome %+v: want nothing proposed while a pair waits", outcome)
	}
	if !warningsHave(outcome.Warnings, `"workerdev"`) {
		t.Errorf("warnings %v do not name the pair the recipe waits for", outcome.Warnings)
	}
	if !strings.Contains(outcome.Line, "not proposed yet") || !strings.Contains(outcome.Line, "workerdev") {
		t.Errorf("line %q does not say the recipe waits for workerdev", outcome.Line)
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
// environments after the application, carries the project's variables with
// every secret generated, and brings the public-build mail catcher along —
// and no tier carries a credential, the Mate's HQ credential on the dev
// half's GIT_TOKEN least of all.
func TestGroupRecipe_ProposesTheLiveProject(t *testing.T) {
	lab := newHQLab(t)
	lab.mock = medusaLikeProject()
	lab.ssh.mock = lab.mock
	lab.wire()
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.PrimarySetupName, m.StageSetupName = "api", "prod"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	outcome := lab.proposeRecipe(lab.mock)
	if outcome.Change == 0 {
		t.Fatalf("nothing proposed: %+v", outcome)
	}
	files := lab.hq.recipeFiles(recipeBranch(outcome.Change))
	stage := files["3 — Stage/import.yaml"]
	for _, want := range []string{
		"name: " + labApp + " stage",
		"API_URL: https://app-${zeropsSubdomainHost}-3000.prg1.zerops.app",
		"JWT_SECRET: {value: <@generateRandomString(<25>)>, sensitive: true}",
		"hostname: mailpit",
		"objectStoragePolicy: public-read",
		"corePackage: SERIOUS",
	} {
		if !strings.Contains(stage, want) {
			t.Errorf("the Stage tier is missing %q:\n%s", want, stage)
		}
	}
	for path, body := range files {
		for _, secret := range []string{"live-jwt-value-0123456789", "live-zcp-key", "live-gitea-token", "live-git-token", "admin:pw", labCredential} {
			if strings.Contains(body, secret) {
				t.Errorf("%s carries %q", path, secret)
			}
		}
	}
	if !warningsHave(outcome.Warnings, `"orphan"`) {
		t.Errorf("the outcome's warnings %v do not name the runtime left out", outcome.Warnings)
	}
}

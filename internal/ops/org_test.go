// Tests for: internal/ops/org.go — org mode's look operations
// (docs/spec-mate.md §2.11).
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// fakeOrg is an organization held in memory, read the way OrgReader reads.
type fakeOrg struct {
	orgID       string
	projects    []platform.Project
	services    []platform.ServiceStack
	projectEnv  map[string][]platform.ProjectEnvVar
	serviceEnv  map[string][]platform.ServiceEnvVar
	routings    map[string][]platform.PublicHTTPRouting
	processes   []platform.ProcessEvent
	appVersions []platform.AppVersionEvent
}

func (f *fakeOrg) OrgID() string { return f.orgID }
func (f *fakeOrg) ListProjects(context.Context) ([]platform.Project, error) {
	return f.projects, nil
}
func (f *fakeOrg) ListServices(_ context.Context, projectID string) ([]platform.ServiceStack, error) {
	var out []platform.ServiceStack
	for _, s := range f.services {
		if projectID == "" || s.ProjectID == projectID {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f *fakeOrg) GetProjectEnv(_ context.Context, projectID string) ([]platform.ProjectEnvVar, error) {
	return f.projectEnv[projectID], nil
}
func (f *fakeOrg) GetServiceEnv(_ context.Context, serviceID string) ([]platform.ServiceEnvVar, error) {
	return f.serviceEnv[serviceID], nil
}
func (f *fakeOrg) GetProjectLog(_ context.Context, projectID string) (*platform.LogAccess, error) {
	return &platform.LogAccess{URL: "https://logs.example/" + projectID}, nil
}
func (f *fakeOrg) ListPublicHTTPRoutings(_ context.Context, projectID string) ([]platform.PublicHTTPRouting, error) {
	return f.routings[projectID], nil
}
func (f *fakeOrg) SearchProcesses(_ context.Context, projectID string, limit int) ([]platform.ProcessEvent, error) {
	var out []platform.ProcessEvent
	for _, p := range f.processes {
		if (projectID == "" || p.ProjectID == projectID) && len(out) < limit {
			out = append(out, p)
		}
	}
	return out, nil
}
func (f *fakeOrg) SearchAppVersions(_ context.Context, projectID string, limit int) ([]platform.AppVersionEvent, error) {
	var out []platform.AppVersionEvent
	for _, a := range f.appVersions {
		if (projectID == "" || a.ProjectID == projectID) && len(out) < limit {
			out = append(out, a)
		}
	}
	return out, nil
}

// fakeLogs records what was asked of the log backend.
type fakeLogs struct {
	asked   []platform.LogFetchParams
	entries []platform.LogEntry
}

func (f *fakeLogs) FetchLogs(_ context.Context, _ *platform.LogAccess, p platform.LogFetchParams) ([]platform.LogEntry, error) {
	f.asked = append(f.asked, p)
	return f.entries, nil
}

func svc(id, project, name, typ, category, status string) platform.ServiceStack {
	return platform.ServiceStack{
		ID: id, ProjectID: project, Name: name, Status: status,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: typ, ServiceStackTypeCategoryName: category},
	}
}

// testOrg is a small organization with one of every kind of project.
func testOrg() *fakeOrg {
	return &fakeOrg{
		orgID: "org1",
		projects: []platform.Project{
			{ID: "pMM", Name: "Chief - Chief", Status: "ACTIVE", Tags: []string{"mate", "chief"}},
			{ID: "pRossy", Name: "Northwind Shop - Rossy", Status: "ACTIVE", Tags: []string{"mate"}},
			{ID: "pBorn", Name: "Contoso - Unborn", Status: "ACTIVE", Tags: []string{"mate"}},
			{ID: "pHQ", Name: "Headquarters", Status: "ACTIVE"},
			{ID: "pProd", Name: "Contoso - production", Status: "ACTIVE"},
			{ID: "pStage", Name: "ZIT - stage", Status: "ACTIVE"},
			{ID: "pTool", Name: "central-prometheus", Status: "ACTIVE", Tags: []string{"prometheus"}},
		},
		services: []platform.ServiceStack{
			svc("sMM", "pMM", "zcp", "zcp@1", "USER", "ACTIVE"),
			svc("sRossy", "pRossy", "zcp", "zcp@1", "USER", "ACTIVE"),
			svc("sRossyApp", "pRossy", "appdev", "nodejs@22", "USER", "ACTIVE"),
			svc("sHQ", "pHQ", "hq", "ubuntu/nodejs@24", "USER", "ACTIVE"),
			svc("sRunner", "pHQ", "runnerzit", "ubuntu@26.04", "USER", "STOPPED"),
			svc("sMedusa", "pProd", "medusa", "nodejs@22", "USER", "ACTIVE"),
			svc("sDB", "pProd", "db", "postgresql@16", "STANDARD", "FAILED"),
			svc("sBuild", "pProd", "buildmedusav1", "nodejs@22", "BUILD", "STOPPED"),
			svc("sCore", "pProd", "core", "core@1", "CORE", "ACTIVE"),
			svc("sStage", "pStage", "app", "go@1", "USER", "ACTIVE"),
		},
	}
}

func TestClassifyOrgProject(t *testing.T) {
	t.Parallel()
	org := testOrg()
	tests := []struct {
		project  string
		wantKind OrgProjectKind
		wantApp  string
		wantMate string
	}{
		{project: "pMM", wantKind: OrgKindChief},
		{project: "pRossy", wantKind: OrgKindMate, wantApp: "Northwind Shop", wantMate: "Rossy"},
		// Tagged mate but no zcp service: not a Mate org mode can talk to.
		{project: "pBorn", wantKind: OrgKindOther},
		{project: "pHQ", wantKind: OrgKindHQ},
		{project: "pProd", wantKind: OrgKindProduction, wantApp: "Contoso"},
		{project: "pStage", wantKind: OrgKindStage, wantApp: "ZIT"},
		{project: "pTool", wantKind: OrgKindOther},
	}
	for _, tt := range tests {
		t.Run(tt.project, func(t *testing.T) {
			t.Parallel()
			var project platform.Project
			for _, p := range org.projects {
				if p.ID == tt.project {
					project = p
				}
			}
			services, _ := org.ListServices(context.Background(), tt.project)
			got := ClassifyOrgProject(project, services)
			if got.Kind != tt.wantKind || got.App != tt.wantApp || got.MateName != tt.wantMate {
				t.Errorf("ClassifyOrgProject(%s) = %+v, want kind=%s app=%q mate=%q", tt.project, got, tt.wantKind, tt.wantApp, tt.wantMate)
			}
		})
	}
}

func TestOrgOverview_GroupsEveryProjectByKind(t *testing.T) {
	t.Parallel()
	got, err := OrgOverview(context.Background(), testOrg())
	if err != nil {
		t.Fatal(err)
	}
	if got.Org != "org1" || got.Projects != 7 {
		t.Fatalf("org=%q projects=%d", got.Org, got.Projects)
	}
	count := map[OrgProjectKind]int{}
	for _, g := range got.Groups {
		count[g.Kind] = len(g.Projects)
	}
	want := map[OrgProjectKind]int{OrgKindChief: 1, OrgKindMate: 1, OrgKindHQ: 1, OrgKindProduction: 1, OrgKindStage: 1, OrgKindOther: 2}
	for k, n := range want {
		if count[k] != n {
			t.Errorf("group %s has %d projects, want %d", k, count[k], n)
		}
	}
	var prod OrgProjectSummary
	for _, g := range got.Groups {
		if g.Kind == OrgKindProduction {
			prod = g.Projects[0]
		}
	}
	// A failing database is named on its project; the spent build container
	// and the core service are not.
	if len(prod.NotRunning) != 1 || prod.NotRunning[0] != "db FAILED" {
		t.Errorf("production notRunning = %v, want [db FAILED]", prod.NotRunning)
	}
	if prod.Services != 2 {
		t.Errorf("production services = %d, want 2 (system services left out)", prod.Services)
	}
}

func TestResolveOrgProject(t *testing.T) {
	t.Parallel()
	org := testOrg()
	org.projects = append(org.projects, platform.Project{ID: "pHQ2", Name: "Headquarters"})
	tests := []struct {
		name    string
		ref     string
		want    string
		wantErr string
	}{
		{name: "by id", ref: "pProd", want: "pProd"},
		{name: "by full name, any case", ref: "contoso - production", want: "pProd"},
		{name: "by a Mate's name", ref: "rossy", want: "pRossy"},
		{name: "a project outside the org", ref: "pElsewhere", wantErr: "no project"},
		{name: "a name two projects share", ref: "Headquarters", wantErr: "pHQ2"},
		{name: "nothing", ref: " ", wantErr: "project is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveOrgProject(context.Background(), org, tt.ref)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got.ID != tt.want {
				t.Fatalf("resolveOrgProject(%q) = %v, %v; want %s", tt.ref, got, err, tt.want)
			}
		})
	}
}

func TestOrgServices_StatusTypePortsDomains(t *testing.T) {
	t.Parallel()
	org := testOrg()
	org.services[5].SubdomainAccess = true
	org.services[5].Ports = []platform.Port{{Port: 9000, Protocol: "tcp", Scheme: "http"}}
	org.serviceEnv = map[string][]platform.ServiceEnvVar{"sMedusa": {{Key: "zeropsSubdomain", Content: "https://medusa-1df2-9000.prg1.zerops.app"}}}
	// The platform lists the subdomain among the routing's domains too; it
	// is one domain, named once.
	org.routings = map[string][]platform.PublicHTTPRouting{"pProd": {{
		Domains:   []platform.PublicHTTPDomain{{Name: "medusa-1df2-9000.prg1.zerops.app"}, {Name: "shop.example.com"}},
		Locations: []platform.PublicHTTPLocation{{Path: "/", Port: 9000, ServiceID: "sMedusa"}},
	}}}
	got, err := OrgServices(context.Background(), org, "Contoso - production")
	if err != nil {
		t.Fatal(err)
	}
	if got.Project.Kind != OrgKindProduction || len(got.Services) != 2 {
		t.Fatalf("project=%+v services=%d (system services must be left out)", got.Project, len(got.Services))
	}
	medusa := got.Services[1]
	if medusa.Hostname != "medusa" {
		medusa = got.Services[0]
	}
	if medusa.Type != "nodejs@22" || medusa.Status != "ACTIVE" || len(medusa.Ports) != 1 || medusa.Ports[0].Port != 9000 {
		t.Errorf("medusa = %+v", medusa)
	}
	wantDomains := []string{"https://medusa-1df2-9000.prg1.zerops.app", "shop.example.com"}
	if strings.Join(medusa.Domains, ",") != strings.Join(wantDomains, ",") {
		t.Errorf("domains = %v, want %v", medusa.Domains, wantDomains)
	}
}

func TestOrgFailures_LeavesOutSpentBuildContainersAndOldFailures(t *testing.T) {
	t.Parallel()
	org := testOrg()
	now := time.Now().UTC()
	at := func(ago time.Duration) string { return now.Add(-ago).Format(time.RFC3339Nano) }
	reason := "exit 1"
	org.appVersions = []platform.AppVersionEvent{
		{ID: "av1", ProjectID: "pProd", ServiceStackID: "sMedusa", Status: "BUILD_FAILED", Created: at(time.Hour)},
		{ID: "av2", ProjectID: "pStage", ServiceStackID: "sStage", Status: "DEPLOY_FAILED", Created: at(2 * time.Hour)},
		{ID: "av3", ProjectID: "pProd", ServiceStackID: "sMedusa", Status: "ACTIVE", Created: at(time.Hour)},
		{ID: "avOld", ProjectID: "pProd", ServiceStackID: "sMedusa", Status: "BUILD_FAILED", Created: at(72 * time.Hour)},
	}
	org.processes = []platform.ProcessEvent{
		{ID: "pr1", ProjectID: "pRossy", ActionName: "stack.restart", Status: "FAILED", Created: at(30 * time.Minute), FailReason: &reason,
			ServiceStacks: []platform.ServiceStackRef{{ID: "sRossyApp", Name: "appdev"}}},
		{ID: "pr2", ProjectID: "pRossy", ActionName: "stack.restart", Status: "FINISHED", Created: at(30 * time.Minute)},
		{ID: "prOld", ProjectID: "pRossy", ActionName: "stack.restart", Status: "FAILED", Created: at(72 * time.Hour)},
	}
	got, err := OrgFailures(context.Background(), org, "24h")
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(got.Failures))
	for _, f := range got.Failures {
		keys = append(keys, string(f.Kind)+":"+f.Project.Name+":"+f.Service+":"+f.Status)
	}
	want := []string{
		"process:Northwind Shop - Rossy:appdev:FAILED",
		"build:Contoso - production:medusa:BUILD_FAILED",
		"deploy:ZIT - stage:app:DEPLOY_FAILED",
		"service:Contoso - production:db:FAILED",
		"service:Headquarters:runnerzit:STOPPED",
	}
	if strings.Join(keys, "\n") != strings.Join(want, "\n") {
		t.Errorf("failures:\n%s\nwant:\n%s", strings.Join(keys, "\n"), strings.Join(want, "\n"))
	}
	if got.Failures[0].Reason != "exit 1" {
		t.Errorf("process reason = %q", got.Failures[0].Reason)
	}
}

func TestOrgEnv_MasksUnlessRevealed(t *testing.T) {
	t.Parallel()
	org := testOrg()
	org.projectEnv = map[string][]platform.ProjectEnvVar{"pProd": {
		{Key: "STRIPE_KEY", Content: "sk_live_secret", Sensitive: true},
		{Key: "APP_URL", Content: "https://shop.example.com"},
		{Key: "EMPTY", Content: ""},
	}}
	org.serviceEnv = map[string][]platform.ServiceEnvVar{"sMedusa": {{Key: "DB_PASS", Content: "hunter2"}}}
	tests := []struct {
		name         string
		service      string
		reveal       []string
		wantShown    map[string]string
		wantHidden   []string
		wantRevealed []string
		wantErr      string
	}{
		{name: "project, masked by default", wantHidden: []string{"STRIPE_KEY", "APP_URL"}},
		{name: "project, one revealed", reveal: []string{"APP_URL"}, wantShown: map[string]string{"APP_URL": "https://shop.example.com"}, wantHidden: []string{"STRIPE_KEY"}, wantRevealed: []string{"APP_URL"}},
		{name: "service, masked by default", service: "medusa", wantHidden: []string{"DB_PASS"}},
		{name: "a reveal of a key that is not there", reveal: []string{"NOPE"}, wantErr: "NOPE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := OrgEnv(context.Background(), org, "pProd", tt.service, tt.reveal)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to name %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(got)
			for _, secret := range []string{"sk_live_secret", "hunter2"} {
				if strings.Contains(string(raw), secret) {
					t.Errorf("result carries an unrevealed value: %s", raw)
				}
			}
			byKey := map[string]OrgEnvVar{}
			for _, v := range got.Vars {
				byKey[v.Key] = v
			}
			for k, want := range tt.wantShown {
				if v := byKey[k]; v.Value != want || !v.Revealed {
					t.Errorf("%s = %+v, want revealed %q", k, v, want)
				}
			}
			for _, k := range tt.wantHidden {
				if v := byKey[k]; v.Value != "" || !v.Masked {
					t.Errorf("%s = %+v, want masked", k, v)
				}
			}
			if strings.Join(got.Revealed, ",") != strings.Join(tt.wantRevealed, ",") {
				t.Errorf("revealed = %v, want %v", got.Revealed, tt.wantRevealed)
			}
		})
	}
}

func TestOrgLogs_RuntimeAndBuild(t *testing.T) {
	t.Parallel()
	org := testOrg()
	buildStack := "sBuild"
	org.appVersions = []platform.AppVersionEvent{
		{ID: "avNew", ProjectID: "pProd", ServiceStackID: "sMedusa", Status: "BUILD_FAILED", Created: time.Now().Format(time.RFC3339Nano), Build: &platform.BuildInfo{ServiceStackID: &buildStack}},
	}
	tests := []struct {
		name      string
		in        OrgLogsInput
		wantSvc   string
		wantTags  []string
		wantBuild string
		wantErr   string
	}{
		{name: "runtime logs of a service", in: OrgLogsInput{Project: "pProd", Service: "medusa", Severity: "error", Search: "panic", Limit: 5}, wantSvc: "sMedusa"},
		{name: "build logs read the newest build", in: OrgLogsInput{Project: "pProd", Service: "medusa", Kind: "build"}, wantSvc: "sBuild", wantTags: []string{"zbuilder@avNew"}, wantBuild: "avNew"},
		{name: "a service the project does not have", in: OrgLogsInput{Project: "pProd", Service: "nope"}, wantErr: "nope"},
		{name: "an unknown kind", in: OrgLogsInput{Project: "pProd", Service: "medusa", Kind: "audit"}, wantErr: "kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logs := &fakeLogs{entries: []platform.LogEntry{{Timestamp: "t1", Severity: "Error", Message: "boom"}}}
			got, err := OrgLogs(context.Background(), org, logs, tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(logs.asked) != 1 || logs.asked[0].ServiceID != tt.wantSvc || strings.Join(logs.asked[0].Tags, ",") != strings.Join(tt.wantTags, ",") {
				t.Fatalf("asked %+v, want service %s tags %v", logs.asked, tt.wantSvc, tt.wantTags)
			}
			if tt.in.Search != "" && (logs.asked[0].Search != tt.in.Search || logs.asked[0].Severity != tt.in.Severity) {
				t.Errorf("search/severity not passed: %+v", logs.asked[0])
			}
			if len(got.Entries) != 1 || got.Entries[0].Message != "boom" {
				t.Errorf("entries = %+v", got.Entries)
			}
			if tt.wantBuild != "" && (got.Build == nil || got.Build.AppVersionID != tt.wantBuild) {
				t.Errorf("build = %+v, want %s", got.Build, tt.wantBuild)
			}
		})
	}
}

func TestOrgProcesses_OrgWideOrOneProject(t *testing.T) {
	t.Parallel()
	org := testOrg()
	org.processes = []platform.ProcessEvent{
		{ID: "a", ProjectID: "pProd", ActionName: "stack.restart", Status: "FINISHED", Created: "t2"},
		{ID: "b", ProjectID: "pRossy", ActionName: "stack.build", Status: "RUNNING", Created: "t1"},
	}
	all, err := OrgProcesses(context.Background(), org, "", 0)
	if err != nil || len(all.Processes) != 2 || all.Processes[0].Project.Name != "Contoso - production" {
		t.Fatalf("org-wide = %+v, %v", all, err)
	}
	one, err := OrgProcesses(context.Background(), org, "Rossy", 0)
	if err != nil || len(one.Processes) != 1 || one.Processes[0].ID != "b" {
		t.Fatalf("one project = %+v, %v", one, err)
	}
	if _, err := OrgProcesses(context.Background(), org, "pElsewhere", 0); err == nil {
		t.Fatal("a project outside the org was read")
	}
}

func TestOrgErrors_ArePlatformErrors(t *testing.T) {
	t.Parallel()
	_, err := resolveOrgProject(context.Background(), testOrg(), "pElsewhere")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
		t.Fatalf("err = %v, want %s", err, platform.ErrInvalidParameter)
	}
}

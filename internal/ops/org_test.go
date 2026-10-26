// Tests for: internal/ops/org.go — org mode's look operations
// (docs/spec-mate.md §2.11).
package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// fakeOrg is an organization held in memory, read the way OrgReader reads.
type fakeOrg struct {
	projects []platform.Project
	services []platform.ServiceStack
	members  []platform.OverviewMember
}

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
func (f *fakeOrg) ListMembers(context.Context) ([]platform.OverviewMember, error) {
	return f.members, nil
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
		members: []platform.OverviewMember{
			{UserID: "u1", FullName: "Dana", Role: "ADMIN", Status: "ACTIVE"},
			{UserID: "u2", FullName: "Eli", Role: "READ_ONLY", Status: "ACTIVE"},
		},
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
		{project: "pHQ", wantKind: OrgKindOther},
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
	if got.Projects != 7 {
		t.Fatalf("projects=%d, want 7", got.Projects)
	}
	if len(got.Members) != 2 || got.Members[0].Role != "ADMIN" {
		t.Errorf("members = %+v, want the organization's two people with their roles", got.Members)
	}
	count := map[OrgProjectKind]int{}
	for _, g := range got.Groups {
		count[g.Kind] = len(g.Projects)
	}
	want := map[OrgProjectKind]int{OrgKindChief: 1, OrgKindMate: 1, OrgKindProduction: 1, OrgKindStage: 1, OrgKindOther: 3}
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

func TestOrgServices_StatusTypePorts(t *testing.T) {
	t.Parallel()
	org := testOrg()
	org.services[5].SubdomainAccess = true
	org.services[5].Ports = []platform.Port{{Port: 9000, Protocol: "tcp", Scheme: "http"}}
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
}

func TestOrgErrors_ArePlatformErrors(t *testing.T) {
	t.Parallel()
	_, err := resolveOrgProject(context.Background(), testOrg(), "pElsewhere")
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidParameter {
		t.Fatalf("err = %v, want %s", err, platform.ErrInvalidParameter)
	}
}

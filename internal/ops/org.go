package ops

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// OrgSource is what org mode reads the organization through — the
// read-only platform.OrgReader in production, which reads HQ's overview:
// projects, services and their state, members with their roles
// (docs/spec-mate.md §2.11, §3.11). Every
// project and service id an org operation acts on is resolved from these
// org-scoped lists first, so an id from another organization is never read.
type OrgSource interface {
	ListProjects(ctx context.Context) ([]platform.Project, error)
	ListServices(ctx context.Context, projectID string) ([]platform.ServiceStack, error)
	ListMembers(ctx context.Context) ([]platform.OverviewMember, error)
}

// OrgProjectKind is what a project is to the organization.
type OrgProjectKind string

const (
	OrgKindChief      OrgProjectKind = "chief"
	OrgKindMate       OrgProjectKind = "mate"
	OrgKindHQ         OrgProjectKind = "hq"
	OrgKindProduction OrgProjectKind = "production"
	OrgKindStage      OrgProjectKind = "stage"
	OrgKindOther      OrgProjectKind = "other"
)

// orgKindOrder is the order the overview lists its groups in.
var orgKindOrder = []OrgProjectKind{OrgKindMate, OrgKindHQ, OrgKindProduction, OrgKindStage, OrgKindOther, OrgKindChief}

const (
	tagMate         = "mate"
	tagChief        = "chief"
	hqHostname      = "hq"
	appNameSplitter = " - "
)

// OrgProjectClass is a project's place in the organization: its kind, the
// application its name places it in ("Contoso - production" → Contoso), and
// for a Mate the Mate's own name ("Northwind Shop - Rossy" → Rossy).
type OrgProjectClass struct {
	Kind     OrgProjectKind `json:"kind"`
	App      string         `json:"app,omitempty"`
	MateName string         `json:"mate,omitempty"`
}

// ClassifyOrgProject reads a project's kind from what Zerops holds: the
// chief tag; a Mate is tagged mate and runs a zcp@ service; HQ runs a
// service named hq; an application's production and stage are named
// "<application> - production" / "<application> - stage". Everything else —
// tools, experiments, a Mate not yet born — is other.
func ClassifyOrgProject(p platform.Project, services []platform.ServiceStack) OrgProjectClass {
	app, own := splitAppName(p.Name)
	switch {
	case slices.Contains(p.Tags, tagChief):
		return OrgProjectClass{Kind: OrgKindChief}
	case slices.Contains(p.Tags, tagMate) && slices.ContainsFunc(services, func(s platform.ServiceStack) bool {
		return strings.HasPrefix(s.ServiceStackTypeInfo.ServiceStackTypeVersionName, zcpTypePrefix)
	}):
		return OrgProjectClass{Kind: OrgKindMate, App: app, MateName: own}
	case slices.ContainsFunc(services, func(s platform.ServiceStack) bool { return s.Name == hqHostname }):
		return OrgProjectClass{Kind: OrgKindHQ}
	}
	switch strings.ToLower(own) {
	case "production", "prod":
		if app != "" {
			return OrgProjectClass{Kind: OrgKindProduction, App: app}
		}
	case "stage", "staging":
		if app != "" {
			return OrgProjectClass{Kind: OrgKindStage, App: app}
		}
	}
	return OrgProjectClass{Kind: OrgKindOther}
}

// splitAppName cuts "<application> - <own name>" at its last separator; a
// name without one is its own name, under no application.
func splitAppName(name string) (app, own string) {
	name = strings.TrimSpace(name)
	i := strings.LastIndex(name, appNameSplitter)
	if i <= 0 || i+len(appNameSplitter) >= len(name) {
		return "", name
	}
	return strings.TrimSpace(name[:i]), strings.TrimSpace(name[i+len(appNameSplitter):])
}

// OrgProjectRef names a project in a result.
type OrgProjectRef struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Kind OrgProjectKind `json:"kind,omitempty"`
}

// OrgProjectSummary is one project in the overview.
type OrgProjectSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	App      string `json:"app,omitempty"`
	MateName string `json:"mate,omitempty"`
	Status   string `json:"status"`
	Services int    `json:"services"`
	// NotRunning names each of the project's own services that is stopped
	// or failing, "<hostname> <status>"; spent build containers and the
	// platform's system services are left out.
	NotRunning []string `json:"notRunning,omitempty"`
}

// OrgGroup is the overview's projects of one kind.
type OrgGroup struct {
	Kind     OrgProjectKind      `json:"kind"`
	Projects []OrgProjectSummary `json:"projects"`
}

// OrgOverviewResult is every project of the organization, grouped by kind,
// and the organization's members with their roles.
type OrgOverviewResult struct {
	Projects int                       `json:"projects"`
	Groups   []OrgGroup                `json:"groups"`
	Members  []platform.OverviewMember `json:"members"`
}

// orgSnapshot is the organization's projects and services, read once.
type orgSnapshot struct {
	projects  []platform.Project
	byProject map[string][]platform.ServiceStack
	byID      map[string]platform.Project
}

func readOrgSnapshot(ctx context.Context, src OrgSource) (*orgSnapshot, error) {
	projects, err := src.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the org's projects: %w", err)
	}
	services, err := src.ListServices(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list the org's services: %w", err)
	}
	snap := &orgSnapshot{projects: projects, byProject: map[string][]platform.ServiceStack{}, byID: map[string]platform.Project{}}
	for _, p := range projects {
		snap.byID[p.ID] = p
	}
	for _, s := range services {
		snap.byProject[s.ProjectID] = append(snap.byProject[s.ProjectID], s)
	}
	return snap, nil
}

// OrgOverview lists every project of the organization, grouped by kind.
func OrgOverview(ctx context.Context, src OrgSource) (*OrgOverviewResult, error) {
	snap, err := readOrgSnapshot(ctx, src)
	if err != nil {
		return nil, err
	}
	grouped := map[OrgProjectKind][]OrgProjectSummary{}
	for _, p := range snap.projects {
		services := snap.byProject[p.ID]
		class := ClassifyOrgProject(p, services)
		summary := OrgProjectSummary{ID: p.ID, Name: p.Name, App: class.App, MateName: class.MateName, Status: p.Status}
		for _, s := range services {
			if s.IsSystem() {
				continue
			}
			summary.Services++
			if notRunning(s) {
				summary.NotRunning = append(summary.NotRunning, s.Name+" "+s.Status)
			}
		}
		grouped[class.Kind] = append(grouped[class.Kind], summary)
	}
	members, err := src.ListMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the org's members: %w", err)
	}
	result := &OrgOverviewResult{Projects: len(snap.projects), Members: members}
	for _, kind := range orgKindOrder {
		projects := grouped[kind]
		if len(projects) == 0 {
			continue
		}
		sort.SliceStable(projects, func(i, j int) bool { return strings.ToLower(projects[i].Name) < strings.ToLower(projects[j].Name) })
		result.Groups = append(result.Groups, OrgGroup{Kind: kind, Projects: projects})
	}
	return result, nil
}

// notRunning reports a service that is stopped or failing.
func notRunning(s platform.ServiceStack) bool {
	return s.Status == platform.ServiceStatusStopped || s.Status == platform.ServiceStatusFailed
}

// resolveOrgProject finds a project of the organization by id, by its full
// name, or by its Mate's name — never outside the organization.
func resolveOrgProject(ctx context.Context, src OrgSource, ref string) (*orgProject, error) {
	snap, err := readOrgSnapshot(ctx, src)
	if err != nil {
		return nil, err
	}
	return snap.resolve(ref)
}

// orgProject is a resolved project with its services and class.
type orgProject struct {
	platform.Project
	Services []platform.ServiceStack
	Class    OrgProjectClass
}

func (p *orgProject) ref() OrgProjectRef {
	return OrgProjectRef{ID: p.ID, Name: p.Name, Kind: p.Class.Kind}
}

func (snap *orgSnapshot) project(p platform.Project) *orgProject {
	services := snap.byProject[p.ID]
	return &orgProject{Project: p, Services: services, Class: ClassifyOrgProject(p, services)}
}

func (snap *orgSnapshot) resolve(ref string) (*orgProject, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter, "project is required", "Pass a project id, its name, or its Mate's name (zerops_org_overview lists them)")
	}
	if p, ok := snap.byID[ref]; ok {
		return snap.project(p), nil
	}
	var matches []*orgProject
	for _, p := range snap.projects {
		op := snap.project(p)
		if strings.EqualFold(p.Name, ref) || (op.Class.Kind == OrgKindMate && strings.EqualFold(op.Class.MateName, ref)) {
			matches = append(matches, op)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("no project %q in this organization", ref),
			"zerops_org_overview lists the organization's projects")
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, m.ID+" ("+m.Name+")")
		}
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("%q names %d projects: %s", ref, len(matches), strings.Join(ids, ", ")),
			"Pass the project id")
	}
}

// service finds one of the project's own services by hostname or id.
func (p *orgProject) service(ref string) (*platform.ServiceStack, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, platform.NewPlatformError(platform.ErrServiceRequired, "service is required", "Pass the service's hostname (zerops_org_services lists them)")
	}
	for i := range p.Services {
		s := &p.Services[i]
		if s.Name == ref || s.ID == ref {
			return s, nil
		}
	}
	return nil, platform.NewPlatformError(platform.ErrServiceNotFound,
		fmt.Sprintf("%s has no service %q", p.Name, ref),
		"zerops_org_services lists the project's services")
}

// OrgServicePort is one of a service's ports.
type OrgServicePort struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol,omitempty"`
	Scheme   string `json:"scheme,omitempty"`
	Public   bool   `json:"public,omitempty"`
}

// OrgService is one service of a project.
type OrgService struct {
	ID       string           `json:"id"`
	Hostname string           `json:"hostname"`
	Type     string           `json:"type"`
	Status   string           `json:"status"`
	Ports    []OrgServicePort `json:"ports,omitempty"`
}

// OrgServicesResult is a project's services.
type OrgServicesResult struct {
	Project  OrgProjectRef `json:"project"`
	Services []OrgService  `json:"services"`
}

// OrgServices lists a project's own services — the platform's system
// services and spent build containers left out — with status, type and ports.
func OrgServices(ctx context.Context, src OrgSource, projectRef string) (*OrgServicesResult, error) {
	p, err := resolveOrgProject(ctx, src, projectRef)
	if err != nil {
		return nil, err
	}
	result := &OrgServicesResult{Project: p.ref(), Services: []OrgService{}}
	for _, s := range p.Services {
		if s.IsSystem() {
			continue
		}
		out := OrgService{ID: s.ID, Hostname: s.Name, Type: s.ServiceStackTypeInfo.ServiceStackTypeVersionName, Status: s.Status}
		for _, port := range s.Ports {
			out.Ports = append(out.Ports, OrgServicePort{Port: port.Port, Protocol: port.Protocol, Scheme: port.Scheme, Public: port.Public})
		}
		result.Services = append(result.Services, out)
	}
	sort.SliceStable(result.Services, func(i, j int) bool { return result.Services[i].Hostname < result.Services[j].Hostname })
	return result, nil
}

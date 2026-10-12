package ops

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// OrgSource is what org mode reads the organization through — the
// read-only platform.OrgReader in production (docs/spec-mate.md §2.11). Every
// project and service id an org operation acts on is resolved from these
// org-scoped lists first, so an id from another organization is never read.
type OrgSource interface {
	OrgID() string
	ListProjects(ctx context.Context) ([]platform.Project, error)
	ListServices(ctx context.Context, projectID string) ([]platform.ServiceStack, error)
	GetProjectEnv(ctx context.Context, projectID string) ([]platform.ProjectEnvVar, error)
	GetServiceEnv(ctx context.Context, serviceID string) ([]platform.ServiceEnvVar, error)
	GetProjectLog(ctx context.Context, projectID string) (*platform.LogAccess, error)
	ListPublicHTTPRoutings(ctx context.Context, projectID string) ([]platform.PublicHTTPRouting, error)
	SearchProcesses(ctx context.Context, projectID string, limit int) ([]platform.ProcessEvent, error)
	SearchAppVersions(ctx context.Context, projectID string, limit int) ([]platform.AppVersionEvent, error)
}

// OrgProjectKind is what a project is to the organization.
type OrgProjectKind string

const (
	OrgKindMastermate OrgProjectKind = "mastermate"
	OrgKindMate       OrgProjectKind = "mate"
	OrgKindHQ         OrgProjectKind = "hq"
	OrgKindProduction OrgProjectKind = "production"
	OrgKindStage      OrgProjectKind = "stage"
	OrgKindOther      OrgProjectKind = "other"
)

// orgKindOrder is the order the overview lists its groups in.
var orgKindOrder = []OrgProjectKind{OrgKindMate, OrgKindHQ, OrgKindProduction, OrgKindStage, OrgKindOther, OrgKindMastermate}

const (
	tagMate         = "mate"
	tagMastermate   = "mastermate"
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
// mastermate tag; a Mate is tagged mate and runs a zcp@ service; HQ runs a
// service named hq; an application's production and stage are named
// "<application> - production" / "<application> - stage". Everything else —
// tools, experiments, a Mate not yet born — is other.
func ClassifyOrgProject(p platform.Project, services []platform.ServiceStack) OrgProjectClass {
	app, own := splitAppName(p.Name)
	switch {
	case slices.Contains(p.Tags, tagMastermate):
		return OrgProjectClass{Kind: OrgKindMastermate}
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

// OrgOverviewResult is every project of the organization, grouped by kind.
type OrgOverviewResult struct {
	Org      string     `json:"org"`
	Projects int        `json:"projects"`
	Groups   []OrgGroup `json:"groups"`
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
	result := &OrgOverviewResult{Org: src.OrgID(), Projects: len(snap.projects)}
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
	// Domains are the service's subdomain URL (when enabled) and the
	// custom domains routed to it.
	Domains []string `json:"domains,omitempty"`
}

// OrgServicesResult is a project's services.
type OrgServicesResult struct {
	Project  OrgProjectRef `json:"project"`
	Services []OrgService  `json:"services"`
}

// OrgServices lists a project's own services — the platform's system
// services and spent build containers left out — with status, type, ports
// and domains.
func OrgServices(ctx context.Context, src OrgSource, projectRef string) (*OrgServicesResult, error) {
	p, err := resolveOrgProject(ctx, src, projectRef)
	if err != nil {
		return nil, err
	}
	routings, err := src.ListPublicHTTPRoutings(ctx, p.ID)
	if err != nil {
		return nil, fmt.Errorf("read %s's domains: %w", p.Name, err)
	}
	custom := map[string][]string{}
	for _, r := range routings {
		for _, loc := range r.Locations {
			for _, d := range r.Domains {
				if !slices.Contains(custom[loc.ServiceID], d.Name) {
					custom[loc.ServiceID] = append(custom[loc.ServiceID], d.Name)
				}
			}
		}
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
		if s.SubdomainAccess {
			envs, err := src.GetServiceEnv(ctx, s.ID)
			if err == nil {
				for _, e := range envs {
					if e.Key == envKeyZeropsSubdomain && e.Content != "" {
						out.Domains = append(out.Domains, e.Content)
					}
				}
			}
		}
		for _, d := range custom[s.ID] {
			if !slices.ContainsFunc(out.Domains, func(u string) bool { return strings.TrimPrefix(u, "https://") == d }) {
				out.Domains = append(out.Domains, d)
			}
		}
		result.Services = append(result.Services, out)
	}
	sort.SliceStable(result.Services, func(i, j int) bool { return result.Services[i].Hostname < result.Services[j].Hostname })
	return result, nil
}

// Log kinds org mode reads.
const (
	OrgLogsRuntime = "runtime"
	OrgLogsBuild   = "build"
)

// OrgLogsInput is what zerops_org_logs reads.
type OrgLogsInput struct {
	Project  string
	Service  string
	Kind     string // runtime (default) | build
	Since    string
	Severity string
	Search   string
	Limit    int
}

// OrgBuildRef names the build a build log belongs to.
type OrgBuildRef struct {
	AppVersionID string `json:"appVersionId"`
	Status       string `json:"status"`
	Created      string `json:"created"`
}

// OrgLogsResult is a service's log lines, newest last.
type OrgLogsResult struct {
	Project OrgProjectRef    `json:"project"`
	Service string           `json:"service"`
	Kind    string           `json:"kind"`
	Build   *OrgBuildRef     `json:"build,omitempty"`
	Entries []LogEntryOutput `json:"entries"`
	HasMore bool             `json:"hasMore"`
}

// orgAppVersionWindow is how many of a project's newest builds the build log
// looks through for the service's newest.
const orgAppVersionWindow = 100

// OrgLogs reads a service's runtime log, or its newest build's log.
func OrgLogs(ctx context.Context, src OrgSource, fetcher platform.LogFetcher, in OrgLogsInput) (*OrgLogsResult, error) {
	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = OrgLogsRuntime
	}
	if kind != OrgLogsRuntime && kind != OrgLogsBuild {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("unknown log kind %q", kind), "kind is runtime or build")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultLogLimit
	}
	p, err := resolveOrgProject(ctx, src, in.Project)
	if err != nil {
		return nil, err
	}
	s, err := p.service(in.Service)
	if err != nil {
		return nil, err
	}
	params := platform.LogFetchParams{ServiceID: s.ID, Severity: in.Severity, Facility: "application", Limit: limit + 1, Search: in.Search}
	result := &OrgLogsResult{Project: p.ref(), Service: s.Name, Kind: kind}
	if kind == OrgLogsRuntime || strings.TrimSpace(in.Since) != "" {
		since, err := parseSince(in.Since)
		if err != nil {
			return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Invalid since value: %v", err), "Use formats like 30s, 5m, 1h, 7d, or ISO 8601 (RFC3339)")
		}
		params.Since = since
	}
	if kind == OrgLogsBuild {
		versions, err := src.SearchAppVersions(ctx, p.ID, orgAppVersionWindow)
		if err != nil {
			return nil, fmt.Errorf("read %s's builds: %w", p.Name, err)
		}
		var newest *platform.AppVersionEvent
		for i := range versions {
			v := &versions[i]
			if v.ServiceStackID != s.ID || v.Build == nil || v.Build.ServiceStackID == nil {
				continue
			}
			if newest == nil || v.Created > newest.Created {
				newest = v
			}
		}
		if newest == nil {
			return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("%s in %s has no build to read", s.Name, p.Name), "Read its runtime log instead")
		}
		params.ServiceID = *newest.Build.ServiceStackID
		params.Tags = []string{buildTagForEvent(newest)}
		result.Build = &OrgBuildRef{AppVersionID: newest.ID, Status: newest.Status, Created: newest.Created}
	}
	access, err := src.GetProjectLog(ctx, p.ID)
	if err != nil {
		return nil, fmt.Errorf("open %s's log: %w", p.Name, err)
	}
	entries, err := fetcher.FetchLogs(ctx, access, params)
	if err != nil {
		return nil, fmt.Errorf("read %s's log: %w", s.Name, err)
	}
	if len(entries) > limit {
		result.HasMore = true
		entries = entries[len(entries)-limit:]
	}
	result.Entries = make([]LogEntryOutput, 0, len(entries))
	for _, e := range entries {
		result.Entries = append(result.Entries, LogEntryOutput{Timestamp: e.Timestamp, Severity: e.Severity, Message: e.Message, Container: e.Container})
	}
	return result, nil
}

// OrgFailureKind is what failed.
type OrgFailureKind string

const (
	OrgFailureProcess OrgFailureKind = "process"
	OrgFailureBuild   OrgFailureKind = "build"
	OrgFailureDeploy  OrgFailureKind = "deploy"
	OrgFailureService OrgFailureKind = "service"
)

// OrgFailure is one thing that failed, or one service that is down.
type OrgFailure struct {
	Kind    OrgFailureKind `json:"kind"`
	At      string         `json:"at,omitempty"`
	Project OrgProjectRef  `json:"project"`
	Service string         `json:"service,omitempty"`
	Status  string         `json:"status"`
	Action  string         `json:"action,omitempty"`
	Reason  string         `json:"reason,omitempty"`
	ID      string         `json:"id,omitempty"`
}

// OrgFailuresResult is the organization's failures since a time, and its
// services that are down now.
type OrgFailuresResult struct {
	Since    string       `json:"since"`
	Failures []OrgFailure `json:"failures"`
	// Scanned says how much was looked through, so "nothing failed" can be
	// told from "nothing was read".
	Scanned map[string]int `json:"scanned"`
}

// orgFailureWindow is how many of the organization's newest processes and
// builds the failure sweep reads.
const orgFailureWindow = 500

// OrgFailures sweeps the organization: failed processes, builds and deploys
// since the given time (newest first), then every own service stopped or
// failing now. Spent build containers and the platform's system services are
// resting, not failing, and left out.
func OrgFailures(ctx context.Context, src OrgSource, sinceRaw string) (*OrgFailuresResult, error) {
	if strings.TrimSpace(sinceRaw) == "" {
		sinceRaw = "24h"
	}
	since, err := parseSince(sinceRaw)
	if err != nil {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid since value: %v", err), "Use formats like 30m, 24h, 7d, or ISO 8601 (RFC3339)")
	}
	snap, err := readOrgSnapshot(ctx, src)
	if err != nil {
		return nil, err
	}
	processes, err := src.SearchProcesses(ctx, "", orgFailureWindow)
	if err != nil {
		return nil, fmt.Errorf("read the org's processes: %w", err)
	}
	versions, err := src.SearchAppVersions(ctx, "", orgFailureWindow)
	if err != nil {
		return nil, fmt.Errorf("read the org's builds: %w", err)
	}
	refOf := func(projectID string) OrgProjectRef {
		p, ok := snap.byID[projectID]
		if !ok {
			return OrgProjectRef{ID: projectID}
		}
		return snap.project(p).ref()
	}
	serviceName := map[string]string{}
	for _, list := range snap.byProject {
		for _, s := range list {
			serviceName[s.ID] = s.Name
		}
	}
	after := func(ts string) bool {
		t, err := time.Parse(time.RFC3339Nano, ts)
		return err == nil && !t.Before(since)
	}

	var timed []OrgFailure
	for _, p := range processes {
		if p.Status != platform.ProcessStatusFailed || !after(p.Created) {
			continue
		}
		f := OrgFailure{Kind: OrgFailureProcess, At: p.Created, Project: refOf(p.ProjectID), Status: p.Status, Action: p.ActionName, ID: p.ID}
		names := make([]string, 0, len(p.ServiceStacks))
		for _, ss := range p.ServiceStacks {
			names = append(names, ss.Name)
		}
		f.Service = strings.Join(names, ",")
		if p.FailReason != nil {
			f.Reason = *p.FailReason
		}
		timed = append(timed, f)
	}
	for _, v := range versions {
		if !strings.HasSuffix(v.Status, "FAILED") || !after(v.Created) {
			continue
		}
		kind := OrgFailureDeploy
		if strings.HasPrefix(v.Status, "BUILD") {
			kind = OrgFailureBuild
		}
		timed = append(timed, OrgFailure{Kind: kind, At: v.Created, Project: refOf(v.ProjectID), Service: serviceName[v.ServiceStackID], Status: v.Status, ID: v.ID})
	}
	sort.SliceStable(timed, func(i, j int) bool { return timed[i].At > timed[j].At })

	var down []OrgFailure
	services := 0
	for _, list := range snap.byProject {
		for _, s := range list {
			if s.IsSystem() {
				continue
			}
			services++
			if notRunning(s) {
				down = append(down, OrgFailure{Kind: OrgFailureService, Project: refOf(s.ProjectID), Service: s.Name, Status: s.Status, ID: s.ID})
			}
		}
	}
	sort.SliceStable(down, func(i, j int) bool {
		if down[i].Project.Name != down[j].Project.Name {
			return strings.ToLower(down[i].Project.Name) < strings.ToLower(down[j].Project.Name)
		}
		return down[i].Service < down[j].Service
	})

	return &OrgFailuresResult{
		Since:    since.UTC().Format(time.RFC3339),
		Failures: append(append([]OrgFailure{}, timed...), down...),
		Scanned:  map[string]int{"projects": len(snap.projects), "services": services, "processes": len(processes), "builds": len(versions)},
	}, nil
}

// OrgEnvVar is one variable. Its value is present only when revealed.
type OrgEnvVar struct {
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"`
	Masked    bool   `json:"masked,omitempty"`
	Revealed  bool   `json:"revealed,omitempty"`
	Empty     bool   `json:"empty,omitempty"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

// OrgEnvResult is a project's or a service's variables.
type OrgEnvResult struct {
	Project  OrgProjectRef `json:"project"`
	Service  string        `json:"service,omitempty"`
	Scope    string        `json:"scope"`
	Vars     []OrgEnvVar   `json:"vars"`
	Revealed []string      `json:"revealed,omitempty"`
	Note     string        `json:"note"`
}

const orgEnvNote = "Values are masked. Pass reveal with a key to read its value; a revealed value is listed in revealed."

// OrgEnv reads a project's variables, or a service's when service is set.
// Every value is masked unless its key is named in reveal; the result lists
// what was revealed.
func OrgEnv(ctx context.Context, src OrgSource, projectRef, serviceRef string, reveal []string) (*OrgEnvResult, error) {
	p, err := resolveOrgProject(ctx, src, projectRef)
	if err != nil {
		return nil, err
	}
	type kv struct {
		key, value string
		sensitive  bool
	}
	var vars []kv
	result := &OrgEnvResult{Project: p.ref(), Scope: OrgScopeProject, Note: orgEnvNote}
	if strings.TrimSpace(serviceRef) != "" {
		s, err := p.service(serviceRef)
		if err != nil {
			return nil, err
		}
		envs, err := src.GetServiceEnv(ctx, s.ID)
		if err != nil {
			return nil, fmt.Errorf("read %s's variables: %w", s.Name, err)
		}
		for _, e := range envs {
			vars = append(vars, kv{e.Key, e.Content, e.Sensitive})
		}
		result.Scope, result.Service = OrgScopeService, s.Name
	} else {
		envs, err := src.GetProjectEnv(ctx, p.ID)
		if err != nil {
			return nil, fmt.Errorf("read %s's variables: %w", p.Name, err)
		}
		for _, e := range envs {
			vars = append(vars, kv{e.Key, e.Content, e.Sensitive})
		}
	}
	want := map[string]bool{}
	for _, k := range reveal {
		if k = strings.TrimSpace(k); k != "" {
			want[k] = true
		}
	}
	for k := range want {
		if !slices.ContainsFunc(vars, func(v kv) bool { return v.key == k }) {
			return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("no variable %s here to reveal", k), "Call without reveal to list the keys")
		}
	}
	sort.SliceStable(vars, func(i, j int) bool { return vars[i].key < vars[j].key })
	result.Vars = make([]OrgEnvVar, 0, len(vars))
	for _, v := range vars {
		out := OrgEnvVar{Key: v.key, Sensitive: v.sensitive, Empty: v.value == ""}
		switch {
		case want[v.key]:
			out.Value, out.Revealed = v.value, true
			result.Revealed = append(result.Revealed, v.key)
		case !out.Empty:
			out.Masked = true
		}
		result.Vars = append(result.Vars, out)
	}
	return result, nil
}

// OrgProcess is one platform process.
type OrgProcess struct {
	ID       string        `json:"id"`
	Action   string        `json:"action"`
	Status   string        `json:"status"`
	Project  OrgProjectRef `json:"project"`
	Services []string      `json:"services,omitempty"`
	Created  string        `json:"created"`
	Finished string        `json:"finished,omitempty"`
	By       string        `json:"by,omitempty"`
	Reason   string        `json:"reason,omitempty"`
}

// OrgProcessesResult is recent platform processes, newest first.
type OrgProcessesResult struct {
	Project   *OrgProjectRef `json:"project,omitempty"`
	Processes []OrgProcess   `json:"processes"`
}

const (
	orgProcessesDefault = 30
	orgProcessesMax     = 200
)

// OrgProcesses lists the organization's recent processes, or one project's.
func OrgProcesses(ctx context.Context, src OrgSource, projectRef string, limit int) (*OrgProcessesResult, error) {
	if limit <= 0 {
		limit = orgProcessesDefault
	}
	limit = min(limit, orgProcessesMax)
	snap, err := readOrgSnapshot(ctx, src)
	if err != nil {
		return nil, err
	}
	result := &OrgProcessesResult{Processes: []OrgProcess{}}
	projectID := ""
	if strings.TrimSpace(projectRef) != "" {
		p, err := snap.resolve(projectRef)
		if err != nil {
			return nil, err
		}
		ref := p.ref()
		result.Project, projectID = &ref, p.ID
	}
	processes, err := src.SearchProcesses(ctx, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("read processes: %w", err)
	}
	for _, pr := range processes {
		out := OrgProcess{ID: pr.ID, Action: pr.ActionName, Status: pr.Status, Created: pr.Created}
		if p, ok := snap.byID[pr.ProjectID]; ok {
			out.Project = snap.project(p).ref()
		} else {
			out.Project = OrgProjectRef{ID: pr.ProjectID}
		}
		for _, ss := range pr.ServiceStacks {
			out.Services = append(out.Services, ss.Name)
		}
		if pr.Finished != nil {
			out.Finished = *pr.Finished
		}
		if pr.CreatedByUser != nil {
			out.By = pr.CreatedByUser.FullName
		} else if pr.CreatedBySystem {
			out.By = "system"
		}
		if pr.FailReason != nil {
			out.Reason = *pr.FailReason
		}
		result.Processes = append(result.Processes, out)
	}
	return result, nil
}

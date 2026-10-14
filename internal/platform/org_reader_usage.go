package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/zeropsio/zerops-go/apiError"
	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/output"
	"github.com/zeropsio/zerops-go/sdkBase"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
)

// OrgPerson is who the Chief acts for: their name, their role in the
// organization (OWNER, ADMIN, BASIC_USER, READ_ONLY, NO_ACCESS) and what it
// lets them do. Finances are their own flags, apart from the role: an
// ADMIN may not see the organization's costs. Through ZCP_ORG_API it is HQ's
// own answer (GET /chief/person); through the region's API, the token's
// membership in the org.
type OrgPerson struct {
	Name              string `json:"name"`
	Role              string `json:"roleCode"`
	CanViewFinances   bool   `json:"canViewFinances"`
	CanEditFinances   bool   `json:"canEditFinances"`
	CanCreateProjects bool   `json:"canCreateProjects"`
}

// chiefPersonPath is HQ's answer of who a Chief's person is; the Zerops API
// has no such path, so only ZCP_ORG_API serves it.
const chiefPersonPath = publicAPIRoot + "/chief/person"

// GetPerson is who the Chief acts for.
func (r *OrgReader) GetPerson(ctx context.Context) (OrgPerson, error) {
	if r.viaOrgAPI {
		return r.chiefPerson(ctx)
	}
	resp, err := r.z.handler.GetUserInfo(ctx)
	if err != nil {
		return OrgPerson{}, mapSDKError(err, "auth")
	}
	out, err := resp.Output()
	if err != nil {
		return OrgPerson{}, mapSDKError(err, "auth")
	}
	for _, m := range out.ClientUserList {
		if m.ClientId.TypedString().String() != r.orgID {
			continue
		}
		return OrgPerson{
			Name:              out.FullName.String(),
			Role:              string(m.RoleCode),
			CanViewFinances:   m.CanViewFinances.Native(),
			CanEditFinances:   m.CanEditFinances.Native(),
			CanCreateProjects: m.CanCreateProjects.Native(),
		}, nil
	}
	return OrgPerson{}, NewPlatformError(ErrOrgNotReachable,
		fmt.Sprintf("the token is no member of %s", r.orgID), "")
}

func (r *OrgReader) chiefPerson(ctx context.Context) (OrgPerson, error) {
	resp := sdkBase.Get(ctx, r.z.env, chiefPersonPath)
	if resp.Err != nil {
		return OrgPerson{}, fmt.Errorf("read the Chief's person: %w", mapSDKError(resp.Err, "auth"))
	}
	decoder := json.NewDecoder(resp.ResponseData)
	if resp.HttpResponse.StatusCode < http.StatusMultipleChoices {
		var person OrgPerson
		if err := decoder.Decode(&person); err != nil {
			return OrgPerson{}, fmt.Errorf("read the Chief's person: decode: %w", err)
		}
		return person, nil
	}
	answer := struct {
		Error apiError.Error `json:"error"`
	}{}
	if err := decoder.Decode(&answer); err != nil {
		return OrgPerson{}, fmt.Errorf("read the Chief's person: %s", resp.HttpResponse.Status)
	}
	answer.Error.HttpStatusCode = resp.HttpResponse.StatusCode
	return OrgPerson{}, fmt.Errorf("read the Chief's person: %w", mapSDKError(answer.Error, "auth"))
}

// OrgPeriodCost is a cost over the platform's fixed periods, in the
// organization's billing currency. A service's rolling periods (the last 7
// and 30 days, the last 12 months, this year) can read zero beside a month
// that does not; its calendar days and months hold.
type OrgPeriodCost struct {
	Today, Yesterday     float64
	ThisMonth, LastMonth float64
	ThisYear, LastYear   float64
	Last24Hours          float64
	Last7Days            float64
	Last30Days           float64
	Last12Months         float64
}

// OrgCostItem is one project's or one service's cost, by its id.
type OrgCostItem struct {
	ID   string
	Cost OrgPeriodCost
}

// OrgCosts is the organization's cost over each period, and each project's
// and each service's. A person outside the org's finances is answered (by
// HQ, as by Zerops) only their projects', and an org total of theirs alone.
type OrgCosts struct {
	Org      OrgPeriodCost
	Projects []OrgCostItem
	Services []OrgCostItem
}

func periodCost(p output.EsTransactionDebitPeriodCost) OrgPeriodCost {
	return OrgPeriodCost{
		Today: p.Today.Native(), Yesterday: p.Yesterday.Native(),
		ThisMonth: p.ThisMonth.Native(), LastMonth: p.LastMonth.Native(),
		ThisYear: p.ThisYear.Native(), LastYear: p.LastYear.Native(),
		Last24Hours: p.Last24hours.Native(), Last7Days: p.Last7days.Native(),
		Last30Days: p.Last30days.Native(), Last12Months: p.Last12month.Native(),
	}
}

// SearchCosts is the organization's costs over the platform's periods.
func (r *OrgReader) SearchCosts(ctx context.Context) (*OrgCosts, error) {
	resp, err := r.z.handler.PostTransactionDebitCostSearch(ctx, body.EsTransactionDebitCost{
		Search: body.EsTransactionDebitCostSearch{eq("clientId", r.orgID)},
	})
	if err != nil {
		return nil, mapSDKError(err, "cost")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "cost")
	}
	costs := &OrgCosts{Org: periodCost(out.Client.PeriodCost)}
	for _, p := range out.Project {
		costs.Projects = append(costs.Projects, OrgCostItem{ID: p.Id.String(), Cost: periodCost(p.PeriodCost)})
	}
	for _, s := range out.Stack {
		costs.Services = append(costs.Services, OrgCostItem{ID: s.Id.String(), Cost: periodCost(s.PeriodCost)})
	}
	return costs, nil
}

// Steps a range is read in: a breakdown's and resource use's. A range is
// aligned to its step: the API refuses one that is not.
const (
	OrgStepHour = "1h"
	OrgStepDay  = "1d"
)

// OrgCostQuery is a cost breakdown: its range, read a step at a time, and a
// line per project, or per service of ProjectID. Only a line per service says
// what its cost is made of; the API leaves a project's parts at zero.
type OrgCostQuery struct {
	From, Till time.Time
	Step       string
	// ProjectID, set, asks for a line per service of that project.
	ProjectID string
}

// OrgCostLine is one project's or one service's cost over one step of a
// range: its total and what it is made of.
type OrgCostLine struct {
	ProjectID, ProjectName string
	// ServiceID and ServiceName are set on a line per service.
	ServiceID, ServiceName string
	From, Till             time.Time
	Total                  float64
	CPU, VCPU, RAM, Disk   float64
	Backups, BuildTime     float64
	Egress, ObjectStorage  float64
}

// orgCostLineDTO is a breakdown line on the wire; the SDK leaves it raw.
type orgCostLineDTO struct {
	ProjectID     string    `json:"projectId"`
	ProjectName   string    `json:"projectName"`
	StackID       string    `json:"stackId"`
	StackName     string    `json:"stackName"`
	From          time.Time `json:"from"`
	Till          time.Time `json:"till"`
	Total         float64   `json:"sumTotalPrice"`
	CPU           float64   `json:"cpu"`
	VCPU          float64   `json:"vCpu"`
	RAM           float64   `json:"ram"`
	Disk          float64   `json:"disc"`
	Backups       float64   `json:"backupStorage"`
	BuildTime     float64   `json:"buildTime"`
	Egress        float64   `json:"masterEgress"`
	ObjectStorage float64   `json:"objectStorage"`
}

// SearchCostBreakdown is the organization's cost over a range, a line per
// project (or service) and step.
func (r *OrgReader) SearchCostBreakdown(ctx context.Context, q OrgCostQuery) ([]OrgCostLine, error) {
	groupBy, search := enum.EsGroupBySearchGroupByEnumProjectId, body.EsGroupBySearchSearch{eq("clientId", r.orgID)}
	if q.ProjectID != "" {
		groupBy, search = enum.EsGroupBySearchGroupByEnumStackId, append(search, eq("projectId", q.ProjectID))
	}
	resp, err := r.z.handler.PostTransactionDebitGroupBySearch(ctx, body.EsGroupBySearch{
		Search:      search,
		From:        types.NewDateTime(q.From.UTC()),
		Till:        types.NewDateTime(q.Till.UTC()),
		TimeZone:    types.NewString("UTC"),
		GroupBy:     groupBy,
		TimeGroupBy: types.NewString(q.Step),
	})
	if err != nil {
		return nil, mapSDKError(err, "cost")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "cost")
	}
	lines := make([]OrgCostLine, 0, len(out.Items))
	for _, raw := range out.Items {
		var l orgCostLineDTO
		if err := json.Unmarshal(raw, &l); err != nil {
			return nil, fmt.Errorf("read a cost line: %w", err)
		}
		if q.ProjectID != "" && l.ProjectID != q.ProjectID {
			continue
		}
		lines = append(lines, OrgCostLine{
			ProjectID: l.ProjectID, ProjectName: l.ProjectName, ServiceID: l.StackID, ServiceName: l.StackName,
			From: l.From, Till: l.Till, Total: l.Total,
			CPU: l.CPU, VCPU: l.VCPU, RAM: l.RAM, Disk: l.Disk,
			Backups: l.Backups, BuildTime: l.BuildTime, Egress: l.Egress, ObjectStorage: l.ObjectStorage,
		})
	}
	return lines, nil
}

// OrgResourceUse is one service's use of its resources over one step, each
// against its limit: dedicated CPU cores, shared vCPUs, RAM and disk in GB.
type OrgResourceUse struct {
	ProjectID, ServiceID string
	From, Till           time.Time
	Containers           int
	CPUUsed, CPULimit    float64
	VCPUUsed, VCPULimit  float64
	RAMUsed, RAMLimit    float64
	DiskUsed, DiskLimit  float64
}

// SearchResourceUse is a project's services' resource use over a range, a
// line per service and step. The API reads one project at a time.
func (r *OrgReader) SearchResourceUse(ctx context.Context, projectID string, from, till time.Time, step string) ([]OrgResourceUse, error) {
	// An id quoted as Go quotes it is a JSON string: ids are plain ASCII.
	native := func(name, value string) body.EsSearchNativeItem {
		return body.EsSearchNativeItem{Name: types.NewString(name), Operator: types.NewString("eq"), Value: types.JsonRawMessage(strconv.Quote(value))}
	}
	resp, err := r.z.handler.PostStatsHistoryGroupBySearch(ctx, body.EsStatsHistoryFilter{
		Search:      body.EsStatsHistoryFilterSearch{native("clientId", r.orgID), native("projectId", projectID)},
		From:        types.NewDateTimeNull(from.UTC()),
		Till:        types.NewDateTimeNull(till.UTC()),
		TimeZone:    types.NewString("UTC"),
		GroupBy:     enum.EsStatsHistoryGroupByEnumServiceStackId,
		TimeGroupBy: types.NewString(step),
	})
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	use := make([]OrgResourceUse, 0, len(out.Items))
	for _, s := range out.Items {
		if s.ProjectId.String() != projectID {
			continue
		}
		containers, _ := s.ContainerCount.Get()
		use = append(use, OrgResourceUse{
			ProjectID: s.ProjectId.String(), ServiceID: s.ServiceStackId.String(),
			From: s.From.Native(), Till: s.Till.Native(), Containers: containers.Native(),
			CPUUsed: s.CpuUsed.Native(), CPULimit: s.CpuLimit.Native(),
			VCPUUsed: s.VCpuUsed.Native(), VCPULimit: s.VCpuLimit.Native(),
			RAMUsed: s.RamUsed.Native(), RAMLimit: s.RamLimit.Native(),
			DiskUsed: s.DiskUsed.Native(), DiskLimit: s.DiskLimit.Native(),
		})
	}
	return use, nil
}

// OrgPortRouting is a public port of a project: a port on its IPv4 or IPv6
// forwarded to one of a service's ports, behind a firewall.
type OrgPortRouting struct {
	ID, ProjectID, ServiceID string
	// IPType is IP_V4 or IP_V6.
	IPType       string
	PublicPort   int
	InternalPort int
	Protocol     string
	// FirewallPolicy is BLACKLIST or WHITELIST over FirewallRanges.
	FirewallPolicy string
	FirewallRanges []string
	// Synced is false while a change waits to be applied.
	Synced bool
}

// ListPortRoutings is a project's public ports.
func (r *OrgReader) ListPortRoutings(ctx context.Context, projectID string) ([]OrgPortRouting, error) {
	resp, err := r.z.handler.PostPublicPortRoutingSearch(ctx, r.orgFilter(orgSearchPage, eq("projectId", projectID)))
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	routings := make([]OrgPortRouting, 0, len(out.Items))
	for _, p := range out.Items {
		if p.ClientId.TypedString().String() != r.orgID || p.ProjectId.TypedString().String() != projectID {
			continue
		}
		routings = append(routings, OrgPortRouting{
			ID: p.Id.TypedString().String(), ProjectID: projectID, ServiceID: p.ServiceStackId.TypedString().String(),
			IPType: string(p.PublicIpType), PublicPort: p.PublicPort.Native(), InternalPort: p.InternalPort.Native(),
			Protocol: string(p.InternalProtocol), FirewallPolicy: string(p.FirewallPolicy),
			FirewallRanges: p.FirewallIpRanges.Native(), Synced: p.IsSynced.Native(),
		})
	}
	return routings, nil
}

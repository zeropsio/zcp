package ops

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// What org mode reads of the organization's money and machines: its costs
// and its services' resource use (docs/spec-mate.md §2.11).

// OrgCostSummary is a project's or a service's cost today and over this and
// last month, in the organization's billing currency: the calendar periods,
// which hold for a service too (platform.OrgPeriodCost).
type OrgCostSummary struct {
	Today     float64 `json:"today"`
	ThisMonth float64 `json:"thisMonth"`
	LastMonth float64 `json:"lastMonth"`
}

// OrgCostPeriods is a total over each of the platform's periods.
type OrgCostPeriods struct {
	Today      float64 `json:"today"`
	Yesterday  float64 `json:"yesterday"`
	Last7Days  float64 `json:"last7Days"`
	Last30Days float64 `json:"last30Days"`
	ThisMonth  float64 `json:"thisMonth"`
	LastMonth  float64 `json:"lastMonth"`
	ThisYear   float64 `json:"thisYear"`
	LastYear   float64 `json:"lastYear"`
}

// OrgProjectCost is one project's cost.
type OrgProjectCost struct {
	OrgProjectRef
	OrgCostSummary
}

// OrgServiceCost is one service's cost.
type OrgServiceCost struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	OrgCostSummary
}

// OrgCostParts is what a cost is made of.
type OrgCostParts struct {
	CPU           float64 `json:"cpu,omitempty"`
	VCPU          float64 `json:"vCpu,omitempty"`
	RAM           float64 `json:"ram,omitempty"`
	Disk          float64 `json:"disk,omitempty"`
	Backups       float64 `json:"backups,omitempty"`
	BuildTime     float64 `json:"buildTime,omitempty"`
	Egress        float64 `json:"egress,omitempty"`
	ObjectStorage float64 `json:"objectStorage,omitempty"`
}

// OrgCostRangeRow is one project's or one service's cost over a range;
// what it is made of only for a service.
type OrgCostRangeRow struct {
	ID    string        `json:"id"`
	Name  string        `json:"name"`
	Total float64       `json:"total"`
	Parts *OrgCostParts `json:"parts,omitempty"`
}

// OrgCostDay is a day's total.
type OrgCostDay struct {
	Date  string  `json:"date"`
	Total float64 `json:"total"`
}

// OrgCostRange is a range's cost: its total, each day's total, and a row per
// project — or per service of one project, with what each is made of.
type OrgCostRange struct {
	From  string            `json:"from"`
	Till  string            `json:"till"`
	Total float64           `json:"total"`
	Parts *OrgCostParts     `json:"parts,omitempty"`
	Days  []OrgCostDay      `json:"days"`
	Rows  []OrgCostRangeRow `json:"rows"`
}

// OrgCostsResult is zerops_org_costs's answer: the platform's periods, or a
// range when one was asked for.
type OrgCostsResult struct {
	Project *OrgProjectRef `json:"project,omitempty"`
	// Note says when the answer holds only the person's own projects.
	Note     string           `json:"note,omitempty"`
	Total    *OrgCostPeriods  `json:"total,omitempty"`
	Projects []OrgProjectCost `json:"projects,omitempty"`
	Services []OrgServiceCost `json:"services,omitempty"`
	// Rest is the project's cost its current services do not account for:
	// services since deleted, and the project's own.
	Rest  *OrgCostSummary `json:"rest,omitempty"`
	Range *OrgCostRange   `json:"range,omitempty"`
}

// OrgCostsInput is what zerops_org_costs reads.
type OrgCostsInput struct {
	// Project narrows the answer to one project's services.
	Project string
	// From and Till ask for a range (dates, UTC; Till inclusive, default
	// today) instead of the platform's periods.
	From, Till string
	Now        time.Time
}

// orgCostRangeMax bounds a range read a day at a time: a month of an
// organization's projects is over a megabyte of lines.
const orgCostRangeMax = 31

// OrgCosts reads the organization's costs, or one project's.
func OrgCosts(ctx context.Context, src OrgSource, in OrgCostsInput) (*OrgCostsResult, error) {
	snap, err := readOrgSnapshot(ctx, src)
	if err != nil {
		return nil, err
	}
	var project *orgProject
	if strings.TrimSpace(in.Project) != "" {
		if project, err = snap.resolve(in.Project); err != nil {
			return nil, err
		}
	}
	result := &OrgCostsResult{Note: financesNote(ctx, src)}
	if project != nil {
		ref := project.ref()
		result.Project = &ref
	}
	if strings.TrimSpace(in.From) != "" || strings.TrimSpace(in.Till) != "" {
		result.Range, err = orgCostRange(ctx, src, snap, project, in)
		return result, err
	}
	costs, err := src.SearchCosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the org's costs: %w", err)
	}
	if project == nil {
		total := periodsOf(costs.Org)
		result.Total = &total
		// Projects since deleted still cost what they ran; they are one row.
		var deleted platform.OrgPeriodCost
		gone := 0
		for _, item := range costs.Projects {
			if isFree(item.Cost) {
				continue
			}
			p, ok := snap.byID[item.ID]
			if !ok {
				deleted = addPeriods(deleted, item.Cost)
				gone++
				continue
			}
			result.Projects = append(result.Projects, OrgProjectCost{OrgProjectRef: snap.project(p).ref(), OrgCostSummary: summaryOf(item.Cost)})
		}
		sort.SliceStable(result.Projects, func(i, j int) bool {
			return costsBefore(result.Projects[i].OrgCostSummary, result.Projects[j].OrgCostSummary, result.Projects[i].Name, result.Projects[j].Name)
		})
		if gone > 0 {
			result.Projects = append(result.Projects, OrgProjectCost{
				OrgProjectRef:  OrgProjectRef{Name: plural(gone, "deleted project")},
				OrgCostSummary: summaryOf(deleted),
			})
		}
		return result, nil
	}
	for _, item := range costs.Projects {
		if item.ID == project.ID {
			total := periodsOf(item.Cost)
			result.Total = &total
		}
	}
	if result.Total == nil {
		result.Total = &OrgCostPeriods{}
	}
	hostnames := map[string]string{}
	for _, s := range project.Services {
		hostnames[s.ID] = s.Name
	}
	rest := OrgCostSummary{Today: result.Total.Today, ThisMonth: result.Total.ThisMonth, LastMonth: result.Total.LastMonth}
	for _, item := range costs.Services {
		hostname, ok := hostnames[item.ID]
		if !ok || isFree(item.Cost) {
			continue
		}
		summary := summaryOf(item.Cost)
		result.Services = append(result.Services, OrgServiceCost{ID: item.ID, Hostname: hostname, OrgCostSummary: summary})
		rest = OrgCostSummary{Today: rest.Today - summary.Today, ThisMonth: rest.ThisMonth - summary.ThisMonth, LastMonth: rest.LastMonth - summary.LastMonth}
	}
	sort.SliceStable(result.Services, func(i, j int) bool {
		return costsBefore(result.Services[i].OrgCostSummary, result.Services[j].OrgCostSummary, result.Services[i].Hostname, result.Services[j].Hostname)
	})
	if rest = (OrgCostSummary{Today: money(rest.Today), ThisMonth: money(rest.ThisMonth), LastMonth: money(rest.LastMonth)}); rest != (OrgCostSummary{}) {
		result.Rest = &rest
	}
	return result, nil
}

// financesNote says when the person reads only their own projects' costs; a
// person not read says nothing.
func financesNote(ctx context.Context, src OrgSource) string {
	person, err := src.GetPerson(ctx)
	if err != nil || person.CanViewFinances {
		return ""
	}
	return "Only the projects " + nameOr(person.Name, "your person") + " reaches: they do not see the organization's finances, so the total is theirs alone."
}

func nameOr(name, fallback string) string {
	if strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}

// orgCostRange reads a range a day at a time: per project, or per service of
// one project.
func orgCostRange(ctx context.Context, src OrgSource, snap *orgSnapshot, project *orgProject, in OrgCostsInput) (*OrgCostRange, error) {
	now := in.Now.UTC()
	today := now.Truncate(24 * time.Hour)
	from, err := parseOrgDate(in.From, today)
	if err != nil {
		return nil, err
	}
	last, err := parseOrgDate(in.Till, today)
	if err != nil {
		return nil, err
	}
	if last.After(today) {
		last = today
	}
	// A range ends on a day's end, today's too: the API answers a day so far
	// only so, and refuses a step that is not whole.
	till := last.AddDate(0, 0, 1)
	if !from.Before(till) {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("from %s is not before till %s", from.Format(time.DateOnly), last.Format(time.DateOnly)), "Pass from on or before till")
	}
	if till.Sub(from) > orgCostRangeMax*24*time.Hour {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("a range is at most %d days", orgCostRangeMax), "Narrow the range, or omit from for this and last month and year")
	}
	q := platform.OrgCostQuery{From: from, Till: till, Step: platform.OrgStepDay}
	if project != nil {
		q.ProjectID = project.ID
	}
	lines, err := src.SearchCostBreakdown(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("read the org's costs over the range: %w", err)
	}
	out := &OrgCostRange{From: from.Format(time.DateOnly), Till: last.Format(time.DateOnly), Days: []OrgCostDay{}, Rows: []OrgCostRangeRow{}}
	rows := map[string]*OrgCostRangeRow{}
	parts := map[string]*OrgCostParts{}
	var whole OrgCostParts
	days := map[string]float64{}
	for _, l := range lines {
		if project != nil && l.ProjectID != project.ID {
			continue
		}
		id, name := l.ProjectID, l.ProjectName
		if project != nil {
			id, name = l.ServiceID, l.ServiceName
		} else if p, ok := snap.byID[l.ProjectID]; ok {
			name = p.Name
		}
		row := rows[id]
		if row == nil {
			row, parts[id] = &OrgCostRangeRow{ID: id, Name: name}, &OrgCostParts{}
			rows[id] = row
		}
		row.Total += l.Total
		addParts(parts[id], l)
		out.Total += l.Total
		addParts(&whole, l)
		days[l.From.UTC().Format(time.DateOnly)] += l.Total
	}
	for id, row := range rows {
		if money(row.Total) == 0 {
			continue
		}
		row.Total = money(row.Total)
		if project != nil {
			p := roundParts(*parts[id])
			row.Parts = &p
		}
		out.Rows = append(out.Rows, *row)
	}
	sort.SliceStable(out.Rows, func(i, j int) bool {
		if out.Rows[i].Total != out.Rows[j].Total {
			return out.Rows[i].Total > out.Rows[j].Total
		}
		return out.Rows[i].Name < out.Rows[j].Name
	})
	for day, total := range days {
		out.Days = append(out.Days, OrgCostDay{Date: day, Total: money(total)})
	}
	sort.Slice(out.Days, func(i, j int) bool { return out.Days[i].Date < out.Days[j].Date })
	out.Total = money(out.Total)
	if project != nil {
		p := roundParts(whole)
		out.Parts = &p
	}
	return out, nil
}

// parseOrgDate reads a date (2026-10-01) or a time (RFC3339) as its UTC day;
// empty is today.
func parseOrgDate(raw string, today time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return today, nil
	}
	if t, err := time.Parse(time.DateOnly, raw); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Truncate(24 * time.Hour), nil
	}
	return time.Time{}, platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("%q is not a date", raw), "Use a date like 2026-10-01")
}

func addParts(p *OrgCostParts, l platform.OrgCostLine) {
	p.CPU += l.CPU
	p.VCPU += l.VCPU
	p.RAM += l.RAM
	p.Disk += l.Disk
	p.Backups += l.Backups
	p.BuildTime += l.BuildTime
	p.Egress += l.Egress
	p.ObjectStorage += l.ObjectStorage
}

func roundParts(p OrgCostParts) OrgCostParts {
	return OrgCostParts{
		CPU: money(p.CPU), VCPU: money(p.VCPU), RAM: money(p.RAM), Disk: money(p.Disk),
		Backups: money(p.Backups), BuildTime: money(p.BuildTime), Egress: money(p.Egress), ObjectStorage: money(p.ObjectStorage),
	}
}

// money rounds an amount to four decimals: a small service costs fractions
// of a cent a day.
func money(x float64) float64 { return math.Round(x*1e4) / 1e4 }

func periodsOf(c platform.OrgPeriodCost) OrgCostPeriods {
	return OrgCostPeriods{
		Today: money(c.Today), Yesterday: money(c.Yesterday), Last7Days: money(c.Last7Days), Last30Days: money(c.Last30Days),
		ThisMonth: money(c.ThisMonth), LastMonth: money(c.LastMonth), ThisYear: money(c.ThisYear), LastYear: money(c.LastYear),
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func addPeriods(a, b platform.OrgPeriodCost) platform.OrgPeriodCost {
	return platform.OrgPeriodCost{
		Today: a.Today + b.Today, Yesterday: a.Yesterday + b.Yesterday,
		ThisMonth: a.ThisMonth + b.ThisMonth, LastMonth: a.LastMonth + b.LastMonth,
		ThisYear: a.ThisYear + b.ThisYear, LastYear: a.LastYear + b.LastYear,
		Last24Hours: a.Last24Hours + b.Last24Hours, Last7Days: a.Last7Days + b.Last7Days,
		Last30Days: a.Last30Days + b.Last30Days, Last12Months: a.Last12Months + b.Last12Months,
	}
}

func summaryOf(c platform.OrgPeriodCost) OrgCostSummary {
	return OrgCostSummary{Today: money(c.Today), ThisMonth: money(c.ThisMonth), LastMonth: money(c.LastMonth)}
}

// isFree reports a cost that rounds to nothing over every period.
func isFree(c platform.OrgPeriodCost) bool {
	return periodsOf(c) == OrgCostPeriods{}
}

// costsBefore orders by this month's cost, then last month's, then name.
func costsBefore(a, b OrgCostSummary, nameA, nameB string) bool {
	if a.ThisMonth != b.ThisMonth {
		return a.ThisMonth > b.ThisMonth
	}
	if a.LastMonth != b.LastMonth {
		return a.LastMonth > b.LastMonth
	}
	return strings.ToLower(nameA) < strings.ToLower(nameB)
}

// OrgUse is one resource's use over a range: in the last step, at its peak
// step, on average, and its limit in the last step.
type OrgUse struct {
	Last    float64 `json:"last"`
	Peak    float64 `json:"peak"`
	Average float64 `json:"average"`
	Limit   float64 `json:"limit"`
}

// OrgUseStep is one service's use in one step.
type OrgUseStep struct {
	From       string  `json:"from"`
	Containers int     `json:"containers"`
	CPUCores   float64 `json:"cpuCores,omitempty"`
	VCPU       float64 `json:"vCpu,omitempty"`
	RAMGB      float64 `json:"ramGb"`
	DiskGB     float64 `json:"diskGb"`
}

// OrgServiceUse is one service's resource use: dedicated CPU cores or
// shared vCPUs (whichever it runs on), RAM and disk in GB.
type OrgServiceUse struct {
	ID         string       `json:"id"`
	Hostname   string       `json:"hostname"`
	Containers int          `json:"containers"`
	CPUCores   *OrgUse      `json:"cpuCores,omitempty"`
	VCPU       *OrgUse      `json:"vCpu,omitempty"`
	RAMGB      OrgUse       `json:"ramGb"`
	DiskGB     OrgUse       `json:"diskGb"`
	Steps      []OrgUseStep `json:"steps,omitempty"`
}

// OrgUsageResult is a project's services' resource use over a range, read
// a step (hour or day) at a time.
type OrgUsageResult struct {
	Project  OrgProjectRef   `json:"project"`
	From     string          `json:"from"`
	Till     string          `json:"till"`
	Step     string          `json:"step"`
	Services []OrgServiceUse `json:"services"`
}

// OrgUsageInput is what zerops_org_usage reads.
type OrgUsageInput struct {
	Project string
	// Service narrows the answer to one service, step by step.
	Service string
	// Since is how far back: 6h, 24h (default), 7d; at most 31d.
	Since string
	Now   time.Time
}

var orgUsageSince = regexp.MustCompile(`^(\d+)(h|d)$`)

// orgUsageHourly is the longest range read an hour at a time.
const orgUsageHourly = 48 * time.Hour

// OrgUsage reads a project's own services' resource use — the platform's
// system services and spent build containers left out: hourly up to 48 hours
// back, daily (whole UTC days) past that.
func OrgUsage(ctx context.Context, src OrgSource, in OrgUsageInput) (*OrgUsageResult, error) {
	since := strings.TrimSpace(in.Since)
	if since == "" {
		since = "24h"
	}
	m := orgUsageSince.FindStringSubmatch(since)
	n := 0
	if m != nil {
		n, _ = strconv.Atoi(m[1])
	}
	span := time.Duration(n) * time.Hour
	if m != nil && m[2] == "d" {
		span *= 24
	}
	if n < 1 || span > 31*24*time.Hour {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("since %q is not a span of 1h to 31d", in.Since), "Use 6h, 24h or 7d")
	}
	p, err := resolveOrgProject(ctx, src, in.Project)
	if err != nil {
		return nil, err
	}
	var only *platform.ServiceStack
	if strings.TrimSpace(in.Service) != "" {
		if only, err = p.service(in.Service); err != nil {
			return nil, err
		}
	}
	now := in.Now.UTC()
	step, unit, till := platform.OrgStepHour, "hour", now.Truncate(time.Hour)
	if span > orgUsageHourly {
		step, unit, till = platform.OrgStepDay, "day", now.Truncate(24*time.Hour)
		span = span.Truncate(24 * time.Hour)
	}
	from := till.Add(-span)
	use, err := src.SearchResourceUse(ctx, p.ID, from, till, step)
	if err != nil {
		return nil, fmt.Errorf("read %s's resource use: %w", p.Name, err)
	}
	hostnames := map[string]string{}
	for _, s := range p.Services {
		if !s.IsSystem() {
			hostnames[s.ID] = s.Name
		}
	}
	byService := map[string][]platform.OrgResourceUse{}
	for _, u := range use {
		if _, ok := hostnames[u.ServiceID]; !ok || (only != nil && u.ServiceID != only.ID) {
			continue
		}
		byService[u.ServiceID] = append(byService[u.ServiceID], u)
	}
	result := &OrgUsageResult{
		Project: p.ref(), From: from.Format(time.RFC3339), Till: till.Format(time.RFC3339), Step: unit,
		Services: []OrgServiceUse{},
	}
	for id, steps := range byService {
		sort.Slice(steps, func(i, j int) bool { return steps[i].From.Before(steps[j].From) })
		result.Services = append(result.Services, serviceUse(id, hostnames[id], steps, only != nil))
	}
	sort.Slice(result.Services, func(i, j int) bool { return result.Services[i].Hostname < result.Services[j].Hostname })
	return result, nil
}

// serviceUse sums one service's steps, oldest first; withSteps lists them.
func serviceUse(id, hostname string, steps []platform.OrgResourceUse, withSteps bool) OrgServiceUse {
	last := steps[len(steps)-1]
	measure := func(used, limit func(platform.OrgResourceUse) float64) OrgUse {
		u := OrgUse{Last: amount(used(last)), Limit: amount(limit(last))}
		sum := 0.0
		for _, s := range steps {
			u.Peak = math.Max(u.Peak, amount(used(s)))
			sum += used(s)
		}
		u.Average = amount(sum / float64(len(steps)))
		return u
	}
	everLimited := func(limit func(platform.OrgResourceUse) float64) bool {
		for _, s := range steps {
			if limit(s) > 0 {
				return true
			}
		}
		return false
	}
	out := OrgServiceUse{
		ID: id, Hostname: hostname, Containers: last.Containers,
		RAMGB:  measure(func(s platform.OrgResourceUse) float64 { return s.RAMUsed }, func(s platform.OrgResourceUse) float64 { return s.RAMLimit }),
		DiskGB: measure(func(s platform.OrgResourceUse) float64 { return s.DiskUsed }, func(s platform.OrgResourceUse) float64 { return s.DiskLimit }),
	}
	if cpuLimit := func(s platform.OrgResourceUse) float64 { return s.CPULimit }; everLimited(cpuLimit) {
		cpu := measure(func(s platform.OrgResourceUse) float64 { return s.CPUUsed }, cpuLimit)
		out.CPUCores = &cpu
	}
	if vcpuLimit := func(s platform.OrgResourceUse) float64 { return s.VCPULimit }; everLimited(vcpuLimit) {
		vcpu := measure(func(s platform.OrgResourceUse) float64 { return s.VCPUUsed }, vcpuLimit)
		out.VCPU = &vcpu
	}
	if withSteps {
		for _, s := range steps {
			out.Steps = append(out.Steps, OrgUseStep{
				From: s.From.UTC().Format(time.RFC3339), Containers: s.Containers,
				CPUCores: amount(s.CPUUsed), VCPU: amount(s.VCPUUsed), RAMGB: amount(s.RAMUsed), DiskGB: amount(s.DiskUsed),
			})
		}
	}
	return out
}

// amount rounds a resource figure to three decimals.
func amount(x float64) float64 { return math.Round(x*1e3) / 1e3 }

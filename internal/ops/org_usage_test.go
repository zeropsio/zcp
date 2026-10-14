// Tests for: internal/ops/org_usage.go and the person and public ports the
// overview and the services list carry — what org mode reads of the
// organization's money and machines (docs/spec-mate.md §2.11).
package ops

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// usageNow is the clock the cost and usage tests read at.
var usageNow = time.Date(2026, 10, 14, 15, 20, 0, 0, time.UTC)

func TestOrgOverview_SaysWhoTheChiefActsFor(t *testing.T) {
	t.Parallel()
	t.Run("their name, role and finance rights", func(t *testing.T) {
		t.Parallel()
		org := testOrg()
		org.person = &platform.OrgPerson{Name: "Dana", Role: "ADMIN", CanCreateProjects: true}
		got, err := OrgOverview(context.Background(), org)
		if err != nil {
			t.Fatal(err)
		}
		if got.ActsFor == nil || *got.ActsFor != *org.person {
			t.Fatalf("actsFor = %+v, want %+v", got.ActsFor, org.person)
		}
	})
	t.Run("an HQ that does not say still answers the overview", func(t *testing.T) {
		t.Parallel()
		got, err := OrgOverview(context.Background(), testOrg())
		if err != nil {
			t.Fatal(err)
		}
		if got.ActsFor != nil || got.Projects != 7 {
			t.Fatalf("actsFor = %+v, projects = %d", got.ActsFor, got.Projects)
		}
	})
}

func TestOrgServices_ListPublicPortsWithTheirFirewall(t *testing.T) {
	t.Parallel()
	medusaOf := func(t *testing.T, org *fakeOrg) OrgService {
		t.Helper()
		got, err := OrgServices(context.Background(), org, "pProd")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range got.Services {
			if s.Hostname == "medusa" {
				return s
			}
		}
		t.Fatalf("no medusa in %+v", got.Services)
		return OrgService{}
	}
	t.Run("each port forwarded to the service", func(t *testing.T) {
		t.Parallel()
		org := testOrg()
		org.ports = map[string][]platform.OrgPortRouting{"pProd": {{
			ServiceID: "sMedusa", IPType: "IP_V4", PublicPort: 2222, InternalPort: 22, Protocol: "tcp",
			FirewallPolicy: "WHITELIST", FirewallRanges: []string{"203.0.113.0/24"}, Synced: false,
		}}}
		want := []OrgPublicPort{{IP: "IP_V4", PublicPort: 2222, Port: 22, Protocol: "tcp", Firewall: "WHITELIST", Ranges: []string{"203.0.113.0/24"}, Pending: true}}
		if got := medusaOf(t, org).PublicPorts; !reflect.DeepEqual(got, want) {
			t.Fatalf("publicPorts = %+v, want %+v", got, want)
		}
	})
	t.Run("an HQ that does not forward them still lists the services", func(t *testing.T) {
		t.Parallel()
		if got := medusaOf(t, testOrg()).PublicPorts; got != nil {
			t.Fatalf("publicPorts = %+v, want none", got)
		}
	})
}

// costOrg is testOrg with this month's costs: production most, a Mate, a
// project since deleted, and a stage that cost nothing.
func costOrg() *fakeOrg {
	org := testOrg()
	org.person = &platform.OrgPerson{Name: "Dana", Role: "OWNER", CanViewFinances: true}
	org.costs = &platform.OrgCosts{
		Org: platform.OrgPeriodCost{Today: 0.5, ThisMonth: 13.00004, LastMonth: 20, Last30Days: 25},
		Projects: []platform.OrgCostItem{
			{ID: "pRossy", Cost: platform.OrgPeriodCost{ThisMonth: 2, Last30Days: 3}},
			{ID: "pGone", Cost: platform.OrgPeriodCost{LastMonth: 1}},
			{ID: "pStage", Cost: platform.OrgPeriodCost{}},
			{ID: "pProd", Cost: platform.OrgPeriodCost{Today: 0.4, ThisMonth: 10, LastMonth: 12, Last30Days: 18}},
		},
		Services: []platform.OrgCostItem{
			{ID: "sDB", Cost: platform.OrgPeriodCost{ThisMonth: 3, Last30Days: 5}},
			{ID: "sMedusa", Cost: platform.OrgPeriodCost{Today: 0.3, ThisMonth: 6, LastMonth: 9, Last30Days: 11}},
			{ID: "sRossyApp", Cost: platform.OrgPeriodCost{ThisMonth: 1}},
		},
	}
	return org
}

func TestOrgCosts_EveryProjectMostExpensiveFirst(t *testing.T) {
	t.Parallel()
	got, err := OrgCosts(context.Background(), costOrg(), OrgCostsInput{Now: usageNow})
	if err != nil {
		t.Fatal(err)
	}
	wantTotal := OrgCostPeriods{Today: 0.5, ThisMonth: 13, LastMonth: 20, Last30Days: 25}
	if got.Total == nil || *got.Total != wantTotal {
		t.Fatalf("total = %+v, want %+v", got.Total, wantTotal)
	}
	names := make([]string, 0, len(got.Projects))
	for _, p := range got.Projects {
		names = append(names, p.Name)
	}
	if want := []string{"Contoso - production", "Northwind Shop - Rossy", "1 deleted project"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("projects = %v, want %v (a free one left out, deleted ones last as one)", names, want)
	}
	if gone := got.Projects[2]; gone.ID != "" || gone.LastMonth != 1 {
		t.Errorf("deleted projects = %+v", gone)
	}
	if got.Projects[0].Kind != OrgKindProduction || got.Projects[0].ThisMonth != 10 || got.Projects[0].Today != 0.4 {
		t.Errorf("production = %+v", got.Projects[0])
	}
	if got.Note != "" || got.Services != nil || got.Range != nil {
		t.Errorf("note=%q services=%v range=%v", got.Note, got.Services, got.Range)
	}
}

func TestOrgCosts_SayWhenTheyAreOnlyThePersonsProjects(t *testing.T) {
	t.Parallel()
	org := costOrg()
	org.person = &platform.OrgPerson{Name: "Dana", Role: "ADMIN"}
	got, err := OrgCosts(context.Background(), org, OrgCostsInput{Now: usageNow})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Note, "Only the projects Dana reaches") {
		t.Fatalf("note = %q", got.Note)
	}
}

func TestOrgCosts_OneProjectsServicesAndWhatTheyLeaveOut(t *testing.T) {
	t.Parallel()
	got, err := OrgCosts(context.Background(), costOrg(), OrgCostsInput{Project: "Contoso - production", Now: usageNow})
	if err != nil {
		t.Fatal(err)
	}
	if got.Project == nil || got.Project.ID != "pProd" || got.Total.ThisMonth != 10 || got.Total.LastMonth != 12 {
		t.Fatalf("project=%+v total=%+v", got.Project, got.Total)
	}
	want := []OrgServiceCost{
		{ID: "sMedusa", Hostname: "medusa", OrgCostSummary: OrgCostSummary{Today: 0.3, ThisMonth: 6, LastMonth: 9}},
		{ID: "sDB", Hostname: "db", OrgCostSummary: OrgCostSummary{ThisMonth: 3}},
	}
	if !reflect.DeepEqual(got.Services, want) {
		t.Fatalf("services = %+v\nwant %+v", got.Services, want)
	}
	wantRest := OrgCostSummary{Today: 0.1, ThisMonth: 1, LastMonth: 3}
	if got.Rest == nil || *got.Rest != wantRest {
		t.Fatalf("rest = %+v, want %+v", got.Rest, wantRest)
	}
}

func TestOrgCosts_ARangeDayByDayWithWhatItIsMadeOf(t *testing.T) {
	t.Parallel()
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	lines := []platform.OrgCostLine{
		{ProjectID: "pProd", ProjectName: "Contoso - production", From: day(1), Total: 1, CPU: 0.6, RAM: 0.4},
		{ProjectID: "pProd", ProjectName: "Contoso - production", From: day(2), Total: 2, CPU: 1, Disk: 1},
		{ProjectID: "pGone", ProjectName: "Old shop", From: day(1), Total: 0.5, Backups: 0.5},
		{ProjectID: "pProd", ServiceID: "sMedusa", ServiceName: "medusa", From: day(1), Total: 0.75, CPU: 0.75},
		{ProjectID: "pProd", ServiceID: "sOld", ServiceName: "oldapi", From: day(2), Total: 0.25, RAM: 0.25},
		{ProjectID: "pRossy", ServiceID: "sRossyApp", ServiceName: "appdev", From: day(2), Total: 9},
	}
	t.Run("per project", func(t *testing.T) {
		t.Parallel()
		org := costOrg()
		org.costLines = lines
		got, err := OrgCosts(context.Background(), org, OrgCostsInput{From: "2026-10-01", Till: "2026-10-02", Now: usageNow})
		if err != nil {
			t.Fatal(err)
		}
		if q := org.asked[0].(platform.OrgCostQuery); !q.From.Equal(day(1)) || !q.Till.Equal(day(3)) || q.Step != platform.OrgStepDay || q.ProjectID != "" {
			t.Fatalf("asked %+v, want 1 Oct to 3 Oct a day at a time, per project", q)
		}
		// A line per project says nothing of what its cost is made of.
		want := &OrgCostRange{
			From: "2026-10-01", Till: "2026-10-02", Total: 3.5,
			Days: []OrgCostDay{{Date: "2026-10-01", Total: 1.5}, {Date: "2026-10-02", Total: 2}},
			Rows: []OrgCostRangeRow{
				{ID: "pProd", Name: "Contoso - production", Total: 3},
				{ID: "pGone", Name: "Old shop", Total: 0.5},
			},
		}
		if !reflect.DeepEqual(got.Range, want) {
			t.Fatalf("range = %+v\nwant %+v", got.Range, want)
		}
		if got.Total != nil || got.Projects != nil {
			t.Errorf("a range answered the periods too: %+v", got)
		}
	})
	t.Run("per service of one project, with what each is made of, deleted ones by their name", func(t *testing.T) {
		t.Parallel()
		org := costOrg()
		org.costLines = lines
		got, err := OrgCosts(context.Background(), org, OrgCostsInput{Project: "pProd", From: "2026-10-01", Now: usageNow})
		if err != nil {
			t.Fatal(err)
		}
		if q := org.asked[0].(platform.OrgCostQuery); q.ProjectID != "pProd" || !q.Till.Equal(day(15)) {
			t.Fatalf("asked %+v, want pProd's services through the end of today", q)
		}
		want := &OrgCostRange{
			From: "2026-10-01", Till: "2026-10-14", Total: 1, Parts: &OrgCostParts{CPU: 0.75, RAM: 0.25},
			Days: []OrgCostDay{{Date: "2026-10-01", Total: 0.75}, {Date: "2026-10-02", Total: 0.25}},
			Rows: []OrgCostRangeRow{
				{ID: "sMedusa", Name: "medusa", Total: 0.75, Parts: &OrgCostParts{CPU: 0.75}},
				{ID: "sOld", Name: "oldapi", Total: 0.25, Parts: &OrgCostParts{RAM: 0.25}},
			},
		}
		if !reflect.DeepEqual(got.Range, want) {
			t.Fatalf("range = %+v\nwant %+v", got.Range, want)
		}
	})
	for _, tt := range []struct{ name, from, till, wantErr string }{
		{name: "a from after its till", from: "2026-10-05", till: "2026-10-01", wantErr: "is not before"},
		{name: "more than 31 days", from: "2026-09-01", till: "2026-10-05", wantErr: "at most 31 days"},
		{name: "no date", from: "last week", wantErr: "is not a date"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			org := costOrg()
			_, err := OrgCosts(context.Background(), org, OrgCostsInput{From: tt.from, Till: tt.till, Now: usageNow})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) || len(org.asked) != 0 {
				t.Fatalf("err = %v, asked %v; want %q and nothing read", err, org.asked, tt.wantErr)
			}
		})
	}
}

func TestOrgUsage_EachServiceAgainstItsLimits(t *testing.T) {
	t.Parallel()
	hour := func(h int) time.Time { return time.Date(2026, 10, 14, h, 0, 0, 0, time.UTC) }
	use := []platform.OrgResourceUse{
		{ProjectID: "pProd", ServiceID: "sMedusa", From: hour(14), Containers: 2, VCPUUsed: 0.3, VCPULimit: 2, RAMUsed: 1.5, RAMLimit: 4, DiskUsed: 1, DiskLimit: 5},
		{ProjectID: "pProd", ServiceID: "sMedusa", From: hour(13), Containers: 1, VCPUUsed: 0.1, VCPULimit: 2, RAMUsed: 2.5, RAMLimit: 3, DiskUsed: 1, DiskLimit: 5},
		{ProjectID: "pProd", ServiceID: "sDB", From: hour(14), Containers: 1, CPUUsed: 0.5, CPULimit: 1, RAMUsed: 0.5, RAMLimit: 1, DiskUsed: 2, DiskLimit: 10},
		// The platform's own core service is not the project's.
		{ProjectID: "pProd", ServiceID: "sCore", From: hour(14), Containers: 1, VCPUUsed: 0.1, VCPULimit: 1, RAMUsed: 0.2, RAMLimit: 0.5},
	}
	medusa := OrgServiceUse{
		ID: "sMedusa", Hostname: "medusa", Containers: 2,
		VCPU:   &OrgUse{Last: 0.3, Peak: 0.3, Average: 0.2, Limit: 2},
		RAMGB:  OrgUse{Last: 1.5, Peak: 2.5, Average: 2, Limit: 4},
		DiskGB: OrgUse{Last: 1, Peak: 1, Average: 1, Limit: 5},
	}
	t.Run("every service over the last day, hour by hour", func(t *testing.T) {
		t.Parallel()
		org := testOrg()
		org.use = use
		got, err := OrgUsage(context.Background(), org, OrgUsageInput{Project: "Contoso - production", Now: usageNow})
		if err != nil {
			t.Fatal(err)
		}
		if asked := org.asked[0].([]any); !reflect.DeepEqual(asked, []any{"pProd", hour(15).Add(-24 * time.Hour), hour(15), platform.OrgStepHour}) {
			t.Fatalf("asked %v, want pProd over the 24 full hours before 15:00", asked)
		}
		db := OrgServiceUse{
			ID: "sDB", Hostname: "db", Containers: 1,
			CPUCores: &OrgUse{Last: 0.5, Peak: 0.5, Average: 0.5, Limit: 1},
			RAMGB:    OrgUse{Last: 0.5, Peak: 0.5, Average: 0.5, Limit: 1},
			DiskGB:   OrgUse{Last: 2, Peak: 2, Average: 2, Limit: 10},
		}
		if want := []OrgServiceUse{db, medusa}; !reflect.DeepEqual(got.Services, want) {
			t.Fatalf("services = %+v\nwant %+v", got.Services, want)
		}
		if got.Step != "hour" || got.Till != "2026-10-14T15:00:00Z" {
			t.Errorf("step=%s till=%s", got.Step, got.Till)
		}
	})
	t.Run("one service step by step", func(t *testing.T) {
		t.Parallel()
		org := testOrg()
		org.use = use
		got, err := OrgUsage(context.Background(), org, OrgUsageInput{Project: "pProd", Service: "medusa", Since: "6h", Now: usageNow})
		if err != nil {
			t.Fatal(err)
		}
		want := medusa
		want.Steps = []OrgUseStep{
			{From: "2026-10-14T13:00:00Z", Containers: 1, VCPU: 0.1, RAMGB: 2.5, DiskGB: 1},
			{From: "2026-10-14T14:00:00Z", Containers: 2, VCPU: 0.3, RAMGB: 1.5, DiskGB: 1},
		}
		if !reflect.DeepEqual(got.Services, []OrgServiceUse{want}) {
			t.Fatalf("services = %+v\nwant %+v", got.Services, []OrgServiceUse{want})
		}
	})
	t.Run("past 48 hours, whole days", func(t *testing.T) {
		t.Parallel()
		org := testOrg()
		if _, err := OrgUsage(context.Background(), org, OrgUsageInput{Project: "pProd", Since: "7d", Now: usageNow}); err != nil {
			t.Fatal(err)
		}
		today := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
		if asked := org.asked[0].([]any); !reflect.DeepEqual(asked, []any{"pProd", today.AddDate(0, 0, -7), today, platform.OrgStepDay}) {
			t.Fatalf("asked %v, want the 7 whole days before today", asked)
		}
	})
	for _, since := range []string{"0h", "32d", "1w", "yesterday"} {
		t.Run("refuses since "+since, func(t *testing.T) {
			t.Parallel()
			org := testOrg()
			if _, err := OrgUsage(context.Background(), org, OrgUsageInput{Project: "pProd", Since: since, Now: usageNow}); err == nil || len(org.asked) != 0 {
				t.Fatalf("since %q: err = %v, asked %v", since, err, org.asked)
			}
		})
	}
}

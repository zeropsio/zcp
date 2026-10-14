// Tests for: internal/platform/org_reader_usage.go — who the Chief acts for,
// the organization's costs, its services' resource use and its public ports
// (docs/spec-mate.md §2.11).
package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// answeringAPI answers each path suffix with its body and records the bodies
// it was sent, by path suffix.
func answeringAPI(t *testing.T, answers map[string]string) (*httptest.Server, map[string]string) {
	t.Helper()
	sent := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := io.ReadAll(r.Body)
		for suffix, answer := range answers {
			if strings.HasSuffix(r.URL.Path, suffix) {
				sent[suffix] = string(got)
				w.Header().Set("Content-Type", "application/json")
				if strings.HasPrefix(answer, "404 ") {
					w.WriteHeader(http.StatusNotFound)
					answer = strings.TrimPrefix(answer, "404 ")
				}
				_, _ = w.Write([]byte(answer))
				return
			}
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, sent
}

func throughHQ(t *testing.T, srv *httptest.Server) *OrgReader {
	t.Helper()
	r, err := OpenOrgReader(context.Background(), OrgAccess{Token: "chief-cred", OrgAPI: srv.URL + "/api/chief/zerops", OrgID: "org2"})
	if err != nil {
		t.Fatalf("OpenOrgReader: %v", err)
	}
	return r
}

func TestOrgReader_GetPerson_IsWhoTheChiefActsFor(t *testing.T) {
	t.Parallel()
	person := OrgPerson{Name: "Dana", Role: "ADMIN", CanCreateProjects: true}
	t.Run("through HQ, HQ's own answer", func(t *testing.T) {
		t.Parallel()
		srv, _ := answeringAPI(t, map[string]string{
			"/chief/person": `{"name":"Dana","roleCode":"ADMIN","canViewFinances":false,"canEditFinances":false,"canCreateProjects":true}`,
		})
		got, err := throughHQ(t, srv).GetPerson(context.Background())
		if err != nil || got != person {
			t.Fatalf("GetPerson = %+v, %v; want %+v", got, err, person)
		}
	})
	t.Run("through an HQ that does not answer it, an error", func(t *testing.T) {
		t.Parallel()
		srv, _ := answeringAPI(t, map[string]string{
			"/chief/person": `404 {"error":{"code":"chiefReadRefused","message":"not a read a Chief makes"}}`,
		})
		if _, err := throughHQ(t, srv).GetPerson(context.Background()); err == nil {
			t.Fatal("GetPerson answered an HQ that refused it")
		}
	})
	t.Run("through the region's API, the token's membership in the org", func(t *testing.T) {
		t.Parallel()
		srv, _ := answeringAPI(t, map[string]string{
			"/user/info": `{"id":"u1","fullName":"Dana","clientUserList":[` +
				`{"id":"cu1","clientId":"org1","roleCode":"OWNER","canViewFinances":true},` +
				`{"id":"cu2","clientId":"org2","roleCode":"ADMIN","canViewFinances":false,"canCreateProjects":true}]}`,
		})
		r, err := OpenOrgReader(context.Background(), OrgAccess{Token: "tok", APIHost: srv.URL, OrgID: "org2"})
		if err != nil {
			t.Fatalf("OpenOrgReader: %v", err)
		}
		got, err := r.GetPerson(context.Background())
		if err != nil || got != person {
			t.Fatalf("GetPerson = %+v, %v; want %+v", got, err, person)
		}
	})
}

func TestOrgReader_SearchCosts_ReadsEachPeriod(t *testing.T) {
	t.Parallel()
	srv, sent := answeringAPI(t, map[string]string{
		"/transaction-debit/cost-search": `{
			"client":{"id":"org2","periodCost":{"today":1.5,"thisMonth":42.25,"lastMonth":30,"averageLast30Days":1.2,"averageLast30DaysPrecision":12}},
			"project":[{"id":"p1","periodCost":{"thisMonth":40}}],
			"stack":[{"id":"s1","periodCost":{"thisMonth":12.5}}]}`,
	})
	got, err := throughHQ(t, srv).SearchCosts(context.Background())
	if err != nil {
		t.Fatalf("SearchCosts: %v", err)
	}
	want := &OrgCosts{
		Org:      OrgPeriodCost{Today: 1.5, ThisMonth: 42.25, LastMonth: 30},
		Projects: []OrgCostItem{{ID: "p1", Cost: OrgPeriodCost{ThisMonth: 40}}},
		Services: []OrgCostItem{{ID: "s1", Cost: OrgPeriodCost{ThisMonth: 12.5}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SearchCosts = %+v\nwant %+v", got, want)
	}
	if !strings.Contains(sent["/transaction-debit/cost-search"], `"org2"`) {
		t.Fatalf("cost search not pinned to org2: %s", sent["/transaction-debit/cost-search"])
	}
}

func TestOrgReader_SearchCostBreakdown_OneProjectsServices(t *testing.T) {
	t.Parallel()
	srv, sent := answeringAPI(t, map[string]string{
		"/transaction-debit/group-by-search": `{"items":[
			{"projectId":"p1","projectName":"Shop - production","stackId":"s1","stackName":"api","from":"2026-10-01T00:00:00Z","sumTotalPrice":0.45,"cpu":0.4,"ram":0.05},
			{"projectId":"p9","projectName":"Elsewhere","stackId":"s9","stackName":"x","from":"2026-10-01T00:00:00Z","sumTotalPrice":3}],
			"totalHits":2,"groupBy":"stackId","timeGroupBy":"1d"}`,
	})
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got, err := throughHQ(t, srv).SearchCostBreakdown(context.Background(), OrgCostQuery{From: from, Till: from.AddDate(0, 0, 1), Step: OrgStepDay, ProjectID: "p1"})
	if err != nil {
		t.Fatalf("SearchCostBreakdown: %v", err)
	}
	if len(got) != 1 || got[0].ServiceID != "s1" || got[0].ServiceName != "api" || got[0].CPU != 0.4 {
		t.Fatalf("lines = %+v, want s1's alone", got)
	}
	asked := sent["/transaction-debit/group-by-search"]
	for _, part := range []string{`"groupBy":"stackId"`, `"projectId"`, `"p1"`, `"org2"`} {
		if !strings.Contains(asked, part) {
			t.Errorf("breakdown lacks %s: %s", part, asked)
		}
	}
}

func TestOrgReader_SearchCostBreakdown_SendsItsRangeAndReadsEachLine(t *testing.T) {
	t.Parallel()
	srv, sent := answeringAPI(t, map[string]string{
		"/transaction-debit/group-by-search": `{"items":[
			{"projectId":"p1","projectName":"Shop - production","stackId":null,"stackName":null,
			 "from":"2026-10-01T00:00:00Z","till":"2026-10-01T23:59:59Z","sumTotalPrice":1.25,
			 "cpu":0.5,"vCpu":0,"ram":0.5,"disc":0.1,"backupStorage":0.05,"buildTime":0.1,"masterEgress":0,"objectStorage":0}],
			"totalHits":1,"groupBy":"projectId","timeGroupBy":"1d"}`,
	})
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got, err := throughHQ(t, srv).SearchCostBreakdown(context.Background(), OrgCostQuery{From: from, Till: from.AddDate(0, 0, 13), Step: OrgStepDay})
	if err != nil {
		t.Fatalf("SearchCostBreakdown: %v", err)
	}
	want := []OrgCostLine{{
		ProjectID: "p1", ProjectName: "Shop - production",
		From: from, Till: time.Date(2026, 10, 1, 23, 59, 59, 0, time.UTC), Total: 1.25,
		CPU: 0.5, RAM: 0.5, Disk: 0.1, Backups: 0.05, BuildTime: 0.1,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SearchCostBreakdown = %+v\nwant %+v", got, want)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(sent["/transaction-debit/group-by-search"]), &body); err != nil {
		t.Fatal(err)
	}
	for field, value := range map[string]any{"from": "2026-10-01T00:00:00Z", "till": "2026-10-14T00:00:00Z", "timeZone": "UTC", "groupBy": "projectId", "timeGroupBy": "1d"} {
		if body[field] != value {
			t.Errorf("sent %s = %v, want %v", field, body[field], value)
		}
	}
}

func TestOrgReader_SearchResourceUse_ReadsOneProjectsServices(t *testing.T) {
	t.Parallel()
	srv, sent := answeringAPI(t, map[string]string{
		"/stats-history/group-by-search": `{"items":[
			{"projectId":"p1","serviceStackId":"s1","from":"2026-10-14T13:00:00Z","till":"2026-10-14T13:59:59Z",
			 "containerCount":1,"cpuUsed":0,"cpuLimit":0,"vCpuUsed":0.095,"vCpuLimit":2,"ramUsed":0.993,"ramLimit":3.25,"diskUsed":1.822,"diskLimit":2.5},
			{"projectId":"pOther","serviceStackId":"s9","from":"2026-10-14T13:00:00Z","till":"2026-10-14T13:59:59Z"}],
			"totalHits":2,"groupBy":"serviceStackId","timeGroupBy":"1h"}`,
	})
	till := time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC)
	got, err := throughHQ(t, srv).SearchResourceUse(context.Background(), "p1", till.Add(-2*time.Hour), till, OrgStepHour)
	if err != nil {
		t.Fatalf("SearchResourceUse: %v", err)
	}
	want := []OrgResourceUse{{
		ProjectID: "p1", ServiceID: "s1",
		From: time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC), Till: time.Date(2026, 10, 14, 13, 59, 59, 0, time.UTC),
		Containers: 1, VCPUUsed: 0.095, VCPULimit: 2, RAMUsed: 0.993, RAMLimit: 3.25, DiskUsed: 1.822, DiskLimit: 2.5,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SearchResourceUse = %+v\nwant %+v", got, want)
	}
	asked := sent["/stats-history/group-by-search"]
	for _, part := range []string{`"projectId"`, `"p1"`, `"clientId"`, `"org2"`, `"from":"2026-10-14T13:00:00Z"`, `"till":"2026-10-14T15:00:00Z"`, `"groupBy":"serviceStackId"`} {
		if !strings.Contains(asked, part) {
			t.Errorf("stats search lacks %s: %s", part, asked)
		}
	}
}

func TestOrgReader_ListPortRoutings_ReadsOneProjectsPublicPorts(t *testing.T) {
	t.Parallel()
	srv, _ := answeringAPI(t, map[string]string{
		"/public-port-routing/search": `{"items":[
			{"id":"r1","clientId":"org2","projectId":"p1","serviceStackId":"s1","publicIpType":"IP_V4","publicPort":5432,"internalPort":5432,
			 "internalProtocol":"tcp","firewallPolicy":"WHITELIST","firewallIpRanges":["203.0.113.0/24"],"isSynced":true},
			{"id":"r2","clientId":"org1","projectId":"p1","serviceStackId":"s1","publicPort":22}],
			"totalHits":2,"limit":1000,"offset":0}`,
	})
	got, err := throughHQ(t, srv).ListPortRoutings(context.Background(), "p1")
	if err != nil {
		t.Fatalf("ListPortRoutings: %v", err)
	}
	want := []OrgPortRouting{{
		ID: "r1", ProjectID: "p1", ServiceID: "s1", IPType: "IP_V4", PublicPort: 5432, InternalPort: 5432,
		Protocol: "tcp", FirewallPolicy: "WHITELIST", FirewallRanges: []string{"203.0.113.0/24"}, Synced: true,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListPortRoutings = %+v\nwant %+v", got, want)
	}
}

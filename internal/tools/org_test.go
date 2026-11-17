// Tests for: internal/tools/org.go — org mode's MCP tools
// (docs/spec-mate.md §2.11).
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// orgFake is a two-project organization: a production environment and a
// Mate.
type orgFake struct{}

func (orgFake) ListProjects(context.Context) ([]platform.Project, error) {
	return []platform.Project{
		{ID: "pProd", Name: "Contoso - production", Status: "ACTIVE"},
		{ID: "pRossy", Name: "Northwind Shop - Rossy", Status: "ACTIVE", Tags: []string{"mate"}},
	}, nil
}
func (orgFake) ListServices(_ context.Context, projectID string) ([]platform.ServiceStack, error) {
	all := []platform.ServiceStack{
		{ID: "sMedusa", ProjectID: "pProd", Name: "medusa", Status: "ACTIVE", Ports: []platform.Port{{Port: 9000, Protocol: "tcp", Scheme: "http"}}, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		{ID: "sZcp", ProjectID: "pRossy", Name: "zcp", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "zcp@1", ServiceStackTypeCategoryName: "USER"}},
	}
	var out []platform.ServiceStack
	for _, s := range all {
		if projectID == "" || s.ProjectID == projectID {
			out = append(out, s)
		}
	}
	return out, nil
}
func (orgFake) ListMembers(context.Context) ([]platform.OverviewMember, error) {
	return []platform.OverviewMember{{UserID: "u1", FullName: "Dana", Role: "ADMIN", Status: "ACTIVE"}}, nil
}

var _ ops.OrgSource = orgFake{}

func orgServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterOrg(srv, orgFake{}, func() string { return "id-1" })
	return srv
}

type orgWithHQNamedService struct{ orgFake }

func (orgWithHQNamedService) ListProjects(ctx context.Context) ([]platform.Project, error) {
	projects, err := (orgFake{}).ListProjects(ctx)
	return append(projects, platform.Project{ID: "pBackup", Name: "HQ backup", Status: "STOPPED"}), err
}

func (orgWithHQNamedService) ListServices(ctx context.Context, projectID string) ([]platform.ServiceStack, error) {
	services, err := (orgFake{}).ListServices(ctx, projectID)
	if projectID == "" || projectID == "pBackup" {
		services = append(services, platform.ServiceStack{ID: "sBackup", ProjectID: "pBackup", Name: "hq", Status: "STOPPED"})
	}
	return services, err
}

func TestOrgTools_HQNamedService_IsAnOrdinaryProject(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterOrg(srv, orgWithHQNamedService{}, func() string { return "id-1" })
	res := callTool(t, srv, "zerops_org_overview", map[string]any{})
	if res.IsError {
		t.Fatalf("overview errored: %s", extractText(res))
	}
	var overview ops.OrgOverviewResult
	if err := json.Unmarshal([]byte(extractText(res)), &overview); err != nil {
		t.Fatal(err)
	}
	for _, group := range overview.Groups {
		for _, project := range group.Projects {
			if project.ID == "pBackup" {
				if group.Kind != ops.OrgKindOther {
					t.Fatalf("project with a service named hq has kind %q, want other", group.Kind)
				}
				return
			}
		}
	}
	t.Fatal("overview omitted the ordinary project with a service named hq")
}

func TestOrgTools_HQNamedService_AccessUsesOrdinaryProjectRules(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterOrg(srv, orgWithHQNamedService{}, func() string { return "id-1" })
	res := callTool(t, srv, "zerops_org_propose_access", map[string]any{
		"project": "pBackup",
		"people":  []map[string]any{{"person": "Dana", "level": "read"}},
		"reason":  "review the backup",
	})
	if res.IsError {
		t.Fatalf("ordinary project access proposal errored: %s", extractText(res))
	}
	var doc struct {
		Proposal struct {
			Kind      string                 `json:"kind"`
			ProjectID string                 `json:"projectId"`
			Params    ops.OrgAccessSetParams `json:"params"`
		} `json:"proposal"`
	}
	if err := json.Unmarshal([]byte(extractText(res)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Proposal.Kind != "access.set" || doc.Proposal.ProjectID != "pBackup" ||
		len(doc.Proposal.Params.Projects) != 1 || doc.Proposal.Params.Projects[0].ID != "pBackup" ||
		len(doc.Proposal.Params.People) != 1 || doc.Proposal.Params.People[0] != (ops.OrgAccessPerson{Person: "Dana", Level: "read"}) {
		t.Fatalf("ordinary project access proposal = %+v", doc.Proposal)
	}
}

// TestOrgTools_EachAnswersWithItsDocument calls every org tool once and
// reads the JSON document it answers with.
func TestOrgTools_EachAnswersWithItsDocument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tool    string
		args    map[string]any
		wantKey string
		wantIn  string
		notIn   string
	}{
		{tool: "zerops_org_overview", args: map[string]any{}, wantKey: "groups", wantIn: `"mate":"Rossy"`},
		{tool: "zerops_org_overview", args: map[string]any{}, wantKey: "members", wantIn: `"role":"ADMIN"`},
		{tool: "zerops_org_services", args: map[string]any{"project": "Contoso - production"}, wantKey: "services", wantIn: `"hostname":"medusa"`},
		{tool: "zerops_org_propose_service", args: map[string]any{"action": "restart", "project": "pProd", "service": "medusa", "reason": "stuck"}, wantKey: "proposal", wantIn: `"kind":"service.restart"`},
		{tool: "zerops_org_propose_env", args: map[string]any{"project": "pProd", "scope": "project", "key": "APP_URL", "value": "https://x", "reason": "domain"}, wantKey: "proposal", wantIn: `"kind":"env.set"`},
		{tool: "zerops_org_propose_message", args: map[string]any{"to": "Rossy", "words": "debug this", "reason": "Rossy's code"}, wantKey: "proposal", wantIn: `"mateName":"Rossy"`},
		{tool: "zerops_org_propose_domain", args: map[string]any{"project": "pProd", "service": "medusa", "domains": []string{"contoso.com", "www.contoso.com"}, "port": 9000, "reason": "the shop's own name"}, wantKey: "proposal", wantIn: `"params":{"domains":["contoso.com","www.contoso.com"],"port":9000}`},
		{tool: "zerops_org_propose_access", args: map[string]any{"project": "Contoso - production", "people": []map[string]any{{"person": "dana@example.com", "level": "full"}}, "reason": "she works on the shop"}, wantKey: "proposal", wantIn: `"people":[{"person":"dana@example.com","level":"full"}]`},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			t.Parallel()
			res := callTool(t, orgServer(), tt.tool, tt.args)
			text := extractText(res)
			if res.IsError {
				t.Fatalf("%s errored: %s", tt.tool, text)
			}
			if res.StructuredContent != nil {
				t.Errorf("%s returned structuredContent", tt.tool)
			}
			var doc map[string]json.RawMessage
			if err := json.Unmarshal([]byte(text), &doc); err != nil {
				t.Fatalf("%s: not one JSON document: %v\n%s", tt.tool, err, text)
			}
			if _, ok := doc[tt.wantKey]; !ok {
				t.Errorf("%s: no %q in %s", tt.tool, tt.wantKey, text)
			}
			if !strings.Contains(text, tt.wantIn) {
				t.Errorf("%s: %q not in %s", tt.tool, tt.wantIn, text)
			}
			if tt.notIn != "" && strings.Contains(text, tt.notIn) {
				t.Errorf("%s: %q leaked into %s", tt.tool, tt.notIn, text)
			}
		})
	}
}

func TestOrgTools_ProposalNoteSaysNothingRan(t *testing.T) {
	t.Parallel()
	res := callTool(t, orgServer(), "zerops_org_propose_service", map[string]any{"action": "stop", "project": "pProd", "service": "medusa", "reason": "unused"})
	var doc ops.OrgProposalResult
	if err := json.Unmarshal([]byte(extractText(res)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Note != ops.ProposalNote || doc.Proposal.ID != "id-1" || doc.Proposal.Kind != ops.ProposalServiceStop {
		t.Errorf("proposal = %+v", doc)
	}
}

func TestOrgTools_ARefusalIsAPlainError(t *testing.T) {
	t.Parallel()
	res := callTool(t, orgServer(), "zerops_org_propose_service", map[string]any{"action": "delete", "project": "pProd", "service": "medusa", "reason": "r"})
	if !res.IsError || !strings.Contains(extractText(res), "delete") {
		t.Errorf("a delete was not refused: %s", extractText(res))
	}
}

// TestOrgTools_Annotations: look tools and proposals change nothing on the
// platform, so every org tool reads as read-only to the agent's client.
func TestOrgTools_Annotations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := orgServer().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0.1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	listed, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"zerops_org_overview": true, "zerops_org_services": true,
		"zerops_org_propose_service": false, "zerops_org_propose_env": false, "zerops_org_propose_message": false,
		"zerops_org_propose_domain": false, "zerops_org_propose_access": false,
	}
	if len(listed.Tools) != len(want) {
		t.Errorf("registered %d tools, want %d", len(listed.Tools), len(want))
	}
	for _, tool := range listed.Tools {
		idempotent, ok := want[tool.Name]
		if !ok {
			t.Errorf("unexpected tool %s", tool.Name)
			continue
		}
		a := tool.Annotations
		if a == nil || a.Title == "" || !a.ReadOnlyHint || a.IdempotentHint != idempotent || a.DestructiveHint == nil || *a.DestructiveHint {
			t.Errorf("%s annotations = %+v", tool.Name, a)
		}
		if n := len(strings.Fields(tool.Description)); n > 60 {
			t.Errorf("%s description has %d words (max 60)", tool.Name, n)
		}
	}
}

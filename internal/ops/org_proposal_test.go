// Tests for: internal/ops/org_proposal.go — org mode's act tools return a
// proposal the person decides on, and run nothing (docs/spec-mate.md §2.11).
package ops

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func fixedID() string { return "11111111-2222-3333-4444-555555555555" }

// TestProposeOrgAction_Shape pins the proposal document the client builds
// its card from, field by field.
func TestProposeOrgAction_Shape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   OrgActionInput
		want string
	}{
		{
			name: "service.restart",
			in:   OrgActionInput{Kind: ProposalServiceRestart, Project: "Contoso - production", Service: "medusa", Reason: "it leaks memory"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"service.restart","summary":"Restart medusa in Contoso - production","reason":"it leaks memory","projectId":"pProd","projectName":"Contoso - production","serviceId":"sMedusa","serviceName":"medusa","params":{}},"note":"Nothing has run. The person approves or declines it on the card."}`,
		},
		{
			name: "service.start of a stopped service",
			in:   OrgActionInput{Kind: ProposalServiceStart, Project: "pHQ", Service: "runnerzit", Reason: "ZIT needs its runner"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"service.start","summary":"Start runnerzit in Headquarters","reason":"ZIT needs its runner","projectId":"pHQ","projectName":"Headquarters","serviceId":"sRunner","serviceName":"runnerzit","params":{}},"note":"Nothing has run. The person approves or declines it on the card."}`,
		},
		{
			name: "service.stop",
			in:   OrgActionInput{Kind: ProposalServiceStop, Project: "ZIT - stage", Service: "app", Reason: "unused"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"service.stop","summary":"Stop app in ZIT - stage","reason":"unused","projectId":"pStage","projectName":"ZIT - stage","serviceId":"sStage","serviceName":"app","params":{}},"note":"Nothing has run. The person approves or declines it on the card."}`,
		},
		{
			name: "env.set on a service, secret",
			in:   OrgActionInput{Kind: ProposalEnvSet, Project: "pProd", Service: "medusa", Scope: "service", Key: "STRIPE_KEY", Value: "sk_test_x", Secret: true, Reason: "rotate"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"env.set","summary":"Set STRIPE_KEY (secret) on medusa in Contoso - production","reason":"rotate","projectId":"pProd","projectName":"Contoso - production","serviceId":"sMedusa","serviceName":"medusa","params":{"scope":"service","key":"STRIPE_KEY","value":"sk_test_x","secret":true}},"note":"Nothing has run. The person approves or declines it on the card."}`,
		},
		{
			name: "env.set on a project",
			in:   OrgActionInput{Kind: ProposalEnvSet, Project: "pProd", Scope: "project", Key: "APP_URL", Value: "https://shop.example.com", Reason: "new domain"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"env.set","summary":"Set APP_URL in Contoso - production","reason":"new domain","projectId":"pProd","projectName":"Contoso - production","params":{"scope":"project","key":"APP_URL","value":"https://shop.example.com","secret":false}},"note":"Nothing has run. The person approves or declines it on the card."}`,
		},
		{
			name: "mate.message by the Mate's name, with an attachment",
			in:   OrgActionInput{Kind: ProposalMateMessage, To: "Rossy", Words: "Prod logs show a panic; please debug.", AttachmentTitle: "medusa errors", AttachmentText: "panic: nil map", Reason: "the fix belongs to Rossy's code"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"mate.message","summary":"Message Rossy in Northwind Shop - Rossy","reason":"the fix belongs to Rossy's code","projectId":"pRossy","projectName":"Northwind Shop - Rossy","params":{"to":{"projectId":"pRossy","mateName":"Rossy"},"words":"Prod logs show a panic; please debug.","attachment":{"title":"medusa errors","text":"panic: nil map"}}},"note":"Nothing has run. The person approves or declines it on the card."}`,
		},
		{
			name: "mate.message by project id, no attachment",
			in:   OrgActionInput{Kind: ProposalMateMessage, To: "pRossy", Words: "Ship the fix.", Reason: "asked"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"mate.message","summary":"Message Rossy in Northwind Shop - Rossy","reason":"asked","projectId":"pRossy","projectName":"Northwind Shop - Rossy","params":{"to":{"projectId":"pRossy","mateName":"Rossy"},"words":"Ship the fix."}},"note":"Nothing has run. The person approves or declines it on the card."}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ProposeOrgAction(context.Background(), testOrg(), fixedID, tt.in)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var gotDoc, wantDoc any
			_ = json.Unmarshal(raw, &gotDoc)
			if err := json.Unmarshal([]byte(tt.want), &wantDoc); err != nil {
				t.Fatalf("bad want: %v", err)
			}
			if !reflect.DeepEqual(gotDoc, wantDoc) {
				t.Errorf("proposal\n got %s\nwant %s", raw, tt.want)
			}
		})
	}
}

// TestProposeOrgAction_ValidatesAgainstTheOrg: a proposal names only what
// the organization really holds, and is refused with a plain error otherwise.
func TestProposeOrgAction_ValidatesAgainstTheOrg(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      OrgActionInput
		wantErr string
	}{
		{name: "an unknown kind", in: OrgActionInput{Kind: "service.delete", Project: "pProd", Service: "medusa", Reason: "r"}, wantErr: "service.delete"},
		{name: "no reason", in: OrgActionInput{Kind: ProposalServiceRestart, Project: "pProd", Service: "medusa"}, wantErr: "reason"},
		{name: "a project outside the org", in: OrgActionInput{Kind: ProposalServiceRestart, Project: "pElsewhere", Service: "medusa", Reason: "r"}, wantErr: "no project"},
		{name: "a service the project does not have", in: OrgActionInput{Kind: ProposalServiceRestart, Project: "pProd", Service: "ghost", Reason: "r"}, wantErr: "ghost"},
		{name: "a system service", in: OrgActionInput{Kind: ProposalServiceRestart, Project: "pProd", Service: "core", Reason: "r"}, wantErr: "core"},
		{name: "start of a running service", in: OrgActionInput{Kind: ProposalServiceStart, Project: "pProd", Service: "medusa", Reason: "r"}, wantErr: "already"},
		{name: "stop of a stopped service", in: OrgActionInput{Kind: ProposalServiceStop, Project: "pHQ", Service: "runnerzit", Reason: "r"}, wantErr: "already"},
		{name: "restart of a stopped service", in: OrgActionInput{Kind: ProposalServiceRestart, Project: "pHQ", Service: "runnerzit", Reason: "r"}, wantErr: "service.start"},
		{name: "env.set with a bad key", in: OrgActionInput{Kind: ProposalEnvSet, Project: "pProd", Scope: "project", Key: "1BAD-KEY", Value: "v", Reason: "r"}, wantErr: "1BAD-KEY"},
		{name: "env.set on a service scope without a service", in: OrgActionInput{Kind: ProposalEnvSet, Project: "pProd", Scope: "service", Key: "K", Value: "v", Reason: "r"}, wantErr: "service"},
		{name: "env.set on a project scope naming a service", in: OrgActionInput{Kind: ProposalEnvSet, Project: "pProd", Scope: "project", Service: "medusa", Key: "K", Value: "v", Reason: "r"}, wantErr: "scope"},
		{name: "env.set with an unknown scope", in: OrgActionInput{Kind: ProposalEnvSet, Project: "pProd", Scope: "org", Key: "K", Value: "v", Reason: "r"}, wantErr: "scope"},
		{name: "mate.message to no Mate", in: OrgActionInput{Kind: ProposalMateMessage, To: "Nobody", Words: "hi", Reason: "r"}, wantErr: "Nobody"},
		{name: "mate.message to a project that is not a Mate", in: OrgActionInput{Kind: ProposalMateMessage, To: "pProd", Words: "hi", Reason: "r"}, wantErr: "not a Mate"},
		{name: "mate.message to the Chief itself", in: OrgActionInput{Kind: ProposalMateMessage, To: "pMM", Words: "hi", Reason: "r"}, wantErr: "is the Chief"},
		{name: "mate.message without words", in: OrgActionInput{Kind: ProposalMateMessage, To: "Rossy", Words: "  ", Reason: "r"}, wantErr: "words"},
		{name: "mate.message with an empty attachment", in: OrgActionInput{Kind: ProposalMateMessage, To: "Rossy", Words: "hi", AttachmentTitle: "logs", Reason: "r"}, wantErr: "attachment"},
		{name: "mate.message with an oversized attachment", in: OrgActionInput{Kind: ProposalMateMessage, To: "Rossy", Words: "hi", AttachmentText: strings.Repeat("x", orgAttachmentMax+1), Reason: "r"}, wantErr: "attachment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ProposeOrgAction(context.Background(), testOrg(), fixedID, tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

// domainOrg is testOrg with medusa serving the web on 9000 beside a metrics
// port.
func domainOrg() *fakeOrg {
	org := testOrg()
	org.services[5].Ports = []platform.Port{
		{Port: 9000, Protocol: "tcp", Scheme: "http"},
		{Port: 9100, Protocol: "tcp", Scheme: "tcp"},
		{Port: 8080, Protocol: "tcp", HTTPSupport: true},
	}
	return org
}

// TestProposeOrgAction_DomainAdd_Shape pins the domain.add document the
// client draws its card from and carries out on approval.
func TestProposeOrgAction_DomainAdd_Shape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   OrgActionInput
		want string
	}{
		{
			name: "a domain and its www, on the service's web port",
			in:   OrgActionInput{Kind: ProposalDomainAdd, Project: "Contoso - production", Service: "medusa", Domains: []string{"contoso.com", "www.contoso.com"}, Port: 9000, Reason: "the shop is ready for its own name"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"domain.add","summary":"Put Contoso - production on contoso.com and www.contoso.com","reason":"the shop is ready for its own name","projectId":"pProd","projectName":"Contoso - production","serviceId":"sMedusa","serviceName":"medusa","params":{"domains":["contoso.com","www.contoso.com"],"port":9000}},"note":"Nothing has run. The person approves or declines it on the card."}`,
		},
		{
			name: "a domain written as people write it, on a port the platform routes",
			in:   OrgActionInput{Kind: ProposalDomainAdd, Project: "pProd", Service: "medusa", Domains: []string{" API.Contoso.com. "}, Port: 8080, Reason: "an API name"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"domain.add","summary":"Put Contoso - production on api.contoso.com","reason":"an API name","projectId":"pProd","projectName":"Contoso - production","serviceId":"sMedusa","serviceName":"medusa","params":{"domains":["api.contoso.com"],"port":8080}},"note":"Nothing has run. The person approves or declines it on the card."}`,
		},
		{
			name: "a domain and every subdomain of it, a wildcard",
			in:   OrgActionInput{Kind: ProposalDomainAdd, Project: "pProd", Service: "medusa", Domains: []string{"contoso.com", "*.contoso.com"}, Port: 9000, Reason: "every shop on its own subdomain"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"domain.add","summary":"Put Contoso - production on contoso.com and *.contoso.com","reason":"every shop on its own subdomain","projectId":"pProd","projectName":"Contoso - production","serviceId":"sMedusa","serviceName":"medusa","params":{"domains":["contoso.com","*.contoso.com"],"port":9000}},"note":"Nothing has run. The person approves or declines it on the card."}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ProposeOrgAction(context.Background(), domainOrg(), fixedID, tt.in)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var gotDoc, wantDoc any
			_ = json.Unmarshal(raw, &gotDoc)
			if err := json.Unmarshal([]byte(tt.want), &wantDoc); err != nil {
				t.Fatalf("bad want: %v", err)
			}
			if !reflect.DeepEqual(gotDoc, wantDoc) {
				t.Errorf("proposal\n got %s\nwant %s", raw, tt.want)
			}
		})
	}
}

// TestProposeOrgAction_DomainAdd_RefusesWhatCannotGoLive: a domain is
// proposed only for the organization's project, onto one of its services'
// web ports.
func TestProposeOrgAction_DomainAdd_RefusesWhatCannotGoLive(t *testing.T) {
	t.Parallel()
	domain := func(project, service string, port int, domains ...string) OrgActionInput {
		return OrgActionInput{Kind: ProposalDomainAdd, Project: project, Service: service, Port: port, Domains: domains, Reason: "r"}
	}
	tests := []struct {
		name    string
		in      OrgActionInput
		wantErr string
	}{
		{name: "a project outside the org", in: domain("pElsewhere", "medusa", 9000, "contoso.com"), wantErr: "no project"},
		{name: "a service the project does not have", in: domain("pProd", "ghost", 9000, "contoso.com"), wantErr: "ghost"},
		{name: "a system service", in: domain("pProd", "core", 9000, "contoso.com"), wantErr: "core"},
		{name: "a Mate's own zcp container", in: domain("pMM", "zcp", 80, "contoso.com"), wantErr: "Mate's own"},
		{name: "no port", in: domain("pProd", "medusa", 0, "contoso.com"), wantErr: "port"},
		{name: "a port the service does not have", in: domain("pProd", "medusa", 3000, "contoso.com"), wantErr: "9000"},
		{name: "a port that does not serve the web", in: domain("pProd", "medusa", 9100, "contoso.com"), wantErr: "9100"},
		{name: "a service with no web port", in: domain("pProd", "db", 5432, "contoso.com"), wantErr: "no web port"},
		{name: "no domain", in: domain("pProd", "medusa", 9000), wantErr: "domain"},
		{name: "a domain with its scheme", in: domain("pProd", "medusa", 9000, "https://contoso.com"), wantErr: "https://contoso.com"},
		{name: "a domain with a path", in: domain("pProd", "medusa", 9000, "contoso.com/shop"), wantErr: "contoso.com/shop"},
		{name: "a bare word", in: domain("pProd", "medusa", 9000, "contoso"), wantErr: "contoso"},
		{name: "a label starting with a hyphen", in: domain("pProd", "medusa", 9000, "-shop.contoso.com"), wantErr: "-shop.contoso.com"},
		{name: "a wildcard inside the name", in: domain("pProd", "medusa", 9000, "shop.*.contoso.com"), wantErr: "shop.*.contoso.com"},
		{name: "a wildcard over a top-level domain", in: domain("pProd", "medusa", 9000, "*.com"), wantErr: "*.com"},
		{name: "the same domain twice", in: domain("pProd", "medusa", 9000, "contoso.com", "Contoso.com"), wantErr: "twice"},
		{name: "no reason", in: OrgActionInput{Kind: ProposalDomainAdd, Project: "pProd", Service: "medusa", Port: 9000, Domains: []string{"contoso.com"}}, wantErr: "reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ProposeOrgAction(context.Background(), domainOrg(), fixedID, tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

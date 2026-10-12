// Tests for: internal/ops/org_proposal.go — org mode's act tools return a
// proposal the person decides on, and run nothing (docs/spec-mate.md §2.11).
package ops

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
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

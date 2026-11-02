// Tests for: internal/ops/app_proposal.go — a Mate proposes a change to its
// application's stage or production; the person decides on the card and
// nothing runs from here (docs/spec-mate.md §2.11).
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// testApp is an application as HQ observes it for a Mate: its stage and
// production, each with its own services.
type testApp struct {
	envs     []AppEnvironment
	services map[string][]platform.ServiceStack
	read     []string
}

func (a *testApp) Environments(context.Context) ([]AppEnvironment, error) { return a.envs, nil }

func (a *testApp) Services(_ context.Context, projectID string) ([]platform.ServiceStack, error) {
	a.read = append(a.read, projectID)
	return a.services[projectID], nil
}

func newTestApp() *testApp {
	return &testApp{
		envs: []AppEnvironment{
			{ProjectID: "pStage", Name: "client demo", Kind: "stage"},
			{ProjectID: "pProd", Name: "shop-production", Kind: "production"},
		},
		services: map[string][]platform.ServiceStack{
			"pStage": {{ID: "sStageApi", Name: "api", Status: platform.ServiceStatusActive}},
			"pProd": {
				{ID: "sApi", Name: "api", Status: platform.ServiceStatusActive},
				{ID: "sWorker", Name: "worker", Status: platform.ServiceStatusStopped},
			},
		},
	}
}

// TestProposeAppAction_Shape pins the proposal a Mate's card is drawn from:
// the same document the Chief's tools return, named by the environment.
func TestProposeAppAction_Shape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   OrgActionInput
		want string
	}{
		{
			name: "service.restart by the environment's title",
			in:   OrgActionInput{Kind: ProposalServiceRestart, Project: "Production", Service: "api", Reason: "502s since the release"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"service.restart","summary":"Restart api in Production","reason":"502s since the release","projectId":"pProd","projectName":"Production","serviceId":"sApi","serviceName":"api","params":{}},"note":"Nothing has run. The person approves or declines it on the card, which carries your reason and how it ends: never restate it or ask them to approve, and don't propose it again unless they ask. You are not told the outcome: read the state again before saying it ran."}`,
		},
		{
			name: "service.start by project and service id",
			in:   OrgActionInput{Kind: ProposalServiceStart, Project: "pProd", Service: "sWorker", Reason: "the queue backs up"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"service.start","summary":"Start worker in Production","reason":"the queue backs up","projectId":"pProd","projectName":"Production","serviceId":"sWorker","serviceName":"worker","params":{}},"note":"Nothing has run. The person approves or declines it on the card, which carries your reason and how it ends: never restate it or ask them to approve, and don't propose it again unless they ask. You are not told the outcome: read the state again before saying it ran."}`,
		},
		{
			name: "service.stop on stage, by its kind",
			in:   OrgActionInput{Kind: ProposalServiceStop, Project: "stage", Service: "api", Reason: "unused this week"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"service.stop","summary":"Stop api in Client demo","reason":"unused this week","projectId":"pStage","projectName":"Client demo","serviceId":"sStageApi","serviceName":"api","params":{}},"note":"Nothing has run. The person approves or declines it on the card, which carries your reason and how it ends: never restate it or ask them to approve, and don't propose it again unless they ask. You are not told the outcome: read the state again before saying it ran."}`,
		},
		{
			name: "env.set on a service",
			in:   OrgActionInput{Kind: ProposalEnvSet, Project: "production", Service: "api", Scope: "service", Key: "LOG_LEVEL", Value: "debug", Reason: "find the 502s"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"env.set","summary":"Set LOG_LEVEL on api in Production","reason":"find the 502s","projectId":"pProd","projectName":"Production","serviceId":"sApi","serviceName":"api","params":{"scope":"service","key":"LOG_LEVEL","value":"debug","secret":false}},"note":"Nothing has run. The person approves or declines it on the card, which carries your reason and how it ends: never restate it or ask them to approve, and don't propose it again unless they ask. You are not told the outcome: read the state again before saying it ran."}`,
		},
		{
			name: "env.set on the project, by HQ's name",
			in:   OrgActionInput{Kind: ProposalEnvSet, Project: "shop-production", Scope: "project", Key: "APP_URL", Value: "https://shop.example.com", Reason: "the new domain"},
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"env.set","summary":"Set APP_URL in Production","reason":"the new domain","projectId":"pProd","projectName":"Production","params":{"scope":"project","key":"APP_URL","value":"https://shop.example.com","secret":false}},"note":"Nothing has run. The person approves or declines it on the card, which carries your reason and how it ends: never restate it or ask them to approve, and don't propose it again unless they ask. You are not told the outcome: read the state again before saying it ran."}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ProposeAppAction(context.Background(), newTestApp(), fixedID, tt.in)
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

// TestProposeAppAction_ReadsOnlyTheTarget_OneEnvironment: a proposal reads the
// services of the environment it names and no other, so it spends one of
// HQ's bounded observations, not one per environment.
func TestProposeAppAction_ReadsOnlyTheTarget_OneEnvironment(t *testing.T) {
	t.Parallel()
	app := newTestApp()
	if _, err := ProposeAppAction(context.Background(), app, fixedID, OrgActionInput{
		Kind: ProposalServiceRestart, Project: "production", Service: "api", Reason: "stuck",
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(app.read, []string{"pProd"}) {
		t.Errorf("services read for %v, want only [pProd]", app.read)
	}
}

// TestProposeAppAction_Refuses: what is not the application's stage or
// production, a service it lacks, an action its state makes pointless, or a
// kind beyond a Mate's is refused with a plain error naming zerops_observe —
// never the Chief's tools.
func TestProposeAppAction_Refuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   OrgActionInput
		want string
	}{
		{name: "no environment", in: OrgActionInput{Kind: ProposalServiceRestart, Service: "api", Reason: "r"}, want: "zerops_observe"},
		{name: "another project", in: OrgActionInput{Kind: ProposalServiceRestart, Project: "Northwind - production", Service: "api", Reason: "r"}, want: "zerops_observe"},
		{name: "no such service", in: OrgActionInput{Kind: ProposalServiceRestart, Project: "production", Service: "db", Reason: "r"}, want: "zerops_observe"},
		{name: "no service named", in: OrgActionInput{Kind: ProposalServiceRestart, Project: "production", Reason: "r"}, want: "zerops_observe"},
		{name: "start a running service", in: OrgActionInput{Kind: ProposalServiceStart, Project: "production", Service: "api", Reason: "r"}, want: "already running"},
		{name: "stop a stopped service", in: OrgActionInput{Kind: ProposalServiceStop, Project: "production", Service: "worker", Reason: "r"}, want: "already stopped"},
		{name: "restart a stopped service", in: OrgActionInput{Kind: ProposalServiceRestart, Project: "production", Service: "worker", Reason: "r"}, want: "propose service.start"},
		{name: "no reason", in: OrgActionInput{Kind: ProposalServiceRestart, Project: "production", Service: "api"}, want: "reason"},
		{name: "a bad variable name", in: OrgActionInput{Kind: ProposalEnvSet, Project: "production", Scope: "project", Key: "1X", Value: "v", Reason: "r"}, want: "not a variable name"},
		{name: "a service variable without the service", in: OrgActionInput{Kind: ProposalEnvSet, Project: "production", Scope: "service", Key: "X", Value: "v", Reason: "r"}, want: "needs the service"},
		{name: "a kind beyond a Mate's", in: OrgActionInput{Kind: ProposalDomainAdd, Project: "production", Service: "api", Reason: "r"}, want: "service.restart"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ProposeAppAction(context.Background(), newTestApp(), fixedID, tt.in)
			if err == nil {
				t.Fatal("want a refusal")
			}
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want a platform error the agent can act on", err)
			}
			text := pe.Message + " " + pe.Suggestion
			if !strings.Contains(text, tt.want) {
				t.Errorf("refusal %q, want it to mention %q", text, tt.want)
			}
			if strings.Contains(text, "zerops_org_") {
				t.Errorf("refusal %q names the Chief's tools", text)
			}
		})
	}
}

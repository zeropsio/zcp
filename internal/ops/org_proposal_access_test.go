// Tests for: internal/ops/org_proposal_access.go — the Chief proposes who
// reaches a project or a whole application; the person's client writes it with
// their own token after their Approve (docs/spec-mate.md §2.11).
package ops

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// accessOrg is testOrg with the application Contoso whole: production, a
// stage and its Mate Alba, and the Chief acting for an organization owner.
func accessOrg() *fakeOrg {
	org := testOrg()
	org.projects = append(org.projects,
		platform.Project{ID: "pCStage", Name: "Contoso - stage", Status: "ACTIVE"},
		platform.Project{ID: "pAlba", Name: "Contoso - Alba", Status: "ACTIVE", Tags: []string{"mate"}},
	)
	org.services = append(org.services, svc("sAlba", "pAlba", "zcp", "zcp@1", "USER", "ACTIVE"))
	org.person = &platform.OrgPerson{Name: "Ola Owner", Role: "OWNER"}
	return org
}

func access(project string, people ...OrgAccessPerson) OrgActionInput {
	return OrgActionInput{Kind: ProposalAccessSet, Project: project, People: people, Reason: "asked"}
}

// TestProposeOrgAction_AccessSet_Shape pins the access.set document the
// client resolves its people from and writes on approval.
func TestProposeOrgAction_AccessSet_Shape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		org  func() *fakeOrg
		in   OrgActionInput
		want string
	}{
		{
			name: "an application by its name reaches every one of its projects at once",
			org:  accessOrg,
			in:   access("contoso", OrgAccessPerson{Person: "Dana", Level: "full"}, OrgAccessPerson{Person: "greta@example.com", Level: "read"}),
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"access.set","summary":"Access to Contoso: Dana full access, greta@example.com read only","reason":"asked","projectId":"pProd","projectName":"Contoso","params":{"projects":[{"id":"pProd","name":"Contoso - production"},{"id":"pCStage","name":"Contoso - stage"},{"id":"pAlba","name":"Contoso - Alba"}],"people":[{"person":"Dana","level":"full"},{"person":"greta@example.com","level":"read"}]}},"note":"Nothing has run. The person approves or declines it on the card."}`,
		},
		{
			name: "one project by its full name reaches that project alone",
			org:  accessOrg,
			in:   access("Contoso - stage", OrgAccessPerson{Person: " Dana ", Level: "none"}, OrgAccessPerson{Person: "Greta", Level: "remove"}),
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"access.set","summary":"Access to Contoso - stage: Dana hidden, Greta back to their organization role","reason":"asked","projectId":"pCStage","projectName":"Contoso - stage","params":{"projects":[{"id":"pCStage","name":"Contoso - stage"}],"people":[{"person":"Dana","level":"none"},{"person":"Greta","level":"remove"}]}},"note":"Nothing has run. The person approves or declines it on the card."}`,
		},
		{
			name: "a person whose role this HQ does not say is not refused: Zerops decides on Approve",
			org: func() *fakeOrg {
				org := accessOrg()
				org.person = nil
				return org
			},
			in:   access("Northwind Shop - Rossy", OrgAccessPerson{Person: "Dana", Level: "read"}),
			want: `{"proposal":{"id":"11111111-2222-3333-4444-555555555555","kind":"access.set","summary":"Access to Northwind Shop - Rossy: Dana read only","reason":"asked","projectId":"pRossy","projectName":"Northwind Shop - Rossy","params":{"projects":[{"id":"pRossy","name":"Northwind Shop - Rossy"}],"people":[{"person":"Dana","level":"read"}]}},"note":"Nothing has run. The person approves or declines it on the card."}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ProposeOrgAction(context.Background(), tt.org(), fixedID, tt.in)
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

// TestProposeOrgAction_AccessSet_RefusesWhatCannotBeGiven: access is proposed
// only on the organization's projects, at a level Mate gives, to each person
// once, and only for a person whose role may change who has access.
func TestProposeOrgAction_AccessSet_RefusesWhatCannotBeGiven(t *testing.T) {
	t.Parallel()
	dana := OrgAccessPerson{Person: "Dana", Level: "full"}
	tests := []struct {
		name    string
		role    string
		in      OrgActionInput
		wantErr string
	}{
		{name: "a project or application outside the org", in: access("Fabrikam", dana), wantErr: "Fabrikam"},
		{name: "the Chief's own project", in: access("pMM", dana), wantErr: "Chief"},
		{name: "HQ's own project, which holds the organization's keys", in: access("pHQ", dana), wantErr: "HQ"},
		{name: "nobody", in: access("Contoso"), wantErr: "person"},
		{name: "a blank person", in: access("Contoso", OrgAccessPerson{Person: " ", Level: "full"}), wantErr: "person"},
		{name: "a level Mate does not give", in: access("Contoso", OrgAccessPerson{Person: "Dana", Level: "owner"}), wantErr: "owner"},
		{name: "the same person twice", in: access("Contoso", dana, OrgAccessPerson{Person: "dana", Level: "read"}), wantErr: "twice"},
		{name: "a person whose role may not change access", role: "BASIC_USER", in: access("Contoso", dana), wantErr: "owner or admin"},
		{name: "no reason", in: OrgActionInput{Kind: ProposalAccessSet, Project: "Contoso", People: []OrgAccessPerson{dana}}, wantErr: "reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			org := accessOrg()
			if tt.role != "" {
				org.person = &platform.OrgPerson{Name: "Dev Eloper", Role: tt.role}
			}
			_, err := ProposeOrgAction(context.Background(), org, fixedID, tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

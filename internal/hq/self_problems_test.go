package hq

import (
	"context"
	"reflect"
	"testing"
)

func TestClient_Self_Problems_DecodesHQFindingsAndOlderHQ(t *testing.T) {
	t.Parallel()
	const finding = `{"code":"invalid_tier","what":"The tier has an invalid minimum CPU","where":{"appId":"app-1","projectId":"project-1","environment":"Production","service":"api","tier":"production","file":"production/import.yaml","repo":"group","change":7,"head":"head-7","releaseTag":"v1","operationId":"operation-1"},"rule":{"source":"zerops-schema","code":"minimum","schemaUrl":"https://schema.example/import.json","schemaDigest":"sha256:abc","schemaPath":"/minimum","instancePath":"/services/0/minCpu"},"next":{"actor":"Mate","action":"Correct the tier and propose it","target":"production/import.yaml"},"evidence":{"processId":"process-1","appVersionId":"version-1","logUrl":"https://logs.example/process-1"}}`
	want := Problem{
		Code: "invalid_tier", What: "The tier has an invalid minimum CPU",
		Where:    ProblemWhere{AppID: "app-1", ProjectID: "project-1", Environment: "Production", Service: "api", Tier: "production", File: "production/import.yaml", Repo: "group", Change: 7, Head: "head-7", ReleaseTag: "v1", OperationID: "operation-1"},
		Rule:     ProblemRule{Source: "zerops-schema", Code: "minimum", SchemaURL: "https://schema.example/import.json", SchemaDigest: "sha256:abc", SchemaPath: "/minimum", InstancePath: "/services/0/minCpu"},
		Next:     ProblemNext{Actor: "Mate", Action: "Correct the tier and propose it", Target: "production/import.yaml"},
		Evidence: &ProblemEvidence{ProcessID: "process-1", AppVersionID: "version-1", LogURL: "https://logs.example/process-1"},
	}
	for _, tt := range []struct {
		name, fields   string
		wantProblems   []Problem
		wantValidation *RecipeValidation
	}{
		{name: "app findings survive without an open change", fields: `"problems":[` + finding + `],"changes":[]`, wantProblems: []Problem{want}},
		{name: "group findings retain HQ's invalid head result", fields: `"changes":[{"repo":"group","number":7,"head":"head-7","problems":[` + finding + `],"recipeValidation":{"head":"head-7","state":"invalid","schemaUrl":"https://schema.example/import.json","schemaDigest":"sha256:abc","problems":[` + finding + `]}}]`, wantValidation: &RecipeValidation{Head: "head-7", State: "invalid", SchemaURL: "https://schema.example/import.json", SchemaDigest: "sha256:abc", Problems: []Problem{want}}},
		{name: "older HQ omits findings", fields: `"changes":[]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, _ := answering(t, `{"projectId":"project-1",`+tt.fields+`}`)
			state, err := client.Self(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(state.Problems, tt.wantProblems) {
				t.Errorf("app problems = %+v, want %+v", state.Problems, tt.wantProblems)
			}
			if tt.wantValidation != nil {
				if len(state.Changes) != 1 {
					t.Fatalf("changes = %+v, want the group change", state.Changes)
				}
				change := state.Changes[0]
				if !reflect.DeepEqual(change.RecipeValidation, tt.wantValidation) {
					t.Errorf("recipeValidation = %+v, want %+v", change.RecipeValidation, tt.wantValidation)
				}
				if !reflect.DeepEqual(change.Problems, []Problem{want}) {
					t.Errorf("change problems = %+v, want %+v", change.Problems, []Problem{want})
				}
			}
		})
	}
}

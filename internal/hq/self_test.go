package hq

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

// selfSample is HQ's answer to GET /api/mate/self for a Mate in an
// application, with an open change in one repository and a merged and a
// closed one in another.
const selfSample = `{"projectId":"p-mate","name":"Ada","face":"face-1","standupRequestedBy":null,"closedOff":false,` +
	`"appId":"0b6e7c1e-8f7a-4a52-9d1c-2f1f5d0a3c11","changes":[` +
	`{"repo":"apidev","number":2,"state":"open","head":"1111111111111111111111111111111111111111","mergedSha":null,"landedHead":null},` +
	`{"repo":"appdev","number":2,"state":"merged","head":"2222222222222222222222222222222222222222",` +
	`"mergedSha":"3333333333333333333333333333333333333333","landedHead":"2222222222222222222222222222222222222222"},` +
	`{"repo":"appdev","number":1,"state":"closed","head":null,"mergedSha":null,"landedHead":null}]}`

func TestClient_Self_CarriesTheMatesApplicationAndItsChanges(t *testing.T) {
	t.Parallel()
	client, calls := answering(t, selfSample)

	state, err := client.Self(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sha := func(s string) *string { return &s }
	want := MateState{
		ProjectID: "p-mate", Name: "Ada", Face: "face-1",
		AppID: sha("0b6e7c1e-8f7a-4a52-9d1c-2f1f5d0a3c11"),
		Changes: []MateChange{
			{Repo: "apidev", Number: 2, State: ChangeOpen, Head: sha("1111111111111111111111111111111111111111")},
			{Repo: "appdev", Number: 2, State: ChangeMerged, Head: sha("2222222222222222222222222222222222222222"),
				MergedSha: sha("3333333333333333333333333333333333333333"), LandedHead: sha("2222222222222222222222222222222222222222")},
			{Repo: "appdev", Number: 1, State: ChangeClosed},
		},
	}
	if !reflect.DeepEqual(state, want) {
		t.Errorf("state = %+v\nwant %+v", state, want)
	}
	wantCall := hqCall{Method: http.MethodGet, Path: "/api/mate/self", Authorization: "Mate good"}
	if len(*calls) != 1 || (*calls)[0] != wantCall {
		t.Errorf("calls = %+v, want [%+v]", *calls, wantCall)
	}
}

// TestClient_Self_GitPushes_DecodesCIAndOlderHQ pins the Git environments'
// pushes beside the changes: a push with the origin's CI, one HQ read no CI
// for, and an older HQ that omits the field (nil, not empty).
func TestClient_Self_GitPushes_DecodesCIAndOlderHQ(t *testing.T) {
	t.Parallel()
	const pushes = `{"projectId":"p-mate","name":"Ada","face":"face-1","standupRequestedBy":null,"appId":"a1","changes":[],"gitPushes":[` +
		`{"environment":"git-stage","tier":"stage","branch":"main","repo":"appdev","sha":"cccccccccccccccccccccccccccccccccccccccc",` +
		`"ci":{"sha":"cccccccccccccccccccccccccccccccccccccccc","verdict":"failed","done":2,"total":2,` +
		`"failed":[{"name":"unit","url":"https://github.com/acme/app/actions/runs/7/job/9"},{"name":"lint","url":null}],` +
		`"required":["unit"],"reason":null,"watch":"finished","readAt":"2026-10-17T10:00:10.000Z","until":"2026-10-17T11:00:00.000Z"}},` +
		`{"environment":"git-prod","tier":"production","branch":"release","repo":"appdev","sha":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}]}`
	client, _ := answering(t, pushes)
	state, err := client.Self(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	url := "https://github.com/acme/app/actions/runs/7/job/9"
	want := []GitPush{
		{Environment: "git-stage", Repo: "appdev", Sha: "cccccccccccccccccccccccccccccccccccccccc",
			CI: &PushCI{Verdict: CIFailed, Done: 2, Total: 2,
				Failed: []PushCIFailure{{Name: "unit", URL: &url}, {Name: "lint"}},
				Watch:  "finished", Until: "2026-10-17T11:00:00.000Z"}},
		{Environment: "git-prod", Repo: "appdev", Sha: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},
	}
	if !reflect.DeepEqual(state.GitPushes, want) {
		t.Errorf("gitPushes = %+v\nwant %+v", state.GitPushes, want)
	}

	older, _ := answering(t, selfSample)
	state, err = older.Self(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.GitPushes != nil {
		t.Errorf("gitPushes from an HQ that omits them = %+v, want nil", state.GitPushes)
	}
}

// A Mate's state carries every repository of its application, with the
// address of its connected origin, and an older HQ that omits them (nil, not
// empty).
func TestClient_Self_Repos_DecodesOriginsAndOlderHQ(t *testing.T) {
	t.Parallel()
	const repos = `{"projectId":"p-mate","name":"Ada","face":"face-1","standupRequestedBy":null,"appId":"a1","changes":[],` +
		`"repos":[{"name":"appdev","origin":{"url":"https://github.com/acme/app"}},{"name":"web"}]}`
	client, _ := answering(t, repos)
	state, err := client.Self(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []MateRepo{{Name: "appdev", Origin: &RepoOrigin{URL: "https://github.com/acme/app"}}, {Name: "web"}}
	if !reflect.DeepEqual(state.Repos, want) {
		t.Errorf("repos = %+v\nwant %+v", state.Repos, want)
	}

	older, _ := answering(t, selfSample)
	state, err = older.Self(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Repos != nil {
		t.Errorf("repos from an HQ that omits them = %+v, want nil", state.Repos)
	}
}

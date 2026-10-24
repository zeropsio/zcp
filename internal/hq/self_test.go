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
	wantCall := hqCall{Method: http.MethodGet, Path: "/api/mate/self", Authorization: "Project good"}
	if len(*calls) != 1 || (*calls)[0] != wantCall {
		t.Errorf("calls = %+v, want [%+v]", *calls, wantCall)
	}
}

// A Mate's state carries every repository of its application, with the
// address of its connected origin and which way the code is kept, and an
// older HQ that omits them (nil, not empty; no way, not one guessed).
func TestClient_Self_Repos_DecodesOriginsAndOlderHQ(t *testing.T) {
	t.Parallel()
	const repos = `{"projectId":"p-mate","name":"Ada","face":"face-1","standupRequestedBy":null,"appId":"a1","changes":[],` +
		`"repos":[{"name":"appdev","origin":{"url":"https://github.com/acme/app","mode":"github"}},{"name":"api","origin":{"url":"https://github.com/acme/api"}},{"name":"web"}]}`
	client, _ := answering(t, repos)
	state, err := client.Self(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []MateRepo{
		{Name: "appdev", Origin: &RepoOrigin{URL: "https://github.com/acme/app", Mode: OriginOnGitHub}},
		{Name: "api", Origin: &RepoOrigin{URL: "https://github.com/acme/api"}},
		{Name: "web"},
	}
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

// HQ says whether the Mate's application has a production; an older HQ says
// nothing, which decodes as nil, never as none.
func TestClient_Self_Production_DecodesAndOlderHQ(t *testing.T) {
	t.Parallel()
	client, _ := answering(t, `{"projectId":"p-mate","name":"Ada","face":"face-1","standupRequestedBy":null,"appId":"a1","changes":[],"production":true}`)
	state, err := client.Self(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Production == nil || !*state.Production {
		t.Errorf("production = %v, want true", state.Production)
	}

	older, _ := answering(t, selfSample)
	state, err = older.Self(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Production != nil {
		t.Errorf("production from an HQ that omits it = %v, want nil", *state.Production)
	}
}

// A change in the Mate's state names the branch it is bound to, as HQ stores
// it, and whether a person handed it this Mate from a Mate that is gone; an
// older HQ names neither.
func TestClient_Self_Changes_DecodeBranchAndHandOver(t *testing.T) {
	t.Parallel()
	const handed = `{"projectId":"p-mate","name":"Ada","face":"face-1","standupRequestedBy":null,"appId":"a1","changes":[` +
		`{"repo":"appdev","number":7,"title":"Fix the login","state":"open","head":null,"mergedSha":null,"landedHead":null,"branch":"mate/p-gone/7","handed":true},` +
		`{"repo":"appdev","number":5,"title":"Add a list","state":"merged","head":null,"mergedSha":null,"landedHead":null,"branch":"add-list"}]}`
	client, _ := answering(t, handed)
	state, err := client.Self(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := make([][2]any, 0, len(state.Changes))
	for _, c := range state.Changes {
		got = append(got, [2]any{c.Branch, c.Handed})
	}
	if want := [][2]any{{"mate/p-gone/7", true}, {"add-list", false}}; !reflect.DeepEqual(got, want) {
		t.Errorf("branch and hand-over = %v, want %v", got, want)
	}

	older, _ := answering(t, selfSample)
	if state, err = older.Self(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, c := range state.Changes {
		if c.Branch != "" || c.Handed {
			t.Errorf("change #%d from an HQ older than branches = %+v, want no branch and not handed", c.Number, c)
		}
	}
}

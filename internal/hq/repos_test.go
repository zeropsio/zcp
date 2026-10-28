package hq

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// The application's repositories as HQ lists them to the Mate, each with the
// remote connected to it: how HQ reaches it — the organisation's GitHub app,
// which mirrors it, or a person's token — and, mirrored, since when GitHub's
// default branch and `main` diverged; none for one HQ alone keeps.
func TestClient_AppRepos_ReadsEachRepositorysRemote(t *testing.T) {
	t.Parallel()
	const repos = `{"repos":[` +
		`{"name":"api","url":"https://hq.example/git/org-1/a1/api.git","mainHead":null,"updatedAt":"2026-10-28T09:00:00.000Z",` +
		`"remote":{"url":"https://github.com/acme/api","repositoryId":null,"keptAt":"2026-10-28T09:00:00.000Z","keptBy":"u-1","expiresAt":null,"invalidSince":null,"via":"token"}},` +
		`{"name":"appdev","url":"https://hq.example/git/org-1/a1/appdev.git","mainHead":"abc","updatedAt":"2026-10-28T09:00:00.000Z",` +
		`"remote":{"url":"https://github.com/acme/app","repositoryId":"42","keptAt":"2026-10-28T09:00:00.000Z","keptBy":"u-1","expiresAt":null,"invalidSince":null,` +
		`"via":"app","divergedSince":"2026-10-28T10:00:00.000Z","governance":"governed"}},` +
		`{"name":"web","url":"https://hq.example/git/org-1/a1/web.git","mainHead":null,"updatedAt":"2026-10-28T09:00:00.000Z","remote":null}]}`
	client, calls := answering(t, repos)

	got, err := client.AppRepos(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	want := []AppRepo{
		{Name: "api", Remote: &RepoRemote{URL: "https://github.com/acme/api", Via: RemoteViaToken}},
		{Name: "appdev", Remote: &RepoRemote{URL: "https://github.com/acme/app", Via: RemoteViaApp, DivergedSince: "2026-10-28T10:00:00.000Z"}},
		{Name: "web"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("repos = %+v\nwant %+v", got, want)
	}
	wantCall := hqCall{Method: http.MethodGet, Path: "/api/apps/a1/repos", Authorization: "Project good"}
	if len(*calls) != 1 || (*calls)[0] != wantCall {
		t.Errorf("calls = %+v, want [%+v]", *calls, wantCall)
	}
}

// A list HQ answers in pages is read whole: each next page asked after the
// last name the one before it named.
func TestClient_AppRepos_ReadsEveryPage(t *testing.T) {
	t.Parallel()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Query().Get("after"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after") == "" {
			_, _ = io.WriteString(w, `{"repos":[{"name":"api","remote":null}],"next":"api"}`)
			return
		}
		_, _ = io.WriteString(w, `{"repos":[{"name":"web","remote":null}]}`)
	}))
	t.Cleanup(srv.Close)

	got, err := keyedClient(t, srv).AppRepos(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if want := []AppRepo{{Name: "api"}, {Name: "web"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("repos = %+v, want %+v", got, want)
	}
	if want := []string{"", "api"}; !reflect.DeepEqual(asked, want) {
		t.Errorf("asked after %q, want %q", asked, want)
	}
}

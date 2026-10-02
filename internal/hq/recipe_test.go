// Tests for: hq/recipe.go — a tier of the application's recipe, read from
// `main` of its recipe repository.
package hq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestClient_RecipeTier_ReadsTheTierFromHQ(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		answer string
		want   RecipeTier
	}{
		{
			name:   "present",
			answer: `{"state":"present","importYaml":"services: []\n","mainHead":"1111111111111111111111111111111111111111"}`,
			want:   RecipeTier{State: RecipePresent, ImportYAML: "services: []\n", MainHead: "1111111111111111111111111111111111111111"},
		},
		{name: "absent", answer: `{"state":"absent"}`, want: RecipeTier{State: RecipeAbsent}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, calls := answering(t, tt.answer)
			got, err := client.RecipeTier(context.Background(), RecipeTierMate)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("tier = %+v, want %+v", got, tt.want)
			}
			if len(*calls) != 1 || (*calls)[0].Method != http.MethodGet || (*calls)[0].Path != "/api/mate/recipe/mate" || (*calls)[0].Authorization != "Mate good" {
				t.Errorf("calls = %+v, want one GET /api/mate/recipe/mate as the Mate", *calls)
			}
		})
	}
}

// TestClient_RepoExists_AsksGitWithoutMakingIt: whether a repository is in
// the application is git's own first ask of a clone, as the Mate — found, or
// not found; any other answer is an error, never "absent".
func TestClient_RepoExists_AsksGitWithoutMakingIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		status  int
		want    bool
		wantErr bool
	}{
		{"found", http.StatusOK, true, false},
		{"not found", http.StatusNotFound, false, false},
		{"refused", http.StatusForbidden, false, true},
		{"a standby", http.StatusServiceUnavailable, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var asked string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, password, _ := r.BasicAuth()
				asked = r.Method + " " + r.URL.RequestURI() + " " + user + ":" + password
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(srv.Close)
			got, err := enrolledClient(t, srv).RepoExists(context.Background(), "app-1", "web")
			if got != tt.want || (err != nil) != tt.wantErr {
				t.Errorf("RepoExists = %v, %v; want %v, error %v", got, err, tt.want, tt.wantErr)
			}
			if want := "GET /git/app-1/web.git/info/refs?service=git-upload-pack " + GitUser + ":good"; asked != want {
				t.Errorf("asked %q, want %q", asked, want)
			}
		})
	}
}

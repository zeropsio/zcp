package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCreateAndImportProject_ForwardsOnlyTheExactMateMarker(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, project string
		want          []string
	}{
		{"metadata removed", "project:\n  name: Sample\n  tags: [mate, 'mate:signer:codex:u-bo', custom, Mate, ' mate ', 'mate:face:rose']\n", []string{"mate"}},
		{"no marker", "project:\n  name: Sample\n  tags: [custom, Mate]\n", nil},
		{"no tags", "project:\n  name: Sample\n", nil},
		{"empty tags", "project:\n  name: Sample\n  tags: []\n", nil},
		{"tag alias", "marker: &marker [mate, custom]\nproject:\n  name: Sample\n  tags: *marker\n", []string{"mate"}},
		{"anchored project", "project: &project\n  name: Sample\n  tags: [mate, custom]\n", []string{"mate"}},
		{"shared mixed tags", "project:\n  name: Sample\n  tags: &shared [mate, custom]\n", []string{"mate"}},
		{"shared dropped tags", "project:\n  name: Sample\n  tags: &shared [custom]\n", nil},
		{"marker only", "project:\n  name: Sample\n  tags: [mate]\n", []string{"mate"}},
		{"alias and merge", "defaults: &defaults\n  tags: [mate, custom]\nproject:\n  <<: *defaults\n  name: Sample\n", []string{"mate"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/rest/public/client/client-x/project/import" {
					t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
				}
				var request struct {
					Yaml string `json:"yaml"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				var got struct {
					Project struct {
						Name        string            `yaml:"name"`
						Tags        []string          `yaml:"tags"`
						CorePackage string            `yaml:"corePackage"`
						Env         map[string]string `yaml:"envVariables"`
					} `yaml:"project"`
					Services []struct {
						Hostname     string   `yaml:"hostname"`
						BuildFromGit string   `yaml:"buildFromGit"`
						Init         string   `yaml:"init"`
						Tags         []string `yaml:"tags"`
					} `yaml:"services"`
				}
				if err := yaml.Unmarshal([]byte(request.Yaml), &got); err != nil {
					t.Error(err)
				}
				if !reflect.DeepEqual(got.Project.Tags, tt.want) {
					t.Errorf("forwarded tags = %v, want %v", got.Project.Tags, tt.want)
				}
				if tt.name == "shared mixed tags" || tt.name == "shared dropped tags" {
					want := []string{"custom"}
					if tt.name == "shared mixed tags" {
						want = []string{"mate", "custom"}
					}
					if len(got.Services) != 1 || !reflect.DeepEqual(got.Services[0].Tags, want) {
						t.Errorf("unrelated service tags changed: %+v, want %v", got.Services, want)
					}
				}
				if got.Project.Name != "Sample" || got.Project.CorePackage != "LIGHT" || got.Project.Env["SETTING"] != "kept" || len(got.Services) != 1 || got.Services[0].BuildFromGit != "https://hq.example/git/app/repo.git" || got.Services[0].Init != "echo hello\necho world\n" {
					t.Errorf("import lost other fields: %+v", got)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"projectId":"p-new","projectName":"Sample","serviceStacks":[]}`))
			}))
			defer srv.Close()
			z, err := NewZeropsClient("fake-key", srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			p := &projectAdminClient{zerops: z, clientID: "client-x"}
			input := tt.project + "  corePackage: LIGHT\n  envVariables:\n    SETTING: kept\nservices:\n  - hostname: app\n    buildFromGit: https://hq.example/git/app/repo.git\n    init: |\n      echo hello\n      echo world\n"
			if tt.name == "shared mixed tags" || tt.name == "shared dropped tags" {
				input += "    tags: *shared\n"
			}
			if tt.name == "anchored project" {
				input += "copy: *project\n"
			}
			if _, err := p.CreateAndImportProject(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Errorf("API calls = %d, want 1", calls)
			}
		})
	}
}

func TestCreateAndImportProject_ExcessiveAliasesFailBeforeAPI(t *testing.T) {
	t.Parallel()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projectId":"p-new","projectName":"Sample","serviceStacks":[]}`))
	}))
	defer srv.Close()
	z, err := NewZeropsClient("fake-key", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	p := &projectAdminClient{zerops: z, clientID: "client-x"}
	parts := []string{"a0: &a0 [x, x]\n"}
	for i := 1; i <= 14; i++ {
		parts = append(parts, fmt.Sprintf("a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1))
	}
	input := strings.Join(parts, "") + "project:\n  name: Sample\n  tags: [mate, custom]\nservices: []\n"
	_, err = p.CreateAndImportProject(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "alias expansion limit") {
		t.Errorf("error = %v, want visible alias expansion limit", err)
	}
	if calls != 0 {
		t.Errorf("API called %d times for an excessive alias expansion", calls)
	}
}

package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

// orgKeyFixture is a made-up organization key, built from parts so no scanner
// takes it for one.
var orgKeyFixture = strings.Join([]string{"fixture", "nebius", "9f8e7d6c5b4a"}, "-")

const tokenFactoryURL = "https://api.tokenfactory.nebius.com/v1"

// aiKeyHQ is a fake HQ answering POST /api/mate/ai-key with status and body,
// recording the Authorization of each call.
type aiKeyHQ struct {
	srv   *httptest.Server
	mu    sync.Mutex
	calls []string
}

func newAIKeyHQ(t *testing.T, status int, body string) *aiKeyHQ {
	t.Helper()
	fake := &aiKeyHQ{}
	fake.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/api/mate/ai-key" {
			http.NotFound(w, r)
			return
		}
		fake.mu.Lock()
		fake.calls = append(fake.calls, r.Header.Get("Authorization"))
		fake.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(fake.srv.Close)
	return fake
}

func (f *aiKeyHQ) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// enrollAt keeps an enrollment of the Mate projectID at hqURL in a fresh
// directory, answering its path.
func enrollAt(t *testing.T, hqURL, projectID string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "enrollment.json")
	if err := hq.SaveEnrollment(path, hq.Enrollment{HQ: hqURL, ProjectID: projectID, Credential: "mate-cred"}); err != nil {
		t.Fatal(err)
	}
	return path
}

// aiKeyMock is a project whose app reads the key through run.envVariables
// and whose web reads nothing of it.
func aiKeyMock() *platform.Mock {
	runtime := func(id, name string) platform.ServiceStack {
		return platform.ServiceStack{ID: id, Name: name, Status: statusActive,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
			ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-" + name}}
	}
	return platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "tea-shop", Status: statusActive}).
		WithServices([]platform.ServiceStack{runtime("svc-app", "app"), runtime("svc-web", "web")}).
		WithAppVersionUserData("av-app", []platform.ServiceEnvVar{
			{Key: "OPENAI_API_KEY", Content: "${NEBIUS_API_KEY}"},
			{Key: "OPENAI_BASE_URL", Content: "${NEBIUS_BASE_URL}"},
		}).
		WithAppVersionUserData("av-web", []platform.ServiceEnvVar{{Key: "NODE_ENV", Content: "production"}}).
		WithProcess(&platform.Process{ID: "proc-projenvset", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "proc-restart-svc-app", Status: statusFinished})
}

const aiKeyAnswer = `{"provider":"nebius","env":"NEBIUS_API_KEY","value":"%s","baseUrl":"` + tokenFactoryURL + `"}`

func TestEnvAIKey_PutsTheOrganizationsKeyInTheSharedVaultWithoutShowingIt(t *testing.T) {
	t.Parallel()
	fake := newAIKeyHQ(t, http.StatusOK, strings.Replace(aiKeyAnswer, "%s", orgKeyFixture, 1))
	mock := aiKeyMock()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "zcp", withOrgAIKeyAt(fake.srv.Client(), enrollAt(t, fake.srv.URL, "proj-1")))

	result := callTool(t, srv, "zerops_env", map[string]any{"action": "ai-key"})
	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("ai-key failed: %s", text)
	}

	if got := fake.seen(); !reflect.DeepEqual(got, []string{"Mate mate-cred"}) {
		t.Errorf("HQ calls = %v, want one as the Mate", got)
	}
	want := []platform.CapturedProjectEnvCreate{
		{ProjectID: "proj-1", Key: "NEBIUS_API_KEY", Content: orgKeyFixture, Sensitive: true},
		{ProjectID: "proj-1", Key: "NEBIUS_BASE_URL", Content: tokenFactoryURL, Sensitive: false},
	}
	if !reflect.DeepEqual(mock.CapturedProjectEnvCreations, want) {
		t.Errorf("Shared vault writes = %+v, want %+v", mock.CapturedProjectEnvCreations, want)
	}
	if strings.Contains(text, orgKeyFixture) {
		t.Fatalf("the answer carries the key: %s", text)
	}

	var answer struct {
		Provider  string `json:"provider"`
		Vault     string `json:"vault"`
		Variables []struct {
			Key       string `json:"key"`
			Sensitive bool   `json:"isSensitive"`
		} `json:"variables"`
		RestartedServices []string `json:"restartedServices"`
		NextActions       string   `json:"nextActions"`
	}
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		t.Fatalf("parse %s: %v", text, err)
	}
	if answer.Provider != "nebius" || answer.Vault != "Shared" {
		t.Errorf("provider, vault = %q, %q", answer.Provider, answer.Vault)
	}
	names := make([]string, 0, len(answer.Variables))
	for _, v := range answer.Variables {
		names = append(names, v.Key)
	}
	if !reflect.DeepEqual(names, []string{"NEBIUS_API_KEY", "NEBIUS_BASE_URL"}) || !answer.Variables[0].Sensitive || answer.Variables[1].Sensitive {
		t.Errorf("variables = %+v, want NEBIUS_API_KEY sensitive and NEBIUS_BASE_URL plain", answer.Variables)
	}
	if !reflect.DeepEqual(answer.RestartedServices, []string{"app"}) {
		t.Errorf("restarted = %v, want the app that reads the key", answer.RestartedServices)
	}
	for _, want := range []string{"${NEBIUS_API_KEY}", "${NEBIUS_BASE_URL}", "run.envVariables"} {
		if !strings.Contains(answer.NextActions, want) {
			t.Errorf("nextActions lacks %q: %s", want, answer.NextActions)
		}
	}
}

func TestEnvAIKey_WithoutAKeyItSaysWhoSetsItAndWritesNothing(t *testing.T) {
	t.Parallel()
	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()

	for _, tc := range []struct {
		name string
		// status and body are HQ's answer to ai-key; status 0 is an HQ that
		// does not answer.
		status int
		body   string
		// enrolledAs is the project the enrollment holds; "" is none.
		enrolledAs string
		wantWords  []string
	}{
		{
			name:   "the organization keeps no key",
			status: http.StatusNotFound, body: `{"code":"no_key"}`, enrolledAs: "proj-1",
			wantWords: []string{"has no AI key yet", "Owner or Admin", "Settings › Zerops › HQ"},
		},
		{
			name:   "the Mate is not enrolled with HQ",
			status: http.StatusOK, body: strings.Replace(aiKeyAnswer, "%s", orgKeyFixture, 1),
			wantWords: []string{"no HQ enrollment yet"},
		},
		{
			name:   "the enrollment is another project's",
			status: http.StatusOK, body: strings.Replace(aiKeyAnswer, "%s", orgKeyFixture, 1), enrolledAs: "proj-other",
			wantWords: []string{"another project"},
		},
		{
			name:       "HQ does not answer",
			enrolledAs: "proj-1",
			wantWords:  []string{"HQ is not answering", "nothing was set"},
		},
		{
			name:   "HQ is not serving now",
			status: http.StatusServiceUnavailable, body: `{"code":"not_active"}`, enrolledAs: "proj-1",
			wantWords: []string{"HQ is not answering", "nothing was set"},
		},
		{
			name:   "HQ refuses",
			status: http.StatusForbidden, body: `{"code":"forbidden"}`, enrolledAs: "proj-1",
			wantWords: []string{"HQ refused", "nothing was set"},
		},
		{
			name:   "HQ answers a key that is not a plain value",
			status: http.StatusOK, body: strings.Replace(aiKeyAnswer, "%s", "<@generateRandomString(<32>)>"+orgKeyFixture, 1), enrolledAs: "proj-1",
			wantWords: []string{"nothing was set"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hqURL, httpClient := downURL, http.DefaultClient
			if tc.status != 0 {
				fake := newAIKeyHQ(t, tc.status, tc.body)
				hqURL, httpClient = fake.srv.URL, fake.srv.Client()
			}
			path := filepath.Join(t.TempDir(), "enrollment.json")
			if tc.enrolledAs != "" {
				path = enrollAt(t, hqURL, tc.enrolledAs)
			}
			mock := aiKeyMock()
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterEnv(srv, mock, "proj-1", "zcp", withOrgAIKeyAt(httpClient, path))

			result := callTool(t, srv, "zerops_env", map[string]any{"action": "ai-key"})
			text := getTextContent(t, result)
			if !result.IsError {
				t.Fatalf("ai-key succeeded: %s", text)
			}
			for _, want := range tc.wantWords {
				if !strings.Contains(text, want) {
					t.Errorf("answer lacks %q: %s", want, text)
				}
			}
			if strings.Contains(text, orgKeyFixture) {
				t.Errorf("the answer carries the key: %s", text)
			}
			if len(mock.CapturedProjectEnvCreations) != 0 {
				t.Errorf("vault writes = %+v, want none", mock.CapturedProjectEnvCreations)
			}
		})
	}
}

func TestEnvAIKey_OutsideAMateThereIsNoSuchAction(t *testing.T) {
	t.Parallel()
	if enum := envInputSchema(false).Properties["action"].Enum; slices.Contains(enum, any("ai-key")) {
		t.Errorf("action enum outside a Mate = %v, want no ai-key", enum)
	}
	if enum := envInputSchema(true).Properties["action"].Enum; !slices.Contains(enum, any("ai-key")) {
		t.Errorf("action enum in a Mate = %v, want ai-key", enum)
	}
	mock := aiKeyMock()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "zcp")

	result := callTool(t, srv, "zerops_env", map[string]any{"action": "ai-key"})
	if !result.IsError {
		t.Errorf("ai-key outside a Mate = %s, want it refused", getTextContent(t, result))
	}
	if len(mock.CapturedProjectEnvCreations) != 0 {
		t.Errorf("vault writes = %+v, want none", mock.CapturedProjectEnvCreations)
	}
}

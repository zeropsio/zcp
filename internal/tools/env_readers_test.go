package tools

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
)

// readersMock is a project of three runtimes and a database: api reads the
// Shared FOO and db's password, worker reads api's own TOKEN through
// `${api_TOKEN}`, web reads nothing, and zcp (the service running this
// session) reads FOO too.
func readersMock() *platform.Mock {
	runtime := func(id, name string) platform.ServiceStack {
		return platform.ServiceStack{ID: id, Name: name, Status: statusActive,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
			ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: "av-" + name}}
	}
	return platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "p", Status: statusActive}).
		WithServices([]platform.ServiceStack{
			runtime("svc-api", "api"),
			runtime("svc-worker", "worker"),
			runtime("svc-web", "web"),
			runtime("svc-zcp", "zcp"),
			{ID: "svc-db", Name: "db", Status: statusActive,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@17", ServiceStackTypeCategoryName: "STANDARD"}},
		}).
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{{ID: "u-tok", Key: "TOKEN", Content: "t", Sensitive: true}}).
		WithServiceEnv("svc-db", []platform.ServiceEnvVar{{ID: "u-pw", Key: "password", Content: "p"}}).
		WithAppVersionUserData("av-api", []platform.ServiceEnvVar{
			{Key: "FOO_URL", Content: "https://${FOO}/x"},
			{Key: "DB_PASS", Content: "${db_password}"},
		}).
		WithAppVersionUserData("av-worker", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "${api_TOKEN}"}}).
		WithAppVersionUserData("av-web", []platform.ServiceEnvVar{{Key: "NODE_ENV", Content: "production"}}).
		WithAppVersionUserData("av-zcp", []platform.ServiceEnvVar{{Key: "F", Content: "${FOO}"}}).
		WithProjectEnv([]platform.ProjectEnvVar{{ID: "pe-foo", Key: "FOO", Content: "old"}}).
		WithProcess(&platform.Process{ID: "proc-projenvset", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "proc-envset-svc-api", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "proc-envset-svc-web", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "proc-projenvdel-pe-foo", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "proc-restart-svc-api", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "proc-restart-svc-worker", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "proc-restart-svc-web", Status: statusFinished})
}

type envChangeWire struct {
	RestartedServices []string            `json:"restartedServices"`
	RestartWarnings   []string            `json:"restartWarnings"`
	RestartSkipped    bool                `json:"restartSkipped"`
	Readers           map[string][]string `json:"readers"`
	NextActions       string              `json:"nextActions"`
}

// TestEnvChange_RestartsOnlyReaders — a set or delete restarts the runtimes
// whose deployed run entries read the key (directly, through a chain, or as
// `${host_KEY}`), never one that does not reference it, never the service
// running this session; nothing reads it → no restart, and the response
// says how to make a service read it.
func TestEnvChange_RestartsOnlyReaders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		args          map[string]any
		mock          func(*platform.Mock) *platform.Mock
		wantRestarted []string
		wantReaders   map[string][]string
		wantNext      []string
		wantWarn      []string
		wantSkipped   bool
	}{
		{
			name:          "Shared key restarts its readers only",
			args:          map[string]any{"action": "set", "project": true, "variables": []any{"FOO=new"}},
			wantRestarted: []string{"api"},
			wantReaders:   map[string][]string{"FOO": {"api", "zcp"}},
			wantWarn:      []string{"zcp"},
		},
		{
			name:          "service key restarts the services that reference it",
			args:          map[string]any{"action": "set", "serviceHostname": "api", "variables": []any{"TOKEN=new"}},
			wantRestarted: []string{"worker"},
			wantReaders:   map[string][]string{"TOKEN": {"worker"}},
		},
		{
			name:        "nothing reads it — no restart, the fix in nextActions",
			args:        map[string]any{"action": "set", "project": true, "variables": []any{"UNUSED=x"}},
			wantReaders: map[string][]string{"UNUSED": nil},
			wantNext:    []string{"Nothing reads UNUSED yet", "NAME: ${UNUSED}", "deploy"},
		},
		{
			name:        "service key nothing reads names the host reference",
			args:        map[string]any{"action": "set", "serviceHostname": "web", "variables": []any{"SIGNING=x"}},
			wantReaders: map[string][]string{"SIGNING": nil},
			wantNext:    []string{"Nothing reads SIGNING yet", "web", "${web_SIGNING}"},
		},
		{
			name:          "delete restarts readers and warns they now read the literal",
			args:          map[string]any{"action": "delete", "project": true, "variables": []any{"FOO"}},
			wantRestarted: []string{"api"},
			wantReaders:   map[string][]string{"FOO": {"api", "zcp"}},
			wantWarn:      []string{"${FOO}", "literal"},
		},
		{
			name:        "skipRestart reports readers and restarts nothing",
			args:        map[string]any{"action": "set", "project": true, "variables": []any{"FOO=new"}, "skipRestart": true},
			wantReaders: map[string][]string{"FOO": {"api", "zcp"}},
			wantSkipped: true,
			wantNext:    []string{"api"},
		},
		{
			name: "a service whose rows cannot be read is restarted in case it reads",
			args: map[string]any{"action": "set", "project": true, "variables": []any{"FOO=new"}},
			mock: func(m *platform.Mock) *platform.Mock {
				return m.WithError("GetAppVersionUserData", errors.New("vpn down"))
			},
			wantRestarted: []string{"api", "web", "worker"},
			wantReaders:   map[string][]string{"FOO": nil},
			wantWarn:      []string{"could not read", "api"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := readersMock()
			if tt.mock != nil {
				mock = tt.mock(mock)
			}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterEnv(srv, mock, "proj-1", "zcp")
			args := map[string]any{}
			maps.Copy(args, tt.args)
			result := callTool(t, srv, "zerops_env", args)
			text := getTextContent(t, result)
			if result.IsError {
				t.Fatalf("unexpected IsError: %s", text)
			}
			var got envChangeWire
			if err := json.Unmarshal([]byte(text), &got); err != nil {
				t.Fatalf("parse result: %v", err)
			}
			slices.Sort(got.RestartedServices)
			if !slices.Equal(got.RestartedServices, tt.wantRestarted) {
				t.Errorf("restarted = %v, want %v (%s)", got.RestartedServices, tt.wantRestarted, text)
			}
			for key, want := range tt.wantReaders {
				readers, ok := got.Readers[key]
				if !ok {
					t.Errorf("readers lack %s: %s", key, text)
				}
				if !slices.Equal(readers, want) {
					t.Errorf("readers[%s] = %v, want %v", key, readers, want)
				}
			}
			for _, want := range tt.wantNext {
				if !strings.Contains(got.NextActions, want) {
					t.Errorf("nextActions miss %q: %s", want, got.NextActions)
				}
			}
			warns := strings.Join(got.RestartWarnings, "\n")
			for _, want := range tt.wantWarn {
				if !strings.Contains(warns, want) {
					t.Errorf("restartWarnings miss %q: %v", want, got.RestartWarnings)
				}
			}
			if got.RestartSkipped != tt.wantSkipped {
				t.Errorf("restartSkipped = %v, want %v", got.RestartSkipped, tt.wantSkipped)
			}
		})
	}
}

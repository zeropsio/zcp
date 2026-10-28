// Tests for: env_request.go — zerops_env action="request": a value only the
// person has is asked once, a wrong saved one replaced on the agent's word,
// and the answer names what restarts once the person saved it
// (docs/spec-mate.md §5.8, docs/spec-zerops-env-lifecycle.md).

package tools

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
)

// requestWire is the answer to action=request, as the agent and Mate read it.
type requestWire struct {
	Requested *struct {
		Key     string `json:"key"`
		Why     string `json:"why"`
		Replace bool   `json:"replace"`
	} `json:"requested"`
	AlreadySet       map[string]any `json:"alreadySet"`
	Readers          []string       `json:"readers"`
	RestartAfterSave []string       `json:"restartAfterSave"`
	NextActions      string         `json:"nextActions"`
}

func parseRequest(t *testing.T, result *mcp.CallToolResult) requestWire {
	t.Helper()
	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("request failed: %s", text)
	}
	var got requestWire
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	return got
}

// flipChangedAt is when the placeholder Mate set (08:46) last changed.
var flipChangedAt = time.Date(2026, 10, 25, 8, 46, 0, 0, time.UTC)

// flipMock is Filip's Shared vault: FLIP_GOOGLE_CLIENT_ID holds a placeholder.
func flipMock() *platform.Mock {
	return platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: statusActive}}).
		WithProjectEnv([]platform.ProjectEnvVar{{ID: "p-flip", Key: "FLIP_GOOGLE_CLIENT_ID", Content: "REDACTED", Sensitive: true, LastUpdate: flipChangedAt}})
}

func flipRequest(extra map[string]any) map[string]any {
	args := map[string]any{"action": "request", "project": true, "key": "FLIP_GOOGLE_CLIENT_ID",
		"label": "Google sign-in client ID", "why": "for Google sign-in on the preview", "sensitive": true}
	maps.Copy(args, extra)
	return args
}

// TestEnvTool_Request_AlreadySet_NamesHowToReplace: a key the vault holds is
// not asked for, as before — and the answer names the way to ask for a new
// value when the saved one is wrong (Filip 10-25, Kaďák 10-28: a placeholder
// and an invalid value read "already in the Shared vault; nothing was asked").
func TestEnvTool_Request_AlreadySet_NamesHowToReplace(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, flipMock(), "proj-1", "")

	got := parseRequest(t, callTool(t, srv, "zerops_env", flipRequest(nil)))
	if got.AlreadySet == nil || got.Requested != nil {
		t.Fatalf("want alreadySet and nothing asked, got %+v", got)
	}
	for _, want := range []string{"nothing was asked", "replace=true", "replaceWhy", "wrong"} {
		if !strings.Contains(got.NextActions, want) {
			t.Errorf("nextActions lacks %q: %s", want, got.NextActions)
		}
	}
}

// TestEnvTool_Request_Replace_AsksForTheHeldKey: replace=true with a
// replaceWhy asks the person for a key the vault holds; the why Mate shows
// carries what is wrong with the saved value. Nothing is written: the saved
// value stays until the person saves a new one.
func TestEnvTool_Request_Replace_AsksForTheHeldKey(t *testing.T) {
	t.Parallel()
	mock := flipMock()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "")

	result := callTool(t, srv, "zerops_env", flipRequest(map[string]any{"replace": true, "replaceWhy": "it is not in Google's format"}))
	got := parseRequest(t, result)
	if got.Requested == nil || got.AlreadySet != nil {
		t.Fatalf("want the held key requested, got %+v", got)
	}
	if !got.Requested.Replace {
		t.Error("requested.replace = false, want true")
	}
	if got.Requested.Why != "for Google sign-in on the preview — replacing the saved value: it is not in Google's format" {
		t.Errorf("requested.why = %q, want the why and what is wrong with the saved value", got.Requested.Why)
	}
	if !strings.Contains(got.NextActions, "saved value stays") {
		t.Errorf("nextActions does not say the saved value stays until the save: %s", got.NextActions)
	}
	if mock.CallCounts["CreateServiceEnvVar"] != 0 || len(mock.CapturedProjectEnvCreations) != 0 || mock.CallCounts["DeleteProjectEnv"] != 0 {
		t.Error("a replace request wrote to the platform")
	}
}

// TestEnvTool_Request_Replace_NeedsReplaceWhy: replace without the line the
// person reads asks nothing.
func TestEnvTool_Request_Replace_NeedsReplaceWhy(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, flipMock(), "proj-1", "")

	result := callTool(t, srv, "zerops_env", flipRequest(map[string]any{"replace": true}))
	text := getTextContent(t, result)
	if !result.IsError || !strings.Contains(text, "replaceWhy") {
		t.Fatalf("want a refusal naming replaceWhy, got: %s", text)
	}
}

// TestEnvTool_Request_OpenAsk_ShowsNoSecondPrompt: a request for a key and
// vault whose ask is still open asks nothing — the person already has the
// prompt (Filip 10-26 20:55: a second request showed a second prompt, and
// Lukáš saved each value twice). The refusal is an error, so neither Mate's
// engine nor its card opens an ask; it says when the open one was asked and
// the way to ask again after a decline. Another key, or the same key in
// another vault, is its own ask.
func TestEnvTool_Request_OpenAsk_ShowsNoSecondPrompt(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: statusActive}})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, mock, "proj-1", "", WithVaultAsks(t.TempDir()))

	if got := parseRequest(t, callTool(t, srv, "zerops_env", flipRequest(nil))); got.Requested == nil {
		t.Fatalf("the first request asked nothing: %+v", got)
	}
	for _, again := range []map[string]any{nil, {"replace": true, "replaceWhy": "it is not in Google's format"}} {
		result := callTool(t, srv, "zerops_env", flipRequest(again))
		text := getTextContent(t, result)
		if !result.IsError {
			t.Fatalf("a request for an open ask (%v) asked again: %s", again, text)
		}
		var wire ErrorWire
		if err := json.Unmarshal([]byte(text), &wire); err != nil {
			t.Fatalf("parse: %v\n%s", err, text)
		}
		if wire.Code != platform.ErrAlreadyAsked {
			t.Errorf("code = %s, want %s", wire.Code, platform.ErrAlreadyAsked)
		}
		for _, want := range []string{"FLIP_GOOGLE_CLIENT_ID", "still open", "UTC", "again=true", "declined"} {
			if !strings.Contains(text, want) {
				t.Errorf("the refusal lacks %q: %s", want, text)
			}
		}
		if strings.Contains(text, `"requested"`) {
			t.Errorf("the refusal carries a requested ask Mate would open: %s", text)
		}
	}

	if got := parseRequest(t, callTool(t, srv, "zerops_env", flipRequest(map[string]any{"key": "FLIP_GOOGLE_CLIENT_SECRET", "label": "Google sign-in client secret"}))); got.Requested == nil {
		t.Errorf("another key was not asked: %+v", got)
	}
	service := flipRequest(map[string]any{"serviceHostname": "api"})
	delete(service, "project")
	if got := parseRequest(t, callTool(t, srv, "zerops_env", service)); got.Requested == nil {
		t.Errorf("the same key in another vault was not asked: %+v", got)
	}
}

// TestEnvTool_Request_AnsweredAsk_AsksNoMore: once the vault holds the key
// written since the ask — the person saved it, or the agent set it — the ask
// is answered: a later request reads the vault as before. A value replaced in
// place keeps its row's id, so its time tells it was saved.
func TestEnvTool_Request_AnsweredAsk_AsksNoMore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		first platform.ProjectEnvVar
		ask   map[string]any
		saved platform.ProjectEnvVar
	}{
		{
			name:  "the person saved a value nobody held",
			ask:   flipRequest(nil),
			saved: platform.ProjectEnvVar{ID: "p-new", Key: "FLIP_GOOGLE_CLIENT_ID", Sensitive: true, LastUpdate: flipChangedAt.Add(time.Hour)},
		},
		{
			name:  "the person replaced the held value in place",
			first: platform.ProjectEnvVar{ID: "p-flip", Key: "FLIP_GOOGLE_CLIENT_ID", Sensitive: true, LastUpdate: flipChangedAt},
			ask:   flipRequest(map[string]any{"replace": true, "replaceWhy": "it is not in Google's format"}),
			saved: platform.ProjectEnvVar{ID: "p-flip", Key: "FLIP_GOOGLE_CLIENT_ID", Sensitive: true, LastUpdate: flipChangedAt.Add(time.Hour)},
		},
		{
			name:  "the agent set it itself",
			first: platform.ProjectEnvVar{ID: "p-flip", Key: "FLIP_GOOGLE_CLIENT_ID", Sensitive: true, LastUpdate: flipChangedAt},
			ask:   flipRequest(map[string]any{"replace": true, "replaceWhy": "it is not in Google's format"}),
			saved: platform.ProjectEnvVar{ID: "p-set", Key: "FLIP_GOOGLE_CLIENT_ID", Sensitive: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock()
			if tt.first.ID != "" {
				mock.WithProjectEnv([]platform.ProjectEnvVar{tt.first})
			}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterEnv(srv, mock, "proj-1", "", WithVaultAsks(t.TempDir()))

			if got := parseRequest(t, callTool(t, srv, "zerops_env", tt.ask)); got.Requested == nil {
				t.Fatalf("the ask asked nothing: %+v", got)
			}
			if result := callTool(t, srv, "zerops_env", flipRequest(nil)); !result.IsError {
				t.Fatalf("before the save, a second request asked: %s", getTextContent(t, result))
			}
			mock.WithProjectEnv([]platform.ProjectEnvVar{tt.saved})
			if got := parseRequest(t, callTool(t, srv, "zerops_env", flipRequest(nil))); got.AlreadySet == nil {
				t.Errorf("after the save, want alreadySet, got %+v", got)
			}
			if got := parseRequest(t, callTool(t, srv, "zerops_env", tt.ask)); got.Requested == nil && tt.ask["replace"] == true {
				t.Errorf("after the save, a replace asks anew; got %+v", got)
			}
		})
	}
}

// TestEnvTool_Request_Again_AsksAfterADecline: the person declined (a message
// said so), so no prompt is open: again=true asks once more.
func TestEnvTool_Request_Again_AsksAfterADecline(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, platform.NewMock(), "proj-1", "", WithVaultAsks(t.TempDir()))

	if got := parseRequest(t, callTool(t, srv, "zerops_env", flipRequest(nil))); got.Requested == nil {
		t.Fatalf("the first request asked nothing: %+v", got)
	}
	if got := parseRequest(t, callTool(t, srv, "zerops_env", flipRequest(map[string]any{"again": true}))); got.Requested == nil {
		t.Errorf("again=true asked nothing: %+v", got)
	}
	if result := callTool(t, srv, "zerops_env", flipRequest(nil)); !result.IsError {
		t.Errorf("the ask again=true opened is not open: %s", getTextContent(t, result))
	}
}

// TestEnvTool_Request_WithoutVaultAsks_AsksEachTime: outside a Mate nothing
// records an ask (WithVaultAsks is a Mate's), so every request answers as
// before.
func TestEnvTool_Request_WithoutVaultAsks_AsksEachTime(t *testing.T) {
	t.Parallel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterEnv(srv, platform.NewMock(), "proj-1", "")

	for i := range 2 {
		if got := parseRequest(t, callTool(t, srv, "zerops_env", flipRequest(nil))); got.Requested == nil {
			t.Errorf("request %d asked nothing: %+v", i+1, got)
		}
	}
}

// TestEnvTool_Request_NamesTheRestartAfterTheSave: the person's save writes
// the vault from their own client — zcp never sees it, and nothing restarts
// (Filip 10-26 20:56: "Neither has restarted yet"). So the answer names the
// services whose deployed entries read the key and the call that restarts
// each once a message says saved — never the service running this session,
// never a restart of its own. A key nothing reads says how a service reads it.
func TestEnvTool_Request_NamesTheRestartAfterTheSave(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        map[string]any
		wantReaders []string
		wantRestart []string
		wantNext    []string
	}{
		{
			name:        "a Shared key a runtime reads",
			args:        map[string]any{"action": "request", "project": true, "key": "STRIPE_SECRET_KEY", "label": "Stripe secret key", "why": "for checkout"},
			wantReaders: []string{"web"},
			wantRestart: []string{"web"},
			wantNext:    []string{"Saving restarts nothing", `zerops_manage action="restart" serviceHostname="web"`},
		},
		{
			name:        "a replaced Shared key this session's service reads too",
			args:        map[string]any{"action": "request", "project": true, "key": "FOO", "label": "Foo", "why": "for the API", "replace": true, "replaceWhy": "it is a placeholder"},
			wantReaders: []string{"api", "zcp"},
			wantRestart: []string{"api"},
			wantNext:    []string{`serviceHostname="api"`, "zcp", "runs this session"},
		},
		{
			name:        "a service's own key another service reads by its host",
			args:        map[string]any{"action": "request", "serviceHostname": "api", "key": "TOKEN", "label": "Token", "why": "for the worker", "replace": true, "replaceWhy": "it expired"},
			wantReaders: []string{"worker"},
			wantRestart: []string{"worker"},
			wantNext:    []string{`serviceHostname="worker"`},
		},
		{
			name:     "a key nothing reads yet",
			args:     map[string]any{"action": "request", "project": true, "key": "SENTRY_DSN", "label": "Sentry DSN", "why": "for error reports"},
			wantNext: []string{"Nothing reads SENTRY_DSN yet", "deploy"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := readersMock().
				WithAppVersionUserData("av-web", []platform.ServiceEnvVar{{Key: "STRIPE_KEY", Content: "${STRIPE_SECRET_KEY}"}})
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterEnv(srv, mock, "proj-1", "zcp")

			got := parseRequest(t, callTool(t, srv, "zerops_env", tt.args))
			if got.Requested == nil {
				t.Fatalf("nothing was asked: %+v", got)
			}
			if !slices.Equal(got.Readers, tt.wantReaders) {
				t.Errorf("readers = %v, want %v", got.Readers, tt.wantReaders)
			}
			if !slices.Equal(got.RestartAfterSave, tt.wantRestart) {
				t.Errorf("restartAfterSave = %v, want %v", got.RestartAfterSave, tt.wantRestart)
			}
			for _, want := range tt.wantNext {
				if !strings.Contains(got.NextActions, want) {
					t.Errorf("nextActions lacks %q: %s", want, got.NextActions)
				}
			}
			if strings.Contains(got.NextActions, `serviceHostname="zcp"`) {
				t.Errorf("nextActions names a restart of this session's own service: %s", got.NextActions)
			}
			if n := mock.CallCounts["RestartService"]; n != 0 {
				t.Errorf("a request restarted %d service(s) before anything was saved", n)
			}
		})
	}
}

// TestEnvSchema_RequestReplaceAndAgain: replace, replaceWhy and again are
// published with what each is for.
func TestEnvSchema_RequestReplaceAndAgain(t *testing.T) {
	t.Parallel()
	props := envInputSchema(false).Properties
	for name, want := range map[string]string{
		"replace":    "replaceWhy",
		"replaceWhy": "wrong with the saved value",
		"again":      "declined",
	} {
		prop, ok := props[name]
		if !ok || prop == nil {
			t.Errorf("%s is not published", name)
			continue
		}
		if !strings.Contains(prop.Description, want) {
			t.Errorf("%s description = %q, want it to name %q", name, prop.Description, want)
		}
	}
}
